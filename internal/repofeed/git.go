//go:build !norepofeed

package repofeed

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitOps provides git plumbing operations for reading/writing to an orphan branch
// without touching any working directory.
type GitOps struct {
	BareDir string // Path to the bare or non-bare git repo (.git dir or repo dir)
	Branch  string // Branch name (e.g. "dev-repofeed")
}

// emailToFilename converts an email address to a safe filename.
func emailToFilename(email string) string {
	h := sha256.Sum256([]byte(email))
	return fmt.Sprintf("%x.json", h[:8])
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

// WriteDevFile writes a developer file to the orphan branch using git plumbing commands.
func (g *GitOps) WriteDevFile(email string, file *DeveloperFile) error {
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	// Use a temporary index file to avoid interfering with any working directory
	tmpIndex, err := os.CreateTemp("", "repofeed-index-*")
	if err != nil {
		return fmt.Errorf("create temp index: %w", err)
	}
	indexFile := tmpIndex.Name()
	tmpIndex.Close()
	os.Remove(indexFile) // Remove so git can create it fresh
	defer os.Remove(indexFile)

	// If the branch already exists, read the existing tree into our temp index
	if g.branchExists() {
		treeHash, err := g.git("rev-parse", g.refName()+"^{tree}")
		if err != nil {
			return fmt.Errorf("rev-parse tree: %w", err)
		}
		if err := g.gitWithEnv([]string{"GIT_INDEX_FILE=" + indexFile}, "read-tree", strings.TrimSpace(treeHash)); err != nil {
			return fmt.Errorf("read-tree: %w", err)
		}
	}

	// Hash the blob
	blobHash, err := g.gitStdin(data, "hash-object", "-w", "--stdin")
	if err != nil {
		return fmt.Errorf("hash-object: %w", err)
	}

	filename := emailToFilename(email)

	// Update the index with the new blob
	if err := g.gitWithEnv(
		[]string{"GIT_INDEX_FILE=" + indexFile},
		"update-index", "--add", "--cacheinfo", "100644", strings.TrimSpace(blobHash), filename,
	); err != nil {
		return fmt.Errorf("update-index: %w", err)
	}

	// Write the tree
	treeHash, err := g.gitWithEnvOutput([]string{"GIT_INDEX_FILE=" + indexFile}, "write-tree")
	if err != nil {
		return fmt.Errorf("write-tree: %w", err)
	}

	// Create the commit
	var commitArgs []string
	if g.branchExists() {
		parentHash, err := g.git("rev-parse", g.refName())
		if err != nil {
			return fmt.Errorf("rev-parse parent: %w", err)
		}
		commitArgs = []string{"commit-tree", strings.TrimSpace(treeHash), "-p", strings.TrimSpace(parentHash), "-m", "Update " + filename}
	} else {
		commitArgs = []string{"commit-tree", strings.TrimSpace(treeHash), "-m", "Initial " + filename}
	}

	commitHash, err := g.gitWithEnvOutput(
		[]string{
			"GIT_AUTHOR_NAME=repofeed",
			"GIT_AUTHOR_EMAIL=repofeed@schmux",
			"GIT_COMMITTER_NAME=repofeed",
			"GIT_COMMITTER_EMAIL=repofeed@schmux",
		},
		commitArgs...,
	)
	if err != nil {
		return fmt.Errorf("commit-tree: %w", err)
	}

	// Update the ref
	if err := g.gitRun("update-ref", g.refName(), strings.TrimSpace(commitHash)); err != nil {
		return fmt.Errorf("update-ref: %w", err)
	}

	return nil
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

// PushToRemote pushes the branch to a remote.
func (g *GitOps) PushToRemote(remote string) error {
	return g.gitRun("push", remote, g.refName())
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

// gitStdin runs a git command with stdin data and returns stdout.
func (g *GitOps) gitStdin(stdin []byte, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.BareDir
	cmd.Stdin = strings.NewReader(string(stdin))
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %w: %s", args[0], err, exitErr.Stderr)
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}

// gitWithEnv runs a git command with extra environment variables (no output capture).
func (g *GitOps) gitWithEnv(env []string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.BareDir
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %w: %s", args[0], err, out)
	}
	return nil
}

// gitWithEnvOutput runs a git command with extra environment variables and returns stdout.
func (g *GitOps) gitWithEnvOutput(env []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.BareDir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %w: %s", args[0], err, exitErr.Stderr)
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}

// GitDirFromWorkDir returns the .git directory for a given working directory.
func GitDirFromWorkDir(workDir string) string {
	return filepath.Join(workDir, ".git")
}
