package vars

import (
	"fmt"
	"maps"
	"strings"
	"testing"
)

type vs = map[string]string

func TestResolve(t *testing.T) {
	tests := []struct {
		name string
		self string
		all  map[string]vs
		want vs
	}{
		{
			name: "plain values untouched",
			self: "web",
			all:  map[string]vs{"web": {"A": "hello", "B": "$HOME ${A} {{A}} $${{"}},
			want: vs{"A": "hello", "B": "$HOME ${A} {{A}} $${{"},
		},
		{
			name: "same service",
			self: "web",
			all:  map[string]vs{"web": {"A": "x", "B": "${{A}}-y"}},
			want: vs{"A": "x", "B": "x-y"},
		},
		{
			name: "whitespace optional",
			self: "web",
			all:  map[string]vs{"web": {"A": "x", "B": "${{ A }}${{A}}${{   A}}${{A  }}"}},
			want: vs{"A": "x", "B": "xxxx"},
		},
		{
			name: "other service",
			self: "web",
			all: map[string]vs{
				"web": {"DB": "${{ db.URL }}"},
				"db":  {"URL": "pg://host"},
			},
			want: vs{"DB": "pg://host"},
		},
		{
			name: "other service resolved in its own scope",
			self: "web",
			all: map[string]vs{
				"web": {"DB": "${{ db.URL }}", "PASSWORD": "wrong"},
				"db":  {"URL": "pg://u:${{PASSWORD}}@${{ HOST }}", "PASSWORD": "secret", "HOST": "db"},
			},
			want: vs{"DB": "pg://u:secret@db", "PASSWORD": "wrong"},
		},
		{
			name: "chain across services",
			self: "a",
			all: map[string]vs{
				"a": {"X": "${{ b.X }}"},
				"b": {"X": "${{ c.X }}!"},
				"c": {"X": "end"},
			},
			want: vs{"X": "end!"},
		},
		{
			name: "multiple references in one value",
			self: "web",
			all: map[string]vs{
				"web": {"URL": "${{ u }}:${{ p }}@${{ db.HOST }}:${{ db.PORT }}", "u": "me", "p": "pw"},
				"db":  {"HOST": "db", "PORT": "5432"},
			},
			want: vs{"URL": "me:pw@db:5432", "u": "me", "p": "pw"},
		},
		{
			name: "same reference used twice",
			self: "web",
			all:  map[string]vs{"web": {"A": "1", "B": "${{A}}${{A}}"}},
			want: vs{"A": "1", "B": "11"},
		},
		{
			name: "missing variable",
			self: "web",
			all:  map[string]vs{"web": {"A": "a${{ NOPE }}b"}},
			want: vs{"A": "ab"},
		},
		{
			name: "missing service",
			self: "web",
			all:  map[string]vs{"web": {"A": "a${{ ghost.KEY }}b"}},
			want: vs{"A": "ab"},
		},
		{
			name: "malformed references untouched",
			self: "web",
			all:  map[string]vs{"web": {"A": "${{}} ${{ }} ${{ a b }} ${{A"}},
			want: vs{"A": "${{}} ${{ }} ${{ a b }} ${{A"},
		},
		{
			name: "service split at first dot",
			self: "web",
			all: map[string]vs{
				"web": {"A": "${{ svc.a.b }}"},
				"svc": {"a.b": "no", "b": "yes"},
			},
			want: vs{"A": "no"},
		},
		{
			name: "unknown self",
			self: "ghost",
			all:  map[string]vs{"web": {"A": "1"}},
			want: vs{},
		},
		{
			name: "diamond is not a cycle",
			self: "web",
			all: map[string]vs{
				"web": {"A": "${{ B }}${{ C }}", "B": "${{ D }}", "C": "${{ D }}", "D": "d"},
			},
			want: vs{"A": "dd", "B": "d", "C": "d", "D": "d"},
		},
		{
			name: "mutual references between services without cycle",
			self: "a",
			all: map[string]vs{
				"a": {"X": "${{ b.Y }}", "Z": "z"},
				"b": {"Y": "${{ a.Z }}"},
			},
			want: vs{"X": "z", "Z": "z"},
		},
		{
			name: "empty",
			self: "web",
			all:  map[string]vs{"web": {}},
			want: vs{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.self, tt.all)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("Resolve() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveDoesNotMutateInput(t *testing.T) {
	all := map[string]vs{"web": {"A": "x", "B": "${{A}}"}}
	if _, err := Resolve("web", all); err != nil {
		t.Fatal(err)
	}
	if all["web"]["B"] != "${{A}}" {
		t.Errorf("input mutated: %q", all["web"]["B"])
	}
}

func TestResolveCycle(t *testing.T) {
	tests := []struct {
		name string
		self string
		all  map[string]vs
		want []string // substrings of the error
	}{
		{
			name: "self reference",
			self: "web",
			all:  map[string]vs{"web": {"A": "${{ A }}"}},
			want: []string{"web.A -> web.A"},
		},
		{
			name: "two variables",
			self: "web",
			all:  map[string]vs{"web": {"A": "${{ B }}", "B": "${{ A }}"}},
			want: []string{"web.A -> web.B -> web.A"},
		},
		{
			name: "across services",
			self: "a",
			all: map[string]vs{
				"a": {"X": "${{ b.Y }}"},
				"b": {"Y": "${{ a.X }}"},
			},
			want: []string{"a.X -> b.Y -> a.X"},
		},
		{
			name: "cycle reachable through a prefix",
			self: "a",
			all: map[string]vs{
				"a": {"X": "${{ b.P }}"},
				"b": {"P": "${{ Q }}", "Q": "${{ R }}", "R": "${{ Q }}"},
			},
			want: []string{"b.Q -> b.R -> b.Q"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.self, tt.all)
			if err == nil {
				t.Fatalf("Resolve() = %q, want error", got)
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not contain %q", err, w)
				}
			}
		})
	}
}

func TestResolveLimits(t *testing.T) {
	tests := []struct {
		name   string
		values vs
	}{
		{"raw value", vs{"A": strings.Repeat("x", maxValueBytes+1)}},
		{"expansion", vs{"A": strings.Repeat("x", maxValueBytes/2+1), "B": "${{A}}${{A}}"}},
	}
	depth := vs{}
	for i := 0; i <= maxReferenceDepth; i++ {
		depth[fmt.Sprintf("V%03d", i)] = fmt.Sprintf("${{V%03d}}", i+1)
	}
	tests = append(tests, struct {
		name   string
		values vs
	}{"depth", depth})
	total := vs{}
	for i := 0; i <= maxResolvedBytes/maxValueBytes; i++ {
		total[fmt.Sprint(i)] = strings.Repeat("x", maxValueBytes)
	}
	tests = append(tests, struct {
		name   string
		values vs
	}{"total", total})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Resolve("web", map[string]vs{"web": tt.values}); err == nil {
				t.Fatal("Resolve accepted oversized input")
			}
		})
	}
	if _, err := Resolve("web", map[string]vs{"web": {"A": strings.Repeat("x", maxValueBytes)}}); err != nil {
		t.Fatal(err)
	}
}
