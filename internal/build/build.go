// Package build clones a Git commit and builds a Docker image from it, using
// the repository's Dockerfile when there is one and Railpack otherwise.
//
// It shells out to git, docker buildx, and railpack, which must be on PATH.
package build

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
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
	// Instance names a dedicated buildx builder with the docker-container
	// driver. Before the first build it is recreated with the limits below,
	// keeping its build cache. Empty uses Docker's default builder, which
	// workload limits do not constrain.
	Instance string
	// Memory caps the dedicated builder's memory in bytes, without swap.
	// Zero means unlimited.
	Memory int64
	// CPUs caps the dedicated builder's CPU time, in cores. Zero means
	// unlimited.
	CPUs float64
	// MinFree is the free space, in bytes, that the filesystems of WorkDir
	// and Docker's root directory need for a build to start. Zero disables
	// the check.
	MinFree uint64

	once       sync.Once
	slot       chan struct{}
	ready      bool   // set up; guarded by slot
	dockerRoot string // Docker's root directory, if known; guarded by slot
}

// Build clones req.Commit and builds req.Image, streaming progress and command
// output to out. The workspace <WorkDir>/<id> is removed when Build returns.
// Commands are killed if ctx is canceled.
func (b *Builder) Build(ctx context.Context, id string, req Request, out io.Writer) error {
	if err := req.validate(id); err != nil {
		return fmt.Errorf("build: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()
	release, err := b.acquire(ctx)
	if err != nil {
		return fmt.Errorf("build: wait for build slot: %w", err)
	}
	defer release()
	workspace, err := filepath.Abs(filepath.Join(b.WorkDir, id))
	if err != nil {
		return fmt.Errorf("build: resolve workspace: %w", err)
	}
	if err := b.setup(ctx); err != nil {
		return err
	}
	if err := b.checkDisk(); err != nil {
		return err
	}
	if err := os.RemoveAll(workspace); err != nil {
		return fmt.Errorf("build: reset workspace: %w", err)
	}
	repo := filepath.Join(workspace, "src")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		return fmt.Errorf("build: create workspace: %w", err)
	}
	defer os.RemoveAll(workspace)

	w := NewRedactor(out, append(secrets(req.RepoURL), slices.Collect(maps.Values(req.Env))...))
	defer w.Flush()

	fmt.Fprintln(w, "==> Cloning")
	if err := clone(ctx, repo, req, w); err != nil {
		return err
	}

	contextDir, err := resolve(repo, req.RootDir)
	if err != nil {
		return fmt.Errorf("build: root directory: %w", err)
	}
	secretDir := filepath.Join(workspace, "secrets")
	if err := writeSecrets(secretDir, req.Env); err != nil {
		return err
	}
	dockerfile, err := findDockerfile(repo, contextDir, req.DockerfilePath)
	if err != nil {
		return fmt.Errorf("build: dockerfile: %w", err)
	}

	if dockerfile != "" {
		fmt.Fprintln(w, "==> Building with Dockerfile")
		err = run(ctx, contextDir, nil, w, "docker", dockerfileArgs(req, b.Instance, dockerfile, contextDir, secretDir)...)
		if err != nil {
			return fmt.Errorf("build: docker build: %w", err)
		}
		return nil
	}

	fmt.Fprintln(w, "==> Building with Railpack")
	plan := filepath.Join(workspace, "railpack-plan.json")
	info := filepath.Join(workspace, "railpack-info.json")
	err = run(ctx, contextDir, prepareEnv(req.Env), w, "railpack", railpackPrepareArgs(req, contextDir, plan, info)...)
	if err != nil {
		return fmt.Errorf("build: railpack prepare: %w", err)
	}
	args := railpackBuildArgs(req, b.Instance, contextDir, plan, secretDir)
	if err := run(ctx, contextDir, nil, w, "docker", args...); err != nil {
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
	repoURL, err := url.Parse(req.RepoURL)
	if err != nil {
		return fmt.Errorf("build: parse clone URL: %w", err)
	}
	if repoURL.User != nil {
		password, _ := repoURL.User.Password()
		basic := base64.StdEncoding.EncodeToString([]byte(repoURL.User.Username() + ":" + password))
		repoURL.User = nil
		env = append(env, "GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=",
			"GIT_CONFIG_KEY_1=http."+repoURL.Scheme+"://"+repoURL.Host+"/.extraHeader", "GIT_CONFIG_VALUE_1=Authorization: Basic "+basic)
	}

	steps := []struct {
		name string
		args []string
	}{
		{"init", []string{"init", "--quiet"}},
		{"fetch", []string{"fetch", "--quiet", "--depth", "1", repoURL.String(), req.Commit}},
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

// dockerfileArgs returns the docker arguments for a Dockerfile build on the
// buildx builder instance, or on the default builder if instance is empty.
func dockerfileArgs(req Request, instance, dockerfile, contextDir, secretDir string) []string {
	args := []string{"buildx", "build"}
	if instance != "" {
		args = append(args, "--builder", instance)
	}
	args = append(args, "--load", "-t", req.Image, "-f", dockerfile)
	for _, k := range sortedKeys(req.Env) {
		args = append(args, "--secret", "id="+k+",src="+filepath.Join(secretDir, k))
	}
	return append(args, contextDir)
}

// railpackPrepareArgs returns the railpack arguments that write the build plan.
func railpackPrepareArgs(req Request, contextDir, plan, info string) []string {
	args := []string{"prepare", contextDir, "--plan-out", plan, "--info-out", info}
	for _, k := range sortedKeys(req.Env) {
		if prepareKey(k) {
			args = append(args, "--env", k)
		}
	}
	return args
}

// railpackBuildArgs passes secret files without altering the Docker client's environment.
func railpackBuildArgs(req Request, instance, contextDir, plan, secretDir string) []string {
	args := dockerfileArgs(req, instance, plan, contextDir, secretDir)
	return append(args[:len(args)-1], "--build-arg", "BUILDKIT_SYNTAX="+railpackFrontend, contextDir)
}

func writeSecrets(dir string, env map[string]string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("build: create secrets: %w", err)
	}
	for k, v := range env {
		if err := os.WriteFile(filepath.Join(dir, k), []byte(v), 0600); err != nil {
			return fmt.Errorf("build: write secret: %w", err)
		}
	}
	return nil
}

// prepareKey excludes variables that control execution of host tools. They are
// still available as build secrets and in the workload's environment.
func prepareKey(k string) bool {
	if k == "PATH" || k == "HOME" || k == "TMPDIR" {
		return false
	}
	for _, p := range []string{"LD_", "DYLD_", "XDG_", "GIT_", "DOCKER_"} {
		if strings.HasPrefix(k, p) {
			return false
		}
	}
	return true
}

func prepareEnv(env map[string]string) []string {
	var out []string
	for _, k := range sortedKeys(env) {
		if prepareKey(k) {
			out = append(out, k+"="+env[k])
		}
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	return slices.Sorted(maps.Keys(m))
}

// run runs a command in dir with extra environment variables, streaming its
// stdout and stderr to out. Cancellation kills its entire process group.
func run(ctx context.Context, dir string, env []string, out io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 10 * time.Second
	return cmd.Run()
}
