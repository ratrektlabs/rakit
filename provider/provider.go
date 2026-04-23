package provider

import "context"

// Message represents a chat message.
type Message struct {
	Role      string
	Content   string
	ToolCalls []ToolCall
}

// ToolCall represents an LLM tool call.
type ToolCall struct {
	ID               string
	Name             string
	Arguments        string
	ThoughtSignature []byte // Provider-specific opaque data to include when echoing back
}

// Provider is the interface for LLM backends.
type Provider interface {
	Name() string
	Model() string
	Models() []string
	SetModel(model string)
	Stream(ctx context.Context, req *Request) (<-chan Event, error)
	Generate(ctx context.Context, req *Request) (*Response, error)
}

// Request is sent to the LLM provider.
type Request struct {
	Model       string
	Messages    []Message
	Tools       []Tool
	System      string // system prompt / instructions
	MaxTokens   int
	Temperature float64
}

// Tool describes a tool the LLM can call.
type Tool struct {
	Name        string
	Description string
	Parameters  any // JSON Schema
}

// Response is a non-streaming LLM response.
type Response struct {
	Content   string
	ToolCalls []ToolCall
	Usage     Usage
}

// Usage tracks token consumption.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Event represents a provider streaming event.
type Event interface {
	Type() EventType
}

type EventType string

const (
	EventTextDelta  EventType = "text-delta"
	EventToolCall   EventType = "tool-call"
	EventToolResult EventType = "tool-result"
	EventDone       EventType = "done"
	EventError      EventType = "error"
)

type TextDeltaEvent struct {
	Delta string
}

func (e *TextDeltaEvent) Type() EventType { return EventTextDelta }

type ToolCallEvent struct {
	ID               string
	Name             string
	Arguments        string
	ThoughtSignature []byte // Provider-specific opaque data to include when echoing back
}

func (e *ToolCallEvent) Type() EventType { return EventToolCall }

type ToolResultProviderEvent struct {
	ID     string
	Result string
}

func (e *ToolResultProviderEvent) Type() EventType { return EventToolResult }

type DoneProviderEvent struct{}

func (e *DoneProviderEvent) Type() EventType { return EventDone }

type ErrorProviderEvent struct {
	Err error
}

func (e *ErrorProviderEvent) Type() EventType { return EventError }
