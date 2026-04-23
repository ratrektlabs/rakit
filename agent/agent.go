package agent

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/ratrektlabs/rakit/mcp"
	"github.com/ratrektlabs/rakit/provider"
	"github.com/ratrektlabs/rakit/skill"
	"github.com/ratrektlabs/rakit/storage/blob"
	"github.com/ratrektlabs/rakit/storage/metadata"
	"github.com/ratrektlabs/rakit/tool"
)

// Agent is the core runtime that orchestrates providers, protocols, and tools.
type Agent struct {
	ID            string
	Provider      provider.Provider
	Protocol      Encoder
	Tools         *tool.Registry
	Skills        *skill.Registry
	MCP           *mcp.Registry
	Store         metadata.Store
	FS            blob.BlobStore
	hooks         []Hook
	compaction    CompactionConfig
	maxIterations int
	// parentSession links a subagent to its parent session
	parentSessionID string
}

// Option configures an Agent.
type Option func(*Agent)

func WithProvider(p provider.Provider) Option {
	return func(a *Agent) { a.Provider = p }
}

// WithProtocol sets the default [Encoder] the agent uses when callers do
// not pass one explicitly. The option name is retained for backward
// compatibility; the underlying type is [Encoder].
func WithProtocol(p Encoder) Option {
	return func(a *Agent) { a.Protocol = p }
}

func WithStore(s metadata.Store) Option {
	return func(a *Agent) {
		a.Store = s
		a.Skills = skill.NewRegistry(s)
		a.MCP = mcp.NewRegistry(s)
	}
}

func WithFS(fs blob.BlobStore) Option {
	return func(a *Agent) { a.FS = fs }
}

func WithTools(tools ...tool.Tool) Option {
	return func(a *Agent) {
		for _, t := range tools {
			a.Tools.Register(t)
		}
	}
}

func WithFunction(name, description string, parameters any, fn tool.ExecuteFunc) Option {
	return func(a *Agent) {
		a.Tools.Register(tool.NewFunctionTool(name, description, parameters, fn))
	}
}

func WithHooks(hooks ...Hook) Option {
	return func(a *Agent) { a.hooks = append(a.hooks, hooks...) }
}

// WithCompaction sets the compaction configuration.
func WithCompaction(cfg CompactionConfig) Option {
	return func(a *Agent) { a.compaction = cfg }
}

// WithMaxIterations sets the maximum number of agentic loop iterations.
// Default is 10. Set to 1 for single-turn behavior.
func WithMaxIterations(n int) Option {
	return func(a *Agent) { a.maxIterations = n }
}

// New creates a new Agent with the given options.
func New(opts ...Option) *Agent {
	a := &Agent{
		ID:            generateID(),
		Tools:         tool.NewRegistry(),
		hooks:         make([]Hook, 0),
		compaction:    DefaultCompactionConfig(),
		maxIterations: 10,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// CreateSession creates a new session for this agent.
func (a *Agent) CreateSession(ctx context.Context) (*metadata.Session, error) {
	return a.CreateSessionForUser(ctx, "")
}

// GetSession loads an existing session from the metadata store.
func (a *Agent) GetSession(ctx context.Context, sessionID string) (*metadata.Session, error) {
	if a.Store == nil {
		return nil, fmt.Errorf("agent: no store configured")
	}
	return a.Store.GetSession(ctx, sessionID)
}

// ListSessions returns all sessions belonging to this agent.
func (a *Agent) ListSessions(ctx context.Context) ([]*metadata.Session, error) {
	if a.Store == nil {
		return nil, fmt.Errorf("agent: no store configured")
	}
	return a.Store.ListSessions(ctx, a.ID)
}

// ListSessionsForUser returns sessions for this agent scoped to a userID.
func (a *Agent) ListSessionsForUser(ctx context.Context, userID string) ([]*metadata.Session, error) {
	if a.Store == nil {
		return nil, fmt.Errorf("agent: no store configured")
	}
	return a.Store.ListSessionsByUser(ctx, a.ID, userID)
}

// DeleteSession removes a session from the metadata store.
func (a *Agent) DeleteSession(ctx context.Context, sessionID string) error {
	if a.Store == nil {
		return fmt.Errorf("agent: no store configured")
	}
	return a.Store.DeleteSession(ctx, sessionID)
}

// CreateSessionForUser creates a new session for this agent scoped to a user.
func (a *Agent) CreateSessionForUser(ctx context.Context, userID string) (*metadata.Session, error) {
	if a.Store == nil {
		return nil, fmt.Errorf("agent: no store configured")
	}
	sess, err := a.Store.CreateSession(ctx, a.ID, userID)
	if err != nil {
		return nil, err
	}
	if a.parentSessionID != "" {
		sess.ParentSessionID = a.parentSessionID
		if err := a.Store.UpdateSession(ctx, sess); err != nil {
			return nil, err
		}
	}
	return sess, nil
}

// SubagentConfig configures a spawned subagent.
type SubagentConfig struct {
	System        string      // system prompt for the subagent
	Tools         []tool.Tool // additional tools (inherits parent tools by default)
	MaxIterations int         // default: parent's value
	InheritTools  bool        // default: true
}

// Spawn creates a child agent that shares storage and provider with the parent.
// The child gets its own session linked to parentSessionID.
func (a *Agent) Spawn(ctx context.Context, parentSessionID string, cfg SubagentConfig) *Agent {
	maxIter := a.maxIterations
	if cfg.MaxIterations > 0 {
		maxIter = cfg.MaxIterations
	}

	child := &Agent{
		ID:              generateID(),
		Provider:        a.Provider,
		Protocol:        a.Protocol,
		Tools:           tool.NewRegistry(),
		Store:           a.Store,
		FS:              a.FS,
		hooks:           a.hooks,
		compaction:      a.compaction,
		maxIterations:   maxIter,
		parentSessionID: parentSessionID,
	}

	// Inherit skills and MCP from parent
	if a.Skills != nil {
		child.Skills = a.Skills
	}
	if a.MCP != nil {
		child.MCP = a.MCP
	}

	// Inherit parent tools if requested (default true)
	if cfg.InheritTools || len(cfg.Tools) == 0 {
		for _, t := range a.Tools.All() {
			child.Tools.Register(t)
		}
	}

	// Register additional tools
	for _, t := range cfg.Tools {
		child.Tools.Register(t)
	}

	return child
}
