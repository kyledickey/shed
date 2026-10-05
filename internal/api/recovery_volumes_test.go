package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/kyledickey/shed/internal/deploy"
)

func TestStartMissingVolumeReturnsConflict(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	f.deployer.controlErr = fmt.Errorf("recreate container: %w", deploy.ErrVolumeMissing)
	rec := f.do("POST", "/api/services/"+svc.ID+"/start", "")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "volume of this service is missing") {
		t.Fatalf("response = %d %s", rec.Code, rec.Body)
	}
}
