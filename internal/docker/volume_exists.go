package docker

import (
	"context"
	"fmt"

	"github.com/moby/moby/client"
)

// VolumeExists reports whether the named volume exists. Unlike
// EnsureVolume, it never creates one.
func (c *Client) VolumeExists(ctx context.Context, name string) (bool, error) {
	_, err := c.api.VolumeInspect(ctx, name, client.VolumeInspectOptions{})
	if IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("docker: inspect volume %s: %w", name, err)
	}
	return true, nil
}
