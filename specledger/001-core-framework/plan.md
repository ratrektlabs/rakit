# Implementation Plan: Core Framework (rl-agent v2)

**Branch**: `001-core-framework` | **Date**: 2026-03-23 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specledger/001-core-framework/spec.md`

## Summary

Lightweight, extensible Go library for building AI-powered applications with zero external dependencies (stdlib only). Core components: Provider (LLM abstraction), Agent (orchestration), Tool (executable functions), Skill (tool bundles), Memory (persistence), HTTP Handler (web exposure).

## Technical Context

**Language/Version**: Go 1.22  
**Primary Dependencies**: Standard library only (encoding/json, net/http, context, sync, testing)  
**Storage**: InMemory (default), SQLite, MongoDB (memory backends)  
**Testing**: go test with table-driven tests, httptest for HTTP  
**Target Platform**: Linux server / any Go-supported platform  
**Project Type**: Library (single module)  
**Performance Goals**: Provider.Complete < 5s, Memory.Get < 10ms for 100 entries, zero allocations in hot paths  
**Constraints**: Zero external deps for core, thread-safe components, <200ms p95 for non-LLM ops  
**Scale/Scope**: Multi-user applications, production-ready memory backends

## Constitution Check

- [x] **Specification-First**: Spec.md complete with interfaces, types, and contracts
- [x] **Test-First**: Contract tests for interfaces, integration tests for providers
- [x] **Code Quality**: gofmt, go vet, staticcheck (via golangci-lint)
- [x] **UX Consistency**: Fluent builder APIs, functional options pattern
- [x] **Performance**: Latency targets defined (<5s provider, <10ms memory)
- [x] **Observability**: Error wrapping with context, structured logging hooks
- [x] **Issue Tracking**: Epic linked to specledger/001-core-framework/

**Complexity Violations**: None identified

---

## User Scenarios → Phase Mapping

| Scenario | Priority | Phase | Acceptance Test |
|----------|----------|-------|-----------------|
| US-1: Basic Agent Completion | P1 | Phase 1-3 | `TestUserStory1_BasicCompletion` |
| US-2: Tool Calling | P1 | Phase 2-3 | `TestUserStory2_ToolCalling` |
| US-3: Streaming Responses | P2 | Phase 1,3 | `TestUserStory3_Streaming` |
| US-4: Memory Integration | P2 | Phase 5 | `TestUserStory4_Memory` |
| US-5: HTTP API Exposure | P3 | Phase 6 | `TestUserStory5_HTTP` |

---

## Functional Requirements → Phase Mapping

| FR | Requirement | Phase(s) |
|----|-------------|----------|
| FR-001 | Multiple LLM providers via common interface | Phase 1, 8 |
| FR-002 | Streaming and non-streaming completion | Phase 1, 3 |
| FR-003 | Tool registration and automatic calling | Phase 2, 3 |
| FR-004 | MaxSteps limit enforcement | Phase 3 |
| FR-005 | Pluggable Memory backends | Phase 5 |
| FR-006 | HTTP endpoints (run, stream, tools, health) | Phase 6 |
| FR-007 | Skill bundling (tools + instructions) | Phase 4 |
| FR-008 | Thread-safe concurrent agent runs | Phase 3 |
| FR-009 | Tool parameter JSON schema validation | Phase 2 |
| FR-010 | Context cancellation at all levels | Phase 1, 3, 5 |
| FR-011 | No logging of API keys/credentials | Phase 1, 8 |
| FR-012 | TLS for all provider HTTP requests | Phase 1, 8 |

---

## Success Criteria → Acceptance Tests

| SC | Criteria | Test File | Test Function |
|----|----------|-----------|---------------|
| SC-001 | Basic completion < 5s latency | `agent/agent_test.go` | `TestSC001_CompletionLatency` |
| SC-002 | Tool loop completes within MaxSteps | `agent/agent_test.go` | `TestSC002_MaxStepsEnforcement` |
| SC-003 | Memory.Get 100 entries < 10ms | `memory/inmemory/inmemory_test.go` | `TestSC003_MemoryGetLatency` |
| SC-004 | Zero external deps in core | `go.mod` | `TestSC004_NoExternalDeps` |
| SC-005 | Mock implementations for all interfaces | `*_test.go` | `TestSC005_MockInterfaces` |
| SC-006 | HTTP handler compliance | `http/handler_test.go` | `TestSC006_HTTPCompliance` |
| SC-007 | Provider tests use fixtures | `provider/*_test.go` | `TestSC007_RecordedFixtures` |

---

## Edge Cases Handling

| Edge Case | Handling Strategy | Phase | Test |
|-----------|-------------------|-------|------|
| Provider rate limited | Return wrapped error with retry info | Phase 1 | `TestEdgeCase_RateLimit` |
| MaxSteps exceeded | Return result with FinishReason="max_steps" | Phase 3 | `TestEdgeCase_MaxStepsExceeded` |
| Tool schema validation fails | Return validation error before execution | Phase 2 | `TestEdgeCase_SchemaValidation` |
| Context cancelled mid-tool | Tools receive context, must respect cancellation | Phase 3 | `TestEdgeCase_ContextCancellation` |

---

## Project Structure

### Documentation

```text
specledger/001-core-framework/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
└── tasks.md             # Phase 2 output
```

### Source Code

```text
rl-agent/
├── agent/
│   ├── agent.go          # Agent interface + implementation
│   └── options.go        # Functional options
├── provider/
│   ├── provider.go       # Provider interface + types
│   ├── openai/
│   │   └── openai.go     # OpenAI implementation
│   ├── anthropic/
│   │   └── anthropic.go  # Anthropic implementation
│   ├── gemini/
│   │   └── gemini.go     # Gemini implementation
│   └── zai/
│       └── zai.go        # Zai implementation
├── tool/
│   ├── tool.go           # Tool interface + registry
│   └── builder.go        # Fluent builder
├── skill/
│   ├── skill.go          # Skill interface + builder
│   └── slash.go          # Slash command types
├── memory/
│   ├── memory.go         # Memory interface + types
│   ├── inmemory/
│   │   └── inmemory.go   # In-memory backend
│   ├── sqlite/
│   │   └── sqlite.go     # SQLite backend
│   └── mongodb/
│       └── mongodb.go    # MongoDB backend
├── http/
│   ├── handler.go        # HTTP handler
│   └── adapters/
│       ├── gin.go        # Gin adapter
│       └── echo.go       # Echo adapter
├── examples/
│   ├── basic/
│   │   └── main.go
│   ├── streaming/
│   │   └── main.go
│   └── web-demo/
│       └── main.go
├── go.mod
└── README.md
```

**Structure Decision**: Single Go module library with package-per-component organization. Clean separation enables users to import only needed packages.

---

## Phase Breakdown

### Phase 1: Provider Component (Days 1-2) — [P1] US-1, US-3

**Goal**: LLM abstraction layer with OpenAI implementation

**Covers**: FR-001, FR-002, FR-010, FR-011, FR-012

| Task | Files | Description |
|------|-------|-------------|
| P1.1 | `provider/provider.go` | Define Provider interface, CompletionRequest, CompletionResponse, StreamEvent, Message, ToolCall, ProviderCapabilities types |
| P1.2 | `provider/provider.go` | Implement stream channel patterns, error wrapping utilities |
| P1.3 | `provider/openai/openai.go` | OpenAI Complete() implementation using net/http with TLS |
| P1.4 | `provider/openai/openai.go` | OpenAI Stream() implementation with SSE parsing |
| P1.5 | `provider/provider_test.go` | Interface contract tests with mock provider (SC-005) |
| P1.6 | `provider/openai/openai_test.go` | OpenAI tests with recorded HTTP fixtures (SC-007) |
| P1.7 | `provider/openai/openai.go` | Rate limit error handling with retry info |

**Acceptance Criteria**:
- [ ] Provider interface compiles with all methods (FR-001)
- [ ] OpenAI Complete() returns valid responses
- [ ] OpenAI Stream() yields events and closes channel (FR-002)
- [ ] All tests pass with `go test ./provider/...`
- [ ] No API keys logged (FR-011)
- [ ] TLS enforced for all requests (FR-012)
- [ ] Context cancellation respected (FR-010)

**Edge Case Tests**:
- [ ] `TestEdgeCase_RateLimit` - rate limit returns wrapped error with retry info

---

### Phase 2: Tool Component (Days 2-3) — [P1] US-2

**Goal**: Executable function registry with fluent builder

**Covers**: FR-003, FR-009

| Task | Files | Description |
|------|-------|-------------|
| T2.1 | `tool/tool.go` | Define Tool interface, ToolRegistry interface |
| T2.2 | `tool/tool.go` | Implement registry with sync.Map for thread-safety |
| T2.3 | `tool/builder.go` | Fluent ToolBuilder with Param(), Action(), Build() |
| T2.4 | `tool/tool.go` | ToolDefinition conversion for provider tools |
| T2.5 | `tool/tool.go` | JSON schema validation for tool parameters (FR-009) |
| T2.6 | `tool/tool_test.go` | Registry tests, builder tests, validation tests |

**Acceptance Criteria**:
- [ ] Tool interface with Name, Description, Parameters, Execute
- [ ] Registry Register/Get/List/ToProviderTools working
- [ ] Fluent builder generates valid tools
- [ ] Thread-safe registry passes concurrent tests
- [ ] Schema validation rejects invalid parameters (FR-009)

**Edge Case Tests**:
- [ ] `TestEdgeCase_SchemaValidation` - validation error before execution

---

### Phase 3: Agent Component (Days 3-5) — [P1] US-1, US-2, [P2] US-3

**Goal**: Orchestration engine with tool-calling loop

**Covers**: FR-002, FR-003, FR-004, FR-008, FR-010

| Task | Files | Description |
|------|-------|-------------|
| A3.1 | `agent/agent.go` | Define Agent interface (Run, Stream, AddTool, AddSkill) |
| A3.2 | `agent/agent.go` | AgentConfig struct with defaults |
| A3.3 | `agent/options.go` | Functional options (WithTools, WithSkills, WithMaxSteps, WithSession) |
| A3.4 | `agent/agent.go` | Run() implementation with tool-calling loop |
| A3.5 | `agent/agent.go` | Stream() implementation with event forwarding |
| A3.6 | `agent/agent.go` | MaxSteps enforcement with FinishReason="max_steps" (FR-004) |
| A3.7 | `agent/agent.go` | Context cancellation at all levels (FR-010) |
| A3.8 | `agent/agent.go` | Thread-safe concurrent runs with sync primitives (FR-008) |
| A3.9 | `agent/agent_test.go` | Run loop tests, tool execution tests, error handling tests |
| A3.10 | `agent/agent_test.go` | User Story 1 acceptance test: `TestUserStory1_BasicCompletion` |
| A3.11 | `agent/agent_test.go` | User Story 2 acceptance test: `TestUserStory2_ToolCalling` |
| A3.12 | `agent/agent_test.go` | User Story 3 acceptance test: `TestUserStory3_Streaming` |

**Acceptance Criteria**:
- [ ] Agent.Run() executes complete tool-calling loops
- [ ] Agent.Stream() forwards provider events + tool events
- [ ] MaxSteps prevents infinite loops (FR-004)
- [ ] Context cancellation stops execution immediately (FR-010)
- [ ] Thread-safe for concurrent runs (FR-008)

**Success Criteria Tests**:
- [ ] `TestSC001_CompletionLatency` - basic completion < 5s
- [ ] `TestSC002_MaxStepsEnforcement` - tool loop completes within MaxSteps

**Edge Case Tests**:
- [ ] `TestEdgeCase_MaxStepsExceeded` - returns FinishReason="max_steps"
- [ ] `TestEdgeCase_ContextCancellation` - mid-tool cancellation handled

---

### Phase 4: Skill Component (Day 5) — [P1] US-2 support

**Goal**: Tool bundles with instructions

**Covers**: FR-007

| Task | Files | Description |
|------|-------|-------------|
| S4.1 | `skill/skill.go` | Define Skill interface |
| S4.2 | `skill/skill.go` | SkillBuilder with WithTool, WithInstruction, AsSlashCommand |
| S4.3 | `skill/slash.go` | SlashCommandDefinition, SlashOption types |
| S4.4 | `skill/skill_test.go` | Builder tests, tool bundling tests |

**Acceptance Criteria**:
- [ ] Skills bundle multiple tools
- [ ] Instructions integrate with system prompt
- [ ] Slash command metadata available

---

### Phase 5: Memory Component (Days 6-7) — [P2] US-4

**Goal**: Conversation persistence with multiple backends

**Covers**: FR-005, FR-010

| Task | Files | Description |
|------|-------|-------------|
| M5.1 | `memory/memory.go` | Define Memory interface, Entry struct, MemoryBackend interface |
| M5.2 | `memory/inmemory/inmemory.go` | Thread-safe in-memory implementation with sync.RWMutex |
| M5.3 | `memory/sqlite/sqlite.go` | SQLite backend using database/sql |
| M5.4 | `memory/memory_test.go` | Backend-agnostic interface tests |
| M5.5 | `memory/inmemory/inmemory_test.go` | InMemory specific tests |
| M5.6 | `memory/memory_test.go` | User Story 4 acceptance test: `TestUserStory4_Memory` |

**Acceptance Criteria**:
- [ ] Memory interface Add/Get/Search/Clear working
- [ ] InMemory backend thread-safe
- [ ] Get returns entries in chronological order
- [ ] Search returns empty if unsupported
- [ ] Context cancellation supported (FR-010)

**Success Criteria Tests**:
- [ ] `TestSC003_MemoryGetLatency` - 100 entries < 10ms

---

### Phase 6: HTTP Handler (Days 8-9) — [P3] US-5

**Goal**: Web exposure with framework adapters

**Covers**: FR-006

| Task | Files | Description |
|------|-------|-------------|
| H6.1 | `http/handler.go` | Handler interface extending http.Handler |
| H6.2 | `http/handler.go` | POST /run, POST /stream (SSE), GET /tools, POST /tools, GET /health |
| H6.3 | `http/handler.go` | CORS support, JSON request/response |
| H6.4 | `http/adapters/gin.go` | Gin adapter function |
| H6.5 | `http/adapters/echo.go` | Echo adapter function |
| H6.6 | `http/handler_test.go` | httptest-based endpoint tests |
| H6.7 | `http/handler_test.go` | User Story 5 acceptance test: `TestUserStory5_HTTP` |

**Acceptance Criteria**:
- [ ] All endpoints functional via http.Handler
- [ ] SSE streaming for /stream endpoint
- [ ] CORS headers present
- [ ] Framework adapters delegate correctly

**Success Criteria Tests**:
- [ ] `TestSC006_HTTPCompliance` - handler passes standard compliance tests

---

### Phase 7: Compaction (Day 9)

**Goal**: Memory size management

| Task | Files | Description |
|------|-------|-------------|
| C7.1 | `memory/compaction.go` | Compactor interface, CompactOptions, CompactStats |
| C7.2 | `memory/compaction.go` | Truncate, summarize, archive strategies |
| C7.3 | `memory/compaction_test.go` | Strategy tests, dry-run tests |

**Acceptance Criteria**:
- [ ] Compact() safe on live data
- [ ] All three strategies implemented
- [ ] DryRun returns stats without modifying

---

### Phase 8: Additional Providers (Day 10) — [P1] FR-001 completion

**Goal**: Anthropic, Gemini, Zai implementations

**Covers**: FR-001, FR-011, FR-012

| Task | Files | Description |
|------|-------|-------------|
| P8.1 | `provider/anthropic/anthropic.go` | Anthropic Complete/Stream with Claude API format |
| P8.2 | `provider/gemini/gemini.go` | Gemini Complete/Stream with Google AI format |
| P8.3 | `provider/zai/zai.go` | Zai Complete/Stream |
| P8.4 | `provider/*_test.go` | Provider-specific tests with fixtures (SC-007) |

**Acceptance Criteria**:
- [ ] All providers implement Provider interface
- [ ] Streaming works for all providers
- [ ] Tool calling supported where available
- [ ] No API keys logged (FR-011)
- [ ] TLS enforced (FR-012)

---

### Phase 9: Examples & Documentation (Day 10)

**Goal**: Usage examples and quickstart

| Task | Files | Description |
|------|-------|-------------|
| E9.1 | `examples/basic/main.go` | Simple agent with one tool (US-1, US-2) |
| E9.2 | `examples/streaming/main.go` | Streaming example with event handling (US-3) |
| E9.3 | `examples/web-demo/main.go` | HTTP server example (US-5) |
| E9.4 | `README.md` | Installation, quickstart, API overview |

**Acceptance Criteria**:
- [ ] All examples compile and run
- [ ] README covers core usage patterns

---

## Testing Strategy

| Level | Scope | Tools | Coverage Target |
|-------|-------|-------|-----------------|
| Unit | Individual functions, builders | go test, table-driven | 80%+ |
| Contract | Interface compliance | Mock implementations | All interfaces |
| Integration | Provider HTTP calls | httptest, recorded fixtures | All providers |
| Acceptance | User scenarios | `TestUserStoryN_*` functions | All 5 scenarios |
| Edge Case | Error handling | `TestEdgeCase_*` functions | All 4 cases |
| Performance | Latency targets | `TestSC00N_*` functions | All 7 criteria |

---

## Acceptance Test Specifications

### User Story 1: Basic Completion

```go
func TestUserStory1_BasicCompletion(t *testing.T) {
    // Scenario 1: agent.Run returns RunResult with content
    agent := agent.New(provider, agent.WithSystemPrompt("You are helpful"))
    result, err := agent.Run(ctx, []Message{{Role: "user", Content: "Hello"}})
    assert.NoError(t, err)
    assert.NotEmpty(t, result.Content)
    
    // Scenario 2: response respects system prompt
    assert.Contains(t, strings.ToLower(result.Content), "help")
}
```

### User Story 2: Tool Calling

```go
func TestUserStory2_ToolCalling(t *testing.T) {
    // Scenario 1: tool called with correct arguments
    weatherTool := tool.New("weather").
        Description("Get weather").
        Param("location", "string", "City", true).
        Action(func(ctx context.Context, args map[string]any) (any, error) {
            return "sunny", nil
        })
    
    agent := agent.New(provider, agent.WithTools(weatherTool))
    result, err := agent.Run(ctx, []Message{{Role: "user", Content: "Weather in NYC?"}})
    assert.NoError(t, err)
    assert.Equal(t, "NYC", capturedLocation)
    
    // Scenario 2: tool error handled gracefully
    failingTool := tool.New("fail").Action(func(...) (any, error) {
        return nil, errors.New("tool failed")
    })
    result, err = agent.Run(ctx, messages)
    assert.NoError(t, err) // Error returned in result, not as Go error
    assert.Contains(t, result.Content, "error")
}
```

### User Story 3: Streaming

```go
func TestUserStory3_Streaming(t *testing.T) {
    // Scenario 1: receive channel of StreamEvents
    events, err := agent.Stream(ctx, messages)
    assert.NoError(t, err)
    
    var received []string
    for event := range events {
        if event.Type == StreamEventContentDelta {
            received = append(received, event.Delta)
        }
    }
    assert.NotEmpty(t, received)
    
    // Scenario 2: context cancellation closes stream cleanly
    ctx, cancel := context.WithCancel(context.Background())
    events, _ = agent.Stream(ctx, messages)
    cancel()
    _, ok := <-events
    assert.False(t, ok) // Channel closed
}
```

### User Story 4: Memory

```go
func TestUserStory4_Memory(t *testing.T) {
    mem := inmemory.New()
    agent := agent.New(provider, agent.WithMemory(mem))
    
    // Scenario 1: messages persisted after run
    agent.Run(ctx, []Message{{Role: "user", Content: "Hi"}}, agent.WithSession("user1", "sess1"))
    entries, _ := mem.Get(ctx, "user1", "sess1", 10)
    assert.Len(t, entries, 2) // user + assistant
    
    // Scenario 2: previous context included
    agent.Run(ctx, []Message{{Role: "user", Content: "Follow up"}}, agent.WithSession("user1", "sess1"))
    entries, _ = mem.Get(ctx, "user1", "sess1", 10)
    assert.Len(t, entries, 4) // 2 turns
}
```

### User Story 5: HTTP

```go
func TestUserStory5_HTTP(t *testing.T) {
    handler := http.NewHandler(agent)
    server := httptest.NewServer(handler)
    defer server.Close()
    
    // Scenario 1: POST /run returns JSON RunResult
    resp, _ := http.Post(server.URL+"/run", "application/json", 
        strings.NewReader(`{"messages":[{"role":"user","content":"Hi"}]}`))
    assert.Equal(t, 200, resp.StatusCode)
    
    // Scenario 2: POST /stream returns SSE events
    resp, _ = http.Post(server.URL+"/stream", "application/json", body)
    assert.Equal(t, 200, resp.StatusCode)
    assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
}
```

---

## Risk Mitigation

| Risk | Mitigation | Phase |
|------|------------|-------|
| API changes in providers | Version-locked fixtures, interface abstraction | 1, 8 |
| Memory leaks in streaming | Channel cleanup tests, defer patterns | 1, 3 |
| Race conditions | sync primitives, go test -race | All |
| Schema validation complexity | Use encoding/json, keep minimal | 2 |
| Context propagation gaps | Audit all goroutines, document patterns | 3, 5 |

---

## Dependencies

- Go 1.22+ (generics, enhanced loops)
- Standard library only for core
- Optional: SQLite driver for sqlite backend
- Optional: MongoDB driver for mongodb backend

---

## Checklist: Spec Coverage

| Spec Section | Covered in Phase |
|--------------|------------------|
| User Story 1 (P1) | Phase 1, 3 |
| User Story 2 (P1) | Phase 2, 3 |
| User Story 3 (P2) | Phase 1, 3 |
| User Story 4 (P2) | Phase 5 |
| User Story 5 (P3) | Phase 6 |
| FR-001 | Phase 1, 8 |
| FR-002 | Phase 1, 3 |
| FR-003 | Phase 2, 3 |
| FR-004 | Phase 3 |
| FR-005 | Phase 5 |
| FR-006 | Phase 6 |
| FR-007 | Phase 4 |
| FR-008 | Phase 3 |
| FR-009 | Phase 2 |
| FR-010 | Phase 1, 3, 5 |
| FR-011 | Phase 1, 8 |
| FR-012 | Phase 1, 8 |
| SC-001 | Phase 3 |
| SC-002 | Phase 3 |
| SC-003 | Phase 5 |
| SC-004 | go.mod |
| SC-005 | All phases |
| SC-006 | Phase 6 |
| SC-007 | Phase 1, 8 |
| Edge: Rate limit | Phase 1 |
| Edge: MaxSteps | Phase 3 |
| Edge: Schema validation | Phase 2 |
| Edge: Context cancellation | Phase 3 |
