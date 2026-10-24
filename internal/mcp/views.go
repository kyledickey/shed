package mcp

import (
	"math"
	"time"

	"github.com/kyledickey/shed/internal/control"
	"github.com/kyledickey/shed/internal/metrics"
	"github.com/kyledickey/shed/internal/update"
)

// The tool results below are compact: empty strings, zero settings, and
// false flags are left out.

type projectsOut struct {
	Projects []projectSummaryOut `json:"projects"`
}

type projectSummaryOut struct {
	ID        string              `json:"id"`
	Name      string              `json:"name"`
	CreatedAt time.Time           `json:"createdAt"`
	Services  []serviceSummaryOut `json:"services"`
}

type serviceSummaryOut struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
}

type projectOut struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	CreatedAt time.Time    `json:"createdAt"`
	Services  []serviceOut `json:"services"`
}

type serviceOut struct {
	ID              string  `json:"id"`
	ProjectID       string  `json:"projectId"`
	Name            string  `json:"name" jsonschema:"also the service's hostname on the project's private network"`
	Kind            string  `json:"kind" jsonschema:"app, postgres, mysql, mongo, or redis"`
	Status          string  `json:"status" jsonschema:"active, deploying, crashed, failed, stopped, or offline"`
	Repo            string  `json:"repo,omitempty" jsonschema:"GitHub repository as owner/name"`
	Branch          string  `json:"branch,omitempty"`
	RootDir         string  `json:"rootDir,omitempty" jsonschema:"directory within the repository that is built"`
	Image           string  `json:"image,omitempty" jsonschema:"Docker image run instead of a build"`
	DockerfilePath  string  `json:"dockerfilePath,omitempty" jsonschema:"absent to use the Dockerfile at the root, or Railpack without one"`
	StartCommand    string  `json:"startCommand,omitempty"`
	Port            int     `json:"port,omitempty" jsonschema:"container port that receives traffic; absent for none"`
	HealthcheckPath string  `json:"healthcheckPath,omitempty"`
	PublicPort      int     `json:"publicPort,omitempty" jsonschema:"host TCP port the service is published on"`
	CPULimit        float64 `json:"cpuLimit,omitempty" jsonschema:"CPU limit in cores; absent for unlimited"`
	MemoryLimit     int64   `json:"memoryLimit,omitempty" jsonschema:"memory limit in bytes; absent for unlimited"`
	AutoDeploy      bool    `json:"autoDeploy,omitempty" jsonschema:"whether pushes to the branch deploy"`
	WaitForCI       bool    `json:"waitForCi,omitempty" jsonschema:"whether push deployments wait for GitHub checks to pass"`
	// Stopped is the user's choice; Status says whether the container runs.
	Stopped          bool             `json:"stopped,omitempty" jsonschema:"whether the user stopped the service"`
	Domains          []domainOut      `json:"domains,omitempty"`
	Volumes          []volumeOut      `json:"volumes,omitempty"`
	LatestDeployment *deploymentOut   `json:"latestDeployment,omitempty"`
	RestoreFence     *restoreFenceOut `json:"restoreFence,omitempty" jsonschema:"set while a failed restore keeps the service from starting"`
	CreatedAt        time.Time        `json:"createdAt"`
}

type domainOut struct {
	Host      string `json:"host"`
	Generated bool   `json:"generated,omitempty"`
}

type volumeOut struct {
	ID        string `json:"id"`
	MountPath string `json:"mountPath"`
}

type restoreFenceOut struct {
	RestoreID string    `json:"restoreId"`
	Phase     string    `json:"phase"`
	CreatedAt time.Time `json:"createdAt"`
}

type deploymentsOut struct {
	Deployments []deploymentOut `json:"deployments"`
}

type deploymentOut struct {
	ID            string     `json:"id"`
	ServiceID     string     `json:"serviceId"`
	Status        string     `json:"status" jsonschema:"queued, waiting (for CI checks), building, deploying, active (serving traffic), failed, crashed, removed (replaced by a newer deployment), canceled, or skipped (CI checks failed)"`
	Trigger       string     `json:"trigger" jsonschema:"push, manual, redeploy, or create"`
	CommitSHA     string     `json:"commitSha,omitempty"`
	CommitMessage string     `json:"commitMessage,omitempty"`
	CommitAuthor  string     `json:"commitAuthor,omitempty"`
	Image         string     `json:"image,omitempty"`
	Error         string     `json:"error,omitempty" jsonschema:"why the deployment failed"`
	CreatedAt     time.Time  `json:"createdAt"`
	StartedAt     *time.Time `json:"startedAt,omitempty"`
	FinishedAt    *time.Time `json:"finishedAt,omitempty"`
	// ImageAvailable is set only in deployment lists.
	ImageAvailable *bool `json:"imageAvailable,omitempty" jsonschema:"whether the image is still on the server, so the deployment can be redeployed"`
}

type variablesOut struct {
	Variables    []variableOut `json:"variables"`
	ResolveError string        `json:"resolveError,omitempty" jsonschema:"why the variables do not resolve; injected names are then missing"`
}

type variableOut struct {
	Name      string `json:"name"`
	Injected  bool   `json:"injected,omitempty" jsonschema:"provided by shed rather than set by the user"`
	Reference bool   `json:"reference,omitempty" jsonschema:"the value refers to other variables with ${{ }}"`
}

type backupsOut struct {
	Policy  backupPolicyOut `json:"policy"`
	Backups []backupOut     `json:"backups" jsonschema:"newest first"`
	// Omitted counts the older backups left out.
	Omitted int         `json:"omitted,omitempty" jsonschema:"how many older backups were left out"`
	Restore *restoreOut `json:"restore,omitempty" jsonschema:"the latest restore of the service"`
}

type backupPolicyOut struct {
	Enabled     bool       `json:"enabled"`
	Schedule    string     `json:"schedule" jsonschema:"cron schedule, in UTC unless it starts with CRON_TZ="`
	Compression string     `json:"compression"`
	KeepLocal   int        `json:"keepLocal" jsonschema:"scheduled backups kept on the server"`
	Upload      bool       `json:"upload" jsonschema:"whether backups are uploaded to S3, when S3 is configured"`
	KeepRemote  int        `json:"keepRemote" jsonschema:"scheduled backups kept in S3"`
	NextRunAt   *time.Time `json:"nextRunAt,omitempty"`
}

type backupOut struct {
	ID          string     `json:"id"`
	Trigger     string     `json:"trigger"`
	Method      string     `json:"method"`
	Status      string     `json:"status"`
	Size        int64      `json:"size" jsonschema:"archive size in bytes"`
	Encrypted   bool       `json:"encrypted,omitempty"`
	Local       bool       `json:"local,omitempty" jsonschema:"whether the archive is on the server"`
	Remote      bool       `json:"remote,omitempty" jsonschema:"whether the archive is in S3"`
	RemoteError string     `json:"remoteError,omitempty"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
}

type restoreOut struct {
	ID         string     `json:"id"`
	BackupID   string     `json:"backupId"`
	Status     string     `json:"status"`
	Error      string     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type updateOut struct {
	Current      string      `json:"current"`
	Latest       *releaseOut `json:"latest,omitempty"`
	Available    bool        `json:"available"`
	CheckedAt    *time.Time  `json:"checkedAt,omitempty"`
	State        string      `json:"state" jsonschema:"idle, checking, downloading, or restarting"`
	Staged       string      `json:"staged,omitempty" jsonschema:"version downloaded and ready to install"`
	Error        string      `json:"error,omitempty" jsonschema:"the last check or download failure"`
	AutoDownload bool        `json:"autoDownload"`
	Unsupported  string      `json:"unsupported,omitempty" jsonschema:"why this build of shed cannot update itself"`
}

type releaseOut struct {
	Version     string    `json:"version"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"publishedAt"`
}

type reposOut struct {
	Repos   []repoOut `json:"repos"`
	Omitted int       `json:"omitted,omitempty" jsonschema:"how many more repositories were left out"`
}

type repoOut struct {
	FullName      string `json:"fullName"`
	DefaultBranch string `json:"defaultBranch"`
	Private       bool   `json:"private,omitempty"`
}

type branchesOut struct {
	Branches []string `json:"branches"`
	Omitted  int      `json:"omitted,omitempty" jsonschema:"how many more branches were left out"`
}

// stat summarizes one metric over a range. Each value is null if nothing
// was sampled.
type stat struct {
	Latest *float64 `json:"latest"`
	Avg    *float64 `json:"avg"`
	Max    *float64 `json:"max"`
}

type serviceMetricsOut struct {
	Range       string    `json:"range"`
	From        time.Time `json:"from"`
	StepSeconds int       `json:"stepSeconds" jsonschema:"length of the buckets the samples are averaged into"`
	CPULimit    float64   `json:"cpuLimit,omitempty" jsonschema:"CPU limit in cores; absent for unlimited"`
	MemoryLimit int64     `json:"memoryLimit,omitempty" jsonschema:"memory limit in bytes; absent for unlimited"`
	CPU         stat      `json:"cpuPercent" jsonschema:"percent of one core"`
	Memory      stat      `json:"memoryBytes"`
	NetRx       stat      `json:"netRxBytesPerSecond"`
	NetTx       stat      `json:"netTxBytesPerSecond"`
	DiskRead    stat      `json:"diskReadBytesPerSecond"`
	DiskWrite   stat      `json:"diskWriteBytesPerSecond"`
}

type hostMetricsOut struct {
	Range       string    `json:"range"`
	From        time.Time `json:"from"`
	StepSeconds int       `json:"stepSeconds" jsonschema:"length of the buckets the samples are averaged into"`
	CPUs        int       `json:"cpus"`
	MemoryTotal int64     `json:"memoryTotalBytes"`
	DiskTotal   int64     `json:"diskTotalBytes"`
	CPU         stat      `json:"cpuPercent" jsonschema:"percent of one core, so up to 100 times cpus"`
	Memory      stat      `json:"memoryBytes"`
	DiskUsed    stat      `json:"diskUsedBytes"`
	NetRx       stat      `json:"netRxBytesPerSecond"`
	NetTx       stat      `json:"netTxBytesPerSecond"`
	DiskRead    stat      `json:"diskReadBytesPerSecond"`
	DiskWrite   stat      `json:"diskWriteBytesPerSecond"`
}

func toProjects(projects []control.ProjectSummary) projectsOut {
	out := projectsOut{Projects: make([]projectSummaryOut, 0, len(projects))}
	for _, p := range projects {
		ps := projectSummaryOut{ID: p.ID, Name: p.Name, CreatedAt: p.CreatedAt, Services: make([]serviceSummaryOut, 0, len(p.Services))}
		for _, svc := range p.Services {
			ps.Services = append(ps.Services, serviceSummaryOut{ID: svc.ID, Name: svc.Name, Kind: svc.Kind, Status: string(svc.Status)})
		}
		out.Projects = append(out.Projects, ps)
	}
	return out
}

func toProject(p control.ProjectView) projectOut {
	out := projectOut{ID: p.ID, Name: p.Name, CreatedAt: p.CreatedAt, Services: make([]serviceOut, 0, len(p.Services))}
	for _, svc := range p.Services {
		out.Services = append(out.Services, toService(svc))
	}
	return out
}

func toService(v control.ServiceView) serviceOut {
	out := serviceOut{
		ID:              v.ID,
		ProjectID:       v.ProjectID,
		Name:            v.Name,
		Kind:            v.Kind,
		Status:          string(v.Status),
		Repo:            v.Repo,
		Branch:          v.Branch,
		RootDir:         v.RootDir,
		Image:           v.Image,
		DockerfilePath:  v.DockerfilePath,
		StartCommand:    v.StartCommand,
		Port:            v.Port,
		HealthcheckPath: v.HealthcheckPath,
		PublicPort:      v.PublicPort,
		CPULimit:        v.CPULimit,
		MemoryLimit:     v.MemoryLimit,
		AutoDeploy:      v.AutoDeploy,
		WaitForCI:       v.WaitForCI,
		Stopped:         v.Stopped,
		CreatedAt:       v.CreatedAt,
	}
	for _, d := range v.Domains {
		out.Domains = append(out.Domains, domainOut{Host: d.Host, Generated: d.Generated})
	}
	for _, vol := range v.Volumes {
		out.Volumes = append(out.Volumes, volumeOut{ID: vol.ID, MountPath: vol.MountPath})
	}
	if d := v.LatestDeployment; d != nil {
		dep := toDeployment(*d)
		out.LatestDeployment = &dep
	}
	if f := v.RestoreFence; f != nil {
		out.RestoreFence = &restoreFenceOut{RestoreID: f.RestoreID, Phase: string(f.Phase), CreatedAt: f.CreatedAt}
	}
	return out
}

func toDeployment(d control.DeploymentView) deploymentOut {
	return deploymentOut{
		ID:             d.ID,
		ServiceID:      d.ServiceID,
		Status:         string(d.Status),
		Trigger:        string(d.Trigger),
		CommitSHA:      d.CommitSHA,
		CommitMessage:  d.CommitMessage,
		CommitAuthor:   d.CommitAuthor,
		Image:          d.Image,
		Error:          d.Error,
		CreatedAt:      d.CreatedAt,
		StartedAt:      d.StartedAt,
		FinishedAt:     d.FinishedAt,
		ImageAvailable: d.ImageAvailable,
	}
}

func toVariables(v control.VariableNames) variablesOut {
	out := variablesOut{Variables: make([]variableOut, 0, len(v.Variables)), ResolveError: v.ResolveError}
	for _, n := range v.Variables {
		out.Variables = append(out.Variables, variableOut{Name: n.Name, Injected: n.Injected, Reference: n.Reference})
	}
	return out
}

func toBackups(l control.BackupList) backupsOut {
	n := min(len(l.Backups), maxBackups)
	out := backupsOut{
		Policy: backupPolicyOut{
			Enabled:     l.Policy.Enabled,
			Schedule:    l.Policy.Schedule,
			Compression: l.Policy.Compression,
			KeepLocal:   l.Policy.KeepLocal,
			Upload:      l.Policy.Upload,
			KeepRemote:  l.Policy.KeepRemote,
			NextRunAt:   l.Policy.NextRun,
		},
		Backups: make([]backupOut, 0, n),
		Omitted: len(l.Backups) - n,
	}
	for _, b := range l.Backups[:n] {
		out.Backups = append(out.Backups, backupOut{
			ID:          b.ID,
			Trigger:     string(b.Trigger),
			Method:      string(b.Method),
			Status:      string(b.Status),
			Size:        b.Size,
			Encrypted:   b.Encrypted,
			Local:       b.Local,
			Remote:      b.RemoteKey != "",
			RemoteError: b.RemoteError,
			Error:       b.Error,
			CreatedAt:   b.CreatedAt,
			FinishedAt:  b.FinishedAt,
		})
	}
	if r := l.Restore; r != nil {
		out.Restore = &restoreOut{
			ID: r.ID, BackupID: r.BackupID, Status: string(r.Status), Error: r.Error,
			CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt,
		}
	}
	return out
}

func toUpdate(st update.Status) updateOut {
	out := updateOut{
		Current:      st.Current,
		Available:    st.Available,
		State:        string(st.State),
		Staged:       st.Staged,
		Error:        st.Err,
		AutoDownload: st.AutoDownload,
		Unsupported:  st.Unsupported,
	}
	if !st.CheckedAt.IsZero() {
		out.CheckedAt = &st.CheckedAt
	}
	if r := st.Latest; r != nil {
		out.Latest = &releaseOut{Version: r.Version, URL: r.URL, PublishedAt: r.PublishedAt}
	}
	return out
}

func toServiceMetrics(s metrics.Series) serviceMetricsOut {
	return serviceMetricsOut{
		Range:       string(s.Range),
		From:        s.Start,
		StepSeconds: int(s.Step.Seconds()),
		CPULimit:    s.CPULimit,
		MemoryLimit: s.MemoryLimit,
		CPU:         summarize(s.CPU),
		Memory:      summarize(s.Memory),
		NetRx:       summarize(s.NetRx),
		NetTx:       summarize(s.NetTx),
		DiskRead:    summarize(s.DiskRead),
		DiskWrite:   summarize(s.DiskWrite),
	}
}

func toHostMetrics(s metrics.HostSeries) hostMetricsOut {
	return hostMetricsOut{
		Range:       string(s.Range),
		From:        s.Start,
		StepSeconds: int(s.Step.Seconds()),
		CPUs:        s.CPUs,
		MemoryTotal: s.MemoryTotal,
		DiskTotal:   s.DiskTotal,
		CPU:         summarize(s.CPU),
		Memory:      summarize(s.Memory),
		DiskUsed:    summarize(s.DiskUsed),
		NetRx:       summarize(s.NetRx),
		NetTx:       summarize(s.NetTx),
		DiskRead:    summarize(s.DiskRead),
		DiskWrite:   summarize(s.DiskWrite),
	}
}

// summarize returns the latest, average, and maximum of the sampled points,
// rounded to two decimals. Nil points were not sampled.
func summarize(points []*float64) stat {
	var (
		out        stat
		sum, peak  float64
		n          int
		latest     float64
		haveLatest bool
	)
	for _, p := range points {
		if p == nil {
			continue
		}
		if n == 0 || *p > peak {
			peak = *p
		}
		sum += *p
		n++
		latest, haveLatest = *p, true
	}
	if !haveLatest {
		return out
	}
	out.Latest = round(latest)
	out.Avg = round(sum / float64(n))
	out.Max = round(peak)
	return out
}

func round(v float64) *float64 {
	r := math.Round(v*100) / 100
	return &r
}
