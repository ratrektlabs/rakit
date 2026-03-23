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

## Phase Breakdown

### Phase 1: Provider Component (Days 1-2)

**Goal**: LLM abstraction layer with OpenAI implementation

| Task | Files | Description |
|------|-------|-------------|
| P1.1 | `provider/provider.go` | Define Provider interface, CompletionRequest, CompletionResponse, StreamEvent, Message, ToolCall, ProviderCapabilities types |
| P1.2 | `provider/provider.go` | Implement stream channel patterns, error wrapping utilities |
| P1.3 | `provider/openai/openai.go` | OpenAI Complete() implementation using net/http |
| P1.4 | `provider/openai/openai.go` | OpenAI Stream() implementation with SSE parsing |
| P1.5 | `provider/provider_test.go` | Interface contract tests with mock provider |
| P1.6 | `provider/openai/openai_test.go` | OpenAI tests with recorded HTTP fixtures |

**Acceptance Criteria**:
- [ ] Provider interface compiles with all methods
- [ ] OpenAI Complete() returns valid responses
- [ ] OpenAI Stream() yields events and closes channel
- [ ] All tests pass with `go test ./provider/...`

---

### Phase 2: Tool Component (Days 2-3)

**Goal**: Executable function registry with fluent builder

| Task | Files | Description |
|------|-------|-------------|
| T2.1 | `tool/tool.go` | Define Tool interface, ToolRegistry interface |
| T2.2 | `tool/tool.go` | Implement registry with sync.Map for thread-safety |
| T2.3 | `tool/builder.go` | Fluent ToolBuilder with Param(), Action(), Build() |
| T2.4 | `tool/tool.go` | ToolDefinition conversion for provider tools |
| T2.5 | `tool/tool_test.go` | Registry tests, builder tests, validation tests |

**Acceptance Criteria**:
- [ ] Tool interface with Name, Description, Parameters, Execute
- [ ] Registry Register/Get/List/ToProviderTools working
- [ ] Fluent builder generates valid tools
- [ ] Thread-safe registry passes concurrent tests

---

### Phase 3: Agent Component (Days 3-5)

**Goal**: Orchestration engine with tool-calling loop

| Task | Files | Description |
|------|-------|-------------|
| A3.1 | `agent/agent.go` | Define Agent interface (Run, Stream, AddTool, AddSkill) |
| A3.2 | `agent/agent.go` | AgentConfig struct with defaults |
| A3.3 | `agent/options.go` | Functional options (WithTools, WithSkills, WithMaxSteps, WithSession) |
| A3.4 | `agent/agent.go` | Run() implementation with tool-calling loop |
| A3.5 | `agent/agent.go` | Stream() implementation with event forwarding |
| A3.6 | `agent/agent.go` | MaxSteps enforcement, context cancellation |
| A3.7 | `agent/agent_test.go` | Run loop tests, tool execution tests, error handling tests |

**Acceptance Criteria**:
- [ ] Agent.Run() executes complete tool-calling loops
- [ ] Agent.Stream() forwards provider events + tool events
- [ ] MaxSteps prevents infinite loops
- [ ] Context cancellation stops execution immediately

---

### Phase 4: Skill Component (Day 5)

**Goal**: Tool bundles with instructions

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

### Phase 5: Memory Component (Days 6-7)

**Goal**: Conversation persistence with multiple backends

| Task | Files | Description |
|------|-------|-------------|
| M5.1 | `memory/memory.go` | Define Memory interface, Entry struct, MemoryBackend interface |
| M5.2 | `memory/inmemory/inmemory.go` | Thread-safe in-memory implementation with sync.RWMutex |
| M5.3 | `memory/sqlite/sqlite.go` | SQLite backend using database/sql |
| M5.4 | `memory/memory_test.go` | Backend-agnostic interface tests |
| M5.5 | `memory/inmemory/inmemory_test.go` | InMemory specific tests |

**Acceptance Criteria**:
- [ ] Memory interface Add/Get/Search/Clear working
- [ ] InMemory backend thread-safe
- [ ] Get returns entries in chronological order
- [ ] Search returns empty if unsupported

---

### Phase 6: HTTP Handler (Days 8-9)

**Goal**: Web exposure with framework adapters

| Task | Files | Description |
|------|-------|-------------|
| H6.1 | `http/handler.go` | Handler interface extending http.Handler |
| H6.2 | `http/handler.go` | POST /run, POST /stream (SSE), GET /tools, POST /tools, GET /health |
| H6.3 | `http/handler.go` | CORS support, JSON request/response |
| H6.4 | `http/adapters/gin.go` | Gin adapter function |
| H6.5 | `http/adapters/echo.go` | Echo adapter function |
| H6.6 | `http/handler_test.go` | httptest-based endpoint tests |

**Acceptance Criteria**:
- [ ] All endpoints functional via http.Handler
- [ ] SSE streaming for /stream endpoint
- [ ] CORS headers present
- [ ] Framework adapters delegate correctly

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

### Phase 8: Additional Providers (Day 10)

**Goal**: Anthropic, Gemini, Zai implementations

| Task | Files | Description |
|------|-------|-------------|
| P8.1 | `provider/anthropic/anthropic.go` | Anthropic Complete/Stream with Claude API format |
| P8.2 | `provider/gemini/gemini.go` | Gemini Complete/Stream with Google AI format |
| P8.3 | `provider/zai/zai.go` | Zai Complete/Stream |
| P8.4 | `provider/*/ *_test.go` | Provider-specific tests with fixtures |

**Acceptance Criteria**:
- [ ] All providers implement Provider interface
- [ ] Streaming works for all providers
- [ ] Tool calling supported where available

---

### Phase 9: Examples & Documentation (Day 10)

**Goal**: Usage examples and quickstart

| Task | Files | Description |
|------|-------|-------------|
| E9.1 | `examples/basic/main.go` | Simple agent with one tool |
| E9.2 | `examples/streaming/main.go` | Streaming example with event handling |
| E9.3 | `examples/web-demo/main.go` | HTTP server example |
| E9.4 | `README.md` | Installation, quickstart, API overview |

**Acceptance Criteria**:
- [ ] All examples compile and run
- [ ] README covers core usage patterns

---

## Testing Strategy

| Level | Scope | Tools |
|-------|-------|-------|
| Unit | Individual functions, builders | go test, table-driven |
| Contract | Interface compliance | Mock implementations |
| Integration | Provider HTTP calls | httptest, recorded fixtures |
| E2E | Full agent loops | Example programs |

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| API changes in providers | Version-locked fixtures, interface abstraction |
| Memory leaks in streaming | Channel cleanup tests, defer patterns |
| Race conditions | sync primitives, go test -race |

## Dependencies

- Go 1.22+ (generics, enhanced loops)
- Standard library only for core
- Optional: SQLite driver for sqlite backend
- Optional: MongoDB driver for mongodb backend
