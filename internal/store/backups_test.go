package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestBackupPolicy(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	sv := mustService(t, s, Service{ProjectID: mustProject(t, s, "p").ID, Name: "a", Kind: "postgres"})

	if _, err := s.BackupPolicy(ctx, sv.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("BackupPolicy on empty error = %v, want ErrNotFound", err)
	}
	tests := []BackupPolicy{
		{ServiceID: sv.ID, Enabled: true, Schedule: "0 3 * * *", Compression: "best", KeepLocal: 7, Upload: true, KeepRemote: 30},
		{ServiceID: sv.ID, Enabled: false, Schedule: "CRON_TZ=Europe/Paris 0 4 * * 1", Compression: "fastest", KeepLocal: 1},
	}
	for _, want := range tests { // The second iteration replaces the first.
		if err := s.PutBackupPolicy(ctx, want); err != nil {
			t.Fatal(err)
		}
		if got, err := s.BackupPolicy(ctx, sv.ID); err != nil || got != want {
			t.Errorf("BackupPolicy() = %+v, %v; want %+v", got, err, want)
		}
	}
}

func TestBackups(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	sv := mustService(t, s, Service{ProjectID: mustProject(t, s, "p").ID, Name: "a", Kind: "postgres"})
	other := mustService(t, s, Service{ProjectID: mustProject(t, s, "q").ID, Name: "b", Kind: "redis"})
	base := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Hour)

	tests := []struct {
		name string
		in   Backup
	}{
		{"first", Backup{ServiceID: sv.ID, Trigger: BackupSchedule, Method: MethodDump, Status: BackupQueued, CreatedAt: base}},
		{"second", Backup{ServiceID: sv.ID, Trigger: BackupManual, Method: MethodVolume, Status: BackupQueued, CreatedAt: base.Add(time.Minute)}},
		{"third", Backup{ServiceID: sv.ID, Trigger: BackupPreRestore, Method: MethodDump, Status: BackupQueued, CreatedAt: base.Add(2 * time.Minute)}},
		{"other service", Backup{ServiceID: other.ID, Trigger: BackupManual, Method: MethodVolume, Status: BackupQueued}},
		{"system", Backup{Trigger: BackupSchedule, Method: MethodSQLite, Status: BackupQueued}},
	}
	created := map[string]Backup{}
	for _, tt := range tests {
		b, err := s.CreateBackup(ctx, tt.in)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if b.ID == "" || b.CreatedAt.IsZero() {
			t.Fatalf("%s: CreateBackup() = %+v, want ID and CreatedAt", tt.name, b)
		}
		got, err := s.Backup(ctx, b.ID)
		if err != nil || !reflect.DeepEqual(got, b) {
			t.Errorf("%s: Backup() = %+v, %v; want %+v", tt.name, got, err, b)
		}
		created[tt.name] = b
	}
	if _, err := s.Backup(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Backup(nope) error = %v, want ErrNotFound", err)
	}
	if created["system"].ServiceID != "" {
		t.Errorf("system ServiceID = %q", created["system"].ServiceID)
	}

	listTests := []struct {
		name      string
		serviceID string
		limit     int
		want      []string
	}{
		{"all newest first", sv.ID, 0, []string{"third", "second", "first"}},
		{"negative limit", sv.ID, -1, []string{"third", "second", "first"}},
		{"limit", sv.ID, 2, []string{"third", "second"}},
		{"other service", other.ID, 0, []string{"other service"}},
		{"system", "", 0, []string{"system"}},
		{"unknown service", "nope", 0, nil},
	}
	for _, tt := range listTests {
		t.Run(tt.name, func(t *testing.T) {
			bs, err := s.Backups(ctx, tt.serviceID, tt.limit)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, b := range bs {
				got = append(got, b.ID)
			}
			var want []string
			for _, n := range tt.want {
				want = append(want, created[n].ID)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Backups() = %v, want %v", got, want)
			}
		})
	}

	// Update writes every mutable column and leaves the immutable ones alone.
	b := created["first"]
	start, end := base.Add(time.Second), base.Add(2*time.Second)
	b.Method, b.Status, b.File, b.Size, b.Encrypted, b.Local = MethodVolume, BackupSucceeded, b.ID+".sql.zst.age", 1234, true, true
	b.RemoteKey, b.RemoteError, b.Error = "backups/x", "upload failed", "warn"
	b.StartedAt, b.FinishedAt = &start, &end
	if err := s.UpdateBackup(ctx, b); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Backup(ctx, b.ID); err != nil || !reflect.DeepEqual(got, b) {
		t.Errorf("Backup() after update = %+v, %v; want %+v", got, err, b)
	}
	if err := s.UpdateBackup(ctx, Backup{ID: "nope"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateBackup(nope) error = %v, want ErrNotFound", err)
	}

	for range 2 { // Deleting a missing backup is not an error.
		if err := s.DeleteBackup(ctx, b.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Backup(ctx, b.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Backup after delete error = %v", err)
	}

	if err := s.DeleteService(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	if bs, _ := s.Backups(ctx, other.ID, 0); len(bs) != 0 {
		t.Errorf("Backups after service delete = %+v", bs)
	}
	if bs, _ := s.Backups(ctx, "", 0); len(bs) != 1 {
		t.Errorf("system backups after service delete = %+v, want kept", bs)
	}
}

func TestDeleteFailedBackupsBefore(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	sv := mustService(t, s, Service{ProjectID: mustProject(t, s, "p").ID, Name: "a", Kind: "postgres"})
	cutoff := time.Now().UTC().Truncate(time.Millisecond)

	tests := []struct {
		name    string
		status  BackupStatus
		created time.Time
		kept    bool
	}{
		{"old failed", BackupFailed, cutoff.Add(-time.Hour), false},
		{"old succeeded", BackupSucceeded, cutoff.Add(-time.Hour), true},
		{"new failed", BackupFailed, cutoff.Add(time.Hour), true},
		{"old queued", BackupQueued, cutoff.Add(-time.Hour), true},
	}
	ids := make([]string, len(tests))
	for i, tt := range tests {
		b, err := s.CreateBackup(ctx, Backup{
			ServiceID: sv.ID, Trigger: BackupManual, Method: MethodDump,
			Status: tt.status, CreatedAt: tt.created,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = b.ID
	}
	if n, err := s.DeleteFailedBackupsBefore(ctx, cutoff); err != nil || n != 1 {
		t.Fatalf("DeleteFailedBackupsBefore() = %d, %v; want 1", n, err)
	}
	for i, tt := range tests {
		_, err := s.Backup(ctx, ids[i])
		if kept := err == nil; kept != tt.kept {
			t.Errorf("%s: kept = %v, want %v (err %v)", tt.name, kept, tt.kept, err)
		}
	}
}

func TestFailInterrupted(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	sv := mustService(t, s, Service{ProjectID: mustProject(t, s, "p").ID, Name: "a", Kind: "postgres"})

	statuses := []BackupStatus{BackupQueued, BackupRunning, BackupUploading, BackupSucceeded, BackupFailed}
	backups := make([]Backup, len(statuses))
	for i, st := range statuses {
		b, err := s.CreateBackup(ctx, Backup{
			ServiceID: sv.ID, Trigger: BackupManual, Method: MethodDump, Status: st, Error: "orig",
		})
		if err != nil {
			t.Fatal(err)
		}
		backups[i] = b
	}
	if n, err := s.FailInterruptedBackups(ctx, "interrupted by restart"); err != nil || n != 3 {
		t.Fatalf("FailInterruptedBackups() = %d, %v; want 3", n, err)
	}
	for i, st := range statuses {
		got, err := s.Backup(ctx, backups[i].ID)
		if err != nil {
			t.Fatal(err)
		}
		interrupted := st == BackupQueued || st == BackupRunning
		switch {
		case st == BackupUploading:
			// The archive is complete; only the upload was interrupted.
			if got.Status != BackupSucceeded || got.Error != "orig" || got.RemoteError != "interrupted by restart" || got.FinishedAt == nil {
				t.Errorf("uploading backup after = %+v, want succeeded with the upload interrupted", got)
			}
		case interrupted && (got.Status != BackupFailed || got.Error != "interrupted by restart" || got.FinishedAt == nil):
			t.Errorf("%s backup after = %+v, want interrupted failure", st, got)
		case !interrupted && (got.Status != st || got.Error != "orig" || got.FinishedAt != nil):
			t.Errorf("%s backup after = %+v, want unchanged", st, got)
		}
	}
	if n, err := s.FailInterruptedBackups(ctx, "x"); err != nil || n != 0 {
		t.Errorf("second FailInterruptedBackups() = %d, %v; want 0", n, err)
	}

	if _, err := s.CreateRestore(ctx, Restore{ServiceID: sv.ID, BackupID: backups[3].ID, Status: RestoreRunning}); err != nil {
		t.Fatal(err)
	}
	done, err := s.CreateRestore(ctx, Restore{ServiceID: sv.ID, BackupID: backups[3].ID, Status: RestoreSucceeded})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.FailInterruptedRestores(ctx, "interrupted by restart"); err != nil || n != 1 {
		t.Fatalf("FailInterruptedRestores() = %d, %v; want 1", n, err)
	}
	if got := latestOrFatal(t, s, sv.ID); got.ID != done.ID || got.Status != RestoreSucceeded || got.Error != "" {
		t.Errorf("succeeded restore after = %+v, want unchanged", got)
	}
	if err := s.UpdateRestore(ctx, Restore{ID: done.ID, Status: RestoreRunning}); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.FailInterruptedRestores(ctx, "again"); n != 1 {
		t.Errorf("FailInterruptedRestores() = %d, want 1", n)
	}
	got, err := s.LatestRestore(ctx, sv.ID)
	if err != nil || got.Status != RestoreFailed || got.Error != "again" || got.FinishedAt == nil {
		t.Errorf("LatestRestore() = %+v, %v", got, err)
	}
}

func latestOrFatal(t *testing.T, s *Store, serviceID string) Restore {
	t.Helper()
	r, err := s.LatestRestore(context.Background(), serviceID)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRestores(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	sv := mustService(t, s, Service{ProjectID: mustProject(t, s, "p").ID, Name: "a", Kind: "postgres"})

	if _, err := s.LatestRestore(ctx, sv.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("LatestRestore on empty error = %v, want ErrNotFound", err)
	}
	first, err := s.CreateRestore(ctx, Restore{ServiceID: sv.ID, BackupID: "gone", Status: RestoreRunning})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == "" || first.CreatedAt.IsZero() {
		t.Fatalf("CreateRestore() = %+v", first)
	}
	second, err := s.CreateRestore(ctx, Restore{ServiceID: sv.ID, BackupID: "b2", Status: RestoreRunning})
	if err != nil {
		t.Fatal(err)
	}
	if got := latestOrFatal(t, s, sv.ID); !reflect.DeepEqual(got, second) {
		t.Errorf("LatestRestore() = %+v, want %+v", got, second)
	}

	end := time.Now().UTC().Truncate(time.Millisecond)
	second.Status, second.Error, second.FinishedAt = RestoreFailed, "boom", &end
	if err := s.UpdateRestore(ctx, second); err != nil {
		t.Fatal(err)
	}
	if got := latestOrFatal(t, s, sv.ID); !reflect.DeepEqual(got, second) {
		t.Errorf("LatestRestore() after update = %+v, want %+v", got, second)
	}
	if err := s.UpdateRestore(ctx, Restore{ID: "nope"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateRestore(nope) error = %v, want ErrNotFound", err)
	}

	if err := s.DeleteService(ctx, sv.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LatestRestore(ctx, sv.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("LatestRestore after service delete error = %v", err)
	}
}

func TestBackupCascadeOnServiceDelete(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	sv := mustService(t, s, Service{ProjectID: mustProject(t, s, "p").ID, Name: "a", Kind: "postgres"})
	if err := s.PutBackupPolicy(ctx, BackupPolicy{ServiceID: sv.ID, Schedule: "@daily", Compression: "best"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateBackup(ctx, Backup{ServiceID: sv.ID, Trigger: BackupManual, Method: MethodDump, Status: BackupQueued}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteService(ctx, sv.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BackupPolicy(ctx, sv.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("BackupPolicy after service delete error = %v", err)
	}
	if bs, err := s.Backups(ctx, sv.ID, 0); err != nil || len(bs) != 0 {
		t.Errorf("Backups after service delete = %+v, %v", bs, err)
	}
}

func TestServicesWithVolumes(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := mustProject(t, s, "p")
	a := mustService(t, s, Service{ProjectID: p.ID, Name: "a", Kind: "postgres"})
	mustService(t, s, Service{ProjectID: p.ID, Name: "novol", Kind: "app"})
	c := mustService(t, s, Service{ProjectID: p.ID, Name: "c", Kind: "redis"})

	if svs, err := s.ServicesWithVolumes(ctx); err != nil || len(svs) != 0 {
		t.Fatalf("ServicesWithVolumes() on none = %+v, %v", svs, err)
	}
	for _, v := range []struct{ svc, path string }{{c.ID, "/data"}, {a.ID, "/var/lib/a"}, {a.ID, "/extra"}} {
		if _, err := s.CreateVolume(ctx, v.svc, v.path); err != nil {
			t.Fatal(err)
		}
	}
	svs, err := s.ServicesWithVolumes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, sv := range svs {
		got = append(got, sv.Name)
	}
	if want := []string{"a", "c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ServicesWithVolumes() = %v, want %v (once each, oldest first)", got, want)
	}
}

func TestSnapshot(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	sv := mustService(t, s, Service{ProjectID: mustProject(t, s, "p").ID, Name: "a", Kind: "postgres"})
	if err := s.SetSetting(ctx, "k", "v"); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "snap.db")
	if err := s.Snapshot(ctx, path); err != nil {
		t.Fatal(err)
	}
	snap, err := Open(path)
	if err != nil {
		t.Fatalf("Open(snapshot): %v", err)
	}
	defer snap.Close()
	if got, err := snap.Setting(ctx, "k"); err != nil || got != "v" {
		t.Errorf("snapshot Setting() = %q, %v", got, err)
	}
	if got, err := snap.Service(ctx, sv.ID); err != nil || got.Name != "a" {
		t.Errorf("snapshot Service() = %+v, %v", got, err)
	}

	if err := s.Snapshot(ctx, path); err == nil {
		t.Error("Snapshot to an existing path succeeded, want error")
	}
}
