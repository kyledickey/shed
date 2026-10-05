package api

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/update"
)

// errFake is an unexpected failure.
var errFake = errors.New("fake failure")

// fakeUpdates returns st, or err from every method that changes state.
type fakeUpdates struct {
	st   update.Status
	err  error
	auto []bool
}

func (f *fakeUpdates) Status(context.Context) (update.Status, error) { return f.st, nil }

func (f *fakeUpdates) result() (update.Status, error) {
	if f.err != nil {
		return update.Status{}, f.err
	}
	return f.st, nil
}

func (f *fakeUpdates) Check(context.Context) (update.Status, error)    { return f.result() }
func (f *fakeUpdates) Download(context.Context) (update.Status, error) { return f.result() }
func (f *fakeUpdates) Install(context.Context) (update.Status, error)  { return f.result() }

func (f *fakeUpdates) SetAutoDownload(_ context.Context, on bool) (update.Status, error) {
	f.auto = append(f.auto, on)
	f.st.AutoDownload = on
	return f.result()
}

func TestGetUpdate(t *testing.T) {
	f := newFixture(t)
	published := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	f.updates.st = update.Status{
		Current:   "v1.0.0",
		Latest:    &update.Release{Version: "v1.1.0", URL: "https://example.com/r", Notes: "n", PublishedAt: published},
		Available: true,
		State:     update.Idle,
		Staged:    "v1.1.0",
	}
	got := f.decode(f.do("GET", "/api/update", ""), http.StatusOK)
	if got["current"] != "v1.0.0" || got["available"] != true || got["staged"] != "v1.1.0" ||
		got["state"] != "idle" || got["checkedAt"] != nil || got["unsupported"] != "" {
		t.Errorf("GET /api/update = %v", got)
	}
	latest, _ := got["latest"].(map[string]any)
	if latest["version"] != "v1.1.0" || latest["publishedAt"] != "2026-10-01T12:00:00Z" {
		t.Errorf("latest = %v", latest)
	}

	f.updates.st = update.Status{Current: "dev", State: update.Idle}
	if got := f.decode(f.do("GET", "/api/update", ""), http.StatusOK); got["latest"] != nil {
		t.Errorf("latest = %v, want null", got["latest"])
	}
}

func TestUpdateActions(t *testing.T) {
	tests := []struct {
		name, method, path, body string
		err                      error
		want                     int
		restarts                 int
	}{
		{name: "check", method: "POST", path: "/api/update/check", want: http.StatusOK},
		{name: "failed check is reported in the status", method: "POST", path: "/api/update/check", err: errFake, want: http.StatusOK},
		{name: "check while busy", method: "POST", path: "/api/update/check", err: update.ErrBusy, want: http.StatusConflict},
		{name: "check a development build", method: "POST", path: "/api/update/check", err: update.ErrUnsupported, want: http.StatusConflict},
		{name: "download", method: "POST", path: "/api/update/download", want: http.StatusAccepted},
		{name: "download without a newer release", method: "POST", path: "/api/update/download", err: update.ErrNoUpdate, want: http.StatusConflict},
		{name: "install", method: "POST", path: "/api/update/install", want: http.StatusAccepted, restarts: 1},
		{name: "install before download", method: "POST", path: "/api/update/install", err: update.ErrNotStaged, want: http.StatusConflict},
		{name: "install fails", method: "POST", path: "/api/update/install", err: errFake, want: http.StatusInternalServerError},
		{name: "settings", method: "PUT", path: "/api/update/settings", body: `{"autoDownload":true}`, want: http.StatusOK},
		{name: "settings without autoDownload", method: "PUT", path: "/api/update/settings", body: `{}`, want: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.updates.st = update.Status{Current: "v1.0.0", State: update.Idle}
			f.updates.err = tt.err
			f.decode(f.do(tt.method, tt.path, tt.body), tt.want)
			if f.restarts != tt.restarts {
				t.Errorf("restarts = %d, want %d", f.restarts, tt.restarts)
			}
		})
	}
}
