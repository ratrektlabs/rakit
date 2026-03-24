package memory

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ratrektlabs/rl-agent/provider"
)

var (
	ErrMissingConversationID = errors.New("conversation id is required")
	ErrConversationNotFound  = errors.New("conversation not found")
)

// Entry represents a single memory entry
type Entry struct {
	ID        string
	Role      provider.MessageRole
	Content   string
	Metadata  map[string]any
	Timestamp time.Time
}

// Memory interface for conversation persistence
type Memory interface {
	Add(ctx context.Context, userID, sessionID string, entry Entry) error
	Get(ctx context.Context, userID, sessionID string, limit int) ([]Entry, error)
	Search(ctx context.Context, userID string, query string, limit int) ([]Entry, error)
	Clear(ctx context.Context, userID, sessionID string) error
}

// MemoryBackend interface for backend implementations
type MemoryBackend interface {
	Connect(ctx context.Context) error
	Close() error
	Memory() Memory
}

// InMemoryBackend implements MemoryBackend with in-memory storage
type InMemoryBackend struct {
	mu      sync.RWMutex
	entries map[string][]Entry // key: userID:sessionID
}

// NewInMemoryBackend creates a new in-memory backend
func NewInMemoryBackend() *InMemoryBackend {
	return &InMemoryBackend{
		entries: make(map[string][]Entry),
	}
}

// Connect implements MemoryBackend (no-op for in-memory)
func (b *InMemoryBackend) Connect(ctx context.Context) error {
	return nil
}

// Close implements MemoryBackend (no-op for in-memory)
func (b *InMemoryBackend) Close() error {
	return nil
}

// Memory implements MemoryBackend
func (b *InMemoryBackend) Memory() Memory {
	return &inMemory{backend: b}
}

// inMemory implements Memory interface
type inMemory struct {
	backend *InMemoryBackend
}

func (m *inMemory) key(userID, sessionID string) string {
	return userID + ":" + sessionID
}

// Add adds a new entry to memory
func (m *inMemory) Add(ctx context.Context, userID, sessionID string, entry Entry) error {
	if userID == "" || sessionID == "" {
		return ErrMissingConversationID
	}

	m.backend.mu.Lock()
	defer m.backend.mu.Unlock()

	key := m.key(userID, sessionID)
	entry.Timestamp = time.Now()
	m.backend.entries[key] = append(m.backend.entries[key], entry)

	return nil
}

// Get retrieves entries from memory
func (m *inMemory) Get(ctx context.Context, userID, sessionID string, limit int) ([]Entry, error) {
	if userID == "" || sessionID == "" {
		return nil, ErrMissingConversationID
	}

	m.backend.mu.RLock()
	defer m.backend.mu.RUnlock()

	key := m.key(userID, sessionID)
	entries, exists := m.backend.entries[key]
	if !exists {
		return []Entry{}, nil
	}

	if limit > 0 && limit < len(entries) {
		return entries[:limit], nil
	}

	return entries, nil
}

// Search searches entries (simple contains match)
func (m *inMemory) Search(ctx context.Context, userID string, query string, limit int) ([]Entry, error) {
	m.backend.mu.RLock()
	defer m.backend.mu.RUnlock()

	var results []Entry
	for key, entries := range m.backend.entries {
		// Only search entries for the given user
		if len(key) > len(userID) && key[:len(userID)] == userID {
			for _, entry := range entries {
				if contains(entry.Content, query) {
					results = append(results, entry)
					if limit > 0 && len(results) >= limit {
						return results, nil
					}
				}
			}
		}
	}

	return results, nil
}

// Clear clears all entries for a session
func (m *inMemory) Clear(ctx context.Context, userID, sessionID string) error {
	if userID == "" || sessionID == "" {
		return ErrMissingConversationID
	}

	m.backend.mu.Lock()
	defer m.backend.mu.Unlock()

	key := m.key(userID, sessionID)
	delete(m.backend.entries, key)

	return nil
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr || len(s) > len(substr) && contains(s[1:], substr)
}
