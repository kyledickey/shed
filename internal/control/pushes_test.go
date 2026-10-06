package control

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/store"
)

// push receives a push of sha to octo/app@main for svc, as the webhook does.
func (f *fixture) push(svc store.Service, sha string) {
	f.t.Helper()
	ctx := context.Background()
	err := f.plane.ReceivePush(ctx, store.PendingPush{
		ServiceID: svc.ID, Repo: "octo/app", Branch: "main",
		CommitSHA: sha, CommitMessage: "Change", CommitAuthor: "Mona",
	})
	if err != nil {
		f.t.Errorf("ReceivePush(%s) = %v", sha, err)
		return
	}
	f.plane.DeployPush(ctx, svc.ID)
}

// replay runs ReplayPushes until the test ends.
func (f *fixture) replay() {
	f.plane.pushRetry = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.plane.ReplayPushes(ctx)
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

func TestBlockedPushDeferred(t *testing.T) {
	for _, blocked := range []error{deploy.ErrServiceBusy, deploy.ErrFenced, deploy.ErrStopped} {
		t.Run(blocked.Error(), func(t *testing.T) {
			f := newFixture(t)
			svc := createApp(t, f.st)
			f.deployer.setDeployErr(blocked)

			f.push(svc, "aaa")
			f.push(svc, "bbb")
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
		})
	}
}

func TestPushDeploysImmediately(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	f.push(svc, "aaa")
	f.waitDeployed("aaa")
	if got := f.deployer.triggers(); !slices.Equal(got, []store.Trigger{store.TriggerPush}) {
		t.Errorf("triggers = %v, want [push]", got)
	}
	if got := f.pendingSHA(svc.ID); got != "" {
		t.Errorf("pending push = %q after deploying it", got)
	}
}

func TestReplayPushesOnStart(t *testing.T) {
	// A push stored before a restart, as ReceivePush stores it.
	f := newFixture(t)
	svc := createApp(t, f.st)
	if _, err := f.st.SetPendingPush(context.Background(), store.PendingPush{
		ServiceID: svc.ID, Repo: "octo/app", Branch: "main", CommitSHA: "aaa",
	}); err != nil {
		t.Fatal(err)
	}
	f.plane.pushRetry = time.Hour // only the first pass can deploy it
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.plane.ReplayPushes(ctx)
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
			f.deployer.setDeployErr(deploy.ErrServiceBusy)
			f.push(svc, "aaa")
			tt.change(t, f.st, svc)
			f.deployer.setDeployErr(nil)
			f.plane.DeployPush(context.Background(), svc.ID)
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
	f.deployer.setDeployErr(errors.New("database is locked"))
	f.push(svc, "aaa")
	if got := f.pendingSHA(svc.ID); got != "aaa" {
		t.Errorf("pending push = %q, want aaa", got)
	}
}

func TestPendingPushOfDeletedService(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	f.deployer.setDeployErr(deploy.ErrDeleting)
	f.push(svc, "aaa")
	if err := f.st.DeleteService(context.Background(), svc.ID); err != nil {
		t.Fatal(err)
	}
	if ps, err := f.st.PendingPushes(context.Background()); err != nil || len(ps) != 0 {
		t.Errorf("PendingPushes() = %v, %v, want none", ps, err)
	}
	err := f.plane.ReceivePush(context.Background(), store.PendingPush{
		ServiceID: svc.ID, Repo: "octo/app", Branch: "main", CommitSHA: "bbb",
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("ReceivePush for a deleted service = %v, want not found", err)
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

// storeDeployments makes f's plane use a storingDeployer with gate.
func (f *fixture) storeDeployments(gate func(sha string)) {
	f.plane.deployer = &storingDeployer{fakeDeployer: f.deployer, st: f.st, gate: gate}
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
		if err := f.plane.ReceivePush(context.Background(), store.PendingPush{
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
	gate, entered, release := holdDeploy("aaa")
	f.storeDeployments(gate)

	var wg sync.WaitGroup
	wg.Go(func() { f.push(svc, "aaa") })
	<-entered
	wg.Go(func() { f.push(svc, "bbb") })
	time.Sleep(20 * time.Millisecond) // let bbb arrive while aaa is deploying
	release()
	wg.Wait()

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
	old, err := f.st.CreateDeployment(context.Background(), store.Deployment{
		ServiceID: svc.ID, Status: store.StatusActive, Trigger: store.TriggerManual, CommitSHA: "old",
	})
	if err != nil {
		t.Fatal(err)
	}
	gate, entered, release := holdDeploy("aaa")
	f.storeDeployments(gate)

	var wg sync.WaitGroup
	var redeployErr error
	wg.Go(func() { f.push(svc, "aaa") })
	<-entered
	wg.Go(func() { _, redeployErr = f.plane.Redeploy(context.Background(), old.ID) })
	time.Sleep(20 * time.Millisecond) // let the rollback arrive while aaa is deploying
	release()
	wg.Wait()

	if redeployErr != nil {
		t.Fatalf("Redeploy() = %v", redeployErr)
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
	old, err := f.st.CreateDeployment(context.Background(), store.Deployment{
		ServiceID: svc.ID, Status: store.StatusActive, Trigger: store.TriggerManual, CommitSHA: "old",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.storeDeployments(nil)
	f.deployer.setDeployErr(deploy.ErrServiceBusy)
	f.push(svc, "aaa")
	if _, err := f.plane.Redeploy(context.Background(), old.ID); err != nil {
		t.Fatalf("Redeploy() = %v", err)
	}
	f.deployer.setDeployErr(nil)
	f.plane.DeployPush(context.Background(), svc.ID)
	if got, want := f.history(svc.ID), []string{"manual old", "redeploy old"}; !slices.Equal(got, want) {
		t.Errorf("deployments = %v, want %v", got, want)
	}
	if got := f.pendingSHA(svc.ID); got != "" {
		t.Errorf("pending push = %q, want dropped", got)
	}
}
