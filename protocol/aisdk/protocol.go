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

// New constructs a new AI SDK protocol encoder.
func New() *Protocol { return &Protocol{} }

// Name returns the encoder's stable identifier.
func (p *Protocol) Name() string { return "ai-sdk" }

// ContentType returns the HTTP Content-Type the AI SDK expects (a
// line-delimited stream of `data: <json>` frames).
func (p *Protocol) ContentType() string { return "text/plain; charset=utf-8" }

// Encode writes a single event as one AI SDK data frame.
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
		// AI SDK has no tool-input-end frame, but it does have
		// tool-input-available which carries the parsed input. Emit it
		// here so a tool call that pauses on an interrupt (no
		// tool-output-available follows) is still well-formed: the
		// dangling tool-input-available is exactly the spec-native
		// signal the client uses to render an approval / input card.
		var input any
		if e.Arguments != "" {
			if err := json.Unmarshal([]byte(e.Arguments), &input); err != nil {
				input = e.Arguments
			}
		} else {
			input = map[string]any{}
		}
		return writeData(w, map[string]any{
			"type":       "tool-input-available",
			"toolCallId": e.ToolCallID,
			"input":      input,
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

// EncodeStream drains events into the writer, flushing after every frame.
//
// On graceful close (events channel closed) it terminates the wire stream
// with a `data: [DONE]\n\n` frame, matching the AI SDK's data-stream-protocol
// expectation. Without this, browser clients block on the read until their
// idle timer fires.
func (p *Protocol) EncodeStream(ctx context.Context, w io.Writer, events <-chan protocol.Event) error {
	flush := func() {
		if flusher, ok := w.(interface{ Flush() }); ok {
			flusher.Flush()
		}
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-events:
			if !ok {
				if _, err := fmt.Fprint(w, "data: [DONE]\n\n"); err != nil {
					return err
				}
				flush()
				return nil
			}
			if err := p.Encode(w, event); err != nil {
				return err
			}
			flush()
		}
	}
}

// Decode is not implemented for the AI SDK; this encoder only writes.
func (p *Protocol) Decode(r io.Reader) (protocol.Event, error) {
	return nil, fmt.Errorf("aisdk: Decode not implemented")
}

// DecodeStream is not implemented for the AI SDK; this encoder only writes.
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
