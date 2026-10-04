// Package vars resolves Railway-style variable references.
//
// A variable value may contain references of the form ${{ KEY }}, which
// expands to another variable of the same service, or ${{ service.KEY }},
// which expands to a variable of another service. Whitespace inside the braces
// is optional.
package vars

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// refPattern matches a reference and captures its body.
var refPattern = regexp.MustCompile(`\$\{\{\s*([^\s{}]+)\s*\}\}`)

// Resolve returns the variables of service self with all references expanded.
//
// all maps service name to the raw variables of that service. A reference to a
// missing service or variable expands to the empty string. Resolve returns an
// error if references form a cycle.
func Resolve(self string, all map[string]map[string]string) (map[string]string, error) {
	r := &resolver{
		all:      all,
		resolved: make(map[ref]string),
		visiting: make(map[ref]bool),
	}
	keys := make([]string, 0, len(all[self]))
	for k := range all[self] {
		keys = append(keys, k)
	}
	slices.Sort(keys) // Deterministic cycle errors.

	out := make(map[string]string, len(keys))
	for _, k := range keys {
		v, err := r.value(ref{self, k})
		if err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, nil
}

// ref identifies one variable of one service.
type ref struct{ service, key string }

func (r ref) String() string { return r.service + "." + r.key }

type resolver struct {
	all      map[string]map[string]string
	resolved map[ref]string
	visiting map[ref]bool
	stack    []ref // visiting refs in order, for cycle reporting
}

// value returns the fully expanded value of v, or "" if it does not exist.
func (r *resolver) value(v ref) (string, error) {
	if s, ok := r.resolved[v]; ok {
		return s, nil
	}
	raw, ok := r.all[v.service][v.key]
	if !ok {
		return "", nil
	}
	if r.visiting[v] {
		return "", r.cycleError(v)
	}
	r.visiting[v] = true
	r.stack = append(r.stack, v)

	s, err := r.expand(v.service, raw)
	if err != nil {
		return "", err
	}

	r.stack = r.stack[:len(r.stack)-1]
	delete(r.visiting, v)
	r.resolved[v] = s
	return s, nil
}

// expand replaces the references in raw, evaluated in the scope of service.
func (r *resolver) expand(service, raw string) (string, error) {
	var b strings.Builder
	last := 0
	for _, m := range refPattern.FindAllStringSubmatchIndex(raw, -1) {
		b.WriteString(raw[last:m[0]])
		last = m[1]

		target := ref{service: service, key: raw[m[2]:m[3]]}
		if svc, key, ok := strings.Cut(target.key, "."); ok {
			target = ref{service: svc, key: key}
		}
		s, err := r.value(target)
		if err != nil {
			return "", err
		}
		b.WriteString(s)
	}
	b.WriteString(raw[last:])
	return b.String(), nil
}

// cycleError describes the cycle that closes at v.
func (r *resolver) cycleError(v ref) error {
	i := slices.Index(r.stack, v)
	names := make([]string, 0, len(r.stack)-i+1)
	for _, s := range r.stack[i:] {
		names = append(names, s.String())
	}
	names = append(names, v.String())
	return fmt.Errorf("vars: reference cycle: %s", strings.Join(names, " -> "))
}
