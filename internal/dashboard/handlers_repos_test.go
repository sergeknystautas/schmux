package dashboard

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanLocalRepos_DiscoversBothVCSTypes(t *testing.T) {
	home := t.TempDir()

	// Create a git repo
	gitRepo := filepath.Join(home, "repo-a")
	if err := os.MkdirAll(filepath.Join(gitRepo, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create a sapling repo
	slRepo := filepath.Join(home, "repo-b")
	if err := os.MkdirAll(filepath.Join(slRepo, ".sl"), 0755); err != nil {
		t.Fatal(err)
	}

	repos := scanLocalRepos(context.Background(), home)

	found := map[string]LocalRepo{}
	for _, r := range repos {
		found[r.Name] = r
	}

	if len(found) != 2 {
		t.Fatalf("expected 2 repos, got %d: %+v", len(found), repos)
	}

	gitR, ok := found["repo-a"]
	if !ok {
		t.Fatal("repo-a not found")
	}
	if gitR.VCS != "git" {
		t.Errorf("expected VCS=git, got %q", gitR.VCS)
	}
	if gitR.Path != gitRepo {
		t.Errorf("expected path=%q, got %q", gitRepo, gitR.Path)
	}

	slR, ok := found["repo-b"]
	if !ok {
		t.Fatal("repo-b not found")
	}
	if slR.VCS != "sapling" {
		t.Errorf("expected VCS=sapling, got %q", slR.VCS)
	}
}

func TestScanLocalRepos_Depth2(t *testing.T) {
	home := t.TempDir()

	// Depth 2: home/code/myproject/.git — should be found
	depth2 := filepath.Join(home, "code", "myproject")
	if err := os.MkdirAll(filepath.Join(depth2, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	repos := scanLocalRepos(context.Background(), home)
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo at depth 2, got %d: %+v", len(repos), repos)
	}
	if repos[0].Name != "myproject" {
		t.Errorf("expected name=myproject, got %q", repos[0].Name)
	}
}

func TestScanLocalRepos_DepthBeyond2NotTraversed(t *testing.T) {
	home := t.TempDir()

	// Depth 3: home/deep/nested/repo/.git — should NOT be found
	deep := filepath.Join(home, "deep", "nested", "repo")
	if err := os.MkdirAll(filepath.Join(deep, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	repos := scanLocalRepos(context.Background(), home)
	if len(repos) != 0 {
		t.Fatalf("expected 0 repos (depth > 2), got %d: %+v", len(repos), repos)
	}
}

func TestScanLocalRepos_SkipsHiddenDirs(t *testing.T) {
	home := t.TempDir()

	// Hidden dir at depth 1 containing a repo
	hidden := filepath.Join(home, ".hidden", "repo")
	if err := os.MkdirAll(filepath.Join(hidden, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	repos := scanLocalRepos(context.Background(), home)
	if len(repos) != 0 {
		t.Fatalf("expected 0 repos (hidden dir skipped), got %d: %+v", len(repos), repos)
	}
}

func TestScanLocalRepos_SkipsOSAndBuildDirs(t *testing.T) {
	home := t.TempDir()

	skipNames := []string{"Library", "Applications", "node_modules", "vendor", "target", "build", "dist"}
	for _, name := range skipNames {
		repoDir := filepath.Join(home, name, "repo")
		if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	repos := scanLocalRepos(context.Background(), home)
	if len(repos) != 0 {
		t.Fatalf("expected 0 repos (skip-list dirs), got %d: %+v", len(repos), repos)
	}
}

func TestScanLocalRepos_DoesNotRecurseIntoRepo(t *testing.T) {
	home := t.TempDir()

	// A repo at depth 1 with a nested repo inside — only the outer should be found
	outer := filepath.Join(home, "outer")
	if err := os.MkdirAll(filepath.Join(outer, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(outer, "inner")
	if err := os.MkdirAll(filepath.Join(inner, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	repos := scanLocalRepos(context.Background(), home)
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo (no recurse into repo), got %d: %+v", len(repos), repos)
	}
	if repos[0].Name != "outer" {
		t.Errorf("expected name=outer, got %q", repos[0].Name)
	}
}

func TestScanLocalRepos_ContextCancellation(t *testing.T) {
	home := t.TempDir()

	// Create a bunch of directories so the scan has work to do
	for i := 0; i < 50; i++ {
		dir := filepath.Join(home, "dir"+string(rune('a'+i%26))+string(rune('0'+i/26)))
		if err := os.MkdirAll(filepath.Join(dir, "sub"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	// Put a repo deep enough that it should only be found if scan gets there
	if err := os.MkdirAll(filepath.Join(home, "zzz-last", ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	// Cancel the context immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Give a tiny bit of time — scan should return promptly, not hang
	done := make(chan struct{})
	var repos []LocalRepo
	go func() {
		repos = scanLocalRepos(ctx, home)
		close(done)
	}()

	select {
	case <-done:
		// Success — scan returned. It may or may not have found repos, but it didn't hang.
	case <-time.After(2 * time.Second):
		t.Fatal("scanLocalRepos did not respect context cancellation")
	}
	_ = repos
}

func TestScanLocalRepos_SymlinksAtDepth1Followed(t *testing.T) {
	home := t.TempDir()

	// Create a real repo outside the home dir
	realRepo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(realRepo, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	// Symlink at depth 1 pointing to the real repo's parent
	symTarget := filepath.Dir(realRepo)
	symlink := filepath.Join(home, "linked-code")
	if err := os.Symlink(symTarget, symlink); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	repos := scanLocalRepos(context.Background(), home)

	found := false
	for _, r := range repos {
		if r.Path == filepath.Join(symlink, filepath.Base(realRepo)) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected symlink at depth 1 to be followed, repos found: %+v", repos)
	}
}

func TestScanLocalRepos_SymlinksAtDepth2NotFollowed(t *testing.T) {
	home := t.TempDir()

	// Create a real repo outside the home dir
	realRepo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(realRepo, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	// A normal dir at depth 1, with a symlink at depth 2 pointing to the real repo
	codeDir := filepath.Join(home, "code")
	if err := os.MkdirAll(codeDir, 0755); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(codeDir, "linked-repo")
	if err := os.Symlink(realRepo, symlink); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	repos := scanLocalRepos(context.Background(), home)

	// The symlink at depth 2 should NOT be followed, so we should not find it
	for _, r := range repos {
		if r.Path == symlink || r.Path == realRepo {
			t.Errorf("symlink at depth 2 should not be followed, but found repo: %+v", r)
		}
	}
}
