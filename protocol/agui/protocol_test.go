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
	if _, ok := end["outcome"]; ok {
		t.Fatalf("RUN_FINISHED for a normal run must not include outcome: %+v", end)
	}
	errFrame := encodeOne(t, &protocol.RunErrorEvent{Message: "boom", Code: "INTERNAL"})
	if errFrame["type"] != "RUN_ERROR" || errFrame["message"] != "boom" || errFrame["code"] != "INTERNAL" {
		t.Fatalf("RUN_ERROR: %+v", errFrame)
	}
}

// TestEncodeRunFinishedInterrupt asserts the AG-UI "Interrupt-Aware Run
// Lifecycle" draft mapping: when the run pauses on interrupts, RUN_FINISHED
// must carry outcome:"interrupt" and a populated interrupts[] using the
// camelCase field names from the draft.
func TestEncodeRunFinishedInterrupt(t *testing.T) {
	frame := encodeOne(t, &protocol.RunFinishedEvent{
		ThreadID: "t1",
		RunID:    "r1",
		Outcome:  protocol.OutcomeInterrupt,
		Interrupts: []protocol.Interrupt{{
			ID:         "intr-1",
			Reason:     "tool_call",
			Message:    "delete_item requires approval",
			ToolCallID: "tc-1",
			ResponseSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"approved": map[string]any{"type": "boolean"},
				},
			},
			Metadata: map[string]any{"rakit.kind": "approval"},
		}},
	})
	if frame["outcome"] != "interrupt" {
		t.Fatalf("outcome=%v want interrupt", frame["outcome"])
	}
	intrs, ok := frame["interrupts"].([]any)
	if !ok || len(intrs) != 1 {
		t.Fatalf("interrupts: %+v", frame["interrupts"])
	}
	intr, _ := intrs[0].(map[string]any)
	if intr["id"] != "intr-1" || intr["reason"] != "tool_call" || intr["toolCallId"] != "tc-1" {
		t.Fatalf("interrupt frame missing required fields: %+v", intr)
	}
	if intr["message"] != "delete_item requires approval" {
		t.Fatalf("interrupt message wrong: %+v", intr)
	}
	if _, ok := intr["responseSchema"].(map[string]any); !ok {
		t.Fatalf("responseSchema missing/typed wrong: %+v", intr["responseSchema"])
	}
	if md, ok := intr["metadata"].(map[string]any); !ok || md["rakit.kind"] != "approval" {
		t.Fatalf("metadata missing/wrong: %+v", intr["metadata"])
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
