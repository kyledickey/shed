package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/store"
)

// push delivers a signed push of sha to octo/app@main.
func (f *fixture) push(sha string) int {
	f.t.Helper()
	body := fmt.Sprintf(`{"ref":"refs/heads/main","after":%q,"repository":{"full_name":"octo/app"},
		"head_commit":{"id":%[1]q,"message":"Change","author":{"name":"Mona"}}}`, sha)
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write([]byte(body))
	req := httptest.NewRequest("POST", "/api/github/webhook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec.Code
}

// replay runs ReplayPushes until the test ends.
func (f *fixture) replay() {
	f.server.pushRetry = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.server.ReplayPushes(ctx)
	}()
	f.t.Cleanup(func() {
		cancel()
		<-done
	})
}

// waitDeployed waits until the deployed commits are want.
func (f *fixture) waitDeployed(want ...string) {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !slices.Equal(f.deployer.deployed(), want) {
		if time.Now().After(deadline) {
			f.t.Fatalf("deployed = %v, want %v", f.deployer.deployed(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

func (f *fixture) pendingSHA(serviceID string) string {
	f.t.Helper()
	p, err := f.st.PendingPush(context.Background(), serviceID)
	if errors.Is(err, store.ErrNotFound) {
		return ""
	}
	if err != nil {
		f.t.Fatal(err)
	}
	return p.CommitSHA
}

func TestWebhookDefersBlockedPush(t *testing.T) {
	for _, blocked := range []error{deploy.ErrServiceBusy, deploy.ErrFenced, deploy.ErrStopped} {
		t.Run(blocked.Error(), func(t *testing.T) {
			f := newFixture(t)
			svc := createApp(t, f.st)
			f.github.Set(newGitHubClient(t, "s3cret"))
			f.deployer.setDeployErr(blocked)

			if code := f.push("aaa"); code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", code)
			}
			if code := f.push("bbb"); code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", code)
			}
			if got := f.deployer.deployed(); len(got) != 0 {
				t.Fatalf("deployed while blocked: %v", got)
			}
			if got := f.pendingSHA(svc.ID); got != "bbb" {
				t.Fatalf("pending push = %q, want the newest, bbb", got)
			}

			f.replay()
			time.Sleep(20 * time.Millisecond) // retries while still blocked
			if got := f.deployer.deployed(); len(got) != 0 {
				t.Fatalf("deployed while blocked: %v", got)
			}
			f.deployer.setDeployErr(nil)
			f.waitDeployed("bbb")
			if got := f.pendingSHA(svc.ID); got != "" {
				t.Errorf("pending push = %q after deploying it", got)
			}
			time.Sleep(20 * time.Millisecond)
			f.waitDeployed("bbb") // and only once

			// A redelivery of a deployed push is not deployed again.
			if code := f.push("bbb"); code != http.StatusAccepted {
				t.Fatalf("redelivery: status = %d, want 202", code)
			}
			f.waitDeployed("bbb")
		})
	}
}

func TestWebhookDeploysImmediately(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	f.github.Set(newGitHubClient(t, "s3cret"))
	if code := f.push("aaa"); code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", code)
	}
	f.waitDeployed("aaa")
	if got := f.pendingSHA(svc.ID); got != "" {
		t.Errorf("pending push = %q after deploying it", got)
	}
}

func TestReplayPushesOnStart(t *testing.T) {
	// A push stored before a restart, as the webhook stores it.
	f := newFixture(t)
	svc := createApp(t, f.st)
	if _, err := f.st.SetPendingPush(context.Background(), store.PendingPush{
		ServiceID: svc.ID, Repo: "octo/app", Branch: "main", CommitSHA: "aaa",
	}); err != nil {
		t.Fatal(err)
	}
	f.server.pushRetry = time.Hour // only the first pass can deploy it
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.server.ReplayPushes(ctx)
	}()
	f.waitDeployed("aaa")
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ReplayPushes did not return after cancellation")
	}
	if got := f.pendingSHA(svc.ID); got != "" {
		t.Errorf("pending push = %q after deploying it", got)
	}
}

func TestPendingPushDropped(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, st *store.Store, svc store.Service)
	}{
		{"auto deploy off", func(t *testing.T, st *store.Store, svc store.Service) {
			svc.AutoDeploy = false
			if err := st.UpdateService(context.Background(), svc); err != nil {
				t.Fatal(err)
			}
		}},
		{"branch changed", func(t *testing.T, st *store.Store, svc store.Service) {
			svc.Branch = "dev"
			if err := st.UpdateService(context.Background(), svc); err != nil {
				t.Fatal(err)
			}
		}},
		{"repo changed", func(t *testing.T, st *store.Store, svc store.Service) {
			svc.Repo = "octo/other"
			if err := st.UpdateService(context.Background(), svc); err != nil {
				t.Fatal(err)
			}
		}},
		{"newer deployment", func(t *testing.T, st *store.Store, svc store.Service) {
			// Most likely within the push's millisecond.
			if _, err := st.CreateDeployment(context.Background(), store.Deployment{
				ServiceID: svc.ID, Status: store.StatusQueued, Trigger: store.TriggerManual, CommitSHA: "manual",
			}); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			svc := createApp(t, f.st)
			f.github.Set(newGitHubClient(t, "s3cret"))
			f.deployer.setDeployErr(deploy.ErrServiceBusy)
			if code := f.push("aaa"); code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", code)
			}
			tt.change(t, f.st, svc)
			f.deployer.setDeployErr(nil)
			f.server.deployPush(context.Background(), svc.ID)
			if got := f.deployer.deployed(); len(got) != 0 {
				t.Errorf("deployed = %v, want none", got)
			}
			if got := f.pendingSHA(svc.ID); got != "" {
				t.Errorf("pending push = %q, want dropped", got)
			}
		})
	}
}

func TestPendingPushKeptOnDeployError(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	f.github.Set(newGitHubClient(t, "s3cret"))
	f.deployer.setDeployErr(errors.New("database is locked"))
	if code := f.push("aaa"); code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", code)
	}
	if got := f.pendingSHA(svc.ID); got != "aaa" {
		t.Errorf("pending push = %q, want aaa", got)
	}
}

func TestWebhookIgnoresDeletedService(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	f.github.Set(newGitHubClient(t, "s3cret"))
	f.deployer.setDeployErr(deploy.ErrDeleting)
	if code := f.push("aaa"); code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", code)
	}
	if err := f.st.DeleteService(context.Background(), svc.ID); err != nil {
		t.Fatal(err)
	}
	if ps, err := f.st.PendingPushes(context.Background()); err != nil || len(ps) != 0 {
		t.Errorf("PendingPushes() = %v, %v, want none", ps, err)
	}
}

// storingDeployer creates deployments in the store, as the real deployer
// does, so that pushes see the deployments earlier pushes created. If gate is
// set, Deploy calls it with the commit before creating the deployment.
type storingDeployer struct {
	*fakeDeployer
	st   *store.Store
	gate func(sha string)
}

func (d *storingDeployer) Deploy(ctx context.Context, serviceID string, trigger store.Trigger, c deploy.Commit) (store.Deployment, error) {
	if d.gate != nil {
		d.gate(c.SHA)
	}
	dep, err := d.fakeDeployer.Deploy(ctx, serviceID, trigger, c)
	if err != nil {
		return dep, err
	}
	return d.st.CreateDeployment(ctx, dep)
}

func (d *storingDeployer) Redeploy(ctx context.Context, id string) (store.Deployment, error) {
	old, err := d.st.Deployment(ctx, id)
	if err != nil {
		return store.Deployment{}, err
	}
	return d.st.CreateDeployment(ctx, store.Deployment{
		ServiceID: old.ServiceID, Status: store.StatusQueued, Trigger: store.TriggerRedeploy, CommitSHA: old.CommitSHA,
	})
}

// storeDeployments makes f's server use a storingDeployer with gate.
func (f *fixture) storeDeployments(gate func(sha string)) {
	f.server.deployer = &storingDeployer{fakeDeployer: f.deployer, st: f.st, gate: gate}
}

// history returns the trigger and commit of a service's deployments, oldest
// first.
func (f *fixture) history(serviceID string) []string {
	f.t.Helper()
	deps, err := f.st.Deployments(context.Background(), serviceID, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []string
	for _, d := range slices.Backward(deps) {
		out = append(out, string(d.Trigger)+" "+d.CommitSHA)
	}
	return out
}

// holdDeploy returns a gate that blocks the deployment of sha, with entered
// closed once it is blocked, until release is called.
func holdDeploy(sha string) (gate func(string), entered <-chan struct{}, release func()) {
	in, out := make(chan struct{}), make(chan struct{})
	var once sync.Once
	return func(got string) {
		if got == sha {
			once.Do(func() { close(in) })
			<-out
		}
	}, in, func() { close(out) }
}

func TestPushesInSameMillisecondAsDeployment(t *testing.T) {
	// Each push follows the deployment of the previous one within about a
	// millisecond, and none of them is mistaken for an older push.
	f := newFixture(t)
	svc := createApp(t, f.st)
	f.storeDeployments(nil)
	var want []string
	for i := range 20 {
		sha := fmt.Sprintf("sha%02d", i)
		// As the webhook does, without its rate limit.
		if err := f.server.receivePush(context.Background(), store.PendingPush{
			ServiceID: svc.ID, Repo: "octo/app", Branch: "main", CommitSHA: sha,
		}); err != nil {
			t.Fatal(err)
		}
		want = append(want, sha)
	}
	f.waitDeployed(want...)
	if got := f.pendingSHA(svc.ID); got != "" {
		t.Errorf("pending push = %q after deploying it", got)
	}
}

func TestPushDuringEarlierPushDeploys(t *testing.T) {
	// A newer push arrives while an older one is being deployed. It must
	// not be taken for one the older push's deployment superseded.
	f := newFixture(t)
	svc := createApp(t, f.st)
	f.github.Set(newGitHubClient(t, "s3cret"))
	gate, entered, release := holdDeploy("aaa")
	f.storeDeployments(gate)

	var wg sync.WaitGroup
	codes := make([]int, 2)
	wg.Go(func() { codes[0] = f.push("aaa") })
	<-entered
	wg.Go(func() { codes[1] = f.push("bbb") })
	time.Sleep(20 * time.Millisecond) // let bbb arrive while aaa is deploying
	release()
	wg.Wait()

	if codes[0] != http.StatusAccepted || codes[1] != http.StatusAccepted {
		t.Fatalf("status = %v, want 202s", codes)
	}
	if got, want := f.history(svc.ID), []string{"push aaa", "push bbb"}; !slices.Equal(got, want) {
		t.Errorf("deployments = %v, want %v", got, want)
	}
	if got := f.pendingSHA(svc.ID); got != "" {
		t.Errorf("pending push = %q after deploying it", got)
	}
}

func TestRedeployDuringPushDeploy(t *testing.T) {
	// A rollback requested while a push is being deployed is created after
	// the push's deployment, so the push does not supersede it.
	f := newFixture(t)
	svc := createApp(t, f.st)
	f.github.Set(newGitHubClient(t, "s3cret"))
	old, err := f.st.CreateDeployment(context.Background(), store.Deployment{
		ServiceID: svc.ID, Status: store.StatusActive, Trigger: store.TriggerManual, CommitSHA: "old",
	})
	if err != nil {
		t.Fatal(err)
	}
	gate, entered, release := holdDeploy("aaa")
	f.storeDeployments(gate)

	var wg sync.WaitGroup
	var pushCode, redeployCode int
	wg.Go(func() { pushCode = f.push("aaa") })
	<-entered
	wg.Go(func() { redeployCode = f.do("POST", "/api/deployments/"+old.ID+"/redeploy", "").Code })
	time.Sleep(20 * time.Millisecond) // let the rollback arrive while aaa is deploying
	release()
	wg.Wait()

	if pushCode != http.StatusAccepted || redeployCode != http.StatusCreated {
		t.Fatalf("push = %d, redeploy = %d, want 202 and 201", pushCode, redeployCode)
	}
	want := []string{"manual old", "push aaa", "redeploy old"}
	if got := f.history(svc.ID); !slices.Equal(got, want) {
		t.Errorf("deployments = %v, want %v", got, want)
	}
}

func TestPendingPushDroppedForRedeploy(t *testing.T) {
	// A push held back while the service is busy is dropped once a rollback
	// is created after it, even within the same millisecond.
	f := newFixture(t)
	svc := createApp(t, f.st)
	f.github.Set(newGitHubClient(t, "s3cret"))
	old, err := f.st.CreateDeployment(context.Background(), store.Deployment{
		ServiceID: svc.ID, Status: store.StatusActive, Trigger: store.TriggerManual, CommitSHA: "old",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.storeDeployments(nil)
	f.deployer.setDeployErr(deploy.ErrServiceBusy)
	if code := f.push("aaa"); code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", code)
	}
	if rec := f.do("POST", "/api/deployments/"+old.ID+"/redeploy", ""); rec.Code != http.StatusCreated {
		t.Fatalf("redeploy = %d %s", rec.Code, rec.Body)
	}
	f.deployer.setDeployErr(nil)
	f.server.deployPush(context.Background(), svc.ID)
	if got, want := f.history(svc.ID), []string{"manual old", "redeploy old"}; !slices.Equal(got, want) {
		t.Errorf("deployments = %v, want %v", got, want)
	}
	if got := f.pendingSHA(svc.ID); got != "" {
		t.Errorf("pending push = %q, want dropped", got)
	}
}
