package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ratrektlabs/rakit/storage/metadata"

	_ "modernc.org/sqlite"
)

var _ metadata.Store = (*Store)(nil)

// Store implements metadata.Store backed by SQLite.
type Store struct {
	db *sql.DB
}

// NewStore opens (or creates) a SQLite database at dbPath and runs migrations.
func NewStore(ctx context.Context, dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("sqlite: create directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %q: %w", dbPath, err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite: set WAL: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite: migrate: %w", err)
	}

	return s, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id                TEXT PRIMARY KEY,
			agent_id          TEXT NOT NULL,
			user_id           TEXT DEFAULT '',
			parent_session_id TEXT DEFAULT '',
			state             TEXT DEFAULT '{}',
			created_at        INTEGER NOT NULL,
			updated_at        INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS session_messages (
			id         TEXT PRIMARY KEY,
			session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
			role       TEXT NOT NULL,
			content    TEXT DEFAULT '',
			tool_calls TEXT DEFAULT '[]',
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tools (
			id            TEXT PRIMARY KEY,
			agent_id      TEXT NOT NULL,
			name          TEXT NOT NULL UNIQUE,
			description   TEXT DEFAULT '',
			parameters    TEXT DEFAULT 'null',
			handler       TEXT DEFAULT 'http',
			endpoint      TEXT DEFAULT '',
			headers       TEXT DEFAULT '{}',
			input_mapping TEXT DEFAULT '{}',
			script_path   TEXT DEFAULT '',
			created_at    INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS skills (
			name         TEXT PRIMARY KEY,
			description  TEXT DEFAULT '',
			version      TEXT DEFAULT '',
			instructions TEXT DEFAULT '',
			tools        TEXT DEFAULT '[]',
			config       TEXT DEFAULT '{}',
			resources    TEXT DEFAULT '[]',
			enabled      INTEGER NOT NULL DEFAULT 1
		)`,
		`CREATE TABLE IF NOT EXISTS memory (
			key   TEXT PRIMARY KEY,
			value BLOB NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS mcp_servers (
			id         TEXT PRIMARY KEY,
			agent_id   TEXT NOT NULL,
			name       TEXT NOT NULL UNIQUE,
			url        TEXT NOT NULL,
			transport  TEXT DEFAULT 'http',
			headers    TEXT DEFAULT '{}',
			enabled    INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL
		)`,
	}
	for _, m := range migrations {
		if _, err := s.db.ExecContext(ctx, m); err != nil {
			return err
		}
	}

	// Alter existing tables to add new columns (no-op if column already exists).
	alters := []string{
		`ALTER TABLE tools ADD COLUMN handler TEXT DEFAULT 'http'`,
		`ALTER TABLE tools ADD COLUMN endpoint TEXT DEFAULT ''`,
		`ALTER TABLE tools ADD COLUMN headers TEXT DEFAULT '{}'`,
		`ALTER TABLE tools ADD COLUMN input_mapping TEXT DEFAULT '{}'`,
		`ALTER TABLE tools ADD COLUMN script_path TEXT DEFAULT ''`,
		`ALTER TABLE tools ADD COLUMN response_field TEXT DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN user_id TEXT DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN parent_session_id TEXT DEFAULT ''`,
		`ALTER TABLE mcp_servers ADD COLUMN transport TEXT DEFAULT 'http'`,
	}
	for _, a := range alters {
		// SQLite ALTER TABLE ADD COLUMN fails if the column already exists.
		// Ignore the error since we only want to add missing columns.
		s.db.ExecContext(ctx, a)
	}

	return nil
}

// --- Sessions ---

func (s *Store) CreateSession(ctx context.Context, agentID, userID string) (*metadata.Session, error) {
	now := time.Now().UnixMilli()
	id := fmt.Sprintf("%d", now)
	state := "{}"

	_, err := s.db.ExecContext(ctx,
		"INSERT INTO sessions (id, agent_id, user_id, state, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, agentID, userID, state, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: create session: %w", err)
	}

	return &metadata.Session{
		ID:        id,
		AgentID:   agentID,
		UserID:    userID,
		Messages:  []metadata.Message{},
		State:     map[string]any{},
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (s *Store) GetSession(ctx context.Context, id string) (*metadata.Session, error) {
	var sess metadata.Session
	var stateJSON string

	err := s.db.QueryRowContext(ctx,
		"SELECT id, agent_id, user_id, parent_session_id, state, created_at, updated_at FROM sessions WHERE id = ?",
		id,
	).Scan(&sess.ID, &sess.AgentID, &sess.UserID, &sess.ParentSessionID, &stateJSON, &sess.CreatedAt, &sess.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite: get session %q: %w", id, err)
	}

	if err := json.Unmarshal([]byte(stateJSON), &sess.State); err != nil {
		sess.State = map[string]any{}
	}

	msgs, err := s.loadMessages(ctx, id)
	if err != nil {
		return nil, err
	}
	sess.Messages = msgs

	return &sess, nil
}

func (s *Store) UpdateSession(ctx context.Context, sess *metadata.Session) error {
	now := time.Now().UnixMilli()
	sess.UpdatedAt = now

	stateJSON, err := json.Marshal(sess.State)
	if err != nil {
		return fmt.Errorf("sqlite: marshal state: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		"UPDATE sessions SET agent_id = ?, user_id = ?, parent_session_id = ?, state = ?, updated_at = ? WHERE id = ?",
		sess.AgentID, sess.UserID, sess.ParentSessionID, string(stateJSON), now, sess.ID,
	)
	if err != nil {
		return fmt.Errorf("sqlite: update session: %w", err)
	}

	// Delete old messages and re-insert.
	_, err = tx.ExecContext(ctx, "DELETE FROM session_messages WHERE session_id = ?", sess.ID)
	if err != nil {
		return fmt.Errorf("sqlite: delete old messages: %w", err)
	}

	for _, msg := range sess.Messages {
		tcJSON, _ := json.Marshal(msg.ToolCalls)
		_, err = tx.ExecContext(ctx,
			"INSERT INTO session_messages (id, session_id, role, content, tool_calls, created_at) VALUES (?, ?, ?, ?, ?, ?)",
			msg.ID, sess.ID, msg.Role, msg.Content, string(tcJSON), msg.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("sqlite: insert message: %w", err)
		}
	}

	return tx.Commit()
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("sqlite: delete session %q: %w", id, err)
	}
	return nil
}

func (s *Store) ListSessions(ctx context.Context, agentID string) ([]*metadata.Session, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, agent_id, user_id, parent_session_id, created_at, updated_at FROM sessions WHERE agent_id = ? ORDER BY updated_at DESC",
		agentID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*metadata.Session
	for rows.Next() {
		var sess metadata.Session
		if err := rows.Scan(&sess.ID, &sess.AgentID, &sess.UserID, &sess.ParentSessionID, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
			return nil, fmt.Errorf("sqlite: scan session: %w", err)
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
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, agent_id, user_id, parent_session_id, created_at, updated_at FROM sessions WHERE agent_id = ? AND user_id = ? ORDER BY updated_at DESC",
		agentID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list sessions by user: %w", err)
	}
	defer rows.Close()

	var sessions []*metadata.Session
	for rows.Next() {
		var sess metadata.Session
		if err := rows.Scan(&sess.ID, &sess.AgentID, &sess.UserID, &sess.ParentSessionID, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
			return nil, fmt.Errorf("sqlite: scan session: %w", err)
		}
		sess.Messages = []metadata.Message{}
		sessions = append(sessions, &sess)
	}
	if sessions == nil {
		sessions = []*metadata.Session{}
	}
	return sessions, nil
}

func (s *Store) loadMessages(ctx context.Context, sessionID string) ([]metadata.Message, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, role, content, tool_calls, created_at FROM session_messages WHERE session_id = ? ORDER BY created_at ASC",
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: load messages: %w", err)
	}
	defer rows.Close()

	var msgs []metadata.Message
	for rows.Next() {
		var msg metadata.Message
		var tcJSON string
		if err := rows.Scan(&msg.ID, &msg.Role, &msg.Content, &tcJSON, &msg.CreatedAt); err != nil {
			return nil, fmt.Errorf("sqlite: scan message: %w", err)
		}
		if err := json.Unmarshal([]byte(tcJSON), &msg.ToolCalls); err != nil {
			msg.ToolCalls = nil
		}
		msgs = append(msgs, msg)
	}
	if msgs == nil {
		msgs = []metadata.Message{}
	}
	return msgs, nil
}

// --- Tools ---

func (s *Store) SaveTool(ctx context.Context, td *metadata.ToolDef) error {
	if td.ID == "" {
		td.ID = fmt.Sprintf("%d", time.Now().UnixMilli())
	}
	if td.CreatedAt == 0 {
		td.CreatedAt = time.Now().UnixMilli()
	}

	paramsJSON, err := json.Marshal(td.Parameters)
	if err != nil {
		return fmt.Errorf("sqlite: marshal params: %w", err)
	}
	headersJSON, err := json.Marshal(td.Headers)
	if err != nil {
		return fmt.Errorf("sqlite: marshal headers: %w", err)
	}
	mappingJSON, err := json.Marshal(td.InputMapping)
	if err != nil {
		return fmt.Errorf("sqlite: marshal input_mapping: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO tools (id, agent_id, name, description, parameters, handler, endpoint, headers, input_mapping, response_field, script_path, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
		   agent_id       = excluded.agent_id,
		   description    = excluded.description,
		   parameters     = excluded.parameters,
		   handler        = excluded.handler,
		   endpoint       = excluded.endpoint,
		   headers        = excluded.headers,
		   input_mapping  = excluded.input_mapping,
		   response_field = excluded.response_field,
		   script_path    = excluded.script_path`,
		td.ID, td.AgentID, td.Name, td.Description, string(paramsJSON),
		td.Handler, td.Endpoint, string(headersJSON), string(mappingJSON), td.ResponseField, td.ScriptPath, td.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("sqlite: save tool %q: %w", td.Name, err)
	}
	return nil
}

func (s *Store) GetTool(ctx context.Context, name string) (*metadata.ToolDef, error) {
	var td metadata.ToolDef
	var paramsJSON, headersJSON, mappingJSON string

	err := s.db.QueryRowContext(ctx,
		"SELECT id, agent_id, name, description, parameters, handler, endpoint, headers, input_mapping, response_field, script_path, created_at FROM tools WHERE name = ?",
		name,
	).Scan(&td.ID, &td.AgentID, &td.Name, &td.Description, &paramsJSON, &td.Handler, &td.Endpoint, &headersJSON, &mappingJSON, &td.ResponseField, &td.ScriptPath, &td.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite: get tool %q: %w", name, err)
	}

	_ = json.Unmarshal([]byte(paramsJSON), &td.Parameters)
	_ = json.Unmarshal([]byte(headersJSON), &td.Headers)
	_ = json.Unmarshal([]byte(mappingJSON), &td.InputMapping)
	if td.Headers == nil {
		td.Headers = map[string]string{}
	}
	if td.InputMapping == nil {
		td.InputMapping = map[string]string{}
	}
	return &td, nil
}

func (s *Store) ListTools(ctx context.Context, agentID string) ([]*metadata.ToolDef, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, agent_id, name, description, parameters, handler, endpoint, headers, input_mapping, response_field, script_path, created_at FROM tools WHERE agent_id = ?",
		agentID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list tools: %w", err)
	}
	defer rows.Close()

	var tools []*metadata.ToolDef
	for rows.Next() {
		var td metadata.ToolDef
		var paramsJSON, headersJSON, mappingJSON string
		if err := rows.Scan(&td.ID, &td.AgentID, &td.Name, &td.Description, &paramsJSON, &td.Handler, &td.Endpoint, &headersJSON, &mappingJSON, &td.ResponseField, &td.ScriptPath, &td.CreatedAt); err != nil {
			return nil, fmt.Errorf("sqlite: scan tool: %w", err)
		}
		_ = json.Unmarshal([]byte(paramsJSON), &td.Parameters)
		_ = json.Unmarshal([]byte(headersJSON), &td.Headers)
		_ = json.Unmarshal([]byte(mappingJSON), &td.InputMapping)
		if td.Headers == nil {
			td.Headers = map[string]string{}
		}
		if td.InputMapping == nil {
			td.InputMapping = map[string]string{}
		}
		tools = append(tools, &td)
	}
	return tools, nil
}

func (s *Store) DeleteTool(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM tools WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("sqlite: delete tool %q: %w", name, err)
	}
	return nil
}

// --- Skills ---

func (s *Store) SaveSkill(ctx context.Context, def *metadata.SkillDef) error {
	toolsJSON, err := json.Marshal(def.Tools)
	if err != nil {
		return fmt.Errorf("sqlite: marshal tools: %w", err)
	}
	configJSON, err := json.Marshal(def.Config)
	if err != nil {
		return fmt.Errorf("sqlite: marshal config: %w", err)
	}
	resJSON, err := json.Marshal(def.Resources)
	if err != nil {
		return fmt.Errorf("sqlite: marshal resources: %w", err)
	}

	enabled := 0
	if def.Enabled {
		enabled = 1
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO skills (name, description, version, instructions, tools, config, resources, enabled)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
		   description  = excluded.description,
		   version      = excluded.version,
		   instructions = excluded.instructions,
		   tools        = excluded.tools,
		   config       = excluded.config,
		   resources    = excluded.resources,
		   enabled      = excluded.enabled`,
		def.Name, def.Description, def.Version, def.Instructions,
		string(toolsJSON), string(configJSON), string(resJSON), enabled,
	)
	if err != nil {
		return fmt.Errorf("sqlite: save skill %q: %w", def.Name, err)
	}
	return nil
}

func (s *Store) GetSkill(ctx context.Context, name string) (*metadata.SkillDef, error) {
	var def metadata.SkillDef
	var toolsJSON, configJSON, resJSON string
	var enabled int

	err := s.db.QueryRowContext(ctx,
		"SELECT name, description, version, instructions, tools, config, resources, enabled FROM skills WHERE name = ?",
		name,
	).Scan(&def.Name, &def.Description, &def.Version, &def.Instructions, &toolsJSON, &configJSON, &resJSON, &enabled)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite: get skill %q: %w", name, err)
	}

	def.Enabled = enabled == 1
	_ = json.Unmarshal([]byte(toolsJSON), &def.Tools)
	_ = json.Unmarshal([]byte(configJSON), &def.Config)
	_ = json.Unmarshal([]byte(resJSON), &def.Resources)

	return &def, nil
}

func (s *Store) ListSkills(ctx context.Context) ([]*metadata.SkillEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT name, description, version, enabled FROM skills",
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list skills: %w", err)
	}
	defer rows.Close()

	var entries []*metadata.SkillEntry
	for rows.Next() {
		var e metadata.SkillEntry
		var enabled int
		if err := rows.Scan(&e.Name, &e.Description, &e.Version, &enabled); err != nil {
			return nil, fmt.Errorf("sqlite: scan skill: %w", err)
		}
		e.Enabled = enabled == 1
		entries = append(entries, &e)
	}
	return entries, nil
}

func (s *Store) DeleteSkill(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM skills WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("sqlite: delete skill %q: %w", name, err)
	}
	return nil
}

// --- Scoped Memory ---

func (s *Store) SetMemory(ctx context.Context, scope metadata.MemoryScope, scopeID, key string, value []byte) error {
	compositeKey := metadata.ScopedKey(scope, scopeID, key)
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO memory (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		compositeKey, value,
	)
	if err != nil {
		return fmt.Errorf("sqlite: set memory %q: %w", compositeKey, err)
	}
	return nil
}

func (s *Store) GetMemory(ctx context.Context, scope metadata.MemoryScope, scopeID, key string) ([]byte, error) {
	compositeKey := metadata.ScopedKey(scope, scopeID, key)
	var value []byte
	err := s.db.QueryRowContext(ctx, "SELECT value FROM memory WHERE key = ?", compositeKey).Scan(&value)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite: get memory %q: %w", compositeKey, err)
	}
	return value, nil
}

func (s *Store) DeleteMemory(ctx context.Context, scope metadata.MemoryScope, scopeID, key string) error {
	compositeKey := metadata.ScopedKey(scope, scopeID, key)
	_, err := s.db.ExecContext(ctx, "DELETE FROM memory WHERE key = ?", compositeKey)
	if err != nil {
		return fmt.Errorf("sqlite: delete memory %q: %w", compositeKey, err)
	}
	return nil
}

func (s *Store) ListMemory(ctx context.Context, scope metadata.MemoryScope, scopeID, prefix string) ([]string, error) {
	compositePrefix := metadata.ScopedKey(scope, scopeID, prefix)
	pattern := compositePrefix + "%"
	rows, err := s.db.QueryContext(ctx, "SELECT key FROM memory WHERE key LIKE ?", pattern)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list memory %q: %w", compositePrefix, err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("sqlite: scan key: %w", err)
		}
		keys = append(keys, key)
	}
	if keys == nil {
		keys = []string{}
	}
	return keys, nil
}

// --- Legacy flat KV (delegates to global-scoped memory) ---

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

// --- MCP Servers ---

func (s *Store) SaveMCPServer(ctx context.Context, srv *metadata.MCPServerDef) error {
	if srv.ID == "" {
		srv.ID = fmt.Sprintf("%d", time.Now().UnixMilli())
	}
	if srv.CreatedAt == 0 {
		srv.CreatedAt = time.Now().UnixMilli()
	}
	if srv.Transport == "" {
		srv.Transport = "http"
	}

	headersJSON, err := json.Marshal(srv.Headers)
	if err != nil {
		return fmt.Errorf("sqlite: marshal headers: %w", err)
	}

	enabled := 0
	if srv.Enabled {
		enabled = 1
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO mcp_servers (id, agent_id, name, url, transport, headers, enabled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
		   agent_id   = excluded.agent_id,
		   url        = excluded.url,
		   transport  = excluded.transport,
		   headers    = excluded.headers,
		   enabled    = excluded.enabled`,
		srv.ID, srv.AgentID, srv.Name, srv.URL, srv.Transport, string(headersJSON), enabled, srv.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("sqlite: save mcp server %q: %w", srv.Name, err)
	}
	return nil
}

func (s *Store) GetMCPServer(ctx context.Context, name string) (*metadata.MCPServerDef, error) {
	var srv metadata.MCPServerDef
	var headersJSON string
	var enabled int

	err := s.db.QueryRowContext(ctx,
		"SELECT id, agent_id, name, url, transport, headers, enabled, created_at FROM mcp_servers WHERE name = ?",
		name,
	).Scan(&srv.ID, &srv.AgentID, &srv.Name, &srv.URL, &srv.Transport, &headersJSON, &enabled, &srv.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite: get mcp server %q: %w", name, err)
	}

	srv.Enabled = enabled == 1
	_ = json.Unmarshal([]byte(headersJSON), &srv.Headers)
	if srv.Headers == nil {
		srv.Headers = map[string]string{}
	}
	if srv.Transport == "" {
		srv.Transport = "http"
	}
	return &srv, nil
}

func (s *Store) ListMCPServers(ctx context.Context, agentID string) ([]*metadata.MCPServerDef, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, agent_id, name, url, transport, headers, enabled, created_at FROM mcp_servers WHERE agent_id = ?",
		agentID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list mcp servers: %w", err)
	}
	defer rows.Close()

	var servers []*metadata.MCPServerDef
	for rows.Next() {
		var srv metadata.MCPServerDef
		var headersJSON string
		var enabled int
		if err := rows.Scan(&srv.ID, &srv.AgentID, &srv.Name, &srv.URL, &srv.Transport, &headersJSON, &enabled, &srv.CreatedAt); err != nil {
			return nil, fmt.Errorf("sqlite: scan mcp server: %w", err)
		}
		srv.Enabled = enabled == 1
		_ = json.Unmarshal([]byte(headersJSON), &srv.Headers)
		if srv.Headers == nil {
			srv.Headers = map[string]string{}
		}
		if srv.Transport == "" {
			srv.Transport = "http"
		}
		servers = append(servers, &srv)
	}
	return servers, nil
}

func (s *Store) DeleteMCPServer(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM mcp_servers WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("sqlite: delete mcp server %q: %w", name, err)
	}
	return nil
}
