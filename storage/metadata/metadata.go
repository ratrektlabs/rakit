package metadata

import "context"

// Session represents a conversation session.
type Session struct {
	ID              string         `json:"id" firestore:"id" bson:"id"`
	AgentID         string         `json:"agentId" firestore:"agentId" bson:"agentid"`
	UserID          string         `json:"userId" firestore:"userId" bson:"userid"`
	ParentSessionID string         `json:"parentSessionId,omitempty" firestore:"parentSessionId" bson:"parentsessionid,omitempty"`
	Messages        []Message      `json:"messages" firestore:"messages" bson:"messages"`
	State           map[string]any `json:"state" firestore:"state" bson:"state"`
	// OpenInterrupts is the set of unresolved interrupts raised by the most
	// recent run on this session. A non-empty slice means a resume is
	// required before any new user input can be processed.
	OpenInterrupts []Interrupt `json:"openInterrupts,omitempty" firestore:"openInterrupts" bson:"openinterrupts,omitempty"`
	CreatedAt      int64       `json:"createdAt" firestore:"createdAt" bson:"createdat"`
	UpdatedAt      int64       `json:"updatedAt" firestore:"updatedAt" bson:"updatedat"`
}

// Interrupt is the persisted shape of an unresolved pause on a session.
//
// This mirrors the in-memory agent.Interrupt but lives in the metadata
// package to avoid a circular dependency between agent/ and storage/. The
// agent package converts between the two shapes when a run pauses or
// resumes.
type Interrupt struct {
	ID             string         `json:"id" firestore:"id" bson:"id"`
	Reason         string         `json:"reason" firestore:"reason" bson:"reason"`
	Message        string         `json:"message,omitempty" firestore:"message" bson:"message,omitempty"`
	ToolCallID     string         `json:"toolCallId,omitempty" firestore:"toolCallId" bson:"toolcallid,omitempty"`
	ResponseSchema map[string]any `json:"responseSchema,omitempty" firestore:"responseSchema" bson:"responseschema,omitempty"`
	ExpiresAtMs    int64          `json:"expiresAtMs,omitempty" firestore:"expiresAtMs" bson:"expiresatms,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty" firestore:"metadata" bson:"metadata,omitempty"`
}

// Message is a single message in a session.
type Message struct {
	ID        string           `json:"id" firestore:"id" bson:"id"`
	Role      string           `json:"role" firestore:"role" bson:"role"`
	Content   string           `json:"content" firestore:"content" bson:"content"`
	ToolCalls []ToolCallRecord `json:"toolCalls" firestore:"toolCalls" bson:"toolcalls"`
	CreatedAt int64            `json:"createdAt" firestore:"createdAt" bson:"createdat"`
}

// ToolCallRecord is a persisted tool call.
//
// Status values:
//   - "pending"           — tool call emitted but not yet classified
//   - "pending_approval"  — blocked on human approval (see agent.ApprovalPolicy)
//   - "pending_client"    — blocked on a client-side tool result
//   - "completed"         — executed successfully
//   - "failed"            — execution returned an error or was cancelled
type ToolCallRecord struct {
	ID        string `json:"id" firestore:"id" bson:"id"`
	Name      string `json:"name" firestore:"name" bson:"name"`
	Arguments string `json:"arguments" firestore:"arguments" bson:"arguments"`
	Result    string `json:"result" firestore:"result" bson:"result"`
	Status    string `json:"status" firestore:"status" bson:"status"`
}

// ToolDef is a persisted tool definition.
type ToolDef struct {
	ID            string            `json:"id" firestore:"id" bson:"id"`
	AgentID       string            `json:"agentId" firestore:"agentId" bson:"agentid"`
	Name          string            `json:"name" firestore:"name" bson:"name"`
	Description   string            `json:"description" firestore:"description" bson:"description"`
	Parameters    any               `json:"parameters" firestore:"parameters" bson:"parameters"`
	Handler       string            `json:"handler" firestore:"handler" bson:"handler"`    // "http", "script"
	Endpoint      string            `json:"endpoint" firestore:"endpoint" bson:"endpoint"` // for http handler
	Headers       map[string]string `json:"headers" firestore:"headers" bson:"headers"`
	InputMapping  map[string]string `json:"inputMapping" firestore:"inputMapping" bson:"inputmapping"`
	ResponseField string            `json:"responseField" firestore:"responseField" bson:"responsefield"`
	ScriptPath    string            `json:"scriptPath" firestore:"scriptPath" bson:"scriptpath"`
	CreatedAt     int64             `json:"createdAt" firestore:"createdAt" bson:"createdat"`
}

// SkillEntry is the lightweight L1 record (name + description).
type SkillEntry struct {
	Name        string `json:"name" firestore:"name" bson:"name"`
	Description string `json:"description" firestore:"description" bson:"description"`
	Version     string `json:"version" firestore:"version" bson:"version"`
	Enabled     bool   `json:"enabled" firestore:"enabled" bson:"enabled"`
}

// SkillDef is the full L2 skill definition stored in metadata.
type SkillDef struct {
	Name         string         `json:"name" firestore:"name" bson:"name"`
	Description  string         `json:"description" firestore:"description" bson:"description"`
	Version      string         `json:"version" firestore:"version" bson:"version"`
	Instructions string         `json:"instructions" firestore:"instructions" bson:"instructions"`
	Tools        []any          `json:"tools" firestore:"tools" bson:"tools"`
	Config       map[string]any `json:"config" firestore:"config" bson:"config"`
	Resources    []any          `json:"resources" firestore:"resources" bson:"resources"`
	Enabled      bool           `json:"enabled" firestore:"enabled" bson:"enabled"`
}

// MCPServerDef is a persisted MCP server definition.
type MCPServerDef struct {
	ID        string            `json:"id" firestore:"id" bson:"id"`
	AgentID   string            `json:"agentId" firestore:"agentId" bson:"agentid"`
	Name      string            `json:"name" firestore:"name" bson:"name"`
	URL       string            `json:"url" firestore:"url" bson:"url"`
	Transport string            `json:"transport" firestore:"transport" bson:"transport"` // "http" (default), "sse"
	Headers   map[string]string `json:"headers" firestore:"headers" bson:"headers"`
	Enabled   bool              `json:"enabled" firestore:"enabled" bson:"enabled"`
	CreatedAt int64             `json:"createdAt" firestore:"createdAt" bson:"createdat"`
}

// MemoryScope defines the scope for memory operations.
type MemoryScope string

const (
	ScopeGlobal MemoryScope = "global"
	ScopeAgent  MemoryScope = "agent"
	ScopeUser   MemoryScope = "user"
)

// Store is the unified metadata interface.
type Store interface {
	// Sessions
	CreateSession(ctx context.Context, agentID, userID string) (*Session, error)
	GetSession(ctx context.Context, id string) (*Session, error)
	ListSessions(ctx context.Context, agentID string) ([]*Session, error)
	ListSessionsByUser(ctx context.Context, agentID, userID string) ([]*Session, error)
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

	// Scoped Memory
	SetMemory(ctx context.Context, scope MemoryScope, scopeID, key string, value []byte) error
	GetMemory(ctx context.Context, scope MemoryScope, scopeID, key string) ([]byte, error)
	DeleteMemory(ctx context.Context, scope MemoryScope, scopeID, key string) error
	ListMemory(ctx context.Context, scope MemoryScope, scopeID, prefix string) ([]string, error)

	// Legacy flat KV (delegates to global-scoped memory)
	Set(ctx context.Context, key string, value []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) ([]string, error)

	// MCP Servers
	SaveMCPServer(ctx context.Context, server *MCPServerDef) error
	GetMCPServer(ctx context.Context, name string) (*MCPServerDef, error)
	ListMCPServers(ctx context.Context, agentID string) ([]*MCPServerDef, error)
	DeleteMCPServer(ctx context.Context, name string) error
}

// ScopedKey builds a composite key for scoped memory storage.
func ScopedKey(scope MemoryScope, scopeID, key string) string {
	return string(scope) + ":" + scopeID + "::" + key
}
