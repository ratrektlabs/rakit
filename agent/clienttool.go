package agent

import "github.com/ratrektlabs/rakit/tool"

// ClientSide is an optional interface a tool.Tool may implement to signal
// that execution happens on the caller/frontend rather than in-process.
//
// When the agent runner encounters a tool whose ClientSide() returns true,
// it does NOT invoke Execute. Instead it emits a ToolCallPendingEvent with
// reason "client_side" and pauses the run. The caller is expected to execute
// the tool wherever appropriate (e.g. in a browser) and resume the agent
// with Agent.Resume, providing the result in the matching ToolDecision.
type ClientSide interface {
	ClientSide() bool
}

// isClientSide reports whether t opts in to client-side execution.
func isClientSide(t tool.Tool) bool {
	if cs, ok := t.(ClientSide); ok {
		return cs.ClientSide()
	}
	return false
}
