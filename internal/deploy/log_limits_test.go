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
