# Developer notes for AI coding agents

This file exists for AI coding agents (Devin, Cursor, Codex, etc.) working on
this codebase. Prefer the prose here over re-deriving conventions from the code.

## Layout

- `agent/` — core `Agent` type, session runner, compaction, hooks.
- `provider/` — `Provider` interface + OpenAI and Gemini implementations.
- `protocol/` — event types and AG-UI / AI SDK streaming protocols.
- `tool/` — `Tool` interface, `Registry`, `FunctionTool`.
- `skill/` — three-layer skill system: `Entry` (L1), `Definition` (L2), `ResourceManager` (L3).
- `mcp/` — Model Context Protocol client with pluggable HTTP / SSE transports.
- `storage/metadata/` — `Store` interface + SQLite / Firestore / MongoDB adapters.
- `storage/blob/` — `BlobStore` interface + local / S3 / Firebase adapters.
- `examples/local/` — end-to-end dev server (HTTP, admin API, embedded UI).
- `examples/cloud-run/` — Cloud Run deployment example.

## Build / test / lint

```bash
make test-race   # canonical pre-commit check
make vet
make lint        # requires golangci-lint
```

## Conventions

- All exported symbols must have a doc comment.
- Prefer narrow interfaces over concrete struct parameters.
- Tool results MUST use `tool.Ok` / `tool.Err` rather than constructing a
  `tool.Result` directly — these set `ExecutedAt` and `Duration` via
  `tool.Measure`.
- Tool calls across providers are keyed by the OpenAI-style `tool_call_id`.
  Always propagate `ToolCall.ID` end-to-end (runner -> provider -> result).
- Registries (`tool.Registry`, `protocol.Registry`) are concurrency-safe and
  may be read from any goroutine.

## Adding a provider

1. Implement `provider.Provider` — `Name`, `Model`, `SetModel`, `Models`,
   `Stream`, `Generate`.
2. `Stream` must close the event channel exactly once and emit a final
   `DoneProviderEvent` (or `ErrorProviderEvent`).
3. Honour `req.System` by emitting a system/instruction message for the
   provider.
4. Map assistant messages with tool_calls to whatever shape the provider
   requires, and map tool result messages back using `ToolCall.ID`.

## Adding a storage adapter

Implement `storage/metadata.Store` in full. There is no optional method —
session, tool, skill, scoped memory, legacy KV, and MCP server operations must
all work. Scoped memory keys are built with `metadata.ScopedKey`; all adapters
MUST use this helper to stay consistent.

## Do not

- Do not commit API keys or `.env` files.
- Do not add `gpt-*` / `gemini-*` model names inline in examples — read them
  from env or admin config so the code keeps working when models rotate.
- Do not skip hooks or amend commits during contributions.
