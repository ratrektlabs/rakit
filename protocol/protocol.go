package protocol

import (
	"context"
	"io"
)

// Event represents a generic agent event.
type Event interface {
	Type() EventType
}

// EventType identifies the kind of event.
type EventType string

const (
	// Lifecycle
	EventRunStarted  EventType = "run-started"
	EventRunFinished EventType = "run-finished"
	EventRunError    EventType = "run-error"

	// Text streaming
	EventTextStart EventType = "text-start"
	EventTextDelta EventType = "text-delta"
	EventTextEnd   EventType = "text-end"

	// Tool calls
	EventToolCallStart   EventType = "tool-call-start"
	EventToolCallArgs    EventType = "tool-call-args"
	EventToolCallEnd     EventType = "tool-call-end"
	EventToolCallPending EventType = "tool-call-pending"
	EventToolResult      EventType = "tool-result"

	// State
	EventStateSnapshot EventType = "state-snapshot"
	EventStateDelta    EventType = "state-delta"

	// Thinking/reasoning
	EventReasoningStart        EventType = "reasoning-start"
	EventReasoningMessageStart EventType = "reasoning-message-start"
	EventReasoningMessageDelta EventType = "reasoning-message-delta"
	EventReasoningMessageEnd   EventType = "reasoning-message-end"
	EventReasoningEnd          EventType = "reasoning-end"

	// Terminal
	EventError EventType = "error"
	EventDone  EventType = "done"
)

// Protocol encodes/decodes agent events to/from a wire format.
type Protocol interface {
	Name() string
	ContentType() string
	Encode(w io.Writer, event Event) error
	EncodeStream(ctx context.Context, w io.Writer, events <-chan Event) error
	Decode(r io.Reader) (Event, error)
	DecodeStream(ctx context.Context, r io.Reader) (<-chan Event, error)
}

// Base event types — protocol implementations use these or define their own.

type BaseEvent struct {
	EventType EventType
}

func (e *BaseEvent) Type() EventType { return e.EventType }

type RunStartedEvent struct {
	ThreadID string
	RunID    string
	Input    string
}

func (e *RunStartedEvent) Type() EventType { return EventRunStarted }

type RunFinishedEvent struct {
	ThreadID string
	RunID    string
}

func (e *RunFinishedEvent) Type() EventType { return EventRunFinished }

type RunErrorEvent struct {
	Message string
	Code    string
}

func (e *RunErrorEvent) Type() EventType { return EventRunError }

type TextStartEvent struct {
	MessageID string
	Role      string
}

func (e *TextStartEvent) Type() EventType { return EventTextStart }

type TextDeltaEvent struct {
	MessageID string
	Delta     string
}

func (e *TextDeltaEvent) Type() EventType { return EventTextDelta }

type TextEndEvent struct {
	MessageID string
}

func (e *TextEndEvent) Type() EventType { return EventTextEnd }

type ToolCallStartEvent struct {
	ToolCallID   string
	ToolCallName string
}

func (e *ToolCallStartEvent) Type() EventType { return EventToolCallStart }

type ToolCallArgsEvent struct {
	ToolCallID string
	Delta      string
}

func (e *ToolCallArgsEvent) Type() EventType { return EventToolCallArgs }

type ToolCallEndEvent struct {
	ToolCallID string
}

func (e *ToolCallEndEvent) Type() EventType { return EventToolCallEnd }

type ToolResultEvent struct {
	ToolCallID string
	Result     string
}

func (e *ToolResultEvent) Type() EventType { return EventToolResult }

// ToolCallPendingEvent signals that a tool call requires human-in-the-loop
// resolution before the agent can continue. Reason is one of:
//   - "approval_required": an ApprovalPolicy gated this call; the client
//     should present an Approve/Reject decision to the user.
//   - "client_side": the tool executes on the client and the agent is
//     waiting for the client to provide a Result.
type ToolCallPendingEvent struct {
	ToolCallID string
	ToolName   string
	Arguments  string
	Reason     string
}

func (e *ToolCallPendingEvent) Type() EventType { return EventToolCallPending }

type StateSnapshotEvent struct {
	Snapshot map[string]any
}

func (e *StateSnapshotEvent) Type() EventType { return EventStateSnapshot }

type StateDeltaEvent struct {
	Delta []any // JSON Patch operations (RFC 6902)
}

func (e *StateDeltaEvent) Type() EventType { return EventStateDelta }

// ReasoningStart marks the beginning of a reasoning process.
type ReasoningStartEvent struct {
	MessageID string
}

func (e *ReasoningStartEvent) Type() EventType { return EventReasoningStart }

// ReasoningMessageStart signals the start of a visible reasoning message.
type ReasoningMessageStartEvent struct {
	MessageID string
	Role      string
}

func (e *ReasoningMessageStartEvent) Type() EventType { return EventReasoningMessageStart }

// ReasoningMessageContent delivers a chunk of reasoning text.
type ReasoningMessageContentEvent struct {
	MessageID string
	Delta     string
}

func (e *ReasoningMessageContentEvent) Type() EventType { return EventReasoningMessageDelta }

// ReasoningMessageEnd signals the end of a reasoning message.
type ReasoningMessageEndEvent struct {
	MessageID string
}

func (e *ReasoningMessageEndEvent) Type() EventType { return EventReasoningMessageEnd }

// ReasoningEnd marks the end of the reasoning process.
type ReasoningEndEvent struct {
	MessageID string
}

func (e *ReasoningEndEvent) Type() EventType { return EventReasoningEnd }

type ErrorEvent struct {
	Err error
}

func (e *ErrorEvent) Type() EventType { return EventError }

type DoneEvent struct{}

func (e *DoneEvent) Type() EventType { return EventDone }
