package api

import (
	"time"

	"github.com/kyledickey/shed/internal/backup"
	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/store"
)

// The JSON types below mirror the TypeScript types of the design document.

type userJSON struct {
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl"`
}

type setupJSON struct {
	GitHubConfigured bool   `json:"githubConfigured"`
	AppSlug          string `json:"appSlug"`
	InstallURL       string `json:"installUrl"`
}

// projectJSON is a project. Services holds serviceSummaryJSON values in
// lists and full serviceJSON values for a single project.
type projectJSON[S any] struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	Services  []S       `json:"services"`
}

type serviceSummaryJSON struct {
	ID     string               `json:"id"`
	Name   string               `json:"name"`
	Kind   string               `json:"kind"`
	Status deploy.ServiceStatus `json:"status"`
}

type serviceJSON struct {
	ID               string               `json:"id"`
	ProjectID        string               `json:"projectId"`
	Name             string               `json:"name"`
	Kind             string               `json:"kind"`
	Repo             string               `json:"repo"`
	Branch           string               `json:"branch"`
	RootDir          string               `json:"rootDir"`
	Image            string               `json:"image"`
	DockerfilePath   string               `json:"dockerfilePath"`
	StartCommand     string               `json:"startCommand"`
	Port             int                  `json:"port"`
	HealthcheckPath  string               `json:"healthcheckPath"`
	PublicPort       int                  `json:"publicPort"`
	AutoDeploy       bool                 `json:"autoDeploy"`
	WaitForCI        bool                 `json:"waitForCi"`
	Status           deploy.ServiceStatus `json:"status"`
	PrivateHost      string               `json:"privateHost"`
	Domains          []domainJSON         `json:"domains"`
	Volumes          []volumeJSON         `json:"volumes"`
	LatestDeployment *deploymentJSON      `json:"latestDeployment"`
	CreatedAt        time.Time            `json:"createdAt"`
}

type deploymentJSON struct {
	ID            string                 `json:"id"`
	ServiceID     string                 `json:"serviceId"`
	Status        store.DeploymentStatus `json:"status"`
	Trigger       store.Trigger          `json:"trigger"`
	CommitSHA     string                 `json:"commitSha"`
	CommitMessage string                 `json:"commitMessage"`
	CommitAuthor  string                 `json:"commitAuthor"`
	Image         string                 `json:"image"`
	Error         string                 `json:"error"`
	CreatedAt     time.Time              `json:"createdAt"`
	StartedAt     *time.Time             `json:"startedAt"`
	FinishedAt    *time.Time             `json:"finishedAt"`
}

type domainJSON struct {
	ID        string `json:"id"`
	Host      string `json:"host"`
	Generated bool   `json:"generated"`
	URL       string `json:"url"`
}

type volumeJSON struct {
	ID        string    `json:"id"`
	MountPath string    `json:"mountPath"`
	CreatedAt time.Time `json:"createdAt"`
}

type repoJSON struct {
	FullName      string `json:"fullName"`
	DefaultBranch string `json:"defaultBranch"`
	Private       bool   `json:"private"`
}

func toDeployment(d store.Deployment) deploymentJSON {
	return deploymentJSON{
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

func toDomain(d store.Domain) domainJSON {
	return domainJSON{ID: d.ID, Host: d.Host, Generated: d.Generated, URL: "https://" + d.Host}
}

func toVolume(v store.Volume) volumeJSON {
	return volumeJSON{ID: v.ID, MountPath: v.MountPath, CreatedAt: v.CreatedAt}
}

type backupPolicyJSON struct {
	Enabled     bool       `json:"enabled"`
	Schedule    string     `json:"schedule"`
	Compression string     `json:"compression"`
	KeepLocal   int        `json:"keepLocal"`
	Upload      bool       `json:"upload"`
	KeepRemote  int        `json:"keepRemote"`
	NextRunAt   *time.Time `json:"nextRunAt"`
}

// backupPolicyInput is the body of a policy update.
type backupPolicyInput struct {
	Enabled     bool   `json:"enabled"`
	Schedule    string `json:"schedule"`
	Compression string `json:"compression"`
	KeepLocal   int    `json:"keepLocal"`
	Upload      bool   `json:"upload"`
	KeepRemote  int    `json:"keepRemote"`
}

type backupJSON struct {
	ID          string     `json:"id"`
	ServiceID   *string    `json:"serviceId"`
	Trigger     string     `json:"trigger"`
	Method      string     `json:"method"`
	Status      string     `json:"status"`
	FileName    string     `json:"fileName"`
	Size        int64      `json:"size"`
	Encrypted   bool       `json:"encrypted"`
	Local       bool       `json:"local"`
	Remote      bool       `json:"remote"`
	RemoteError string     `json:"remoteError"`
	Error       string     `json:"error"`
	CreatedAt   time.Time  `json:"createdAt"`
	FinishedAt  *time.Time `json:"finishedAt"`
}

type restoreJSON struct {
	ID         string     `json:"id"`
	ServiceID  string     `json:"serviceId"`
	BackupID   string     `json:"backupId"`
	Status     string     `json:"status"`
	Error      string     `json:"error"`
	CreatedAt  time.Time  `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt"`
}

type serviceBackupsJSON struct {
	Policy  backupPolicyJSON `json:"policy"`
	Backups []backupJSON     `json:"backups"`
	Restore *restoreJSON     `json:"restore"`
}

type systemBackupsJSON struct {
	Policy  backupPolicyJSON `json:"policy"`
	Backups []backupJSON     `json:"backups"`
}

type s3SettingsJSON struct {
	Endpoint    string `json:"endpoint"`
	Region      string `json:"region"`
	Bucket      string `json:"bucket"`
	Prefix      string `json:"prefix"`
	AccessKeyID string `json:"accessKeyId"`
	PathStyle   bool   `json:"pathStyle"`
	HasSecret   bool   `json:"hasSecret"`
}

type backupSettingsJSON struct {
	S3         *s3SettingsJSON `json:"s3"`
	Encryption struct {
		Enabled   bool   `json:"enabled"`
		Recipient string `json:"recipient"`
	} `json:"encryption"`
}

// s3InputJSON is the S3 destination of a settings update. An empty
// secretAccessKey keeps the stored secret.
type s3InputJSON struct {
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	Prefix          string `json:"prefix"`
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
	PathStyle       bool   `json:"pathStyle"`
}

// backupSettingsInput is the body of a settings update or S3 test. Encryption
// is a pointer so that an empty body is rejected rather than turning
// encryption off.
type backupSettingsInput struct {
	S3         *s3InputJSON `json:"s3"`
	Encryption *struct {
		Enabled bool `json:"enabled"`
	} `json:"encryption"`
}

func toBackupPolicy(p backup.Policy) backupPolicyJSON {
	return backupPolicyJSON{
		Enabled:     p.Enabled,
		Schedule:    p.Schedule,
		Compression: p.Compression,
		KeepLocal:   p.KeepLocal,
		Upload:      p.Upload,
		KeepRemote:  p.KeepRemote,
		NextRunAt:   p.NextRun,
	}
}

// toBackup converts b. serviceName is empty for shed.db.
func toBackup(b store.Backup, serviceName string) backupJSON {
	var serviceID *string
	if b.ServiceID != "" {
		serviceID = &b.ServiceID
	}
	return backupJSON{
		ID:          b.ID,
		ServiceID:   serviceID,
		Trigger:     string(b.Trigger),
		Method:      string(b.Method),
		Status:      string(b.Status),
		FileName:    backup.DownloadName(b, serviceName),
		Size:        b.Size,
		Encrypted:   b.Encrypted,
		Local:       b.Local,
		Remote:      b.RemoteKey != "",
		RemoteError: b.RemoteError,
		Error:       b.Error,
		CreatedAt:   b.CreatedAt,
		FinishedAt:  b.FinishedAt,
	}
}

func toRestore(r store.Restore) restoreJSON {
	return restoreJSON{
		ID:         r.ID,
		ServiceID:  r.ServiceID,
		BackupID:   r.BackupID,
		Status:     string(r.Status),
		Error:      r.Error,
		CreatedAt:  r.CreatedAt,
		FinishedAt: r.FinishedAt,
	}
}
