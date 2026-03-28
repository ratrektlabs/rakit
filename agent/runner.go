package agent

import (
	"context"
	"fmt"

	"github.com/ratrektlabs/rl-agent/protocol"
	"github.com/ratrektlabs/rl-agent/provider"
)

// Run starts the agent with its default protocol.
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
