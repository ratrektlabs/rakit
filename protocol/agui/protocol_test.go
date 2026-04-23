package agui_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ratrektlabs/rakit/protocol"
	"github.com/ratrektlabs/rakit/protocol/agui"
)

func encodeOne(t *testing.T, event protocol.Event) map[string]any {
	t.Helper()
	p := agui.New()
	var buf bytes.Buffer
	if err := p.Encode(&buf, event); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	payload := strings.TrimPrefix(strings.TrimSpace(buf.String()), "data: ")
	var m map[string]any
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Fatalf("parse frame: %v, raw=%q", err, buf.String())
	}
	return m
}

func TestEncodeRunLifecycle(t *testing.T) {
	start := encodeOne(t, &protocol.RunStartedEvent{ThreadID: "t1", RunID: "r1"})
	if start["type"] != "RUN_STARTED" || start["threadId"] != "t1" || start["runId"] != "r1" {
		t.Fatalf("RUN_STARTED: %+v", start)
	}
	end := encodeOne(t, &protocol.RunFinishedEvent{ThreadID: "t1", RunID: "r1"})
	if end["type"] != "RUN_FINISHED" {
		t.Fatalf("RUN_FINISHED: %+v", end)
	}
	errFrame := encodeOne(t, &protocol.RunErrorEvent{Message: "boom", Code: "INTERNAL"})
	if errFrame["type"] != "RUN_ERROR" || errFrame["message"] != "boom" || errFrame["code"] != "INTERNAL" {
		t.Fatalf("RUN_ERROR: %+v", errFrame)
	}
}

func TestEncodeToolCallPending(t *testing.T) {
	m := encodeOne(t, &protocol.ToolCallPendingEvent{
		ToolCallID: "tc1",
		ToolName:   "danger",
		Arguments:  `{"a":1}`,
		Reason:     "client_side",
	})
	// AG-UI emits the pending signal as a spec-compliant CUSTOM event
	// (the protocol reserves CUSTOM for application-specific extensions).
	if m["type"] != "CUSTOM" {
		t.Fatalf("type=%v", m["type"])
	}
	if m["name"] != "tool_call_pending" {
		t.Fatalf("name=%v", m["name"])
	}
	v, ok := m["value"].(map[string]any)
	if !ok {
		t.Fatalf("value not an object: %+v", m)
	}
	if v["toolCallId"] != "tc1" || v["reason"] != "client_side" {
		t.Fatalf("fields lost: %+v", v)
	}
}

func TestEncodeTextMessageLifecycle(t *testing.T) {
	m := encodeOne(t, &protocol.TextStartEvent{MessageID: "m1"})
	if m["type"] != "TEXT_MESSAGE_START" || m["role"] != "assistant" {
		t.Fatalf("TEXT_MESSAGE_START: %+v", m)
	}
	m = encodeOne(t, &protocol.TextDeltaEvent{MessageID: "m1", Delta: "hi"})
	if m["type"] != "TEXT_MESSAGE_CONTENT" || m["delta"] != "hi" {
		t.Fatalf("TEXT_MESSAGE_CONTENT: %+v", m)
	}
	m = encodeOne(t, &protocol.TextEndEvent{MessageID: "m1"})
	if m["type"] != "TEXT_MESSAGE_END" {
		t.Fatalf("TEXT_MESSAGE_END: %+v", m)
	}
}

func TestEncodeToolLifecycle(t *testing.T) {
	m := encodeOne(t, &protocol.ToolCallStartEvent{ToolCallID: "c1", ToolCallName: "add"})
	if m["type"] != "TOOL_CALL_START" || m["toolCallName"] != "add" {
		t.Fatalf("TOOL_CALL_START: %+v", m)
	}
	m = encodeOne(t, &protocol.ToolCallArgsEvent{ToolCallID: "c1", Delta: `{"x":1}`})
	if m["type"] != "TOOL_CALL_ARGS" || m["delta"] != `{"x":1}` {
		t.Fatalf("TOOL_CALL_ARGS: %+v", m)
	}
	m = encodeOne(t, &protocol.ToolCallEndEvent{ToolCallID: "c1"})
	if m["type"] != "TOOL_CALL_END" {
		t.Fatalf("TOOL_CALL_END: %+v", m)
	}
	m = encodeOne(t, &protocol.ToolResultEvent{ToolCallID: "c1", Result: "3"})
	if m["type"] != "TOOL_CALL_RESULT" || m["content"] != "3" || m["role"] != "tool" {
		t.Fatalf("TOOL_CALL_RESULT: %+v", m)
	}
}

func TestEncodeState(t *testing.T) {
	m := encodeOne(t, &protocol.StateSnapshotEvent{Snapshot: map[string]any{"foo": "bar"}})
	if m["type"] != "STATE_SNAPSHOT" {
		t.Fatalf("STATE_SNAPSHOT: %+v", m)
	}
	snap, _ := m["snapshot"].(map[string]any)
	if snap["foo"] != "bar" {
		t.Fatalf("snapshot body wrong: %+v", m)
	}
}

func TestContentType(t *testing.T) {
	if agui.New().ContentType() != "text/event-stream" {
		t.Fatal("agui ContentType wrong")
	}
}
