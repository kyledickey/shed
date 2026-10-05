package docker

import (
	"net/netip"
	"reflect"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
)

func TestIsolatedOptions(t *testing.T) {
	port, _ := network.PortFrom(5432, network.TCP)
	init := true
	info := container.InspectResponse{
		Image: "sha256:abc",
		Config: &container.Config{
			Image:        "postgres:18-alpine",
			Entrypoint:   []string{"docker-entrypoint.sh"},
			Cmd:          []string{"postgres", "-c", "max_connections=50"},
			Env:          []string{"POSTGRES_PASSWORD=secret"},
			User:         "postgres",
			StopSignal:   "SIGINT",
			Labels:       map[string]string{"shed.service": "s1", "shed.deployment": "d1"},
			ExposedPorts: network.PortSet{port: {}},
		},
		HostConfig: &container.HostConfig{
			NetworkMode:   "shed-p1",
			PortBindings:  network.PortMap{port: {{HostIP: netip.IPv4Unspecified(), HostPort: "5432"}}},
			RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
			Mounts:        []mount.Mount{{Type: mount.TypeVolume, Source: "shed-vol-v1", Target: "/var/lib/postgresql"}},
			Resources:     container.Resources{NanoCPUs: 5e8, Memory: 1 << 30},
			ShmSize:       64 << 20,
			Init:          &init,
		},
	}
	opts, err := isolatedOptions(info, "shed-restore-r1-db", map[string]string{"shed.backup": "r1"})
	if err != nil {
		t.Fatal(err)
	}
	hc, cfg := opts.HostConfig, opts.Config
	// Isolation: no network but loopback, no published ports, never
	// restarted by Docker, and none of the service's labels.
	if hc.NetworkMode != "none" || opts.NetworkingConfig != nil {
		t.Errorf("network mode %q, networking %+v; want none", hc.NetworkMode, opts.NetworkingConfig)
	}
	if len(hc.PortBindings) != 0 || len(cfg.ExposedPorts) != 0 || hc.PublishAllPorts {
		t.Errorf("ports %v, exposed %v; want none", hc.PortBindings, cfg.ExposedPorts)
	}
	if hc.RestartPolicy.Name != container.RestartPolicyDisabled {
		t.Errorf("restart policy %q, want no", hc.RestartPolicy.Name)
	}
	if want := map[string]string{"shed.backup": "r1"}; !reflect.DeepEqual(cfg.Labels, want) {
		t.Errorf("labels %v, want %v", cfg.Labels, want)
	}
	// The same database: image, command, credentials, and data.
	if opts.Name != "shed-restore-r1-db" || cfg.Image != "sha256:abc" || cfg.User != "postgres" || cfg.StopSignal != "SIGINT" {
		t.Errorf("name %q, config %+v", opts.Name, cfg)
	}
	if !reflect.DeepEqual(cfg.Entrypoint, info.Config.Entrypoint) || !reflect.DeepEqual(cfg.Cmd, info.Config.Cmd) ||
		!reflect.DeepEqual(cfg.Env, info.Config.Env) {
		t.Errorf("entrypoint %q, cmd %q, env %q", cfg.Entrypoint, cfg.Cmd, cfg.Env)
	}
	if !reflect.DeepEqual(hc.Mounts, info.HostConfig.Mounts) || hc.Resources.Memory != 1<<30 || hc.Resources.NanoCPUs != 5e8 ||
		hc.ShmSize != 64<<20 || hc.Init == nil || !*hc.Init {
		t.Errorf("host config %+v", hc)
	}
	// The copy does not alias the original's slices.
	cfg.Env[0] = "changed"
	if info.Config.Env[0] != "POSTGRES_PASSWORD=secret" {
		t.Error("copy shares the environment with the original")
	}

	if _, err := isolatedOptions(container.InspectResponse{}, "x", nil); err == nil {
		t.Error("no error for a container without configuration")
	}
}
