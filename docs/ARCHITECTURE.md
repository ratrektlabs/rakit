# rl-agent Architecture Design (v1)

> A Go-based agent framework with pluggable protocols, providers, and storage.

## Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                      RL-AGENT FRAMEWORK (v1)                        │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌───────────────────────────────────────────────────────────────┐ │
│  │                    PROTOCOL LAYER                             │ │
│  │                                                               │ │
│  │  ┌─────────────────────┐  ┌─────────────────────┐            │ │
│  │  │ AG-UI               │  │ AI SDK              │            │ │
│  │  │ (CopilotKit)        │  │ (Vercel)            │            │ │
│  │  │                     │  │                     │            │ │
│  │  │ • Run lifecycle     │  │ • start/finish      │            │ │
│  │  │ • Text streaming    │  │ • text-delta        │            │ │
│  │  │ • Tool calls        │  │ • tool-call         │            │ │
│  │  │ • State sync        │  │ • tool-result       │            │ │
│  │  │ • Reasoning         │  │                     │            │ │
│  │  └─────────────────────┘  └─────────────────────┘            │ │
│  │                                                               │ │
│  │  Registry with content negotiation and protocol conversion    │ │
│  └───────────────────────────────────────────────────────────────┘ │
│                              │                                      │
│                              ▼                                      │
│  ┌───────────────────────────────────────────────────────────────┐ │
│  │                    PROVIDER LAYER                             │ │
│  │                                                               │ │
│  │  ┌─────────────────────┐  ┌─────────────────────┐            │ │
│  │  │ OpenAI              │  │ Gemini              │            │ │
│  │  │ • GPT-5.4           │  │ • Gemini 3.1 Pro    │            │ │
│  │  │ • GPT-5.4 Mini      │  │ • Gemini 3.1 Flash  │            │ │
│  │  │ • GPT-5.4 Nano      │  │   Lite              │            │ │
│  │  └─────────────────────┘  └─────────────────────┘            │ │
│  └───────────────────────────────────────────────────────────────┘ │
│                              │                                      │
│                              ▼                                      │
│  ┌───────────────────────────────────────────────────────────────┐ │
│  │                    AGENT RUNTIME                             │ │
│  │                                                               │ │
│  │  type Agent struct {                                          │ │
│  │    ID       string                                           │ │
│  │    Provider provider.Provider                                 │ │
│  │    Protocol protocol.Protocol                                 │ │
│  │    Tools    *tool.Registry                                    │ │
│  │    Skills   *skill.Registry                                     │ │
│  │    Store    metadata.Store    // sessions, tools, memory      │ │
│  │    FS       blob.BlobStore    // virtual workspace            │ │
│  │  }                                                            │ │
│  │                                                               │ │
│  │  func (a *Agent) Run(ctx, input) (<-chan Event, error)       │ │
│  │  func (a *Agent) RunWithProtocol(ctx, input, p Protocol)     │ │
│  └───────────────────────────────────────────────────────────────┘ │
│                              │                                      │
│                              ▼                                      │
│  ┌───────────────────────────────────────────────────────────────┐ │
│  │                    SKILL SYSTEM (3-Layer)                    │ │
│  │                                                               │ │
│  │  L1 Registration  →  metadata store (name, description)      │ │
│  │  L2 Prompt         →  metadata store (full YAML definition)  │ │
│  │  L3 Resources      →  blob store (scripts, templates, files) │ │
│  └───────────────────────────────────────────────────────────────┘ │
│                              │                                      │
│                              ▼                                      │
│  ┌───────────────────────────────────────────────────────────────┐ │
│  │                    STORAGE LAYER                             │ │
│  │                                                               │ │
│  │  ┌───────────────────────────┐  ┌─────────────────────────┐ │ │
│  │  │ Metadata Store            │  │ Blob Store              │ │ │
│  │  │                           │  │ (Virtual Workspace)     │ │ │
│  │  │ ┌─────────┐ ┌──────────┐ │  │                         │ │ │
│  │  │ │Firestore│ │ MongoDB  │ │  │ ┌─────┐ ┌─────────────┐ │ │ │
│  │  │ └─────────┘ └──────────┘ │  │ │ S3  │ │ Firebase    │ │ │ │
│  │  │                           │  │ │     │ │ Storage     │ │ │ │
│  │  │ Stores:                   │  │ └─────┘ └─────────────┘ │ │ │
│  │  │ • Sessions (messages)     │  │                         │ │ │
│  │  │ • Tool definitions        │  │ Stores:                 │ │ │
│  │  │ • Memory (key-value)      │  │ • Agent files           │ │ │
│  │  └───────────────────────────┘  │ • Generated artifacts   │ │ │
│  │                                 └─────────────────────────┘ │ │
│  └───────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Protocol Layer

### Interface

```go
package protocol

import (
    "context"
    "io"
)

// Event represents a generic agent event
type Event interface {
    Type() EventType
}

type EventType string

const (
    // Lifecycle
    EventRunStarted  EventType = "run-started"
    EventRunFinished EventType = "run-finished"
    EventRunError    EventType = "run-error"

    // Text streaming
    EventTextStart   EventType = "text-start"
    EventTextDelta   EventType = "text-delta"
    EventTextEnd     EventType = "text-end"

    // Tool calls
    EventToolCallStart EventType = "tool-call-start"
    EventToolCallArgs  EventType = "tool-call-args"
    EventToolCallEnd   EventType = "tool-call-end"
    EventToolResult    EventType = "tool-result"

    // State
    EventStateSnapshot EventType = "state-snapshot"
    EventStateDelta    EventType = "state-delta"

    // Thinking/reasoning
    EventThinking EventType = "thinking"

    // Terminal
    EventError EventType = "error"
    EventDone  EventType = "done"
)

// Protocol interface — implement this for new protocols
type Protocol interface {
    // Name returns the protocol identifier
    Name() string

    // ContentType returns the HTTP content type
    ContentType() string

    // Encode converts an internal event to wire format
    Encode(w io.Writer, event Event) error

    // EncodeStream encodes a stream of events
    EncodeStream(ctx context.Context, w io.Writer, events <-chan Event) error

    // Decode converts wire format to an internal event
    Decode(r io.Reader) (Event, error)

    // DecodeStream decodes a stream of events
    DecodeStream(ctx context.Context, r io.Reader) (<-chan Event, error)
}
```

### Registry

```go
package protocol

// Registry manages protocol registration and negotiation
type Registry struct {
    protocols map[string]Protocol
    default   Protocol
}

func NewRegistry() *Registry {
    return &Registry{
        protocols: make(map[string]Protocol),
    }
}

func (r *Registry) Register(p Protocol) {
    r.protocols[p.Name()] = p
}

func (r *Registry) Get(name string) Protocol {
    return r.protocols[name]
}

func (r *Registry) SetDefault(p Protocol) {
    r.default = p
}

func (r *Registry) Default() Protocol {
    return r.default
}

// Negotiate selects protocol from Accept header or content type
func (r *Registry) Negotiate(accept string) Protocol {
    // Parse Accept header:
    // "text/vnd.ag-ui"        → AG-UI protocol
    // "text/vnd.ai-sdk"       → AI SDK protocol
    // "text/event-stream"     → default
}
```

### AG-UI Protocol (CopilotKit)

AG-UI is an open, event-based protocol for connecting AI agents to user-facing apps. Transport-agnostic (SSE, WebSocket, etc).

```go
package agui

type AGUIProtocol struct{}

func (p *AGUIProtocol) Name() string { return "ag-ui" }
func (p *AGUIProtocol) ContentType() string {
    return "text/event-stream"
}

func (p *AGUIProtocol) Encode(w io.Writer, event Event) error {
    switch e := event.(type) {
    case *RunStartedEvent:
        // event: RunStarted
        // data: {"type":"RunStarted","threadId":"...","runId":"..."}
    case *TextMessageStartEvent:
        // event: TextMessageStart
        // data: {"type":"TextMessageStart","messageId":"...","role":"assistant"}
    case *TextMessageContentEvent:
        // event: TextMessageContent
        // data: {"type":"TextMessageContent","messageId":"...","delta":"..."}
    case *TextMessageEndEvent:
        // event: TextMessageEnd
        // data: {"type":"TextMessageEnd","messageId":"..."}
    case *ToolCallStartEvent:
        // event: ToolCallStart
        // data: {"type":"ToolCallStart","toolCallId":"...","toolCallName":"..."}
    case *ToolCallArgsEvent:
        // event: ToolCallArgs
        // data: {"type":"ToolCallArgs","toolCallId":"...","delta":"..."}
    case *ToolCallEndEvent:
        // event: ToolCallEnd
        // data: {"type":"ToolCallEnd","toolCallId":"..."}
    case *StateSnapshotEvent:
        // event: StateSnapshot
        // data: {"type":"StateSnapshot","snapshot":{...}}
    case *StateDeltaEvent:
        // event: StateDelta
        // data: {"type":"StateDelta","delta":[...]}  // JSON Patch RFC 6902
    case *RunFinishedEvent:
        // event: RunFinished
        // data: {"type":"RunFinished","threadId":"...","runId":"..."}
    case *RunErrorEvent:
        // event: RunError
        // data: {"type":"RunError","message":"...","code":"..."}
    }
    return nil
}
```

### AI SDK Protocol (Vercel)

The Vercel AI SDK uses a simple SSE-based streaming format.

```go
package aisdk

type AISDKProtocol struct{}

func (p *AISDKProtocol) Name() string { return "ai-sdk" }
func (p *AISDKProtocol) ContentType() string {
    return "text/plain; charset=utf-8"
}

func (p *AISDKProtocol) Encode(w io.Writer, event Event) error {
    switch e := event.(type) {
    case *TextStartEvent:
        // data: {"type":"start","messageId":"..."}
    case *TextDeltaEvent:
        // data: {"type":"text-delta","textDelta":"..."}
    case *ToolCallEvent:
        // data: {"type":"tool-call","toolCallId":"...","toolName":"...","args":"..."}
    case *ToolResultEvent:
        // data: {"type":"tool-result","toolCallId":"...","result":"..."}
    case *DoneEvent:
        // data: {"type":"finish"}
    }
    return nil
}
```

### Protocol Comparison

| Feature         | AG-UI                    | AI SDK                 |
|-----------------|--------------------------|------------------------|
| Source          | CopilotKit (open spec)   | Vercel AI SDK          |
| Transport       | SSE / WebSocket / any    | SSE                    |
| Event types     | 20+ (lifecycle, state)   | ~5 (simple streaming)  |
| State sync      | Snapshot + JSON Patch    | No                     |
| Tool streaming  | Start → Args → End       | Single tool-call event |
| Reasoning       | Yes                      | No                     |
| Best for        | Rich agent UIs           | Simple chat streaming  |

---

## Provider Layer

### Interface

```go
package provider

import "context"

// Message represents a chat message
type Message struct {
    Role      string
    Content   string
    ToolCalls []ToolCall
}

type ToolCall struct {
    ID        string
    Name      string
    Arguments string
}

// Provider interface for LLM backends
type Provider interface {
    // Name returns the provider identifier
    Name() string

    // Model returns the configured model ID
    Model() string

    // Models returns all available model IDs for this provider
    Models() []string

    // Stream generates a streaming response
    Stream(ctx context.Context, req *Request) (<-chan Event, error)

    // Generate generates a non-streaming response
    Generate(ctx context.Context, req *Request) (*Response, error)
}

type Request struct {
    Model       string
    Messages    []Message
    Tools       []Tool
    MaxTokens   int
    Temperature float64
}

type Tool struct {
    Name        string
    Description string
    Parameters  any // JSON Schema
}

type Response struct {
    Content   string
    ToolCalls []ToolCall
    Usage     Usage
}

type Usage struct {
    InputTokens  int
    OutputTokens int
}
```

### OpenAI Provider

```go
package openai

type Provider struct {
    apiKey  string
    model   string
    client  *http.Client
}

func New(model, apiKey string) *Provider {
    return &Provider{
        apiKey: apiKey,
        model:  model,
        client: http.DefaultClient,
    }
}

func (p *Provider) Name() string  { return "openai" }
func (p *Provider) Model() string { return p.model }

func (p *Provider) Models() []string {
    return []string{"gpt-5.4", "gpt-5.4-mini", "gpt-5.4-nano"}
}

func (p *Provider) Stream(ctx context.Context, req *Request) (<-chan provider.Event, error) {
    // POST https://api.openai.com/v1/chat/completions
    // with stream: true
}

func (p *Provider) Generate(ctx context.Context, req *Request) (*provider.Response, error) {
    // POST https://api.openai.com/v1/chat/completions
    // with stream: false
}
```

### Gemini Provider

```go
package gemini

type Provider struct {
    apiKey string
    model  string
    client *genai.Client // Google's generative AI SDK
}

func New(model, apiKey string) *Provider {
    // Initialize using google.golang.org/genai
}

func (p *Provider) Name() string  { return "gemini" }
func (p *Provider) Model() string { return p.model }

func (p *Provider) Models() []string {
    return []string{"gemini-3.1-pro-preview", "gemini-3.1-flash-lite-preview"}
}

func (p *Provider) Stream(ctx context.Context, req *Request) (<-chan provider.Event, error) {
    // Use genai client with streaming
}

func (p *Provider) Generate(ctx context.Context, req *Request) (*provider.Response, error) {
    // Use genai client without streaming
}
```

---

## Agent Runtime

### Core Structure

```go
package agent

import (
    "context"

    "github.com/ratrektlabs/rl-agent/protocol"
    "github.com/ratrektlabs/rl-agent/provider"
    "github.com/ratrektlabs/rl-agent/tool"
    "github.com/ratrektlabs/rl-agent/skill"
    "github.com/ratrektlabs/rl-agent/storage/metadata"
    "github.com/ratrektlabs/rl-agent/storage/blob"
)

type Agent struct {
    ID       string
    Provider provider.Provider
    Protocol protocol.Protocol
    Tools    *tool.Registry
    Skills   *skill.Registry
    Store    metadata.Store
    FS       blob.BlobStore

    // Hooks for observability
    hooks []Hook
}

type Hook interface {
    OnEvent(ctx context.Context, e protocol.Event) error
    OnError(ctx context.Context, err error) error
}
```

### Options

```go
package agent

type Option func(*Agent)

func WithProvider(p provider.Provider) Option {
    return func(a *Agent) { a.Provider = p }
}

func WithProtocol(p protocol.Protocol) Option {
    return func(a *Agent) { a.Protocol = p }
}

func WithStore(s metadata.Store) Option {
    return func(a *Agent) {
        a.Store = s
        a.Skills = skill.NewRegistry(s)
    }
}

func WithFS(fs blob.BlobStore) Option {
    return func(a *Agent) { a.FS = fs }
}

func WithTools(tools ...tool.Tool) Option {
    return func(a *Agent) {
        for _, t := range tools {
            a.Tools.Register(t)
        }
    }
}

func WithHooks(hooks ...Hook) Option {
    return func(a *Agent) { a.hooks = append(a.hooks, hooks...) }
}

func New(opts ...Option) *Agent {
    a := &Agent{
        ID:    generateID(),
        Tools: tool.NewRegistry(),
        hooks: make([]Hook, 0),
    }
    for _, opt := range opts {
        opt(a)
    }
    return a
}
```

### Runner

```go
package agent

func (a *Agent) Run(ctx context.Context, input string) (<-chan protocol.Event, error) {
    return a.RunWithProtocol(ctx, input, a.Protocol)
}

func (a *Agent) RunWithProtocol(
    ctx context.Context,
    input string,
    p protocol.Protocol,
) (<-chan protocol.Event, error) {
    events := make(chan protocol.Event, 100)

    go func() {
        defer close(events)

        // Build request (uses the model the user selected)
        req := &provider.Request{
            Model:    a.Provider.Model(),
            Messages: []provider.Message{{Role: "user", Content: input}},
            Tools:    a.Tools.Schema(),
        }

        // Stream from provider
        stream, err := a.Provider.Stream(ctx, req)
        if err != nil {
            events <- &protocol.ErrorEvent{Error: err}
            return
        }

        for event := range stream {
            // Apply hooks
            for _, h := range a.hooks {
                if err := h.OnEvent(ctx, event); err != nil {
                    events <- &protocol.ErrorEvent{Error: err}
                }
            }

            // Forward to output
            events <- event
        }
    }()

    return events, nil
}
```

---

## Tool System

### Interface

```go
package tool

import "context"

// Tool represents an executable capability
type Tool interface {
    // Name returns the tool identifier
    Name() string

    // Description for the LLM
    Description() string

    // Parameters returns the JSON Schema for inputs
    Parameters() any

    // Execute runs the tool
    Execute(ctx context.Context, input map[string]any) (any, error)
}

// Registry manages available tools
type Registry struct {
    tools map[string]Tool
}

func NewRegistry() *Registry {
    return &Registry{
        tools: make(map[string]Tool),
    }
}

func (r *Registry) Register(t Tool) {
    r.tools[t.Name()] = t
}

func (r *Registry) Get(name string) Tool {
    return r.tools[name]
}

// Schema returns all tool definitions for provider requests
func (r *Registry) Schema() []provider.Tool {
    var schemas []provider.Tool
    for _, t := range r.tools {
        schemas = append(schemas, provider.Tool{
            Name:        t.Name(),
            Description: t.Description(),
            Parameters:  t.Parameters(),
        })
    }
    return schemas
}
```

---

## Skill System (3-Layer)

Inspired by Claude's skill design — lazy loading at each layer to keep overhead minimal.

```
┌─────────────────────────────────────────────────────────┐
│                    SKILL LIFECYCLE                       │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  L1 REGISTRATION (lightweight)                          │
│  ├── Name + description only                            │
│  ├── Used to decide which skills to activate            │
│  └── Stored in: metadata store                          │
│              │                                          │
│              ▼ skill selected                            │
│  L2 PROMPT (full definition)                            │
│  ├── Complete YAML: tools, instructions, config         │
│  ├── Loaded when skill is activated for a run           │
│  └── Stored in: metadata store                          │
│              │                                          │
│              ▼ skill executing                           │
│  L3 RESOURCES (assets)                                  │
│  ├── Scripts, templates, files the skill needs          │
│  ├── Loaded on-demand during execution                  │
│  └── Stored in: blob store (agent workspace)            │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

### L1 — Registration

Lightweight index for skill selection. Only name and description are loaded.

```go
package skill

// Entry is the lightweight L1 registration record
type Entry struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    Version     string `json:"version"`
    Enabled     bool   `json:"enabled"`
}

// Registry manages L1 skill entries (backed by metadata store)
type Registry struct {
    store metadata.Store
}

func NewRegistry(store metadata.Store) *Registry {
    return &Registry{store: store}
}

// List returns all registered skill entries (L1 only)
func (r *Registry) List(ctx context.Context) ([]*Entry, error) { ... }

// Register adds a new skill definition (stores L1 + L2)
func (r *Registry) Register(ctx context.Context, def *Definition) error { ... }

// Unregister removes a skill
func (r *Registry) Unregister(ctx context.Context, name string) error { ... }

// Enable/Disable toggles a skill without removing it
func (r *Registry) Enable(ctx context.Context, name string) error { ... }
func (r *Registry) Disable(ctx context.Context, name string) error { ... }
```

### L2 — Prompt (Full Definition)

Full YAML skill definition loaded when a skill is activated.

```go
package skill

import (
    "github.com/ratrektlabs/rl-agent/tool"
)

// Definition is the full L2 skill definition
type Definition struct {
    Name        string         `json:"name"`
    Description string         `json:"description"`
    Version     string         `json:"version"`
    Instructions string        `json:"instructions"` // System prompt additions
    Tools       []ToolDef      `json:"tools"`
    Config      map[string]any `json:"config"`
    Resources   []Resource     `json:"resources"`    // L3 references
}

type ToolDef struct {
    Name        string         `json:"name"`
    Description string         `json:"description"`
    Parameters  any            `json:"parameters"`   // JSON Schema
    Handler     string         `json:"handler"`       // "http", "script"
    // HTTP handler config
    Endpoint     string            `json:"endpoint,omitempty"`
    Headers      map[string]string `json:"headers,omitempty"`
    InputMapping map[string]string `json:"input_mapping,omitempty"`
    // Script handler config
    ScriptPath   string            `json:"script_path,omitempty"` // L3 blob path
}

type Resource struct {
    Name string `json:"name"` // e.g. "prompt_template", "search_script"
    Path string `json:"path"` // blob store path, e.g. "skills/weather/script.py"
    Type string `json:"type"` // "script", "template", "file"
}

// Loader fetches L2 definitions from metadata store
type Loader struct {
    store metadata.Store
}

func NewLoader(store metadata.Store) *Loader {
    return &Loader{store: store}
}

// Load fetches the full definition for an active skill
func (l *Loader) Load(ctx context.Context, name string) (*Definition, error) { ... }

// LoadAll fetches definitions for all enabled skills
func (l *Loader) LoadEnabled(ctx context.Context) ([]*Definition, error) { ... }

// ToTools converts a Definition's ToolDefs into executable tool.Tools
func (l *Loader) ToTools(def *Definition) ([]tool.Tool, error) { ... }
```

### L3 — Resources

Scripts, templates, and files loaded on-demand from blob store during execution.

```go
package skill

import (
    "context"
    "github.com/ratrektlabs/rl-agent/storage/blob"
)

// ResourceManager loads L3 resources from blob store
type ResourceManager struct {
    fs blob.BlobStore
}

func NewResourceManager(fs blob.BlobStore) *ResourceManager {
    return &ResourceManager{fs: fs}
}

// Load reads a resource file from the agent workspace
func (r *ResourceManager) Load(ctx context.Context, path string) ([]byte, error) {
    return r.fs.Read(ctx, path)
}

// LoadTemplate reads and returns a template resource
func (r *ResourceManager) LoadTemplate(ctx context.Context, path string) (string, error) { ... }

// LoadScript reads a script resource for execution
func (r *ResourceManager) LoadScript(ctx context.Context, path string) ([]byte, error) { ... }

// Store saves a resource to the agent workspace
func (r *ResourceManager) Store(ctx context.Context, path string, data []byte) error {
    return r.fs.Write(ctx, path, data)
}
```

### Skill YAML Format

```yaml
# Stored in metadata store as the L2 definition
name: weather
description: Get weather information for any location
version: "1.0"

instructions: |
  You have access to weather data. Use the get_weather tool
  to fetch current conditions for any location.

tools:
  - name: get_weather
    description: Get current weather for a location
    parameters:
      type: object
      properties:
        location:
          type: string
          description: City name or coordinates
      required:
        - location
    handler: http
    endpoint: https://api.weatherapi.com/v1/current.json
    headers:
      Authorization: "Bearer ${WEATHER_API_KEY}"
    input_mapping:
      location: "q"

resources:
  - name: weather_template
    path: skills/weather/response_template.txt
    type: template

config:
  units: celsius
  cache_ttl: 300
```

### HTTP Tool Handler

```go
package skill

import "context"

// HTTPTool is a tool that calls an HTTP endpoint
type HTTPTool struct {
    name        string
    description string
    parameters  any
    endpoint    string
    headers     map[string]string
    inputMap    map[string]string
}

func (t *HTTPTool) Name() string        { return t.name }
func (t *HTTPTool) Description() string { return t.description }
func (t *HTTPTool) Parameters() any     { return t.parameters }

func (t *HTTPTool) Execute(ctx context.Context, input map[string]any) (any, error) {
    // Build HTTP request from input_mapping
    // Call endpoint
    // Return response
}

// ScriptTool is a tool that executes a script from blob store
type ScriptTool struct {
    name        string
    description string
    parameters  any
    scriptPath  string
    resources   *ResourceManager
}

func (t *ScriptTool) Execute(ctx context.Context, input map[string]any) (any, error) {
    // Load script from blob store (L3)
    script, err := t.resources.LoadScript(ctx, t.scriptPath)
    // Execute script with input
}
```

---

## Storage Layer

### Metadata Store

Unified interface for sessions, tools, and memory. Ship with Firestore and MongoDB adapters.

```go
package metadata

import "context"

// Session represents a conversation session
type Session struct {
    ID        string
    AgentID   string
    Messages  []Message
    State     map[string]any
    CreatedAt int64
    UpdatedAt int64
}

// Message represents a single message in a session
type Message struct {
    ID        string
    Role      string
    Content   string
    ToolCalls []ToolCallRecord
    CreatedAt int64
}

// ToolCallRecord is a persisted tool call
type ToolCallRecord struct {
    ID        string
    Name      string
    Arguments string
    Result    string
    Status    string // "pending", "completed", "failed"
}

// ToolDef is a persisted tool definition
type ToolDef struct {
    ID          string
    AgentID     string
    Name        string
    Description string
    Parameters  any
    CreatedAt   int64
}

// Store interface — implement for new backends
type Store interface {
    // Sessions
    CreateSession(ctx context.Context, agentID string) (*Session, error)
    GetSession(ctx context.Context, id string) (*Session, error)
    UpdateSession(ctx context.Context, s *Session) error
    DeleteSession(ctx context.Context, id string) error

    // Tools (persisted definitions)
    SaveTool(ctx context.Context, tool *ToolDef) error
    GetTool(ctx context.Context, name string) (*ToolDef, error)
    ListTools(ctx context.Context, agentID string) ([]*ToolDef, error)
    DeleteTool(ctx context.Context, name string) error

    // Skills (persisted definitions + L1 registry)
    SaveSkill(ctx context.Context, def *SkillDef) error
    GetSkill(ctx context.Context, name string) (*SkillDef, error)
    ListSkills(ctx context.Context) ([]*SkillEntry, error) // L1: name + description only
    DeleteSkill(ctx context.Context, name string) error

    // Memory (key-value)
    Set(ctx context.Context, key string, value []byte) error
    Get(ctx context.Context, key string) ([]byte, error)
    Delete(ctx context.Context, key string) error
    List(ctx context.Context, prefix string) ([]string, error)
}

// SkillEntry is the lightweight L1 record (name + description)
type SkillEntry struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    Version     string `json:"version"`
    Enabled     bool   `json:"enabled"`
}

// SkillDef is the full L2 skill definition stored in metadata
type SkillDef struct {
    Name         string         `json:"name"`
    Description  string         `json:"description"`
    Version      string         `json:"version"`
    Instructions string         `json:"instructions"`
    Tools        []any          `json:"tools"`        // tool definitions
    Config       map[string]any `json:"config"`
    Resources    []any          `json:"resources"`     // L3 references (blob paths)
    Enabled      bool           `json:"enabled"`
}
```

#### Firestore Adapter

```go
package firestore

type Store struct {
    client *firestore.Client
}

func New(projectID string) (*Store, error) {
    client, err := firestore.NewClient(context.Background(), projectID)
    if err != nil {
        return nil, err
    }
    return &Store{client: client}, nil
}

// Collections: sessions, tools, memory
// Sessions → {sessionID} document with messages subcollection
// Tools → {toolName} document
// Memory → {key} document with value field
```

#### MongoDB Adapter

```go
package mongo

type Store struct {
    client   *mongo.Client
    database string
}

func New(uri, database string) (*Store, error) {
    client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(uri))
    if err != nil {
        return nil, err
    }
    return &Store{client: client, database: database}, nil
}

// Collections: sessions, tools, memory
// Sessions → documents with embedded messages
// Tools → documents indexed by name
// Memory → documents with key/value pairs
```

### Blob Store

Virtual filesystem for agent workspaces. Ship with S3 and Firebase Storage adapters.

```go
package blob

import "context"

// BlobStore interface for agent virtual workspace
type BlobStore interface {
    Read(ctx context.Context, path string) ([]byte, error)
    Write(ctx context.Context, path string, data []byte) error
    Delete(ctx context.Context, path string) error
    List(ctx context.Context, prefix string) ([]string, error)
}
```

#### S3 Adapter

```go
package s3

type BlobStore struct {
    client *s3.Client
    bucket string
    prefix string
}

func New(bucket, prefix string, opts ...Option) (*BlobStore, error) {
    // Initialize AWS S3 client
    // Works with AWS S3, MinIO, Cloudflare R2, etc.
}

func (s *BlobStore) Read(ctx context.Context, path string) ([]byte, error) {
    // GET object from s3://bucket/prefix/path
}

func (s *BlobStore) Write(ctx context.Context, path string, data []byte) error {
    // PUT object to s3://bucket/prefix/path
}
```

#### Firebase Storage Adapter

```go
package firebase

type BlobStore struct {
    client *storage.Client
    bucket string
}

func New(bucket string) (*BlobStore, error) {
    // Initialize Firebase Storage client
}

func (s *BlobStore) Read(ctx context.Context, path string) ([]byte, error) {
    // Read from Firebase Storage bucket
}

func (s *BlobStore) Write(ctx context.Context, path string, data []byte) error {
    // Write to Firebase Storage bucket
}
```

---

## HTTP Server Example

```go
package main

import (
    "context"
    "net/http"

    "github.com/ratrektlabs/rl-agent/agent"
    "github.com/ratrektlabs/rl-agent/protocol"
    "github.com/ratrektlabs/rl-agent/protocol/agui"
    "github.com/ratrektlabs/rl-agent/protocol/aisdk"
    "github.com/ratrektlabs/rl-agent/provider/gemini"
    "github.com/ratrektlabs/rl-agent/storage/metadata/firestore"
    "github.com/ratrektlabs/rl-agent/storage/blob/s3"
)

func main() {
    // Register protocols
    registry := protocol.NewRegistry()
    registry.Register(agui.New())
    registry.Register(aisdk.New())
    registry.SetDefault(aisdk.New())

    // Create agent with storage
    store, _ := firestore.New("my-project")
    fs, _ := s3.New("my-agent-workspace", "agents/")

    a := agent.New(
        agent.WithProvider(gemini.New("gemini-3.1-pro-preview", apiKey)),
        agent.WithProtocol(aisdk.New()),
        agent.WithStore(store),
        agent.WithFS(fs),
    )

    // Register a skill (stores L1 + L2 in metadata, resources in blob)
    skillDef := &skill.Definition{
        Name:         "weather",
        Description:  "Get weather information for any location",
        Instructions: "Use the get_weather tool to fetch current conditions.",
        Tools: []skill.ToolDef{{
            Name: "get_weather", Description: "Get weather for a location",
            Handler: "http", Endpoint: "https://api.weatherapi.com/v1/current.json",
        }},
    }
    a.Skills.Register(ctx, skillDef)

    // HTTP handler with protocol negotiation
    http.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
        proto := registry.Negotiate(r.Header.Get("Accept"))
        if proto == nil {
            proto = registry.Default()
        }

        w.Header().Set("Content-Type", proto.ContentType())

        var req struct {
            Message string `json:"message"`
        }
        json.NewDecoder(r.Body).Decode(&req)

        events, err := a.RunWithProtocol(r.Context(), req.Message, proto)
        if err != nil {
            http.Error(w, err.Error(), 500)
            return
        }

        proto.EncodeStream(r.Context(), w, events)
    })

    http.ListenAndServe(":8080", nil)
}
```

---

## Package Structure (v1)

```
github.com/ratrektlabs/rl-agent
├── go.mod
├── go.sum
├── agent/
│   ├── agent.go            # Agent struct + options
│   ├── runner.go           # Run / RunWithProtocol
│   └── hook.go             # Observability hooks
├── provider/
│   ├── provider.go         # Provider interface + types
│   ├── openai/
│   │   └── provider.go
│   └── gemini/
│       └── provider.go
├── protocol/
│   ├── protocol.go         # Protocol interface + event types
│   ├── registry.go         # Protocol registry + negotiation
│   ├── agui/
│   │   └── protocol.go     # AG-UI (CopilotKit)
│   └── aisdk/
│       └── protocol.go     # AI SDK (Vercel)
├── tool/
│   ├── tool.go             # Tool interface
│   └── registry.go         # Tool registry
├── skill/
│   ├── skill.go            # Skill interface, Entry, Definition types
│   ├── registry.go         # L1 Registry (backed by metadata store)
│   ├── loader.go           # L2 Loader (full definition from metadata)
│   ├── resources.go        # L3 ResourceManager (from blob store)
│   └── handlers.go         # HTTPTool, ScriptTool handlers
├── storage/
│   ├── metadata/
│   │   ├── metadata.go     # Store interface (sessions, tools, memory)
│   │   ├── firestore/
│   │   │   └── store.go
│   │   └── mongo/
│   │       └── store.go
│   └── blob/
│       ├── blob.go         # BlobStore interface
│       ├── s3/
│       │   └── store.go
│       └── firebase/
│           └── store.go
└── examples/
    ├── cloud-run/
    │   ├── main.go
    │   └── Dockerfile
    └── local/
        └── main.go
```

---

## Design Decisions

### Why protocol abstraction?
Different frontends expect different streaming formats. AG-UI for CopilotKit users, AI SDK for Vercel users. Abstracting the protocol lets any provider output in either format.

### Why separate provider and protocol?
- **Provider**: how we talk to LLMs (OpenAI, Gemini)
- **Protocol**: how we talk to clients (AG-UI, AI SDK)

This separation means any provider can output in any protocol format.

### Why 3-layer skill design?
- **L1 (Registration)**: Keep overhead minimal — only load name+description to decide which skills to use
- **L2 (Prompt)**: Load full definition only when a skill is activated for a run
- **L3 (Resources)**: Load scripts/templates on-demand from blob store during execution
- Skills are stored in metadata store, not filesystem — no local files required
- Scripts, templates, and other assets live in the agent workspace (blob store)

### Why unified metadata store?
Sessions, tools, and memory all need persistence with the same access patterns. A single Store interface keeps things simple and lets users pick one backend (Firestore or MongoDB) for everything.

### Why blob storage for virtual workspace?
Agents need a filesystem-like space for generated files, artifacts, and workspace data. S3 is universal (works with AWS, MinIO, Cloudflare R2). Firebase Storage covers the GCP ecosystem.
