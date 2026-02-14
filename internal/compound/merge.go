package compound

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// MergeAction represents the action to take for a file change.
type MergeAction int

const (
	MergeActionSkip     MergeAction = iota // File unchanged or identical to overlay
	MergeActionFastPath                    // Overlay unchanged, workspace is strictly newer
	MergeActionLLMMerge                    // Both diverged, need LLM merge
)

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
		if err := os.WriteFile(overlayPath, content, 0644); err != nil {
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
		return mergeJSONLLines(wsContent, overlayContent, overlayPath)
	}

	// Safety: binary files -> last-write-wins
	if IsBinary(wsPath) || IsBinary(overlayPath) {
		fmt.Printf("[compound] binary file detected, using last-write-wins: %s\n", wsPath)
		if err := os.WriteFile(overlayPath, wsContent, 0644); err != nil {
			return nil, fmt.Errorf("failed to write overlay file: %w", err)
		}
		return wsContent, nil
	}

	// Safety: large files -> last-write-wins
	info, _ := os.Stat(wsPath)
	if info != nil && info.Size() > maxLLMMergeFileSize {
		fmt.Printf("[compound] file too large for LLM merge (%d bytes), using last-write-wins: %s\n", info.Size(), wsPath)
		if err := os.WriteFile(overlayPath, wsContent, 0644); err != nil {
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
			if err := os.WriteFile(overlayPath, merged, 0644); err != nil {
				return nil, fmt.Errorf("failed to write merged overlay file: %w", err)
			}
			fmt.Printf("[compound] LLM merge successful: %s\n", wsPath)
			return merged, nil
		}
		if err != nil {
			fmt.Printf("[compound] LLM merge failed, falling back to last-write-wins: %v\n", err)
		} else {
			fmt.Printf("[compound] LLM returned empty response, falling back to last-write-wins\n")
		}
	}

	// Fallback: last-write-wins
	if err := os.WriteFile(overlayPath, wsContent, 0644); err != nil {
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
	if err := os.WriteFile(overlayPath, result, 0644); err != nil {
		return nil, fmt.Errorf("failed to write merged JSONL: %w", err)
	}
	fmt.Printf("[compound] JSONL line-union merge: %d unique lines\n", len(merged))
	return result, nil
}
