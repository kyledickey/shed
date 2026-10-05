package docker

import (
	"context"
	"fmt"
)

// ResolveImage returns the immutable local ID of the image referenced by ref.
func (c *Client) ResolveImage(ctx context.Context, ref string) (string, error) {
	image, err := c.api.ImageInspect(ctx, ref)
	if err != nil {
		return "", fmt.Errorf("docker: resolve image %s: %w", ref, err)
	}
	if image.ID == "" {
		return "", fmt.Errorf("docker: resolve image %s: empty image ID", ref)
	}
	return image.ID, nil
}
