package victoria

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

func TestSearchFilter(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"blank", "   ", ""},
		{"word", "timeout", `"timeout"`},
		{"words", "connection  refused", `"connection" AND "refused"`},
		{"phrase", `"connection refused" db`, `"connection refused" AND "db"`},
		{"escaped quote", `"say \"hi\""`, `"say \"hi\""`},
		{"escaped backslash", `"a\\b"`, `"a\\b"`},
		{"field", "level:error", `"level":"error"`},
		{"field phrase", `path:"/api/v1 x"`, `"path":"/api/v1 x"`},
		{"dotted field", "shed.source:otlp", `"shed.source":"otlp"`},
		{"not a field", "http://x:80", `"http://x:80"`},
		{"empty value", "level: x", `"x"`},
		{"unicode", "ünï 日本", `"ünï" AND "日本"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SearchFilter(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("SearchFilter(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

func TestSearchFilterErrors(t *testing.T) {
	for name, in := range map[string]string{
		"open quote":       `"abc`,
		"open field quote": `f:"abc`,
		"too long":         strings.Repeat("a", maxSearchBytes+1),
		"too many terms":   strings.Repeat("a ", maxSearchTerms+1),
	} {
		if _, err := SearchFilter(in); !errors.Is(err, ErrSearch) {
			t.Errorf("%s: err = %v, want ErrSearch", name, err)
		}
	}
}

// literal matches one LogsQL string literal.
var literal = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)

// skeleton replaces every string literal of a filter by Q.
func skeleton(filter string) string {
	return literal.ReplaceAllString(filter, "Q")
}

// TestSearchFilterCannotEscape feeds hostile searches and checks that, once
// the string literals are blanked, nothing but the fixed filter skeleton
// remains: every character of the input is inside a literal, so no pipe,
// parenthesis, operator, or stream filter can come from it.
func TestSearchFilterCannotEscape(t *testing.T) {
	attacks := []string{
		`x" OR "a"="a`,
		`x") OR ("1"="1`,
		`foo | delete`,
		`foo | stats count()`,
		`a or b`,
		`a OR b AND NOT c`,
		`) OR (`,
		`_stream:{"shed.service.id"="other"}`,
		`_stream:{shed.service.id=other}`,
		`"_stream":"x"`,
		`{shed.service.id="other"}`,
		`* | limit 1000000`,
		`-foo !bar *`,
		`"x\" OR \"y"`,
		`f:"x\" OR \"y"`,
		`"unterminated\\" | drop _msg`,
		"x\nOR\ny",
		`level:error or service:other`,
		"\x00 \" \\ ' ` ( ) { } | ~ ! = < > ;",
	}
	allowed := regexp.MustCompile(`^(Q(:Q)?)?( AND Q(:Q)?)*$`)
	for _, in := range attacks {
		got, err := SearchFilter(in)
		if err != nil {
			continue
		}
		if sk := skeleton(got); !allowed.MatchString(sk) {
			t.Errorf("SearchFilter(%q) = %s; skeleton %q has syntax of its own", in, got, sk)
		}
		if strings.Count(got, "_stream") > 0 && !strings.Contains(got, `"_stream`) {
			t.Errorf("SearchFilter(%q) = %s leaks an unquoted _stream", in, got)
		}
	}
}

func TestStreamFilter(t *testing.T) {
	got := StreamFilter("shed.service.id", `a"b`, "shed.source", "edge")
	if want := `_stream:{"shed.service.id"="a\"b","shed.source"="edge"}`; got != want {
		t.Errorf("StreamFilter = %s, want %s", got, want)
	}
	if sk := skeleton(got); sk != `_stream:{Q=Q,Q=Q}` {
		t.Errorf("skeleton %s", sk)
	}
}
