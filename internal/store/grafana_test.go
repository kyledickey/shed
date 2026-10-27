package store

import (
	"context"
	"slices"
	"testing"
)

func TestGrafanaDashboards(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a := GrafanaDashboard{UID: "svc-logs-s1", Template: "svc-logs", Folder: "s-s1", TemplateVersion: 1, GrafanaVersion: 1}
	b := GrafanaDashboard{UID: "proj-overview-p1", Template: "proj-overview", Folder: "p-p1", TemplateVersion: 1, GrafanaVersion: 3}
	for _, d := range []GrafanaDashboard{a, b} {
		if err := s.PutGrafanaDashboard(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	a.GrafanaVersion, a.Customized = 4, true
	if err := s.PutGrafanaDashboard(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, err := s.GrafanaDashboards(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := []GrafanaDashboard{b, a}; !slices.Equal(got, want) {
		t.Fatalf("GrafanaDashboards() = %+v, want %+v", got, want)
	}

	if err := s.ForgetGrafanaFolder(ctx, "s-s1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GrafanaDashboards(ctx); !slices.Equal(got, []GrafanaDashboard{b}) {
		t.Fatalf("after ForgetGrafanaFolder: %+v, want only %s", got, b.UID)
	}
	if err := s.DeleteGrafanaDashboards(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GrafanaDashboards(ctx); len(got) != 0 {
		t.Fatalf("after DeleteGrafanaDashboards: %+v, want none", got)
	}
}
