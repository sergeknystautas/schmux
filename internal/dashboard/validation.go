package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// validateGitFilePaths checks that none of the file paths contain path traversal
// components (e.g., "../"). Returns an error message if any path is invalid.
func validateGitFilePaths(files []string) string {
	for _, f := range files {
		cleaned := filepath.Clean(f)
		if cleaned == "." || filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
			return fmt.Sprintf("invalid file path: %q", f)
		}
	}
	return ""
}

// isPathWithinDir checks whether fullPath is contained within baseDir after
// cleaning both paths. Returns false if fullPath escapes baseDir via path
// traversal (e.g., "../").
func isPathWithinDir(fullPath, baseDir string) bool {
	cleanFull := filepath.Clean(fullPath)
	cleanBase := filepath.Clean(baseDir)
	return strings.HasPrefix(cleanFull, cleanBase+string(filepath.Separator)) || cleanFull == cleanBase
}

// caseSensitiveFileExists checks whether a file with the exact given name
// (case-sensitive) exists in dir. This is needed because macOS APFS is
// case-insensitive — os.Stat("Foo.md") succeeds even if the file is "foo.md".
func caseSensitiveFileExists(dir, filename string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Name() == filename {
			return true
		}
	}
	return false
}
