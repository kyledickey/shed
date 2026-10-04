package deploy

import (
	"bytes"
	"context"
	"fmt"
	"github.com/kyledickey/shed/internal/build"
	"io"
	"strings"
	"sync"
	"time"
)

// syncWriter serializes writes to w. Each pipeline log line is a single
// Write, so lines from concurrent writers never interleave.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// maxLine bounds how much of an unterminated line lineWriter buffers.
const maxLine = 64 << 10

// lineWriter copies container output to out a whole line at a time, dropping
// the timestamp Docker puts in front of each line.
type lineWriter struct {
	out io.Writer
	buf []byte
}

func (l *lineWriter) Write(p []byte) (int, error) {
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		l.emit(l.buf[:i])
		l.buf = l.buf[i+1:]
	}
	if len(l.buf) > maxLine {
		l.Flush()
	}
	return len(p), nil
}

// Flush writes a buffered unterminated line.
func (l *lineWriter) Flush() {
	if len(l.buf) > 0 {
		l.emit(l.buf)
		l.buf = nil
	}
}

func (l *lineWriter) emit(b []byte) {
	line := strings.TrimRight(string(b), "\r")
	if ts, rest, ok := strings.Cut(line, " "); ok {
		if _, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			line = rest
		}
	}
	if strings.HasPrefix(line, "==> ") {
		line = " " + line // Not a pipeline step heading.
	}
	fmt.Fprintln(l.out, line)
}

// follower copies a container's output into the build log in the background.
type follower struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// follow starts copying the output of container id, from its start, into the
// build log.
func (j *job) follow(ctx context.Context, id string) *follower {
	ctx, cancel := context.WithCancel(ctx)
	f := &follower{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(f.done)
		lw := &lineWriter{out: j.out}
		redactor := build.NewRedactor(lw, j.secrets)
		err := j.docker.Logs(ctx, id, -1, true, redactor)
		if flushErr := redactor.Flush(); err == nil {
			err = flushErr
		}
		lw.Flush()
		if err != nil && ctx.Err() == nil {
			j.printf("Reading container output: %v", err)
		}
	}()
	return f
}

// drain waits up to timeout for the output to end on its own, as it does once
// the container has exited.
func (f *follower) drain(timeout time.Duration) {
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-f.done:
	case <-t.C:
	}
}

// stop stops copying and waits until everything copied has been written. It
// may be called more than once.
func (f *follower) stop() {
	f.cancel()
	<-f.done
}
