package control

import (
	"context"
	"errors"
	"io"
	"maps"
	"slices"

	"github.com/kyledickey/shed/internal/build"
	"github.com/kyledickey/shed/internal/github"
	"github.com/kyledickey/shed/internal/metrics"
	"github.com/kyledickey/shed/internal/update"
)

// parseRange parses a metrics range, empty meaning the last hour.
func parseRange(s string) (metrics.Range, error) {
	r, err := metrics.ParseRange(s)
	if err != nil {
		return "", errorf(ErrInvalid, "range must be one of 1h, 6h, 24h, or 7d")
	}
	return r, nil
}

// ServiceMetrics returns a service's resource usage over rng: "1h", "6h",
// "24h", or "7d", empty meaning "1h".
func (p *Plane) ServiceMetrics(ctx context.Context, serviceID, rng string) (metrics.Series, error) {
	r, err := parseRange(rng)
	if err != nil {
		return metrics.Series{}, err
	}
	svc, err := p.store.Service(ctx, serviceID)
	if err != nil {
		return metrics.Series{}, err
	}
	return p.metrics.Query(ctx, svc.ID, r)
}

// HostMetrics returns the host's resource usage over rng, as for
// ServiceMetrics.
func (p *Plane) HostMetrics(ctx context.Context, rng string) (metrics.HostSeries, error) {
	r, err := parseRange(rng)
	if err != nil {
		return metrics.HostSeries{}, err
	}
	return p.metrics.QueryHost(ctx, r)
}

// ShedLog returns the last n lines of shed's own log, n clamped to 1 through
// MaxLogLines, with the saved values of every service's variables masked.
func (p *Plane) ShedLog(ctx context.Context, n int) ([]string, error) {
	lines := p.logs.Lines(logLines(n))
	services, err := p.store.AllServices(ctx)
	if err != nil {
		return nil, err
	}
	var values []string
	for _, svc := range services {
		vars, err := p.store.Variables(ctx, svc.ID)
		if err != nil {
			return nil, err
		}
		values = slices.AppendSeq(values, maps.Values(vars))
	}
	redactor := build.NewRedactor(io.Discard, values)
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = redactor.Redact(l)
	}
	return out, nil
}

// FollowShedLog calls emit with the kept lines of shed's own log, then with
// each new line until ctx is done. Values are not masked.
func (p *Plane) FollowShedLog(ctx context.Context, emit func(line string)) {
	p.logs.Follow(ctx, emit)
}

// UpdateStatus returns the state of shed's updates.
func (p *Plane) UpdateStatus(ctx context.Context) (update.Status, error) {
	return p.updates.Status(ctx)
}

// CheckUpdate checks GitHub for a newer release now. A failed check is
// recorded in the status, like a failed background check, rather than
// returned.
func (p *Plane) CheckUpdate(ctx context.Context) (update.Status, error) {
	_, err := p.updates.Check(ctx)
	switch {
	case errors.Is(err, update.ErrUnsupported), errors.Is(err, update.ErrBusy):
		return update.Status{}, err
	case err != nil:
		p.log.Warn("update check failed", "err", err)
	}
	return p.updates.Status(ctx)
}

// DownloadUpdate starts downloading the newest release.
func (p *Plane) DownloadUpdate(ctx context.Context) (update.Status, error) {
	return p.updates.Download(ctx)
}

// SetAutoDownload sets whether new releases are downloaded when found.
func (p *Plane) SetAutoDownload(ctx context.Context, on bool) (update.Status, error) {
	return p.updates.SetAutoDownload(ctx, on)
}

// InstallUpdate swaps in the downloaded release. shed runs it once
// restarted, which is up to the caller.
func (p *Plane) InstallUpdate(ctx context.Context) (update.Status, error) {
	return p.updates.Install(ctx)
}

// Repos returns the repositories the GitHub App is installed on.
func (p *Plane) Repos(ctx context.Context) ([]github.Repo, error) {
	gh, err := p.requireGitHub()
	if err != nil {
		return nil, err
	}
	repos, err := gh.Repos(ctx)
	if err != nil {
		return nil, errorf(ErrUpstream, "%v", err)
	}
	return repos, nil
}

// Branches returns the branch names of a repository the GitHub App is
// installed on.
func (p *Plane) Branches(ctx context.Context, owner, repo string) ([]string, error) {
	fullName := owner + "/" + repo
	if !repoName.MatchString(fullName) {
		return nil, errorf(ErrInvalid, "repository must be owner/name")
	}
	gh, err := p.requireGitHub()
	if err != nil {
		return nil, err
	}
	branches, err := gh.Branches(ctx, fullName)
	if err != nil {
		return nil, errorf(ErrUpstream, "%v", err)
	}
	if branches == nil {
		branches = []string{}
	}
	return branches, nil
}
