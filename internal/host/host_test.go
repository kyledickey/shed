package host

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRead(t *testing.T) {
	dir := t.TempDir()
	proc, sys := filepath.Join(dir, "proc"), filepath.Join(dir, "sys")
	write(t, filepath.Join(proc, "stat"), `cpu  100 10 50 800 40 0 5 0 7 0
cpu0 50 5 25 400 20 0 3 0 4 0
cpu1 50 5 25 400 20 0 2 0 3 0
intr 12345
`)
	write(t, filepath.Join(proc, "meminfo"), `MemTotal:        2048 kB
MemFree:          512 kB
MemAvailable:    1024 kB
`)
	write(t, filepath.Join(proc, "net/dev"), `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:    999       1    0    0    0     0          0         0      999       1    0    0    0     0       0          0
  eth0:   1000      10    0    0    0     0          0         0     2000      20    0    0    0     0       0          0
docker0:   5000      10    0    0    0     0          0         0     5000      20    0    0    0     0       0          0
`)
	write(t, filepath.Join(proc, "diskstats"), `   8       0 sda 10 0 100 0 20 0 200 0 0 0 0
   8       1 sda1 10 0 100 0 20 0 200 0 0 0 0
   7       0 loop0 1 0 9999 0 0 0 0 0 0 0 0
 253       0 dm-0 1 0 9999 0 1 0 9999 0 0 0 0
`)
	write(t, filepath.Join(sys, "class/net/eth0/device/vendor"), "")
	write(t, filepath.Join(sys, "class/net/lo/type"), "")
	write(t, filepath.Join(sys, "block/sda/device/model"), "")
	write(t, filepath.Join(sys, "block/loop0/size"), "")

	s, err := Reader{Path: dir, Proc: proc, Sys: sys}.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.CPUs != 2 || s.CPUTotal != 1005 || s.CPUIdle != 840 {
		t.Errorf("cpu = %d cpus, total %d, idle %d; want 2, 1005, 840", s.CPUs, s.CPUTotal, s.CPUIdle)
	}
	if s.MemoryTotal != 2048<<10 || s.MemoryAvailable != 1024<<10 {
		t.Errorf("memory = %d of %d", s.MemoryAvailable, s.MemoryTotal)
	}
	if s.NetRx != 1000 || s.NetTx != 2000 {
		t.Errorf("net = %d in, %d out; want 1000, 2000", s.NetRx, s.NetTx)
	}
	if s.DiskRead != 100*512 || s.DiskWrite != 200*512 {
		t.Errorf("disk = %d read, %d written; want %d, %d", s.DiskRead, s.DiskWrite, 100*512, 200*512)
	}
	if s.DiskTotal == 0 || s.DiskUsed > s.DiskTotal {
		t.Errorf("filesystem = %d used of %d", s.DiskUsed, s.DiskTotal)
	}
}

func TestReadErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := (Reader{Proc: dir, Sys: dir}).Read(context.Background()); err == nil {
		t.Error("Read() with an empty procfs succeeded")
	}
	write(t, filepath.Join(dir, "stat"), "cpu 1 2\n")
	if _, err := (Reader{Proc: dir, Sys: dir}).Read(context.Background()); err == nil {
		t.Error("Read() with a short cpu line succeeded")
	}
}
