package metadata_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ratrektlabs/rakit/storage/metadata"
	"github.com/ratrektlabs/rakit/storage/metadata/jsonl"
	"github.com/ratrektlabs/rakit/storage/metadata/sqlite"
)

type storeFactory func(*testing.T) metadata.Store

func TestStoreConformance(t *testing.T) {
	factories := map[string]storeFactory{
		"sqlite": func(t *testing.T) metadata.Store {
			t.Helper()
			store, err := sqlite.NewStore(context.Background(), filepath.Join(t.TempDir(), "metadata.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			return store
		},
		"jsonl": func(t *testing.T) metadata.Store {
			t.Helper()
			store, err := jsonl.NewStore(context.Background(), jsonl.Config{
				RootDir:       t.TempDir(),
				WorkspacePath: filepath.Join(t.TempDir(), "workspace"),
			})
			if err != nil {
				t.Fatal(err)
			}
			return store
		},
	}

	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			store := factory(t)
			ctx := context.Background()
			sess, err := store.CreateSession(ctx, "agent", "user")
			if err != nil {
				t.Fatal(err)
			}
			if sess.Revision != 1 {
				t.Fatalf("new session revision=%d want 1", sess.Revision)
			}
			expectedRevision := sess.Revision
			sess.Messages = []metadata.Message{{ID: "m1", Role: "user", Content: "hello"}}
			sess.State = map[string]any{"ok": true}
			if err := store.UpdateSession(ctx, sess); err != nil {
				t.Fatal(err)
			}
			if sess.Revision != expectedRevision+1 {
				t.Fatalf("updated revision=%d want %d", sess.Revision, expectedRevision+1)
			}
			got, err := store.GetSession(ctx, sess.ID)
			if err != nil || got == nil || len(got.Messages) != 1 || got.State["ok"] != true {
				t.Fatalf("session round-trip: err=%v got=%+v", err, got)
			}
			if got.Revision != sess.Revision {
				t.Fatalf("round-trip revision=%d want %d", got.Revision, sess.Revision)
			}
			stale, err := store.GetSession(ctx, sess.ID)
			if err != nil {
				t.Fatal(err)
			}
			sess.Messages = append(sess.Messages, metadata.Message{ID: "m2", Role: "assistant", Content: "new"})
			if err := store.UpdateSession(ctx, sess); err != nil {
				t.Fatal(err)
			}
			stale.Messages = append(stale.Messages, metadata.Message{ID: "stale", Role: "user", Content: "stale"})
			if err := store.UpdateSession(ctx, stale); !errors.Is(err, metadata.ErrSessionConflict) {
				t.Fatalf("stale update err=%v", err)
			}
			current, err := store.GetSession(ctx, sess.ID)
			if err != nil || current == nil || len(current.Messages) != 2 || current.Messages[1].ID != "m2" {
				t.Fatalf("stale update changed session: err=%v current=%+v", err, current)
			}
			if got, err := store.GetSession(ctx, "missing"); err != nil || got != nil {
				t.Fatalf("missing session: err=%v got=%v", err, got)
			}
			if got, err := store.ListSessions(ctx, "agent"); err != nil || len(got) != 1 || len(got[0].Messages) != 0 {
				t.Fatalf("session summary: err=%v got=%v", err, got)
			}

			toolDef := &metadata.ToolDef{AgentID: "agent", Name: "tool", Parameters: map[string]any{"type": "object"}}
			if err := store.SaveTool(ctx, toolDef); err != nil {
				t.Fatal(err)
			}
			if got, err := store.GetTool(ctx, "tool"); err != nil || got == nil || got.ID == "" {
				t.Fatalf("tool: err=%v got=%+v", err, got)
			}
			if err := store.DeleteTool(ctx, "tool"); err != nil {
				t.Fatal(err)
			}

			if err := store.SaveSkill(ctx, &metadata.SkillDef{Name: "skill", Enabled: true}); err != nil {
				t.Fatal(err)
			}
			if got, err := store.ListSkills(ctx); err != nil || len(got) != 1 || got[0].Name != "skill" {
				t.Fatalf("skill: err=%v got=%v", err, got)
			}

			if err := store.SetMemory(ctx, metadata.ScopeAgent, "agent", "key", []byte{0, 255}); err != nil {
				t.Fatal(err)
			}
			if got, err := store.GetMemory(ctx, metadata.ScopeAgent, "agent", "key"); err != nil || string(got) != string([]byte{0, 255}) {
				t.Fatalf("memory: err=%v got=%v", err, got)
			}

			if err := store.SaveMCPServer(ctx, &metadata.MCPServerDef{AgentID: "agent", Name: "mcp", URL: "http://example.test"}); err != nil {
				t.Fatal(err)
			}
			if got, err := store.GetMCPServer(ctx, "mcp"); err != nil || got == nil || got.Transport != "http" {
				t.Fatalf("MCP: err=%v got=%+v", err, got)
			}
		})
	}
}
