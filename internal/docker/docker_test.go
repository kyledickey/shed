package docker

import (
	"slices"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
)

func TestTagRepo(t *testing.T) {
	tests := []struct{ ref, want string }{
		{"shed/abc:def", "shed/abc"},
		{"shed/abc", "shed/abc"},
		{"localhost:5000/shed/abc:def", "localhost:5000/shed/abc"},
		{"localhost:5000/shed/abc", "localhost:5000/shed/abc"},
	}
	for _, tt := range tests {
		if got := tagRepo(tt.ref); got != tt.want {
			t.Errorf("tagRepo(%q) = %q, want %q", tt.ref, got, tt.want)
		}
	}
}

func TestTCPPorts(t *testing.T) {
	exposed := map[string]struct{}{"8080/tcp": {}, "80/tcp": {}, "53/udp": {}, "443": {}, "bogus": {}}
	got := tcpPorts(exposed)
	if want := []int{80, 443, 8080}; !slices.Equal(got, want) {
		t.Errorf("tcpPorts = %v, want %v", got, want)
	}
	if got := tcpPorts(nil); got != nil {
		t.Errorf("tcpPorts(nil) = %v, want nil", got)
	}
}

func TestStatsFrom(t *testing.T) {
	read := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	base := func(mem map[string]uint64) container.StatsResponse {
		return container.StatsResponse{
			Read: read,
			CPUStats: container.CPUStats{
				CPUUsage:    container.CPUUsage{TotalUsage: 500, PercpuUsage: []uint64{1, 2}},
				SystemUsage: 9000,
			},
			MemoryStats: container.MemoryStats{Usage: 1000, Stats: mem},
			Networks: map[string]container.NetworkStats{
				"eth0": {RxBytes: 10, TxBytes: 20},
				"eth1": {RxBytes: 1, TxBytes: 2},
			},
			BlkioStats: container.BlkioStats{IoServiceBytesRecursive: []container.BlkioStatEntry{
				{Op: "Read", Value: 100}, {Op: "read", Value: 5},
				{Op: "Write", Value: 7}, {Op: "Total", Value: 112},
			}},
		}
	}
	tests := []struct {
		name    string
		mem     map[string]uint64
		wantMem uint64
	}{
		{"no stats", nil, 1000},
		{"cgroup v2", map[string]uint64{"inactive_file": 300, "total_inactive_file": 1}, 700},
		{"cgroup v1", map[string]uint64{"total_inactive_file": 200, "cache": 1}, 800},
		{"cache only", map[string]uint64{"cache": 100}, 900},
		{"inactive above usage", map[string]uint64{"inactive_file": 2000}, 1000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := Stats{
				Read: read, CPUTotal: 500, SystemCPU: 9000, OnlineCPUs: 2,
				MemoryUsage: tt.wantMem, NetRx: 11, NetTx: 22, DiskRead: 105, DiskWrite: 7,
			}
			if got := statsFrom(base(tt.mem)); got != want {
				t.Errorf("statsFrom = %+v, want %+v", got, want)
			}
		})
	}
}

func TestVolumeMounts(t *testing.T) {
	got := volumeMounts([]Mount{
		{Volume: "data", Target: "/var/lib/data"},
		{Volume: "conf", Target: "/etc/conf", ReadOnly: true},
	})
	want := []mount.Mount{
		{Type: mount.TypeVolume, Source: "data", Target: "/var/lib/data"},
		{Type: mount.TypeVolume, Source: "conf", Target: "/etc/conf", ReadOnly: true},
	}
	if !slices.Equal(got, want) {
		t.Errorf("volumeMounts = %+v, want %+v", got, want)
	}
	if got := volumeMounts(nil); got == nil || len(got) != 0 {
		t.Errorf("volumeMounts(nil) = %#v, want empty non-nil slice", got)
	}
}
