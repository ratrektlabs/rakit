package jsonl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type processLock struct {
	ch chan struct{}
}

type processLockRegistry struct {
	mu    sync.Mutex
	locks map[string]*processLock
}

var storeLocks = processLockRegistry{locks: make(map[string]*processLock)}

func (r *processLockRegistry) acquire(ctx context.Context, key string) (func(), error) {
	r.mu.Lock()
	lock := r.locks[key]
	if lock == nil {
		lock = &processLock{ch: make(chan struct{}, 1)}
		lock.ch <- struct{}{}
		r.locks[key] = lock
	}
	r.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-lock.ch:
		return func() { lock.ch <- struct{}{} }, nil
	}
}

func (s *Store) withLock(ctx context.Context, fn func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	unlockProcess, err := storeLocks.acquire(ctx, s.projectDir)
	if err != nil {
		return err
	}
	defer unlockProcess()

	lockPath := filepath.Join(s.locksDir, "store.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("jsonl: open lock: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("jsonl: protect lock: %w", err)
	}
	defer f.Close()

	for {
		locked, lockErr := tryPlatformLock(f)
		if lockErr != nil {
			return fmt.Errorf("jsonl: acquire lock: %w", lockErr)
		}
		if locked {
			break
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
	defer func() { _ = unlockPlatformLock(f) }()
	return fn()
}
