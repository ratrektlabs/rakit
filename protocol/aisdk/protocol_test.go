package aisdk_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ratrektlabs/rakit/protocol"
	"github.com/ratrektlabs/rakit/protocol/aisdk"
)

func encode(t *testing.T, event protocol.Event) string {
	t.Helper()
	p := aisdk.New()
	var buf bytes.Buffer
	if err := p.Encode(&buf, event); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return buf.String()
}

// parseSSE parses `data: {...}\n\n` frames from an SSE body.
func parseSSE(s string) []map[string]any {
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(s), "\n\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			out = append(out, map[string]any{"__done__": true})
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(payload), &m); err != nil {
			continue
		}
		out = append(out, m)
	}
	return out
}

func TestEncodeTextDelta(t *testing.T) {
	frames := parseSSE(encode(t, &protocol.TextDeltaEvent{Delta: "hi"}))
	if len(frames) != 1 {
		t.Fatalf("frames=%d want 1", len(frames))
	}
	if frames[0]["type"] != "text-delta" || frames[0]["delta"] != "hi" {
		t.Fatalf("bad frame: %+v", frames[0])
	}
}

func TestEncodeTextStart(t *testing.T) {
	frames := parseSSE(encode(t, &protocol.TextStartEvent{MessageID: "m1"}))
	if frames[0]["type"] != "start" || frames[0]["messageId"] != "m1" {
		t.Fatalf("bad frame: %+v", frames[0])
	}
}

func TestEncodeToolLifecycle(t *testing.T) {
	start := parseSSE(encode(t, &protocol.ToolCallStartEvent{ToolCallID: "c1", ToolCallName: "add"}))
	if start[0]["type"] != "tool-input-start" || start[0]["toolCallId"] != "c1" || start[0]["toolName"] != "add" {
		t.Fatalf("start frame: %+v", start[0])
	}
	args := parseSSE(encode(t, &protocol.ToolCallArgsEvent{ToolCallID: "c1", Delta: `{"x":1}`}))
	if args[0]["type"] != "tool-input-delta" || args[0]["delta"] != `{"x":1}` {
		t.Fatalf("args frame: %+v", args[0])
	}
	// ToolCallEnd carries the parsed input through the spec-native
	// tool-input-available frame. A dangling tool-input-available with no
	// follow-up tool-output-available is the AI SDK signal for an
	// approval / client-side interrupt.
	end := parseSSE(encode(t, &protocol.ToolCallEndEvent{ToolCallID: "c1", Arguments: `{"x":1}`}))
	if end[0]["type"] != "tool-input-available" || end[0]["toolCallId"] != "c1" {
		t.Fatalf("end frame: %+v", end[0])
	}
	input, ok := end[0]["input"].(map[string]any)
	if !ok || input["x"] != float64(1) {
		t.Fatalf("end frame input not parsed: %+v", end[0]["input"])
	}
	result := parseSSE(encode(t, &protocol.ToolResultEvent{ToolCallID: "c1", Result: "3"}))
	if result[0]["type"] != "tool-output-available" || result[0]["output"] != "3" {
		t.Fatalf("result frame: %+v", result[0])
	}
}

// TestEncodeStreamTerminatesWithDone guards the AI SDK contract: every
// stream must end with a `data: [DONE]\n\n` sentinel so browser clients
// stop reading. Previously rakit closed the channel without writing the
// sentinel, leaving clients to time out.
func TestEncodeStreamTerminatesWithDone(t *testing.T) {
	p := aisdk.New()
	events := make(chan protocol.Event, 1)
	events <- &protocol.TextDeltaEvent{Delta: "hi"}
	close(events)
	var buf bytes.Buffer
	if err := p.EncodeStream(context.Background(), &buf, events); err != nil {
		t.Fatalf("EncodeStream: %v", err)
	}
	if !strings.HasSuffix(strings.TrimRight(buf.String(), "\n"), "data: [DONE]") {
		t.Fatalf("stream did not end with [DONE] sentinel:\n%s", buf.String())
	}
}

// TestEncodeStreamEmitsSingleDone guards against a regression where a
// runner-emitted DoneEvent and the channel-close handler each wrote a
// `data: [DONE]\n\n` frame. AI SDK clients stop reading at the first
// sentinel, so a duplicate emitted mid-stream silently drops every
// subsequent frame (notably the spec-native tool-input-available the
// HIL flow relies on).
func TestEncodeStreamEmitsSingleDone(t *testing.T) {
	p := aisdk.New()
	events := make(chan protocol.Event, 4)
	events <- &protocol.TextDeltaEvent{Delta: "hi"}
	events <- &protocol.DoneEvent{}
	events <- &protocol.ToolCallEndEvent{ToolCallID: "c1", Arguments: `{"x":1}`}
	close(events)

	var buf bytes.Buffer
	if err := p.EncodeStream(context.Background(), &buf, events); err != nil {
		t.Fatalf("EncodeStream: %v", err)
	}

	got := strings.Count(buf.String(), "data: [DONE]")
	if got != 1 {
		t.Fatalf("got %d [DONE] sentinels, want exactly 1:\n%s", got, buf.String())
	}
	if !strings.HasSuffix(strings.TrimRight(buf.String(), "\n"), "data: [DONE]") {
		t.Fatalf("the single [DONE] sentinel must be the final frame:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), `"type":"tool-input-available"`) {
		t.Fatalf("post-DoneEvent frames were dropped:\n%s", buf.String())
	}
}

func TestEncodeError(t *testing.T) {
	frames := parseSSE(encode(t, &protocol.ErrorEvent{Err: errors.New("oops")}))
	if frames[0]["type"] != "error" || frames[0]["error"] != "oops" {
		t.Fatalf("bad frame: %+v", frames[0])
	}
}

// TestEncodeDoneIsSilent guards the contract that [Encode] must not
// emit the [DONE] sentinel on a [DoneEvent]. Channel close in
// [EncodeStream] is the single authoritative end-of-stream signal; if
// [Encode] also wrote [DONE], any frame emitted afterward by the runner
// would be dropped by AI SDK clients.
func TestEncodeDoneIsSilent(t *testing.T) {
	s := encode(t, &protocol.DoneEvent{})
	if strings.Contains(s, "data: [DONE]") {
		t.Fatalf("DoneEvent must not emit a [DONE] frame; got: %q", s)
	}
}

func TestDecodeNotImplemented(t *testing.T) {
	p := aisdk.New()
	if _, err := p.Decode(bytes.NewReader(nil)); err == nil {
		t.Fatal("Decode should return not-implemented error")
	}
	if _, err := p.DecodeStream(context.Background(), io.NopCloser(bytes.NewReader(nil))); err == nil {
		t.Fatal("DecodeStream should return not-implemented error")
	}
}
