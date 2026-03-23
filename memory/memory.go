package memory

import (
	"context"
	"time"
)

type EntryType string

const (
	EntryTypeMessage     EntryType = "message"
	EntryTypeAction      EntryType = "action"
	EntryTypeObservation EntryType = "observation"
	EntryTypeSystem      EntryType = "system"
)

type Entry struct {
	ID        string                 `json:"id"`
	Type      EntryType              `json:"type,omitempty"`
	Role      string                 `json:"role"`
	Content   string                 `json:"content"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

type SearchOptions struct {
	Query    string    `json:"query"`
	Limit    int       `json:"limit"`
	Offset   int       `json:"offset"`
	Type     EntryType `json:"type,omitempty"`
	FromDate time.Time `json:"from_date,omitempty"`
	ToDate   time.Time `json:"to_date,omitempty"`
}

type SearchResult struct {
	Entry
	Score float64 `json:"score"`
}

type Memory interface {
	Add(ctx context.Context, userID, sessionID string, entry Entry) error
	Get(ctx context.Context, userID, sessionID string, limit int) ([]Entry, error)
	Search(ctx context.Context, userID string, opts SearchOptions) ([]SearchResult, error)
	Clear(ctx context.Context, userID, sessionID string) error
}

type MemoryBackend interface {
	Connect(ctx context.Context) error
	Close() error
	Memory() Memory
}

type InMemoryBackend struct {
	mu     interface{}
	data   map[string]map[string][]Entry
	memory Memory
}

func NewInMemoryBackend() *InMemoryBackend {
	backend := &InMemoryBackend{
		data: make(map[string]map[string][]Entry),
	}
	backend.memory = &inMemoryMemory{backend: backend}
	return backend
}

func (b *InMemoryBackend) Connect(ctx context.Context) error {
	return nil
}

func (b *InMemoryBackend) Close() error {
	return nil
}

func (b *InMemoryBackend) Memory() Memory {
	return b.memory
}

type inMemoryMemory struct {
	backend *InMemoryBackend
}

func (m *inMemoryMemory) Add(ctx context.Context, userID, sessionID string, entry Entry) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	if entry.ID == "" {
		entry.ID = generateID()
	}

	if m.backend.data[userID] == nil {
		m.backend.data[userID] = make(map[string][]Entry)
	}

	m.backend.data[userID][sessionID] = append(m.backend.data[userID][sessionID], entry)
	return nil
}

func (m *inMemoryMemory) Get(ctx context.Context, userID, sessionID string, limit int) ([]Entry, error) {
	sessions, ok := m.backend.data[userID]
	if !ok {
		return []Entry{}, nil
	}

	entries, ok := sessions[sessionID]
	if !ok {
		return []Entry{}, nil
	}

	if limit <= 0 || limit > len(entries) {
		limit = len(entries)
	}

	if limit < len(entries) {
		start := len(entries) - limit
		entries = entries[start:]
	}

	result := make([]Entry, len(entries))
	copy(result, entries)
	return result, nil
}

func (m *inMemoryMemory) Search(ctx context.Context, userID string, opts SearchOptions) ([]SearchResult, error) {
	sessions, ok := m.backend.data[userID]
	if !ok {
		return []SearchResult{}, nil
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	var results []SearchResult

	for _, entries := range sessions {
		for _, entry := range entries {
			if opts.Type != "" && entry.Type != opts.Type {
				continue
			}

			if !opts.FromDate.IsZero() && entry.Timestamp.Before(opts.FromDate) {
				continue
			}

			if !opts.ToDate.IsZero() && entry.Timestamp.After(opts.ToDate) {
				continue
			}

			if opts.Query != "" {
				score := simpleMatch(entry.Content, opts.Query)
				if score > 0 {
					results = append(results, SearchResult{
						Entry: entry,
						Score: score,
					})
				}
			} else {
				results = append(results, SearchResult{
					Entry: entry,
					Score: 1.0,
				})
			}
		}
	}

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

func (m *inMemoryMemory) Clear(ctx context.Context, userID, sessionID string) error {
	if m.backend.data[userID] != nil {
		delete(m.backend.data[userID], sessionID)
		if len(m.backend.data[userID]) == 0 {
			delete(m.backend.data, userID)
		}
	}
	return nil
}

func simpleMatch(content, query string) float64 {
	if content == "" || query == "" {
		return 0
	}

	content = toLower(content)
	query = toLower(query)

	if contains(content, query) {
		return 1.0
	}

	return 0
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
