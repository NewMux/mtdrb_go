package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// TestS3PresignerAgainstARealStore drives the presigner against a running
// S3-compatible server, the way the app and a browser use it. Skipped unless
// STORAGE_TEST_ENDPOINT is set, so CI stays hermetic:
//
//	STORAGE_TEST_ENDPOINT=localhost:8333 STORAGE_TEST_ACCESS_KEY=... \
//	STORAGE_TEST_SECRET_KEY=... go test ./internal/media -run RealStore
func TestS3PresignerAgainstARealStore(t *testing.T) {
	endpoint := os.Getenv("STORAGE_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("STORAGE_TEST_ENDPOINT not set")
	}
	ctx := context.Background()
	bucket := "cp-live-" + strings.ToLower(ids.New().String()[:8])
	p, err := NewS3Presigner(S3Config{
		Endpoint: endpoint, Region: "us-east-1", Bucket: bucket,
		AccessKey: os.Getenv("STORAGE_TEST_ACCESS_KEY"), SecretKey: os.Getenv("STORAGE_TEST_SECRET_KEY"),
		UseSSL: os.Getenv("STORAGE_TEST_SSL") == "true",
	})
	if err != nil {
		t.Fatal(err)
	}

	// A missing bucket is an error unless creation was asked for.
	if err := p.EnsureBucket(ctx, "us-east-1", false); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing bucket without create: %v", err)
	}
	if err := p.EnsureBucket(ctx, "us-east-1", true); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	if err := p.EnsureBucket(ctx, "us-east-1", false); err != nil {
		t.Fatalf("existing bucket: %v", err)
	}

	key := "tenant/progress_photo/" + ids.New().String() + ".jpg"
	photo := bytes.Repeat([]byte{0xff, 0xd8, 0xff}, 1000)

	if _, err := p.Stat(ctx, key); !errors.Is(err, ErrNotStored) {
		t.Fatalf("stat before upload = %v, want ErrNotStored", err)
	}

	put, err := p.PresignPut(ctx, key, "image/jpeg", int64(len(photo)), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// The wrong type is refused by the signature.
	if status := upload(t, put, "text/html", photo); status/100 == 2 {
		t.Errorf("a PUT with a different Content-Type than was signed succeeded (%d)", status)
	}
	// What the app sends: exactly the approved type and size.
	if status := upload(t, put, "image/jpeg", photo); status != http.StatusOK {
		t.Fatalf("upload = %d", status)
	}

	stored, err := p.Stat(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Size != int64(len(photo)) || stored.ContentType != "image/jpeg" {
		t.Errorf("stat = %+v", stored)
	}

	get, err := p.PresignGet(ctx, key, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(get) // #nosec G107 -- a URL this test minted
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, photo) {
		t.Errorf("download = %d, %d bytes", resp.StatusCode, len(body))
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("a sensitive download was served with Cache-Control %q", cc)
	}

	// Anonymous reads are refused: the bucket is private.
	anon, err := http.Get(strings.Split(get, "?")[0]) // #nosec G107
	if err == nil {
		_ = anon.Body.Close()
		if anon.StatusCode == http.StatusOK {
			t.Error("the object is readable without a signature")
		}
	}

	if err := p.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Stat(ctx, key); !errors.Is(err, ErrNotStored) {
		t.Errorf("stat after delete = %v", err)
	}
	_ = p.client.RemoveBucket(ctx, bucket)
}

func upload(t *testing.T, url, contentType string, body []byte) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}
