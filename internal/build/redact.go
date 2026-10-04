package build

import (
	"bytes"
	"io"
	"net/url"
	"slices"
	"strings"
)

const redacted = "***"

// secrets returns the substrings of a clone URL that must not appear in logs.
func secrets(repoURL string) []string {
	list := []string{repoURL}
	if u, err := url.Parse(repoURL); err == nil && u.User != nil {
		if pw, ok := u.User.Password(); ok {
			list = append(list, pw)
		} else if name := u.User.Username(); name != "" {
			list = append(list, name) // Token as the user name.
		}
		list = append(list, u.User.String())
	}
	// Replace longest first so partial matches do not leave remnants.
	slices.SortFunc(list, func(a, b string) int { return len(b) - len(a) })
	return list
}

// redactor is an io.Writer that replaces secrets in the text written to it.
// It works line by line so that a secret split across writes is still caught.
// Callers must call Flush when done.
type redactor struct {
	w       io.Writer
	secrets []string
	buf     []byte
}

func newRedactor(w io.Writer, secrets []string) *redactor {
	return &redactor{w: w, secrets: secrets}
}

func (r *redactor) Write(p []byte) (int, error) {
	r.buf = append(r.buf, p...)
	for {
		i := bytes.IndexByte(r.buf, '\n')
		if i < 0 {
			break
		}
		line := r.buf[:i+1]
		_, err := io.WriteString(r.w, r.redact(string(line)))
		r.buf = r.buf[i+1:]
		if err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// Flush writes any buffered partial line.
func (r *redactor) Flush() error {
	if len(r.buf) == 0 {
		return nil
	}
	_, err := io.WriteString(r.w, r.redact(string(r.buf)))
	r.buf = nil
	return err
}

func (r *redactor) redact(s string) string {
	for _, secret := range r.secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, redacted)
		}
	}
	return s
}
