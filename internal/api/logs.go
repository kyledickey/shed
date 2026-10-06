package api

import "net/http"

// shedLogs streams shed's recent log lines, then follows new ones until the
// client goes away.
func (s *Server) shedLogs(w http.ResponseWriter, r *http.Request) error {
	stream := startSSE(w, r)
	defer stream.close()
	s.control.FollowShedLog(r.Context(), func(line string) { stream.send("log", line) })
	return nil
}
