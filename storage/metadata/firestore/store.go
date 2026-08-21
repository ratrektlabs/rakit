package firestore

import (
	"context"
	"fmt"
	"sort"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/ratrektlabs/rakit/storage/metadata"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	sessionsCol   = "sessions"
	messagesCol   = "messages"
	toolsCol      = "tools"
	skillsCol     = "skills"
	memoryCol     = "memory"
	mcpServersCol = "mcp_servers"
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

// CreateSession creates a new session for the given agentID and userID and returns it.
func (s *Store) CreateSession(ctx context.Context, agentID, userID string) (*metadata.Session, error) {
	now := time.Now().Unix()
	session := &metadata.Session{
		ID:             "",
		AgentID:        agentID,
		UserID:         userID,
		Messages:       []metadata.Message{},
		State:          map[string]any{},
		OpenInterrupts: []metadata.Interrupt{},
		Revision:       1,
		CreatedAt:      now,
		UpdatedAt:      now,
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
		if isNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("firestore: get session %s: %w", id, err)
	}

	var session metadata.Session
	if err := doc.DataTo(&session); err != nil {
		return nil, fmt.Errorf("firestore: decode session %s: %w", id, err)
	}
	// Older adapter versions wrote acronym-style field names even though the
	// metadata tags used lower camel case. Keep those documents readable; the
	// next update rewrites them using the canonical lower-camel names.
	data := doc.Data()
	if session.AgentID == "" {
		session.AgentID = stringFromMap(data, "agentId", "agentID")
	}
	if session.UserID == "" {
		session.UserID = stringFromMap(data, "userId", "userID")
	}
	if session.ParentSessionID == "" {
		session.ParentSessionID = stringFromMap(data, "parentSessionId", "parentSessionID")
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
	if err := validateSessionUpdate(sess); err != nil {
		return err
	}

	existingMessages, err := s.loadExistingMessages(ctx, sess.ID)
	if err != nil {
		return err
	}
	committed, err := s.commitSessionUpdate(ctx, sess, existingMessages)
	if err != nil {
		return fmt.Errorf("firestore: update session %s: %w", sess.ID, err)
	}
	applyCommittedSession(sess, committed)
	return nil
}

func validateSessionUpdate(sess *metadata.Session) error {
	if sess == nil {
		return fmt.Errorf("firestore: update session: nil session")
	}
	if sess.ID == "" {
		return fmt.Errorf("firestore: update session: empty id")
	}
	const maxStoredRevision uint64 = 1<<63 - 1
	if sess.Revision >= maxStoredRevision {
		return fmt.Errorf("firestore: update session %s: revision overflow", sess.ID)
	}
	return nil
}

func (s *Store) loadExistingMessages(ctx context.Context, sessionID string) ([]metadata.Message, error) {
	messages, err := s.loadMessages(ctx, sessionID)
	if err != nil && !isNotFound(err) {
		return nil, err
	}
	if messages == nil {
		return []metadata.Message{}, nil
	}
	return messages, nil
}

func (s *Store) commitSessionUpdate(
	ctx context.Context,
	requested *metadata.Session,
	existingMessages []metadata.Message,
) (*metadata.Session, error) {
	ref := s.client.Collection(sessionsCol).Doc(requested.ID)
	committed := *requested
	committed.Revision = requested.Revision + 1
	if committed.OpenInterrupts == nil {
		committed.OpenInterrupts = []metadata.Interrupt{}
	}

	err := s.client.RunTransaction(ctx, func(_ context.Context, tx *firestore.Transaction) error {
		if err := readStoredSessionMetadata(tx, ref, requested.Revision, &committed); err != nil {
			return err
		}
		if err := tx.Set(ref, toSessionMap(&committed)); err != nil {
			return fmt.Errorf("set session: %w", err)
		}
		return reconcileSessionMessages(tx, ref.Collection(messagesCol), requested.Messages, existingMessages)
	})
	if err != nil {
		return nil, err
	}
	return &committed, nil
}

func readStoredSessionMetadata(
	tx *firestore.Transaction,
	ref *firestore.DocumentRef,
	expectedRevision uint64,
	committed *metadata.Session,
) error {
	doc, err := tx.Get(ref)
	if err != nil {
		if isNotFound(err) {
			return fmt.Errorf("not found")
		}
		return fmt.Errorf("get session: %w", err)
	}
	data := doc.Data()
	storedRevision, err := revisionFromMap(data)
	if err != nil {
		return err
	}
	if storedRevision != expectedRevision {
		return fmt.Errorf(
			"%w (stored revision %d, supplied %d)",
			metadata.ErrSessionConflict,
			storedRevision,
			expectedRevision,
		)
	}
	committed.CreatedAt, err = int64FromMap(data, "createdAt")
	if err != nil {
		return err
	}
	currentUpdatedAt, err := int64FromMap(data, "updatedAt")
	if err != nil {
		return err
	}
	committed.UpdatedAt, err = nextFirestoreUpdatedAt(currentUpdatedAt)
	return err
}

func nextFirestoreUpdatedAt(current int64) (int64, error) {
	now := time.Now().Unix()
	if now > current {
		return now, nil
	}
	if current == int64(^uint64(0)>>1) {
		return 0, fmt.Errorf("updatedAt overflow")
	}
	return current + 1, nil
}

func reconcileSessionMessages(
	tx *firestore.Transaction,
	collection *firestore.CollectionRef,
	current []metadata.Message,
	existing []metadata.Message,
) error {
	currentIDs := make(map[string]struct{}, len(current))
	for _, message := range current {
		if message.ID == "" {
			continue
		}
		currentIDs[message.ID] = struct{}{}
		if err := tx.Set(collection.Doc(message.ID), toMessageMap(&message)); err != nil {
			return fmt.Errorf("set message %s: %w", message.ID, err)
		}
	}
	for _, message := range existing {
		if _, retained := currentIDs[message.ID]; retained || message.ID == "" {
			continue
		}
		if err := tx.Delete(collection.Doc(message.ID)); err != nil {
			return fmt.Errorf("delete message %s: %w", message.ID, err)
		}
	}
	return nil
}

func applyCommittedSession(target *metadata.Session, committed *metadata.Session) {
	target.Revision = committed.Revision
	target.CreatedAt = committed.CreatedAt
	target.UpdatedAt = committed.UpdatedAt
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
	queries := []firestore.Query{
		s.client.Collection(sessionsCol).Where("agentId", "==", agentID),
		// Compatibility with documents written before field names were aligned
		// with metadata.Session's JSON and Firestore tags.
		s.client.Collection(sessionsCol).Where("agentID", "==", agentID),
	}
	return collectSessionSummaries(ctx, queries, "list sessions")
}

// ListSessionsByUser returns sessions for the given agentID and userID.
func (s *Store) ListSessionsByUser(ctx context.Context, agentID, userID string) ([]*metadata.Session, error) {
	queries := []firestore.Query{
		s.client.Collection(sessionsCol).
			Where("agentId", "==", agentID).
			Where("userId", "==", userID),
		s.client.Collection(sessionsCol).
			Where("agentID", "==", agentID).
			Where("userID", "==", userID),
	}
	return collectSessionSummaries(ctx, queries, "list sessions by user")
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
// Scoped Memory
// ---------------------------------------------------------------------------

// SetMemory stores a scoped key-value pair in the memory collection.
func (s *Store) SetMemory(ctx context.Context, scope metadata.MemoryScope, scopeID, key string, value []byte) error {
	docID := metadata.ScopedKey(scope, scopeID, key)
	if docID == "" {
		return fmt.Errorf("firestore: set memory: empty key")
	}
	ref := s.client.Collection(memoryCol).Doc(docID)
	_, err := ref.Set(ctx, map[string]any{
		"key":   docID,
		"value": value,
	})
	if err != nil {
		return fmt.Errorf("firestore: set memory %s: %w", docID, err)
	}
	return nil
}

// GetMemory retrieves a scoped value from the memory collection.
func (s *Store) GetMemory(ctx context.Context, scope metadata.MemoryScope, scopeID, key string) ([]byte, error) {
	docID := metadata.ScopedKey(scope, scopeID, key)
	doc, err := s.client.Collection(memoryCol).Doc(docID).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("firestore: get memory %s: %w", docID, err)
	}

	raw, err := doc.DataAt("value")
	if err != nil {
		return nil, fmt.Errorf("firestore: get memory value for %s: %w", docID, err)
	}

	switch v := raw.(type) {
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("firestore: unexpected value type for key %s: %T", docID, raw)
	}
}

// DeleteMemory removes a scoped key from the memory collection.
func (s *Store) DeleteMemory(ctx context.Context, scope metadata.MemoryScope, scopeID, key string) error {
	docID := metadata.ScopedKey(scope, scopeID, key)
	_, err := s.client.Collection(memoryCol).Doc(docID).Delete(ctx)
	if err != nil {
		return fmt.Errorf("firestore: delete memory %s: %w", docID, err)
	}
	return nil
}

// ListMemory returns all keys matching the scoped prefix from the memory collection.
func (s *Store) ListMemory(ctx context.Context, scope metadata.MemoryScope, scopeID, prefix string) ([]string, error) {
	scopedPrefix := metadata.ScopedKey(scope, scopeID, prefix)

	var iter *firestore.DocumentIterator
	if scopedPrefix != "" {
		nextPrefix := prefixIncrement(scopedPrefix)
		iter = s.client.Collection(memoryCol).
			Where("key", ">=", scopedPrefix).
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
			return nil, fmt.Errorf("firestore: list memory prefix %q: %w", scopedPrefix, err)
		}
		key, _ := doc.DataAt("key")
		if s, ok := key.(string); ok {
			keys = append(keys, s)
		}
	}
	return keys, nil
}

// ---------------------------------------------------------------------------
// Legacy flat KV (delegates to global-scoped memory)
// ---------------------------------------------------------------------------

// Set stores a key-value pair using global scope.
func (s *Store) Set(ctx context.Context, key string, value []byte) error {
	return s.SetMemory(ctx, metadata.ScopeGlobal, "", key, value)
}

// Get retrieves a value by key using global scope.
func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	return s.GetMemory(ctx, metadata.ScopeGlobal, "", key)
}

// Delete removes a key using global scope.
func (s *Store) Delete(ctx context.Context, key string) error {
	return s.DeleteMemory(ctx, metadata.ScopeGlobal, "", key)
}

// List returns all keys with the given prefix using global scope.
func (s *Store) List(ctx context.Context, prefix string) ([]string, error) {
	return s.ListMemory(ctx, metadata.ScopeGlobal, "", prefix)
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
	interrupts := sess.OpenInterrupts
	if interrupts == nil {
		interrupts = []metadata.Interrupt{}
	}
	return map[string]any{
		"id":              sess.ID,
		"agentId":         sess.AgentID,
		"userId":          sess.UserID,
		"parentSessionId": sess.ParentSessionID,
		"state":           sess.State,
		"openInterrupts":  interrupts,
		"revision":        int64(sess.Revision),
		"createdAt":       sess.CreatedAt,
		"updatedAt":       sess.UpdatedAt,
	}
}

func collectSessionSummaries(
	ctx context.Context,
	queries []firestore.Query,
	operation string,
) ([]*metadata.Session, error) {
	byID := make(map[string]*metadata.Session)
	for _, query := range queries {
		iter := query.Documents(ctx)
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				iter.Stop()
				return nil, fmt.Errorf("firestore: %s: %w", operation, err)
			}
			sess, err := sessionSummaryFromMap(doc.Ref.ID, doc.Data())
			if err != nil {
				iter.Stop()
				return nil, err
			}
			byID[sess.ID] = sess
		}
		iter.Stop()
	}

	sessions := make([]*metadata.Session, 0, len(byID))
	for _, sess := range byID {
		sessions = append(sessions, sess)
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		if sessions[i].UpdatedAt != sessions[j].UpdatedAt {
			return sessions[i].UpdatedAt > sessions[j].UpdatedAt
		}
		return sessions[i].ID < sessions[j].ID
	})
	return sessions, nil
}

func sessionSummaryFromMap(id string, data map[string]any) (*metadata.Session, error) {
	revision, err := revisionFromMap(data)
	if err != nil {
		return nil, err
	}
	createdAt, err := int64FromMap(data, "createdAt")
	if err != nil {
		return nil, err
	}
	updatedAt, err := int64FromMap(data, "updatedAt")
	if err != nil {
		return nil, err
	}
	return &metadata.Session{
		ID:              id,
		AgentID:         stringFromMap(data, "agentId", "agentID"),
		UserID:          stringFromMap(data, "userId", "userID"),
		ParentSessionID: stringFromMap(data, "parentSessionId", "parentSessionID"),
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
		Revision:        revision,
		Messages:        []metadata.Message{},
	}, nil
}

func stringFromMap(data map[string]any, key string, legacyKey string) string {
	if value, ok := data[key].(string); ok {
		return value
	}
	value, _ := data[legacyKey].(string)
	return value
}

func revisionFromMap(data map[string]any) (uint64, error) {
	raw, ok := data["revision"]
	if !ok || raw == nil {
		return 0, nil
	}
	switch value := raw.(type) {
	case int64:
		if value < 0 {
			return 0, fmt.Errorf("firestore: invalid negative session revision %d", value)
		}
		return uint64(value), nil
	case int:
		if value < 0 {
			return 0, fmt.Errorf("firestore: invalid negative session revision %d", value)
		}
		return uint64(value), nil
	case uint64:
		return value, nil
	case uint:
		return uint64(value), nil
	default:
		return 0, fmt.Errorf("firestore: invalid session revision type %T", raw)
	}
}

func int64FromMap(data map[string]any, key string) (int64, error) {
	raw, ok := data[key]
	if !ok || raw == nil {
		return 0, nil
	}
	switch value := raw.(type) {
	case int64:
		return value, nil
	case int:
		return int64(value), nil
	case uint64:
		const maxInt64Value uint64 = 1<<63 - 1
		if value > maxInt64Value {
			return 0, fmt.Errorf("firestore: %s exceeds int64", key)
		}
		return int64(value), nil
	case uint:
		return int64(value), nil
	default:
		return 0, fmt.Errorf("firestore: invalid %s type %T", key, raw)
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
	return status.Code(err) == codes.NotFound
}

// Verify interface compliance at compile time.
var _ metadata.Store = (*Store)(nil)

// ---------------------------------------------------------------------------
// MCP Servers
// ---------------------------------------------------------------------------

func (s *Store) SaveMCPServer(ctx context.Context, srv *metadata.MCPServerDef) error {
	if srv.Name == "" {
		return fmt.Errorf("firestore: save mcp server: name is required")
	}
	ref := s.client.Collection(mcpServersCol).Doc(srv.Name)
	_, err := ref.Set(ctx, srv)
	if err != nil {
		return fmt.Errorf("firestore: save mcp server %s: %w", srv.Name, err)
	}
	return nil
}

func (s *Store) GetMCPServer(ctx context.Context, name string) (*metadata.MCPServerDef, error) {
	doc, err := s.client.Collection(mcpServersCol).Doc(name).Get(ctx)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("firestore: get mcp server %s: %w", name, err)
	}
	var srv metadata.MCPServerDef
	if err := doc.DataTo(&srv); err != nil {
		return nil, fmt.Errorf("firestore: decode mcp server %s: %w", name, err)
	}
	return &srv, nil
}

func (s *Store) ListMCPServers(ctx context.Context, agentID string) ([]*metadata.MCPServerDef, error) {
	iter := s.client.Collection(mcpServersCol).
		Where("AgentID", "==", agentID).
		Documents(ctx)
	defer iter.Stop()

	var servers []*metadata.MCPServerDef
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("firestore: list mcp servers: %w", err)
		}
		var srv metadata.MCPServerDef
		if err := doc.DataTo(&srv); err != nil {
			return nil, fmt.Errorf("firestore: decode mcp server: %w", err)
		}
		servers = append(servers, &srv)
	}
	return servers, nil
}

func (s *Store) DeleteMCPServer(ctx context.Context, name string) error {
	_, err := s.client.Collection(mcpServersCol).Doc(name).Delete(ctx)
	if err != nil {
		return fmt.Errorf("firestore: delete mcp server %s: %w", name, err)
	}
	return nil
}
