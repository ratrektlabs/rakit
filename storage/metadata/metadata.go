package metadata

import "context"

// Session represents a conversation session.
type Session struct {
	ID        string
	AgentID   string
	Messages  []Message
	State     map[string]any
	CreatedAt int64
	UpdatedAt int64
}

// Message is a single message in a session.
type Message struct {
	ID        string
	Role      string
	Content   string
	ToolCalls []ToolCallRecord
	CreatedAt int64
}

// ToolCallRecord is a persisted tool call.
type ToolCallRecord struct {
	ID        string
	Name      string
	Arguments string
	Result    string
	Status    string // "pending", "completed", "failed"
}

// ToolDef is a persisted tool definition.
type ToolDef struct {
	ID          string
	AgentID     string
	Name        string
	Description string
	Parameters  any
	CreatedAt   int64
}

// SkillEntry is the lightweight L1 record (name + description).
type SkillEntry struct {
	Name        string `json:"name" firestore:"name"`
	Description string `json:"description" firestore:"description"`
	Version     string `json:"version" firestore:"version"`
	Enabled     bool   `json:"enabled" firestore:"enabled"`
}

// SkillDef is the full L2 skill definition stored in metadata.
type SkillDef struct {
	Name         string         `json:"name" firestore:"name"`
	Description  string         `json:"description" firestore:"description"`
	Version      string         `json:"version" firestore:"version"`
	Instructions string         `json:"instructions" firestore:"instructions"`
	Tools        []any          `json:"tools" firestore:"tools"`
	Config       map[string]any `json:"config" firestore:"config"`
	Resources    []any          `json:"resources" firestore:"resources"`
	Enabled      bool           `json:"enabled" firestore:"enabled"`
}

// Store is the unified metadata interface.
type Store interface {
	// Sessions
	CreateSession(ctx context.Context, agentID string) (*Session, error)
	GetSession(ctx context.Context, id string) (*Session, error)
	UpdateSession(ctx context.Context, s *Session) error
	DeleteSession(ctx context.Context, id string) error

	// Tools
	SaveTool(ctx context.Context, tool *ToolDef) error
	GetTool(ctx context.Context, name string) (*ToolDef, error)
	ListTools(ctx context.Context, agentID string) ([]*ToolDef, error)
	DeleteTool(ctx context.Context, name string) error

	// Skills
	SaveSkill(ctx context.Context, def *SkillDef) error
	GetSkill(ctx context.Context, name string) (*SkillDef, error)
	ListSkills(ctx context.Context) ([]*SkillEntry, error)
	DeleteSkill(ctx context.Context, name string) error

	// Memory (key-value)
	Set(ctx context.Context, key string, value []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) ([]string, error)
}
