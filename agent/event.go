package agent

// Event is the protocol-agnostic type produced by a run. Encoders translate
// these into their wire formats. The agent loop never imports a protocol
// package; callers pair a run's event stream with an [Encoder] at the I/O
// boundary.
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
	EventToolCallStart EventType = "tool-call-start"
	EventToolCallArgs  EventType = "tool-call-args"
	EventToolCallEnd   EventType = "tool-call-end"
	EventToolResult    EventType = "tool-result"

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

// BaseEvent is an embeddable helper for simple event types.
type BaseEvent struct {
	EventType EventType
}

// Type returns the event type.
func (e *BaseEvent) Type() EventType { return e.EventType }

// RunStartedEvent signals the beginning of an agent run.
type RunStartedEvent struct {
	ThreadID string
	RunID    string
	Input    string
}

// Type returns the event type.
func (e *RunStartedEvent) Type() EventType { return EventRunStarted }

// RunFinishedEvent signals the terminal state of an agent run.
//
// [Outcome] reports whether the run completed successfully, was paused by
// interrupts, or ended in an error. [Interrupts] is populated iff
// Outcome == [OutcomeInterrupt]; [Result] MAY be set iff Outcome is
// [OutcomeSuccess] or unset. A zero-value Outcome is treated as success for
// backward compatibility with older encoders.
type RunFinishedEvent struct {
	ThreadID   string
	RunID      string
	Outcome    RunOutcome
	Interrupts []Interrupt
	Result     any
}

// Type returns the event type.
func (e *RunFinishedEvent) Type() EventType { return EventRunFinished }

// RunErrorEvent signals that the run terminated with an error.
type RunErrorEvent struct {
	Message string
	Code    string
}

// Type returns the event type.
func (e *RunErrorEvent) Type() EventType { return EventRunError }

// TextStartEvent marks the start of a streamed text message.
type TextStartEvent struct {
	MessageID string
	Role      string
}

// Type returns the event type.
func (e *TextStartEvent) Type() EventType { return EventTextStart }

// TextDeltaEvent delivers a chunk of streamed text.
type TextDeltaEvent struct {
	MessageID string
	Delta     string
}

// Type returns the event type.
func (e *TextDeltaEvent) Type() EventType { return EventTextDelta }

// TextEndEvent marks the end of a streamed text message.
type TextEndEvent struct {
	MessageID string
}

// Type returns the event type.
func (e *TextEndEvent) Type() EventType { return EventTextEnd }

// ToolCallStartEvent announces a tool invocation from the provider.
type ToolCallStartEvent struct {
	ToolCallID   string
	ToolCallName string
}

// Type returns the event type.
func (e *ToolCallStartEvent) Type() EventType { return EventToolCallStart }

// ToolCallArgsEvent streams the JSON arguments of a tool call.
type ToolCallArgsEvent struct {
	ToolCallID string
	Delta      string
}

// Type returns the event type.
func (e *ToolCallArgsEvent) Type() EventType { return EventToolCallArgs }

// ToolCallEndEvent marks the end of a tool call's argument stream.
type ToolCallEndEvent struct {
	ToolCallID string
}

// Type returns the event type.
func (e *ToolCallEndEvent) Type() EventType { return EventToolCallEnd }

// ToolResultEvent delivers the result of an executed tool call.
type ToolResultEvent struct {
	ToolCallID string
	Result     string
}

// Type returns the event type.
func (e *ToolResultEvent) Type() EventType { return EventToolResult }

// StateSnapshotEvent delivers a full snapshot of agent state.
type StateSnapshotEvent struct {
	Snapshot map[string]any
}

// Type returns the event type.
func (e *StateSnapshotEvent) Type() EventType { return EventStateSnapshot }

// StateDeltaEvent delivers a JSON Patch (RFC 6902) of agent state changes.
type StateDeltaEvent struct {
	Delta []any
}

// Type returns the event type.
func (e *StateDeltaEvent) Type() EventType { return EventStateDelta }

// ReasoningStartEvent marks the beginning of a reasoning process.
type ReasoningStartEvent struct {
	MessageID string
}

// Type returns the event type.
func (e *ReasoningStartEvent) Type() EventType { return EventReasoningStart }

// ReasoningMessageStartEvent signals the start of a visible reasoning message.
type ReasoningMessageStartEvent struct {
	MessageID string
	Role      string
}

// Type returns the event type.
func (e *ReasoningMessageStartEvent) Type() EventType { return EventReasoningMessageStart }

// ReasoningMessageContentEvent delivers a chunk of reasoning text.
type ReasoningMessageContentEvent struct {
	MessageID string
	Delta     string
}

// Type returns the event type.
func (e *ReasoningMessageContentEvent) Type() EventType { return EventReasoningMessageDelta }

// ReasoningMessageEndEvent signals the end of a reasoning message.
type ReasoningMessageEndEvent struct {
	MessageID string
}

// Type returns the event type.
func (e *ReasoningMessageEndEvent) Type() EventType { return EventReasoningMessageEnd }

// ReasoningEndEvent marks the end of the reasoning process.
type ReasoningEndEvent struct {
	MessageID string
}

// Type returns the event type.
func (e *ReasoningEndEvent) Type() EventType { return EventReasoningEnd }

// ErrorEvent carries a non-fatal error surfaced from the run.
type ErrorEvent struct {
	Err error
}

// Type returns the event type.
func (e *ErrorEvent) Type() EventType { return EventError }

// DoneEvent is a terminal marker some encoders use to close a stream.
type DoneEvent struct{}

// Type returns the event type.
func (e *DoneEvent) Type() EventType { return EventDone }
