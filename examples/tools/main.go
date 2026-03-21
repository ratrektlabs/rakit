package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/ratrektlabs/rl-agent/agent"
	"github.com/ratrektlabs/rl-agent/provider"
	"github.com/ratrektlabs/rl-agent/provider/openai"
	"github.com/ratrektlabs/rl-agent/tool"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	registry := tool.NewRegistry()

	registry.MustRegister(tool.New("get_weather").
		Desc("Get the current weather for a location").
		Param("location", "string", "City and country, e.g., 'Paris, France'", true).
		Param("unit", "string", "Temperature unit: 'celsius' or 'fahrenheit'", false).
		Action(func(ctx context.Context, params map[string]any) (any, error) {
			location, _ := params["location"].(string)
			unit := "celsius"
			if u, ok := params["unit"].(string); ok && u != "" {
				unit = u
			}
			return map[string]any{
				"location":    location,
				"temperature": 22,
				"unit":        unit,
				"condition":   "sunny",
				"humidity":    45,
			}, nil
		}).MustBuild())

	registry.MustRegister(tool.New("calculate").
		Desc("Perform a mathematical calculation").
		Param("expression", "string", "Math expression to evaluate, e.g., '2+2*3'", true).
		Action(func(ctx context.Context, params map[string]any) (any, error) {
			expr, _ := params["expression"].(string)
			return map[string]any{
				"expression": expr,
				"result":     "42",
				"note":       "This is a mock calculator",
			}, nil
		}).MustBuild())

	p := openai.NewProvider(provider.ProviderConfig{
		APIKey: apiKey,
		Model:  "gpt-4o",
	})

	a := agent.NewBuilder(p).
		WithModel("gpt-4o").
		WithSystemPrompt("You are a helpful assistant. Use tools when appropriate.").
		WithToolRegistry(registry).
		Build()

	ctx := context.Background()

	fmt.Println("Example 1: Using weather tool")
	fmt.Println("---")

	output, err := a.Run(ctx, []provider.Message{
		{Role: provider.RoleUser, Content: "What's the weather like in Tokyo?"},
	})
	if err != nil {
		log.Fatalf("Run failed: %v", err)
	}

	fmt.Printf("Response: %s\n", output.Message.Content)
	fmt.Printf("Tool calls made: %d\n", len(output.ToolResults))

	for _, result := range output.ToolResults {
		fmt.Printf("  - %s: success=%v\n", result.ToolName, result.Success)
	}

	fmt.Println("\nExample 2: Using calculate tool")
	fmt.Println("---")

	output, err = a.Run(ctx, []provider.Message{
		{Role: provider.RoleUser, Content: "Calculate 123 * 456"},
	})
	if err != nil {
		log.Fatalf("Run failed: %v", err)
	}

	fmt.Printf("Response: %s\n", output.Message.Content)
	fmt.Printf("Steps: %d\n", output.Steps)

	fmt.Println("\nRegistered tools:")
	for _, info := range registry.List() {
		fmt.Printf("  - %s: %s\n", info.Name, info.Description)
	}
}
