package build

import (
	"bytes"
	"encoding/base64"
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
		password, _ := u.User.Password()
		list = append(list, u.User.String(), base64.StdEncoding.EncodeToString([]byte(u.User.Username()+":"+password)))
	}
	// Replace longest first so partial matches do not leave remnants.
	slices.SortFunc(list, func(a, b string) int { return len(b) - len(a) })
	return list
}

// Redactor is an io.Writer that replaces secrets in the text written to it.
// It works line by line so that a secret split across writes is still caught.
// Callers must call Flush when done.
type Redactor struct {
	w       io.Writer
	secrets []string
	buf     []byte
}

// NewRedactor returns a writer that masks literal secret values across writes.
func NewRedactor(w io.Writer, secrets []string) *Redactor {
	secrets = slices.Clone(secrets)
	slices.SortFunc(secrets, func(a, b string) int { return len(b) - len(a) })
	return &Redactor{w: w, secrets: secrets}
}

// Write buffers output until a complete line is available.
func (r *Redactor) Write(p []byte) (int, error) {
	r.buf = append(r.buf, p...)
	for {
		i := bytes.IndexByte(r.buf, '\n')
		if i < 0 {
			break
		}
		line := r.buf[:i+1]
		_, err := io.WriteString(r.w, r.Redact(string(line)))
		r.buf = r.buf[i+1:]
		if err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// Flush writes any buffered partial line.
func (r *Redactor) Flush() error {
	if len(r.buf) == 0 {
		return nil
	}
	_, err := io.WriteString(r.w, r.Redact(string(r.buf)))
	r.buf = nil
	return err
}

// Redact masks literal secret values in s.
func (r *Redactor) Redact(s string) string {
	for _, secret := range r.secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, redacted)
		}
	}
	return s
}
