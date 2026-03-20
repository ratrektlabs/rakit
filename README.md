# rl-agent

A lightweight, extensible agentic framework for Go. Build AI-powered applications with multi-provider support, memory, tools, and real-time streaming.

## Features

- **🪶 Lightweight** - Zero external dependencies, 100% Go standard library
- **🔌 Multi-Provider** - OpenAI, Anthropic (Claude), Google Gemini, ZAI
- **🔄 Streaming** - Real-time responses via Go channels
- **🛠️ Tools** - Function calling with JSON schema validation
- **🧠 Memory** - Pluggable memory backends (InMemory, SQLite, MongoDB)
- **🧩 Skills** - Modular capability packages
- **📡 Protocols** - AGUI protocol support for frontend integration
- **🔐 Multi-Tenant** - Built for remote, multi-user applications

## Installation

```bash
go get github.com/ratrektlabs/rl-agent
```

## Quick Start

### Basic Agent

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/ratrektlabs/rl-agent/agent"
    "github.com/ratrektlabs/rl-agent/provider/openai"
)

func main() {
    // Create provider
    prov := openai.NewProvider(provider.ProviderConfig{
        APIKey: os.Getenv("OPENAI_API_KEY"),
        Model:  "gpt-4o",
    })

    // Create agent
    ag := agent.NewAgent(prov,
        agent.WithSystemPrompt("You are a helpful assistant."),
    )

    // Run agent
    ctx := context.Background()
    resp, err := ag.Run(ctx, "Hello, what can you do?")
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(resp.Content)
}
```

### With Streaming

```go
// Stream responses
eventChan, err := ag.RunStream(ctx, "Tell me a story")
if err != nil {
    log.Fatal(err)
}

for event := range eventChan {
    switch event.Type {
    case provider.StreamEventContentDelta:
        fmt.Print(event.Delta)
    case provider.StreamEventDone:
        fmt.Println("\n[Done]")
    case provider.StreamEventError:
        log.Printf("Error: %v", event.Error)
    }
}
```

### With Tools

```go
// Define a tool
weatherTool := tool.NewTool(
    "get_weather",
    "Get current weather for a location",
    tool.Schema{
        Type: "object",
        Properties: map[string]tool.Schema{
            "location": {Type: "string", Description: "City name"},
        },
        Required: []string{"location"},
    },
    func(ctx context.Context, args map[string]any) (any, error) {
        location := args["location"].(string)
        return fmt.Sprintf("Weather in %s: Sunny, 25°C", location), nil
    },
)

// Register tool
registry := tool.NewRegistry()
registry.Register(weatherTool)

// Create agent with tool
ag := agent.NewAgent(prov,
    agent.WithTools(registry),
)
```

### With Memory

```go
// In-memory backend (for development)
memBackend := memory.NewInMemoryBackend()

// Create agent with memory
ag := agent.NewAgent(prov,
    agent.WithMemory(memBackend),
    agent.WithSession("user-123", "session-456"),
)
```

## Providers

### OpenAI

```go
prov := openai.NewProvider(provider.ProviderConfig{
    APIKey: os.Getenv("OPENAI_API_KEY"),
    Model:  "gpt-4o",  // or gpt-4, gpt-3.5-turbo
})
```

### Anthropic (Claude)

```go
prov := anthropic.NewProvider(provider.ProviderConfig{
    APIKey: os.Getenv("ANTHROPIC_API_KEY"),
    Model:  "claude-sonnet-4-20250514",
})
```

### Google Gemini

```go
prov := gemini.NewProvider(provider.ProviderConfig{
    APIKey: os.Getenv("GEMINI_API_KEY"),
    Model:  "gemini-1.5-flash",
})
```

### ZAI

```go
prov := zai.NewProvider(provider.ProviderConfig{
    APIKey: os.Getenv("ZAI_API_KEY"),
    Model:  "zai-1",
})
```

## Architecture

```
rl-agent/
├── agent/           # Core agent implementation
│   └── agent.go     # Agent struct, Run/RunStream, options
├── provider/        # LLM providers
│   ├── provider.go  # Provider interface
│   ├── openai/      # OpenAI API
│   ├── anthropic/   # Anthropic Claude API
│   ├── gemini/      # Google Gemini API
│   └── zai/         # ZAI API
├── tool/            # Tool system
│   └── tool.go      # Tool interface, Registry
├── memory/          # Memory system
│   └── memory.go    # Memory interface, backends
└── protocol/        # Communication protocols
    └── agui/        # AGUI protocol (coming soon)
```

## Agent Options

```go
ag := agent.NewAgent(prov,
    agent.WithSystemPrompt("You are..."),
    agent.WithModel("gpt-4o"),
    agent.WithTemperature(0.7),
    agent.WithMaxTokens(4096),
    agent.WithTools(registry),
    agent.WithMemory(memBackend),
    agent.WithSession(userID, sessionID),
    agent.WithMaxSteps(10),
    agent.WithHook(agent.Hooks{
        BeforeStep: func(ctx context.Context, step int) error {
            fmt.Printf("Starting step %d\n", step)
            return nil
        },
        AfterStep: func(ctx context.Context, step int, resp *provider.CompletionResponse) error {
            fmt.Printf("Completed step %d\n", step)
            return nil
        },
        OnToolCall: func(ctx context.Context, tc *provider.ToolCall) (any, error) {
            fmt.Printf("Tool called: %s\n", tc.Function.Name)
            return nil, nil
        },
    }),
)
```

## Why rl-agent?

| Feature | rl-agent | LangChain | Claude Code |
|---------|----------|-----------|-------------|
| Language | Go | Python | Node.js |
| Dependencies | Zero | Heavy | Heavy |
| Use Case | Apps | Apps/Scripts | Local Assistant |
| Deployment | Remote/Local | Remote/Local | Local Only |
| Multi-Tenant | ✅ | ✅ | ❌ |
| Streaming | ✅ | ✅ | ✅ |

## Roadmap

- [x] Provider interface
- [x] OpenAI, Anthropic, Gemini, ZAI providers
- [x] Tool system with registry
- [x] Memory interface
- [ ] SQLite memory backend
- [ ] MongoDB memory backend
- [ ] Skills system
- [ ] AGUI protocol support
- [ ] SSE protocol support
- [ ] Session management
- [ ] Examples & documentation

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - see [LICENSE](LICENSE) for details.

## Credits

Built with ❤️ by [RatrektLabs](https://github.com/ratrektlabs)
