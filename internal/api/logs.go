package api

import (
	"context"
	"net/http"

	"github.com/kyledickey/shed/internal/logtail"
)

// Logs streams shed's own log. *logtail.Tail implements it.
type Logs interface {
	Follow(ctx context.Context, emit func(line string))
}

var _ Logs = (*logtail.Tail)(nil)

// shedLogs streams shed's recent log lines, then follows new ones until the
// client goes away.
func (s *Server) shedLogs(w http.ResponseWriter, r *http.Request) error {
	stream := startSSE(w, r)
	defer stream.close()
	s.logs.Follow(r.Context(), func(line string) { stream.send("log", line) })
	return nil
}
