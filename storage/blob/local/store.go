package local

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ratrektlabs/rl-agent/storage/blob"
)

var _ blob.BlobStore = (*Store)(nil)

// Store implements blob.BlobStore backed by the local filesystem.
type Store struct {
	baseDir string
}

// New creates a local blob store rooted at baseDir.
// The directory is created if it doesn't exist.
func New(baseDir string) (*Store, error) {
	abs, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, fmt.Errorf("local: resolve path %q: %w", baseDir, err)
	}
	if err := os.MkdirAll(abs, 0755); err != nil {
		return nil, fmt.Errorf("local: mkdir %q: %w", abs, err)
	}
	return &Store{baseDir: abs}, nil
}

func (s *Store) Read(ctx context.Context, path string) ([]byte, error) {
	p, err := s.safePath(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("local: read %q: %w", path, err)
	}
	return data, nil
}

func (s *Store) Write(ctx context.Context, path string, data []byte) error {
	p, err := s.safePath(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("local: mkdir %q: %w", dir, err)
	}
	if err := os.WriteFile(p, data, 0644); err != nil {
		return fmt.Errorf("local: write %q: %w", path, err)
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, path string) error {
	p, err := s.safePath(path)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("local: delete %q: %w", path, err)
	}
	return nil
}

func (s *Store) List(ctx context.Context, prefix string) ([]string, error) {
	searchDir := s.baseDir
	// Walk the prefix directory structure to narrow the scan.
	if prefix != "" {
		searchDir = filepath.Join(s.baseDir, prefix)
		// If the prefix is a partial directory name, walk the parent.
		for {
			fi, err := os.Stat(searchDir)
			if err != nil {
				// Try parent.
				parent := filepath.Dir(searchDir)
				if parent == searchDir || parent == s.baseDir {
					searchDir = s.baseDir
					break
				}
				searchDir = parent
				continue
			}
			if fi.IsDir() {
				break
			}
			// It's a file, list from parent.
			searchDir = filepath.Dir(searchDir)
			break
		}
	}

	var results []string
	err := filepath.WalkDir(searchDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(s.baseDir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if prefix == "" || strings.HasPrefix(rel, prefix) {
			results = append(results, rel)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("local: list %q: %w", prefix, err)
	}

	sort.Strings(results)
	if results == nil {
		results = []string{}
	}
	return results, nil
}

// safePath joins path to baseDir and ensures the result doesn't escape baseDir.
func (s *Store) safePath(path string) (string, error) {
	joined := filepath.Join(s.baseDir, path)
	abs, err := filepath.Abs(joined)
	if err != nil {
		return "", fmt.Errorf("local: resolve %q: %w", path, err)
	}
	if !strings.HasPrefix(abs, s.baseDir) {
		return "", fmt.Errorf("local: path %q escapes base directory", path)
	}
	return abs, nil
}
