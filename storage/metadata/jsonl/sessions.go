package jsonl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/ratrektlabs/rakit/storage/metadata"
)

// CreateSession creates and durably journals a new session.
func (s *Store) CreateSession(ctx context.Context, agentID, userID string) (*metadata.Session, error) {
	var created *metadata.Session
	err := s.withLock(ctx, func() error {
		for i := 0; i < 10; i++ {
			id := newID("ses-")
			path, err := s.sessionPath(id)
			if err != nil {
				return err
			}
			if _, err := os.Stat(path); err == nil {
				continue
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}

			now := time.Now().UnixMilli()
			sess := &metadata.Session{
				ID:             id,
				AgentID:        agentID,
				UserID:         userID,
				Messages:       []metadata.Message{},
				State:          map[string]any{},
				OpenInterrupts: []metadata.Interrupt{},
				Revision:       1,
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			e, err := newEnvelope("session.created", id, "", metaFromSession(sess))
			if err != nil {
				return fmt.Errorf("jsonl: create session: %w", err)
			}
			if err := appendEnvelope(path, e); err != nil {
				return fmt.Errorf("jsonl: create session: %w", err)
			}
			// The transcript is authoritative. The index is only a rebuildable
			// acceleration structure, so an index failure must not make a
			// durably-created session look like a failed operation.
			_ = s.appendIndexLocked(sess, false)
			created = sess
			return nil
		}
		return errors.New("jsonl: unable to allocate a unique session id")
	})
	return created, err
}

// GetSession loads a session by replaying its transcript. A missing or
// logically deleted session returns nil, nil.
func (s *Store) GetSession(ctx context.Context, id string) (*metadata.Session, error) {
	var result *metadata.Session
	err := s.withLock(ctx, func() error {
		var err error
		result, err = s.getSessionLocked(id)
		return err
	})
	return result, err
}

// UpdateSession persists a complete session snapshot using optimistic
// revision checking. The JSONL transcript receives only the message and
// metadata events needed to reach the supplied snapshot.
func (s *Store) UpdateSession(ctx context.Context, sess *metadata.Session) error {
	if sess == nil {
		return errors.New("jsonl: cannot persist a nil session")
	}
	if err := validateSessionID(sess.ID); err != nil {
		return err
	}
	var persisted *metadata.Session
	err := s.withLock(ctx, func() error {
		current, err := s.getSessionLocked(sess.ID)
		if err != nil {
			return err
		}
		if current == nil {
			return fmt.Errorf("jsonl: update session %q: not found", sess.ID)
		}
		if sess.Revision != current.Revision {
			return fmt.Errorf("%w: %q is at revision %d, supplied %d", metadata.ErrSessionConflict, sess.ID, current.Revision, sess.Revision)
		}
		if current.Revision == ^uint64(0) {
			return fmt.Errorf("jsonl: session %q revision overflow", sess.ID)
		}

		next := cloneSession(sess)
		next.ID = current.ID
		next.CreatedAt = current.CreatedAt
		next.Revision = current.Revision + 1
		next.UpdatedAt = time.Now().UnixMilli()
		if next.UpdatedAt <= current.UpdatedAt {
			if current.UpdatedAt == int64(^uint64(0)>>1) {
				return fmt.Errorf("jsonl: session %q updatedAt overflow", sess.ID)
			}
			next.UpdatedAt = current.UpdatedAt + 1
		}
		normalizeSession(next)

		path, err := s.sessionPath(sess.ID)
		if err != nil {
			return err
		}
		transactionID := newID("txn-")
		messageEvents, err := sessionMessageChanges(transactionID, sess.ID, current.Messages, next.Messages)
		if err != nil {
			return err
		}
		metaEvent, err := newEnvelope("session.updated", sess.ID, "", metaFromSession(next))
		if err != nil {
			return fmt.Errorf("jsonl: marshal session update: %w", err)
		}
		metaEvent.TransactionID = transactionID

		// Message records and their metadata commit share a transaction ID.
		// Replay applies the message records only after it sees this final
		// session.updated marker, so a crash between appends leaves no visible
		// partial session update.
		for _, event := range messageEvents {
			if err := appendEnvelope(path, event); err != nil {
				return fmt.Errorf("jsonl: append session transaction: %w", err)
			}
		}
		if err := appendEnvelope(path, metaEvent); err != nil {
			return fmt.Errorf("jsonl: update session: %w", err)
		}

		persisted = next
		// The transcript commit above is the source of truth. Returning an
		// error after that point would invite a retry with a stale revision.
		// ListSessions repairs this derived index by scanning transcripts.
		_ = s.appendIndexLocked(next, false)
		return nil
	})
	if err != nil {
		return err
	}
	*sess = *persisted
	return nil
}

func cloneSession(sess *metadata.Session) *metadata.Session {
	raw, err := json.Marshal(sess)
	if err != nil {
		copy := *sess
		return &copy
	}
	var copy metadata.Session
	if err := json.Unmarshal(raw, &copy); err != nil {
		copy = *sess
	}
	return &copy
}

func sessionMessageChanges(transactionID, sessionID string, current, next []metadata.Message) ([]envelope, error) {
	var events []envelope
	incremental := len(next) >= len(current)
	if incremental {
		for i := range current {
			if current[i].ID != next[i].ID {
				incremental = false
				break
			}
		}
	}
	if incremental {
		for i := range next {
			if i < len(current) && reflect.DeepEqual(current[i], next[i]) {
				continue
			}
			e, err := newEnvelope("message.upsert", sessionID, next[i].ID, next[i])
			if err != nil {
				return nil, fmt.Errorf("jsonl: marshal message %q: %w", next[i].ID, err)
			}
			e.TransactionID = transactionID
			events = append(events, e)
		}
		return events, nil
	}

	payload := messageReplacement{Messages: next}
	e, err := newEnvelope("messages.replaced", sessionID, "", payload)
	if err != nil {
		return nil, fmt.Errorf("jsonl: marshal message replacement: %w", err)
	}
	e.TransactionID = transactionID
	return []envelope{e}, nil
}

// DeleteSession appends a tombstone before moving and removing the physical
// transcript. If a process crashes after the tombstone, replay still treats
// the session as deleted.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	if err := validateSessionID(id); err != nil {
		return err
	}
	return s.withLock(ctx, func() error {
		path, err := s.sessionPath(id)
		if err != nil {
			return err
		}
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		current, err := s.getSessionLocked(id)
		if err != nil {
			return err
		}
		if current != nil {
			e, err := newEnvelope("session.deleted", id, "", map[string]string{"id": id})
			if err != nil {
				return err
			}
			if err := appendEnvelope(path, e); err != nil {
				return fmt.Errorf("jsonl: delete session %q: %w", id, err)
			}
		}
		_ = s.appendIndexLocked(nil, true, id)

		deletedPath := path + ".deleted"
		_ = os.Remove(deletedPath)
		if err := os.Rename(path, deletedPath); err != nil {
			return fmt.Errorf("jsonl: stage deleted session %q: %w", id, err)
		}
		if err := syncDir(filepath.Dir(path)); err != nil {
			return fmt.Errorf("jsonl: sync deleted session %q: %w", id, err)
		}
		if err := os.Remove(deletedPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("jsonl: remove deleted session %q: %w", id, err)
		}
		return syncDir(filepath.Dir(path))
	})
}

// ListSessions returns lightweight session summaries sorted by updated time.
func (s *Store) ListSessions(ctx context.Context, agentID string) ([]*metadata.Session, error) {
	return s.listSessions(ctx, func(sess *metadata.Session) bool { return sess.AgentID == agentID })
}

// ListSessionsByUser returns lightweight summaries filtered by agent and user.
func (s *Store) ListSessionsByUser(ctx context.Context, agentID, userID string) ([]*metadata.Session, error) {
	return s.listSessions(ctx, func(sess *metadata.Session) bool {
		return sess.AgentID == agentID && sess.UserID == userID
	})
}

func (s *Store) listSessions(ctx context.Context, match func(*metadata.Session) bool) ([]*metadata.Session, error) {
	var result []*metadata.Session
	err := s.withLock(ctx, func() error {
		all, err := s.scanSessionsLocked()
		if err != nil {
			return err
		}
		for _, sess := range all {
			if match(sess) {
				result = append(result, summarySession(summaryFromSession(sess)))
			}
		}
		sortSessions(result)
		return s.rebuildIndexLocked(all)
	})
	if result == nil {
		result = []*metadata.Session{}
	}
	return result, err
}

func (s *Store) getSessionLocked(id string) (*metadata.Session, error) {
	path, err := s.sessionPath(id)
	if err != nil {
		return nil, err
	}
	records, err := readEnvelopes(path)
	if err != nil {
		return nil, fmt.Errorf("jsonl: read session %q: %w", id, err)
	}
	if len(records) == 0 {
		return nil, nil
	}
	sess, deleted, err := replaySession(id, records)
	if err != nil {
		return nil, fmt.Errorf("jsonl: replay session %q: %w", id, err)
	}
	if deleted {
		return nil, nil
	}
	return sess, nil
}

func (s *Store) scanSessionsLocked() ([]*metadata.Session, error) {
	entries, err := os.ReadDir(s.transcriptDir)
	if err != nil {
		return nil, err
	}
	var sessions []*metadata.Session
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".jsonl")
		if err := validateSessionID(id); err != nil {
			return nil, err
		}
		sess, err := s.getSessionLocked(id)
		if err != nil {
			return nil, err
		}
		if sess != nil {
			sessions = append(sessions, sess)
		}
	}
	return sessions, nil
}

func replaySession(id string, records []envelope) (*metadata.Session, bool, error) {
	var sess metadata.Session
	created := false
	deleted := false
	pending := make(map[string][]envelope)
	committed := make(map[string]struct{})
	for _, record := range records {
		if record.SessionID != "" && record.SessionID != id {
			return nil, false, fmt.Errorf("record %s belongs to session %q", record.EventID, record.SessionID)
		}
		switch record.Type {
		case "session.created":
			if created || deleted {
				return nil, false, errors.New("invalid session.created position")
			}
			var meta sessionMeta
			if err := json.Unmarshal(record.Payload, &meta); err != nil {
				return nil, false, fmt.Errorf("decode session.created: %w", err)
			}
			if meta.ID == "" || meta.ID != id {
				return nil, false, fmt.Errorf("session.created id %q does not match filename %q", meta.ID, id)
			}
			meta.apply(&sess)
			created = true
		case "session.updated":
			if !created || deleted {
				return nil, false, errors.New("invalid session.updated position")
			}
			var meta sessionMeta
			if err := json.Unmarshal(record.Payload, &meta); err != nil {
				return nil, false, fmt.Errorf("decode session.updated: %w", err)
			}
			if meta.ID == "" || meta.ID != id {
				return nil, false, fmt.Errorf("session.updated id %q does not match filename %q", meta.ID, id)
			}
			if record.TransactionID != "" {
				if _, ok := committed[record.TransactionID]; ok {
					return nil, false, fmt.Errorf("duplicate committed transaction %q", record.TransactionID)
				}
				for _, event := range pending[record.TransactionID] {
					if err := applySessionMessageEvent(&sess, event); err != nil {
						return nil, false, err
					}
				}
				delete(pending, record.TransactionID)
				committed[record.TransactionID] = struct{}{}
			}
			messages := sess.Messages
			meta.apply(&sess)
			sess.Messages = messages
		case "message.upsert":
			if !created || deleted {
				return nil, false, errors.New("invalid message.upsert position")
			}
			if record.TransactionID != "" {
				if _, ok := committed[record.TransactionID]; ok {
					return nil, false, fmt.Errorf("message event follows committed transaction %q", record.TransactionID)
				}
				pending[record.TransactionID] = append(pending[record.TransactionID], record)
				continue
			}
			if err := applySessionMessageEvent(&sess, record); err != nil {
				return nil, false, err
			}
		case "messages.replaced":
			if !created || deleted {
				return nil, false, errors.New("invalid messages.replaced position")
			}
			if record.TransactionID != "" {
				if _, ok := committed[record.TransactionID]; ok {
					return nil, false, fmt.Errorf("message event follows committed transaction %q", record.TransactionID)
				}
				pending[record.TransactionID] = append(pending[record.TransactionID], record)
				continue
			}
			if err := applySessionMessageEvent(&sess, record); err != nil {
				return nil, false, err
			}
		case "session.deleted":
			if !created || deleted {
				return nil, false, errors.New("invalid session.deleted position")
			}
			deleted = true
		default:
			return nil, false, fmt.Errorf("unknown session event type %q", record.Type)
		}
	}
	if !created {
		return nil, false, errors.New("session transcript has no session.created record")
	}
	if sess.ID != "" && sess.ID != id {
		return nil, false, fmt.Errorf("session metadata id %q does not match filename %q", sess.ID, id)
	}
	sess.ID = id
	normalizeSession(&sess)
	return &sess, deleted, nil
}

func applySessionMessageEvent(sess *metadata.Session, record envelope) error {
	switch record.Type {
	case "message.upsert":
		var message metadata.Message
		if err := json.Unmarshal(record.Payload, &message); err != nil {
			return fmt.Errorf("decode message.upsert: %w", err)
		}
		for i := range sess.Messages {
			if sess.Messages[i].ID == message.ID {
				sess.Messages[i] = message
				return nil
			}
		}
		sess.Messages = append(sess.Messages, message)
		return nil
	case "messages.replaced":
		var replacement messageReplacement
		if err := json.Unmarshal(record.Payload, &replacement); err != nil {
			return fmt.Errorf("decode messages.replaced: %w", err)
		}
		sess.Messages = replacement.Messages
		return nil
	default:
		return fmt.Errorf("invalid session message event type %q", record.Type)
	}
}

func (s *Store) appendIndexLocked(sess *metadata.Session, deleted bool, deletedID ...string) error {
	path := filepath.Join(s.storeDir, "session-index.jsonl")
	var (
		typ string
		id  string
		val any
	)
	if deleted {
		if len(deletedID) != 1 || deletedID[0] == "" {
			return errors.New("jsonl: deleted session index record requires an id")
		}
		typ = "session.index.delete"
		id = deletedID[0]
		val = map[string]string{"id": id}
	} else {
		typ = "session.index.upsert"
		id = sess.ID
		val = summaryFromSession(sess)
	}
	e, err := newEnvelope(typ, id, id, val)
	if err != nil {
		return err
	}
	if err := appendEnvelope(path, e); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() <= mutableCompactionBytes {
		return nil
	}
	records, err := readEnvelopes(path)
	if err != nil {
		return err
	}
	live := make(map[string]envelope)
	for _, record := range records {
		switch record.Type {
		case "session.index.upsert":
			live[record.Key] = record
		case "session.index.delete":
			delete(live, record.Key)
		default:
			return fmt.Errorf("jsonl: unknown session index event type %q", record.Type)
		}
	}
	if (len(records)-len(live))*2 < len(records) {
		return nil
	}
	all, err := s.scanSessionsLocked()
	if err != nil {
		return err
	}
	return s.rebuildIndexLocked(all)
}

func (s *Store) rebuildIndexLocked(sessions []*metadata.Session) error {
	path := filepath.Join(s.storeDir, "session-index.jsonl")
	var records []envelope
	for _, sess := range sessions {
		e, err := newEnvelope("session.index.upsert", sess.ID, sess.ID, summaryFromSession(sess))
		if err != nil {
			return err
		}
		records = append(records, e)
	}
	return rewriteEnvelopes(path, records)
}
