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
	deps, err := s.control.Deployments(r.Context(), r.PathValue("id"), control.MaxDeployments)
	if err != nil {
		return err
	}
	out := make([]deploymentJSON, 0, len(deps))
	for _, d := range deps {
		out = append(out, toDeployment(d))
	}
	return writeJSON(w, http.StatusOK, out)
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
