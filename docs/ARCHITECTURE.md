# rakit Architecture

## Overview

```mermaid
graph TB
    Client[Client]

    subgraph Protocol Layer
        AGUI[AG-UI / CopilotKit]
        AISDK[AI SDK / Vercel]
    end

    subgraph Agent Runtime
        RWS[RunWithSession<br>+ Compaction]
    end

    subgraph Provider Layer
        OpenAI[OpenAI]
        Gemini[Gemini]
    end

    subgraph Storage Layer
        Meta[Metadata Store]
        Blob[Blob Store]
    end

    Client --> AGUI
    Client --> AISDK
    AGUI --> RWS
    AISDK --> RWS
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
| `RunWithSession` | Yes | Yes | Multi-turn with persistence |

### Session Flow

```mermaid
sequenceDiagram
    participant C as Client
    participant A as Agent
    participant S as Store
    participant P as Provider

    C->>A: RunWithSession(sessionID, input)
    A->>S: GetSession(sessionID)
    S-->>A: Session + history
    A->>A: Append user message
    A->>A: shouldCompact()?
    alt Over threshold
        A->>P: Generate(summary)
        P-->>A: Summary
        A->>A: Replace old msgs
    end
    A->>P: Stream(history)
    P-->>C: Events (streamed)
    A->>S: UpdateSession()
```

## Package Structure

```
github.com/ratrektlabs/rakit
├── agent/          # Agent runtime, runner, compaction, hooks
├── provider/       # Provider interface + OpenAI, Gemini
├── protocol/       # Protocol interface + AG-UI, AI SDK, registry
├── tool/           # Tool interface + registry
├── skill/          # 3-layer skill system
├── storage/
│   ├── metadata/   # Store interface + SQLite, Firestore, MongoDB
│   └── blob/       # BlobStore interface + local, S3, Firebase
└── examples/       # local (SQLite), cloud-run (MongoDB + S3)
```

## Storage

| Type | Interface | Adapters |
|------|-----------|----------|
| Metadata | Sessions, tools, skills, memory (KV) | SQLite, Firestore, MongoDB |
| Blob | Read, Write, Delete, List | Local FS, S3, Firebase Storage |
