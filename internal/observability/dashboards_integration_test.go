//go:build integration

// This test needs Docker and network access, and that no shed-telemetry-*
// containers or volumes exist. It enables the real stack through the
// Manager with the dashboards worker registered, and checks folders,
// dashboards, customization, reset, annotations, and archiving in Grafana:
//
//	go test -tags integration -run DashboardsIntegration -v -timeout 20m ./internal/observability/
package observability_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/grafana"
	"github.com/kyledickey/shed/internal/observability"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/telemetry"
	"github.com/kyledickey/shed/internal/victoria"
)

func TestDashboardsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	for _, c := range []string{"metrics", "logs", "traces", "grafana"} {
		name := "shed-telemetry-" + c
		if out, _ := exec.Command("docker", "ps", "-aq", "--filter", "name=^"+name+"$").Output(); len(out) > 0 {
			t.Fatalf("container %s exists; this test would replace it", name)
		}
		if exec.Command("docker", "volume", "inspect", name).Run() == nil {
			t.Fatalf("volume %s exists; this test would delete it", name)
		}
		t.Cleanup(func() {
			if t.Failed() {
				logs, _ := exec.Command("docker", "logs", "--tail", "40", name).CombinedOutput()
				t.Logf("%s logs:\n%s", name, logs)
			}
			exec.Command("docker", "rm", "-f", name).Run()
			exec.Command("docker", "volume", "rm", "-f", name).Run()
		})
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	st, err := store.Open(filepath.Join(t.TempDir(), "shed.db"), make([]byte, store.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dc, err := docker.New()
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Close()

	addrs := observability.Addrs{Metrics: freeAddr(t), Logs: freeAddr(t), Traces: freeAddr(t), Grafana: freeAddr(t)}
	newClient := func(pw string) *grafana.Client {
		return grafana.New("http://"+addrs.Grafana+"/grafana", pw, &http.Client{Timeout: 30 * time.Second})
	}
	const grafanaURL = "http://shed.test/grafana/"
	stack := observability.New(observability.Config{
		Store:        st,
		Docker:       dc,
		Metrics:      victoria.NewMetrics("http://"+addrs.Metrics, nil),
		Logs:         victoria.NewLogs("http://"+addrs.Logs, nil),
		Traces:       victoria.NewTraces("http://"+addrs.Traces, nil),
		Grafana:      func(pw string) observability.GrafanaAPI { return newClient(pw) },
		Addrs:        addrs,
		Dir:          filepath.Join(t.TempDir(), "telemetry"),
		GrafanaURL:   grafanaURL,
		OTLPEndpoint: "http://shed.internal:4318",
		Log:          log,
	})
	dashboards := observability.NewDashboards(observability.DashboardsConfig{
		Store: st,
		Grafana: func(ctx context.Context) (observability.DashboardAPI, error) {
			pw, err := stack.AdminPassword(ctx)
			return newClient(pw), err
		},
		GrafanaURL: grafanaURL,
		Log:        log,
	})
	stack.AddWorker(dashboards.Run)

	project, err := st.CreateProject(ctx, "shop")
	if err != nil {
		t.Fatal(err)
	}
	app, err := st.CreateService(ctx, store.Service{ProjectID: project.ID, Name: "web", Kind: "app", Image: "nginx"})
	if err != nil {
		t.Fatal(err)
	}
	db, err := st.CreateService(ctx, store.Service{ProjectID: project.ID, Name: "db", Kind: "postgres", Image: "postgres:17"})
	if err != nil {
		t.Fatal(err)
	}

	s := observability.DefaultSettings()
	s.Enabled = true
	if _, err := stack.SetSettings(ctx, s); err != nil {
		t.Fatal(err)
	}
	runCtx, stop := context.WithCancel(ctx)
	ran := make(chan struct{})
	go func() {
		defer close(ran)
		stack.Run(runCtx)
	}()
	defer func() {
		stop()
		<-ran
	}()

	pw, err := stack.AdminPassword(ctx)
	if err != nil {
		t.Fatal(err)
	}
	gf := newClient(pw)
	eventually := func(what string, ok func() bool) {
		t.Helper()
		began := time.Now()
		for !ok() {
			if time.Since(began) > 10*time.Minute {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(time.Second)
		}
		t.Logf("%s after %s", what, time.Since(began).Round(time.Second))
	}
	record := func(uid string) (store.GrafanaDashboard, bool) {
		rows, err := st.GrafanaDashboards(ctx)
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(rows, func(r store.GrafanaDashboard) bool { return r.UID == uid })
		if i < 0 {
			return store.GrafanaDashboard{}, false
		}
		return rows[i], true
	}

	start := time.Now()
	pgUID, logsUID := "svc-postgres-"+db.ID, "svc-logs-"+app.ID
	eventually("the services' dashboards", func() bool {
		_, okPg := record(pgUID)
		_, okLogs := record(logsUID)
		return okPg && okLogs
	})
	t.Logf("dashboards appeared %s after start", time.Since(start).Round(time.Second))
	for _, want := range []grafana.Folder{
		{UID: grafana.ShedFolder, Title: "shed"},
		{UID: grafana.ArchivedFolder, Title: "Archived"},
		{UID: grafana.ProjectFolder(project.ID), Title: "shop"},
		{UID: grafana.ServiceFolder(app.ID), Title: "web", ParentUID: grafana.ProjectFolder(project.ID)},
		{UID: grafana.ServiceFolder(db.ID), Title: "db", ParentUID: grafana.ProjectFolder(project.ID)},
	} {
		if got, err := gf.Folder(ctx, want.UID); err != nil || got != want {
			t.Errorf("folder %s = %+v, %v; want %+v", want.UID, got, err, want)
		}
	}
	wantIn := map[string][]grafana.Dashboard{
		grafana.ShedFolder:                grafana.ShedDashboards(),
		grafana.ProjectFolder(project.ID): grafana.ProjectDashboards(project.ID),
		grafana.ServiceFolder(app.ID):     grafana.ServiceDashboards(app.ID, "app"),
		grafana.ServiceFolder(db.ID):      grafana.ServiceDashboards(db.ID, "postgres"),
	}
	eventually("every default dashboard", func() bool {
		for _, ds := range wantIn {
			for _, d := range ds {
				if _, ok := record(d.UID); !ok {
					return false
				}
			}
		}
		return true
	})
	for folder, ds := range wantIn {
		for _, d := range ds {
			meta, err := gf.Dashboard(ctx, d.UID)
			rec, _ := record(d.UID)
			if err != nil || meta.FolderUID != folder || meta.Title != d.Title || rec.GrafanaVersion != meta.Version || rec.Customized {
				t.Errorf("%s = %+v, %v with record %+v; want %q in %s at the recorded version", d.UID, meta, err, rec, d.Title, folder)
			}
		}
	}

	// Save the Logs dashboard as a user would.
	before, err := gf.Dashboard(ctx, logsUID)
	if err != nil {
		t.Fatal(err)
	}
	logs := slices.IndexFunc(wantIn[grafana.ServiceFolder(app.ID)], func(d grafana.Dashboard) bool { return d.UID == logsUID })
	var model map[string]any
	if err := json.Unmarshal(wantIn[grafana.ServiceFolder(app.ID)][logs].JSON, &model); err != nil {
		t.Fatal(err)
	}
	model["title"] = "My logs"
	mine, _ := json.Marshal(model)
	saved, err := gf.PutDashboard(ctx, grafana.ServiceFolder(app.ID), mine, before.Version)
	if err != nil || saved <= before.Version {
		t.Fatalf("user save = version %d, %v; want above %d", saved, err, before.Version)
	}
	dashboards.Refresh()
	eventually("the saved dashboard marked customized", func() bool {
		rec, _ := record(logsUID)
		return rec.Customized && rec.GrafanaVersion == saved
	})
	dashboards.Refresh()
	time.Sleep(2 * time.Second)
	if got, err := gf.Dashboard(ctx, logsUID); err != nil || got.Title != "My logs" || got.Version != saved {
		t.Errorf("customized dashboard = %+v, %v; want the user's at version %d", got, err, saved)
	}
	sd, err := dashboards.Service(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if i := slices.IndexFunc(sd.Dashboards, func(l observability.DashboardLink) bool { return l.UID == logsUID }); i < 0 || !sd.Dashboards[i].Customized {
		t.Errorf("service dashboards = %+v, want %s customized", sd.Dashboards, logsUID)
	}

	if err := dashboards.Reset(ctx, logsUID); err != nil {
		t.Fatal(err)
	}
	if got, err := gf.Dashboard(ctx, logsUID); err != nil || got.Title != "Logs" {
		t.Errorf("after reset: %+v, %v; want the default", got, err)
	}
	if rec, _ := record(logsUID); rec.Customized {
		t.Errorf("record after reset = %+v, want not customized", rec)
	}

	at := time.Now().Truncate(time.Millisecond)
	dashboards.Annotate(telemetry.Event{
		Kind: telemetry.EventRestart, Start: at, Text: "Service web restarted",
		Service: telemetry.Service{ProjectID: project.ID, ID: app.ID, Name: "web"},
	})
	eventually("the annotation", func() bool {
		var got []struct {
			Tags []string `json:"tags"`
			Time int64    `json:"time"`
		}
		q := url.Values{"tags": {"service:" + app.ID}, "type": {"annotation"}}
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+addrs.Grafana+"/grafana/api/annotations?"+q.Encode(), nil)
		req.SetBasicAuth(grafana.AdminUser, pw)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		if json.NewDecoder(resp.Body).Decode(&got) != nil || len(got) != 1 {
			return false
		}
		want := []string{"shed", "restart", "service:" + app.ID, "project:" + project.ID}
		slices.Sort(want)
		slices.Sort(got[0].Tags)
		return slices.Equal(got[0].Tags, want) && got[0].Time == at.UnixMilli()
	})

	if err := st.RenameProject(ctx, project.ID, "store"); err != nil {
		t.Fatal(err)
	}
	dashboards.Refresh()
	eventually("the project folder retitled", func() bool {
		f, err := gf.Folder(ctx, grafana.ProjectFolder(project.ID))
		return err == nil && f.Title == "store"
	})

	if err := st.DeleteService(ctx, app.ID); err != nil {
		t.Fatal(err)
	}
	dashboards.Refresh()
	want := grafana.Folder{
		UID:       grafana.ServiceFolder(app.ID),
		Title:     "store / web (deleted " + time.Now().Format(time.DateOnly) + ")",
		ParentUID: grafana.ArchivedFolder,
	}
	eventually("the deleted service's folder archived", func() bool {
		f, err := gf.Folder(ctx, want.UID)
		return err == nil && f == want
	})
	if _, err := gf.Dashboard(ctx, logsUID); err != nil {
		t.Errorf("archived service's Logs dashboard: %v", err)
	}
	if _, ok := record(logsUID); ok {
		t.Error("the archived service's dashboards are still recorded")
	}
	if _, err := gf.Dashboard(ctx, pgUID); err != nil || !strings.HasPrefix(pgUID, "svc-postgres-") {
		t.Errorf("the remaining service's dashboard: %v", err)
	}

	began := time.Now()
	stop()
	<-ran
	t.Logf("stopped in %s", time.Since(began).Round(time.Second))
	if err := dashboards.Restore(ctx); !errors.Is(err, observability.ErrDisabled) {
		t.Errorf("Restore after the worker stopped = %v, want ErrDisabled", err)
	}
}
