package difftool

import (
	"os"
	"path/filepath"
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

// IsBinaryFile reports whether the file looks binary, via a null-byte scan of
// its first 8KB. filePath is relative to repoDir. Unreadable files report
// not-binary.
//
// The check is deliberately in-process: a previous implementation shelled out
// to `git diff --no-index` per file (~8ms of fork/exec each), which dominated
// /api/diff latency on workspaces with hundreds of untracked files — ~700x
// slower than this read while classifying the same files as binary. Files
// whose first null byte appears after 8KB are misread as text; git's diff
// endpoints cap served content anyway, so the cost of that miss is a garbled
// diff view, not incorrect data.
func IsBinaryFile(repoDir string, filePath string) bool {
	return isBinaryHeuristic(filepath.Join(repoDir, filePath))
}
