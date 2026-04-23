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
	result := parseSSE(encode(t, &protocol.ToolResultEvent{ToolCallID: "c1", Result: "3"}))
	if result[0]["type"] != "tool-output-available" || result[0]["output"] != "3" {
		t.Fatalf("result frame: %+v", result[0])
	}
	// ToolCallEnd is intentionally suppressed for AI SDK.
	if s := encode(t, &protocol.ToolCallEndEvent{ToolCallID: "c1"}); s != "" {
		t.Fatalf("ToolCallEnd should emit nothing, got %q", s)
	}
}

func TestEncodeError(t *testing.T) {
	frames := parseSSE(encode(t, &protocol.ErrorEvent{Err: errors.New("oops")}))
	if frames[0]["type"] != "error" || frames[0]["error"] != "oops" {
		t.Fatalf("bad frame: %+v", frames[0])
	}
}

func TestEncodeDone(t *testing.T) {
	s := encode(t, &protocol.DoneEvent{})
	if !strings.Contains(s, "data: [DONE]") {
		t.Fatalf("done frame missing [DONE] sentinel: %q", s)
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
