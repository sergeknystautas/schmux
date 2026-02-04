package conflictresolve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/oneshot"
)

var (
	ErrDisabled        = errors.New("conflict resolve is disabled")
	ErrTargetNotFound  = errors.New("conflict resolve target not found")
	ErrInvalidResponse = errors.New("invalid conflict resolve response")
)

// FileAction describes what the LLM did to resolve a single conflicted file.
type FileAction struct {
	Action      string `json:"action"`                // "modified" or "deleted"
	Description string `json:"description,omitempty"` // optional per-file explanation
}

// OneshotResult is the parsed response from a conflict resolution one-shot call.
type OneshotResult struct {
	AllResolved bool                  `json:"all_resolved"`
	Confidence  string                `json:"confidence"`
	Summary     string                `json:"summary"`
	Files       map[string]FileAction `json:"files"`
}

// BuildPrompt constructs the prompt for a conflict resolution one-shot call.
// The LLM is expected to read and edit the conflicted files in-place at the
// given workspace path, then report back what it did via JSON.
func BuildPrompt(workspacePath, defaultBranchHash, localCommitHash, localCommitMessage string, conflictedFiles []string) string {
	var b strings.Builder

	b.WriteString("You are resolving a git rebase conflict.\n\n")
	b.WriteString("One commit from the default branch is being rebased. During replay of a local\n")
	b.WriteString("commit, git produced conflicts in the files listed below.\n\n")
	b.WriteString(fmt.Sprintf("Workspace path: %s\n", workspacePath))
	b.WriteString(fmt.Sprintf("Default branch commit: %s\n", defaultBranchHash))
	b.WriteString(fmt.Sprintf("Local commit being replayed: %s %q\n\n", localCommitHash, localCommitMessage))
	b.WriteString("Conflicted files:\n")

	// Sort file paths for deterministic prompt ordering
	sorted := make([]string, len(conflictedFiles))
	copy(sorted, conflictedFiles)
	sort.Strings(sorted)

	for _, path := range sorted {
		b.WriteString(fmt.Sprintf("  - %s\n", path))
	}

	b.WriteString(`
Instructions:
1. Read each conflicted file (they contain <<<<<<< / ======= / >>>>>>> markers).
2. Resolve the conflict so the intent of BOTH sides is preserved.
3. Write the resolved contents back to the file (or delete the file if the
   correct resolution is removal).
4. Return ONLY a JSON object describing what you did.

Expected JSON format:

{
  "all_resolved": true,
  "confidence": "high",
  "summary": "Brief description of what you did",
  "files": {
    "path/to/file.go": {"action": "modified", "description": "Merged both changes"},
    "path/to/obsolete.go": {"action": "deleted", "description": "File was removed by incoming commit"}
  }
}

Rules:
- "all_resolved" must be true only if you resolved ALL conflicts in ALL files
- "confidence" must be "high", "medium", or "low"
- "files" must have an entry for every conflicted file listed above
- Each file entry must have "action" set to "modified" or "deleted"
- Each file entry must include "description"
- If "modified", the file on disk must contain the resolved contents with NO conflict markers
- If "deleted", you must have deleted the file from disk
- The "action" field is used to stage changes: "modified" -> git add, "deleted" -> git rm
- Do NOT include any text outside the JSON object
- Output MUST be valid JSON only
`)

	return b.String()
}

// Execute runs the conflict resolution one-shot call against the configured target.
// The workspacePath sets the working directory for the oneshot process so the LLM
// agent can read and edit the conflicted files.
func Execute(ctx context.Context, cfg *config.Config, prompt string, workspacePath string) (OneshotResult, error) {
	targetName := cfg.GetConflictResolveTarget()
	if targetName == "" {
		return OneshotResult{}, ErrDisabled
	}

	timeout := time.Duration(cfg.GetConflictResolveTimeoutMs()) * time.Millisecond

	response, err := oneshot.ExecuteTarget(ctx, cfg, targetName, prompt, oneshot.SchemaConflictResolve, timeout, workspacePath)
	if err != nil {
		if errors.Is(err, oneshot.ErrTargetNotFound) {
			return OneshotResult{}, ErrTargetNotFound
		}
		return OneshotResult{}, fmt.Errorf("oneshot execute: %w", err)
	}

	result, err := ParseResult(response)
	if err != nil {
		return OneshotResult{}, err
	}

	return result, nil
}

// ParseResult parses a JSON response from the LLM.
func ParseResult(raw string) (OneshotResult, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return OneshotResult{}, ErrInvalidResponse
	}

	var result OneshotResult
	if err := json.Unmarshal([]byte(trimmed), &result); err != nil {
		return OneshotResult{}, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}

	return result, nil
}
