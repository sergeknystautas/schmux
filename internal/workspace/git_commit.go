package workspace

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// maxFileContentSize is the maximum size of file content to include in the response (1MB).
// This matches the limit used by the diff endpoint in handlers.go.
const maxFileContentSize = 1024 * 1024

// commitHashRegex validates commit hash format (hex only, 4-40 chars).
var commitHashRegex = regexp.MustCompile(`^[a-fA-F0-9]{4,40}$`)

// GetCommitDetail returns detailed information about a specific commit.
func (m *Manager) GetCommitDetail(ctx context.Context, workspaceID, commitHash string) (*contracts.CommitDetailResponse, error) {
	// Validate commit hash format
	if err := validateCommitHash(commitHash); err != nil {
		return nil, fmt.Errorf("invalid commit hash: %w", err)
	}

	// Look up workspace
	ws, ok := m.state.GetWorkspace(workspaceID)
	if !ok {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}

	gitDir := ws.Path

	// Verify commit exists in this repo and resolve to full hash
	fullHash, err := resolveAndValidateCommit(ctx, gitDir, commitHash)
	if err != nil {
		return nil, err
	}

	// Get commit metadata
	metadata, err := getCommitMetadata(ctx, gitDir, fullHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get commit metadata: %w", err)
	}

	// Get parent commits
	parents, err := getCommitParents(ctx, gitDir, fullHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get commit parents: %w", err)
	}

	// Get file diffs
	files, err := getCommitDiffFiles(ctx, gitDir, fullHash, parents)
	if err != nil {
		return nil, fmt.Errorf("failed to get commit diff: %w", err)
	}

	return &contracts.CommitDetailResponse{
		Hash:        fullHash,
		ShortHash:   fullHash[:7],
		AuthorName:  metadata.authorName,
		AuthorEmail: metadata.authorEmail,
		Timestamp:   metadata.timestamp,
		Message:     metadata.message,
		Parents:     parents,
		IsMerge:     len(parents) > 1,
		Files:       files,
	}, nil
}

// validateCommitHash validates that a commit hash has a valid format.
func validateCommitHash(hash string) error {
	if hash == "" {
		return fmt.Errorf("commit hash is empty")
	}
	if len(hash) > 40 {
		return fmt.Errorf("commit hash too long")
	}
	if !commitHashRegex.MatchString(hash) {
		return fmt.Errorf("commit hash contains invalid characters")
	}
	// Block dangerous patterns
	if strings.Contains(hash, "..") ||
		strings.Contains(hash, "$") ||
		strings.Contains(hash, "`") ||
		strings.Contains(hash, ";") ||
		strings.Contains(hash, "|") ||
		strings.Contains(hash, "&") ||
		strings.Contains(hash, " ") {
		return fmt.Errorf("commit hash contains forbidden characters")
	}
	return nil
}

// resolveAndValidateCommit resolves a commit hash and verifies it exists in the repo.
func resolveAndValidateCommit(ctx context.Context, gitDir, hash string) (string, error) {
	// First verify the object exists and is a commit
	cmd := exec.CommandContext(ctx, "git", "-C", gitDir, "cat-file", "-t", hash)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("commit not found: %s", hash)
	}
	objType := strings.TrimSpace(string(output))
	if objType != "commit" {
		return "", fmt.Errorf("object is not a commit: %s", hash)
	}

	// Resolve to full hash
	cmd = exec.CommandContext(ctx, "git", "-C", gitDir, "rev-parse", hash)
	output, err = cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to resolve commit: %s", hash)
	}
	return strings.TrimSpace(string(output)), nil
}

// commitMetadata holds parsed commit metadata.
type commitMetadata struct {
	authorName  string
	authorEmail string
	timestamp   string
	message     string
}

// getCommitMetadata retrieves commit metadata using git log with null-delimited format.
func getCommitMetadata(ctx context.Context, gitDir, hash string) (*commitMetadata, error) {
	// Use null-delimited format for reliable parsing
	// %an = author name, %ae = author email, %aI = ISO 8601 timestamp, %B = raw body
	cmd := exec.CommandContext(ctx, "git", "-C", gitDir, "log", "-1",
		"--format=%an%x00%ae%x00%aI%x00%B", hash)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	parts := strings.SplitN(string(output), "\x00", 4)
	if len(parts) < 4 {
		return nil, fmt.Errorf("unexpected git log output format")
	}

	return &commitMetadata{
		authorName:  parts[0],
		authorEmail: parts[1],
		timestamp:   parts[2],
		message:     strings.TrimSpace(parts[3]),
	}, nil
}

// getCommitParents returns the parent commit hashes.
func getCommitParents(ctx context.Context, gitDir, hash string) ([]string, error) {
	// git rev-parse hash^@ returns all parent hashes, one per line
	cmd := exec.CommandContext(ctx, "git", "-C", gitDir, "rev-parse", hash+"^@")
	output, err := cmd.Output()
	if err != nil {
		// Root commits have no parents - this is not an error
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 128 {
			return []string{}, nil
		}
		// For other errors, check if output is empty (also indicates root commit)
		if len(output) == 0 {
			return []string{}, nil
		}
		return nil, err
	}

	var parents []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line != "" {
			parents = append(parents, line)
		}
	}
	return parents, nil
}

// getCommitDiffFiles returns the file diffs for a commit.
func getCommitDiffFiles(ctx context.Context, gitDir, hash string, parents []string) ([]contracts.FileDiff, error) {
	var files []contracts.FileDiff

	// Determine diff range
	var diffArgs []string
	if len(parents) == 0 {
		// Root commit - use git show with --name-status
		return getRootCommitFiles(ctx, gitDir, hash)
	}
	// For merge commits, diff against first parent (standard git show behavior)
	diffArgs = []string{parents[0] + ".." + hash}

	// Get file list with status and line counts
	// Use --numstat for line counts and --name-status for status/paths
	numstatCmd := exec.CommandContext(ctx, "git", "-C", gitDir, "diff", "--numstat", "-M", "-C", diffArgs[0])
	numstatOutput, err := numstatCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --numstat failed: %w", err)
	}

	nameStatusCmd := exec.CommandContext(ctx, "git", "-C", gitDir, "diff", "--name-status", "-M", "-C", diffArgs[0])
	nameStatusOutput, err := nameStatusCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-status failed: %w", err)
	}

	// Parse numstat for line counts and binary detection
	numstatMap := parseNumstat(string(numstatOutput))

	// Parse name-status for file status and paths
	for _, line := range strings.Split(string(nameStatusOutput), "\n") {
		if line == "" {
			continue
		}

		fileDiff, err := parseNameStatusLine(line, numstatMap)
		if err != nil {
			continue // Skip unparseable lines
		}

		// Get file contents if not binary
		if !fileDiff.IsBinary {
			if fileDiff.Status != "deleted" && fileDiff.Status != "added" {
				// Modified or renamed - get both old and new content
				fileDiff.OldContent = getFileAtCommit(ctx, gitDir, parents[0], fileDiff.OldPath, fileDiff.NewPath)
				fileDiff.NewContent = getFileAtCommit(ctx, gitDir, hash, fileDiff.NewPath, fileDiff.NewPath)
			} else if fileDiff.Status == "added" {
				fileDiff.NewContent = getFileAtCommit(ctx, gitDir, hash, fileDiff.NewPath, fileDiff.NewPath)
			} else if fileDiff.Status == "deleted" {
				fileDiff.OldContent = getFileAtCommit(ctx, gitDir, parents[0], fileDiff.OldPath, fileDiff.OldPath)
			}
		}

		files = append(files, fileDiff)
	}

	return files, nil
}

// getRootCommitFiles handles root commits which have no parent to diff against.
func getRootCommitFiles(ctx context.Context, gitDir, hash string) ([]contracts.FileDiff, error) {
	// Use git show --name-status for root commits
	cmd := exec.CommandContext(ctx, "git", "-C", gitDir, "show", "--name-status", "--format=", hash)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	// Get numstat for line counts
	numstatCmd := exec.CommandContext(ctx, "git", "-C", gitDir, "show", "--numstat", "--format=", hash)
	numstatOutput, _ := numstatCmd.Output()
	numstatMap := parseNumstat(string(numstatOutput))

	var files []contracts.FileDiff
	for _, line := range strings.Split(string(output), "\n") {
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		status := parts[0]
		filePath := parts[1]

		// For root commits, all files are added
		fileDiff := contracts.FileDiff{
			NewPath: filePath,
			Status:  "added",
		}

		// Check for binary and get line counts
		if ns, ok := numstatMap[filePath]; ok {
			fileDiff.IsBinary = ns.isBinary
			fileDiff.LinesAdded = ns.added
			fileDiff.LinesRemoved = ns.removed
		}

		// Get file content if not binary
		if !fileDiff.IsBinary && status == "A" {
			fileDiff.NewContent = getFileAtCommit(ctx, gitDir, hash, filePath, filePath)
		}

		files = append(files, fileDiff)
	}

	return files, nil
}

// numstatEntry holds parsed numstat data.
type numstatEntry struct {
	added    int
	removed  int
	isBinary bool
}

// parseNumstat parses git diff --numstat output into a map.
func parseNumstat(output string) map[string]numstatEntry {
	result := make(map[string]numstatEntry)
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}

		addedStr := parts[0]
		removedStr := parts[1]
		// For renames, numstat shows "oldpath => newpath" or just the path
		filePath := parts[2]
		if strings.Contains(filePath, " => ") {
			// Extract new path from rename format
			renameParts := strings.Split(filePath, " => ")
			if len(renameParts) == 2 {
				filePath = renameParts[1]
			}
		}

		entry := numstatEntry{}
		if addedStr == "-" && removedStr == "-" {
			entry.isBinary = true
		} else {
			entry.added, _ = strconv.Atoi(addedStr)
			entry.removed, _ = strconv.Atoi(removedStr)
		}
		result[filePath] = entry
	}
	return result
}

// parseNameStatusLine parses a single line from git diff --name-status.
func parseNameStatusLine(line string, numstatMap map[string]numstatEntry) (contracts.FileDiff, error) {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return contracts.FileDiff{}, fmt.Errorf("invalid name-status line")
	}

	statusCode := parts[0]
	// Extract first character for status (handles R100, C100, etc.)
	status := statusCode[0:1]

	fileDiff := contracts.FileDiff{}

	switch status {
	case "A":
		fileDiff.Status = "added"
		fileDiff.NewPath = parts[1]
	case "D":
		fileDiff.Status = "deleted"
		fileDiff.OldPath = parts[1]
		fileDiff.NewPath = parts[1]
	case "M":
		fileDiff.Status = "modified"
		fileDiff.OldPath = parts[1]
		fileDiff.NewPath = parts[1]
	case "R":
		fileDiff.Status = "renamed"
		if len(parts) >= 3 {
			fileDiff.OldPath = parts[1]
			fileDiff.NewPath = parts[2]
		} else {
			fileDiff.NewPath = parts[1]
		}
	case "C":
		fileDiff.Status = "copied"
		if len(parts) >= 3 {
			fileDiff.OldPath = parts[1]
			fileDiff.NewPath = parts[2]
		} else {
			fileDiff.NewPath = parts[1]
		}
	default:
		fileDiff.Status = "modified"
		fileDiff.OldPath = parts[1]
		fileDiff.NewPath = parts[1]
	}

	// Look up numstat data
	lookupPath := fileDiff.NewPath
	if lookupPath == "" {
		lookupPath = fileDiff.OldPath
	}
	if ns, ok := numstatMap[lookupPath]; ok {
		fileDiff.IsBinary = ns.isBinary
		fileDiff.LinesAdded = ns.added
		fileDiff.LinesRemoved = ns.removed
	}

	return fileDiff, nil
}

// getFileAtCommit retrieves file content at a specific commit.
func getFileAtCommit(ctx context.Context, gitDir, commit, path, fallbackPath string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", gitDir, "show", commit+":"+path)
	output, err := cmd.Output()
	if err != nil && fallbackPath != path {
		// Try fallback path
		cmd = exec.CommandContext(ctx, "git", "-C", gitDir, "show", commit+":"+fallbackPath)
		output, err = cmd.Output()
	}
	if err != nil {
		return ""
	}

	// Cap content size to match handleDiff behavior
	if len(output) > maxFileContentSize {
		output = output[:maxFileContentSize]
	}

	// Check for binary content (null bytes in first 8KB)
	checkLen := len(output)
	if checkLen > 8192 {
		checkLen = 8192
	}
	for i := 0; i < checkLen; i++ {
		if output[i] == 0 {
			return "" // Binary file, return empty
		}
	}

	return string(output)
}
