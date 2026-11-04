package api

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/store"
)

func TestUnavailableImageReturnsConflict(t *testing.T) {
	f := newFixture(t)
	f.deployer.redeployErr = deploy.ErrImageUnavailable
	rec := f.do("POST", "/api/deployments/old/redeploy", "")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "image is no longer on the server") {
		t.Fatalf("response = %d %s", rec.Code, rec.Body)
	}
}

func TestDeploymentListOmitsUnknownAvailability(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	if _, err := f.st.CreateDeployment(context.Background(), store.Deployment{
		ServiceID: svc.ID, Status: store.StatusRemoved, Trigger: store.TriggerManual, Image: "sha256:present",
	}); err != nil {
		t.Fatal(err)
	}
	rec := f.do("GET", "/api/services/"+svc.ID+"/deployments", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("response = %d %s", rec.Code, rec.Body)
	}
	// The fake deployer cannot tell, so the field is left out.
	var page struct{ Deployments []map[string]any }
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	got := page.Deployments
	if len(got) != 1 || got[0]["image"] != "sha256:present" {
		t.Fatalf("deployments = %v", got)
	}
	if _, ok := got[0]["imageAvailable"]; ok {
		t.Errorf("imageAvailable = %v, want it left out", got[0]["imageAvailable"])
	}
}

func TestDeploymentPages(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	var ids []string // Oldest first.
	for range 3 {
		d, err := f.st.CreateDeployment(context.Background(), store.Deployment{
			ServiceID: svc.ID, Status: store.StatusRemoved, Trigger: store.TriggerManual,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, d.ID)
	}
	base := "/api/services/" + svc.ID + "/deployments"
	for _, tt := range []struct {
		name, target string
		want         []string
		next         any
	}{
		{"default", base, []string{ids[2], ids[1], ids[0]}, nil},
		{"first page", base + "?limit=2", []string{ids[2], ids[1]}, ids[1]},
		{"last page", base + "?limit=2&before=" + ids[1], []string{ids[0]}, nil},
		{"unknown cursor", base + "?before=nope", nil, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := f.decode(f.do("GET", tt.target, ""), http.StatusOK)
			var gotIDs []string
			for _, d := range got["deployments"].([]any) {
				gotIDs = append(gotIDs, d.(map[string]any)["id"].(string))
			}
			if !slices.Equal(gotIDs, tt.want) || got["next"] != tt.next {
				t.Errorf("page = %v next %v, want %v next %v", gotIDs, got["next"], tt.want, tt.next)
			}
		})
	}
	for _, q := range []string{"?limit=0", "?limit=-1", "?limit=ten"} {
		f.decode(f.do("GET", base+q, ""), http.StatusBadRequest)
	}
	f.decode(f.do("GET", "/api/services/nope/deployments", ""), http.StatusNotFound)
}
