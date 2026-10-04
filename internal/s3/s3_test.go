package s3

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	valid := Config{
		Endpoint:        "http://127.0.0.1:9000",
		Bucket:          "b",
		AccessKeyID:     "id",
		SecretAccessKey: "secret",
	}
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string // substring; empty means success
	}{
		{"valid http", func(*Config) {}, ""},
		{"valid https with region", func(c *Config) {
			c.Endpoint = "https://s3.us-east-1.amazonaws.com"
			c.Region = "us-east-1"
		}, ""},
		{"trailing slash", func(c *Config) { c.Endpoint = "https://s3.example.com/" }, ""},
		{"path style", func(c *Config) { c.PathStyle = true }, ""},
		{"empty endpoint", func(c *Config) { c.Endpoint = "" }, "http:// or https://"},
		{"no scheme", func(c *Config) { c.Endpoint = "s3.example.com" }, "http:// or https://"},
		{"host and port without scheme", func(c *Config) { c.Endpoint = "localhost:9000" }, "http:// or https://"},
		{"bad scheme", func(c *Config) { c.Endpoint = "ftp://s3.example.com" }, "http:// or https://"},
		{"no host", func(c *Config) { c.Endpoint = "https://" }, "no host"},
		{"path", func(c *Config) { c.Endpoint = "https://s3.example.com/bucket" }, "without a path"},
		{"query", func(c *Config) { c.Endpoint = "https://s3.example.com?x=1" }, "without a path"},
		{"userinfo", func(c *Config) { c.Endpoint = "https://u:p@s3.example.com" }, "without a path"},
		{"unparseable", func(c *Config) { c.Endpoint = "http://[::1" }, "invalid endpoint"},
		{"no bucket", func(c *Config) { c.Bucket = "" }, "bucket is required"},
		{"no access key", func(c *Config) { c.AccessKeyID = "" }, "access key ID is required"},
		{"no secret", func(c *Config) { c.SecretAccessKey = "" }, "secret access key is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.mutate(&cfg)
			c, err := New(cfg)
			if tt.wantErr == "" {
				if err != nil || c == nil {
					t.Fatalf("New() = %v, %v; want success", c, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("New() error = %v; want containing %q", err, tt.wantErr)
			}
		})
	}
}

// fakeS3 is a minimal path-style S3 server for a single bucket.
type fakeS3 struct {
	bucket    string
	accessKey string

	mu         sync.Mutex
	objects    map[string][]byte
	uploads    map[string]map[int][]byte
	nextUpload int
}

func newFakeS3(t *testing.T, bucket, accessKey string) (*fakeS3, Config) {
	t.Helper()
	f := &fakeS3{bucket: bucket, accessKey: accessKey, objects: map[string][]byte{}, uploads: map[string]map[int][]byte{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, Config{
		Endpoint:        srv.URL,
		Region:          "us-east-1",
		Bucket:          bucket,
		AccessKeyID:     accessKey,
		SecretAccessKey: "secret",
		PathStyle:       true,
	}
}

func (f *fakeS3) fail(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`, code, msg)
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(r.Header.Get("Authorization"), "Credential="+f.accessKey+"/") {
		f.fail(w, r, http.StatusForbidden, "InvalidAccessKeyId", "The access key ID you provided does not exist.")
		return
	}
	bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if bucket != f.bucket {
		f.fail(w, r, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	q := r.URL.Query()
	switch {
	case r.Method == http.MethodPost && q.Has("uploads"):
		f.nextUpload++
		id := fmt.Sprintf("upload-%d", f.nextUpload)
		f.uploads[id] = map[int][]byte{}
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><InitiateMultipartUploadResult><Bucket>%s</Bucket><Key>%s</Key><UploadId>%s</UploadId></InitiateMultipartUploadResult>`, bucket, key, id)
	case r.Method == http.MethodPut && q.Has("uploadId"):
		body, err := readBody(r)
		if err != nil {
			f.fail(w, r, http.StatusBadRequest, "IncompleteBody", err.Error())
			return
		}
		n, _ := strconv.Atoi(q.Get("partNumber"))
		f.uploads[q.Get("uploadId")][n] = body
		w.Header().Set("ETag", fmt.Sprintf(`"part-%d"`, n))
	case r.Method == http.MethodPost && q.Has("uploadId"):
		parts := f.uploads[q.Get("uploadId")]
		var all []byte
		for n := 1; n <= len(parts); n++ {
			all = append(all, parts[n]...)
		}
		delete(f.uploads, q.Get("uploadId"))
		f.objects[key] = all
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><CompleteMultipartUploadResult><Bucket>%s</Bucket><Key>%s</Key><ETag>"fake"</ETag></CompleteMultipartUploadResult>`, bucket, key)
	case r.Method == http.MethodPut:
		body, err := readBody(r)
		if err != nil {
			f.fail(w, r, http.StatusBadRequest, "IncompleteBody", err.Error())
			return
		}
		f.objects[key] = body
		w.Header().Set("ETag", `"fake"`)
	case r.Method == http.MethodGet || r.Method == http.MethodHead:
		body, ok := f.objects[key]
		if !ok {
			f.fail(w, r, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.")
			return
		}
		w.Header().Set("ETag", `"fake"`)
		http.ServeContent(w, r, key, modTime, bytes.NewReader(body))
	case r.Method == http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		f.fail(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed", "method not allowed")
	}
}

// readBody returns the request payload, decoding the aws-chunked framing that
// minio uses for streaming signatures.
func readBody(r *http.Request) ([]byte, error) {
	if !strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") {
		return io.ReadAll(r.Body)
	}
	br := bufio.NewReader(r.Body)
	var out []byte
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		size, _, _ := strings.Cut(strings.TrimSpace(line), ";")
		n, err := strconv.ParseInt(size, 16, 64)
		if err != nil {
			return nil, fmt.Errorf("bad chunk header %q", line)
		}
		if n == 0 {
			return out, nil
		}
		chunk := make([]byte, n)
		if _, err := io.ReadFull(br, chunk); err != nil {
			return nil, err
		}
		out = append(out, chunk...)
		if _, err := br.Discard(2); err != nil {
			return nil, err
		}
	}
}

func (f *fakeS3) has(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.objects[key]
	return ok
}

func TestPutGetDelete(t *testing.T) {
	ctx := context.Background()
	fake, cfg := newFakeS3(t, "bucket", "AKIDTEST")
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	const key = "backups/svc/one.tar.zst"
	if err := c.Put(ctx, key, strings.NewReader("hello"), 5); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !fake.has(key) {
		t.Fatal("object was not stored")
	}

	rc, err := c.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil || string(got) != "hello" {
		t.Fatalf("Get body = %q, %v; want hello", got, err)
	}

	if err := c.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if fake.has(key) {
		t.Fatal("object still stored after Delete")
	}
	if err := c.Delete(ctx, key); err != nil {
		t.Fatalf("Delete of missing key: %v", err)
	}
}

func TestPutUnknownSize(t *testing.T) {
	fake, cfg := newFakeS3(t, "bucket", "AKIDTEST")
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Put(context.Background(), "k", strings.NewReader("streamed"), -1); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if string(fake.objects["k"]) != "streamed" {
		t.Fatalf("stored = %q", fake.objects["k"])
	}
}

func TestGetNotFound(t *testing.T) {
	_, cfg := newFakeS3(t, "bucket", "AKIDTEST")
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rc, err := c.Get(context.Background(), "missing")
	if rc != nil {
		rc.Close()
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get error = %v; want ErrNotFound", err)
	}
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string // substring; empty means success
	}{
		{"ok", func(*Config) {}, ""},
		{"missing bucket", func(c *Config) { c.Bucket = "other" }, `bucket "other" does not exist`},
		{"bad access key", func(c *Config) { c.AccessKeyID = "WRONG" }, "invalid access key ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake, cfg := newFakeS3(t, "bucket", "AKIDTEST")
			tt.mutate(&cfg)
			c, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			err = c.Check(context.Background(), "probe")
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Check: %v", err)
				}
				if fake.has("probe") {
					t.Fatal("probe object was not deleted")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Check error = %v; want containing %q", err, tt.wantErr)
			}
			if !strings.HasPrefix(err.Error(), "s3: ") {
				t.Fatalf("Check error %q lacks s3: prefix", err)
			}
		})
	}
}

var modTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
