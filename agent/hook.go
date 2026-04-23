package agent

import (
	"context"

	"github.com/ratrektlabs/rakit/protocol"
)

// Hook provides observability callbacks for agent events.
type Hook interface {
	OnEvent(ctx context.Context, e protocol.Event) error
	OnError(ctx context.Context, err error) error
}

// HookFunc is a convenience type for single-function hooks.
type HookFunc func(ctx context.Context, e protocol.Event) error

func (f HookFunc) OnEvent(ctx context.Context, e protocol.Event) error { return f(ctx, e) }
func (f HookFunc) OnError(_ context.Context, err error) error          { return err }
