package observability

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/kyledickey/shed/internal/grafana"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/telemetry"
)

// ErrNotDashboard reports a dashboard UID that is not one of shed's default
// dashboards.
var ErrNotDashboard = errors.New("not one of shed's dashboards")

// DashboardStore keeps the projects and services that dashboards follow,
// and shed's records of the dashboards it created. *store.Store implements
// it.
type DashboardStore interface {
	Projects(ctx context.Context) ([]store.Project, error)
	AllServices(ctx context.Context) ([]store.Service, error)
	Service(ctx context.Context, id string) (store.Service, error)
	GrafanaDashboards(ctx context.Context) ([]store.GrafanaDashboard, error)
	PutGrafanaDashboard(ctx context.Context, d store.GrafanaDashboard) error
	ForgetGrafanaFolder(ctx context.Context, folder string) error
}

var _ DashboardStore = (*store.Store)(nil)

// DashboardAPI is the part of Grafana's API that folders, dashboards,
// annotations, and alerts need. *grafana.Client implements it.
type DashboardAPI interface {
	Folder(ctx context.Context, uid string) (grafana.Folder, error)
	EnsureFolder(ctx context.Context, f grafana.Folder) error
	Dashboard(ctx context.Context, uid string) (grafana.DashboardMeta, error)
	PutDashboard(ctx context.Context, folderUID string, dashboard json.RawMessage, replaces int) (int, error)
	CreateAnnotation(ctx context.Context, a grafana.Annotation) (int64, error)
	FiringAlerts(ctx context.Context) ([]grafana.Alert, error)
}

var _ DashboardAPI = (*grafana.Client)(nil)

// DashboardsConfig configures Dashboards.
type DashboardsConfig struct {
	Store DashboardStore
	// Grafana returns a client that signs in as Grafana's admin.
	Grafana func(ctx context.Context) (DashboardAPI, error)
	// GrafanaURL is the public URL Grafana is served at,
	// "<server.url>/grafana/".
	GrafanaURL string
	Log        *slog.Logger
}

// Dashboards keeps Grafana's folders and dashboards in step with shed's
// projects and services, and writes annotations. [Dashboards.Run] is a
// [Worker]: everything happens only while it runs, that is, while telemetry
// is on. It is safe for concurrent use.
type Dashboards struct {
	cfg   DashboardsConfig
	every time.Duration // between passes
	retry time.Duration // after a failed pass
	now   func() time.Time
	kick  chan struct{}

	mu sync.Mutex // serializes reconciliation, Reset, and Restore

	liveMu sync.Mutex
	live   *live // non-nil while Run runs; guarded by liveMu
}

// live is what one run of the worker shares with the other methods.
type live struct {
	api    DashboardAPI
	events chan telemetry.Event
}

// reconcileEvery is how often Dashboards reconciles without being asked, to
// catch changes it was not told about.
const reconcileEvery = 10 * time.Minute

// annotationQueue is how many events wait for Grafana before new ones are
// dropped.
const annotationQueue = 256

// NewDashboards returns Dashboards. Register its Run with
// [Manager.AddWorker].
func NewDashboards(cfg DashboardsConfig) *Dashboards {
	return &Dashboards{cfg: cfg, every: reconcileEvery, retry: retryEvery, now: time.Now, kick: make(chan struct{}, 1)}
}

// Run reconciles now, after every [Dashboards.Refresh], every 10 minutes,
// and a minute after a failed pass, and writes annotations, until ctx is
// done.
func (d *Dashboards) Run(ctx context.Context) {
	api, err := d.cfg.Grafana(ctx)
	if err != nil {
		d.cfg.Log.Warn("grafana dashboards", "err", err)
		return
	}
	l := &live{api: api, events: make(chan telemetry.Event, annotationQueue)}
	d.setLive(l)
	defer d.setLive(nil)
	var wg sync.WaitGroup
	defer wg.Wait()
	wg.Go(func() { d.annotate(ctx, l) })

	for {
		wait := d.every
		if err := d.reconcile(ctx, api, false); err != nil && ctx.Err() == nil {
			d.cfg.Log.Warn("reconcile grafana dashboards", "err", err)
			wait = min(wait, d.retry)
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-d.kick:
		case <-timer.C:
		}
		timer.Stop()
	}
}

// Refresh asks for a reconciliation soon, after a project or service was
// created, renamed, or deleted. It does not block.
func (d *Dashboards) Refresh() {
	select {
	case d.kick <- struct{}{}:
	default:
	}
}

func (d *Dashboards) setLive(l *live) {
	d.liveMu.Lock()
	defer d.liveMu.Unlock()
	d.live = l
}

// current returns the running worker's state, or ErrDisabled.
func (d *Dashboards) current() (*live, error) {
	d.liveMu.Lock()
	defer d.liveMu.Unlock()
	if d.live == nil {
		return nil, ErrDisabled
	}
	return d.live, nil
}

// scope is a folder and the default dashboards in it.
type scope struct {
	folder     grafana.Folder
	dashboards []grafana.Dashboard
}

// scopes returns every folder shed keeps and its dashboards, parents before
// children.
func scopes(projects []store.Project, services []store.Service) []scope {
	out := []scope{
		{grafana.Folder{UID: grafana.ShedFolder, Title: "shed"}, grafana.ShedDashboards()},
		{grafana.Folder{UID: grafana.ArchivedFolder, Title: "Archived"}, nil},
	}
	for _, p := range projects {
		out = append(out, scope{grafana.Folder{UID: grafana.ProjectFolder(p.ID), Title: p.Name}, grafana.ProjectDashboards(p.ID)})
	}
	for _, s := range services {
		f := grafana.Folder{UID: grafana.ServiceFolder(s.ID), Title: s.Name, ParentUID: grafana.ProjectFolder(s.ProjectID)}
		out = append(out, scope{f, grafana.ServiceDashboards(s.ID, s.Kind)})
	}
	return out
}

func (d *Dashboards) scopes(ctx context.Context) ([]scope, error) {
	projects, err := d.cfg.Store.Projects(ctx)
	if err != nil {
		return nil, err
	}
	services, err := d.cfg.Store.AllServices(ctx)
	if err != nil {
		return nil, err
	}
	return scopes(projects, services), nil
}

func (d *Dashboards) records(ctx context.Context) (map[string]store.GrafanaDashboard, error) {
	rows, err := d.cfg.Store.GrafanaDashboards(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]store.GrafanaDashboard, len(rows))
	for _, r := range rows {
		out[r.UID] = r
	}
	return out, nil
}

// reconcile makes every folder exist with its title and parent, creates or
// updates the default dashboards as [grafana.Reconcile] decides, and
// archives the folders of deleted projects and services. With restore, it
// also recreates dashboards the user deleted.
func (d *Dashboards) reconcile(ctx context.Context, api DashboardAPI, restore bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	all, err := d.scopes(ctx)
	if err != nil {
		return err
	}
	recs, err := d.records(ctx)
	if err != nil {
		return err
	}
	var errs []error
	kept := make(map[string]bool, len(all))
	for _, s := range all {
		kept[s.folder.UID] = true
		if err := api.EnsureFolder(ctx, s.folder); err != nil {
			errs = append(errs, err)
			continue
		}
		for _, db := range s.dashboards {
			var rec *store.GrafanaDashboard
			if r, ok := recs[db.UID]; ok {
				rec = &r
			}
			errs = append(errs, d.sync(ctx, api, s.folder.UID, db, rec, restore))
		}
	}
	gone := make(map[string]bool)
	for _, r := range recs {
		if !kept[r.Folder] {
			gone[r.Folder] = true
		}
	}
	errs = append(errs, d.archive(ctx, api, gone))
	return errors.Join(errs...)
}

// sync reconciles one dashboard in folder with rec, shed's record of it.
func (d *Dashboards) sync(ctx context.Context, api DashboardAPI, folder string, db grafana.Dashboard, rec *store.GrafanaDashboard, restore bool) error {
	var remote grafana.Remote
	meta, err := api.Dashboard(ctx, db.UID)
	switch {
	case err == nil:
		remote = grafana.Remote{Exists: true, Version: meta.Version}
	case !errors.Is(err, grafana.ErrNotFound):
		return err
	}
	var r *grafana.Record
	if rec != nil {
		r = &grafana.Record{TemplateVersion: rec.TemplateVersion, GrafanaVersion: rec.GrafanaVersion, Customized: rec.Customized}
	}
	action := grafana.Reconcile(r, remote, db.Version)
	if restore && !remote.Exists {
		action = grafana.Create
	}
	row := store.GrafanaDashboard{UID: db.UID, Template: db.Template, Folder: folder}
	switch action {
	case grafana.Create, grafana.Overwrite:
		replaces := 0
		if action == grafana.Overwrite {
			replaces = rec.GrafanaVersion
		}
		v, err := api.PutDashboard(ctx, folder, db.JSON, replaces)
		if errors.Is(err, grafana.ErrConflict) {
			// Someone wrote it meanwhile; the next pass sees what they did.
			return nil
		}
		if err != nil {
			return err
		}
		row.TemplateVersion, row.GrafanaVersion = db.Version, v
	case grafana.MarkCustomized:
		if rec != nil {
			row.TemplateVersion = rec.TemplateVersion
		}
		row.GrafanaVersion, row.Customized = remote.Version, true
	default:
		return nil
	}
	return d.cfg.Store.PutGrafanaDashboard(ctx, row)
}

// archive moves the folders in gone, those of deleted projects and
// services, under Archived and forgets their dashboards. Project folders
// sort first and take their service folders with them.
func (d *Dashboards) archive(ctx context.Context, api DashboardAPI, gone map[string]bool) error {
	date := d.now().Format(time.DateOnly)
	var errs []error
	for _, uid := range slices.Sorted(maps.Keys(gone)) {
		err := archiveFolder(ctx, api, uid, gone, date)
		if err == nil {
			err = d.cfg.Store.ForgetGrafanaFolder(ctx, uid)
		}
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// archiveFolder moves folder uid under Archived as "<parent> / <title>
// (deleted <date>)", or "<title> (deleted <date>)" at the top level, unless
// the folder is gone, archived, or inside another folder in gone.
func archiveFolder(ctx context.Context, api DashboardAPI, uid string, gone map[string]bool, date string) error {
	f, err := api.Folder(ctx, uid)
	switch {
	case errors.Is(err, grafana.ErrNotFound):
		return nil
	case err != nil:
		return err
	case f.ParentUID == grafana.ArchivedFolder || gone[f.ParentUID]:
		return nil
	}
	title := f.Title
	if f.ParentUID != "" {
		parent, err := api.Folder(ctx, f.ParentUID)
		if err != nil {
			return err
		}
		title = parent.Title + " / " + title
	}
	return api.EnsureFolder(ctx, grafana.Folder{UID: uid, Title: title + " (deleted " + date + ")", ParentUID: grafana.ArchivedFolder})
}

// Reset overwrites the default dashboard uid with its current template and
// clears its customized mark. It fails with ErrNotDashboard for any other
// UID, and with ErrDisabled while telemetry is off.
func (d *Dashboards) Reset(ctx context.Context, uid string) error {
	l, err := d.current()
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	all, err := d.scopes(ctx)
	if err != nil {
		return err
	}
	for _, s := range all {
		for _, db := range s.dashboards {
			if db.UID == uid {
				return d.reset(ctx, l.api, s.folder, db)
			}
		}
	}
	return ErrNotDashboard
}

func (d *Dashboards) reset(ctx context.Context, api DashboardAPI, folder grafana.Folder, db grafana.Dashboard) error {
	replaces := 0
	meta, err := api.Dashboard(ctx, db.UID)
	switch {
	case err == nil:
		replaces = meta.Version
	case !errors.Is(err, grafana.ErrNotFound):
		return err
	}
	if err := api.EnsureFolder(ctx, folder); err != nil {
		return err
	}
	v, err := api.PutDashboard(ctx, folder.UID, db.JSON, replaces)
	if err != nil {
		return err
	}
	return d.cfg.Store.PutGrafanaDashboard(ctx, store.GrafanaDashboard{
		UID: db.UID, Template: db.Template, Folder: folder.UID, TemplateVersion: db.Version, GrafanaVersion: v,
	})
}

// Restore recreates the default dashboards that are missing from Grafana,
// including those a user deleted. It fails with ErrDisabled while
// telemetry is off.
func (d *Dashboards) Restore(ctx context.Context) error {
	l, err := d.current()
	if err != nil {
		return err
	}
	return d.reconcile(ctx, l.api, true)
}

// DashboardLink is one of a service's default dashboards.
type DashboardLink struct {
	UID, Title, URL string
	// Customized is set once a user saved the dashboard.
	Customized bool
	// Outdated is set for a customized dashboard whose template has
	// changed since shed last wrote it.
	Outdated bool
}

// ServiceDashboards is what Grafana holds for one service.
type ServiceDashboards struct {
	// FolderURL is "" while telemetry is off.
	FolderURL  string
	Dashboards []DashboardLink
	Alerts     []Alert
}

// Service returns the folder, default dashboards, and firing alerts of
// service id. While telemetry is off, it is empty.
func (d *Dashboards) Service(ctx context.Context, id string) (ServiceDashboards, error) {
	l, err := d.current()
	if err != nil {
		return ServiceDashboards{}, nil
	}
	svc, err := d.cfg.Store.Service(ctx, id)
	if err != nil {
		return ServiceDashboards{}, err
	}
	recs, err := d.records(ctx)
	if err != nil {
		return ServiceDashboards{}, err
	}
	out := ServiceDashboards{FolderURL: d.cfg.GrafanaURL + "dashboards/f/" + grafana.ServiceFolder(id) + "/"}
	for _, db := range grafana.ServiceDashboards(svc.ID, svc.Kind) {
		link := DashboardLink{UID: db.UID, Title: db.Title, URL: d.cfg.GrafanaURL + "d/" + db.UID}
		if rec, ok := recs[db.UID]; ok && rec.Customized {
			link.Customized, link.Outdated = true, db.Version > rec.TemplateVersion
		}
		out.Dashboards = append(out.Dashboards, link)
	}
	probe, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	if firing, err := l.api.FiringAlerts(probe); err == nil {
		for _, a := range firingAlerts(firing) {
			if a.ServiceID == id {
				out.Alerts = append(out.Alerts, a)
			}
		}
	}
	return out, nil
}

// Annotate queues e to be written as a Grafana annotation. It does not
// block, and drops e while telemetry is off or the queue is full.
func (d *Dashboards) Annotate(e telemetry.Event) {
	l, err := d.current()
	if err != nil {
		return
	}
	select {
	case l.events <- e:
	default:
		d.cfg.Log.Warn("annotation queue full; dropping event", "kind", e.Kind, "service", e.Service.ID)
	}
}

// annotationTimeout bounds each annotation write.
const annotationTimeout = 10 * time.Second

// annotate writes the queued events until ctx is done. A failure is logged
// and the event dropped.
func (d *Dashboards) annotate(ctx context.Context, l *live) {
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-l.events:
			wctx, cancel := context.WithTimeout(ctx, annotationTimeout)
			_, err := l.api.CreateAnnotation(wctx, annotation(e))
			cancel()
			if err != nil && ctx.Err() == nil {
				d.cfg.Log.Warn("write annotation", "kind", e.Kind, "service", e.Service.ID, "err", err)
			}
		}
	}
}

// annotation returns e as a Grafana annotation, tagged shed, its kind, and
// its service and project, which the dashboards' annotation queries match.
func annotation(e telemetry.Event) grafana.Annotation {
	tags := []string{"shed", string(e.Kind)}
	if e.Service.ID != "" {
		tags = append(tags, "service:"+e.Service.ID)
	}
	if e.Service.ProjectID != "" {
		tags = append(tags, "project:"+e.Service.ProjectID)
	}
	return grafana.Annotation{Time: e.Start, End: e.End, Tags: tags, Text: e.Text}
}
