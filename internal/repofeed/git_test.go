//go:build !norepofeed

package repofeed

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initBareRepo creates a bare git repo for testing.
func initBareRepo(t *testing.T) string {
	t.Helper()
	// Set git identity for CI runners that have no global config.
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@test.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@test.com")
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "--bare", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	return dir
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

func TestWriteDevFile_CreatesOrphanBranch(t *testing.T) {
	bareDir := initBareRepo(t)
	g := &GitOps{BareDir: bareDir, Branch: "dev-repofeed"}

	devFile := &DeveloperFile{
		Developer:   "alice@example.com",
		DisplayName: "Alice",
		Updated:     "2026-04-13T10:00:00Z",
		Repos: map[string]*RepoActivities{
			"myrepo": {Activities: []Activity{{
				ID:           "abc123",
				Intent:       "fix login bug",
				Status:       StatusActive,
				Started:      "2026-04-13T09:00:00Z",
				Branches:     []string{"fix/login"},
				SessionCount: 1,
				Agents:       []string{},
			}}},
		},
	}

	if err := g.WriteDevFile("alice@example.com", devFile); err != nil {
		t.Fatalf("write: %v", err)
	}

	files, err := g.ReadAllDevFiles()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	if files[0].Developer != "alice@example.com" {
		t.Errorf("developer = %q, want alice@example.com", files[0].Developer)
	}
	if files[0].DisplayName != "Alice" {
		t.Errorf("display_name = %q, want Alice", files[0].DisplayName)
	}
	repo, ok := files[0].Repos["myrepo"]
	if !ok {
		t.Fatal("missing repo 'myrepo'")
	}
	if len(repo.Activities) != 1 {
		t.Fatalf("got %d activities, want 1", len(repo.Activities))
	}
	if repo.Activities[0].Intent != "fix login bug" {
		t.Errorf("intent = %q, want 'fix login bug'", repo.Activities[0].Intent)
	}
}

func TestWriteDevFile_UpdatesExistingFile(t *testing.T) {
	bareDir := initBareRepo(t)
	g := &GitOps{BareDir: bareDir, Branch: "dev-repofeed"}

	v1 := &DeveloperFile{
		Developer: "bob@example.com",
		Updated:   "2026-04-13T10:00:00Z",
		Repos: map[string]*RepoActivities{
			"repo1": {Activities: []Activity{{ID: "a1", Intent: "first task", Status: StatusActive}}},
		},
	}
	if err := g.WriteDevFile("bob@example.com", v1); err != nil {
		t.Fatalf("write v1: %v", err)
	}

	v2 := &DeveloperFile{
		Developer: "bob@example.com",
		Updated:   "2026-04-13T11:00:00Z",
		Repos: map[string]*RepoActivities{
			"repo1": {Activities: []Activity{{ID: "a2", Intent: "second task", Status: StatusActive}}},
		},
	}
	if err := g.WriteDevFile("bob@example.com", v2); err != nil {
		t.Fatalf("write v2: %v", err)
	}

	files, err := g.ReadAllDevFiles()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1 (same email overwrites)", len(files))
	}
	if files[0].Updated != "2026-04-13T11:00:00Z" {
		t.Errorf("updated = %q, want v2 timestamp", files[0].Updated)
	}
	if files[0].Repos["repo1"].Activities[0].Intent != "second task" {
		t.Errorf("intent = %q, want 'second task'", files[0].Repos["repo1"].Activities[0].Intent)
	}
}

func TestWriteDevFile_MultipleDevFiles(t *testing.T) {
	bareDir := initBareRepo(t)
	g := &GitOps{BareDir: bareDir, Branch: "dev-repofeed"}

	alice := &DeveloperFile{
		Developer: "alice@example.com",
		Updated:   "2026-04-13T10:00:00Z",
		Repos:     map[string]*RepoActivities{},
	}
	bob := &DeveloperFile{
		Developer: "bob@example.com",
		Updated:   "2026-04-13T10:00:00Z",
		Repos:     map[string]*RepoActivities{},
	}

	if err := g.WriteDevFile("alice@example.com", alice); err != nil {
		t.Fatalf("write alice: %v", err)
	}
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

	devs := map[string]bool{}
	for _, f := range files {
		devs[f.Developer] = true
	}
	if !devs["alice@example.com"] || !devs["bob@example.com"] {
		t.Errorf("expected both alice and bob, got %v", devs)
	}
}

func TestPushToRemote(t *testing.T) {
	// Create "origin" bare repo
	originDir := initBareRepo(t)

	// Create "local" bare repo with origin as remote
	localDir := initBareRepo(t)
	cmd := exec.Command("git", "remote", "add", "origin", originDir)
	cmd.Dir = localDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("add remote: %v\n%s", err, out)
	}

	// Write a dev file locally
	local := &GitOps{BareDir: localDir, Branch: "dev-repofeed"}
	devFile := &DeveloperFile{
		Developer: "carol@example.com",
		Updated:   "2026-04-13T10:00:00Z",
		Repos: map[string]*RepoActivities{
			"myrepo": {Activities: []Activity{{ID: "c1", Intent: "add feature", Status: StatusActive}}},
		},
	}
	if err := local.WriteDevFile("carol@example.com", devFile); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Push to origin
	if err := local.PushToRemote("origin"); err != nil {
		t.Fatalf("push: %v", err)
	}

	// Read from origin directly
	origin := &GitOps{BareDir: originDir, Branch: "dev-repofeed"}
	files, err := origin.ReadAllDevFiles()
	if err != nil {
		t.Fatalf("read origin: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("origin got %d files, want 1", len(files))
	}
	if files[0].Developer != "carol@example.com" {
		t.Errorf("developer = %q, want carol@example.com", files[0].Developer)
	}
}

func TestCleanupStaleIndexFiles(t *testing.T) {
	dir := t.TempDir()

	// Create some stale index files
	for _, name := range []string{"repofeed-idx-001", "repofeed-idx-002"} {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	// Create a non-matching file that should NOT be removed
	keep, err := os.Create(filepath.Join(dir, "keep-me.txt"))
	if err != nil {
		t.Fatal(err)
	}
	keep.Close()

	CleanupStaleIndexFiles(dir)

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file remaining, got %d", len(entries))
	}
	if entries[0].Name() != "keep-me.txt" {
		t.Errorf("expected keep-me.txt to remain, got %s", entries[0].Name())
	}
}
