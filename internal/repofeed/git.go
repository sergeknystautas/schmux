//go:build !norepofeed

package repofeed

import (
	"bytes"
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

// WriteDevFile writes a developer's JSON file into the orphan branch using git plumbing,
// without touching any working directory. The filename is derived from the email via SHA256.
func (g *GitOps) WriteDevFile(email string, devFile *DeveloperFile) error {
	data, err := json.MarshalIndent(devFile, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	// 1. Write blob to object store
	blobSHA, err := g.gitStdin("hash-object", data, "-w", "--stdin")
	if err != nil {
		return fmt.Errorf("hash-object: %w", err)
	}
	blobSHA = strings.TrimSpace(blobSHA)

	// 2. Create temp index path — must not exist on disk (git requires a fresh index)
	tmpFile, err := os.CreateTemp(g.BareDir, "repofeed-idx-")
	if err != nil {
		return fmt.Errorf("create temp index: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	os.Remove(tmpPath)
	defer os.Remove(tmpPath) // cleanup on crash

	filename := devFileNameFromEmail(email)

	// 3. Build the tree in the temp index
	env := []string{"GIT_INDEX_FILE=" + tmpPath}

	if g.branchExists() {
		if err := g.gitRunEnv(env, "read-tree", g.refName()); err != nil {
			return fmt.Errorf("read-tree: %w", err)
		}
	}

	cacheInfo := fmt.Sprintf("100644,%s,%s", blobSHA, filename)
	if err := g.gitRunEnv(env, "update-index", "--add", "--cacheinfo", cacheInfo); err != nil {
		return fmt.Errorf("update-index: %w", err)
	}

	treeSHA, err := g.gitEnv(env, "write-tree")
	if err != nil {
		return fmt.Errorf("write-tree: %w", err)
	}
	treeSHA = strings.TrimSpace(treeSHA)

	// 4. Create commit
	commitArgs := []string{"commit-tree", treeSHA, "-m", "update " + filename}
	if g.branchExists() {
		parentSHA, err := g.git("rev-parse", g.refName())
		if err != nil {
			return fmt.Errorf("rev-parse parent: %w", err)
		}
		commitArgs = append(commitArgs, "-p", strings.TrimSpace(parentSHA))
	}

	commitSHA, err := g.git(commitArgs...)
	if err != nil {
		return fmt.Errorf("commit-tree: %w", err)
	}
	commitSHA = strings.TrimSpace(commitSHA)

	// 5. Update branch ref
	if err := g.gitRun("update-ref", g.refName(), commitSHA); err != nil {
		return fmt.Errorf("update-ref: %w", err)
	}

	return nil
}

// PushToRemote pushes the branch to a remote.
func (g *GitOps) PushToRemote(remote string) error {
	return g.gitRun("push", remote, g.refName())
}

// GitDirFromWorkDir returns the .git directory for a working directory.
func GitDirFromWorkDir(workDir string) string {
	return workDir + "/.git"
}

// CleanupStaleIndexFiles removes leftover repofeed-idx-* temp files from a previous crash.
func CleanupStaleIndexFiles(bareDir string) {
	matches, err := filepath.Glob(filepath.Join(bareDir, "repofeed-idx-*"))
	if err != nil {
		return
	}
	for _, m := range matches {
		os.Remove(m)
	}
}

// devFileNameFromEmail returns the filename for a developer's file: <sha256[:12]>.json
func devFileNameFromEmail(email string) string {
	h := sha256.Sum256([]byte(email))
	return fmt.Sprintf("%x.json", h[:6])
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

// gitStdin runs a git command with data piped to stdin and returns stdout.
func (g *GitOps) gitStdin(name string, stdin []byte, args ...string) (string, error) {
	fullArgs := append([]string{name}, args...)
	cmd := exec.Command("git", fullArgs...)
	cmd.Dir = g.BareDir
	cmd.Stdin = bytes.NewReader(stdin)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %w: %s", name, err, exitErr.Stderr)
		}
		return "", fmt.Errorf("git %s: %w", name, err)
	}
	return string(out), nil
}

// gitEnv runs a git command with extra environment variables and returns stdout.
func (g *GitOps) gitEnv(env []string, args ...string) (string, error) {
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

// gitRunEnv runs a git command with extra environment variables without capturing output.
func (g *GitOps) gitRunEnv(env []string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.BareDir
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %w: %s", args[0], err, out)
	}
	return nil
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
