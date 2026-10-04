package docker

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestExitError(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &ExitError{Code: 3})
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != 3 {
		t.Fatalf("errors.As(%v) = %v, want code 3", err, ee)
	}
	if got, want := ee.Error(), "docker: exit status 3"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

type failingReader struct{ err error }

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }

func TestErrReader(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name string
		r    io.Reader
		want error
	}{
		{"eof is not an error", strings.NewReader("data"), nil},
		{"read failure is kept", failingReader{boom}, boom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			er := &errReader{r: tt.r}
			_, _ = io.Copy(io.Discard, er)
			if !errors.Is(er.err, tt.want) {
				t.Errorf("err = %v, want %v", er.err, tt.want)
			}
		})
	}
}
