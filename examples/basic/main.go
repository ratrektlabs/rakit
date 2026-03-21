package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/ratrektlabs/rl-agent/agent"
	"github.com/ratrektlabs/rl-agent/provider"
	"github.com/ratrektlabs/rl-agent/provider/openai"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	p := openai.NewProvider(provider.ProviderConfig{
		APIKey: apiKey,
		Model:  "gpt-4o",
	})

	a := agent.NewBuilder(p).
		WithModel("gpt-4o").
		WithSystemPrompt("You are a helpful assistant.").
		WithMaxTokens(1024).
		Build()

	ctx := context.Background()

	output, err := a.Run(ctx, []provider.Message{
		{Role: provider.RoleUser, Content: "Hello! What's 2+2?"},
	})
	if err != nil {
		log.Fatalf("Run failed: %v", err)
	}

	fmt.Printf("Response: %s\n", output.Message.Content)
	fmt.Printf("Steps: %d\n", output.Steps)
	fmt.Printf("State: %s\n", output.State)
}
