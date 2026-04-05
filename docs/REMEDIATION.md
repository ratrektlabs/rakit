# Remediation Plan

Gaps between the current implementation and the target vision for rakit as a **Remote Agent Kit** framework.

> All items below have been **implemented** on the `feat/agent-improvements` branch.

---

## 1. User Identity & Scoped Memory

**Status:** DONE

**Problem:**
Memory is a flat key-value store with convention-based prefixes. Sessions are scoped by `agentID` but not by user. There is no `userID` anywhere in the codebase.

**Target:**
Memory should be scoped by user, agent, or global. Sessions should belong to a user.

**Changes:**

### 1.1 Add `UserID` to Session

```go
// storage/metadata/metadata.go
type Session struct {
    ID        string         `json:"id"`
    AgentID   string         `json:"agentId"`
    UserID    string         `json:"userId"`     // NEW
    Messages  []Message      `json:"messages"`
    State     map[string]any `json:"state"`
    CreatedAt int64          `json:"createdAt"`
    UpdatedAt int64          `json:"updatedAt"`
}
```

- `CreateSession(ctx, agentID, userID)` — add userID parameter
- `ListSessions(ctx, agentID)` — add optional `ListSessionsForUser(ctx, agentID, userID)`

### 1.2 Scoped Memory Interface

Replace the flat KV methods with a scoped memory interface:

```go
// storage/metadata/metadata.go
type MemoryScope string

const (
    ScopeGlobal MemoryScope = "global"
    ScopeAgent  MemoryScope = "agent"
    ScopeUser   MemoryScope = "user"
)

// Memory methods become:
SetMemory(ctx context.Context, scope MemoryScope, scopeID string, key string, value []byte) error
GetMemory(ctx context.Context, scope MemoryScope, scopeID string, key string) ([]byte, error)
DeleteMemory(ctx context.Context, scope MemoryScope, scopeID string, key string) error
ListMemory(ctx context.Context, scope MemoryScope, scopeID string, prefix string) ([]string, error)
```

Under the hood, this can still be flat KV with composite keys like `global::key`, `agent:<id>::key`, `user:<id>::key`.

### 1.3 Files to change

- `storage/metadata/metadata.go` — interface changes
- `storage/metadata/sqlite/store.go` — implementation
- `storage/metadata/firestore/store.go` — implementation
- `storage/metadata/mongo/store.go` — implementation
- `agent/agent.go` — `CreateSession` signature
- `agent/runner.go` — pass userID through context or session
- `examples/local/main.go` — accept userID in `/chat` request
- `examples/local/admin.go` — update memory endpoints

---

## 2. Subagent Spawning

**Status:** Missing — no subagent concept

**Problem:**
The `Agent` struct has no way to spawn child agents, share context, or coordinate results. A parent agent cannot delegate subtasks.

**Target:**
An agent should be able to spawn a child agent as a tool call, with its own session, inherited tools/skills, and result reporting back to the parent.

**Changes:**

### 2.1 Subagent Definition

```go
// agent/subagent.go
type SubagentConfig struct {
    ID            string           // optional — auto-generated if empty
    Provider      provider.Provider // optional — inherit from parent
    Tools         []tool.Tool       // additional tools (inherits parent tools by default)
    Skills        []string          // skill names to enable (inherits parent by default)
    System        string            // system prompt override
    MaxIterations int               // default: parent's value
    InheritTools  bool              // default: true
}
```

### 2.2 Spawn Method

```go
// agent/agent.go
func (a *Agent) Spawn(ctx context.Context, cfg SubagentConfig) *Agent
```

- Creates a child `Agent` sharing the same `Store` and `FS`
- Gets its own session (linked to parent session via metadata)
- Inherits parent tools/skills unless overridden
- Returns results as a tool result to the parent's agentic loop

### 2.3 Built-in `spawn_agent` Tool

Register a built-in tool that the LLM can call to spawn a subagent:

```go
tool.NewFunctionTool("spawn_agent", "Spawn a subagent to handle a subtask", spawnSchema, func(ctx, input) {
    child := parentAgent.Spawn(ctx, SubagentConfig{
        System: input["instructions"].(string),
    })
    sess, _ := child.CreateSession(ctx)
    events, _ := child.RunWithSession(ctx, sess.ID, input["task"].(string), protocol)
    // Collect final response
    return tool.Ok(finalResponse)
})
```

### 2.4 Session Lineage

```go
// storage/metadata/metadata.go
type Session struct {
    // ... existing fields ...
    ParentSessionID string `json:"parentSessionId,omitempty"` // NEW — links child to parent
}
```

### 2.5 Files to change

- `agent/subagent.go` — new file
- `agent/agent.go` — `Spawn()` method
- `storage/metadata/metadata.go` — `ParentSessionID` field
- `examples/local/main.go` — opt-in to subagent tool

---

## 3. SSE MCP Transport

**Status:** Partial — only Streamable HTTP (JSON-RPC over POST)

**Problem:**
`mcp/client.go` does synchronous request-response via HTTP POST. It does not support SSE transport, which some MCP servers use for streaming tool results or server-initiated events.

**Target:**
Support both Streamable HTTP (current) and SSE transport for MCP servers.

**Changes:**

### 3.1 Transport Interface

```go
// mcp/transport.go
type Transport interface {
    Send(ctx context.Context, method string, params any, result any) error
    Notify(ctx context.Context, method string, params any) error
    Close() error
}
```

### 3.2 Two Implementations

```go
// mcp/transport_http.go — current logic extracted from client.go
type HTTPTransport struct { ... }

// mcp/transport_sse.go — new SSE transport
type SSETransport struct { ... }
```

The SSE transport:
- Opens a persistent GET connection for server-sent events
- Sends JSON-RPC requests via POST to the server's message endpoint
- Matches responses by JSON-RPC ID
- Handles server-initiated notifications

### 3.3 Auto-detection

```go
// MCPServerDef gets a Transport field
type MCPServerDef struct {
    // ... existing fields ...
    Transport string `json:"transport"` // "http" (default), "sse"
}
```

The client picks the transport based on config, or auto-detects by checking the server's response headers.

### 3.4 Files to change

- `mcp/transport.go` — new file, transport interface
- `mcp/transport_http.go` — new file, extract from `client.go`
- `mcp/transport_sse.go` — new file, SSE implementation
- `mcp/client.go` — refactor to use `Transport` interface
- `mcp/types.go` — update if needed
- `storage/metadata/metadata.go` — `Transport` field on `MCPServerDef`
- All metadata store implementations — schema migration for new field

---

## 4. Script Tool Execution

**Status:** Stub — loads script from blob store but does not execute

**Problem:**
`skill/handlers.go` `ScriptTool.Execute()` is a placeholder. No actual script execution happens.

**Target:**
Execute scripts loaded from blob store in a sandboxed environment.

**Changes:**

### 4.1 Phase 1 — Subprocess Execution

Simple and practical first step:

```go
// skill/handlers.go
func (t *ScriptTool) Execute(ctx context.Context, input map[string]any) (*tool.Result, error) {
    script, _ := t.rm.Load(ctx, t.scriptPath)

    // Write to temp file, execute with interpreter based on extension
    // .py → python3, .sh → bash, .js → node
    // Pass input as JSON via stdin
    // Capture stdout as result
}
```

- Timeout enforcement via context
- Input passed as JSON on stdin
- Output captured from stdout (expected as JSON)
- Stderr captured for error reporting

### 4.2 Phase 2 — Sandboxed Execution (future)

- WASM runtime (wazero) for untrusted scripts
- Docker container execution for heavier workloads
- Resource limits (CPU, memory, network)

### 4.3 Files to change

- `skill/handlers.go` — implement `ScriptTool.Execute()`
- `skill/handlers_test.go` — new test file

---

## 5. Dynamic Tool Loading (Hot Reload)

**Status:** Partial — registry rebuilds per run, but no mid-session reload

**Problem:**
`buildMergedRegistry()` builds the tool registry at the start of each `RunWithSession` call. Tools added via admin API mid-session are not available until the next run.

**Target:**
Tools registered via admin API or MCP discovery should be available in the current session without requiring a new message.

**Changes:**

### 5.1 Approach

Instead of rebuilding per-run, rebuild per-iteration of the agentic loop:

```go
// agent/runner.go — inside the agentic loop
for i := 0; i < a.maxIterations; i++ {
    // Rebuild registry each iteration
    registry, systemPrompt, err := a.buildMergedRegistry(ctx)
    // ... rest of loop
}
```

This is simple and sufficient — tools are refreshed between tool call rounds within a single run.

### 5.2 Files to change

- `agent/runner.go` — move `buildMergedRegistry` inside the loop

---

## Priority Order

| # | Item | Impact | Effort |
|---|------|--------|--------|
| 1 | User Identity & Scoped Memory | High — foundational for multi-user | Medium |
| 2 | Subagent Spawning | High — core differentiator | Medium |
| 3 | Script Tool Execution (Phase 1) | Medium — enables skill resources | Low |
| 4 | SSE MCP Transport | Medium — broader MCP compatibility | Medium |
| 5 | Dynamic Tool Loading | Low — quality of life | Low |
