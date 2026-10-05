package deploy

import (
	"context"
	"testing"

	"github.com/kyledickey/shed/internal/store"
)

func TestResolveVariables(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	db, err := f.st.CreateService(ctx, store.Service{ProjectID: f.svc.ProjectID, Name: "db", Kind: "postgres"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetVariables(ctx, db.ID, map[string]string{"URL": "pg://${{ SHED_PRIVATE_DOMAIN }}"}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetVariables(ctx, f.svc.ID, map[string]string{
		"GREETING": "hi from ${{ SHED_SERVICE_NAME }}",
		"DB":       "${{ db.URL }}",
		"SHA":      "${{ SHED_GIT_COMMIT_SHA }}",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := f.d.ResolveVariables(ctx, f.svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"GREETING": "hi from web", "DB": "pg://db", "SHA": ""}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("before deploy: %s = %q, want %q", k, got[k], v)
		}
	}

	f.wait(t, f.deploy(t).ID, terminal)
	got, err = f.d.ResolveVariables(ctx, f.svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got["SHA"] != "abc1234def" {
		t.Errorf("after deploy: SHA = %q, want the active commit", got["SHA"])
	}
}
