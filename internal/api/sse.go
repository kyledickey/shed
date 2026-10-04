package api

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// heartbeatInterval is how often an idle event stream sends a comment, which
// keeps proxies from closing it.
const heartbeatInterval = 15 * time.Second

// sseStream writes server-sent events. It is safe for concurrent use.
type sseStream struct {
	mu     sync.Mutex
	w      http.ResponseWriter
	rc     *http.ResponseController
	closed bool // guarded by mu; the handler has returned
	stop   chan struct{}
}

// startSSE sends the event stream headers and starts the heartbeat, which
// runs until close.
func startSSE(w http.ResponseWriter, r *http.Request) *sseStream {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	s := &sseStream{w: w, rc: http.NewResponseController(w), stop: make(chan struct{})}
	s.flush()
	go func() {
		t := time.NewTicker(heartbeatInterval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				s.write(": ping\n\n")
			case <-s.stop:
				return
			case <-r.Context().Done():
				return
			}
		}
	}()
	return s
}

// send writes one event. Each line of data becomes a data field.
func (s *sseStream) send(event, data string) {
	var b strings.Builder
	fmt.Fprintf(&b, "event: %s\n", event)
	for line := range strings.SplitSeq(data, "\n") {
		fmt.Fprintf(&b, "data: %s\n", line)
	}
	b.WriteString("\n")
	s.write(b.String())
}

func (s *sseStream) write(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if _, err := s.w.Write([]byte(msg)); err == nil {
		s.flush()
	}
}

func (s *sseStream) flush() {
	_ = s.rc.Flush() // A failed write is noticed through the request context.
}

// close stops the heartbeat. Nothing is written afterwards.
func (s *sseStream) close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	close(s.stop)
}

// lineWriter is an io.Writer that emits each complete line it is given.
type lineWriter struct {
	emit func(string)
	buf  []byte
}

func (lw *lineWriter) Write(p []byte) (int, error) {
	lw.buf = append(lw.buf, p...)
	for {
		i := bytes.IndexByte(lw.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		lw.emit(strings.TrimSuffix(string(lw.buf[:i]), "\r"))
		lw.buf = lw.buf[i+1:]
	}
}

// flush emits a final unterminated line, if any.
func (lw *lineWriter) flush() {
	if len(lw.buf) > 0 {
		lw.emit(string(lw.buf))
		lw.buf = nil
	}
}
