// Package update keeps shed current. It checks GitHub for new releases,
// downloads them and verifies their signed checksums, and swaps the shed
// binary on disk. Restarting into the new binary is up to the caller.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"aead.dev/minisign"
	"golang.org/x/mod/semver"
	"golang.org/x/sys/unix"
)

// autoDownloadKey is the setting that downloads new releases automatically.
const autoDownloadKey = "update.auto_download"

const (
	defaultAPIURL   = "https://api.github.com"
	defaultInterval = 6 * time.Hour
	firstCheck      = time.Minute
)

var (
	// ErrUnsupported means this build cannot update itself.
	ErrUnsupported = errors.New("update: this build cannot update itself")
	// ErrNoUpdate means there is no newer release to download.
	ErrNoUpdate = errors.New("update: no newer release")
	// ErrBusy means a check, download, or install is already in progress.
	ErrBusy = errors.New("update: an update operation is in progress")
	// ErrNotStaged means no release has been downloaded to install.
	ErrNotStaged = errors.New("update: no update has been downloaded")
)

// Settings stores the update preferences.
type Settings interface {
	// Setting returns the value of key, and false if it is not set.
	Setting(ctx context.Context, key string) (string, bool, error)
	SetSetting(ctx context.Context, key, value string) error
}

// Config configures an Updater.
type Config struct {
	// Repo is the GitHub repository releases are published to, "owner/name".
	Repo string
	// Version is the running version, a semver tag such as "v1.2.3".
	// Anything else is a development build, which cannot update itself.
	Version string
	// PublicKey is the minisign public key that signs checksums.txt.
	PublicKey string
	// Binary is the path of the shed binary that Install replaces.
	Binary string
	// Dir holds downloaded releases until they are installed.
	Dir      string
	Settings Settings
	// HTTPClient makes the GitHub requests. Nil means a client with a
	// 10-minute timeout.
	HTTPClient *http.Client
	// APIURL is the GitHub API base URL. Empty means api.github.com.
	APIURL string
	// Interval is the time between background checks. Zero means 6 hours.
	Interval time.Duration
	Log      *slog.Logger
}

// Release is a published shed release.
type Release struct {
	// Version is the release tag, such as "v1.2.3".
	Version     string
	URL         string
	Notes       string
	PublishedAt time.Time

	assets map[string]string // download URL by asset name
}

// State is what the Updater is doing.
type State string

// States of an Updater.
const (
	Idle        State = "idle"
	Checking    State = "checking"
	Downloading State = "downloading"
	Restarting  State = "restarting"
)

// Status reports the running version, the latest release, and the progress
// of updating to it.
type Status struct {
	Current string
	// Latest is the latest release, or nil until a check finds one.
	Latest *Release
	// Available reports whether Latest is newer than Current.
	Available bool
	// CheckedAt is the time of the last successful check.
	CheckedAt time.Time
	State     State
	// Staged is the version downloaded, verified, and ready to install.
	Staged string
	// Err is the last check or download failure.
	Err          string
	AutoDownload bool
	// Unsupported says why this build cannot update itself, or is empty.
	Unsupported string
}

// Updater checks for, downloads, and installs shed releases.
type Updater struct {
	cfg         Config
	key         minisign.PublicKey
	client      *http.Client
	unsupported string
	kick        chan struct{} // wakes Run to download

	mu         sync.Mutex
	state      State
	latest     *Release
	checkedAt  time.Time
	staged     string // version
	stagedPath string
	err        string
}

// New returns an Updater. It fails if the public key is invalid.
func New(cfg Config) (*Updater, error) {
	var key minisign.PublicKey
	if err := key.UnmarshalText([]byte(cfg.PublicKey)); err != nil {
		return nil, fmt.Errorf("update: public key: %w", err)
	}
	if cfg.APIURL == "" {
		cfg.APIURL = defaultAPIURL
	}
	if cfg.Interval == 0 {
		cfg.Interval = defaultInterval
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	u := &Updater{cfg: cfg, key: key, client: client, kick: make(chan struct{}, 1), state: Idle}
	switch dir := filepath.Dir(cfg.Binary); {
	case !semver.IsValid(cfg.Version):
		u.unsupported = fmt.Sprintf("%s is a development build; install a release to get updates", cfg.Version)
	case unix.Access(dir, unix.W_OK) != nil:
		u.unsupported = fmt.Sprintf("shed cannot write to %s, where its binary is", dir)
	}
	return u, nil
}

// Run checks for a new release a minute after it starts and then
// periodically, and performs downloads, until ctx is done. It does nothing
// for builds that cannot update themselves.
func (u *Updater) Run(ctx context.Context) {
	if u.unsupported != "" {
		return
	}
	// Downloads left by an earlier run are not trusted; fetch them again.
	if err := os.RemoveAll(u.cfg.Dir); err != nil {
		u.cfg.Log.Warn("update: clear downloads", "err", err)
	}
	timer := time.NewTimer(firstCheck)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			timer.Reset(u.cfg.Interval)
			if _, err := u.Check(ctx); err != nil && !errors.Is(err, ErrBusy) {
				u.cfg.Log.Warn("update check failed", "err", err)
			}
		case <-u.kick:
			u.download(ctx)
		}
	}
}

// Status returns the current status.
func (u *Updater) Status(ctx context.Context) (Status, error) {
	auto, err := u.autoDownload(ctx)
	if err != nil {
		return Status{}, err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return Status{
		Current:      u.cfg.Version,
		Latest:       u.latest,
		Available:    u.available(),
		CheckedAt:    u.checkedAt,
		State:        u.state,
		Staged:       u.staged,
		Err:          u.err,
		AutoDownload: auto,
		Unsupported:  u.unsupported,
	}, nil
}

// Check asks GitHub for the latest release and, with automatic downloads on,
// starts downloading it if it is newer. A failed check is returned and also
// reported in the status.
func (u *Updater) Check(ctx context.Context) (Status, error) {
	if err := u.begin(Checking); err != nil {
		return Status{}, err
	}
	rel, err := u.fetchLatest(ctx)
	u.mu.Lock()
	u.state = Idle
	if err != nil {
		u.err = err.Error()
	} else {
		u.latest, u.checkedAt, u.err = rel, time.Now(), ""
	}
	u.mu.Unlock()
	if err != nil {
		return Status{}, err
	}
	auto, err := u.autoDownload(ctx)
	if err != nil {
		return Status{}, err
	}
	if auto {
		if _, err := u.Download(ctx); err != nil && !errors.Is(err, ErrNoUpdate) && !errors.Is(err, ErrBusy) {
			return Status{}, err
		}
	}
	return u.Status(ctx)
}

// Download starts downloading the latest release in the background, unless
// it is already downloaded. Run performs the download.
func (u *Updater) Download(ctx context.Context) (Status, error) {
	u.mu.Lock()
	switch {
	case u.unsupported != "":
		u.mu.Unlock()
		return Status{}, ErrUnsupported
	case !u.available():
		u.mu.Unlock()
		return Status{}, ErrNoUpdate
	case u.state != Idle:
		u.mu.Unlock()
		return Status{}, ErrBusy
	case u.staged != u.latest.Version:
		u.state = Downloading
		select {
		case u.kick <- struct{}{}:
		default:
		}
	}
	u.mu.Unlock()
	return u.Status(ctx)
}

// SetAutoDownload turns automatic downloads on or off. Turning them on
// starts downloading a newer release that is already known.
func (u *Updater) SetAutoDownload(ctx context.Context, on bool) (Status, error) {
	if err := u.cfg.Settings.SetSetting(ctx, autoDownloadKey, fmt.Sprint(on)); err != nil {
		return Status{}, fmt.Errorf("update: save auto-download: %w", err)
	}
	if on {
		if _, err := u.Download(ctx); err != nil && !errors.Is(err, ErrNoUpdate) &&
			!errors.Is(err, ErrBusy) && !errors.Is(err, ErrUnsupported) {
			return Status{}, err
		}
	}
	return u.Status(ctx)
}

// Install replaces the shed binary with the downloaded release, keeping the
// replaced one next to it with a .prev suffix. On success the Updater stays
// in the Restarting state: the caller is expected to restart shed.
func (u *Updater) Install(ctx context.Context) (Status, error) {
	u.mu.Lock()
	switch {
	case u.unsupported != "":
		u.mu.Unlock()
		return Status{}, ErrUnsupported
	case u.staged == "":
		u.mu.Unlock()
		return Status{}, ErrNotStaged
	case u.state != Idle:
		u.mu.Unlock()
		return Status{}, ErrBusy
	}
	u.state = Restarting
	version, path := u.staged, u.stagedPath
	u.mu.Unlock()

	if err := replace(u.cfg.Binary, path); err != nil {
		u.mu.Lock()
		u.state, u.err = Idle, err.Error()
		u.mu.Unlock()
		return Status{}, err
	}
	u.cfg.Log.Info("installed update", "version", version, "binary", u.cfg.Binary)
	return u.Status(ctx)
}

// begin moves an idle, supported Updater into state.
func (u *Updater) begin(state State) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.unsupported != "" {
		return ErrUnsupported
	}
	if u.state != Idle {
		return ErrBusy
	}
	u.state = state
	return nil
}

// available reports whether the latest release is newer than the running
// version. The caller holds mu.
func (u *Updater) available() bool {
	return u.unsupported == "" && u.latest != nil && semver.Compare(u.latest.Version, u.cfg.Version) > 0
}

func (u *Updater) autoDownload(ctx context.Context) (bool, error) {
	v, ok, err := u.cfg.Settings.Setting(ctx, autoDownloadKey)
	if err != nil {
		return false, fmt.Errorf("update: load auto-download: %w", err)
	}
	return ok && v == "true", nil
}

// download downloads the latest release if Download asked for it, and
// records the result.
func (u *Updater) download(ctx context.Context) {
	u.mu.Lock()
	if u.state != Downloading {
		u.mu.Unlock()
		return
	}
	// Fetching clears the download directory, and with it any earlier
	// download.
	rel := u.latest
	u.staged, u.stagedPath = "", ""
	u.mu.Unlock()

	path, err := u.fetch(ctx, rel)
	u.mu.Lock()
	defer u.mu.Unlock()
	u.state = Idle
	if err != nil {
		u.err = err.Error()
		u.cfg.Log.Warn("update download failed", "version", rel.Version, "err", err)
		return
	}
	u.staged, u.stagedPath, u.err = rel.Version, path, ""
	u.cfg.Log.Info("update downloaded", "version", rel.Version)
}

// githubRelease is the part of GitHub's release object shed reads.
type githubRelease struct {
	TagName     string    `json:"tag_name"`
	HTMLURL     string    `json:"html_url"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// fetchLatest returns the latest published release, which excludes drafts
// and pre-releases, or nil if there is none.
func (u *Updater) fetchLatest(ctx context.Context) (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", u.cfg.APIURL, u.cfg.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "shed/"+u.cfg.Version)
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("update: check latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update: check latest release: GitHub returned %s", resp.Status)
	}
	var gr githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return nil, fmt.Errorf("update: decode latest release: %w", err)
	}
	if !semver.IsValid(gr.TagName) {
		return nil, fmt.Errorf("update: latest release tag %q is not a version", gr.TagName)
	}
	rel := &Release{
		Version:     gr.TagName,
		URL:         gr.HTMLURL,
		Notes:       strings.TrimSpace(gr.Body),
		PublishedAt: gr.PublishedAt,
		assets:      make(map[string]string, len(gr.Assets)),
	}
	for _, a := range gr.Assets {
		rel.assets[a.Name] = a.URL
	}
	return rel, nil
}
