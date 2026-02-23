package compound

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/log"
)

// pkgLogger is the package-level logger for compound merge operations.
// Set via SetLogger from the daemon initialization.
var pkgLogger *log.Logger

// SetLogger sets the package-level logger for compound operations.
func SetLogger(l *log.Logger) {
	pkgLogger = l
}

// MergeAction represents the action to take for a file change.
type MergeAction int

const (
	MergeActionSkip     MergeAction = iota // File unchanged or identical to overlay
	MergeActionFastPath                    // Overlay unchanged, workspace is strictly newer
	MergeActionLLMMerge                    // Both diverged, need LLM merge
)

// String returns a human-readable name for the merge action.
func (a MergeAction) String() string {
	switch a {
	case MergeActionSkip:
		return "skip"
	case MergeActionFastPath:
		return "fast-path"
	case MergeActionLLMMerge:
		return "llm-merge"
	default:
		return fmt.Sprintf("unknown(%d)", int(a))
	}
}

const maxLLMMergeFileSize = 100 * 1024 // 100KB

// LLMExecutor is a function that sends a prompt to an LLM and returns the response.
type LLMExecutor func(ctx context.Context, prompt string, timeout time.Duration) (string, error)

// DetermineMergeAction decides which merge path to take based on content hashes.
func DetermineMergeAction(wsPath, overlayPath, manifestHash string) (MergeAction, error) {
	wsHash, err := FileHash(wsPath)
	if err != nil {
		return MergeActionSkip, fmt.Errorf("failed to hash workspace file: %w", err)
	}

	// Path 1: Skip — workspace file unchanged from manifest
	if wsHash == manifestHash {
		return MergeActionSkip, nil
	}

	// Check if overlay and workspace have identical content
	overlayHash, err := FileHash(overlayPath)
	if err != nil {
		// Overlay file missing — treat as fast path
		return MergeActionFastPath, nil
	}

	if wsHash == overlayHash {
		return MergeActionSkip, nil
	}

	// Path 2: Fast path — overlay unchanged from manifest
	if overlayHash == manifestHash {
		return MergeActionFastPath, nil
	}

	// Path 3: Both diverged
	return MergeActionLLMMerge, nil
}

// ExecuteMerge performs the merge and writes the result to the overlay.
func ExecuteMerge(ctx context.Context, action MergeAction, wsPath, overlayPath string, executor LLMExecutor) ([]byte, error) {
	switch action {
	case MergeActionSkip:
		return nil, nil
	case MergeActionFastPath:
		content, err := os.ReadFile(wsPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read workspace file: %w", err)
		}
		if err := atomicWriteFile(overlayPath, content, 0644); err != nil {
			return nil, fmt.Errorf("failed to write overlay file: %w", err)
		}
		return content, nil
	case MergeActionLLMMerge:
		return executeLLMMerge(ctx, wsPath, overlayPath, executor)
	default:
		return nil, fmt.Errorf("unknown merge action: %d", action)
	}
}

func executeLLMMerge(ctx context.Context, wsPath, overlayPath string, executor LLMExecutor) ([]byte, error) {
	wsContent, err := os.ReadFile(wsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read workspace file: %w", err)
	}

	overlayContent, err := os.ReadFile(overlayPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read overlay file: %w", err)
	}

	// JSONL files: line-level union (no LLM needed)
	if strings.HasSuffix(wsPath, ".jsonl") {
		if len(wsContent)+len(overlayContent) > maxLLMMergeFileSize {
			if pkgLogger != nil {
				pkgLogger.Warn("JSONL file too large for merge, using last-write-wins", "bytes", len(wsContent)+len(overlayContent), "path", wsPath)
			}
			if err := atomicWriteFile(overlayPath, wsContent, 0644); err != nil {
				return nil, fmt.Errorf("failed to write overlay file: %w", err)
			}
			return wsContent, nil
		}
		return mergeJSONLLines(wsContent, overlayContent, overlayPath)
	}

	// Safety: binary files -> last-write-wins
	if isBinaryContent(wsContent) || isBinaryContent(overlayContent) {
		if pkgLogger != nil {
			pkgLogger.Warn("binary file detected, using last-write-wins", "path", wsPath)
		}
		if err := atomicWriteFile(overlayPath, wsContent, 0644); err != nil {
			return nil, fmt.Errorf("failed to write overlay file: %w", err)
		}
		return wsContent, nil
	}

	// Safety: large files -> last-write-wins
	if len(wsContent) > maxLLMMergeFileSize {
		if pkgLogger != nil {
			pkgLogger.Warn("file too large for LLM merge, using last-write-wins", "bytes", len(wsContent), "path", wsPath)
		}
		if err := atomicWriteFile(overlayPath, wsContent, 0644); err != nil {
			return nil, fmt.Errorf("failed to write overlay file: %w", err)
		}
		return wsContent, nil
	}

	// Try LLM merge
	if executor != nil {
		prompt := BuildMergePrompt(string(overlayContent), string(wsContent))
		response, err := executor(ctx, prompt, 30*time.Second)
		if err == nil && strings.TrimSpace(response) != "" {
			merged := []byte(response)
			if err := atomicWriteFile(overlayPath, merged, 0644); err != nil {
				return nil, fmt.Errorf("failed to write merged overlay file: %w", err)
			}
			if pkgLogger != nil {
				pkgLogger.Info("LLM merge successful", "path", wsPath)
			}
			return merged, nil
		}
		if err != nil {
			if pkgLogger != nil {
				pkgLogger.Warn("LLM merge failed, falling back to last-write-wins", "err", err)
			}
		} else {
			if pkgLogger != nil {
				pkgLogger.Warn("LLM returned empty response, falling back to last-write-wins")
			}
		}
	}

	// Fallback: last-write-wins
	if err := atomicWriteFile(overlayPath, wsContent, 0644); err != nil {
		return nil, fmt.Errorf("failed to write overlay file (LWW fallback): %w", err)
	}
	return wsContent, nil
}

// BuildMergePrompt constructs the LLM prompt for merging two file versions.
func BuildMergePrompt(overlayContent, workspaceContent string) string {
	return fmt.Sprintf(`Merge these two versions of a configuration file. Both have been modified independently from a common base.

Rules:
- For JSON files with arrays: union the arrays (keep all unique entries from both)
- For key-value settings: keep entries from both versions
- Never remove entries that exist in either version
- If values conflict for the same key, prefer VERSION B (the workspace version)
- Output ONLY the merged file content, no explanation or markdown fencing

VERSION A (current overlay):
%s

VERSION B (workspace modification):
%s`, overlayContent, workspaceContent)
}

// mergeJSONLLines performs a line-level union of two JSONL files.
// Deduplicates by exact line content. Preserves order: overlay lines first, then new workspace lines.
//
// NOTE: Deduplication uses exact string comparison of trimmed lines. Semantically identical
// JSON objects with different key ordering (e.g., {"a":1,"b":2} vs {"b":2,"a":1}) will be
// treated as different lines and both kept in the output.
func mergeJSONLLines(wsContent, overlayContent []byte, overlayPath string) ([]byte, error) {
	seen := make(map[string]bool)
	var merged []string

	// Add overlay lines first (preserves existing order)
	for _, line := range strings.Split(strings.TrimSpace(string(overlayContent)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !seen[line] {
			seen[line] = true
			merged = append(merged, line)
		}
	}

	// Add workspace-only lines
	for _, line := range strings.Split(strings.TrimSpace(string(wsContent)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !seen[line] {
			seen[line] = true
			merged = append(merged, line)
		}
	}

	result := []byte(strings.Join(merged, "\n") + "\n")
	if err := atomicWriteFile(overlayPath, result, 0644); err != nil {
		return nil, fmt.Errorf("failed to write merged JSONL: %w", err)
	}
	if pkgLogger != nil {
		pkgLogger.Info("JSONL line-union merge", "unique_lines", len(merged))
	}
	return result, nil
}

// isBinaryContent checks if content appears to be binary by looking for null bytes
// in the first 8KB (same heuristic as git).
func isBinaryContent(content []byte) bool {
	check := content
	if len(check) > 8192 {
		check = check[:8192]
	}
	return bytes.Contains(check, []byte{0})
}

// atomicWriteFile writes data to path atomically using a temp file and rename.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".merge-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}
