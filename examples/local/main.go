package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/ratrektlabs/rakit/agent"
	"github.com/ratrektlabs/rakit/protocol"
	"github.com/ratrektlabs/rakit/protocol/aisdk"
	"github.com/ratrektlabs/rakit/provider/gemini"
	"github.com/ratrektlabs/rakit/skill"
	blobLocal "github.com/ratrektlabs/rakit/storage/blob/local"
	metaSQLite "github.com/ratrektlabs/rakit/storage/metadata/sqlite"
)

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

	fs, err := blobLocal.New("./data/workspace")
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
		agent.WithFS(fs),
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

	mux.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
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
			// Client disconnect is normal, don't log it
			if r.Context().Err() == nil {
				log.Printf("Stream error: %v", err)
			}
		}
	})

	// Admin API
	agent.RegisterHandlers(mux, a)

	addr := ":8080"
	fmt.Printf("Agent server listening on %s\n", addr)
	fmt.Println("Data stored in ./data/")
	fmt.Println("Admin API at /api/v1/")
	log.Fatal(http.ListenAndServe(addr, mux))
}
