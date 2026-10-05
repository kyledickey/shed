//go:build integration

package s3

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// integrationClient returns a client for the bucket named by the
// SHED_S3_TEST_* environment variables, creating the bucket if needed. The
// test is skipped when they are unset.
func integrationClient(t *testing.T) *Client {
	t.Helper()
	endpoint := os.Getenv("SHED_S3_TEST_ENDPOINT")
	bucket := os.Getenv("SHED_S3_TEST_BUCKET")
	accessKey := os.Getenv("SHED_S3_TEST_ACCESS_KEY")
	secretKey := os.Getenv("SHED_S3_TEST_SECRET_KEY")
	if endpoint == "" || bucket == "" || accessKey == "" || secretKey == "" {
		t.Skip("SHED_S3_TEST_ENDPOINT, _BUCKET, _ACCESS_KEY, and _SECRET_KEY are not all set")
	}
	cfg := Config{
		Endpoint:        endpoint,
		Bucket:          bucket,
		AccessKeyID:     accessKey,
		SecretAccessKey: secretKey,
		PathStyle:       true,
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	u, _ := url.Parse(endpoint)
	ctx := context.Background()
	if ok, err := c.mc.BucketExists(ctx, bucket); err == nil && !ok {
		admin, err := minio.New(u.Host, &minio.Options{
			Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
			Secure: u.Scheme == "https",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := admin.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			t.Fatalf("create bucket: %v", err)
		}
	}
	return c
}

func TestIntegrationRoundTrip(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()
	key := fmt.Sprintf("shed-test/%d/small", time.Now().UnixNano())
	t.Cleanup(func() { c.Delete(ctx, key) })

	if err := c.Put(ctx, key, bytes.NewReader([]byte("hello")), 5); err != nil {
		t.Fatalf("Put: %v", err)
	}
	rc, err := c.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil || string(got) != "hello" {
		t.Fatalf("Get body = %q, %v", got, err)
	}

	if err := c.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := c.Get(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete error = %v; want ErrNotFound", err)
	}
	if err := c.Delete(ctx, key); err != nil {
		t.Fatalf("Delete of missing key: %v", err)
	}
}

func TestIntegrationMultipart(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()
	key := fmt.Sprintf("shed-test/%d/large", time.Now().UnixNano())
	t.Cleanup(func() { c.Delete(ctx, key) })

	// 70 MiB of deterministic data, streamed with unknown size and more than
	// one part. The reader hides its length so Put cannot infer it.
	const total = 70 << 20
	gen := func() io.Reader {
		return io.LimitReader(rand.NewChaCha8([32]byte{1}), total)
	}
	want := sha256.New()
	if _, err := io.Copy(want, gen()); err != nil {
		t.Fatal(err)
	}
	if err := c.Put(ctx, key, gen(), -1); err != nil {
		t.Fatalf("Put: %v", err)
	}

	rc, err := c.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	got := sha256.New()
	n, err := io.Copy(got, rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if n != total || !bytes.Equal(got.Sum(nil), want.Sum(nil)) {
		t.Fatalf("downloaded %d bytes, checksum match = %v; want %d bytes and a match", n, bytes.Equal(got.Sum(nil), want.Sum(nil)), total)
	}
}

func TestIntegrationCheck(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()
	if err := c.Check(ctx, "shed-test/check-probe"); err != nil {
		t.Fatalf("Check: %v", err)
	}

	bad := *c
	bad.bucket = "shed-test-no-such-bucket"
	err := bad.Check(ctx, "shed-test/check-probe")
	if err == nil {
		t.Fatal("Check on a missing bucket succeeded")
	}
	t.Logf("missing bucket: %v", err)
}
