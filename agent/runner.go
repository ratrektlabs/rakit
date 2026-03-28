package agent

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ratrektlabs/rakit/protocol"
	"github.com/ratrektlabs/rakit/provider"
	"github.com/ratrektlabs/rakit/storage/metadata"
)

// Run starts the agent with its default protocol (no session persistence).
func (a *Agent) Run(ctx context.Context, input string) (<-chan protocol.Event, error) {
	return a.RunWithProtocol(ctx, input, a.Protocol)
}

// RunWithProtocol starts the agent with a specific output protocol.
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

	events := make(chan protocol.Event, 100)

	go func() {
		defer close(events)

		// Build request
		req := &provider.Request{
			Model:    a.Provider.Model(),
			Messages: []provider.Message{{Role: "user", Content: input}},
			Tools:    a.Tools.Schema(),
		}

		// Stream from provider
		stream, err := a.Provider.Stream(ctx, req)
		if err != nil {
			events <- &protocol.ErrorEvent{Err: err}
			return
		}

		for event := range stream {
			// Convert provider events to protocol events
			protoEvent := convertEvent(event)
			if protoEvent == nil {
				continue
			}

			// Apply hooks
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

// RunWithSession starts the agent with session persistence and compaction.
// It loads the session, appends the user message, runs compaction if needed,
// sends the full history to the provider, and saves the response back.
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

	// 4. Build provider request with full history
	req := &provider.Request{
		Model:    a.Provider.Model(),
		Messages: metadataToProviderMessages(sess.Messages),
		Tools:    a.Tools.Schema(),
	}

	events := make(chan protocol.Event, 100)

	go func() {
		defer close(events)

		// 5. Stream from provider
		stream, err := a.Provider.Stream(ctx, req)
		if err != nil {
			events <- &protocol.ErrorEvent{Err: err}
			return
		}

		// 6. Accumulate response for session persistence
		var responseContent string
		var responseToolCalls []provider.ToolCall

		for event := range stream {
			// Accumulate content/tool calls
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

		// 7. Save assistant response back to session
		if responseContent != "" || len(responseToolCalls) > 0 {
			sess.Messages = append(sess.Messages, metadata.Message{
				ID:        generateID(),
				Role:      "assistant",
				Content:   responseContent,
				ToolCalls: providerToolCallsToRecords(responseToolCalls),
				CreatedAt: time.Now().UnixMilli(),
			})
			if err := a.Store.UpdateSession(ctx, sess); err != nil {
				log.Printf("session save failed: %v", err)
			}
		}
	}()

	return events, nil
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
	case *provider.DoneProviderEvent:
		return &protocol.DoneEvent{}
	case *provider.ErrorProviderEvent:
		return &protocol.ErrorEvent{Err: ev.Err}
	}
	return nil
}
