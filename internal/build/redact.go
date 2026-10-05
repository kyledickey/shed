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
// It retains only a secret-sized tail so matches split across writes are caught.
// Callers must call Flush when done.
type Redactor struct {
	w       io.Writer
	secrets []string
	buf     []byte
	tail    int
}

// NewRedactor returns a writer that masks literal secret values across writes.
func NewRedactor(w io.Writer, secrets []string) *Redactor {
	secrets = slices.Clone(secrets)
	slices.SortFunc(secrets, func(a, b string) int { return len(b) - len(a) })
	tail := 0
	if len(secrets) > 0 {
		tail = max(0, len(secrets[0])-1)
	}
	return &Redactor{w: w, secrets: secrets, tail: tail}
}

// Write streams redacted output while retaining a bounded tail for split matches.
func (r *Redactor) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		n := min(len(p), 32*1024)
		r.buf = append(r.buf, p[:n]...)
		p = p[n:]
		written += n
		if err := r.drain(false); err != nil {
			return written, err
		}
	}
	return written, nil
}

// Flush writes any buffered partial match.
func (r *Redactor) Flush() error { return r.drain(true) }

func (r *Redactor) drain(final bool) error {
	consumed := 0
	defer func() { r.buf = append(r.buf[:0], r.buf[consumed:]...) }()
	for consumed < len(r.buf) {
		remaining := r.buf[consumed:]
		safe := len(remaining)
		if !final {
			safe -= r.tail
		}
		if safe <= 0 {
			return nil
		}
		first, length := safe, 0
		for _, secret := range r.secrets {
			if secret == "" {
				continue
			}
			if i := bytes.Index(remaining, []byte(secret)); i >= 0 && i < first {
				first, length = i, len(secret)
			}
		}
		if first > 0 {
			if _, err := r.w.Write(remaining[:first]); err != nil {
				return err
			}
			consumed += first
		}
		if length > 0 {
			if _, err := io.WriteString(r.w, redacted); err != nil {
				return err
			}
			consumed += length
		}
	}
	return nil
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
