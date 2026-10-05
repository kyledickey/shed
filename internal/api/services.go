package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
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
	envKey   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
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

type newServiceRequest struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	Image  string `json:"image"`
}

// createService creates a service and starts its first deployment. Database
// kinds get their template's image, port, volume, and variables.
func (s *Server) createService(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	project, err := s.store.Project(ctx, r.PathValue("id"))
	if err != nil {
		return err
	}
	var req newServiceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	if !dnsLabel.MatchString(req.Name) {
		return errorf(http.StatusBadRequest, "service name must be a DNS label: lowercase letters, digits, and hyphens")
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
		return errorf(http.StatusBadRequest, "unknown service kind %q", req.Kind)
	case req.Repo != "":
		svc.Repo, svc.Branch, svc.Port = req.Repo, req.Branch, defaultAppPort
		if err := validateService(svc); err != nil {
			return err
		}
		if commit, err = s.branchHead(ctx, svc); err != nil {
			return err
		}
	case req.Image != "":
		svc.Image = strings.TrimSpace(req.Image)
	default:
		return errorf(http.StatusBadRequest, "an app needs a repository and branch, or an image")
	}

	svc, err = s.store.CreateService(ctx, svc)
	if errors.Is(err, store.ErrConflict) {
		return errorf(http.StatusConflict, "a service named %q already exists in this project", req.Name)
	}
	if err != nil {
		return err
	}
	if isDB {
		if err := s.applyTemplate(ctx, svc, tmpl); err != nil {
			if derr := s.store.DeleteService(ctx, svc.ID); derr != nil {
				s.log.Error("delete half-created service", "service", svc.ID, "err", derr)
			}
			return err
		}
	}
	if _, err := s.deployer.Deploy(ctx, svc.ID, store.TriggerCreate, commit); err != nil {
		s.log.Error("start first deployment", "service", svc.ID, "err", err)
	}
	return s.writeService(ctx, w, svc, http.StatusCreated)
}

// applyTemplate gives a new database service its volume and variables.
func (s *Server) applyTemplate(ctx context.Context, svc store.Service, tmpl catalog.Template) error {
	if _, err := s.store.CreateVolume(ctx, svc.ID, tmpl.MountPath); err != nil {
		return err
	}
	return s.store.SetVariables(ctx, svc.ID, tmpl.Vars())
}

func (s *Server) getService(w http.ResponseWriter, r *http.Request) error {
	svc, err := s.store.Service(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return s.writeService(r.Context(), w, svc, http.StatusOK)
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
	svc, err := s.store.Service(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	var p servicePatch
	if err := decodeJSON(w, r, &p); err != nil {
		return err
	}
	set(&svc.Repo, p.Repo)
	set(&svc.Branch, p.Branch)
	set(&svc.RootDir, p.RootDir)
	set(&svc.Image, p.Image)
	set(&svc.DockerfilePath, p.DockerfilePath)
	set(&svc.StartCommand, p.StartCommand)
	set(&svc.Port, p.Port)
	set(&svc.HealthcheckPath, p.HealthcheckPath)
	set(&svc.PublicPort, p.PublicPort)
	set(&svc.CPULimit, p.CPULimit)
	set(&svc.MemoryLimit, p.MemoryLimit)
	set(&svc.AutoDeploy, p.AutoDeploy)
	set(&svc.WaitForCI, p.WaitForCI)
	if err := validateService(svc); err != nil {
		return err
	}
	if err := s.store.UpdateService(r.Context(), svc); err != nil {
		return err
	}
	return s.writeService(r.Context(), w, svc, http.StatusOK)
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
		return errorf(http.StatusBadRequest, "an app needs a repository or an image")
	case svc.Repo != "" && !repoName.MatchString(svc.Repo):
		return errorf(http.StatusBadRequest, "repository must be owner/name")
	case svc.Repo != "" && svc.Branch == "":
		return errorf(http.StatusBadRequest, "branch is required")
	case svc.Port < 0 || svc.Port > 65535:
		return errorf(http.StatusBadRequest, "port must be between 0 and 65535")
	case svc.PublicPort < 0 || svc.PublicPort > 65535:
		return errorf(http.StatusBadRequest, "public port must be between 0 and 65535")
	case svc.PublicPort > 0 && svc.Port == 0:
		return errorf(http.StatusBadRequest, "a public port needs a container port")
	case svc.CPULimit != 0 && (svc.CPULimit < 0.01 || svc.CPULimit > float64(runtime.NumCPU())):
		return errorf(http.StatusBadRequest, "CPU limit must be 0 (unlimited) or between 0.01 and %d cores", runtime.NumCPU())
	case svc.MemoryLimit != 0 && svc.MemoryLimit < minMemoryLimit:
		return errorf(http.StatusBadRequest, "memory limit must be 0 (unlimited) or at least 64 MiB")
	case svc.HealthcheckPath != "" && !strings.HasPrefix(svc.HealthcheckPath, "/"):
		return errorf(http.StatusBadRequest, "health check path must start with /")
	case !relative(svc.RootDir):
		return errorf(http.StatusBadRequest, "root directory must be a relative path inside the repository")
	case !relative(svc.DockerfilePath):
		return errorf(http.StatusBadRequest, "Dockerfile path must be a relative path inside the root directory")
	}
	return nil
}

func relative(p string) bool {
	return p == "" || filepath.IsLocal(p)
}

func (s *Server) deleteService(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	resume, err := s.pauseBackups(r.Context(), id)
	if err != nil {
		return err
	}
	defer resume()
	if err := s.deployer.DeleteService(r.Context(), id); err != nil {
		return err
	}
	s.forgetBackups(r.Context(), id)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// controlService returns a handler that applies op, such as stopping, to a
// service and responds with the updated service.
func (s *Server) controlService(op func(Deployer, context.Context, string) error) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		id := r.PathValue("id")
		if err := op(s.deployer, r.Context(), id); err != nil {
			return err
		}
		return s.getService(w, r)
	}
}

func (s *Server) getVariables(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if _, err := s.store.Service(r.Context(), id); err != nil {
		return err
	}
	vars, err := s.store.Variables(r.Context(), id)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, vars)
}

func (s *Server) putVariables(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if _, err := s.store.Service(r.Context(), id); err != nil {
		return err
	}
	vars := map[string]string{}
	if err := decodeJSON(w, r, &vars); err != nil {
		return err
	}
	for k := range vars {
		if !envKey.MatchString(k) {
			return errorf(http.StatusBadRequest, "invalid variable name %q", k)
		}
	}
	if err := s.store.SetVariables(r.Context(), id, vars); err != nil {
		return err
	}
	return s.getVariables(w, r)
}

func (s *Server) createDomain(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	svc, err := s.store.Service(ctx, r.PathValue("id"))
	if err != nil {
		return err
	}
	var req struct {
		Host string `json:"host"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	host := strings.ToLower(strings.TrimSpace(req.Host))
	generated := host == ""
	if generated {
		if host, err = s.generatedHost(ctx, svc); err != nil {
			return err
		}
	}
	if !validHost(host) {
		return errorf(http.StatusBadRequest, "%q is not a valid host name", host)
	}
	dashboard, err := url.Parse(s.baseURL)
	if err != nil {
		return err
	}
	if strings.EqualFold(host, strings.TrimSuffix(dashboard.Hostname(), ".")) {
		return errorf(http.StatusConflict, "the dashboard hostname is reserved")
	}
	d, err := s.store.CreateDomain(ctx, svc.ID, host, generated)
	if errors.Is(err, store.ErrConflict) {
		return errorf(http.StatusConflict, "%s is already in use", host)
	}
	if err != nil {
		return err
	}
	s.applyDomainRoutes(ctx, w)
	return writeJSON(w, http.StatusCreated, toDomain(d))
}

// generatedHost returns <service>-<project>.<base domain> for svc.
func (s *Server) generatedHost(ctx context.Context, svc store.Service) (string, error) {
	if s.baseDomain == "" {
		return "", errorf(http.StatusBadRequest, "proxy.base_domain is not configured, so domains cannot be generated")
	}
	project, err := s.store.Project(ctx, svc.ProjectID)
	if err != nil {
		return "", err
	}
	label := svc.Name
	if ps := slug(project.Name); ps != "" {
		label += "-" + ps
	}
	if len(label) > 63 {
		label = strings.TrimRight(label[:63], "-")
	}
	return label + "." + s.baseDomain, nil
}

// validHost reports whether host is a valid DNS host name.
func validHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for label := range strings.SplitSeq(host, ".") {
		if !dnsLabel.MatchString(label) {
			return false
		}
	}
	return true
}

func (s *Server) deleteDomain(w http.ResponseWriter, r *http.Request) error {
	if err := s.store.DeleteDomain(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	s.applyDomainRoutes(r.Context(), w)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) createVolume(w http.ResponseWriter, r *http.Request) error {
	svc, err := s.store.Service(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	var req struct {
		MountPath string `json:"mountPath"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	p := req.MountPath
	if !path.IsAbs(p) || path.Clean(p) != p || p == "/" {
		return errorf(http.StatusBadRequest, "mount path must be a clean absolute path other than /")
	}
	v, err := s.store.CreateVolume(r.Context(), svc.ID, p)
	if errors.Is(err, store.ErrConflict) {
		return errorf(http.StatusConflict, "a volume is already mounted at %s", p)
	}
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusCreated, toVolume(v))
}

func (s *Server) deleteVolume(w http.ResponseWriter, r *http.Request) error {
	if err := s.deployer.DeleteVolume(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) writeService(ctx context.Context, w http.ResponseWriter, svc store.Service, status int) error {
	statuses, err := s.deployer.ServiceStatuses(ctx, svc.ProjectID)
	if err != nil {
		return err
	}
	view, err := s.serviceView(ctx, svc, statuses)
	if err != nil {
		return err
	}
	return writeJSON(w, status, view)
}

// serviceView assembles the full JSON form of a service.
func (s *Server) serviceView(ctx context.Context, svc store.Service, statuses map[string]deploy.ServiceStatus) (serviceJSON, error) {
	domains, err := s.store.Domains(ctx, svc.ID)
	if err != nil {
		return serviceJSON{}, err
	}
	vols, err := s.store.Volumes(ctx, svc.ID)
	if err != nil {
		return serviceJSON{}, err
	}
	var latest *deploymentJSON
	dep, err := s.store.LatestDeployment(ctx, svc.ID)
	switch {
	case err == nil:
		d := toDeployment(dep)
		latest = &d
	case !errors.Is(err, store.ErrNotFound):
		return serviceJSON{}, err
	}
	var fence *restoreFenceJSON
	f, err := s.store.RestoreFence(ctx, svc.ID)
	switch {
	case err == nil:
		fence = &restoreFenceJSON{RestoreID: f.RestoreID, Phase: f.Phase, CreatedAt: f.CreatedAt}
	case !errors.Is(err, store.ErrNotFound):
		return serviceJSON{}, err
	}
	status := statuses[svc.ID]
	if status == "" {
		status = deploy.StatusOffline
	}
	view := serviceJSON{
		ID:               svc.ID,
		ProjectID:        svc.ProjectID,
		Name:             svc.Name,
		Kind:             svc.Kind,
		Repo:             svc.Repo,
		Branch:           svc.Branch,
		RootDir:          svc.RootDir,
		Image:            svc.Image,
		DockerfilePath:   svc.DockerfilePath,
		StartCommand:     svc.StartCommand,
		Port:             svc.Port,
		HealthcheckPath:  svc.HealthcheckPath,
		PublicPort:       svc.PublicPort,
		CPULimit:         svc.CPULimit,
		MemoryLimit:      svc.MemoryLimit,
		AutoDeploy:       svc.AutoDeploy,
		WaitForCI:        svc.WaitForCI,
		Status:           status,
		PrivateHost:      svc.Name,
		Domains:          make([]domainJSON, 0, len(domains)),
		Volumes:          make([]volumeJSON, 0, len(vols)),
		LatestDeployment: latest,
		RestoreFence:     fence,
		CreatedAt:        svc.CreatedAt,
	}
	for _, d := range domains {
		view.Domains = append(view.Domains, toDomain(d))
	}
	for _, v := range vols {
		view.Volumes = append(view.Volumes, toVolume(v))
	}
	return view, nil
}
