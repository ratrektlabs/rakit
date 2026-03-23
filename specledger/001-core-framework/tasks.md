# Tasks: Core Framework (rl-agent v2)

**Branch**: `001-core-framework` | **Generated**: 2026-03-23 | **Plan**: [plan.md](./plan.md)

## Status Legend
- [ ] Not started
- [~] In progress
- [x] Complete
- [!] Blocked

---

## Phase 1: Provider Component (Days 1-2)

### P1.1 - Define Provider Types
- [ ] Create `provider/provider.go`
- [ ] Define `Provider` interface with Name(), Complete(), Stream(), Capabilities()
- [ ] Define `CompletionRequest` struct
- [ ] Define `CompletionResponse` struct
- [ ] Define `StreamEvent` struct and `StreamEventType`
- [ ] Define `Message` struct and `MessageRole`
- [ ] Define `ToolCall` struct
- [ ] Define `ProviderCapabilities` struct

### P1.2 - Stream Patterns & Errors
- [ ] Implement stream channel patterns in `provider/provider.go`
- [ ] Create error wrapping utilities with context

### P1.3 - OpenAI Complete
- [ ] Create `provider/openai/openai.go`
- [ ] Implement OpenAI client struct with API key configuration
- [ ] Implement `Complete()` using `net/http`
- [ ] Handle request/response JSON marshaling
- [ ] Implement error handling

### P1.4 - OpenAI Stream
- [ ] Implement `Stream()` with SSE parsing in `provider/openai/openai.go`
- [ ] Parse `data:` lines from SSE response
- [ ] Yield `StreamEvent` on channel
- [ ] Close channel on completion or error

### P1.5 - Provider Contract Tests
- [ ] Create `provider/provider_test.go`
- [ ] Create mock provider implementation
- [ ] Test interface compliance
- [ ] Test all Provider methods

### P1.6 - OpenAI Tests
- [ ] Create `provider/openai/openai_test.go`
- [ ] Record HTTP fixtures for tests
- [ ] Test Complete() with various responses
- [ ] Test Stream() event handling
- [ ] Test error cases

**Phase 1 Acceptance**:
- [ ] Provider interface compiles with all methods
- [ ] OpenAI Complete() returns valid responses
- [ ] OpenAI Stream() yields events and closes channel
- [ ] All tests pass with `go test ./provider/...`

---

## Phase 2: Tool Component (Days 2-3)

### T2.1 - Tool Interface
- [ ] Create `tool/tool.go`
- [ ] Define `Tool` interface with Name(), Description(), Parameters(), Execute()
- [ ] Define `ToolInfo` struct

### T2.2 - Tool Registry
- [ ] Define `ToolRegistry` interface in `tool/tool.go`
- [ ] Implement registry with `sync.Map` for thread-safety
- [ ] Implement `Register(tool Tool) error`
- [ ] Implement `Get(name string) (Tool, error)`
- [ ] Implement `List() []ToolInfo`
- [ ] Implement `ToProviderTools() []ToolDefinition`

### T2.3 - Fluent Builder
- [ ] Create `tool/builder.go`
- [ ] Implement `ToolBuilder` struct
- [ ] Implement `New(name)` constructor
- [ ] Implement `Description(desc)` method
- [ ] Implement `Param(name, typ, desc, required)` method
- [ ] Implement `Action(fn)` method
- [ ] Implement `Build()` method with validation

### T2.4 - Tool Definition Conversion
- [ ] Implement `ToolDefinition` conversion for provider tools in `tool/tool.go`

### T2.5 - Tool Tests
- [ ] Create `tool/tool_test.go`
- [ ] Test registry Register/Get/List operations
- [ ] Test builder with valid/invalid configurations
- [ ] Test validation logic
- [ ] Test concurrent registry access with `-race`

**Phase 2 Acceptance**:
- [ ] Tool interface with Name, Description, Parameters, Execute
- [ ] Registry Register/Get/List/ToProviderTools working
- [ ] Fluent builder generates valid tools
- [ ] Thread-safe registry passes concurrent tests

---

## Phase 3: Agent Component (Days 3-5)

### A3.1 - Agent Interface
- [ ] Create `agent/agent.go`
- [ ] Define `Agent` interface with Run(), Stream(), AddTool(), AddSkill()
- [ ] Define `RunResult` struct

### A3.2 - Agent Config
- [ ] Implement `AgentConfig` struct with defaults in `agent/agent.go`
- [ ] Set sensible defaults (MaxSteps, Temperature, etc.)

### A3.3 - Functional Options
- [ ] Create `agent/options.go`
- [ ] Implement `RunOption` type
- [ ] Implement `RunConfig` struct
- [ ] Implement `WithTools(tools ...Tool)` option
- [ ] Implement `WithSkills(skills ...Skill)` option
- [ ] Implement `WithMaxSteps(n int)` option
- [ ] Implement `WithSession(userID, sessionID string)` option

### A3.4 - Run Implementation
- [ ] Implement `Run()` with tool-calling loop in `agent/agent.go`
- [ ] Build request with system prompt + messages + tools
- [ ] Call provider.Complete
- [ ] Handle tool calls: execute tools, add results to messages
- [ ] Loop until no tool calls or MaxSteps reached

### A3.5 - Stream Implementation
- [ ] Implement `Stream()` in `agent/agent.go`
- [ ] Forward provider stream events
- [ ] Generate tool execution events
- [ ] Handle tool results in stream context

### A3.6 - Safety Controls
- [ ] Implement MaxSteps enforcement
- [ ] Implement context cancellation handling
- [ ] Ensure immediate stop on context cancel

### A3.7 - Agent Tests
- [ ] Create `agent/agent_test.go`
- [ ] Test Run loop with mock provider
- [ ] Test tool execution within loop
- [ ] Test MaxSteps limit enforcement
- [ ] Test context cancellation
- [ ] Test error handling

**Phase 3 Acceptance**:
- [ ] Agent.Run() executes complete tool-calling loops
- [ ] Agent.Stream() forwards provider events + tool events
- [ ] MaxSteps prevents infinite loops
- [ ] Context cancellation stops execution immediately

---

## Phase 4: Skill Component (Day 5)

### S4.1 - Skill Interface
- [ ] Create `skill/skill.go`
- [ ] Define `Skill` interface with Name(), Description(), Tools(), Instructions(), SlashCommand()
- [ ] Define `SlashCommandDefinition` struct

### S4.2 - Skill Builder
- [ ] Implement `SkillBuilder` in `skill/skill.go`
- [ ] Implement `New(name)` constructor
- [ ] Implement `Description(desc)` method
- [ ] Implement `WithTool(tool)` method
- [ ] Implement `WithInstruction(instruction)` method
- [ ] Implement `AsSlashCommand(name, desc)` method

### S4.3 - Slash Command Types
- [ ] Create `skill/slash.go`
- [ ] Define `SlashOption` struct
- [ ] Define `OptionType` constants
- [ ] Implement `WithSlashOption()` builder method

### S4.4 - Skill Tests
- [ ] Create `skill/skill_test.go`
- [ ] Test builder with multiple tools
- [ ] Test instruction integration
- [ ] Test slash command metadata

**Phase 4 Acceptance**:
- [ ] Skills bundle multiple tools
- [ ] Instructions integrate with system prompt
- [ ] Slash command metadata available

---

## Phase 5: Memory Component (Days 6-7)

### M5.1 - Memory Interface
- [ ] Create `memory/memory.go`
- [ ] Define `Memory` interface with Add(), Get(), Search(), Clear()
- [ ] Define `Entry` struct with ID, Role, Content, Metadata, Timestamp
- [ ] Define `MemoryBackend` interface with Connect(), Close(), Memory()

### M5.2 - InMemory Backend
- [ ] Create `memory/inmemory/inmemory.go`
- [ ] Implement thread-safe storage with `sync.RWMutex`
- [ ] Implement `Add()` method
- [ ] Implement `Get()` with chronological ordering
- [ ] Implement `Search()` (return empty for InMemory)
- [ ] Implement `Clear()` method

### M5.3 - SQLite Backend
- [ ] Create `memory/sqlite/sqlite.go`
- [ ] Implement schema creation
- [ ] Implement all Memory methods with SQL queries
- [ ] Implement Search with LIKE query

### M5.4 - Memory Interface Tests
- [ ] Create `memory/memory_test.go`
- [ ] Create backend-agnostic test suite
- [ ] Test Add/Get/Clear operations
- [ ] Test chronological ordering

### M5.5 - InMemory Tests
- [ ] Create `memory/inmemory/inmemory_test.go`
- [ ] Test concurrent access with `-race`
- [ ] Test thread-safety guarantees

**Phase 5 Acceptance**:
- [ ] Memory interface Add/Get/Search/Clear working
- [ ] InMemory backend thread-safe
- [ ] Get returns entries in chronological order
- [ ] Search returns empty if unsupported

---

## Phase 6: HTTP Handler (Days 8-9)

### H6.1 - Handler Interface
- [ ] Create `http/handler.go`
- [ ] Define `Handler` interface extending `http.Handler`
- [ ] Define request/response types

### H6.2 - Endpoints
- [ ] Implement `POST /run` endpoint
- [ ] Implement `POST /stream` with SSE
- [ ] Implement `GET /tools` endpoint
- [ ] Implement `POST /tools` endpoint
- [ ] Implement `GET /health` endpoint

### H6.3 - CORS & JSON
- [ ] Implement CORS support
- [ ] Implement JSON request parsing
- [ ] Implement JSON response writing
- [ ] Implement error response formatting

### H6.4 - Gin Adapter
- [ ] Create `http/adapters/gin.go`
- [ ] Implement `GinHandler(agent Agent)` function
- [ ] Delegate to standard handler

### H6.5 - Echo Adapter
- [ ] Create `http/adapters/echo.go`
- [ ] Implement `EchoHandler(agent Agent)` function
- [ ] Delegate to standard handler

### H6.6 - Handler Tests
- [ ] Create `http/handler_test.go`
- [ ] Use `httptest` for endpoint testing
- [ ] Test all endpoints
- [ ] Test SSE streaming
- [ ] Test CORS headers

**Phase 6 Acceptance**:
- [ ] All endpoints functional via http.Handler
- [ ] SSE streaming for /stream endpoint
- [ ] CORS headers present
- [ ] Framework adapters delegate correctly

---

## Phase 7: Compaction (Day 9)

### C7.1 - Compactor Interface
- [ ] Create `memory/compaction.go`
- [ ] Define `Compactor` interface
- [ ] Define `CompactOptions` struct
- [ ] Define `CompactStats` struct
- [ ] Define `CompactStrategy` constants

### C7.2 - Strategies
- [ ] Implement `truncate` strategy
- [ ] Implement `summarize` strategy (requires provider)
- [ ] Implement `archive` strategy
- [ ] Implement `DryRun` mode

### C7.3 - Compaction Tests
- [ ] Create `memory/compaction_test.go`
- [ ] Test each strategy
- [ ] Test DryRun returns stats without modifying
- [ ] Test safety on live data

**Phase 7 Acceptance**:
- [ ] Compact() safe on live data
- [ ] All three strategies implemented
- [ ] DryRun returns stats without modifying

---

## Phase 8: Additional Providers (Day 10)

### P8.1 - Anthropic Provider
- [ ] Create `provider/anthropic/anthropic.go`
- [ ] Implement Claude API format for messages
- [ ] Implement `Complete()` with Anthropic API
- [ ] Implement `Stream()` with Anthropic SSE format
- [ ] Create `provider/anthropic/anthropic_test.go` with fixtures

### P8.2 - Gemini Provider
- [ ] Create `provider/gemini/gemini.go`
- [ ] Implement Google AI format for messages
- [ ] Implement `Complete()` with Gemini API
- [ ] Implement `Stream()` with Gemini format
- [ ] Create `provider/gemini/gemini_test.go` with fixtures

### P8.3 - Zai Provider
- [ ] Create `provider/zai/zai.go`
- [ ] Implement `Complete()` with Zai API
- [ ] Implement `Stream()` with Zai format
- [ ] Create `provider/zai/zai_test.go` with fixtures

**Phase 8 Acceptance**:
- [ ] All providers implement Provider interface
- [ ] Streaming works for all providers
- [ ] Tool calling supported where available

---

## Phase 9: Examples & Documentation (Day 10)

### E9.1 - Basic Example
- [ ] Create `examples/basic/main.go`
- [ ] Simple agent with one tool
- [ ] Demonstrate basic usage pattern

### E9.2 - Streaming Example
- [ ] Create `examples/streaming/main.go`
- [ ] Demonstrate event handling
- [ ] Show real-time output

### E9.3 - Web Demo
- [ ] Create `examples/web-demo/main.go`
- [ ] HTTP server with handler
- [ ] Demonstrate web integration

### E9.4 - README
- [ ] Update `README.md` with installation instructions
- [ ] Add quickstart guide
- [ ] Document API overview
- [ ] Add code examples

**Phase 9 Acceptance**:
- [ ] All examples compile and run
- [ ] README covers core usage patterns

---

## Summary

| Phase | Tasks | Est. Days | Status |
|-------|-------|-----------|--------|
| 1. Provider | 6 | 2 | [ ] |
| 2. Tool | 5 | 1-2 | [ ] |
| 3. Agent | 7 | 2-3 | [ ] |
| 4. Skill | 4 | 1 | [ ] |
| 5. Memory | 5 | 2 | [ ] |
| 6. HTTP | 6 | 2 | [ ] |
| 7. Compaction | 3 | 1 | [ ] |
| 8. Providers | 3 | 1 | [ ] |
| 9. Examples | 4 | 1 | [ ] |
| **Total** | **43** | **10** | |

---

## Testing Commands

```bash
# Run all tests
go test ./...

# Run with race detection
go test -race ./...

# Run specific package
go test ./provider/...
go test ./agent/...

# Run with coverage
go test -cover ./...

# Run linter
golangci-lint run
```

---

## Notes

- All components must be thread-safe
- Use functional options pattern for configuration
- Errors must be wrapped with context
- Standard library only for core (optional deps for backends)
