package agui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/ratrektlabs/rakit/protocol"
)

// Protocol implements the AG-UI (CopilotKit) streaming format.
type Protocol struct{}

func New() *Protocol { return &Protocol{} }

func (p *Protocol) Name() string { return "ag-ui" }

func (p *Protocol) ContentType() string { return "text/event-stream" }

func (p *Protocol) Encode(w io.Writer, event protocol.Event) error {
	switch e := event.(type) {
	case *protocol.RunStartedEvent:
		return writeSSE(w, map[string]any{
			"type":     "RUN_STARTED",
			"threadId": e.ThreadID,
			"runId":    e.RunID,
		})
	case *protocol.TextStartEvent:
		return writeSSE(w, map[string]any{
			"type":      "TEXT_MESSAGE_START",
			"messageId": e.MessageID,
			"role":      "assistant",
		})
	case *protocol.TextDeltaEvent:
		return writeSSE(w, map[string]any{
			"type":      "TEXT_MESSAGE_CONTENT",
			"messageId": e.MessageID,
			"delta":     e.Delta,
		})
	case *protocol.TextEndEvent:
		return writeSSE(w, map[string]any{
			"type":      "TEXT_MESSAGE_END",
			"messageId": e.MessageID,
		})
	case *protocol.ToolCallStartEvent:
		return writeSSE(w, map[string]any{
			"type":         "TOOL_CALL_START",
			"toolCallId":   e.ToolCallID,
			"toolCallName": e.ToolCallName,
		})
	case *protocol.ToolCallArgsEvent:
		return writeSSE(w, map[string]any{
			"type":       "TOOL_CALL_ARGS",
			"toolCallId": e.ToolCallID,
			"delta":      e.Delta,
		})
	case *protocol.ToolCallEndEvent:
		return writeSSE(w, map[string]any{
			"type":       "TOOL_CALL_END",
			"toolCallId": e.ToolCallID,
		})
	case *protocol.ToolCallPendingEvent:
		// AG-UI has no first-class "pending tool call" event. The spec
		// reserves the CUSTOM event for application-specific extensions,
		// so that is what we use here. Clients look for
		// name == "tool_call_pending" and read value.* for details.
		return writeSSE(w, map[string]any{
			"type": "CUSTOM",
			"name": "tool_call_pending",
			"value": map[string]any{
				"toolCallId": e.ToolCallID,
				"toolName":   e.ToolName,
				"arguments":  e.Arguments,
				"reason":     e.Reason,
			},
		})
	case *protocol.ToolResultEvent:
		return writeSSE(w, map[string]any{
			"type":       "TOOL_CALL_RESULT",
			"toolCallId": e.ToolCallID,
			"content":    e.Result,
			"role":       "tool",
		})
	case *protocol.StateSnapshotEvent:
		return writeSSE(w, map[string]any{
			"type":     "STATE_SNAPSHOT",
			"snapshot": e.Snapshot,
		})
	case *protocol.StateDeltaEvent:
		return writeSSE(w, map[string]any{
			"type":  "STATE_DELTA",
			"delta": e.Delta,
		})
	case *protocol.ReasoningStartEvent:
		return writeSSE(w, map[string]any{
			"type":      "REASONING_START",
			"messageId": e.MessageID,
		})
	case *protocol.ReasoningMessageStartEvent:
		return writeSSE(w, map[string]any{
			"type":      "REASONING_MESSAGE_START",
			"messageId": e.MessageID,
			"role":      e.Role,
		})
	case *protocol.ReasoningMessageContentEvent:
		return writeSSE(w, map[string]any{
			"type":      "REASONING_MESSAGE_CONTENT",
			"messageId": e.MessageID,
			"delta":     e.Delta,
		})
	case *protocol.ReasoningMessageEndEvent:
		return writeSSE(w, map[string]any{
			"type":      "REASONING_MESSAGE_END",
			"messageId": e.MessageID,
		})
	case *protocol.ReasoningEndEvent:
		return writeSSE(w, map[string]any{
			"type":      "REASONING_END",
			"messageId": e.MessageID,
		})
	case *protocol.RunFinishedEvent:
		return writeSSE(w, map[string]any{
			"type":     "RUN_FINISHED",
			"threadId": e.ThreadID,
			"runId":    e.RunID,
		})
	case *protocol.RunErrorEvent:
		return writeSSE(w, map[string]any{
			"type":    "RUN_ERROR",
			"message": e.Message,
			"code":    e.Code,
		})
	case *protocol.DoneEvent:
		return nil
	}
	return nil
}

func (p *Protocol) EncodeStream(ctx context.Context, w io.Writer, events <-chan protocol.Event) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-events:
			if !ok {
				return nil
			}
			if err := p.Encode(w, event); err != nil {
				return err
			}
			if flusher, ok := w.(interface{ Flush() }); ok {
				flusher.Flush()
			}
		}
	}
}

func (p *Protocol) Decode(r io.Reader) (protocol.Event, error) {
	return nil, fmt.Errorf("agui: Decode not implemented")
}

func (p *Protocol) DecodeStream(ctx context.Context, r io.Reader) (<-chan protocol.Event, error) {
	return nil, fmt.Errorf("agui: DecodeStream not implemented")
}

func writeSSE(w io.Writer, data map[string]any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", b)
	return err
}
