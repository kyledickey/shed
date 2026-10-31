package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/kyledickey/shed/internal/github"
)

// sessionCookie is the cookie shed's auth package reads.
const sessionCookie = "shed_session"

// credentials are what `shedlab auth` saves so that labs can reuse them.
type credentials struct {
	Login    string    `json:"login"`
	GitHubID int64     `json:"githubId"`
	App      *savedApp `json:"app,omitempty"`
	Tunnel   bool      `json:"tunnel"`
}

// savedApp is the GitHub App every lab imports.
type savedApp struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	ClientID      string `json:"clientId"`
	ClientSecret  string `json:"clientSecret"`
	WebhookSecret string `json:"webhookSecret"`
	PrivateKey    string `json:"privateKey"`
}

func configDir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "shedlab")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "shedlab")
}

func credentialsPath() string { return filepath.Join(configDir(), "credentials.json") }

// loadCredentials returns the saved credentials, or zero ones if there are none.
func loadCredentials() (credentials, error) {
	var c credentials
	raw, err := os.ReadFile(credentialsPath())
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(raw, &c)
}

func (c credentials) save() error {
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(credentialsPath(), raw, 0o600)
}

// githubUser looks up a login's numeric ID, which shed keys users by.
func githubUser(ctx context.Context, login string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/users/"+login, nil)
	if err != nil {
		return 0, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("look up GitHub user %q: %s", login, res.Status)
	}
	var user struct {
		ID int64 `json:"id"`
	}
	return user.ID, json.NewDecoder(res.Body).Decode(&user)
}

var validLogin = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$`)

func checkLogin(login string) error {
	if !validLogin.MatchString(login) {
		return errors.New("enter a GitHub username")
	}
	return nil
}

// labManifest returns shed's own App manifest, changed so that one App serves
// every lab: it lists each slot's URLs and comes back to this CLI when created.
func labManifest(name, redirect string) ([]byte, error) {
	raw, err := github.Manifest(slot(1).publicURL(), name)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	var callbacks []string
	for s := slot(1); s <= slotCount; s++ {
		callbacks = append(callbacks, s.publicURL()+"/api/auth/callback", s.localURL()+"/api/auth/callback")
	}
	m["callback_urls"] = callbacks
	m["redirect_url"] = redirect
	m["public"] = false
	delete(m, "setup_url")
	return json.Marshal(m)
}

var manifestPage = template.Must(template.New("manifest").Parse(`<!doctype html>
<meta charset="utf-8"><title>Create GitHub App</title>
<form method="post" action="https://github.com/settings/apps/new?state={{.State}}">
<input type="hidden" name="manifest" value="{{.Manifest}}"></form>
<script>document.forms[0].submit()</script>`))

// createGitHubApp walks the browser through GitHub's manifest flow, which
// needs one click, and returns the new App's credentials.
func createGitHubApp(ctx context.Context, p *progress, login string) (github.App, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return github.App{}, err
	}
	defer ln.Close()
	base := "http://" + ln.Addr().String()
	state, err := randomToken()
	if err != nil {
		return github.App{}, err
	}
	name := "shed-lab-" + login
	if len(name) > 34 {
		name = strings.TrimRight(name[:34], "-")
	}
	manifest, err := labManifest(name, base+"/callback")
	if err != nil {
		return github.App{}, err
	}

	type result struct {
		app github.App
		err error
	}
	done := make(chan result, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		manifestPage.Execute(w, struct{ State, Manifest string }{state, string(manifest)})
	})
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "invalid state", http.StatusForbidden)
			return
		}
		app, err := github.ConvertManifest(r.Context(), nil, r.URL.Query().Get("code"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			done <- result{err: err}
			return
		}
		http.Redirect(w, r, "https://github.com/apps/"+app.Slug+"/installations/new", http.StatusFound)
		done <- result{app: app}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	defer srv.Close()

	p.set("approve it in your browser")
	if !openBrowser(base) {
		p.set("open " + base)
	}
	select {
	case r := <-done:
		// Give the redirect to the install page time to reach the browser.
		time.Sleep(200 * time.Millisecond)
		return r.app, r.err
	case <-ctx.Done():
		return github.App{}, ctx.Err()
	}
}

func openBrowser(url string) bool {
	return exec.Command("xdg-open", url).Start() == nil
}

func tunnelDir() string     { return configDir() }
func tunnelCert() string    { return filepath.Join(tunnelDir(), ".cloudflared", "cert.pem") }
func tunnelCreds() string   { return filepath.Join(tunnelDir(), "tunnel.json") }
func tunnelConfig() string  { return filepath.Join(tunnelDir(), "tunnel.yml") }
func tunnelPidFile() string { return filepath.Join(dataDir(), "tunnel.pid") }
func tunnelLogFile() string { return filepath.Join(dataDir(), "tunnel.log") }

// setupTunnel creates the Cloudflare tunnel and the DNS records for every
// slot. Each part is skipped if it already exists, so a rerun picks up where a
// failed one stopped. The login happens under a private HOME so that it can't
// overwrite the certificate of whichever zone cloudflared already uses.
func setupTunnel(ctx context.Context, p *progress) error {
	if err := os.MkdirAll(dataDir(), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(tunnelCert()); errors.Is(err, fs.ErrNotExist) {
		if err := cloudflaredLogin(ctx, p); err != nil {
			return err
		}
	}
	cf := func(args ...string) (string, error) {
		return run(ctx, "cloudflared", append([]string{"tunnel", "--origincert", tunnelCert()}, args...)...)
	}
	if _, err := os.Stat(tunnelCreds()); errors.Is(err, fs.ErrNotExist) {
		p.set("creating the tunnel")
		if _, err := cf("create", "--credentials-file", tunnelCreds(), "shedlab"); err != nil {
			return err
		}
	}
	raw, err := os.ReadFile(tunnelCreds())
	if err != nil {
		return err
	}
	var creds struct {
		TunnelID string `json:"TunnelID"`
	}
	if err := json.Unmarshal(raw, &creds); err != nil {
		return err
	}
	var ingress strings.Builder
	for s := slot(1); s <= slotCount; s++ {
		host := s.name() + "." + publicDomain
		p.set("pointing " + host + " at it")
		if _, err := cf("route", "dns", "--overwrite-dns", creds.TunnelID, host); err != nil {
			return err
		}
		fmt.Fprintf(&ingress, "  - hostname: %s\n    service: http://127.0.0.1:%d\n", host, s.dashPort())
	}
	config := fmt.Sprintf("tunnel: %s\ncredentials-file: %s\ningress:\n%s  - service: http_status:404\n",
		creds.TunnelID, tunnelCreds(), ingress.String())
	return writeFileAtomic(tunnelConfig(), []byte(config), 0o600)
}

var cloudflareLoginURL = regexp.MustCompile(`https://\S+`)

// cloudflaredLogin waits for the browser step of `cloudflared tunnel login`,
// showing the link in case cloudflared couldn't open the browser.
func cloudflaredLogin(ctx context.Context, p *progress) error {
	p.set("choose " + publicDomain + " in your browser")
	cmd := exec.CommandContext(ctx, "cloudflared", "tunnel", "login")
	cmd.Env = append(os.Environ(), "HOME="+tunnelDir())
	out, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	cmd.Stdout = cmd.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cloudflared login: %w", err)
	}
	var output strings.Builder
	lines := bufio.NewScanner(out)
	for lines.Scan() {
		output.WriteString(lines.Text() + "\n")
		if link := cloudflareLoginURL.FindString(lines.Text()); link != "" {
			p.set("choose " + publicDomain + " at " + link)
		}
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("cloudflared login: %w\n%s", err, tail([]byte(output.String())))
	}
	return nil
}

func tunnelPID() int {
	raw, err := os.ReadFile(tunnelPidFile())
	if err != nil {
		return 0
	}
	var pid int
	if _, err := fmt.Sscan(string(raw), &pid); err != nil {
		return 0
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || !strings.Contains(string(cmdline), tunnelConfig()) {
		return 0
	}
	return pid
}

// ensureTunnel starts the tunnel process if it isn't running. It outlives
// this command, like the VMs do.
func ensureTunnel() error {
	if tunnelPID() != 0 {
		return nil
	}
	logFile, err := os.OpenFile(tunnelLogFile(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.Command("cloudflared", "tunnel", "--no-autoupdate", "--config", tunnelConfig(), "run")
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = detached()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start tunnel: %w", err)
	}
	return writeFileAtomic(tunnelPidFile(), []byte(fmt.Sprint(cmd.Process.Pid)), 0o600)
}
