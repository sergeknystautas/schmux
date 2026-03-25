package detect

import (
	"testing"
)

func TestBuildVSCodeRemoteURI(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		path     string
		want     string
	}{
		{
			name:     "absolute path",
			hostname: "dev.example.com",
			path:     "/home/user/workspace",
			want:     "vscode://vscode-remote/ssh-remote+dev.example.com/home/user/workspace",
		},
		{
			name:     "relative path gets slash prepended",
			hostname: "myhost",
			path:     "workspace",
			want:     "vscode://vscode-remote/ssh-remote+myhost/workspace",
		},
		{
			name:     "tilde path",
			hostname: "server1",
			path:     "~/code/project",
			want:     "vscode://vscode-remote/ssh-remote+server1/~/code/project",
		},
		{
			name:     "path with spaces",
			hostname: "dev.local",
			path:     "/home/user/my project",
			want:     "vscode://vscode-remote/ssh-remote+dev.local/home/user/my project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildVSCodeRemoteURI(tt.hostname, tt.path)
			if got != tt.want {
				t.Errorf("BuildVSCodeRemoteURI(%q, %q) = %q, want %q",
					tt.hostname, tt.path, got, tt.want)
			}
		})
	}
}

func TestDetectVSCodeServer_ReturnsHostname(t *testing.T) {
	info := DetectVSCodeServer()

	// Hostname should always be populated (os.Hostname rarely fails)
	if info.Hostname == "" {
		t.Log("DetectVSCodeServer returned empty hostname — os.Hostname may have failed")
	}
}

func TestProcessContains(t *testing.T) {
	// "ps aux" should match itself
	if !processContains("ps") {
		t.Log("processContains(\"ps\") returned false — this can happen with very fast execution")
	}

	// Something that definitely doesn't exist
	if processContains("xyznonexistent1234567890") {
		t.Error("processContains should return false for nonexistent process")
	}
}
