package docker

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

// CreateIsolated creates a stopped copy of the container id, named name and
// labeled with labels instead of the original's labels, and returns its ID.
// The copy runs the same image, entrypoint, command, environment, user, and
// mounts, with the same resource limits. It has no network but loopback and
// publishes no ports, so nothing outside it can connect to it, and Docker
// never restarts it, not even when the daemon starts again. Anonymous
// volumes of the original are not shared: the copy gets its own.
func (c *Client) CreateIsolated(ctx context.Context, id, name string, labels map[string]string) (string, error) {
	res, err := c.api.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("docker: inspect container %s: %w", id, err)
	}
	opts, err := isolatedOptions(res.Container, name, labels)
	if err != nil {
		return "", fmt.Errorf("docker: copy container %s: %w", id, err)
	}
	created, err := c.api.ContainerCreate(ctx, opts)
	if err != nil {
		return "", fmt.Errorf("docker: create container %s: %w", name, err)
	}
	return created.ID, nil
}

// isolatedOptions returns the options that create an isolated copy of the
// container info. See CreateIsolated.
func isolatedOptions(info container.InspectResponse, name string, labels map[string]string) (client.ContainerCreateOptions, error) {
	if info.Config == nil || info.HostConfig == nil {
		return client.ContainerCreateOptions{}, errors.New("incomplete container configuration")
	}
	cfg, hc := info.Config, info.HostConfig
	image := info.Image // The image ID, in case the tag moved since.
	if image == "" {
		image = cfg.Image
	}
	var mounts []mount.Mount
	for _, m := range hc.Mounts {
		// Mounts are the ones requested at creation; volumes Docker created
		// for the image's VOLUME paths are not among them.
		mounts = append(mounts, mount.Mount{Type: m.Type, Source: m.Source, Target: m.Target, ReadOnly: m.ReadOnly})
	}
	return client.ContainerCreateOptions{
		Name: name,
		Config: &container.Config{
			Image:       image,
			Entrypoint:  slices.Clone(cfg.Entrypoint),
			Cmd:         slices.Clone(cfg.Cmd),
			Env:         slices.Clone(cfg.Env),
			User:        cfg.User,
			WorkingDir:  cfg.WorkingDir,
			StopSignal:  cfg.StopSignal,
			StopTimeout: cfg.StopTimeout,
			Labels:      maps.Clone(labels),
		},
		HostConfig: &container.HostConfig{
			NetworkMode:   "none",
			RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyDisabled},
			Mounts:        mounts,
			Resources:     hc.Resources,
			ShmSize:       hc.ShmSize,
			Tmpfs:         maps.Clone(hc.Tmpfs),
			Init:          hc.Init,
			LogConfig:     hc.LogConfig,
		},
	}, nil
}
