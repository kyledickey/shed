// Package s3 is a small client for S3-compatible object storage. It covers
// the handful of operations shed needs for backups: upload, download, delete,
// and a configuration check.
package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// partSize is the multipart part size for uploads. With an unknown object
// size, minio buffers one part in memory, so this bounds memory use while
// still allowing objects of up to about 640 GiB with the 10000-part limit.
const partSize = 64 << 20

// ErrNotFound is returned when an object does not exist.
var ErrNotFound = errors.New("s3: object not found")

// Config describes an S3-compatible bucket.
type Config struct {
	// Endpoint is a URL such as "https://s3.us-east-1.amazonaws.com" or
	// "http://127.0.0.1:9000". The scheme decides whether TLS is used.
	Endpoint string
	// Region is the bucket region. It may be empty.
	Region string
	// Bucket is the bucket name.
	Bucket string
	// AccessKeyID and SecretAccessKey are the credentials.
	AccessKeyID     string
	SecretAccessKey string
	// PathStyle selects path-style addressing, which MinIO and many
	// self-hosted stores need. When false, the addressing style is chosen
	// automatically (virtual-host style where the endpoint supports it).
	PathStyle bool
}

// Client talks to one bucket. It is safe for concurrent use.
type Client struct {
	mc     *minio.Client
	bucket string
}

// New validates cfg and returns a Client for it. It does not contact the
// server; use [Client.Check] for that.
func New(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("s3: invalid endpoint %q: %w", cfg.Endpoint, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("s3: endpoint %q must start with http:// or https://", cfg.Endpoint)
	}
	if u.Host == "" || u.Hostname() == "" {
		return nil, fmt.Errorf("s3: endpoint %q has no host", cfg.Endpoint)
	}
	if u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, fmt.Errorf("s3: endpoint %q must be a bare scheme and host, without a path, query, or credentials", cfg.Endpoint)
	}
	if cfg.Bucket == "" {
		return nil, errors.New("s3: bucket is required")
	}
	if cfg.AccessKeyID == "" {
		return nil, errors.New("s3: access key ID is required")
	}
	if cfg.SecretAccessKey == "" {
		return nil, errors.New("s3: secret access key is required")
	}

	lookup := minio.BucketLookupAuto
	if cfg.PathStyle {
		lookup = minio.BucketLookupPath
	}
	mc, err := minio.New(u.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure:       u.Scheme == "https",
		Region:       cfg.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("s3: create client: %w", err)
	}
	return &Client{mc: mc, bucket: cfg.Bucket}, nil
}

// Put uploads r as the object key. A size of -1 means the length is unknown;
// the data is then streamed in multipart parts.
func (c *Client) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	_, err := c.mc.PutObject(ctx, c.bucket, key, r, size, minio.PutObjectOptions{
		PartSize: partSize,
	})
	if err != nil {
		return fmt.Errorf("s3: put %s: %w", key, c.explain(err))
	}
	return nil
}

// Get opens the object key for reading. It returns an error wrapping
// [ErrNotFound] if the object does not exist. The caller must close the
// returned reader.
func (c *Client) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	rc, err := c.open(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("s3: get %s: %w", key, err)
	}
	return rc, nil
}

// open starts a download and returns a readable error if it fails.
func (c *Client) open(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, c.explain(err)
	}
	// GetObject is lazy; Stat forces the request so errors surface now.
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		return nil, c.explain(err)
	}
	return obj, nil
}

// Delete removes the object key. Deleting a missing object is not an error.
func (c *Client) Delete(ctx context.Context, key string) error {
	err := c.mc.RemoveObject(ctx, c.bucket, key, minio.RemoveObjectOptions{})
	if err != nil && !errors.Is(c.explain(err), ErrNotFound) {
		return fmt.Errorf("s3: delete %s: %w", key, c.explain(err))
	}
	return nil
}

// Check verifies the credentials, the bucket, and write access by uploading a
// small probe object at key, reading it back, and deleting it. The returned
// errors are meant to be shown to a person.
func (c *Client) Check(ctx context.Context, key string) error {
	probe := []byte("shed s3 check\n")
	_, err := c.mc.PutObject(ctx, c.bucket, key, bytes.NewReader(probe), int64(len(probe)), minio.PutObjectOptions{})
	if err != nil {
		return fmt.Errorf("s3: %w", c.explain(err))
	}
	deleted := false
	defer func() {
		if !deleted {
			_ = c.mc.RemoveObject(context.WithoutCancel(ctx), c.bucket, key, minio.RemoveObjectOptions{})
		}
	}()

	rc, err := c.open(ctx, key)
	if err != nil {
		return fmt.Errorf("s3: read back probe object: %w", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("s3: read back probe object: %w", c.explain(err))
	}
	if !bytes.Equal(got, probe) {
		return errors.New("s3: read back probe object: content differs from what was written")
	}
	if err := c.mc.RemoveObject(ctx, c.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("s3: delete probe object: %w", c.explain(err))
	}
	deleted = true
	return nil
}

// explain converts a minio error into a readable one. Missing objects map to
// [ErrNotFound]; other server errors are rewritten with the bucket name and
// the server's message. Errors that did not come from the server pass through.
func (c *Client) explain(err error) error {
	resp := minio.ToErrorResponse(err)
	switch resp.Code {
	case "":
		return err
	case "NoSuchKey":
		return ErrNotFound
	case "NoSuchBucket":
		return fmt.Errorf("bucket %q does not exist", c.bucket)
	case "AccessDenied":
		return fmt.Errorf("access denied: %s", describe(resp))
	case "InvalidAccessKeyId":
		return fmt.Errorf("invalid access key ID: %s", describe(resp))
	case "SignatureDoesNotMatch":
		return fmt.Errorf("signature mismatch, check the secret access key and region: %s", describe(resp))
	}
	return fmt.Errorf("%s: %s", resp.Code, describe(resp))
}

// describe returns the server's message, or the HTTP status when it sent none.
func describe(resp minio.ErrorResponse) string {
	if m := strings.TrimSpace(resp.Message); m != "" {
		return m
	}
	return http.StatusText(resp.StatusCode)
}
