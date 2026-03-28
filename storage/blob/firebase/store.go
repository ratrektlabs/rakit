package firebase

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"cloud.google.com/go/storage"
	firebase "firebase.google.com/go/v4"
	fbStorage "firebase.google.com/go/v4/storage"
	"github.com/ratrektlabs/rakit/storage/blob"
	"google.golang.org/api/iterator"
)

// Store implements blob.BlobStore backed by Firebase / Google Cloud Storage.
type Store struct {
	client *fbStorage.Client
	bucket string
}

// Option configures a Store during construction.
type Option func(*Store)

// New creates a new Firebase-backed BlobStore using the Firebase Admin SDK.
//
// If no pre-configured client is supplied via options, a default Firebase app is
// initialised (which relies on GOOGLE_APPLICATION_CREDENTIALS or the default
// service account when running on GCP).
//
// Parameters:
//   - ctx:    context for initialising the Firebase client
//   - bucket: Cloud Storage bucket name (without "gs://" prefix)
//   - opts:   optional functional options (WithApp, WithClient)
func New(ctx context.Context, bucket string, opts ...Option) (*Store, error) {
	s := &Store{
		bucket: strings.TrimPrefix(bucket, "gs://"),
	}

	for _, o := range opts {
		o(s)
	}

	if s.client == nil {
		app, err := firebase.NewApp(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("firebase: init app: %w", err)
		}
		c, err := app.Storage(ctx)
		if err != nil {
			return nil, fmt.Errorf("firebase: init storage client: %w", err)
		}
		s.client = c
	}

	return s, nil
}

// WithApp provides a pre-initialised Firebase App. The Store derives its
// Storage client from this app using context.Background. If you need to
// control the context, initialise the client manually and use WithClient.
func WithApp(app *firebase.App) Option {
	return func(s *Store) {
		c, err := app.Storage(context.Background())
		if err == nil {
			s.client = c
		}
	}
}

// WithClient provides a pre-built Firebase Storage client, giving full control
// over initialisation to the caller.
func WithClient(client *fbStorage.Client) Option {
	return func(s *Store) {
		s.client = client
	}
}

// bucketHandle returns a handle to the configured bucket. The Firebase Admin
// SDK's Bucket method validates the bucket name and returns an error for
// invalid names.
func (s *Store) bucketHandle(ctx context.Context) (*storage.BucketHandle, error) {
	return s.client.Bucket(s.bucket)
}

// Read downloads the object at path and returns its contents.
func (s *Store) Read(ctx context.Context, path string) ([]byte, error) {
	bh, err := s.bucketHandle(ctx)
	if err != nil {
		return nil, err
	}

	reader, err := bh.Object(path).NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("firebase: read %q: %w", path, err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("firebase: read body %q: %w", path, err)
	}
	return data, nil
}

// Write uploads data to the given path, overwriting any existing object.
func (s *Store) Write(ctx context.Context, path string, data []byte) error {
	bh, err := s.bucketHandle(ctx)
	if err != nil {
		return err
	}

	writer := bh.Object(path).NewWriter(ctx)
	writer.ContentType = "application/octet-stream"

	if _, err := writer.Write(data); err != nil {
		return fmt.Errorf("firebase: write %q: %w", path, err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("firebase: close writer %q: %w", path, err)
	}
	return nil
}

// Delete removes the object at path from the bucket.
func (s *Store) Delete(ctx context.Context, path string) error {
	bh, err := s.bucketHandle(ctx)
	if err != nil {
		return err
	}

	if err := bh.Object(path).Delete(ctx); err != nil {
		return fmt.Errorf("firebase: delete %q: %w", path, err)
	}
	return nil
}

// List returns object names that begin with prefix. Returned paths are
// relative to the bucket root. Results are sorted lexicographically.
func (s *Store) List(ctx context.Context, prefix string) ([]string, error) {
	bh, err := s.bucketHandle(ctx)
	if err != nil {
		return nil, err
	}

	it := bh.Objects(ctx, &storage.Query{Prefix: prefix})

	var results []string
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("firebase: list %q: %w", prefix, err)
		}

		// Skip directory placeholder objects.
		if strings.HasSuffix(attrs.Name, "/") {
			continue
		}
		// Name holds the full object path within the bucket.
		if attrs.Name != "" {
			results = append(results, attrs.Name)
		}
	}

	sort.Strings(results)

	if results == nil {
		results = []string{}
	}
	return results, nil
}

// Compile-time interface check.
var _ blob.BlobStore = (*Store)(nil)
