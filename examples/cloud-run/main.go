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
	"github.com/ratrektlabs/rakit/provider/openai"
	"github.com/ratrektlabs/rakit/skill"
	blobS3 "github.com/ratrektlabs/rakit/storage/blob/s3"
	metaMongo "github.com/ratrektlabs/rakit/storage/metadata/mongo"
)

// envOr returns the value of the named env var, or def if unset/empty.
func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func main() {
	ctx := context.Background()

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY is required")
	}
	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		log.Fatal("MONGODB_URI is required")
	}

	// Storage
	store, err := metaMongo.NewStore(ctx, mongoURI, "rakit")
	if err != nil {
		log.Fatalf("Failed to create MongoDB store: %v", err)
	}
	defer store.Close(ctx)

	fs, err := blobS3.New(ctx, "my-agent-workspace", blobS3.WithPrefix("agents"))
	if err != nil {
		log.Fatalf("Failed to create S3 store: %v", err)
	}

	// Agent
	a := agent.New(
		agent.WithProvider(openai.New(envOr("OPENAI_MODEL", "gpt-4o-mini"), apiKey)),
		agent.WithProtocol(aisdk.New()),
		agent.WithStore(store),
		agent.WithFS(fs),
	)

	// Register skills
	_ = a.Skills.Register(ctx, &skill.Definition{
		Name:         "calculator",
		Description:  "Perform arithmetic calculations",
		Instructions: "Use the calculate tool for math operations.",
		Tools: []skill.ToolDef{{
			Name:        "calculate",
			Description: "Evaluate a math expression",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"expression": map[string]any{
						"type":        "string",
						"description": "Math expression to evaluate",
					},
				},
				"required": []string{"expression"},
			},
			Handler:  "http",
			Endpoint: "https://api.mathjs.org/v4/",
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

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("Agent server listening on :%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
