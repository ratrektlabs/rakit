package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ratrektlabs/rl-agent/agent"
	"github.com/ratrektlabs/rl-agent/provider"
	"github.com/ratrektlabs/rl-agent/tool"
)

var (
	ErrMissingAgent  = errors.New("agent is required")
	ErrInvalidBody   = errors.New("invalid request body")
	ErrInvalidMethod = errors.New("method not allowed")
	ErrRouteNotFound = errors.New("route not found")
)

type RunRequest struct {
	Messages  []provider.Message `json:"messages"`
	SessionID string             `json:"session_id,omitempty"`
	MaxSteps  int                `json:"max_steps,omitempty"`
	Tools     []string           `json:"tools,omitempty"`
	Skills    []string           `json:"skills,omitempty"`
	Stream    bool               `json:"stream,omitempty"`
}

type RunResponse struct {
	Content      string              `json:"content,omitempty"`
	ToolCalls    []provider.ToolCall `json:"tool_calls,omitempty"`
	FinishReason string              `json:"finish_reason,omitempty"`
	Steps        int                 `json:"steps,omitempty"`
	Error        string              `json:"error,omitempty"`
}

type ToolInfo struct {
	Name        string                          `json:"name"`
	Description string                          `json:"description"`
	Parameters  map[string]tool.ParameterSchema `json:"parameters,omitempty"`
}

type RegisterToolRequest struct {
	Name        string                          `json:"name"`
	Description string                          `json:"description"`
	Parameters  map[string]tool.ParameterSchema `json:"parameters,omitempty"`
}

type HealthResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
	Version   string `json:"version"`
}

type ErrorResponse struct {
	Error string `json:"error"`
	Code  int    `json:"code"`
}

type SSEEvent struct {
	Type         string             `json:"type"`
	Delta        string             `json:"delta,omitempty"`
	ToolCall     *provider.ToolCall `json:"tool_call,omitempty"`
	Step         int                `json:"step,omitempty"`
	FinishReason string             `json:"finish_reason,omitempty"`
	Error        string             `json:"error,omitempty"`
}

type Handler struct {
	agent    agent.Agent
	registry tool.ToolRegistry
	prefix   string
	mu       sync.RWMutex
}

type HandlerOption func(*Handler)

func WithPrefix(prefix string) HandlerOption {
	return func(h *Handler) {
		h.prefix = prefix
	}
}

func NewHandler(ag agent.Agent, registry tool.ToolRegistry, opts ...HandlerOption) (*Handler, error) {
	if ag == nil {
		return nil, ErrMissingAgent
	}
	if registry == nil {
		registry = tool.NewRegistry()
	}
	h := &Handler{
		agent:    ag,
		registry: registry,
		prefix:   "/api",
	}
	for _, opt := range opts {
		opt(h)
	}
	return h, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	path := strings.TrimPrefix(r.URL.Path, h.prefix)
	if path == "" {
		path = "/"
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch {
	case path == "/health" && r.Method == http.MethodGet:
		h.handleHealth(w, r)
	case path == "/run" && r.Method == http.MethodPost:
		h.handleRun(w, r)
	case path == "/stream" && r.Method == http.MethodPost:
		h.handleStream(w, r)
	case path == "/tools" && r.Method == http.MethodGet:
		h.handleListTools(w, r)
	case path == "/tools" && r.Method == http.MethodPost:
		h.handleRegisterTool(w, r)
	default:
		h.writeError(w, http.StatusNotFound, ErrRouteNotFound)
	}
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, HealthResponse{
		Status:    "ok",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Version:   "2.0.0",
	})
}

func (h *Handler) handleRun(w http.ResponseWriter, r *http.Request) {
	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, ErrInvalidBody)
		return
	}

	if len(req.Messages) == 0 {
		h.writeError(w, http.StatusBadRequest, errors.New("messages are required"))
		return
	}

	ctx := r.Context()
	result, err := h.agent.Run(ctx, req.Messages)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, RunResponse{Error: err.Error()})
		return
	}

	h.writeJSON(w, http.StatusOK, RunResponse{
		Content:      result.Content,
		ToolCalls:    result.ToolCalls,
		FinishReason: string(result.FinishReason),
		Steps:        result.Steps,
	})
}

func (h *Handler) handleStream(w http.ResponseWriter, r *http.Request) {
	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, ErrInvalidBody)
		return
	}

	if len(req.Messages) == 0 {
		h.writeError(w, http.StatusBadRequest, errors.New("messages are required"))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, errors.New("streaming not supported"))
		return
	}

	ctx := r.Context()
	events, err := h.agent.Stream(ctx, req.Messages)
	if err != nil {
		h.writeSSE(w, flusher, SSEEvent{Type: "error", Error: err.Error()})
		return
	}

	for event := range events {
		if ctx.Err() != nil {
			return
		}
		sse := SSEEvent{
			Type:         string(event.Type),
			Delta:        event.Delta,
			Step:         event.Step,
			FinishReason: string(event.FinishReason),
		}
		if event.ToolCall != nil {
			sse.ToolCall = event.ToolCall
		}
		if event.Error != nil {
			sse.Error = event.Error.Error()
		}
		h.writeSSE(w, flusher, sse)
		if event.Type == agent.StreamEventDone || event.Type == agent.StreamEventError {
			break
		}
	}
}

func (h *Handler) handleListTools(w http.ResponseWriter, r *http.Request) {
	tools := h.registry.List()
	infos := make([]ToolInfo, len(tools))
	for i, t := range tools {
		infos[i] = ToolInfo{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		}
	}
	h.writeJSON(w, http.StatusOK, infos)
}

func (h *Handler) handleRegisterTool(w http.ResponseWriter, r *http.Request) {
	var req RegisterToolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, ErrInvalidBody)
		return
	}

	if req.Name == "" {
		h.writeError(w, http.StatusBadRequest, errors.New("tool name is required"))
		return
	}

	t := &jsonTool{
		name:        req.Name,
		description: req.Description,
		parameters:  req.Parameters,
	}

	if err := h.registry.Register(t); err != nil {
		h.writeError(w, http.StatusConflict, err)
		return
	}

	h.writeJSON(w, http.StatusCreated, ToolInfo{
		Name:        req.Name,
		Description: req.Description,
		Parameters:  req.Parameters,
	})
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, err error) {
	h.writeJSON(w, status, ErrorResponse{
		Error: err.Error(),
		Code:  status,
	})
}

func (h *Handler) writeSSE(w http.ResponseWriter, flusher http.Flusher, event SSEEvent) {
	data, _ := json.Marshal(event)
	w.Write([]byte("data: "))
	w.Write(data)
	w.Write([]byte("\n\n"))
	flusher.Flush()
}

type jsonTool struct {
	name        string
	description string
	parameters  map[string]tool.ParameterSchema
}

func (t *jsonTool) Name() string                                { return t.name }
func (t *jsonTool) Description() string                         { return t.description }
func (t *jsonTool) Parameters() map[string]tool.ParameterSchema { return t.parameters }
func (t *jsonTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return map[string]any{"registered": true, "name": t.name}, nil
}
