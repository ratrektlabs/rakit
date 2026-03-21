package http

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/ratrektlabs/rl-agent/agent"
	"github.com/ratrektlabs/rl-agent/provider"
	"github.com/ratrektlabs/rl-agent/skill"
	"github.com/ratrektlabs/rl-agent/tool"
)

type RunRequest struct {
	Messages []provider.Message `json:"messages"`
	Options  *RunOptions        `json:"options,omitempty"`
}

type RunOptions struct {
	SessionID    string              `json:"session_id,omitempty"`
	UserID       string              `json:"user_id,omitempty"`
	MaxSteps     int                 `json:"max_steps,omitempty"`
	Tools        []ToolRegistration  `json:"tools,omitempty"`
	Skills       []SkillRegistration `json:"skills,omitempty"`
	ReplaceTools bool                `json:"replace_tools,omitempty"`
}

type ToolRegistration struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type SkillRegistration struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type RunResponse struct {
	Success     bool                        `json:"success"`
	Message     *provider.Message           `json:"message,omitempty"`
	Steps       int                         `json:"steps,omitempty"`
	Usage       *provider.Usage             `json:"usage,omitempty"`
	State       string                      `json:"state,omitempty"`
	ToolResults []agent.ToolExecutionResult `json:"tool_results,omitempty"`
	Error       string                      `json:"error,omitempty"`
	RequestID   string                      `json:"request_id,omitempty"`
	ProcessTime int64                       `json:"process_time_ms,omitempty"`
}

type StreamRequest struct {
	Messages []provider.Message `json:"messages"`
	Options  *RunOptions        `json:"options,omitempty"`
}

type ToolsListResponse struct {
	Tools []ToolInfo `json:"tools"`
}

type ToolInfo struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type SkillsListResponse struct {
	Skills []SkillInfo `json:"skills"`
}

type SkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type HealthResponse struct {
	Status    string `json:"status"`
	Timestamp int64  `json:"timestamp"`
	Version   string `json:"version,omitempty"`
}

type ErrorResponse struct {
	Error     string `json:"error"`
	Code      int    `json:"code"`
	RequestID string `json:"request_id,omitempty"`
}

type Middleware func(http.Handler) http.Handler

type HandlerConfig struct {
	RequestIDHeader    string
	RequestIDGenerator func() string
	Version            string
	RetryInterval      time.Duration
}

type HandlerOption func(*HandlerConfig)

func WithRequestIDHeader(header string) HandlerOption {
	return func(c *HandlerConfig) {
		c.RequestIDHeader = header
	}
}

func WithRequestIDGenerator(fn func() string) HandlerOption {
	return func(c *HandlerConfig) {
		c.RequestIDGenerator = fn
	}
}

func WithVersion(version string) HandlerOption {
	return func(c *HandlerConfig) {
		c.Version = version
	}
}

type AgentHandler struct {
	agent  *agent.Agent
	config *HandlerConfig
	mu     sync.RWMutex
}

func NewAgentHandler(a *agent.Agent, opts ...HandlerOption) *AgentHandler {
	config := &HandlerConfig{
		RequestIDHeader:    "X-Request-ID",
		RequestIDGenerator: defaultRequestIDGenerator,
		Version:            "1.0.0",
	}
	for _, opt := range opts {
		opt(config)
	}
	return &AgentHandler{
		agent:  a,
		config: config,
	}
}

func (h *AgentHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/run" && r.Method == http.MethodPost:
		h.handleRun(w, r)
	case path == "/stream" && r.Method == http.MethodPost:
		h.handleStream(w, r)
	case path == "/tools" && r.Method == http.MethodGet:
		h.handleListTools(w, r)
	case path == "/tools" && r.Method == http.MethodPost:
		h.handleRegisterTool(w, r)
	case path == "/skills" && r.Method == http.MethodGet:
		h.handleListSkills(w, r)
	case path == "/skills" && r.Method == http.MethodPost:
		h.handleRegisterSkill(w, r)
	case path == "/health" && r.Method == http.MethodGet:
		h.handleHealth(w, r)
	default:
		h.writeError(w, r, "not found", http.StatusNotFound)
	}
}

func (h *AgentHandler) handleRun(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := h.getRequestID(r)

	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorWithID(w, "invalid request body", http.StatusBadRequest, requestID)
		return
	}

	if len(req.Messages) == 0 {
		h.writeErrorWithID(w, "messages are required", http.StatusBadRequest, requestID)
		return
	}

	opts := h.buildRunOptions(req.Options)

	ctx := r.Context()
	output, err := h.agent.Run(ctx, req.Messages, opts...)
	if err != nil {
		h.writeErrorWithID(w, err.Error(), http.StatusInternalServerError, requestID)
		return
	}

	resp := RunResponse{
		Success:     true,
		Message:     &output.Message,
		Steps:       output.Steps,
		Usage:       &output.Usage,
		State:       string(output.State),
		ToolResults: output.ToolResults,
		RequestID:   requestID,
		ProcessTime: time.Since(start).Milliseconds(),
	}

	h.writeJSON(w, resp)
}

func (h *AgentHandler) handleStream(w http.ResponseWriter, r *http.Request) {
	var req StreamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, r, "invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.Messages) == 0 {
		h.writeError(w, r, "messages are required", http.StatusBadRequest)
		return
	}

	opts := h.buildRunOptions(req.Options)
	h.streamSSE(r.Context(), w, r, req.Messages, opts)
}

func (h *AgentHandler) handleListTools(w http.ResponseWriter, r *http.Request) {
	tools := h.agent.GetToolRegistry().List()

	toolInfos := make([]ToolInfo, len(tools))
	for i, t := range tools {
		toolInfos[i] = ToolInfo{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		}
	}

	h.writeJSON(w, ToolsListResponse{Tools: toolInfos})
}

func (h *AgentHandler) handleRegisterTool(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		Parameters  map[string]interface{} `json:"parameters"`
		Function    json.RawMessage        `json:"function"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, r, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		h.writeError(w, r, "tool name is required", http.StatusBadRequest)
		return
	}

	builder := tool.New(req.Name).
		Desc(req.Description).
		Action(func(ctx context.Context, params map[string]any) (any, error) {
			return map[string]interface{}{"registered": true, "name": req.Name}, nil
		})

	for name, param := range req.Parameters {
		if pm, ok := param.(map[string]interface{}); ok {
			desc, _ := pm["description"].(string)
			typ, _ := pm["type"].(string)
			required, _ := pm["required"].(bool)
			builder.Param(name, typ, desc, required)
		}
	}

	t, err := builder.Build()
	if err != nil {
		h.writeError(w, r, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.agent.AddTool(t); err != nil {
		h.writeError(w, r, err.Error(), http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, map[string]interface{}{
		"success": true,
		"name":    req.Name,
	})
}

func (h *AgentHandler) handleListSkills(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, SkillsListResponse{Skills: []SkillInfo{}})
}

func (h *AgentHandler) handleRegisterSkill(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, r, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		h.writeError(w, r, "skill name is required", http.StatusBadRequest)
		return
	}

	s, err := skill.New(req.Name).
		WithDescription(req.Description).
		Build()
	if err != nil {
		h.writeError(w, r, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.agent.AddSkill(s); err != nil {
		h.writeError(w, r, err.Error(), http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, map[string]interface{}{
		"success": true,
		"name":    req.Name,
	})
}

func (h *AgentHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now().Unix(),
		Version:   h.config.Version,
	})
}

func (h *AgentHandler) buildRunOptions(opts *RunOptions) []agent.RunOption {
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
	if opts.ReplaceTools {
		runOpts = append(runOpts, agent.ReplaceAllTools())
	}

	return runOpts
}

func (h *AgentHandler) getRequestID(r *http.Request) string {
	if id := r.Header.Get(h.config.RequestIDHeader); id != "" {
		return id
	}
	return h.config.RequestIDGenerator()
}

func (h *AgentHandler) writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (h *AgentHandler) writeError(w http.ResponseWriter, r *http.Request, msg string, code int) {
	requestID := h.getRequestID(r)
	h.writeErrorWithID(w, msg, code, requestID)
}

func (h *AgentHandler) writeErrorWithID(w http.ResponseWriter, msg string, code int, requestID string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set(h.config.RequestIDHeader, requestID)
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error:     msg,
		Code:      code,
		RequestID: requestID,
	})
}

func defaultRequestIDGenerator() string {
	return time.Now().Format("20060102150405") + randomString(8)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[time.Now().Nanosecond()%len(letters)]
	}
	return string(b)
}

func Chain(h http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

type HandlerBuilder struct {
	handler    *AgentHandler
	middleware []Middleware
}

func NewHandlerBuilder(a *agent.Agent) *HandlerBuilder {
	return &HandlerBuilder{
		handler: NewAgentHandler(a),
	}
}

func (b *HandlerBuilder) WithConfig(opts ...HandlerOption) *HandlerBuilder {
	b.handler = NewAgentHandler(b.handler.agent, opts...)
	return b
}

func (b *HandlerBuilder) Use(m Middleware) *HandlerBuilder {
	b.middleware = append(b.middleware, m)
	return b
}

func (b *HandlerBuilder) Build() http.Handler {
	if len(b.middleware) == 0 {
		return b.handler
	}
	return Chain(b.handler, b.middleware...)
}
