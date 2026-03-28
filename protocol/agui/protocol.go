package agui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/ratrektlabs/rl-agent/protocol"
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
			"type":     "RunStarted",
			"threadId": e.ThreadID,
			"runId":    e.RunID,
		})
	case *protocol.TextStartEvent:
		return writeSSE(w, map[string]any{
			"type":      "TextMessageStart",
			"messageId": e.MessageID,
			"role":      "assistant",
		})
	case *protocol.TextDeltaEvent:
		return writeSSE(w, map[string]any{
			"type":      "TextMessageContent",
			"messageId": e.MessageID,
			"delta":     e.Delta,
		})
	case *protocol.TextEndEvent:
		return writeSSE(w, map[string]any{
			"type":      "TextMessageEnd",
			"messageId": e.MessageID,
		})
	case *protocol.ToolCallStartEvent:
		return writeSSE(w, map[string]any{
			"type":         "ToolCallStart",
			"toolCallId":   e.ToolCallID,
			"toolCallName": e.ToolCallName,
		})
	case *protocol.ToolCallArgsEvent:
		return writeSSE(w, map[string]any{
			"type":       "ToolCallArgs",
			"toolCallId": e.ToolCallID,
			"delta":      e.Delta,
		})
	case *protocol.ToolCallEndEvent:
		return writeSSE(w, map[string]any{
			"type":       "ToolCallEnd",
			"toolCallId": e.ToolCallID,
		})
	case *protocol.ToolResultEvent:
		return writeSSE(w, map[string]any{
			"type":       "ToolCallResult",
			"toolCallId": e.ToolCallID,
			"content":    e.Result,
			"role":       "tool",
		})
	case *protocol.StateSnapshotEvent:
		return writeSSE(w, map[string]any{
			"type":     "StateSnapshot",
			"snapshot": e.Snapshot,
		})
	case *protocol.StateDeltaEvent:
		return writeSSE(w, map[string]any{
			"type":  "StateDelta",
			"delta": e.Delta,
		})
	case *protocol.ThinkingEvent:
		return writeSSE(w, map[string]any{
			"type":  "Reasoning",
			"delta": e.Delta,
		})
	case *protocol.RunFinishedEvent:
		return writeSSE(w, map[string]any{
			"type":     "RunFinished",
			"threadId": e.ThreadID,
			"runId":    e.RunID,
		})
	case *protocol.RunErrorEvent:
		return writeSSE(w, map[string]any{
			"type":    "RunError",
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
