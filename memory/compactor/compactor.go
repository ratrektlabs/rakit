package compactor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ratrektlabs/rl-agent/memory"
)

type CompactorConfig struct {
	DefaultInterval    time.Duration
	DefaultMaxEntries  int
	DefaultStrategy    memory.CompactionStrategy
	Enabled            bool
}

type SessionConfig struct {
	UserID      string
	SessionID   string
	Interval    time.Duration
	MaxEntries   int
	Strategy    memory.CompactionStrategy
	MaxAge       time.Duration
	SummarizePrompt string
	Enabled      bool
}

type Stats struct {
	TotalCompactions   int64
	TotalEntriesRemoved int64
	TotalBytesSaved    int64
	LastCompaction     time.Time
	Errors              int64
}

type Compactor struct {
	mu          sync.RWMutex
	mem         memory.Memory
	config      CompactorConfig
	sessions   map[string]*sessionConfig
	stats      Stats
	stopChan   chan struct{}
	doneChan   chan struct{}
	running    bool
	wg         sync.WaitGroup
}

type sessionConfig struct {
	config    SessionConfig
	cancel   context.CancelFunc
	lastRun time.Time
}

func New(mem memory.Memory, config CompactorConfig) *Compactor {
	if config.DefaultInterval == 0 {
		config.DefaultInterval = 5 * time.Minute
	}
	if config.DefaultMaxEntries == 0 {
		config.DefaultMaxEntries = 100
	}
	if config.DefaultStrategy == "" {
		config.DefaultStrategy = memory.CompactionStrategyTruncate
	}
	return &Compactor{
		mem:       mem,
		config:    config,
		sessions: make(map[string]*sessionConfig),
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}
}

func DefaultConfig() CompactorConfig {
	return CompactorConfig{
		DefaultInterval:   5 * time.Minute,
		DefaultMaxEntries:  100,
		DefaultStrategy:    memory.CompactionStrategyTruncate,
		Enabled:            true,
	}
}

func (c *Compactor) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return fmt.Errorf("compactor already running")
	}

	c.running = true
	c.wg.Add(1)

	go c.run(ctx)

	return nil
}

func (c *Compactor) Stop() error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = false
	c.mu.Unlock()

	close(c.stopChan)
	c.wg.Wait()

	return nil
}

func (c *Compactor) run(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopChan:
			return
		case <-ticker.C:
			c.checkAndCompact(ctx)
		}
	}
}

func (c *Compactor) checkAndCompact(ctx context.Context) {
	c.mu.RLock()
	sessions := make(map[string]*sessionConfig, len(c.sessions))
	for k, v := range c.sessions {
		sessions[k] = v
	}
	c.mu.RUnlock()

	now := time.Now()

	for key, sess := range sessions {
		if !sess.config.Enabled {
			continue
		}

		interval := sess.config.Interval
		if interval == 0 {
			interval = c.config.DefaultInterval
		}

		if sess.lastRun.IsZero() || now.Sub(sess.lastRun) >= interval {
			go c.compactSession(ctx, sess)
		}
	}
}

func (c *Compactor) compactSession(ctx context.Context, sess *sessionConfig) {
	sessionKey := fmt.Sprintf("%s:%s", sess.config.UserID, sess.config.SessionID)

	opts := memory.CompactionOptions{
		MaxEntries:      sess.config.MaxEntries,
		MaxAge:          sess.config.MaxAge,
		Strategy:        sess.config.Strategy,
		SummarizePrompt: sess.config.SummarizePrompt,
	}

	if opts.MaxEntries == 0 {
		opts.MaxEntries = c.config.DefaultMaxEntries
	}
	if opts.Strategy == "" {
		opts.Strategy = c.config.DefaultStrategy
	}

	stats, err := c.mem.Compact(ctx, sess.config.UserID, sess.config.SessionID, opts)
	if err != nil {
		c.mu.Lock()
		c.stats.Errors++
		c.mu.Unlock()
		return
	}

	c.mu.Lock()
	sess.lastRun = time.Now()
	c.stats.TotalCompactions++
	c.stats.TotalEntriesRemoved += int64(stats.EntriesRemoved + stats.EntriesArchived)
	c.stats.TotalBytesSaved += stats.BytesSaved
	c.stats.LastCompaction = time.Now()
	c.mu.Unlock()
}

func (c *Compactor) RegisterSession(config SessionConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	sessionKey := fmt.Sprintf("%s:%s", config.UserID, config.SessionID)

	if _, exists := c.sessions[sessionKey]; exists {
		return fmt.Errorf("session %s already registered", sessionKey)
	}

	if config.Interval == 0 {
		config.Interval = c.config.DefaultInterval
	}
	if config.MaxEntries == 0 {
		config.MaxEntries = c.config.DefaultMaxEntries
	}
	if config.Strategy == "" {
		config.Strategy = c.config.DefaultStrategy
	}

	c.sessions[sessionKey] = &sessionConfig{
		config:  config,
		lastRun: time.Time{},
	}

	return nil
}

func (c *Compactor) UnregisterSession(userID, sessionID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	sessionKey := fmt.Sprintf("%s:%s", userID, sessionID)

	if _, exists := c.sessions[sessionKey]; !exists {
		return fmt.Errorf("session %s not registered", sessionKey)
	}

	delete(c.sessions[sessionKey)

	return nil
}

func (c *Compactor) UpdateSession(userID, sessionID string, config SessionConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	sessionKey := fmt.Sprintf("%s:%s", userID, sessionID)

	sess, exists := c.sessions[sessionKey]
	if !exists {
		return fmt.Errorf("session %s not registered", sessionKey)
	}

	if config.Interval > 0 {
		sess.config.Interval = config.Interval
	}
	if config.MaxEntries > 0 {
		sess.config.MaxEntries = config.MaxEntries
	}
	if config.Strategy != "" {
		sess.config.Strategy = config.Strategy
	}
	sess.config.Enabled = config.Enabled

	return nil
}

func (c *Compactor) GetStats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stats
}

func (c *Compactor) GetSessionStats(userID, sessionID string) (Stats, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	sessionKey := fmt.Sprintf("%s:%s", userID, sessionID)

	if _, exists := c.sessions[sessionKey]; !exists {
		return Stats{}, fmt.Errorf("session %s not registered", sessionKey)
	}

	return c.stats, nil
}

func (c *Compactor) CompactNow(ctx context.Context, userID, sessionID string) (*memory.CompactionStats, error) {
	opts := memory.CompactionOptions{
		Strategy: c.config.DefaultStrategy,
	}

	sess, exists := c.getSession(userID, sessionID)
	if exists {
		if sess.config.MaxEntries > 0 {
			opts.MaxEntries = sess.config.MaxEntries
		}
		if sess.config.Strategy != "" {
			opts.Strategy = sess.config.Strategy
		}
		opts.MaxAge = sess.config.MaxAge
		opts.SummarizePrompt = sess.config.SummarizePrompt
	} else {
		opts.MaxEntries = c.config.DefaultMaxEntries
	}

	stats, err := c.mem.Compact(ctx, userID, sessionID, opts)
	if err != nil {
		c.mu.Lock()
		c.stats.Errors++
		c.mu.Unlock()
		return nil, err
	}

	c.mu.Lock()
	c.stats.TotalCompactions++
	c.stats.TotalEntriesRemoved += int64(stats.EntriesRemoved + stats.EntriesArchived)
	c.stats.TotalBytesSaved += stats.BytesSaved
	c.stats.LastCompaction = time.Now()
	c.mu.Unlock()

	return stats, nil
}

func (c *Compactor) getSession(userID, sessionID string) (*sessionConfig, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	sessionKey := fmt.Sprintf("%s:%s", userID, sessionID)

	sess, exists := c.sessions[sessionKey]
	return sess, exists
}

func (c *Compactor) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}

func (c *Compactor) ListSessions() []SessionConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var configs []SessionConfig
	for _, sess := range c.sessions {
		configs = append(configs, sess.config)
	}
	return configs
}
