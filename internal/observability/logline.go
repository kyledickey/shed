package observability

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// maxLineBytes is where a line of container output is cut.
	maxLineBytes = 16 << 10
	// maxLineFields bounds the fields kept from a JSON line.
	maxLineFields = 64
)

// logLine is one line of a container's output, as Docker timestamped it.
type logLine struct {
	time   time.Time
	stream string // stdout or stderr
	text   string
}

// parsedLine is a line's message, level, and fields, masked.
type parsedLine struct {
	msg    string
	level  string
	fields map[string]string
}

// Keys of a JSON line that become the message and the level.
var (
	msgKeys   = []string{"msg", "message"}
	levelKeys = []string{"level", "severity", "lvl"}
)

// levelTokens maps a line's leading token to its level.
var levelTokens = map[string]string{
	"ERROR": "error", "WARN": "warn", "WARNING": "warn", "INFO": "info", "DEBUG": "debug",
}

// parseLine cuts text at maxLineBytes and reads it: a JSON object keeps its
// top-level fields (the first maxLineFields by name), with msg or message
// as the message and level, severity, or lvl as the level; other text is
// the message, its level a leading ERROR, WARN, INFO, or DEBUG token. The
// message and every field value are masked.
func parseLine(text string, mask func(string) string) parsedLine {
	text = cut(text, maxLineBytes)
	if p, ok := parseJSONLine(text, mask); ok {
		return p
	}
	return parsedLine{msg: mask(text), level: leadingLevel(text)}
}

func parseJSONLine(text string, mask func(string) string) (parsedLine, bool) {
	if !strings.HasPrefix(strings.TrimSpace(text), "{") {
		return parsedLine{}, false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		return parsedLine{}, false
	}
	values := make(map[string]string, len(obj))
	for k, raw := range obj {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			s = string(raw)
		}
		values[k] = s
	}
	p := parsedLine{fields: make(map[string]string)}
	if k, ok := firstKey(values, msgKeys); ok {
		p.msg = values[k]
		delete(values, k)
	}
	if k, ok := firstKey(values, levelKeys); ok {
		p.level = strings.ToLower(values[k])
		delete(values, k)
	}
	if p.msg == "" {
		p.msg = text
	}
	p.msg = mask(p.msg)
	keys := slices.Sorted(maps.Keys(values))
	for _, k := range keys[:min(len(keys), maxLineFields)] {
		p.fields[k] = mask(values[k])
	}
	return p, true
}

func firstKey(values map[string]string, keys []string) (string, bool) {
	for _, k := range keys {
		if _, ok := values[k]; ok {
			return k, true
		}
	}
	return "", false
}

// leadingLevel returns the level named by text's first word, such as
// "ERROR", "[warn]", or "INFO:", or "".
func leadingLevel(text string) string {
	word, _, _ := strings.Cut(strings.TrimSpace(text), " ")
	word = strings.Trim(word, "[]:")
	return levelTokens[strings.ToUpper(word)]
}

// cut shortens s to at most n bytes, at a rune boundary.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
