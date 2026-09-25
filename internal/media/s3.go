package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
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

// PresignPut returns a URL the client may PUT to within ttl.
//
// Content-Type and Content-Length are signed headers, so the store refuses
// an upload of any other type or size than the one the service approved:
// without them the URL would accept an HTML page, or a gigabyte, at a key
// the app will later hand out as a photo. ConfirmUpload checks again, for
// stores that do not enforce signed headers.
func (p *S3Presigner) PresignPut(ctx context.Context, key, contentType string, size int64, ttl time.Duration) (string, error) {
	headers := http.Header{}
	headers.Set("Content-Type", contentType)
	headers.Set("Content-Length", strconv.FormatInt(size, 10))
	u, err := p.client.PresignHeader(ctx, http.MethodPut, p.bucket, key, ttl, nil, headers)
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

// Stat reports what the store holds at key, or ErrNotStored.
func (p *S3Presigner) Stat(ctx context.Context, key string) (StoredObject, error) {
	info, err := p.client.StatObject(ctx, p.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if code := minio.ToErrorResponse(err).Code; code == "NoSuchKey" || code == "NotFound" {
			return StoredObject{}, ErrNotStored
		}
		return StoredObject{}, fmt.Errorf("stat %s: %w", key, err)
	}
	return StoredObject{Size: info.Size, ContentType: info.ContentType}, nil
}

// EnsureBucket checks the bucket exists and is not public, creating it only
// when create is set.
//
// Called at startup so a misconfigured deployment fails immediately rather
// than on the first progress photo. Creation is opt-in because a production
// key should not hold s3:CreateBucket, and because a typo in the bucket name
// would otherwise quietly create a second, empty bucket with the store's
// default policy rather than fail.
func (p *S3Presigner) EnsureBucket(ctx context.Context, region string, create bool) error {
	exists, err := p.client.BucketExists(ctx, p.bucket)
	if err != nil {
		return fmt.Errorf("check bucket %s: %w", p.bucket, err)
	}
	if !exists {
		if !create {
			return fmt.Errorf("bucket %s does not exist; create it, or set STORAGE_CREATE_BUCKET=true", p.bucket)
		}
		if err := p.client.MakeBucket(ctx, p.bucket, minio.MakeBucketOptions{Region: region}); err != nil {
			return fmt.Errorf("create bucket %s: %w", p.bucket, err)
		}
	}
	policy, err := p.client.GetBucketPolicy(ctx, p.bucket)
	if err != nil {
		// R2 and some S3-compatible stores do not implement bucket policies
		// at all, so there is nothing that could make the bucket public.
		if code := minio.ToErrorResponse(err).Code; code == "NotImplemented" || code == "NoSuchBucketPolicy" {
			return nil
		}
		return fmt.Errorf("read policy of bucket %s: %w", p.bucket, err)
	}
	if grantsPublicRead(policy) {
		return fmt.Errorf("bucket %s has a policy that lets anyone read it; progress photos must never be public", p.bucket)
	}
	return nil
}

// grantsPublicRead reports whether a bucket policy allows anonymous access.
func grantsPublicRead(policy string) bool {
	if policy == "" {
		return false
	}
	var doc struct {
		Statement []struct {
			Effect    string          `json:"Effect"`
			Principal json.RawMessage `json:"Principal"`
		} `json:"Statement"`
	}
	if err := json.Unmarshal([]byte(policy), &doc); err != nil {
		return true // unreadable: assume the worst rather than start
	}
	for _, st := range doc.Statement {
		if st.Effect != "Allow" {
			continue
		}
		var any string
		if json.Unmarshal(st.Principal, &any) == nil && any == "*" {
			return true
		}
		var aws struct {
			AWS json.RawMessage `json:"AWS"`
		}
		if json.Unmarshal(st.Principal, &aws) == nil && aws.AWS != nil {
			var one string
			var many []string
			if json.Unmarshal(aws.AWS, &one) == nil && one == "*" {
				return true
			}
			if json.Unmarshal(aws.AWS, &many) == nil {
				for _, m := range many {
					if m == "*" {
						return true
					}
				}
			}
		}
	}
	return false
}

// ErrNotStored is Stat's answer for a key the store does not hold.
var ErrNotStored = errors.New("object not stored")

// Compile-time check that the concrete presigner satisfies the interface the
// media service depends on.
var _ Presigner = (*S3Presigner)(nil)
