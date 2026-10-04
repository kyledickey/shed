package backup

import (
	"errors"
	"fmt"
	"io"
	"runtime"

	"filippo.io/age"
	"github.com/klauspost/compress/zstd"
)

// Compression levels of a policy.
const (
	compressFastest = "fastest"
	compressDefault = "default"
	compressBetter  = "better"
	compressBest    = "best"
)

// encoderOptions returns the zstd options for a policy compression level.
func encoderOptions(level string) ([]zstd.EOption, error) {
	// Concurrent blocks would compress faster, but buffer hundreds of MiB.
	// Streaming keeps the encoder near the window size.
	opts := []zstd.EOption{zstd.WithEncoderConcurrency(min(runtime.GOMAXPROCS(0), 4))}
	switch level {
	case compressFastest:
		opts = append(opts, zstd.WithEncoderLevel(zstd.SpeedFastest))
	case compressDefault:
		opts = append(opts, zstd.WithEncoderLevel(zstd.SpeedDefault))
	case compressBetter:
		opts = append(opts, zstd.WithEncoderLevel(zstd.SpeedBetterCompression))
	case compressBest:
		// A larger window finds more of the long-range repetition in dumps.
		// 16 MiB stays well inside what the zstd CLI decodes by default.
		opts = append(opts, zstd.WithEncoderLevel(zstd.SpeedBestCompression), zstd.WithWindowSize(16<<20))
	default:
		return nil, fmt.Errorf("unknown compression %q", level)
	}
	return opts, nil
}

// encoder compresses, and optionally encrypts, what is written to it.
type encoder struct {
	zw  *zstd.Encoder
	enc io.WriteCloser // nil without encryption
}

// newEncoder returns a writer that compresses data at level and, if
// recipient is not nil, encrypts the result to it, writing to w. Close
// flushes everything to w but does not close w.
func newEncoder(w io.Writer, level string, recipient age.Recipient) (*encoder, error) {
	opts, err := encoderOptions(level)
	if err != nil {
		return nil, err
	}
	e := &encoder{}
	if recipient != nil {
		e.enc, err = age.Encrypt(w, recipient)
		if err != nil {
			return nil, fmt.Errorf("encrypt: %w", err)
		}
		w = e.enc
	}
	e.zw, err = zstd.NewWriter(w, opts...)
	if err != nil {
		return nil, fmt.Errorf("compress: %w", err)
	}
	return e, nil
}

func (e *encoder) Write(p []byte) (int, error) { return e.zw.Write(p) }

// Close finishes the zstd frame and then the age stream.
func (e *encoder) Close() error {
	if err := e.zw.Close(); err != nil {
		return fmt.Errorf("compress: %w", err)
	}
	if e.enc != nil {
		if err := e.enc.Close(); err != nil {
			return fmt.Errorf("encrypt: %w", err)
		}
	}
	return nil
}

// decrypt returns r decrypted with identity, or r itself if identity is nil.
// It fails at once if the archive is not encrypted to identity; corrupted
// data is reported by later reads.
func decrypt(r io.Reader, identity age.Identity) (io.Reader, error) {
	if identity == nil {
		return r, nil
	}
	dr, err := age.Decrypt(r, identity)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return dr, nil
}

// decompressor reads a zstd stream.
type decompressor struct{ d *zstd.Decoder }

// decompress returns a reader of the decompressed stream r. Closing it
// releases the decoder but does not close r.
func decompress(r io.Reader) (io.ReadCloser, error) {
	d, err := zstd.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}
	return decompressor{d}, nil
}

func (d decompressor) Read(p []byte) (int, error) {
	n, err := d.d.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		err = fmt.Errorf("decompress: %w", err)
	}
	return n, err
}

func (d decompressor) Close() error {
	d.d.Close()
	return nil
}

// decode returns the original data of the archive r: decrypted with identity
// when it is not nil, then decompressed.
func decode(r io.Reader, identity age.Identity) (io.ReadCloser, error) {
	dr, err := decrypt(r, identity)
	if err != nil {
		return nil, err
	}
	return decompress(dr)
}
