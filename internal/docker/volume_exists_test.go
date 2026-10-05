package docker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moby/moby/client"
)

func TestVolumeExists(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    bool
		wantErr bool
	}{
		{"exists", 200, `{"Name":"shed-vol-v1","Driver":"local"}`, true, false},
		{"missing", 404, `{"message":"get shed-vol-v1: no such volume"}`, false, false},
		{"daemon error", 500, `{"message":"boom"}`, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := testTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/volumes/shed-vol-v1") {
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				}
				w := httptest.NewRecorder()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
				return w.Result(), nil
			})
			api, err := client.New(client.WithHost("http://docker.invalid"), client.WithAPIVersion("1.54"), client.WithHTTPClient(&http.Client{Transport: transport}))
			if err != nil {
				t.Fatal(err)
			}
			defer api.Close()
			c := &Client{api: api}
			got, err := c.VolumeExists(context.Background(), "shed-vol-v1")
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want error %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("exists = %v, want %v", got, tt.want)
			}
			if err != nil && IsNotFound(err) {
				t.Fatalf("daemon error reported as not found: %v", err)
			}
		})
	}
}
