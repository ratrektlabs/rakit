// Package protocol is a thin re-export layer for backward compatibility.
// The authoritative event types and encoder interface live in the agent
// package. Encoders (ag-ui, ai-sdk, ...) continue to live here.
package protocol

import (
	"github.com/ratrektlabs/rakit/agent"
)

// Event re-exports [agent.Event].
type Event = agent.Event

// EventType re-exports [agent.EventType].
type EventType = agent.EventType

// Event type constants re-exported from the agent package.
const (
	EventRunStarted            = agent.EventRunStarted
	EventRunFinished           = agent.EventRunFinished
	EventRunError              = agent.EventRunError
	EventTextStart             = agent.EventTextStart
	EventTextDelta             = agent.EventTextDelta
	EventTextEnd               = agent.EventTextEnd
	EventToolCallStart         = agent.EventToolCallStart
	EventToolCallArgs          = agent.EventToolCallArgs
	EventToolCallEnd           = agent.EventToolCallEnd
	EventToolResult            = agent.EventToolResult
	EventStateSnapshot         = agent.EventStateSnapshot
	EventStateDelta            = agent.EventStateDelta
	EventReasoningStart        = agent.EventReasoningStart
	EventReasoningMessageStart = agent.EventReasoningMessageStart
	EventReasoningMessageDelta = agent.EventReasoningMessageDelta
	EventReasoningMessageEnd   = agent.EventReasoningMessageEnd
	EventReasoningEnd          = agent.EventReasoningEnd
	EventError                 = agent.EventError
	EventDone                  = agent.EventDone
)

// Protocol re-exports [agent.Encoder] under its historical name so existing
// callers keep compiling.
type Protocol = agent.Encoder

// Event struct re-exports.
type (
	BaseEvent                    = agent.BaseEvent
	RunStartedEvent              = agent.RunStartedEvent
	RunFinishedEvent             = agent.RunFinishedEvent
	RunErrorEvent                = agent.RunErrorEvent
	TextStartEvent               = agent.TextStartEvent
	TextDeltaEvent               = agent.TextDeltaEvent
	TextEndEvent                 = agent.TextEndEvent
	ToolCallStartEvent           = agent.ToolCallStartEvent
	ToolCallArgsEvent            = agent.ToolCallArgsEvent
	ToolCallEndEvent             = agent.ToolCallEndEvent
	ToolResultEvent              = agent.ToolResultEvent
	StateSnapshotEvent           = agent.StateSnapshotEvent
	StateDeltaEvent              = agent.StateDeltaEvent
	ReasoningStartEvent          = agent.ReasoningStartEvent
	ReasoningMessageStartEvent   = agent.ReasoningMessageStartEvent
	ReasoningMessageContentEvent = agent.ReasoningMessageContentEvent
	ReasoningMessageEndEvent     = agent.ReasoningMessageEndEvent
	ReasoningEndEvent            = agent.ReasoningEndEvent
	ErrorEvent                   = agent.ErrorEvent
	DoneEvent                    = agent.DoneEvent
)
