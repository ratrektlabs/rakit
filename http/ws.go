package http

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/ratrektlabs/rl-agent/agent"
	"github.com/ratrektlabs/rl-agent/provider"
)

type WSMessageType string

const (
	WSMessageRun    WSMessageType = "run"
	WSMessageStream WSMessageType = "stream"
	WSMessageCancel WSMessageType = "cancel"
	WSMessagePing   WSMessageType = "ping"
	WSMessagePong   WSMessageType = "pong"
)

type WSEventType string

const (
	WSEventRunStarted   WSEventType = "run_started"
	WSEventRunFinished  WSEventType = "run_finished"
	WSEventStepStart    WSEventType = "step_start"
	WSEventStepEnd      WSEventType = "step_end"
	WSEventContentDelta WSEventType = "content_delta"
	WSEventToolCall     WSEventType = "tool_call"
	WSEventToolResult   WSEventType = "tool_result"
	WSEventError        WSEventType = "error"
	WSEventCancelled    WSEventType = "cancelled"
	WSEventPong         WSEventType = "pong"
)

type WSMessage struct {
	Type    WSMessageType   `json:"type"`
	ID      string          `json:"id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type WSEvent struct {
	Type      WSEventType                `json:"type"`
	ID        string                     `json:"id,omitempty"`
	Timestamp int64                      `json:"timestamp"`
	Step      int                        `json:"step,omitempty"`
	Delta     string                     `json:"delta,omitempty"`
	ToolCall  *provider.ToolCall         `json:"tool_call,omitempty"`
	Result    *agent.ToolExecutionResult `json:"result,omitempty"`
	Error     string                     `json:"error,omitempty"`
	Output    *agent.RunOutput           `json:"output,omitempty"`
}

type WSRunPayload struct {
	Messages []provider.Message `json:"messages"`
	Options  *RunOptions        `json:"options,omitempty"`
}

type WSServerConfig struct {
	ReadBufferSize  int
	WriteBufferSize int
	PingInterval    time.Duration
	PongWait        time.Duration
	WriteWait       time.Duration
}

type WSServerOption func(*WSServerConfig)

func WithReadBufferSize(size int) WSServerOption {
	return func(c *WSServerConfig) {
		c.ReadBufferSize = size
	}
}

func WithWriteBufferSize(size int) WSServerOption {
	return func(c *WSServerConfig) {
		c.WriteBufferSize = size
	}
}

func WithPingInterval(d time.Duration) WSServerOption {
	return func(c *WSServerConfig) {
		c.PingInterval = d
	}
}

type WSConnection interface {
	SendJSON(v interface{}) error
	ReceiveJSON(v interface{}) error
	Close() error
	Context() context.Context
}

type WSUpgrader interface {
	Upgrade(w http.ResponseWriter, r *http.Request) (WSConnection, error)
}

type WSServer struct {
	agent    *agent.Agent
	config   *WSServerConfig
	upgrader WSUpgrader
	mu       sync.RWMutex
	conns    map[string]WSConnection
}

func NewWSServer(a *agent.Agent, opts ...WSServerOption) *WSServer {
	config := &WSServerConfig{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		PingInterval:    30 * time.Second,
		PongWait:        60 * time.Second,
		WriteWait:       10 * time.Second,
	}
	for _, opt := range opts {
		opt(config)
	}

	return &WSServer{
		agent:  a,
		config: config,
		conns:  make(map[string]WSConnection),
	}
}

func (s *WSServer) SetUpgrader(u WSUpgrader) {
	s.upgrader = u
}

func (s *WSServer) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.upgrader == nil {
			http.Error(w, "websocket upgrader not configured", http.StatusInternalServerError)
			return
		}

		conn, err := s.upgrader.Upgrade(w, r)
		if err != nil {
			return
		}
		defer conn.Close()

		connID := generateConnID()
		s.addConnection(connID, conn)
		defer s.removeConnection(connID)

		s.handleConnection(conn)
	}
}

func (s *WSServer) handleConnection(conn WSConnection) {
	ctx := conn.Context()

	for {
		var msg WSMessage
		if err := conn.ReceiveJSON(&msg); err != nil {
			return
		}

		switch msg.Type {
		case WSMessageRun:
			s.handleRun(ctx, conn, msg)
		case WSMessageStream:
			s.handleStream(ctx, conn, msg)
		case WSMessageCancel:
			s.sendEvent(conn, WSEvent{
				Type:      WSEventCancelled,
				ID:        msg.ID,
				Timestamp: time.Now().UnixMilli(),
			})
		case WSMessagePing:
			s.sendEvent(conn, WSEvent{
				Type:      WSEventPong,
				ID:        msg.ID,
				Timestamp: time.Now().UnixMilli(),
			})
		}
	}
}

func (s *WSServer) handleRun(ctx context.Context, conn WSConnection, msg WSMessage) {
	var payload WSRunPayload
	if len(msg.Payload) > 0 {
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			s.sendError(conn, msg.ID, "invalid payload")
			return
		}
	}

	if len(payload.Messages) == 0 {
		s.sendError(conn, msg.ID, "messages are required")
		return
	}

	s.sendEvent(conn, WSEvent{
		Type:      WSEventRunStarted,
		ID:        msg.ID,
		Timestamp: time.Now().UnixMilli(),
	})

	opts := buildOptsFromPayload(payload.Options)
	output, err := s.agent.Run(ctx, payload.Messages, opts...)
	if err != nil {
		s.sendError(conn, msg.ID, err.Error())
		return
	}

	s.sendEvent(conn, WSEvent{
		Type:      WSEventRunFinished,
		ID:        msg.ID,
		Timestamp: time.Now().UnixMilli(),
		Output:    output,
	})
}

func (s *WSServer) handleStream(ctx context.Context, conn WSConnection, msg WSMessage) {
	var payload WSRunPayload
	if len(msg.Payload) > 0 {
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			s.sendError(conn, msg.ID, "invalid payload")
			return
		}
	}

	if len(payload.Messages) == 0 {
		s.sendError(conn, msg.ID, "messages are required")
		return
	}

	s.sendEvent(conn, WSEvent{
		Type:      WSEventRunStarted,
		ID:        msg.ID,
		Timestamp: time.Now().UnixMilli(),
	})

	opts := buildOptsFromPayload(payload.Options)
	events, err := s.agent.RunStream(ctx, payload.Messages, opts...)
	if err != nil {
		s.sendError(conn, msg.ID, err.Error())
		return
	}

	for event := range events {
		var wsEvent WSEvent
		wsEvent.ID = msg.ID
		wsEvent.Timestamp = time.Now().UnixMilli()

		switch event.Type {
		case agent.StreamEventTypeStepStart:
			wsEvent.Type = WSEventStepStart
			wsEvent.Step = event.Step

		case agent.StreamEventTypeContentDelta:
			wsEvent.Type = WSEventContentDelta
			wsEvent.Delta = event.Delta

		case agent.StreamEventTypeToolCall:
			wsEvent.Type = WSEventToolCall
			wsEvent.ToolCall = event.ToolCall

		case agent.StreamEventTypeToolResult:
			wsEvent.Type = WSEventToolResult
			wsEvent.Result = event.ToolResult

		case agent.StreamEventTypeStepEnd:
			wsEvent.Type = WSEventStepEnd
			wsEvent.Step = event.Step

		case agent.StreamEventTypeError:
			wsEvent.Type = WSEventError
			if event.Error != nil {
				wsEvent.Error = event.Error.Error()
			}

		case agent.StreamEventTypeFinished:
			wsEvent.Type = WSEventRunFinished
		}

		if err := s.sendEvent(conn, wsEvent); err != nil {
			return
		}
	}
}

func (s *WSServer) sendEvent(conn WSConnection, event WSEvent) error {
	return conn.SendJSON(event)
}

func (s *WSServer) sendError(conn WSConnection, id, errMsg string) {
	s.sendEvent(conn, WSEvent{
		Type:      WSEventError,
		ID:        id,
		Timestamp: time.Now().UnixMilli(),
		Error:     errMsg,
	})
}

func (s *WSServer) addConnection(id string, conn WSConnection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns[id] = conn
}

func (s *WSServer) removeConnection(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, id)
}

func (s *WSServer) Broadcast(event WSEvent) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, conn := range s.conns {
		if err := conn.SendJSON(event); err != nil {
			continue
		}
	}
	return nil
}

func buildOptsFromPayload(opts *RunOptions) []agent.RunOption {
	if opts == nil {
		return nil
	}

	var runOpts []agent.RunOption

	if opts.SessionID != "" {
		runOpts = append(runOpts, agent.WithSession(opts.SessionID))
	}
	if opts.UserID != "" {
		runOpts = append(runOpts, agent.WithUser(opts.UserID))
	}
	if opts.MaxSteps > 0 {
		runOpts = append(runOpts, agent.WithRunMaxSteps(opts.MaxSteps))
	}

	return runOpts
}

func generateConnID() string {
	return "conn_" + time.Now().Format("20060102150405")
}

type stdWSConnection struct {
	conn   interface{}
	ctx    context.Context
	send   chan json.RawMessage
	recv   chan json.RawMessage
	close  chan struct{}
	mu     sync.Mutex
	closed bool
}

func NewStdWSConnection(ctx context.Context) *stdWSConnection {
	return &stdWSConnection{
		ctx:   ctx,
		send:  make(chan json.RawMessage, 256),
		recv:  make(chan json.RawMessage, 256),
		close: make(chan struct{}),
	}
}

func (c *stdWSConnection) SendJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	select {
	case c.send <- data:
	default:
	}
	return nil
}

func (c *stdWSConnection) ReceiveJSON(v interface{}) error {
	select {
	case data := <-c.recv:
		return json.Unmarshal(data, v)
	case <-c.ctx.Done():
		return c.ctx.Err()
	case <-c.close:
		return nil
	}
}

func (c *stdWSConnection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.closed {
		c.closed = true
		close(c.close)
	}
	return nil
}

func (c *stdWSConnection) Context() context.Context {
	return c.ctx
}

func (c *stdWSConnection) OnMessage(data json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.closed {
		select {
		case c.recv <- data:
		default:
		}
	}
}

func (c *stdWSConnection) Outgoing() <-chan json.RawMessage {
	return c.send
}

func WSHandler(a *agent.Agent, upgrader WSUpgrader) http.HandlerFunc {
	server := NewWSServer(a)
	server.SetUpgrader(upgrader)
	return server.Handler()
}
