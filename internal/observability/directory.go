package observability

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/otlp"
	"github.com/kyledickey/shed/internal/store"
)

// The labels and network names the deployer gives service containers.
const (
	labelProject    = "shed.project"
	labelService    = "shed.service"
	labelDeployment = "shed.deployment"
	networkPrefix   = "shed-"
)

// DirectoryStore names projects, services, and domains. *store.Store
// implements it.
type DirectoryStore interface {
	Projects(ctx context.Context) ([]store.Project, error)
	AllServices(ctx context.Context) ([]store.Service, error)
	AllDomains(ctx context.Context) ([]store.Domain, error)
}

var _ DirectoryStore = (*store.Store)(nil)

// ContainerLister lists containers by label. *docker.Client implements it.
type ContainerLister interface {
	List(ctx context.Context, labels map[string]string) ([]docker.Container, error)
}

// shedSource is shed itself in the label schema.
var shedSource = otlp.Source{ServiceID: "shed", ServiceName: "shed", ServiceKind: "shed", Origin: "shed"}

// serviceContainer is a container of a shed service.
type serviceContainer struct {
	ID      string
	Running bool
	// Addr is the container's address on its project network, if it has one.
	Addr   netip.Addr
	Source otlp.Source
}

// directory resolves shed's containers and domains to the services they
// belong to, as of one moment.
type directory struct {
	projects   map[string]bool // project IDs
	containers []serviceContainer
	byAddr     map[netip.Addr]otlp.Source
	byHost     map[string]otlp.Source
}

// loadDirectory reads the services, their containers, and their domains.
// Containers of services that no longer exist are left out.
func loadDirectory(ctx context.Context, st DirectoryStore, dc ContainerLister) (*directory, error) {
	projects, err := st.Projects(ctx)
	if err != nil {
		return nil, fmt.Errorf("observability: directory: %w", err)
	}
	services, err := st.AllServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("observability: directory: %w", err)
	}
	domains, err := st.AllDomains(ctx)
	if err != nil {
		return nil, fmt.Errorf("observability: directory: %w", err)
	}
	containers, err := dc.List(ctx, map[string]string{labelService: ""})
	if err != nil {
		return nil, fmt.Errorf("observability: directory: %w", err)
	}

	d := &directory{
		projects: make(map[string]bool, len(projects)),
		byAddr:   make(map[netip.Addr]otlp.Source),
		byHost:   make(map[string]otlp.Source, len(domains)),
	}
	names := make(map[string]string, len(projects))
	for _, p := range projects {
		d.projects[p.ID] = true
		names[p.ID] = p.Name
	}
	bySvc := make(map[string]otlp.Source, len(services))
	for _, s := range services {
		bySvc[s.ID] = otlp.Source{
			ProjectID: s.ProjectID, ProjectName: names[s.ProjectID],
			ServiceID: s.ID, ServiceName: s.Name, ServiceKind: s.Kind,
		}
	}
	for _, c := range containers {
		src, ok := bySvc[c.Labels[labelService]]
		if !ok {
			continue
		}
		src.DeploymentID = c.Labels[labelDeployment]
		sc := serviceContainer{ID: c.ID, Running: c.Running, Source: src}
		if a, err := netip.ParseAddr(c.IPs[networkPrefix+src.ProjectID]); err == nil {
			sc.Addr = a
			d.byAddr[a] = src
		}
		d.containers = append(d.containers, sc)
	}
	for _, dom := range domains {
		if src, ok := bySvc[dom.ServiceID]; ok {
			d.byHost[strings.ToLower(dom.Host)] = src
		}
	}
	return d, nil
}
