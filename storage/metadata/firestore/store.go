package firestore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/ratrektlabs/rakit/storage/metadata"
	"google.golang.org/api/iterator"
)

const (
	sessionsCol = "sessions"
	messagesCol = "messages"
	toolsCol    = "tools"
	skillsCol   = "skills"
	memoryCol   = "memory"
)

// Store implements metadata.Store backed by Google Cloud Firestore.
type Store struct {
	client *firestore.Client
}

// NewStore creates a new Firestore-backed metadata store.
func NewStore(ctx context.Context, projectID string) (*Store, error) {
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("firestore: create client: %w", err)
	}
	return &Store{client: client}, nil
}

// Close releases the underlying Firestore client.
func (s *Store) Close() error {
	return s.client.Close()
}

// ---------------------------------------------------------------------------
// Sessions
// ---------------------------------------------------------------------------

// CreateSession creates a new session for the given agentID and returns it.
func (s *Store) CreateSession(ctx context.Context, agentID string) (*metadata.Session, error) {
	now := time.Now().Unix()
	session := &metadata.Session{
		ID:        "",
		AgentID:   agentID,
		Messages:  []metadata.Message{},
		State:     map[string]any{},
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Firestore auto-generates the document ID.
	ref, _, err := s.client.Collection(sessionsCol).Add(ctx, toSessionMap(session))
	if err != nil {
		return nil, fmt.Errorf("firestore: create session: %w", err)
	}

	session.ID = ref.ID
	// Write back once to persist the ID inside the document body.
	_, err = ref.Update(ctx, []firestore.Update{
		{Path: "id", Value: ref.ID},
	})
	if err != nil {
		return nil, fmt.Errorf("firestore: set session id: %w", err)
	}
	return session, nil
}

// GetSession retrieves a session by ID including its messages subcollection.
func (s *Store) GetSession(ctx context.Context, id string) (*metadata.Session, error) {
	doc, err := s.client.Collection(sessionsCol).Doc(id).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("firestore: get session %s: %w", id, err)
	}

	var session metadata.Session
	if err := doc.DataTo(&session); err != nil {
		return nil, fmt.Errorf("firestore: decode session %s: %w", id, err)
	}
	session.ID = id

	// Load messages from subcollection.
	msgs, err := s.loadMessages(ctx, id)
	if err != nil {
		return nil, err
	}
	session.Messages = msgs
	return &session, nil
}

// UpdateSession writes the session document and upserts all messages.
func (s *Store) UpdateSession(ctx context.Context, sess *metadata.Session) error {
	if sess.ID == "" {
		return fmt.Errorf("firestore: update session: empty id")
	}

	now := time.Now().Unix()
	sess.UpdatedAt = now

	ref := s.client.Collection(sessionsCol).Doc(sess.ID)

	err := s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		// Persist the session metadata (without the messages slice itself).
		if err := tx.Set(ref, toSessionMap(sess)); err != nil {
			return fmt.Errorf("set session: %w", err)
		}

		// Upsert each message into the subcollection.
		msgCol := ref.Collection(messagesCol)
		for _, m := range sess.Messages {
			if m.ID == "" {
				continue
			}
			msgRef := msgCol.Doc(m.ID)
			if err := tx.Set(msgRef, toMessageMap(&m)); err != nil {
				return fmt.Errorf("set message %s: %w", m.ID, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("firestore: update session %s: %w", sess.ID, err)
	}
	return nil
}

// DeleteSession removes a session document and all its message subdocuments.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	ref := s.client.Collection(sessionsCol).Doc(id)

	// Delete all messages in the subcollection first.
	if err := s.deleteSubcollection(ctx, ref.Collection(messagesCol)); err != nil {
		return fmt.Errorf("firestore: delete session %s messages: %w", id, err)
	}

	_, err := ref.Delete(ctx)
	if err != nil {
		return fmt.Errorf("firestore: delete session %s: %w", id, err)
	}
	return nil
}

func (s *Store) ListSessions(ctx context.Context, agentID string) ([]*metadata.Session, error) {
	iter := s.client.Collection(sessionsCol).Where("agentID", "==", agentID).Documents(ctx)
	defer iter.Stop()

	var sessions []*metadata.Session
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		data := doc.Data()
		agentID, _ := data["agentID"].(string)
		createdAt, _ := data["createdAt"].(int64)
		updatedAt, _ := data["updatedAt"].(int64)
		sess := &metadata.Session{
			ID:        doc.Ref.ID,
			AgentID:   agentID,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
			Messages:  []metadata.Message{},
		}
		sessions = append(sessions, sess)
	}
	if sessions == nil {
		sessions = []*metadata.Session{}
	}
	return sessions, nil
}

// ---------------------------------------------------------------------------
// Tools
// ---------------------------------------------------------------------------

// SaveTool persists a tool definition, keyed by its Name within the agent document.
func (s *Store) SaveTool(ctx context.Context, tool *metadata.ToolDef) error {
	if tool.AgentID == "" || tool.Name == "" {
		return fmt.Errorf("firestore: save tool: agentID and name are required")
	}

	// Document ID is agentID; tools are stored in a map field inside it.
	ref := s.client.Collection(toolsCol).Doc(tool.AgentID)

	err := s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		m := toToolMap(tool)
		return tx.Set(ref, map[string]any{
			"agentID": tool.AgentID,
			"tools":   map[string]any{tool.Name: m},
		}, firestore.MergeAll)
	})
	if err != nil {
		return fmt.Errorf("firestore: save tool %s: %w", tool.Name, err)
	}
	return nil
}

// GetTool retrieves a single tool by name. It must scan agent documents because
// tools are organized per-agent; we query the "name" field across the collection.
func (s *Store) GetTool(ctx context.Context, name string) (*metadata.ToolDef, error) {
	iter := s.client.Collection(toolsCol).
		Where("name", "==", name).
		Limit(1).
		Documents(ctx)

	doc, err := iter.Next()
	if err == iterator.Done {
		return nil, fmt.Errorf("firestore: tool %s not found", name)
	}
	if err != nil {
		return nil, fmt.Errorf("firestore: get tool %s: %w", name, err)
	}

	var tool metadata.ToolDef
	if err := doc.DataTo(&tool); err != nil {
		return nil, fmt.Errorf("firestore: decode tool %s: %w", name, err)
	}
	return &tool, nil
}

// ListTools returns all tools for the given agentID.
func (s *Store) ListTools(ctx context.Context, agentID string) ([]*metadata.ToolDef, error) {
	doc, err := s.client.Collection(toolsCol).Doc(agentID).Get(ctx)
	if err != nil {
		// Not found means no tools for this agent.
		if isNotFound(err) {
			return []*metadata.ToolDef{}, nil
		}
		return nil, fmt.Errorf("firestore: list tools for agent %s: %w", agentID, err)
	}

	var result struct {
		Tools map[string]map[string]any `firestore:"tools"`
	}
	if err := doc.DataTo(&result); err != nil {
		return nil, fmt.Errorf("firestore: decode tools for agent %s: %w", agentID, err)
	}

	out := make([]*metadata.ToolDef, 0, len(result.Tools))
	for _, raw := range result.Tools {
		tool := toolFromMap(raw)
		if tool != nil {
			out = append(out, tool)
		}
	}
	return out, nil
}

// DeleteTool removes a tool by name from the agent document.
func (s *Store) DeleteTool(ctx context.Context, name string) error {
	// Find the agent doc that contains this tool.
	iter := s.client.Collection(toolsCol).
		Where("name", "==", name).
		Limit(1).
		Documents(ctx)

	doc, err := iter.Next()
	if err == iterator.Done {
		return fmt.Errorf("firestore: tool %s not found", name)
	}
	if err != nil {
		return fmt.Errorf("firestore: find tool %s for delete: %w", name, err)
	}

	_, err = doc.Ref.Update(ctx, []firestore.Update{
		{Path: "tools." + name, Value: firestore.Delete},
	})
	if err != nil {
		return fmt.Errorf("firestore: delete tool %s: %w", name, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Skills
// ---------------------------------------------------------------------------

// SaveSkill persists a full skill definition keyed by its Name.
func (s *Store) SaveSkill(ctx context.Context, def *metadata.SkillDef) error {
	if def.Name == "" {
		return fmt.Errorf("firestore: save skill: name is required")
	}
	ref := s.client.Collection(skillsCol).Doc(def.Name)
	_, err := ref.Set(ctx, def)
	if err != nil {
		return fmt.Errorf("firestore: save skill %s: %w", def.Name, err)
	}
	return nil
}

// GetSkill retrieves a skill definition by name.
func (s *Store) GetSkill(ctx context.Context, name string) (*metadata.SkillDef, error) {
	doc, err := s.client.Collection(skillsCol).Doc(name).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("firestore: get skill %s: %w", name, err)
	}
	var def metadata.SkillDef
	if err := doc.DataTo(&def); err != nil {
		return nil, fmt.Errorf("firestore: decode skill %s: %w", name, err)
	}
	return &def, nil
}

// ListSkills returns the lightweight L1 entries for all skills.
func (s *Store) ListSkills(ctx context.Context) ([]*metadata.SkillEntry, error) {
	iter := s.client.Collection(skillsCol).Documents(ctx)
	defer iter.Stop()

	var entries []*metadata.SkillEntry
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("firestore: list skills: %w", err)
		}
		var entry metadata.SkillEntry
		if err := doc.DataTo(&entry); err != nil {
			return nil, fmt.Errorf("firestore: decode skill entry %s: %w", doc.Ref.ID, err)
		}
		entries = append(entries, &entry)
	}
	return entries, nil
}

// DeleteSkill removes a skill definition by name.
func (s *Store) DeleteSkill(ctx context.Context, name string) error {
	_, err := s.client.Collection(skillsCol).Doc(name).Delete(ctx)
	if err != nil {
		return fmt.Errorf("firestore: delete skill %s: %w", name, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Memory (key-value)
// ---------------------------------------------------------------------------

// Set stores a key-value pair in the memory collection.
func (s *Store) Set(ctx context.Context, key string, value []byte) error {
	if key == "" {
		return fmt.Errorf("firestore: set: empty key")
	}
	ref := s.client.Collection(memoryCol).Doc(key)
	_, err := ref.Set(ctx, map[string]any{
		"key":   key,
		"value": value,
	})
	if err != nil {
		return fmt.Errorf("firestore: set %s: %w", key, err)
	}
	return nil
}

// Get retrieves the value for a key from the memory collection.
func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	doc, err := s.client.Collection(memoryCol).Doc(key).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("firestore: get %s: %w", key, err)
	}

	raw, err := doc.DataAt("value")
	if err != nil {
		return nil, fmt.Errorf("firestore: get value for %s: %w", key, err)
	}

	switch v := raw.(type) {
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("firestore: unexpected value type for key %s: %T", key, raw)
	}
}

// Delete removes a key from the memory collection.
func (s *Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.Collection(memoryCol).Doc(key).Delete(ctx)
	if err != nil {
		return fmt.Errorf("firestore: delete %s: %w", key, err)
	}
	return nil
}

// List returns all keys with the given prefix from the memory collection.
func (s *Store) List(ctx context.Context, prefix string) ([]string, error) {
	var iter *firestore.DocumentIterator
	if prefix != "" {
		// Use a range query: keys >= prefix and < prefix with last rune incremented.
		nextPrefix := prefixIncrement(prefix)
		iter = s.client.Collection(memoryCol).
			Where("key", ">=", prefix).
			Where("key", "<", nextPrefix).
			Documents(ctx)
	} else {
		iter = s.client.Collection(memoryCol).Documents(ctx)
	}
	defer iter.Stop()

	var keys []string
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("firestore: list prefix %q: %w", prefix, err)
		}
		key, _ := doc.DataAt("key")
		if s, ok := key.(string); ok {
			keys = append(keys, s)
		}
	}
	return keys, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// loadMessages reads all documents from the messages subcollection of a session.
func (s *Store) loadMessages(ctx context.Context, sessionID string) ([]metadata.Message, error) {
	iter := s.client.Collection(sessionsCol).Doc(sessionID).Collection(messagesCol).
		OrderBy("createdAt", firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	var msgs []metadata.Message
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("firestore: load messages for session %s: %w", sessionID, err)
		}
		var m metadata.Message
		if err := doc.DataTo(&m); err != nil {
			return nil, fmt.Errorf("firestore: decode message %s: %w", doc.Ref.ID, err)
		}
		m.ID = doc.Ref.ID
		msgs = append(msgs, m)
	}
	return msgs, nil
}

// deleteSubcollection removes all documents in a subcollection.
// Firestore does not automatically delete subcollections.
func (s *Store) deleteSubcollection(ctx context.Context, col *firestore.CollectionRef) error {
	iter := col.Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			return nil
		}
		if err != nil {
			return fmt.Errorf("iterate subcollection: %w", err)
		}
		_, err = doc.Ref.Delete(ctx)
		if err != nil {
			return fmt.Errorf("delete subdocument %s: %w", doc.Ref.ID, err)
		}
	}
}

// prefixIncrement returns the lexicographically next string after prefix.
// Used for Firestore range queries on string prefixes.
func prefixIncrement(prefix string) string {
	if len(prefix) == 0 {
		return ""
	}
	runes := []rune(prefix)
	runes[len(runes)-1]++
	return string(runes)
}

// toSessionMap converts a Session to a map suitable for Firestore.
// Messages are stored in a subcollection, not inline.
func toSessionMap(sess *metadata.Session) map[string]any {
	return map[string]any{
		"id":        sess.ID,
		"agentID":   sess.AgentID,
		"state":     sess.State,
		"createdAt": sess.CreatedAt,
		"updatedAt": sess.UpdatedAt,
	}
}

// toMessageMap converts a Message to a map suitable for Firestore.
func toMessageMap(m *metadata.Message) map[string]any {
	return map[string]any{
		"id":        m.ID,
		"role":      m.Role,
		"content":   m.Content,
		"toolCalls": m.ToolCalls,
		"createdAt": m.CreatedAt,
	}
}

// toToolMap converts a ToolDef to a map suitable for Firestore.
func toToolMap(tool *metadata.ToolDef) map[string]any {
	return map[string]any{
		"id":          tool.ID,
		"agentID":     tool.AgentID,
		"name":        tool.Name,
		"description": tool.Description,
		"parameters":  tool.Parameters,
		"createdAt":   tool.CreatedAt,
	}
}

// toolFromMap reconstructs a ToolDef from a raw Firestore map.
func toolFromMap(m map[string]any) *metadata.ToolDef {
	if m == nil {
		return nil
	}
	t := &metadata.ToolDef{}
	if v, ok := m["id"].(string); ok {
		t.ID = v
	}
	if v, ok := m["agentID"].(string); ok {
		t.AgentID = v
	}
	if v, ok := m["name"].(string); ok {
		t.Name = v
	}
	if v, ok := m["description"].(string); ok {
		t.Description = v
	}
	t.Parameters = m["parameters"]
	if v, ok := m["createdAt"].(int64); ok {
		t.CreatedAt = v
	}
	return t
}

// isNotFound returns true if the error indicates a missing document.
func isNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not-found")
}

// Verify interface compliance at compile time.
var _ metadata.Store = (*Store)(nil)
