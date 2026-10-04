package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// ExitError reports that a command run by Exec exited with a non-zero status.
type ExitError struct {
	Code int
}

// Error implements the error interface.
func (e *ExitError) Error() string {
	return fmt.Sprintf("docker: exit status %d", e.Code)
}

// Exec runs cmd in the running container id. stdin may be nil. stdout and
// stderr receive the command's output separately. Output is streamed, never
// buffered in memory. It returns an *ExitError when the command exits non-zero.
//
// stdin is copied in full and then closed, so the command sees EOF. If reading
// stdin fails, the connection is aborted rather than closed cleanly, so the
// command cannot mistake truncated input for complete input, and Exec returns
// the read error. Exec does not return until the copy of stdin has finished,
// so a Read on stdin must not block forever once the command is gone.
//
// Canceling ctx aborts the stream and returns ctx's error. The command itself
// may keep running in the container.
func (c *Client) Exec(ctx context.Context, id string, cmd []string, stdin io.Reader, stdout, stderr io.Writer) error {
	created, err := c.api.ExecCreate(ctx, id, client.ExecCreateOptions{
		AttachStdin:  stdin != nil,
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
	})
	if err != nil {
		return fmt.Errorf("docker: exec in %s: create: %w", id, err)
	}
	hr, err := c.api.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return fmt.Errorf("docker: exec in %s: attach: %w", id, err)
	}
	defer hr.Close()
	// Closing the hijacked connection is the only way to interrupt a blocked
	// read or write on it.
	defer context.AfterFunc(ctx, hr.Close)()

	stdinDone := make(chan error, 1)
	if stdin != nil {
		go func() { stdinDone <- pumpStdin(&hr.HijackedResponse, stdin) }()
	} else {
		stdinDone <- nil
	}

	_, copyErr := stdcopy.StdCopy(stdout, stderr, hr.Reader)
	// The output is over, so any remaining stdin writes are pointless. Closing
	// also unblocks a pump that is stuck writing to a command that has exited.
	hr.Close()
	stdinErr := <-stdinDone

	if ctx.Err() != nil {
		return fmt.Errorf("docker: exec in %s: %w", id, ctx.Err())
	}
	if stdinErr != nil {
		return fmt.Errorf("docker: exec in %s: read stdin: %w", id, stdinErr)
	}
	if copyErr != nil {
		return fmt.Errorf("docker: exec in %s: read output: %w", id, copyErr)
	}

	code, err := c.exitCode(ctx, created.ID)
	if err != nil {
		return fmt.Errorf("docker: exec in %s: %w", id, err)
	}
	if code != 0 {
		return &ExitError{Code: code}
	}
	return nil
}

// pumpStdin copies stdin to the exec connection and then half-closes it. A
// failure to write is not reported: it means the command or the connection
// went away, which the output stream reports. A failure to read aborts the
// connection and is returned.
func pumpStdin(hr *client.HijackedResponse, stdin io.Reader) error {
	r := &errReader{r: stdin}
	_, werr := io.Copy(hr.Conn, r)
	if r.err != nil {
		hr.Close()
		return r.err
	}
	if werr != nil {
		return nil
	}
	// A failed CloseWrite means the connection is already gone.
	_ = hr.CloseWrite()
	return nil
}

// errReader remembers the first read error other than io.EOF, so that it can
// be told apart from a failure to write.
type errReader struct {
	r   io.Reader
	err error
}

func (e *errReader) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) && e.err == nil {
		e.err = err
	}
	return n, err
}

// exitCode returns the exit code of the finished exec. The daemon can report
// the exec as running for a moment after its output stream closes.
func (c *Client) exitCode(ctx context.Context, execID string) (int, error) {
	for {
		res, err := c.api.ExecInspect(ctx, execID, client.ExecInspectOptions{})
		if err != nil {
			return 0, fmt.Errorf("inspect exec: %w", err)
		}
		if !res.Running {
			return res.ExitCode, nil
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// CopyFrom returns a tar stream of path in container id, using the Docker
// archive API. It works on created and stopped containers. The caller closes
// the stream.
func (c *Client) CopyFrom(ctx context.Context, id, path string) (io.ReadCloser, error) {
	res, err := c.api.CopyFromContainer(ctx, id, client.CopyFromContainerOptions{SourcePath: path})
	if err != nil {
		return nil, fmt.Errorf("docker: copy %s from %s: %w", path, id, err)
	}
	return res.Content, nil
}

// CopyTo extracts the tar stream r into the directory dir of container id. The
// directory must exist.
func (c *Client) CopyTo(ctx context.Context, id, dir string, r io.Reader) error {
	_, err := c.api.CopyToContainer(ctx, id, client.CopyToContainerOptions{DestinationPath: dir, Content: r})
	if err != nil {
		return fmt.Errorf("docker: copy to %s in %s: %w", dir, id, err)
	}
	return nil
}
