package memory

import (
	"testing"
	"time"

	"github.com/ratrektlabs/rl-agent/provider"
)

func TestMemoryBackend_Interface(t *testing.T) {
	var _ MemoryBackend = (*InMemoryBackend)(nil)
}

func TestInMemoryBackend_Add(t *testing.T) {
	backend := NewInMemoryBackend()

	conv := &conversation{
		id: "conv-1",
		messages: []provider.Message{
			{Role: provider.RoleUser, Content: "Hello"},
			{Role: provider.RoleAssistant, Content: "Hi there!"},
			{Role: provider.RoleUser, Content: "How are you?"},
			{Role: provider.RoleAssistant, Content: "I can help with that"},
			{Role: provider.RoleUser, Content: "What is the weather?"},
			{Role: provider.RoleAssistant, Content: "The weather is sunny"},
		},
		createdAt: time.Now(),
	}

	ctx := context.Background()
	err := backend.AddConversation(ctx, conv.id, conv.messages)
	if err != nil {
		t.Fatalf("failed to add conversation: %v", err)
	}

	retrieved, err := backend.GetMessages(ctx, conv.id)
	if err != nil {
		t.Fatalf("failed to get messages: %v", err)
	}

	if len(retrieved) != len(conv.messages) {
		t.Errorf("expected %d messages, got %d", len(retrieved))
	}

	if retrieved[0].Content != "Hello" {
		t.Errorf("expected first message to be 'Hello', got %s", retrieved[0].Content)
	}
}
