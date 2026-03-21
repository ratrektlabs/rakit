package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/ratrektlabs/rl-agent/agent"
	"github.com/ratrektlabs/rl-agent/provider"
	"github.com/ratrektlabs/rl-agent/provider/anthropic"
	"github.com/ratrektlabs/rl-agent/provider/gemini"
	"github.com/ratrektlabs/rl-agent/provider/openai"
	"github.com/ratrektlabs/rl-agent/provider/zai"
)

type Session struct {
	ID        string
	Agent     *agent.Agent
	Provider  string
	Model     string
	APIKey    string
	CreatedAt time.Time
	Messages  []provider.Message
}

type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

func (sm *SessionManager) Get(id string) (*Session, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	s, ok := sm.sessions[id]
	return s, ok
}

func (sm *SessionManager) Create(id, prov, model, apiKey string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	s := &Session{
		ID:        id,
		Provider:  prov,
		Model:     model,
		APIKey:    apiKey,
		CreatedAt: time.Now(),
		Messages:  make([]provider.Message, 0),
	}
	sm.sessions[id] = s
	return s
}

func (sm *SessionManager) Delete(id string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.sessions, id)
}

func createProvider(provName, model, apiKey string) (provider.Provider, error) {
	config := provider.ProviderConfig{
		APIKey: apiKey,
		Model:  model,
	}

	switch provName {
	case "openai":
		return openai.NewProvider(config), nil
	case "anthropic":
		return anthropic.NewProvider(config), nil
	case "gemini":
		return gemini.NewProvider(config), nil
	case "zai":
		return zai.NewProvider(config), nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", provName)
	}
}

func createAgent(prov provider.Provider, model string) *agent.Agent {
	return agent.NewBuilder(prov).
		WithModel(model).
		WithSystemPrompt("You are a helpful assistant. Be concise and friendly.").
		WithMaxTokens(4096).
		WithTemperature(0.7).
		Build()
}

var sessionManager = NewSessionManager()

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/chat", handleChat)
	mux.HandleFunc("/api/stream", handleStream)
	mux.HandleFunc("/api/providers", handleProviders)
	mux.HandleFunc("/api/models", handleModels)
	mux.HandleFunc("/api/session", handleSession)
	mux.HandleFunc("/api/tools", handleTools)
	mux.HandleFunc("/api/health", handleHealth)

	fileServer := http.FileServer(http.Dir("./static"))
	mux.Handle("/", fileServer)

	corsHandler := corsMiddleware(mux)

	addr := "0.0.0.0:" + port
	log.Printf("Starting server on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, corsHandler))
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

type ChatRequest struct {
	SessionID string `json:"session_id"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	APIKey    string `json:"api_key"`
	Message   string `json:"message"`
}

type ChatResponse struct {
	Success bool   `json:"success"`
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, ChatResponse{Success: false, Error: "Invalid request body"}, http.StatusBadRequest)
		return
	}

	session, exists := sessionManager.Get(req.SessionID)
	if !exists {
		prov, err := createProvider(req.Provider, req.Model, req.APIKey)
		if err != nil {
			writeJSON(w, ChatResponse{Success: false, Error: err.Error()}, http.StatusBadRequest)
			return
		}
		session = sessionManager.Create(req.SessionID, req.Provider, req.Model, req.APIKey)
		session.Agent = createAgent(prov, req.Model)
	}

	session.Messages = append(session.Messages, provider.Message{
		Role:    provider.RoleUser,
		Content: req.Message,
	})

	output, err := session.Agent.Run(r.Context(), session.Messages)
	if err != nil {
		writeJSON(w, ChatResponse{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}

	session.Messages = append(session.Messages, output.Message)

	writeJSON(w, ChatResponse{
		Success: true,
		Content: output.Message.Content,
	}, http.StatusOK)
}

func handleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	session, exists := sessionManager.Get(req.SessionID)
	if !exists {
		prov, err := createProvider(req.Provider, req.Model, req.APIKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		session = sessionManager.Create(req.SessionID, req.Provider, req.Model, req.APIKey)
		session.Agent = createAgent(prov, req.Model)
	}

	session.Messages = append(session.Messages, provider.Message{
		Role:    provider.RoleUser,
		Content: req.Message,
	})

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	events, err := session.Agent.RunStream(r.Context(), session.Messages)
	if err != nil {
		writeSSE(w, "error", map[string]string{"error": err.Error()})
		flusher.Flush()
		return
	}

	var fullContent string
	var toolCalls []provider.ToolCall

	for event := range events {
		switch event.Type {
		case agent.StreamEventTypeStepStart:
			writeSSE(w, "step_start", map[string]int{"step": event.Step})

		case agent.StreamEventTypeContentDelta:
			fullContent += event.Delta
			writeSSE(w, "content_delta", map[string]string{"delta": event.Delta})

		case agent.StreamEventTypeToolCall:
			if event.ToolCall != nil {
				toolCalls = append(toolCalls, *event.ToolCall)
				writeSSE(w, "tool_call", map[string]interface{}{
					"name":      event.ToolCall.Function.Name,
					"arguments": string(event.ToolCall.Function.Arguments),
				})
			}

		case agent.StreamEventTypeToolResult:
			if event.ToolResult != nil {
				writeSSE(w, "tool_result", map[string]interface{}{
					"tool_name": event.ToolResult.ToolName,
					"success":   event.ToolResult.Success,
					"result":    event.ToolResult.Result,
					"error":     event.ToolResult.Error,
				})
			}

		case agent.StreamEventTypeStepEnd:
			writeSSE(w, "step_end", map[string]int{"step": event.Step})

		case agent.StreamEventTypeError:
			errMsg := ""
			if event.Error != nil {
				errMsg = event.Error.Error()
			}
			writeSSE(w, "error", map[string]string{"error": errMsg})

		case agent.StreamEventTypeFinished:
			writeSSE(w, "finished", map[string]bool{"done": true})
		}
		flusher.Flush()
	}

	session.Messages = append(session.Messages, provider.Message{
		Role:      provider.RoleAssistant,
		Content:   fullContent,
		ToolCalls: toolCalls,
	})
}

type ProviderInfo struct {
	Name   string   `json:"name"`
	Models []string `json:"models"`
}

func handleProviders(w http.ResponseWriter, r *http.Request) {
	providers := []ProviderInfo{
		{Name: "openai", Models: []string{"gpt-4o", "gpt-4o-mini", "gpt-4-turbo", "gpt-3.5-turbo"}},
		{Name: "anthropic", Models: []string{"claude-sonnet-4-20250514", "claude-3-5-sonnet-20241022", "claude-3-opus-20240229", "claude-3-haiku-20240307"}},
		{Name: "gemini", Models: []string{"gemini-1.5-flash", "gemini-1.5-pro", "gemini-pro"}},
		{Name: "zai", Models: []string{"zai-1"}},
	}
	writeJSON(w, providers, http.StatusOK)
}

func handleModels(w http.ResponseWriter, r *http.Request) {
	prov := r.URL.Query().Get("provider")
	models := []string{}

	switch prov {
	case "openai":
		models = []string{"gpt-4o", "gpt-4o-mini", "gpt-4-turbo", "gpt-3.5-turbo"}
	case "anthropic":
		models = []string{"claude-sonnet-4-20250514", "claude-3-5-sonnet-20241022", "claude-3-opus-20240229", "claude-3-haiku-20240307"}
	case "gemini":
		models = []string{"gemini-1.5-flash", "gemini-1.5-pro", "gemini-pro"}
	case "zai":
		models = []string{"zai-1"}
	default:
		models = []string{}
	}

	writeJSON(w, map[string][]string{"models": models}, http.StatusOK)
}

type SessionRequest struct {
	SessionID string `json:"session_id"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	APIKey    string `json:"api_key"`
}

func handleSession(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req SessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, map[string]string{"error": "Invalid request"}, http.StatusBadRequest)
			return
		}

		prov, err := createProvider(req.Provider, req.Model, req.APIKey)
		if err != nil {
			writeJSON(w, map[string]string{"error": err.Error()}, http.StatusBadRequest)
			return
		}

		session := sessionManager.Create(req.SessionID, req.Provider, req.Model, req.APIKey)
		session.Agent = createAgent(prov, req.Model)

		writeJSON(w, map[string]string{"status": "created", "session_id": req.SessionID}, http.StatusOK)

	case http.MethodDelete:
		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, map[string]string{"error": "Invalid request"}, http.StatusBadRequest)
			return
		}
		sessionManager.Delete(req.SessionID)
		writeJSON(w, map[string]string{"status": "deleted"}, http.StatusOK)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tools := []map[string]interface{}{
		{
			"name":        "get_weather",
			"description": "Get current weather for a location",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"location": map[string]interface{}{
						"type":        "string",
						"description": "City name",
					},
				},
				"required": []string{"location"},
			},
		},
		{
			"name":        "calculate",
			"description": "Perform a mathematical calculation",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"expression": map[string]interface{}{
						"type":        "string",
						"description": "Math expression to evaluate",
					},
				},
				"required": []string{"expression"},
			},
		},
		{
			"name":        "search_web",
			"description": "Search the web for information",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query",
					},
				},
				"required": []string{"query"},
			},
		},
	}

	writeJSON(w, map[string]interface{}{"tools": tools}, http.StatusOK)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
		"version":   "1.0.0",
	}, http.StatusOK)
}

func writeJSON(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeSSE(w http.ResponseWriter, eventType string, data interface{}) {
	dataBytes, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\n", eventType)
	fmt.Fprintf(w, "data: %s\n\n", dataBytes)
}
