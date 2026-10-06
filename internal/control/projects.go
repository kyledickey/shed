package control

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/kyledickey/shed/internal/store"
)

// ListProjects returns every project with a summary of its services.
func (p *Plane) ListProjects(ctx context.Context) ([]ProjectSummary, error) {
	projects, err := p.store.Projects(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectSummary, 0, len(projects))
	for _, pr := range projects {
		services, err := p.store.Services(ctx, pr.ID)
		if err != nil {
			return nil, err
		}
		statuses, err := p.deployer.ServiceStatuses(ctx, pr.ID)
		if err != nil {
			return nil, err
		}
		summaries := make([]ServiceSummary, 0, len(services))
		for _, svc := range services {
			summaries = append(summaries, ServiceSummary{
				ID: svc.ID, Name: svc.Name, Kind: svc.Kind, Status: statuses[svc.ID],
			})
		}
		out = append(out, ProjectSummary{Project: pr, Services: summaries})
	}
	return out, nil
}

// Project returns a project with its full services.
func (p *Plane) Project(ctx context.Context, id string) (ProjectView, error) {
	pr, err := p.store.Project(ctx, id)
	if err != nil {
		return ProjectView{}, err
	}
	return p.projectView(ctx, pr)
}

// ProjectRecord returns the stored project, without the lookups of its
// services that Project makes.
func (p *Plane) ProjectRecord(ctx context.Context, id string) (store.Project, error) {
	return p.store.Project(ctx, id)
}

// CreateProject creates an empty project.
func (p *Plane) CreateProject(ctx context.Context, name string) (ProjectView, error) {
	name, err := projectName(name)
	if err != nil {
		return ProjectView{}, err
	}
	pr, err := p.store.CreateProject(ctx, name)
	if errors.Is(err, store.ErrConflict) {
		return ProjectView{}, errorf(ErrConflict, "a project named %q already exists", name)
	}
	if err != nil {
		return ProjectView{}, err
	}
	return p.projectView(ctx, pr)
}

// RenameProject renames a project.
func (p *Plane) RenameProject(ctx context.Context, id, name string) (ProjectView, error) {
	name, err := projectName(name)
	if err != nil {
		return ProjectView{}, err
	}
	err = p.store.RenameProject(ctx, id, name)
	if errors.Is(err, store.ErrConflict) {
		return ProjectView{}, errorf(ErrConflict, "a project named %q already exists", name)
	}
	if err != nil {
		return ProjectView{}, err
	}
	return p.Project(ctx, id)
}

// DeleteProject deletes a project with its services, their containers,
// volumes, and backups.
func (p *Plane) DeleteProject(ctx context.Context, id string) error {
	services, err := p.store.Services(ctx, id)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(services))
	for _, svc := range services {
		ids = append(ids, svc.ID)
	}
	resume, err := p.pauseBackups(ctx, ids...)
	if err != nil {
		return err
	}
	defer resume()
	if err := p.deployer.DeleteProject(ctx, id); err != nil {
		return err
	}
	p.forgetBackups(ctx, ids...)
	return nil
}

// projectView returns a project with its full services.
func (p *Plane) projectView(ctx context.Context, pr store.Project) (ProjectView, error) {
	services, err := p.store.Services(ctx, pr.ID)
	if err != nil {
		return ProjectView{}, err
	}
	statuses, err := p.deployer.ServiceStatuses(ctx, pr.ID)
	if err != nil {
		return ProjectView{}, err
	}
	views := make([]ServiceView, 0, len(services))
	for _, svc := range services {
		v, err := p.serviceView(ctx, svc, statuses)
		if err != nil {
			return ProjectView{}, err
		}
		views = append(views, v)
	}
	return ProjectView{Project: pr, Services: views}, nil
}

// projectName validates and normalizes a project name.
func projectName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 64 {
		return "", errorf(ErrInvalid, "project name must be 1 to 64 characters")
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
