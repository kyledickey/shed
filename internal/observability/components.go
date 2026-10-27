package observability

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/grafana"
)

// Component is one container of the telemetry stack.
type Component string

// The components, in the order they start.
const (
	Metrics Component = "metrics"
	Logs    Component = "logs"
	Traces  Component = "traces"
	Grafana Component = "grafana"
)

// order lists every component in the order they start.
var order = []Component{Metrics, Logs, Traces, Grafana}

// stores are the components Grafana reads from.
var stores = []Component{Metrics, Logs, Traces}

// Labels on the components' containers.
const (
	// LabelComponent names the component a container runs.
	LabelComponent = "shed.telemetry"
	// labelSpec holds the hash of the spec a container was created from.
	labelSpec = "shed.spec"
)

// component is what shed runs for one Component.
type component struct {
	// image is the pinned reference, tag and digest; version is its tag.
	image, version string
	memory         int64
	cpus           float64
	// data is where the component's volume is mounted.
	data string
}

// The pinned images are bumped with shed releases. Grafana 13 needs more than
// 256 MiB while it installs its plugins on first start.
var components = map[Component]component{
	Metrics: {
		image:   "victoriametrics/victoria-metrics:v1.155.0@sha256:f60dc268b54ecf87f6e09d740c2b732838aa9193090cc741417c9f6386a130df",
		version: "v1.155.0", memory: 512 << 20, cpus: 1, data: "/storage",
	},
	Logs: {
		image:   "victoriametrics/victoria-logs:v1.53.0@sha256:251121fa882af99b95ba0c230a4a2f412ea602d2698c64a96c58dc9842bb755d",
		version: "v1.53.0", memory: 512 << 20, cpus: 1, data: "/storage",
	},
	Traces: {
		image:   "victoriametrics/victoria-traces:v0.12.1@sha256:609abddf298c9642496045296f30a3968b5a4b79b678373e2e7ef29c844f5578",
		version: "v0.12.1", memory: 384 << 20, cpus: 1, data: "/storage",
	},
	Grafana: {
		image:   "grafana/grafana:13.2.4@sha256:dbbc38ea21103cefb50396340cd88afec779c7e070b0a7bf5a75b4ce46651d3f",
		version: "13.2.4", memory: 512 << 20, cpus: 0.5, data: "/var/lib/grafana",
	},
}

// containerName returns the name of c's container.
func containerName(c Component) string { return "shed-telemetry-" + string(c) }

// volumeName returns the name of c's data volume.
func volumeName(c Component) string { return "shed-telemetry-" + string(c) }

// Addrs are the loopback host:port addresses the components listen on.
type Addrs struct {
	Metrics, Logs, Traces, Grafana string
}

// stores returns the addresses Grafana's data sources point at.
func (a Addrs) stores() grafana.Stores {
	return grafana.Stores{Metrics: a.Metrics, Logs: a.Logs, Traces: a.Traces}
}

// Paths under the telemetry directory, bind-mounted read-only.
const (
	// scrapeDir holds scrape.yaml. The directory is mounted rather than the
	// file, so that replacing the file by renaming reaches the container.
	scrapeDir    = "scrape"
	scrapeMount  = "/etc/shed"
	provisioning = "grafana/provisioning"
)

// specInput is everything a component's container depends on.
type specInput struct {
	settings Settings
	addrs    Addrs
	// dir is the telemetry directory on the host.
	dir string
	// minFree is build.min_free_mb in bytes.
	minFree int64
	// grafanaURL is the public URL Grafana is served at.
	grafanaURL    string
	adminPassword string
}

// spec returns the container c should run as, labeled with the hash of
// everything it depends on.
func spec(c Component, in specInput) (docker.RunSpec, error) {
	def := components[c]
	s := docker.RunSpec{
		Name:    containerName(c),
		Image:   def.image,
		Network: docker.HostNetwork,
		Mounts:  []docker.Mount{{Volume: volumeName(c), Target: def.data}},
		CPUs:    def.cpus,
		Memory:  def.memory,
	}
	// disk is the logs and traces disk limit in bytes, split 60/40.
	disk := int64(in.settings.DiskLimitGB) << 30
	switch c {
	case Metrics:
		s.Cmd = []string{
			"-storageDataPath=" + def.data,
			"-httpListenAddr=" + in.addrs.Metrics,
			fmt.Sprintf("-retentionPeriod=%dd", in.settings.MetricsRetentionDays),
			fmt.Sprintf("-storage.minFreeDiskSpaceBytes=%d", in.minFree),
			"-promscrape.config=" + scrapeMount + "/scrape.yaml",
			"-promscrape.configCheckInterval=10s",
			"-opentelemetry.usePrometheusNaming",
			"-search.latencyOffset=5s",
		}
		s.Binds = []docker.Bind{{Source: filepath.Join(in.dir, scrapeDir), Target: scrapeMount, ReadOnly: true}}
	case Logs:
		s.Cmd = []string{
			"-storageDataPath=" + def.data,
			"-httpListenAddr=" + in.addrs.Logs,
			fmt.Sprintf("-retentionPeriod=%dd", in.settings.LogsRetentionDays),
			fmt.Sprintf("-retention.maxDiskSpaceUsageBytes=%d", disk*6/10),
		}
	case Traces:
		s.Cmd = []string{
			"-storageDataPath=" + def.data,
			"-httpListenAddr=" + in.addrs.Traces,
			fmt.Sprintf("-retentionPeriod=%dd", in.settings.TracesRetentionDays),
			fmt.Sprintf("-retention.maxDiskSpaceUsageBytes=%d", disk*4/10),
		}
	case Grafana:
		env, err := grafana.Env(grafana.Config{
			RootURL:       in.grafanaURL,
			Addr:          in.addrs.Grafana,
			AdminPassword: in.adminPassword,
			Plugins:       []string{grafana.MetricsPlugin, grafana.LogsPlugin},
		})
		if err != nil {
			return docker.RunSpec{}, err
		}
		s.Env = env
		s.Binds = []docker.Bind{{Source: filepath.Join(in.dir, provisioning), Target: grafana.ProvisioningPath, ReadOnly: true}}
	}
	// Grafana applies its data sources file only when it starts, so the
	// store addresses it names are part of its spec.
	b, err := json.Marshal(struct {
		Spec   docker.RunSpec
		Stores grafana.Stores
	}{s, in.addrs.stores()})
	if err != nil {
		return docker.RunSpec{}, err
	}
	sum := sha256.Sum256(b)
	s.Labels = map[string]string{LabelComponent: string(c), labelSpec: hex.EncodeToString(sum[:8])}
	return s, nil
}

// scrapeConfig is VictoriaMetrics' scrape configuration.
type scrapeConfig struct {
	ScrapeConfigs []scrapeJob `yaml:"scrape_configs"`
}

type scrapeJob struct {
	JobName        string         `yaml:"job_name"`
	ScrapeInterval string         `yaml:"scrape_interval"`
	StaticConfigs  []staticConfig `yaml:"static_configs"`
}

type staticConfig struct {
	Targets []string          `yaml:"targets"`
	Labels  map[string]string `yaml:"labels"`
}

// shedLabels are the labels of records about shed itself.
var shedLabels = map[string]string{
	"shed_service_id":   "shed",
	"shed_service_name": "shed",
	"shed_service_kind": "shed",
	"shed_source":       "shed",
}

// writeScrapeConfig writes scrape.yaml, which scrapes the stores' own
// metrics, under dir.
func writeScrapeConfig(dir string, a Addrs) error {
	cfg := scrapeConfig{ScrapeConfigs: []scrapeJob{{
		JobName:        "shed-telemetry",
		ScrapeInterval: "15s",
		StaticConfigs:  []staticConfig{{Targets: []string{a.Metrics, a.Logs, a.Traces}, Labels: shedLabels}},
	}}}
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, scrapeDir, "scrape.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("observability: write scrape config: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("observability: write scrape config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("observability: write scrape config: %w", err)
	}
	return readable(filepath.Dir(path))
}

// readable makes the tree at root readable by any user, whatever shed's
// umask: the components do not run as root (Grafana is uid 472).
func readable(root string) error {
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		mode := fs.FileMode(0o644)
		if d.IsDir() {
			mode = 0o755
		}
		return os.Chmod(path, mode)
	})
	if err != nil {
		return fmt.Errorf("observability: make %s readable: %w", root, err)
	}
	return nil
}
