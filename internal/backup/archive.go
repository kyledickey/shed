package backup

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// relMount returns a mount path as it appears in archives: cleaned and
// relative to "/".
func relMount(mountPath string) string {
	return strings.TrimPrefix(path.Clean("/"+mountPath), "/")
}

// under reports whether the archive name is rel or inside it.
func under(name, rel string) bool {
	return name == rel || strings.HasPrefix(name, rel+"/")
}

// owner returns the mount, among rels, that contains name: the longest one,
// so that nested mounts win. It returns "" if none does.
func owner(name string, rels []string) string {
	best := ""
	for _, r := range rels {
		if under(name, r) && len(r) > len(best) {
			best = r
		}
	}
	return best
}

// cleanName validates an archive entry name and returns it cleaned. Absolute
// names and names that leave the archive root are rejected.
func cleanName(name string) (string, error) {
	if name == "" || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("unsafe archive entry %q", name)
	}
	c := path.Clean(name)
	if c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return "", fmt.Errorf("unsafe archive entry %q", name)
	}
	return c, nil
}

// copyHeader returns a copy of h named name that a tar.Writer accepts
// whatever format h was read in. Ownership, modes, times, and extended
// attributes are kept.
func copyHeader(h *tar.Header, name, linkname string) *tar.Header {
	c := *h
	c.Name = name
	c.Linkname = linkname
	c.Format = tar.FormatUnknown
	c.PAXRecords = nil
	for k, v := range h.PAXRecords {
		switch k {
		case "path", "linkpath", "size", "uid", "gid", "uname", "gname", "mtime", "atime", "ctime":
		default:
			if c.PAXRecords == nil {
				c.PAXRecords = make(map[string]string)
			}
			c.PAXRecords[k] = v
		}
	}
	if c.Typeflag == tar.TypeDir && !strings.HasSuffix(c.Name, "/") {
		c.Name += "/"
	}
	return &c
}

// rerootVolume copies the Docker archive of the mount mountPath, whose entries
// are named after the mount's base name, to tw with entries rooted at the
// mount path relative to "/". Entries inside any of the nested mount paths
// are left out; they are archived from their own volume.
func rerootVolume(tr *tar.Reader, tw *tar.Writer, mountPath string, nested []string) error {
	rel := relMount(mountPath)
	base := path.Base(rel)
	rename := func(name string) (string, error) {
		c, err := cleanName(name)
		if err != nil {
			return "", err
		}
		if !under(c, base) {
			return "", fmt.Errorf("unexpected archive entry %q for %s", name, mountPath)
		}
		return rel + strings.TrimPrefix(c, base), nil
	}
	nestedRels := make([]string, len(nested))
	for i, n := range nested {
		nestedRels[i] = relMount(n)
	}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read archive of %s: %w", mountPath, err)
		}
		name, err := rename(h.Name)
		if err != nil {
			return err
		}
		if owner(name, nestedRels) != "" {
			continue
		}
		link := h.Linkname
		if h.Typeflag == tar.TypeLink {
			if link, err = rename(h.Linkname); err != nil {
				return err
			}
		}
		if err := tw.WriteHeader(copyHeader(h, name, link)); err != nil {
			return fmt.Errorf("write archive: %w", err)
		}
		if _, err := io.Copy(tw, tr); err != nil {
			return fmt.Errorf("copy %s: %w", name, err)
		}
	}
}

// writeVolumes writes one tar of the mounts of container id to w, reading
// each with CopyFrom.
func writeVolumes(ctx context.Context, d Docker, id string, mountPaths []string, w io.Writer) error {
	tw := tar.NewWriter(w)
	for _, mp := range mountPaths {
		var nested []string
		for _, other := range mountPaths {
			if other != mp && under(relMount(other), relMount(mp)) {
				nested = append(nested, other)
			}
		}
		rc, err := d.CopyFrom(ctx, id, mp)
		if err != nil {
			return err
		}
		err = rerootVolume(tar.NewReader(rc), tw, mp, nested)
		rc.Close()
		if err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("write archive: %w", err)
	}
	return nil
}

// archiveFilter validates the entries of a volume archive and decides which
// ones belong to the volumes being restored.
type archiveFilter struct {
	mounts   []string        // relative mount paths of the service's volumes
	symlinks map[string]bool // symlinks seen so far
}

func newArchiveFilter(mountPaths []string) *archiveFilter {
	f := &archiveFilter{symlinks: make(map[string]bool)}
	for _, mp := range mountPaths {
		f.mounts = append(f.mounts, relMount(mp))
	}
	return f
}

// check validates h and returns its cleaned name and link name and the
// relative mount path it belongs to, or "" if it belongs to none of the
// volumes. Entries must not leave the archive root, must not be placed
// beneath a symlink of the archive, and hard links must stay inside their
// volume.
func (f *archiveFilter) check(h *tar.Header) (name, link, mount string, err error) {
	name, err = cleanName(h.Name)
	if err != nil {
		return "", "", "", err
	}
	for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
		if f.symlinks[dir] {
			return "", "", "", fmt.Errorf("unsafe archive entry %q: beneath symlink %q", h.Name, dir)
		}
	}
	mount = owner(name, f.mounts)
	link = h.Linkname
	switch h.Typeflag {
	case tar.TypeSymlink:
		f.symlinks[name] = true
	case tar.TypeLink:
		if link, err = cleanName(h.Linkname); err != nil {
			return "", "", "", err
		}
		if mount != "" && owner(link, f.mounts) != mount {
			return "", "", "", fmt.Errorf("unsafe archive entry %q: hard link to %q outside its volume", h.Name, h.Linkname)
		}
	}
	return name, link, mount, nil
}

// scanVolumes reads a whole volume archive, validating every entry. It
// returns which of the mount paths have entries in it, in the order given,
// and the top-level directories of entries outside all of them.
func scanVolumes(r io.Reader, mountPaths []string) (present, unknown []string, err error) {
	f := newArchiveFilter(mountPaths)
	seen := make(map[string]bool)
	other := make(map[string]bool)
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read archive: %w", err)
		}
		name, _, mount, err := f.check(h)
		if err != nil {
			return nil, nil, err
		}
		if mount != "" {
			seen[mount] = true
		} else if top, _, _ := strings.Cut(name, "/"); !other[top] {
			other[top] = true
			unknown = append(unknown, "/"+top)
		}
		// Reading the contents verifies the decryption and compression.
		if _, err := io.Copy(io.Discard, tr); err != nil {
			return nil, nil, fmt.Errorf("read archive: %w", err)
		}
	}
	for _, mp := range mountPaths {
		if seen[relMount(mp)] {
			present = append(present, mp)
		}
	}
	return present, unknown, nil
}

// filterVolumes copies the entries of the volume archive r that belong to
// the mount paths to w, as a tar to extract at "/".
func filterVolumes(r io.Reader, w io.Writer, mountPaths []string) error {
	f := newArchiveFilter(mountPaths)
	tr := tar.NewReader(r)
	tw := tar.NewWriter(w)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}
		name, link, mount, err := f.check(h)
		if err != nil {
			return err
		}
		if mount == "" {
			continue
		}
		if err := tw.WriteHeader(copyHeader(h, name, link)); err != nil {
			return fmt.Errorf("write archive: %w", err)
		}
		if _, err := io.Copy(tw, tr); err != nil {
			return fmt.Errorf("copy %s: %w", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("write archive: %w", err)
	}
	return nil
}
