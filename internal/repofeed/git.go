//go:build !norepofeed

package repofeed

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// GitOps provides git plumbing operations for reading/writing to an orphan branch
// without touching any working directory.
type GitOps struct {
	BareDir string // Path to the bare or non-bare git repo (.git dir or repo dir)
	Branch  string // Branch name (e.g. "dev-repofeed")
}

// refName returns the full ref name for the branch.
func (g *GitOps) refName() string {
	return "refs/heads/" + g.Branch
}

// branchExists checks if the branch ref exists.
func (g *GitOps) branchExists() bool {
	cmd := exec.Command("git", "rev-parse", "--verify", g.refName())
	cmd.Dir = g.BareDir
	return cmd.Run() == nil
}

// ReadAllDevFiles reads all developer files from the orphan branch.
func (g *GitOps) ReadAllDevFiles() ([]*DeveloperFile, error) {
	if !g.branchExists() {
		return nil, nil
	}

	// List tree entries
	output, err := g.git("ls-tree", g.refName())
	if err != nil {
		return nil, fmt.Errorf("ls-tree: %w", err)
	}

	output = strings.TrimSpace(output)
	if output == "" {
		return nil, nil
	}

	var files []*DeveloperFile
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		// Format: <mode> <type> <hash>\t<filename>
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		filename := parts[1]
		if !strings.HasSuffix(filename, ".json") {
			continue
		}
		fields := strings.Fields(parts[0])
		if len(fields) < 3 {
			continue
		}
		blobHash := fields[2]

		// Read the blob
		content, err := g.git("cat-file", "-p", blobHash)
		if err != nil {
			continue // skip unreadable blobs
		}

		var devFile DeveloperFile
		if err := json.Unmarshal([]byte(content), &devFile); err != nil {
			continue // skip unparseable files
		}
		files = append(files, &devFile)
	}

	return files, nil
}

// FetchFromRemote fetches the branch from a remote.
func (g *GitOps) FetchFromRemote(remote string) error {
	return g.gitRun("fetch", remote, g.refName()+":"+g.refName())
}

// git runs a git command and returns stdout.
func (g *GitOps) git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.BareDir
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %w: %s", args[0], err, exitErr.Stderr)
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}

// gitRun runs a git command without capturing output.
func (g *GitOps) gitRun(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.BareDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %w: %s", args[0], err, out)
	}
	return nil
}
