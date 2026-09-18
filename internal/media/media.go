// Package media manages files held in private object storage: progress photos,
// receipts, signature images and form-check videos.
//
// Object bytes never pass through this API. A client asks for an upload URL,
// PUTs directly to storage, then confirms; downloads work the same way in
// reverse. That keeps large files off the request path, and it means the only
// way to reach an object is a URL this service minted after an authorisation
// check.
//
// Nothing in the bucket is public. Presigned URLs expire in minutes, and
// progress photos are marked sensitive so they can never be served with a
// cacheable response (ADR 0003).
package media

import (
	"context"
	"fmt"
	"mime"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/db"
	"github.com/NewMux/mtdrb_go/internal/platform/clock"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// Kind is what an object depicts.
type Kind string

const (
	KindProgressPhoto  Kind = "progress_photo"
	KindReceipt        Kind = "receipt"
	KindSignature      Kind = "signature"
	KindFormCheckVideo Kind = "form_check_video"
	KindExerciseDemo   Kind = "exercise_demo"
	KindOther          Kind = "other"
)

// Sensitivity marks objects needing stricter handling.
type Sensitivity string

const (
	Standard  Sensitivity = "standard"
	Sensitive Sensitivity = "sensitive"
)

// sensitivityFor classifies a kind.
//
// Progress photos are often of a partially clothed body, and a receipt can
// carry a bank account number. Both are sensitive by default rather than by
// the uploader remembering to say so.
func sensitivityFor(k Kind) Sensitivity {
	switch k {
	case KindProgressPhoto, KindReceipt, KindSignature:
		return Sensitive
	default:
		return Standard
	}
}

// Limits on what may be uploaded, by kind.
var maxBytes = map[Kind]int64{
	KindProgressPhoto:  15 << 20,  // 15 MiB
	KindReceipt:        15 << 20,  //
	KindSignature:      2 << 20,   //
	KindFormCheckVideo: 200 << 20, // a phone clip of a working set
	KindExerciseDemo:   200 << 20,
	KindOther:          25 << 20,
}

// allowedTypes restricts uploads to media the app can actually render.
//
// An allowlist, not a blocklist: the bucket must never end up serving an HTML
// file from an origin a viewer's browser might trust.
var allowedTypes = map[Kind]map[string]string{
	KindProgressPhoto:  {"image/jpeg": ".jpg", "image/png": ".png", "image/heic": ".heic", "image/webp": ".webp"},
	KindReceipt:        {"image/jpeg": ".jpg", "image/png": ".png", "image/heic": ".heic", "image/webp": ".webp", "application/pdf": ".pdf"},
	KindSignature:      {"image/png": ".png", "image/svg+xml": ".svg", "image/jpeg": ".jpg"},
	KindFormCheckVideo: {"video/mp4": ".mp4", "video/quicktime": ".mov", "video/webm": ".webm"},
	KindExerciseDemo:   {"video/mp4": ".mp4", "video/quicktime": ".mov", "video/webm": ".webm"},
	KindOther:          {"image/jpeg": ".jpg", "image/png": ".png", "application/pdf": ".pdf"},
}

// Object is a stored file's metadata.
type Object struct {
	ID          ids.ID      `json:"id"`
	Kind        Kind        `json:"kind"`
	Sensitivity Sensitivity `json:"sensitivity"`
	ContentType string      `json:"content_type"`
	ByteSize    int64       `json:"byte_size"`
	ClientID    *ids.ID     `json:"client_id,omitempty"`
	ConfirmedAt *time.Time  `json:"confirmed_at,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
	StorageKey  string      `json:"-"`
}

// Presigner mints time-limited URLs for direct storage access.
//
// An interface rather than a concrete S3 client so the service is testable
// without a live bucket, and so MinIO, S3 and R2 are interchangeable.
type Presigner interface {
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, error)
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
	Delete(ctx context.Context, key string) error
}

// Service issues upload and download URLs and tracks object metadata.
type Service struct {
	presigner Presigner
	clock     clock.Clock
	ttl       time.Duration
}

// NewService builds the media service.
func NewService(p Presigner, c clock.Clock, ttl time.Duration) *Service {
	if c == nil {
		c = clock.System{}
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &Service{presigner: p, clock: c, ttl: ttl}
}

// UploadRequest describes a file a client wants to upload.
type UploadRequest struct {
	Kind        Kind
	ContentType string
	ByteSize    int64
	ClientID    *ids.ID
	UploadedBy  *ids.ID
}

// Upload is the instruction set returned to a client for a direct upload.
type Upload struct {
	Object    Object    `json:"object"`
	URL       string    `json:"upload_url"`
	ExpiresAt time.Time `json:"expires_at"`
	// Headers the client must send verbatim, or the presigned signature will
	// not match what storage expects.
	Headers map[string]string `json:"headers"`
}

// RequestUpload validates a proposed upload and returns a presigned PUT URL.
//
// The row is written before the upload happens, in an unconfirmed state, so
// that an abandoned upload leaves a sweepable record rather than an orphaned
// object nobody knows about.
func (s *Service) RequestUpload(ctx context.Context, tx pgx.Tx, tenantID ids.ID, req UploadRequest) (Upload, error) {
	contentType := normalizeContentType(req.ContentType)

	types, known := allowedTypes[req.Kind]
	if !known {
		return Upload{}, errs.Invalid(errs.CodeValidation, "unknown media kind %q", req.Kind).
			WithField("kind", "is not a supported media kind")
	}
	ext, permitted := types[contentType]
	if !permitted {
		return Upload{}, errs.Invalid(errs.CodeValidation,
			"%s does not accept %s files", req.Kind, contentType).
			WithField("content_type", "is not allowed for this media kind")
	}
	if req.ByteSize <= 0 {
		return Upload{}, errs.Invalid(errs.CodeValidation, "an upload needs a size").
			WithField("byte_size", "must be greater than zero")
	}
	if limit := maxBytes[req.Kind]; req.ByteSize > limit {
		return Upload{}, errs.Invalid(errs.CodeValidation,
			"a %s may be at most %d bytes, got %d", req.Kind, limit, req.ByteSize).
			WithField("byte_size", fmt.Sprintf("must be at most %d bytes", limit))
	}

	obj := Object{
		ID:          ids.New(),
		Kind:        req.Kind,
		Sensitivity: sensitivityFor(req.Kind),
		ContentType: contentType,
		ByteSize:    req.ByteSize,
		ClientID:    req.ClientID,
	}
	// The tenant prefix means a bug in key generation cannot collide across
	// tenants, and it makes a whole tenant's objects removable by prefix.
	obj.StorageKey = path.Join(tenantID.String(), string(req.Kind), obj.ID.String()+ext)

	if err := tx.QueryRow(ctx, `
		INSERT INTO media_objects
			(id, tenant_id, storage_key, kind, sensitivity, content_type, byte_size, client_id, uploaded_by)
		VALUES ($1, $2, $3, $4::media_kind, $5::media_sensitivity, $6, $7, $8, $9)
		RETURNING created_at`,
		obj.ID, tenantID, obj.StorageKey, string(obj.Kind), string(obj.Sensitivity),
		obj.ContentType, obj.ByteSize, obj.ClientID, req.UploadedBy,
	).Scan(&obj.CreatedAt); err != nil {
		if db.IsForeignKeyViolation(err) {
			return Upload{}, errs.NotFound("client")
		}
		return Upload{}, errs.Internal(err, "record media object")
	}

	url, err := s.presigner.PresignPut(ctx, obj.StorageKey, obj.ContentType, s.ttl)
	if err != nil {
		return Upload{}, errs.Internal(err, "presign upload")
	}

	return Upload{
		Object:    obj,
		URL:       url,
		ExpiresAt: s.clock.Now().Add(s.ttl),
		Headers:   map[string]string{"Content-Type": obj.ContentType},
	}, nil
}

// ConfirmUpload marks an object as successfully stored.
func (s *Service) ConfirmUpload(ctx context.Context, tx pgx.Tx, objectID ids.ID) (Object, error) {
	tag, err := tx.Exec(ctx,
		`UPDATE media_objects SET confirmed_at = now()
		  WHERE id = $1 AND confirmed_at IS NULL AND deleted_at IS NULL`, objectID)
	if err != nil {
		return Object{}, errs.Internal(err, "confirm upload")
	}
	if tag.RowsAffected() == 0 {
		// Either unknown, already confirmed, or deleted. Confirming twice is
		// harmless, so this is only an error when the object cannot be found.
		return s.Get(ctx, tx, objectID)
	}
	return s.Get(ctx, tx, objectID)
}

// Get loads an object's metadata.
func (s *Service) Get(ctx context.Context, tx pgx.Tx, objectID ids.ID) (Object, error) {
	var o Object
	var kind, sensitivity string
	err := tx.QueryRow(ctx, `
		SELECT id, storage_key, kind::text, sensitivity::text, content_type, byte_size,
		       client_id, confirmed_at, created_at
		  FROM media_objects WHERE id = $1 AND deleted_at IS NULL`, objectID,
	).Scan(&o.ID, &o.StorageKey, &kind, &sensitivity, &o.ContentType, &o.ByteSize,
		&o.ClientID, &o.ConfirmedAt, &o.CreatedAt)
	if err != nil {
		if db.IsNoRows(err) {
			return Object{}, errs.NotFound("media object")
		}
		return Object{}, errs.Internal(err, "load media object")
	}
	o.Kind, o.Sensitivity = Kind(kind), Sensitivity(sensitivity)
	return o, nil
}

// Download is a time-limited link to fetch an object.
type Download struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
	// Sensitive tells the client never to persist or cache the bytes.
	Sensitive bool `json:"sensitive"`
}

// DownloadURL issues a short-lived presigned GET.
//
// The row is fetched first, under row-level security, so a caller who cannot
// see the object cannot obtain a link to it: authorisation happens before the
// URL exists rather than after it is handed out.
func (s *Service) DownloadURL(ctx context.Context, tx pgx.Tx, objectID ids.ID) (Download, error) {
	obj, err := s.Get(ctx, tx, objectID)
	if err != nil {
		return Download{}, err
	}
	url, err := s.presigner.PresignGet(ctx, obj.StorageKey, s.ttl)
	if err != nil {
		return Download{}, errs.Internal(err, "presign download")
	}
	return Download{
		URL:       url,
		ExpiresAt: s.clock.Now().Add(s.ttl),
		Sensitive: obj.Sensitivity == Sensitive,
	}, nil
}

// ListForClient returns a client's objects of a given kind, newest first —
// the progress photo gallery.
func (s *Service) ListForClient(ctx context.Context, tx pgx.Tx, clientID ids.ID, kind Kind) ([]Object, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, storage_key, kind::text, sensitivity::text, content_type, byte_size,
		       client_id, confirmed_at, created_at
		  FROM media_objects
		 WHERE client_id = $1 AND kind = $2::media_kind
		   AND deleted_at IS NULL AND confirmed_at IS NOT NULL
		 ORDER BY created_at DESC`, clientID, string(kind))
	if err != nil {
		return nil, errs.Internal(err, "list media objects")
	}
	defer rows.Close()

	var out []Object
	for rows.Next() {
		var o Object
		var k, sens string
		if err := rows.Scan(&o.ID, &o.StorageKey, &k, &sens, &o.ContentType, &o.ByteSize,
			&o.ClientID, &o.ConfirmedAt, &o.CreatedAt); err != nil {
			return nil, errs.Internal(err, "scan media object")
		}
		o.Kind, o.Sensitivity = Kind(k), Sensitivity(sens)
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err, "read media objects")
	}
	return out, nil
}

// Delete soft-deletes the row and removes the stored object.
//
// The row is retained so anything referencing it — a waiver signature, say —
// does not break; only the bytes go.
func (s *Service) Delete(ctx context.Context, tx pgx.Tx, objectID ids.ID) error {
	obj, err := s.Get(ctx, tx, objectID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE media_objects SET deleted_at = now() WHERE id = $1`, objectID); err != nil {
		return errs.Internal(err, "delete media object")
	}
	if err := s.presigner.Delete(ctx, obj.StorageKey); err != nil {
		return errs.Internal(err, "remove stored object")
	}
	return nil
}

// normalizeContentType strips parameters and lowercases, so that
// "IMAGE/JPEG; charset=binary" matches the allowlist.
func normalizeContentType(ct string) string {
	parsed, _, err := mime.ParseMediaType(strings.TrimSpace(ct))
	if err != nil {
		return strings.ToLower(strings.TrimSpace(ct))
	}
	return strings.ToLower(parsed)
}
