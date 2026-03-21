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
		WithSystemPrompt("You are a helpful assistant. Be concise.").
		Build()

	ctx := context.Background()

	fmt.Println("Starting streaming response...")
	fmt.Println("---")

	events, err := a.RunStream(ctx, []provider.Message{
		{Role: provider.RoleUser, Content: "Tell me a short story about a robot learning to paint."},
	})
	if err != nil {
		log.Fatalf("RunStream failed: %v", err)
	}

	for event := range events {
		switch event.Type {
		case agent.StreamEventTypeStepStart:
			fmt.Printf("\n[Step %d started]\n", event.Step)
		case agent.StreamEventTypeContentDelta:
			fmt.Print(event.Delta)
		case agent.StreamEventTypeToolCall:
			if event.ToolCall != nil {
				fmt.Printf("\n[Tool call: %s]\n", event.ToolCall.Function.Name)
			}
		case agent.StreamEventTypeToolResult:
			if event.ToolResult != nil {
				fmt.Printf("\n[Tool result: %v]\n", event.ToolResult.Result)
			}
		case agent.StreamEventTypeStepEnd:
			fmt.Printf("\n[Step %d ended]\n", event.Step)
		case agent.StreamEventTypeError:
			fmt.Printf("\n[Error: %v]\n", event.Error)
		case agent.StreamEventTypeFinished:
			fmt.Println("\n---")
			fmt.Println("Stream completed!")
		}
	}
}
