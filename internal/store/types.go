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
	// CPULimit is the container's CPU quota in cores; zero means unlimited.
	CPULimit float64
	// MemoryLimit is the container's memory limit in bytes, with no extra
	// swap; zero means unlimited.
	MemoryLimit int64
	AutoDeploy  bool
	WaitForCI   bool
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
	Image string
	// Port is the container port the deployment was started with; zero
	// means none. Routes to the deployment use it, not the service's
	// current port.
	Port        int
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

// HostSample is the resource usage of the whole host at one instant.
type HostSample struct {
	// Time is stored at second precision.
	Time time.Time
	// CPU is the percentage of one core in use.
	CPU float64
	// Memory and DiskUsed are the memory and filesystem space in use in
	// bytes.
	Memory, DiskUsed int64
	// NetRx, NetTx, DiskRead, and DiskWrite are rates in bytes per second.
	NetRx, NetTx        float64
	DiskRead, DiskWrite float64
}

// HostBucket holds the averages of the host samples that fall into one
// bucket of a time window.
type HostBucket struct {
	// Index is the bucket's position in the window, from zero.
	Index                 int
	CPU, Memory, DiskUsed float64
	NetRx, NetTx          float64
	DiskRead, DiskWrite   float64
}

// BackupPolicy configures a service's scheduled backups.
type BackupPolicy struct {
	ServiceID string
	Enabled   bool
	// Schedule is a cron expression, in UTC unless it starts with CRON_TZ=.
	Schedule string
	// Compression is fastest, default, better, or best.
	Compression string
	// KeepLocal is how many scheduled backups keep their archive on disk.
	KeepLocal int
	// Upload reports whether archives are also stored in S3.
	Upload bool
	// KeepRemote is how many scheduled backups keep their object in S3.
	KeepRemote int
}

// BackupTrigger records what caused a backup.
type BackupTrigger string

// Backup triggers.
const (
	BackupSchedule   BackupTrigger = "schedule"
	BackupManual     BackupTrigger = "manual"
	BackupPreRestore BackupTrigger = "pre-restore"
)

// BackupMethod is how a backup captures data.
type BackupMethod string

// Backup methods.
const (
	MethodDump   BackupMethod = "dump"   // database dump
	MethodVolume BackupMethod = "volume" // archive of the service's volumes
	MethodSQLite BackupMethod = "sqlite" // snapshot of shed's own database
)

// BackupStatus is the lifecycle state of a backup.
type BackupStatus string

// Backup statuses.
const (
	BackupQueued    BackupStatus = "queued"
	BackupRunning   BackupStatus = "running"
	BackupUploading BackupStatus = "uploading" // archive written, upload to S3 in progress
	BackupSucceeded BackupStatus = "succeeded"
	BackupFailed    BackupStatus = "failed"
)

// Backup is one archive of a service's data, or of shed's own database.
type Backup struct {
	ID string
	// ServiceID is empty for a backup of shed.db.
	ServiceID string
	Trigger   BackupTrigger
	Method    BackupMethod
	Status    BackupStatus
	// File is the archive's file name.
	File string
	// Size is the archive size in bytes.
	Size      int64
	Encrypted bool
	// Local reports whether the archive is present on disk.
	Local bool
	// RemoteKey is the S3 object key, or empty if the archive was not uploaded.
	RemoteKey string
	// DestinationID is the BackupDestination that RemoteKey is in.
	DestinationID string
	// RemoteError is the last upload failure.
	RemoteError string
	// Error describes why the backup failed.
	Error      string
	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// BackupDestination is an S3 location that backups are uploaded to. Its
// location fields never change once stored; only its credentials do.
type BackupDestination struct {
	ID              string
	Endpoint        string
	Region          string
	Bucket          string
	Prefix          string
	PathStyle       bool
	AccessKeyID     string
	SecretAccessKey string
	CreatedAt       time.Time
}

// RestoreStatus is the lifecycle state of a restore.
type RestoreStatus string

// Restore statuses.
const (
	RestoreRunning   RestoreStatus = "running"
	RestoreSucceeded RestoreStatus = "succeeded"
	RestoreFailed    RestoreStatus = "failed"
)

// Restore is one attempt to put a backup's data back into a service.
type Restore struct {
	ID        string
	ServiceID string
	// BackupID is not a foreign key, since the backup may be pruned later.
	BackupID string
	Status   RestoreStatus
	// Error describes why the restore failed.
	Error      string
	CreatedAt  time.Time
	FinishedAt *time.Time
}

// RestorePhase is how far a restore that changes a service's data has got.
type RestorePhase string

// Restore phases.
const (
	// RestoreRetaining means the service is being stopped and its volumes
	// copied aside. The volumes themselves are unchanged.
	RestoreRetaining RestorePhase = "retaining"
	// RestoreReplacing means the copies are complete and the volumes are
	// being replaced, so they may hold partial data.
	RestoreReplacing RestorePhase = "replacing"
	// RestoreLoading means a dump is being loaded into the running
	// database, so its data may be partial, and the load may still be
	// running in the database's container.
	RestoreLoading RestorePhase = "loading"
)

// RestoreFence keeps a service stopped while a restore changes its data. It
// is stored, so it outlasts a restart of shed: the service cannot be started
// or deployed until the restore completes, its previous data is put back, or
// the user clears the fence.
type RestoreFence struct {
	ServiceID string
	RestoreID string
	Phase     RestorePhase
	// Image is the image of the helper containers that mount the volumes.
	Image string
	// VolumeIDs are the volumes being replaced.
	VolumeIDs []string
	// WasStopped records whether the service was stopped before the fence.
	WasStopped bool
	CreatedAt  time.Time
}
