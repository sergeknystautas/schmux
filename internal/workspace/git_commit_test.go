package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/state"
)

func TestValidateCommitHash(t *testing.T) {
	tests := []struct {
		name    string
		hash    string
		wantErr bool
	}{
		// Valid hashes
		{"short hash", "abc1234", false},
		{"full hash", "abc1234567890abcdef1234567890abcdef12345", false},
		{"minimum length", "abcd", false},
		{"uppercase", "ABCD1234", false},
		{"mixed case", "AbCd1234", false},

		// Invalid hashes
		{"empty", "", true},
		{"too short", "abc", true},
		{"too long", "abc1234567890abcdef1234567890abcdef1234567", true},
		{"non-hex chars", "xyz1234", true},
		{"path traversal", "abc..def", true},
		{"shell injection $", "abc$def", true},
		{"shell injection backtick", "abc`def", true},
		{"shell injection semicolon", "abc;def", true},
		{"shell injection pipe", "abc|def", true},
		{"shell injection ampersand", "abc&def", true},
		{"space", "abc def", true},
		{"newline", "abc\ndef", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCommitHash(tt.hash)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateCommitHash(%q) error = %v, wantErr %v", tt.hash, err, tt.wantErr)
			}
		})
	}
}

// setupTestRepo creates a temporary git repository for testing.
func setupTestRepo(t *testing.T) (string, func()) {
	t.Helper()

	dir, err := os.MkdirTemp("", "git-commit-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	cleanup := func() {
		os.RemoveAll(dir)
	}

	// Initialize git repo
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test Author",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test Author",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\noutput: %s", args, err, output)
		}
	}

	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test Author")

	return dir, cleanup
}

func TestGetCommitDetail_BasicCommit(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	// Create a file and commit
	filePath := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(filePath, []byte("line 1\nline 2\nline 3\n"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test Author",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test Author",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
		return strings.TrimSpace(string(output))
	}

	run("add", "test.txt")
	run("commit", "-m", "Initial commit\n\nThis is the commit body.")

	hash := run("rev-parse", "HEAD")
	shortHash := hash[:7]

	// Create workspace manager with mock state
	st := state.New(filepath.Join(t.TempDir(), "state.json"))
	st.AddWorkspace(state.Workspace{
		ID:   "ws-test",
		Path: dir,
		Repo: "test-repo",
	})

	m := &Manager{state: st}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := m.GetCommitDetail(ctx, "ws-test", shortHash)
	if err != nil {
		t.Fatalf("GetCommitDetail failed: %v", err)
	}

	// Verify response
	if resp.Hash != hash {
		t.Errorf("Hash = %q, want %q", resp.Hash, hash)
	}
	if resp.ShortHash != shortHash {
		t.Errorf("ShortHash = %q, want %q", resp.ShortHash, shortHash)
	}
	if resp.AuthorName != "Test Author" {
		t.Errorf("AuthorName = %q, want %q", resp.AuthorName, "Test Author")
	}
	if resp.AuthorEmail != "test@example.com" {
		t.Errorf("AuthorEmail = %q, want %q", resp.AuthorEmail, "test@example.com")
	}
	if !strings.Contains(resp.Message, "Initial commit") {
		t.Errorf("Message = %q, want to contain 'Initial commit'", resp.Message)
	}
	if !strings.Contains(resp.Message, "This is the commit body") {
		t.Errorf("Message = %q, want to contain 'This is the commit body'", resp.Message)
	}
	if resp.IsMerge {
		t.Error("IsMerge = true, want false")
	}
	// Root commit should have no parents
	if len(resp.Parents) != 0 {
		t.Errorf("Parents = %v, want empty (root commit)", resp.Parents)
	}
	// Should have one file
	if len(resp.Files) != 1 {
		t.Errorf("len(Files) = %d, want 1", len(resp.Files))
	}
	if len(resp.Files) > 0 {
		f := resp.Files[0]
		if f.NewPath != "test.txt" {
			t.Errorf("Files[0].NewPath = %q, want %q", f.NewPath, "test.txt")
		}
		if f.Status != "added" {
			t.Errorf("Files[0].Status = %q, want %q", f.Status, "added")
		}
		if f.LinesAdded != 3 {
			t.Errorf("Files[0].LinesAdded = %d, want 3", f.LinesAdded)
		}
	}
}

func TestGetCommitDetail_ModifiedFile(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test Author",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test Author",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
		return strings.TrimSpace(string(output))
	}

	// Create initial commit
	filePath := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(filePath, []byte("line 1\nline 2\n"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	run("add", "test.txt")
	run("commit", "-m", "Initial commit")

	// Modify and commit
	if err := os.WriteFile(filePath, []byte("line 1\nline 2 modified\nline 3 added\n"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	run("add", "test.txt")
	run("commit", "-m", "Modify file")

	hash := run("rev-parse", "HEAD")
	parentHash := run("rev-parse", "HEAD^")

	st := state.New(filepath.Join(t.TempDir(), "state.json"))
	st.AddWorkspace(state.Workspace{
		ID:   "ws-test",
		Path: dir,
		Repo: "test-repo",
	})

	m := &Manager{state: st}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := m.GetCommitDetail(ctx, "ws-test", hash[:7])
	if err != nil {
		t.Fatalf("GetCommitDetail failed: %v", err)
	}

	// Should have one parent
	if len(resp.Parents) != 1 {
		t.Errorf("len(Parents) = %d, want 1", len(resp.Parents))
	}
	if len(resp.Parents) > 0 && resp.Parents[0] != parentHash {
		t.Errorf("Parents[0] = %q, want %q", resp.Parents[0], parentHash)
	}

	// Should have one modified file
	if len(resp.Files) != 1 {
		t.Errorf("len(Files) = %d, want 1", len(resp.Files))
	}
	if len(resp.Files) > 0 {
		f := resp.Files[0]
		if f.Status != "modified" {
			t.Errorf("Files[0].Status = %q, want %q", f.Status, "modified")
		}
		if f.OldContent == "" {
			t.Error("Files[0].OldContent is empty, want old content")
		}
		if f.NewContent == "" {
			t.Error("Files[0].NewContent is empty, want new content")
		}
	}
}

func TestGetCommitDetail_DeletedFile(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test Author",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test Author",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
		return strings.TrimSpace(string(output))
	}

	// Create initial commit
	filePath := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(filePath, []byte("content\n"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	run("add", "test.txt")
	run("commit", "-m", "Initial commit")

	// Delete and commit
	os.Remove(filePath)
	run("add", "test.txt")
	run("commit", "-m", "Delete file")

	hash := run("rev-parse", "HEAD")

	st := state.New(filepath.Join(t.TempDir(), "state.json"))
	st.AddWorkspace(state.Workspace{
		ID:   "ws-test",
		Path: dir,
		Repo: "test-repo",
	})

	m := &Manager{state: st}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := m.GetCommitDetail(ctx, "ws-test", hash[:7])
	if err != nil {
		t.Fatalf("GetCommitDetail failed: %v", err)
	}

	if len(resp.Files) != 1 {
		t.Errorf("len(Files) = %d, want 1", len(resp.Files))
	}
	if len(resp.Files) > 0 {
		f := resp.Files[0]
		if f.Status != "deleted" {
			t.Errorf("Files[0].Status = %q, want %q", f.Status, "deleted")
		}
		if f.OldContent == "" {
			t.Error("Files[0].OldContent is empty, want old content")
		}
	}
}

func TestGetCommitDetail_RenamedFile(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test Author",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test Author",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
		return strings.TrimSpace(string(output))
	}

	// Create initial commit
	filePath := filepath.Join(dir, "old.txt")
	if err := os.WriteFile(filePath, []byte("content\n"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	run("add", "old.txt")
	run("commit", "-m", "Initial commit")

	// Rename and commit
	newPath := filepath.Join(dir, "new.txt")
	os.Rename(filePath, newPath)
	run("add", "-A")
	run("commit", "-m", "Rename file")

	hash := run("rev-parse", "HEAD")

	st := state.New(filepath.Join(t.TempDir(), "state.json"))
	st.AddWorkspace(state.Workspace{
		ID:   "ws-test",
		Path: dir,
		Repo: "test-repo",
	})

	m := &Manager{state: st}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := m.GetCommitDetail(ctx, "ws-test", hash[:7])
	if err != nil {
		t.Fatalf("GetCommitDetail failed: %v", err)
	}

	if len(resp.Files) != 1 {
		t.Errorf("len(Files) = %d, want 1", len(resp.Files))
	}
	if len(resp.Files) > 0 {
		f := resp.Files[0]
		if f.Status != "renamed" {
			t.Errorf("Files[0].Status = %q, want %q", f.Status, "renamed")
		}
		if f.OldPath != "old.txt" {
			t.Errorf("Files[0].OldPath = %q, want %q", f.OldPath, "old.txt")
		}
		if f.NewPath != "new.txt" {
			t.Errorf("Files[0].NewPath = %q, want %q", f.NewPath, "new.txt")
		}
	}
}

func TestGetCommitDetail_BinaryFile(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test Author",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test Author",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
		return strings.TrimSpace(string(output))
	}

	// Create a binary file (contains null bytes)
	binaryPath := filepath.Join(dir, "image.png")
	binaryContent := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00}
	if err := os.WriteFile(binaryPath, binaryContent, 0644); err != nil {
		t.Fatalf("failed to write binary file: %v", err)
	}
	run("add", "image.png")
	run("commit", "-m", "Add binary file")

	hash := run("rev-parse", "HEAD")

	st := state.New(filepath.Join(t.TempDir(), "state.json"))
	st.AddWorkspace(state.Workspace{
		ID:   "ws-test",
		Path: dir,
		Repo: "test-repo",
	})

	m := &Manager{state: st}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := m.GetCommitDetail(ctx, "ws-test", hash[:7])
	if err != nil {
		t.Fatalf("GetCommitDetail failed: %v", err)
	}

	if len(resp.Files) != 1 {
		t.Errorf("len(Files) = %d, want 1", len(resp.Files))
	}
	if len(resp.Files) > 0 {
		f := resp.Files[0]
		if !f.IsBinary {
			t.Error("Files[0].IsBinary = false, want true")
		}
		if f.NewContent != "" {
			t.Error("Files[0].NewContent should be empty for binary files")
		}
	}
}

func TestGetCommitDetail_InvalidHash(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	st := state.New(filepath.Join(t.TempDir(), "state.json"))
	st.AddWorkspace(state.Workspace{
		ID:   "ws-test",
		Path: dir,
		Repo: "test-repo",
	})

	m := &Manager{state: st}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test various invalid hashes
	invalidHashes := []string{
		"",
		"abc",                    // too short
		"xyz1234",                // non-hex
		"abc;rm -rf /",           // injection attempt
		"abc`whoami`",            // backtick injection
		"abc$(id)",               // command substitution
		"../../../../etc/passwd", // path traversal
	}

	for _, hash := range invalidHashes {
		_, err := m.GetCommitDetail(ctx, "ws-test", hash)
		if err == nil {
			t.Errorf("GetCommitDetail(%q) should have failed", hash)
		}
	}
}

func TestGetCommitDetail_NonexistentHash(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	// Create a commit so the repo is valid
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test Author",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test Author",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
	}

	filePath := filepath.Join(dir, "test.txt")
	os.WriteFile(filePath, []byte("content"), 0644)
	run("add", "test.txt")
	run("commit", "-m", "Initial")

	st := state.New(filepath.Join(t.TempDir(), "state.json"))
	st.AddWorkspace(state.Workspace{
		ID:   "ws-test",
		Path: dir,
		Repo: "test-repo",
	})

	m := &Manager{state: st}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Valid format but doesn't exist
	_, err := m.GetCommitDetail(ctx, "ws-test", "abcd1234")
	if err == nil {
		t.Error("GetCommitDetail should have failed for nonexistent hash")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestGetCommitDetail_MergeCommit(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test Author",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test Author",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
		return strings.TrimSpace(string(output))
	}

	// Create initial commit on main
	file1 := filepath.Join(dir, "main.txt")
	os.WriteFile(file1, []byte("main content\n"), 0644)
	run("add", "main.txt")
	run("commit", "-m", "Initial on main")

	// Get the initial branch name (could be main or master depending on git version)
	mainBranch := run("rev-parse", "--abbrev-ref", "HEAD")

	// Create feature branch
	run("checkout", "-b", "feature")
	file2 := filepath.Join(dir, "feature.txt")
	os.WriteFile(file2, []byte("feature content\n"), 0644)
	run("add", "feature.txt")
	run("commit", "-m", "Feature commit")

	// Back to main branch and make another commit
	run("checkout", mainBranch)
	file3 := filepath.Join(dir, "main2.txt")
	os.WriteFile(file3, []byte("main2 content\n"), 0644)
	run("add", "main2.txt")
	run("commit", "-m", "Second main commit")

	// Merge feature
	run("merge", "feature", "-m", "Merge feature branch")

	hash := run("rev-parse", "HEAD")

	st := state.New(filepath.Join(t.TempDir(), "state.json"))
	st.AddWorkspace(state.Workspace{
		ID:   "ws-test",
		Path: dir,
		Repo: "test-repo",
	})

	m := &Manager{state: st}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := m.GetCommitDetail(ctx, "ws-test", hash[:7])
	if err != nil {
		t.Fatalf("GetCommitDetail failed: %v", err)
	}

	if !resp.IsMerge {
		t.Error("IsMerge = false, want true")
	}
	if len(resp.Parents) != 2 {
		t.Errorf("len(Parents) = %d, want 2 for merge commit", len(resp.Parents))
	}
}

func TestGetCommitDetail_ISO8601Timestamp(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test Author",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test Author",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
		return strings.TrimSpace(string(output))
	}

	filePath := filepath.Join(dir, "test.txt")
	os.WriteFile(filePath, []byte("content"), 0644)
	run("add", "test.txt")
	run("commit", "-m", "Test commit")

	hash := run("rev-parse", "HEAD")

	st := state.New(filepath.Join(t.TempDir(), "state.json"))
	st.AddWorkspace(state.Workspace{
		ID:   "ws-test",
		Path: dir,
		Repo: "test-repo",
	})

	m := &Manager{state: st}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := m.GetCommitDetail(ctx, "ws-test", hash[:7])
	if err != nil {
		t.Fatalf("GetCommitDetail failed: %v", err)
	}

	// Verify timestamp is ISO 8601 format (e.g., 2026-02-15T10:30:00-08:00)
	_, parseErr := time.Parse(time.RFC3339, resp.Timestamp)
	if parseErr != nil {
		t.Errorf("Timestamp %q is not valid ISO 8601: %v", resp.Timestamp, parseErr)
	}
}

func TestGetCommitDetail_LineCounts(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test Author",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test Author",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
		return strings.TrimSpace(string(output))
	}

	// Create initial file
	filePath := filepath.Join(dir, "test.txt")
	os.WriteFile(filePath, []byte("line 1\nline 2\nline 3\nline 4\nline 5\n"), 0644)
	run("add", "test.txt")
	run("commit", "-m", "Initial")

	// Modify: remove 2 lines, add 3 lines
	os.WriteFile(filePath, []byte("line 1\nline 3\nnew line A\nnew line B\nnew line C\n"), 0644)
	run("add", "test.txt")
	run("commit", "-m", "Modify file")

	hash := run("rev-parse", "HEAD")

	st := state.New(filepath.Join(t.TempDir(), "state.json"))
	st.AddWorkspace(state.Workspace{
		ID:   "ws-test",
		Path: dir,
		Repo: "test-repo",
	})

	m := &Manager{state: st}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := m.GetCommitDetail(ctx, "ws-test", hash[:7])
	if err != nil {
		t.Fatalf("GetCommitDetail failed: %v", err)
	}

	if len(resp.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1", len(resp.Files))
	}

	f := resp.Files[0]
	// We removed "line 2", "line 4", "line 5" (3 lines) and added "new line A/B/C" (3 lines)
	// But also "line 3" stayed, and we went from 5 lines to 5 lines
	// Actually: original had lines 1,2,3,4,5; new has 1,3,A,B,C
	// So we removed 2,4,5 (3 lines) and added A,B,C (3 lines)
	if f.LinesAdded != 3 {
		t.Errorf("LinesAdded = %d, want 3", f.LinesAdded)
	}
	if f.LinesRemoved != 3 {
		t.Errorf("LinesRemoved = %d, want 3", f.LinesRemoved)
	}
}
