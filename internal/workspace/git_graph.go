package workspace

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

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

	// Build final node list
	var nodes []contracts.GitGraphNode
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

		nodes = append(nodes, contracts.GitGraphNode{
			Hash:         n.Hash,
			ShortHash:    n.ShortHash,
			Message:      n.Message,
			Author:       n.Author,
			Timestamp:    n.Timestamp,
			Parents:      nonNilSlice(n.Parents),
			Branches:     nonNilSlice(branchList),
			IsHead:       nonNilSlice(isHead),
			WorkspaceIDs: nonNilSlice(workspaceIDs),
		})

		if len(nodes) >= maxCommits {
			break
		}
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
