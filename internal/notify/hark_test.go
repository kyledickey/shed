package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

const harkToken = "tok_s3cretS3cret"

type harkCaptured struct {
	method, path, contentType, key string
	body                           map[string]any
}

// harkServer returns a sender whose requests go to a test server that
// records each request and replies with status, header, and body.
func harkServer(t *testing.T, status int, header http.Header, body string) (harkSender, *harkCaptured) {
	t.Helper()
	got := &harkCaptured{}
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
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(ts.Close)
	s, err := openHark(ts.URL+"/hooks/"+harkToken, ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	return s, got
}

func TestOpenHark(t *testing.T) {
	tests := []struct {
		url string
		ok  bool
	}{
		{"https://hark.ryan.ceo/hooks/" + harkToken, true},
		{"http://127.0.0.1:8080/hooks/" + harkToken, true},
		{"http://localhost/hooks/" + harkToken, true},
		{"http://[::1]/hooks/" + harkToken, true},
		{"http://hark.ryan.ceo/hooks/" + harkToken, false},
		{"ftp://hark.ryan.ceo/hooks/" + harkToken, false},
		{"https:///hooks/" + harkToken, false},
		{"https://hark.ryan.ceo/hooks/", false},
		{"https://hark.ryan.ceo/hooks/" + harkToken + "/events", false},
		{"https://hark.ryan.ceo/other/" + harkToken, false},
		{"https://hark.ryan.ceo/hooks/" + harkToken + "?a=b", false},
		{"https://hark.ryan.ceo/hooks/" + harkToken + "#frag", false},
		{"https://u:p@hark.ryan.ceo/hooks/" + harkToken, false},
		{"://" + harkToken, false},
	}
	for _, tt := range tests {
		_, err := openHark(tt.url, nil)
		if (err == nil) != tt.ok {
			t.Errorf("openHark(%q) error = %v, want ok %v", tt.url, err, tt.ok)
		}
		if err != nil && strings.Contains(err.Error(), harkToken) {
			t.Errorf("openHark(%q) error %q contains the token", tt.url, err)
		}
	}
}

func TestValidateHarkTargetNotLeaked(t *testing.T) {
	n := New(&fakeStore{}, nil, Hark(nil))
	target := "http://hark.example/hooks/tok_s3cret"
	err := n.Validate(Channel{Name: "phone", Provider: "hark", Target: target})
	if err == nil {
		t.Fatal("Validate accepted plain http to a remote host")
	}
	if strings.Contains(err.Error(), "tok_s3cret") {
		t.Errorf("error %q leaks the token", err)
	}
}

func TestHarkSendRequest(t *testing.T) {
	s, got := harkServer(t, http.StatusOK, nil, `{"ok":true,"eventId":"evt_1","delivered":2}`)
	err := s.Send(context.Background(), Message{
		Key:     "deploy:1:failed",
		Title:   "shed",
		Body:    "  Deploy failed.  ",
		URL:     "https://shed.example/deployments/1",
		Project: "acme",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodPost || got.path != "/hooks/"+harkToken {
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
		"url":     "https://shed.example/deployments/1",
		"project": "acme",
	}
	for k, v := range want {
		if got.body[k] != v {
			t.Errorf("body[%q] = %v, want %v", k, got.body[k], v)
		}
	}
}

func TestHarkSendOmitsEmptyFields(t *testing.T) {
	s, got := harkServer(t, http.StatusOK, nil, `{"ok":true,"eventId":"evt_1","delivered":1}`)
	if err := s.Send(context.Background(), Message{Body: "hi"}); err != nil {
		t.Fatal(err)
	}
	if len(got.body) != 1 || got.body["body"] != "hi" {
		t.Errorf("body = %v, want only body", got.body)
	}
	if got.key != "" {
		t.Errorf("Idempotency-Key = %q, want none", got.key)
	}
}

func TestHarkSendRejectsLongKey(t *testing.T) {
	s, got := harkServer(t, http.StatusOK, nil, `{}`)
	err := s.Send(context.Background(), Message{Body: "hi", Key: strings.Repeat("k", harkMaxKey+1)})
	if err == nil {
		t.Fatal("Send succeeded, want error")
	}
	if got.method != "" {
		t.Error("Send made a request")
	}
}

func TestHarkSendStatus(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		header  http.Header
		body    string
		wantIs  error
		wantErr string
		perm    bool // errors.Is(err, ErrPermanent)
	}{
		{name: "delivered", status: http.StatusOK, body: `{"ok":true,"eventId":"evt_1","delivered":2}`},
		{name: "no devices", status: http.StatusOK, body: `{"ok":true,"eventId":"evt_1","delivered":0}`, wantIs: ErrNotDelivered, perm: true},
		{name: "processing", status: http.StatusAccepted, body: `{"ok":true}`},
		{name: "invalid payload", status: http.StatusBadRequest, body: `{"error":"body too long"}`, wantErr: "body too long", perm: true},
		{name: "pro feature", status: http.StatusPaymentRequired, body: `{"message":"Pro required"}`, wantErr: "Pro required", perm: true},
		{name: "unknown token", status: http.StatusNotFound, body: `{"error":"not found"}`, wantIs: errHarkUnknownToken, perm: true},
		{name: "key reused", status: http.StatusConflict, body: `{"error":"idempotency key reused"}`, wantErr: "idempotency key reused", perm: true},
		{
			name: "rate limit header", status: http.StatusTooManyRequests,
			header: http.Header{"Retry-After": {"60"}}, body: `{"retryAfterSeconds":30}`,
			wantErr: "rate limited, retry after 60s",
		},
		{
			name: "rate limit body", status: http.StatusTooManyRequests,
			body: `{"retryAfterSeconds":30}`, wantErr: "rate limited, retry after 30s",
		},
		{
			name: "monthly allowance", status: http.StatusTooManyRequests,
			body: `{"error":"monthly limit"}`, wantErr: "monthly allowance exhausted",
		},
		{name: "upstream failure", status: http.StatusBadGateway, body: "upstream failure", wantErr: "upstream failure"},
		{
			name: "error echoes token", status: http.StatusBadRequest,
			body: `{"error":"bad token ` + harkToken + `"}`, wantErr: "bad token <token>", perm: true,
		},
		{name: "malformed success", status: http.StatusOK, body: "not json", wantErr: "decode response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := harkServer(t, tt.status, tt.header, tt.body)
			err := s.Send(context.Background(), Message{Body: "hi"})
			if err != nil && strings.Contains(err.Error(), harkToken) {
				t.Errorf("error %q contains the token", err)
			}
			if got := errors.Is(err, ErrPermanent); got != tt.perm {
				t.Errorf("permanent = %v, want %v (err %v)", got, tt.perm, err)
			}
			switch {
			case tt.wantIs != nil:
				if !errors.Is(err, tt.wantIs) {
					t.Fatalf("err = %v, want %v", err, tt.wantIs)
				}
			case tt.wantErr != "":
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
			case err != nil:
				t.Fatalf("err = %v, want nil", err)
			}
		})
	}
}

func TestHarkSendTransportErrorHidesToken(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	addr := ts.URL
	ts.Close()
	s, err := openHark(addr+"/hooks/"+harkToken, &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	err = s.Send(context.Background(), Message{Body: "hi"})
	if err == nil {
		t.Fatal("Send succeeded against a closed server")
	}
	if strings.Contains(err.Error(), harkToken) {
		t.Errorf("error %q contains the token", err)
	}
}

func TestHarkSendCanceled(t *testing.T) {
	s, _ := harkServer(t, http.StatusOK, nil, `{}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := s.Send(ctx, Message{Body: "hi"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if strings.Contains(err.Error(), harkToken) {
		t.Errorf("error %q contains the token", err)
	}
}

func TestHarkSendTruncates(t *testing.T) {
	s, got := harkServer(t, http.StatusOK, nil, `{"ok":true,"eventId":"evt_1","delivered":1}`)
	err := s.Send(context.Background(), Message{
		Body:    strings.Repeat("a", 9000),
		Title:   strings.Repeat("é", 100),
		Project: strings.Repeat("p", 80),
	})
	if err != nil {
		t.Fatal(err)
	}
	limits := map[string]int{"body": harkMaxBody, "title": harkMaxTitle, "project": harkMaxProject}
	for field, limit := range limits {
		str, _ := got.body[field].(string)
		if n := utf8.RuneCountInString(str); n != limit {
			t.Errorf("%s has %d runes, want %d", field, n, limit)
		}
	}
	if str := got.body["project"].(string); strings.HasSuffix(str, "…") {
		t.Errorf("project at the limit was truncated: %q", str)
	}
}

func TestHarkSendBodyByteLimit(t *testing.T) {
	s, got := harkServer(t, http.StatusOK, nil, `{"ok":true,"eventId":"evt_1","delivered":1}`)
	if err := s.Send(context.Background(), Message{Body: strings.Repeat("😀", 8000)}); err != nil {
		t.Fatal(err)
	}
	if str := got.body["body"].(string); len(str) > harkMaxBodyBytes {
		t.Errorf("body is %d bytes, want at most %d", len(str), harkMaxBodyBytes)
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

func TestHark(t *testing.T) {
	var gotKey string
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		gotKey = r.Header.Get("Idempotency-Key")
		_, _ = io.WriteString(w, `{"ok":true,"eventId":"evt_1","delivered":1}`)
	}))
	defer ts.Close()

	s, err := Hark(ts.Client()).Open(ts.URL + "/hooks/tok_1")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	err = s.Send(context.Background(), Message{
		Key: "deploy:1:failed", Title: "shed", Body: "Deploy failed",
		URL: "https://shed.example/p/1", Project: "web",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotKey != "deploy:1:failed" || gotBody["body"] != "Deploy failed" {
		t.Errorf("key = %q, body = %v", gotKey, gotBody)
	}
}

func TestHarkRateLimitReachable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()
	n := New(&fakeStore{}, nil, Hark(ts.Client()))

	err := n.Test(context.Background(), Channel{Name: "phone", Provider: "hark", Target: ts.URL + "/hooks/tok_1"})
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("Test error = %v, want it to contain %q", err, "rate limited")
	}
	if strings.Contains(err.Error(), "tok_1") {
		t.Errorf("error %q leaks the token", err)
	}
}
