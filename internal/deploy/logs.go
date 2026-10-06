package deploy

import (
	"bufio"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/kyledickey/shed/internal/build"
	"github.com/kyledickey/shed/internal/store"
)

// FollowLog replays the build log of a deployment and follows it while the
// deployment is in progress. It calls line for every log line, and status
// with the current status and then whenever it changes. It returns nil once
// the deployment has finished and its log has been read, or the error of ctx
// if ctx ends first.
func (d *Deployer) FollowLog(ctx context.Context, deploymentID string, line func(string), status func(store.DeploymentStatus)) error {
	dep, err := d.store.Deployment(ctx, deploymentID)
	if err != nil {
		return err
	}
	var (
		f       *os.File
		r       *bufio.Reader
		partial string // an unterminated last line
		last    store.DeploymentStatus
	)
	defer func() {
		if f != nil {
			f.Close()
		}
	}()
	for {
		if dep.Status != last {
			last = dep.Status
			status(last)
		}
		// The status is read before the log, so a finished deployment's log
		// is complete by now.
		done := dep.Status.Terminal()

		if f == nil {
			f, err = os.Open(d.logPath(deploymentID))
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			if f != nil {
				r = bufio.NewReader(f)
			}
		}
		for r != nil {
			chunk, err := r.ReadSlice('\n')
			s := string(chunk)
			if errors.Is(err, bufio.ErrBufferFull) {
				partial += s
				if len(partial) >= maxLine {
					line(partial)
					partial = ""
				}
				continue
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					return err
				}
				partial += s
				if len(partial) >= maxLine {
					line(partial)
					partial = ""
				}
				break
			}
			line(strings.TrimSuffix(partial+s, "\n"))
			partial = ""
		}

		if done {
			if partial != "" {
				line(partial)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d.logPoll):
		}
		if dep, err = d.store.Deployment(ctx, deploymentID); err != nil {
			return err
		}
	}
}

// pruneHistory deletes a service's deployments beyond the newest d.keep,
// with their build logs.
func (d *Deployer) pruneHistory(serviceID string) {
	if d.keep <= 0 {
		return
	}
	ids, err := d.store.PruneDeployments(context.Background(), serviceID, d.keep)
	if err != nil {
		d.log.Error("prune deployment history", "service", serviceID, "err", err)
		return
	}
	for _, id := range ids {
		if err := os.Remove(d.logPath(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			d.log.Warn("remove build log", "deployment", id, "err", err)
		}
	}
}

// BuildLog returns the last n lines of a deployment's build log, for showing
// outside the dashboard. The log was masked as it was written; BuildLog also
// masks the values given by maskedValues, which covers values saved since.
// Lines longer than 64 KiB are split.
func (d *Deployer) BuildLog(ctx context.Context, deploymentID string, n int) ([]string, error) {
	dep, err := d.store.Deployment(ctx, deploymentID)
	if err != nil {
		return nil, err
	}
	lines, err := d.tailLog(deploymentID, n)
	if err != nil {
		return nil, err
	}
	secrets, err := d.maskedValues(ctx, dep)
	if err != nil {
		return nil, err
	}
	redactor := build.NewRedactor(io.Discard, secrets)
	for i, l := range lines {
		lines[i] = redactor.Redact(l)
	}
	return lines, nil
}

// tailLog returns the last n lines of a deployment's build log, or none if
// it has no log.
func (d *Deployer) tailLog(deploymentID string, n int) ([]string, error) {
	if n <= 0 {
		return []string{}, nil
	}
	f, err := os.Open(d.logPath(deploymentID))
	if errors.Is(err, fs.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ring := make([]string, 0, n)
	next := 0
	add := func(line string) {
		if len(ring) < n {
			ring = append(ring, line)
			return
		}
		ring[next] = line
		next = (next + 1) % n
	}
	r := bufio.NewReaderSize(f, maxLine)
	partial := ""
	for {
		chunk, err := r.ReadSlice('\n')
		partial += string(chunk)
		if err == nil {
			add(strings.TrimSuffix(partial, "\n"))
			partial = ""
			continue
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			if len(partial) >= maxLine {
				add(partial)
				partial = ""
			}
			continue
		}
		if !errors.Is(err, io.EOF) {
			return nil, err
		}
		break
	}
	if partial != "" {
		add(partial)
	}
	return append(ring[next:], ring[:next]...), nil
}

// RuntimeLogs writes the last tail lines of the output of a service's active
// container to w. With follow, it then follows the output until ctx ends or
// the container stops. The values of the variables the container was started
// with are masked, not those saved since. It returns ErrNoContainer if the
// service has no active container.
func (d *Deployer) RuntimeLogs(ctx context.Context, serviceID string, tail int, follow bool, w io.Writer) error {
	dep, err := d.store.ActiveDeployment(ctx, serviceID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && dep.ContainerID == "") {
		return ErrNoContainer
	}
	if err != nil {
		return err
	}
	var secrets []string
	if dep.Runtime != nil {
		secrets = secretValues(dep.Runtime.Env, dep.Runtime.SecretKeys)
	} else if secrets, err = d.currentSecrets(ctx, serviceID, dep); err != nil {
		return err
	}
	redactor := build.NewRedactor(w, secrets)
	err = d.docker.Logs(ctx, dep.ContainerID, tail, follow, redactor)
	return errors.Join(err, redactor.Flush())
}

// currentSecrets returns the values to mask in the logs of a deployment
// activated before runtimes were recorded, resolved from the current
// variables.
func (d *Deployer) currentSecrets(ctx context.Context, serviceID string, dep store.Deployment) ([]string, error) {
	svc, err := d.store.Service(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	project, err := d.store.Project(ctx, svc.ProjectID)
	if err != nil {
		return nil, err
	}
	svc.Port = dep.Port
	env, err := d.environment(ctx, svc, project, dep.CommitSHA)
	if err != nil {
		return nil, err
	}
	keys, err := d.secretKeys(ctx, svc, env)
	if err != nil {
		return nil, err
	}
	return secretValues(env, keys), nil
}
