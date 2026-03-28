package agent

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/ratrektlabs/rakit/protocol"
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
	Protocol      protocol.Protocol
	Tools         *tool.Registry
	Skills        *skill.Registry
	Store         metadata.Store
	FS            blob.BlobStore
	hooks         []Hook
	compaction    CompactionConfig
	maxIterations int
}

// Option configures an Agent.
type Option func(*Agent)

func WithProvider(p provider.Provider) Option {
	return func(a *Agent) { a.Provider = p }
}

func WithProtocol(p protocol.Protocol) Option {
	return func(a *Agent) { a.Protocol = p }
}

func WithStore(s metadata.Store) Option {
	return func(a *Agent) {
		a.Store = s
		a.Skills = skill.NewRegistry(s)
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
	if a.Store == nil {
		return nil, fmt.Errorf("agent: no store configured")
	}
	return a.Store.CreateSession(ctx, a.ID)
}
