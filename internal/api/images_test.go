package api

import (
	"context"
	"encoding/json"
	"net/http"
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
	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0]["image"] != "sha256:present" {
		t.Fatalf("deployments = %v", got)
	}
	if _, ok := got[0]["imageAvailable"]; ok {
		t.Errorf("imageAvailable = %v, want it left out", got[0]["imageAvailable"])
	}
}
