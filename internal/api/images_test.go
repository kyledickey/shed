package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/store"
)

type imageDeployer struct {
	*fakeDeployer
	available map[string]bool
	err       error
}

func (d *imageDeployer) AvailableImages(context.Context, string, []string) (map[string]bool, error) {
	return d.available, d.err
}
func (d *imageDeployer) Redeploy(context.Context, string) (store.Deployment, error) {
	return store.Deployment{}, deploy.ErrImageUnavailable
}

func TestUnavailableImageReturnsConflict(t *testing.T) {
	f := newFixture(t)
	f.server.deployer = &imageDeployer{fakeDeployer: f.deployer}
	rec := f.do("POST", "/api/deployments/old/redeploy", "")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "image is no longer on the server") {
		t.Fatalf("response = %d %s", rec.Code, rec.Body)
	}
}

func TestDeploymentListImageAvailability(t *testing.T) {
	for _, tc := range []struct {
		name      string
		available map[string]bool
		err       error
	}{
		{name: "available", available: map[string]bool{"sha256:present": true}},
		{name: "missing", available: map[string]bool{}},
		{name: "docker unavailable", err: errors.New("docker unreachable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			svc := createApp(t, f.st)
			_, err := f.st.CreateDeployment(context.Background(), store.Deployment{ServiceID: svc.ID, Status: store.StatusRemoved, Trigger: store.TriggerManual, Image: "sha256:present"})
			if err != nil {
				t.Fatal(err)
			}
			f.server.deployer = &imageDeployer{fakeDeployer: f.deployer, available: tc.available, err: tc.err}
			rec := f.do("GET", "/api/services/"+svc.ID+"/deployments", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("response = %d %s", rec.Code, rec.Body)
			}
			var got []deploymentJSON
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 {
				t.Fatalf("deployments = %d", len(got))
			}
			if tc.err != nil {
				if got[0].ImageAvailable != nil {
					t.Fatal("unknown availability reported as known")
				}
				return
			}
			if got[0].ImageAvailable == nil || *got[0].ImageAvailable != tc.available["sha256:present"] {
				t.Fatalf("availability = %v", got[0].ImageAvailable)
			}
		})
	}
}
