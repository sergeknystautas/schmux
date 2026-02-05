package difftool

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// isBinaryHeuristic checks if a file is binary by looking for null bytes in the first 8KB.
// This is fast but may miss binary files without early null bytes.
func isBinaryHeuristic(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 8192)
	n, _ := f.Read(buf)
	for i := 0; i < n; i++ {
		if buf[i] == 0 {
			return true
		}
	}
	return false
}

// IsBinaryFile checks if a file is binary using git's detection.
// It runs 'git diff --numstat --no-index /dev/null <file>' and checks if git reports it as binary.
// This respects .gitattributes and uses git's internal heuristics.
// The repoDir should be the git repository root (used for .gitattributes context).
func IsBinaryFile(ctx context.Context, repoDir string, filePath string) bool {
	// Fast path: check for null bytes in first 8KB
	if isBinaryHeuristic(filePath) {
		return true
	}

	// Use git's detection for cases the heuristic misses
	// (e.g., image files, archives, or text files with null bytes only later)
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "diff", "--numstat", "--no-index", "/dev/null", filePath)
	output, err := cmd.Output()
	if err != nil && len(output) == 0 {
		// If git failed and produced no output, fall back to heuristic result (not binary)
		return false
	}
	// Git outputs "-\t-\t..." for binary files
	return strings.HasPrefix(string(output), "-\t-")
}
