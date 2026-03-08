package repofeed

import (
	"os/exec"
	"testing"
)

// initBareRepo creates a bare git repo for testing.
func initBareRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "--bare", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	return dir
}

func TestWriteAndReadDevFile(t *testing.T) {
	bareDir := initBareRepo(t)

	devFile := &DeveloperFile{
		Developer:   "alice@example.com",
		DisplayName: "Alice",
		Updated:     "2026-03-07T14:00:00Z",
		Repos: map[string]*RepoActivities{
			"schmux": {Activities: []Activity{
				{ID: "abc", Intent: "test intent", Status: StatusActive, Started: "2026-03-07T13:00:00Z"},
			}},
		},
	}

	g := &GitOps{BareDir: bareDir, Branch: "dev-repofeed"}

	// Write
	if err := g.WriteDevFile("alice@example.com", devFile); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read back
	files, err := g.ReadAllDevFiles()
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	if files[0].Developer != "alice@example.com" {
		t.Errorf("developer: got %q, want %q", files[0].Developer, "alice@example.com")
	}
}

func TestWriteMultipleDevFiles(t *testing.T) {
	bareDir := initBareRepo(t)
	g := &GitOps{BareDir: bareDir, Branch: "dev-repofeed"}

	// Write alice
	alice := &DeveloperFile{Developer: "alice@example.com", DisplayName: "Alice", Updated: "2026-03-07T14:00:00Z", Repos: map[string]*RepoActivities{}}
	if err := g.WriteDevFile("alice@example.com", alice); err != nil {
		t.Fatalf("write alice: %v", err)
	}

	// Write bob (should not overwrite alice)
	bob := &DeveloperFile{Developer: "bob@example.com", DisplayName: "Bob", Updated: "2026-03-07T15:00:00Z", Repos: map[string]*RepoActivities{}}
	if err := g.WriteDevFile("bob@example.com", bob); err != nil {
		t.Fatalf("write bob: %v", err)
	}

	files, err := g.ReadAllDevFiles()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
}

func TestReadAllDevFiles_NoBranch(t *testing.T) {
	bareDir := initBareRepo(t)
	g := &GitOps{BareDir: bareDir, Branch: "dev-repofeed"}

	files, err := g.ReadAllDevFiles()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("got %d files, want 0 for nonexistent branch", len(files))
	}
}
