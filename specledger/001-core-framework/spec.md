# rl-agent v2 Specification

## Overview

**Name:** rl-agent
**Version:** 2.0.0
**Type:** Go Library
**Purpose:** Lightweight, extensible agentic framework for building AI-powered applications

## Core Principles

1. **Zero external dependencies** - Standard library only for core
2. **Interface-first design** - All components via interfaces
3. **Composable** - Mix and match components as needed
4. **Developer-first API** - Simple, intuitive, fluent
5. **Remote-first** - Built for multi-user applications, not local assistants

---

## Component: Provider

### Purpose
Abstraction layer for multiple LLM providers

### Interface
```go
type Provider interface {
    // Name returns the provider name (e.g., "openai", "anthropic")
    Name() string
    
    // Complete sends a non-streaming completion request
    Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
    
    // Stream sends a streaming completion request, returns event channel
    Stream(ctx context.Context, req *CompletionRequest) (<-chan StreamEvent, error)
    
    // Capabilities returns what this provider supports
    Capabilities() ProviderCapabilities
}
```

### Data Types
```go
type CompletionRequest struct {
    Messages    []Message
    Model       string
    MaxTokens   *int
    Temperature *float64
    Tools       []ToolDefinition
}

type CompletionResponse struct {
    Content    string
    ToolCalls  []ToolCall
    Usage      Usage
    FinishReason string
}

type StreamEvent struct {
    Type      StreamEventType  // content_delta, tool_call, done, error
    Delta     string
    ToolCall  *ToolCall
    Error     error
}

type Message struct {
    Role       MessageRole  // system, user, assistant, tool
    Content    string
    ToolCalls  []ToolCall
    ToolResult *ToolResult
}

type ToolCall struct {
    ID       string
    Name     string
    Arguments json.RawMessage
}
```

### Capabilities
```go
type ProviderCapabilities struct {
    Streaming    bool
    ToolCalling  bool
    Vision       bool
}
```

### Implementations (v1)
- [ ] OpenAI - `provider/openai`
- [ ] Anthropic - `provider/anthropic`
- [ ] Gemini - `provider/gemini`
- [ ] Zai - `provider/zai`

### Contract
- All providers MUST implement both Complete and Stream
- Stream MUST close channel when done or on error
- Tool calling is optional (check Capabilities)
- Providers MUST be thread-safe
- Errors MUST be wrapped with context

---

## Component: Agent

### Purpose
Orchestrates provider, tools, and memory to execute agent loops

### Interface
```go
type Agent interface {
    // Run executes the agent with given messages and options
    Run(ctx context.Context, messages []Message, opts ...RunOption) (*RunResult, error)
    
    // Stream executes the agent with streaming
    Stream(ctx context.Context, messages []Message, opts ...RunOption) (<-chan StreamEvent, error)
    
    // AddTool registers a tool
    AddTool(tool Tool) error
    
    // AddSkill registers a skill
    AddSkill(skill Skill) error
}
```

### Configuration
```go
type AgentConfig struct {
    Provider     Provider
    SystemPrompt string
    Model        string
    MaxSteps     int
    Temperature  float64
    MaxTokens    int
}

type RunOption func(*RunConfig)

// Options
func WithTools(tools ...Tool) RunOption
func WithSkills(skills ...Skill) RunOption
func WithMaxSteps(n int) RunOption
func WithSession(userID, sessionID string) RunOption
```

### Run Loop
1. Build request with system prompt + messages + tools
2. Call provider.Complete or provider.Stream
3. If tool calls present:
   a. Execute tools
   b. Add tool results to messages
   c. Go to step 1
4. Return final response

### Contract
- Agent MUST respect context cancellation
- Agent MUST limit loops to MaxSteps
- Tool execution errors MUST be handled gracefully
- Agent MUST be thread-safe for concurrent runs

---

## Component: Tool

### Purpose
Define executable functions that agents can call

### Interface
```go
type Tool interface {
    Name() string
    Description() string
    Parameters() jsonschema.Schema
    Execute(ctx context.Context, args map[string]any) (any, error)
}
```

### Registry
```go
type ToolRegistry interface {
    Register(tool Tool) error
    Get(name string) (Tool, error)
    List() []ToolInfo
    ToProviderTools() []ToolDefinition
}
```

### Builder (Fluent API)
```go
tool.New("weather").
    Description("Get weather for a location").
    Param("location", "string", "City name", true).
    Action(func(ctx context.Context, args map[string]any) (any, error) {
        return getWeather(args["location"].(string)), nil
    })
```

### Contract
- Tool names MUST be unique in registry
- Execute MUST be thread-safe
- Errors MUST be returned, not panics
- Parameters MUST be validated against schema

---

## Component: Skill

### Purpose
Bundle of tools + instructions for specific capabilities

### Interface
```go
type Skill interface {
    Name() string
    Description() string
    Tools() []Tool
    Instructions() string
    SlashCommand() *SlashCommandDefinition  // optional, nil if not a slash command
}
```

### Builder
```go
skill.New("calculator").
    Description("Math operations").
    WithTool(addTool).
    WithTool(subtractTool).
    WithInstruction("Always show your work").
    AsSlashCommand("calc", "Perform calculations").
    WithSlashOption("expression", "Math expression", OptionTypeString, true)
```

### Contract
- Skills can have zero or more tools
- Instructions are appended to system prompt
- SlashCommand is optional for Discord/Slack integration

---

## Component: Memory

### Purpose
Persist conversation history and context

### Interface
```go
type Memory interface {
    Add(ctx context.Context, userID, sessionID string, entry Entry) error
    Get(ctx context.Context, userID, sessionID string, limit int) ([]Entry, error)
    Search(ctx context.Context, userID string, query string, limit int) ([]Entry, error)
    Clear(ctx context.Context, userID, sessionID string) error
}

type Entry struct {
    ID        string
    Role      string
    Content   string
    Metadata  map[string]any
    Timestamp time.Time
}
```

### Backend Interface
```go
type MemoryBackend interface {
    Connect(ctx context.Context) error
    Close() error
    Memory() Memory
}
```

### Implementations (v1)
- [ ] InMemory - `memory/inmemory` (default, for testing)
- [ ] SQLite - `memory/sqlite` (local persistence)
- [ ] MongoDB - `memory/mongodb` (production)

### Contract
- Backends MUST be thread-safe
- Get MUST return entries in chronological order (oldest first)
- Search is optional (return empty if not supported)

---

## Component: HTTP Handler

### Purpose
Expose agent via HTTP for any web framework

### Interface
```go
type Handler interface {
    http.Handler
    
    // Endpoints:
    // POST /run - run agent
    // POST /stream - SSE streaming
    // GET /tools - list tools
    // POST /tools - register tool
    // GET /health - health check
}
```

### Factory
```go
func NewHandler(agent Agent, opts ...HandlerOption) Handler

// Options
func WithPrefix(prefix string) HandlerOption
func WithMiddleware(middleware ...Middleware) HandlerOption
```

### Framework Adapters
```go
// Gin
r.POST("/api/run", adapters.GinHandler(agent))

// Echo  
e.POST("/api/run", adapters.EchoHandler(agent))

// Standard
http.Handle("/api/", http.NewHandler(agent))
```

### Contract
- Handler MUST implement standard http.Handler
- Handler MUST support CORS
- Handler MUST handle JSON request/response
- Streaming MUST use SSE format

---

## Component: Compaction

### Purpose
Manage memory size by summarizing/archiving old entries

### Interface
```go
type Compactor interface {
    Compact(ctx context.Context, userID, sessionID string, opts CompactOptions) (*CompactStats, error)
}

type CompactOptions struct {
    MaxEntries  int
    MaxAge      time.Duration
    Strategy    CompactStrategy  // truncate, summarize, archive
    DryRun      bool
}

type CompactStats struct {
    EntriesBefore  int
    EntriesAfter   int
    BytesSaved     int64
    Duration       time.Duration
}
```

### Contract
- Compact MUST be safe to run on live data
- Summarize strategy requires LLM provider (optional)
- Archive strategy moves to separate table/collection

---

## File Structure

```
rl-agent/
├── agent/
│   ├── agent.go          # Agent interface + implementation
│   └── options.go        # Functional options
├── provider/
│   ├── provider.go       # Provider interface + types
│   ├── openai/
│   ├── anthropic/
│   ├── gemini/
│   └── zai/
├── tool/
│   ├── tool.go           # Tool interface + registry
│   └── builder.go        # Fluent builder
├── skill/
│   ├── skill.go          # Skill interface + builder
│   └── slash.go          # Slash command types
├── memory/
│   ├── memory.go         # Memory interface + types
│   ├── inmemory/
│   ├── sqlite/
│   └── mongodb/
├── http/
│   ├── handler.go        # HTTP handler
│   └── adapters/         # Framework adapters
├── examples/
│   ├── basic/
│   ├── streaming/
│   └── web-demo/
├── go.mod
└── README.md
```

---

## Implementation Priority

### Phase 1: Core (Week 1)
1. [ ] Provider interface + types
2. [ ] OpenAI provider implementation
3. [ ] Agent interface + implementation
4. [ ] Tool interface + registry
5. [ ] Basic example

### Phase 2: Extend (Week 2)
1. [ ] Anthropic, Gemini, Zai providers
2. [ ] Skill interface + builder
3. [ ] Memory interface + InMemory
4. [ ] Streaming example

### Phase 3: Production (Week 3)
1. [ ] SQLite memory backend
2. [ ] HTTP handler + adapters
3. [ ] Web demo
4. [ ] Compaction

---

## Testing Requirements

- All interfaces MUST have mock implementations
- Provider tests MUST use recorded HTTP fixtures
- Agent tests MUST cover tool calling loops
- Memory tests MUST be backend-agnostic
- HTTP tests MUST cover all endpoints

---

## Performance Requirements

- Provider.Complete latency: < 5s for typical requests
- Agent.Run: scales linearly with steps
- Memory.Get: < 10ms for 100 entries
- Zero allocations in hot paths where possible

---

## Security Requirements

- API keys MUST NOT be logged
- Provider requests MUST use TLS
- Memory backends MUST sanitize input
- HTTP handler MUST rate limit by default
