# 🛶 rakit

> *Rakit* means bamboo raft in Indonesian — a simple, sturdy vessel that carries you across the river. Built from natural materials, flexible yet reliable. That's what this framework aims to be for AI agents.

**R**emote **A**gent **K**it — a Go framework for building AI agents that stream to any frontend, with persistence out of the box.

## Features

- Multi-provider LLM support (OpenAI, Gemini)
- Dual protocol streaming (AG-UI / CopilotKit, Vercel AI SDK)
- Session persistence with automatic compaction
- 3-layer skill system (registration -> prompt -> resources)
- Pluggable storage (SQLite, Firestore, MongoDB + S3, Firebase, local FS)
- Content negotiation — one agent, any frontend

## Install

```bash
go get github.com/ratrektlabs/rakit
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "github.com/ratrektlabs/rakit/agent"
    "github.com/ratrektlabs/rakit/protocol/aisdk"
    "github.com/ratrektlabs/rakit/provider/openai"
    metaSQLite "github.com/ratrektlabs/rakit/storage/metadata/sqlite"
    blobLocal "github.com/ratrektlabs/rakit/storage/blob/local"
)

func main() {
    ctx := context.Background()

    store, _ := metaSQLite.NewStore(ctx, "./data/agent.db")
    defer store.Close()

    fs, _ := blobLocal.New("./data/workspace")

    a := agent.New(
        agent.WithProvider(openai.New("gpt-5.4", "sk-...")),
        agent.WithProtocol(aisdk.New()),
        agent.WithStore(store),
        agent.WithFS(fs),
    )

    // Session-aware run with persistence and compaction
    sess, _ := a.CreateSession(ctx)
    events, _ := a.RunWithSession(ctx, sess.ID, "Hello!", aisdk.New())
    for e := range events {
        fmt.Println(e)
    }
}
```

## Core Concepts

### Sessions

Sessions persist multi-turn conversations across requests. Each session stores the full message history (user, assistant, tool calls) in the metadata store.

```go
// Create a new session
sess, _ := a.CreateSession(ctx)

// First message
a.RunWithSession(ctx, sess.ID, "My name is Alice", aisdk.New())

// Next request carries full context — agent remembers
a.RunWithSession(ctx, sess.ID, "What's my name?", aisdk.New())
```

Sessions survive server restarts. Load an existing session by passing its ID.

### Memory

A key-value store backed by the metadata adapter. Use it for agent facts, user preferences, or any persistent state.

```go
// Store a value
a.Store.Set(ctx, "user:alice:timezone", []byte("UTC-5"))

// Retrieve it later
value, _ := a.Store.Get(ctx, "user:alice:timezone")

// List keys by prefix
keys, _ := a.Store.List(ctx, "user:alice:")
```

### Tools

Tools are capabilities the LLM can call during a run. Define them with a JSON Schema for parameters and an execution handler.

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() any
    Execute(ctx context.Context, input map[string]any) (any, error)
}
```

Register tools on the agent:

```go
a := agent.New(
    agent.WithTools(myTool1, myTool2),
)
```

Tool definitions are also persisted in the metadata store so they survive restarts.

### Skills

Skills are declarative agent capabilities organized in 3 layers for lazy loading:

| Layer | What | Stored Where | Loaded When |
|-------|------|-------------|-------------|
| L1 — Registration | Name + description | Metadata store | Skill listing |
| L2 — Prompt | Instructions, tools, config | Metadata store | Skill activation |
| L3 — Resources | Scripts, templates, files | Blob store | On-demand execution |

```go
a.Skills.Register(ctx, &skill.Definition{
    Name:         "weather",
    Description:  "Get weather for any location",
    Instructions: "Use the get_weather tool to fetch current conditions.",
    Tools: []skill.ToolDef{{
        Name:        "get_weather",
        Description: "Get weather for a location",
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "location": map[string]any{"type": "string"},
            },
            "required": []string{"location"},
        },
        Handler:  "http",
        Endpoint: "https://api.weatherapi.com/v1/current.json",
    }},
})
```

- **L1** keeps overhead minimal — only name and description are loaded for listing
- **L2** loads the full definition (instructions + tool schemas) when a skill is activated
- **L3** loads scripts or templates from the blob store only during execution

### Compaction

When a session's message history grows too long, rakit automatically summarizes older messages using the LLM. This keeps context windows manageable without losing important context.

```go
a := agent.New(
    agent.WithCompaction(agent.CompactionConfig{
        MaxMessages: 20,  // trigger compaction above 20 messages
        KeepRecent:  6,   // always keep last 6 messages intact
        SummaryRole: "system",
    }),
    // ...
)
```

How it works:
1. When `RunWithSession` detects history exceeds `MaxMessages`, it splits the messages
2. Older messages are sent to the LLM for summarization (non-streaming)
3. The summary replaces the old messages, recent messages are preserved verbatim
4. If summarization fails, the run continues with full history — never blocks

### Protocols

rakit supports two streaming protocols with automatic content negotiation:

**AG-UI (CopilotKit)** — rich event-based protocol with run lifecycle, state sync, tool streaming, and reasoning events.

**AI SDK (Vercel)** — lightweight SSE format for simple chat streaming.

```go
reg := protocol.NewRegistry()
reg.Register(agui.New())
reg.Register(aisdk.New())
reg.SetDefault(aisdk.New())

// Client sends Accept header, rakit picks the right protocol
p := reg.Negotiate(r.Header.Get("Accept"))
```

### Providers

```go
// OpenAI
p := openai.New("gpt-5.4", apiKey)    // or gpt-5.4-mini, gpt-5.4-nano

// Gemini
p, _ := gemini.New("gemini-3.1-pro-preview", apiKey)
```

Both implement the same `provider.Provider` interface — swap freely. Model is selected at construction time.

## Storage Adapters

**Metadata** (sessions, tools, skills, memory):

| Adapter | Import | Use case |
|---------|--------|----------|
| SQLite | `storage/metadata/sqlite` | Local development |
| Firestore | `storage/metadata/firestore` | GCP production |
| MongoDB | `storage/metadata/mongo` | Multi-cloud production |

**Blob** (agent workspace — files, scripts, artifacts):

| Adapter | Import | Use case |
|---------|--------|----------|
| Local FS | `storage/blob/local` | Local development |
| S3 | `storage/blob/s3` | AWS, MinIO, Cloudflare R2 |
| Firebase | `storage/blob/firebase` | GCP production |

## Documentation

| Doc | Description |
|-----|-------------|
| [Architecture](docs/ARCHITECTURE.md) | Layer design, data flow, package structure |

## Examples

| Example | Description | Storage |
|---------|-------------|---------|
| [examples/local](examples/local) | Local dev server | SQLite + Local FS |
| [examples/cloud-run](examples/cloud-run) | Cloud Run deployment | MongoDB + S3 |

## License

[MIT](./LICENSE) — RatrektLabs
