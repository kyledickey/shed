package docker

import cerrdefs "github.com/containerd/errdefs"

// IsConflict reports whether err means that an object is still in use.
func IsConflict(err error) bool { return cerrdefs.IsConflict(err) }
