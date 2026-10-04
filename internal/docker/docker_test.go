package docker

import "testing"

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
