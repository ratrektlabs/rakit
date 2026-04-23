package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ratrektlabs/rakit/provider"
	"github.com/ratrektlabs/rakit/storage/metadata"
	"github.com/ratrektlabs/rakit/tool"
)

func TestFindPendingToolCallsMessage(t *testing.T) {
	msgs := []metadata.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", ToolCalls: []metadata.ToolCallRecord{{ID: "a", Status: "completed"}}},
		{Role: "user", Content: "again"},
		{Role: "assistant", ToolCalls: []metadata.ToolCallRecord{
			{ID: "b", Status: "pending_approval"},
			{ID: "c", Status: "completed"},
		}},
	}
	if got := findPendingToolCallsMessage(msgs); got != 3 {
		t.Fatalf("got %d want 3", got)
	}

	if got := findPendingToolCallsMessage(msgs[:2]); got != -1 {
		t.Fatalf("got %d want -1 for no pending", got)
	}
}

func TestIsPendingStatus(t *testing.T) {
	cases := map[string]bool{
		"pending_approval": true,
		"pending_client":   true,
		"completed":        false,
		"failed":           false,
		"":                 false,
	}
	for s, want := range cases {
		if got := isPendingStatus(s); got != want {
			t.Errorf("isPendingStatus(%q)=%v want %v", s, got, want)
		}
	}
}

func TestRejectionResultShape(t *testing.T) {
	s := rejectionResult("tc_1", "user rejected")
	var decoded map[string]string
	if err := json.Unmarshal([]byte(s), &decoded); err != nil {
		t.Fatalf("result not valid JSON: %v", err)
	}
	if decoded["toolCallId"] != "tc_1" {
		t.Fatalf("toolCallId lost: %+v", decoded)
	}
	if !strings.Contains(decoded["error"], "user rejected") {
		t.Fatalf("error message lost: %+v", decoded)
	}
}

// resolvePendingCall tests

type stubTool struct {
	name string
	fn   func(map[string]any) (*tool.Result, error)
}

func (s *stubTool) Name() string        { return s.name }
func (s *stubTool) Description() string { return "" }
func (s *stubTool) Parameters() any     { return nil }
func (s *stubTool) Execute(_ context.Context, input map[string]any) (*tool.Result, error) {
	return s.fn(input)
}

func newRegistry(tools ...tool.Tool) *tool.Registry {
	r := tool.NewRegistry()
	for _, t := range tools {
		r.Register(t)
	}
	return r
}

func TestResolvePendingCall_ApprovalRejected(t *testing.T) {
	a := &Agent{}
	rec := &metadata.ToolCallRecord{ID: "tc", Name: "delete_item", Status: "pending_approval"}
	dec := ToolDecision{ToolCallID: "tc", Approve: false, Message: "nope"}
	result, status := a.resolvePendingCall(context.Background(), rec, dec, true, newRegistry())
	if status != "failed" {
		t.Fatalf("status=%q want failed", status)
	}
	if !strings.Contains(result, "nope") {
		t.Fatalf("rejection message lost: %s", result)
	}
}

func TestResolvePendingCall_ApprovalApprovedExecutes(t *testing.T) {
	a := &Agent{}
	executed := false
	ttool := &stubTool{
		name: "delete_item",
		fn: func(input map[string]any) (*tool.Result, error) {
			executed = true
			return &tool.Result{Data: map[string]any{"deleted": input["id"]}}, nil
		},
	}
	rec := &metadata.ToolCallRecord{
		ID: "tc", Name: "delete_item", Status: "pending_approval",
		Arguments: `{"id":"42"}`,
	}
	dec := ToolDecision{ToolCallID: "tc", Approve: true}
	result, status := a.resolvePendingCall(context.Background(), rec, dec, true, newRegistry(ttool))
	if !executed {
		t.Fatal("tool must execute when approval granted")
	}
	if status != "completed" {
		t.Fatalf("status=%q want completed", status)
	}
	if !strings.Contains(result, `"deleted":"42"`) {
		t.Fatalf("result missing payload: %s", result)
	}
}

func TestResolvePendingCall_ClientSideUsesProvidedResult(t *testing.T) {
	a := &Agent{}
	rec := &metadata.ToolCallRecord{ID: "tc", Name: "browser_time", Status: "pending_client"}
	dec := ToolDecision{ToolCallID: "tc", Approve: true, Result: `{"iso":"2025-01-01"}`}
	result, status := a.resolvePendingCall(context.Background(), rec, dec, true, newRegistry())
	if status != "completed" {
		t.Fatalf("status=%q want completed", status)
	}
	if result != `{"iso":"2025-01-01"}` {
		t.Fatalf("result=%s want passthrough", result)
	}
}

func TestResolvePendingCall_ClientSideMissingResultFails(t *testing.T) {
	a := &Agent{}
	rec := &metadata.ToolCallRecord{ID: "tc", Name: "browser_time", Status: "pending_client"}
	dec := ToolDecision{ToolCallID: "tc"}
	_, status := a.resolvePendingCall(context.Background(), rec, dec, true, newRegistry())
	if status != "failed" {
		t.Fatalf("status=%q want failed when client result missing", status)
	}
}

func TestResolvePendingCall_NoDecisionFails(t *testing.T) {
	a := &Agent{}
	rec := &metadata.ToolCallRecord{ID: "tc", Name: "anything", Status: "pending_approval"}
	_, status := a.resolvePendingCall(context.Background(), rec, ToolDecision{}, false, newRegistry())
	if status != "failed" {
		t.Fatalf("status=%q want failed when no decision provided", status)
	}
}

func TestMetadataToProviderMessages_SynthesizesToolResults(t *testing.T) {
	msgs := []metadata.Message{
		{Role: "user", Content: "do it"},
		{
			Role: "assistant",
			ToolCalls: []metadata.ToolCallRecord{
				{ID: "tc1", Name: "echo", Arguments: `{"a":1}`, Result: `{"ok":true}`, Status: "completed"},
				{ID: "tc2", Name: "pending", Arguments: `{}`, Status: "pending_approval"},
			},
		},
	}
	out := metadataToProviderMessages(msgs)
	// Expect: user, assistant (with both calls), tool result for completed, (no tool msg for pending)
	if len(out) != 3 {
		t.Fatalf("len=%d want 3 (user, assistant, tool)", len(out))
	}
	if out[2].Role != "tool" {
		t.Fatalf("msg[2].role=%q want tool", out[2].Role)
	}
	if len(out[2].ToolCalls) != 1 || out[2].ToolCalls[0].ID != "tc1" {
		t.Fatalf("synthesized tool msg must reference tc1: %+v", out[2])
	}
	if out[2].Content != `{"ok":true}` {
		t.Fatalf("tool content lost: %q", out[2].Content)
	}
}

var _ provider.Provider = (*stubProvider)(nil)
