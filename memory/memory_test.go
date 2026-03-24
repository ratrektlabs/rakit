package memory

import (
	"context"
	"testing"

	"github.com/ratrektlabs/rl-agent/provider"
)

func TestMemoryBackend_Interface(t *testing.T) {
	var _ MemoryBackend = (*InMemoryBackend)(nil)
}

func TestInMemoryBackend_Connect_Close(t *testing.T) {
	backend := NewInMemoryBackend()
	ctx := context.Background()

	if err := backend.Connect(ctx); err != nil {
		t.Errorf("Connect() error = %v", err)
	}

	if err := backend.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestInMemoryBackend_Memory(t *testing.T) {
	backend := NewInMemoryBackend()
	mem := backend.Memory()
	if mem == nil {
		t.Error("Memory() returned nil")
	}
}

func TestInMemory_Add(t *testing.T) {
	backend := NewInMemoryBackend()
	mem := backend.Memory()
	ctx := context.Background()

	entry := Entry{
		ID:      "msg-1",
		Role:    provider.RoleUser,
		Content: "Hello",
	}

	err := mem.Add(ctx, "user-1", "session-1", entry)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	entries, err := mem.Get(ctx, "user-1", "session-1", 0)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0].Content != "Hello" {
		t.Errorf("expected Content 'Hello', got %s", entries[0].Content)
	}

	if entries[0].Timestamp.IsZero() {
		t.Error("expected Timestamp to be set")
	}
}

func TestInMemory_Add_MissingID(t *testing.T) {
	backend := NewInMemoryBackend()
	mem := backend.Memory()
	ctx := context.Background()

	entry := Entry{ID: "msg-1", Role: provider.RoleUser, Content: "Hello"}

	if err := mem.Add(ctx, "", "session-1", entry); err != ErrMissingConversationID {
		t.Errorf("expected ErrMissingConversationID, got %v", err)
	}

	if err := mem.Add(ctx, "user-1", "", entry); err != ErrMissingConversationID {
		t.Errorf("expected ErrMissingConversationID, got %v", err)
	}
}

func TestInMemory_Get(t *testing.T) {
	backend := NewInMemoryBackend()
	mem := backend.Memory()
	ctx := context.Background()

	entries := []Entry{
		{ID: "msg-1", Role: provider.RoleUser, Content: "Hello"},
		{ID: "msg-2", Role: provider.RoleAssistant, Content: "Hi there!"},
		{ID: "msg-3", Role: provider.RoleUser, Content: "How are you?"},
	}

	for _, e := range entries {
		mem.Add(ctx, "user-1", "session-1", e)
	}

	got, err := mem.Get(ctx, "user-1", "session-1", 0)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if len(got) != 3 {
		t.Errorf("expected 3 entries, got %d", len(got))
	}
}

func TestInMemory_Get_WithLimit(t *testing.T) {
	backend := NewInMemoryBackend()
	mem := backend.Memory()
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		mem.Add(ctx, "user-1", "session-1", Entry{
			ID:      "msg-" + string(rune('0'+i)),
			Content: "Message",
		})
	}

	got, err := mem.Get(ctx, "user-1", "session-1", 5)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if len(got) != 5 {
		t.Errorf("expected 5 entries with limit, got %d", len(got))
	}
}

func TestInMemory_Get_MissingID(t *testing.T) {
	backend := NewInMemoryBackend()
	mem := backend.Memory()
	ctx := context.Background()

	_, err := mem.Get(ctx, "", "session-1", 0)
	if err != ErrMissingConversationID {
		t.Errorf("expected ErrMissingConversationID, got %v", err)
	}
}

func TestInMemory_Get_NonExistent(t *testing.T) {
	backend := NewInMemoryBackend()
	mem := backend.Memory()
	ctx := context.Background()

	got, err := mem.Get(ctx, "user-1", "session-1", 0)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if len(got) != 0 {
		t.Errorf("expected empty slice for non-existent session, got %d", len(got))
	}
}

func TestInMemory_Search(t *testing.T) {
	backend := NewInMemoryBackend()
	mem := backend.Memory()
	ctx := context.Background()

	entries := []Entry{
		{ID: "msg-1", Role: provider.RoleUser, Content: "Hello world"},
		{ID: "msg-2", Role: provider.RoleAssistant, Content: "Hi there!"},
		{ID: "msg-3", Role: provider.RoleUser, Content: "What is the weather?"},
		{ID: "msg-4", Role: provider.RoleAssistant, Content: "The weather is sunny"},
	}

	for _, e := range entries {
		mem.Add(ctx, "user-1", "session-1", e)
	}

	results, err := mem.Search(ctx, "user-1", "weather", 0)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results for 'weather', got %d", len(results))
	}
}

func TestInMemory_Search_WithLimit(t *testing.T) {
	backend := NewInMemoryBackend()
	mem := backend.Memory()
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		mem.Add(ctx, "user-1", "session-1", Entry{
			ID:      "msg-" + string(rune('0'+i)),
			Content: "test query match",
		})
	}

	results, err := mem.Search(ctx, "user-1", "test", 3)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(results) != 3 {
		t.Errorf("expected 3 results with limit, got %d", len(results))
	}
}

func TestInMemory_Clear(t *testing.T) {
	backend := NewInMemoryBackend()
	mem := backend.Memory()
	ctx := context.Background()

	mem.Add(ctx, "user-1", "session-1", Entry{Content: "Hello"})
	mem.Add(ctx, "user-1", "session-1", Entry{Content: "World"})

	got, _ := mem.Get(ctx, "user-1", "session-1", 0)
	if len(got) != 2 {
		t.Fatalf("expected 2 entries before clear, got %d", len(got))
	}

	err := mem.Clear(ctx, "user-1", "session-1")
	if err != nil {
		t.Fatalf("Clear() error = %v", err)
	}

	got, _ = mem.Get(ctx, "user-1", "session-1", 0)
	if len(got) != 0 {
		t.Errorf("expected 0 entries after clear, got %d", len(got))
	}
}

func TestInMemory_Clear_MissingID(t *testing.T) {
	backend := NewInMemoryBackend()
	mem := backend.Memory()
	ctx := context.Background()

	if err := mem.Clear(ctx, "", "session-1"); err != ErrMissingConversationID {
		t.Errorf("expected ErrMissingConversationID, got %v", err)
	}
}

func TestInMemory_MultipleSessions(t *testing.T) {
	backend := NewInMemoryBackend()
	mem := backend.Memory()
	ctx := context.Background()

	mem.Add(ctx, "user-1", "session-1", Entry{Content: "Session 1 message"})
	mem.Add(ctx, "user-1", "session-2", Entry{Content: "Session 2 message"})
	mem.Add(ctx, "user-2", "session-1", Entry{Content: "User 2 message"})

	s1, _ := mem.Get(ctx, "user-1", "session-1", 0)
	s2, _ := mem.Get(ctx, "user-1", "session-2", 0)
	u2, _ := mem.Get(ctx, "user-2", "session-1", 0)

	if len(s1) != 1 || s1[0].Content != "Session 1 message" {
		t.Error("session-1 retrieval failed")
	}
	if len(s2) != 1 || s2[0].Content != "Session 2 message" {
		t.Error("session-2 retrieval failed")
	}
	if len(u2) != 1 || u2[0].Content != "User 2 message" {
		t.Error("user-2 retrieval failed")
	}
}

func TestInMemory_Metadata(t *testing.T) {
	backend := NewInMemoryBackend()
	mem := backend.Memory()
	ctx := context.Background()

	entry := Entry{
		ID:      "msg-1",
		Role:    provider.RoleUser,
		Content: "Hello",
		Metadata: map[string]any{
			"source":  "api",
			"version": 1.0,
		},
	}

	mem.Add(ctx, "user-1", "session-1", entry)

	got, _ := mem.Get(ctx, "user-1", "session-1", 0)
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}

	if got[0].Metadata["source"] != "api" {
		t.Errorf("expected metadata source 'api', got %v", got[0].Metadata["source"])
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		s, substr string
		want      bool
	}{
		{"hello world", "hello", true},
		{"hello world", "world", true},
		{"hello world", "foo", false},
		{"hello", "hello", true},
		{"hi", "hello", false},
		{"", "", true},
		{"a", "", true},
		{"", "a", false},
	}

	for _, tt := range tests {
		got := contains(tt.s, tt.substr)
		if got != tt.want {
			t.Errorf("contains(%q, %q) = %v, want %v", tt.s, tt.substr, got, tt.want)
		}
	}
}
