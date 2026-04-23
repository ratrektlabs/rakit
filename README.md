# rakit

> *Rakit* means bamboo raft in Indonesian — a simple, sturdy vessel that carries you across the river. Built from natural materials, flexible yet reliable. That's what this framework aims to be for AI agents.

**R**emote **A**gent **K**it — a Go framework for building AI agents that stream to any frontend, with persistence out of the box.

## Features

- Multi-provider LLM support (OpenAI, Gemini)
- Dual protocol streaming (AG-UI / CopilotKit, Vercel AI SDK)
- **Agentic loop** — tools execute and results feed back automatically until the agent is done
- **Subagent spawning** — agents can spawn child agents with inherited tools, linked sessions, and independent reasoning
- **MCP support** — Model Context Protocol client with HTTP/SSE transports and dynamic tool discovery
- Session persistence with automatic compaction
- **Scoped memory** — key-value store with global, agent, and user scopes
- 3-layer skill system (registration -> prompt -> resources) with instruction injection
- Pluggable storage (SQLite, Firestore, MongoDB + S3, Firebase, local FS)
- Content negotiation — one agent, any frontend
- **Admin REST API** — manage sessions, skills, tools, MCP servers, memory, workspace, and provider at runtime
- **Function tools** — register Go functions as tools directly with `WithFunction`
- **Human-in-the-loop** — gate tool calls on user approval, delegate tools to the browser, or interrupt & resume runs

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
    "github.com/ratrektlabs/rakit/tool"
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
        agent.WithFunction("greet", "Greet a user by name", map[string]any{
            "type": "object",
            "properties": map[string]any{
                "name": map[string]any{"type": "string"},
            },
            "required": []string{"name"},
        }, func(ctx context.Context, input map[string]any) (*tool.Result, error) {
            name := input["name"].(string)
            return tool.Ok(fmt.Sprintf("Hello, %s!", name)), nil
        }),
    )

    // Session-aware run with agentic loop (tools execute automatically)
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

// Create a session scoped to a user
sess, _ := a.CreateSessionForUser(ctx, "user-123")

// First message
a.RunWithSession(ctx, sess.ID, "My name is Alice", aisdk.New())

// Next request carries full context — agent remembers
a.RunWithSession(ctx, sess.ID, "What's my name?", aisdk.New())
```

Sessions survive server restarts. Load an existing session by passing its ID. Sessions can be filtered by user:

```go
sessions, _ := store.ListSessionsByUser(ctx, agentID, "user-123")
```

### Memory

A scoped key-value store backed by the metadata adapter. Memory supports three scopes:

| Scope | Use case |
|-------|----------|
| `global` | Shared across all agents and users |
| `agent` | Scoped to a specific agent |
| `user` | Scoped to a specific user |

```go
// Scoped memory
store.SetMemory(ctx, metadata.ScopeUser, "user-123", "timezone", []byte("UTC-5"))
value, _ := store.GetMemory(ctx, metadata.ScopeUser, "user-123", "timezone")
keys, _ := store.ListMemory(ctx, metadata.ScopeUser, "user-123", "")

// Legacy flat API (delegates to global scope)
store.Set(ctx, "key", []byte("value"))
value, _ := store.Get(ctx, "key")
```

### Tools

Tools are capabilities the LLM can call during a run. Each tool returns a structured `Result` with status tracking.

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() any
    Execute(ctx context.Context, input map[string]any) (*Result, error)
}
```

Register tools on the agent:

```go
a := agent.New(
    // Register tool implementations
    agent.WithTools(myTool1, myTool2),

    // Or register Go functions directly
    agent.WithFunction("search", "Search the web", params,
        func(ctx context.Context, input map[string]any) (*tool.Result, error) {
            return tool.Ok(results), nil
        },
    ),
)
```

Tool results include structured metadata:

```go
tool.Ok(data)                                    // success with data
tool.Err("connection refused", "Check service")  // error with fix recommendation
tool.Measure(func() (*tool.Result, error) { })   // auto-fills Duration
```

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

Skill instructions are automatically injected into the system prompt when the skill is enabled.

### MCP (Model Context Protocol)

rakit includes an MCP client that connects to external MCP servers and discovers their tools at runtime.

```go
// MCP registry is auto-created when you set a store
a := agent.New(agent.WithStore(store))

// Register an MCP server
store.SaveMCPServer(ctx, &metadata.MCPServerDef{
    Name:      "filesystem",
    URL:       "http://localhost:3000",
    Transport: "http",
    AgentID:   a.ID,
    Enabled:   true,
})

// Tools from enabled MCP servers are automatically merged into the agent's tool registry
// per-iteration, so newly added servers are picked up without restart
```

Supported transports: HTTP (JSON-RPC) and SSE (Server-Sent Events).

### Subagents

Agents can spawn child agents for complex subtasks. Child agents inherit tools, skills, and storage from the parent, with their own session linked to the parent.

```go
// Spawn programmatically
child := a.Spawn(ctx, parentSessionID, agent.SubagentConfig{
    System:       "You are a research assistant.",
    InheritTools: true,
})

// Or use the built-in spawn_agent tool — the LLM can spawn subagents itself
a.Tools.Register(a.SpawnAgentTool(protocol))
```

### Human-in-the-loop

rakit pauses an agent run whenever a tool call needs human intervention. There
are three primitives you can compose, all additive — existing code keeps its
current behavior unless you opt in.

**1. Approval policy.** Gate one or more tools on explicit user approval:

```go
a := agent.New(
    agent.WithProvider(prov),
    agent.WithStore(store),
    agent.WithProtocol(aisdk.New()),
    agent.WithApprovalPolicy(agent.RequireFor("delete_item", "drop_table")),
)
```

Helpers: `agent.RequireAll()`, `agent.RequireNone()`, `agent.RequireFor(names...)`,
or implement `agent.ApprovalPolicy` / pass an `agent.ApprovalPolicyFunc` for
arbitrary logic.

When the model calls a gated tool, the runner persists the call as
`pending_approval` on the session, emits a `ToolCallPendingEvent`, and ends
the stream. Resume with:

```go
events, _ := a.Resume(ctx, sessionID, []agent.ToolDecision{
    {ToolCallID: "tc_1", Approve: true},  // server executes the tool
    // or
    {ToolCallID: "tc_2", Approve: false, Message: "not now"}, // synthetic rejection
}, aisdk.New())
```

**2. Client-side tools.** Register a tool whose `Handler` is `"client"` to
have the frontend execute it:

```go
a.Skills.Register(ctx, &skill.Definition{
    Name: "browser",
    Tools: []skill.ToolDef{{
        Name:        "browser_time",
        Description: "Returns the caller's current time",
        Handler:     "client",
        Parameters:  map[string]any{"type": "object"},
    }},
})
```

The runner signals the pause with a standard protocol event — AG-UI
emits a `CUSTOM` event with `name: "tool_call_pending"`, and AI SDK emits
a `data-tool-call-pending` custom data part. Both carry the same payload:
`{toolCallId, toolName, arguments, reason}` where `reason` is either
`"approval_required"` or `"client_side"`. The frontend runs the tool
and resumes with `Result`:

```js
// examples/local/index.html — runClientTool + /chat/resume
const result = await runClientTool(name, args);
await fetch('/chat/resume', {
    method: 'POST',
    headers: {'Content-Type': 'application/json', 'Accept': 'text/vnd.ai-sdk'},
    body: JSON.stringify({
        sessionId,
        decisions: [{ toolCallId, approve: true, result: JSON.stringify(result) }],
    }),
});
```

**3. Interrupt.** Cancel an in-flight run without losing pending state:

```go
a.Interrupt(sessionID) // signals the current run; pending tool calls survive
```

Run `examples/local` (the `delete_item` tool is approval-gated and the
`browser_time` tool is client-side) to see the full loop end-to-end.

### Compaction

When a session's message history grows too long, rakit automatically summarizes older messages using the LLM. This keeps context windows manageable without losing important context.

```go
a := agent.New(
    agent.WithCompaction(agent.CompactionConfig{
        MaxMessages: 20,  // trigger compaction above 20 messages
        KeepRecent:  6,   // always keep last 6 messages intact
        SummaryRole: "system",
    }),
)
```

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
p := openai.New("gpt-5.4", apiKey)

// Gemini
p, _ := gemini.New("gemini-3.1-pro-preview", apiKey)
```

Both implement the same `provider.Provider` interface — swap freely. Model can be changed at runtime:

```go
p.SetModel("gemini-2.0-flash")
```

## Storage Adapters

**Metadata** (sessions, tools, skills, memory, MCP servers):

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
| [examples/local](examples/local) | Local dev server with admin UI and REST API | SQLite + Local FS |
| [examples/cloud-run](examples/cloud-run) | Cloud Run deployment | MongoDB + S3 |

## License

[MIT](./LICENSE) — RatrektLabs
