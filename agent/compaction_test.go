package agent

import (
	"context"
	"testing"

	"github.com/ratrektlabs/rakit/provider"
	"github.com/ratrektlabs/rakit/storage/metadata"
)

func TestShouldCompact(t *testing.T) {
	cfg := CompactionConfig{MaxMessages: 10, KeepRecent: 4}
	if shouldCompact(make([]metadata.Message, 4), cfg) {
		t.Fatal("must not compact at KeepRecent")
	}
	if shouldCompact(make([]metadata.Message, 10), cfg) {
		t.Fatal("must not compact at MaxMessages exactly")
	}
	if !shouldCompact(make([]metadata.Message, 11), cfg) {
		t.Fatal("must compact above MaxMessages")
	}
}

func TestMetadataToProviderMessages(t *testing.T) {
	in := []metadata.Message{
		{Role: "user", Content: "hi"},
		{
			Role:    "assistant",
			Content: "calling",
			ToolCalls: []metadata.ToolCallRecord{
				{ID: "tc1", Name: "add", Arguments: `{"a":1}`},
			},
		},
	}
	out := metadataToProviderMessages(in)
	if len(out) != 2 {
		t.Fatalf("len=%d want 2", len(out))
	}
	if out[0].Role != "user" || out[0].Content != "hi" {
		t.Fatalf("msg0=%+v", out[0])
	}
	if len(out[1].ToolCalls) != 1 {
		t.Fatalf("assistant tool calls len=%d", len(out[1].ToolCalls))
	}
	if out[1].ToolCalls[0].ID != "tc1" || out[1].ToolCalls[0].Name != "add" {
		t.Fatalf("tool call meta lost: %+v", out[1].ToolCalls[0])
	}
}

func TestProviderToolCallsToRecords(t *testing.T) {
	in := []provider.ToolCall{{ID: "c", Name: "t", Arguments: "{}"}}
	out := providerToolCallsToRecords(in)
	if len(out) != 1 || out[0].Status != "completed" {
		t.Fatalf("records=%+v", out)
	}
	if providerToolCallsToRecords(nil) != nil {
		t.Fatal("nil input want nil output")
	}
}

// stubProvider returns a fixed summary. It satisfies provider.Provider for compaction tests.
type stubProvider struct{ text string }

func (s *stubProvider) Name() string     { return "stub" }
func (s *stubProvider) Model() string    { return "stub" }
func (s *stubProvider) Models() []string { return []string{"stub"} }
func (s *stubProvider) SetModel(string)  {}
func (s *stubProvider) Stream(ctx context.Context, _ *provider.Request) (<-chan provider.Event, error) {
	ch := make(chan provider.Event)
	close(ch)
	return ch, nil
}
func (s *stubProvider) Generate(_ context.Context, _ *provider.Request) (*provider.Response, error) {
	return &provider.Response{Content: s.text}, nil
}

func TestCompactShortHistoryNoOp(t *testing.T) {
	cfg := CompactionConfig{MaxMessages: 10, KeepRecent: 4}
	msgs := make([]metadata.Message, 3)
	out, err := compact(context.Background(), &stubProvider{text: "summary"}, msgs, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(msgs) {
		t.Fatalf("len=%d want %d (no-op)", len(out), len(msgs))
	}
}

func TestCompactSummarizesOldMessages(t *testing.T) {
	cfg := CompactionConfig{MaxMessages: 6, KeepRecent: 2, SummaryRole: "system"}
	msgs := []metadata.Message{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
		{Role: "user", Content: "c"},
		{Role: "assistant", Content: "d"},
		{Role: "user", Content: "e"},
		{Role: "assistant", Content: "f"},
	}
	out, err := compact(context.Background(), &stubProvider{text: "SUMMARY"}, msgs, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1+2 {
		t.Fatalf("len=%d want 3 (summary + 2 recent)", len(out))
	}
	if out[0].Role != "system" || out[0].Content != "SUMMARY" {
		t.Fatalf("summary msg wrong: %+v", out[0])
	}
	if out[1].Content != "e" || out[2].Content != "f" {
		t.Fatalf("recent msgs wrong: %+v / %+v", out[1], out[2])
	}
}
