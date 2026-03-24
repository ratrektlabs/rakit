package agent

import (
	"context"
	"testing"

	"github.com/ratrektlabs/rl-agent/provider"
	"github.com/ratrektlabs/rl-agent/tool"
)

func TestDefaultConfig(t *testing.T) {
	mockProvider := &mockProvider{}
	cfg := DefaultConfig(mockProvider)

	if cfg.Provider == nil {
		t.Error("expected provider to be set")
	}
	if cfg.MaxSteps != 10 {
		t.Errorf("expected MaxSteps 10, got %d", cfg.MaxSteps)
	}
	if cfg.MaxTokens != 4096 {
		t.Errorf("expected MaxTokens 4096, got %d", cfg.MaxTokens)
	}
	if cfg.Temperature == nil || *cfg.Temperature != 0.7 {
		t.Errorf("expected Temperature 0.7, got %v", cfg.Temperature)
	}
}

func TestNew(t *testing.T) {
	mockProvider := &mockProvider{}
	cfg := DefaultConfig(mockProvider)
	ag := New(cfg)

	if ag == nil {
		t.Fatal("expected agent to be created")
	}
}

func TestNew_DefaultValues(t *testing.T) {
	mockProvider := &mockProvider{}
	cfg := &Config{
		Provider: mockProvider,
		MaxSteps: 0,
	}
	ag := New(cfg)

	if ag == nil {
		t.Fatal("expected agent to be created")
	}
}

func TestAgent_AddTool(t *testing.T) {
	mockProvider := &mockProvider{}
	ag := New(DefaultConfig(mockProvider))

	testTool := &mockTool{}
	err := ag.AddTool(testTool)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAgent_AddSkill(t *testing.T) {
	mockProvider := &mockProvider{}
	ag := New(DefaultConfig(mockProvider))

	skill := &mockSkill{}
	err := ag.AddSkill(skill)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFinishReason_Constants(t *testing.T) {
	tests := []struct {
		reason   FinishReason
		expected string
	}{
		{FinishReasonStop, "stop"},
		{FinishReasonToolCalls, "tool_calls"},
		{FinishReasonMaxSteps, "max_steps"},
		{FinishReasonError, "error"},
		{FinishReasonCancelled, "cancelled"},
	}

	for _, tt := range tests {
		if string(tt.reason) != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, tt.reason)
		}
	}
}

func TestStreamEventType_Constants(t *testing.T) {
	tests := []struct {
		eventType StreamEventType
		expected  string
	}{
		{StreamEventContentDelta, "content_delta"},
		{StreamEventToolCallStart, "tool_call_start"},
		{StreamEventToolCallEnd, "tool_call_end"},
		{StreamEventToolResult, "tool_result"},
		{StreamEventStepStart, "step_start"},
		{StreamEventStepEnd, "step_end"},
		{StreamEventError, "error"},
		{StreamEventDone, "done"},
	}

	for _, tt := range tests {
		if string(tt.eventType) != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, tt.eventType)
		}
	}
}

func TestRunResult_Struct(t *testing.T) {
	result := &RunResult{
		Content:      "test content",
		ToolCalls:    []provider.ToolCall{{ID: "1", Name: "test"}},
		FinishReason: FinishReasonStop,
		Steps:        3,
		Usage:        provider.UsageStats{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
	}

	if result.Content != "test content" {
		t.Errorf("expected 'test content', got %s", result.Content)
	}
	if len(result.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.FinishReason != FinishReasonStop {
		t.Errorf("expected FinishReasonStop, got %s", result.FinishReason)
	}
	if result.Steps != 3 {
		t.Errorf("expected 3 steps, got %d", result.Steps)
	}
}

func TestStreamEvent_Struct(t *testing.T) {
	event := StreamEvent{
		Type:  StreamEventContentDelta,
		Delta: "hello",
		Step:  1,
	}

	if event.Type != StreamEventContentDelta {
		t.Errorf("expected StreamEventContentDelta, got %s", event.Type)
	}
	if event.Delta != "hello" {
		t.Errorf("expected 'hello', got %s", event.Delta)
	}
}

func TestToolResultEvent_Struct(t *testing.T) {
	event := &ToolResultEvent{
		Name:   "calculator",
		Input:  map[string]any{"expr": "1+1"},
		Output: 2,
	}

	if event.Name != "calculator" {
		t.Errorf("expected 'calculator', got %s", event.Name)
	}
	if event.Output != 2 {
		t.Errorf("expected 2, got %v", event.Output)
	}
}

type mockProvider struct{}

func (m *mockProvider) Name() string { return "mock" }
func (m *mockProvider) Complete(ctx context.Context, req provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return &provider.CompletionResponse{Content: "ok"}, nil
}
func (m *mockProvider) Stream(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent)
	close(ch)
	return ch, nil
}
func (m *mockProvider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{}
}

type mockTool struct{}

func (m *mockTool) Name() string        { return "mock_tool" }
func (m *mockTool) Description() string { return "A mock tool" }
func (m *mockTool) Parameters() map[string]tool.ParameterSchema {
	return map[string]tool.ParameterSchema{}
}
func (m *mockTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return "mock result", nil
}

type mockSkill struct{}

func (m *mockSkill) Name() string        { return "mock_skill" }
func (m *mockSkill) Description() string { return "A mock skill" }
func (m *mockSkill) Prompt() string      { return "You are a helpful assistant" }
func (m *mockSkill) Tools() []tool.Tool  { return nil }
