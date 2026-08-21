package agent

import (
	"context"
	"fmt"

	"github.com/ratrektlabs/rakit/storage/metadata"
)

// persistSession is the single session durability path used by the agent.
// Storage adapters decide whether a snapshot is rewritten, upserted, or
// translated into an append-only journal internally.
func (a *Agent) persistSession(ctx context.Context, sess *metadata.Session) error {
	if a.Store == nil {
		return fmt.Errorf("agent: no store configured")
	}
	return a.Store.UpdateSession(ctx, sess)
}
