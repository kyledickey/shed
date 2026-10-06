package control

import (
	"time"

	"github.com/kyledickey/shed/internal/backup"
	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/store"
)

// ProjectSummary is a project in a list, with a summary of each service.
type ProjectSummary struct {
	store.Project
	Services []ServiceSummary
}

// ServiceSummary is the name, kind, and live status of a service.
type ServiceSummary struct {
	ID     string
	Name   string
	Kind   string
	Status deploy.ServiceStatus
}

// ProjectView is a project with its full services.
type ProjectView struct {
	store.Project
	Services []ServiceView
}

// ServiceView is a service with its settings, live status, domains, volumes,
// latest deployment, and restore fence. It holds no variable values.
type ServiceView struct {
	store.Service
	// Status is derived from the service's deployments and container;
	// offline if it has neither.
	Status  deploy.ServiceStatus
	Domains []store.Domain
	Volumes []store.Volume
	// LatestDeployment is nil if the service has never been deployed.
	LatestDeployment *DeploymentView
	// RestoreFence is set while a failed restore keeps the service from
	// starting.
	RestoreFence *store.RestoreFence
}

// DeploymentView is a deployment without its runtime record, which holds the
// resolved variables its container was started with.
type DeploymentView struct {
	ID            string
	ServiceID     string
	Status        store.DeploymentStatus
	Trigger       store.Trigger
	CommitSHA     string
	CommitMessage string
	CommitAuthor  string
	Image         string
	Error         string
	CreatedAt     time.Time
	StartedAt     *time.Time
	FinishedAt    *time.Time
	// ImageAvailable reports whether the deployment's image is still on the
	// server, so that it can be redeployed. It is set only in deployment
	// lists, for deployments with an image, when Docker could be asked.
	ImageAvailable *bool
}

func toDeployment(d store.Deployment) DeploymentView {
	return DeploymentView{
		ID:            d.ID,
		ServiceID:     d.ServiceID,
		Status:        d.Status,
		Trigger:       d.Trigger,
		CommitSHA:     d.CommitSHA,
		CommitMessage: d.CommitMessage,
		CommitAuthor:  d.CommitAuthor,
		Image:         d.Image,
		Error:         d.Error,
		CreatedAt:     d.CreatedAt,
		StartedAt:     d.StartedAt,
		FinishedAt:    d.FinishedAt,
	}
}

// BackupView is a backup with the name its archive downloads as.
type BackupView struct {
	store.Backup
	FileName string
}

// BackupList is the backup policy and the newest backups of a service, or
// of shed.db.
type BackupList struct {
	Policy  backup.Policy
	Backups []BackupView
	// Restore is the service's latest restore, if any; always nil for
	// shed.db.
	Restore *store.Restore
}

// VariableName describes one variable of a service without its value.
type VariableName struct {
	Name string
	// Injected is set for variables shed provides, such as PORT and
	// SHED_PRIVATE_DOMAIN, and clear for the service's own.
	Injected bool
	// Reference is set for own variables whose value refers to other
	// variables with ${{ }}.
	Reference bool
}

// VariableNames lists the variables a service's next deployment would see.
type VariableNames struct {
	// Variables are sorted by name. An own variable that overrides an
	// injected one is listed once, as own.
	Variables []VariableName
	// ResolveError says why the variables do not resolve, if they do not.
	// The injected names are then missing.
	ResolveError string
}
