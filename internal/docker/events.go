package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
)

// Event is something the daemon reports about a container or an image.
type Event struct {
	Time time.Time
	// Type is "container" or "image".
	Type string
	// Action is start, die, kill, restart, oom, exec_create, or pull.
	Action string
	// ID is the container ID, or the image reference of an image event.
	ID string
	// Name is the container name.
	Name string
	// Attributes are the container's labels and the daemon's own
	// attributes (name, image, exitCode, execID).
	Attributes map[string]string
	// ExitCode is the exit code of a die event.
	ExitCode int
	// ExecID and Command describe an exec_create event. Command is the
	// command line as the daemon renders it: the arguments joined by spaces,
	// without quoting.
	ExecID, Command string
}

// containerActions are the container actions Events reports; the image
// action it reports is pull.
var containerActions = []string{"start", "die", "kill", "restart", "oom", "exec_create"}

// eventFrom converts m, reporting false for events Events does not report.
func eventFrom(m events.Message) (Event, bool) {
	action := string(m.Action)
	command := ""
	if base, rest, ok := strings.Cut(action, ": "); ok {
		action, command = base, rest
	}
	switch {
	case m.Type == events.ContainerEventType:
		if !slices.Contains(containerActions, action) {
			return Event{}, false
		}
	case m.Type == events.ImageEventType && action == "pull":
	default:
		return Event{}, false
	}
	e := Event{
		Time:       time.Unix(0, m.TimeNano),
		Type:       string(m.Type),
		Action:     action,
		ID:         m.Actor.ID,
		Name:       m.Actor.Attributes["name"],
		Attributes: m.Actor.Attributes,
	}
	if m.TimeNano == 0 {
		e.Time = time.Unix(m.Time, 0)
	}
	if action == "exec_create" {
		e.ExecID, e.Command = m.Actor.Attributes["execID"], command
	}
	if action == "die" {
		e.ExitCode, _ = strconv.Atoi(m.Actor.Attributes["exitCode"])
	}
	if m.Type == events.ImageEventType {
		e.Name = m.Actor.ID
	}
	return e, true
}

// Events calls fn for each event of the daemon, in order, from since (the
// present if zero) until the stream breaks or ctx is done, and returns why.
// It reports container events (start, die, kill, restart, oom, exec_create)
// and image pulls. Containers are not filtered: Docker cannot match one of
// several labels, so callers select by Attributes.
func (c *Client) Events(ctx context.Context, since time.Time, fn func(Event)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	f := make(client.Filters)
	f.Add("type", "container", "image")
	f.Add("event", append(slices.Clone(containerActions), "pull")...)
	opts := client.EventsListOptions{Filters: f}
	if !since.IsZero() {
		opts.Since = fmt.Sprintf("%d.%09d", since.Unix(), since.Nanosecond())
	}
	res := c.api.Events(ctx, opts)
	for {
		select {
		case m := <-res.Messages:
			if e, ok := eventFrom(m); ok {
				fn(e)
			}
		case err, ok := <-res.Err:
			if !ok || errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("docker: events: %w", err)
		}
	}
}

// Bounds of the set of exec IDs that shed started itself.
const (
	ownExecTTL = 10 * time.Minute
	ownExecMax = 4096
)

// execSet remembers exec IDs for a while. The zero value is ready to use.
type execSet struct {
	mu  sync.Mutex
	ids map[string]time.Time // expiry by exec ID
	now func() time.Time
}

func (s *execSet) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// add remembers id for [ownExecTTL]. At [ownExecMax] entries it first drops
// the expired ones, then the oldest.
func (s *execSet) add(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock()
	if s.ids == nil {
		s.ids = make(map[string]time.Time)
	}
	if len(s.ids) >= ownExecMax {
		for k, exp := range s.ids {
			if !exp.After(now) {
				delete(s.ids, k)
			}
		}
	}
	if len(s.ids) >= ownExecMax {
		var oldest string
		var at time.Time
		for k, exp := range s.ids {
			if oldest == "" || exp.Before(at) {
				oldest, at = k, exp
			}
		}
		delete(s.ids, oldest)
	}
	s.ids[id] = now.Add(ownExecTTL)
}

func (s *execSet) has(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.ids[id]
	return ok && exp.After(s.clock())
}

// IsOwnExec reports whether id is an exec that [Client.Exec] started, within
// the last ten minutes. The daemon announces an exec before Exec has
// returned its ID, so a caller deciding on an event should wait a moment.
func (c *Client) IsOwnExec(id string) bool {
	return c.execs.has(id)
}
