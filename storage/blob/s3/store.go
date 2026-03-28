package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/ratrektlabs/rl-agent/storage/blob"
)

// Store implements blob.BlobStore backed by an S3-compatible bucket.
type Store struct {
	client   *s3.Client
	bucket   string
	prefix   string
	endpoint string
}

// Option configures a Store during construction.
type Option func(*Store)

// WithPrefix sets a key prefix applied to every operation.
func WithPrefix(p string) Option {
	return func(s *Store) {
		s.prefix = strings.TrimSuffix(p, "/")
	}
}

// WithEndpoint overrides the default AWS endpoint (MinIO, Cloudflare R2, etc).
func WithEndpoint(url string) Option {
	return func(s *Store) {
		s.endpoint = url
	}
}

// New creates a new S3-backed BlobStore.
func New(ctx context.Context, bucket string, opts ...Option) (*Store, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("s3: load aws config: %w", err)
	}

	s := &Store{bucket: bucket}
	for _, o := range opts {
		o(s)
	}

	var clientOpts []func(*s3.Options)
	if s.endpoint != "" {
		clientOpts = append(clientOpts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(s.endpoint)
			o.UsePathStyle = true
		})
	}

	s.client = s3.NewFromConfig(cfg, clientOpts...)
	return s, nil
}

func (s *Store) fullKey(p string) string {
	if s.prefix == "" {
		return p
	}
	return s.prefix + "/" + p
}

func (s *Store) stripPrefix(key string) string {
	if s.prefix == "" {
		return key
	}
	return strings.TrimPrefix(key, s.prefix+"/")
}

func (s *Store) Read(ctx context.Context, path string) ([]byte, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.fullKey(path)),
	})
	if err != nil {
		return nil, fmt.Errorf("s3: read %q: %w", path, err)
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("s3: read body: %w", err)
	}
	return data, nil
}

func (s *Store) Write(ctx context.Context, path string, data []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.fullKey(path)),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return fmt.Errorf("s3: write %q: %w", path, err)
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, path string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.fullKey(path)),
	})
	if err != nil {
		return fmt.Errorf("s3: delete %q: %w", path, err)
	}
	return nil
}

func (s *Store) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string

	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(s.fullKey(prefix)),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("s3: list: %w", err)
		}
		for _, obj := range page.Contents {
			if obj.Key == nil {
				continue
			}
			key := *obj.Key
			if strings.HasSuffix(key, "/") {
				continue
			}
			keys = append(keys, s.stripPrefix(key))
		}
	}

	if keys == nil {
		keys = []string{}
	}
	return keys, nil
}

var _ blob.BlobStore = (*Store)(nil)
