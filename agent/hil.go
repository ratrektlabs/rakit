package agent

import (
	"strings"
	"time"
)

// RunOutcome is the terminal state of an agent run.
//
// An empty value is treated as [OutcomeSuccess] for backward compatibility.
type RunOutcome string

const (
	// OutcomeSuccess means the run completed without an interrupt or error.
	OutcomeSuccess RunOutcome = "success"
	// OutcomeInterrupt means the run paused to wait for a human or client.
	// The caller must resolve every open interrupt via [Agent.Resume] before
	// submitting new input on the same session.
	OutcomeInterrupt RunOutcome = "interrupt"
	// OutcomeError means the run terminated with an error.
	OutcomeError RunOutcome = "error"
)

// Interrupt is a protocol-agnostic pause request produced by the runner.
//
// The shape mirrors the AG-UI "Interrupt-Aware Run Lifecycle" draft. Encoders
// map it to their native wire format (e.g. AG-UI emits it inside
// RUN_FINISHED.interrupts[]; the AI SDK encoder surfaces tool-bound
// interrupts as a dangling tool-input-available part).
type Interrupt struct {
	// ID is the correlation key used in Interrupt → Resume round-trips.
	ID string
	// Reason categorises the interrupt. Well-known values are "tool_call",
	// "input_required", and "confirmation". Custom reasons should be
	// namespaced (e.g. "rakit:subagent_wait").
	Reason string
	// Message is a human-readable fallback prompt. Clients that do not
	// understand [Reason] can render this alone as a generic paused card.
	Message string
	// ToolCallID binds the interrupt to a specific prior tool call. It must
	// be set when [Reason] == "tool_call".
	ToolCallID string
	// ResponseSchema is a JSON Schema describing the expected shape of the
	// [ResumeInput.Payload] used to resolve this interrupt.
	ResponseSchema map[string]any
	// ExpiresAt is an optional wall-clock deadline. Zero means no expiry.
	ExpiresAt time.Time
	// Metadata is a free-form extension channel for framework-specific data.
	Metadata map[string]any
}

// ResumeStatus reports how the caller handled an open interrupt.
type ResumeStatus string

const (
	// ResumeResolved means the user supplied a meaningful response carried
	// in [ResumeInput.Payload]. Denials are expressed inside the payload
	// (e.g. {"approved": false}), not as a separate status.
	ResumeResolved ResumeStatus = "resolved"
	// ResumeCancelled means the user abandoned the interrupt without
	// providing input. [ResumeInput.Payload] should be omitted.
	ResumeCancelled ResumeStatus = "cancelled"
)

// ResumeInput resolves one open [Interrupt] on a session.
//
// It mirrors the AG-UI RunAgentInput.resume[] element verbatim so encoders
// can decode a resume envelope directly into a slice of these.
type ResumeInput struct {
	// InterruptID must reference an open [Interrupt.ID] on the session.
	InterruptID string
	// Status is either [ResumeResolved] or [ResumeCancelled].
	Status ResumeStatus
	// Payload is validated against the interrupt's [Interrupt.ResponseSchema]
	// by the caller. The runner does not validate structure; it only reads
	// well-known keys ("approved", "output") when synthesising the tool
	// result for the provider.
	Payload any
}

// ApprovalPolicy decides whether a given tool call must pause for human
// approval before being executed by the runner.
//
// The interface receives the raw tool name and the JSON-encoded arguments
// so policies can gate dynamically (for example, "delete_item" with
// arguments.amount > 10000).
type ApprovalPolicy interface {
	// RequiresApproval returns true when the runner must emit an [Interrupt]
	// with Reason == "tool_call" instead of executing the tool.
	RequiresApproval(toolName string, arguments string) bool
}

// ApprovalPolicyFunc adapts a plain function to the [ApprovalPolicy]
// interface.
type ApprovalPolicyFunc func(toolName string, arguments string) bool

// RequiresApproval calls the underlying function.
func (f ApprovalPolicyFunc) RequiresApproval(toolName, arguments string) bool {
	return f(toolName, arguments)
}

// RequireNone is the default [ApprovalPolicy]: nothing is gated.
func RequireNone() ApprovalPolicy {
	return ApprovalPolicyFunc(func(string, string) bool { return false })
}

// RequireAll gates every tool call.
func RequireAll() ApprovalPolicy {
	return ApprovalPolicyFunc(func(string, string) bool { return true })
}

// RequireFor returns an [ApprovalPolicy] that gates only the named tools.
//
// Tool names are compared case-sensitively.
func RequireFor(names ...string) ApprovalPolicy {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return ApprovalPolicyFunc(func(name, _ string) bool {
		_, ok := set[name]
		return ok
	})
}

// ClientSide is an optional marker interface. Tools that implement it and
// return true are never executed by the runner; instead the runner emits an
// [Interrupt] with Reason == "tool_call" so the caller can supply a result
// via [Agent.Resume].
//
// This models the "client-side tool" pattern from the AI SDK
// (a tool without an execute function) in a protocol-agnostic way.
type ClientSide interface {
	// ClientSide reports whether the tool is client-executed.
	ClientSide() bool
}

// interruptKind classifies an interrupt for runner-internal bookkeeping.
// Stored in [Interrupt.Metadata] under the key "rakit.kind".
const (
	kindApproval   = "approval_required"
	kindClientSide = "client_side"
)

// interruptKind reports the rakit-internal classification stored in Metadata.
func interruptKind(intr Interrupt) string {
	if intr.Metadata == nil {
		return ""
	}
	if s, ok := intr.Metadata["rakit.kind"].(string); ok {
		return s
	}
	return ""
}

// namespaced returns true when the reason uses the reserved namespaced form
// (e.g. "rakit:subagent_wait"). Unused today; reserved for future reasons.
func namespaced(reason string) bool {
	return strings.Contains(reason, ":")
}
