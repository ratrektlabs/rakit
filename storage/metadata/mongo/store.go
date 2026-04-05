package mongo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ratrektlabs/rakit/storage/metadata"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	collSessions   = "sessions"
	collTools      = "tools"
	collSkills     = "skills"
	collMemory     = "memory"
	collMCPServers = "mcp_servers"
)

// Store implements metadata.Store backed by MongoDB.
type Store struct {
	client     *mongo.Client
	db         *mongo.Database
	dbName     string
	sessions   *mongo.Collection
	tools      *mongo.Collection
	skills     *mongo.Collection
	memory     *mongo.Collection
	mcpServers *mongo.Collection
}

// NewStore connects to MongoDB at uri, selects the given database, and returns
// a ready-to-use Store. The caller should call Close when done.
func NewStore(ctx context.Context, uri, dbName string) (*Store, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}

	// Ping to verify connectivity.
	if err = client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("mongo ping: %w", err)
	}

	db := client.Database(dbName)
	s := &Store{
		client:     client,
		db:         db,
		dbName:     dbName,
		sessions:   db.Collection(collSessions),
		tools:      db.Collection(collTools),
		skills:     db.Collection(collSkills),
		memory:     db.Collection(collMemory),
		mcpServers: db.Collection(collMCPServers),
	}
	return s, nil
}

// Close disconnects the underlying MongoDB client.
func (s *Store) Close(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}

// ---------------------------------------------------------------------------
// Sessions
// ---------------------------------------------------------------------------

func (s *Store) CreateSession(ctx context.Context, agentID, userID string) (*metadata.Session, error) {
	now := time.Now().Unix()
	sess := &metadata.Session{
		ID:        newID(),
		AgentID:   agentID,
		UserID:    userID,
		Messages:  []metadata.Message{},
		State:     map[string]any{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := s.sessions.InsertOne(ctx, sess)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return sess, nil
}

func (s *Store) GetSession(ctx context.Context, id string) (*metadata.Session, error) {
	var sess metadata.Session
	err := s.sessions.FindOne(ctx, bson.D{{Key: "id", Value: id}}).Decode(&sess)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("get session %q: %w", id, err)
	}
	return &sess, nil
}

func (s *Store) UpdateSession(ctx context.Context, sess *metadata.Session) error {
	if sess == nil {
		return fmt.Errorf("update session: nil session")
	}
	sess.UpdatedAt = time.Now().Unix()
	res, err := s.sessions.ReplaceOne(ctx, bson.D{{Key: "id", Value: sess.ID}}, sess)
	if err != nil {
		return fmt.Errorf("update session %q: %w", sess.ID, err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("update session %q: not found", sess.ID)
	}
	return nil
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.sessions.DeleteOne(ctx, bson.D{{Key: "id", Value: id}})
	if err != nil {
		return fmt.Errorf("delete session %q: %w", id, err)
	}
	return nil
}

func (s *Store) ListSessions(ctx context.Context, agentID string) ([]*metadata.Session, error) {
	cursor, err := s.sessions.Find(ctx, bson.D{{Key: "agentid", Value: agentID}})
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer cursor.Close(ctx)

	var sessions []*metadata.Session
	for cursor.Next(ctx) {
		var sess metadata.Session
		if err := cursor.Decode(&sess); err != nil {
			return nil, fmt.Errorf("decode session: %w", err)
		}
		sess.Messages = []metadata.Message{}
		sessions = append(sessions, &sess)
	}
	if sessions == nil {
		sessions = []*metadata.Session{}
	}
	return sessions, nil
}

func (s *Store) ListSessionsByUser(ctx context.Context, agentID, userID string) ([]*metadata.Session, error) {
	filter := bson.D{
		{Key: "agentid", Value: agentID},
		{Key: "userid", Value: userID},
	}
	cursor, err := s.sessions.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list sessions by user: %w", err)
	}
	defer cursor.Close(ctx)

	var sessions []*metadata.Session
	for cursor.Next(ctx) {
		var sess metadata.Session
		if err := cursor.Decode(&sess); err != nil {
			return nil, fmt.Errorf("decode session: %w", err)
		}
		sess.Messages = []metadata.Message{}
		sessions = append(sessions, &sess)
	}
	if sessions == nil {
		sessions = []*metadata.Session{}
	}
	return sessions, nil
}

// ---------------------------------------------------------------------------
// Tools
// ---------------------------------------------------------------------------

func (s *Store) SaveTool(ctx context.Context, tool *metadata.ToolDef) error {
	if tool == nil {
		return fmt.Errorf("save tool: nil tool")
	}
	opts := options.Replace().SetUpsert(true)
	_, err := s.tools.ReplaceOne(ctx, bson.D{{Key: "name", Value: tool.Name}}, tool, opts)
	if err != nil {
		return fmt.Errorf("save tool %q: %w", tool.Name, err)
	}
	return nil
}

func (s *Store) GetTool(ctx context.Context, name string) (*metadata.ToolDef, error) {
	var tool metadata.ToolDef
	err := s.tools.FindOne(ctx, bson.D{{Key: "name", Value: name}}).Decode(&tool)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("get tool %q: %w", name, err)
	}
	return &tool, nil
}

func (s *Store) ListTools(ctx context.Context, agentID string) ([]*metadata.ToolDef, error) {
	cursor, err := s.tools.Find(ctx, bson.D{{Key: "agentid", Value: agentID}})
	if err != nil {
		return nil, fmt.Errorf("list tools for agent %q: %w", agentID, err)
	}
	defer cursor.Close(ctx)

	var results []*metadata.ToolDef
	if err = cursor.All(ctx, &results); err != nil {
		return nil, fmt.Errorf("decode tools: %w", err)
	}
	return results, nil
}

func (s *Store) DeleteTool(ctx context.Context, name string) error {
	_, err := s.tools.DeleteOne(ctx, bson.D{{Key: "name", Value: name}})
	if err != nil {
		return fmt.Errorf("delete tool %q: %w", name, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Skills
// ---------------------------------------------------------------------------

func (s *Store) SaveSkill(ctx context.Context, def *metadata.SkillDef) error {
	if def == nil {
		return fmt.Errorf("save skill: nil def")
	}
	opts := options.Replace().SetUpsert(true)
	_, err := s.skills.ReplaceOne(ctx, bson.D{{Key: "name", Value: def.Name}}, def, opts)
	if err != nil {
		return fmt.Errorf("save skill %q: %w", def.Name, err)
	}
	return nil
}

func (s *Store) GetSkill(ctx context.Context, name string) (*metadata.SkillDef, error) {
	var def metadata.SkillDef
	err := s.skills.FindOne(ctx, bson.D{{Key: "name", Value: name}}).Decode(&def)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("get skill %q: %w", name, err)
	}
	return &def, nil
}

func (s *Store) ListSkills(ctx context.Context) ([]*metadata.SkillEntry, error) {
	// Only project the lightweight L1 fields.
	projection := bson.D{
		{Key: "name", Value: 1},
		{Key: "description", Value: 1},
		{Key: "version", Value: 1},
		{Key: "enabled", Value: 1},
	}
	opts := options.Find().SetProjection(projection)

	cursor, err := s.skills.Find(ctx, bson.D{}, opts)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	defer cursor.Close(ctx)

	var entries []*metadata.SkillEntry
	if err = cursor.All(ctx, &entries); err != nil {
		return nil, fmt.Errorf("decode skills: %w", err)
	}
	return entries, nil
}

func (s *Store) DeleteSkill(ctx context.Context, name string) error {
	_, err := s.skills.DeleteOne(ctx, bson.D{{Key: "name", Value: name}})
	if err != nil {
		return fmt.Errorf("delete skill %q: %w", name, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Scoped Memory
// ---------------------------------------------------------------------------

// memoryDoc is the internal BSON document shape for the memory collection.
type memoryDoc struct {
	Key   string `bson:"key"`
	Value []byte `bson:"value"`
}

func (s *Store) SetMemory(ctx context.Context, scope metadata.MemoryScope, scopeID, key string, value []byte) error {
	compositeKey := metadata.ScopedKey(scope, scopeID, key)
	doc := memoryDoc{Key: compositeKey, Value: value}
	opts := options.Replace().SetUpsert(true)
	_, err := s.memory.ReplaceOne(ctx, bson.D{{Key: "key", Value: compositeKey}}, doc, opts)
	if err != nil {
		return fmt.Errorf("set memory %q: %w", compositeKey, err)
	}
	return nil
}

func (s *Store) GetMemory(ctx context.Context, scope metadata.MemoryScope, scopeID, key string) ([]byte, error) {
	compositeKey := metadata.ScopedKey(scope, scopeID, key)
	var doc memoryDoc
	err := s.memory.FindOne(ctx, bson.D{{Key: "key", Value: compositeKey}}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("get memory %q: %w", compositeKey, err)
	}
	return doc.Value, nil
}

func (s *Store) DeleteMemory(ctx context.Context, scope metadata.MemoryScope, scopeID, key string) error {
	compositeKey := metadata.ScopedKey(scope, scopeID, key)
	_, err := s.memory.DeleteOne(ctx, bson.D{{Key: "key", Value: compositeKey}})
	if err != nil {
		return fmt.Errorf("delete memory %q: %w", compositeKey, err)
	}
	return nil
}

func (s *Store) ListMemory(ctx context.Context, scope metadata.MemoryScope, scopeID, prefix string) ([]string, error) {
	compositePrefix := metadata.ScopedKey(scope, scopeID, prefix)
	filter := bson.D{{
		Key: "key",
		Value: bson.D{{
			Key:   "$regex",
			Value: "^" + escapeRegex(compositePrefix),
		}},
	}}

	projection := bson.D{{Key: "key", Value: 1}}
	opts := options.Find().SetProjection(projection)

	cursor, err := s.memory.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("list memory prefix %q: %w", compositePrefix, err)
	}
	defer cursor.Close(ctx)

	var keys []string
	for cursor.Next(ctx) {
		var doc struct {
			Key string `bson:"key"`
		}
		if err = cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("decode memory key: %w", err)
		}
		keys = append(keys, doc.Key)
	}
	if err = cursor.Err(); err != nil {
		return nil, fmt.Errorf("cursor error: %w", err)
	}
	return keys, nil
}

// ---------------------------------------------------------------------------
// Legacy flat KV (delegates to global-scoped memory)
// ---------------------------------------------------------------------------

func (s *Store) Set(ctx context.Context, key string, value []byte) error {
	return s.SetMemory(ctx, metadata.ScopeGlobal, "", key, value)
}

func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	return s.GetMemory(ctx, metadata.ScopeGlobal, "", key)
}

func (s *Store) Delete(ctx context.Context, key string) error {
	return s.DeleteMemory(ctx, metadata.ScopeGlobal, "", key)
}

func (s *Store) List(ctx context.Context, prefix string) ([]string, error) {
	return s.ListMemory(ctx, metadata.ScopeGlobal, "", prefix)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newID generates a unique string identifier.
func newID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// escapeRegex escapes special regex characters in s so it can be used inside
// a $regex pattern without unintended metacharacter interpretation.
func escapeRegex(s string) string {
	var b []byte
	for _, c := range s {
		switch c {
		case '.', '^', '$', '*', '+', '?', '(', ')', '[', ']', '{', '}', '|', '\\':
			b = append(b, '\\')
		}
		b = append(b, byte(c))
	}
	return string(b)
}

// Verify interface compliance at compile time.
var _ metadata.Store = (*Store)(nil)

// ---------------------------------------------------------------------------
// MCP Servers
// ---------------------------------------------------------------------------

func (s *Store) SaveMCPServer(ctx context.Context, srv *metadata.MCPServerDef) error {
	if srv == nil {
		return fmt.Errorf("save mcp server: nil server")
	}
	opts := options.Replace().SetUpsert(true)
	_, err := s.mcpServers.ReplaceOne(ctx, bson.D{{Key: "name", Value: srv.Name}}, srv, opts)
	if err != nil {
		return fmt.Errorf("save mcp server %q: %w", srv.Name, err)
	}
	return nil
}

func (s *Store) GetMCPServer(ctx context.Context, name string) (*metadata.MCPServerDef, error) {
	var srv metadata.MCPServerDef
	err := s.mcpServers.FindOne(ctx, bson.D{{Key: "name", Value: name}}).Decode(&srv)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("get mcp server %q: %w", name, err)
	}
	return &srv, nil
}

func (s *Store) ListMCPServers(ctx context.Context, agentID string) ([]*metadata.MCPServerDef, error) {
	cursor, err := s.mcpServers.Find(ctx, bson.D{{Key: "agentid", Value: agentID}})
	if err != nil {
		return nil, fmt.Errorf("list mcp servers for agent %q: %w", agentID, err)
	}
	defer cursor.Close(ctx)

	var results []*metadata.MCPServerDef
	if err = cursor.All(ctx, &results); err != nil {
		return nil, fmt.Errorf("decode mcp servers: %w", err)
	}
	return results, nil
}

func (s *Store) DeleteMCPServer(ctx context.Context, name string) error {
	_, err := s.mcpServers.DeleteOne(ctx, bson.D{{Key: "name", Value: name}})
	if err != nil {
		return fmt.Errorf("delete mcp server %q: %w", name, err)
	}
	return nil
}
