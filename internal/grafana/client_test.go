package grafana

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGrafana keeps folders and dashboards in memory and checks versions on
// dashboard writes the way Grafana does.
type fakeGrafana struct {
	mu          sync.Mutex
	folders     map[string]Folder
	dashboards  map[string]fakeDashboard
	annotations []map[string]any
	alerts      string
	reloads     int
}

type fakeDashboard struct {
	model     map[string]any
	version   int
	folderUID string
}

func newFake(t *testing.T) (*fakeGrafana, *Client) {
	f := &fakeGrafana{folders: map[string]Folder{}, dashboards: map[string]fakeDashboard{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /grafana/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"database": "ok", "version": "12.0.0"})
	})
	mux.HandleFunc("GET /grafana/api/folders/{uid}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		folder, ok := f.folders[r.PathValue("uid")]
		if !ok {
			http.Error(w, `{"message":"folder not found"}`, http.StatusNotFound)
			return
		}
		writeJSON(w, folder)
	})
	mux.HandleFunc("POST /grafana/api/folders", func(w http.ResponseWriter, r *http.Request) {
		var in Folder
		decode(t, r, &in)
		f.mu.Lock()
		defer f.mu.Unlock()
		if _, ok := f.folders[in.ParentUID]; in.ParentUID != "" && !ok {
			http.Error(w, "parent not found", http.StatusBadRequest)
			return
		}
		if _, ok := f.folders[in.UID]; ok {
			http.Error(w, "exists", http.StatusConflict)
			return
		}
		f.folders[in.UID] = in
		writeJSON(w, in)
	})
	mux.HandleFunc("PUT /grafana/api/folders/{uid}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Title     string `json:"title"`
			Overwrite bool   `json:"overwrite"`
		}
		decode(t, r, &in)
		f.mu.Lock()
		defer f.mu.Unlock()
		folder := f.folders[r.PathValue("uid")]
		folder.Title = in.Title
		f.folders[folder.UID] = folder
		writeJSON(w, folder)
	})
	mux.HandleFunc("POST /grafana/api/folders/{uid}/move", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ParentUID string `json:"parentUid"`
		}
		decode(t, r, &in)
		f.mu.Lock()
		defer f.mu.Unlock()
		folder := f.folders[r.PathValue("uid")]
		folder.ParentUID = in.ParentUID
		f.folders[folder.UID] = folder
		writeJSON(w, folder)
	})
	mux.HandleFunc("GET /grafana/api/dashboards/uid/{uid}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		d, ok := f.dashboards[r.PathValue("uid")]
		if !ok {
			http.Error(w, `{"message":"Dashboard not found"}`, http.StatusNotFound)
			return
		}
		model := map[string]any{}
		for k, v := range d.model {
			model[k] = v
		}
		model["version"] = d.version
		writeJSON(w, map[string]any{"dashboard": model, "meta": map[string]any{"folderUid": d.folderUID}})
	})
	mux.HandleFunc("POST /grafana/api/dashboards/db", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Dashboard map[string]any `json:"dashboard"`
			FolderUID string         `json:"folderUid"`
			Overwrite bool           `json:"overwrite"`
		}
		decode(t, r, &in)
		uid, _ := in.Dashboard["uid"].(string)
		version, _ := in.Dashboard["version"].(float64)
		delete(in.Dashboard, "version")
		f.mu.Lock()
		defer f.mu.Unlock()
		cur, exists := f.dashboards[uid]
		if exists && int(version) != cur.version && !in.Overwrite {
			http.Error(w, `{"message":"Dashboard already exists. Use overwrite flag to update."}`, http.StatusConflict)
			return
		}
		next := fakeDashboard{model: in.Dashboard, version: cur.version + 1, folderUID: in.FolderUID}
		f.dashboards[uid] = next
		writeJSON(w, map[string]any{"uid": uid, "status": "success", "version": next.version})
	})
	mux.HandleFunc("GET /grafana/api/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f.mu.Lock()
		defer f.mu.Unlock()
		hits := []DashboardHit{}
		for uid, d := range f.dashboards {
			title, _ := d.model["title"].(string)
			var tags []string
			for _, tag := range d.model["tags"].([]any) {
				tags = append(tags, tag.(string))
			}
			if q.Get("type") != "dash-db" ||
				!strings.Contains(strings.ToLower(title), strings.ToLower(q.Get("query"))) ||
				(len(q["folderUIDs"]) > 0 && !slices.Contains(q["folderUIDs"], d.folderUID)) {
				continue
			}
			matches := true
			for _, tag := range q["tag"] {
				matches = matches && slices.Contains(tags, tag)
			}
			if matches {
				hits = append(hits, DashboardHit{UID: uid, Title: title, URL: "/grafana/d/" + uid, FolderUID: d.folderUID, Tags: tags})
			}
		}
		slices.SortFunc(hits, func(a, b DashboardHit) int { return strings.Compare(a.UID, b.UID) })
		writeJSON(w, hits)
	})
	mux.HandleFunc("POST /grafana/api/annotations", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		decode(t, r, &in)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.annotations = append(f.annotations, in)
		writeJSON(w, map[string]any{"message": "Annotation added", "id": len(f.annotations)})
	})
	mux.HandleFunc("GET /grafana/api/prometheus/grafana/api/v1/alerts", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(f.alerts))
	})
	mux.HandleFunc("POST /grafana/api/admin/provisioning/alerting/reload", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.reloads++
		writeJSON(w, map[string]string{"message": "Alerting config reloaded"})
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); !ok || user != "shed" || pass != "pw" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return f, New(srv.URL+"/grafana/", "pw", srv.Client())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func decode(t *testing.T, r *http.Request, v any) {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		t.Errorf("%s %s: %v", r.Method, r.URL.Path, err)
	}
}

func TestHealth(t *testing.T) {
	_, c := newFake(t)
	h, err := c.Health(context.Background())
	if err != nil || h.Version != "12.0.0" {
		t.Fatalf("Health = %+v, %v", h, err)
	}
}

func TestWrongPassword(t *testing.T) {
	_, c := newFake(t)
	c.password = "wrong"
	if _, err := c.Health(context.Background()); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("Health with a wrong password = %v, want 401", err)
	}
}

func TestEnsureFolder(t *testing.T) {
	f, c := newFake(t)
	ctx := context.Background()

	if _, err := c.Folder(ctx, "p-abc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Folder of a missing folder = %v, want ErrNotFound", err)
	}
	steps := []Folder{
		{UID: ArchivedFolder, Title: "Archived"},
		{UID: ProjectFolder("abc"), Title: "shop"},
		{UID: ServiceFolder("def"), Title: "api", ParentUID: ProjectFolder("abc")},
		// Unchanged.
		{UID: ServiceFolder("def"), Title: "api", ParentUID: ProjectFolder("abc")},
		// Renamed.
		{UID: ServiceFolder("def"), Title: "backend", ParentUID: ProjectFolder("abc")},
		// Archived: moved and renamed.
		{UID: ServiceFolder("def"), Title: "shop / backend (deleted 2026-10-26)", ParentUID: ArchivedFolder},
	}
	for _, s := range steps {
		if err := c.EnsureFolder(ctx, s); err != nil {
			t.Fatalf("EnsureFolder(%+v): %v", s, err)
		}
		got, err := c.Folder(ctx, s.UID)
		if err != nil || got != s {
			t.Fatalf("after EnsureFolder(%+v): %+v, %v", s, got, err)
		}
	}
	if len(f.folders) != 3 {
		t.Errorf("%d folders, want 3", len(f.folders))
	}
	if err := c.EnsureFolder(ctx, Folder{UID: "s-x", Title: "x", ParentUID: "p-missing"}); err == nil {
		t.Error("EnsureFolder under a missing parent succeeded")
	}
}

func TestDashboardVersions(t *testing.T) {
	_, c := newFake(t)
	ctx := context.Background()
	d := ServiceDashboards("svc123456789", "app")[0]

	if _, err := c.Dashboard(ctx, d.UID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Dashboard before creating = %v, want ErrNotFound", err)
	}
	v1, err := c.PutDashboard(ctx, "s-svc123456789", d.JSON, 0)
	if err != nil || v1 != 1 {
		t.Fatalf("create = %d, %v", v1, err)
	}
	if _, err := c.PutDashboard(ctx, "s-svc123456789", d.JSON, 0); !errors.Is(err, ErrConflict) {
		t.Fatalf("creating again = %v, want ErrConflict", err)
	}
	v2, err := c.PutDashboard(ctx, "s-svc123456789", d.JSON, v1)
	if err != nil || v2 != 2 {
		t.Fatalf("overwrite version 1 = %d, %v", v2, err)
	}
	if _, err := c.PutDashboard(ctx, "s-svc123456789", d.JSON, v1); !errors.Is(err, ErrConflict) {
		t.Fatalf("overwrite with a stale version = %v, want ErrConflict", err)
	}
	got, err := c.Dashboard(ctx, d.UID)
	want := DashboardMeta{UID: d.UID, Title: "Overview", Version: 2, FolderUID: "s-svc123456789"}
	if err != nil || got != want {
		t.Fatalf("Dashboard = %+v, %v, want %+v", got, err, want)
	}
}

func TestSearchDashboards(t *testing.T) {
	_, c := newFake(t)
	ctx := context.Background()
	for _, d := range ServiceDashboards("aaa", "redis") {
		if _, err := c.PutDashboard(ctx, "s-aaa", d.JSON, 0); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range ServiceDashboards("bbb", "app") {
		if _, err := c.PutDashboard(ctx, "s-bbb", d.JSON, 0); err != nil {
			t.Fatal(err)
		}
	}
	uids := func(hits []DashboardHit) []string {
		var out []string
		for _, h := range hits {
			out = append(out, h.UID)
		}
		return out
	}
	hits, err := c.SearchDashboards(ctx, SearchQuery{FolderUIDs: []string{"s-aaa"}})
	if want := []string{"svc-logs-aaa", "svc-overview-aaa", "svc-redis-aaa"}; err != nil || !slices.Equal(uids(hits), want) {
		t.Errorf("search folder s-aaa = %v, %v, want %v", uids(hits), err, want)
	}
	hits, err = c.SearchDashboards(ctx, SearchQuery{Query: "http", Tags: []string{"service:bbb"}})
	if want := []string{"svc-http-bbb"}; err != nil || !slices.Equal(uids(hits), want) {
		t.Errorf("search http in service bbb = %v, %v, want %v", uids(hits), err, want)
	}
}

func TestCreateAnnotation(t *testing.T) {
	f, c := newFake(t)
	start := time.UnixMilli(1_700_000_000_000)
	id, err := c.CreateAnnotation(context.Background(), Annotation{
		Time: start, End: start.Add(time.Minute), Tags: []string{"shed", "deploy.active", "service:abc"}, Text: "deployed 1a2b3c",
	})
	if err != nil || id != 1 {
		t.Fatalf("CreateAnnotation = %d, %v", id, err)
	}
	got := f.annotations[0]
	if got["time"] != float64(1_700_000_000_000) || got["timeEnd"] != float64(1_700_000_060_000) ||
		got["text"] != "deployed 1a2b3c" || len(got["tags"].([]any)) != 3 {
		t.Errorf("annotation sent = %v", got)
	}
	if _, err := c.CreateAnnotation(context.Background(), Annotation{Time: start, Tags: []string{"shed"}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.annotations[1]["timeEnd"]; ok {
		t.Error("point annotation sent timeEnd")
	}
}

func TestFiringAlerts(t *testing.T) {
	f, c := newFake(t)
	f.alerts = `{"status":"success","data":{"alerts":[
		{"labels":{"alertname":"Service restarting","shed_service_id":"abc"},"annotations":{"summary":"api restarted"},"state":"Alerting","activeAt":"2026-10-26T10:00:00Z","value":"4"},
		{"labels":{"alertname":"Host disk almost full"},"annotations":{},"state":"Pending","activeAt":"2026-10-26T10:01:00Z","value":"0.9"},
		{"labels":{"alertname":"Deploy failed"},"annotations":{},"state":"Alerting (Error)","activeAt":"2026-10-26T10:02:00Z","value":""},
		{"labels":{"alertname":"Backup failed"},"annotations":{},"state":"Normal","activeAt":null,"value":""}
	]}}`
	alerts, err := c.FiringAlerts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range alerts {
		names = append(names, a.Labels["alertname"])
	}
	if want := []string{"Service restarting", "Deploy failed"}; !slices.Equal(names, want) {
		t.Fatalf("firing = %v, want %v", names, want)
	}
	if a := alerts[0]; a.Annotations["summary"] != "api restarted" || a.Value != "4" ||
		!a.ActiveAt.Equal(time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("alert = %+v", a)
	}
}

func TestReloadAlertRules(t *testing.T) {
	f, c := newFake(t)
	if err := c.ReloadAlertRules(context.Background()); err != nil || f.reloads != 1 {
		t.Fatalf("ReloadAlertRules = %v, %d reloads", err, f.reloads)
	}
}
