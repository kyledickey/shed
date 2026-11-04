package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/kyledickey/shed/internal/control"
	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/store"
)

// runtimeLogTail is how many lines of runtime log are replayed.
const runtimeLogTail = 500

func (s *Server) listDeployments(w http.ResponseWriter, r *http.Request) error {
	page, err := pageParams(r)
	if err != nil {
		return err
	}
	deps, err := s.control.Deployments(r.Context(), r.PathValue("id"), page)
	if err != nil {
		return err
	}
	out := deploymentPageJSON{Deployments: make([]deploymentJSON, 0, len(deps.Deployments)), Next: nextCursor(deps.Next)}
	for _, d := range deps.Deployments {
		out.Deployments = append(out.Deployments, toDeployment(d))
	}
	return writeJSON(w, http.StatusOK, out)
}

// pageParams reads the before and limit parameters of a list of
// deployments or backups. A limit above control.MaxPage is lowered to it.
func pageParams(r *http.Request) (control.Page, error) {
	q := r.URL.Query()
	limit, err := positiveInt(q.Get("limit"), "limit")
	if err != nil {
		return control.Page{}, err
	}
	return control.Page{Before: q.Get("before"), Limit: int(min(limit, control.MaxPage))}, nil
}

// nextCursor returns the JSON form of a page's next cursor: null on the last
// page.
func nextCursor(next string) *string {
	if next == "" {
		return nil
	}
	return &next
}

// createDeployment deploys the head of the service's branch, or its image.
func (s *Server) createDeployment(w http.ResponseWriter, r *http.Request) error {
	dep, err := s.control.Deploy(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusCreated, toDeployment(dep))
}

func (s *Server) getDeployment(w http.ResponseWriter, r *http.Request) error {
	dep, err := s.control.Deployment(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toDeployment(dep))
}

func (s *Server) redeploy(w http.ResponseWriter, r *http.Request) error {
	dep, err := s.control.Redeploy(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusCreated, toDeployment(dep))
}

func (s *Server) cancelDeployment(w http.ResponseWriter, r *http.Request) error {
	dep, err := s.control.CancelDeployment(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toDeployment(dep))
}

// deploymentLogs streams a deployment's build log and status changes.
func (s *Server) deploymentLogs(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if _, err := s.control.Deployment(r.Context(), id); err != nil {
		return err
	}
	stream := startSSE(w, r)
	defer stream.close()
	err := s.control.FollowBuildLog(r.Context(), id,
		func(line string) { stream.send("log", line) },
		func(st store.DeploymentStatus) {
			data, _ := json.Marshal(map[string]store.DeploymentStatus{"status": st})
			stream.send("status", string(data))
		})
	if err != nil {
		if r.Context().Err() == nil {
			s.log.Error("follow build log", "deployment", id, "err", err)
		}
		return nil
	}
	stream.send("end", "")
	return nil
}

// serviceLogs streams the runtime log of a service's active container. The
// stream ends when the container stops, or at once if there is none.
func (s *Server) serviceLogs(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if _, err := s.control.ServiceRecord(r.Context(), id); err != nil {
		return err
	}
	stream := startSSE(w, r)
	defer stream.close()
	lines := &lineWriter{emit: func(line string) { stream.send("log", line) }}
	err := s.control.FollowRuntimeLogs(r.Context(), id, runtimeLogTail, lines)
	lines.flush()
	if r.Context().Err() != nil {
		return nil
	}
	if err != nil && !errors.Is(err, deploy.ErrNoContainer) {
		s.log.Warn("follow runtime log", "service", id, "err", err)
	}
	stream.send("end", "")
	return nil
}
