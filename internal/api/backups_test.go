package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/backup"
	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/store"
)

// fakeBackups keeps backups in the store and records what it is asked to do.
// Every method that can fail returns err when it is set.
type fakeBackups struct {
	st *store.Store

	mu       sync.Mutex
	err      error
	calls    []string
	policy   backup.Policy
	setPol   backup.PolicyInput
	settings backup.Settings
	setSet   backup.SettingsInput
	tested   backup.S3Input
	identity string
	archive  string
}

func (f *fakeBackups) record(call string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	return f.err
}

func (f *fakeBackups) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeBackups) BackUp(ctx context.Context, serviceID string) (store.Backup, error) {
	if err := f.record("backup " + serviceID); err != nil {
		return store.Backup{}, err
	}
	return f.st.CreateBackup(ctx, store.Backup{
		ServiceID: serviceID, Trigger: store.BackupManual, Method: store.MethodVolume,
		Status: store.BackupQueued, File: "id.tar.zst", CreatedAt: time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC),
	})
}

func (f *fakeBackups) Backups(ctx context.Context, serviceID string, limit int) ([]store.Backup, error) {
	if err := f.record(fmt.Sprintf("backups %s %d", serviceID, limit)); err != nil {
		return nil, err
	}
	return f.st.Backups(ctx, serviceID, limit)
}

func (f *fakeBackups) Restore(ctx context.Context, backupID string) (store.Restore, error) {
	if err := f.record("restore " + backupID); err != nil {
		return store.Restore{}, err
	}
	b, err := f.st.Backup(ctx, backupID)
	if err != nil {
		return store.Restore{}, err
	}
	return f.st.CreateRestore(ctx, store.Restore{ServiceID: b.ServiceID, BackupID: b.ID, Status: store.RestoreRunning})
}

func (f *fakeBackups) Delete(_ context.Context, backupID string) error {
	return f.record("delete " + backupID)
}

func (f *fakeBackups) Open(_ context.Context, backupID string) (io.ReadCloser, error) {
	if err := f.record("open " + backupID); err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader(f.archive)), nil
}

func (f *fakeBackups) ForgetService(_ context.Context, serviceID string) error {
	return f.record("forget " + serviceID)
}

func (f *fakeBackups) Policy(_ context.Context, serviceID string) (backup.Policy, error) {
	if err := f.record("policy " + serviceID); err != nil {
		return backup.Policy{}, err
	}
	return f.policy, nil
}

func (f *fakeBackups) SetPolicy(_ context.Context, serviceID string, in backup.PolicyInput) (backup.Policy, error) {
	if err := f.record("set policy " + serviceID); err != nil {
		return backup.Policy{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setPol = in
	return backup.Policy{PolicyInput: in}, nil
}

func (f *fakeBackups) Settings(context.Context) (backup.Settings, error) {
	if err := f.record("settings"); err != nil {
		return backup.Settings{}, err
	}
	return f.settings, nil
}

func (f *fakeBackups) SetSettings(_ context.Context, in backup.SettingsInput) (backup.Settings, error) {
	if err := f.record("set settings"); err != nil {
		return backup.Settings{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setSet = in
	return f.settings, nil
}

func (f *fakeBackups) TestS3(_ context.Context, in backup.S3Input) error {
	if err := f.record("test s3"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tested = in
	return nil
}

func (f *fakeBackups) Identity(context.Context) (string, error) {
	if err := f.record("identity"); err != nil {
		return "", err
	}
	return f.identity, nil
}

// invalidBackup is an ErrInvalid with a user-facing message, as the manager
// returns them.
type invalidBackup string

func (e invalidBackup) Error() string        { return string(e) }
func (e invalidBackup) Is(target error) bool { return target == backup.ErrInvalid }

var backupTime = time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC)

func (f *fixture) createBackup(serviceID string, b store.Backup) store.Backup {
	f.t.Helper()
	b.ServiceID = serviceID
	if b.Trigger == "" {
		b.Trigger = store.BackupSchedule
	}
	if b.Method == "" {
		b.Method = store.MethodVolume
	}
	if b.File == "" {
		b.File = "id.tar.zst"
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = backupTime
	}
	b, err := f.st.CreateBackup(context.Background(), b)
	if err != nil {
		f.t.Fatal(err)
	}
	return b
}

func TestServiceBackups(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	base := "/api/services/" + svc.ID + "/backups"
	f.decode(f.do("GET", "/api/services/nope/backups", ""), http.StatusNotFound)

	f.backups.policy = backup.Policy{PolicyInput: backup.PolicyInput{
		Schedule: "@daily", Compression: "best", KeepLocal: 7, KeepRemote: 30,
	}}
	empty := f.decode(f.do("GET", base, ""), http.StatusOK)
	policy := empty["policy"].(map[string]any)
	if policy["enabled"] != false || policy["nextRunAt"] != nil || policy["schedule"] != "@daily" || policy["keepLocal"] != 7.0 {
		t.Errorf("policy = %v", policy)
	}
	if list, ok := empty["backups"].([]any); !ok || len(list) != 0 {
		t.Errorf("backups = %#v, want []", empty["backups"])
	}
	if v, ok := empty["restore"]; !ok || v != nil {
		t.Errorf("restore = %#v, want null", v)
	}

	next := backupTime.Add(24 * time.Hour)
	f.backups.policy.Enabled, f.backups.policy.NextRun = true, &next
	finished := backupTime.Add(time.Minute)
	b := f.createBackup(svc.ID, store.Backup{
		Method: store.MethodDump, Status: store.BackupSucceeded, File: "id.sql.zst.age", Size: 1234,
		Encrypted: true, Local: true, RemoteKey: "services/x/id.sql.zst.age", RemoteError: "boom", FinishedAt: &finished,
	})
	f.createBackup(svc.ID, store.Backup{CreatedAt: backupTime.Add(-time.Hour), Status: store.BackupFailed, Error: "bad"})
	rs, err := f.st.CreateRestore(context.Background(), store.Restore{ServiceID: svc.ID, BackupID: b.ID, Status: store.RestoreRunning})
	if err != nil {
		t.Fatal(err)
	}

	got := f.decode(f.do("GET", base, ""), http.StatusOK)
	if p := got["policy"].(map[string]any); p["nextRunAt"] != "2026-10-05T03:00:00Z" || p["enabled"] != true {
		t.Errorf("policy = %v", p)
	}
	list := got["backups"].([]any)
	if len(list) != 2 {
		t.Fatalf("backups = %v, want 2", list)
	}
	want := map[string]any{
		"id": b.ID, "serviceId": svc.ID, "trigger": "schedule", "method": "dump", "status": "succeeded",
		"fileName": "web-20261004-030000.sql.zst", "size": 1234.0, "encrypted": true, "local": true, "remote": true,
		"remoteError": "boom", "error": "", "createdAt": "2026-10-04T03:00:00Z", "finishedAt": "2026-10-04T03:01:00Z",
	}
	first := list[0].(map[string]any)
	for k, v := range want {
		if first[k] != v {
			t.Errorf("backup[%s] = %v, want %v", k, first[k], v)
		}
	}
	if len(first) != len(want) {
		t.Errorf("backup has %d fields, want %d: %v", len(first), len(want), first)
	}
	if second := list[1].(map[string]any); second["finishedAt"] != nil || second["remote"] != false || second["error"] != "bad" {
		t.Errorf("failed backup = %v", second)
	}
	r := got["restore"].(map[string]any)
	if r["id"] != rs.ID || r["serviceId"] != svc.ID || r["backupId"] != b.ID || r["status"] != "running" ||
		r["error"] != "" || r["finishedAt"] != nil || r["createdAt"] == nil || len(r) != 7 {
		t.Errorf("restore = %v", r)
	}
	if !slices.Contains(f.backups.seen(), "backups "+svc.ID+" 100") {
		t.Errorf("calls = %v, want a limit of 100", f.backups.seen())
	}
}

func TestSystemBackups(t *testing.T) {
	f := newFixture(t)
	next := backupTime.Add(time.Hour)
	f.backups.policy = backup.Policy{PolicyInput: backup.PolicyInput{Enabled: true, Schedule: "@daily"}, NextRun: &next}
	f.createBackup("", store.Backup{Method: store.MethodSQLite, Status: store.BackupSucceeded, File: "id.db.zst"})

	got := f.decode(f.do("GET", "/api/backups/system", ""), http.StatusOK)
	if _, ok := got["restore"]; ok {
		t.Errorf("system backups have a restore: %v", got)
	}
	if got["policy"].(map[string]any)["nextRunAt"] != "2026-10-04T04:00:00Z" {
		t.Errorf("policy = %v", got["policy"])
	}
	list := got["backups"].([]any)
	if len(list) != 1 {
		t.Fatalf("backups = %v", list)
	}
	b := list[0].(map[string]any)
	if v, ok := b["serviceId"]; !ok || v != nil {
		t.Errorf("serviceId = %#v, want null", v)
	}
	if b["fileName"] != "shed-20261004-030000.db.zst" || b["method"] != "sqlite" {
		t.Errorf("backup = %v", b)
	}
}

func TestBackupPolicyUpdate(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	body := `{"enabled":true,"schedule":"0 4 * * *","compression":"fast","keepLocal":0,"upload":true,"keepRemote":3}`

	got := f.decode(f.do("PUT", "/api/services/"+svc.ID+"/backups/policy", body), http.StatusOK)
	if got["schedule"] != "0 4 * * *" || got["keepRemote"] != 3.0 || got["upload"] != true || got["nextRunAt"] != nil {
		t.Errorf("policy = %v", got)
	}
	want := backup.PolicyInput{Enabled: true, Schedule: "0 4 * * *", Compression: "fast", Upload: true, KeepRemote: 3}
	if f.backups.setPol != want {
		t.Errorf("input = %+v, want %+v", f.backups.setPol, want)
	}
	if !slices.Contains(f.backups.seen(), "set policy "+svc.ID) {
		t.Errorf("calls = %v", f.backups.seen())
	}

	f.decode(f.do("PUT", "/api/backups/system/policy", body), http.StatusOK)
	if !slices.Contains(f.backups.seen(), "set policy ") {
		t.Errorf("calls = %v, want a system policy update", f.backups.seen())
	}

	f.decode(f.do("PUT", "/api/services/nope/backups/policy", body), http.StatusNotFound)
	if r := f.do("PUT", "/api/services/"+svc.ID+"/backups/policy", `{`); r.Code != http.StatusBadRequest {
		t.Errorf("bad JSON: status = %d", r.Code)
	}

	f.backups.err = invalidBackup(`invalid schedule "x": nope`)
	if e := f.decode(f.do("PUT", "/api/backups/system/policy", body), http.StatusBadRequest); e["error"] != `invalid schedule "x": nope` {
		t.Errorf("error = %v, want the manager's message", e["error"])
	}
}

func TestRunBackup(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)

	got := f.decode(f.do("POST", "/api/services/"+svc.ID+"/backups", ""), http.StatusAccepted)
	if got["serviceId"] != svc.ID || got["status"] != "queued" || got["trigger"] != "manual" || got["fileName"] != "web-20261004-030000.tar.zst" {
		t.Errorf("backup = %v", got)
	}
	sys := f.decode(f.do("POST", "/api/backups/system", ""), http.StatusAccepted)
	if v, ok := sys["serviceId"]; !ok || v != nil || sys["fileName"] != "shed-20261004-030000.tar.zst" {
		t.Errorf("system backup = %v", sys)
	}
	if calls := f.backups.seen(); !slices.Equal(calls, []string{"backup " + svc.ID, "backup "}) {
		t.Errorf("calls = %v", calls)
	}

	f.decode(f.do("POST", "/api/services/nope/backups", ""), http.StatusNotFound)
	if calls := f.backups.seen(); len(calls) != 2 {
		t.Errorf("unknown service reached the manager: %v", calls)
	}

	for _, tt := range []struct {
		err    error
		status int
		msg    string
	}{
		{backup.ErrBusy, http.StatusConflict, "a backup or restore is already in progress"},
		{backup.ErrNoVolumes, http.StatusBadRequest, "service has no volumes to back up"},
		{invalidBackup("service has not been deployed yet"), http.StatusBadRequest, "service has not been deployed yet"},
	} {
		f.backups.err = tt.err
		if e := f.decode(f.do("POST", "/api/services/"+svc.ID+"/backups", ""), tt.status); e["error"] != tt.msg {
			t.Errorf("%v: error = %v, want %q", tt.err, e["error"], tt.msg)
		}
	}
}

func TestBackupErrorMapping(t *testing.T) {
	f := newFixture(t)
	b := f.createBackup("", store.Backup{Status: store.BackupSucceeded})
	for _, tt := range []struct {
		err    error
		status int
		msg    string
	}{
		{backup.ErrBusy, http.StatusConflict, "a backup or restore is already in progress"},
		{fmt.Errorf("wrapped: %w", backup.ErrBusy), http.StatusConflict, "a backup or restore is already in progress"},
		{backup.ErrNoVolumes, http.StatusBadRequest, "service has no volumes to back up"},
		{backup.ErrStopped, http.StatusServiceUnavailable, "shutting down"},
		{invalidBackup("only successful backups can be restored"), http.StatusBadRequest, "only successful backups can be restored"},
		{fmt.Errorf("backup: %w", store.ErrNotFound), http.StatusNotFound, "not found"},
		{deploy.ErrDeleting, http.StatusConflict, "service is being deleted"},
		{deploy.ErrServiceBusy, http.StatusConflict, "service is busy with a backup or restore; try again when it finishes"},
		{errors.New("disk on fire"), http.StatusInternalServerError, "internal error"},
	} {
		f.backups.err = tt.err
		for _, route := range []struct{ method, path string }{
			{"POST", "/api/backups/" + b.ID + "/restore"},
			{"DELETE", "/api/backups/" + b.ID},
		} {
			e := f.decode(f.do(route.method, route.path, ""), tt.status)
			if e["error"] != tt.msg {
				t.Errorf("%s %s with %v: error = %v, want %q", route.method, route.path, tt.err, e["error"], tt.msg)
			}
		}
	}
}

func TestRestoreAndDeleteBackup(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	b := f.createBackup(svc.ID, store.Backup{Status: store.BackupSucceeded, Local: true})

	got := f.decode(f.do("POST", "/api/backups/"+b.ID+"/restore", ""), http.StatusAccepted)
	if got["serviceId"] != svc.ID || got["backupId"] != b.ID || got["status"] != "running" || got["id"] == "" || got["finishedAt"] != nil {
		t.Errorf("restore = %v", got)
	}

	rec := f.do("DELETE", "/api/backups/"+b.ID, "")
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Errorf("delete: status = %d, body = %q", rec.Code, rec.Body)
	}
	if calls := f.backups.seen(); !slices.Equal(calls, []string{"restore " + b.ID, "delete " + b.ID}) {
		t.Errorf("calls = %v", calls)
	}
}

func TestDownloadBackup(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	b := f.createBackup(svc.ID, store.Backup{Status: store.BackupSucceeded, File: "id.sql.zst.age", Encrypted: true, Local: true})
	sys := f.createBackup("", store.Backup{Status: store.BackupSucceeded, Method: store.MethodSQLite, File: "id.db.zst"})
	f.backups.archive = "zstd bytes"

	rec := f.do("GET", "/api/backups/"+b.ID+"/download", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "zstd bytes" {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/zstd" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename=web-20261004-030000.sql.zst` {
		t.Errorf("Content-Disposition = %q", got)
	}
	if got := rec.Header().Get("Content-Length"); got != "" {
		t.Errorf("Content-Length = %q, want none", got)
	}

	rec = f.do("GET", "/api/backups/"+sys.ID+"/download", "")
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename=shed-20261004-030000.db.zst` {
		t.Errorf("system Content-Disposition = %q", got)
	}

	f.decode(f.do("GET", "/api/backups/nope/download", ""), http.StatusNotFound)

	f.backups.err = invalidBackup("only successful backups can be downloaded")
	rec = f.do("GET", "/api/backups/"+b.ID+"/download", "")
	e := f.decode(rec, http.StatusBadRequest)
	if e["error"] != "only successful backups can be downloaded" || rec.Header().Get("Content-Disposition") != "" {
		t.Errorf("failed download: %v, headers %v", e, rec.Header())
	}
}

func TestBackupSettings(t *testing.T) {
	f := newFixture(t)
	f.backups.settings = backup.Settings{
		S3: &backup.S3Settings{
			Endpoint: "https://s3.example.com", Region: "us-east-1", Bucket: "b", Prefix: "shed",
			AccessKeyID: "AKIA", PathStyle: true, HasSecret: true,
		},
		Encrypt: true, Recipient: "age1abc",
	}

	rec := f.do("GET", "/api/backups/settings", "")
	got := f.decode(rec, http.StatusOK)
	s3 := got["s3"].(map[string]any)
	wantS3 := map[string]any{
		"endpoint": "https://s3.example.com", "region": "us-east-1", "bucket": "b", "prefix": "shed",
		"accessKeyId": "AKIA", "pathStyle": true, "hasSecret": true,
	}
	for k, v := range wantS3 {
		if s3[k] != v {
			t.Errorf("s3[%s] = %v, want %v", k, s3[k], v)
		}
	}
	if len(s3) != len(wantS3) || strings.Contains(strings.ToLower(rec.Body.String()), "secretaccesskey") {
		t.Errorf("s3 has unexpected fields: %v", s3)
	}
	if enc := got["encryption"].(map[string]any); enc["enabled"] != true || enc["recipient"] != "age1abc" || len(enc) != 2 {
		t.Errorf("encryption = %v", enc)
	}

	f.backups.settings = backup.Settings{}
	if got := f.decode(f.do("GET", "/api/backups/settings", ""), http.StatusOK); got["s3"] != nil || got["encryption"].(map[string]any)["recipient"] != "" {
		t.Errorf("empty settings = %v", got)
	}

	f.backups.settings = backup.Settings{S3: &backup.S3Settings{Bucket: "b", HasSecret: true}}
	put := `{"s3":{"endpoint":"https://s3.example.com","region":"r","bucket":"b","prefix":"p","accessKeyId":"AK","pathStyle":true,"secretAccessKey":"hunter2"},"encryption":{"enabled":true}}`
	rec = f.do("PUT", "/api/backups/settings", put)
	f.decode(rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Errorf("response leaks the secret: %s", rec.Body)
	}
	wantIn := backup.SettingsInput{
		S3: &backup.S3Input{
			Endpoint: "https://s3.example.com", Region: "r", Bucket: "b", Prefix: "p",
			AccessKeyID: "AK", SecretAccessKey: "hunter2", PathStyle: true,
		},
		Encrypt: true,
	}
	if got := f.backups.setSet; got.S3 == nil || *got.S3 != *wantIn.S3 || got.Encrypt != wantIn.Encrypt {
		t.Errorf("input = %+v, want %+v", got, wantIn)
	}

	f.decode(f.do("PUT", "/api/backups/settings", `{"s3":null,"encryption":{"enabled":false}}`), http.StatusOK)
	if got := f.backups.setSet; got.S3 != nil || got.Encrypt {
		t.Errorf("input = %+v, want no S3 and no encryption", got)
	}
	f.decode(f.do("PUT", "/api/backups/settings", `{"s3":null}`), http.StatusBadRequest)
	f.decode(f.do("PUT", "/api/backups/settings", ``), http.StatusBadRequest)

	f.backups.err = invalidBackup("s3: endpoint must start with http:// or https://")
	if e := f.decode(f.do("PUT", "/api/backups/settings", put), http.StatusBadRequest); e["error"] != "s3: endpoint must start with http:// or https://" {
		t.Errorf("error = %v", e["error"])
	}
}

func TestTestBackupSettings(t *testing.T) {
	f := newFixture(t)
	body := `{"s3":{"endpoint":"https://s3.example.com","region":"","bucket":"b","prefix":"","accessKeyId":"AK","pathStyle":false},"encryption":{"enabled":false}}`

	rec := f.do("POST", "/api/backups/settings/test", body)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Errorf("status = %d, body = %q", rec.Code, rec.Body)
	}
	if got := f.backups.tested; got.Bucket != "b" || got.SecretAccessKey != "" || got.AccessKeyID != "AK" {
		t.Errorf("tested = %+v", got)
	}

	before := len(f.backups.seen())
	f.decode(f.do("POST", "/api/backups/settings/test", `{"s3":null,"encryption":{"enabled":false}}`), http.StatusBadRequest)
	f.decode(f.do("POST", "/api/backups/settings/test", ``), http.StatusBadRequest)
	if len(f.backups.seen()) != before {
		t.Errorf("a missing destination reached the manager: %v", f.backups.seen())
	}

	f.backups.err = invalidBackup("access denied")
	if e := f.decode(f.do("POST", "/api/backups/settings/test", body), http.StatusBadRequest); e["error"] != "access denied" {
		t.Errorf("error = %v, want the S3 failure", e["error"])
	}
}

func TestBackupKey(t *testing.T) {
	f := newFixture(t)
	f.backups.identity = "AGE-SECRET-KEY-1ABC"
	rec := f.do("GET", "/api/backups/settings/key", "")
	got := f.decode(rec, http.StatusOK)
	if got["identity"] != "AGE-SECRET-KEY-1ABC" || len(got) != 1 {
		t.Errorf("key = %v", got)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}

	f.backups.err = fmt.Errorf("backup: %w", store.ErrNotFound)
	f.decode(f.do("GET", "/api/backups/settings/key", ""), http.StatusNotFound)
}

func TestDeleteForgetsBackups(t *testing.T) {
	f := newFixture(t)
	a := createApp(t, f.st)
	b, err := f.st.CreateService(context.Background(), store.Service{ProjectID: a.ProjectID, Name: "db", Kind: "postgres"})
	if err != nil {
		t.Fatal(err)
	}

	if rec := f.do("DELETE", "/api/services/"+a.ID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete service: status = %d", rec.Code)
	}
	if calls := f.backups.seen(); !slices.Equal(calls, []string{"forget " + a.ID}) {
		t.Errorf("calls = %v, want forget of the service", calls)
	}

	// A failure to clean up does not fail the deletion.
	f.backups.err = errors.New("disk on fire")
	if rec := f.do("DELETE", "/api/projects/"+a.ProjectID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete project: status = %d; body: %s", rec.Code, rec.Body)
	}
	calls := f.backups.seen()[1:]
	slices.Sort(calls)
	want := []string{"forget " + a.ID, "forget " + b.ID}
	slices.Sort(want)
	if !slices.Equal(calls, want) {
		t.Errorf("project delete calls = %v, want %v", calls, want)
	}
}

func TestBackupRouteProtection(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	b := f.createBackup(svc.ID, store.Backup{Status: store.BackupSucceeded})

	for _, route := range []struct{ method, path string }{
		{"GET", "/api/services/" + svc.ID + "/backups"},
		{"GET", "/api/backups/system"},
		{"GET", "/api/backups/" + b.ID + "/download"},
		{"GET", "/api/backups/settings"},
		{"GET", "/api/backups/settings/key"},
		{"POST", "/api/backups/" + b.ID + "/restore"},
		{"DELETE", "/api/backups/" + b.ID},
	} {
		req := httptest.NewRequest(route.method, route.path, nil)
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: status = %d, want 401", route.method, route.path, rec.Code)
		}
	}

	for _, route := range []struct{ method, path string }{
		{"POST", "/api/services/" + svc.ID + "/backups"},
		{"PUT", "/api/services/" + svc.ID + "/backups/policy"},
		{"POST", "/api/backups/system"},
		{"PUT", "/api/backups/settings"},
		{"POST", "/api/backups/settings/test"},
		{"POST", "/api/backups/" + b.ID + "/restore"},
		{"DELETE", "/api/backups/" + b.ID},
	} {
		req := httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`))
		req.AddCookie(f.cookie)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://evil.example")
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s from another origin: status = %d, want 403", route.method, route.path, rec.Code)
		}
		if route.method == http.MethodDelete {
			continue
		}
		req = httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`))
		req.AddCookie(f.cookie)
		req.Header.Set("Content-Type", "text/plain")
		rec = httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("%s %s as text/plain: status = %d, want 415", route.method, route.path, rec.Code)
		}
	}
	if calls := f.backups.seen(); len(calls) != 0 {
		t.Errorf("rejected requests reached the manager: %v", calls)
	}
}
