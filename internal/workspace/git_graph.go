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
	defaultMaxCommits  = 200
	defaultContextSize = 5
)

// GetGitGraph returns the commit graph for a workspace, showing the local branch
// vs origin/{defaultBranch} with the graph scoped to the divergence region.
func (m *Manager) GetGitGraph(ctx context.Context, workspaceID string, maxCommits int, contextSize int) (*contracts.GitGraphResponse, error) {
	if maxCommits <= 0 {
		maxCommits = defaultMaxCommits
	}
	if contextSize <= 0 {
		contextSize = defaultContextSize
	}

	// Look up workspace
	ws, ok := m.state.GetWorkspace(workspaceID)
	if !ok {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}

	gitDir := ws.Path
	localBranch := ws.Branch

	// Detect default branch
	defaultBranch := m.getDefaultBranch(ctx, gitDir)
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

	// Determine what to log
	var rawNodes []rawNode
	var err error

	if originMainHead == "" || localHead == originMainHead {
		// No divergence or no origin — just show recent commits from HEAD
		rawNodes, err = runGitLog(ctx, gitDir, []string{"HEAD"}, contextSize+1)
	} else if forkPoint == "" {
		// No common ancestor — show both independently
		rawNodes, err = runGitLog(ctx, gitDir, []string{"HEAD", originMain}, maxCommits)
	} else {
		// Normal divergence — get commits in the divergence region plus context
		rawNodes, err = m.getGraphNodes(ctx, gitDir, forkPoint, originMain, maxCommits, contextSize)
	}
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	if len(rawNodes) == 0 {
		return &contracts.GitGraphResponse{
			Repo:     ws.Repo,
			Nodes:    []contracts.GitGraphNode{},
			Branches: map[string]contracts.GitGraphBranch{},
		}, nil
	}

	// Build hash → node index
	nodeIndex := make(map[string]int, len(rawNodes))
	for i, n := range rawNodes {
		nodeIndex[n.Hash] = i
	}

	// Derive branch membership by walking from each HEAD
	nodeBranches := make(map[string]map[string]bool, len(rawNodes))
	walkBranchMembership(rawNodes, nodeIndex, localHead, localBranch, nodeBranches)
	if originMainHead != "" {
		walkBranchMembership(rawNodes, nodeIndex, originMainHead, defaultBranch, nodeBranches)
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
	annotatedNodes := make(map[string]contracts.GitGraphNode, len(rawNodes))
	for _, n := range rawNodes {
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
			Parents:      nonNilSlice(n.Parents),
			Branches:     nonNilSlice(branchList),
			IsHead:       nonNilSlice(isHead),
			WorkspaceIDs: nonNilSlice(workspaceIDs),
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
	parsedTimes := make(map[string]time.Time, len(rawNodes))
	for _, n := range rawNodes {
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
	childrenMap := make(map[string][]string, len(rawNodes))
	graphParents := make(map[string][]string, len(rawNodes))
	hashSet := make(map[string]bool, len(rawNodes))
	for _, n := range rawNodes {
		hashSet[n.Hash] = true
	}
	for _, n := range rawNodes {
		for _, p := range n.Parents {
			if hashSet[p] {
				childrenMap[p] = append(childrenMap[p], n.Hash)
				graphParents[n.Hash] = append(graphParents[n.Hash], p)
			}
		}
	}

	// Find roots (nodes with no in-graph parents).
	var roots []string
	for _, n := range rawNodes {
		if len(graphParents[n.Hash]) == 0 {
			roots = append(roots, n.Hash)
		}
	}

	// Sort roots by compare (reversed because we pop from back = stack).
	sort.Slice(roots, func(i, j int) bool {
		return sortAscCompare(roots[i], roots[j]) > 0 // reversed for stack pop
	})

	// remaining[hash] = number of in-graph parents not yet visited.
	remaining := make(map[string]int, len(rawNodes))
	for _, n := range rawNodes {
		remaining[n.Hash] = len(graphParents[n.Hash])
	}

	// DFS walk (ISL sortImpl pattern).
	// toVisit is a deque: pop from back (stack), unshift to front for deferred merges.
	toVisit := make([]string, len(roots))
	copy(toVisit, roots)
	visited := make(map[string]bool, len(rawNodes))
	var topoOrder []string

	for len(toVisit) > 0 {
		// Pop from back (stack behavior).
		next := toVisit[len(toVisit)-1]
		toVisit = toVisit[:len(toVisit)-1]

		if visited[next] {
			continue
		}

		// If this node still has unvisited parents, defer it to the front.
		if remaining[next] > 0 {
			toVisit = append([]string{next}, toVisit...)
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
	nodes := make([]contracts.GitGraphNode, 0, len(topoOrder))
	for i := len(topoOrder) - 1; i >= 0; i-- {
		nodes = append(nodes, annotatedNodes[topoOrder[i]])
	}
	if len(nodes) > maxCommits {
		nodes = nodes[:maxCommits]
	}

	// Build branches map
	branchesMap := make(map[string]contracts.GitGraphBranch)
	if originMainHead != "" {
		branchesMap[defaultBranch] = contracts.GitGraphBranch{
			Head:         originMainHead,
			IsMain:       true,
			WorkspaceIDs: nonNilSlice(branchWorkspaces[defaultBranch]),
		}
	}
	branchesMap[localBranch] = contracts.GitGraphBranch{
		Head:         localHead,
		IsMain:       localBranch == defaultBranch,
		WorkspaceIDs: nonNilSlice(branchWorkspaces[localBranch]),
	}

	return &contracts.GitGraphResponse{
		Repo:     ws.Repo,
		Nodes:    nodes,
		Branches: branchesMap,
	}, nil
}

// getGraphNodes fetches commits for the divergence region: local ahead, origin ahead, fork point, context.
func (m *Manager) getGraphNodes(ctx context.Context, gitDir, forkPoint, originMain string, maxCommits, contextSize int) ([]rawNode, error) {
	// Get all commits reachable from HEAD or origin/main but not before fork point's parents,
	// plus some context commits below the fork point.
	// Strategy: log HEAD + origin/main with --ancestry-path from fork point, then add context.

	// Commits in the divergence region (HEAD and origin/main down to fork point)
	args := []string{"log",
		"--format=%H|%h|%s|%an|%aI|%P",
		"--topo-order",
		"HEAD", originMain,
		"--not", forkPoint + "^",
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = gitDir
	output, err := cmd.Output()
	if err != nil {
		// Fallback: try without --not (fork point might be root commit)
		return runGitLog(ctx, gitDir, []string{"HEAD", originMain}, maxCommits)
	}

	nodes := parseGitLogOutput(string(output))

	// Add context commits below the fork point
	if contextSize > 0 {
		contextArgs := []string{"log",
			"--format=%H|%h|%s|%an|%aI|%P",
			"--topo-order",
			fmt.Sprintf("--max-count=%d", contextSize),
			forkPoint,
		}
		ctxCmd := exec.CommandContext(ctx, "git", contextArgs...)
		ctxCmd.Dir = gitDir
		ctxOutput, ctxErr := ctxCmd.Output()
		if ctxErr == nil {
			contextNodes := parseGitLogOutput(string(ctxOutput))
			// Append context, deduplicating
			seen := make(map[string]bool, len(nodes))
			for _, n := range nodes {
				seen[n.Hash] = true
			}
			for _, n := range contextNodes {
				if !seen[n.Hash] {
					seen[n.Hash] = true
					nodes = append(nodes, n)
				}
			}
		}
	}

	return nodes, nil
}

// rawNode is an intermediate parsed commit before annotation.
type rawNode struct {
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

// runGitLog runs git log and parses the output into rawNode structs.
func runGitLog(ctx context.Context, repoPath string, refs []string, maxCommits int) ([]rawNode, error) {
	args := []string{"log",
		"--format=%H|%h|%s|%an|%aI|%P",
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

	return parseGitLogOutput(string(output)), nil
}

// parseGitLogOutput parses pipe-delimited git log output into rawNode structs.
func parseGitLogOutput(output string) []rawNode {
	var nodes []rawNode
	seen := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 6)
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
			parents = strings.Fields(parts[5])
		}

		nodes = append(nodes, rawNode{
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

// walkBranchMembership marks all nodes reachable from head as belonging to branch.
func walkBranchMembership(nodes []rawNode, nodeIndex map[string]int, head, branch string, nodeBranches map[string]map[string]bool) {
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

// nonNilSlice returns the slice or an empty non-nil slice if nil.
func nonNilSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
