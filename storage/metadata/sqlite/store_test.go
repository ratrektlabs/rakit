package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ratrektlabs/rakit/storage/metadata"
	"github.com/ratrektlabs/rakit/storage/metadata/sqlite"
)

func newTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.NewStore(context.Background(), filepath.Join(dir, "rakit.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestSessionsCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	sess, err := s.CreateSession(ctx, "agent-1", "user-a")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.ID == "" || sess.AgentID != "agent-1" || sess.UserID != "user-a" {
		t.Fatalf("unexpected session: %+v", sess)
	}

	sess.State = map[string]any{"counter": 1.0}
	sess.Messages = []metadata.Message{
		{ID: "m1", Role: "user", Content: "hello", CreatedAt: 1},
		{
			ID:      "m2",
			Role:    "assistant",
			Content: "hi",
			ToolCalls: []metadata.ToolCallRecord{
				{ID: "tc1", Name: "echo", Arguments: `{"x":1}`, Status: "completed"},
			},
			CreatedAt: 2,
		},
	}
	if err := s.UpdateSession(ctx, sess); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}

	got, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("msgs=%d want 2", len(got.Messages))
	}
	if got.State["counter"] != 1.0 {
		t.Fatalf("state counter=%v", got.State["counter"])
	}
	if len(got.Messages[1].ToolCalls) != 1 || got.Messages[1].ToolCalls[0].ID != "tc1" {
		t.Fatalf("tool call not round-tripped: %+v", got.Messages[1])
	}

	// ListSessions
	sessions, err := s.ListSessions(ctx, "agent-1")
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("ListSessions len=%d", len(sessions))
	}

	// ListSessionsByUser
	byUser, err := s.ListSessionsByUser(ctx, "agent-1", "user-a")
	if err != nil {
		t.Fatalf("ListSessionsByUser: %v", err)
	}
	if len(byUser) != 1 {
		t.Fatalf("byUser len=%d", len(byUser))
	}
	other, err := s.ListSessionsByUser(ctx, "agent-1", "user-b")
	if err != nil {
		t.Fatalf("ListSessionsByUser(other): %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("other-user sessions=%d want 0", len(other))
	}

	// DeleteSession
	if err := s.DeleteSession(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetSession(ctx, sess.ID)
	if got != nil {
		t.Fatalf("session not deleted: %+v", got)
	}
}

func TestSessionNotFoundReturnsNil(t *testing.T) {
	s := newTestStore(t)
	got, err := s.GetSession(context.Background(), "does-not-exist")
	if err != nil {
		t.Fatalf("err=%v want nil", err)
	}
	if got != nil {
		t.Fatalf("want nil session, got %+v", got)
	}
}

func TestToolsCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	td := &metadata.ToolDef{
		AgentID:     "agent-1",
		Name:        "calculate",
		Description: "desc",
		Parameters:  map[string]any{"type": "object"},
		Handler:     "http",
		Endpoint:    "https://api.example.com",
		Headers:     map[string]string{"X-Key": "abc"},
	}
	if err := s.SaveTool(ctx, td); err != nil {
		t.Fatalf("SaveTool: %v", err)
	}

	got, err := s.GetTool(ctx, "calculate")
	if err != nil || got == nil {
		t.Fatalf("GetTool: err=%v got=%+v", err, got)
	}
	if got.Endpoint != "https://api.example.com" || got.Headers["X-Key"] != "abc" {
		t.Fatalf("round-trip wrong: %+v", got)
	}

	tools, err := s.ListTools(ctx, "agent-1")
	if err != nil || len(tools) != 1 {
		t.Fatalf("ListTools: %v, len=%d", err, len(tools))
	}

	if err := s.DeleteTool(ctx, "calculate"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetTool(ctx, "calculate")
	if got != nil {
		t.Fatal("tool not deleted")
	}
}

func TestSkillsCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	def := &metadata.SkillDef{
		Name:         "sk1",
		Description:  "desc",
		Version:      "1.0.0",
		Instructions: "do stuff",
		Tools:        []any{map[string]any{"name": "t"}},
		Config:       map[string]any{"k": "v"},
		Resources:    []any{map[string]any{"path": "r.txt"}},
		Enabled:      true,
	}
	if err := s.SaveSkill(ctx, def); err != nil {
		t.Fatalf("SaveSkill: %v", err)
	}

	got, err := s.GetSkill(ctx, "sk1")
	if err != nil || got == nil {
		t.Fatalf("GetSkill: %v", err)
	}
	if !got.Enabled || got.Version != "1.0.0" {
		t.Fatalf("skill round-trip wrong: %+v", got)
	}

	entries, err := s.ListSkills(ctx)
	if err != nil || len(entries) != 1 {
		t.Fatalf("ListSkills: %v, len=%d", err, len(entries))
	}
	if entries[0].Name != "sk1" || !entries[0].Enabled {
		t.Fatalf("entry wrong: %+v", entries[0])
	}

	// Disable via Save(enabled=false)
	def.Enabled = false
	if err := s.SaveSkill(ctx, def); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetSkill(ctx, "sk1")
	if got.Enabled {
		t.Fatal("skill should be disabled")
	}

	if err := s.DeleteSkill(ctx, "sk1"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetSkill(ctx, "sk1")
	if got != nil {
		t.Fatal("skill not deleted")
	}
}

func TestScopedMemory(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Distinct scopes do not collide.
	if err := s.SetMemory(ctx, metadata.ScopeAgent, "a1", "k", []byte("agent-val")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMemory(ctx, metadata.ScopeUser, "u1", "k", []byte("user-val")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMemory(ctx, metadata.ScopeGlobal, "", "k", []byte("global-val")); err != nil {
		t.Fatal(err)
	}

	agentVal, _ := s.GetMemory(ctx, metadata.ScopeAgent, "a1", "k")
	userVal, _ := s.GetMemory(ctx, metadata.ScopeUser, "u1", "k")
	globalVal, _ := s.GetMemory(ctx, metadata.ScopeGlobal, "", "k")
	if string(agentVal) != "agent-val" || string(userVal) != "user-val" || string(globalVal) != "global-val" {
		t.Fatalf("scope collision: agent=%q user=%q global=%q", agentVal, userVal, globalVal)
	}

	// Missing key returns nil, nil.
	val, err := s.GetMemory(ctx, metadata.ScopeAgent, "a1", "missing")
	if err != nil {
		t.Fatalf("GetMemory(missing) err=%v", err)
	}
	if val != nil {
		t.Fatalf("GetMemory(missing) val=%q want nil", val)
	}

	// List honors prefix and scope.
	_ = s.SetMemory(ctx, metadata.ScopeAgent, "a1", "prefix/one", []byte("1"))
	_ = s.SetMemory(ctx, metadata.ScopeAgent, "a1", "prefix/two", []byte("2"))
	_ = s.SetMemory(ctx, metadata.ScopeAgent, "a1", "other", []byte("x"))
	keys, err := s.ListMemory(ctx, metadata.ScopeAgent, "a1", "prefix/")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("ListMemory len=%d want 2: %v", len(keys), keys)
	}

	// Delete
	if err := s.DeleteMemory(ctx, metadata.ScopeAgent, "a1", "k"); err != nil {
		t.Fatal(err)
	}
	val, _ = s.GetMemory(ctx, metadata.ScopeAgent, "a1", "k")
	if val != nil {
		t.Fatal("DeleteMemory failed")
	}
}

func TestLegacyKVDelegatesToGlobalScope(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.Set(ctx, "legacy", []byte("v")); err != nil {
		t.Fatal(err)
	}
	// Reading via SetMemory scope should find the same value.
	got, err := s.GetMemory(ctx, metadata.ScopeGlobal, "", "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v" {
		t.Fatalf("legacy KV not mirrored to global scope: got=%q", got)
	}
}

func TestMCPServersCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	srv := &metadata.MCPServerDef{
		AgentID:   "agent-1",
		Name:      "remote",
		URL:       "http://example.com/mcp",
		Transport: "http",
		Enabled:   true,
	}
	if err := s.SaveMCPServer(ctx, srv); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetMCPServer(ctx, "remote")
	if err != nil || got == nil {
		t.Fatalf("GetMCPServer err=%v got=%+v", err, got)
	}
	if got.URL != "http://example.com/mcp" {
		t.Fatalf("round-trip wrong: %+v", got)
	}

	// Default transport assignment on save if empty.
	srv2 := &metadata.MCPServerDef{AgentID: "agent-1", Name: "remote2", URL: "x", Enabled: false}
	if err := s.SaveMCPServer(ctx, srv2); err != nil {
		t.Fatal(err)
	}
	got2, _ := s.GetMCPServer(ctx, "remote2")
	if got2.Transport != "http" {
		t.Fatalf("default transport not applied: %q", got2.Transport)
	}

	list, err := s.ListMCPServers(ctx, "agent-1")
	if err != nil || len(list) != 2 {
		t.Fatalf("ListMCPServers err=%v len=%d", err, len(list))
	}

	if err := s.DeleteMCPServer(ctx, "remote"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetMCPServer(ctx, "remote")
	if got != nil {
		t.Fatal("server not deleted")
	}
}
