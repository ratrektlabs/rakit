// Package jsonl provides a durable, filesystem-only metadata store.
//
// A Store keeps each session as an append-only JSONL transcript and stores
// mutable metadata in append-only JSONL logs. The store is scoped to one
// canonical workspace path, which gives it a Claude Code-style project
// directory layout while preserving the metadata.Store API.
package jsonl

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ratrektlabs/rakit/storage/metadata"
)

const (
	// schemaVersion is bumped when the on-disk envelope shape changes in an
	// incompatible way.
	schemaVersion = 1

	// mutableCompactionBytes is deliberately fixed for predictable local
	// storage behavior. Session transcripts are never compacted.
	mutableCompactionBytes = 16 << 20
)

// Config configures a JSONL metadata store.
type Config struct {
	// RootDir is the parent directory containing the projects directory.
	RootDir string
	// WorkspacePath is the project/workspace path used to scope this store.
	WorkspacePath string
}

// Store implements metadata.Store using only local files.
type Store struct {
	rootDir       string
	workspace     string
	projectDir    string
	storeDir      string
	locksDir      string
	transcriptDir string
}

var (
	_ metadata.Store = (*Store)(nil)
)

// envelope is the common on-disk record format for every JSONL file.
type envelope struct {
	Version       int             `json:"version"`
	EventID       string          `json:"eventId"`
	TransactionID string          `json:"transactionId,omitempty"`
	Type          string          `json:"type"`
	SessionID     string          `json:"sessionId,omitempty"`
	Key           string          `json:"key,omitempty"`
	Timestamp     int64           `json:"timestamp"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

// sessionMeta is the session portion shared by session.created and
// session.updated records. Messages are journaled separately.
type sessionMeta struct {
	ID              string               `json:"id"`
	AgentID         string               `json:"agentId"`
	UserID          string               `json:"userId"`
	ParentSessionID string               `json:"parentSessionId,omitempty"`
	State           map[string]any       `json:"state"`
	OpenInterrupts  []metadata.Interrupt `json:"openInterrupts"`
	Revision        uint64               `json:"revision"`
	CreatedAt       int64                `json:"createdAt"`
	UpdatedAt       int64                `json:"updatedAt"`
}

type messageReplacement struct {
	Messages []metadata.Message `json:"messages"`
}

type sessionIndexEntry struct {
	ID              string `json:"id"`
	AgentID         string `json:"agentId"`
	UserID          string `json:"userId"`
	ParentSessionID string `json:"parentSessionId,omitempty"`
	Revision        uint64 `json:"revision"`
	CreatedAt       int64  `json:"createdAt"`
	UpdatedAt       int64  `json:"updatedAt"`
}

type memoryValue struct {
	Value []byte `json:"value"`
}

// NewStore creates a JSONL metadata store and creates its private directory
// tree. RootDir and WorkspacePath must both be non-empty. WorkspacePath is
// converted to an absolute, cleaned path before it is encoded into the
// project directory name.
func NewStore(ctx context.Context, cfg Config) (*Store, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.RootDir) == "" {
		return nil, errors.New("jsonl: RootDir is required")
	}
	if strings.TrimSpace(cfg.WorkspacePath) == "" {
		return nil, errors.New("jsonl: WorkspacePath is required")
	}

	root, err := filepath.Abs(cfg.RootDir)
	if err != nil {
		return nil, fmt.Errorf("jsonl: resolve root directory: %w", err)
	}
	workspace, err := filepath.Abs(cfg.WorkspacePath)
	if err != nil {
		return nil, fmt.Errorf("jsonl: resolve workspace path: %w", err)
	}
	root = filepath.Clean(root)
	workspace = filepath.Clean(workspace)

	projectsDir := filepath.Join(root, "projects")
	slug, hash := workspaceSlug(workspace)
	projectDir := filepath.Join(projectsDir, slug+"-"+hash)
	s := &Store{
		rootDir:       root,
		workspace:     workspace,
		projectDir:    projectDir,
		storeDir:      filepath.Join(projectDir, "_store"),
		locksDir:      filepath.Join(projectDir, "_locks"),
		transcriptDir: projectDir,
	}

	for _, dir := range []string{root, projectsDir, projectDir, s.storeDir, s.locksDir} {
		if err := ensurePrivateDir(dir); err != nil {
			return nil, fmt.Errorf("jsonl: create directory %q: %w", dir, err)
		}
	}
	if err := recoverDeletedTranscripts(projectDir); err != nil {
		return nil, fmt.Errorf("jsonl: recover deleted transcripts: %w", err)
	}
	return s, nil
}

// Close releases no long-lived resources; JSONL opens and closes files per
// operation. It exists as a convenience for callers that treat local stores
// uniformly with database-backed stores.
func (s *Store) Close() error { return nil }

func workspaceSlug(workspace string) (string, string) {
	var b strings.Builder
	lastDash := false
	for _, r := range workspace {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-.")
	if slug == "" {
		slug = "workspace"
	}
	hash := sha256.Sum256([]byte(workspace))
	return slug, hex.EncodeToString(hash[:])[:12]
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}

func recoverDeletedTranscripts(projectDir string) error {
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return err
	}
	removed := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl.deleted") {
			continue
		}
		if err := os.Remove(filepath.Join(projectDir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		removed = true
	}
	if removed {
		return syncDir(projectDir)
	}
	return nil
}

func (s *Store) sessionPath(id string) (string, error) {
	if err := validateSessionID(id); err != nil {
		return "", err
	}
	path := filepath.Join(s.transcriptDir, id+".jsonl")
	rel, err := filepath.Rel(s.transcriptDir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("jsonl: unsafe session path %q", id)
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("jsonl: session path %q is a symlink", id)
	}
	return path, nil
}

func validateSessionID(id string) error {
	if id == "" || id == "." || id == ".." || len(id) > 256 {
		return fmt.Errorf("jsonl: invalid session id %q", id)
	}
	if strings.ContainsAny(id, `/\\`) {
		return fmt.Errorf("jsonl: invalid session id %q", id)
	}
	for _, r := range id {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-' {
			continue
		}
		return fmt.Errorf("jsonl: invalid session id %q", id)
	}
	return nil
}

func newID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return prefix + hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
}

func normalizeSession(s *metadata.Session) {
	if s.Messages == nil {
		s.Messages = []metadata.Message{}
	}
	if s.State == nil {
		s.State = map[string]any{}
	}
	if s.OpenInterrupts == nil {
		s.OpenInterrupts = []metadata.Interrupt{}
	}
}

func metaFromSession(s *metadata.Session) sessionMeta {
	state := s.State
	if state == nil {
		state = map[string]any{}
	}
	interrupts := s.OpenInterrupts
	if interrupts == nil {
		interrupts = []metadata.Interrupt{}
	}
	return sessionMeta{
		ID:              s.ID,
		AgentID:         s.AgentID,
		UserID:          s.UserID,
		ParentSessionID: s.ParentSessionID,
		State:           state,
		OpenInterrupts:  interrupts,
		Revision:        s.Revision,
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
	}
}

func (m sessionMeta) apply(s *metadata.Session) {
	s.ID = m.ID
	s.AgentID = m.AgentID
	s.UserID = m.UserID
	s.ParentSessionID = m.ParentSessionID
	s.State = m.State
	s.OpenInterrupts = m.OpenInterrupts
	s.Revision = m.Revision
	s.CreatedAt = m.CreatedAt
	s.UpdatedAt = m.UpdatedAt
}

func summaryFromSession(s *metadata.Session) sessionIndexEntry {
	return sessionIndexEntry{
		ID:              s.ID,
		AgentID:         s.AgentID,
		UserID:          s.UserID,
		ParentSessionID: s.ParentSessionID,
		Revision:        s.Revision,
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
	}
}

func summarySession(e sessionIndexEntry) *metadata.Session {
	return &metadata.Session{
		ID:              e.ID,
		AgentID:         e.AgentID,
		UserID:          e.UserID,
		ParentSessionID: e.ParentSessionID,
		Revision:        e.Revision,
		Messages:        []metadata.Message{},
		CreatedAt:       e.CreatedAt,
		UpdatedAt:       e.UpdatedAt,
	}
}

func newEnvelope(typ, sessionID, key string, payload any) (envelope, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return envelope{}, err
	}
	return envelope{
		Version:   schemaVersion,
		EventID:   newID("evt-"),
		Type:      typ,
		SessionID: sessionID,
		Key:       key,
		Timestamp: time.Now().UnixMilli(),
		Payload:   raw,
	}, nil
}

func appendEnvelope(path string, e envelope) error {
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	line = append(line, '\n')
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	_, writeErr := f.Write(line)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return syncDir(filepath.Dir(path))
}

func readEnvelopes(path string) ([]envelope, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	var records []envelope
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			complete := readErr == nil
			raw := bytes.TrimSuffix(line, []byte{'\n'})
			raw = bytes.TrimSuffix(raw, []byte{'\r'})
			if len(bytes.TrimSpace(raw)) == 0 {
				return nil, fmt.Errorf("jsonl: malformed empty record in %s", path)
			}
			var e envelope
			if err := json.Unmarshal(raw, &e); err != nil {
				kind := "complete"
				if !complete {
					kind = "unterminated final"
				}
				return nil, fmt.Errorf("jsonl: malformed %s record in %s: %w", kind, path, err)
			}
			if e.Version != schemaVersion {
				return nil, fmt.Errorf("jsonl: unsupported envelope version %d in %s", e.Version, path)
			}
			if e.EventID == "" || e.Type == "" {
				return nil, fmt.Errorf("jsonl: incomplete envelope in %s", path)
			}
			records = append(records, e)
			if !complete {
				if err := repairFinalNewline(path); err != nil {
					return nil, fmt.Errorf("jsonl: repair final record in %s: %w", path, err)
				}
				return records, nil
			}
		}
		if readErr == io.EOF {
			return records, nil
		}
		if readErr != nil {
			return nil, fmt.Errorf("jsonl: read %s: %w", path, readErr)
		}
	}
}

func repairFinalNewline(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write([]byte{'\n'})
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if runtime.GOOS == "windows" {
			return nil
		}
		return err
	}
	if runtime.GOOS == "windows" {
		return f.Close()
	}
	err = f.Sync()
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func rewriteEnvelopes(path string, records []envelope) error {
	dir := filepath.Dir(path)
	if err := ensurePrivateDir(dir); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".jsonl-rewrite-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	writer := bufio.NewWriter(tmp)
	for _, record := range records {
		line, err := json.Marshal(record)
		if err != nil {
			_ = tmp.Close()
			return err
		}
		if _, err := writer.Write(append(line, '\n')); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := replaceFile(tmpPath, path); err != nil {
		return err
	}
	return syncDir(dir)
}

func sortSessions(sessions []*metadata.Session) {
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].UpdatedAt != sessions[j].UpdatedAt {
			return sessions[i].UpdatedAt > sessions[j].UpdatedAt
		}
		return sessions[i].ID < sessions[j].ID
	})
}
