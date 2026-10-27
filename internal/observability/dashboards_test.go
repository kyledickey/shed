package observability

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/grafana"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/telemetry"
)

// grafanaDashboard is a dashboard stored in fakeDashboardAPI.
type grafanaDashboard struct {
	folder  string
	version int
	json    string
}

// fakeDashboardAPI keeps folders, dashboards, and annotations in memory,
// with Grafana's version rules.
type fakeDashboardAPI struct {
	mu          sync.Mutex
	folders     map[string]grafana.Folder
	dashboards  map[string]*grafanaDashboard
	annotations []grafana.Annotation
	alerts      []grafana.Alert
}

func newFakeDashboardAPI() *fakeDashboardAPI {
	return &fakeDashboardAPI{folders: map[string]grafana.Folder{}, dashboards: map[string]*grafanaDashboard{}}
}

func (g *fakeDashboardAPI) Folder(_ context.Context, uid string) (grafana.Folder, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	f, ok := g.folders[uid]
	if !ok {
		return grafana.Folder{}, grafana.ErrNotFound
	}
	return f, nil
}

func (g *fakeDashboardAPI) EnsureFolder(_ context.Context, f grafana.Folder) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.folders[f.ParentUID]; f.ParentUID != "" && !ok {
		return errors.New("parent folder not found")
	}
	g.folders[f.UID] = f
	return nil
}

func (g *fakeDashboardAPI) Dashboard(_ context.Context, uid string) (grafana.DashboardMeta, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	d, ok := g.dashboards[uid]
	if !ok {
		return grafana.DashboardMeta{}, grafana.ErrNotFound
	}
	return grafana.DashboardMeta{UID: uid, Version: d.version, FolderUID: d.folder}, nil
}

func (g *fakeDashboardAPI) PutDashboard(_ context.Context, folder string, js json.RawMessage, replaces int) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.folders[folder]; !ok {
		return 0, errors.New("folder not found")
	}
	var m struct {
		UID string `json:"uid"`
	}
	if err := json.Unmarshal(js, &m); err != nil {
		return 0, err
	}
	cur, ok := g.dashboards[m.UID]
	switch {
	case !ok && replaces == 0:
		g.dashboards[m.UID] = &grafanaDashboard{folder: folder, version: 1, json: string(js)}
		return 1, nil
	case !ok || cur.version != replaces:
		return 0, grafana.ErrConflict
	case cur.json == string(js) && cur.folder == folder:
		return cur.version, nil
	}
	cur.folder, cur.json = folder, string(js)
	cur.version++
	return cur.version, nil
}

func (g *fakeDashboardAPI) CreateAnnotation(_ context.Context, a grafana.Annotation) (int64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.annotations = append(g.annotations, a)
	return int64(len(g.annotations)), nil
}

func (g *fakeDashboardAPI) FiringAlerts(context.Context) ([]grafana.Alert, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.alerts, nil
}

// userSave saves dashboard uid as a user would in Grafana.
func (g *fakeDashboardAPI) userSave(t *testing.T, uid string) {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	d, ok := g.dashboards[uid]
	if !ok {
		t.Fatalf("no dashboard %s to save", uid)
	}
	d.json = `{"uid":"` + uid + `","title":"mine"}`
	d.version++
}

func (g *fakeDashboardAPI) userDelete(uid string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.dashboards, uid)
}

func (g *fakeDashboardAPI) dashboard(uid string) *grafanaDashboard {
	g.mu.Lock()
	defer g.mu.Unlock()
	if d, ok := g.dashboards[uid]; ok {
		c := *d
		return &c
	}
	return nil
}

func (g *fakeDashboardAPI) folder(uid string) (grafana.Folder, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	f, ok := g.folders[uid]
	return f, ok
}

func (g *fakeDashboardAPI) takeAnnotations() []grafana.Annotation {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := g.annotations
	g.annotations = nil
	return out
}

type dashboardsHarness struct {
	st      *store.Store
	api     *fakeDashboardAPI
	d       *Dashboards
	project store.Project
	app     store.Service
	db      store.Service
}

func newDashboardsHarness(t *testing.T) *dashboardsHarness {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "shed.db"), make([]byte, store.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	h := &dashboardsHarness{st: st, api: newFakeDashboardAPI()}
	if h.project, err = st.CreateProject(ctx, "demo"); err != nil {
		t.Fatal(err)
	}
	if h.app, err = st.CreateService(ctx, store.Service{ProjectID: h.project.ID, Name: "web", Kind: "app", Image: "nginx"}); err != nil {
		t.Fatal(err)
	}
	if h.db, err = st.CreateService(ctx, store.Service{ProjectID: h.project.ID, Name: "db", Kind: "postgres", Image: "postgres:17"}); err != nil {
		t.Fatal(err)
	}
	h.d = NewDashboards(DashboardsConfig{
		Store:      st,
		Grafana:    func(context.Context) (DashboardAPI, error) { return h.api, nil },
		GrafanaURL: "https://shed.example.com/grafana/",
		Log:        slog.New(slog.DiscardHandler),
	})
	h.d.now = func() time.Time { return time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC) }
	return h
}

// reconcile runs one pass as the worker would.
func (h *dashboardsHarness) reconcile(t *testing.T) {
	t.Helper()
	if err := h.d.reconcile(context.Background(), h.api, false); err != nil {
		t.Fatal(err)
	}
}

// start runs the worker until the test ends and waits for its first pass.
func (h *dashboardsHarness) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.d.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	waitFor(t, func() bool {
		_, err := h.d.current()
		return err == nil && h.api.dashboard("svc-overview-"+h.app.ID) != nil
	})
}

func (h *dashboardsHarness) record(t *testing.T, uid string) (store.GrafanaDashboard, bool) {
	t.Helper()
	rows, err := h.st.GrafanaDashboards(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(rows, func(r store.GrafanaDashboard) bool { return r.UID == uid })
	if i < 0 {
		return store.GrafanaDashboard{}, false
	}
	return rows[i], true
}

// template returns the current default of dashboard uid.
func template(t *testing.T, uid string, ds []grafana.Dashboard) grafana.Dashboard {
	t.Helper()
	i := slices.IndexFunc(ds, func(d grafana.Dashboard) bool { return d.UID == uid })
	if i < 0 {
		t.Fatalf("no template renders %s", uid)
	}
	return ds[i]
}

func TestDashboardsCreate(t *testing.T) {
	h := newDashboardsHarness(t)
	h.reconcile(t)

	wantFolders := []grafana.Folder{
		{UID: grafana.ShedFolder, Title: "shed"},
		{UID: grafana.ArchivedFolder, Title: "Archived"},
		{UID: grafana.ProjectFolder(h.project.ID), Title: "demo"},
		{UID: grafana.ServiceFolder(h.app.ID), Title: "web", ParentUID: grafana.ProjectFolder(h.project.ID)},
		{UID: grafana.ServiceFolder(h.db.ID), Title: "db", ParentUID: grafana.ProjectFolder(h.project.ID)},
	}
	for _, want := range wantFolders {
		if got, ok := h.api.folder(want.UID); !ok || got != want {
			t.Errorf("folder %s = %+v, want %+v", want.UID, got, want)
		}
	}

	placed := map[string][]grafana.Dashboard{
		grafana.ShedFolder:                  grafana.ShedDashboards(),
		grafana.ProjectFolder(h.project.ID): grafana.ProjectDashboards(h.project.ID),
		grafana.ServiceFolder(h.app.ID):     grafana.ServiceDashboards(h.app.ID, "app"),
		grafana.ServiceFolder(h.db.ID):      grafana.ServiceDashboards(h.db.ID, "postgres"),
	}
	n := 0
	for folder, ds := range placed {
		for _, want := range ds {
			n++
			got := h.api.dashboard(want.UID)
			if got == nil || got.folder != folder || got.json != string(want.JSON) {
				t.Errorf("dashboard %s = %+v, want the template in %s", want.UID, got, folder)
				continue
			}
			rec, ok := h.record(t, want.UID)
			wantRec := store.GrafanaDashboard{UID: want.UID, Template: want.Template, Folder: folder, TemplateVersion: want.Version, GrafanaVersion: got.version}
			if !ok || rec != wantRec {
				t.Errorf("record of %s = %+v, want %+v", want.UID, rec, wantRec)
			}
		}
	}
	if !slices.ContainsFunc(placed[grafana.ServiceFolder(h.db.ID)], func(d grafana.Dashboard) bool { return d.Template == "svc-postgres" }) {
		t.Error("the postgres service has no Postgres dashboard")
	}

	h.reconcile(t)
	for _, ds := range placed {
		for _, d := range ds {
			if got := h.api.dashboard(d.UID); got.version != 1 {
				t.Errorf("%s at version %d after a second pass, want it untouched at 1", d.UID, got.version)
			}
		}
	}
	if len(h.api.dashboards) != n {
		t.Errorf("Grafana holds %d dashboards, want %d", len(h.api.dashboards), n)
	}
}

func TestDashboardsOverwriteUnlessCustomized(t *testing.T) {
	h := newDashboardsHarness(t)
	ctx := context.Background()
	h.reconcile(t)
	ds := grafana.ServiceDashboards(h.app.ID, "app")
	overview := template(t, "svc-overview-"+h.app.ID, ds)
	logs := template(t, "svc-logs-"+h.app.ID, ds)

	// Pretend shed wrote an older template of both.
	for _, d := range []grafana.Dashboard{overview, logs} {
		h.api.dashboards[d.UID].json = `{"uid":"` + d.UID + `","title":"old"}`
		rec, _ := h.record(t, d.UID)
		rec.TemplateVersion = d.Version - 1
		if err := h.st.PutGrafanaDashboard(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	h.api.userSave(t, logs.UID)
	h.reconcile(t)

	if got := h.api.dashboard(overview.UID); got.json != string(overview.JSON) || got.version != 2 {
		t.Errorf("overview = version %d %s, want the current template at version 2", got.version, got.json)
	}
	if rec, _ := h.record(t, overview.UID); rec.TemplateVersion != overview.Version || rec.GrafanaVersion != 2 || rec.Customized {
		t.Errorf("overview record = %+v, want template %d at Grafana version 2", rec, overview.Version)
	}
	if got := h.api.dashboard(logs.UID); got.json == string(logs.JSON) || got.version != 2 {
		t.Errorf("customized logs dashboard overwritten: version %d %s", got.version, got.json)
	}
	rec, _ := h.record(t, logs.UID)
	if !rec.Customized || rec.GrafanaVersion != 2 || rec.TemplateVersion != logs.Version-1 {
		t.Errorf("logs record = %+v, want customized at Grafana version 2, template %d", rec, logs.Version-1)
	}

	h.start(t)
	sd, err := h.d.Service(ctx, h.app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://shed.example.com/grafana/dashboards/f/" + grafana.ServiceFolder(h.app.ID) + "/"; sd.FolderURL != want {
		t.Errorf("folder URL = %q, want %q", sd.FolderURL, want)
	}
	wantLinks := []DashboardLink{
		{UID: overview.UID, Title: "Overview", URL: "https://shed.example.com/grafana/d/" + overview.UID},
		{UID: logs.UID, Title: "Logs", URL: "https://shed.example.com/grafana/d/" + logs.UID, Customized: true, Outdated: true},
	}
	if got := sd.Dashboards[:2]; !slices.Equal(got, wantLinks) {
		t.Errorf("dashboards = %+v, want %+v", got, wantLinks)
	}
	if len(sd.Dashboards) != len(ds) {
		t.Errorf("%d dashboards listed, want %d", len(sd.Dashboards), len(ds))
	}

	if err := h.d.Reset(ctx, logs.UID); err != nil {
		t.Fatal(err)
	}
	if got := h.api.dashboard(logs.UID); got.json != string(logs.JSON) || got.version != 3 {
		t.Errorf("after reset: version %d %s, want the template at version 3", got.version, got.json)
	}
	if rec, _ := h.record(t, logs.UID); rec.Customized || rec.TemplateVersion != logs.Version || rec.GrafanaVersion != 3 {
		t.Errorf("record after reset = %+v, want not customized at template %d, Grafana version 3", rec, logs.Version)
	}
	if err := h.d.Reset(ctx, "svc-overview-elsewhere"); !errors.Is(err, ErrNotDashboard) {
		t.Errorf("Reset of an unknown UID = %v, want ErrNotDashboard", err)
	}
}

func TestDashboardsLostRecord(t *testing.T) {
	h := newDashboardsHarness(t)
	h.reconcile(t)
	uid := "svc-http-" + h.app.ID
	if err := h.st.ForgetGrafanaFolder(context.Background(), grafana.ServiceFolder(h.app.ID)); err != nil {
		t.Fatal(err)
	}
	h.api.userSave(t, uid)
	h.reconcile(t)
	if rec, ok := h.record(t, uid); !ok || !rec.Customized || rec.GrafanaVersion != 2 {
		t.Errorf("record = %+v, %v; want customized at Grafana version 2", rec, ok)
	}
	if got := h.api.dashboard(uid); got.version != 2 {
		t.Errorf("dashboard rewritten to version %d", got.version)
	}
}

func TestDashboardsDeletedByUserStayGone(t *testing.T) {
	h := newDashboardsHarness(t)
	ctx := context.Background()
	h.reconcile(t)
	uid := "svc-traces-" + h.app.ID
	h.api.userDelete(uid)
	h.reconcile(t)
	if h.api.dashboard(uid) != nil {
		t.Fatal("reconciliation recreated a dashboard the user deleted")
	}
	if err := h.d.Restore(ctx); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Restore while not running = %v, want ErrDisabled", err)
	}

	h.start(t)
	if err := h.d.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if got := h.api.dashboard(uid); got == nil || got.folder != grafana.ServiceFolder(h.app.ID) {
		t.Fatalf("after Restore: %+v, want it back in the service folder", got)
	}
	if rec, _ := h.record(t, uid); rec.Customized || rec.GrafanaVersion != 1 {
		t.Errorf("record after Restore = %+v", rec)
	}
}

func TestDashboardsArchiveAndRename(t *testing.T) {
	h := newDashboardsHarness(t)
	ctx := context.Background()
	h.start(t)
	appFolder, dbFolder, projFolder := grafana.ServiceFolder(h.app.ID), grafana.ServiceFolder(h.db.ID), grafana.ProjectFolder(h.project.ID)

	if err := h.st.RenameProject(ctx, h.project.ID, "shop"); err != nil {
		t.Fatal(err)
	}
	h.db.Name = "pg"
	if err := h.st.UpdateService(ctx, h.db); err != nil {
		t.Fatal(err)
	}
	h.d.Refresh()
	waitFor(t, func() bool {
		p, _ := h.api.folder(projFolder)
		s, _ := h.api.folder(dbFolder)
		return p.Title == "shop" && s.Title == "pg"
	})

	if err := h.st.DeleteService(ctx, h.app.ID); err != nil {
		t.Fatal(err)
	}
	h.d.Refresh()
	want := grafana.Folder{UID: appFolder, Title: "shop / web (deleted 2026-10-27)", ParentUID: grafana.ArchivedFolder}
	waitFor(t, func() bool { f, _ := h.api.folder(appFolder); return f == want })
	if _, ok := h.record(t, "svc-overview-"+h.app.ID); ok {
		t.Error("the archived service's dashboards are still recorded")
	}
	if h.api.dashboard("svc-overview-"+h.app.ID) == nil {
		t.Error("archiving deleted the service's dashboards")
	}

	if err := h.st.DeleteProject(ctx, h.project.ID); err != nil {
		t.Fatal(err)
	}
	h.d.Refresh()
	wantProj := grafana.Folder{UID: projFolder, Title: "shop (deleted 2026-10-27)", ParentUID: grafana.ArchivedFolder}
	waitFor(t, func() bool { f, _ := h.api.folder(projFolder); return f == wantProj })
	if f, _ := h.api.folder(dbFolder); f.ParentUID != projFolder || f.Title != "pg" {
		t.Errorf("service folder of the deleted project = %+v, want it left inside the project folder", f)
	}
	if f, _ := h.api.folder(appFolder); f != want {
		t.Errorf("earlier archived folder changed to %+v", f)
	}
	rows, err := h.st.GrafanaDashboards(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Folder != grafana.ShedFolder {
			t.Errorf("record %s in %s kept after its project was deleted", r.UID, r.Folder)
		}
	}
}

func TestAnnotations(t *testing.T) {
	h := newDashboardsHarness(t)
	start := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	svc := telemetry.Service{ProjectID: h.project.ID, ID: h.app.ID, Name: "web"}
	h.d.Annotate(telemetry.Event{Kind: telemetry.EventRestart, Service: svc, Start: start})
	h.start(t)
	h.d.Annotate(telemetry.Event{Kind: telemetry.EventDeployActive, Service: svc, Start: start, End: start.Add(time.Minute), Text: "Deployment d1 active"})
	h.d.Annotate(telemetry.Event{Kind: telemetry.EventBackup, Start: start, Text: "Backed up shed.db"})
	var got []grafana.Annotation
	waitFor(t, func() bool { got = append(got, h.api.takeAnnotations()...); return len(got) >= 2 })

	want := []grafana.Annotation{
		{Time: start, End: start.Add(time.Minute), Tags: []string{"shed", "deploy.active", "service:" + h.app.ID, "project:" + h.project.ID}, Text: "Deployment d1 active"},
		{Time: start, Tags: []string{"shed", "backup"}, Text: "Backed up shed.db"},
	}
	if len(got) != len(want) {
		t.Fatalf("annotations = %+v, want %+v (none from before the worker ran)", got, want)
	}
	for i := range want {
		if !got[i].Time.Equal(want[i].Time) || !got[i].End.Equal(want[i].End) || !slices.Equal(got[i].Tags, want[i].Tags) || got[i].Text != want[i].Text {
			t.Errorf("annotation %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestFiringAlerts(t *testing.T) {
	since := time.Date(2026, 10, 27, 11, 0, 0, 0, time.UTC)
	firing := []grafana.Alert{
		{Labels: map[string]string{"alertname": "Service restarting", "shed_service_id": "svc1", "shed_service_name": "web"}, ActiveAt: since},
		{Labels: map[string]string{"alertname": "Host disk above 85%"}, ActiveAt: since},
		{Labels: map[string]string{"alertname": "OOM killed", "shed_service_id": "svc2"}, ActiveAt: since},
	}
	want := []Alert{
		{Name: "Service restarting", ServiceID: "svc1", Since: since},
		{Name: "Host disk above 85%", Since: since},
		{Name: "OOM killed", ServiceID: "svc2", Since: since},
	}
	if got := firingAlerts(firing); !slices.Equal(got, want) {
		t.Fatalf("firingAlerts() = %+v, want %+v", got, want)
	}

	h := newDashboardsHarness(t)
	h.api.alerts = []grafana.Alert{
		{Labels: map[string]string{"alertname": "Service restarting", "shed_service_id": h.app.ID}, ActiveAt: since},
		{Labels: map[string]string{"alertname": "Connections above 80%", "shed_service_id": h.db.ID}, ActiveAt: since},
	}
	ctx := context.Background()
	if sd, err := h.d.Service(ctx, h.app.ID); err != nil || sd.FolderURL != "" || len(sd.Alerts) != 0 {
		t.Fatalf("Service() while off = %+v, %v; want empty", sd, err)
	}
	h.start(t)
	sd, err := h.d.Service(ctx, h.app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Alert{{Name: "Service restarting", ServiceID: h.app.ID, Since: since}}; !slices.Equal(sd.Alerts, want) {
		t.Errorf("alerts of web = %+v, want %+v", sd.Alerts, want)
	}
	if _, err := h.d.Service(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Service() of an unknown service = %v, want ErrNotFound", err)
	}
}
