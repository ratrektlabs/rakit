# rl-agent

A Go framework for building AI agents that stream to any frontend, with persistence out of the box.

## Why rl-agent?

- **Stream to any frontend** — AG-UI (CopilotKit) and Vercel AI SDK protocols built in, with content negotiation
- **Multi-provider** — OpenAI and Gemini with a unified interface, pick your model at construction time
- **Persistent by default** — Sessions, tools, skills, and memory survive restarts
- **Agent workspace** — Virtual filesystem backed by S3 or local storage for generated artifacts
- **Compaction** — LLM-powered conversation summarization when history grows too long
- **Opinionated but extensible** — Clean interfaces for every layer, easy to add providers, protocols, or storage backends

## Quick Start

```bash
go get github.com/ratrektlabs/rl-agent
```

```go
package main

import (
    "context"
    "github.com/ratrektlabs/rl-agent/agent"
    "github.com/ratrektlabs/rl-agent/protocol/aisdk"
    "github.com/ratrektlabs/rl-agent/provider/openai"
    metaSQLite "github.com/ratrektlabs/rl-agent/storage/metadata/sqlite"
    blobLocal "github.com/ratrektlabs/rl-agent/storage/blob/local"
)

func main() {
    ctx := context.Background()

    // Local storage — zero external dependencies
    store, _ := metaSQLite.NewStore(ctx, "./data/agent.db")
    defer store.Close()

    fs, _ := blobLocal.New("./data/workspace")

    // Create agent
    a := agent.New(
        agent.WithProvider(openai.New("gpt-5.4", "sk-...")),
        agent.WithProtocol(aisdk.New()),
        agent.WithStore(store),
        agent.WithFS(fs),
    )

    // Stateless run (single message, no persistence)
    events, _ := a.Run(ctx, "Hello!")
    for e := range events {
        // handle events...
    }

    // Session-aware run (full history, compaction, persistence)
    sess, _ := a.CreateSession(ctx)
    events, _ = a.RunWithSession(ctx, sess.ID, "Remember my name is Alice", aisdk.New())
    for e := range events {
        // handle events...
    }

    // Next message carries full conversation context
    events, _ = a.RunWithSession(ctx, sess.ID, "What's my name?", aisdk.New())
}
```

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│                      CLIENT                              │
│           (CopilotKit, Vercel AI, Custom)                │
└────────────────────────┬─────────────────────────────────┘
                         │  content negotiation
                         ▼
┌──────────────────────────────────────────────────────────┐
│                   PROTOCOL LAYER                         │
│    AG-UI (CopilotKit)  │  AI SDK (Vercel)               │
└────────────────────────┬─────────────────────────────────┘
                         ▼
┌──────────────────────────────────────────────────────────┐
│                    AGENT RUNTIME                         │
│   Run / RunWithProtocol / RunWithSession + Compaction   │
└────────────────────────┬─────────────────────────────────┘
                         ▼
┌──────────────────────────────────────────────────────────┐
│                   PROVIDER LAYER                         │
│          OpenAI (GPT-5.4)  │  Gemini (3.1 Pro)          │
└────────────────────────┬─────────────────────────────────┘
                         ▼
┌──────────────────────────────────────────────────────────┐
│                    STORAGE LAYER                         │
│  Metadata: SQLite │ Firestore │ MongoDB                  │
│  Blob:     Local  │ S3       │ Firebase Storage          │
└──────────────────────────────────────────────────────────┘
```

### Run Modes

| Method | Session | Compaction | Use Case |
|--------|---------|------------|----------|
| `Run` | No | No | Stateless single-turn |
| `RunWithProtocol` | No | No | Stateless with custom protocol |
| `RunWithSession` | Yes | Yes | Full multi-turn with persistence |

## Protocols

### AG-UI (CopilotKit)

Full event-based protocol with run lifecycle, text streaming, tool calls, state sync, and reasoning.

```go
import "github.com/ratrektlabs/rl-agent/protocol/agui"

p := agui.New()
// Content-Type: text/event-stream
// Event types: RUN_STARTED, TEXT_MESSAGE_START, TEXT_MESSAGE_CONTENT,
// TOOL_CALL_START, TOOL_CALL_ARGS, STATE_SNAPSHOT, STATE_DELTA,
// REASONING_START, REASONING_MESSAGE_CONTENT, RUN_FINISHED, ...
```

### AI SDK (Vercel)

Lightweight SSE format for simple chat streaming.

```go
import "github.com/ratrektlabs/rl-agent/protocol/aisdk"

p := aisdk.New()
// Content-Type: text/plain; charset=utf-8
// Event types: start, text-delta, tool-input-start, tool-input-delta,
// tool-output-available, reasoning, state-snapshot, state-delta
```

### Content Negotiation

```go
reg := protocol.NewRegistry()
reg.Register(agui.New())
reg.Register(aisdk.New())
reg.SetDefault(aisdk.New())

// Automatically selects protocol from Accept header
p := reg.Negotiate(r.Header.Get("Accept"))
```

| Accept Header | Protocol |
|---------------|----------|
| `text/vnd.ag-ui` | AG-UI |
| `text/vnd.ai-sdk` | AI SDK |
| `text/event-stream` | Default |

## Providers

```go
// OpenAI
import "github.com/ratrektlabs/rl-agent/provider/openai"
p := openai.New("gpt-5.4", apiKey)     // or gpt-5.4-mini, gpt-5.4-nano

// Gemini
import "github.com/ratrektlabs/rl-agent/provider/gemini"
p, err := gemini.New("gemini-3.1-pro-preview", apiKey)  // or gemini-3.1-flash-lite-preview
```

Both implement the same `provider.Provider` interface — swap freely.

## Compaction

When conversation history exceeds a configurable threshold, `RunWithSession` automatically summarizes older messages using the LLM and replaces them with a compact summary. Recent messages are preserved verbatim.

```go
a := agent.New(
    agent.WithCompaction(agent.CompactionConfig{
        MaxMessages: 20,  // trigger compaction above 20 messages
        KeepRecent:  6,   // always keep last 6 messages intact
        SummaryRole: "system",
    }),
    // ...other options
)
```

Compaction is non-blocking — if the summarization call fails, the run continues with the full history.

## Skills (3-Layer)

Lazy-loaded skill system inspired by Claude's design:

| Layer | What | Where | When |
|-------|------|-------|------|
| L1 Registration | Name + description | Metadata store | Skill listing |
| L2 Prompt | Full definition (instructions, tools, config) | Metadata store | Skill activation |
| L3 Resources | Scripts, templates, files | Blob store | On-demand execution |

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

## Storage

### Metadata Store

Stores sessions, tools, skills, and key-value memory.

```go
// SQLite (local development)
import metaSQLite "github.com/ratrektlabs/rl-agent/storage/metadata/sqlite"
store, _ := metaSQLite.NewStore(ctx, "./data/agent.db")
defer store.Close()

// Firestore
import metaFirestore "github.com/ratrektlabs/rl-agent/storage/metadata/firestore"
store, _ := metaFirestore.NewStore(ctx, "my-gcp-project")

// MongoDB
import metaMongo "github.com/ratrektlabs/rl-agent/storage/metadata/mongo"
store, _ := metaMongo.NewStore(ctx, "mongodb://localhost:27017", "rl_agent")
defer store.Close(ctx)
```

### Blob Store

Virtual filesystem for agent workspaces — generated files, scripts, artifacts.

```go
// Local filesystem (development)
import blobLocal "github.com/ratrektlabs/rl-agent/storage/blob/local"
fs, _ := blobLocal.New("./data/workspace")

// S3 (AWS, MinIO, Cloudflare R2)
import blobS3 "github.com/ratrektlabs/rl-agent/storage/blob/s3"
fs, _ := blobS3.New(ctx, "my-bucket", blobS3.WithPrefix("agents"))

// Firebase Storage
import blobFirebase "github.com/ratrektlabs/rl-agent/storage/blob/firebase"
fs, _ := blobFirebase.New(ctx, "my-bucket.appspot.com")
```

## HTTP Server Example

```go
reg := protocol.NewRegistry()
reg.Register(agui.New())
reg.Register(aisdk.New())
reg.SetDefault(aisdk.New())

http.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
    p := reg.Negotiate(r.Header.Get("Accept"))
    if p == nil {
        p = reg.Default()
    }
    w.Header().Set("Content-Type", p.ContentType())

    var req struct {
        Message   string `json:"message"`
        SessionID string `json:"sessionId"`
    }
    json.NewDecoder(r.Body).Decode(&req)

    if req.SessionID == "" {
        sess, _ := a.CreateSession(r.Context())
        req.SessionID = sess.ID
    }

    events, err := a.RunWithSession(r.Context(), req.SessionID, req.Message, p)
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }

    p.EncodeStream(r.Context(), w, events)
})
```

## Examples

| Example | Description | Storage |
|---------|-------------|---------|
| [examples/local](./examples/local) | Local development server | SQLite + Local FS |
| [examples/cloud-run](./examples/cloud-run) | Google Cloud Run deployment | MongoDB + S3 |

## Project Status

This project is in active development. The API may change between minor versions before v1.0.

## License

[MIT](./LICENSE) — RatrektLabs
