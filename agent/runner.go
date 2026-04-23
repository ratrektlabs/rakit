package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ratrektlabs/rakit/protocol"
	"github.com/ratrektlabs/rakit/provider"
	"github.com/ratrektlabs/rakit/skill"
	"github.com/ratrektlabs/rakit/storage/metadata"
	"github.com/ratrektlabs/rakit/tool"
)

// Run starts the agent with its default protocol (no session persistence).
func (a *Agent) Run(ctx context.Context, input string) (<-chan protocol.Event, error) {
	return a.RunWithProtocol(ctx, input, a.Protocol)
}

// RunWithProtocol starts the agent with a specific output protocol (single-turn, no persistence).
func (a *Agent) RunWithProtocol(
	ctx context.Context,
	input string,
	p protocol.Protocol,
) (<-chan protocol.Event, error) {
	if a.Provider == nil {
		return nil, fmt.Errorf("agent: no provider configured")
	}
	if p == nil {
		return nil, fmt.Errorf("agent: no protocol configured")
	}

	registry, systemPrompt, err := a.buildMergedRegistry(ctx)
	if err != nil {
		log.Printf("warning: failed to load dynamic tools: %v", err)
		registry = a.Tools // fallback to static tools only
	}

	events := make(chan protocol.Event, 100)

	go func() {
		defer close(events)

		threadID := generateID()
		runID := generateID()

		// Emit RunStarted
		events <- &protocol.RunStartedEvent{ThreadID: threadID, RunID: runID}

		req := &provider.Request{
			Model:    a.Provider.Model(),
			Messages: []provider.Message{{Role: "user", Content: input}},
			Tools:    registry.Schema(),
			System:   systemPrompt,
		}

		stream, err := a.Provider.Stream(ctx, req)
		if err != nil {
			events <- &protocol.ErrorEvent{Err: err}
			return
		}

		var textMessageID string
		textStarted := false

		for event := range stream {
			// Emit TextStart before first text delta
			if _, ok := event.(*provider.TextDeltaEvent); ok && !textStarted {
				textStarted = true
				textMessageID = generateID()
				events <- &protocol.TextStartEvent{MessageID: textMessageID, Role: "assistant"}
			}

			protoEvent := convertEvent(event)
			if protoEvent == nil {
				continue
			}

			for _, h := range a.hooks {
				if err := h.OnEvent(ctx, protoEvent); err != nil {
					events <- &protocol.ErrorEvent{Err: err}
				}
			}

			events <- protoEvent
		}

		// Emit TextEnd if we started a text message
		if textStarted {
			events <- &protocol.TextEndEvent{MessageID: textMessageID}
		}

		// Emit RunFinished
		events <- &protocol.RunFinishedEvent{ThreadID: threadID, RunID: runID}
	}()

	return events, nil
}

// RunWithSession starts the agent with session persistence, compaction, and an agentic loop.
// It loads the session, merges tools from skills + persisted tools + static tools,
// then loops: stream from provider -> accumulate text + tool calls -> execute tools ->
// feed results back -> loop until text-only response or max iterations.
func (a *Agent) RunWithSession(
	ctx context.Context,
	sessionID string,
	input string,
	p protocol.Protocol,
) (<-chan protocol.Event, error) {
	if a.Provider == nil {
		return nil, fmt.Errorf("agent: no provider configured")
	}
	if p == nil {
		return nil, fmt.Errorf("agent: no protocol configured")
	}
	if a.Store == nil {
		return nil, fmt.Errorf("agent: no store configured")
	}

	// 1. Load session
	sess, err := a.Store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("agent: load session %q: %w", sessionID, err)
	}
	if sess == nil {
		return nil, fmt.Errorf("agent: session %q not found", sessionID)
	}

	// 2. Append user message
	sess.Messages = append(sess.Messages, metadata.Message{
		ID:        generateID(),
		Role:      "user",
		Content:   input,
		CreatedAt: time.Now().UnixMilli(),
	})

	// 3. Compact if needed
	if shouldCompact(sess.Messages, a.compaction) {
		compacted, err := compact(ctx, a.Provider, sess.Messages, a.compaction)
		if err != nil {
			log.Printf("compaction failed: %v", err)
		} else {
			sess.Messages = compacted
		}
	}

	events := make(chan protocol.Event, 100)

	runCtx, cancel := context.WithCancel(ctx)
	a.runCancels.Store(sess.ID, cancel)

	go func() {
		defer close(events)
		defer a.runCancels.Delete(sess.ID)
		defer cancel()

		threadID := sess.ID
		runID := generateID()
		events <- &protocol.RunStartedEvent{ThreadID: threadID, RunID: runID}

		providerMsgs := metadataToProviderMessages(sess.Messages)
		a.agenticLoop(runCtx, events, sess, providerMsgs)

		events <- &protocol.RunFinishedEvent{ThreadID: threadID, RunID: runID}

		// Save session using Background ctx so the write survives request
		// cancellation and client Interrupts.
		if err := a.Store.UpdateSession(context.Background(), sess); err != nil {
			log.Printf("session save failed: %v", err)
		}
	}()

	return events, nil
}

// ToolDecision resolves a pending tool call when resuming a paused agent run.
// Each decision is matched to a pending tool call by ToolCallID.
//
//   - For "pending_approval" calls:
//
//   - Approve=true  → the server executes the registered tool (unless
//     Result is set, which overrides execution with the provided value).
//
//   - Approve=false → the agent sees a synthetic rejection result so it
//     can react naturally (apologize, ask again, etc.). Message, if
//     set, is included in the rejection payload.
//
//   - For "pending_client" calls:
//
//   - Result is required and is passed through as the tool's output.
//     Approve is ignored (the client has already executed the tool).
type ToolDecision struct {
	ToolCallID string
	Approve    bool
	Result     string
	Message    string
}

// Resume continues a paused agent run by resolving its pending tool calls.
//
// A run pauses when it encounters a tool call that requires human approval
// (per ApprovalPolicy) or is client-side (per the ClientSide marker
// interface). Resume applies the provided decisions to the session's pending
// tool calls, emits a ToolResultEvent for each, and re-enters the agentic
// loop so the model can react to the tool outputs.
func (a *Agent) Resume(
	ctx context.Context,
	sessionID string,
	decisions []ToolDecision,
	p protocol.Protocol,
) (<-chan protocol.Event, error) {
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

	pendingIdx := findPendingToolCallsMessage(sess.Messages)
	if pendingIdx < 0 {
		return nil, fmt.Errorf("agent: no pending tool calls on session %q", sessionID)
	}

	decMap := make(map[string]ToolDecision, len(decisions))
	for _, d := range decisions {
		decMap[d.ToolCallID] = d
	}

	registry, _, err := a.buildMergedRegistry(ctx)
	if err != nil {
		log.Printf("warning: failed to load dynamic tools: %v", err)
		registry = a.Tools
	}

	events := make(chan protocol.Event, 100)

	runCtx, cancel := context.WithCancel(ctx)
	a.runCancels.Store(sess.ID, cancel)

	go func() {
		defer close(events)
		defer a.runCancels.Delete(sess.ID)
		defer cancel()

		threadID := sess.ID
		runID := generateID()
		events <- &protocol.RunStartedEvent{ThreadID: threadID, RunID: runID}

		// Resolve each pending tool call on the session's paused assistant
		// message. Write results back into the stored ToolCallRecords so the
		// history is self-consistent across subsequent calls.
		for i := range sess.Messages[pendingIdx].ToolCalls {
			rec := &sess.Messages[pendingIdx].ToolCalls[i]
			if !isPendingStatus(rec.Status) {
				continue
			}
			dec, hasDec := decMap[rec.ID]
			resultStr, status := a.resolvePendingCall(runCtx, rec, dec, hasDec, registry)
			rec.Result = resultStr
			rec.Status = status

			events <- &protocol.ToolResultEvent{
				ToolCallID: rec.ID,
				Result:     resultStr,
			}
		}

		// Continue the agentic loop with the freshly-resolved history.
		providerMsgs := metadataToProviderMessages(sess.Messages)
		a.agenticLoop(runCtx, events, sess, providerMsgs)

		events <- &protocol.RunFinishedEvent{ThreadID: threadID, RunID: runID}

		if err := a.Store.UpdateSession(context.Background(), sess); err != nil {
			log.Printf("session save failed: %v", err)
		}
	}()

	return events, nil
}

// Interrupt cancels an in-flight agent run for the given session. It is a
// no-op if no run is currently active. Safe to call from any goroutine.
//
// Interrupt signals the runner via context cancellation; the runner finishes
// its current provider stream and exits between iterations. Any partially
// completed tool calls are preserved on the session so Resume can complete
// or reject them later.
func (a *Agent) Interrupt(sessionID string) {
	a.runCancels.Range(func(key, value any) bool {
		if key == sessionID {
			if cancel, ok := value.(context.CancelFunc); ok {
				cancel()
			}
		}
		return true
	})
}

// findPendingToolCallsMessage returns the index of the most recent assistant
// message that carries at least one pending tool call, or -1 if none.
func findPendingToolCallsMessage(msgs []metadata.Message) int {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "assistant" {
			continue
		}
		for _, tc := range msgs[i].ToolCalls {
			if isPendingStatus(tc.Status) {
				return i
			}
		}
	}
	return -1
}

// resolvePendingCall turns a (ToolCallRecord, ToolDecision) into a concrete
// result string and a final status ("completed" or "failed").
func (a *Agent) resolvePendingCall(
	ctx context.Context,
	rec *metadata.ToolCallRecord,
	dec ToolDecision,
	hasDec bool,
	registry *tool.Registry,
) (string, string) {
	if !hasDec {
		return rejectionResult(rec.ID, "no decision provided for pending tool call"), "failed"
	}

	// Approval-gated call that was rejected.
	if rec.Status == "pending_approval" && !dec.Approve {
		msg := dec.Message
		if msg == "" {
			msg = "user rejected the tool call"
		}
		return rejectionResult(rec.ID, msg), "failed"
	}

	// Caller provided an explicit result — use it verbatim.
	if dec.Result != "" {
		return dec.Result, "completed"
	}

	// Client-side call without a result is a protocol error.
	if rec.Status == "pending_client" {
		return rejectionResult(rec.ID, "client tool resolved without a result"), "failed"
	}

	// Approval-gated call that was approved — execute the tool server-side.
	t := registry.Get(rec.Name)
	if t == nil {
		return rejectionResult(rec.ID, fmt.Sprintf("tool %q not found at resume time", rec.Name)), "failed"
	}
	var input map[string]any
	if rec.Arguments != "" {
		if err := json.Unmarshal([]byte(rec.Arguments), &input); err != nil {
			return rejectionResult(rec.ID, fmt.Sprintf("invalid arguments: %v", err)), "failed"
		}
	}
	result, err := t.Execute(ctx, input)
	if err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error()), "failed"
	}
	b, _ := json.Marshal(result.Data)
	return string(b), "completed"
}

func rejectionResult(toolCallID, msg string) string {
	b, _ := json.Marshal(map[string]string{
		"error":      msg,
		"toolCallId": toolCallID,
	})
	return string(b)
}

// agenticLoop runs the core provider-stream / tool-exec iteration until the
// model returns a text-only response, the run pauses for human-in-the-loop,
// max iterations is reached, or ctx is cancelled. It mutates sess.Messages
// and expects the caller to persist the session after it returns.
//
// This is shared by RunWithSession (after appending the user message) and by
// Resume (after resolving pending tool calls).
func (a *Agent) agenticLoop(
	ctx context.Context,
	events chan<- protocol.Event,
	sess *metadata.Session,
	providerMsgs []provider.Message,
) {
	for i := 0; i < a.maxIterations; i++ {
		if ctx.Err() != nil {
			events <- &protocol.ErrorEvent{Err: ctx.Err()}
			return
		}

		// Build merged tool registry per-iteration (dynamic tool loading)
		registry, systemPrompt, err := a.buildMergedRegistry(ctx)
		if err != nil {
			log.Printf("warning: failed to load dynamic tools: %v", err)
			registry = a.Tools
		}

		req := &provider.Request{
			Model:    a.Provider.Model(),
			Messages: providerMsgs,
			Tools:    registry.Schema(),
			System:   systemPrompt,
		}

		stream, err := a.Provider.Stream(ctx, req)
		if err != nil {
			events <- &protocol.ErrorEvent{Err: err}
			return
		}

		var responseContent string
		var responseToolCalls []provider.ToolCall
		var textMessageID string
		textStarted := false

		for event := range stream {
			switch ev := event.(type) {
			case *provider.TextDeltaEvent:
				if !textStarted {
					textStarted = true
					textMessageID = generateID()
					startEvt := &protocol.TextStartEvent{MessageID: textMessageID, Role: "assistant"}
					for _, h := range a.hooks {
						if err := h.OnEvent(ctx, startEvt); err != nil {
							events <- &protocol.ErrorEvent{Err: err}
						}
					}
					events <- startEvt
				}
				responseContent += ev.Delta
			case *provider.ToolCallEvent:
				responseToolCalls = append(responseToolCalls, provider.ToolCall{
					ID:               ev.ID,
					Name:             ev.Name,
					Arguments:        ev.Arguments,
					ThoughtSignature: ev.ThoughtSignature,
				})
			}

			protoEvent := convertEvent(event)
			if protoEvent == nil {
				continue
			}

			for _, h := range a.hooks {
				if err := h.OnEvent(ctx, protoEvent); err != nil {
					events <- &protocol.ErrorEvent{Err: err}
				}
			}

			events <- protoEvent
		}

		if textStarted {
			endEvt := &protocol.TextEndEvent{MessageID: textMessageID}
			for _, h := range a.hooks {
				if err := h.OnEvent(ctx, endEvt); err != nil {
					events <- &protocol.ErrorEvent{Err: err}
				}
			}
			events <- endEvt
		}

		// No tool calls — final response, save and break.
		if len(responseToolCalls) == 0 {
			if responseContent != "" {
				sess.Messages = append(sess.Messages, metadata.Message{
					ID:        generateID(),
					Role:      "assistant",
					Content:   responseContent,
					CreatedAt: time.Now().UnixMilli(),
				})
			}
			return
		}

		// Save assistant message with tool calls to both provider history
		// and the persisted session.
		assistantMsg := provider.Message{
			Role:      "assistant",
			Content:   responseContent,
			ToolCalls: responseToolCalls,
		}
		providerMsgs = append(providerMsgs, assistantMsg)

		tcRecords := providerToolCallsToRecords(responseToolCalls)
		assistantMsgIdx := len(sess.Messages)
		sess.Messages = append(sess.Messages, metadata.Message{
			ID:        generateID(),
			Role:      "assistant",
			Content:   responseContent,
			ToolCalls: tcRecords,
			CreatedAt: time.Now().UnixMilli(),
		})

		// Emit the full argument payload for each call so clients can
		// render the request pane immediately.
		for _, tc := range responseToolCalls {
			events <- &protocol.ToolCallArgsEvent{
				ToolCallID: tc.ID,
				Delta:      tc.Arguments,
			}
		}

		// Human-in-the-loop classification: if any call in this batch
		// requires approval (per ApprovalPolicy) or is a client-side tool,
		// pause the run. Persist the pending state, emit
		// ToolCallPendingEvents, and return — Resume will continue the run
		// once the caller provides decisions.
		pendingReasons := make([]string, len(responseToolCalls))
		anyPending := false
		for idx, tc := range responseToolCalls {
			t := registry.Get(tc.Name)
			if t == nil {
				continue
			}
			switch {
			case isClientSide(t):
				pendingReasons[idx] = "client_side"
				anyPending = true
			case a.approval != nil && a.approval.Require(tc):
				pendingReasons[idx] = "approval_required"
				anyPending = true
			}
		}
		if anyPending {
			for idx, reason := range pendingReasons {
				if reason == "" {
					continue
				}
				status := "pending_approval"
				if reason == "client_side" {
					status = "pending_client"
				}
				if idx < len(sess.Messages[assistantMsgIdx].ToolCalls) {
					sess.Messages[assistantMsgIdx].ToolCalls[idx].Status = status
				}
				events <- &protocol.ToolCallPendingEvent{
					ToolCallID: responseToolCalls[idx].ID,
					ToolName:   responseToolCalls[idx].Name,
					Arguments:  responseToolCalls[idx].Arguments,
					Reason:     reason,
				}
			}
			return
		}

		// Execute each tool call server-side.
		for i, tc := range responseToolCalls {
			resultStr, status := a.executeToolCall(ctx, tc, registry)

			if i < len(sess.Messages[assistantMsgIdx].ToolCalls) {
				sess.Messages[assistantMsgIdx].ToolCalls[i].Result = resultStr
				sess.Messages[assistantMsgIdx].ToolCalls[i].Status = status
			}

			providerMsgs = append(providerMsgs, provider.Message{
				Role:      "tool",
				Content:   resultStr,
				ToolCalls: []provider.ToolCall{{ID: tc.ID, Name: tc.Name}},
			})

			events <- &protocol.ToolResultEvent{
				ToolCallID: tc.ID,
				Result:     resultStr,
			}
		}
	}
}

// executeToolCall invokes a single registered tool, returning its serialized
// result and a terminal status ("completed" or "failed"). Unknown tools and
// malformed arguments are converted to friendly errors the model can read.
func (a *Agent) executeToolCall(
	ctx context.Context,
	tc provider.ToolCall,
	registry *tool.Registry,
) (string, string) {
	t := registry.Get(tc.Name)
	if t == nil {
		return rejectionResult(tc.ID, fmt.Sprintf("tool %q not found", tc.Name)), "failed"
	}
	var input map[string]any
	if tc.Arguments != "" {
		if err := json.Unmarshal([]byte(tc.Arguments), &input); err != nil {
			return rejectionResult(tc.ID, fmt.Sprintf("invalid arguments: %v", err)), "failed"
		}
	}
	result, err := t.Execute(ctx, input)
	if err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error()), "failed"
	}
	b, _ := json.Marshal(result.Data)
	return string(b), "completed"
}

// RunSubagent spawns a child agent, creates a session, runs it, and collects the final text response.
// This is the method called by the built-in spawn_agent tool.
func (a *Agent) RunSubagent(ctx context.Context, parentSessionID, task, system string, p protocol.Protocol) (string, error) {
	child := a.Spawn(ctx, parentSessionID, SubagentConfig{
		System:       system,
		InheritTools: true,
	})

	sess, err := child.CreateSession(ctx)
	if err != nil {
		return "", fmt.Errorf("subagent: create session: %w", err)
	}

	events, err := child.RunWithSession(ctx, sess.ID, task, p)
	if err != nil {
		return "", fmt.Errorf("subagent: run: %w", err)
	}

	// Collect all text deltas into the final response
	var result strings.Builder
	for e := range events {
		if delta, ok := e.(*protocol.TextDeltaEvent); ok {
			result.WriteString(delta.Delta)
		}
	}

	return result.String(), nil
}

// SpawnAgentTool returns a tool.Tool that lets the LLM spawn subagents.
func (a *Agent) SpawnAgentTool(p protocol.Protocol) tool.Tool {
	return tool.NewFunctionTool(
		"spawn_agent",
		"Spawn a subagent to handle a subtask autonomously. The subagent inherits all tools and skills. Use this for complex subtasks that benefit from independent reasoning.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task": map[string]any{
					"type":        "string",
					"description": "The task or question for the subagent to handle",
				},
				"instructions": map[string]any{
					"type":        "string",
					"description": "System instructions for the subagent (optional)",
				},
			},
			"required": []string{"task"},
		},
		func(ctx context.Context, input map[string]any) (*tool.Result, error) {
			taskStr, _ := input["task"].(string)
			instructions, _ := input["instructions"].(string)

			// Get the session ID from context if available
			sessionID := ""
			if sid, ok := ctx.Value(sessionIDKey).(string); ok {
				sessionID = sid
			}

			result, err := a.RunSubagent(ctx, sessionID, taskStr, instructions, p)
			if err != nil {
				return tool.Err(err.Error(), "Check that the agent has a provider configured"), nil
			}
			return tool.Ok(result), nil
		},
	)
}

// contextKey type for session ID in context
type contextKey string

const sessionIDKey contextKey = "sessionID"

// buildMergedRegistry creates a per-run tool registry that merges tools from
// enabled skills, persisted tool definitions, MCP servers, and statically registered tools.
// It also collects instructions from all enabled skills into a single system prompt.
// Precedence: static tools > MCP tools > persisted tools > skill tools (last registered wins).
func (a *Agent) buildMergedRegistry(ctx context.Context) (*tool.Registry, string, error) {
	registry := tool.NewRegistry()
	var instructions []string

	// 1. Load tools from enabled skills
	if a.Skills != nil && a.Store != nil {
		entries, err := a.Skills.List(ctx)
		if err != nil {
			return nil, "", fmt.Errorf("list skills: %w", err)
		}

		for _, entry := range entries {
			if !entry.Enabled {
				continue
			}

			def, err := a.Skills.Get(ctx, entry.Name)
			if err != nil {
				log.Printf("warning: failed to load skill %q: %v", entry.Name, err)
				continue
			}

			// Collect instructions
			if def.Instructions != "" {
				instructions = append(instructions, fmt.Sprintf("[%s]\n%s", def.Name, def.Instructions))
			}

			rm := skill.NewResourceManager(a.FS)
			for _, raw := range def.Tools {
				// def.Tools is []any (from JSON storage); convert to skill.ToolDef
				rawJSON, err := json.Marshal(raw)
				if err != nil {
					log.Printf("warning: failed to marshal tool def from skill %q: %v", entry.Name, err)
					continue
				}
				var td skill.ToolDef
				if err := json.Unmarshal(rawJSON, &td); err != nil {
					log.Printf("warning: failed to unmarshal tool def from skill %q: %v", entry.Name, err)
					continue
				}
				t, err := skill.ToolFromDef(td, rm)
				if err != nil {
					log.Printf("warning: failed to build tool %q from skill %q: %v", td.Name, entry.Name, err)
					continue
				}
				registry.Register(t)
			}
		}
	}

	// 2. Load persisted tools from metadata store
	if a.Store != nil {
		toolDefs, err := a.Store.ListTools(ctx, a.ID)
		if err != nil {
			return nil, "", fmt.Errorf("list tools: %w", err)
		}

		rm := skill.NewResourceManager(a.FS)
		for _, td := range toolDefs {
			skillTD := skill.ToolDef{
				Name:          td.Name,
				Description:   td.Description,
				Parameters:    td.Parameters,
				Handler:       td.Handler,
				Endpoint:      td.Endpoint,
				Headers:       td.Headers,
				InputMapping:  td.InputMapping,
				ResponseField: td.ResponseField,
				ScriptPath:    td.ScriptPath,
			}
			t, err := skill.ToolFromDef(skillTD, rm)
			if err != nil {
				log.Printf("warning: failed to build persisted tool %q: %v", td.Name, err)
				continue
			}
			registry.Register(t)
		}
	}

	// 3. Load tools from MCP servers
	if a.MCP != nil {
		mcpTools, err := a.MCP.DiscoverTools(ctx, a.ID)
		if err != nil {
			log.Printf("warning: MCP tool discovery failed: %v", err)
		} else {
			for _, t := range mcpTools {
				registry.Register(t)
			}
		}
	}

	// 4. Static tools (highest priority — registered last, overwrites on collision)
	for _, t := range a.Tools.All() {
		registry.Register(t)
	}

	return registry, strings.Join(instructions, "\n\n"), nil
}

func convertEvent(e provider.Event) protocol.Event {
	switch ev := e.(type) {
	case *provider.TextDeltaEvent:
		return &protocol.TextDeltaEvent{Delta: ev.Delta}
	case *provider.ToolCallEvent:
		return &protocol.ToolCallStartEvent{
			ToolCallID:   ev.ID,
			ToolCallName: ev.Name,
		}
	case *provider.ToolResultProviderEvent:
		return &protocol.ToolResultEvent{
			ToolCallID: ev.ID,
			Result:     ev.Result,
		}
	case *provider.DoneProviderEvent:
		return &protocol.DoneEvent{}
	case *provider.ErrorProviderEvent:
		return &protocol.ErrorEvent{Err: ev.Err}
	}
	return nil
}
