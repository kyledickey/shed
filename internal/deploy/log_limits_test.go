package deploy

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/kyledickey/shed/internal/store"
)

func TestFollowLogBoundsUnterminatedLines(t *testing.T) {
	f := newFixture(t)
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	input := strings.Repeat("x", maxLine*5+17)
	if err := os.WriteFile(f.d.logPath(dep.ID), []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	err := f.d.FollowLog(context.Background(), dep.ID, func(line string) {
		if len(line) > maxLine+4096 {
			t.Fatalf("unbounded line: %d", len(line))
		}
		output.WriteString(line)
	}, func(store.DeploymentStatus) {})
	if err != nil {
		t.Fatal(err)
	}
	if output.String() != input {
		t.Fatal("log changed")
	}
}

func TestCappedWriter(t *testing.T) {
	tests := []struct {
		name   string
		limit  int64
		writes []string
		want   string
	}{
		{"under", 10, []string{"abc\n", "def\n"}, "abc\ndef\n"},
		{"exact", 4, []string{"abc\n"}, "abc\n"},
		{"over", 5, []string{"abc\n", "defgh\n", "ijk\n"}, "abc\nd\n" + truncatedNotice + "\n"},
		{"at limit then more", 4, []string{"abc\n", "d"}, "abc\n\n" + truncatedNotice + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf strings.Builder
			w := &cappedWriter{w: &buf, left: tt.limit}
			for _, s := range tt.writes {
				if n, err := w.Write([]byte(s)); n != len(s) || err != nil {
					t.Fatalf("Write(%q) = %d, %v", s, n, err)
				}
			}
			if buf.String() != tt.want {
				t.Errorf("got %q, want %q", buf.String(), tt.want)
			}
		})
	}
}

func TestBuildLogLimit(t *testing.T) {
	f := newFixture(t)
	f.d.logLimit = 16
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if dep.Status != store.StatusActive {
		t.Fatalf("status %s: %s", dep.Status, dep.Error)
	}
	data, err := os.ReadFile(f.d.logPath(dep.ID))
	if err != nil {
		t.Fatal(err)
	}
	if want := int(f.d.logLimit) + len(truncatedNotice) + 2; len(data) != want || !strings.HasSuffix(string(data), truncatedNotice+"\n") {
		t.Errorf("log of %d bytes, want %d ending in the notice: %q", len(data), want, data)
	}
}

func TestDeploymentHistoryPruned(t *testing.T) {
	f := newFixture(t)
	f.d.keep = 2
	var ids []string
	for range 4 {
		dep := f.wait(t, f.deploy(t).ID, terminal)
		f.settle(t)
		ids = append(ids, dep.ID)
	}
	ds, err := f.st.Deployments(context.Background(), f.svc.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 2 || ds[0].ID != ids[3] || ds[1].ID != ids[2] {
		t.Fatalf("kept %+v", ds)
	}
	for i, id := range ids {
		_, err := os.Stat(f.d.logPath(id))
		if kept := i >= 2; kept != (err == nil) {
			t.Errorf("deployment %d: log exists = %v, want %v", i, err == nil, kept)
		}
	}
}
