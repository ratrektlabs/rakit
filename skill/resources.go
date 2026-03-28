package skill

import (
	"context"

	"github.com/ratrektlabs/rakit/storage/blob"
)

// ResourceManager loads L3 resources from blob store on demand.
type ResourceManager struct {
	fs blob.BlobStore
}

func NewResourceManager(fs blob.BlobStore) *ResourceManager {
	return &ResourceManager{fs: fs}
}

// Load reads a resource file from the agent workspace.
func (r *ResourceManager) Load(ctx context.Context, path string) ([]byte, error) {
	return r.fs.Read(ctx, path)
}

// Store saves a resource to the agent workspace.
func (r *ResourceManager) Store(ctx context.Context, path string, data []byte) error {
	return r.fs.Write(ctx, path, data)
}
