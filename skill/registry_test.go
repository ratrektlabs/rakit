package skill_test

import (
	"context"
	"testing"

	"github.com/ratrektlabs/rakit/skill"
	"github.com/ratrektlabs/rakit/storage/metadata"
	"github.com/ratrektlabs/rakit/storage/metadata/sqlite"
)

func newStore(t *testing.T) metadata.Store {
	t.Helper()
	s, err := sqlite.NewStore(context.Background(), t.TempDir()+"/db.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestRegistryRegisterAndGet(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	reg := skill.NewRegistry(store)

	def := &skill.Definition{
		Name:         "web-search",
		Description:  "search the web",
		Version:      "1.0.0",
		Instructions: "use this to search the web",
		Tools: []skill.ToolDef{
			{
				Name:        "search",
				Description: "search query",
				Parameters:  map[string]any{"type": "object"},
				Handler:     "http",
				Endpoint:    "https://example.com",
			},
		},
		Resources: []skill.Resource{
			{Name: "template", Path: "res/tmpl.txt", Type: "file"},
		},
	}
	if err := reg.Register(ctx, def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	entries, err := reg.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "web-search" || !entries[0].Enabled {
		t.Fatalf("entries=%+v", entries)
	}

	got, err := reg.Get(ctx, "web-search")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Instructions != "use this to search the web" {
		t.Fatalf("instructions not round-tripped: %+v", got)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "search" {
		t.Fatalf("tools not round-tripped: %+v", got.Tools)
	}
	if len(got.Resources) != 1 || got.Resources[0].Path != "res/tmpl.txt" {
		t.Fatalf("resources not round-tripped: %+v", got.Resources)
	}
}

func TestRegistryEnableDisable(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	reg := skill.NewRegistry(store)

	_ = reg.Register(ctx, &skill.Definition{Name: "s1"})

	if err := reg.Disable(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	entries, _ := reg.List(ctx)
	if entries[0].Enabled {
		t.Fatal("skill should be disabled")
	}

	if err := reg.Enable(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	entries, _ = reg.List(ctx)
	if !entries[0].Enabled {
		t.Fatal("skill should be enabled")
	}
}

func TestRegistryUnregister(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	reg := skill.NewRegistry(store)

	_ = reg.Register(ctx, &skill.Definition{Name: "gone"})
	if err := reg.Unregister(ctx, "gone"); err != nil {
		t.Fatal(err)
	}
	entries, _ := reg.List(ctx)
	if len(entries) != 0 {
		t.Fatalf("entries=%+v want empty", entries)
	}
}
