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

// New constructs a new AG-UI protocol encoder.
func New() *Protocol { return &Protocol{} }

// Name returns the encoder's stable identifier.
func (p *Protocol) Name() string { return "ag-ui" }

// ContentType returns the HTTP Content-Type AG-UI streams over (SSE).
func (p *Protocol) ContentType() string { return "text/event-stream" }

// Encode writes a single event as one AG-UI SSE frame.
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
		frame := map[string]any{
			"type":     "RUN_FINISHED",
			"threadId": e.ThreadID,
			"runId":    e.RunID,
		}
		// Per the AG-UI "Interrupt-Aware Run Lifecycle" draft, an
		// interrupted run terminates with RUN_FINISHED carrying
		// outcome:"interrupt" and a populated interrupts[]. Successful
		// runs omit both fields for back-compat with stable AG-UI
		// clients.
		if e.Outcome == protocol.OutcomeInterrupt {
			frame["outcome"] = "interrupt"
			frame["interrupts"] = interruptsToWire(e.Interrupts)
		} else if e.Result != nil {
			frame["result"] = e.Result
		}
		return writeSSE(w, frame)
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

// EncodeStream drains events into the writer, flushing after every frame so
// browser SSE clients see incremental output.
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

// Decode is not implemented for AG-UI; this encoder only writes.
func (p *Protocol) Decode(r io.Reader) (protocol.Event, error) {
	return nil, fmt.Errorf("agui: Decode not implemented")
}

// DecodeStream is not implemented for AG-UI; this encoder only writes.
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

// interruptsToWire converts the agent-level [agent.Interrupt] slice into the
// JSON shape defined by the AG-UI "Interrupt-Aware Run Lifecycle" draft.
//
// Field names match the draft verbatim (camelCase). Zero-value fields are
// omitted so the wire output matches the spec's optionality.
func interruptsToWire(in []protocol.Interrupt) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, intr := range in {
		frame := map[string]any{
			"id":      intr.ID,
			"reason":  intr.Reason,
			"message": intr.Message,
		}
		if intr.ToolCallID != "" {
			frame["toolCallId"] = intr.ToolCallID
		}
		if len(intr.ResponseSchema) > 0 {
			frame["responseSchema"] = intr.ResponseSchema
		}
		if !intr.ExpiresAt.IsZero() {
			frame["expiresAt"] = intr.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		if len(intr.Metadata) > 0 {
			frame["metadata"] = intr.Metadata
		}
		out = append(out, frame)
	}
	return out
}
