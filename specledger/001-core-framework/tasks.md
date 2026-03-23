# Tasks: Core Framework (rl-agent v2)

**Branch**: `001-core-framework` | **Generated**: 2026-03-23 | **Plan**: [plan.md](./plan.md)

## Status Legend
- [ ] Not started
- [~] In progress
- [x] Complete
- [!] Blocked

---

## User Scenario Coverage

| Scenario | Priority | Phases | Acceptance Test |
|----------|----------|--------|-----------------|
| US-1: Basic Agent Completion | P1 | 1, 3 | `TestUserStory1_BasicCompletion` |
| US-2: Tool Calling | P1 | 2, 3 | `TestUserStory2_ToolCalling` |
| US-3: Streaming Responses | P2 | 1, 3 | `TestUserStory3_Streaming` |
| US-4: Memory Integration | P2 | 5 | `TestUserStory4_Memory` |
| US-5: HTTP API Exposure | P3 | 6 | `TestUserStory5_HTTP` |

---

## Phase 1: Provider Component (Days 1-2) `[P1] US-1, US-3`

**FR Coverage**: FR-001, FR-002, FR-010, FR-011, FR-012
**Test File**: `provider/provider_test.go`, `provider/openai/openai_test.go`

### P1.1 - Define Provider Types `[FR-001, FR-002]`
- [ ] Create `provider/provider.go`
- [ ] Define `Provider` interface: `Name()`, `Complete()`, `Stream()`, `Capabilities()`
- [ ] Define `CompletionRequest` struct
- [ ] Define `CompletionResponse` struct
- [ ] Define `StreamEvent` struct and `StreamEventType` constants
- [ ] Define `Message` struct and `MessageRole` constants
- [ ] Define `ToolCall` struct
- [ ] Define `ProviderCapabilities` struct
- **Acceptance**: Interface compiles with all methods
- **Test**: `provider/provider_test.go` - mock implementation

### P1.2 - Stream Patterns & Errors `[FR-002, FR-010]`
- [ ] Implement stream channel patterns in `provider/provider.go`
- [ ] Create error wrapping utilities with context
- [ ] Implement `ProviderError` type with retry info
- **Acceptance**: Errors wrapped with context, streams use proper channel patterns
- **Test**: `provider/provider_test.go` - error handling tests

### P1.3 - OpenAI Complete `[FR-001, FR-011, FR-012]`
- [ ] Create `provider/openai/openai.go`
- [ ] Implement OpenAI client struct with API key configuration
- [ ] Implement `Complete()` using `net/http` with TLS
- [ ] Handle request/response JSON marshaling
- [ ] Implement rate limit error handling with retry info
- **Acceptance**: Complete() returns valid responses, TLS enforced, no API keys logged
- **Test**: `provider/openai/openai_test.go` - `TestEdgeCase_RateLimit`, `TestSC007_RecordedFixtures`

### P1.4 - OpenAI Stream `[FR-002, FR-010]`
- [ ] Implement `Stream()` with SSE parsing in `provider/openai/openai.go`
- [ ] Parse `data:` lines from SSE response
- [ ] Yield `StreamEvent` on channel
- [ ] Close channel on completion or error
- [ ] Handle context cancellation
- **Acceptance**: Stream() yields events and closes channel, context respected
- **Test**: `provider/openai/openai_test.go` - streaming tests

### P1.5 - Provider Contract Tests `[SC-005]`
- [ ] Create `provider/provider_test.go`
- [ ] Create mock provider implementation
- [ ] Test interface compliance
- [ ] Test all Provider methods
- **Acceptance**: Mock implementations for all interfaces
- **Test**: `provider/provider_test.go` - `TestSC005_MockInterfaces`

### P1.6 - OpenAI Tests `[SC-007]`
- [ ] Create `provider/openai/openai_test.go`
- [ ] Record HTTP fixtures for tests
- [ ] Test Complete() with various responses
- [ ] Test Stream() event handling
- [ ] Test error cases (rate limit, timeout, invalid response)
- **Acceptance**: All tests pass with recorded fixtures
- **Test**: `provider/openai/openai_test.go` - `TestSC007_RecordedFixtures`

### P1.7 - Rate Limit Edge Case `[Edge]`
- [ ] Implement rate limit detection
- [ ] Return wrapped error with retry-after info
- **Acceptance**: Rate limit returns wrapped error with retry info
- **Test**: `provider/openai/openai_test.go` - `TestEdgeCase_RateLimit`

**Phase 1 Acceptance**:
- [ ] Provider interface compiles with all methods (FR-001)
- [ ] OpenAI Complete() returns valid responses
- [ ] OpenAI Stream() yields events and closes channel (FR-002)
- [ ] All tests pass with `go test ./provider/...`
- [ ] No API keys logged (FR-011)
- [ ] TLS enforced for all requests (FR-012)
- [ ] Context cancellation respected (FR-010)

---

## Phase 2: Tool Component (Days 2-3) `[P1] US-2`

**FR Coverage**: FR-003, FR-009
**Test File**: `tool/tool_test.go`

### T2.1 - Tool Interface `[FR-003]`
- [ ] Create `tool/tool.go`
- [ ] Define `Tool` interface: `Name()`, `Description()`, `Parameters()`, `Execute()`
- [ ] Define `ToolInfo` struct
- [ ] Define `ToolParameter` struct with JSON schema fields
- **Acceptance**: Interface defines all required methods
- **Test**: `tool/tool_test.go` - interface compliance

### T2.2 - Tool Registry `[FR-003, FR-008]`
- [ ] Define `ToolRegistry` interface in `tool/tool.go`
- [ ] Implement registry with `sync.Map` for thread-safety
- [ ] Implement `Register(tool Tool) error`
- [ ] Implement `Get(name string) (Tool, error)`
- [ ] Implement `List() []ToolInfo`
- [ ] Implement `ToProviderTools() []ToolDefinition`
- **Acceptance**: Registry thread-safe, all operations working
- **Test**: `tool/tool_test.go` - concurrent access with `-race`

### T2.3 - Fluent Builder `[FR-003]`
- [ ] Create `tool/builder.go`
- [ ] Implement `ToolBuilder` struct
- [ ] Implement `New(name)` constructor
- [ ] Implement `Description(desc)` method
- [ ] Implement `Param(name, typ, desc, required)` method
- [ ] Implement `Action(fn)` method with `func(context.Context, map[string]any) (any, error)`
- [ ] Implement `Build()` method with validation
- **Acceptance**: Fluent builder generates valid tools
- **Test**: `tool/tool_test.go` - builder tests

### T2.4 - Tool Definition Conversion `[FR-003]`
- [ ] Implement `ToolDefinition` struct for provider tools
- [ ] Implement conversion from Tool to ToolDefinition
- **Acceptance**: Tools convert to provider-compatible format
- **Test**: `tool/tool_test.go` - conversion tests

### T2.5 - Schema Validation `[FR-009]`
- [ ] Implement JSON schema validation for tool parameters
- [ ] Validate required fields present
- [ ] Validate types match schema
- **Acceptance**: Schema validation rejects invalid parameters before execution
- **Test**: `tool/tool_test.go` - `TestEdgeCase_SchemaValidation`

### T2.6 - Tool Tests `[FR-003, FR-009]`
- [ ] Create `tool/tool_test.go`
- [ ] Test registry Register/Get/List operations
- [ ] Test builder with valid/invalid configurations
- [ ] Test validation logic
- [ ] Test concurrent registry access with `-race`
- **Acceptance**: 80%+ coverage, all tests pass
- **Test**: `tool/tool_test.go`

**Phase 2 Acceptance**:
- [ ] Tool interface with Name, Description, Parameters, Execute
- [ ] Registry Register/Get/List/ToProviderTools working
- [ ] Fluent builder generates valid tools
- [ ] Thread-safe registry passes concurrent tests
- [ ] Schema validation rejects invalid parameters (FR-009)

---

## Phase 3: Agent Component (Days 3-5) `[P1] US-1, US-2, [P2] US-3`

**FR Coverage**: FR-002, FR-003, FR-004, FR-008, FR-010
**Test File**: `agent/agent_test.go`

### A3.1 - Agent Interface `[FR-002, FR-003]`
- [ ] Create `agent/agent.go`
- [ ] Define `Agent` interface: `Run()`, `Stream()`, `AddTool()`, `AddSkill()`
- [ ] Define `RunResult` struct with Content, ToolCalls, FinishReason
- [ ] Define `StreamResult` struct
- **Acceptance**: Interface defines all required methods
- **Test**: `agent/agent_test.go` - interface compliance

### A3.2 - Agent Config `[FR-004]`
- [ ] Implement `AgentConfig` struct with defaults in `agent/agent.go`
- [ ] Set sensible defaults (MaxSteps=10, Temperature=0.7)
- [ ] Implement `NewAgent()` constructor with provider
- **Acceptance**: Config provides sensible defaults
- **Test**: `agent/agent_test.go` - config tests

### A3.3 - Functional Options `[FR-003, FR-004, FR-005]`
- [ ] Create `agent/options.go`
- [ ] Implement `RunOption` type
- [ ] Implement `RunConfig` struct
- [ ] Implement `WithSystemPrompt(prompt string)` option
- [ ] Implement `WithTools(tools ...Tool)` option
- [ ] Implement `WithSkills(skills ...Skill)` option
- [ ] Implement `WithMaxSteps(n int)` option
- [ ] Implement `WithSession(userID, sessionID string)` option
- [ ] Implement `WithMemory(mem Memory)` option
- **Acceptance**: Functional options pattern working
- **Test**: `agent/agent_test.go` - options tests

### A3.4 - Run Implementation `[FR-002, FR-003]`
- [ ] Implement `Run()` with tool-calling loop in `agent/agent.go`
- [ ] Build request with system prompt + messages + tools
- [ ] Call provider.Complete
- [ ] Handle tool calls: execute tools, add results to messages
- [ ] Loop until no tool calls or MaxSteps reached
- **Acceptance**: Run() executes complete tool-calling loops
- **Test**: `agent/agent_test.go` - `TestUserStory2_ToolCalling`

### A3.5 - Stream Implementation `[FR-002]`
- [ ] Implement `Stream()` in `agent/agent.go`
- [ ] Forward provider stream events
- [ ] Generate tool execution events
- [ ] Handle tool results in stream context
- **Acceptance**: Stream() forwards provider events + tool events
- **Test**: `agent/agent_test.go` - `TestUserStory3_Streaming`

### A3.6 - MaxSteps Enforcement `[FR-004]`
- [ ] Implement MaxSteps limit check in loop
- [ ] Return result with FinishReason="max_steps" when exceeded
- **Acceptance**: MaxSteps prevents infinite loops
- **Test**: `agent/agent_test.go` - `TestSC002_MaxStepsEnforcement`, `TestEdgeCase_MaxStepsExceeded`

### A3.7 - Context Cancellation `[FR-010]`
- [ ] Implement context cancellation at all levels
- [ ] Pass context to provider calls
- [ ] Pass context to tool execution
- [ ] Ensure immediate stop on context cancel
- **Acceptance**: Context cancellation stops execution immediately
- **Test**: `agent/agent_test.go` - `TestEdgeCase_ContextCancellation`

### A3.8 - Thread Safety `[FR-008]`
- [ ] Implement thread-safe concurrent runs with sync primitives
- [ ] Protect shared state with mutexes
- **Acceptance**: Thread-safe for concurrent runs
- **Test**: `agent/agent_test.go` - concurrent run tests with `-race`

### A3.9 - Agent Unit Tests
- [ ] Create `agent/agent_test.go`
- [ ] Test Run loop with mock provider
- [ ] Test tool execution within loop
- [ ] Test MaxSteps limit enforcement
- [ ] Test context cancellation
- [ ] Test error handling
- **Acceptance**: 80%+ coverage
- **Test**: `agent/agent_test.go`

### A3.10 - User Story 1 Test `[US-1]`
- [ ] Implement `TestUserStory1_BasicCompletion`
- [ ] Scenario 1: agent.Run returns RunResult with content
- [ ] Scenario 2: response respects system prompt
- **Acceptance**: Test passes with mock provider
- **Test**: `agent/agent_test.go` - `TestUserStory1_BasicCompletion`

### A3.11 - User Story 2 Test `[US-2]`
- [ ] Implement `TestUserStory2_ToolCalling`
- [ ] Scenario 1: tool called with correct arguments
- [ ] Scenario 2: tool error handled gracefully
- **Acceptance**: Test passes with mock provider and tools
- **Test**: `agent/agent_test.go` - `TestUserStory2_ToolCalling`

### A3.12 - User Story 3 Test `[US-3]`
- [ ] Implement `TestUserStory3_Streaming`
- [ ] Scenario 1: receive channel of StreamEvents
- [ ] Scenario 2: context cancellation closes stream cleanly
- **Acceptance**: Test passes with streaming mock
- **Test**: `agent/agent_test.go` - `TestUserStory3_Streaming`

### A3.13 - Success Criteria Tests `[SC-001, SC-002]`
- [ ] Implement `TestSC001_CompletionLatency` - basic completion < 5s
- [ ] Implement `TestSC002_MaxStepsEnforcement` - tool loop completes within MaxSteps
- **Acceptance**: Latency tests pass
- **Test**: `agent/agent_test.go`

**Phase 3 Acceptance**:
- [ ] Agent.Run() executes complete tool-calling loops
- [ ] Agent.Stream() forwards provider events + tool events
- [ ] MaxSteps prevents infinite loops (FR-004)
- [ ] Context cancellation stops execution immediately (FR-010)
- [ ] Thread-safe for concurrent runs (FR-008)
- [ ] US-1, US-2, US-3 acceptance tests pass

---

## Phase 4: Skill Component (Day 5) `[P1] US-2 support`

**FR Coverage**: FR-007
**Test File**: `skill/skill_test.go`

### S4.1 - Skill Interface `[FR-007]`
- [ ] Create `skill/skill.go`
- [ ] Define `Skill` interface: `Name()`, `Description()`, `Tools()`, `Instructions()`, `SlashCommand()`
- [ ] Define `SkillInfo` struct
- **Acceptance**: Interface defines all required methods
- **Test**: `skill/skill_test.go` - interface compliance

### S4.2 - Skill Builder `[FR-007]`
- [ ] Implement `SkillBuilder` in `skill/skill.go`
- [ ] Implement `New(name)` constructor
- [ ] Implement `Description(desc)` method
- [ ] Implement `WithTool(tool Tool)` method
- [ ] Implement `WithInstruction(instruction string)` method
- [ ] Implement `AsSlashCommand(name, desc string)` method
- [ ] Implement `Build()` method with validation
- **Acceptance**: Builder creates valid skills
- **Test**: `skill/skill_test.go` - builder tests

### S4.3 - Slash Command Types `[FR-007]`
- [ ] Create `skill/slash.go`
- [ ] Define `SlashCommandDefinition` struct
- [ ] Define `SlashOption` struct
- [ ] Define `OptionType` constants (String, Integer, Boolean, etc.)
- [ ] Implement `WithSlashOption()` builder method
- **Acceptance**: Slash command metadata available
- **Test**: `skill/skill_test.go` - slash command tests

### S4.4 - Skill Tests `[FR-007]`
- [ ] Create `skill/skill_test.go`
- [ ] Test builder with multiple tools
- [ ] Test instruction integration
- [ ] Test slash command metadata
- **Acceptance**: 80%+ coverage
- **Test**: `skill/skill_test.go`

**Phase 4 Acceptance**:
- [ ] Skills bundle multiple tools
- [ ] Instructions integrate with system prompt
- [ ] Slash command metadata available

---

## Phase 5: Memory Component (Days 6-7) `[P2] US-4`

**FR Coverage**: FR-005, FR-010
**Test File**: `memory/memory_test.go`, `memory/inmemory/inmemory_test.go`

### M5.1 - Memory Interface `[FR-005, FR-010]`
- [ ] Create `memory/memory.go`
- [ ] Define `Memory` interface: `Add()`, `Get()`, `Search()`, `Clear()`
- [ ] Define `Entry` struct: ID, Role, Content, Metadata, Timestamp
- [ ] Define `MemoryBackend` interface: `Connect()`, `Close()`, `Memory()`
- [ ] All methods accept context for cancellation
- **Acceptance**: Interface defines all required methods
- **Test**: `memory/memory_test.go` - interface compliance

### M5.2 - InMemory Backend `[FR-005, FR-008, FR-010]`
- [ ] Create `memory/inmemory/inmemory.go`
- [ ] Implement thread-safe storage with `sync.RWMutex`
- [ ] Implement `Add(ctx, userID, sessionID, entry)` method
- [ ] Implement `Get(ctx, userID, sessionID, limit)` with chronological ordering
- [ ] Implement `Search(ctx, query)` - return empty for InMemory (unsupported)
- [ ] Implement `Clear(ctx, userID, sessionID)` method
- **Acceptance**: Thread-safe, all operations working
- **Test**: `memory/inmemory/inmemory_test.go` - concurrent tests with `-race`

### M5.3 - SQLite Backend `[FR-005]`
- [ ] Create `memory/sqlite/sqlite.go`
- [ ] Implement schema creation (entries table with indexes)
- [ ] Implement all Memory methods with SQL queries
- [ ] Implement Search with LIKE query
- [ ] Handle connection pooling
- **Acceptance**: SQLite backend functional
- **Test**: `memory/sqlite/sqlite_test.go`

### M5.4 - Memory Interface Tests `[SC-005]`
- [ ] Create `memory/memory_test.go`
- [ ] Create backend-agnostic test suite
- [ ] Test Add/Get/Clear operations
- [ ] Test chronological ordering
- [ ] Test context cancellation
- **Acceptance**: Backend-agnostic tests pass for all backends
- **Test**: `memory/memory_test.go`

### M5.5 - InMemory Tests `[SC-003]`
- [ ] Create `memory/inmemory/inmemory_test.go`
- [ ] Test concurrent access with `-race`
- [ ] Test thread-safety guarantees
- [ ] Implement `TestSC003_MemoryGetLatency` - 100 entries < 10ms
- **Acceptance**: Latency test passes
- **Test**: `memory/inmemory/inmemory_test.go` - `TestSC003_MemoryGetLatency`

### M5.6 - User Story 4 Test `[US-4]`
- [ ] Implement `TestUserStory4_Memory`
- [ ] Scenario 1: messages persisted after run
- [ ] Scenario 2: previous context included in subsequent runs
- **Acceptance**: Test passes
- **Test**: `memory/memory_test.go` - `TestUserStory4_Memory`

**Phase 5 Acceptance**:
- [ ] Memory interface Add/Get/Search/Clear working
- [ ] InMemory backend thread-safe
- [ ] Get returns entries in chronological order
- [ ] Search returns empty if unsupported
- [ ] Context cancellation supported (FR-010)
- [ ] US-4 acceptance test passes

---

## Phase 6: HTTP Handler (Days 8-9) `[P3] US-5`

**FR Coverage**: FR-006
**Test File**: `http/handler_test.go`

### H6.1 - Handler Interface `[FR-006]`
- [ ] Create `http/handler.go`
- [ ] Define `Handler` interface extending `http.Handler`
- [ ] Define `RunRequest` struct
- [ ] Define `RunResponse` struct
- [ ] Define `StreamRequest` struct
- [ ] Define `ErrorResponse` struct
- **Acceptance**: Interface defines all required types
- **Test**: `http/handler_test.go`

### H6.2 - Endpoints `[FR-006]`
- [ ] Implement `POST /run` endpoint - returns JSON RunResult
- [ ] Implement `POST /stream` with SSE - returns text/event-stream
- [ ] Implement `GET /tools` endpoint - lists registered tools
- [ ] Implement `POST /tools` endpoint - executes a tool directly
- [ ] Implement `GET /health` endpoint - returns 200 OK
- **Acceptance**: All endpoints functional
- **Test**: `http/handler_test.go` - endpoint tests

### H6.3 - CORS & JSON `[FR-006]`
- [ ] Implement CORS middleware
- [ ] Add CORS headers: Access-Control-Allow-Origin, Methods, Headers
- [ ] Implement JSON request parsing
- [ ] Implement JSON response writing
- [ ] Implement error response formatting with proper status codes
- **Acceptance**: CORS headers present, JSON handling correct
- **Test**: `http/handler_test.go` - CORS tests

### H6.4 - Gin Adapter `[FR-006]`
- [ ] Create `http/adapters/gin.go`
- [ ] Implement `GinHandler(agent Agent) gin.HandlerFunc` function
- [ ] Delegate to standard handler
- **Acceptance**: Adapter delegates correctly
- **Test**: `http/adapters/gin_test.go`

### H6.5 - Echo Adapter `[FR-006]`
- [ ] Create `http/adapters/echo.go`
- [ ] Implement `EchoHandler(agent Agent) echo.HandlerFunc` function
- [ ] Delegate to standard handler
- **Acceptance**: Adapter delegates correctly
- **Test**: `http/adapters/echo_test.go`

### H6.6 - Handler Tests `[SC-006]`
- [ ] Create `http/handler_test.go`
- [ ] Use `httptest` for endpoint testing
- [ ] Test all endpoints
- [ ] Test SSE streaming
- [ ] Test CORS headers
- [ ] Implement `TestSC006_HTTPCompliance`
- **Acceptance**: Compliance tests pass
- **Test**: `http/handler_test.go` - `TestSC006_HTTPCompliance`

### H6.7 - User Story 5 Test `[US-5]`
- [ ] Implement `TestUserStory5_HTTP`
- [ ] Scenario 1: POST /run returns JSON RunResult
- [ ] Scenario 2: POST /stream returns SSE events
- **Acceptance**: Test passes
- **Test**: `http/handler_test.go` - `TestUserStory5_HTTP`

**Phase 6 Acceptance**:
- [ ] All endpoints functional via http.Handler
- [ ] SSE streaming for /stream endpoint
- [ ] CORS headers present
- [ ] Framework adapters delegate correctly
- [ ] US-5 acceptance test passes

---

## Phase 7: Compaction (Day 9)

**Test File**: `memory/compaction_test.go`

### C7.1 - Compactor Interface
- [ ] Create `memory/compaction.go`
- [ ] Define `Compactor` interface with `Compact(ctx, opts) (CompactStats, error)`
- [ ] Define `CompactOptions` struct: Strategy, DryRun, MaxEntries, etc.
- [ ] Define `CompactStats` struct: EntriesRemoved, BytesFreed, etc.
- [ ] Define `CompactStrategy` constants: Truncate, Summarize, Archive
- **Acceptance**: Interface defines all required types
- **Test**: `memory/compaction_test.go`

### C7.2 - Strategies
- [ ] Implement `truncate` strategy - remove oldest entries beyond threshold
- [ ] Implement `summarize` strategy - requires provider for summarization
- [ ] Implement `archive` strategy - move old entries to archive storage
- [ ] Implement `DryRun` mode - return stats without modifying
- **Acceptance**: All three strategies implemented
- **Test**: `memory/compaction_test.go` - strategy tests

### C7.3 - Compaction Tests
- [ ] Create `memory/compaction_test.go`
- [ ] Test each strategy
- [ ] Test DryRun returns stats without modifying
- [ ] Test safety on live data (concurrent access during compaction)
- **Acceptance**: 80%+ coverage
- **Test**: `memory/compaction_test.go`

**Phase 7 Acceptance**:
- [ ] Compact() safe on live data
- [ ] All three strategies implemented
- [ ] DryRun returns stats without modifying

---

## Phase 8: Additional Providers (Day 10) `[P1] FR-001 completion`

**FR Coverage**: FR-001, FR-011, FR-012
**Test File**: `provider/anthropic/anthropic_test.go`, `provider/gemini/gemini_test.go`, `provider/zai/zai_test.go`

### P8.1 - Anthropic Provider `[FR-001, FR-011, FR-012]`
- [ ] Create `provider/anthropic/anthropic.go`
- [ ] Implement Claude API format for messages (system, user, assistant)
- [ ] Implement `Complete()` with Anthropic API (messages endpoint)
- [ ] Implement `Stream()` with Anthropic SSE format
- [ ] Handle Claude-specific tool calling format
- [ ] Create `provider/anthropic/anthropic_test.go` with recorded fixtures
- **Acceptance**: Provider implements interface, no API keys logged, TLS enforced
- **Test**: `provider/anthropic/anthropic_test.go` - `TestSC007_RecordedFixtures`

### P8.2 - Gemini Provider `[FR-001, FR-011, FR-012]`
- [ ] Create `provider/gemini/gemini.go`
- [ ] Implement Google AI format for messages (contents array)
- [ ] Implement `Complete()` with Gemini API (generateContent endpoint)
- [ ] Implement `Stream()` with Gemini streaming format
- [ ] Handle Gemini-specific tool/function calling format
- [ ] Create `provider/gemini/gemini_test.go` with recorded fixtures
- **Acceptance**: Provider implements interface, no API keys logged, TLS enforced
- **Test**: `provider/gemini/gemini_test.go` - `TestSC007_RecordedFixtures`

### P8.3 - Zai Provider `[FR-001, FR-011, FR-012]`
- [ ] Create `provider/zai/zai.go`
- [ ] Implement `Complete()` with Zai API
- [ ] Implement `Stream()` with Zai format
- [ ] Handle Zai-specific tool calling format (if supported)
- [ ] Create `provider/zai/zai_test.go` with recorded fixtures
- **Acceptance**: Provider implements interface, no API keys logged, TLS enforced
- **Test**: `provider/zai/zai_test.go` - `TestSC007_RecordedFixtures`

### P8.4 - Provider Tests `[SC-007]`
- [ ] Create provider-specific test files for all providers
- [ ] Record HTTP fixtures for Anthropic, Gemini, Zai
- [ ] Test Complete() with various responses for each provider
- [ ] Test Stream() event handling for each provider
- [ ] Test error cases (rate limit, timeout, invalid response)
- **Acceptance**: All providers tested with recorded fixtures
- **Test**: `provider/*_test.go` - `TestSC007_RecordedFixtures`

**Phase 8 Acceptance**:
- [ ] All providers implement Provider interface (FR-001)
- [ ] Streaming works for all providers
- [ ] Tool calling supported where available
- [ ] No API keys logged (FR-011)
- [ ] TLS enforced (FR-012)

---

## Phase 9: Examples & Documentation (Day 10)

### E9.1 - Basic Example `[US-1, US-2]`
- [ ] Create `examples/basic/main.go`
- [ ] Simple agent with one tool (weather example)
- [ ] Demonstrate basic usage pattern
- [ ] Show Run() with tool execution
- **Acceptance**: Example compiles and runs
- **Test**: `go run examples/basic/main.go`

### E9.2 - Streaming Example `[US-3]`
- [ ] Create `examples/streaming/main.go`
- [ ] Demonstrate Stream() event handling
- [ ] Show real-time output printing
- [ ] Handle all event types
- **Acceptance**: Example compiles and runs
- **Test**: `go run examples/streaming/main.go`

### E9.3 - Web Demo `[US-5]`
- [ ] Create `examples/web-demo/main.go`
- [ ] HTTP server with handler
- [ ] Demonstrate web integration
- [ ] Include health endpoint usage
- **Acceptance**: Example compiles and runs
- **Test**: `go run examples/web-demo/main.go`

### E9.4 - README
- [ ] Update `README.md` with installation instructions
- [ ] Add quickstart guide
- [ ] Document API overview for each component
- [ ] Add code examples for common patterns
- **Acceptance**: README covers core usage patterns
- **Test**: Manual review

### E9.5 - SC-004 Verification `[SC-004]`
- [ ] Verify `go.mod` has no external dependencies for core
- [ ] Create test or script to validate zero external deps
- **Acceptance**: Core has zero external deps
- **Test**: `go mod graph | grep -v "std" | wc -l` should be 0 for core

**Phase 9 Acceptance**:
- [ ] All examples compile and run
- [ ] README covers core usage patterns
- [ ] SC-004 verified (zero external deps)

---

## Edge Case Tests Summary

| Test | Phase | File | Description |
|------|-------|------|-------------|
| `TestEdgeCase_RateLimit` | 1 | `provider/openai/openai_test.go` | Rate limit returns wrapped error with retry info |
| `TestEdgeCase_SchemaValidation` | 2 | `tool/tool_test.go` | Validation error before execution |
| `TestEdgeCase_MaxStepsExceeded` | 3 | `agent/agent_test.go` | Returns FinishReason="max_steps" |
| `TestEdgeCase_ContextCancellation` | 3 | `agent/agent_test.go` | Mid-tool cancellation handled |

---

## Success Criteria Tests Summary

| SC | Test | Phase | File |
|----|------|-------|------|
| SC-001 | `TestSC001_CompletionLatency` | 3 | `agent/agent_test.go` |
| SC-002 | `TestSC002_MaxStepsEnforcement` | 3 | `agent/agent_test.go` |
| SC-003 | `TestSC003_MemoryGetLatency` | 5 | `memory/inmemory/inmemory_test.go` |
| SC-004 | `TestSC004_NoExternalDeps` | 9 | `go.mod` |
| SC-005 | `TestSC005_MockInterfaces` | All | `*_test.go` |
| SC-006 | `TestSC006_HTTPCompliance` | 6 | `http/handler_test.go` |
| SC-007 | `TestSC007_RecordedFixtures` | 1, 8 | `provider/*_test.go` |

---

## Summary

| Phase | Tasks | Est. Days | Priority | FR Coverage | Status |
|-------|-------|-----------|----------|-------------|--------|
| 1. Provider | 7 | 2 | P1 | FR-001, FR-002, FR-010, FR-011, FR-012 | [ ] |
| 2. Tool | 6 | 1-2 | P1 | FR-003, FR-009 | [ ] |
| 3. Agent | 13 | 2-3 | P1/P2 | FR-002, FR-003, FR-004, FR-008, FR-010 | [ ] |
| 4. Skill | 4 | 1 | P1 | FR-007 | [ ] |
| 5. Memory | 6 | 2 | P2 | FR-005, FR-010 | [ ] |
| 6. HTTP | 7 | 2 | P3 | FR-006 | [ ] |
| 7. Compaction | 3 | 1 | - | - | [ ] |
| 8. Providers | 4 | 1 | P1 | FR-001, FR-011, FR-012 | [ ] |
| 9. Examples | 5 | 1 | - | SC-004 | [ ] |
| **Total** | **55** | **10** | | | |

---

## FR Coverage Matrix

| FR | Requirement | Phases | Key Tests |
|----|-------------|--------|-----------|
| FR-001 | Multiple LLM providers | 1, 8 | Provider interface tests |
| FR-002 | Streaming and non-streaming | 1, 3 | US-3, Stream tests |
| FR-003 | Tool registration and calling | 2, 3 | US-2, Tool tests |
| FR-004 | MaxSteps limit enforcement | 3 | SC-002, Edge:MaxSteps |
| FR-005 | Pluggable Memory backends | 5 | US-4, Memory tests |
| FR-006 | HTTP endpoints | 6 | US-5, SC-006 |
| FR-007 | Skill bundling | 4 | Skill tests |
| FR-008 | Thread-safe concurrent runs | 3 | Race tests |
| FR-009 | Tool schema validation | 2 | Edge:SchemaValidation |
| FR-010 | Context cancellation | 1, 3, 5 | Edge:ContextCancellation |
| FR-011 | No logging of API keys | 1, 8 | Manual audit |
| FR-012 | TLS for all HTTP requests | 1, 8 | Provider tests |

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

# Run specific test
go test -run TestUserStory1_BasicCompletion ./agent/...
go test -run TestEdgeCase_RateLimit ./provider/openai/...
go test -run TestSC003_MemoryGetLatency ./memory/inmemory/...
```

---

## Notes

- All components must be thread-safe (FR-008)
- Use functional options pattern for configuration
- Errors must be wrapped with context
- Standard library only for core (optional deps for backends)
- Each phase should have acceptance tests that map to user scenarios
- Edge cases must have explicit test coverage
