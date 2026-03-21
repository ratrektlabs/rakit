package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/ratrektlabs/rl-agent/agent"
	"github.com/ratrektlabs/rl-agent/provider"
)

type EventType string

const (
	EventTypeMessage    EventType = "message"
	EventTypeDelta      EventType = "delta"
	EventTypeToolCall   EventType = "tool_call"
	EventTypeToolResult EventType = "tool_result"
	EventTypeDone       EventType = "done"
	EventTypeError      EventType = "error"
	EventTypeHeartbeat  EventType = "heartbeat"
)

type Event struct {
	Type      EventType   `json:"type"`
	Timestamp int64       `json:"timestamp"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
}

type MessageData struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type DeltaData struct {
	Delta string `json:"delta"`
}

type ToolCallData struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolResultData struct {
	ToolCallID string      `json:"tool_call_id"`
	Result     interface{} `json:"result"`
	Success    bool        `json:"success"`
	Error      string      `json:"error,omitempty"`
}

type Streamer struct {
	agent     *agent.Agent
	events    chan Event
	mu        sync.RWMutex
	closed    bool
	heartbeat time.Duration
}

type Option func(*Streamer)

func WithHeartbeat(d time.Duration) Option {
	return func(s *Streamer) {
		s.heartbeat = d
	}
}

func NewStreamer(a *agent.Agent, opts ...Option) *Streamer {
	s := &Streamer{
		agent:     a,
		events:    make(chan Event, 256),
		heartbeat: 15 * time.Second,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Streamer) Events() <-chan Event {
	return s.events
}

func (s *Streamer) Stream(ctx context.Context, messages []provider.Message, opts ...agent.RunOption) error {
	agentEvents, err := s.agent.RunStream(ctx, messages, opts...)
	if err != nil {
		s.emit(Event{
			Type:  EventTypeError,
			Error: err.Error(),
		})
		return err
	}

	go func() {
		defer s.Close()

		for agentEvent := range agentEvents {
			switch agentEvent.Type {
			case agent.StreamEventTypeStepStart:
				s.emit(Event{
					Type: EventTypeMessage,
					Data: MessageData{
						Role:    "assistant",
						Content: "",
					},
				})

			case agent.StreamEventTypeContentDelta:
				s.emit(Event{
					Type: EventTypeDelta,
					Data: DeltaData{
						Delta: agentEvent.Delta,
					},
				})

			case agent.StreamEventTypeToolCall:
				if agentEvent.ToolCall != nil {
					tc := agentEvent.ToolCall
					s.emit(Event{
						Type: EventTypeToolCall,
						Data: ToolCallData{
							ID:        tc.ID,
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						},
					})
				}

			case agent.StreamEventTypeToolResult:
				if agentEvent.ToolResult != nil {
					result := agentEvent.ToolResult
					var resultErr string
					if !result.Success {
						resultErr = result.Error
					}
					s.emit(Event{
						Type: EventTypeToolResult,
						Data: ToolResultData{
							ToolCallID: result.ToolName,
							Result:     result.Result,
							Success:    result.Success,
							Error:      resultErr,
						},
					})
				}

			case agent.StreamEventTypeError:
				errMsg := ""
				if agentEvent.Error != nil {
					errMsg = agentEvent.Error.Error()
				}
				s.emit(Event{
					Type:  EventTypeError,
					Error: errMsg,
				})
				return

			case agent.StreamEventTypeFinished:
				s.emit(Event{
					Type: EventTypeDone,
				})
			}
		}
	}()

	return nil
}

func (s *Streamer) emit(event Event) {
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

func (s *Streamer) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.closed {
		s.closed = true
		close(s.events)
	}
}

func WriteEvent(w io.Writer, event Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

type responseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func (rw *responseWriter) Write(p []byte) (int, error) {
	n, err := rw.w.Write(p)
	if rw.flusher != nil {
		rw.flusher.Flush()
	}
	return n, err
}

func (rw *responseWriter) WriteString(s string) (int, error) {
	n, err := io.WriteString(rw.w, s)
	if rw.flusher != nil {
		rw.flusher.Flush()
	}
	return n, err
}

func HTTPHandler(a *agent.Agent) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		StreamHTTPHandler(a, w, r)
	}
}

func StreamHTTPHandler(a *agent.Agent, w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	var messages []provider.Message
	if err := json.NewDecoder(r.Body).Decode(&messages); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	streamer := NewStreamer(a)
	if err := streamer.Stream(ctx, messages); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rw := &responseWriter{w: w, flusher: flusher}

	for {
		select {
		case <-ctx.Done():
			streamer.Close()
			return
		case event, ok := <-streamer.Events():
			if !ok {
				return
			}
			if err := WriteEvent(rw, event); err != nil {
				streamer.Close()
				return
			}
		}
	}
}

func NewStreamEndpoint(a *agent.Agent) http.HandlerFunc {
	return HTTPHandler(a)
}
