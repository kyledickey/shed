// Package host reads the resource usage of the Linux machine shed runs on
// from procfs, sysfs, and statfs.
//
// Counters are cumulative since boot; callers turn two readings into rates.
// Network and disk I/O only count physical devices, those with a device link
// in sysfs, so that traffic through Docker's bridges and veth pairs, loop
// devices, and device-mapper volumes is not counted twice.
package host

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Stats is one reading of the host's resource usage.
type Stats struct {
	// CPUs is the number of online CPUs.
	CPUs int
	// CPUTotal and CPUIdle are the cumulative time of all CPUs, and the
	// part of it spent idle or waiting for I/O, in clock ticks.
	CPUTotal, CPUIdle uint64
	// MemoryTotal and MemoryAvailable are in bytes. Available memory
	// includes reclaimable page cache.
	MemoryTotal, MemoryAvailable uint64
	// NetRx and NetTx are cumulative bytes over physical network interfaces.
	NetRx, NetTx uint64
	// DiskRead and DiskWrite are cumulative bytes over physical disks.
	DiskRead, DiskWrite uint64
	// DiskTotal and DiskUsed are the size and used bytes of the filesystem
	// holding the reader's path.
	DiskTotal, DiskUsed uint64
}

// Reader reads host stats. The zero value reads /proc and /sys and reports
// the filesystem of /.
type Reader struct {
	// Path is a path on the filesystem whose usage is reported; empty
	// means /.
	Path string
	// Proc and Sys are the procfs and sysfs mount points; empty means
	// /proc and /sys.
	Proc, Sys string
}

// Read returns the current stats.
func (r Reader) Read(ctx context.Context) (Stats, error) {
	var s Stats
	if err := ctx.Err(); err != nil {
		return s, err
	}
	proc, sys, path := or(r.Proc, "/proc"), or(r.Sys, "/sys"), or(r.Path, "/")
	if err := readCPU(filepath.Join(proc, "stat"), &s); err != nil {
		return s, err
	}
	if err := readMemory(filepath.Join(proc, "meminfo"), &s); err != nil {
		return s, err
	}
	if err := readNet(filepath.Join(proc, "net/dev"), filepath.Join(sys, "class/net"), &s); err != nil {
		return s, err
	}
	if err := readDisks(filepath.Join(proc, "diskstats"), filepath.Join(sys, "block"), &s); err != nil {
		return s, err
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return s, fmt.Errorf("host: statfs %s: %w", path, err)
	}
	bsize := uint64(fs.Bsize)
	s.DiskTotal = uint64(fs.Blocks) * bsize
	s.DiskUsed = (uint64(fs.Blocks) - uint64(fs.Bfree)) * bsize
	return s, nil
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// readCPU parses the aggregate cpu line and counts the cpuN lines of
// /proc/stat. The total is user through steal; guest time is already part
// of user and nice.
func readCPU(path string, s *Stats) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("host: %w", err)
	}
	for line := range strings.Lines(string(data)) {
		f := strings.Fields(line)
		if len(f) == 0 || !strings.HasPrefix(f[0], "cpu") {
			continue
		}
		if f[0] != "cpu" {
			s.CPUs++
			continue
		}
		if len(f) < 9 {
			return fmt.Errorf("host: %s: short cpu line", path)
		}
		for i, v := range f[1:9] {
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return fmt.Errorf("host: %s: %w", path, err)
			}
			s.CPUTotal += n
			if i == 3 || i == 4 { // idle, iowait
				s.CPUIdle += n
			}
		}
	}
	if s.CPUTotal == 0 {
		return fmt.Errorf("host: %s: no cpu line", path)
	}
	return nil
}

func readMemory(path string, s *Stats) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("host: %w", err)
	}
	for line := range strings.Lines(string(data)) {
		key, rest, ok := strings.Cut(line, ":")
		if !ok || (key != "MemTotal" && key != "MemAvailable") {
			continue
		}
		f := strings.Fields(rest)
		if len(f) == 0 {
			return fmt.Errorf("host: %s: empty %s", path, key)
		}
		kb, err := strconv.ParseUint(f[0], 10, 64)
		if err != nil {
			return fmt.Errorf("host: %s: %s: %w", path, key, err)
		}
		if key == "MemTotal" {
			s.MemoryTotal = kb * 1024
		} else {
			s.MemoryAvailable = kb * 1024
		}
	}
	if s.MemoryTotal == 0 {
		return fmt.Errorf("host: %s: no MemTotal", path)
	}
	return nil
}

// readNet sums received and transmitted bytes in /proc/net/dev over the
// interfaces that have a device in sysfs.
func readNet(path, classNet string, s *Stats) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("host: %w", err)
	}
	for line := range strings.Lines(string(data)) {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue // header
		}
		name = strings.TrimSpace(name)
		if !physical(filepath.Join(classNet, name)) {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 9 {
			return fmt.Errorf("host: %s: short line for %s", path, name)
		}
		rx, err1 := strconv.ParseUint(f[0], 10, 64)
		tx, err2 := strconv.ParseUint(f[8], 10, 64)
		if err1 != nil || err2 != nil {
			return fmt.Errorf("host: %s: bad counters for %s", path, name)
		}
		s.NetRx += rx
		s.NetTx += tx
	}
	return nil
}

// readDisks sums sectors read and written in /proc/diskstats over the whole
// disks that have a device in sysfs. Sectors there are always 512 bytes.
func readDisks(path, block string, s *Stats) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("host: %w", err)
	}
	for line := range strings.Lines(string(data)) {
		f := strings.Fields(line)
		if len(f) < 10 || !physical(filepath.Join(block, f[2])) {
			continue
		}
		read, err1 := strconv.ParseUint(f[5], 10, 64)
		written, err2 := strconv.ParseUint(f[9], 10, 64)
		if err1 != nil || err2 != nil {
			return fmt.Errorf("host: %s: bad counters for %s", path, f[2])
		}
		s.DiskRead += read * 512
		s.DiskWrite += written * 512
	}
	return nil
}

// physical reports whether the sysfs entry dir has a device link, which
// virtual interfaces and block devices lack.
func physical(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "device"))
	return err == nil
}
