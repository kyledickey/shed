package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

func TestRunEnforcesLimits(t *testing.T) {
	var config struct{ HostConfig container.HostConfig }
	transport := testTransport(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/create"):
			if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
				t.Error(err)
			}
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"Id":"test-container"}`))
		case strings.HasSuffix(r.URL.Path, "/start"):
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
			w.WriteHeader(500)
		}

		return w.Result(), nil
	})
	api, err := client.New(client.WithHost("http://docker.invalid"), client.WithAPIVersion("1.54"), client.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	c := &Client{api: api}
	if _, err := c.Run(context.Background(), RunSpec{Name: "test", Image: "app"}); err != nil {
		t.Fatal(err)
	}
	h := config.HostConfig
	if h.Memory != 1<<30 || h.MemorySwap != h.Memory || h.NanoCPUs != 1_000_000_000 || h.PidsLimit == nil || *h.PidsLimit != 512 {
		t.Fatalf("unbounded resources: %+v", h.Resources)
	}
	if h.LogConfig.Type != "json-file" || h.LogConfig.Config["max-size"] != "10m" || h.LogConfig.Config["max-file"] != "3" {
		t.Fatalf("unbounded logs: %+v", h.LogConfig)
	}
}

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
