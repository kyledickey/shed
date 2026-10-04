package backup

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"

	"filippo.io/age"
)

func TestCodecRoundTrip(t *testing.T) {
	data := bytes.Repeat([]byte("shed backup data; "), 50_000)
	random := make([]byte, 300_000)
	rand.Read(random)
	data = append(data, random...)

	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []string{compressFastest, compressDefault, compressBetter, compressBest} {
		for _, encrypted := range []bool{false, true} {
			var recipient age.Recipient
			var identity age.Identity
			if encrypted {
				recipient, identity = id.Recipient(), id
			}
			var archive bytes.Buffer
			enc, err := newEncoder(&archive, level, recipient)
			if err != nil {
				t.Fatalf("%s: %v", level, err)
			}
			if _, err := enc.Write(data); err != nil {
				t.Fatal(err)
			}
			if err := enc.Close(); err != nil {
				t.Fatal(err)
			}
			if archive.Len() >= len(data) {
				t.Errorf("%s, encrypted %v: archive of %d bytes is not smaller than %d", level, encrypted, archive.Len(), len(data))
			}
			r, err := decode(bytes.NewReader(archive.Bytes()), identity)
			if err != nil {
				t.Fatalf("%s, encrypted %v: decode: %v", level, encrypted, err)
			}
			got, err := io.ReadAll(r)
			r.Close()
			if err != nil {
				t.Fatalf("%s, encrypted %v: read: %v", level, encrypted, err)
			}
			if !bytes.Equal(got, data) {
				t.Errorf("%s, encrypted %v: round trip changed the data", level, encrypted)
			}
		}
	}
}

func TestCodecErrors(t *testing.T) {
	if _, err := newEncoder(io.Discard, "ultra", nil); err == nil {
		t.Error("newEncoder with an unknown level succeeded")
	}

	id, _ := age.GenerateX25519Identity()
	other, _ := age.GenerateX25519Identity()
	var archive bytes.Buffer
	enc, err := newEncoder(&archive, compressFastest, id.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(enc, "secret data")
	enc.Close()

	if _, err := decode(bytes.NewReader(archive.Bytes()), other); err == nil {
		t.Error("decode with the wrong identity succeeded")
	}

	corrupt := bytes.Clone(archive.Bytes())
	corrupt[len(corrupt)-5] ^= 0xff
	r, err := decode(bytes.NewReader(corrupt), id)
	if err == nil {
		_, err = io.ReadAll(r)
		r.Close()
	}
	if err == nil {
		t.Error("decoding a corrupted archive succeeded")
	}
}
