package control

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/kyledickey/shed/internal/catalog"
	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/store"
)

var (
	// dnsLabel matches service names and each label of a host name.
	dnsLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	repoName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
)

const (
	// defaultAppPort is the port of new repo apps, which receive it as PORT.
	defaultAppPort = 8080
	// defaultCPULimit and defaultMemoryLimit are the container limits of new
	// services: one core and 1 GiB.
	defaultCPULimit    = 1
	defaultMemoryLimit = 1 << 30
	// minMemoryLimit is the smallest memory limit a service may set.
	minMemoryLimit = 64 << 20
)

// NewService describes a service to create.
type NewService struct {
	// Name is the service's DNS label, unique in its project.
	Name string
	// Kind is "app" or a database kind of the catalog, such as "postgres".
	Kind string
	// Repo and Branch are the source of a repo app; Image that of an image
	// app.
	Repo   string
	Branch string
	Image  string
}

// CreateService creates a service in a project and starts its first
// deployment. Database kinds get their template's image, port, volume, and
// variables; repo apps deploy the head of their branch.
func (p *Plane) CreateService(ctx context.Context, projectID string, req NewService) (ServiceView, error) {
	project, err := p.store.Project(ctx, projectID)
	if err != nil {
		return ServiceView{}, err
	}
	if !dnsLabel.MatchString(req.Name) {
		return ServiceView{}, errorf(ErrInvalid, "service name must be a DNS label: lowercase letters, digits, and hyphens")
	}

	svc := store.Service{
		ProjectID:   project.ID,
		Name:        req.Name,
		Kind:        req.Kind,
		CPULimit:    defaultCPULimit,
		MemoryLimit: defaultMemoryLimit,
		AutoDeploy:  true,
	}
	var commit deploy.Commit
	tmpl, isDB := catalog.Lookup(req.Kind)
	switch {
	case isDB:
		svc.Image, svc.Port = tmpl.Image, tmpl.Port
	case req.Kind != "app":
		return ServiceView{}, errorf(ErrInvalid, "unknown service kind %q", req.Kind)
	case req.Repo != "":
		svc.Repo, svc.Branch, svc.Port = req.Repo, req.Branch, defaultAppPort
		if err := validateService(svc); err != nil {
			return ServiceView{}, err
		}
		if commit, err = p.branchHead(ctx, svc); err != nil {
			return ServiceView{}, err
		}
	case req.Image != "":
		svc.Image = strings.TrimSpace(req.Image)
	default:
		return ServiceView{}, errorf(ErrInvalid, "an app needs a repository and branch, or an image")
	}

	svc, err = p.store.CreateService(ctx, svc)
	if errors.Is(err, store.ErrConflict) {
		return ServiceView{}, errorf(ErrConflict, "a service named %q already exists in this project", req.Name)
	}
	if err != nil {
		return ServiceView{}, err
	}
	if isDB {
		if err := p.applyTemplate(ctx, svc, tmpl); err != nil {
			if derr := p.store.DeleteService(ctx, svc.ID); derr != nil {
				p.log.Error("delete half-created service", "service", svc.ID, "err", derr)
			}
			return ServiceView{}, err
		}
	}
	p.pushMu.Lock()
	_, err = p.deployer.Deploy(ctx, svc.ID, store.TriggerCreate, commit)
	p.pushMu.Unlock()
	if err != nil {
		p.log.Error("start first deployment", "service", svc.ID, "err", err)
	}
	return p.withStatus(ctx, svc)
}

// applyTemplate gives a new database service its volume and variables.
func (p *Plane) applyTemplate(ctx context.Context, svc store.Service, tmpl catalog.Template) error {
	if _, err := p.store.CreateVolume(ctx, svc.ID, tmpl.MountPath); err != nil {
		return err
	}
	return p.store.SetVariables(ctx, svc.ID, tmpl.Vars())
}

// branchHead returns the commit at the head of a repo service's branch.
func (p *Plane) branchHead(ctx context.Context, svc store.Service) (deploy.Commit, error) {
	gh, err := p.requireGitHub()
	if err != nil {
		return deploy.Commit{}, err
	}
	c, err := gh.Commit(ctx, svc.Repo, svc.Branch)
	if err != nil {
		p.log.Warn("resolve branch head", "repo", svc.Repo, "branch", svc.Branch, "err", err)
		return deploy.Commit{}, errorf(ErrInvalid,
			"cannot read branch %s of %s; is the GitHub App installed on it?", svc.Branch, svc.Repo)
	}
	return deploy.Commit{SHA: c.SHA, Message: c.Message, Author: c.Author}, nil
}

// Service returns a service with its live status and resources.
func (p *Plane) Service(ctx context.Context, id string) (ServiceView, error) {
	svc, err := p.store.Service(ctx, id)
	if err != nil {
		return ServiceView{}, err
	}
	return p.withStatus(ctx, svc)
}

// ServiceRecord returns the stored settings of a service, without the
// lookups of its live state that Service makes.
func (p *Plane) ServiceRecord(ctx context.Context, id string) (store.Service, error) {
	return p.store.Service(ctx, id)
}

// ServicePatch holds the settings to change; nil fields are kept.
type ServicePatch struct {
	Repo            *string
	Branch          *string
	RootDir         *string
	Image           *string
	DockerfilePath  *string
	StartCommand    *string
	Port            *int
	HealthcheckPath *string
	PublicPort      *int
	CPULimit        *float64
	MemoryLimit     *int64
	AutoDeploy      *bool
	WaitForCI       *bool
}

// UpdateService saves settings of a service. They apply from its next
// deployment.
func (p *Plane) UpdateService(ctx context.Context, id string, patch ServicePatch) (ServiceView, error) {
	svc, err := p.store.Service(ctx, id)
	if err != nil {
		return ServiceView{}, err
	}
	set(&svc.Repo, patch.Repo)
	set(&svc.Branch, patch.Branch)
	set(&svc.RootDir, patch.RootDir)
	set(&svc.Image, patch.Image)
	set(&svc.DockerfilePath, patch.DockerfilePath)
	set(&svc.StartCommand, patch.StartCommand)
	set(&svc.Port, patch.Port)
	set(&svc.HealthcheckPath, patch.HealthcheckPath)
	set(&svc.PublicPort, patch.PublicPort)
	set(&svc.CPULimit, patch.CPULimit)
	set(&svc.MemoryLimit, patch.MemoryLimit)
	set(&svc.AutoDeploy, patch.AutoDeploy)
	set(&svc.WaitForCI, patch.WaitForCI)
	if err := validateService(svc); err != nil {
		return ServiceView{}, err
	}
	if err := p.store.UpdateService(ctx, svc); err != nil {
		return ServiceView{}, err
	}
	return p.withStatus(ctx, svc)
}

func set[T any](dst *T, v *T) {
	if v != nil {
		*dst = *v
	}
}

// validateService checks the editable settings of a service.
func validateService(svc store.Service) error {
	switch {
	case svc.Kind == "app" && svc.Repo == "" && svc.Image == "":
		return errorf(ErrInvalid, "an app needs a repository or an image")
	case svc.Repo != "" && !repoName.MatchString(svc.Repo):
		return errorf(ErrInvalid, "repository must be owner/name")
	case svc.Repo != "" && svc.Branch == "":
		return errorf(ErrInvalid, "branch is required")
	case svc.Port < 0 || svc.Port > 65535:
		return errorf(ErrInvalid, "port must be between 0 and 65535")
	case svc.PublicPort < 0 || svc.PublicPort > 65535:
		return errorf(ErrInvalid, "public port must be between 0 and 65535")
	case svc.PublicPort > 0 && svc.Port == 0:
		return errorf(ErrInvalid, "a public port needs a container port")
	case svc.CPULimit != 0 && (svc.CPULimit < 0.01 || svc.CPULimit > float64(runtime.NumCPU())):
		return errorf(ErrInvalid, "CPU limit must be 0 (unlimited) or between 0.01 and %d cores", runtime.NumCPU())
	case svc.MemoryLimit != 0 && svc.MemoryLimit < minMemoryLimit:
		return errorf(ErrInvalid, "memory limit must be 0 (unlimited) or at least 64 MiB")
	case svc.HealthcheckPath != "" && !strings.HasPrefix(svc.HealthcheckPath, "/"):
		return errorf(ErrInvalid, "health check path must start with /")
	case !relative(svc.RootDir):
		return errorf(ErrInvalid, "root directory must be a relative path inside the repository")
	case !relative(svc.DockerfilePath):
		return errorf(ErrInvalid, "Dockerfile path must be a relative path inside the root directory")
	}
	return nil
}

// relative reports whether p is empty or a local relative path.
func relative(p string) bool {
	return p == "" || filepath.IsLocal(p)
}

// DeleteService deletes a service with its container, volumes, and backups.
func (p *Plane) DeleteService(ctx context.Context, id string) error {
	resume, err := p.pauseBackups(ctx, id)
	if err != nil {
		return err
	}
	defer resume()
	if err := p.deployer.DeleteService(ctx, id); err != nil {
		return err
	}
	p.forgetBackups(ctx, id)
	return nil
}

// StopService stops a service's container and keeps it stopped.
func (p *Plane) StopService(ctx context.Context, id string) error {
	return p.deployer.StopService(ctx, id)
}

// StartService starts a stopped service.
func (p *Plane) StartService(ctx context.Context, id string) error {
	return p.deployer.StartService(ctx, id)
}

// RestartService restarts a service's container.
func (p *Plane) RestartService(ctx context.Context, id string) error {
	return p.deployer.RestartService(ctx, id)
}

// ClearRestoreFence lifts the fence a failed restore left on a service,
// keeping its data as it is.
func (p *Plane) ClearRestoreFence(ctx context.Context, id string) error {
	return p.deployer.ClearRestoreFence(ctx, id)
}

// withStatus returns the view of svc with the live status of its project.
func (p *Plane) withStatus(ctx context.Context, svc store.Service) (ServiceView, error) {
	statuses, err := p.deployer.ServiceStatuses(ctx, svc.ProjectID)
	if err != nil {
		return ServiceView{}, err
	}
	return p.serviceView(ctx, svc, statuses)
}

// serviceView assembles the full view of a service.
func (p *Plane) serviceView(ctx context.Context, svc store.Service, statuses map[string]deploy.ServiceStatus) (ServiceView, error) {
	domains, err := p.store.Domains(ctx, svc.ID)
	if err != nil {
		return ServiceView{}, err
	}
	vols, err := p.store.Volumes(ctx, svc.ID)
	if err != nil {
		return ServiceView{}, err
	}
	view := ServiceView{
		Service: svc,
		Status:  statuses[svc.ID],
		Domains: domains,
		Volumes: vols,
	}
	if view.Status == "" {
		view.Status = deploy.StatusOffline
	}
	switch dep, err := p.store.LatestDeployment(ctx, svc.ID); {
	case err == nil:
		d := toDeployment(dep)
		view.LatestDeployment = &d
	case !errors.Is(err, store.ErrNotFound):
		return ServiceView{}, err
	}
	switch f, err := p.store.RestoreFence(ctx, svc.ID); {
	case err == nil:
		view.RestoreFence = &f
	case !errors.Is(err, store.ErrNotFound):
		return ServiceView{}, err
	}
	return view, nil
}
