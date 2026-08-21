package jsonl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ratrektlabs/rakit/storage/metadata"
)

func newTestStore(t *testing.T, workspace string) *Store {
	t.Helper()
	store, err := NewStore(context.Background(), Config{
		RootDir:       t.TempDir(),
		WorkspacePath: workspace,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func TestStoreImplementsAllMetadataOperations(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "/workspaces/rakit")

	sess, err := s.CreateSession(ctx, "agent-1", "user-a")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sess.ParentSessionID = "parent"
	sess.State = map[string]any{"counter": 1.0}
	sess.Messages = []metadata.Message{
		{ID: "m1", Role: "user", Content: "hello", CreatedAt: 1},
		{ID: "m2", Role: "assistant", Content: "hi", ToolCalls: []metadata.ToolCallRecord{
			{ID: "tc1", Name: "echo", Arguments: `{"x":1}`, Result: "ok", Status: "completed"},
		}, CreatedAt: 2},
	}
	if err := s.UpdateSession(ctx, sess); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}

	got, err := s.GetSession(ctx, sess.ID)
	if err != nil || got == nil {
		t.Fatalf("GetSession: err=%v got=%+v", err, got)
	}
	if got.ParentSessionID != "parent" || got.State["counter"] != 1.0 || len(got.Messages) != 2 {
		t.Fatalf("session round-trip wrong: %+v", got)
	}
	if got.Messages[1].ToolCalls[0].Result != "ok" {
		t.Fatalf("tool call round-trip wrong: %+v", got.Messages[1])
	}

	if summaries, err := s.ListSessions(ctx, "agent-1"); err != nil || len(summaries) != 1 || len(summaries[0].Messages) != 0 {
		t.Fatalf("ListSessions: err=%v summaries=%+v", err, summaries)
	}
	if summaries, err := s.ListSessionsByUser(ctx, "agent-1", "user-b"); err != nil || len(summaries) != 0 {
		t.Fatalf("ListSessionsByUser: err=%v summaries=%+v", err, summaries)
	}

	toolDef := &metadata.ToolDef{AgentID: "agent-1", Name: "calculate", Parameters: map[string]any{"type": "object"}}
	if err := s.SaveTool(ctx, toolDef); err != nil {
		t.Fatal(err)
	}
	toolID, toolCreated := toolDef.ID, toolDef.CreatedAt
	toolDef.Description = "updated"
	toolDef.ID = "caller-id-must-not-replace"
	toolDef.CreatedAt = 123
	if err := s.SaveTool(ctx, toolDef); err != nil {
		t.Fatal(err)
	}
	gotTool, err := s.GetTool(ctx, "calculate")
	if err != nil || gotTool == nil || gotTool.ID != toolID || gotTool.CreatedAt != toolCreated || gotTool.Description != "updated" {
		t.Fatalf("tool round-trip/retention wrong: err=%v tool=%+v", err, gotTool)
	}

	skillDef := &metadata.SkillDef{Name: "skill", Description: "desc", Version: "1", Instructions: "do", Enabled: true}
	if err := s.SaveSkill(ctx, skillDef); err != nil {
		t.Fatal(err)
	}
	entries, err := s.ListSkills(ctx)
	if err != nil || len(entries) != 1 || entries[0].Name != "skill" || !entries[0].Enabled {
		t.Fatalf("skills: err=%v entries=%+v", err, entries)
	}

	value := []byte{0, 1, 2, 255}
	if err := s.SetMemory(ctx, metadata.ScopeUser, "user-a", "binary", value); err != nil {
		t.Fatal(err)
	}
	value[0] = 99
	gotValue, err := s.GetMemory(ctx, metadata.ScopeUser, "user-a", "binary")
	if err != nil || string(gotValue) != string([]byte{0, 1, 2, 255}) {
		t.Fatalf("memory round-trip: err=%v value=%v", err, gotValue)
	}
	if err := s.SetMemory(ctx, metadata.ScopeUser, "user-a", "empty", []byte{}); err != nil {
		t.Fatal(err)
	}
	empty, err := s.GetMemory(ctx, metadata.ScopeUser, "user-a", "empty")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty memory round-trip: err=%v value=%v", err, empty)
	}
	if err := s.Set(ctx, "legacy", []byte("v")); err != nil {
		t.Fatal(err)
	}
	keys, err := s.List(ctx, "legacy")
	if err != nil || len(keys) != 1 || keys[0] != metadata.ScopedKey(metadata.ScopeGlobal, "", "legacy") {
		t.Fatalf("legacy list: err=%v keys=%v", err, keys)
	}

	server := &metadata.MCPServerDef{AgentID: "agent-1", Name: "remote", URL: "http://example.test", Enabled: true}
	if err := s.SaveMCPServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	gotServer, err := s.GetMCPServer(ctx, "remote")
	if err != nil || gotServer == nil || gotServer.Transport != "http" || gotServer.ID != server.ID {
		t.Fatalf("MCP round-trip: err=%v server=%+v", err, gotServer)
	}

	if err := s.DeleteTool(ctx, "calculate"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSkill(ctx, "skill"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMemory(ctx, metadata.ScopeUser, "user-a", "binary"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMCPServer(ctx, "remote"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSession(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSession(ctx, sess.ID); err != nil {
		t.Fatal("idempotent delete: ", err)
	}
	deleted, err := s.GetSession(ctx, sess.ID)
	if err != nil || deleted != nil {
		t.Fatalf("deleted session: err=%v session=%+v", err, deleted)
	}
}

func TestWorkspaceLayoutReopenAndIndexRebuild(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(t.TempDir(), "my project")
	s, err := NewStore(context.Background(), Config{RootDir: root, WorkspacePath: workspace})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := s.CreateSession(context.Background(), "a", "u")
	if err != nil {
		t.Fatal(err)
	}
	slug, hash := workspaceSlug(filepath.Clean(workspace))
	if filepath.Base(s.projectDir) != slug+"-"+hash {
		t.Fatalf("unexpected project directory: %s", s.projectDir)
	}
	path, _ := s.sessionPath(sess.ID)
	if runtime.GOOS != "windows" {
		if mode := fileMode(t, s.projectDir); mode.Perm() != 0o700 {
			t.Fatalf("project mode=%o want 700", mode.Perm())
		}
		if mode := fileMode(t, path); mode.Perm() != 0o600 {
			t.Fatalf("session mode=%o want 600", mode.Perm())
		}
	}
	if _, err := os.Stat(filepath.Join(s.storeDir, "session-index.jsonl")); err != nil {
		t.Fatalf("session index missing: %v", err)
	}
	if err := os.Remove(filepath.Join(s.storeDir, "session-index.jsonl")); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ListSessions(context.Background(), "a"); err != nil || len(got) != 1 {
		t.Fatalf("list after index removal: err=%v got=%v", err, got)
	}
	if _, err := os.Stat(filepath.Join(s.storeDir, "session-index.jsonl")); err != nil {
		t.Fatalf("index was not rebuilt: %v", err)
	}

	reopened, err := NewStore(context.Background(), Config{RootDir: root, WorkspacePath: workspace})
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.GetSession(context.Background(), sess.ID)
	if err != nil || got == nil || got.ID != sess.ID {
		t.Fatalf("reopen: err=%v session=%+v", err, got)
	}
}

func TestSessionConflictAndMonotonicRevision(t *testing.T) {
	s := newTestStore(t, "/workspaces/conflict")
	ctx := context.Background()
	sess, err := s.CreateSession(ctx, "a", "u")
	if err != nil {
		t.Fatal(err)
	}
	left, _ := s.GetSession(ctx, sess.ID)
	right, _ := s.GetSession(ctx, sess.ID)
	left.Messages = append(left.Messages, metadata.Message{ID: "left", Role: "user", Content: "left"})
	if err := s.UpdateSession(ctx, left); err != nil {
		t.Fatal(err)
	}
	if left.Revision != sess.Revision+1 {
		t.Fatalf("revision did not advance: old=%d new=%d", sess.Revision, left.Revision)
	}
	if got, err := s.GetSession(ctx, sess.ID); err != nil || got == nil || got.Revision != left.Revision {
		t.Fatalf("persisted revision: err=%v session=%+v", err, got)
	}
	right.Messages = append(right.Messages, metadata.Message{ID: "right", Role: "user", Content: "right"})
	err = s.UpdateSession(ctx, right)
	if !errors.Is(err, metadata.ErrSessionConflict) {
		t.Fatalf("stale update err=%v want ErrSessionConflict", err)
	}
}

func TestUpdateMissingSessionReturnsError(t *testing.T) {
	s := newTestStore(t, "/workspaces/missing")
	err := s.UpdateSession(context.Background(), &metadata.Session{ID: "missing", Revision: 0})
	if err == nil || errors.Is(err, metadata.ErrSessionConflict) {
		t.Fatalf("missing update err=%v", err)
	}
}

func TestLegacyTranscriptWithoutRevisionDefaultsToZero(t *testing.T) {
	s := newTestStore(t, "/workspaces/legacy-revision")
	ctx := context.Background()
	sess, err := s.CreateSession(ctx, "a", "u")
	if err != nil {
		t.Fatal(err)
	}
	path, err := s.sessionPath(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte(`"revision":1,`), nil, 1)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	legacy, err := s.GetSession(ctx, sess.ID)
	if err != nil || legacy == nil || legacy.Revision != 0 {
		t.Fatalf("legacy session: err=%v session=%+v", err, legacy)
	}
	legacy.Messages = []metadata.Message{{ID: "legacy-message", Role: "user", Content: "hello"}}
	if err := s.UpdateSession(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Revision != 1 {
		t.Fatalf("legacy update revision=%d want 1", legacy.Revision)
	}
}

func TestUpdateSessionProducesOneMetadataEventPerSnapshot(t *testing.T) {
	s := newTestStore(t, "/workspaces/event-count")
	ctx := context.Background()
	sess, err := s.CreateSession(ctx, "a", "u")
	if err != nil {
		t.Fatal(err)
	}
	sess.Messages = []metadata.Message{{ID: "m1", Role: "user", Content: "hello"}}
	if err := s.UpdateSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	path, err := s.sessionPath(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	records, err := readEnvelopes(path)
	if err != nil {
		t.Fatal(err)
	}
	var created, updated, messages int
	for _, record := range records {
		switch record.Type {
		case "session.created":
			created++
		case "session.updated":
			updated++
		case "message.upsert":
			messages++
		}
	}
	if created != 1 || updated != 1 || messages != 1 {
		t.Fatalf("event counts created=%d updated=%d messages=%d records=%+v", created, updated, messages, records)
	}
}

func TestReplayIgnoresUncommittedSessionTransaction(t *testing.T) {
	s := newTestStore(t, "/workspaces/uncommitted-transaction")
	ctx := context.Background()
	sess, err := s.CreateSession(ctx, "a", "u")
	if err != nil {
		t.Fatal(err)
	}
	path, err := s.sessionPath(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := newEnvelope("message.upsert", sess.ID, "orphan", metadata.Message{
		ID: "orphan", Role: "assistant", Content: "must stay invisible",
	})
	if err != nil {
		t.Fatal(err)
	}
	orphan.TransactionID = "txn-interrupted"
	if err := appendEnvelope(path, orphan); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 0 || got.Revision != sess.Revision {
		t.Fatalf("uncommitted transaction became visible: %+v", got)
	}

	got.Messages = append(got.Messages, metadata.Message{ID: "committed", Role: "user", Content: "visible"})
	if err := s.UpdateSession(ctx, got); err != nil {
		t.Fatal(err)
	}
	reloaded, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Messages) != 1 || reloaded.Messages[0].ID != "committed" {
		t.Fatalf("retry committed orphaned data: %+v", reloaded.Messages)
	}
}

func TestCommittedTranscriptSurvivesIndexFailure(t *testing.T) {
	s := newTestStore(t, "/workspaces/index-failure")
	ctx := context.Background()
	sess, err := s.CreateSession(ctx, "a", "u")
	if err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(s.storeDir, "session-index.jsonl")
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(indexPath, 0o700); err != nil {
		t.Fatal(err)
	}

	sess.Messages = append(sess.Messages, metadata.Message{ID: "m1", Role: "user", Content: "durable"})
	if err := s.UpdateSession(ctx, sess); err != nil {
		t.Fatalf("derived index failure escaped committed update: %v", err)
	}
	got, err := s.GetSession(ctx, sess.ID)
	if err != nil || got == nil || len(got.Messages) != 1 || got.Revision != sess.Revision {
		t.Fatalf("committed transcript lost: err=%v session=%+v", err, got)
	}
}

func TestDeleteCompletesAfterPersistedTombstone(t *testing.T) {
	s := newTestStore(t, "/workspaces/delete-recovery")
	ctx := context.Background()
	sess, err := s.CreateSession(ctx, "a", "u")
	if err != nil {
		t.Fatal(err)
	}
	path, err := s.sessionPath(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	tombstone, err := newEnvelope("session.deleted", sess.ID, "", map[string]string{"id": sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := appendEnvelope(path, tombstone); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteSession(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("tombstoned transcript still exists: %v", err)
	}
}

func TestTranscriptRepairsOnlyValidUnterminatedTail(t *testing.T) {
	s := newTestStore(t, "/workspaces/recovery")
	ctx := context.Background()
	sess, err := s.CreateSession(ctx, "a", "u")
	if err != nil {
		t.Fatal(err)
	}
	path, _ := s.sessionPath(sess.ID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatal("created transcript is not newline terminated")
	}
	if err := os.WriteFile(path, data[:len(data)-1], 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetSession(ctx, sess.ID); err != nil || got == nil {
		t.Fatalf("valid unterminated tail was not repaired: err=%v got=%v", err, got)
	}
	repaired, _ := os.ReadFile(path)
	if len(repaired) == 0 || repaired[len(repaired)-1] != '\n' {
		t.Fatal("unterminated tail was not repaired")
	}

	if f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600); err != nil {
		t.Fatal(err)
	} else {
		_, _ = f.WriteString("not-json\n")
		_ = f.Close()
	}
	if _, err := s.GetSession(ctx, sess.ID); err == nil || !strings.Contains(err.Error(), "malformed complete") {
		t.Fatalf("complete malformed record err=%v", err)
	}
}

func TestSessionPathRejectsTraversalAndUnsafeCharacters(t *testing.T) {
	s := newTestStore(t, "/workspaces/path-safety")
	for _, id := range []string{"../escape", "nested/session", "C:session", "wild*card"} {
		if _, err := s.GetSession(context.Background(), id); err == nil {
			t.Fatalf("GetSession(%q) accepted unsafe id", id)
		}
		if err := s.DeleteSession(context.Background(), id); err == nil {
			t.Fatalf("DeleteSession(%q) accepted unsafe id", id)
		}
	}
}

func TestUnknownEnvelopeVersionFails(t *testing.T) {
	s := newTestStore(t, "/workspaces/version")
	sess, err := s.CreateSession(context.Background(), "a", "u")
	if err != nil {
		t.Fatal(err)
	}
	path, _ := s.sessionPath(sess.ID)
	line, _ := json.Marshal(envelope{Version: 99, EventID: "future", Type: "session.updated", SessionID: sess.ID})
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write(append(line, '\n'))
	_ = f.Close()
	if _, err := s.GetSession(context.Background(), sess.ID); err == nil || !strings.Contains(err.Error(), "unsupported envelope version") {
		t.Fatalf("unknown version err=%v", err)
	}
}

func TestConcurrentStoresSerializeMutableWrites(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(t.TempDir(), "concurrent")
	first, err := NewStore(context.Background(), Config{RootDir: root, WorkspacePath: workspace})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewStore(context.Background(), Config{RootDir: root, WorkspacePath: workspace})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store := first
			if i%2 == 1 {
				store = second
			}
			key := "k-" + string(rune('a'+i))
			if err := store.Set(context.Background(), key, []byte(key)); err != nil {
				t.Errorf("Set(%q): %v", key, err)
			}
		}(i)
	}
	wg.Wait()
	keys, err := first.List(context.Background(), "k-")
	if err != nil || len(keys) != 20 {
		t.Fatalf("concurrent keys: err=%v len=%d keys=%v", err, len(keys), keys)
	}
}

func TestLockContextCancellation(t *testing.T) {
	s := newTestStore(t, "/workspaces/lock-cancel")
	release, err := storeLocks.acquire(context.Background(), s.projectDir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, err := s.GetSession(ctx, "missing"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock cancellation err=%v", err)
	}
}

func TestMutableLogCompaction(t *testing.T) {
	s := newTestStore(t, "/workspaces/compaction")
	payload := make([]byte, 1<<20)
	for i := range payload {
		payload[i] = byte(i)
	}
	for i := 0; i < 18; i++ {
		if err := s.Set(context.Background(), "large", payload); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(s.domainPath("memory"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() >= mutableCompactionBytes {
		t.Fatalf("mutable log was not compacted: size=%d", info.Size())
	}
	got, err := s.Get(context.Background(), "large")
	if err != nil || len(got) != len(payload) {
		t.Fatalf("compacted value: err=%v len=%d", err, len(got))
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()
}
