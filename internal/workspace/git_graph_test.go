package workspace

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// setupGitGraphTest creates a "remote" repo, clones it as bare (simulating the origin query repo),
// and returns a Manager configured to use it. The remote repo has an initial commit on main.
func setupGitGraphTest(t *testing.T, repoName string) (mgr *Manager, remoteDir string, bareDir string) {
	t.Helper()

	// Create "remote" repo
	remoteDir = t.TempDir()
	runGit(t, remoteDir, "init", "-b", "main")
	runGit(t, remoteDir, "config", "user.email", "test@test.com")
	runGit(t, remoteDir, "config", "user.name", "Test User")
	writeFile(t, remoteDir, "README.md", "initial")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "initial commit")

	// Clone as bare repo — directory must match extractRepoName(remoteDir)+".git"
	// since GetGitGraph uses extractRepoName on the repo URL to find the bare clone.
	bareBase := t.TempDir()
	derivedName := extractRepoName(remoteDir)
	bareDir = filepath.Join(bareBase, derivedName+".git")
	runGit(t, bareBase, "clone", "--bare", remoteDir, bareDir)

	// Configure fetch refspec for origin/ refs
	cmd := exec.Command("git", "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	cmd.Dir = bareDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config: %v\n%s", err, output)
	}

	// Fetch to populate origin/ refs
	runGit(t, bareDir, "fetch", "origin")

	// Create config and state
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.Repos = []config.Repo{{Name: repoName, URL: remoteDir}}
	// Override bare repos path
	cfg.BareReposPathOverride = bareBase

	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath)

	mgr = New(cfg, st, statePath)
	return mgr, remoteDir, bareDir
}

// addCommitOnBranch creates a branch from current HEAD in the remote, adds a commit, and pushes it.
func addCommitOnBranch(t *testing.T, remoteDir, bareDir, branch, msg string) {
	t.Helper()
	runGit(t, remoteDir, "checkout", "-B", branch)
	writeFile(t, remoteDir, branch+".txt", msg)
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", msg)
	runGit(t, remoteDir, "checkout", "main")
	// Fetch into bare repo
	runGit(t, bareDir, "fetch", "origin")
}

// addCommitOnMain adds a commit to main in the remote and fetches into bare.
func addCommitOnMain(t *testing.T, remoteDir, bareDir, msg string) {
	t.Helper()
	runGit(t, remoteDir, "checkout", "main")
	writeFile(t, remoteDir, msg+".txt", msg)
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", msg)
	runGit(t, bareDir, "fetch", "origin")
}

// getCommitHash returns the hash of a ref in the remote repo.
func getCommitHash(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse %s: %v", ref, err)
	}
	return trimOutput(output)
}

func trimOutput(b []byte) string {
	s := string(b)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func TestGitGraph_SingleBranch(t *testing.T) {
	mgr, remoteDir, bareDir := setupGitGraphTest(t, "testrepo")
	ctx := context.Background()

	// Create a branch with 2 commits
	runGit(t, remoteDir, "checkout", "-b", "feature-a")
	writeFile(t, remoteDir, "a1.txt", "a1")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "feature-a commit 1")
	writeFile(t, remoteDir, "a2.txt", "a2")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "feature-a commit 2")
	runGit(t, remoteDir, "checkout", "main")
	runGit(t, bareDir, "fetch", "origin")

	// Add workspace for the branch
	mgr.state.AddWorkspace(state.Workspace{ID: "ws-1", Repo: remoteDir, Branch: "feature-a"})

	resp, err := mgr.GetGitGraph(ctx, "testrepo", 200, nil)
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	if resp.Repo != remoteDir {
		t.Errorf("expected repo %s, got %s", remoteDir, resp.Repo)
	}

	if len(resp.Nodes) < 3 {
		t.Fatalf("expected at least 3 nodes (2 branch + 1 initial), got %d", len(resp.Nodes))
	}

	// Check branches map
	if _, ok := resp.Branches["main"]; !ok {
		t.Error("expected main in branches map")
	}
	if _, ok := resp.Branches["feature-a"]; !ok {
		t.Error("expected feature-a in branches map")
	}
	if !resp.Branches["main"].IsMain {
		t.Error("expected main.is_main to be true")
	}

	// Head node should have is_head and workspace_ids
	headHash := resp.Branches["feature-a"].Head
	for _, node := range resp.Nodes {
		if node.Hash == headHash {
			if len(node.IsHead) == 0 {
				t.Error("expected HEAD node to have is_head")
			}
			if len(node.WorkspaceIDs) == 0 {
				t.Error("expected HEAD node to have workspace_ids")
			}
			break
		}
	}
}

func TestGitGraph_MultipleBranches(t *testing.T) {
	mgr, remoteDir, bareDir := setupGitGraphTest(t, "testrepo")
	ctx := context.Background()

	// Branch A from main
	addCommitOnBranch(t, remoteDir, bareDir, "feature-a", "commit on a")

	// Add another commit to main
	addCommitOnMain(t, remoteDir, bareDir, "main-second")

	// Branch B from latest main
	addCommitOnBranch(t, remoteDir, bareDir, "feature-b", "commit on b")

	mgr.state.AddWorkspace(state.Workspace{ID: "ws-a", Repo: remoteDir, Branch: "feature-a"})
	mgr.state.AddWorkspace(state.Workspace{ID: "ws-b", Repo: remoteDir, Branch: "feature-b"})

	resp, err := mgr.GetGitGraph(ctx, "testrepo", 200, nil)
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	if len(resp.Branches) != 3 {
		t.Fatalf("expected 3 branches (main, feature-a, feature-b), got %d", len(resp.Branches))
	}

	// Both branch heads should be in the nodes
	foundA, foundB := false, false
	for _, node := range resp.Nodes {
		if node.Hash == resp.Branches["feature-a"].Head {
			foundA = true
		}
		if node.Hash == resp.Branches["feature-b"].Head {
			foundB = true
		}
	}
	if !foundA {
		t.Error("feature-a HEAD not found in nodes")
	}
	if !foundB {
		t.Error("feature-b HEAD not found in nodes")
	}
}

func TestGitGraph_MergeCommit(t *testing.T) {
	mgr, remoteDir, bareDir := setupGitGraphTest(t, "testrepo")
	ctx := context.Background()

	// Create branch, commit, then merge into main
	runGit(t, remoteDir, "checkout", "-b", "feature-merge")
	writeFile(t, remoteDir, "merge.txt", "merge content")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "merge branch commit")
	runGit(t, remoteDir, "checkout", "main")
	runGit(t, remoteDir, "merge", "--no-ff", "feature-merge", "-m", "Merge feature-merge")
	runGit(t, bareDir, "fetch", "origin")

	resp, err := mgr.GetGitGraph(ctx, "testrepo", 200, nil)
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	// Find the merge commit — it should have 2 parents
	found := false
	for _, node := range resp.Nodes {
		if len(node.Parents) == 2 {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a merge commit with 2 parents")
	}
}

func TestGitGraph_ForkPointDetection(t *testing.T) {
	mgr, remoteDir, bareDir := setupGitGraphTest(t, "testrepo")
	ctx := context.Background()

	// Add commits to main after initial
	addCommitOnMain(t, remoteDir, bareDir, "main-2")

	// Fork a branch from here
	forkHash := getCommitHash(t, remoteDir, "main")
	addCommitOnBranch(t, remoteDir, bareDir, "feature-fork", "branch after fork")

	// More commits on main
	addCommitOnMain(t, remoteDir, bareDir, "main-3")

	mgr.state.AddWorkspace(state.Workspace{ID: "ws-fork", Repo: remoteDir, Branch: "feature-fork"})

	resp, err := mgr.GetGitGraph(ctx, "testrepo", 200, nil)
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	// Fork point commit should be in the graph
	found := false
	for _, node := range resp.Nodes {
		if node.Hash == forkHash {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("fork point %s not found in graph nodes", forkHash[:8])
	}
}

func TestGitGraph_MaxCommits(t *testing.T) {
	mgr, remoteDir, bareDir := setupGitGraphTest(t, "testrepo")
	ctx := context.Background()

	// Add many commits to main
	for i := 0; i < 20; i++ {
		addCommitOnMain(t, remoteDir, bareDir, "bulk-"+string(rune('a'+i)))
	}

	resp, err := mgr.GetGitGraph(ctx, "testrepo", 5, nil)
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	if len(resp.Nodes) > 5 {
		t.Errorf("expected at most 5 nodes, got %d", len(resp.Nodes))
	}
}

func TestGitGraph_BranchFilter(t *testing.T) {
	mgr, remoteDir, bareDir := setupGitGraphTest(t, "testrepo")
	ctx := context.Background()

	addCommitOnBranch(t, remoteDir, bareDir, "feature-x", "x commit")
	addCommitOnBranch(t, remoteDir, bareDir, "feature-y", "y commit")

	mgr.state.AddWorkspace(state.Workspace{ID: "ws-x", Repo: remoteDir, Branch: "feature-x"})
	mgr.state.AddWorkspace(state.Workspace{ID: "ws-y", Repo: remoteDir, Branch: "feature-y"})

	// Filter to only feature-x
	resp, err := mgr.GetGitGraph(ctx, "testrepo", 200, []string{"feature-x"})
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	if _, ok := resp.Branches["feature-y"]; ok {
		t.Error("feature-y should not be in branches when filtered to feature-x")
	}
	if _, ok := resp.Branches["feature-x"]; !ok {
		t.Error("feature-x should be in branches")
	}
	if _, ok := resp.Branches["main"]; !ok {
		t.Error("main should always be included")
	}
}

func TestGitGraph_WorkspaceAnnotation(t *testing.T) {
	mgr, remoteDir, bareDir := setupGitGraphTest(t, "testrepo")
	ctx := context.Background()

	addCommitOnBranch(t, remoteDir, bareDir, "feature-ann", "annotated commit")

	mgr.state.AddWorkspace(state.Workspace{ID: "ws-ann-1", Repo: remoteDir, Branch: "feature-ann"})
	mgr.state.AddWorkspace(state.Workspace{ID: "ws-ann-2", Repo: remoteDir, Branch: "feature-ann"})

	resp, err := mgr.GetGitGraph(ctx, "testrepo", 200, nil)
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	// Branch should have 2 workspace IDs
	branch, ok := resp.Branches["feature-ann"]
	if !ok {
		t.Fatal("expected feature-ann in branches")
	}
	if len(branch.WorkspaceIDs) != 2 {
		t.Errorf("expected 2 workspace_ids on branch, got %d", len(branch.WorkspaceIDs))
	}

	// HEAD node should have workspace_ids, non-HEAD nodes should not
	headHash := branch.Head
	for _, node := range resp.Nodes {
		if node.Hash == headHash {
			if len(node.WorkspaceIDs) != 2 {
				t.Errorf("expected 2 workspace_ids on HEAD node, got %d", len(node.WorkspaceIDs))
			}
		} else {
			if len(node.WorkspaceIDs) != 0 {
				t.Errorf("non-HEAD node %s should have 0 workspace_ids, got %d", node.ShortHash, len(node.WorkspaceIDs))
			}
		}
	}
}

func TestGitGraph_UnknownBranchIgnored(t *testing.T) {
	mgr, _, _ := setupGitGraphTest(t, "testrepo")
	ctx := context.Background()

	// Filter to a branch that doesn't exist — should not error
	resp, err := mgr.GetGitGraph(ctx, "testrepo", 200, []string{"nonexistent-branch"})
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	// Should still have main
	if _, ok := resp.Branches["main"]; !ok {
		t.Error("expected main in branches even when filter has unknown branch")
	}
}

func TestGitGraph_UnknownRepo(t *testing.T) {
	mgr, _, _ := setupGitGraphTest(t, "testrepo")
	ctx := context.Background()

	_, err := mgr.GetGitGraph(ctx, "no-such-repo", 200, nil)
	if err == nil {
		t.Fatal("expected error for unknown repo")
	}
}

func TestGitGraph_Trimming(t *testing.T) {
	mgr, remoteDir, bareDir := setupGitGraphTest(t, "testrepo")
	ctx := context.Background()

	// Add 15 commits to main
	for i := 0; i < 15; i++ {
		addCommitOnMain(t, remoteDir, bareDir, "main-"+string(rune('a'+i)))
	}

	// Fork a branch from latest main
	addCommitOnBranch(t, remoteDir, bareDir, "feature-trim", "trim branch commit")

	mgr.state.AddWorkspace(state.Workspace{ID: "ws-trim", Repo: remoteDir, Branch: "feature-trim"})

	resp, err := mgr.GetGitGraph(ctx, "testrepo", 200, nil)
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	// Should include the branch commit + fork point + some main context
	// but not necessarily all 16 main commits
	if len(resp.Nodes) == 0 {
		t.Fatal("expected non-empty graph")
	}

	// Branch HEAD should be present
	if _, ok := resp.Branches["feature-trim"]; !ok {
		t.Error("expected feature-trim in branches")
	}
}

func TestGitGraph_MultipleMergeBases(t *testing.T) {
	mgr, remoteDir, bareDir := setupGitGraphTest(t, "testrepo")
	ctx := context.Background()

	// Create feature branch
	runGit(t, remoteDir, "checkout", "-b", "feature-multi")
	writeFile(t, remoteDir, "multi1.txt", "content1")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "multi commit 1")
	runGit(t, remoteDir, "checkout", "main")

	// Advance main
	addCommitOnMain(t, remoteDir, bareDir, "main-advance")

	// Merge main into feature (creates a second merge base later)
	runGit(t, remoteDir, "checkout", "feature-multi")
	runGit(t, remoteDir, "merge", "main", "-m", "sync main into feature")

	// More work on feature
	writeFile(t, remoteDir, "multi2.txt", "content2")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "multi commit 2")
	runGit(t, remoteDir, "checkout", "main")
	runGit(t, bareDir, "fetch", "origin")

	mgr.state.AddWorkspace(state.Workspace{ID: "ws-multi", Repo: remoteDir, Branch: "feature-multi"})

	resp, err := mgr.GetGitGraph(ctx, "testrepo", 200, nil)
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	// Should include nodes from both branches without error
	if len(resp.Nodes) < 3 {
		t.Errorf("expected at least 3 nodes, got %d", len(resp.Nodes))
	}
	if _, ok := resp.Branches["feature-multi"]; !ok {
		t.Error("expected feature-multi in branches")
	}
}
