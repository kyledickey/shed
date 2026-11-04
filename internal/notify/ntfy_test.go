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
)

const ntfyToken = "tk_s3cretS3cret"

type ntfyCaptured struct {
	method, path, contentType string
	user, pass                string
	auth                      bool
	body                      map[string]any
}

// ntfyServer returns a sender for target (with "<server>" replaced by the
// test server's address) whose requests are recorded and answered with
// status and body.
func ntfyServer(t *testing.T, target string, status int, body string) (ntfySender, *ntfyCaptured) {
	t.Helper()
	got := &ntfyCaptured{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		got.method, got.path = r.Method, r.URL.Path
		got.contentType = r.Header.Get("Content-Type")
		got.user, got.pass, got.auth = r.BasicAuth()
		got.body = nil
		_ = json.Unmarshal(raw, &got.body)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(ts.Close)
	host := strings.TrimPrefix(ts.URL, "http://")
	s, err := openNtfy(strings.ReplaceAll(target, "<server>", host), ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	return s, got
}

func TestOpenNtfy(t *testing.T) {
	tests := []struct {
		url string
		ok  bool
	}{
		{"https://ntfy.sh/mytopic", true},
		{"https://:" + ntfyToken + "@ntfy.example.com/alerts", true},
		{"https://user:" + ntfyToken + "@ntfy.example.com/alerts", true},
		{"https://ntfy.example.com/base/path/my_topic-1", true},
		{"http://127.0.0.1:8080/alerts", true},
		{"http://ntfy.sh/mytopic", false},
		{"ftp://ntfy.sh/mytopic", false},
		{"https:///mytopic", false},
		{"https://ntfy.sh", false},
		{"https://ntfy.sh/", false},
		{"https://ntfy.sh/mytopic/", false},
		{"https://ntfy.sh/my.topic", false},
		{"https://ntfy.sh/" + strings.Repeat("t", 65), false},
		{"https://:" + ntfyToken + "@ntfy.sh/mytopic?a=b", false},
		{"https://:" + ntfyToken + "@ntfy.sh/mytopic#frag", false},
		{"://" + ntfyToken, false},
	}
	for _, tt := range tests {
		_, err := openNtfy(tt.url, nil)
		if (err == nil) != tt.ok {
			t.Errorf("openNtfy(%q) error = %v, want ok %v", tt.url, err, tt.ok)
		}
		if err != nil && strings.Contains(err.Error(), ntfyToken) {
			t.Errorf("openNtfy(%q) error %q contains the token", tt.url, err)
		}
	}
}

func TestNtfySendRequest(t *testing.T) {
	s, got := ntfyServer(t, "http://:"+ntfyToken+"@<server>/base/alerts", http.StatusOK, `{"id":"x"}`)
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
	if got.method != http.MethodPost || got.path != "/base/" {
		t.Errorf("request = %s %s, want POST /base/", got.method, got.path)
	}
	if got.contentType != "application/json" {
		t.Errorf("Content-Type = %q", got.contentType)
	}
	if !got.auth || got.user != "" || got.pass != ntfyToken {
		t.Errorf("basic auth = %v %q %q", got.auth, got.user, got.pass)
	}
	want := map[string]any{
		"topic":   "alerts",
		"message": "Deploy failed.",
		"title":   "shed",
		"click":   "https://shed.example/deployments/1",
		"tags":    []any{"acme"},
	}
	for k, v := range want {
		gb, _ := json.Marshal(got.body[k])
		wb, _ := json.Marshal(v)
		if string(gb) != string(wb) {
			t.Errorf("body[%q] = %s, want %s", k, gb, wb)
		}
	}
}

func TestNtfySendMinimal(t *testing.T) {
	s, got := ntfyServer(t, "http://<server>/alerts", http.StatusOK, `{}`)
	if err := s.Send(context.Background(), Message{Body: strings.Repeat("a", 5000)}); err != nil {
		t.Fatal(err)
	}
	if got.auth {
		t.Error("request has basic auth, want none")
	}
	if len(got.body) != 2 || got.body["topic"] != "alerts" {
		t.Errorf("body = %v, want only topic and message", got.body)
	}
	if msg, _ := got.body["message"].(string); len(msg) > ntfyMaxMessage {
		t.Errorf("message is %d bytes, want at most %d", len(msg), ntfyMaxMessage)
	}
}

func TestNtfySendStatus(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
		perm    bool
	}{
		{name: "ok", status: http.StatusOK, body: `{"id":"x"}`},
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{"code":40101,"error":"unauthorized"}`, wantErr: "unauthorized", perm: true},
		{name: "forbidden", status: http.StatusForbidden, body: `{"error":"forbidden"}`, wantErr: "403", perm: true},
		{name: "rate limited", status: http.StatusTooManyRequests, body: `{"error":"limit reached"}`, wantErr: "limit reached"},
		{name: "server error", status: http.StatusBadGateway, body: "bad gateway", wantErr: "502"},
		{
			name: "error echoes token", status: http.StatusBadRequest,
			body: `{"error":"bad token ` + ntfyToken + `"}`, wantErr: "bad token <password>", perm: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := ntfyServer(t, "http://:"+ntfyToken+"@<server>/alerts", tt.status, tt.body)
			err := s.Send(context.Background(), Message{Body: "hi"})
			if err != nil && strings.Contains(err.Error(), ntfyToken) {
				t.Errorf("error %q contains the token", err)
			}
			if got := errors.Is(err, ErrPermanent); got != tt.perm {
				t.Errorf("permanent = %v, want %v (err %v)", got, tt.perm, err)
			}
			switch {
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

func TestNtfySendTransportErrorHidesToken(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	host := strings.TrimPrefix(ts.URL, "http://")
	ts.Close()
	s, err := Ntfy(&http.Client{}).Open("http://:" + ntfyToken + "@" + host + "/alerts")
	if err != nil {
		t.Fatal(err)
	}
	err = s.Send(context.Background(), Message{Body: "hi"})
	if err == nil {
		t.Fatal("Send succeeded against a closed server")
	}
	if strings.Contains(err.Error(), ntfyToken) {
		t.Errorf("error %q contains the token", err)
	}
}
