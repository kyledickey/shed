package api

import (
	"time"

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
