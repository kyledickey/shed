package control

import (
	"bytes"
	"context"
	"io"
	"strings"

	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/store"
)

// MaxDeployments is the most deployments Deployments returns.
const MaxDeployments = 50

// Deployments returns up to limit of a service's newest deployments, newest
// first, with whether each one's image is still on the server. A limit
// outside 1 to MaxDeployments means MaxDeployments.
func (p *Plane) Deployments(ctx context.Context, serviceID string, limit int) ([]DeploymentView, error) {
	if limit <= 0 || limit > MaxDeployments {
		limit = MaxDeployments
	}
	if _, err := p.store.Service(ctx, serviceID); err != nil {
		return nil, err
	}
	deps, err := p.store.Deployments(ctx, serviceID, limit)
	if err != nil {
		return nil, err
	}
	images := make([]string, 0, len(deps))
	for _, d := range deps {
		images = append(images, d.Image)
	}
	available, err := p.deployer.AvailableImages(ctx, serviceID, images)
	if err != nil {
		p.log.Warn("check deployment images", "service", serviceID, "err", err)
		available = nil
	}
	out := make([]DeploymentView, 0, len(deps))
	for _, d := range deps {
		v := toDeployment(d)
		if available != nil && d.Image != "" {
			ok := available[d.Image]
			v.ImageAvailable = &ok
		}
		out = append(out, v)
	}
	return out, nil
}

// Deployment returns a deployment.
func (p *Plane) Deployment(ctx context.Context, id string) (DeploymentView, error) {
	d, err := p.store.Deployment(ctx, id)
	if err != nil {
		return DeploymentView{}, err
	}
	return toDeployment(d), nil
}

// Deploy deploys the head of a service's branch, or its image.
func (p *Plane) Deploy(ctx context.Context, serviceID string) (DeploymentView, error) {
	svc, err := p.store.Service(ctx, serviceID)
	if err != nil {
		return DeploymentView{}, err
	}
	var commit deploy.Commit
	if svc.Kind == "app" && svc.Repo != "" {
		if commit, err = p.branchHead(ctx, svc); err != nil {
			return DeploymentView{}, err
		}
	}
	// Under pushMu, so that a pending push cannot pass its check against
	// the latest deployment and then supersede this one.
	p.pushMu.Lock()
	dep, err := p.deployer.Deploy(ctx, svc.ID, store.TriggerManual, commit)
	p.pushMu.Unlock()
	if err != nil {
		return DeploymentView{}, err
	}
	return toDeployment(dep), nil
}

// Redeploy deploys the image of an earlier deployment again.
func (p *Plane) Redeploy(ctx context.Context, deploymentID string) (DeploymentView, error) {
	p.pushMu.Lock() // see Deploy
	dep, err := p.deployer.Redeploy(ctx, deploymentID)
	p.pushMu.Unlock()
	if err != nil {
		return DeploymentView{}, err
	}
	return toDeployment(dep), nil
}

// CancelDeployment cancels a deployment in progress.
func (p *Plane) CancelDeployment(ctx context.Context, deploymentID string) (DeploymentView, error) {
	dep, err := p.deployer.Cancel(ctx, deploymentID)
	if err != nil {
		return DeploymentView{}, err
	}
	return toDeployment(dep), nil
}

// MaxLogLines is the most lines BuildLog, RuntimeLogs, and ShedLog return.
const MaxLogLines = 2000

// logLines clamps n to 1 through MaxLogLines.
func logLines(n int) int {
	return min(max(n, 1), MaxLogLines)
}

// BuildLog returns the last n lines of a deployment's build log, n clamped
// to 1 through MaxLogLines, with variable values masked.
func (p *Plane) BuildLog(ctx context.Context, deploymentID string, n int) ([]string, error) {
	return p.deployer.BuildLog(ctx, deploymentID, logLines(n))
}

// RuntimeLogs returns the last n lines of the output of a service's active
// container, n clamped to 1 through MaxLogLines, with the values of the
// variables it was started with masked. Each line starts with its RFC 3339
// timestamp. It returns deploy.ErrNoContainer if the service has no active
// container.
func (p *Plane) RuntimeLogs(ctx context.Context, serviceID string, n int) ([]string, error) {
	if _, err := p.store.Service(ctx, serviceID); err != nil {
		return nil, err
	}
	var lines lineBuffer
	if err := p.deployer.RuntimeLogs(ctx, serviceID, logLines(n), false, &lines); err != nil {
		return nil, err
	}
	return lines.done(), nil
}

// FollowBuildLog replays a deployment's build log and follows it while the
// deployment is in progress, calling line for every line and status with
// the current status and each change. It returns nil once the deployment
// has finished, or the error of ctx if ctx ends first.
func (p *Plane) FollowBuildLog(ctx context.Context, deploymentID string, line func(string), status func(store.DeploymentStatus)) error {
	return p.deployer.FollowLog(ctx, deploymentID, line, status)
}

// FollowRuntimeLogs writes the last tail lines of the output of a service's
// active container to w, then follows it until ctx ends or the container
// stops. The values of the variables the container was started with are
// masked. It returns deploy.ErrNoContainer if the service has no active
// container.
func (p *Plane) FollowRuntimeLogs(ctx context.Context, serviceID string, tail int, w io.Writer) error {
	return p.deployer.RuntimeLogs(ctx, serviceID, tail, true, w)
}

// maxLineBytes bounds one line of a log snapshot; longer lines are split.
const maxLineBytes = 64 << 10

// lineBuffer collects written text as lines.
type lineBuffer struct {
	lines   []string
	partial []byte
}

func (b *lineBuffer) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		chunk := p[:min(len(p), maxLineBytes-len(b.partial))]
		if i := bytes.IndexByte(chunk, '\n'); i >= 0 {
			b.partial = append(b.partial, chunk[:i]...)
			b.flush()
			p = p[i+1:]
			continue
		}
		b.partial = append(b.partial, chunk...)
		p = p[len(chunk):]
		if len(b.partial) == maxLineBytes {
			b.flush()
		}
	}
	return n, nil
}

func (b *lineBuffer) flush() {
	b.lines = append(b.lines, strings.TrimSuffix(string(b.partial), "\r"))
	b.partial = b.partial[:0]
}

// done returns the lines, with a final unterminated one.
func (b *lineBuffer) done() []string {
	if len(b.partial) > 0 {
		b.flush()
	}
	if b.lines == nil {
		return []string{}
	}
	return b.lines
}
