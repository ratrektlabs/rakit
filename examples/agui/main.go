package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/ratrektlabs/rl-agent/agent"
	"github.com/ratrektlabs/rl-agent/protocol/agui"
	"github.com/ratrektlabs/rl-agent/provider"
	"github.com/ratrektlabs/rl-agent/provider/openai"
	"github.com/ratrektlabs/rl-agent/session"
	"github.com/ratrektlabs/rl-agent/tool"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	registry := tool.NewRegistry()
	registry.MustRegister(tool.New("get_time").
		Desc("Get the current time").
		Action(func(ctx context.Context, params map[string]any) (any, error) {
			return map[string]any{
				"time":     "12:00 PM",
				"date":     "2024-01-15",
				"timezone": "UTC",
			}, nil
		}).MustBuild())

	p := openai.NewProvider(provider.ProviderConfig{
		APIKey: apiKey,
		Model:  "gpt-4o",
	})

	a := agent.NewBuilder(p).
		WithModel("gpt-4o").
		WithSystemPrompt("You are a helpful assistant with access to tools.").
		WithToolRegistry(registry).
		Build()

	sessionMgr := session.NewManager(
		session.WithDefaultTTL(30*60*1000000000),
		session.WithCleanupInterval(60*60*1000000000),
	)

	mux := http.NewServeMux()

	mux.HandleFunc("/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			UserID    string            `json:"user_id"`
			SessionID string            `json:"session_id"`
			ThreadID  string            `json:"thread_id"`
			Message   string            `json:"message"`
			Options   map[string]string `json:"options,omitempty"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		ctx := r.Context()

		sess, err := sessionMgr.Get(ctx, req.SessionID)
		if err != nil {
			sess, _ = sessionMgr.CreateWithID(ctx, req.UserID, req.SessionID)
		}

		handler := agui.NewBuilder(a).
			WithUserID(req.UserID).
			WithSessionID(sess.ID).
			WithThreadID(req.ThreadID).
			WithModel("gpt-4o").
			Build()

		output, err := handler.Run(ctx, []provider.Message{
			{Role: provider.RoleUser, Content: req.Message},
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"run_id":    handler.RunID(),
			"thread_id": handler.ThreadID(),
			"message":   output.Message.Content,
			"steps":     output.Steps,
			"state":     output.State,
		})
	})

	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			UserID    string `json:"user_id"`
			SessionID string `json:"session_id"`
			ThreadID  string `json:"thread_id"`
			Message   string `json:"message"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		ctx := r.Context()

		sess, err := sessionMgr.Get(ctx, req.SessionID)
		if err != nil {
			sess, _ = sessionMgr.CreateWithID(ctx, req.UserID, req.SessionID)
		}

		handler := agui.NewBuilder(a).
			WithUserID(req.UserID).
			WithSessionID(sess.ID).
			WithThreadID(req.ThreadID).
			WithModel("gpt-4o").
			Build()

		events, err := handler.RunStream(ctx, []provider.Message{
			{Role: provider.RoleUser, Content: req.Message},
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		for event := range events {
			if err := agui.WriteSSE(w, event); err != nil {
				return
			}
			flusher.Flush()
		}
	})

	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var req struct {
				UserID string `json:"user_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			sess, _ := sessionMgr.Create(r.Context(), req.UserID)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(sess)

		case http.MethodGet:
			sessionID := r.URL.Query().Get("id")
			if sessionID == "" {
				http.Error(w, "session id required", http.StatusBadRequest)
				return
			}
			sess, err := sessionMgr.Get(r.Context(), sessionID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(sess)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("AGUI server starting on :%s\n", port)
	fmt.Println("Endpoints:")
	fmt.Println("  POST /run      - Run agent (non-streaming)")
	fmt.Println("  POST /stream   - Run agent with AGUI SSE events")
	fmt.Println("  POST /session  - Create session")
	fmt.Println("  GET  /session?id=<id> - Get session")

	log.Fatal(http.ListenAndServe(":"+port, mux))
}
