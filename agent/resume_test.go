package agent

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ratrektlabs/rakit/provider"
	"github.com/ratrektlabs/rakit/storage/metadata/sqlite"
	"github.com/ratrektlabs/rakit/tool"
)

// noopEncoder is the minimal [Encoder] the runner asks for. Tests only need
// it to be non-nil; none of the HIL logic inspects the encoder.
type noopEncoder struct{}

func (noopEncoder) Name() string                  { return "noop" }
func (noopEncoder) ContentType() string           { return "text/plain" }
func (noopEncoder) Encode(io.Writer, Event) error { return nil }
func (noopEncoder) EncodeStream(context.Context, io.Writer, <-chan Event) error {
	return nil
}
func (noopEncoder) Decode(io.Reader) (Event, error) { return nil, io.EOF }
func (noopEncoder) DecodeStream(context.Context, io.Reader) (<-chan Event, error) {
	ch := make(chan Event)
	close(ch)
	return ch, nil
}

// turnScript describes one provider response: a slice of raw events the stub
// emits on the n-th call to Stream.
type turnScript []provider.Event

// scriptedProvider plays back a sequence of turns one per Stream call.
type scriptedProvider struct {
	turns []turnScript
	calls atomic.Int32
}

func (p *scriptedProvider) Name() string     { return "scripted" }
func (p *scriptedProvider) Model() string    { return "scripted" }
func (p *scriptedProvider) Models() []string { return []string{"scripted"} }
func (p *scriptedProvider) SetModel(string)  {}
func (p *scriptedProvider) Stream(_ context.Context, _ *provider.Request) (<-chan provider.Event, error) {
	idx := int(p.calls.Add(1)) - 1
	ch := make(chan provider.Event, 16)
	go func() {
		defer close(ch)
		if idx >= len(p.turns) {
			return
		}
		for _, ev := range p.turns[idx] {
			ch <- ev
		}
	}()
	return ch, nil
}
func (p *scriptedProvider) Generate(_ context.Context, _ *provider.Request) (*provider.Response, error) {
	return &provider.Response{}, nil
}

// clientTool is a Tool that also implements ClientSide.
type clientTool struct{ *tool.FunctionTool }

func (clientTool) ClientSide() bool { return true }

// newTestAgent builds an Agent backed by an in-memory sqlite store and the
// given scripted provider.
func newTestAgent(t *testing.T, p *scriptedProvider, opts ...Option) (*Agent, string) {
	t.Helper()
	store, err := sqlite.NewStore(context.Background(), t.TempDir()+"/meta.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	base := []Option{
		WithProvider(p),
		WithStore(store),
		WithProtocol(noopEncoder{}),
		WithMaxIterations(5),
	}
	a := New(append(base, opts...)...)
	sess, err := a.CreateSession(context.Background())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return a, sess.ID
}

// drain consumes every event and returns the terminal RunFinishedEvent.
func drain(t *testing.T, ch <-chan Event) *RunFinishedEvent {
	t.Helper()
	var last *RunFinishedEvent
	for ev := range ch {
		if rf, ok := ev.(*RunFinishedEvent); ok {
			last = rf
		}
		if errEv, ok := ev.(*ErrorEvent); ok {
			t.Fatalf("runner emitted error: %v", errEv.Err)
		}
	}
	if last == nil {
		t.Fatal("no RunFinishedEvent emitted")
	}
	return last
}

// ---------------------------------------------------------------------------
// Classification / pause
// ---------------------------------------------------------------------------

func TestClassifyPendingGatesApprovalTools(t *testing.T) {
	a := &Agent{approvalPolicy: RequireFor("delete_item")}
	reg := tool.NewRegistry()
	reg.Register(tool.NewFunctionTool("delete_item", "d", nil, func(context.Context, map[string]any) (*tool.Result, error) {
		return tool.Ok(nil), nil
	}))
	intrs := a.classifyPending([]provider.ToolCall{
		{ID: "tc-1", Name: "delete_item", Arguments: "{}"},
		{ID: "tc-2", Name: "echo", Arguments: "{}"},
	}, reg)
	if len(intrs) != 1 || intrs[0].ToolCallID != "tc-1" {
		t.Fatalf("intrs=%+v", intrs)
	}
	if interruptKind(intrs[0]) != kindApproval {
		t.Fatalf("kind=%q", interruptKind(intrs[0]))
	}
}

func TestClassifyPendingGatesClientSideTools(t *testing.T) {
	a := &Agent{}
	reg := tool.NewRegistry()
	ft := tool.NewFunctionTool("browser_time", "d", nil, func(context.Context, map[string]any) (*tool.Result, error) {
		return tool.Ok(nil), nil
	})
	reg.Register(clientTool{FunctionTool: ft})
	intrs := a.classifyPending([]provider.ToolCall{{ID: "tc-1", Name: "browser_time", Arguments: "{}"}}, reg)
	if len(intrs) != 1 || interruptKind(intrs[0]) != kindClientSide {
		t.Fatalf("intrs=%+v", intrs)
	}
}

// ---------------------------------------------------------------------------
// Resume — happy + failure paths
// ---------------------------------------------------------------------------

// pauseAndReturnSession drives an agent through one turn that pauses on a
// gated tool call and returns the session id + the raised interrupt.
func pauseAndResume(
	t *testing.T,
	gated provider.ToolCall,
	tools []tool.Tool,
	policy ApprovalPolicy,
	resumeInputs func(intrID string) []ResumeInput,
	postResumeText string,
) *RunFinishedEvent {
	t.Helper()
	p := &scriptedProvider{
		turns: []turnScript{
			{&provider.ToolCallEvent{ID: gated.ID, Name: gated.Name, Arguments: gated.Arguments}},
			{&provider.TextDeltaEvent{Delta: postResumeText}},
		},
	}
	opts := []Option{}
	if policy != nil {
		opts = append(opts, WithApprovalPolicy(policy))
	}
	a, sessID := newTestAgent(t, p, opts...)
	for _, tl := range tools {
		a.Tools.Register(tl)
	}
	enc := a.Protocol
	evs, err := a.RunWithSession(context.Background(), sessID, "hi", enc)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	rf := drain(t, evs)
	if rf.Outcome != OutcomeInterrupt {
		t.Fatalf("first turn outcome=%q want interrupt", rf.Outcome)
	}
	if len(rf.Interrupts) != 1 {
		t.Fatalf("interrupts=%d", len(rf.Interrupts))
	}
	// Resume.
	inputs := resumeInputs(rf.Interrupts[0].ID)
	evs, err = a.Resume(context.Background(), sessID, inputs, enc)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	return drain(t, evs)
}

func TestResumeApprovedExecutesGatedTool(t *testing.T) {
	var executed atomic.Bool
	tl := tool.NewFunctionTool("delete_item", "d", nil, func(context.Context, map[string]any) (*tool.Result, error) {
		executed.Store(true)
		return tool.Ok(map[string]string{"deleted": "yes"}), nil
	})
	rf := pauseAndResume(t,
		provider.ToolCall{ID: "tc-approve", Name: "delete_item", Arguments: "{}"},
		[]tool.Tool{tl},
		RequireFor("delete_item"),
		func(id string) []ResumeInput {
			return []ResumeInput{{InterruptID: id, Status: ResumeResolved, Payload: map[string]any{"approved": true}}}
		},
		"done",
	)
	if !executed.Load() {
		t.Fatal("gated tool was not executed after approval")
	}
	if rf.Outcome != OutcomeSuccess && rf.Outcome != "" {
		t.Fatalf("post-resume outcome=%q", rf.Outcome)
	}
}

func TestResumeRejectedSynthesizesUserRejected(t *testing.T) {
	var executed atomic.Bool
	tl := tool.NewFunctionTool("delete_item", "d", nil, func(context.Context, map[string]any) (*tool.Result, error) {
		executed.Store(true)
		return tool.Ok(nil), nil
	})
	// Collect tool results from the resume stream to assert on them.
	p := &scriptedProvider{
		turns: []turnScript{
			{&provider.ToolCallEvent{ID: "tc-r", Name: "delete_item", Arguments: "{}"}},
			{&provider.TextDeltaEvent{Delta: "ack"}},
		},
	}
	a, sessID := newTestAgent(t, p, WithApprovalPolicy(RequireFor("delete_item")))
	a.Tools.Register(tl)
	evs, err := a.RunWithSession(context.Background(), sessID, "hi", a.Protocol)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	rf := drain(t, evs)
	evs, err = a.Resume(context.Background(), sessID, []ResumeInput{{
		InterruptID: rf.Interrupts[0].ID,
		Status:      ResumeResolved,
		Payload:     map[string]any{"approved": false},
	}}, a.Protocol)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	var results []string
	for ev := range evs {
		if tr, ok := ev.(*ToolResultEvent); ok {
			results = append(results, tr.Result)
		}
	}
	if executed.Load() {
		t.Fatal("tool must not execute on rejection")
	}
	if len(results) != 1 || !strings.Contains(results[0], "user rejected") {
		t.Fatalf("tool-result=%v", results)
	}
}

func TestResumeCancelledSynthesizesCancelled(t *testing.T) {
	tl := tool.NewFunctionTool("delete_item", "d", nil, func(context.Context, map[string]any) (*tool.Result, error) {
		return tool.Ok(nil), nil
	})
	p := &scriptedProvider{
		turns: []turnScript{
			{&provider.ToolCallEvent{ID: "tc-c", Name: "delete_item", Arguments: "{}"}},
			{&provider.TextDeltaEvent{Delta: "ok"}},
		},
	}
	a, sessID := newTestAgent(t, p, WithApprovalPolicy(RequireFor("delete_item")))
	a.Tools.Register(tl)
	evs, _ := a.RunWithSession(context.Background(), sessID, "hi", a.Protocol)
	rf := drain(t, evs)
	evs, err := a.Resume(context.Background(), sessID, []ResumeInput{{
		InterruptID: rf.Interrupts[0].ID,
		Status:      ResumeCancelled,
	}}, a.Protocol)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	var results []string
	for ev := range evs {
		if tr, ok := ev.(*ToolResultEvent); ok {
			results = append(results, tr.Result)
		}
	}
	if len(results) != 1 || !strings.Contains(results[0], "cancelled") {
		t.Fatalf("tool-result=%v", results)
	}
}

func TestResumeClientSideUsesPayload(t *testing.T) {
	ft := tool.NewFunctionTool("browser_time", "d", nil, func(context.Context, map[string]any) (*tool.Result, error) {
		t.Fatal("client-side tool must not execute on the server")
		return tool.Ok(nil), nil
	})
	p := &scriptedProvider{
		turns: []turnScript{
			{&provider.ToolCallEvent{ID: "tc-cs", Name: "browser_time", Arguments: "{}"}},
			{&provider.TextDeltaEvent{Delta: "thanks"}},
		},
	}
	a, sessID := newTestAgent(t, p)
	a.Tools.Register(clientTool{FunctionTool: ft})
	evs, _ := a.RunWithSession(context.Background(), sessID, "hi", a.Protocol)
	rf := drain(t, evs)
	if interruptKind(rf.Interrupts[0]) != kindClientSide {
		t.Fatalf("kind=%q", interruptKind(rf.Interrupts[0]))
	}
	evs, err := a.Resume(context.Background(), sessID, []ResumeInput{{
		InterruptID: rf.Interrupts[0].ID,
		Status:      ResumeResolved,
		Payload: map[string]any{"output": map[string]any{
			"iso":      "2026-01-01T00:00:00Z",
			"timezone": "UTC",
		}},
	}}, a.Protocol)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	var results []string
	for ev := range evs {
		if tr, ok := ev.(*ToolResultEvent); ok {
			results = append(results, tr.Result)
		}
	}
	if len(results) != 1 {
		t.Fatalf("results=%v", results)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(results[0]), &got); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if got["timezone"] != "UTC" || got["iso"] != "2026-01-01T00:00:00Z" {
		t.Fatalf("client payload lost: %+v", got)
	}
}

// ---------------------------------------------------------------------------
// Resume — validation errors
// ---------------------------------------------------------------------------

func pauseOnce(t *testing.T) (*Agent, string, string) {
	t.Helper()
	tl := tool.NewFunctionTool("delete_item", "d", nil, func(context.Context, map[string]any) (*tool.Result, error) {
		return tool.Ok(nil), nil
	})
	p := &scriptedProvider{
		turns: []turnScript{{&provider.ToolCallEvent{ID: "tc-v", Name: "delete_item", Arguments: "{}"}}},
	}
	a, sessID := newTestAgent(t, p, WithApprovalPolicy(RequireFor("delete_item")))
	a.Tools.Register(tl)
	evs, err := a.RunWithSession(context.Background(), sessID, "hi", a.Protocol)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	rf := drain(t, evs)
	if len(rf.Interrupts) != 1 {
		t.Fatalf("interrupts=%d", len(rf.Interrupts))
	}
	return a, sessID, rf.Interrupts[0].ID
}

func TestResumeValidationNoOpenInterrupts(t *testing.T) {
	p := &scriptedProvider{turns: []turnScript{{&provider.TextDeltaEvent{Delta: "hi"}}}}
	a, sessID := newTestAgent(t, p)
	evs, _ := a.RunWithSession(context.Background(), sessID, "hi", a.Protocol)
	_ = drain(t, evs)
	if _, err := a.Resume(context.Background(), sessID, nil, a.Protocol); err == nil ||
		!strings.Contains(err.Error(), "no open interrupts") {
		t.Fatalf("err=%v", err)
	}
}

func TestResumeValidationUnknownID(t *testing.T) {
	a, sessID, _ := pauseOnce(t)
	_, err := a.Resume(context.Background(), sessID, []ResumeInput{{
		InterruptID: "does-not-exist",
		Status:      ResumeResolved,
	}}, a.Protocol)
	if err == nil || !strings.Contains(err.Error(), "unknown interruptId") {
		t.Fatalf("err=%v", err)
	}
}

func TestResumeValidationMissingID(t *testing.T) {
	a, sessID, _ := pauseOnce(t)
	_, err := a.Resume(context.Background(), sessID, nil, a.Protocol)
	if err == nil || !strings.Contains(err.Error(), "must address") {
		t.Fatalf("err=%v", err)
	}
}

func TestResumeValidationDuplicateID(t *testing.T) {
	a, sessID, intrID := pauseOnce(t)
	_, err := a.Resume(context.Background(), sessID, []ResumeInput{
		{InterruptID: intrID, Status: ResumeResolved, Payload: map[string]any{"approved": true}},
		{InterruptID: intrID, Status: ResumeResolved, Payload: map[string]any{"approved": true}},
	}, a.Protocol)
	if err == nil || !strings.Contains(err.Error(), "duplicate resume input") {
		t.Fatalf("err=%v", err)
	}
}

func TestResumeValidationEmptyInterruptID(t *testing.T) {
	a, sessID, _ := pauseOnce(t)
	_, err := a.Resume(context.Background(), sessID, []ResumeInput{{InterruptID: "", Status: ResumeResolved}}, a.Protocol)
	if err == nil || !strings.Contains(err.Error(), "missing interruptId") {
		t.Fatalf("err=%v", err)
	}
}

func TestInterruptRoundTrip(t *testing.T) {
	orig := []Interrupt{{
		ID:         "i1",
		Reason:     "tool_call",
		ToolCallID: "tc1",
		Message:    "go?",
		Metadata:   map[string]any{"rakit.kind": kindApproval},
	}}
	md := interruptsToMetadata(orig)
	if len(md) != 1 || md[0].ID != "i1" {
		t.Fatalf("to metadata: %+v", md)
	}
	back := metadataToInterrupts(md)
	if len(back) != 1 || back[0].ToolCallID != "tc1" || interruptKind(back[0]) != kindApproval {
		t.Fatalf("round trip: %+v", back)
	}
	if interruptsToMetadata(nil) != nil || metadataToInterrupts(nil) != nil {
		t.Fatal("nil should round-trip to nil")
	}
}
