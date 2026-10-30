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
	if _, err := c.Run(context.Background(), RunSpec{Name: "test", Image: "app", CPUs: 1.5, Memory: 512 << 20}); err != nil {
		t.Fatal(err)
	}
	h := config.HostConfig
	if h.Memory != 512<<20 || h.MemorySwap != h.Memory || h.NanoCPUs != 1_500_000_000 || h.PidsLimit == nil || *h.PidsLimit != 512 {
		t.Fatalf("unbounded resources: %+v", h.Resources)
	}
	if h.LogConfig.Type != "json-file" || h.LogConfig.Config["max-size"] != "10m" || h.LogConfig.Config["max-file"] != "3" {
		t.Fatalf("unbounded logs: %+v", h.LogConfig)
	}
}

func TestWorkloadResourcesUnlimited(t *testing.T) {
	r := workloadResources(RunSpec{})
	if r.Memory != 0 || r.MemorySwap != 0 || r.NanoCPUs != 0 || r.PidsLimit == nil || *r.PidsLimit != 512 {
		t.Fatalf("workloadResources(no limits) = %+v", r)
	}
}

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCreateHostNetworkWithBinds(t *testing.T) {
	var body struct {
		HostConfig       container.HostConfig
		NetworkingConfig *struct{ EndpointsConfig map[string]any }
	}
	transport := testTransport(func(r *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		w := httptest.NewRecorder()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"Id":"test-container"}`))
		return w.Result(), nil
	})
	api, err := client.New(client.WithHost("http://docker.invalid"), client.WithAPIVersion("1.54"), client.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	c := &Client{api: api}
	_, err = c.Create(context.Background(), RunSpec{
		Name: "test", Image: "app", Network: HostNetwork,
		Mounts: []Mount{{Volume: "data", Target: "/data"}},
		Binds:  []Bind{{Source: "/srv/conf", Target: "/etc/conf", ReadOnly: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := body.HostConfig
	if h.NetworkMode != "host" {
		t.Errorf("network mode %q, want host", h.NetworkMode)
	}
	if body.NetworkingConfig != nil && len(body.NetworkingConfig.EndpointsConfig) > 0 {
		t.Errorf("endpoints %v on the host network", body.NetworkingConfig.EndpointsConfig)
	}
	if len(h.Mounts) != 2 || h.Mounts[0].Type != "volume" || h.Mounts[1].Type != "bind" ||
		h.Mounts[1].Source != "/srv/conf" || h.Mounts[1].Target != "/etc/conf" || !h.Mounts[1].ReadOnly {
		t.Errorf("mounts %+v, want the volume and a read-only bind", h.Mounts)
	}
}
