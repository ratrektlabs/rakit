package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/ratrektlabs/rl-agent/agent"
	"github.com/ratrektlabs/rl-agent/protocol"
	"github.com/ratrektlabs/rl-agent/protocol/aisdk"
	"github.com/ratrektlabs/rl-agent/provider/gemini"
	"github.com/ratrektlabs/rl-agent/skill"
	blobS3 "github.com/ratrektlabs/rl-agent/storage/blob/s3"
	metaFirestore "github.com/ratrektlabs/rl-agent/storage/metadata/firestore"
)

func main() {
	ctx := context.Background()

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY is required")
	}
	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID == "" {
		log.Fatal("GOOGLE_CLOUD_PROJECT is required")
	}

	// Storage
	store, err := metaFirestore.NewStore(ctx, projectID)
	if err != nil {
		log.Fatalf("Failed to create Firestore store: %v", err)
	}
	defer store.Close()

	fs, err := blobS3.New(ctx, "my-agent-workspace", blobS3.WithPrefix("agents"))
	if err != nil {
		log.Fatalf("Failed to create S3 store: %v", err)
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
	http.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		p := reg.Negotiate(r.Header.Get("Accept"))
		if p == nil {
			p = reg.Default()
		}

		w.Header().Set("Content-Type", p.ContentType())

		var req struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		events, err := a.RunWithProtocol(r.Context(), req.Message, p)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := p.EncodeStream(r.Context(), w, events); err != nil {
			log.Printf("Stream error: %v", err)
		}
	})

	addr := ":8080"
	fmt.Printf("Agent server listening on %s\n", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
