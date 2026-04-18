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

// isValidResourceID — rejects /, \, ., \x00, len > 128.
// Use for opaque IDs (curationID, recordingID, sessionID).
func isValidResourceID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	if strings.ContainsAny(id, "/\\.\x00") {
		return false
	}
	return true
}

// isValidRepoName — accepts [A-Za-z0-9_.-]+, len <= 128, rejects ".." and
// any leading "." (to avoid "...", ".foo/..", etc.). Use for repo names
// which legitimately contain dots (e.g., "owner.repo", "corp.org").
//
// SCOPE: HTTP-route validator only. The Repo struct (internal/config/config.go)
// continues to accept arbitrary strings for the Name field — this validator
// is a defense at the API perimeter, NOT a config-schema constraint.
func isValidRepoName(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	if s == ".." || strings.HasPrefix(s, ".") {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.':
		default:
			return false
		}
	}
	return true
}
