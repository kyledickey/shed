package web

import (
	"maps"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestPackageGraph checks that the docs' package graph lists every internal
// package with the internal packages it imports, as go list reports them.
func TestPackageGraph(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", `{{.ImportPath}} {{join .Imports " "}}`, "../internal/...").Output()
	if err != nil {
		t.Skipf("go list: %v", err)
	}
	const prefix = "github.com/kyledickey/shed/internal/"
	want := map[string][]string{}
	for line := range strings.Lines(string(out)) {
		fields := strings.Fields(line)
		name := strings.TrimPrefix(fields[0], prefix)
		if strings.Contains(name, "/") {
			continue // test helpers such as telemetry/telemetrytest
		}
		imports := []string{}
		for _, imp := range fields[1:] {
			if dep, ok := strings.CutPrefix(imp, prefix); ok && !strings.Contains(dep, "/") {
				imports = append(imports, dep)
			}
		}
		slices.Sort(imports)
		want[name] = imports
	}

	src, err := os.ReadFile("src/features/docs/components/codebase/graph.ts")
	if err != nil {
		t.Fatal(err)
	}
	entry := regexp.MustCompile(`\{\s*name:\s*"([a-z0-9]+)",\s*imports:\s*\[([^\]]*)\]`)
	quoted := regexp.MustCompile(`"([a-z0-9]+)"`)
	got := map[string][]string{}
	for _, m := range entry.FindAllStringSubmatch(string(src), -1) {
		imports := []string{}
		for _, q := range quoted.FindAllStringSubmatch(m[2], -1) {
			imports = append(imports, q[1])
		}
		slices.Sort(imports)
		got[m[1]] = imports
	}

	for _, name := range slices.Sorted(maps.Keys(want)) {
		if g, ok := got[name]; !ok {
			t.Errorf("graph.ts lacks package %s (imports %v)", name, want[name])
		} else if !slices.Equal(g, want[name]) {
			t.Errorf("graph.ts: %s imports %v, go list says %v", name, g, want[name])
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("graph.ts lists %s, which is not an internal package", name)
		}
	}
}
