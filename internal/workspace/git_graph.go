package workspace

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

const (
	defaultMaxTotal    = 200 // Total commits to display
	defaultMainContext = 5   // Commits on main BEFORE fork point (historical context)
	defaultMaxLocal    = 50  // Commits on local feature branch
)

// GetGitGraph returns the commit graph for a workspace, showing the local branch
// vs origin/{defaultBranch} with the graph scoped to the divergence region.
//
// Parameters:
//   - maxTotal: Maximum total commits to display (applied after category limits).
//     The actual result may be smaller than maxTotal if category limits are hit first.
//   - mainContext: Number of commits on main BEFORE fork point (historical context).
func (m *Manager) GetGitGraph(ctx context.Context, workspaceID string, maxTotal int, mainContext int) (*contracts.GitGraphResponse, error) {
	if maxTotal <= 0 {
		maxTotal = defaultMaxTotal
	}
	if mainContext <= 0 {
		mainContext = defaultMainContext
	}

	// Look up workspace
	ws, ok := m.state.GetWorkspace(workspaceID)
	if !ok {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}

	gitDir := ws.Path
	localBranch := ws.Branch

	// Detect default branch (use cached version keyed by repo URL)
	defaultBranch, err := m.GetDefaultBranch(ctx, ws.Repo)
	if err != nil {
		defaultBranch = "main" // fallback if detection fails
	}
	originMain := "origin/" + defaultBranch

	// Resolve local HEAD and origin/main
	localHead := resolveRef(ctx, gitDir, "HEAD")
	originMainHead := resolveRef(ctx, gitDir, originMain)

	if localHead == "" {
		return nil, fmt.Errorf("cannot resolve HEAD in workspace %s", workspaceID)
	}

	// Build workspace ID mapping for annotations
	branchWorkspaces := make(map[string][]string)
	for _, w := range m.state.GetWorkspaces() {
		if w.Repo == ws.Repo {
			branchWorkspaces[w.Branch] = append(branchWorkspaces[w.Branch], w.ID)
		}
	}

	// Find fork point
	var forkPoint string
	if originMainHead != "" && localHead != originMainHead {
		forkPoint = findMergeBase(ctx, gitDir, "HEAD", originMain)
	}

	// Get main-ahead count (commits on origin/main that aren't on HEAD)
	mainAheadCount := 0
	if originMainHead != "" && localHead != originMainHead {
		mainAheadCount = getCommitCount(ctx, gitDir, "HEAD.."+originMain)
	}

	// Get newest timestamp of commits ahead on main
	var mainAheadNewestTimestamp string
	if mainAheadCount > 0 {
		mainAheadNewestTimestamp = getNewestTimestamp(ctx, gitDir, "HEAD.."+originMain)
	}

	// Determine what to log
	var rawNodes []RawNode
	var localTruncated bool

	if originMainHead == "" || localHead == originMainHead {
		// No divergence or no origin — just show recent commits from HEAD
		rawNodes, err = runGitLog(ctx, gitDir, []string{"HEAD"}, mainContext+1)
	} else if forkPoint == "" {
		// No common ancestor — show both independently
		rawNodes, err = runGitLog(ctx, gitDir, []string{"HEAD", originMain}, maxTotal)
	} else {
		// Normal divergence — get local commits + context (no main-ahead data)
		maxLocal := maxTotal - mainContext
		if maxLocal < 5 {
			maxLocal = 5
		}
		rawNodes, localTruncated, err = m.getGraphNodes(ctx, gitDir, forkPoint, mainContext, maxLocal)
	}
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	resp := BuildGraphResponse(rawNodes, localBranch, defaultBranch, localHead, originMainHead, forkPoint, branchWorkspaces, ws.Repo, maxTotal, mainAheadCount)
	resp.LocalTruncated = localTruncated
	resp.MainAheadNewestTimestamp = mainAheadNewestTimestamp
	return resp, nil
}

// BuildGraphResponse builds a GitGraphResponse from raw nodes and branch metadata.
// This is used by both local and remote graph handlers.
func BuildGraphResponse(nodes []RawNode, localBranch, defaultBranch, localHead, originMainHead, forkPoint string, branchWorkspaces map[string][]string, repo string, maxTotal int, mainAheadCount int) *contracts.GitGraphResponse {
	if len(nodes) == 0 {
		return &contracts.GitGraphResponse{
			Repo:           repo,
			Nodes:          []contracts.GitGraphNode{},
			Branches:       map[string]contracts.GitGraphBranch{},
			MainAheadCount: mainAheadCount,
		}
	}

	// Build hash → node index
	nodeIndex := make(map[string]int, len(nodes))
	for i, n := range nodes {
		nodeIndex[n.Hash] = i
	}

	// Derive branch membership by walking from each HEAD
	nodeBranches := make(map[string]map[string]bool, len(nodes))
	WalkBranchMembership(nodes, nodeIndex, localHead, localBranch, nodeBranches)
	if originMainHead != "" {
		// If originMainHead is not in the graph (main-ahead commits excluded),
		// walk from forkPoint instead - the context commits are on main.
		mainWalkStart := originMainHead
		if _, inGraph := nodeIndex[originMainHead]; !inGraph && forkPoint != "" {
			mainWalkStart = forkPoint
		}
		WalkBranchMembership(nodes, nodeIndex, mainWalkStart, defaultBranch, nodeBranches)
	}

	// The two branch names
	branches := []string{defaultBranch, localBranch}
	branchHeads := map[string]string{
		localBranch: localHead,
	}
	if originMainHead != "" {
		branchHeads[defaultBranch] = originMainHead
	}

	// Build annotated node map keyed by hash.
	annotatedNodes := make(map[string]contracts.GitGraphNode, len(nodes))
	for _, n := range nodes {
		var branchList []string
		if bm, ok := nodeBranches[n.Hash]; ok {
			for _, branch := range branches {
				if bm[branch] {
					branchList = append(branchList, branch)
				}
			}
		}

		var isHead []string
		var workspaceIDs []string
		for _, branch := range branches {
			if branchHeads[branch] == n.Hash {
				isHead = append(isHead, branch)
				workspaceIDs = append(workspaceIDs, branchWorkspaces[branch]...)
			}
		}

		annotatedNodes[n.Hash] = contracts.GitGraphNode{
			Hash:         n.Hash,
			ShortHash:    n.ShortHash,
			Message:      n.Message,
			Author:       n.Author,
			Timestamp:    n.Timestamp,
			Parents:      NonNilSlice(n.Parents),
			Branches:     NonNilSlice(branchList),
			IsHead:       NonNilSlice(isHead),
			WorkspaceIDs: NonNilSlice(workspaceIDs),
		}
	}

	// ISL-style DFS topological sort with sortAscCompare tie-breaks, then reverse.
	//
	// This replicates ISL's BaseDag.sortAsc (base_dag.ts:250-302):
	// - DFS from roots, using a stack (not a BFS queue).
	// - When a node still has unvisited parents (merge), defer it to the front.
	// - After visiting a node, push its children (sorted by compare) to the back.
	// - This avoids interleaving branches: it follows one branch continuously
	//   until completing it or hitting a merge.
	// - Reverse the result for rendering (heads first).

	// Parse timestamps into time.Time for proper comparison (not string-based).
	parsedTimes := make(map[string]time.Time, len(nodes))
	for _, n := range nodes {
		t, err := time.Parse(time.RFC3339, n.Timestamp)
		if err != nil {
			t = time.Time{} // zero time for unparseable
		}
		parsedTimes[n.Hash] = t
	}

	// sortAscCompare: the ISL tie-break comparator.
	// Returns negative if a < b (a should come first in ascending order).
	sortAscCompare := func(aHash, bHash string) int {
		bmA := nodeBranches[aHash]
		bmB := nodeBranches[bHash]

		// Phase: draft (on local, not on main) sorts before public.
		draftA := localBranch != defaultBranch && bmA[localBranch] && !bmA[defaultBranch]
		draftB := localBranch != defaultBranch && bmB[localBranch] && !bmB[defaultBranch]
		if draftA != draftB {
			if draftA {
				return -1
			}
			return 1
		}

		// Date: older before newer (using parsed time, not string comparison).
		tA := parsedTimes[aHash]
		tB := parsedTimes[bHash]
		if !tA.Equal(tB) {
			if tA.Before(tB) {
				return -1
			}
			return 1
		}

		// Hash: descending (higher hash sorts first = lower sort value).
		if aHash > bHash {
			return -1
		}
		if aHash < bHash {
			return 1
		}
		return 0
	}

	// Build parent→children adjacency (within the graph).
	childrenMap := make(map[string][]string, len(nodes))
	graphParents := make(map[string][]string, len(nodes))
	hashSet := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		hashSet[n.Hash] = true
	}
	for _, n := range nodes {
		for _, p := range n.Parents {
			if hashSet[p] {
				childrenMap[p] = append(childrenMap[p], n.Hash)
				graphParents[n.Hash] = append(graphParents[n.Hash], p)
			}
		}
	}

	// Find roots (nodes with no in-graph parents).
	var roots []string
	for _, n := range nodes {
		if len(graphParents[n.Hash]) == 0 {
			roots = append(roots, n.Hash)
		}
	}

	// Sort roots by compare (reversed because we pop from back = stack).
	sort.Slice(roots, func(i, j int) bool {
		return sortAscCompare(roots[i], roots[j]) > 0 // reversed for stack pop
	})

	// remaining[hash] = number of in-graph parents not yet visited.
	remaining := make(map[string]int, len(nodes))
	for _, n := range nodes {
		remaining[n.Hash] = len(graphParents[n.Hash])
	}

	// DFS walk (ISL sortImpl pattern).
	// Uses a deque with a front index to avoid O(n) prepend operations.
	// Elements before frontIdx are "front of deque" (deferred merges).
	// Elements at frontIdx..end are the stack (pop from back).
	toVisit := make([]string, 0, len(nodes))
	toVisit = append(toVisit, roots...)
	frontIdx := 0
	visited := make(map[string]bool, len(nodes))
	var topoOrder []string

	for frontIdx < len(toVisit) {
		// Pop from back (stack behavior).
		next := toVisit[len(toVisit)-1]
		toVisit = toVisit[:len(toVisit)-1]

		// If we've consumed past the front section, reset
		if len(toVisit) < frontIdx {
			frontIdx = len(toVisit)
		}

		if visited[next] {
			continue
		}

		// If this node still has unvisited parents, defer it to the front.
		if remaining[next] > 0 {
			// Insert at frontIdx position
			toVisit = append(toVisit, "")
			copy(toVisit[frontIdx+1:], toVisit[frontIdx:])
			toVisit[frontIdx] = next
			frontIdx++
			continue
		}

		// Output it.
		topoOrder = append(topoOrder, next)
		visited[next] = true

		// Push children (sorted by compare, reversed for stack).
		ch := childrenMap[next]
		if len(ch) > 1 {
			sort.Slice(ch, func(i, j int) bool {
				return sortAscCompare(ch[i], ch[j]) > 0 // reversed for stack pop
			})
		}
		for _, c := range ch {
			remaining[c]--
		}
		toVisit = append(toVisit, ch...)
	}

	// Reverse for rendering (heads → roots).
	resultNodes := make([]contracts.GitGraphNode, 0, len(topoOrder))
	for i := len(topoOrder) - 1; i >= 0; i-- {
		resultNodes = append(resultNodes, annotatedNodes[topoOrder[i]])
	}
	// Apply maxTotal limit as final cap (category limits are applied earlier in getGraphNodes)
	if len(resultNodes) > maxTotal {
		resultNodes = resultNodes[:maxTotal]
	}

	// Build branches map
	branchesMap := make(map[string]contracts.GitGraphBranch)
	if originMainHead != "" {
		branchesMap[defaultBranch] = contracts.GitGraphBranch{
			Head:         originMainHead,
			IsMain:       true,
			WorkspaceIDs: NonNilSlice(branchWorkspaces[defaultBranch]),
		}
	}
	branchesMap[localBranch] = contracts.GitGraphBranch{
		Head:         localHead,
		IsMain:       localBranch == defaultBranch,
		WorkspaceIDs: NonNilSlice(branchWorkspaces[localBranch]),
	}

	return &contracts.GitGraphResponse{
		Repo:           repo,
		Nodes:          resultNodes,
		Branches:       branchesMap,
		MainAheadCount: mainAheadCount,
	}
}

// getGraphNodes fetches commits for the graph: local commits + context (historical).
// Main-ahead commits are NOT included - only their count is returned separately.
func (m *Manager) getGraphNodes(ctx context.Context, gitDir, forkPoint string, mainContext int, maxLocal int) ([]RawNode, bool, error) {
	var allNodes []RawNode
	seen := make(map[string]bool)

	// 1. Fetch context commits: commits from forkPoint going back (historical context)
	if mainContext > 0 {
		contextArgs := []string{"log",
			"--format=%H%x00%h%x00%s%x00%an%x00%aI%x00%P",
			"--topo-order",
			fmt.Sprintf("--max-count=%d", mainContext),
			forkPoint,
		}
		contextCmd := exec.CommandContext(ctx, "git", contextArgs...)
		contextCmd.Dir = gitDir
		contextOutput, contextErr := contextCmd.Output()
		if contextErr == nil {
			contextNodes := ParseGitLogOutput(string(contextOutput))
			for _, n := range contextNodes {
				if !seen[n.Hash] {
					seen[n.Hash] = true
					allNodes = append(allNodes, n)
				}
			}
		}
	}

	// 2. Fetch local commits: all commits from HEAD that haven't been seen yet
	localArgs := []string{"log",
		"--format=%H%x00%h%x00%s%x00%an%x00%aI%x00%P",
		"--topo-order",
		fmt.Sprintf("--max-count=%d", maxLocal),
		"HEAD",
	}
	localCmd := exec.CommandContext(ctx, "git", localArgs...)
	localCmd.Dir = gitDir
	localOutput, localErr := localCmd.Output()
	if localErr == nil {
		localNodes := ParseGitLogOutput(string(localOutput))
		localTruncated := len(localNodes) >= maxLocal
		for _, n := range localNodes {
			if !seen[n.Hash] {
				seen[n.Hash] = true
				allNodes = append(allNodes, n)
			}
		}

		return allNodes, localTruncated, nil
	}

	// Ensure fork point is always included to keep graph connected
	if forkPoint != "" && !seen[forkPoint] {
		fpArgs := []string{"log",
			"--format=%H%x00%h%x00%s%x00%an%x00%aI%x00%P",
			"--max-count=1",
			forkPoint,
		}
		fpCmd := exec.CommandContext(ctx, "git", fpArgs...)
		fpCmd.Dir = gitDir
		fpOutput, fpErr := fpCmd.Output()
		if fpErr == nil {
			fpNodes := ParseGitLogOutput(string(fpOutput))
			for _, n := range fpNodes {
				if !seen[n.Hash] {
					seen[n.Hash] = true
					allNodes = append(allNodes, n)
				}
			}
		}
	}

	return allNodes, false, nil
}

// RawNode is an intermediate parsed commit before annotation.
type RawNode struct {
	Hash      string
	ShortHash string
	Message   string
	Author    string
	Timestamp string
	Parents   []string
}

// resolveRef resolves a git ref to its commit hash.
func resolveRef(ctx context.Context, repoPath, ref string) string {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", ref)
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// findMergeBase returns the merge base between two refs.
func findMergeBase(ctx context.Context, repoPath, ref1, ref2 string) string {
	cmd := exec.CommandContext(ctx, "git", "merge-base", ref1, ref2)
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// getCommitCount returns the number of commits in a range (e.g., "HEAD..origin/main").
func getCommitCount(ctx context.Context, repoPath, rangeSpec string) int {
	cmd := exec.CommandContext(ctx, "git", "rev-list", "--count", rangeSpec)
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return 0
	}
	count := 0
	fmt.Sscanf(strings.TrimSpace(string(output)), "%d", &count)
	return count
}

// getNewestTimestamp returns the timestamp of the newest commit in a range.
func getNewestTimestamp(ctx context.Context, repoPath, rangeSpec string) string {
	cmd := exec.CommandContext(ctx, "git", "log", "--format=%aI", "-1", rangeSpec)
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// runGitLog runs git log and parses the output into RawNode structs.
func runGitLog(ctx context.Context, repoPath string, refs []string, maxCommits int) ([]RawNode, error) {
	args := []string{"log",
		"--format=%H%x00%h%x00%s%x00%an%x00%aI%x00%P",
		"--topo-order",
		fmt.Sprintf("--max-count=%d", maxCommits),
	}
	args = append(args, refs...)

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	return ParseGitLogOutput(string(output)), nil
}

// nullHash is the all-zeros hash used by Sapling for absent parents (e.g., p2node on non-merge commits).
const nullHash = "0000000000000000000000000000000000000000"

// ParseGitLogOutput parses git log output into RawNode structs.
// Supports both null-byte (\x00) and pipe (|) field delimiters:
// - Local handler uses \x00 (via %x00 in git format) to avoid pipe collisions
// - Remote handler VCS command builders use | (pipe) for shell compatibility
func ParseGitLogOutput(output string) []RawNode {
	var nodes []RawNode
	seen := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		// Auto-detect delimiter: try null byte first, fall back to pipe
		delimiter := "\x00"
		parts := strings.SplitN(line, delimiter, 6)
		if len(parts) < 6 {
			delimiter = "|"
			parts = strings.SplitN(line, delimiter, 6)
		}
		if len(parts) < 6 {
			continue
		}
		hash := parts[0]
		if seen[hash] {
			continue
		}
		seen[hash] = true

		var parents []string
		if parts[5] != "" {
			for _, p := range strings.Fields(parts[5]) {
				// Filter out Sapling's null hash for absent parents
				if p != nullHash {
					parents = append(parents, p)
				}
			}
		}

		nodes = append(nodes, RawNode{
			Hash:      hash,
			ShortHash: parts[1],
			Message:   parts[2],
			Author:    parts[3],
			Timestamp: parts[4],
			Parents:   parents,
		})
	}
	return nodes
}

// WalkBranchMembership marks all nodes reachable from head as belonging to branch.
func WalkBranchMembership(nodes []RawNode, nodeIndex map[string]int, head, branch string, nodeBranches map[string]map[string]bool) {
	stack := []string{head}
	for len(stack) > 0 {
		hash := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if _, ok := nodeBranches[hash]; !ok {
			nodeBranches[hash] = make(map[string]bool)
		}
		if nodeBranches[hash][branch] {
			continue
		}
		nodeBranches[hash][branch] = true

		idx, ok := nodeIndex[hash]
		if !ok {
			continue
		}
		for _, parent := range nodes[idx].Parents {
			if _, inGraph := nodeIndex[parent]; inGraph {
				stack = append(stack, parent)
			}
		}
	}
}

// NonNilSlice returns the slice or an empty non-nil slice if nil.
func NonNilSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
