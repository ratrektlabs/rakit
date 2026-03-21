package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/ratrektlabs/rl-agent/agent"
	_ "github.com/ratrektlabs/rl-agent/memory"
	"github.com/ratrektlabs/rl-agent/provider"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	fmt.Println("=== In-Memory Example ===")
	runInMemoryExample(apiKey)

	fmt.Println("\n=== SQLite Memory Example ===")
	runSQLiteExample(apiKey)
}

func runInMemoryExample(apiKey string) {
	backend := memory.NewInMemoryBackend()
	mem := backend.Memory()

	p := openai.NewProvider(provider.ProviderConfig{
		APIKey: apiKey,
		Model:  "gpt-4o",
	})

	a := agent.NewBuilder(p).
		WithModel("gpt-4o").
		WithSystemPrompt("You are a helpful assistant.").
		WithMemory(mem).
		Build()

	ctx := context.Background()
	userID := "user-123"
	sessionID := "session-abc"

	_, _ = a.Run(ctx, []provider.Message{
		{Role: provider.RoleUser, Content: "My name is Alice and I love pizza."},
	}, agent.WithUser(userID), agent.WithSession(sessionID))

	entries, _ := mem.Get(ctx, userID, sessionID, 10)
	fmt.Printf("Stored %d memory entries\n", len(entries))

	results, _ := mem.Search(ctx, userID, memory.SearchOptions{
		Query: "pizza",
		Limit: 5,
	})
	fmt.Printf("Found %d results for 'pizza'\n", len(results))
	for _, r := range results {
		fmt.Printf("  - %s: %s\n", r.Role, r.Content)
	}
}

func runSQLiteExample(apiKey string) {
	dbPath := ":memory:"

	backend, err := sqlite.NewBackend(dbPath)
	if err != nil {
		log.Printf("SQLite backend not available: %v", err)
		fmt.Println("Skipping SQLite example (sqlite driver not available)")
		return
	}
	defer backend.Close()

	if err := backend.Connect(context.Background()); err != nil {
		log.Printf("Failed to connect: %v", err)
		return
	}

	mem := backend.Memory()

	p := openai.NewProvider(provider.ProviderConfig{
		APIKey: apiKey,
		Model:  "gpt-4o",
	})

	a := agent.NewBuilder(p).
		WithModel("gpt-4o").
		WithSystemPrompt("You are a helpful assistant.").
		WithMemory(mem).
		Build()

	ctx := context.Background()
	userID := "user-456"
	sessionID := "session-xyz"

	_, _ = a.Run(ctx, []provider.Message{
		{Role: provider.RoleUser, Content: "I work as a software engineer."},
	}, agent.WithUser(userID), agent.WithSession(sessionID))

	entries, _ := mem.Get(ctx, userID, sessionID, 10)
	fmt.Printf("Stored %d entries in SQLite\n", len(entries))

	results, _ := mem.Search(ctx, userID, memory.SearchOptions{
		Query: "engineer",
		Limit: 5,
	})
	fmt.Printf("Found %d results for 'engineer'\n", len(results))
	for _, r := range results {
		fmt.Printf("  - %s: %s (score: %.2f)\n", r.Role, r.Content, r.Score)
	}
}
