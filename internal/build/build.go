// Package build clones a Git commit and builds a Docker image from it, using
// the repository's Dockerfile when there is one and Railpack otherwise.
//
// It shells out to git, docker buildx, and railpack, which must be on PATH.
package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const railpackFrontend = "ghcr.io/railwayapp/railpack-frontend"

// envKeyPattern restricts build variable names, which are embedded in command
// line arguments.
var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Request describes an image to build.
type Request struct {
	// RepoURL is the clone URL. It may embed credentials and is never logged.
	RepoURL string
	// Commit is the commit SHA (or ref) to build.
	Commit string
	// RootDir is the repository subdirectory used as the build context.
	// Empty means the repository root.
	RootDir string
	// DockerfilePath is the Dockerfile location relative to RootDir. Empty
	// means Dockerfile if it exists, and Railpack otherwise.
	DockerfilePath string
	// Image is the tag to produce, such as "shed/abc:def".
	Image string
	// Env holds build-time variables.
	Env map[string]string
}

// Builder builds images in per-build workspaces.
type Builder struct {
	// WorkDir is the parent directory of the per-build workspaces.
	WorkDir string
}

// Build clones req.Commit and builds req.Image, streaming progress and command
// output to out. The workspace <WorkDir>/<id> is removed when Build returns.
// Commands are killed if ctx is canceled.
func (b *Builder) Build(ctx context.Context, id string, req Request, out io.Writer) error {
	if err := req.validate(id); err != nil {
		return fmt.Errorf("build: %w", err)
	}
	workspace, err := filepath.Abs(filepath.Join(b.WorkDir, id))
	if err != nil {
		return fmt.Errorf("build: resolve workspace: %w", err)
	}
	if err := os.RemoveAll(workspace); err != nil {
		return fmt.Errorf("build: reset workspace: %w", err)
	}
	repo := filepath.Join(workspace, "src")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		return fmt.Errorf("build: create workspace: %w", err)
	}
	defer os.RemoveAll(workspace)

	w := newRedactor(out, secrets(req.RepoURL))
	defer w.Flush()

	fmt.Fprintln(w, "==> Cloning")
	if err := clone(ctx, repo, req, w); err != nil {
		return err
	}

	contextDir, err := resolve(repo, req.RootDir)
	if err != nil {
		return fmt.Errorf("build: root directory: %w", err)
	}
	dockerfile, err := findDockerfile(repo, contextDir, req.DockerfilePath)
	if err != nil {
		return fmt.Errorf("build: dockerfile: %w", err)
	}

	if dockerfile != "" {
		fmt.Fprintln(w, "==> Building with Dockerfile")
		err = run(ctx, contextDir, nil, w, "docker", dockerfileArgs(req, dockerfile, contextDir)...)
		if err != nil {
			return fmt.Errorf("build: docker build: %w", err)
		}
		return nil
	}

	fmt.Fprintln(w, "==> Building with Railpack")
	plan := filepath.Join(contextDir, "railpack-plan.json")
	info := filepath.Join(contextDir, "railpack-info.json")
	err = run(ctx, contextDir, nil, w, "railpack", railpackPrepareArgs(req, contextDir, plan, info)...)
	if err != nil {
		return fmt.Errorf("build: railpack prepare: %w", err)
	}
	args, env := railpackBuildArgs(req, contextDir, plan)
	if err := run(ctx, contextDir, env, w, "docker", args...); err != nil {
		return fmt.Errorf("build: docker build: %w", err)
	}
	return nil
}

func (r Request) validate(id string) error {
	switch {
	case !filepath.IsLocal(id) || id != filepath.Base(id):
		return fmt.Errorf("invalid build id %q", id)
	case r.RepoURL == "" || strings.HasPrefix(r.RepoURL, "-"):
		return errors.New("invalid repository URL")
	case r.Commit == "" || strings.HasPrefix(r.Commit, "-"):
		return fmt.Errorf("invalid commit %q", r.Commit)
	case r.Image == "":
		return errors.New("image is required")
	case !isLocal(r.RootDir):
		return fmt.Errorf("root directory %q escapes the repository", r.RootDir)
	case !isLocal(r.DockerfilePath):
		return fmt.Errorf("dockerfile path %q escapes the build context", r.DockerfilePath)
	}
	for k := range r.Env {
		if !envKeyPattern.MatchString(k) {
			return fmt.Errorf("invalid variable name %q", k)
		}
	}
	return nil
}

// isLocal reports whether the relative path rel stays within its base
// directory. The empty path is local.
func isLocal(rel string) bool {
	return rel == "" || filepath.IsLocal(rel)
}

// clone fetches req.Commit into the empty directory dir.
func clone(ctx context.Context, dir string, req Request, out io.Writer) error {
	env := []string{"GIT_TERMINAL_PROMPT=0"}
	steps := []struct {
		name string
		args []string
	}{
		{"init", []string{"init", "--quiet"}},
		{"fetch", []string{"fetch", "--quiet", "--depth", "1", req.RepoURL, req.Commit}},
		{"checkout", []string{"checkout", "--quiet", "FETCH_HEAD"}},
	}
	for _, s := range steps {
		if err := run(ctx, dir, env, out, "git", s.args...); err != nil {
			return fmt.Errorf("build: git %s: %w", s.name, err)
		}
	}
	return nil
}

// resolve returns the absolute, symlink-free path of rel inside repo. It
// fails if the path does not exist or leaves repo through a symlink.
func resolve(repo, rel string) (string, error) {
	root, err := filepath.EvalSymlinks(repo)
	if err != nil {
		return "", err
	}
	p, err := filepath.EvalSymlinks(filepath.Join(root, rel))
	if err != nil {
		return "", err
	}
	if r, err := filepath.Rel(root, p); err != nil || !filepath.IsLocal(r) {
		return "", fmt.Errorf("%q escapes the repository", rel)
	}
	return p, nil
}

// findDockerfile returns the Dockerfile to build with, or "" if Railpack
// should be used. An explicitly configured Dockerfile must exist.
func findDockerfile(repo, contextDir, configured string) (string, error) {
	rel, err := filepath.Rel(repo, contextDir)
	if err != nil {
		return "", err
	}
	if configured != "" {
		return resolve(repo, filepath.Join(rel, configured))
	}
	p, err := resolve(repo, filepath.Join(rel, "Dockerfile"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return p, err
}

// dockerfileArgs returns the docker arguments for a Dockerfile build.
func dockerfileArgs(req Request, dockerfile, contextDir string) []string {
	args := []string{"buildx", "build", "--load", "-t", req.Image, "-f", dockerfile}
	for _, k := range sortedKeys(req.Env) {
		args = append(args, "--build-arg", k+"="+req.Env[k])
	}
	return append(args, contextDir)
}

// railpackPrepareArgs returns the railpack arguments that write the build plan.
func railpackPrepareArgs(req Request, contextDir, plan, info string) []string {
	args := []string{"prepare", contextDir, "--plan-out", plan, "--info-out", info}
	for _, k := range sortedKeys(req.Env) {
		args = append(args, "--env", k+"="+req.Env[k])
	}
	return args
}

// railpackBuildArgs returns the docker arguments that build plan with the
// Railpack frontend, and the environment variables that carry its secrets.
func railpackBuildArgs(req Request, contextDir, plan string) (args, env []string) {
	args = []string{
		"buildx", "build", "--load", "-t", req.Image,
		"--build-arg", "BUILDKIT_SYNTAX=" + railpackFrontend,
		"-f", plan,
	}
	for _, k := range sortedKeys(req.Env) {
		args = append(args, "--secret", "id="+k+",env="+k)
		env = append(env, k+"="+req.Env[k])
	}
	return append(args, contextDir), env
}

func sortedKeys(m map[string]string) []string {
	return slices.Sorted(maps.Keys(m))
}

// run runs a command in dir with extra environment variables, streaming its
// stdout and stderr to out. On cancellation the command is interrupted, then
// killed if it does not exit promptly.
func run(ctx context.Context, dir string, env []string, out io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second
	return cmd.Run()
}
