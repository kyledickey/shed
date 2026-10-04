package docker

import (
	"slices"
	"testing"
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
