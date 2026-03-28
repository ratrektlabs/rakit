# rakit

**R**emote **A**gent **K**it — a Go framework for building AI agents that stream to any frontend, with persistence out of the box.

## Features

- Multi-provider LLM support (OpenAI, Gemini)
- Dual protocol streaming (AG-UI / CopilotKit, Vercel AI SDK)
- Session persistence with automatic compaction
- 3-layer skill system (registration → prompt → resources)
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

## Documentation

| Doc | Description |
|-----|-------------|
| [Architecture](docs/ARCHITECTURE.md) | Layer design, interfaces, data flow |
| [Examples](examples/) | Local dev server, Cloud Run deployment |

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

## Status

Active development. API may change before v1.0.

## License

[MIT](./LICENSE) — RatrektLabs
