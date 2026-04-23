package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ratrektlabs/rakit/provider"
	"github.com/ratrektlabs/rakit/storage/metadata"
	"github.com/ratrektlabs/rakit/tool"
)

// interruptID returns a random, URL-safe identifier for a new [Interrupt].
func interruptID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fall back to a timestamp-derived value — collisions are not
		// security-relevant, only uniqueness within an open-interrupts set.
		return fmt.Sprintf("intr-%d", time.Now().UnixNano())
	}
	return "intr-" + hex.EncodeToString(b[:])
}

// classifyPending returns an [Interrupt] for every tool call that must not be
// executed inline. A tool call is gated when:
//   - its tool implements [ClientSide] and returns true, or
//   - the agent's [ApprovalPolicy] returns true for its name and arguments.
//
// A tool call whose name is not in the registry is NOT gated here — the
// existing "tool not found" path in the runner will produce a failed
// [ToolResultEvent] for it.
func (a *Agent) classifyPending(calls []provider.ToolCall, registry *tool.Registry) []Interrupt {
	if len(calls) == 0 {
		return nil
	}
	var out []Interrupt
	for _, tc := range calls {
		var kind string
		if registry != nil {
			if t := registry.Get(tc.Name); t != nil {
				if cs, ok := t.(ClientSide); ok && cs.ClientSide() {
					kind = kindClientSide
				}
			}
		}
		if kind == "" && a.approvalPolicy != nil && a.approvalPolicy.RequiresApproval(tc.Name, tc.Arguments) {
			kind = kindApproval
		}
		if kind == "" {
			continue
		}
		message := fmt.Sprintf("Tool %q requires approval before it can run.", tc.Name)
		if kind == kindClientSide {
			message = fmt.Sprintf("Tool %q runs on the client; provide its result to continue.", tc.Name)
		}
		out = append(out, Interrupt{
			ID:         interruptID(),
			Reason:     "tool_call",
			Message:    message,
			ToolCallID: tc.ID,
			Metadata:   map[string]any{"rakit.kind": kind},
		})
	}
	return out
}

// pendingStatusFor maps a tool-call id to the persisted status that should be
// recorded on the corresponding [metadata.ToolCallRecord] when the batch is
// paused.
func pendingStatusFor(toolCallID string, intrs []Interrupt) string {
	for _, intr := range intrs {
		if intr.ToolCallID != toolCallID {
			continue
		}
		switch interruptKind(intr) {
		case kindClientSide:
			return "pending_client"
		case kindApproval:
			return "pending_approval"
		}
	}
	return "pending"
}

// interruptsToMetadata converts in-memory interrupts to the persisted shape.
func interruptsToMetadata(in []Interrupt) []metadata.Interrupt {
	if len(in) == 0 {
		return nil
	}
	out := make([]metadata.Interrupt, len(in))
	for i, intr := range in {
		var expiresAtMs int64
		if !intr.ExpiresAt.IsZero() {
			expiresAtMs = intr.ExpiresAt.UnixMilli()
		}
		out[i] = metadata.Interrupt{
			ID:             intr.ID,
			Reason:         intr.Reason,
			Message:        intr.Message,
			ToolCallID:     intr.ToolCallID,
			ResponseSchema: intr.ResponseSchema,
			ExpiresAtMs:    expiresAtMs,
			Metadata:       intr.Metadata,
		}
	}
	return out
}

// metadataToInterrupts converts persisted interrupts back to the in-memory
// shape used by the runner and encoders.
func metadataToInterrupts(in []metadata.Interrupt) []Interrupt {
	if len(in) == 0 {
		return nil
	}
	out := make([]Interrupt, len(in))
	for i, intr := range in {
		var expiresAt time.Time
		if intr.ExpiresAtMs > 0 {
			expiresAt = time.UnixMilli(intr.ExpiresAtMs)
		}
		out[i] = Interrupt{
			ID:             intr.ID,
			Reason:         intr.Reason,
			Message:        intr.Message,
			ToolCallID:     intr.ToolCallID,
			ResponseSchema: intr.ResponseSchema,
			ExpiresAt:      expiresAt,
			Metadata:       intr.Metadata,
		}
	}
	return out
}

// Resume resolves one or more open [Interrupt]s on a session and re-enters
// the agentic loop with the synthesized tool results appended.
//
// The rules mirror the AG-UI "Interrupt-Aware Run Lifecycle" draft:
//   - the session must have at least one open interrupt;
//   - every open interrupt must be addressed exactly once by [inputs];
//   - unknown or duplicate interrupt ids are rejected.
//
// For each tool-call-bound interrupt, the runner synthesizes a tool-result
// message from the resume payload:
//   - payload {"approved": true}    → execute the original tool and use its output;
//   - payload {"approved": false}   → synthesize {"error":"user rejected"};
//   - Status == [ResumeCancelled]   → synthesize {"error":"cancelled"};
//   - payload with an "output" key (client-side tools) → use that verbatim;
//   - any other shape               → use the raw payload as the result JSON.
//
// Resume returns a fresh event channel identical in shape to [Agent.Run].
func (a *Agent) Resume(
	ctx context.Context,
	sessionID string,
	inputs []ResumeInput,
	p Encoder,
) (<-chan Event, error) {
	if a.Provider == nil {
		return nil, fmt.Errorf("agent: no provider configured")
	}
	if p == nil {
		return nil, fmt.Errorf("agent: no protocol configured")
	}
	if a.Store == nil {
		return nil, fmt.Errorf("agent: no store configured")
	}

	sess, err := a.Store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("agent: load session %q: %w", sessionID, err)
	}
	if sess == nil {
		return nil, fmt.Errorf("agent: session %q not found", sessionID)
	}
	if len(sess.OpenInterrupts) == 0 {
		return nil, fmt.Errorf("agent: session %q has no open interrupts", sessionID)
	}

	open := make(map[string]metadata.Interrupt, len(sess.OpenInterrupts))
	for _, intr := range sess.OpenInterrupts {
		open[intr.ID] = intr
	}
	seen := make(map[string]bool, len(inputs))
	for _, inp := range inputs {
		if inp.InterruptID == "" {
			return nil, fmt.Errorf("agent: resume input missing interruptId")
		}
		if _, ok := open[inp.InterruptID]; !ok {
			return nil, fmt.Errorf("agent: unknown interruptId %q", inp.InterruptID)
		}
		if seen[inp.InterruptID] {
			return nil, fmt.Errorf("agent: duplicate resume input for interruptId %q", inp.InterruptID)
		}
		seen[inp.InterruptID] = true
	}
	for id := range open {
		if !seen[id] {
			return nil, fmt.Errorf("agent: resume must address interruptId %q", id)
		}
	}

	// Find the assistant message whose tool calls were paused.
	pausedIdx := -1
	for i := len(sess.Messages) - 1; i >= 0; i-- {
		m := sess.Messages[i]
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			if strings.HasPrefix(tc.Status, "pending") {
				pausedIdx = i
				break
			}
		}
		if pausedIdx >= 0 {
			break
		}
	}
	if pausedIdx < 0 {
		return nil, fmt.Errorf("agent: session %q has open interrupts but no paused tool-call batch", sessionID)
	}

	inputByToolCallID := make(map[string]ResumeInput, len(inputs))
	for _, inp := range inputs {
		intr := open[inp.InterruptID]
		if intr.ToolCallID == "" {
			continue
		}
		inputByToolCallID[intr.ToolCallID] = inp
	}

	events := make(chan Event, 100)

	go func() {
		defer close(events)

		threadID := sess.ID
		runID := generateID()
		events <- &RunStartedEvent{ThreadID: threadID, RunID: runID}

		registry, _, err := a.buildMergedRegistry(ctx)
		if err != nil {
			log.Printf("warning: failed to load dynamic tools: %v", err)
			registry = a.Tools
		}

		// Build provider messages from history through the paused assistant
		// turn (it already contains the tool_calls).
		providerMsgs := metadataToProviderMessages(sess.Messages[:pausedIdx+1])

		paused := &sess.Messages[pausedIdx]
		for i := range paused.ToolCalls {
			tc := paused.ToolCalls[i]
			resultStr, status := a.resolveToolCall(ctx, tc, inputByToolCallID, registry)
			paused.ToolCalls[i].Result = resultStr
			paused.ToolCalls[i].Status = status
			providerMsgs = append(providerMsgs, provider.Message{
				Role:      "tool",
				Content:   resultStr,
				ToolCalls: []provider.ToolCall{{ID: tc.ID, Name: tc.Name}},
			})
			events <- &ToolResultEvent{ToolCallID: tc.ID, Result: resultStr}
		}

		sess.OpenInterrupts = nil
		if err := a.Store.UpdateSession(context.Background(), sess); err != nil {
			log.Printf("session save failed: %v", err)
		}

		a.continueAgenticLoop(ctx, sess, providerMsgs, events, threadID, runID)
	}()

	return events, nil
}

// resolveToolCall synthesizes a tool-result payload for a single paused tool
// call by combining the persisted record with the caller's resume input.
//
// When the call is a client-side tool, the caller's payload is used as the
// result (common keys: "output" or arbitrary shape). When the call is
// approval-gated, {"approved": true} triggers inline execution, while
// {"approved": false} or [ResumeCancelled] yields a user-rejection error
// payload. Unknown payload shapes are stored verbatim.
func (a *Agent) resolveToolCall(
	ctx context.Context,
	rec metadata.ToolCallRecord,
	inputByToolCallID map[string]ResumeInput,
	registry *tool.Registry,
) (string, string) {
	inp, gated := inputByToolCallID[rec.ID]
	if !gated {
		// Not interrupt-bound — execute inline using the persisted args.
		return a.executeTool(ctx, rec, registry)
	}

	if inp.Status == ResumeCancelled {
		b, _ := json.Marshal(map[string]string{"error": "cancelled"})
		return string(b), "failed"
	}

	switch rec.Status {
	case "pending_approval":
		if approved, ok := asApprovalPayload(inp.Payload); ok {
			if !approved {
				b, _ := json.Marshal(map[string]string{"error": "user rejected"})
				return string(b), "failed"
			}
			return a.executeTool(ctx, rec, registry)
		}
		// Unknown payload shape — treat as rejection to be safe.
		b, _ := json.Marshal(map[string]string{"error": "user rejected"})
		return string(b), "failed"

	case "pending_client":
		return clientSidePayloadJSON(inp.Payload), "completed"
	}

	// Any other status (shouldn't happen) — fall through to serialise payload.
	b, err := json.Marshal(inp.Payload)
	if err != nil {
		b, _ = json.Marshal(map[string]string{"error": err.Error()})
		return string(b), "failed"
	}
	return string(b), "completed"
}

// executeTool runs the tool referenced by rec using its persisted arguments.
func (a *Agent) executeTool(
	ctx context.Context,
	rec metadata.ToolCallRecord,
	registry *tool.Registry,
) (string, string) {
	t := registry.Get(rec.Name)
	if t == nil {
		b, _ := json.Marshal(map[string]string{
			"error": fmt.Sprintf("tool %q not found", rec.Name),
		})
		return string(b), "failed"
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(rec.Arguments), &input); err != nil {
		b, _ := json.Marshal(map[string]string{
			"error": fmt.Sprintf("invalid arguments: %v", err),
		})
		return string(b), "failed"
	}
	result, err := t.Execute(ctx, input)
	if err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error()), "failed"
	}
	b, _ := json.Marshal(result.Data)
	return string(b), "completed"
}

// asApprovalPayload reads the canonical {"approved": bool} shape out of a
// resume payload. It accepts a raw bool or a map with an "approved" key.
func asApprovalPayload(payload any) (bool, bool) {
	switch v := payload.(type) {
	case nil:
		return false, false
	case bool:
		return v, true
	case map[string]any:
		if b, ok := v["approved"].(bool); ok {
			return b, true
		}
	}
	return false, false
}

// clientSidePayloadJSON serialises a client-side resume payload into a
// tool-result string. If the payload carries an "output" key, that value is
// used verbatim; otherwise the full payload is serialised.
func clientSidePayloadJSON(payload any) string {
	if m, ok := payload.(map[string]any); ok {
		if out, ok := m["output"]; ok {
			b, err := json.Marshal(out)
			if err == nil {
				return string(b)
			}
		}
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return `{"error": "invalid payload"}`
	}
	return string(b)
}
