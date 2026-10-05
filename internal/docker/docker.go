// Package docker is a thin wrapper over the Docker Engine API client that
// exposes only the operations shed needs.
package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// Client talks to a Docker Engine.
type Client struct {
	api *client.Client
}

// New returns a Client configured from the standard DOCKER_* environment
// variables. The API version is negotiated with the daemon on first use.
func New() (*Client, error) {
	api, err := client.New(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("docker: new client: %w", err)
	}
	return &Client{api: api}, nil
}

// Close releases the resources held by c.
func (c *Client) Close() error {
	return c.api.Close()
}

// Ping checks that the daemon is reachable.
func (c *Client) Ping(ctx context.Context) error {
	if _, err := c.api.Ping(ctx, client.PingOptions{NegotiateAPIVersion: true}); err != nil {
		return fmt.Errorf("docker: ping: %w", err)
	}
	return nil
}

// IsNotFound reports whether err means that a Docker object does not exist.
func IsNotFound(err error) bool {
	return cerrdefs.IsNotFound(err)
}

// EnsureNetwork creates the bridge network name if it does not exist.
func (c *Client) EnsureNetwork(ctx context.Context, name string) error {
	_, err := c.api.NetworkInspect(ctx, name, client.NetworkInspectOptions{})
	if err == nil {
		return nil
	}
	if !IsNotFound(err) {
		return fmt.Errorf("docker: inspect network %s: %w", name, err)
	}
	_, err = c.api.NetworkCreate(ctx, name, client.NetworkCreateOptions{Driver: "bridge"})
	if err != nil && !cerrdefs.IsConflict(err) { // Conflict: created concurrently.
		return fmt.Errorf("docker: create network %s: %w", name, err)
	}
	return nil
}

// RemoveNetwork removes the network name. A missing network is not an error.
func (c *Client) RemoveNetwork(ctx context.Context, name string) error {
	_, err := c.api.NetworkRemove(ctx, name, client.NetworkRemoveOptions{})
	if err != nil && !IsNotFound(err) {
		return fmt.Errorf("docker: remove network %s: %w", name, err)
	}
	return nil
}

// ReconnectNetwork replaces the DNS aliases, besides its name, of the
// container id on the network name, to which it must be connected. Docker
// cannot change the aliases of a connected container, so it is disconnected
// and connected again, asking for the addresses it had so that they do not
// change.
func (c *Client) ReconnectNetwork(ctx context.Context, name, id string, aliases []string) error {
	res, err := c.api.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return fmt.Errorf("docker: inspect container %s: %w", id, err)
	}
	var ep *network.EndpointSettings
	if ns := res.Container.NetworkSettings; ns != nil {
		ep = ns.Networks[name]
	}
	if ep == nil || !ep.IPAddress.IsValid() && !ep.GlobalIPv6Address.IsValid() {
		return fmt.Errorf("docker: reconnect container %s: no address on network %s", id, name)
	}
	ipam := &network.EndpointIPAMConfig{IPv4Address: ep.IPAddress, IPv6Address: ep.GlobalIPv6Address}

	if err := c.DisconnectNetwork(ctx, name, id); err != nil {
		return err
	}
	_, err = c.api.NetworkConnect(ctx, name, client.NetworkConnectOptions{
		Container:      id,
		EndpointConfig: &network.EndpointSettings{Aliases: aliases, IPAMConfig: ipam},
	})
	if err != nil {
		return fmt.Errorf("docker: connect container %s to network %s: %w", id, name, err)
	}
	return nil
}

// DisconnectNetwork disconnects the container id from the network name,
// which also drops its DNS aliases there.
func (c *Client) DisconnectNetwork(ctx context.Context, name, id string) error {
	_, err := c.api.NetworkDisconnect(ctx, name, client.NetworkDisconnectOptions{Container: id})
	if err != nil {
		return fmt.Errorf("docker: disconnect container %s from network %s: %w", id, name, err)
	}
	return nil
}

// EnsureVolume creates the named volume if it does not exist.
func (c *Client) EnsureVolume(ctx context.Context, name string) error {
	if _, err := c.api.VolumeCreate(ctx, client.VolumeCreateOptions{Name: name}); err != nil {
		return fmt.Errorf("docker: create volume %s: %w", name, err)
	}
	return nil
}

// RemoveVolume removes the named volume. A missing volume is not an error.
func (c *Client) RemoveVolume(ctx context.Context, name string) error {
	_, err := c.api.VolumeRemove(ctx, name, client.VolumeRemoveOptions{})
	if err != nil && !IsNotFound(err) {
		return fmt.Errorf("docker: remove volume %s: %w", name, err)
	}
	return nil
}

// PullImage pulls ref and writes human-readable progress lines to w.
func (c *Client) PullImage(ctx context.Context, ref string, w io.Writer) error {
	resp, err := c.api.ImagePull(ctx, ref, client.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("docker: pull %s: %w", ref, err)
	}
	defer resp.Close()

	// The daemon reports byte-level progress many times per layer; print a
	// line only when the status of a layer changes.
	last := make(map[string]string)
	for msg, err := range resp.JSONMessages(ctx) {
		if err != nil {
			return fmt.Errorf("docker: pull %s: %w", ref, err)
		}
		if msg.Status == "" || last[msg.ID] == msg.Status {
			continue
		}
		last[msg.ID] = msg.Status
		line := msg.Status
		if msg.ID != "" {
			line = msg.ID + ": " + msg.Status
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return fmt.Errorf("docker: pull %s: write progress: %w", ref, err)
		}
	}
	return nil
}

// RemoveImage removes the image ref. A missing image is not an error.
func (c *Client) RemoveImage(ctx context.Context, ref string) error {
	_, err := c.api.ImageRemove(ctx, ref, client.ImageRemoveOptions{})
	if err != nil && !IsNotFound(err) {
		return fmt.Errorf("docker: remove image %s: %w", ref, err)
	}
	return nil
}

// ExposedPorts returns the TCP ports that the image ref declares as exposed,
// in ascending order.
func (c *Client) ExposedPorts(ctx context.Context, ref string) ([]int, error) {
	res, err := c.api.ImageInspect(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("docker: inspect image %s: %w", ref, err)
	}
	if res.Config == nil {
		return nil, nil
	}
	return tcpPorts(res.Config.ExposedPorts), nil
}

// tcpPorts returns the TCP port numbers in exposed, whose keys look like
// "80/tcp", in ascending order. Other protocols and malformed keys are
// skipped.
func tcpPorts(exposed map[string]struct{}) []int {
	var ports []int
	for key := range exposed {
		p, err := network.ParsePort(key)
		if err != nil || p.Proto() != network.TCP {
			continue
		}
		ports = append(ports, int(p.Num()))
	}
	slices.Sort(ports)
	return ports
}

// Image is a tagged local image.
type Image struct {
	Ref     string // repo:tag
	Created time.Time
}

// ListImages returns the local images tagged in repository repo, one entry per
// tag.
func (c *Client) ListImages(ctx context.Context, repo string) ([]Image, error) {
	res, err := c.api.ImageList(ctx, client.ImageListOptions{
		Filters: make(client.Filters).Add("reference", repo),
	})
	if err != nil {
		return nil, fmt.Errorf("docker: list images %s: %w", repo, err)
	}
	var images []Image
	for _, sum := range res.Items {
		for _, tag := range sum.RepoTags {
			if tagRepo(tag) == repo {
				images = append(images, Image{Ref: tag, Created: time.Unix(sum.Created, 0)})
			}
		}
	}
	return images, nil
}

// tagRepo returns the repository part of an image reference of the form
// repo:tag. A colon before the last slash belongs to a registry port.
func tagRepo(ref string) string {
	i := strings.LastIndexByte(ref, ':')
	if i < 0 || strings.Contains(ref[i:], "/") {
		return ref
	}
	return ref[:i]
}

// Mount attaches a named volume to a container path.
type Mount struct {
	Volume, Target string
	ReadOnly       bool
}

// volumeMounts converts ms to Docker volume mounts.
func volumeMounts(ms []Mount) []mount.Mount {
	mounts := make([]mount.Mount, 0, len(ms))
	for _, m := range ms {
		mounts = append(mounts, mount.Mount{Type: mount.TypeVolume, Source: m.Volume, Target: m.Target, ReadOnly: m.ReadOnly})
	}
	return mounts
}

// PortBinding publishes a container TCP port on all host interfaces.
type PortBinding struct {
	HostPort, ContainerPort int
}

// RunSpec describes a container to create, and usually to start.
type RunSpec struct {
	Name    string
	Image   string
	Cmd     []string // nil means the image default
	Env     []string // KEY=VALUE
	Labels  map[string]string
	Network string
	Aliases []string // DNS names on Network
	Mounts  []Mount
	Publish []PortBinding
	// CPUs is the CPU quota in cores and Memory the memory limit in bytes,
	// with no extra swap. Zero means unlimited.
	CPUs   float64
	Memory int64
}

// Run creates and starts a container and returns its ID. The container is
// restarted unless explicitly stopped.
//
// A failed start can be ambiguous: Docker may have started the container
// although the request failed, for example when the connection dropped. Run
// then removes the container. If that removal fails too, Run returns the
// container's ID along with the error, since the container may be running.
func (c *Client) Run(ctx context.Context, spec RunSpec) (string, error) {
	id, err := c.Create(ctx, spec)
	if err != nil {
		return "", err
	}
	if _, err := c.api.ContainerStart(ctx, id, client.ContainerStartOptions{}); err != nil {
		startErr := fmt.Errorf("docker: start container %s: %w", spec.Name, err)
		if rmErr := c.Remove(context.WithoutCancel(ctx), id); rmErr != nil {
			return id, errors.Join(startErr, rmErr)
		}
		return "", startErr
	}
	return id, nil
}

// Create creates a container from spec without starting it and returns its ID.
// An empty spec.Network leaves the container with no networking configuration,
// so it uses the daemon's default network if it is ever started.
func (c *Client) Create(ctx context.Context, spec RunSpec) (string, error) {
	exposed := make(network.PortSet)
	bindings := make(network.PortMap)
	for _, p := range spec.Publish {
		port, ok := network.PortFrom(uint16(p.ContainerPort), network.TCP)
		if !ok {
			return "", fmt.Errorf("docker: create container %s: invalid container port %d", spec.Name, p.ContainerPort)
		}
		exposed[port] = struct{}{}
		bindings[port] = append(bindings[port], network.PortBinding{
			HostIP:   netip.IPv4Unspecified(),
			HostPort: strconv.Itoa(p.HostPort),
		})
	}
	opts := client.ContainerCreateOptions{
		Name: spec.Name,
		Config: &container.Config{
			Image:        spec.Image,
			Cmd:          spec.Cmd,
			Env:          spec.Env,
			Labels:       spec.Labels,
			ExposedPorts: exposed,
		},
		HostConfig: &container.HostConfig{
			NetworkMode:   container.NetworkMode(spec.Network),
			PortBindings:  bindings,
			Mounts:        volumeMounts(spec.Mounts),
			RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
			Resources:     workloadResources(spec),
			LogConfig:     container.LogConfig{Type: "json-file", Config: map[string]string{"max-size": "10m", "max-file": "3"}},
		},
	}
	if spec.Network != "" {
		opts.NetworkingConfig = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				spec.Network: {Aliases: spec.Aliases},
			},
		}
	}

	created, err := c.api.ContainerCreate(ctx, opts)
	if err != nil {
		return "", fmt.Errorf("docker: create container %s: %w", spec.Name, err)
	}
	return created.ID, nil
}

// Start starts an existing, stopped container.
func (c *Client) Start(ctx context.Context, id string) error {
	if _, err := c.api.ContainerStart(ctx, id, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("docker: start container %s: %w", id, err)
	}
	return nil
}

// Stop stops the container, killing it if it has not exited within timeout.
func (c *Client) Stop(ctx context.Context, id string, timeout time.Duration) error {
	secs := int(timeout.Seconds())
	_, err := c.api.ContainerStop(ctx, id, client.ContainerStopOptions{Timeout: &secs})
	if err != nil {
		return fmt.Errorf("docker: stop container %s: %w", id, err)
	}
	return nil
}

// Restart stops the container, killing it if it has not exited within
// timeout, and starts it again.
func (c *Client) Restart(ctx context.Context, id string, timeout time.Duration) error {
	secs := int(timeout.Seconds())
	_, err := c.api.ContainerRestart(ctx, id, client.ContainerRestartOptions{Timeout: &secs})
	if err != nil {
		return fmt.Errorf("docker: restart container %s: %w", id, err)
	}
	return nil
}

// Remove force-removes the container and its anonymous volumes, such as those
// Docker creates for the VOLUME paths of its image. Named volumes (like
// shed-vol-*) are never removed by RemoveVolumes: Docker deletes only the
// volumes it generated names for, like docker rm -v. A missing container is
// not an error.
func (c *Client) Remove(ctx context.Context, id string) error {
	_, err := c.api.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
	if err != nil && !IsNotFound(err) {
		return fmt.Errorf("docker: remove container %s: %w", id, err)
	}
	return nil
}

// Container is the observed state of a container.
type Container struct {
	ID      string
	Name    string
	Image   string
	State   string // created, running, paused, restarting, removing, exited or dead
	Running bool
	// ExitCode is the exit code of the last run.
	ExitCode int
	// IPs maps network name to the container's IP address on it.
	IPs    map[string]string
	Labels map[string]string
	// Volumes lists the named volumes mounted into the container.
	Volumes []string
	// CPULimit is the configured CPU limit in cores; zero means unlimited.
	// Only Inspect sets it.
	CPULimit float64
	// MemoryLimit is the configured memory limit in bytes; zero means
	// unlimited. Only Inspect sets it.
	MemoryLimit int64
}

// Inspect returns the state of the container.
func (c *Client) Inspect(ctx context.Context, id string) (Container, error) {
	res, err := c.api.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return Container{}, fmt.Errorf("docker: inspect container %s: %w", id, err)
	}
	info := res.Container
	ctr := Container{
		ID:   info.ID,
		Name: strings.TrimPrefix(info.Name, "/"),
		IPs:  make(map[string]string),
	}
	if info.Config != nil {
		ctr.Image = info.Config.Image
		ctr.Labels = info.Config.Labels
	}
	if info.State != nil {
		ctr.State = string(info.State.Status)
		ctr.Running = info.State.Running
		ctr.ExitCode = info.State.ExitCode
	}
	if info.NetworkSettings != nil {
		addIPs(ctr.IPs, info.NetworkSettings.Networks)
	}
	if info.HostConfig != nil {
		ctr.CPULimit = float64(info.HostConfig.NanoCPUs) / 1e9
		ctr.MemoryLimit = info.HostConfig.Memory
	}
	ctr.Volumes = volumeNames(info.Mounts)
	return ctr, nil
}

// List returns all containers, stopped ones included, that have every one of
// the given labels. An empty label value matches any value.
func (c *Client) List(ctx context.Context, labels map[string]string) ([]Container, error) {
	filters := make(client.Filters)
	for k, v := range labels {
		if v == "" {
			filters.Add("label", k)
		} else {
			filters.Add("label", k+"="+v)
		}
	}
	res, err := c.api.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: filters})
	if err != nil {
		return nil, fmt.Errorf("docker: list containers: %w", err)
	}
	containers := make([]Container, 0, len(res.Items))
	for _, s := range res.Items {
		ctr := Container{
			ID:      s.ID,
			Image:   s.Image,
			State:   string(s.State),
			Running: s.State == container.StateRunning,
			IPs:     make(map[string]string),
			Labels:  s.Labels,
		}
		if len(s.Names) > 0 {
			ctr.Name = strings.TrimPrefix(s.Names[0], "/")
		}
		if s.NetworkSettings != nil {
			addIPs(ctr.IPs, s.NetworkSettings.Networks)
		}
		ctr.Volumes = volumeNames(s.Mounts)
		containers = append(containers, ctr)
	}
	return containers, nil
}

func addIPs(ips map[string]string, networks map[string]*network.EndpointSettings) {
	for name, ep := range networks {
		if ep != nil && ep.IPAddress.IsValid() {
			ips[name] = ep.IPAddress.String()
		}
	}
}

func volumeNames(mounts []container.MountPoint) []string {
	var names []string
	for _, m := range mounts {
		if m.Type == mount.TypeVolume && m.Name != "" {
			names = append(names, m.Name)
		}
	}
	return names
}

// Logs copies the container's stdout and stderr to w, each line prefixed
// with its RFC 3339 timestamp and a space. A negative tail means all lines. With follow, Logs blocks until ctx is done or the container
// exits; canceling ctx is not an error.
func (c *Client) Logs(ctx context.Context, id string, tail int, follow bool, w io.Writer) error {
	tailArg := "all"
	if tail >= 0 {
		tailArg = strconv.Itoa(tail)
	}
	rc, err := c.api.ContainerLogs(ctx, id, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tailArg,
		Timestamps: true,
	})
	if err != nil {
		return fmt.Errorf("docker: logs %s: %w", id, err)
	}
	defer rc.Close()

	if _, err := stdcopy.StdCopy(w, w, rc); err != nil && ctx.Err() == nil {
		return fmt.Errorf("docker: logs %s: %w", id, err)
	}
	return nil
}

// Stats is a point-in-time sample of a container's resource usage. Counters
// are cumulative since the container started; rates come from the difference
// between two samples.
type Stats struct {
	Read time.Time
	// CPUTotal is the CPU time the container has used, in nanoseconds.
	CPUTotal uint64
	// SystemCPU is the CPU time the host has used across all CPUs, in
	// nanoseconds; zero if the daemon does not report it.
	SystemCPU  uint64
	OnlineCPUs uint32
	// MemoryUsage is the memory in use in bytes, excluding inactive page
	// cache.
	MemoryUsage uint64
	// NetRx and NetTx are bytes received and sent, summed over all
	// interfaces.
	NetRx, NetTx uint64
	// DiskRead and DiskWrite are block device bytes read and written.
	DiskRead, DiskWrite uint64
}

// Stats returns a single sample of the container's resource usage.
func (c *Client) Stats(ctx context.Context, id string) (Stats, error) {
	res, err := c.api.ContainerStats(ctx, id, client.ContainerStatsOptions{})
	if err != nil {
		return Stats{}, fmt.Errorf("docker: stats %s: %w", id, err)
	}
	defer res.Body.Close()
	var resp container.StatsResponse
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return Stats{}, fmt.Errorf("docker: decode stats %s: %w", id, err)
	}
	return statsFrom(resp), nil
}

func statsFrom(r container.StatsResponse) Stats {
	s := Stats{
		Read:        r.Read,
		CPUTotal:    r.CPUStats.CPUUsage.TotalUsage,
		SystemCPU:   r.CPUStats.SystemUsage,
		OnlineCPUs:  r.CPUStats.OnlineCPUs,
		MemoryUsage: r.MemoryStats.Usage,
	}
	if s.OnlineCPUs == 0 {
		s.OnlineCPUs = uint32(len(r.CPUStats.CPUUsage.PercpuUsage))
	}
	// Page cache can be reclaimed, so it does not count as usage. The key is
	// inactive_file on cgroup v2 and total_inactive_file on cgroup v1.
	for _, key := range []string{"inactive_file", "total_inactive_file", "cache"} {
		if v, ok := r.MemoryStats.Stats[key]; ok {
			if v < s.MemoryUsage {
				s.MemoryUsage -= v
			}
			break
		}
	}
	for _, n := range r.Networks {
		s.NetRx += n.RxBytes
		s.NetTx += n.TxBytes
	}
	for _, e := range r.BlkioStats.IoServiceBytesRecursive {
		switch strings.ToLower(e.Op) {
		case "read":
			s.DiskRead += e.Value
		case "write":
			s.DiskWrite += e.Value
		}
	}
	return s
}

// workloadResources caps each workload, including database containers.
func workloadResources(spec RunSpec) container.Resources {
	pids := int64(512)
	r := container.Resources{NanoCPUs: int64(spec.CPUs * 1e9), PidsLimit: &pids}
	if spec.Memory > 0 {
		r.Memory, r.MemorySwap = spec.Memory, spec.Memory
	}
	return r
}
