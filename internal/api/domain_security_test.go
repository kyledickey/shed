package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDashboardDomainReserved(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	req := httptest.NewRequest("POST", "/api/services/"+svc.ID+"/domains", strings.NewReader(`{"host":"LOCALHOST"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(f.cookie)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	domains, err := f.st.Domains(req.Context(), svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(domains) != 0 {
		t.Fatal("reserved domain persisted")
	}
}
