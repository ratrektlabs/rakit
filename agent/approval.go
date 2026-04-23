package agent

import "github.com/ratrektlabs/rakit/provider"

// ApprovalPolicy decides whether a tool call requires explicit human approval
// before the agent runner executes it. Returning true pauses the run, persists
// the pending tool call on the session, and emits a ToolCallPendingEvent;
// execution resumes once the caller invokes Agent.Resume with a matching
// ToolDecision.
//
// Approval policies are orthogonal to client-side tools — a tool that
// implements the ClientSide marker interface is always paused regardless of
// policy, because the client is responsible for producing its result.
type ApprovalPolicy interface {
	Require(call provider.ToolCall) bool
}

// ApprovalPolicyFunc adapts a plain function to the ApprovalPolicy interface.
type ApprovalPolicyFunc func(call provider.ToolCall) bool

// Require implements ApprovalPolicy.
func (f ApprovalPolicyFunc) Require(call provider.ToolCall) bool { return f(call) }

// RequireAll gates every tool call on human approval. Useful for high-stakes
// agents that should never execute a tool without a user in the loop.
func RequireAll() ApprovalPolicy {
	return ApprovalPolicyFunc(func(_ provider.ToolCall) bool { return true })
}

// RequireNone disables approval gating. This is equivalent to not configuring
// any policy and is provided for symmetry / explicit opt-out.
func RequireNone() ApprovalPolicy {
	return ApprovalPolicyFunc(func(_ provider.ToolCall) bool { return false })
}

// RequireFor gates only the named tools. All other tool calls execute
// immediately as usual.
func RequireFor(names ...string) ApprovalPolicy {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return ApprovalPolicyFunc(func(call provider.ToolCall) bool {
		_, ok := set[call.Name]
		return ok
	})
}
