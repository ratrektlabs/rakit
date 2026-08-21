package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ratrektlabs/rakit/provider"
	"github.com/ratrektlabs/rakit/storage/metadata"
	"github.com/ratrektlabs/rakit/storage/metadata/sqlite"
	"github.com/ratrektlabs/rakit/tool"
)

type recordingStore struct {
	metadata.Store

	mu       sync.Mutex
	updates  []metadata.Session
	failAt   int
	failErr  error
	failOnce bool
	failed   bool
}

func (s *recordingStore) UpdateSession(ctx context.Context, sess *metadata.Session) error {
	s.mu.Lock()
	s.failErr = defaultPersistenceError(s.failErr)
	s.updates = append(s.updates, metadata.Session{ID: sess.ID, Revision: sess.Revision})
	call := len(s.updates)
	shouldFail := s.failAt == call && (!s.failOnce || !s.failed)
	if shouldFail {
		s.failed = true
	}
	s.mu.Unlock()
	if shouldFail {
		return s.failErr
	}
	if err := s.Store.UpdateSession(ctx, sess); err != nil {
		return err
	}

	var snapshot metadata.Session
	raw, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return err
	}
	s.mu.Lock()
	s.updates[call-1] = snapshot
	s.mu.Unlock()
	return nil
}

func defaultPersistenceError(err error) error {
	if err == nil {
		return errors.New("injected persistence failure")
	}
	return err
}

func newRecordingAgent(t *testing.T, p *scriptedProvider, opts ...Option) (*Agent, *recordingStore, string) {
	t.Helper()
	base, err := sqlite.NewStore(context.Background(), filepath.Join(t.TempDir(), "metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = base.Close() })
	store := &recordingStore{Store: base}
	baseOpts := []Option{
		WithProvider(p),
		WithStore(store),
		WithProtocol(noopEncoder{}),
		WithMaxIterations(5),
	}
	a := New(append(baseOpts, opts...)...)
	sess, err := a.CreateSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return a, store, sess.ID
}

func (s *recordingStore) snapshots() []metadata.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]metadata.Session, len(s.updates))
	copy(out, s.updates)
	return out
}

func TestAgentUsesGenericUpdateSessionWithoutRedundantFinalWrite(t *testing.T) {
	p := &scriptedProvider{turns: []turnScript{{&provider.TextDeltaEvent{Delta: "hello"}}}}
	a, store, sessionID := newRecordingAgent(t, p)
	events, err := a.RunWithSession(context.Background(), sessionID, "hi", a.Protocol)
	if err != nil {
		t.Fatal(err)
	}
	drain(t, events)

	updates := store.snapshots()
	if len(updates) != 2 {
		t.Fatalf("UpdateSession calls=%d want 2 (user and assistant)", len(updates))
	}
	if len(updates[0].Messages) != 1 || updates[0].Messages[0].Role != "user" {
		t.Fatalf("first durable state=%+v", updates[0])
	}
	if len(updates[1].Messages) != 2 || updates[1].Messages[1].Role != "assistant" {
		t.Fatalf("final durable state=%+v", updates[1])
	}
	if updates[1].Revision != updates[0].Revision+1 {
		t.Fatalf("revisions=%d,%d", updates[0].Revision, updates[1].Revision)
	}
}

func TestAgentPersistsToolBatchAndEachResult(t *testing.T) {
	p := &scriptedProvider{turns: []turnScript{
		{&provider.ToolCallEvent{ID: "tc-1", Name: "echo", Arguments: `{"value":"ok"}`}},
		{&provider.TextDeltaEvent{Delta: "done"}},
	}}
	a, store, sessionID := newRecordingAgent(t, p)
	a.Tools.Register(tool.NewFunctionTool("echo", "echo", nil, func(context.Context, map[string]any) (*tool.Result, error) {
		return tool.Ok(map[string]string{"value": "ok"}), nil
	}))
	events, err := a.RunWithSession(context.Background(), sessionID, "hi", a.Protocol)
	if err != nil {
		t.Fatal(err)
	}
	drain(t, events)

	updates := store.snapshots()
	if len(updates) != 4 {
		t.Fatalf("UpdateSession calls=%d want 4 (user, tool batch, result, final assistant)", len(updates))
	}
	if len(updates[1].Messages) != 2 || len(updates[1].Messages[1].ToolCalls) != 1 || updates[1].Messages[1].ToolCalls[0].Status != "pending" {
		t.Fatalf("tool batch state=%+v", updates[1])
	}
	if updates[2].Messages[1].ToolCalls[0].Status != "completed" || updates[2].Messages[1].ToolCalls[0].Result == "" {
		t.Fatalf("tool result state=%+v", updates[2])
	}
}

func TestInitialPersistenceFailureStopsProvider(t *testing.T) {
	p := &scriptedProvider{turns: []turnScript{{&provider.TextDeltaEvent{Delta: "must not run"}}}}
	a, store, sessionID := newRecordingAgent(t, p)
	store.failAt = 1
	store.failOnce = true
	if _, err := a.RunWithSession(context.Background(), sessionID, "hi", a.Protocol); err == nil {
		t.Fatal("RunWithSession succeeded after initial persistence failure")
	}
	if p.calls.Load() != 0 {
		t.Fatalf("provider calls=%d want 0", p.calls.Load())
	}
}

func TestPersistenceFailureNeverEmitsSuccessfulFinish(t *testing.T) {
	p := &scriptedProvider{turns: []turnScript{{&provider.TextDeltaEvent{Delta: "hello"}}}}
	a, store, sessionID := newRecordingAgent(t, p)
	store.failAt = 2
	store.failOnce = true
	events, err := a.RunWithSession(context.Background(), sessionID, "hi", a.Protocol)
	if err != nil {
		t.Fatal(err)
	}
	var sawError, sawFinished bool
	for event := range events {
		switch event.(type) {
		case *ErrorEvent:
			sawError = true
		case *RunFinishedEvent:
			sawFinished = true
		}
	}
	if !sawError || sawFinished {
		t.Fatalf("sawError=%v sawFinished=%v", sawError, sawFinished)
	}
}

func TestProviderFailureDoesNotPersistIncompleteAssistant(t *testing.T) {
	p := &scriptedProvider{turns: []turnScript{{
		&provider.TextDeltaEvent{Delta: "partial"},
		&provider.ErrorProviderEvent{Err: errors.New("provider failed")},
	}}}
	a, store, sessionID := newRecordingAgent(t, p)
	events, err := a.RunWithSession(context.Background(), sessionID, "hi", a.Protocol)
	if err != nil {
		t.Fatal(err)
	}
	var sawError, sawFinished bool
	for event := range events {
		switch event.(type) {
		case *ErrorEvent:
			sawError = true
		case *RunFinishedEvent:
			sawFinished = true
		}
	}
	if !sawError || sawFinished {
		t.Fatalf("sawError=%v sawFinished=%v", sawError, sawFinished)
	}
	updates := store.snapshots()
	if len(updates) != 1 || len(updates[0].Messages) != 1 {
		t.Fatalf("incomplete assistant persisted: updates=%+v", updates)
	}
}

func TestResumeSkipsAlreadyTerminalToolCallsAfterPartialPersistence(t *testing.T) {
	p := &scriptedProvider{turns: []turnScript{
		{
			&provider.ToolCallEvent{ID: "tc-1", Name: "client-one", Arguments: "{}"},
			&provider.ToolCallEvent{ID: "tc-2", Name: "client-two", Arguments: "{}"},
		},
		{&provider.TextDeltaEvent{Delta: "continued"}},
	}}
	a, store, sessionID := newRecordingAgent(t, p)
	for _, name := range []string{"client-one", "client-two"} {
		ft := tool.NewFunctionTool(name, name, nil, func(context.Context, map[string]any) (*tool.Result, error) {
			t.Fatal("client-side tool must not execute on the server")
			return tool.Ok(nil), nil
		})
		a.Tools.Register(clientTool{FunctionTool: ft})
	}

	events, err := a.RunWithSession(context.Background(), sessionID, "hi", a.Protocol)
	if err != nil {
		t.Fatal(err)
	}
	paused := drain(t, events)
	if paused.Outcome != OutcomeInterrupt || len(paused.Interrupts) != 2 {
		t.Fatalf("pause outcome=%q interrupts=%d", paused.Outcome, len(paused.Interrupts))
	}
	inputs := make([]ResumeInput, len(paused.Interrupts))
	for i, intr := range paused.Interrupts {
		inputs[i] = ResumeInput{
			InterruptID: intr.ID,
			Status:      ResumeResolved,
			Payload:     map[string]any{"output": map[string]any{"index": i}},
		}
	}
	store.failAt = 5 // user, assistant, interrupt, first resume result, second result
	store.failOnce = true
	events, err = a.Resume(context.Background(), sessionID, inputs, a.Protocol)
	if err != nil {
		t.Fatal(err)
	}
	for range events {
		// The injected failure terminates the resume before the provider is called.
	}

	partial, err := store.GetSession(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(partial.OpenInterrupts) != 1 || len(partial.Messages) == 0 {
		t.Fatalf("partial resume state=%+v", partial)
	}
	remainingID := partial.OpenInterrupts[0].ID
	events, err = a.Resume(context.Background(), sessionID, []ResumeInput{{
		InterruptID: remainingID,
		Status:      ResumeResolved,
		Payload:     map[string]any{"output": map[string]any{"index": 1}},
	}}, a.Protocol)
	if err != nil {
		t.Fatal(err)
	}
	var results []string
	var finished bool
	for event := range events {
		switch event := event.(type) {
		case *ToolResultEvent:
			if event.ToolCallID == "tc-1" {
				t.Fatal("already-terminal tool call was emitted again")
			}
			results = append(results, event.ToolCallID)
		case *RunFinishedEvent:
			finished = true
		case *ErrorEvent:
			t.Fatalf("retry emitted error: %v", event.Err)
		}
	}
	if len(results) != 1 || results[0] != "tc-2" || !finished {
		t.Fatalf("retry results=%v finished=%v", results, finished)
	}
	if p.calls.Load() != 2 {
		t.Fatalf("provider calls=%d want initial plus successful retry", p.calls.Load())
	}
}
