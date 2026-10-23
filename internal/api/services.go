package api

import (
	"context"
	"net/http"

	"github.com/kyledickey/shed/internal/control"
)

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) error {
	projects, err := s.control.ListProjects(r.Context())
	if err != nil {
		return err
	}
	out := make([]projectJSON[serviceSummaryJSON], 0, len(projects))
	for _, p := range projects {
		summaries := make([]serviceSummaryJSON, 0, len(p.Services))
		for _, svc := range p.Services {
			summaries = append(summaries, serviceSummaryJSON(svc))
		}
		out = append(out, projectJSON[serviceSummaryJSON]{
			ID: p.ID, Name: p.Name, CreatedAt: p.CreatedAt, Services: summaries,
		})
	}
	return writeJSON(w, http.StatusOK, out)
}

type projectRequest struct {
	Name string `json:"name"`
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) error {
	var req projectRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	p, err := s.control.CreateProject(r.Context(), req.Name)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusCreated, toProject(p))
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) error {
	p, err := s.control.Project(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toProject(p))
}

func (s *Server) renameProject(w http.ResponseWriter, r *http.Request) error {
	var req projectRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	p, err := s.control.RenameProject(r.Context(), r.PathValue("id"), req.Name)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toProject(p))
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) error {
	if err := s.control.DeleteProject(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

type newServiceRequest struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	Image  string `json:"image"`
}

// createService creates a service and starts its first deployment.
func (s *Server) createService(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.control.ProjectRecord(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	var req newServiceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	svc, err := s.control.CreateService(r.Context(), r.PathValue("id"), control.NewService(req))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusCreated, toService(svc))
}

func (s *Server) getService(w http.ResponseWriter, r *http.Request) error {
	svc, err := s.control.Service(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toService(svc))
}

type servicePatch struct {
	Repo            *string  `json:"repo"`
	Branch          *string  `json:"branch"`
	RootDir         *string  `json:"rootDir"`
	Image           *string  `json:"image"`
	DockerfilePath  *string  `json:"dockerfilePath"`
	StartCommand    *string  `json:"startCommand"`
	Port            *int     `json:"port"`
	HealthcheckPath *string  `json:"healthcheckPath"`
	PublicPort      *int     `json:"publicPort"`
	CPULimit        *float64 `json:"cpuLimit"`
	MemoryLimit     *int64   `json:"memoryLimit"`
	AutoDeploy      *bool    `json:"autoDeploy"`
	WaitForCI       *bool    `json:"waitForCi"`
}

// patchService saves settings; they apply from the next deployment.
func (s *Server) patchService(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.control.ServiceRecord(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	var p servicePatch
	if err := decodeJSON(w, r, &p); err != nil {
		return err
	}
	svc, err := s.control.UpdateService(r.Context(), r.PathValue("id"), control.ServicePatch(p))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toService(svc))
}

func (s *Server) deleteService(w http.ResponseWriter, r *http.Request) error {
	if err := s.control.DeleteService(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// controlService returns a handler that applies op, such as stopping, to a
// service and responds with the updated service.
func (s *Server) controlService(op func(Control, context.Context, string) error) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		if err := op(s.control, r.Context(), r.PathValue("id")); err != nil {
			return err
		}
		return s.getService(w, r)
	}
}

func (s *Server) getVariables(w http.ResponseWriter, r *http.Request) error {
	vars, err := s.control.Variables(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, vars)
}

func (s *Server) getResolvedVariables(w http.ResponseWriter, r *http.Request) error {
	vars, err := s.control.ResolvedVariables(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, vars)
}

func (s *Server) putVariables(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.control.ServiceRecord(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	vars := map[string]string{}
	if err := decodeJSON(w, r, &vars); err != nil {
		return err
	}
	vars, err := s.control.SetVariables(r.Context(), r.PathValue("id"), vars)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, vars)
}

// routesPendingHeader is set, to "pending", on the response to a domain
// change that is stored but not yet routed.
const routesPendingHeader = "Shed-Routes"

func markRoutesPending(w http.ResponseWriter, pending bool) {
	if pending {
		w.Header().Set(routesPendingHeader, "pending")
	}
}

func (s *Server) createDomain(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.control.ServiceRecord(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	var req struct {
		Host string `json:"host"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	d, pending, err := s.control.CreateDomain(r.Context(), r.PathValue("id"), req.Host)
	if err != nil {
		return err
	}
	markRoutesPending(w, pending)
	return writeJSON(w, http.StatusCreated, toDomain(d))
}

func (s *Server) deleteDomain(w http.ResponseWriter, r *http.Request) error {
	pending, err := s.control.DeleteDomain(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	markRoutesPending(w, pending)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) createVolume(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.control.ServiceRecord(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	var req struct {
		MountPath string `json:"mountPath"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	v, err := s.control.CreateVolume(r.Context(), r.PathValue("id"), req.MountPath)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusCreated, toVolume(v))
}

func (s *Server) deleteVolume(w http.ResponseWriter, r *http.Request) error {
	if err := s.control.DeleteVolume(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
