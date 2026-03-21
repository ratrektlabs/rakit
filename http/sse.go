package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ratrektlabs/rl-agent/agent"
	"github.com/ratrektlabs/rl-agent/provider"
)

type SSEEventType string

const (
	SSEEventStepStart    SSEEventType = "step_start"
	SSEEventStepEnd      SSEEventType = "step_end"
	SSEEventContentDelta SSEEventType = "content_delta"
	SSEEventToolCall     SSEEventType = "tool_call"
	SSEEventToolResult   SSEEventType = "tool_result"
	SSEEventFinished     SSEEventType = "finished"
	SSEEventError        SSEEventType = "error"
	SSEEventHeartbeat    SSEEventType = "heartbeat"
)

type SSEEvent struct {
	Type       SSEEventType               `json:"type"`
	Timestamp  int64                      `json:"timestamp"`
	Step       int                        `json:"step,omitempty"`
	Delta      string                     `json:"delta,omitempty"`
	ToolCall   *provider.ToolCall         `json:"tool_call,omitempty"`
	ToolResult *agent.ToolExecutionResult `json:"tool_result,omitempty"`
	Error      string                     `json:"error,omitempty"`
}

type SSEConfig struct {
	HeartbeatInterval time.Duration
	RetryInterval     time.Duration
	BufferSize        int
}

type SSEOption func(*SSEConfig)

func WithHeartbeat(d time.Duration) SSEOption {
	return func(c *SSEConfig) {
		c.HeartbeatInterval = d
	}
}

func WithRetry(d time.Duration) SSEOption {
	return func(c *SSEConfig) {
		c.RetryInterval = d
	}
}

func WithBufferSize(size int) SSEOption {
	return func(c *SSEConfig) {
		c.BufferSize = size
	}
}

type SSEStreamer struct {
	agent  *agent.Agent
	config *SSEConfig
	events chan SSEEvent
	mu     sync.RWMutex
	closed bool
}

func NewSSEStreamer(a *agent.Agent, opts ...SSEOption) *SSEStreamer {
	config := &SSEConfig{
		HeartbeatInterval: 15 * time.Second,
		RetryInterval:     3 * time.Second,
		BufferSize:        256,
	}
	for _, opt := range opts {
		opt(config)
	}

	return &SSEStreamer{
		agent:  a,
		config: config,
		events: make(chan SSEEvent, config.BufferSize),
	}
}

func (s *SSEStreamer) Events() <-chan SSEEvent {
	return s.events
}

func (s *SSEStreamer) Stream(ctx context.Context, messages []provider.Message, opts ...agent.RunOption) error {
	agentEvents, err := s.agent.RunStream(ctx, messages, opts...)
	if err != nil {
		s.emit(SSEEvent{
			Type:  SSEEventError,
			Error: err.Error(),
		})
		return err
	}

	go func() {
		defer s.Close()

		for agentEvent := range agentEvents {
			switch agentEvent.Type {
			case agent.StreamEventTypeStepStart:
				s.emit(SSEEvent{
					Type: SSEEventStepStart,
					Step: agentEvent.Step,
				})

			case agent.StreamEventTypeContentDelta:
				s.emit(SSEEvent{
					Type:  SSEEventContentDelta,
					Delta: agentEvent.Delta,
				})

			case agent.StreamEventTypeToolCall:
				s.emit(SSEEvent{
					Type:     SSEEventToolCall,
					ToolCall: agentEvent.ToolCall,
				})

			case agent.StreamEventTypeToolResult:
				s.emit(SSEEvent{
					Type:       SSEEventToolResult,
					ToolResult: agentEvent.ToolResult,
				})

			case agent.StreamEventTypeStepEnd:
				s.emit(SSEEvent{
					Type: SSEEventStepEnd,
					Step: agentEvent.Step,
				})

			case agent.StreamEventTypeError:
				errMsg := ""
				if agentEvent.Error != nil {
					errMsg = agentEvent.Error.Error()
				}
				s.emit(SSEEvent{
					Type:  SSEEventError,
					Error: errMsg,
				})
				return

			case agent.StreamEventTypeFinished:
				s.emit(SSEEvent{
					Type: SSEEventFinished,
				})
			}
		}
	}()

	return nil
}

func (s *SSEStreamer) emit(event SSEEvent) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return
	}

	event.Timestamp = time.Now().UnixMilli()

	select {
	case s.events <- event:
	default:
	}
}

func (s *SSEStreamer) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.closed {
		s.closed = true
		close(s.events)
	}
}

func (h *AgentHandler) streamSSE(ctx context.Context, w http.ResponseWriter, r *http.Request, messages []provider.Message, opts []agent.RunOption) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeError(w, r, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	streamer := NewSSEStreamer(h.agent, WithHeartbeat(15*time.Second))
	if err := streamer.Stream(ctx, messages, opts...); err != nil {
		h.writeError(w, r, err.Error(), http.StatusInternalServerError)
		return
	}

	heartbeat := time.NewTicker(streamer.config.HeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			streamer.Close()
			return

		case <-heartbeat.C:
			if err := writeSSEEvent(w, SSEEvent{Type: SSEEventHeartbeat}); err != nil {
				streamer.Close()
				return
			}
			flusher.Flush()

		case event, ok := <-streamer.Events():
			if !ok {
				return
			}
			if err := writeSSEEvent(w, event); err != nil {
				streamer.Close()
				return
			}
			flusher.Flush()

			if event.Type == SSEEventFinished || event.Type == SSEEventError {
				return
			}
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, event SSEEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	if event.Type != "" {
		fmt.Fprintf(w, "event: %s\n", event.Type)
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	return nil
}

func SSEHandler(a *agent.Agent, opts ...SSEOption) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		var req StreamRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if len(req.Messages) == 0 {
			http.Error(w, "messages are required", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		streamer := NewSSEStreamer(a, opts...)
		ctx := r.Context()

		if err := streamer.Stream(ctx, req.Messages); err != nil {
			writeSSEEvent(w, SSEEvent{Type: SSEEventError, Error: err.Error()})
			flusher.Flush()
			return
		}

		heartbeat := time.NewTicker(streamer.config.HeartbeatInterval)
		defer heartbeat.Stop()

		for {
			select {
			case <-ctx.Done():
				streamer.Close()
				return

			case <-heartbeat.C:
				writeSSEEvent(w, SSEEvent{Type: SSEEventHeartbeat})
				flusher.Flush()

			case event, ok := <-streamer.Events():
				if !ok {
					return
				}
				writeSSEEvent(w, event)
				flusher.Flush()

				if event.Type == SSEEventFinished || event.Type == SSEEventError {
					return
				}
			}
		}
	}
}

type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	mu      sync.Mutex
}

func NewSSEWriter(w http.ResponseWriter) *SSEWriter {
	flusher, _ := w.(http.Flusher)
	return &SSEWriter{
		w:       w,
		flusher: flusher,
	}
}

func (sw *SSEWriter) WriteEvent(event SSEEvent) error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	if event.Type != "" {
		fmt.Fprintf(sw.w, "event: %s\n", event.Type)
	}
	fmt.Fprintf(sw.w, "data: %s\n\n", data)

	if sw.flusher != nil {
		sw.flusher.Flush()
	}

	return nil
}

func (sw *SSEWriter) WriteMessage(eventType, data string) error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	if eventType != "" {
		fmt.Fprintf(sw.w, "event: %s\n", eventType)
	}
	fmt.Fprintf(sw.w, "data: %s\n\n", data)

	if sw.flusher != nil {
		sw.flusher.Flush()
	}

	return nil
}

func (sw *SSEWriter) SetHeaders() {
	sw.w.Header().Set("Content-Type", "text/event-stream")
	sw.w.Header().Set("Cache-Control", "no-cache")
	sw.w.Header().Set("Connection", "keep-alive")
	sw.w.Header().Set("X-Accel-Buffering", "no")
}
