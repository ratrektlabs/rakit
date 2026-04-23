# Human-in-the-Loop (HIL) — Design Proposal

**Status:** Draft · awaiting review before implementation
**Branch:** `devin/1776965021-hil-redesign-docs`
**Related spec:** [AG-UI Interrupt-Aware Run Lifecycle (draft)](https://docs.ag-ui.com/drafts/interrupts), [AI SDK v5 Chatbot Tool Usage](https://ai-sdk.dev/docs/ai-sdk-ui/chatbot-tool-usage)

---

## 1. Why we're rewriting this

A prior attempt (PR #4, closed) bolted HIL onto rakit by inventing a new
`tool_call_pending` event and piping it through both encoders. That failed
the requirement in two ways:

1. **It was a custom protocol extension.** AG-UI has no `TOOL_CALL_PENDING`
   event and AI SDK has no `tool-call-pending` part. Encoding HIL through
   bespoke wire types forces every client to learn a rakit-specific contract
   instead of the native spec one.
2. **It pushed HIL state into the core agent loop via `protocol.Event`.** The
   runner at `agent/runner.go:11` imports `protocol` directly and builds
   `protocol.RunStartedEvent`, `protocol.ToolCallStartEvent`, etc. There is
   no abstraction between "what the agent did" and "how we serialize it over
   the wire" — so every new domain concept (interrupts, resumes, checkpoints)
   has to be threaded through the single `protocol.Event` union, which then
   leaks into every encoder.

This document proposes a redesign that:

- **Treats HIL as a domain-level concept** belonging to `agent/`, expressed
  through a small `Interrupt` / `Resume` vocabulary that matches the AG-UI
  draft spec 1:1.
- **Maps to AG-UI and AI SDK using only the primitives those specs already
  define.** No custom event types. No custom `data-*` parts for anything
  that can be expressed natively.
- **Decouples the core agent loop from any specific protocol** by introducing
  a thin `agent.Stream` abstraction. Encoders observe the stream and
  translate into their native wire format.

We are not shipping code in this PR — only the design. Implementation follows
a separate PR after sign-off.

---

## 2. Principles

1. **Spec-native mapping, always.** If AG-UI or AI SDK already defines a
   native primitive for a HIL concept, we use it. We never invent a wire
   type that duplicates or competes with a native primitive.
2. **One domain model, many encodings.** The agent loop speaks in domain
   terms (`Interrupt`, `Resume`, `RunOutcome`). Encoders translate.
   Neither protocol package can leak back into `agent/`.
3. **Terminal-model interrupts, per AG-UI draft.** An interrupt closes the
   run. A resume starts a new run that references the previous interrupts
   by ID. This matches the AG-UI draft and also matches the natural
   behavior of stateless HTTP sessions.
4. **HIL is opt-in and additive.** Agents that don't use interrupts behave
   exactly as they do today. No API breaks.
5. **No session-level hidden state.** Everything the resumed run needs —
   partial tool calls, pending interrupt IDs, accumulated messages — is
   persisted via the existing `metadata.Store` contract or emitted via
   `MESSAGES_SNAPSHOT` / `STATE_SNAPSHOT` at the interrupt boundary. This
   matches AG-UI's "resume-mode-agnostic" requirement.

---

## 3. Current coupling and what we'll break

### 3a. `agent/` imports `protocol/`

`agent/runner.go` constructs `protocol.*Event` values and pushes them onto
a `chan protocol.Event`. Seven separate event types are constructed
inline. Consequences:

- The runner must know which protocol concept it's producing (e.g.
  `TextStart` vs `TextDelta`), even though neither is an agent concern —
  they're presentation artefacts of streaming.
- Adding a domain concept (interrupts, reasoning, partial state) requires
  adding a new `protocol.Event` variant and updating every encoder — even
  ones that don't care.
- The runner package tests import `protocol`, which drags in encoder
  internals.

### 3b. `skill/handlers.go` knows nothing about HIL

Handlers today have exactly three kinds: `http`, `mcp`, `function`. A
tool call either runs inline in the runner (via `tool.Registry`) or is
rejected. There is no spot for "this one needs human approval" or "this
one runs in the client's browser".

### 3c. `metadata.ToolCallRecord` has no `Status`

Sessions record tool calls only after they complete. There is no way to
persist a pending call across a process restart, which blocks any
resume-based HIL design.

---

## 4. Proposed core domain (`agent/`)

All additions. No removals. The existing channel-of-events return from
`Run*` is retained for back-compat; a new parallel API returns a richer
`RunResult`.

```go
// agent/hil.go (new)

// Interrupt is a protocol-agnostic request for human (or client) input
// that pauses the agent loop. It intentionally mirrors the AG-UI draft
// Interrupt type so the AG-UI encoding is a direct 1:1 mapping.
//
// Reason taxonomy matches the AG-UI draft:
//   - "tool_call"      — bound to a specific tool call awaiting decision/result
//   - "input_required" — agent needs structured input
//   - "confirmation"   — free-standing yes/no decision
//   - "<namespace>:<x>" — framework-specific (e.g. "rakit:subagent_wait")
type Interrupt struct {
    ID             string            // stable correlation key
    Reason         string            // see taxonomy above
    Message        string            // human-readable fallback prompt
    ToolCallID     string            // set iff Reason == "tool_call"
    ResponseSchema map[string]any    // JSON Schema describing expected Payload
    ExpiresAt      time.Time         // zero = no expiry
    Metadata       map[string]any    // framework-specific
}

// ResumeInput is what the caller passes back in to resolve one or more
// open interrupts on a session. It matches AG-UI's RunAgentInput.resume[]
// element verbatim.
type ResumeInput struct {
    InterruptID string         // must match an open interrupt on the thread
    Status      ResumeStatus   // "resolved" or "cancelled"
    Payload     any            // validated against the interrupt's ResponseSchema
}

type ResumeStatus string
const (
    ResumeResolved  ResumeStatus = "resolved"
    ResumeCancelled ResumeStatus = "cancelled"
)

// RunOutcome is the terminal state of a run.
type RunOutcome string
const (
    OutcomeSuccess   RunOutcome = "success"
    OutcomeInterrupt RunOutcome = "interrupt"
    OutcomeError     RunOutcome = "error"
)

// RunResult is what a caller gets from the new typed API. The event
// channel still fires during the run (for streaming), but the terminal
// result is structured instead of just "channel closed".
type RunResult struct {
    ThreadID   string
    RunID      string
    Outcome    RunOutcome
    Interrupts []Interrupt     // present iff Outcome == OutcomeInterrupt
    Error      error           // present iff Outcome == OutcomeError
}
```

### Tool classification

A tool becomes HIL-sensitive through one of two opt-in markers, both
implemented as interfaces so any caller can supply its own tool:

```go
// agent/hil.go (continued)

// ApprovalPolicy decides whether a given tool call must pause for human
// approval before execution.
type ApprovalPolicy interface {
    RequiresApproval(tc provider.ToolCall) bool
}

// ClientSide marks a tool as client-executed. The runner emits an
// Interrupt with Reason="tool_call" and does NOT invoke the tool itself;
// the caller is expected to supply a ResumeInput with the tool's result.
type ClientSide interface {
    ClientSide() bool
}
```

Both are discovered by the runner via interface assertion on values
fetched from `tool.Registry`. Existing tools are unaffected because
neither interface is required.

### The runner's pause point

Inside the agentic loop, after the provider finishes a turn and we have
the full set of requested `ToolCall`s, classify each one:

| Classification      | Condition                                                                                    | Behaviour                                                                                              |
| ------------------- | -------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| executable          | neither `ApprovalPolicy.RequiresApproval(tc)` nor `tool.ClientSide() == true`                | run inline, emit `ToolResultEvent`                                                                     |
| approval-gated      | `ApprovalPolicy.RequiresApproval(tc) == true`                                                | do NOT run; persist `ToolCallRecord{Status: "pending_approval"}`; accumulate `Interrupt{Reason:"tool_call", ...}` |
| client-side         | `tool.ClientSide() == true`                                                                  | do NOT run; persist `ToolCallRecord{Status: "pending_client"}`; accumulate `Interrupt{Reason:"tool_call", ...}`  |

If any interrupts are accumulated after the classification pass, the
runner:

1. Emits any pending `MESSAGES_SNAPSHOT` / `STATE_SNAPSHOT` frames (AG-UI
   contract rule; harmless for AI SDK).
2. Emits a terminal marker through the domain stream — not a protocol
   event — carrying the interrupt list.
3. Closes the channel.

Resume:

1. Caller invokes `Agent.Resume(ctx, sessionID, []ResumeInput, protocol)`.
2. Runner validates every open interrupt is addressed (AG-UI contract
   rule 3).
3. For each `InterruptID`, the runner either (a) runs the approved tool
   server-side, (b) accepts the client-provided result verbatim, or (c)
   synthesizes a rejection payload and a `tool` message so the provider
   sees the decision.
4. The agentic loop resumes with the synthesized tool-result messages
   already in context; it's indistinguishable from a normal loop tick
   to the provider.

---

## 5. Decoupling the runner from the protocol

We introduce one new abstraction that `agent/` owns and `protocol/` depends
on — never the other way around. Implementation outline:

```go
// agent/stream.go (new)

// Event is the protocol-agnostic event type produced by a run. Encoders
// translate these into their native wire formats.
type Event interface {
    sealed() // marker, prevents third-party impls
}

// Existing protocol.Event variants get shadow types under agent/ that
// carry the same fields. The runner emits agent.* types, never protocol.*.
// A trivial adapter at the agent/protocol boundary translates
// agent.Event -> protocol.Event for back-compat callers.
type TextDelta struct { MessageID, Delta string }
type ToolCallStart struct { ToolCallID, ToolName string }
// ... etc
type InterruptRaised struct { Interrupts []Interrupt }
type RunFinishedResult struct { Result any } // Optional, per AG-UI draft
```

With this split:

- `agent/runner.go` loses `import "github.com/ratrektlabs/rakit/protocol"`.
- The protocol package becomes a pure adapter: `agui.Encode(agent.Event)`,
  `aisdk.Encode(agent.Event)`. Each encoder is free to drop or collapse
  events that don't map cleanly onto its spec (e.g. AI SDK doesn't have a
  reasoning part set, AG-UI doesn't use `data-*` parts).
- Adding a new protocol (LangGraph stream, Anthropic stream, etc.) is a
  new encoder in its own package. No changes to the runner.
- Tests for the runner stop depending on encoder internals.

We keep the existing `protocol.Event` types for one release as re-exports
from `agent`, so `protocol` stays compatible. They become deprecated in
the release after.

---

## 6. AG-UI encoding (spec-native, no extensions)

The AG-UI draft gives us an exact 1:1 target. Encoder behaviour:

### Before the pause

Everything the runner emits flows through the existing AG-UI encoder
unchanged. For each pending tool call we still emit
`TOOL_CALL_START` + `TOOL_CALL_ARGS` so the client renders a partial tool
card.

### At the pause

The encoder emits, in order:

1. (Optional) `MESSAGES_SNAPSHOT` with the full message history so far.
2. (Optional) `STATE_SNAPSHOT` for any agent state the client depends on.
3. `RUN_FINISHED` with:
   ```json
   {
     "type": "RUN_FINISHED",
     "threadId": "...",
     "runId": "...",
     "outcome": "interrupt",
     "interrupts": [
       {
         "id": "<ulid>",
         "reason": "tool_call",
         "message": "delete_item requires human approval",
         "toolCallId": "<ID>",
         "responseSchema": { "type": "object", "properties": { "approved": { "type": "boolean" } } }
       }
     ]
   }
   ```

This is exactly the shape defined in the [AG-UI draft §Updates to
`RUN_FINISHED`](https://docs.ag-ui.com/drafts/interrupts#updates-to-run_finished).
No new event types. No `CUSTOM`. No rakit-specific fields at the
top-level.

### On resume

The AG-UI input envelope is `RunAgentInput`. The draft spec adds:

```json
{
  "threadId": "...",
  "resume": [
    { "interruptId": "<id>", "status": "resolved", "payload": { "approved": true } }
  ]
}
```

Our HTTP surface exposes this via a single endpoint per the AG-UI draft
— **not** a rakit-specific `/chat/resume`. The endpoint is the same one
that accepts normal `RunAgentInput`; presence of `resume[]` is what
distinguishes a resume from a fresh turn. This matches AG-UI contract
rule 2.

Error paths map onto `RUN_ERROR` only — no new error events. Specifically:

- Expired resume, unknown interrupt ID, schema mismatch, partial resume —
  all produce `RUN_ERROR` per AG-UI contract rules 5–8.

---

## 7. AI SDK encoding (spec-native, no `data-*` abuse)

The AI SDK doesn't have a first-class "interrupt" construct. It does,
however, have a native HIL primitive: **a tool with no `execute` function
is automatically a user-interaction tool.** The AI SDK emits the tool
call parts (`tool-input-start`, `tool-input-delta`, `tool-input-available`)
and stops the stream cleanly. The client renders the tool call, collects
user input, calls `addToolOutput`/`addToolResult`, then re-submits the
conversation. `sendAutomaticallyWhen: lastAssistantMessageIsCompleteWithToolCalls`
then closes the loop.

**We piggyback on this, not around it.**

### Tool-bound interrupts (`reason: "tool_call"`)

Emit exactly the parts the AI SDK already defines:

```
{"type":"tool-input-start","toolCallId":"<id>","toolName":"delete_item"}
{"type":"tool-input-delta","toolCallId":"<id>","delta":"{\"id\":\"42\"}"}
{"type":"tool-input-available","toolCallId":"<id>","input":{"id":"42"}}
```

Then end the stream cleanly — **no `tool-output-available` part**, because
the tool hasn't run. This is exactly how Vercel's own
`askForConfirmation` example terminates; the client knows to render a UI
because there's no output part.

On resume, the client calls `addToolResult(...)` which submits the
conversation back. The rakit AI SDK decoder sees a UI message whose last
assistant part is `tool-output-available` with our `toolCallId`, maps
that back into a `ResumeInput{InterruptID, Status: resolved, Payload:
output}`, and feeds it to `Agent.Resume(...)`.

**No wire extension.** We already emit all three of those part types for
normal server-side tools; the "stop before output" behaviour is the only
difference.

### Non-tool interrupts (`reason: "input_required"` or `"confirmation"`)

The AI SDK has no native "request input" primitive outside of a tool. The
spec-compliant workaround is to use a dynamic tool — which the AI SDK
[explicitly supports](https://ai-sdk.dev/docs/reference/ai-sdk-core/dynamic-tool)
for exactly this kind of runtime-defined tool. We synthesize a tool call
targeting a built-in rakit tool name (e.g. `ag_ui_interrupt`) and encode
the interrupt's `responseSchema` as the tool's `inputSchema`:

```
{"type":"tool-input-start","toolCallId":"<id>","toolName":"ag_ui_interrupt"}
{"type":"tool-input-available","toolCallId":"<id>","input":{"reason":"confirmation","message":"Proceed?"}}
```

The UI renders it the same way it renders any tool call without an
output. The client responds with `addToolResult({approved: true})`,
which we decode back into a `ResumeInput` with `Status: "resolved"` and
that payload.

Why a dynamic tool rather than `data-*`? Because AI SDK clients already
render tool parts natively; `data-*` parts are for out-of-band structured
data and require client-side renderers. Reusing the tool part keeps the
default UI working end-to-end.

### Cancel / interrupt mid-run

AI SDK's native `abort` on the `useChat` hook closes the stream. Our
encoder treats stream cancellation as an implicit `ResumeCancelled`
candidate — pending tool calls stay in `pending_*` status, and the next
user message (a new `streamText` call with the same thread) either
provides missing results via `tool-output-available` (resolved) or omits
them (we materialize `Status: "cancelled"` on the corresponding
interrupts).

---

## 8. Storage impact

Two additive changes to `metadata.Store`:

1. `ToolCallRecord.Status` becomes `"completed" | "pending_approval" | "pending_client" | "failed"`. Existing records migrate to `"completed"`.
2. A new `Session.OpenInterrupts []Interrupt` slice is persisted across the
   pause. On resume we validate inbound `ResumeInput` against this list,
   which implements AG-UI contract rule 3 ("cover all open interrupts").

All adapters (sqlite / firestore / mongodb) need a trivial migration
column. Tests get expanded to cover pending-then-completed transitions.

---

## 9. HTTP surface for `examples/local`

Changes in the demo server are guided by the principle that the wire
contract is owned by the protocol spec, not by rakit.

- **AG-UI** (`Accept: text/vnd.ag-ui`): one endpoint `POST /chat`, input is
  `RunAgentInput` (with optional `resume[]` per draft). No
  `/chat/resume`, no `/chat/interrupt`. `Interrupt` delivery is
  inside `RUN_FINISHED`.
- **AI SDK** (`Accept: application/json`): one endpoint `POST /chat`, input
  is the standard AI SDK UI messages envelope. Resume happens via
  `addToolResult(...)` on the client submitting a new message — no
  rakit-specific endpoint.
- **Generic interrupt** (not tied to either protocol): `POST /chat/interrupt`
  remains, but becomes a thin operational utility rather than part of
  the HIL contract. It just cancels the run's context; pending
  interrupts survive and are visible on the next turn.

This removes the rakit-specific `/chat/resume` endpoint that PR #4
introduced.

---

## 10. Frontend impact (`examples/local/index.html`)

- Parse `RUN_FINISHED.outcome === "interrupt"` and render a card per
  interrupt using `message` + `responseSchema` (falling back to raw JSON
  if no schema is provided).
- For AI SDK, honour the dangling `tool-input-available` without
  `tool-output-available` as the signal to render an approval or input
  card.
- Replace the custom resume POST with a standard `RunAgentInput` POST
  containing `resume[]` for AG-UI, and `addToolResult` + new message for
  AI SDK.

---

## 11. Migration & backward compatibility

- `Agent.Run*` returning a `chan protocol.Event` stays. The new
  `Agent.RunTyped*` variant returns `(chan agent.Event, *RunResult, error)`.
- `protocol.Event` types are re-exported from `agent/` for one release.
- Any caller that does not use the `ApprovalPolicy` or `ClientSide`
  interfaces, nor inspects `RunResult`, sees zero behaviour change.
- The previously shipped custom `tool_call_pending` events (from PR #4)
  are not present on `main` — there is nothing to migrate away from in
  the codebase, only in any downstream fork that merged PR #4 before it
  was closed. A note will be added to the changelog.

---

## 12. Open questions for reviewer

1. **`RunFinishedResult.Result` type.** The AG-UI draft allows `any`. Do
   we want to constrain it to a specific shape for rakit, or mirror the
   draft exactly? Proposal: mirror exactly.
2. **`ag_ui_interrupt` tool name for AI SDK non-tool interrupts.** The
   name needs to be stable and namespaced. Proposal:
   `rakit_interrupt_<reason>`.
3. **`ApprovalPolicy` vs. per-tool marker interface.** We could also
   express approval-gating through an interface on the tool itself
   (`RequiresApproval() bool`) instead of a separate policy. The policy
   form lets policies be dynamic (e.g. "approval if args.amount >
   10000"), which the marker form doesn't. Proposal: ship the policy,
   and also support the marker as a convenience for the static case.
4. **Persisting open interrupts.** Do we persist them on the session
   itself, or create a separate `interrupts` table? Proposal: session
   field first; promote to its own table if we add multi-session
   aggregation queries.

---

## 13. Rollout plan

Four PRs, each reviewable on its own:

1. **This PR** — design doc only. No code changes. Goal: align on the
   approach before touching code.
2. **Decoupling** — introduce `agent.Event`, rewire the runner to emit it,
   and retrofit encoders. No HIL behaviour yet; pure refactor.
3. **HIL core** — `Interrupt`, `Resume`, `ApprovalPolicy`, `ClientSide`,
   runner classification pass, `metadata` additions. No FE changes, AI
   SDK encoder still emits dangling `tool-input-available`, AG-UI
   encoder emits draft-spec `RUN_FINISHED outcome:"interrupt"`. Unit
   tests only.
3a. (Optional) LangGraph interop test — prove that a rakit run with an
    interrupt can drive a CopilotKit UI unchanged.
4. **Examples & docs** — update `examples/local` FE + backend, update
   `README.md`, add a walkthrough doc.

Each PR is mergeable independently. This PR is a prereq for (2)–(4).
