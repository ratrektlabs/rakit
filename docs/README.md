# rl-agent Documentation

## Overview

rl-agent is a Go-based agent framework designed for building AI agents with pluggable protocols, providers, and storage backends.

## Documentation

| Document | Description |
|----------|-------------|
| [ARCHITECTURE.md](./ARCHITECTURE.md) | Full system architecture and design |

## Key Features

- **Protocol Layer**: Output in AG-UI (CopilotKit) or AI SDK (Vercel) format
- **Provider Layer**: Use OpenAI or Gemini as your LLM backend
- **Skill System**: 3-layer lazy-loaded skills (registration → prompt → resources) persisted in metadata store
- **Metadata Storage**: Persist sessions, tools, skills, and memory (Firestore or MongoDB)
- **Virtual Workspace**: Blob-backed filesystem for agent files and skill resources (S3 or Firebase Storage)

## Quick Start

```go
package main

import (
    "github.com/ratrektlabs/rl-agent/agent"
    "github.com/ratrektlabs/rl-agent/provider/gemini"
    "github.com/ratrektlabs/rl-agent/protocol/aisdk"
    "github.com/ratrektlabs/rl-agent/skill"
    "github.com/ratrektlabs/rl-agent/storage/metadata/firestore"
    "github.com/ratrektlabs/rl-agent/storage/blob/s3"
)

func main() {
    store, _ := firestore.New("my-project")
    fs, _ := s3.New("my-agent-workspace", "agents/")

    a := agent.New(
        agent.WithProvider(gemini.New("gemini-3.1-pro-preview", apiKey)),
        agent.WithProtocol(aisdk.New()),
        agent.WithStore(store),
        agent.WithFS(fs),
    )

    // Register a skill (stored in metadata + blob)
    a.Skills.Register(ctx, &skill.Definition{
        Name:        "weather",
        Description: "Get weather information for any location",
        // ...
    })

    events, _ := a.Run(ctx, "Hello!")
    for e := range events {
        // Handle events
    }
}
```
