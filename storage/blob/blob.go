package blob

import "context"

// BlobStore is the interface for the agent virtual workspace.
type BlobStore interface {
	Read(ctx context.Context, path string) ([]byte, error)
	Write(ctx context.Context, path string, data []byte) error
	Delete(ctx context.Context, path string) error
	List(ctx context.Context, prefix string) ([]string, error)
}
