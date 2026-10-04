package build

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRequestValidate(t *testing.T) {
	ok := Request{RepoURL: "https://example.com/r.git", Commit: "abc123", Image: "shed/a:b"}
	tests := []struct {
		name    string
		id      string
		mutate  func(*Request)
		wantErr bool
	}{
		{"valid", "id1", func(*Request) {}, false},
		{"valid subdirs", "id1", func(r *Request) { r.RootDir = "apps/web"; r.DockerfilePath = "docker/Dockerfile" }, false},
		{"id with separator", "a/b", func(*Request) {}, true},
		{"id dotdot", "..", func(*Request) {}, true},
		{"empty id", "", func(*Request) {}, true},
		{"missing url", "id1", func(r *Request) { r.RepoURL = "" }, true},
		{"url option injection", "id1", func(r *Request) { r.RepoURL = "--upload-pack=x" }, true},
		{"commit option injection", "id1", func(r *Request) { r.Commit = "--foo" }, true},
		{"missing image", "id1", func(r *Request) { r.Image = "" }, true},
		{"root dotdot", "id1", func(r *Request) { r.RootDir = "../x" }, true},
		{"root absolute", "id1", func(r *Request) { r.RootDir = "/etc" }, true},
		{"root sneaky", "id1", func(r *Request) { r.RootDir = "a/../../x" }, true},
		{"dockerfile dotdot", "id1", func(r *Request) { r.DockerfilePath = "../Dockerfile" }, true},
		{"dockerfile absolute", "id1", func(r *Request) { r.DockerfilePath = "/etc/passwd" }, true},
		{"bad env key", "id1", func(r *Request) { r.Env = map[string]string{"A,env=PATH": "x"} }, true},
		{"good env key", "id1", func(r *Request) { r.Env = map[string]string{"_A1": "x"} }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := ok
			tt.mutate(&req)
			if err := req.validate(tt.id); (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFindDockerfile(t *testing.T) {
	repo := t.TempDir()
	write := func(rel string) {
		t.Helper()
		p := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("FROM scratch\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Dockerfile")
	write("svc/docker/Dockerfile.prod")
	write("noauto/README")
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "Dockerfile"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}

	root, _ := filepath.EvalSymlinks(repo)
	tests := []struct {
		name       string
		rootDir    string
		configured string
		want       string // relative to repo; "" = Railpack
		wantErr    bool
	}{
		{"auto at root", "", "", "Dockerfile", false},
		{"auto absent", "noauto", "", "", false},
		{"configured in subdir", "svc", "docker/Dockerfile.prod", "svc/docker/Dockerfile.prod", false},
		{"configured missing", "", "nope", "", true},
		{"root missing", "missing", "", "", true},
		{"symlink escape for root", "link", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctxDir, err := resolve(repo, tt.rootDir)
			if err == nil {
				var got string
				got, err = findDockerfile(repo, ctxDir, tt.configured)
				if want := filepath.Join(root, tt.want); tt.want != "" && got != want {
					t.Errorf("findDockerfile() = %q, want %q", got, want)
				} else if tt.want == "" && got != "" {
					t.Errorf("findDockerfile() = %q, want Railpack", got)
				}
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestResolveSymlinkedDockerfileEscape(t *testing.T) {
	repo := t.TempDir()
	outside := filepath.Join(t.TempDir(), "Dockerfile")
	if err := os.WriteFile(outside, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "Dockerfile")); err != nil {
		t.Fatal(err)
	}
	if _, err := findDockerfile(repo, repo, ""); err == nil {
		t.Error("findDockerfile() accepted a Dockerfile symlinked outside the repository")
	}
}

func TestArgs(t *testing.T) {
	req := Request{Image: "shed/a:b", Env: map[string]string{"B": "2", "A": "1"}}

	t.Run("dockerfile", func(t *testing.T) {
		got := dockerfileArgs(req, "/ctx/Dockerfile", "/ctx")
		want := []string{"buildx", "build", "--load", "-t", "shed/a:b", "-f", "/ctx/Dockerfile",
			"--build-arg", "A=1", "--build-arg", "B=2", "/ctx"}
		if !slices.Equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("railpack prepare", func(t *testing.T) {
		got := railpackPrepareArgs(req, "/ctx", "/ctx/plan.json", "/ctx/info.json")
		want := []string{"prepare", "/ctx", "--plan-out", "/ctx/plan.json", "--info-out", "/ctx/info.json",
			"--env", "A=1", "--env", "B=2"}
		if !slices.Equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("railpack build", func(t *testing.T) {
		args, env := railpackBuildArgs(req, "/ctx", "/ctx/plan.json")
		want := []string{"buildx", "build", "--load", "-t", "shed/a:b",
			"--build-arg", "BUILDKIT_SYNTAX=ghcr.io/railwayapp/railpack-frontend", "-f", "/ctx/plan.json",
			"--secret", "id=A,env=A", "--secret", "id=B,env=B", "/ctx"}
		if !slices.Equal(args, want) {
			t.Errorf("args = %q, want %q", args, want)
		}
		if wantEnv := []string{"A=1", "B=2"}; !slices.Equal(env, wantEnv) {
			t.Errorf("env = %q, want %q", env, wantEnv)
		}
	})
	t.Run("no env", func(t *testing.T) {
		args, env := railpackBuildArgs(Request{Image: "x"}, "/ctx", "/p")
		if slices.Contains(args, "--secret") || len(env) != 0 {
			t.Errorf("unexpected secrets: %q %q", args, env)
		}
	})
}

func TestRedactor(t *testing.T) {
	const url = "https://x-access-token:s3cr3t@github.com/o/r.git"
	tests := []struct {
		name   string
		writes []string
		want   string
	}{
		{"full url", []string{"fetching " + url + "\n"}, "fetching ***\n"},
		{"password only", []string{"bad token s3cr3t here\n"}, "bad token *** here\n"},
		{"split across writes", []string{"token s3c", "r3t done\nnext\n"}, "token *** done\nnext\n"},
		{"partial last line flushed", []string{"tail s3cr3t"}, "tail ***"},
		{"untouched", []string{"hello\nworld\n"}, "hello\nworld\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			r := newRedactor(&buf, secrets(url))
			for _, w := range tt.writes {
				if _, err := r.Write([]byte(w)); err != nil {
					t.Fatal(err)
				}
			}
			r.Flush()
			if buf.String() != tt.want {
				t.Errorf("got %q, want %q", buf.String(), tt.want)
			}
		})
	}
}

func TestClone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	src := t.TempDir()
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git(src, "init", "--quiet")
	if err := os.WriteFile(filepath.Join(src, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(src, "add", ".")
	git(src, "commit", "--quiet", "-m", "init")
	sha := git(src, "rev-parse", "HEAD")

	dir := t.TempDir()
	var out bytes.Buffer
	req := Request{RepoURL: "file://" + src, Commit: sha}
	if err := clone(context.Background(), dir, req, &out); err != nil {
		t.Fatalf("clone() error = %v\n%s", err, out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "hello.txt")); err != nil {
		t.Error(err)
	}
}

func TestBuildRemovesWorkspace(t *testing.T) {
	work := t.TempDir()
	b := &Builder{WorkDir: work}
	var out bytes.Buffer
	req := Request{RepoURL: "file:///does/not/exist", Commit: "abc", Image: "x"}
	if err := b.Build(context.Background(), "id1", req, &out); err == nil {
		t.Fatal("Build() succeeded, want clone error")
	}
	if !strings.Contains(out.String(), "==> Cloning") {
		t.Errorf("output missing step header: %q", out.String())
	}
	if entries, _ := os.ReadDir(work); len(entries) != 0 {
		t.Errorf("workspace not removed: %v", entries)
	}
}

func TestBuildRelativeWorkDir(t *testing.T) {
	tests := []struct {
		name       string
		dockerfile bool
		rootDir    string
	}{
		{"dockerfile", true, ""},
		{"dockerfile subdirectory", true, "app"},
		{"railpack", false, ""},
		{"railpack subdirectory", false, "app"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			work := t.TempDir()
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			rel, err := filepath.Rel(cwd, work)
			if err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			gitScript := "#!/bin/sh\nset -eu\nmkdir -p app\n"
			if tt.dockerfile {
				gitScript += "touch Dockerfile app/Dockerfile\n"
			}
			scripts := map[string]string{
				"git": gitScript,
				"railpack": `#!/bin/sh
set -eu
test "$1" = prepare
test -d "$2"
test "$3" = --plan-out
touch "$4"
test "$5" = --info-out
touch "$6"
`,
				"docker": `#!/bin/sh
set -eu
while [ "$#" -gt 1 ]; do
  if [ "$1" = -f ]; then
    shift
    test -f "$1"
  fi
  shift
done
test -d "$1"
case "$1" in
  /*) ;;
  *) exit 1 ;;
esac
`,
			}
			for name, script := range scripts {
				if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			b := &Builder{WorkDir: rel}
			req := Request{RepoURL: "https://example.com/r.git", Commit: "abc", Image: "x", RootDir: tt.rootDir}
			var out bytes.Buffer
			if err := b.Build(context.Background(), "id1", req, &out); err != nil {
				t.Fatalf("Build() error = %v\n%s", err, out.String())
			}
			if entries, err := os.ReadDir(work); err != nil || len(entries) != 0 {
				t.Errorf("workspace cleanup: entries = %v, error = %v", entries, err)
			}
		})
	}
}
