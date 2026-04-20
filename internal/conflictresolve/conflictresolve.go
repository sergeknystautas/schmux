package conflictresolve

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/schema"
)

func init() {
	// Register the OneshotResult type for JSON schema generation.
	schema.Register(schema.LabelConflictResolve, OneshotResult{})
}

// executorFunc is the function used to run a oneshot target. Package-level var for testability.
var executorFunc = oneshot.ExecuteTarget

// FileAction describes what the LLM did to resolve a single conflicted file.
// Struct tags control JSON schema generation via swaggest/jsonschema-go.
type FileAction struct {
	Action      string   `json:"action" required:"true"`      // "modified" or "deleted"
	Description string   `json:"description" required:"true"` // per-file explanation
	_           struct{} `additionalProperties:"false"`
}

// OneshotResult is the parsed response from a conflict resolution one-shot call.
// Struct tags control JSON schema generation via swaggest/jsonschema-go.
type OneshotResult struct {
	AllResolved bool                  `json:"all_resolved" required:"true"`
	Confidence  string                `json:"confidence" required:"true"`
	Summary     string                `json:"summary" required:"true"`
	Files       map[string]FileAction `json:"files" required:"true" nullable:"false"`
	_           struct{}              `additionalProperties:"false"`
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
  "summary": "Detailed explanation of what the conflict was, the approach taken to resolve it, and any concerns or trade-offs involved.\nInclude specifics about what each side was trying to do and how you merged them.\nUse \\n newlines to separate paragraphs or logical sections for readability.",
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
- The "summary" field should use \n newlines to separate paragraphs or sections for readability
- Do NOT include any text outside the JSON object
- Output MUST be valid JSON only
`)

	return b.String()
}

// Execute runs the conflict resolution one-shot call against the configured target.
// The workspacePath sets the working directory for the oneshot process so the LLM
// agent can read and edit the conflicted files.
// The second return value is the raw LLM response text, returned on parse errors
// so callers can surface it to the user. It is empty when the error is pre-parse
// (e.g. disabled, target not found, execution failure) or on success.
//
// Envelope handling note: oneshot.Execute already strips the Claude
// `structured_output` / `result` envelope before returning, so oneshot.ParseJSON
// only ever sees the inner payload. If a non-Claude tool ever produces a
// Claude-shaped envelope, parsing would fail here — the upstream stripper
// (internal/oneshot/oneshot.go parseClaudeStructuredOutput) is the single
// source of truth.
//
// Error sentinels surfaced (all from internal/oneshot):
//   - oneshot.ErrDisabled          (no conflict_resolve target configured)
//   - oneshot.ErrTargetNotFound    (configured target missing)
//   - oneshot.ErrInvalidResponse   (LLM output not parseable as OneshotResult)
func Execute(ctx context.Context, cfg *config.Config, prompt string, workspacePath string) (OneshotResult, string, error) {
	targetName := cfg.GetConflictResolveTarget()
	timeout := time.Duration(cfg.GetConflictResolveTimeoutMs()) * time.Millisecond

	response, err := executorFunc(ctx, cfg, targetName, prompt, schema.LabelConflictResolve, timeout, workspacePath)
	if err != nil {
		return OneshotResult{}, "", err
	}

	result, err := oneshot.ParseJSON[OneshotResult](response)
	if err != nil {
		return OneshotResult{}, response, err
	}

	// Validate required fields beyond JSON structure: confidence must be populated.
	if result.Confidence == "" {
		return OneshotResult{}, response, fmt.Errorf("%w: response JSON does not contain expected fields", oneshot.ErrInvalidResponse)
	}

	normalizeSummary(&result)
	return result, "", nil
}

// normalizeSummary replaces literal "\n" text (which LLMs often produce in JSON
// strings instead of actual newlines) with real newlines.
func normalizeSummary(r *OneshotResult) {
	r.Summary = strings.ReplaceAll(r.Summary, `\n`, "\n")
	for k, f := range r.Files {
		f.Description = strings.ReplaceAll(f.Description, `\n`, "\n")
		r.Files[k] = f
	}
}
