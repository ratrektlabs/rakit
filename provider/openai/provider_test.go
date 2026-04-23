package openai

import (
	"encoding/json"
	"testing"

	"github.com/ratrektlabs/rakit/provider"
)

// TestBuildParamsIncludesSystem ensures the system prompt is included as a
// leading system message so skills + compaction summaries reach the model.
func TestBuildParamsIncludesSystem(t *testing.T) {
	p := New("gpt-5.4-mini", "test-key")
	params := p.buildParams(&provider.Request{
		System: "you are helpful",
		Messages: []provider.Message{
			{Role: "user", Content: "hi"},
		},
	})
	if len(params.Messages) != 2 {
		t.Fatalf("want 2 messages (system + user), got %d", len(params.Messages))
	}
	raw, err := json.Marshal(params.Messages[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["role"] != "system" {
		t.Fatalf("first message role=%v want system: %+v", m["role"], m)
	}
	if m["content"] != "you are helpful" {
		t.Fatalf("system content=%v", m["content"])
	}
}

// TestBuildParamsSkipsEmptySystem ensures we don't send an empty system message.
func TestBuildParamsSkipsEmptySystem(t *testing.T) {
	p := New("gpt-5.4", "k")
	params := p.buildParams(&provider.Request{
		Messages: []provider.Message{{Role: "user", Content: "hi"}},
	})
	if len(params.Messages) != 1 {
		t.Fatalf("want 1 message, got %d", len(params.Messages))
	}
}

// TestAssistantWithToolCallsIsSingleMessage ensures that an assistant message
// with tool_calls is serialized as a single assistant message carrying the
// tool_calls array — not as a series of separate tool messages.
func TestAssistantWithToolCallsIsSingleMessage(t *testing.T) {
	msgs := toOpenAIMessages(provider.Message{
		Role:    "assistant",
		Content: "calling tools",
		ToolCalls: []provider.ToolCall{
			{ID: "call_1", Name: "add", Arguments: `{"a":1,"b":2}`},
			{ID: "call_2", Name: "mul", Arguments: `{"a":3,"b":4}`},
		},
	})
	if len(msgs) != 1 {
		t.Fatalf("want 1 assistant message, got %d", len(msgs))
	}
	raw, err := json.Marshal(msgs[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["role"] != "assistant" {
		t.Fatalf("role=%v want assistant", m["role"])
	}
	tcs, ok := m["tool_calls"].([]any)
	if !ok {
		t.Fatalf("tool_calls missing or wrong shape: %+v", m)
	}
	if len(tcs) != 2 {
		t.Fatalf("tool_calls len=%d want 2", len(tcs))
	}
	first, _ := tcs[0].(map[string]any)
	if first["id"] != "call_1" {
		t.Fatalf("first tool_call id=%v want call_1", first["id"])
	}
	fn, _ := first["function"].(map[string]any)
	if fn["name"] != "add" || fn["arguments"] != `{"a":1,"b":2}` {
		t.Fatalf("first tool_call function=%+v", fn)
	}
}

// TestToolMessageCarriesToolCallID ensures tool role messages preserve the
// tool_call_id so OpenAI can correlate results with their originating calls.
func TestToolMessageCarriesToolCallID(t *testing.T) {
	msgs := toOpenAIMessages(provider.Message{
		Role:    "tool",
		Content: `{"result": 3}`,
		ToolCalls: []provider.ToolCall{
			{ID: "call_1", Name: "add"},
		},
	})
	if len(msgs) != 1 {
		t.Fatalf("want 1 message, got %d", len(msgs))
	}
	raw, _ := json.Marshal(msgs[0])
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["role"] != "tool" {
		t.Fatalf("role=%v want tool", m["role"])
	}
	if m["tool_call_id"] != "call_1" {
		t.Fatalf("tool_call_id=%v want call_1", m["tool_call_id"])
	}
	if m["content"] != `{"result": 3}` {
		t.Fatalf("content=%v", m["content"])
	}
}

func TestUserAndSystemRoundTrip(t *testing.T) {
	msgs := toOpenAIMessages(provider.Message{Role: "user", Content: "hi"})
	if len(msgs) != 1 {
		t.Fatalf("user: want 1 message, got %d", len(msgs))
	}
	raw, _ := json.Marshal(msgs[0])
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["role"] != "user" || m["content"] != "hi" {
		t.Fatalf("user round-trip: %+v", m)
	}

	msgs = toOpenAIMessages(provider.Message{Role: "system", Content: "sys"})
	raw, _ = json.Marshal(msgs[0])
	m = nil
	_ = json.Unmarshal(raw, &m)
	if m["role"] != "system" || m["content"] != "sys" {
		t.Fatalf("system round-trip: %+v", m)
	}
}

func TestToolSchemaAttached(t *testing.T) {
	p := New("gpt-5.4", "k")
	params := p.buildParams(&provider.Request{
		Tools: []provider.Tool{{
			Name:        "add",
			Description: "adds",
			Parameters:  map[string]any{"type": "object"},
		}},
	})
	if len(params.Tools) != 1 {
		t.Fatalf("tools len=%d want 1", len(params.Tools))
	}
}

func TestToOpenAIParamsNilSafe(t *testing.T) {
	if toOpenAIParams(nil) != nil {
		t.Fatal("nil schema should stay nil")
	}
	if p := toOpenAIParams(map[string]any{"type": "object"}); p == nil {
		t.Fatal("non-nil schema produced nil params")
	}
}

func TestProviderMetadata(t *testing.T) {
	p := New("m", "k")
	if p.Name() != "openai" {
		t.Fatal("name")
	}
	if p.Model() != "m" {
		t.Fatal("model")
	}
	p.SetModel("m2")
	if p.Model() != "m2" {
		t.Fatal("SetModel")
	}
	if len(p.Models()) == 0 {
		t.Fatal("Models empty")
	}
}
