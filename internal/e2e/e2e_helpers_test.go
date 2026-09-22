package e2e

import "testing"

func TestTmuxListReportsNoServer(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "server already stopped",
			output: "no server running on /tmp/tmux-0/schmux",
			want:   true,
		},
		{
			name:   "server exits while client is connected",
			output: "server exited unexpectedly",
			want:   true,
		},
		{
			name:   "unrelated tmux error",
			output: "error connecting to /tmp/tmux-0/schmux (permission denied)",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tmuxListReportsNoServer(tt.output); got != tt.want {
				t.Fatalf("tmuxListReportsNoServer(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}
