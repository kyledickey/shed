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
			s, err := r.ReadString('\n')
			if err != nil {
				if !errors.Is(err, io.EOF) {
					return err
				}
				partial += s
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

// RuntimeLogs writes the last tail lines of the output of a service's active
// container to w, then follows it until ctx ends or the container stops. It
// returns ErrNoContainer if the service has no active container.
func (d *Deployer) RuntimeLogs(ctx context.Context, serviceID string, tail int, w io.Writer) error {
	dep, err := d.store.ActiveDeployment(ctx, serviceID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && dep.ContainerID == "") {
		return ErrNoContainer
	}
	if err != nil {
		return err
	}
	return d.docker.Logs(ctx, dep.ContainerID, tail, true, w)
}
