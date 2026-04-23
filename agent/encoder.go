package agent

import (
	"context"
	"io"
)

// Encoder translates a run's [Event] stream into an external wire format.
// Implementations live in the protocol/* packages. The agent package defines
// this interface so the core loop has no dependency on any specific protocol.
type Encoder interface {
	// Name returns a stable identifier for this encoder (e.g. "ag-ui").
	Name() string
	// ContentType returns the HTTP Content-Type the encoder writes.
	ContentType() string
	// Encode writes a single event.
	Encode(w io.Writer, event Event) error
	// EncodeStream drains events until the channel is closed or the context
	// is done, writing each one in order.
	EncodeStream(ctx context.Context, w io.Writer, events <-chan Event) error
	// Decode parses a single event from the reader.
	Decode(r io.Reader) (Event, error)
	// DecodeStream yields events parsed from the reader until it is closed
	// or the context is done.
	DecodeStream(ctx context.Context, r io.Reader) (<-chan Event, error)
}
