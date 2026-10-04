package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/kyledickey/shed/internal/store"
)

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) error {
	projects, err := s.store.Projects(r.Context())
	if err != nil {
		return err
	}
	out := make([]projectJSON[serviceSummaryJSON], 0, len(projects))
	for _, p := range projects {
		services, err := s.store.Services(r.Context(), p.ID)
		if err != nil {
			return err
		}
		statuses, err := s.deployer.ServiceStatuses(r.Context(), p.ID)
		if err != nil {
			return err
		}
		summaries := make([]serviceSummaryJSON, 0, len(services))
		for _, svc := range services {
			summaries = append(summaries, serviceSummaryJSON{
				ID: svc.ID, Name: svc.Name, Kind: svc.Kind, Status: statuses[svc.ID],
			})
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
	name, err := projectName(req.Name)
	if err != nil {
		return err
	}
	p, err := s.store.CreateProject(r.Context(), name)
	if errors.Is(err, store.ErrConflict) {
		return errorf(http.StatusConflict, "a project named %q already exists", name)
	}
	if err != nil {
		return err
	}
	view, err := s.projectView(r.Context(), p)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusCreated, view)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) error {
	p, err := s.store.Project(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	view, err := s.projectView(r.Context(), p)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, view)
}

func (s *Server) renameProject(w http.ResponseWriter, r *http.Request) error {
	var req projectRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	name, err := projectName(req.Name)
	if err != nil {
		return err
	}
	id := r.PathValue("id")
	err = s.store.RenameProject(r.Context(), id, name)
	if errors.Is(err, store.ErrConflict) {
		return errorf(http.StatusConflict, "a project named %q already exists", name)
	}
	if err != nil {
		return err
	}
	return s.getProject(w, r)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) error {
	if err := s.deployer.DeleteProject(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// projectView returns a project with its full services.
func (s *Server) projectView(ctx context.Context, p store.Project) (projectJSON[serviceJSON], error) {
	services, err := s.store.Services(ctx, p.ID)
	if err != nil {
		return projectJSON[serviceJSON]{}, err
	}
	statuses, err := s.deployer.ServiceStatuses(ctx, p.ID)
	if err != nil {
		return projectJSON[serviceJSON]{}, err
	}
	views := make([]serviceJSON, 0, len(services))
	for _, svc := range services {
		v, err := s.serviceView(ctx, svc, statuses)
		if err != nil {
			return projectJSON[serviceJSON]{}, err
		}
		views = append(views, v)
	}
	return projectJSON[serviceJSON]{ID: p.ID, Name: p.Name, CreatedAt: p.CreatedAt, Services: views}, nil
}

// projectName validates and normalizes a project name.
func projectName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 64 {
		return "", errorf(http.StatusBadRequest, "project name must be 1 to 64 characters")
	}
	return name, nil
}

// slug turns a project name into a DNS label fragment: lowercase letters and
// digits separated by single hyphens.
func slug(name string) string {
	var b strings.Builder
	hyphen := false
	for _, c := range strings.ToLower(name) {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			if hyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(c)
			hyphen = false
		} else {
			hyphen = true
		}
	}
	return b.String()
}
