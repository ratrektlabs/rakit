package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ratrektlabs/rl-agent/agent"
	"github.com/ratrektlabs/rl-agent/provider"
	"github.com/ratrektlabs/rl-agent/tool"
)

type mockProvider struct {
	name string
	resp *provider.CompletionResponse
	err  error
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{SupportsStreaming: true, SupportsToolCalling: true}
}
func (m *mockProvider) Complete(ctx context.Context, req provider.CompletionRequest) (*provider.CompletionResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.resp != nil {
		return m.resp, nil
	}
	return &provider.CompletionResponse{Content: "test response", FinishReason: "stop"}, nil
}
func (m *mockProvider) Stream(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan provider.StreamEvent, 2)
	go func() {
		defer close(ch)
		ch <- provider.StreamEvent{Type: provider.StreamEventContentDelta, Delta: "hello "}
		ch <- provider.StreamEvent{Type: provider.StreamEventContentDelta, Delta: "world"}
		ch <- provider.StreamEvent{Type: provider.StreamEventDone, FinishReason: "stop"}
	}()
	return ch, nil
}

func newTestHandler(t *testing.T) *Handler {
	p := &mockProvider{name: "mock"}
	cfg := agent.DefaultConfig(p)
	ag := agent.New(cfg)
	registry := tool.NewRegistry()
	h, err := NewHandler(ag, registry)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	return h
}

func TestNewHandler_NilAgent(t *testing.T) {
	_, err := NewHandler(nil, nil)
	if err != ErrMissingAgent {
		t.Errorf("expected ErrMissingAgent, got %v", err)
	}
}

func TestNewHandler_Success(t *testing.T) {
	h := newTestHandler(t)
	if h == nil {
		t.Error("expected handler, got nil")
	}
}

func TestWithPrefix(t *testing.T) {
	h := newTestHandler(t)
	h2, err := NewHandler(h.agent, h.registry, WithPrefix("/v1"))
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	if h2.prefix != "/v1" {
		t.Errorf("expected prefix /v1, got %s", h2.prefix)
	}
}

func TestHandler_Health(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("expected status 'ok', got %s", resp.Status)
	}
}

func TestHandler_Health_Prefix(t *testing.T) {
	p := &mockProvider{name: "mock"}
	ag := agent.New(agent.DefaultConfig(p))
	h, _ := NewHandler(ag, tool.NewRegistry(), WithPrefix("/v1"))

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestHandler_Run(t *testing.T) {
	h := newTestHandler(t)
	body := RunRequest{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hello"}},
	}
	data, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/run", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp RunResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Content != "test response" {
		t.Errorf("expected content 'test response', got %s", resp.Content)
	}
}

func TestHandler_Run_InvalidBody(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/run", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestHandler_Run_NoMessages(t *testing.T) {
	h := newTestHandler(t)
	body := RunRequest{Messages: []provider.Message{}}
	data, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/run", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestHandler_Run_MethodNotAllowed(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/run", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestHandler_Stream(t *testing.T) {
	h := newTestHandler(t)
	body := RunRequest{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hello"}},
		Stream:   true,
	}
	data, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/stream", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected content-type 'text/event-stream', got %s", rec.Header().Get("Content-Type"))
	}
}

func TestHandler_Stream_InvalidBody(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/stream", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestHandler_Stream_NoMessages(t *testing.T) {
	h := newTestHandler(t)
	body := RunRequest{Messages: []provider.Message{}}
	data, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/stream", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestHandler_ListTools(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/tools", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var tools []ToolInfo
	if err := json.NewDecoder(rec.Body).Decode(&tools); err != nil {
		t.Fatalf("decode error: %v", err)
	}
}

func TestHandler_RegisterTool(t *testing.T) {
	h := newTestHandler(t)
	body := RegisterToolRequest{
		Name:        "test-tool",
		Description: "A test tool",
	}
	data, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/tools", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, rec.Code)
	}

	var resp ToolInfo
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Name != "test-tool" {
		t.Errorf("expected name 'test-tool', got %s", resp.Name)
	}
}

func TestHandler_RegisterTool_MissingName(t *testing.T) {
	h := newTestHandler(t)
	body := RegisterToolRequest{Name: ""}
	data, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/tools", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestHandler_RegisterTool_Duplicate(t *testing.T) {
	h := newTestHandler(t)
	body := RegisterToolRequest{Name: "dup-tool"}
	data, _ := json.Marshal(body)

	req1 := httptest.NewRequest(http.MethodPost, "/api/tools", bytes.NewReader(data))
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusCreated {
		t.Fatalf("first register failed: %d", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/tools", bytes.NewReader(data))
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusConflict {
		t.Errorf("expected status %d, got %d", http.StatusConflict, rec2.Code)
	}
}

func TestHandler_CORS(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodOptions, "/api/run", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected CORS header")
	}
}

func TestHandler_NotFound(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/unknown", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestJsonTool(t *testing.T) {
	jt := &jsonTool{
		name:        "test",
		description: "desc",
		parameters: map[string]tool.ParameterSchema{
			"arg": {Type: tool.TypeString, Description: "arg desc"},
		},
	}

	if jt.Name() != "test" {
		t.Errorf("expected name 'test', got %s", jt.Name())
	}
	if jt.Description() != "desc" {
		t.Errorf("expected description 'desc', got %s", jt.Description())
	}
	if len(jt.Parameters()) != 1 {
		t.Errorf("expected 1 parameter, got %d", len(jt.Parameters()))
	}

	result, err := jt.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.(map[string]any)["name"] != "test" {
		t.Error("unexpected execute result")
	}
}
