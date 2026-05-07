# Testing `examples/local` end-to-end

This skill covers runtime testing of the `examples/local` dev server, including
the human-in-the-loop (HIL) flows on AG-UI and AI SDK.

## Devin Secrets Needed

- `GEMINI_API_KEY` — required to actually exercise chat streaming and tool
  calls. Saved at user scope. The example also accepts `OPENAI_API_KEY` as an
  alternative.

## Bringing the example up

```bash
export PATH=$PATH:/usr/local/go/bin
cd /home/ubuntu/repos/rakit/examples/local
go build -o /tmp/rakit-local .
mkdir -p /tmp/rakit-data && cd /tmp/rakit-data
/tmp/rakit-local > /tmp/rakit-server.log 2>&1 &
```

- Server listens on `:8080` (hardcoded in `main.go`).
- Dashboard at `http://localhost:8080/` (single-page app embedded in the
  binary via `//go:embed`).
- SQLite + blob store data is rooted at the cwd's `./data/` — start the
  server from a writable directory.
- Model names default to `gpt-4o-mini` / `gemini-2.5-flash` and can be
  overridden via `OPENAI_MODEL` / `GEMINI_MODEL`. Per `AGENTS.md`, do NOT
  hardcode `gpt-*` / `gemini-*` strings in tests or examples.

Sanity check the server is up:

```bash
curl -sS http://localhost:8080/api/v1/sessions   # → {"sessions":[]}
```

## Testing UI vs shell

**Chrome silently exits in this VM** (`exit 7`, empty `chrome.log`) regardless
of flags or user-data-dir. If Chrome won't launch, do NOT try to record a UI
session — pivot to shell-based adversarial testing of raw SSE bytes. For HIL
spec compliance this is actually a stronger proof than a UI screenshot since
the key claim is wire-format conformance.

If Chrome works in a future VM, the dashboard renders interrupt cards with
**Approve** / **Reject** buttons (data attributes `data-hil-action` and
`data-tool-id`). `browser_time` auto-resolves with `{iso, timezone, epochMs}`
in `resolveClientSide()` (`examples/local/index.html` ~line 1246).

## Protocol selection

The `/chat` handler negotiates by `Accept` header:

| Accept value          | Protocol  | Content-Type response          |
|-----------------------|-----------|--------------------------------|
| `text/vnd.ag-ui`      | AG-UI     | `text/event-stream`            |
| `text/vnd.ai-sdk`     | AI SDK    | `text/plain; charset=utf-8`    |
| `text/event-stream`   | default (AI SDK in `examples/local`) | depends on registry default |

Do NOT pass plain `Accept: text/event-stream` and expect AG-UI — you'll get
the registry default (AI SDK in this example).

The response always includes the `X-Session-Id` header (also exposed via
`Access-Control-Expose-Headers`). Capture it with `curl -D /tmp/headers`.

## HIL wire-format testing recipe

Gated tools (`delete_item`) and client-side tools (`browser_time`) both
emerge as **interrupts** at the end of a run.

### Pause turn (any protocol)

```bash
curl -sS -N -D /tmp/agui.headers \
  -H 'Accept: text/vnd.ag-ui' \
  -H 'Content-Type: application/json' \
  -X POST http://localhost:8080/chat \
  -d '{"message":"Please delete item with id \"42\"","userId":"default"}' \
  > /tmp/agui-pause.sse
```

Extract sessionId + interruptId for the resume turn:

```bash
SESS=$(grep -i '^X-Session-Id: ' /tmp/agui.headers | awk '{print $2}' | tr -d '\r')
INTR=$(grep -oE '"id":"intr-[a-f0-9]+"' /tmp/agui-pause.sse | head -1 | cut -d'"' -f4)
```

### Resume turn (same `/chat` envelope, no rakit-specific route)

Approve:

```bash
curl -sS -N -H 'Accept: text/vnd.ag-ui' -H 'Content-Type: application/json' \
  -X POST http://localhost:8080/chat \
  -d "{\"sessionId\":\"$SESS\",\"userId\":\"default\",\"resume\":[{\"interruptId\":\"$INTR\",\"status\":\"resolved\",\"payload\":{\"approved\":true}}]}"
```

Reject: same envelope, `"approved":false`. Server synthesises
`{"error":"user rejected"}` as the tool result (`agent/resume.go`).

Client-side tools: send `"payload":{"output":...}` or any other JSON shape;
the runner uses the payload verbatim as the tool result.

### Spec assertions worth checking (PR #10 made these true)

- AG-UI `RUN_FINISHED` carries `"outcome":"interrupt"` + `"interrupts":[{...}]`
  (camelCase per the [Interrupt-Aware Run Lifecycle draft](https://docs.ag-ui.com/drafts/interrupts)).
- AI SDK pause stream emits `tool-input-available` with NO follow-up
  `tool-output-available`. Exactly one trailing `data: [DONE]`. Earlier
  versions had a duplicate-sentinel bug — `grep -c '^data: \[DONE\]$' == 1`.
- Approve resume returns a non-error `TOOL_CALL_RESULT`; reject returns one
  containing the literal `user rejected`.
- Note on `delete_item`: the registered HTTP handler points at
  `https://httpbin.org/post` with `ResponseField:"json"`, so the runtime
  result is the unwrapped echoed input (`{"id":"42"}`), not the full httpbin
  envelope. Don't assert on `httpbin.org/post` in the result body — assert on
  the echoed args instead.

## Build / test / lint commands

From `AGENTS.md`:

```bash
make test-race   # canonical pre-commit check
make vet
make lint        # requires golangci-lint
```

Run unit tests from the repo root, not from a subpackage. Tests use
race-detector by default in CI.