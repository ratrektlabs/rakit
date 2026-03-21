package session

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionExpired  = errors.New("session expired")
)

type State map[string]interface{}

type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	State     State     `json:"state"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Metadata  Metadata  `json:"metadata,omitempty"`
}

type Metadata map[string]interface{}

type Config struct {
	DefaultTTL      time.Duration
	CleanupInterval time.Duration
	MaxSessions     int
}

type Option func(*Config)

func WithDefaultTTL(ttl time.Duration) Option {
	return func(c *Config) {
		c.DefaultTTL = ttl
	}
}

func WithCleanupInterval(interval time.Duration) Option {
	return func(c *Config) {
		c.CleanupInterval = interval
	}
}

func WithMaxSessions(max int) Option {
	return func(c *Config) {
		c.MaxSessions = max
	}
}

type Manager struct {
	mu           sync.RWMutex
	sessions     map[string]*Session
	userSessions map[string]map[string]struct{}
	config       Config
	stopCleanup  chan struct{}
}

func NewManager(opts ...Option) *Manager {
	config := Config{
		DefaultTTL:      30 * time.Minute,
		CleanupInterval: 5 * time.Minute,
		MaxSessions:     10000,
	}

	for _, opt := range opts {
		opt(&config)
	}

	m := &Manager{
		sessions:     make(map[string]*Session),
		userSessions: make(map[string]map[string]struct{}),
		config:       config,
		stopCleanup:  make(chan struct{}),
	}

	go m.cleanup()

	return m
}

func (m *Manager) Create(ctx context.Context, userID string) (*Session, error) {
	return m.CreateWithID(ctx, userID, generateID())
}

func (m *Manager) CreateWithID(ctx context.Context, userID, sessionID string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.config.MaxSessions > 0 && len(m.sessions) >= m.config.MaxSessions {
		m.evictOldest()
	}

	now := time.Now()
	session := &Session{
		ID:        sessionID,
		UserID:    userID,
		State:     make(State),
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: now.Add(m.config.DefaultTTL),
		Metadata:  make(Metadata),
	}

	m.sessions[sessionID] = session

	if m.userSessions[userID] == nil {
		m.userSessions[userID] = make(map[string]struct{})
	}
	m.userSessions[userID][sessionID] = struct{}{}

	return session, nil
}

func (m *Manager) Get(ctx context.Context, sessionID string) (*Session, error) {
	m.mu.RLock()
	session, exists := m.sessions[sessionID]
	m.mu.RUnlock()

	if !exists {
		return nil, ErrSessionNotFound
	}

	if time.Now().After(session.ExpiresAt) {
		m.Delete(ctx, sessionID)
		return nil, ErrSessionExpired
	}

	return session, nil
}

func (m *Manager) GetByUser(ctx context.Context, userID string) ([]*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	userSessionIDs, exists := m.userSessions[userID]
	if !exists {
		return []*Session{}, nil
	}

	sessions := make([]*Session, 0, len(userSessionIDs))
	now := time.Now()

	for sessionID := range userSessionIDs {
		session, ok := m.sessions[sessionID]
		if ok && now.Before(session.ExpiresAt) {
			sessions = append(sessions, session)
		}
	}

	return sessions, nil
}

func (m *Manager) Update(ctx context.Context, sessionID string, updates func(*Session)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, exists := m.sessions[sessionID]
	if !exists {
		return ErrSessionNotFound
	}

	if time.Now().After(session.ExpiresAt) {
		return ErrSessionExpired
	}

	updates(session)
	session.UpdatedAt = time.Now()

	return nil
}

func (m *Manager) Touch(ctx context.Context, sessionID string) error {
	return m.Update(ctx, sessionID, func(s *Session) {
		s.ExpiresAt = time.Now().Add(m.config.DefaultTTL)
	})
}

func (m *Manager) Delete(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, exists := m.sessions[sessionID]
	if !exists {
		return nil
	}

	delete(m.sessions, sessionID)

	if userSessions, ok := m.userSessions[session.UserID]; ok {
		delete(userSessions, sessionID)
		if len(userSessions) == 0 {
			delete(m.userSessions, session.UserID)
		}
	}

	return nil
}

func (m *Manager) DeleteByUser(ctx context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	userSessionIDs, exists := m.userSessions[userID]
	if !exists {
		return nil
	}

	for sessionID := range userSessionIDs {
		delete(m.sessions, sessionID)
	}

	delete(m.userSessions, userID)

	return nil
}

func (m *Manager) SetState(ctx context.Context, sessionID string, key string, value interface{}) error {
	return m.Update(ctx, sessionID, func(s *Session) {
		s.State[key] = value
	})
}

func (m *Manager) GetState(ctx context.Context, sessionID string, key string) (interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, exists := m.sessions[sessionID]
	if !exists {
		return nil, ErrSessionNotFound
	}

	if time.Now().After(session.ExpiresAt) {
		return nil, ErrSessionExpired
	}

	return session.State[key], nil
}

func (m *Manager) SetMetadata(ctx context.Context, sessionID string, key string, value interface{}) error {
	return m.Update(ctx, sessionID, func(s *Session) {
		if s.Metadata == nil {
			s.Metadata = make(Metadata)
		}
		s.Metadata[key] = value
	})
}

func (m *Manager) Extend(ctx context.Context, sessionID string, ttl time.Duration) error {
	return m.Update(ctx, sessionID, func(s *Session) {
		s.ExpiresAt = time.Now().Add(ttl)
	})
}

func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

func (m *Manager) CountByUser(userID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if userSessions, ok := m.userSessions[userID]; ok {
		return len(userSessions)
	}
	return 0
}

func (m *Manager) Close() error {
	close(m.stopCleanup)
	return nil
}

func (m *Manager) cleanup() {
	ticker := time.NewTicker(m.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCleanup:
			return
		case <-ticker.C:
			m.cleanupExpired()
		}
	}
}

func (m *Manager) cleanupExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for sessionID, session := range m.sessions {
		if now.After(session.ExpiresAt) {
			delete(m.sessions, sessionID)

			if userSessions, ok := m.userSessions[session.UserID]; ok {
				delete(userSessions, sessionID)
				if len(userSessions) == 0 {
					delete(m.userSessions, session.UserID)
				}
			}
		}
	}
}

func (m *Manager) evictOldest() {
	var oldestID string
	var oldestTime time.Time

	for id, session := range m.sessions {
		if oldestID == "" || session.UpdatedAt.Before(oldestTime) {
			oldestID = id
			oldestTime = session.UpdatedAt
		}
	}

	if oldestID != "" {
		session := m.sessions[oldestID]
		delete(m.sessions, oldestID)

		if userSessions, ok := m.userSessions[session.UserID]; ok {
			delete(userSessions, oldestID)
			if len(userSessions) == 0 {
				delete(m.userSessions, session.UserID)
			}
		}
	}
}

func generateID() string {
	return time.Now().Format("20060102150405") + randomSuffix(8)
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
