# rakit Architecture

## Overview

```mermaid
graph TB
    Client[Client<br>CopilotKit / Vercel AI / Custom]

    subgraph Protocol Layer
        AGUI[AG-UI<br>CopilotKit]
        AISDK[AI SDK<br>Vercel]
        Reg[Registry +<br>Negotiation]
    end

    subgraph Agent Runtime
        Run[Run]
        RWP[RunWithProtocol]
        RWS[RunWithSession<br>+ Compaction]
    end

    subgraph Provider Layer
        OpenAI[OpenAI<br>GPT-5.4]
        Gemini[Gemini<br>3.1 Pro]
    end

    subgraph Storage Layer
        Meta[Metadata Store<br>sessions · tools · skills · memory]
        Blob[Blob Store<br>agent workspace]
    end

    Client --> Reg
    Reg --> AGUI
    Reg --> AISDK
    AGUI --> RWS
    AISDK --> RWS
    RWP --> Run
    Run --> OpenAI
    Run --> Gemini
    RWS --> OpenAI
    RWS --> Gemini
    RWS --> Meta
    RWS --> Blob
```

## Run Modes

| Method | Session | Compaction | Use Case |
|--------|---------|------------|----------|
| `Run` | No | No | Stateless single-turn |
| `RunWithProtocol` | No | No | Stateless with custom protocol |
| `RunWithSession` | Yes | Yes | Full multi-turn with persistence |

### RunWithSession Flow

```mermaid
sequenceDiagram
    participant C as Client
    participant A as Agent
    participant S as Metadata Store
    participant P as Provider

    C->>A: RunWithSession(sessionID, input, protocol)
    A->>S: GetSession(sessionID)
    S-->>A: Session with history
    A->>A: Append user message
    A->>A: shouldCompact()?
    alt History exceeds threshold
        A->>P: Generate(summary request)
        P-->>A: Summary
        A->>A: Replace old messages with summary
    end
    A->>P: Stream(full message history)
    P-->>A: Streaming events
    A->>A: Accumulate response
    A-->>C: Protocol events (streamed)
    A->>S: UpdateSession(append assistant message)
```

## Protocol Layer

```mermaid
graph LR
    subgraph Registry
        Negotiate[Negotiate<br>Accept header]
    end

    Negotiate -->|text/vnd.ag-ui| AGUI[AG-UI Protocol<br>text/event-stream]
    Negotiate -->|text/vnd.ai-sdk| AISDK[AI SDK Protocol<br>text/plain]
    Negotiate -->|default| Default[Default Protocol]

    subgraph AG-UI Events
        RS[RUN_STARTED]
        TMS[TEXT_MESSAGE_START]
        TMC[TEXT_MESSAGE_CONTENT]
        TME[TEXT_MESSAGE_END]
        TCS[TOOL_CALL_START]
        TCA[TOOL_CALL_ARGS]
        SS[STATE_SNAPSHOT]
        SD[STATE_DELTA]
        RG[REASONING_START]
        RF[RUN_FINISHED]
    end

    subgraph AI SDK Events
        Start[start]
        TD[text-delta]
        TIS[tool-input-start]
        TID[tool-input-delta]
        TOA[tool-output-available]
        Done[finish]
    end
```

| Feature | AG-UI | AI SDK |
|---------|-------|--------|
| Source | CopilotKit (open spec) | Vercel AI SDK |
| Transport | SSE / WebSocket | SSE |
| Event types | 20+ | ~5 |
| State sync | Snapshot + JSON Patch | No |
| Tool streaming | Start → Args → End | Single event |
| Reasoning | Yes | No |
| Best for | Rich agent UIs | Simple chat |

## Provider Layer

```go
type Provider interface {
    Name() string
    Model() string
    Models() []string
    Stream(ctx context.Context, req *Request) (<-chan Event, error)
    Generate(ctx context.Context, req *Request) (*Response, error)
}
```

| Provider | Models |
|----------|--------|
| OpenAI | `gpt-5.4`, `gpt-5.4-mini`, `gpt-5.4-nano` |
| Gemini | `gemini-3.1-pro-preview`, `gemini-3.1-flash-lite-preview` |

Model is selected at construction time: `openai.New("gpt-5.4", apiKey)`.

## Skill System (3-Layer)

```mermaid
graph TB
    subgraph L1 - Registration
        L1[Name + Description<br>metadata store]
    end

    subgraph L2 - Prompt
        L2[Full Definition<br>instructions · tools · config<br>metadata store]
    end

    subgraph L3 - Resources
        L3[Scripts · Templates · Files<br>blob store]
    end

    L1 -->|skill selected| L2
    L2 -->|executing| L3

    style L1 fill:#e1f5fe
    style L2 fill:#b3e5fc
    style L3 fill:#81d4fa
```

| Layer | What | Where | When |
|-------|------|-------|------|
| L1 | Name + description | Metadata store | Skill listing |
| L2 | Full definition (instructions, tools, config) | Metadata store | Skill activation |
| L3 | Scripts, templates, files | Blob store | On-demand execution |

## Compaction

When a session's message history exceeds `MaxMessages` (default: 20), the agent uses the LLM to summarize older messages into a single system message. The most recent `KeepRecent` (default: 6) messages are preserved verbatim.

```go
agent.WithCompaction(agent.CompactionConfig{
    MaxMessages: 20,
    KeepRecent:  6,
    SummaryRole: "system",
})
```

Compaction is non-blocking — if summarization fails, the run continues with full history.

## Storage Layer

### Metadata Store

```go
type Store interface {
    // Sessions
    CreateSession(ctx, agentID) (*Session, error)
    GetSession(ctx, id) (*Session, error)
    UpdateSession(ctx, s *Session) error
    DeleteSession(ctx, id) error

    // Tools
    SaveTool(ctx, tool *ToolDef) error
    GetTool(ctx, name) (*ToolDef, error)
    ListTools(ctx, agentID) ([]*ToolDef, error)
    DeleteTool(ctx, name) error

    // Skills
    SaveSkill(ctx, def *SkillDef) error
    GetSkill(ctx, name) (*SkillDef, error)
    ListSkills(ctx) ([]*SkillEntry, error)
    DeleteSkill(ctx, name) error

    // Memory (key-value)
    Set(ctx, key, value) error
    Get(ctx, key) ([]byte, error)
    Delete(ctx, key) error
    List(ctx, prefix) ([]string, error)
}
```

| Adapter | Import | Environment |
|---------|--------|-------------|
| SQLite | `storage/metadata/sqlite` | Local dev |
| Firestore | `storage/metadata/firestore` | GCP |
| MongoDB | `storage/metadata/mongo` | Multi-cloud |

### Blob Store

```go
type BlobStore interface {
    Read(ctx, path) ([]byte, error)
    Write(ctx, path, data) error
    Delete(ctx, path) error
    List(ctx, prefix) ([]string, error)
}
```

| Adapter | Import | Environment |
|---------|--------|-------------|
| Local FS | `storage/blob/local` | Local dev |
| S3 | `storage/blob/s3` | AWS, MinIO, R2 |
| Firebase | `storage/blob/firebase` | GCP |

## Package Structure

```
github.com/ratrektlabs/rakit
├── agent/
│   ├── agent.go          # Agent struct + options
│   ├── runner.go         # Run / RunWithProtocol / RunWithSession
│   ├── compaction.go     # LLM summarization + message conversion
│   └── hook.go           # Observability hooks
├── provider/
│   ├── provider.go       # Provider interface + types
│   ├── openai/
│   │   └── provider.go
│   └── gemini/
│       └── provider.go
├── protocol/
│   ├── protocol.go       # Protocol interface + event types
│   ├── registry.go       # Protocol registry + negotiation
│   ├── agui/
│   │   └── protocol.go   # AG-UI (CopilotKit)
│   └── aisdk/
│       └── protocol.go   # AI SDK (Vercel)
├── tool/
│   ├── tool.go           # Tool interface
│   └── registry.go       # Tool registry
├── skill/
│   ├── skill.go          # Types: Entry, Definition, ToolDef, Resource
│   ├── registry.go       # L1 Registry (metadata store)
│   ├── loader.go         # L2 Loader (full definition)
│   ├── resources.go      # L3 ResourceManager (blob store)
│   └── handlers.go       # HTTPTool, ScriptTool
├── storage/
│   ├── metadata/
│   │   ├── metadata.go   # Store interface + types
│   │   ├── sqlite/
│   │   ├── firestore/
│   │   └── mongo/
│   └── blob/
│       ├── blob.go       # BlobStore interface
│       ├── local/
│       ├── s3/
│       └── firebase/
└── examples/
    ├── local/            # Local dev server (SQLite + local FS)
    └── cloud-run/        # Cloud Run deployment (MongoDB + S3)
```
