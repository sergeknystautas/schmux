package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/commitmessage"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/schema"
)

// CommitMessageRequest is the request body for POST /api/commit/generate.
type CommitMessageRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

// CommitFile represents a file in the commit with stats.
type CommitFile struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
}

// CommitMessageResponse is the response body for POST /api/commit/generate.
type CommitMessageResponse struct {
	Message string       `json:"message"`
	Files   []CommitFile `json:"files"`
}

// CommitPrompt returns the base prompt template for generating commit messages.
// This is used by both oneshot and sessions. Sessions add pre-commit instructions.
func CommitPrompt() string {
	return `Please create a thorough git commit message for these files.

Do not include the generated or co-authored lines in your response.

Keep the message focused on the features and user-facing changes, not just code changes.`
}

// BuildOneshotCommitPrompt builds the oneshot commit prompt with diff output.
func BuildOneshotCommitPrompt(diff string) string {
	return CommitPrompt() + "\n\nOutput only the commit message, no preamble or explanation.\n\n" + diff
}

// handleCommitPrompt handles GET /api/commit/prompt.
// Returns the prompt template for generating commit messages.
func (s *Server) handleCommitPrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"prompt": CommitPrompt()}); err != nil {
		s.logger.Error("failed to encode response", "handler", "commit-prompt", "err", err)
	}
}

// handleCommitGenerate handles POST /api/commit/generate.
// Generates a commit message by running oneshot with the commit prompt.
func (s *Server) handleCommitGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req CommitMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.WorkspaceID == "" {
		writeJSONError(w, "workspace_id is required", http.StatusBadRequest)
		return
	}

	ws, ok := s.state.GetWorkspace(req.WorkspaceID)
	if !ok {
		writeJSONError(w, "workspace not found", http.StatusNotFound)
		return
	}

	// Run git diff HEAD --numstat to get file stats for response (staged + unstaged)
	ctx := r.Context()
	numstatCmd := exec.CommandContext(ctx, "git", "diff", "HEAD", "--numstat")
	numstatCmd.Dir = ws.Path
	numstatOutput, err := numstatCmd.CombinedOutput()
	if err != nil {
		s.logger.Error("git diff --numstat failed", "output", string(numstatOutput))
		writeJSONError(w, "git operation failed", http.StatusInternalServerError)
		return
	}
	files := parseNumstat(string(numstatOutput))

	// Run git diff HEAD to get actual diff for prompt (staged + unstaged)
	diffCmd := exec.CommandContext(ctx, "git", "diff", "HEAD")
	diffCmd.Dir = ws.Path
	diffOutput, err := diffCmd.CombinedOutput()
	if err != nil {
		s.logger.Error("git diff failed", "output", string(diffOutput))
		writeJSONError(w, "git operation failed", http.StatusInternalServerError)
		return
	}

	// Cap diff output to prevent unbounded memory usage in prompt
	const maxDiffSize = 100 * 1024 // 100KB
	diffStr := string(diffOutput)
	if len(diffStr) > maxDiffSize {
		diffStr = diffStr[:maxDiffSize] + "\n\n... (diff truncated)"
	}

	// Build the prompt
	prompt := BuildOneshotCommitPrompt(diffStr)

	// Check if commit message target is configured
	targetName := s.config.GetCommitMessageTarget()
	if targetName == "" {
		s.logger.Info("commit-generate: not configured", "workspace", req.WorkspaceID)
		writeJSONError(w, "No commit_message target configured. Select a model in Settings > Code Review.", http.StatusBadRequest)
		return
	}

	s.logger.Info("commit-generate: asking target", "workspace", req.WorkspaceID, "target", targetName)
	start := time.Now()

	timeout := 60 * time.Second
	rawResult, err := oneshot.ExecuteTarget(ctx, s.config, targetName, prompt, schema.LabelCommitMessage, timeout, ws.Path)
	if err != nil {
		s.logger.Error("commit-generate: failed", "workspace", req.WorkspaceID, "err", err)
		writeJSONError(w, fmt.Sprintf("oneshot failed: %v", err), http.StatusInternalServerError)
		return
	}

	var result commitmessage.Result
	if err := json.Unmarshal([]byte(rawResult), &result); err != nil {
		s.logger.Error("commit-generate: failed to parse response", "workspace", req.WorkspaceID, "err", err)
		writeJSONError(w, fmt.Sprintf("failed to parse response: %v", err), http.StatusInternalServerError)
		return
	}

	s.logger.Info("commit-generate: completed", "workspace", req.WorkspaceID, "elapsed", time.Since(start))

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(CommitMessageResponse{
		Message: strings.TrimSpace(result.Message),
		Files:   files,
	}); err != nil {
		s.logger.Error("failed to encode response", "handler", "commit-generate", "err", err)
	}
}

// parseNumstat parses git diff --numstat output into CommitFile structs.
func parseNumstat(output string) []CommitFile {
	var files []CommitFile
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// numstat format: "added\tdeleted\tpath"
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			continue
		}
		var added, deleted int
		fmt.Sscanf(parts[0], "%d", &added)
		fmt.Sscanf(parts[1], "%d", &deleted)
		files = append(files, CommitFile{
			Path:    parts[2],
			Added:   added,
			Deleted: deleted,
		})
	}
	return files
}
