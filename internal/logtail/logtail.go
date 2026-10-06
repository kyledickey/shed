// Package logtail keeps the most recent lines of a log in memory and streams
// new ones to followers.
//
// A [Tail] is an io.Writer, so it can sit next to the log file behind an
// io.MultiWriter. Writes never block on followers: a follower that falls
// behind misses lines rather than slowing the logger down.
package logtail

import (
	"bytes"
	"context"
	"sync"
)

// followBuffer is how many lines a follower may fall behind before lines are
// dropped for it.
const followBuffer = 256

// Tail is an io.Writer that remembers the last lines written to it. It is
// safe for concurrent use.
type Tail struct {
	mu      sync.Mutex
	lines   []string // ring of up to max lines, oldest at next once full
	next    int
	max     int
	partial []byte // an unterminated last line
	subs    map[chan string]struct{}
}

// New returns a Tail that keeps the last max lines.
func New(max int) *Tail {
	return &Tail{max: max, subs: make(map[chan string]struct{})}
}

// Write splits p into lines, keeps them, and sends them to followers. It
// always succeeds.
func (t *Tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(p)
	for {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			t.partial = append(t.partial, p...)
			return n, nil
		}
		line := string(append(t.partial, p[:i]...))
		t.partial = t.partial[:0]
		p = p[i+1:]
		t.add(line)
	}
}

func (t *Tail) add(line string) {
	if len(t.lines) < t.max {
		t.lines = append(t.lines, line)
	} else {
		t.lines[t.next] = line
		t.next = (t.next + 1) % t.max
	}
	for ch := range t.subs {
		select {
		case ch <- line:
		default:
		}
	}
}

// Lines returns up to the last n kept lines, oldest first.
func (t *Tail) Lines(n int) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	all := append(append([]string(nil), t.lines[t.next:]...), t.lines[:t.next]...)
	return all[len(all)-min(max(n, 0), len(all)):]
}

// Follow calls emit with every kept line, oldest first, then with each new
// line until ctx is done. emit is called from Follow's goroutine only.
func (t *Tail) Follow(ctx context.Context, emit func(line string)) {
	ch := make(chan string, followBuffer)
	t.mu.Lock()
	history := append(append([]string(nil), t.lines[t.next:]...), t.lines[:t.next]...)
	t.subs[ch] = struct{}{}
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		delete(t.subs, ch)
		t.mu.Unlock()
	}()

	for _, line := range history {
		emit(line)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case line := <-ch:
			emit(line)
		}
	}
}
