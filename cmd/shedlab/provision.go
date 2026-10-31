package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kyledickey/shed/internal/github"
)

func detached() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// repoRoot returns the shed checkout this command runs in.
func repoRoot(ctx context.Context) (string, error) {
	out, err := run(ctx, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", errors.New("run shedlab inside the shed repository")
	}
	root := strings.TrimSpace(out)
	if _, err := os.Stat(filepath.Join(root, "cmd", "shed", "main.go")); err != nil {
		return "", errors.New("run shedlab inside the shed repository")
	}
	return root, nil
}

// buildShed compiles this checkout, rebuilding the dashboard first if any of
// its sources are newer than the build the binary would embed.
func buildShed(ctx context.Context, p *progress, root string) (string, error) {
	if webStale(root) {
		p.set("building the dashboard")
		cmd := exec.CommandContext(ctx, "make", "web")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("make web: %w\n%s", err, tail(out))
		}
	}
	p.set("compiling")
	version, _ := run(ctx, "git", "-C", root, "describe", "--tags", "--always", "--dirty")
	output := filepath.Join(dataDir(), "shed")
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath",
		"-ldflags", "-s -w -X main.version=dev-"+strings.TrimSpace(version), "-o", output, "./cmd/shed")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build: %w\n%s", err, tail(out))
	}
	return output, nil
}

func webStale(root string) bool {
	built, err := os.Stat(filepath.Join(root, "web", "dist", "index.html"))
	if err != nil {
		return true
	}
	stale := false
	_ = filepath.WalkDir(filepath.Join(root, "web", "src"), func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if info, err := d.Info(); err == nil && info.ModTime().After(built.ModTime()) {
			stale = true
			return filepath.SkipAll
		}
		return nil
	})
	return stale
}

// guestConfig is the lab's /etc/shed/shed.toml. It differs from an installed
// host's only in using the slot's ports and a loopback domain for apps, whose
// certificates then come from Caddy's internal CA.
func guestConfig(l lab) string {
	s := l.Slot
	return fmt.Sprintf(`[server]
listen = "0.0.0.0:%d"
url = %q

[data]
dir = "/var/lib/shed"

[proxy]
enabled = true
http_port = %d
https_port = %d
acme_email = ""
base_domain = %q

[auth]
allowed_users = [%q]

[build]
memory_mb = 2048
cpus = 2
min_free_mb = 2048

[deployments]
log_max_mb = 10
keep = 50

[log]
level = "info"
`, s.dashPort(), l.URL, s.httpPort(), s.httpsPort(), s.localHost(), l.Login)
}

// configure writes the config for this slot.
func configure(ctx context.Context, l lab) error {
	return l.vm().sshInput(ctx, guestConfig(l), "sudo tee /etc/shed/shed.toml >/dev/null && sudo chmod 600 /etc/shed/shed.toml")
}

// installBinary replaces shed in the guest and restarts it.
func installBinary(ctx context.Context, l lab, binary string) error {
	v := l.vm()
	if err := v.upload(ctx, binary, "/tmp/shed"); err != nil {
		return err
	}
	if _, err := v.ssh(ctx, "sudo install -m 755 /tmp/shed /usr/local/bin/shed && rm /tmp/shed && sudo systemctl enable -q shed && sudo systemctl restart shed"); err != nil {
		return err
	}
	return waitHealthy(ctx, l)
}

func waitHealthy(ctx context.Context, l lab) error {
	return poll(ctx, 2*time.Minute, "shed to answer on "+l.Slot.name(), func() error {
		return apiClient{base: l.api().base}.do(ctx, http.MethodGet, "/api/setup", nil, nil)
	})
}

// storeTime is the layout shed's store writes times in.
const storeTime = "2006-01-02T15:04:05.000Z"

// seedSession adds the lab's user and a session directly to shed.db. shed has
// no way around GitHub sign-in, by design; this needs root on the machine,
// which is exactly what makes it safe to do from here and nowhere else.
func seedSession(ctx context.Context, l lab, githubID int64) error {
	sum := sha256.Sum256([]byte(l.Session))
	now := time.Now().UTC()
	sql := fmt.Sprintf(`INSERT INTO users (github_id, login, created_at) VALUES (%d, '%s', '%s')
ON CONFLICT (github_id) DO UPDATE SET login = excluded.login;
INSERT OR REPLACE INTO sessions (token_hash, github_id, expires_at) VALUES ('%s', %d, '%s');
`, githubID, l.Login, now.Format(storeTime), hex.EncodeToString(sum[:]), githubID, now.AddDate(1, 0, 0).Format(storeTime))
	return l.vm().sshInput(ctx, sql, "sudo sqlite3 -cmd '.timeout 10000' "+guestShedDB)
}

// importApp gives the lab the saved GitHub App through the same endpoint as
// the setup page's import form, then points the App's webhook at this lab if
// the lab is reachable from GitHub.
func importApp(ctx context.Context, l lab, c credentials) error {
	var setup struct {
		Configured bool `json:"githubConfigured"`
	}
	if err := l.api().do(ctx, http.MethodGet, "/api/setup", nil, &setup); err != nil {
		return err
	}
	// A lab started from a snapshot may already have the App.
	if !setup.Configured {
		log, err := l.vm().ssh(ctx, "sudo cat /etc/shed/shed.log")
		if err != nil {
			return err
		}
		token, ok := parseSetupToken(log)
		if !ok {
			return errors.New("no setup token in shed's log")
		}
		err = l.api().do(ctx, http.MethodPost, "/api/setup/github/import", map[string]any{
			"token": token, "appId": c.App.ID, "clientId": c.App.ClientID, "clientSecret": c.App.ClientSecret,
			"webhookSecret": c.App.WebhookSecret, "privateKey": c.App.PrivateKey,
		}, nil)
		if err != nil {
			return err
		}
	}
	if !l.Public {
		return nil
	}
	client, err := github.New(github.App{ID: c.App.ID, ClientID: c.App.ClientID, PrivateKey: []byte(c.App.PrivateKey)}, nil)
	if err != nil {
		return err
	}
	return client.SetWebhookURL(ctx, l.URL+"/api/github/webhook")
}

// provision is what `up` needs to know.
type provision struct {
	scenario scenario
	release  string
	from     string // Snapshot name, or empty.
	noAuth   bool
}

// planUp returns the lab and the steps that bring it up. The slot is claimed
// here; its directory stays on failure so that the logs can be read.
func planUp(ctx context.Context, opts provision) (lab, []step, error) {
	root, err := repoRoot(ctx)
	if err != nil {
		return lab{}, nil, err
	}
	creds, err := loadCredentials()
	if err != nil {
		return lab{}, nil, err
	}
	s, err := freeSlot()
	if err != nil {
		return lab{}, nil, err
	}
	session, err := randomToken()
	if err != nil {
		return lab{}, nil, err
	}
	l := lab{
		Slot: s, Scenario: opts.scenario.Name, Source: "dev", Auth: authNone, Login: "lab", URL: s.localURL(),
		Session: session, CPUs: opts.scenario.CPUs, MemoryGB: opts.scenario.MemoryGB, Created: time.Now(),
	}
	githubID := int64(1)
	if !opts.noAuth && creds.App != nil {
		l.Auth, l.Login, githubID = authGitHub, creds.Login, creds.GitHubID
		if creds.Tunnel {
			l.Public, l.URL = true, s.publicURL()
		}
	}
	if opts.scenario.needsGitHub() && l.Auth != authGitHub {
		os.RemoveAll(labDir(s))
		return lab{}, nil, fmt.Errorf("scenario %s deploys from GitHub; run `shedlab auth` first", opts.scenario.Name)
	}
	if opts.release != "" {
		l.Source = opts.release
	}
	backing := basePath()
	if opts.from != "" {
		backing = snapshotDisk(opts.from)
		l.Source = "snapshot " + opts.from
	}
	if err := l.save(); err != nil {
		return lab{}, nil, err
	}

	var binary string
	var steps []step
	if _, err := os.Stat(basePath()); errors.Is(err, fs.ErrNotExist) && opts.from == "" {
		steps = append(steps, step{"Build base image (first run only)", func(ctx context.Context, p *progress) error {
			return bakeBase(ctx, p, root)
		}})
	}
	if opts.release == "" {
		steps = append(steps, step{"Build shed", func(ctx context.Context, p *progress) (err error) {
			binary, err = buildShed(ctx, p, root)
			return err
		}})
	}
	steps = append(steps,
		step{"Boot " + s.name(), func(ctx context.Context, p *progress) error {
			publicKey, err := ensureSSHKey(ctx)
			if err != nil {
				return err
			}
			v := l.vm()
			if err := v.clone(ctx, backing, opts.scenario.DiskGB); err != nil {
				return err
			}
			if err := v.writeSeed(publicKey); err != nil {
				return err
			}
			if err := v.start(ctx); err != nil {
				return err
			}
			return v.waitSSH(ctx)
		}},
		step{"Start shed", func(ctx context.Context, p *progress) error {
			if err := configure(ctx, l); err != nil {
				return err
			}
			if opts.release != "" {
				return installRelease(ctx, l, root, opts.release)
			}
			return installBinary(ctx, l, binary)
		}},
		step{"Sign in", func(ctx context.Context, p *progress) error {
			if err := seedSession(ctx, l, githubID); err != nil {
				return err
			}
			if l.Auth == authNone {
				return nil
			}
			p.set("importing the GitHub App")
			if err := importApp(ctx, l, creds); err != nil {
				return err
			}
			if l.Public {
				return ensureTunnel()
			}
			return nil
		}},
	)
	if opts.from == "" && (opts.scenario.Telemetry || len(opts.scenario.Projects) > 0) {
		steps = append(steps, step{"Deploy " + opts.scenario.Name, func(ctx context.Context, p *progress) error {
			return l.api().apply(ctx, p, opts.scenario)
		}})
	}
	if n := opts.scenario.serviceCount(); opts.from == "" && n > 0 {
		steps = append(steps, step{"Wait for services", func(ctx context.Context, p *progress) error {
			return l.api().waitForServices(ctx, p, n)
		}})
	}
	return l, steps, nil
}

// installRelease runs the real installer for a release; with the config
// already in place it behaves as an upgrade.
func installRelease(ctx context.Context, l lab, root, release string) error {
	v := l.vm()
	if err := v.upload(ctx, filepath.Join(root, "deploy", "install.sh"), "/tmp/install.sh"); err != nil {
		return err
	}
	if _, err := v.ssh(ctx, "sudo systemctl enable -q shed && sudo SHED_YES=1 SHED_VERSION="+release+" bash /tmp/install.sh"); err != nil {
		return err
	}
	return waitHealthy(ctx, l)
}

func snapshotDir() string             { return filepath.Join(dataDir(), "snapshots") }
func snapshotDisk(name string) string { return filepath.Join(snapshotDir(), name+".qcow2") }
