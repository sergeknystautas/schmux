package workspace

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

func TestTruncateString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "short string unchanged",
			input:  "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "exact length unchanged",
			input:  "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "long string truncated with ellipsis",
			input:  "hello world this is a long string",
			maxLen: 10,
			want:   "hello w...",
		},
		{
			name:   "empty string unchanged",
			input:  "",
			maxLen: 10,
			want:   "",
		},
		{
			name:   "maxLen 3 keeps ellipsis",
			input:  "abcdef",
			maxLen: 3,
			want:   "abc",
		},
		{
			name:   "maxLen 4 truncates with ellipsis",
			input:  "abcdef",
			maxLen: 4,
			want:   "a...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateString(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

// TestLinearSyncFromDefault_RejectsOrphanDefaultBranch verifies that LinearSyncFromDefault
// returns an error when origin/main is an orphan commit with no shared ancestry.
func TestLinearSyncFromDefault_RejectsOrphanDefaultBranch(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create a "remote" repo with an initial commit on main
	remoteDir := gitTestWorkTree(t)

	// Clone it to create the workspace
	tmpDir := t.TempDir()
	cloneDir := filepath.Join(tmpDir, "clone")
	runGit(t, tmpDir, "clone", remoteDir, "clone")
	runGit(t, cloneDir, "config", "user.email", "test@test.com")
	runGit(t, cloneDir, "config", "user.name", "Test")

	// Make a local commit on the feature branch so we're "ahead"
	runGit(t, cloneDir, "checkout", "-b", "feature")
	writeFile(t, cloneDir, "local.txt", "local work")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "local commit")

	// Force-push an orphan commit to main on the remote
	runGit(t, remoteDir, "checkout", "--orphan", "orphan-temp")
	writeFile(t, remoteDir, "orphan.txt", "orphan content")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "orphan commit")
	// Force update main to point to the orphan commit
	runGit(t, remoteDir, "branch", "-f", "main")
	runGit(t, remoteDir, "checkout", "main")

	// Set up workspace manager
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath)
	m := New(cfg, st, statePath)
	m.setDefaultBranch(remoteDir, "main")

	// Add workspace to state
	w := state.Workspace{
		ID:     "test-001",
		Repo:   remoteDir,
		Branch: "feature",
		Path:   cloneDir,
	}
	st.AddWorkspace(w)

	ctx := context.Background()
	result, err := m.LinearSyncFromDefault(ctx, "test-001")

	if err == nil {
		t.Fatalf("LinearSyncFromDefault() should have returned an error for orphan default branch, got result: %+v", result)
	}

	if !strings.Contains(err.Error(), "no common ancestor") {
		t.Errorf("error should mention 'no common ancestor', got: %v", err)
	}

	if !strings.Contains(err.Error(), "force-pushed") {
		t.Errorf("error should mention 'force-pushed', got: %v", err)
	}
}
