package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/store"
)

// deploymentsPageSize is how many deployments a service lists.
const deploymentsPageSize = 50

// runtimeLogTail is how many lines of runtime log are replayed.
const runtimeLogTail = 500

func (s *Server) listDeployments(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if _, err := s.store.Service(r.Context(), id); err != nil {
		return err
	}
	deps, err := s.store.Deployments(r.Context(), id, deploymentsPageSize)
	if err != nil {
		return err
	}
	images := make([]string, 0, len(deps))
	for _, d := range deps {
		images = append(images, d.Image)
	}
	available, err := s.deployer.AvailableImages(r.Context(), id, images)
	if err != nil {
		s.log.Warn("check deployment images", "service", id, "err", err)
		available = nil
	}
	out := make([]deploymentJSON, 0, len(deps))
	for _, d := range deps {
		j := toDeployment(d)
		if available != nil && d.Image != "" {
			ok := available[d.Image]
			j.ImageAvailable = &ok
		}
		out = append(out, j)
	}
	return writeJSON(w, http.StatusOK, out)
}

// createDeployment deploys the head of the service's branch, or its image.
func (s *Server) createDeployment(w http.ResponseWriter, r *http.Request) error {
	svc, err := s.store.Service(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	var commit deploy.Commit
	if svc.Kind == "app" && svc.Repo != "" {
		if commit, err = s.branchHead(r.Context(), svc); err != nil {
			return err
		}
	}
	// Under s.pushMu, so that a pending push cannot pass its check against
	// the latest deployment and then supersede this one.
	s.pushMu.Lock()
	dep, err := s.deployer.Deploy(r.Context(), svc.ID, store.TriggerManual, commit)
	s.pushMu.Unlock()
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusCreated, toDeployment(dep))
}

func (s *Server) getDeployment(w http.ResponseWriter, r *http.Request) error {
	dep, err := s.store.Deployment(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toDeployment(dep))
}

func (s *Server) redeploy(w http.ResponseWriter, r *http.Request) error {
	s.pushMu.Lock() // see createDeployment
	dep, err := s.deployer.Redeploy(r.Context(), r.PathValue("id"))
	s.pushMu.Unlock()
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusCreated, toDeployment(dep))
}

func (s *Server) cancelDeployment(w http.ResponseWriter, r *http.Request) error {
	dep, err := s.deployer.Cancel(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toDeployment(dep))
}

// deploymentLogs streams a deployment's build log and status changes.
func (s *Server) deploymentLogs(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if _, err := s.store.Deployment(r.Context(), id); err != nil {
		return err
	}
	stream := startSSE(w, r)
	defer stream.close()
	err := s.deployer.FollowLog(r.Context(), id,
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
	if _, err := s.store.Service(r.Context(), id); err != nil {
		return err
	}
	stream := startSSE(w, r)
	defer stream.close()
	lines := &lineWriter{emit: func(line string) { stream.send("log", line) }}
	err := s.deployer.RuntimeLogs(r.Context(), id, runtimeLogTail, lines)
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
