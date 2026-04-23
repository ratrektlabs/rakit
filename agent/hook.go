package agent

import (
	"context"
)

// Hook provides observability callbacks for agent events.
type Hook interface {
	OnEvent(ctx context.Context, e Event) error
	OnError(ctx context.Context, err error) error
}

// HookFunc is a convenience type for single-function hooks.
type HookFunc func(ctx context.Context, e Event) error

// OnEvent invokes the function.
func (f HookFunc) OnEvent(ctx context.Context, e Event) error { return f(ctx, e) }

// OnError returns the error unchanged.
func (f HookFunc) OnError(_ context.Context, err error) error { return err }
