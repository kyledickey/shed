package victoria

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// ErrSearch reports a search text [SearchFilter] cannot turn into a filter.
var ErrSearch = errors.New("invalid log search")

// Bounds on a search text.
const (
	maxSearchBytes = 512
	maxSearchTerms = 16
)

// Quote returns s as a LogsQL string literal. Inside it, nothing but the
// closing quote has a meaning, so a value cannot add filters, pipes, or
// operators to a query.
func Quote(s string) string {
	return strconv.Quote(s)
}

// StreamFilter returns the filter for the records of the streams whose
// fields equal the given values. fields is a list of name, value pairs.
func StreamFilter(fields ...string) string {
	parts := make([]string, 0, len(fields)/2)
	for i := 0; i+1 < len(fields); i += 2 {
		parts = append(parts, Quote(fields[i])+"="+Quote(fields[i+1]))
	}
	return "_stream:{" + strings.Join(parts, ",") + "}"
}

// SearchFilter turns the text a person types into a search box into LogsQL
// filters, joined by AND, or "" for blank text. The text is a list of terms
// separated by spaces: a word, a "quoted phrase" with \" and \\ escapes, or
// field:value or field:"quoted value" (a URL such as http://x stays a
// word). Every term matches a word or phrase;
// there are no operators, wildcards, pipes, or negation, and every user
// character ends up inside a quoted literal, so the filters only narrow what
// the caller already selected.
func SearchFilter(text string) (string, error) {
	if len(text) > maxSearchBytes {
		return "", fmt.Errorf("%w: at most %d bytes", ErrSearch, maxSearchBytes)
	}
	terms, err := searchTerms(text)
	if err != nil {
		return "", err
	}
	if len(terms) > maxSearchTerms {
		return "", fmt.Errorf("%w: at most %d terms", ErrSearch, maxSearchTerms)
	}
	filters := make([]string, 0, len(terms))
	for _, t := range terms {
		switch {
		case t.value == "":
			continue
		case t.field != "":
			filters = append(filters, Quote(t.field)+":"+Quote(t.value))
		default:
			filters = append(filters, Quote(t.value))
		}
	}
	return strings.Join(filters, " AND "), nil
}

type searchTerm struct {
	// field is empty for a term that searches the message.
	field, value string
}

// searchTerms splits text into terms.
func searchTerms(text string) ([]searchTerm, error) {
	var terms []searchTerm
	rest := strings.TrimSpace(text)
	for rest != "" {
		var t searchTerm
		var err error
		t, rest, err = nextTerm(rest)
		if err != nil {
			return nil, err
		}
		terms = append(terms, t)
		rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
	}
	return terms, nil
}

// nextTerm reads one term off the front of s.
func nextTerm(s string) (searchTerm, string, error) {
	if s[0] == '"' {
		v, rest, err := quoted(s)
		return searchTerm{value: v}, rest, err
	}
	end := strings.IndexFunc(s, unicode.IsSpace)
	if end < 0 {
		end = len(s)
	}
	word := s[:end]
	if i := strings.IndexByte(word, ':'); i > 0 && validField(word[:i]) && !strings.HasPrefix(word[i+1:], "//") {
		field, after := word[:i], s[i+1:]
		if strings.HasPrefix(after, `"`) {
			v, rest, err := quoted(after)
			return searchTerm{field: field, value: v}, rest, err
		}
		return searchTerm{field: field, value: word[i+1:]}, s[end:], nil
	}
	return searchTerm{value: word}, s[end:], nil
}

// quoted reads a quoted phrase off the front of s, which starts with a quote.
func quoted(s string) (value, rest string, err error) {
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\' && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\'):
			b.WriteByte(s[i+1])
			i++
		case c == '"':
			return b.String(), s[i+1:], nil
		default:
			b.WriteByte(c)
		}
	}
	return "", "", fmt.Errorf("%w: a quote is not closed", ErrSearch)
}

// validField reports whether s can name a field in a search: letters,
// digits, and _ . - only.
func validField(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !(r == '_' || r == '.' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}
