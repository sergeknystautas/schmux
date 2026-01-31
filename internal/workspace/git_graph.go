package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

const (
	defaultMaxCommits  = 200
	mainContextCommits = 5
)

// GetGitGraph returns the commit graph for a repo, showing all active workspace branches
// and their relationship to main.
func (m *Manager) GetGitGraph(ctx context.Context, repoName string, maxCommits int, branchFilter []string) (*contracts.GitGraphResponse, error) {
	if maxCommits <= 0 {
		maxCommits = defaultMaxCommits
	}

	// Look up repo by name
	repo, ok := m.config.FindRepo(repoName)
	if !ok {
		return nil, fmt.Errorf("repo not found: %s", repoName)
	}

	// Get bare repo path
	bareReposPath := m.config.GetBareReposPath()
	if bareReposPath == "" {
		return nil, fmt.Errorf("bare repos path not configured")
	}
	bareRepoName := extractRepoName(repo.URL)
	bareRepoPath := filepath.Join(bareReposPath, bareRepoName+".git")
	if _, err := os.Stat(bareRepoPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("bare clone not found for %s", repoName)
	}

	// Detect default branch
	defaultBranch := m.getDefaultBranch(ctx, bareRepoPath)

	// Build branch → workspace ID mapping from state
	workspaces := m.state.GetWorkspaces()
	branchWorkspaces := make(map[string][]string) // branch name → workspace IDs
	for _, ws := range workspaces {
		if ws.Repo == repo.URL {
			branchWorkspaces[ws.Branch] = append(branchWorkspaces[ws.Branch], ws.ID)
		}
	}

	// Determine which branches to include
	includeBranches := m.resolveIncludeBranches(ctx, bareRepoPath, defaultBranch, branchFilter, branchWorkspaces)

	// Resolve HEAD hash for each branch
	branchHeads := make(map[string]string) // branch name → commit hash
	for _, branch := range includeBranches {
		hash := m.resolveRef(ctx, bareRepoPath, "refs/remotes/origin/"+branch)
		if hash == "" {
			// Also try refs/heads/ for bare repos where refs might be stored directly
			hash = m.resolveRef(ctx, bareRepoPath, "refs/heads/"+branch)
		}
		if hash != "" {
			branchHeads[branch] = hash
		}
	}

	// Filter to branches that actually resolved
	var resolvedBranches []string
	for _, branch := range includeBranches {
		if _, ok := branchHeads[branch]; ok {
			resolvedBranches = append(resolvedBranches, branch)
		}
	}
	if len(resolvedBranches) == 0 {
		return &contracts.GitGraphResponse{
			Repo:     repo.URL,
			Nodes:    []contracts.GitGraphNode{},
			Branches: map[string]contracts.GitGraphBranch{},
		}, nil
	}

	// Find fork points for trimming
	mainHead := branchHeads[defaultBranch]
	forkPoints := make(map[string][]string) // branch → merge base hashes
	for _, branch := range resolvedBranches {
		if branch == defaultBranch {
			continue
		}
		bases := m.findMergeBases(ctx, bareRepoPath, branchHeads[branch], mainHead)
		if len(bases) > 0 {
			forkPoints[branch] = bases
		}
	}

	// Build ref args for git log
	var refArgs []string
	for _, branch := range resolvedBranches {
		refArgs = append(refArgs, branchHeads[branch])
	}

	// Run git log
	rawNodes, err := m.runGitLog(ctx, bareRepoPath, refArgs, maxCommits)
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}
	if len(rawNodes) == 0 {
		return &contracts.GitGraphResponse{
			Repo:     repo.URL,
			Nodes:    []contracts.GitGraphNode{},
			Branches: map[string]contracts.GitGraphBranch{},
		}, nil
	}

	// Build hash → node index for parent walking
	nodeIndex := make(map[string]int, len(rawNodes))
	for i, n := range rawNodes {
		nodeIndex[n.Hash] = i
	}

	// Derive branch membership by walking from each branch HEAD
	nodeBranches := make(map[string]map[string]bool, len(rawNodes))
	for _, branch := range resolvedBranches {
		head := branchHeads[branch]
		m.walkBranchMembership(rawNodes, nodeIndex, head, branch, nodeBranches)
	}

	// Trim: determine which nodes to keep
	keepSet := m.computeTrimSet(rawNodes, nodeIndex, resolvedBranches, branchHeads, forkPoints, defaultBranch)

	// Build final node list (preserving topo order)
	var nodes []contracts.GitGraphNode
	for _, n := range rawNodes {
		if !keepSet[n.Hash] {
			continue
		}

		// Populate branches for this node
		var branches []string
		if bm, ok := nodeBranches[n.Hash]; ok {
			for _, branch := range resolvedBranches {
				if bm[branch] {
					branches = append(branches, branch)
				}
			}
		}

		// Populate is_head and workspace_ids
		var isHead []string
		var workspaceIDs []string
		for _, branch := range resolvedBranches {
			if branchHeads[branch] == n.Hash {
				isHead = append(isHead, branch)
				workspaceIDs = append(workspaceIDs, branchWorkspaces[branch]...)
			}
		}

		// Filter parents to only include hashes in the kept set
		var parents []string
		for _, p := range n.Parents {
			parents = append(parents, p)
		}

		nodes = append(nodes, contracts.GitGraphNode{
			Hash:         n.Hash,
			ShortHash:    n.ShortHash,
			Message:      n.Message,
			Author:       n.Author,
			Timestamp:    n.Timestamp,
			Parents:      nonNilSlice(parents),
			Branches:     nonNilSlice(branches),
			IsHead:       nonNilSlice(isHead),
			WorkspaceIDs: nonNilSlice(workspaceIDs),
		})

		if len(nodes) >= maxCommits {
			break
		}
	}

	// Build branches map
	branchesMap := make(map[string]contracts.GitGraphBranch, len(resolvedBranches))
	for _, branch := range resolvedBranches {
		branchesMap[branch] = contracts.GitGraphBranch{
			Head:         branchHeads[branch],
			IsMain:       branch == defaultBranch,
			WorkspaceIDs: nonNilSlice(branchWorkspaces[branch]),
		}
	}

	return &contracts.GitGraphResponse{
		Repo:     repo.URL,
		Nodes:    nodes,
		Branches: branchesMap,
	}, nil
}

// rawNode is an intermediate parsed commit before trimming/annotation.
type rawNode struct {
	Hash      string
	ShortHash string
	Message   string
	Author    string
	Timestamp string
	Parents   []string
}

// resolveIncludeBranches determines which branches to include in the graph.
func (m *Manager) resolveIncludeBranches(ctx context.Context, bareRepoPath, defaultBranch string, branchFilter []string, branchWorkspaces map[string][]string) []string {
	seen := make(map[string]bool)
	var result []string

	add := func(branch string) {
		if !seen[branch] {
			seen[branch] = true
			result = append(result, branch)
		}
	}

	// Always include main/default
	add(defaultBranch)

	if len(branchFilter) > 0 {
		// Use explicit filter
		for _, b := range branchFilter {
			add(b)
		}
	} else {
		// Include all active workspace branches
		for branch := range branchWorkspaces {
			add(branch)
		}
	}

	return result
}

// resolveRef resolves a git ref to its commit hash.
func (m *Manager) resolveRef(ctx context.Context, repoPath, ref string) string {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", ref)
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// findMergeBases returns merge base commits between two refs.
func (m *Manager) findMergeBases(ctx context.Context, repoPath, ref1, ref2 string) []string {
	if ref1 == "" || ref2 == "" {
		return nil
	}
	cmd := exec.CommandContext(ctx, "git", "merge-base", "--all", ref1, ref2)
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	var bases []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			bases = append(bases, line)
		}
	}
	return bases
}

// runGitLog runs git log and parses the output into rawNode structs.
func (m *Manager) runGitLog(ctx context.Context, repoPath string, refs []string, maxCommits int) ([]rawNode, error) {
	args := []string{"log",
		"--format=%H|%h|%s|%an|%aI|%P",
		"--topo-order",
		fmt.Sprintf("--max-count=%d", maxCommits*2), // fetch extra for trimming
	}
	args = append(args, refs...)

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	var nodes []rawNode
	seen := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
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

	return nodes, nil
}

// walkBranchMembership marks all nodes reachable from head as belonging to branch.
func (m *Manager) walkBranchMembership(nodes []rawNode, nodeIndex map[string]int, head, branch string, nodeBranches map[string]map[string]bool) {
	stack := []string{head}
	for len(stack) > 0 {
		hash := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if _, ok := nodeBranches[hash]; !ok {
			nodeBranches[hash] = make(map[string]bool)
		}
		if nodeBranches[hash][branch] {
			continue // already visited
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

// computeTrimSet determines which commit hashes to keep in the final output.
func (m *Manager) computeTrimSet(nodes []rawNode, nodeIndex map[string]int, branches []string, branchHeads map[string]string, forkPoints map[string][]string, defaultBranch string) map[string]bool {
	keep := make(map[string]bool)

	// Collect all fork point hashes
	allForkPoints := make(map[string]bool)
	for _, bases := range forkPoints {
		for _, b := range bases {
			allForkPoints[b] = true
		}
	}

	// For each non-main branch: keep all commits from HEAD down to (and including) fork point
	for _, branch := range branches {
		if branch == defaultBranch {
			continue
		}
		head := branchHeads[branch]
		bases := forkPoints[branch]
		baseSet := make(map[string]bool, len(bases))
		for _, b := range bases {
			baseSet[b] = true
		}

		// Walk from head, keep everything until we pass all fork points
		stack := []string{head}
		visited := make(map[string]bool)
		for len(stack) > 0 {
			hash := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if visited[hash] {
				continue
			}
			visited[hash] = true
			keep[hash] = true

			// Stop walking past fork points
			if baseSet[hash] {
				continue
			}

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

	// For main: keep from HEAD down to the oldest fork point + context
	mainHead := branchHeads[defaultBranch]
	if mainHead != "" {
		// Find the oldest fork point index in the topo-sorted list
		oldestForkIdx := -1
		for hash := range allForkPoints {
			if idx, ok := nodeIndex[hash]; ok {
				if idx > oldestForkIdx {
					oldestForkIdx = idx
				}
			}
		}

		// Walk main from HEAD, keeping nodes
		contextRemaining := mainContextCommits
		pastOldestFork := false
		stack := []string{mainHead}
		visited := make(map[string]bool)
		for len(stack) > 0 {
			hash := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if visited[hash] {
				continue
			}
			visited[hash] = true
			keep[hash] = true

			if pastOldestFork {
				contextRemaining--
				if contextRemaining <= 0 {
					continue
				}
			}

			idx, ok := nodeIndex[hash]
			if !ok {
				continue
			}

			// Check if we've passed the oldest fork point
			if !pastOldestFork && allForkPoints[hash] {
				// Check if this is the oldest (highest index) fork point
				if idx >= oldestForkIdx {
					pastOldestFork = true
				}
			}

			for _, parent := range nodes[idx].Parents {
				if _, inGraph := nodeIndex[parent]; inGraph {
					stack = append(stack, parent)
				}
			}
		}
	}

	// If no fork points exist (e.g., single branch), keep all from heads
	if len(allForkPoints) == 0 {
		for _, branch := range branches {
			head := branchHeads[branch]
			stack := []string{head}
			visited := make(map[string]bool)
			for len(stack) > 0 {
				hash := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if visited[hash] {
					continue
				}
				visited[hash] = true
				keep[hash] = true

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
	}

	return keep
}

// nonNilSlice returns the slice or an empty non-nil slice if nil.
func nonNilSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
