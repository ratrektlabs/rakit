package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ratrektlabs/rakit/agent"
	"github.com/ratrektlabs/rakit/protocol"
	"github.com/ratrektlabs/rakit/protocol/aisdk"
	"github.com/ratrektlabs/rakit/protocol/agui"
	"github.com/ratrektlabs/rakit/provider"
	"github.com/ratrektlabs/rakit/provider/gemini"
	"github.com/ratrektlabs/rakit/provider/openai"
	"github.com/ratrektlabs/rakit/skill"
	blobLocal "github.com/ratrektlabs/rakit/storage/blob/local"
	"github.com/ratrektlabs/rakit/storage/metadata"
	metaSQLite "github.com/ratrektlabs/rakit/storage/metadata/sqlite"
)

//go:embed index.html
var frontendFS embed.FS

// providerConfig is the persisted provider configuration stored in the metadata store.
type providerConfig struct {
	Provider string `json:"provider"`
	APIKey   string `json:"apiKey"`
	Model    string `json:"model"`
}

// loadProviderConfig tries to create a provider from env vars, falling back to
// config persisted in the metadata store. Returns nil if no config is available.
func loadProviderConfig(ctx context.Context, store metadata.Store) (provider.Provider, error) {
	// Try environment variables first
	geminiKey := os.Getenv("GEMINI_API_KEY")
	if geminiKey != "" {
		return gemini.New("gemini-3.1-pro-preview", geminiKey)
	}
	openaiKey := os.Getenv("OPENAI_API_KEY")
	if openaiKey != "" {
		return openai.New("gpt-5.4", openaiKey), nil
	}

	// Fall back to persisted config
	data, err := store.Get(ctx, "__config:provider")
	if err != nil || data == nil {
		return nil, nil // No saved config — that's okay
	}

	var cfg providerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid saved provider config: %w", err)
	}
	if cfg.APIKey == "" {
		return nil, nil
	}

	switch cfg.Provider {
	case "gemini":
		return gemini.New(cfg.Model, cfg.APIKey)
	case "openai":
		return openai.New(cfg.Model, cfg.APIKey), nil
	default:
		return nil, fmt.Errorf("unknown saved provider: %s", cfg.Provider)
	}
}

func main() {
	ctx := context.Background()

	// Storage (local — no external services required)
	store, err := metaSQLite.NewStore(ctx, "./data/agent.db")
	if err != nil {
		log.Fatalf("Failed to create SQLite store: %v", err)
	}
	defer store.Close()

	blobStore, err := blobLocal.New("./data/workspace")
	if err != nil {
		log.Fatalf("Failed to create local blob store: %v", err)
	}

	// Provider — try env vars first, then fall back to saved config in store
	var prov provider.Provider
	if p, err := loadProviderConfig(ctx, store); err != nil {
		log.Printf("Warning: could not load saved provider config: %v", err)
	} else if p != nil {
		prov = p
	}

	// Agent
	a := agent.New(
		agent.WithProvider(prov),
		agent.WithProtocol(aisdk.New()),
		agent.WithStore(store),
		agent.WithFS(blobStore),
	)

	// Register a skill
	_ = a.Skills.Register(ctx, &skill.Definition{
		Name:         "echo",
		Description:  "Echoes back the input",
		Instructions: "Use the echo tool to repeat what the user says.",
		Tools: []skill.ToolDef{{
			Name:        "echo",
			Description: "Echo back the input text",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{"type": "string"},
				},
				"required": []string{"text"},
			},
			Handler:  "http",
			Endpoint: "https://httpbin.org/post",
		}},
	})

	// Protocol registry
	reg := protocol.NewRegistry()
	reg.Register(aisdk.New())
	reg.Register(agui.New())
	reg.SetDefault(aisdk.New())

	// HTTP handler
	mux := http.NewServeMux()

	mux.HandleFunc("POST /chat", func(w http.ResponseWriter, r *http.Request) {
		if a.Provider == nil {
			http.Error(w, "no provider configured — set one via Provider settings", http.StatusServiceUnavailable)
			return
		}

		p := reg.Negotiate(r.Header.Get("Accept"))
		if p == nil {
			p = reg.Default()
		}

		w.Header().Set("Content-Type", p.ContentType())

		var req struct {
			Message   string `json:"message"`
			SessionID string `json:"sessionId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Create session if not provided
		if req.SessionID == "" {
			sess, err := a.CreateSession(r.Context())
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			req.SessionID = sess.ID
		}

		events, err := a.RunWithSession(r.Context(), req.SessionID, req.Message, p)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := p.EncodeStream(r.Context(), w, events); err != nil {
			if r.Context().Err() == nil {
				log.Printf("Stream error: %v", err)
			}
		}
	})

	// Admin API
	registerAdminHandlers(mux, a)

	// Frontend — serve embedded index.html at /
	dist, _ := fs.Sub(frontendFS, ".")
	mux.Handle("GET /", http.FileServer(http.FS(dist)))

	addr := ":8080"
	fmt.Printf("Agent server listening on %s\n", addr)
	fmt.Println("Data stored in ./data/")
	fmt.Println("Admin API at /api/v1/")
	if prov == nil {
		fmt.Println("No provider configured — set one via the dashboard Provider settings")
	}
	fmt.Printf("Dashboard at http://localhost%s\n", addr)
	log.Fatal(http.ListenAndServe(addr, corsMiddleware(requestLogger(mux))))
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lw := &loggingWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, lw.status, time.Since(start))
	})
}

type loggingWriter struct {
	http.ResponseWriter
	status int
}

func (lw *loggingWriter) WriteHeader(code int) {
	lw.status = code
	lw.ResponseWriter.WriteHeader(code)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type, Accept")
		h.Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
