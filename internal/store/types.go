package store

import "time"

// User is a person who has signed in with GitHub.
type User struct {
	GitHubID  int64
	Login     string
	Name      string
	AvatarURL string
	CreatedAt time.Time
}

// Project groups services that share a private network.
type Project struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

// Service is a deployable unit within a project.
type Service struct {
	ID        string
	ProjectID string
	// Name is a DNS label; it is also the service's private hostname.
	Name string
	// Kind is one of app, postgres, mysql, mongo, or redis.
	Kind string
	// Repo is the GitHub repository as owner/name.
	Repo   string
	Branch string
	// RootDir is the directory within the repository to build.
	RootDir string
	// Image is the image to pull for services that are not built from a repo.
	Image string
	// DockerfilePath is empty to auto-detect a Dockerfile or fall back to
	// Railpack.
	DockerfilePath string
	StartCommand   string
	// Port is the container port; zero means none.
	Port            int
	HealthcheckPath string
	// PublicPort is the host TCP port to publish; zero means none.
	PublicPort int
	AutoDeploy bool
	WaitForCI  bool
	// Stopped reports whether the user stopped the service. Its active
	// deployment is kept, but its container is not run until it is started or
	// a new deployment goes live.
	Stopped   bool
	CreatedAt time.Time
}

// Volume is persistent storage mounted into a service's container.
type Volume struct {
	ID        string
	ServiceID string
	MountPath string
	CreatedAt time.Time
}

// Domain routes a hostname to a service.
type Domain struct {
	ID        string
	ServiceID string
	// Host is always lowercase.
	Host string
	// Generated reports whether shed derived the host from the base domain.
	Generated bool
	CreatedAt time.Time
}

// DeploymentStatus is the lifecycle state of a deployment.
type DeploymentStatus string

// Deployment statuses.
const (
	StatusQueued    DeploymentStatus = "queued"
	StatusWaiting   DeploymentStatus = "waiting" // waiting for CI
	StatusBuilding  DeploymentStatus = "building"
	StatusDeploying DeploymentStatus = "deploying"
	StatusActive    DeploymentStatus = "active"
	StatusFailed    DeploymentStatus = "failed"
	StatusCrashed   DeploymentStatus = "crashed"
	StatusRemoved   DeploymentStatus = "removed" // superseded by a newer deployment
	StatusCanceled  DeploymentStatus = "canceled"
	StatusSkipped   DeploymentStatus = "skipped" // CI failed
)

// Terminal reports whether the deployment has left the deploy pipeline, that
// is, whether it is anything other than queued, waiting, building, or
// deploying. Active deployments are terminal in this sense.
func (s DeploymentStatus) Terminal() bool {
	switch s {
	case StatusQueued, StatusWaiting, StatusBuilding, StatusDeploying:
		return false
	}
	return true
}

// Trigger records what caused a deployment.
type Trigger string

// Deployment triggers.
const (
	TriggerPush     Trigger = "push"
	TriggerManual   Trigger = "manual"
	TriggerRedeploy Trigger = "redeploy"
	TriggerCreate   Trigger = "create"
)

// Deployment is one attempt to build and run a service.
type Deployment struct {
	ID            string
	ServiceID     string
	Status        DeploymentStatus
	Trigger       Trigger
	CommitSHA     string
	CommitMessage string
	CommitAuthor  string
	// Image is the built or pulled image reference.
	Image       string
	ContainerID string
	// Error describes why the deployment failed.
	Error      string
	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// MetricSample is a service's resource usage at one instant, summed over its
// containers.
type MetricSample struct {
	ServiceID string
	// Time is stored at second precision.
	Time time.Time
	// CPU is the percentage of one core in use.
	CPU float64
	// Memory is the memory in use in bytes.
	Memory int64
	// NetRx, NetTx, DiskRead, and DiskWrite are rates in bytes per second.
	NetRx, NetTx        float64
	DiskRead, DiskWrite float64
}

// MetricBucket holds the averages of the metric samples that fall into one
// bucket of a time window.
type MetricBucket struct {
	// Index is the bucket's position in the window, from zero.
	Index               int
	CPU, Memory         float64
	NetRx, NetTx        float64
	DiskRead, DiskWrite float64
}
