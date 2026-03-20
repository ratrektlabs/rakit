# rl-agent Framework Research

## Overview

Lightweight agentic framework untuk Go, focused on building **remote, multi-user AI applications** (bukan local coding assistants seperti Claude Code/OpenClaw).

---

## Core Features

### 1. Lightweight Philosophy
- Minimal dependencies
- Fast startup & low memory footprint
- Modular - ambil cuma yang lu butuh
- Idiomatic Go (concurrency, interfaces, error handling)

### 2. Memory System

**Pattern dari research:**
- **Short-term (Ephemeral)**: Session-based, in-context window
- **Long-term (Persistent)**: Stored across sessions

**Design approach:**
- Memory interface dengan multiple backend implementations
- Support untuk semantic search (embeddings) future-ready
- Timeline/event stream untuk audit trail

**Storage backends (v1):**
- SQLite (local/dev, simple deployment)
- MongoDB (production, scalable, document-based)

### 3. Tools & Skills

**Tools = Functions yang agent bisa panggil**
- Define tool interface (name, description, parameters schema, execute)
- JSON schema untuk parameter validation
- Tool registry untuk discovery
- Support sync & async execution

**Skills = Modular capability packages**
- Bundle of instructions + tools + templates
- Discovery & load on demand
- Interoperable format (inspired by AgentSkills spec)

### 4. Multi-Provider LLM

**Abstraction pattern:**
```go
type Provider interface {
    Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
    Stream(ctx context.Context, req *CompletionRequest) (<-chan StreamEvent, error)
}
```

**Providers (v1):**
- OpenAI (GPT-4, GPT-4o, etc.)
- Anthropic (Claude 3/4)
- Google Gemini
- Zai (GLM, Pony)

**Key features:**
- Unified message format (normalize differences)
- Streaming via Go channels
- Tool/function calling support
- Error handling & retries
- Token estimation

**Reference libs:**
- github.com/plexusone/omnillm
- github.com/teilomillet/gollm
- github.com/skanakakorn/llm-sdk-go

### 5. Remote-First, Multi-User

**Differentiator vs Claude Code/OpenClaw:**
- Built for **app development**, not local assistance
- Multi-tenant by design
- Session management per user
- API-first architecture
- Auth & authorization hooks

**Architecture:**
```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Client A  │────▶│             │────▶│   Agent A   │
├─────────────┤     │   rl-agent  │     ├─────────────┤
│   Client B  │────▶│   Server    │────▶│   Agent B   │
├─────────────┤     │             │     ├─────────────┤
│   Client C  │────▶│   (Core)    │────▶│   Agent C   │
└─────────────┘     └─────────────┘     └─────────────┘
```

### 6. Protocol Support

**AGUI Protocol (Agent-User Interaction) - Primary Focus:**

**Event types (16 standardized):**
- Lifecycle: `RUN_STARTED`, `RUN_FINISHED`, `RUN_ERROR`, `STEP_STARTED`, `STEP_FINISHED`
- Text: `TEXT_MESSAGE_START`, `TEXT_MESSAGE_CONTENT`, `TEXT_MESSAGE_END`
- Tool: `TOOL_CALL_START`, `TOOL_CALL_ARGS`, `TOOL_CALL_END`
- State: `STATE_SNAPSHOT`, `STATE_DELTA`, `MESSAGES_SNAPSHOT`
- Special: `RAW`, `CUSTOM`

**Key concepts:**
- Event-driven communication
- Bidirectional (user ↔ agent)
- Transport agnostic (SSE, WebSocket, HTTP binary)
- State management via snapshots + deltas (JSON Patch RFC 6902)

**Future protocols (extensible):**
- SSE (Server-Sent Events) - simple streaming
- Non-streaming HTTP - traditional request/response

---

## Proposed Architecture

```
rl-agent/
├── agent/           # Core agent implementation
│   ├── agent.go     # Agent struct, Run(), lifecycle
│   └── options.go   # Config options
├── provider/        # LLM providers
│   ├── provider.go  # Provider interface
│   ├── openai/
│   ├── anthropic/
│   ├── gemini/
│   └── zai/
├── memory/          # Memory system
│   ├── memory.go    # Memory interface
│   ├── sqlite/
│   └── mongodb/
├── tool/            # Tool system
│   ├── tool.go      # Tool interface, registry
│   └── builtin/     # Built-in tools
├── skill/           # Skills system
│   ├── skill.go     # Skill interface, loader
│   └── registry/
├── protocol/        # Communication protocols
│   ├── protocol.go  # Protocol interface
│   └── agui/        # AGUI implementation
├── storage/         # Shared storage abstractions
│   └── storage.go
├── session/         # Session management
│   └── session.go
└── examples/        # Usage examples
```

---

## Key Interfaces (Draft)

### Provider
```go
type Provider interface {
    Name() string
    Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
    Stream(ctx context.Context, req *CompletionRequest) (<-chan StreamEvent, error)
    SupportsStreaming() bool
    SupportsTools() bool
}
```

### Memory
```go
type Memory interface {
    Add(ctx context.Context, userID, sessionID string, entry MemoryEntry) error
    Get(ctx context.Context, userID, sessionID string, limit int) ([]MemoryEntry, error)
    Search(ctx context.Context, userID string, query string, limit int) ([]MemoryEntry, error)
    Clear(ctx context.Context, userID, sessionID string) error
}

type MemoryBackend interface {
    Connect(ctx context.Context) error
    Close() error
    Memory() Memory
}
```

### Tool
```go
type Tool interface {
    Name() string
    Description() string
    Parameters() jsonschema.Schema
    Execute(ctx context.Context, params map[string]any) (any, error)
}
```

### Protocol
```go
type Protocol interface {
    Name() string
    HandleAgentRun(ctx context.Context, agent *Agent, input RunInput) (<-chan Event, error)
}
```

---

## Design Principles

1. **Interface-first** - Semua komponen via interface, gampang di-extend
2. **Concurrency-native** - Go routines & channels everywhere
3. **Error-explicit** - Proper error handling, no panics in library code
4. **Configurable** - Functional options pattern
5. **Observable** - Hooks untuk logging, metrics, tracing
6. **Testable** - Mock interfaces, table-driven tests

---

## MVP Scope

**Phase 1 (Core):**
- [ ] Provider interface + OpenAI implementation
- [ ] Agent core (run loop, message handling)
- [ ] Memory interface + SQLite backend
- [ ] Tool interface + registry
- [ ] Basic AGUI protocol support

**Phase 2 (Extend):**
- [ ] Anthropic, Gemini, Zai providers
- [ ] MongoDB backend
- [ ] Skills system
- [ ] Session management
- [ ] Full AGUI event types

**Phase 3 (Polish):**
- [ ] SSE protocol
- [ ] Non-streaming HTTP
- [ ] Examples & docs
- [ ] Performance optimization

---

## References

### Go Agent Frameworks
- github.com/google/adk-go - Google's Agent Development Kit
- github.com/cloudwego/eino - LLM app framework
- github.com/Protocol-Lattice/go-agent - Production agents with UTCP

### LLM Abstraction
- github.com/plexusone/omnillm - Multi-provider SDK
- github.com/teilomillet/gollm - Unified interface

### AGUI Protocol
- docs.ag-ui.com - Official documentation
- github.com/ag-ui-protocol/ag-ui - Protocol spec

### Memory Patterns
- letta.com/blog/agent-memory - Memory architectures
- AgentSkills spec - Skill interoperability
