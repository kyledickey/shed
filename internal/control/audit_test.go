package control

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/kyledickey/shed/internal/audit"
	"github.com/kyledickey/shed/internal/backup"
	"github.com/kyledickey/shed/internal/store"
)

// fakeAudit keeps recorded events with their actor, and lists and stores
// settings with a real log.
type fakeAudit struct {
	*audit.Log
	mu     sync.Mutex
	events []recorded
}

type recorded struct {
	audit.Event
	Actor audit.Actor
}

func (f *fakeAudit) Record(ctx context.Context, e audit.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, recorded{e, audit.ActorFrom(ctx)})
}

// take returns the events recorded since the last call.
func (f *fakeAudit) take() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.events
	f.events = nil
	return out
}

// detailJSON returns the detail of e as JSON, as the log stores it.
func detailJSON(t *testing.T, e audit.Event) string {
	t.Helper()
	b, err := json.Marshal(e.Detail)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAuditRecords(t *testing.T) {
	f := newFixture(t)
	user := audit.Actor{Kind: audit.User, Name: "octocat", IP: "203.0.113.9"}
	ctx := audit.WithActor(context.Background(), user)
	secrets := []string{"s3cr3t-alpha", "s3cr3t-beta", "s3cr3t-gamma", "op-token-123", "aws-secret-456"}

	var prj ProjectView
	var svc ServiceView
	var domain store.Domain
	var volume store.Volume
	var dep DeploymentView
	steps := []struct {
		name    string
		run     func() error
		action  string
		outcome audit.Outcome
		detail  string // JSON the detail must equal, or "" to skip
	}{
		{"create project", func() (err error) {
			prj, err = f.plane.CreateProject(ctx, " shop ")
			return err
		}, "project.create", audit.OK, `{"name":"shop"}`},
		{"create invalid project", func() error {
			_, err := f.plane.CreateProject(ctx, "")
			return err
		}, "project.create", audit.Failed, `{"name":""}`},
		{"rename project", func() error {
			_, err := f.plane.RenameProject(ctx, prj.ID, "store")
			return err
		}, "project.rename", audit.OK, `{"from":"shop","name":"store"}`},
		{"create service", func() (err error) {
			svc, err = f.plane.CreateService(ctx, prj.ID, NewService{Name: "web", Kind: "app", Image: "nginx"})
			return err
		}, "service.create", audit.OK, `{"image":"nginx","kind":"app","name":"web"}`},
		{"update service", func() error {
			port, start := 9090, "./serve --token s3cr3t-alpha"
			_, err := f.plane.UpdateService(ctx, svc.ID, ServicePatch{Port: &port, StartCommand: &start})
			return err
		}, "service.update", audit.OK, `{"fields":["startCommand","port"],"service":"web"}`},
		{"set variables", func() error {
			_, err := f.plane.SetVariables(ctx, svc.ID, map[string]string{"A": "s3cr3t-alpha", "B": "s3cr3t-beta"})
			return err
		}, "service.variables.update", audit.OK, `{"added":["A","B"],"changed":[],"removed":[],"service":"web"}`},
		{"change variables", func() error {
			_, err := f.plane.SetVariables(ctx, svc.ID, map[string]string{"A": "s3cr3t-gamma", "C": "s3cr3t-beta"})
			return err
		}, "service.variables.update", audit.OK, `{"added":["C"],"changed":["A"],"removed":["B"],"service":"web"}`},
		{"invalid variables", func() error {
			_, err := f.plane.SetVariables(ctx, svc.ID, map[string]string{"1X": "s3cr3t-alpha"})
			return err
		}, "service.variables.update", audit.Failed, `{"keys":["1X"],"service":"web"}`},
		{"read variables", func() error {
			_, err := f.plane.Variables(ctx, svc.ID)
			return err
		}, "service.variables.read", audit.OK, `{"service":"web"}`},
		{"read resolved variables", func() error {
			_, err := f.plane.ResolvedVariables(ctx, svc.ID)
			return err
		}, "service.variables.resolved.read", audit.OK, `{"service":"web"}`},
		{"stop", func() error { return f.plane.StopService(ctx, svc.ID) }, "service.stop", audit.OK, `{"service":"web"}`},
		{"stop unknown", func() error { return f.plane.StopService(ctx, "nope") }, "service.stop", audit.OK, `{}`},
		{"deploy", func() (err error) {
			dep, err = f.plane.Deploy(ctx, svc.ID)
			return err
		}, "deployment.create", audit.OK, `{"service":"web","trigger":"manual"}`},
		{"cancel", func() error {
			_, err := f.plane.CancelDeployment(ctx, dep.ID)
			return err
		}, "deployment.cancel", audit.Failed, ""},
		{"create domain", func() (err error) {
			domain, _, err = f.plane.CreateDomain(ctx, svc.ID, "Shop.Example.com")
			return err
		}, "domain.create", audit.OK, `{"host":"shop.example.com","service":"web"}`},
		{"create reserved domain", func() error {
			_, _, err := f.plane.CreateDomain(ctx, svc.ID, "shed.example.com")
			return err
		}, "domain.create", audit.Failed, `{"host":"shed.example.com","service":"web"}`},
		{"delete domain", func() error {
			_, err := f.plane.DeleteDomain(ctx, domain.ID)
			return err
		}, "domain.delete", audit.OK, `{"host":"shop.example.com","service":"web"}`},
		{"create volume", func() (err error) {
			volume, err = f.plane.CreateVolume(ctx, svc.ID, "/data")
			return err
		}, "volume.create", audit.OK, `{"mountPath":"/data","service":"web"}`},
		{"delete volume", func() error { return f.plane.DeleteVolume(ctx, volume.ID) },
			"volume.delete", audit.OK, `{"mountPath":"/data","service":"web"}`},
		{"back up shed.db", func() error {
			_, err := f.plane.BackUpSystem(ctx)
			return err
		}, "backup.run", audit.OK, `{"system":true}`},
		{"read backup key", func() error {
			_, err := f.plane.BackupIdentity(ctx)
			return err
		}, "backup.key.read", audit.OK, `null`},
		{"backup policy", func() error {
			_, err := f.plane.SetServiceBackupPolicy(ctx, svc.ID, backup.PolicyInput{Enabled: true, Schedule: "@daily"})
			return err
		}, "backup.policy.update", audit.OK, `{"fields":["enabled","schedule"],"service":"web"}`},
		{"backup settings", func() error {
			_, err := f.plane.SetBackupSettings(ctx, backup.SettingsInput{Encrypt: true, S3: &backup.S3Input{
				Endpoint: "https://s3.example.com", Bucket: "b", AccessKeyID: "AKIA", SecretAccessKey: "aws-secret-456",
			}})
			return err
		}, "backup.settings.update", audit.OK,
			`{"fields":["encrypt","s3","endpoint","bucket","accessKeyId","secretAccessKey"]}`},
		{"connect 1Password", func() error {
			_, err := f.plane.ConnectOnePassword(ctx, "op-token-123", "")
			return err
		}, "onepassword.connect", audit.Failed, `{"vault":""}`},
		{"auto download", func() error {
			_, err := f.plane.SetAutoDownload(ctx, true)
			return err
		}, "update.settings.update", audit.OK, `{"autoDownload":true}`},
		{"audit retention", func() error {
			_, err := f.plane.SetAuditSettings(ctx, audit.Settings{RetentionDays: 0})
			return err
		}, "audit.settings.update", audit.Failed, `{"retentionDays":0}`},
		{"delete service", func() error { return f.plane.DeleteService(ctx, svc.ID) },
			"service.delete", audit.OK, `{"service":"web"}`},
		{"delete project", func() error { return f.plane.DeleteProject(ctx, prj.ID) },
			"project.delete", audit.OK, `{"name":"store","services":["web"]}`}, // the fake deployer deletes nothing
	}
	var all []recorded
	for _, s := range steps {
		err := s.run()
		got := f.audit.take()
		all = append(all, got...)
		if len(got) != 1 {
			t.Errorf("%s: recorded %d events, want 1: %+v", s.name, len(got), got)
			continue
		}
		e := got[0]
		if e.Action != s.action || e.Outcome != s.outcome || e.Actor != user {
			t.Errorf("%s: recorded %s %s by %+v (err %v), want %s %s by %+v",
				s.name, e.Action, e.Outcome, e.Actor, err, s.action, s.outcome, user)
		}
		if (err != nil) != (e.Outcome == audit.Failed) {
			t.Errorf("%s: outcome %s for error %v", s.name, e.Outcome, err)
		}
		if d := detailJSON(t, e.Event); s.detail != "" && d != s.detail {
			t.Errorf("%s: detail = %s, want %s", s.name, d, s.detail)
		}
	}
	for _, e := range all {
		d := detailJSON(t, e.Event)
		for _, secret := range secrets {
			if strings.Contains(d, secret) || strings.Contains(fmt.Sprint(e.Event), secret) {
				t.Errorf("%s recorded secret %q: %s", e.Action, secret, d)
			}
		}
	}
}

func TestAuditPushDeploymentIsGitHub(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	ctx := audit.WithActor(context.Background(), audit.Actor{Kind: audit.GitHub, IP: "140.82.112.1"})
	f.push(svc, "abc")
	f.audit.take()
	if err := f.plane.ReceivePush(ctx, store.PendingPush{
		ServiceID: svc.ID, Repo: svc.Repo, Branch: svc.Branch, CommitSHA: "def",
	}); err != nil {
		t.Fatal(err)
	}
	got := f.audit.take()
	if len(got) != 1 || got[0].Action != "deployment.create" || got[0].Actor.Kind != audit.GitHub ||
		got[0].Detail["trigger"] != store.TriggerPush || got[0].Detail["commit"] != "def" {
		t.Errorf("events = %+v, want one push deployment by github", got)
	}
}

func TestAuditEventsOutcome(t *testing.T) {
	f := newFixture(t)
	_, err := f.plane.AuditEvents(context.Background(), store.AuditFilter{Outcome: "maybe"})
	wantKind(t, err, ErrInvalid, "outcome")
}
