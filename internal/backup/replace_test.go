package backup

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestReadManifest(t *testing.T) {
	// The same tree, archived with the hard link either way round.
	a := makeTar(t,
		entry{name: "data/", typ: tar.TypeDir},
		entry{name: "data/a", body: "same"},
		entry{name: "data/b", typ: tar.TypeLink, link: "data/a"},
		entry{name: "data/l", typ: tar.TypeSymlink, link: "a"},
	)
	b := makeTar(t,
		entry{name: "data/b", body: "same"},
		entry{name: "data/a", typ: tar.TypeLink, link: "data/b"},
		entry{name: "data/l", typ: tar.TypeSymlink, link: "a"},
	)
	ma, err := readManifest(bytes.NewReader(a))
	if err != nil {
		t.Fatal(err)
	}
	mb, err := readManifest(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if got := ma.missing(mb); !reflect.DeepEqual(got, []string{"data"}) {
		t.Errorf("missing = %q, want only the directory", got)
	}
	if got := mb.missing(ma); len(got) != 0 {
		t.Errorf("missing = %q, want none", got)
	}

	tests := []struct {
		name string
		tar  []byte
		want string
	}{
		{"different content", makeTar(t, entry{name: "data/a", body: "diff"}), "data/a"},
		{"different symlink", makeTar(t, entry{name: "data/a", body: "same"}, entry{name: "data/l", typ: tar.TypeSymlink, link: "b"}), "data/l"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := readManifest(bytes.NewReader(tt.tar))
			if err != nil {
				t.Fatal(err)
			}
			if got := mb.missing(m); !strings.Contains(strings.Join(got, ","), tt.want) {
				t.Errorf("missing = %q, want %q among them", got, tt.want)
			}
		})
	}

	dangling := makeTar(t, entry{name: "data/b", typ: tar.TypeLink, link: "data/a"})
	if _, err := readManifest(bytes.NewReader(dangling)); err == nil {
		t.Error("hard link to a missing file accepted")
	}
	unsafe := makeTar(t, entry{name: "../x", body: "x"})
	if _, err := readManifest(bytes.NewReader(unsafe)); err == nil {
		t.Error("unsafe name accepted")
	}
}

func TestTee(t *testing.T) {
	errFill, errSink := errors.New("fill failed"), errors.New("sink failed")
	write := func(w io.Writer) error {
		for range 1000 {
			if _, err := w.Write(bytes.Repeat([]byte("x"), 1024)); err != nil {
				return err
			}
		}
		return nil
	}
	readAll := func(r io.Reader) error { _, err := io.Copy(io.Discard, r); return err }
	tests := []struct {
		name  string
		fill  func(io.Writer) error
		sinks []func(io.Reader) error
		want  error
	}{
		{"success", write, []func(io.Reader) error{readAll, readAll}, nil},
		{"sink stops early", write, []func(io.Reader) error{readAll, func(io.Reader) error { return nil }}, nil},
		{"fill fails", func(w io.Writer) error { write(w); return errFill }, []func(io.Reader) error{readAll}, errFill},
		{"sink fails", write, []func(io.Reader) error{readAll, func(io.Reader) error { return errSink }}, errSink},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tee(tt.fill, tt.sinks...); !errors.Is(err, tt.want) {
				t.Errorf("tee = %v, want %v", err, tt.want)
			}
		})
	}
}
