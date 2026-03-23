package provider

import (
	"context"
	"errors"
	"testing"
	"time"
)

type MockProvider struct {
	name          string
	response      *CompletionResponse
	streamEvents  []StreamEvent
	err           error
	capabilities  ProviderCapabilities
	calledWith    CompletionRequest
	completeCalls int
	streamCalls   int
}

func NewMockProvider() *MockProvider {
	return &MockProvider{
		name: "mock",
		capabilities: ProviderCapabilities{
			SupportsStreaming:   true,
			SupportsToolCalling: true,
			SupportsVision:      false,
		},
	}
}

func (m *MockProvider) Name() string {
	return m.name
}

func (m *MockProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	m.completeCalls++
	m.calledWith = req
	if m.err != nil {
		return nil, m.err
	}
	if m.response != nil {
		return m.response, nil
	}
	return &CompletionResponse{
		ID:      "mock-id",
		Model:   req.Model,
		Content: "mock response",
	}, nil
}

func (m *MockProvider) Stream(ctx context.Context, req CompletionRequest) (<-chan StreamEvent, error) {
	m.streamCalls++
	m.calledWith = req
	if m.err != nil {
		return nil, m.err
	}
	out := make(chan StreamEvent, len(m.streamEvents)+1)
	go func() {
		defer close(out)
		for _, event := range m.streamEvents {
			select {
			case out <- event:
			case <-ctx.Done():
				return
			}
		}
		out <- StreamEvent{Type: StreamEventDone, FinishReason: "stop"}
	}()
	return out, nil
}

func (m *MockProvider) Capabilities() ProviderCapabilities {
	return m.capabilities
}

func (m *MockProvider) SetResponse(resp *CompletionResponse) {
	m.response = resp
}

func (m *MockProvider) SetStreamEvents(events []StreamEvent) {
	m.streamEvents = events
}

func (m *MockProvider) SetError(err error) {
	m.err = err
}

func TestProviderInterface(t *testing.T) {
	var _ Provider = NewMockProvider()
}

func TestMockProvider_Complete(t *testing.T) {
	ctx := context.Background()
	mock := NewMockProvider()
	mock.SetResponse(&CompletionResponse{
		ID:      "test-123",
		Model:   "gpt-4",
		Content: "Hello, world!",
	})
	req := CompletionRequest{
		Model:    "gpt-4",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}
	resp, err := mock.Complete(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello, world!" {
		t.Errorf("expected content 'Hello, world!', got '%s'", resp.Content)
	}
	if mock.completeCalls != 1 {
		t.Errorf("expected 1 complete call, got %d", mock.completeCalls)
	}
	if mock.calledWith.Model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got '%s'", mock.calledWith.Model)
	}
}

func TestMockProvider_Stream(t *testing.T) {
	ctx := context.Background()
	mock := NewMockProvider()
	mock.SetStreamEvents([]StreamEvent{
		{Type: StreamEventContentDelta, Delta: "Hello"},
		{Type: StreamEventContentDelta, Delta: " world"},
	})
	req := CompletionRequest{
		Model:    "gpt-4",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}
	events, err := mock.Stream(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var received []string
	for event := range events {
		if event.Type == StreamEventContentDelta {
			received = append(received, event.Delta)
		}
	}
	if len(received) != 2 {
		t.Errorf("expected 2 deltas, got %d", len(received))
	}
	if mock.streamCalls != 1 {
		t.Errorf("expected 1 stream call, got %d", mock.streamCalls)
	}
}

func TestMockProvider_Capabilities(t *testing.T) {
	mock := NewMockProvider()
	caps := mock.Capabilities()
	if !caps.SupportsStreaming {
		t.Error("expected SupportsStreaming to be true")
	}
	if !caps.SupportsToolCalling {
		t.Error("expected SupportsToolCalling to be true")
	}
}

func TestMockProvider_Error(t *testing.T) {
	ctx := context.Background()
	mock := NewMockProvider()
	testErr := errors.New("test error")
	mock.SetError(testErr)
	req := CompletionRequest{
		Model:    "gpt-4",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}
	_, err := mock.Complete(ctx, req)
	if err != testErr {
		t.Errorf("expected test error, got %v", err)
	}
}

func TestProviderError(t *testing.T) {
	pe := &ProviderError{
		Provider:    "openai",
		StatusCode:  429,
		Message:     "rate limit exceeded",
		RetryAfter:  30 * time.Second,
		IsRetryable: true,
	}
	if !pe.IsRetryable {
		t.Error("expected IsRetryable to be true")
	}
	if !errors.Is(pe, pe) {
		t.Error("error should match itself")
	}
	errMsg := pe.Error()
	if !contains(errMsg, "openai") || !contains(errMsg, "429") {
		t.Errorf("unexpected error message: %s", errMsg)
	}
}

func TestProviderError_Unwrap(t *testing.T) {
	innerErr := errors.New("inner error")
	pe := &ProviderError{
		Provider:   "openai",
		StatusCode: 500,
		Message:    "internal error",
		Err:        innerErr,
	}
	unwrapped := errors.Unwrap(pe)
	if unwrapped != innerErr {
		t.Errorf("expected inner error, got %v", unwrapped)
	}
}

func TestNewProviderError(t *testing.T) {
	pe := NewProviderError("openai", 500, "test error", nil)
	if pe.Provider != "openai" {
		t.Errorf("expected provider 'openai', got '%s'", pe.Provider)
	}
	if pe.StatusCode != 500 {
		t.Errorf("expected status 500, got %d", pe.StatusCode)
	}
	if !pe.IsRetryable {
		t.Error("expected 500 to be retryable")
	}
}

func TestNewRateLimitError(t *testing.T) {
	pe := NewRateLimitError("openai", 60*time.Second)
	if pe.StatusCode != 429 {
		t.Errorf("expected status 429, got %d", pe.StatusCode)
	}
	if pe.RetryAfter != 60*time.Second {
		t.Errorf("expected retry after 60s, got %v", pe.RetryAfter)
	}
	if !pe.IsRetryable {
		t.Error("expected rate limit to be retryable")
	}
}

func TestWrapError(t *testing.T) {
	innerErr := errors.New("inner error")
	wrapped := WrapError("openai", innerErr)
	pe, ok := wrapped.(*ProviderError)
	if !ok {
		t.Fatal("expected ProviderError")
	}
	if pe.Provider != "openai" {
		t.Errorf("expected provider 'openai', got '%s'", pe.Provider)
	}
	if !errors.Is(pe.Err, innerErr) {
		t.Error("expected inner error to be preserved")
	}
}

func TestWrapError_Nil(t *testing.T) {
	if WrapError("openai", nil) != nil {
		t.Error("expected nil for nil error")
	}
}

func TestWrapError_AlreadyProviderError(t *testing.T) {
	pe := &ProviderError{Provider: "anthropic", StatusCode: 500}
	wrapped := WrapError("openai", pe)
	if wrapped != pe {
		t.Error("expected same ProviderError to be returned")
	}
}

func TestStreamBuilder(t *testing.T) {
	ctx := context.Background()
	builder := NewStreamBuilder(ctx, 10)
	go func() {
		builder.EmitDelta("Hello")
		builder.EmitDelta(" world")
		builder.EmitDone("stop")
	}()
	var deltas []string
	var finishReason string
	for event := range builder.Channel() {
		switch event.Type {
		case StreamEventContentDelta:
			deltas = append(deltas, event.Delta)
		case StreamEventDone:
			finishReason = event.FinishReason
		}
	}
	if len(deltas) != 2 {
		t.Errorf("expected 2 deltas, got %d", len(deltas))
	}
	if finishReason != "stop" {
		t.Errorf("expected finish reason 'stop', got '%s'", finishReason)
	}
}

func TestStreamBuilder_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	builder := NewStreamBuilder(ctx, 10)
	cancel()
	ok := builder.EmitDelta("test")
	if ok {
		t.Error("expected EmitDelta to return false on cancelled context")
	}
}

func TestSafeStream(t *testing.T) {
	ctx := context.Background()
	inner := make(chan StreamEvent, 3)
	inner <- StreamEvent{Type: StreamEventContentDelta, Delta: "a"}
	inner <- StreamEvent{Type: StreamEventContentDelta, Delta: "b"}
	close(inner)
	out := SafeStream(ctx, inner, nil)
	var received []string
	for event := range out {
		if event.Type == StreamEventContentDelta {
			received = append(received, event.Delta)
		}
	}
	if len(received) != 2 {
		t.Errorf("expected 2 events, got %d", len(received))
	}
}

func TestSafeStream_WithError(t *testing.T) {
	ctx := context.Background()
	testErr := errors.New("test error")
	out := SafeStream(ctx, nil, testErr)
	event, ok := <-out
	if !ok {
		t.Fatal("expected event")
	}
	if event.Type != StreamEventError {
		t.Errorf("expected error event, got %s", event.Type)
	}
	if event.Error.Error() != "test error" {
		t.Errorf("expected 'test error', got '%v'", event.Error)
	}
}

func TestMessageTypes(t *testing.T) {
	if RoleSystem != "system" {
		t.Errorf("expected RoleSystem='system', got '%s'", RoleSystem)
	}
	if RoleUser != "user" {
		t.Errorf("expected RoleUser='user', got '%s'", RoleUser)
	}
	if RoleAssistant != "assistant" {
		t.Errorf("expected RoleAssistant='assistant', got '%s'", RoleAssistant)
	}
	if RoleTool != "tool" {
		t.Errorf("expected RoleTool='tool', got '%s'", RoleTool)
	}
}

func TestStreamEventTypes(t *testing.T) {
	tests := []struct {
		event  StreamEventType
		expect string
	}{
		{StreamEventContentStart, "content_start"},
		{StreamEventContentDelta, "content_delta"},
		{StreamEventContentEnd, "content_end"},
		{StreamEventToolCallStart, "tool_call_start"},
		{StreamEventToolCallDelta, "tool_call_delta"},
		{StreamEventToolCallEnd, "tool_call_end"},
		{StreamEventError, "error"},
		{StreamEventDone, "done"},
	}
	for _, tt := range tests {
		if string(tt.event) != tt.expect {
			t.Errorf("expected %s, got %s", tt.expect, tt.event)
		}
	}
}

func TestCompletionRequest(t *testing.T) {
	temp := 0.7
	req := CompletionRequest{
		Model:       "gpt-4",
		Messages:    []Message{{Role: RoleUser, Content: "test"}},
		Tools:       []ToolDefinition{{Type: "function", Function: ToolFunctionDef{Name: "test"}}},
		Temperature: &temp,
		MaxTokens:   100,
		Stream:      true,
	}
	if req.Model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got '%s'", req.Model)
	}
	if *req.Temperature != 0.7 {
		t.Errorf("expected temperature 0.7, got %f", *req.Temperature)
	}
	if len(req.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(req.Tools))
	}
}

func TestCompletionResponse(t *testing.T) {
	resp := CompletionResponse{
		ID:           "test-id",
		Model:        "gpt-4",
		Content:      "response content",
		FinishReason: "stop",
		ToolCalls:    []ToolCall{{ID: "tc-1", Name: "test", Arguments: map[string]any{"arg": "val"}}},
		Usage:        UsageStats{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
	if resp.ID != "test-id" {
		t.Errorf("expected ID 'test-id', got '%s'", resp.ID)
	}
	if len(resp.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("expected 15 total tokens, got %d", resp.Usage.TotalTokens)
	}
}

func TestToolCall(t *testing.T) {
	tc := ToolCall{
		ID:        "call-123",
		Name:      "get_weather",
		Arguments: map[string]any{"location": "NYC"},
	}
	if tc.ID != "call-123" {
		t.Errorf("expected ID 'call-123', got '%s'", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Errorf("expected name 'get_weather', got '%s'", tc.Name)
	}
	if tc.Arguments["location"] != "NYC" {
		t.Errorf("expected location 'NYC', got '%v'", tc.Arguments["location"])
	}
}

func TestToolDefinition(t *testing.T) {
	td := ToolDefinition{
		Type: "function",
		Function: ToolFunctionDef{
			Name:        "get_weather",
			Description: "Get weather info",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"location": map[string]any{"type": "string"},
				},
			},
		},
	}
	if td.Type != "function" {
		t.Errorf("expected type 'function', got '%s'", td.Type)
	}
	if td.Function.Name != "get_weather" {
		t.Errorf("expected name 'get_weather', got '%s'", td.Function.Name)
	}
}

func TestProviderCapabilities(t *testing.T) {
	caps := ProviderCapabilities{
		SupportsStreaming:   true,
		SupportsToolCalling: true,
		SupportsVision:      false,
	}
	if !caps.SupportsStreaming {
		t.Error("expected SupportsStreaming to be true")
	}
	if caps.SupportsVision {
		t.Error("expected SupportsVision to be false")
	}
}

func TestSC005_MockInterfaces(t *testing.T) {
	var p Provider = NewMockProvider()
	_ = p.Name()
	_ = p.Capabilities()
	_, _ = p.Complete(context.Background(), CompletionRequest{})
	_, _ = p.Stream(context.Background(), CompletionRequest{})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
