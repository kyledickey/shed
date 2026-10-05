package api

import (
	"strings"
	"testing"
)

func TestLineWriterBounds(t *testing.T) {
	var lines []string
	lw := &lineWriter{emit: func(line string) {
		if len(line) > maxLogLineBytes {
			t.Fatalf("line has %d bytes", len(line))
		}
		lines = append(lines, line)
	}}
	input := strings.Repeat("x", maxLogLineBytes*3+11)
	for _, part := range []string{input[:7], input[7:]} {
		if n, err := lw.Write([]byte(part)); err != nil || n != len(part) {
			t.Fatalf("Write = %d, %v", n, err)
		}
		if len(lw.buf) > maxLogLineBytes || cap(lw.buf) > 2*maxLogLineBytes {
			t.Fatalf("buffer unbounded: %d/%d", len(lw.buf), cap(lw.buf))
		}
	}
	lw.flush()
	if strings.Join(lines, "") != input {
		t.Fatal("output lost or duplicated")
	}
}

func TestLineWriterSplitCRLF(t *testing.T) {
	var lines []string
	lw := &lineWriter{emit: func(line string) { lines = append(lines, line) }}
	lw.Write([]byte("hello\r"))
	lw.Write([]byte("\nworld\nlast"))
	lw.flush()
	if strings.Join(lines, "|") != "hello|world|last" {
		t.Fatalf("lines = %q", lines)
	}
}
