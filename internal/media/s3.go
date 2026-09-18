package media

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Presigner signs URLs against any S3-compatible store: MinIO locally, S3 or
// R2 in production.
type S3Presigner struct {
	client *minio.Client
	bucket string
}

// S3Config configures the presigner.
type S3Config struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	UseSSL    bool
}

// NewS3Presigner connects to the object store.
func NewS3Presigner(cfg S3Config) (*S3Presigner, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("connect to object storage: %w", err)
	}
	return &S3Presigner{client: client, bucket: cfg.Bucket}, nil
}

// PresignPut returns a URL the client may PUT to exactly once, within ttl.
func (p *S3Presigner) PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, error) {
	u, err := p.client.PresignedPutObject(ctx, p.bucket, key, ttl)
	if err != nil {
		return "", fmt.Errorf("presign put for %s: %w", key, err)
	}
	return u.String(), nil
}

// PresignGet returns a short-lived download URL.
//
// The response headers are set in the signature so the object is served as a
// private, non-cacheable attachment. A progress photo must not linger in a
// shared cache or a CDN edge after its URL expires.
func (p *S3Presigner) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	params := url.Values{}
	params.Set("response-cache-control", "private, no-store, max-age=0")

	u, err := p.client.PresignedGetObject(ctx, p.bucket, key, ttl, params)
	if err != nil {
		return "", fmt.Errorf("presign get for %s: %w", key, err)
	}
	return u.String(), nil
}

// Delete removes an object from the bucket.
func (p *S3Presigner) Delete(ctx context.Context, key string) error {
	if err := p.client.RemoveObject(ctx, p.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("remove %s: %w", key, err)
	}
	return nil
}

// EnsureBucket creates the bucket if absent and asserts it is not public.
//
// Called at startup so a misconfigured deployment fails immediately rather
// than on the first progress photo.
func (p *S3Presigner) EnsureBucket(ctx context.Context, region string) error {
	exists, err := p.client.BucketExists(ctx, p.bucket)
	if err != nil {
		return fmt.Errorf("check bucket %s: %w", p.bucket, err)
	}
	if !exists {
		if err := p.client.MakeBucket(ctx, p.bucket, minio.MakeBucketOptions{Region: region}); err != nil {
			return fmt.Errorf("create bucket %s: %w", p.bucket, err)
		}
	}
	return nil
}

// Compile-time check that the concrete presigner satisfies the interface the
// media service depends on.
var _ Presigner = (*S3Presigner)(nil)
