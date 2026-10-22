package hark

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

const token = "tok_s3cretS3cret"

type captured struct {
	method, path, contentType, key string
	body                           map[string]any
}

// server returns a Client whose requests go to a test server that records
// each request and replies with status, header, and body.
func server(t *testing.T, status int, header http.Header, body string) (*Client, *captured) {
	t.Helper()
	got := &captured{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		got.method, got.path = r.Method, r.URL.Path
		got.contentType = r.Header.Get("Content-Type")
		got.key = r.Header.Get("Idempotency-Key")
		got.body = nil
		_ = json.Unmarshal(raw, &got.body)
		for k, v := range header {
			w.Header()[k] = v
		}
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(ts.Close)
	c, err := New(ts.URL+"/hooks/"+token, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c, got
}

func TestNew(t *testing.T) {
	tests := []struct {
		url string
		ok  bool
	}{
		{"https://hark.ryan.ceo/hooks/" + token, true},
		{"http://127.0.0.1:8080/hooks/" + token, true},
		{"http://localhost/hooks/" + token, true},
		{"http://[::1]/hooks/" + token, true},
		{"http://hark.ryan.ceo/hooks/" + token, false},
		{"ftp://hark.ryan.ceo/hooks/" + token, false},
		{"https:///hooks/" + token, false},
		{"https://hark.ryan.ceo/hooks/", false},
		{"https://hark.ryan.ceo/hooks/" + token + "/events", false},
		{"https://hark.ryan.ceo/other/" + token, false},
		{"https://hark.ryan.ceo/hooks/" + token + "?a=b", false},
		{"https://hark.ryan.ceo/hooks/" + token + "#frag", false},
		{"https://u:p@hark.ryan.ceo/hooks/" + token, false},
		{"://" + token, false},
	}
	for _, tt := range tests {
		_, err := New(tt.url, nil)
		if (err == nil) != tt.ok {
			t.Errorf("New(%q) error = %v, want ok %v", tt.url, err, tt.ok)
		}
		if err != nil && strings.Contains(err.Error(), token) {
			t.Errorf("New(%q) error %q contains the token", tt.url, err)
		}
	}
}

func TestSendRequest(t *testing.T) {
	c, got := server(t, http.StatusOK, nil, `{"ok":true,"eventId":"evt_1","delivered":2}`)
	n := Notification{
		Body:    "  Deploy failed.  ",
		Title:   "shed",
		Summary: "web failed",
		URL:     "https://shed.example/deployments/1",
		Project: "acme",
	}
	res, err := c.Send(context.Background(), n, "deploy:1:failed")
	if err != nil {
		t.Fatal(err)
	}
	if res != (Result{EventID: "evt_1", Delivered: 2}) {
		t.Errorf("result = %+v", res)
	}
	if got.method != http.MethodPost || got.path != "/hooks/"+token {
		t.Errorf("request = %s %s", got.method, got.path)
	}
	if got.contentType != "application/json" {
		t.Errorf("Content-Type = %q", got.contentType)
	}
	if got.key != "deploy:1:failed" {
		t.Errorf("Idempotency-Key = %q", got.key)
	}
	want := map[string]any{
		"body":    "Deploy failed.",
		"title":   "shed",
		"summary": "web failed",
		"url":     "https://shed.example/deployments/1",
		"project": "acme",
	}
	for k, v := range want {
		if got.body[k] != v {
			t.Errorf("body[%q] = %v, want %v", k, got.body[k], v)
		}
	}
}

func TestSendOmitsEmptyFields(t *testing.T) {
	c, got := server(t, http.StatusOK, nil, `{"ok":true,"eventId":"evt_1","delivered":0}`)
	res, err := c.Send(context.Background(), Notification{Body: "hi"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Delivered != 0 {
		t.Errorf("Delivered = %d, want 0", res.Delivered)
	}
	if len(got.body) != 1 || got.body["body"] != "hi" {
		t.Errorf("body = %v, want only body", got.body)
	}
	if got.key != "" {
		t.Errorf("Idempotency-Key = %q, want none", got.key)
	}
}

func TestSendRejectsBeforeRequest(t *testing.T) {
	tests := []struct {
		name string
		n    Notification
		key  string
	}{
		{"empty body", Notification{Body: " \n "}, ""},
		{"long key", Notification{Body: "hi"}, strings.Repeat("k", 201)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, got := server(t, http.StatusOK, nil, `{}`)
			if _, err := c.Send(context.Background(), tt.n, tt.key); err == nil {
				t.Fatal("Send succeeded, want error")
			}
			if got.method != "" {
				t.Error("Send made a request")
			}
		})
	}
}

func TestSendStatus(t *testing.T) {
	tests := []struct {
		name   string
		status int
		header http.Header
		body   string
		check  func(t *testing.T, res Result, err error)
	}{
		{
			name: "processing", status: http.StatusAccepted, body: `{"ok":true}`,
			check: func(t *testing.T, res Result, err error) {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
			},
		},
		{
			name: "invalid payload", status: http.StatusBadRequest, body: `{"error":"body too long"}`,
			check: wantErrContaining("body too long"),
		},
		{
			name: "pro feature", status: http.StatusPaymentRequired, body: `{"message":"Pro required"}`,
			check: wantErrContaining("Pro required"),
		},
		{
			name: "unknown token", status: http.StatusNotFound, body: `{"error":"not found"}`,
			check: func(t *testing.T, _ Result, err error) {
				if !errors.Is(err, ErrUnknownToken) {
					t.Fatalf("err = %v, want ErrUnknownToken", err)
				}
			},
		},
		{
			name: "key reused", status: http.StatusConflict, body: `{"error":"idempotency key reused"}`,
			check: wantErrContaining("idempotency key reused"),
		},
		{
			name: "rate limit header", status: http.StatusTooManyRequests,
			header: http.Header{"Retry-After": {"60"}}, body: `{"retryAfterSeconds":30}`,
			check: wantRetryAfter(60 * time.Second),
		},
		{
			name: "rate limit body", status: http.StatusTooManyRequests, body: `{"retryAfterSeconds":30}`,
			check: wantRetryAfter(30 * time.Second),
		},
		{
			name: "monthly allowance", status: http.StatusTooManyRequests, body: `{"error":"monthly limit"}`,
			check: wantRetryAfter(0),
		},
		{
			name: "expo rejected", status: http.StatusBadGateway, body: "upstream failure",
			check: wantErrContaining("upstream failure"),
		},
		{
			name: "error echoes token", status: http.StatusBadRequest, body: `{"error":"bad token ` + token + `"}`,
			check: wantErrContaining("bad token <token>"),
		},
		{
			name: "malformed success", status: http.StatusOK, body: "not json",
			check: wantErrContaining("decode response"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := server(t, tt.status, tt.header, tt.body)
			res, err := c.Send(context.Background(), Notification{Body: "hi"}, "")
			if err != nil && strings.Contains(err.Error(), token) {
				t.Errorf("error %q contains the token", err)
			}
			tt.check(t, res, err)
		})
	}
}

func wantErrContaining(s string) func(*testing.T, Result, error) {
	return func(t *testing.T, _ Result, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), s) {
			t.Fatalf("err = %v, want it to contain %q", err, s)
		}
	}
}

func wantRetryAfter(d time.Duration) func(*testing.T, Result, error) {
	return func(t *testing.T, _ Result, err error) {
		t.Helper()
		rl, ok := errors.AsType[*RateLimitError](err)
		if !ok {
			t.Fatalf("err = %v, want *RateLimitError", err)
		}
		if rl.RetryAfter != d {
			t.Errorf("RetryAfter = %s, want %s", rl.RetryAfter, d)
		}
	}
}

func TestSendTransportErrorHidesToken(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	addr := ts.URL
	ts.Close()
	c, err := New(addr+"/hooks/"+token, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Send(context.Background(), Notification{Body: "hi"}, "")
	if err == nil {
		t.Fatal("Send succeeded against a closed server")
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("error %q contains the token", err)
	}
}

func TestSendCanceled(t *testing.T) {
	c, _ := server(t, http.StatusOK, nil, `{}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Send(ctx, Notification{Body: "hi"}, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("error %q contains the token", err)
	}
}

func TestSendTruncates(t *testing.T) {
	c, got := server(t, http.StatusOK, nil, `{"ok":true,"eventId":"evt_1","delivered":1}`)
	n := Notification{
		Body:    strings.Repeat("a", 9000),
		Title:   strings.Repeat("é", 100),
		Summary: strings.Repeat("b", 600),
		Project: strings.Repeat("p", 80),
	}
	if _, err := c.Send(context.Background(), n, ""); err != nil {
		t.Fatal(err)
	}
	limits := map[string]int{"body": 8000, "title": 80, "summary": 500, "project": 80}
	for field, limit := range limits {
		s, _ := got.body[field].(string)
		if n := utf8.RuneCountInString(s); n != limit {
			t.Errorf("%s has %d runes, want %d", field, n, limit)
		}
	}
	if s := got.body["project"].(string); strings.HasSuffix(s, "…") {
		t.Errorf("project at the limit was truncated: %q", s)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		s                  string
		maxRunes, maxBytes int
		want               string
	}{
		{"hello", 5, 5, "hello"},
		{"hello!", 5, 100, "hell…"},
		{"日本語テキスト", 4, 100, "日本語…"},
		{"日本語テキスト", 100, 10, "日本…"},
		{"ab\xffcd", 4, 100, "ab\xff…"},
	}
	for _, tt := range tests {
		got := truncate(tt.s, tt.maxRunes, tt.maxBytes)
		if got != tt.want {
			t.Errorf("truncate(%q, %d, %d) = %q, want %q", tt.s, tt.maxRunes, tt.maxBytes, got, tt.want)
		}
		if len(got) > tt.maxBytes {
			t.Errorf("truncate(%q, %d, %d) is %d bytes", tt.s, tt.maxRunes, tt.maxBytes, len(got))
		}
	}
}

func TestSendBodyByteLimit(t *testing.T) {
	c, got := server(t, http.StatusOK, nil, `{"ok":true,"eventId":"evt_1","delivered":1}`)
	if _, err := c.Send(context.Background(), Notification{Body: strings.Repeat("😀", 8000)}, ""); err != nil {
		t.Fatal(err)
	}
	if s := got.body["body"].(string); len(s) > 16<<10 {
		t.Errorf("body is %d bytes, want at most %d", len(s), 16<<10)
	}
}
