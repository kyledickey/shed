package backup

import (
	"archive/tar"
	"bytes"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

// entry is a tar entry for tests.
type entry struct {
	name, link string
	typ        byte
	body       string
	uid        int
	mode       int64
}

func makeTar(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		h := &tar.Header{Name: e.name, Linkname: e.link, Typeflag: typ, Size: int64(len(e.body)), Uid: e.uid, Mode: mode, ModTime: time.Unix(1e9, 0)}
		if typ != tar.TypeReg {
			h.Size = 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		io.WriteString(tw, e.body)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func readTar(t *testing.T, r io.Reader) []entry {
	t.Helper()
	var out []entry
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(tr)
		out = append(out, entry{name: h.Name, link: h.Linkname, typ: h.Typeflag, body: string(body), uid: h.Uid, mode: h.Mode})
	}
}

func TestRerootVolume(t *testing.T) {
	in := makeTar(t,
		entry{name: "data/", typ: tar.TypeDir, uid: 70, mode: 0o700},
		entry{name: "data/a", body: "A", uid: 70, mode: 0o600},
		entry{name: "data/sub/b", body: "B"},
		entry{name: "data/l", typ: tar.TypeSymlink, link: "/etc/passwd"},
		entry{name: "data/h", typ: tar.TypeLink, link: "data/a"},
		entry{name: "data/nested/x", body: "skipped"},
	)
	var out bytes.Buffer
	tw := tar.NewWriter(&out)
	if err := rerootVolume(tar.NewReader(bytes.NewReader(in)), tw, "/var/lib/data/", []string{"/var/lib/data/nested"}); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	got := readTar(t, &out)
	want := []entry{
		{name: "var/lib/data/", typ: tar.TypeDir, uid: 70, mode: 0o700},
		{name: "var/lib/data/a", typ: tar.TypeReg, body: "A", uid: 70, mode: 0o600},
		{name: "var/lib/data/sub/b", typ: tar.TypeReg, body: "B", mode: 0o644},
		{name: "var/lib/data/l", typ: tar.TypeSymlink, link: "/etc/passwd", mode: 0o644},
		{name: "var/lib/data/h", typ: tar.TypeLink, link: "var/lib/data/a", mode: 0o644},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rerootVolume:\n got %+v\nwant %+v", got, want)
	}

	for _, bad := range []entry{
		{name: "other/x", body: "x"},
		{name: "data/../../etc/x", body: "x"},
		{name: "/data/x", body: "x"},
		{name: "data/h", typ: tar.TypeLink, link: "../etc/shadow"},
	} {
		in := makeTar(t, bad)
		if err := rerootVolume(tar.NewReader(bytes.NewReader(in)), tar.NewWriter(io.Discard), "/data", nil); err == nil {
			t.Errorf("rerootVolume accepted %+v", bad)
		}
	}
}

func TestScanAndFilterVolumes(t *testing.T) {
	archive := makeTar(t,
		entry{name: "var/lib/data/", typ: tar.TypeDir},
		entry{name: "var/lib/data/a", body: "A"},
		entry{name: "srv/old/x", body: "gone"},
		entry{name: "cache/y", body: "Y"},
		entry{name: "cache/h", typ: tar.TypeLink, link: "cache/y"},
	)
	mounts := []string{"/var/lib/data", "/cache", "/absent"}
	present, unknown, err := scanVolumes(bytes.NewReader(archive), mounts)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/var/lib/data", "/cache"}; !reflect.DeepEqual(present, want) {
		t.Errorf("present = %v, want %v", present, want)
	}
	if want := []string{"/srv"}; !reflect.DeepEqual(unknown, want) {
		t.Errorf("unknown = %v, want %v", unknown, want)
	}

	var out bytes.Buffer
	if err := filterVolumes(bytes.NewReader(archive), &out, []string{"/var/lib/data"}); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range readTar(t, &out) {
		names = append(names, e.name)
	}
	if want := []string{"var/lib/data/", "var/lib/data/a"}; !reflect.DeepEqual(names, want) {
		t.Errorf("filtered = %v, want %v", names, want)
	}
}

func TestArchiveRejectsUnsafeEntries(t *testing.T) {
	tests := []struct {
		name    string
		entries []entry
	}{
		{"parent", []entry{{name: "data/../../etc/passwd", body: "x"}}},
		{"dotdot", []entry{{name: "..", typ: tar.TypeDir}}},
		{"absolute", []entry{{name: "/etc/passwd", body: "x"}}},
		{"beneath symlink", []entry{
			{name: "data/l", typ: tar.TypeSymlink, link: "/etc"},
			{name: "data/l/passwd", body: "x"},
		}},
		{"hard link out of volume", []entry{{name: "data/h", typ: tar.TypeLink, link: "other/secret"}}},
		{"hard link traversal", []entry{{name: "data/h", typ: tar.TypeLink, link: "data/../../x"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archive := makeTar(t, tt.entries...)
			if _, _, err := scanVolumes(bytes.NewReader(archive), []string{"/data", "/other"}); err == nil || !strings.Contains(err.Error(), "unsafe") {
				t.Errorf("scanVolumes error = %v, want unsafe entry", err)
			}
			if err := filterVolumes(bytes.NewReader(archive), io.Discard, []string{"/data", "/other"}); err == nil {
				t.Error("filterVolumes accepted the archive")
			}
		})
	}
}

func TestRedisFiles(t *testing.T) {
	var out bytes.Buffer
	err := redisFiles(&out, 3, time.Unix(1e9, 0), func(w io.Writer) error {
		_, err := io.WriteString(w, "RDB")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	got := readTar(t, &out)
	want := []entry{
		{name: "data/dump.rdb", typ: tar.TypeReg, body: "RDB", mode: 0o644},
		{name: "data/appendonlydir/", typ: tar.TypeDir, mode: 0o755},
		{name: "data/appendonlydir/appendonly.aof.1.base.rdb", typ: tar.TypeLink, link: "data/dump.rdb"},
		{name: "data/appendonlydir/appendonly.aof.manifest", typ: tar.TypeReg, body: "file appendonly.aof.1.base.rdb seq 1 type b\n", mode: 0o644},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("redisFiles:\n got %+v\nwant %+v", got, want)
	}
	// The files pass the restore filter.
	if err := filterVolumes(bytes.NewReader(out.Bytes()), io.Discard, []string{"/data"}); err != nil {
		t.Error(err)
	}
}
