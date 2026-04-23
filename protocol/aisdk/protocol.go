package aisdk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/ratrektlabs/rakit/protocol"
)

// Protocol implements the Vercel AI SDK streaming format.
type Protocol struct{}

func New() *Protocol { return &Protocol{} }

func (p *Protocol) Name() string { return "ai-sdk" }

func (p *Protocol) ContentType() string { return "text/plain; charset=utf-8" }

func (p *Protocol) Encode(w io.Writer, event protocol.Event) error {
	switch e := event.(type) {
	case *protocol.TextStartEvent:
		return writeData(w, map[string]any{
			"type":      "start",
			"messageId": e.MessageID,
		})
	case *protocol.TextDeltaEvent:
		return writeData(w, map[string]any{
			"type":  "text-delta",
			"delta": e.Delta,
		})
	case *protocol.ToolCallStartEvent:
		return writeData(w, map[string]any{
			"type":       "tool-input-start",
			"toolCallId": e.ToolCallID,
			"toolName":   e.ToolCallName,
		})
	case *protocol.ToolCallArgsEvent:
		return writeData(w, map[string]any{
			"type":       "tool-input-delta",
			"toolCallId": e.ToolCallID,
			"delta":      e.Delta,
		})
	case *protocol.ToolCallEndEvent:
		return nil // AI SDK doesn't have a tool-input-end
	case *protocol.ToolCallPendingEvent:
		// Vercel AI SDK v5 exposes "data-*" parts for custom server data.
		// Clients listen for type == "data-tool-call-pending" and read
		// data.* for details.
		return writeData(w, map[string]any{
			"type": "data-tool-call-pending",
			"id":   e.ToolCallID,
			"data": map[string]any{
				"toolCallId": e.ToolCallID,
				"toolName":   e.ToolName,
				"arguments":  e.Arguments,
				"reason":     e.Reason,
			},
		})
	case *protocol.ToolResultEvent:
		return writeData(w, map[string]any{
			"type":       "tool-output-available",
			"toolCallId": e.ToolCallID,
			"output":     e.Result,
		})
	case *protocol.ReasoningMessageContentEvent:
		return writeData(w, map[string]any{
			"type":  "reasoning",
			"delta": e.Delta,
		})
	case *protocol.StateSnapshotEvent:
		return writeData(w, map[string]any{
			"type":     "state-snapshot",
			"snapshot": e.Snapshot,
		})
	case *protocol.StateDeltaEvent:
		return writeData(w, map[string]any{
			"type":  "state-delta",
			"delta": e.Delta,
		})
	case *protocol.DoneEvent:
		_, err := fmt.Fprint(w, "data: [DONE]\n\n")
		return err
	case *protocol.ErrorEvent:
		return writeData(w, map[string]any{
			"type":  "error",
			"error": e.Err.Error(),
		})
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
	return nil, fmt.Errorf("aisdk: Decode not implemented")
}

func (p *Protocol) DecodeStream(ctx context.Context, r io.Reader) (<-chan protocol.Event, error) {
	return nil, fmt.Errorf("aisdk: DecodeStream not implemented")
}

func writeData(w io.Writer, data map[string]any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", b)
	return err
}
