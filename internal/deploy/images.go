package deploy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kyledickey/shed/internal/docker"
)

// ErrImageUnavailable is returned when a deployment's image is no longer on
// this host, such as after old images were pruned or on a new host, so its
// container cannot be run without building or pulling it again.
var ErrImageUnavailable = errors.New("deploy: deployment's image is no longer on this host")

// checkImage returns an error wrapping ErrImageUnavailable if the image ref
// is not on the host.
func (d *Deployer) checkImage(ctx context.Context, ref string) error {
	_, err := d.docker.ResolveImage(ctx, ref)
	if docker.IsNotFound(err) {
		return fmt.Errorf("%w: %s", ErrImageUnavailable, ref)
	}
	if err != nil {
		return fmt.Errorf("deploy: check image %s: %w", ref, err)
	}
	return nil
}

// AvailableImages reports which of images, the images of a service's
// deployments, are on this host, and so can be redeployed.
func (d *Deployer) AvailableImages(ctx context.Context, serviceID string, images []string) (map[string]bool, error) {
	available := make(map[string]bool, len(images))
	built := imageRepo(serviceID) + ":"
	var listed bool
	for _, ref := range images {
		if _, ok := available[ref]; ok || ref == "" {
			continue
		}
		if !strings.HasPrefix(ref, built) {
			err := d.checkImage(ctx, ref)
			if err != nil && !errors.Is(err, ErrImageUnavailable) {
				return nil, err
			}
			available[ref] = err == nil
			continue
		}
		if !listed {
			// One listing covers every image built for the service.
			local, err := d.docker.ListImages(ctx, imageRepo(serviceID))
			if err != nil {
				return nil, fmt.Errorf("deploy: available images: %w", err)
			}
			for _, img := range local {
				available[img.Ref] = true
			}
			listed = true
			if available[ref] {
				continue
			}
		}
		available[ref] = false
	}
	return available, nil
}
