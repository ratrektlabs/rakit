package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ratrektlabs/rakit/provider"
	"github.com/ratrektlabs/rakit/skill"
	"github.com/ratrektlabs/rakit/storage/metadata"
	"github.com/ratrektlabs/rakit/tool"
)

// Run starts the agent with its default protocol (no session persistence).
func (a *Agent) Run(ctx context.Context, input string) (<-chan Event, error) {
	return a.RunWithProtocol(ctx, input, a.Protocol)
}

// RunWithProtocol starts the agent with a specific output protocol (single-turn, no persistence).
func (a *Agent) RunWithProtocol(
	ctx context.Context,
	input string,
	p Encoder,
) (<-chan Event, error) {
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

	events := make(chan Event, 100)

	go func() {
		defer close(events)

		threadID := generateID()
		runID := generateID()

		// Emit RunStarted
		events <- &RunStartedEvent{ThreadID: threadID, RunID: runID}

		req := &provider.Request{
			Model:    a.Provider.Model(),
			Messages: []provider.Message{{Role: "user", Content: input}},
			Tools:    registry.Schema(),
			System:   systemPrompt,
		}

		stream, err := a.Provider.Stream(ctx, req)
		if err != nil {
			events <- &ErrorEvent{Err: err}
			return
		}

		var textMessageID string
		textStarted := false

		for event := range stream {
			// Emit TextStart before first text delta
			if _, ok := event.(*provider.TextDeltaEvent); ok && !textStarted {
				textStarted = true
				textMessageID = generateID()
				events <- &TextStartEvent{MessageID: textMessageID, Role: "assistant"}
			}

			protoEvent := convertEvent(event)
			if protoEvent == nil {
				continue
			}

			for _, h := range a.hooks {
				if err := h.OnEvent(ctx, protoEvent); err != nil {
					events <- &ErrorEvent{Err: err}
				}
			}

			events <- protoEvent
		}

		// Emit TextEnd if we started a text message
		if textStarted {
			events <- &TextEndEvent{MessageID: textMessageID}
		}

		// Emit RunFinished
		events <- &RunFinishedEvent{ThreadID: threadID, RunID: runID}
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

	events := make(chan Event, 100)

	go func() {
		defer close(events)

		threadID := sess.ID
		runID := generateID()
		events <- &RunStartedEvent{ThreadID: threadID, RunID: runID}

		// Build initial provider messages from session history
		providerMsgs := metadataToProviderMessages(sess.Messages)

		a.continueAgenticLoop(ctx, sess, providerMsgs, events, threadID, runID)
	}()

	return events, nil
}

// continueAgenticLoop drives the provider ↔ tool loop for a session that
// already has its user/tool messages staged in providerMsgs. Both
// [Agent.RunWithSession] and [Agent.Resume] funnel into this method. It
// returns after emitting a terminal [RunFinishedEvent] (or an [ErrorEvent]
// on fatal failure) and persisting the session.
func (a *Agent) continueAgenticLoop(
	ctx context.Context,
	sess *metadata.Session,
	providerMsgs []provider.Message,
	events chan<- Event,
	threadID, runID string,
) {
	// Agentic loop
	for i := 0; i < a.maxIterations; i++ {
		// Check context cancellation between iterations
		if ctx.Err() != nil {
			events <- &ErrorEvent{Err: ctx.Err()}
			return
		}

		// 4. Build merged tool registry per-iteration (dynamic tool loading)
		registry, systemPrompt, err := a.buildMergedRegistry(ctx)
		if err != nil {
			log.Printf("warning: failed to load dynamic tools: %v", err)
			registry = a.Tools
		}

		// Build request
		req := &provider.Request{
			Model:    a.Provider.Model(),
			Messages: providerMsgs,
			Tools:    registry.Schema(),
			System:   systemPrompt,
		}

		// Stream from provider
		stream, err := a.Provider.Stream(ctx, req)
		if err != nil {
			events <- &ErrorEvent{Err: err}
			return
		}

		// Accumulate response
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
					startEvt := &TextStartEvent{MessageID: textMessageID, Role: "assistant"}
					for _, h := range a.hooks {
						if err := h.OnEvent(ctx, startEvt); err != nil {
							events <- &ErrorEvent{Err: err}
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
					events <- &ErrorEvent{Err: err}
				}
			}

			events <- protoEvent
		}

		// Emit TextEnd if we started a text message
		if textStarted {
			endEvt := &TextEndEvent{MessageID: textMessageID}
			for _, h := range a.hooks {
				if err := h.OnEvent(ctx, endEvt); err != nil {
					events <- &ErrorEvent{Err: err}
				}
			}
			events <- endEvt
		}

		// No tool calls — final response, save and break
		if len(responseToolCalls) == 0 {
			if responseContent != "" {
				sess.Messages = append(sess.Messages, metadata.Message{
					ID:        generateID(),
					Role:      "assistant",
					Content:   responseContent,
					CreatedAt: time.Now().UnixMilli(),
				})
			}
			break
		}

		// Save assistant message with tool calls to history
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

		// Emit tool call arguments so clients can display request data
		for _, tc := range responseToolCalls {
			events <- &ToolCallArgsEvent{
				ToolCallID: tc.ID,
				Delta:      tc.Arguments,
			}
		}

		// Human-in-the-loop classification. If any tool call requires
		// approval or is client-side, raise interrupts for the whole
		// batch and end the run with OutcomeInterrupt. The caller
		// resolves them via Agent.Resume, which re-enters this loop.
		if intrs := a.classifyPending(responseToolCalls, registry); len(intrs) > 0 {
			for i, tc := range responseToolCalls {
				if i >= len(sess.Messages[assistantMsgIdx].ToolCalls) {
					break
				}
				sess.Messages[assistantMsgIdx].ToolCalls[i].Status = pendingStatusFor(tc.ID, intrs)
			}
			sess.OpenInterrupts = interruptsToMetadata(intrs)
			if err := a.Store.UpdateSession(context.Background(), sess); err != nil {
				log.Printf("session save failed: %v", err)
			}
			events <- &RunFinishedEvent{
				ThreadID:   threadID,
				RunID:      runID,
				Outcome:    OutcomeInterrupt,
				Interrupts: intrs,
			}
			return
		}

		// Execute each tool call
		for i, tc := range responseToolCalls {
			t := registry.Get(tc.Name)
			if t == nil {
				resultJSON, _ := json.Marshal(map[string]string{
					"error": fmt.Sprintf("tool %q not found", tc.Name),
				})
				resultStr := string(resultJSON)
				providerMsgs = append(providerMsgs, provider.Message{
					Role:      "tool",
					Content:   resultStr,
					ToolCalls: []provider.ToolCall{{ID: tc.ID, Name: tc.Name}},
				})

				// Backfill result into session message
				if i < len(sess.Messages[assistantMsgIdx].ToolCalls) {
					sess.Messages[assistantMsgIdx].ToolCalls[i].Result = resultStr
					sess.Messages[assistantMsgIdx].ToolCalls[i].Status = "failed"
				}

				events <- &ToolResultEvent{
					ToolCallID: tc.ID,
					Result:     resultStr,
				}
				continue
			}

			var input map[string]any
			if err := json.Unmarshal([]byte(tc.Arguments), &input); err != nil {
				resultJSON, _ := json.Marshal(map[string]string{
					"error": fmt.Sprintf("invalid arguments: %v", err),
				})
				resultStr := string(resultJSON)
				providerMsgs = append(providerMsgs, provider.Message{
					Role:      "tool",
					Content:   resultStr,
					ToolCalls: []provider.ToolCall{{ID: tc.ID, Name: tc.Name}},
				})

				if i < len(sess.Messages[assistantMsgIdx].ToolCalls) {
					sess.Messages[assistantMsgIdx].ToolCalls[i].Result = resultStr
					sess.Messages[assistantMsgIdx].ToolCalls[i].Status = "failed"
				}

				events <- &ToolResultEvent{
					ToolCallID: tc.ID,
					Result:     resultStr,
				}
				continue
			}

			result, err := t.Execute(ctx, input)
			var resultStr string
			var status string
			if err != nil {
				resultStr = fmt.Sprintf(`{"error": "%s"}`, err.Error())
				status = "failed"
			} else {
				b, _ := json.Marshal(result.Data)
				resultStr = string(b)
				status = "completed"
			}

			// Backfill result into session message
			if i < len(sess.Messages[assistantMsgIdx].ToolCalls) {
				sess.Messages[assistantMsgIdx].ToolCalls[i].Result = resultStr
				sess.Messages[assistantMsgIdx].ToolCalls[i].Status = status
			}

			// Append tool result for next iteration
			providerMsgs = append(providerMsgs, provider.Message{
				Role:      "tool",
				Content:   resultStr,
				ToolCalls: []provider.ToolCall{{ID: tc.ID, Name: tc.Name}},
			})

			events <- &ToolResultEvent{
				ToolCallID: tc.ID,
				Result:     resultStr,
			}
		}
	}

	// Emit RunFinished
	events <- &RunFinishedEvent{ThreadID: threadID, RunID: runID, Outcome: OutcomeSuccess}

	// Save session (use Background context to survive request cancellation)
	if err := a.Store.UpdateSession(context.Background(), sess); err != nil {
		log.Printf("session save failed: %v", err)
	}
}

// RunSubagent spawns a child agent, creates a session, runs it, and collects the final text response.
// This is the method called by the built-in spawn_agent tool.
func (a *Agent) RunSubagent(ctx context.Context, parentSessionID, task, system string, p Encoder) (string, error) {
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
		if delta, ok := e.(*TextDeltaEvent); ok {
			result.WriteString(delta.Delta)
		}
	}

	return result.String(), nil
}

// SpawnAgentTool returns a tool.Tool that lets the LLM spawn subagents.
func (a *Agent) SpawnAgentTool(p Encoder) tool.Tool {
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

func convertEvent(e provider.Event) Event {
	switch ev := e.(type) {
	case *provider.TextDeltaEvent:
		return &TextDeltaEvent{Delta: ev.Delta}
	case *provider.ToolCallEvent:
		return &ToolCallStartEvent{
			ToolCallID:   ev.ID,
			ToolCallName: ev.Name,
		}
	case *provider.ToolResultProviderEvent:
		return &ToolResultEvent{
			ToolCallID: ev.ID,
			Result:     ev.Result,
		}
	case *provider.DoneProviderEvent:
		return &DoneEvent{}
	case *provider.ErrorProviderEvent:
		return &ErrorEvent{Err: ev.Err}
	}
	return nil
}
