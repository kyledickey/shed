package docker

import "testing"

func TestBridge(t *testing.T) {
	tests := []struct {
		id      string
		options map[string]string
		want    string
	}{
		{"7d86d31b1478e7cca9ebed7e73aa0fdeec46c5ca29497431d3007d2d9e15ed99", nil, "br-7d86d31b1478"},
		{"7d86d31b1478e7cc", map[string]string{"com.docker.network.bridge.name": "docker1"}, "docker1"},
	}
	for _, tt := range tests {
		if got := bridge(tt.id, tt.options); got != tt.want {
			t.Errorf("bridge(%q, %v) = %q, want %q", tt.id, tt.options, got, tt.want)
		}
	}
}
