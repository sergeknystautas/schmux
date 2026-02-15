package workspace

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// setupWorkspaceGraphTest creates a "remote" repo, clones it into a workspace directory,
// and returns a Manager with the workspace registered in state.
// The remote has an initial commit on main. The workspace is checked out on the given branch.
func setupWorkspaceGraphTest(t *testing.T, branch string) (mgr *Manager, remoteDir, wsDir, wsID string) {
	t.Helper()
	wsID = "ws-test-1"

	// Create "remote" repo
	remoteDir = t.TempDir()
	runGit(t, remoteDir, "init", "-b", "main")
	runGit(t, remoteDir, "config", "user.email", "test@test.com")
	runGit(t, remoteDir, "config", "user.name", "Test User")
	writeFile(t, remoteDir, "README.md", "initial")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "initial commit")

	// Clone into workspace dir
	wsDir = filepath.Join(t.TempDir(), "workspace")
	runGit(t, t.TempDir(), "clone", remoteDir, wsDir)
	runGit(t, wsDir, "config", "user.email", "test@test.com")
	runGit(t, wsDir, "config", "user.name", "Test User")

	// Create and checkout branch if not main
	if branch != "main" {
		runGit(t, wsDir, "checkout", "-b", branch)
	}

	// Config + state
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.Repos = []config.Repo{testRepoWithBarePath("testrepo", remoteDir)}

	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath)
	st.AddWorkspace(state.Workspace{ID: wsID, Repo: remoteDir, Branch: branch, Path: wsDir})

	mgr = New(cfg, st, statePath)
	return
}

// commitOnWorkspace adds a commit to the workspace's current branch.
func commitOnWorkspace(t *testing.T, wsDir, filename, msg string) {
	t.Helper()
	writeFile(t, wsDir, filename, msg)
	runGit(t, wsDir, "add", ".")
	runGit(t, wsDir, "commit", "-m", msg)
}

// commitOnRemote adds a commit to main in the remote and fetches into the workspace.
func commitOnRemote(t *testing.T, remoteDir, wsDir, filename, msg string) {
	t.Helper()
	writeFile(t, remoteDir, filename, msg)
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", msg)
	runGit(t, wsDir, "fetch", "origin")
}

func getHash(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse %s: %v", ref, err)
	}
	s := string(out)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func TestGitGraph_SingleBranch(t *testing.T) {
	mgr, _, wsDir, wsID := setupWorkspaceGraphTest(t, "feature-a")
	ctx := context.Background()

	// Add 2 commits on the branch
	commitOnWorkspace(t, wsDir, "a1.txt", "feature-a commit 1")
	commitOnWorkspace(t, wsDir, "a2.txt", "feature-a commit 2")

	resp, err := mgr.GetGitGraph(ctx, wsID, 200, 5)
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	if resp.Repo == "" {
		t.Error("expected non-empty repo")
	}

	// Should have at least 3 nodes: 2 branch commits + initial (fork point)
	if len(resp.Nodes) < 3 {
		t.Fatalf("expected at least 3 nodes, got %d", len(resp.Nodes))
	}

	// Check branches map has both main and feature-a
	if _, ok := resp.Branches["main"]; !ok {
		t.Error("expected main in branches")
	}
	if _, ok := resp.Branches["feature-a"]; !ok {
		t.Error("expected feature-a in branches")
	}
	if !resp.Branches["main"].IsMain {
		t.Error("expected main.is_main to be true")
	}

	// HEAD node should have is_head
	headHash := resp.Branches["feature-a"].Head
	for _, node := range resp.Nodes {
		if node.Hash == headHash {
			if len(node.IsHead) == 0 {
				t.Error("expected HEAD node to have is_head")
			}
			break
		}
	}
}

func TestGitGraph_BranchBehind(t *testing.T) {
	mgr, remoteDir, wsDir, wsID := setupWorkspaceGraphTest(t, "feature-behind")
	ctx := context.Background()

	// Add commits to remote main (workspace is behind)
	commitOnRemote(t, remoteDir, wsDir, "remote1.txt", "remote commit 1")
	commitOnRemote(t, remoteDir, wsDir, "remote2.txt", "remote commit 2")

	resp, err := mgr.GetGitGraph(ctx, wsID, 200, 5)
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	// Should show the origin/main commits that are ahead
	mainBranch := resp.Branches["main"]
	if mainBranch.Head == "" {
		t.Fatal("expected main branch head")
	}

	// Main head should be different from the local branch head (local is behind)
	localBranch := resp.Branches["feature-behind"]
	if mainBranch.Head == localBranch.Head {
		t.Error("expected main and local heads to differ (local is behind)")
	}

	// Should report main-ahead count (2 remote commits)
	if resp.MainAheadCount != 2 {
		t.Errorf("expected MainAheadCount=2, got %d", resp.MainAheadCount)
	}

	// Should have nodes for local branch (fork point + context)
	if len(resp.Nodes) < 1 {
		t.Fatalf("expected at least 1 node (fork point), got %d", len(resp.Nodes))
	}
}

func TestGitGraph_AheadAndBehind(t *testing.T) {
	mgr, remoteDir, wsDir, wsID := setupWorkspaceGraphTest(t, "feature-diverge")
	ctx := context.Background()

	// Add a local commit (ahead)
	commitOnWorkspace(t, wsDir, "local.txt", "local commit")

	// Add a remote commit (behind)
	commitOnRemote(t, remoteDir, wsDir, "remote.txt", "remote commit on main")

	resp, err := mgr.GetGitGraph(ctx, wsID, 200, 5)
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	// Should have both local and remote heads
	localBranch := resp.Branches["feature-diverge"]
	mainBranch := resp.Branches["main"]
	if localBranch.Head == mainBranch.Head {
		t.Error("expected different heads for diverged branches")
	}

	// Should report main-ahead count (1 remote commit)
	if resp.MainAheadCount != 1 {
		t.Errorf("expected MainAheadCount=1, got %d", resp.MainAheadCount)
	}

	// Should have at least 2 nodes: local commit, fork point
	if len(resp.Nodes) < 2 {
		t.Fatalf("expected at least 2 nodes (local + fork point), got %d", len(resp.Nodes))
	}

	// Verify local head is in nodes (main head is NOT in nodes - it's just a count)
	foundLocal := false
	for _, node := range resp.Nodes {
		if node.Hash == localBranch.Head {
			foundLocal = true
		}
	}
	if !foundLocal {
		t.Error("local branch HEAD not found in nodes")
	}
}

// sliceContains checks if a string slice contains a value.
func sliceContains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func TestGitGraph_OrderMatchesISLSort(t *testing.T) {
	mgr, remoteDir, wsDir, wsID := setupWorkspaceGraphTest(t, "feature-order")

	// Create two draft commits on the local branch
	commitOnWorkspace(t, wsDir, "draft1.txt", "draft 1")
	commitOnWorkspace(t, wsDir, "draft2.txt", "draft 2")

	// Create two public commits on main (origin/main) by committing
	// directly to the remote and fetching into the workspace.
	commitOnRemote(t, remoteDir, wsDir, "public1.txt", "public 1")
	commitOnRemote(t, remoteDir, wsDir, "public2.txt", "public 2")

	resp, err := mgr.GetGitGraph(context.Background(), wsID, 200, 5)
	if err != nil {
		t.Fatalf("GetGitGraph failed: %v", err)
	}

	// Main-ahead count should be 2 (the public commits)
	if resp.MainAheadCount != 2 {
		t.Errorf("expected MainAheadCount=2, got %d", resp.MainAheadCount)
	}

	// Nodes should include local commits + context (fork point + ancestors)
	// Not the main-ahead commits
	if len(resp.Nodes) < 2 {
		t.Fatalf("expected at least 2 nodes (local commits), got %d", len(resp.Nodes))
	}

	// ISL invariant: children never appear below their ancestors.
	// Build position map and verify.
	pos := make(map[string]int, len(resp.Nodes))
	for i, n := range resp.Nodes {
		pos[n.Hash] = i
	}
	for i, n := range resp.Nodes {
		for _, parentHash := range n.Parents {
			parentPos, ok := pos[parentHash]
			if !ok {
				continue // parent not in graph (trimmed)
			}
			if i >= parentPos {
				t.Errorf("child %s (pos %d) appears at or below parent %s (pos %d) — violates topo invariant",
					n.ShortHash, i, resp.Nodes[parentPos].ShortHash, parentPos)
			}
		}
	}

	// Verify draft commits (local-only) are present.
	// Public commits (main-ahead) are NOT in nodes - they're just counted.
	var draftCount int
	for _, n := range resp.Nodes {
		onMain := sliceContains(n.Branches, "main")
		onLocal := sliceContains(n.Branches, "feature-order")
		if onLocal && !onMain {
			draftCount++
		}
	}
	if draftCount < 2 {
		t.Errorf("expected at least 2 draft nodes, got %d", draftCount)
	}
}

func TestGitGraph_MergeCommit(t *testing.T) {
	mgr, _, wsDir, wsID := setupWorkspaceGraphTest(t, "main")
	ctx := context.Background()

	// Create a branch, commit, then merge into main
	runGit(t, wsDir, "checkout", "-b", "feature-merge")
	commitOnWorkspace(t, wsDir, "merge.txt", "merge branch commit")
	runGit(t, wsDir, "checkout", "main")
	runGit(t, wsDir, "merge", "--no-ff", "feature-merge", "-m", "Merge feature-merge")

	// Update workspace to be on main
	mgr.state.AddWorkspace(state.Workspace{ID: "ws-test-1", Repo: "", Branch: "main", Path: wsDir})

	resp, err := mgr.GetGitGraph(ctx, wsID, 200, 5)
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	// Find merge commit with 2 parents
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
	mgr, remoteDir, wsDir, wsID := setupWorkspaceGraphTest(t, "feature-fork")
	ctx := context.Background()

	// The fork point is the initial commit (where we branched from main)
	forkHash := getHash(t, wsDir, "HEAD")

	// Add local commit
	commitOnWorkspace(t, wsDir, "local.txt", "local work")

	// Add remote commit
	commitOnRemote(t, remoteDir, wsDir, "remote.txt", "remote work")

	resp, err := mgr.GetGitGraph(ctx, wsID, 200, 5)
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	// Fork point should be in the graph
	found := false
	for _, node := range resp.Nodes {
		if node.Hash == forkHash {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("fork point %s not found in graph", forkHash[:8])
	}
}

func TestGitGraph_MaxCommits(t *testing.T) {
	mgr, _, wsDir, wsID := setupWorkspaceGraphTest(t, "feature-many")
	ctx := context.Background()

	// Add many commits
	for i := 0; i < 20; i++ {
		commitOnWorkspace(t, wsDir, "file"+string(rune('a'+i))+".txt", "commit "+string(rune('a'+i)))
	}

	resp, err := mgr.GetGitGraph(ctx, wsID, 5, 2)
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	if len(resp.Nodes) > 5 {
		t.Errorf("expected at most 5 nodes, got %d", len(resp.Nodes))
	}
}

func TestGitGraph_NoDivergence(t *testing.T) {
	mgr, _, _, wsID := setupWorkspaceGraphTest(t, "main")
	ctx := context.Background()

	// Branch is main, no divergence — should show recent commits
	resp, err := mgr.GetGitGraph(ctx, wsID, 200, 5)
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	// Should have at least the initial commit
	if len(resp.Nodes) == 0 {
		t.Error("expected at least 1 node")
	}
}

func TestGitGraph_WorkspaceAnnotation(t *testing.T) {
	mgr, _, wsDir, wsID := setupWorkspaceGraphTest(t, "feature-ann")
	ctx := context.Background()

	// Add a second workspace on the same branch
	mgr.state.AddWorkspace(state.Workspace{ID: "ws-ann-2", Repo: mgr.state.GetWorkspaces()[0].Repo, Branch: "feature-ann", Path: wsDir})

	commitOnWorkspace(t, wsDir, "ann.txt", "annotated commit")

	resp, err := mgr.GetGitGraph(ctx, wsID, 200, 5)
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	branch := resp.Branches["feature-ann"]
	if len(branch.WorkspaceIDs) != 2 {
		t.Errorf("expected 2 workspace_ids on branch, got %d", len(branch.WorkspaceIDs))
	}

	// HEAD node should have workspace_ids
	for _, node := range resp.Nodes {
		if node.Hash == branch.Head {
			if len(node.WorkspaceIDs) != 2 {
				t.Errorf("expected 2 workspace_ids on HEAD node, got %d", len(node.WorkspaceIDs))
			}
		} else if len(node.WorkspaceIDs) != 0 {
			t.Errorf("non-HEAD node %s should have 0 workspace_ids, got %d", node.ShortHash, len(node.WorkspaceIDs))
		}
	}
}

func TestGitGraph_UnknownWorkspace(t *testing.T) {
	mgr, _, _, _ := setupWorkspaceGraphTest(t, "main")
	ctx := context.Background()

	_, err := mgr.GetGitGraph(ctx, "nonexistent-ws", 200, 5)
	if err == nil {
		t.Fatal("expected error for unknown workspace")
	}
}

func TestGitGraph_Trimming(t *testing.T) {
	mgr, remoteDir, wsDir, wsID := setupWorkspaceGraphTest(t, "feature-trim")
	ctx := context.Background()

	// Add 15 commits to remote main
	for i := 0; i < 15; i++ {
		commitOnRemote(t, remoteDir, wsDir, "remote"+string(rune('a'+i))+".txt", "remote-"+string(rune('a'+i)))
	}

	// Add 1 local commit
	commitOnWorkspace(t, wsDir, "local.txt", "local work")

	resp, err := mgr.GetGitGraph(ctx, wsID, 200, 3)
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	// Should NOT have all 15+1+1 = 17 commits
	// Should have: 1 local + ~15 remote (down to fork) + fork + 3 context
	// But the trimming should keep it focused on the divergence region
	if len(resp.Nodes) == 0 {
		t.Fatal("expected non-empty graph")
	}

	// Both heads should be present
	if _, ok := resp.Branches["feature-trim"]; !ok {
		t.Error("expected feature-trim in branches")
	}
	if _, ok := resp.Branches["main"]; !ok {
		t.Error("expected main in branches")
	}
}

func TestGitGraph_MultipleMergeBases(t *testing.T) {
	mgr, remoteDir, wsDir, wsID := setupWorkspaceGraphTest(t, "feature-multi")
	ctx := context.Background()

	// Add local commit
	commitOnWorkspace(t, wsDir, "multi1.txt", "multi commit 1")

	// Add remote commit and fetch
	commitOnRemote(t, remoteDir, wsDir, "remote-advance.txt", "main advance")

	// Merge origin/main into local branch
	runGit(t, wsDir, "merge", "origin/main", "-m", "sync main into feature")

	// More local work
	commitOnWorkspace(t, wsDir, "multi2.txt", "multi commit 2")

	resp, err := mgr.GetGitGraph(ctx, wsID, 200, 5)
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	if len(resp.Nodes) < 3 {
		t.Errorf("expected at least 3 nodes, got %d", len(resp.Nodes))
	}
	if _, ok := resp.Branches["feature-multi"]; !ok {
		t.Error("expected feature-multi in branches")
	}
}

func TestGitGraph_InvalidWorkspacePath(t *testing.T) {
	// Workspace points to a non-existent directory — git commands should fail.
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.CreateDefault(configPath)

	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath)
	st.AddWorkspace(state.Workspace{
		ID:     "ws-bad-path",
		Repo:   "fake-repo",
		Branch: "main",
		Path:   "/nonexistent/path/to/workspace",
	})
	mgr := New(cfg, st, statePath)

	_, err := mgr.GetGitGraph(context.Background(), "ws-bad-path", 200, 5)
	if err == nil {
		t.Fatal("expected error for workspace with invalid path")
	}
}

func TestGitGraph_CorruptedGitDir(t *testing.T) {
	// Workspace directory exists but is not a git repo — git commands should fail.
	wsDir := t.TempDir()

	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.CreateDefault(configPath)

	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath)
	st.AddWorkspace(state.Workspace{
		ID:     "ws-no-git",
		Repo:   "fake-repo",
		Branch: "main",
		Path:   wsDir,
	})
	mgr := New(cfg, st, statePath)

	_, err := mgr.GetGitGraph(context.Background(), "ws-no-git", 200, 5)
	if err == nil {
		t.Fatal("expected error for workspace that is not a git repo")
	}
}

func TestGitGraph_CancelledContext(t *testing.T) {
	mgr, _, _, wsID := setupWorkspaceGraphTest(t, "feature-cancel")

	// Cancel the context before calling — git commands should fail.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := mgr.GetGitGraph(ctx, wsID, 200, 5)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestGitGraph_LocalTruncated(t *testing.T) {
	mgr, _, wsDir, wsID := setupWorkspaceGraphTest(t, "feature-truncate")
	ctx := context.Background()

	// Add 10 commits on the local branch
	for i := 0; i < 10; i++ {
		commitOnWorkspace(t, wsDir, fmt.Sprintf("file%d.txt", i), fmt.Sprintf("commit %d", i))
	}

	// Request with maxTotal=5 — should truncate local commits
	resp, err := mgr.GetGitGraph(ctx, wsID, 5, 2)
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	if !resp.LocalTruncated {
		t.Error("expected LocalTruncated=true when local commits exceed maxTotal")
	}

	if len(resp.Nodes) > 5 {
		t.Errorf("expected at most 5 nodes, got %d", len(resp.Nodes))
	}

	// HEAD should still be in the result
	headHash := resp.Branches["feature-truncate"].Head
	found := false
	for _, n := range resp.Nodes {
		if n.Hash == headHash {
			found = true
			break
		}
	}
	if !found {
		t.Error("HEAD commit should be in truncated result")
	}
}

func TestGitGraph_LocalNotTruncated(t *testing.T) {
	mgr, _, wsDir, wsID := setupWorkspaceGraphTest(t, "feature-notrunc")
	ctx := context.Background()

	// Add 3 commits — well under limit
	for i := 0; i < 3; i++ {
		commitOnWorkspace(t, wsDir, fmt.Sprintf("file%d.txt", i), fmt.Sprintf("commit %d", i))
	}

	resp, err := mgr.GetGitGraph(ctx, wsID, 200, 5)
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	if resp.LocalTruncated {
		t.Error("expected LocalTruncated=false when local commits fit within limit")
	}
}

func TestGitGraph_MainAheadNewestTimestamp(t *testing.T) {
	mgr, remoteDir, wsDir, wsID := setupWorkspaceGraphTest(t, "feature-timestamp")
	ctx := context.Background()

	// Add a local commit
	commitOnWorkspace(t, wsDir, "local.txt", "local work")

	// Add remote commits
	commitOnRemote(t, remoteDir, wsDir, "remote1.txt", "remote 1")
	commitOnRemote(t, remoteDir, wsDir, "remote2.txt", "remote 2")

	resp, err := mgr.GetGitGraph(ctx, wsID, 200, 5)
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	if resp.MainAheadCount != 2 {
		t.Errorf("expected MainAheadCount=2, got %d", resp.MainAheadCount)
	}

	if resp.MainAheadNewestTimestamp == "" {
		t.Error("expected MainAheadNewestTimestamp to be populated when main is ahead")
	}
}

func TestGitGraph_ManyMergeCommits(t *testing.T) {
	mgr, remoteDir, wsDir, wsID := setupWorkspaceGraphTest(t, "feature-merges")
	ctx := context.Background()

	// Create pattern: local commit, then merge from main, repeat
	for i := 0; i < 5; i++ {
		commitOnWorkspace(t, wsDir, fmt.Sprintf("local%d.txt", i), fmt.Sprintf("local %d", i))
		commitOnRemote(t, remoteDir, wsDir, fmt.Sprintf("remote%d.txt", i), fmt.Sprintf("remote %d", i))
		runGit(t, wsDir, "merge", "origin/main", "-m", fmt.Sprintf("merge %d", i))
	}

	resp, err := mgr.GetGitGraph(ctx, wsID, 200, 5)
	if err != nil {
		t.Fatalf("GetGitGraph: %v", err)
	}

	// ISL invariant: children never appear below their ancestors
	pos := make(map[string]int, len(resp.Nodes))
	for i, n := range resp.Nodes {
		pos[n.Hash] = i
	}
	for i, n := range resp.Nodes {
		for _, parentHash := range n.Parents {
			parentPos, ok := pos[parentHash]
			if !ok {
				continue
			}
			if i >= parentPos {
				t.Errorf("child %s (pos %d) appears at or below parent %s (pos %d)",
					n.ShortHash, i, resp.Nodes[parentPos].ShortHash, parentPos)
			}
		}
	}
}
