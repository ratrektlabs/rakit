package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

	registry, err := a.buildMergedRegistry(ctx)
	if err != nil {
		log.Printf("warning: failed to load dynamic tools: %v", err)
		registry = a.Tools // fallback to static tools only
	}

	events := make(chan protocol.Event, 100)

	go func() {
		defer close(events)

		req := &provider.Request{
			Model:    a.Provider.Model(),
			Messages: []provider.Message{{Role: "user", Content: input}},
			Tools:    registry.Schema(),
		}

		stream, err := a.Provider.Stream(ctx, req)
		if err != nil {
			events <- &protocol.ErrorEvent{Err: err}
			return
		}

		for event := range stream {
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
	}()

	return events, nil
}

// RunWithSession starts the agent with session persistence, compaction, and an agentic loop.
// It loads the session, merges tools from skills + persisted tools + static tools,
// then loops: stream from provider → accumulate text + tool calls → execute tools →
// feed results back → loop until text-only response or max iterations.
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

	// 4. Build merged tool registry (skill tools + persisted tools + static tools)
	registry, err := a.buildMergedRegistry(ctx)
	if err != nil {
		log.Printf("warning: failed to load dynamic tools: %v", err)
		registry = a.Tools
	}

	events := make(chan protocol.Event, 100)

	go func() {
		defer close(events)

		// Build initial provider messages from session history
		providerMsgs := metadataToProviderMessages(sess.Messages)

		// Agentic loop
		for i := 0; i < a.maxIterations; i++ {
			// Check context cancellation between iterations
			if ctx.Err() != nil {
				events <- &protocol.ErrorEvent{Err: ctx.Err()}
				return
			}

			// Build request
			req := &provider.Request{
				Model:    a.Provider.Model(),
				Messages: providerMsgs,
				Tools:    registry.Schema(),
			}

			// Stream from provider
			stream, err := a.Provider.Stream(ctx, req)
			if err != nil {
				events <- &protocol.ErrorEvent{Err: err}
				return
			}

			// Accumulate response
			var responseContent string
			var responseToolCalls []provider.ToolCall

			for event := range stream {
				switch ev := event.(type) {
				case *provider.TextDeltaEvent:
					responseContent += ev.Delta
				case *provider.ToolCallEvent:
					responseToolCalls = append(responseToolCalls, provider.ToolCall{
						ID:        ev.ID,
						Name:      ev.Name,
						Arguments: ev.Arguments,
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

			sess.Messages = append(sess.Messages, metadata.Message{
				ID:        generateID(),
				Role:      "assistant",
				Content:   responseContent,
				ToolCalls: providerToolCallsToRecords(responseToolCalls),
				CreatedAt: time.Now().UnixMilli(),
			})

			// Execute each tool call
			for _, tc := range responseToolCalls {
				t := registry.Get(tc.Name)
				if t == nil {
					resultJSON, _ := json.Marshal(map[string]string{
						"error": fmt.Sprintf("tool %q not found", tc.Name),
					})
					providerMsgs = append(providerMsgs, provider.Message{
						Role:    "tool",
						Content: string(resultJSON),
					})

					events <- &protocol.ToolResultEvent{
						ToolCallID: tc.ID,
						Result:     string(resultJSON),
					}
					continue
				}

				var input map[string]any
				if err := json.Unmarshal([]byte(tc.Arguments), &input); err != nil {
					resultJSON, _ := json.Marshal(map[string]string{
						"error": fmt.Sprintf("invalid arguments: %v", err),
					})
					providerMsgs = append(providerMsgs, provider.Message{
						Role:    "tool",
						Content: string(resultJSON),
					})

					events <- &protocol.ToolResultEvent{
						ToolCallID: tc.ID,
						Result:     string(resultJSON),
					}
					continue
				}

				result, err := t.Execute(ctx, input)
				var resultStr string
				if err != nil {
					resultStr = fmt.Sprintf(`{"error": "%s"`, err.Error())
				} else {
					b, _ := json.Marshal(result)
					resultStr = string(b)
				}

				// Append tool result for next iteration
				providerMsgs = append(providerMsgs, provider.Message{
					Role:    "tool",
					Content: resultStr,
				})

				events <- &protocol.ToolResultEvent{
					ToolCallID: tc.ID,
					Result:     resultStr,
				}
			}
		}

		// Save session
		if err := a.Store.UpdateSession(ctx, sess); err != nil {
			log.Printf("session save failed: %v", err)
		}
	}()

	return events, nil
}

// buildMergedRegistry creates a per-run tool registry that merges tools from
// enabled skills, persisted tool definitions, and statically registered tools.
// Precedence: static tools > persisted tools > skill tools (last registered wins).
func (a *Agent) buildMergedRegistry(ctx context.Context) (*tool.Registry, error) {
	registry := tool.NewRegistry()

	// 1. Load tools from enabled skills
	if a.Skills != nil && a.Store != nil {
		entries, err := a.Skills.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("list skills: %w", err)
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

			rm := skill.NewResourceManager(a.FS)
			for _, td := range def.Tools {
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
			return nil, fmt.Errorf("list tools: %w", err)
		}

		rm := skill.NewResourceManager(a.FS)
		for _, td := range toolDefs {
			skillTD := skill.ToolDef{
				Name:         td.Name,
				Description:  td.Description,
				Parameters:   td.Parameters,
				Handler:      td.Handler,
				Endpoint:     td.Endpoint,
				Headers:      td.Headers,
				InputMapping: td.InputMapping,
				ScriptPath:   td.ScriptPath,
			}
			t, err := skill.ToolFromDef(skillTD, rm)
			if err != nil {
				log.Printf("warning: failed to build persisted tool %q: %v", td.Name, err)
				continue
			}
			registry.Register(t)
		}
	}

	// 3. Static tools (highest priority — registered last, overwrites on collision)
	for _, t := range a.Tools.All() {
		registry.Register(t)
	}

	return registry, nil
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
