package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ratrektlabs/rl-agent/memory"
)

type Backend struct {
	mu   sync.RWMutex
	db   *sql.DB
	path string

	stmtAdd       *sql.Stmt
	stmtGet       *sql.Stmt
	stmtSearch    *sql.Stmt
	stmtClear     *sql.Stmt
	stmtGetAll    *sql.Stmt
	stmtCount     *sql.Stmt
	stmtDeleteOld *sql.Stmt
	stmtArchive   *sql.Stmt
}

func New(path string) *Backend {
	return &Backend{
		path: path,
	}
}

func NewInMemory() *Backend {
	return &Backend{
		path: ":memory:",
	}
}

func (b *Backend) Connect(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	var err error
	b.db, err = sql.Open("sqlite", b.path)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	b.db.SetMaxOpenConns(10)
	b.db.SetMaxIdleConns(5)
	b.db.SetConnMaxLifetime(time.Hour)

	if err := b.migrate(ctx); err != nil {
		return fmt.Errorf("failed to migrate: %w", err)
	}

	if err := b.prepareStatements(ctx); err != nil {
		return fmt.Errorf("failed to prepare statements: %w", err)
	}

	return nil
}

func (b *Backend) migrate(ctx context.Context) error {
	schema := `
	CREATE TABLE IF NOT EXISTS memory_entries (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		session_id TEXT NOT NULL,
		type TEXT,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		metadata TEXT,
		created_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS memory_entries_archive (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		session_id TEXT NOT NULL,
		type TEXT,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		metadata TEXT,
		created_at DATETIME NOT NULL,
		archived_at DATETIME NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_memory_entries_user_id ON memory_entries(user_id);
	CREATE INDEX IF NOT EXISTS idx_memory_entries_session_id ON memory_entries(session_id);
	CREATE INDEX IF NOT EXISTS idx_memory_entries_created_at ON memory_entries(created_at);
	CREATE INDEX IF NOT EXISTS idx_memory_entries_user_session ON memory_entries(user_id, session_id);
	CREATE INDEX IF NOT EXISTS idx_archive_user_session ON memory_entries_archive(user_id, session_id);
	`

	_, err := b.db.ExecContext(ctx, schema)
	return err
}

func (b *Backend) prepareStatements(ctx context.Context) error {
	var err error

	b.stmtAdd, err = b.db.PrepareContext(ctx, `
		INSERT INTO memory_entries (id, user_id, session_id, type, role, content, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare add statement: %w", err)
	}

	b.stmtGet, err = b.db.PrepareContext(ctx, `
		SELECT id, type, role, content, metadata, created_at
		FROM memory_entries
		WHERE user_id = ? AND session_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare get statement: %w", err)
	}

	b.stmtSearch, err = b.db.PrepareContext(ctx, `
		SELECT id, type, role, content, metadata, created_at
		FROM memory_entries
		WHERE user_id = ? AND content LIKE ?
		ORDER BY created_at DESC
		LIMIT ?
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare search statement: %w", err)
	}

	b.stmtClear, err = b.db.PrepareContext(ctx, `
		DELETE FROM memory_entries
		WHERE user_id = ? AND session_id = ?
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare clear statement: %w", err)
	}

	b.stmtGetAll, err = b.db.PrepareContext(ctx, `
		SELECT id, type, role, content, metadata, created_at
		FROM memory_entries
		WHERE user_id = ? AND session_id = ?
		ORDER BY created_at ASC
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare get all statement: %w", err)
	}

	b.stmtCount, err = b.db.PrepareContext(ctx, `
		SELECT COUNT(*) FROM memory_entries
		WHERE user_id = ? AND session_id = ?
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare count statement: %w", err)
	}

	b.stmtDeleteOld, err = b.db.PrepareContext(ctx, `
		DELETE FROM memory_entries
		WHERE user_id = ? AND session_id = ? AND id IN (
			SELECT id FROM memory_entries
			WHERE user_id = ? AND session_id = ?
			ORDER BY created_at ASC
			LIMIT ?
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare delete old statement: %w", err)
	}

	b.stmtArchive, err = b.db.PrepareContext(ctx, `
		INSERT INTO memory_entries_archive (id, user_id, session_id, type, role, content, metadata, created_at, archived_at)
		SELECT id, user_id, session_id, type, role, content, metadata, created_at, ?
		FROM memory_entries
		WHERE user_id = ? AND session_id = ? AND id IN (
			SELECT id FROM memory_entries
			WHERE user_id = ? AND session_id = ?
			ORDER BY created_at ASC
			LIMIT ?
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare archive statement: %w", err)
	}

	return nil
}

func (b *Backend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.stmtAdd != nil {
		b.stmtAdd.Close()
	}
	if b.stmtGet != nil {
		b.stmtGet.Close()
	}
	if b.stmtSearch != nil {
		b.stmtSearch.Close()
	}
	if b.stmtClear != nil {
		b.stmtClear.Close()
	}
	if b.stmtGetAll != nil {
		b.stmtGetAll.Close()
	}
	if b.stmtCount != nil {
		b.stmtCount.Close()
	}
	if b.stmtDeleteOld != nil {
		b.stmtDeleteOld.Close()
	}
	if b.stmtArchive != nil {
		b.stmtArchive.Close()
	}

	if b.db != nil {
		return b.db.Close()
	}
	return nil
}

func (b *Backend) Memory() memory.Memory {
	return &sqliteMemory{backend: b}
}

type sqliteMemory struct {
	backend *Backend
}

func (m *sqliteMemory) Add(ctx context.Context, userID, sessionID string, entry memory.Entry) error {
	m.backend.mu.Lock()
	defer m.backend.mu.Unlock()

	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	if entry.ID == "" {
		entry.ID = generateID()
	}

	var metadataJSON []byte
	var err error
	if entry.Metadata != nil {
		metadataJSON, err = json.Marshal(entry.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
	}

	_, err = m.backend.stmtAdd.ExecContext(
		ctx,
		entry.ID,
		userID,
		sessionID,
		entry.Type,
		entry.Role,
		entry.Content,
		string(metadataJSON),
		entry.Timestamp.UTC(),
	)
	if err != nil {
		return fmt.Errorf("failed to add entry: %w", err)
	}

	return nil
}

func (m *sqliteMemory) Get(ctx context.Context, userID, sessionID string, limit int) ([]memory.Entry, error) {
	m.backend.mu.RLock()
	defer m.backend.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}

	rows, err := m.backend.stmtGet.QueryContext(ctx, userID, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get entries: %w", err)
	}
	defer rows.Close()

	var entries []memory.Entry
	for rows.Next() {
		entry, err := m.scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	reverseEntries(entries)

	return entries, nil
}

func (m *sqliteMemory) Search(ctx context.Context, userID string, opts memory.SearchOptions) ([]memory.SearchResult, error) {
	m.backend.mu.RLock()
	defer m.backend.mu.RUnlock()

	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	searchPattern := "%" + opts.Query + "%"

	rows, err := m.backend.stmtSearch.QueryContext(ctx, userID, searchPattern, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to search entries: %w", err)
	}
	defer rows.Close()

	var results []memory.SearchResult
	for rows.Next() {
		entry, err := m.scanEntry(rows)
		if err != nil {
			return nil, err
		}

		if opts.Type != "" && entry.Type != opts.Type {
			continue
		}

		if !opts.FromDate.IsZero() && entry.Timestamp.Before(opts.FromDate) {
			continue
		}

		if !opts.ToDate.IsZero() && entry.Timestamp.After(opts.ToDate) {
			continue
		}

		score := calculateScore(entry.Content, opts.Query)

		results = append(results, memory.SearchResult{
			Entry: entry,
			Score: score,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return results, nil
}

func (m *sqliteMemory) Clear(ctx context.Context, userID, sessionID string) error {
	m.backend.mu.Lock()
	defer m.backend.mu.Unlock()

	_, err := m.backend.stmtClear.ExecContext(ctx, userID, sessionID)
	if err != nil {
		return fmt.Errorf("failed to clear entries: %w", err)
	}

	return nil
}

func (m *sqliteMemory) GetAll(ctx context.Context, userID, sessionID string) ([]memory.Entry, error) {
	m.backend.mu.RLock()
	defer m.backend.mu.RUnlock()

	rows, err := m.backend.stmtGetAll.QueryContext(ctx, userID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get all entries: %w", err)
	}
	defer rows.Close()

	var entries []memory.Entry
	for rows.Next() {
		entry, err := m.scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return entries, nil
}

func (m *sqliteMemory) Count(ctx context.Context, userID, sessionID string) (int, error) {
	m.backend.mu.RLock()
	defer m.backend.mu.RUnlock()

	var count int
	err := m.backend.stmtCount.QueryRowContext(ctx, userID, sessionID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count entries: %w", err)
	}

	return count, nil
}

func (m *sqliteMemory) Compact(ctx context.Context, userID, sessionID string, opts memory.CompactionOptions) (*memory.CompactionStats, error) {
	start := time.Now()
	stats := &memory.CompactionStats{Strategy: opts.Strategy}

	entries, err := m.GetAll(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}

	stats.EntriesBefore = len(entries)
	stats.BytesBefore = calculateTotalBytes(entries)

	if len(entries) == 0 {
		stats.EntriesAfter = 0
		stats.Duration = time.Since(start)
		return stats, nil
	}

	keepRecent := opts.KeepRecent
	if keepRecent <= 0 {
		keepRecent = opts.MaxEntries
	}
	if keepRecent <= 0 {
		keepRecent = len(entries)
	}

	var toCompactCount int
	if keepRecent < len(entries) {
		toCompactCount = len(entries) - keepRecent
	}

	if opts.MaxAge > 0 {
		cutoff := time.Now().Add(-opts.MaxAge)
		for _, e := range entries {
			if e.Timestamp.Before(cutoff) {
				toCompactCount++
			}
		}
	}

	if toCompactCount == 0 {
		stats.EntriesAfter = len(entries)
		stats.BytesAfter = stats.BytesBefore
		stats.Duration = time.Since(start)
		return stats, nil
	}

	if opts.DryRun {
		stats.EntriesAfter = len(entries) - toCompactCount
		stats.EntriesRemoved = toCompactCount
		var keptEntries []memory.Entry
		if keepRecent < len(entries) {
			keptEntries = entries[len(entries)-keepRecent:]
		} else {
			keptEntries = entries
		}
		stats.BytesAfter = calculateTotalBytes(keptEntries)
		stats.BytesSaved = stats.BytesBefore - stats.BytesAfter
		stats.Duration = time.Since(start)
		return stats, nil
	}

	m.backend.mu.Lock()
	defer m.backend.mu.Unlock()

	switch opts.Strategy {
	case memory.CompactionStrategyTruncate:
		_, err = m.backend.stmtDeleteOld.ExecContext(ctx, userID, sessionID, userID, sessionID, toCompactCount)
		if err != nil {
			return nil, fmt.Errorf("failed to truncate entries: %w", err)
		}
		stats.EntriesRemoved = toCompactCount

	case memory.CompactionStrategySummarize:
		if toCompactCount > 0 && opts.LLMProvider != nil {
			toCompact := entries[:toCompactCount]
			prompt := opts.SummarizePrompt
			if prompt == "" {
				prompt = "Summarize the following conversation history concisely, preserving key information and decisions:"
			}
			summary, err := opts.LLMProvider.Summarize(ctx, toCompact, prompt)
			if err != nil {
				return nil, fmt.Errorf("failed to summarize entries: %w", err)
			}

			_, err = m.backend.stmtDeleteOld.ExecContext(ctx, userID, sessionID, userID, sessionID, toCompactCount)
			if err != nil {
				return nil, fmt.Errorf("failed to delete entries after summary: %w", err)
			}

			summaryEntry := memory.Entry{
				ID:        generateID(),
				Type:      memory.EntryTypeSummary,
				Role:      "system",
				Content:   summary,
				Timestamp: time.Now(),
				Metadata: map[string]interface{}{
					"compacted_entries": len(toCompact),
					"compaction_time":   time.Now().Format(time.RFC3339),
				},
			}

			var metadataJSON []byte
			if summaryEntry.Metadata != nil {
				metadataJSON, _ = json.Marshal(summaryEntry.Metadata)
			}

			_, err = m.backend.stmtAdd.ExecContext(
				ctx,
				summaryEntry.ID,
				userID,
				sessionID,
				summaryEntry.Type,
				summaryEntry.Role,
				summaryEntry.Content,
				string(metadataJSON),
				summaryEntry.Timestamp.UTC(),
			)
			if err != nil {
				return nil, fmt.Errorf("failed to add summary entry: %w", err)
			}
			stats.EntriesSummary = toCompactCount
		} else {
			_, err = m.backend.stmtDeleteOld.ExecContext(ctx, userID, sessionID, userID, sessionID, toCompactCount)
			if err != nil {
				return nil, fmt.Errorf("failed to truncate entries: %w", err)
			}
			stats.EntriesRemoved = toCompactCount
		}

	case memory.CompactionStrategyArchive:
		_, err = m.backend.stmtArchive.ExecContext(ctx, time.Now().UTC(), userID, sessionID, userID, sessionID, toCompactCount)
		if err != nil {
			return nil, fmt.Errorf("failed to archive entries: %w", err)
		}
		_, err = m.backend.stmtDeleteOld.ExecContext(ctx, userID, sessionID, userID, sessionID, toCompactCount)
		if err != nil {
			return nil, fmt.Errorf("failed to delete archived entries: %w", err)
		}
		stats.EntriesArchived = toCompactCount

	default:
		_, err = m.backend.stmtDeleteOld.ExecContext(ctx, userID, sessionID, userID, sessionID, toCompactCount)
		if err != nil {
			return nil, fmt.Errorf("failed to truncate entries: %w", err)
		}
		stats.EntriesRemoved = toCompactCount
	}

	var newCount int
	err = m.backend.stmtCount.QueryRowContext(ctx, userID, sessionID).Scan(&newCount)
	if err != nil {
		newCount = len(entries) - toCompactCount
	}
	stats.EntriesAfter = newCount

	keptEntries, _ := m.GetAll(ctx, userID, sessionID)
	stats.BytesAfter = calculateTotalBytes(keptEntries)
	stats.BytesSaved = stats.BytesBefore - stats.BytesAfter
	stats.Duration = time.Since(start)

	return stats, nil
}

func calculateTotalBytes(entries []memory.Entry) int64 {
	var total int64
	for _, e := range entries {
		total += int64(len(e.Content))
	}
	return total
}

type scanner interface {
	Scan(dest ...interface{}) error
}

func (m *sqliteMemory) scanEntry(row scanner) (memory.Entry, error) {
	var entry memory.Entry
	var metadataStr sql.NullString
	var createdAt time.Time
	var entryType sql.NullString

	err := row.Scan(
		&entry.ID,
		&entryType,
		&entry.Role,
		&entry.Content,
		&metadataStr,
		&createdAt,
	)
	if err != nil {
		return memory.Entry{}, fmt.Errorf("failed to scan entry: %w", err)
	}

	entry.Type = memory.EntryType(entryType.String)
	entry.Timestamp = createdAt

	if metadataStr.Valid && metadataStr.String != "" {
		entry.Metadata = make(map[string]interface{})
		if err := json.Unmarshal([]byte(metadataStr.String), &entry.Metadata); err != nil {
			return memory.Entry{}, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	return entry, nil
}

func calculateScore(content, query string) float64 {
	if content == "" || query == "" {
		return 0
	}

	contentLower := toLower(content)
	queryLower := toLower(query)

	if contentLower == queryLower {
		return 1.0
	}

	if contains(contentLower, queryLower) {
		return 0.8
	}

	return 0.5
}

func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result[i] = c
	}
	return string(result)
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func reverseEntries(entries []memory.Entry) {
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
}

func generateID() string {
	return time.Now().Format("20060102150405") + randomSuffix(6)
}

func randomSuffix(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	now := time.Now().UnixNano()
	for i := range b {
		b[i] = letters[(now+int64(i))%int64(len(letters))]
	}
	return string(b)
}
