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
	"github.com/ratrektlabs/rakit/provider/gemini"
	"github.com/ratrektlabs/rakit/skill"
	blobLocal "github.com/ratrektlabs/rakit/storage/blob/local"
	metaSQLite "github.com/ratrektlabs/rakit/storage/metadata/sqlite"
)

//go:embed index.html
var frontendFS embed.FS

func main() {
	ctx := context.Background()

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY or OPENAI_API_KEY is required")
	}

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

	// Provider
	prov, err := gemini.New("gemini-3.1-pro-preview", apiKey)
	if err != nil {
		log.Fatalf("Failed to create Gemini provider: %v", err)
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
	reg.SetDefault(aisdk.New())

	// HTTP handler
	mux := http.NewServeMux()

	mux.HandleFunc("POST /chat", func(w http.ResponseWriter, r *http.Request) {
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
