package workspace

import (
	"context"
	"testing"
	"time"
)

func TestParseDefaultBranch(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "standard main",
			output: "ref: refs/heads/main\tHEAD\nabc123\tHEAD\n",
			want:   "main",
		},
		{
			name:   "master branch",
			output: "ref: refs/heads/master\tHEAD\nabc123\tHEAD\n",
			want:   "master",
		},
		{
			name:   "custom branch",
			output: "ref: refs/heads/develop\tHEAD\nabc123\tHEAD\n",
			want:   "develop",
		},
		{
			name:   "no symref (old server)",
			output: "abc123\tHEAD\n",
			want:   "main",
		},
		{
			name:   "empty output",
			output: "",
			want:   "main",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDefaultBranch(tt.output)
			if got != tt.want {
				t.Errorf("parseDefaultBranch() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProbeRepoAccess_InvalidURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := ProbeRepoAccess(ctx, "not-a-valid-url")
	if result.Accessible {
		t.Error("expected inaccessible for invalid URL")
	}
	if result.Error == "" {
		t.Error("expected error message for invalid URL")
	}
}
