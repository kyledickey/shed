package api

import (
	"context"
	"net/http"
	"testing"
)

func TestDomainRoutesPendingHeader(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)

	f.deployer.mu.Lock()
	f.deployer.routeFails = 1
	f.deployer.mu.Unlock()
	rec := f.do("POST", "/api/services/"+svc.ID+"/domains", `{"host":"app.example.org"}`)
	f.decode(rec, http.StatusCreated)
	if got := rec.Header().Get(routesPendingHeader); got != "pending" {
		t.Errorf("%s = %q, want pending", routesPendingHeader, got)
	}

	ds, err := f.st.Domains(context.Background(), svc.ID)
	if err != nil || len(ds) != 1 {
		t.Fatalf("Domains() = %v, %v", ds, err)
	}
	rec = f.do("DELETE", "/api/domains/"+ds[0].ID, "")
	if rec.Code != http.StatusNoContent || rec.Header().Get(routesPendingHeader) != "" {
		t.Errorf("delete: status = %d, %s = %q; want 204 and no header",
			rec.Code, routesPendingHeader, rec.Header().Get(routesPendingHeader))
	}
}
