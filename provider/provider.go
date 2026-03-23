package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

type MessageRole string

const (
	RoleSystem    MessageRole = "system"
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleTool      MessageRole = "tool"
)

type StreamEventType string

const (
	StreamEventContentStart  StreamEventType = "content_start"
	StreamEventContentDelta  StreamEventType = "content_delta"
	StreamEventContentEnd    StreamEventType = "content_end"
	StreamEventToolCallStart StreamEventType = "tool_call_start"
	StreamEventToolCallDelta StreamEventType = "tool_call_delta"
	StreamEventToolCallEnd   StreamEventType = "tool_call_end"
	StreamEventError         StreamEventType = "error"
	StreamEventDone          StreamEventType = "done"
)

type Message struct {
	Role       MessageRole `json:"role"`
	Content    string      `json:"content,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	Name       string      `json:"name,omitempty"`
}

type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type ToolDefinition struct {
	Type     string          `json:"type"`
	Function ToolFunctionDef `json:"function"`
}

type ToolFunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type ProviderCapabilities struct {
	SupportsStreaming   bool `json:"supports_streaming"`
	SupportsToolCalling bool `json:"supports_tool_calling"`
	SupportsVision      bool `json:"supports_vision"`
}

type CompletionRequest struct {
	Model       string           `json:"model"`
	Messages    []Message        `json:"messages"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	Temperature *float64         `json:"temperature,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Stream      bool             `json:"stream,omitempty"`
}

type CompletionResponse struct {
	ID           string     `json:"id"`
	Model        string     `json:"model"`
	Content      string     `json:"content"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string     `json:"finish_reason"`
	Usage        UsageStats `json:"usage,omitempty"`
}

type UsageStats struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type StreamEvent struct {
	Type         StreamEventType `json:"type"`
	Delta        string          `json:"delta,omitempty"`
	ToolCall     *ToolCall       `json:"tool_call,omitempty"`
	Error        error           `json:"error,omitempty"`
	FinishReason string          `json:"finish_reason,omitempty"`
}

type Provider interface {
	Name() string
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
	Stream(ctx context.Context, req CompletionRequest) (<-chan StreamEvent, error)
	Capabilities() ProviderCapabilities
}

type ProviderError struct {
	Provider    string
	StatusCode  int
	Message     string
	RetryAfter  time.Duration
	IsRetryable bool
	Err         error
}

func (e *ProviderError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("provider %s: %s (status %d): %v", e.Provider, e.Message, e.StatusCode, e.Err)
	}
	return fmt.Sprintf("provider %s: %s (status %d)", e.Provider, e.Message, e.StatusCode)
}

func (e *ProviderError) Unwrap() error {
	return e.Err
}

func NewProviderError(provider string, statusCode int, message string, err error) *ProviderError {
	return &ProviderError{
		Provider:    provider,
		StatusCode:  statusCode,
		Message:     message,
		IsRetryable: isRetryableStatus(statusCode),
		Err:         err,
	}
}

func NewRateLimitError(provider string, retryAfter time.Duration) *ProviderError {
	return &ProviderError{
		Provider:    provider,
		StatusCode:  http.StatusTooManyRequests,
		Message:     "rate limit exceeded",
		RetryAfter:  retryAfter,
		IsRetryable: true,
	}
}

func isRetryableStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests ||
		statusCode >= http.StatusInternalServerError
}

var (
	ErrContextCancelled      = errors.New("context cancelled")
	ErrNoResponse            = errors.New("no response from provider")
	ErrInvalidResponse       = errors.New("invalid response from provider")
	ErrStreamingNotSupported = errors.New("streaming not supported")
)

func WrapError(provider string, err error) error {
	if err == nil {
		return nil
	}
	if pe, ok := err.(*ProviderError); ok {
		return pe
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &ProviderError{
			Provider:   provider,
			Message:    "context cancelled or deadline exceeded",
			Err:        err,
			StatusCode: 0,
		}
	}
	return &ProviderError{
		Provider: provider,
		Message:  err.Error(),
		Err:      err,
	}
}

func SafeStream(ctx context.Context, events <-chan StreamEvent, err error) <-chan StreamEvent {
	out := make(chan StreamEvent)
	if err != nil {
		go func() {
			defer close(out)
			select {
			case out <- StreamEvent{Type: StreamEventError, Error: err}:
			case <-ctx.Done():
			}
		}()
		return out
	}
	go func() {
		defer close(out)
		for {
			select {
			case event, ok := <-events:
				if !ok {
					return
				}
				select {
				case out <- event:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

type StreamBuilder struct {
	events chan StreamEvent
	ctx    context.Context
}

func NewStreamBuilder(ctx context.Context, bufferSize int) *StreamBuilder {
	return &StreamBuilder{
		events: make(chan StreamEvent, bufferSize),
		ctx:    ctx,
	}
}

func (b *StreamBuilder) Emit(event StreamEvent) bool {
	select {
	case b.events <- event:
		return true
	case <-b.ctx.Done():
		return false
	}
}

func (b *StreamBuilder) EmitDelta(delta string) bool {
	return b.Emit(StreamEvent{Type: StreamEventContentDelta, Delta: delta})
}

func (b *StreamBuilder) EmitToolCall(tc *ToolCall) bool {
	return b.Emit(StreamEvent{Type: StreamEventToolCallEnd, ToolCall: tc})
}

func (b *StreamBuilder) EmitError(err error) bool {
	return b.Emit(StreamEvent{Type: StreamEventError, Error: err})
}

func (b *StreamBuilder) EmitDone(finishReason string) bool {
	return b.Emit(StreamEvent{Type: StreamEventDone, FinishReason: finishReason})
}

func (b *StreamBuilder) Channel() <-chan StreamEvent {
	return b.events
}

func (b *StreamBuilder) Close() {
	close(b.events)
}
