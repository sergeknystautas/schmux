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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"prompt": CommitPrompt()})
}

// handleCommitGenerate handles POST /api/commit/generate.
// Generates a commit message by running oneshot with the commit prompt.
func (s *Server) handleCommitGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req CommitMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	if req.WorkspaceID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "workspace_id is required"})
		return
	}

	ws, ok := s.state.GetWorkspace(req.WorkspaceID)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "workspace not found"})
		return
	}

	// Run git diff HEAD --numstat to get file stats for response (staged + unstaged)
	ctx := r.Context()
	numstatCmd := exec.CommandContext(ctx, "git", "diff", "HEAD", "--numstat")
	numstatCmd.Dir = ws.Path
	numstatOutput, err := numstatCmd.CombinedOutput()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Printf("[commit-generate] git diff --numstat failed: %s\n", string(numstatOutput))
		json.NewEncoder(w).Encode(map[string]string{"error": "git operation failed"})
		return
	}
	files := parseNumstat(string(numstatOutput))

	// Run git diff HEAD to get actual diff for prompt (staged + unstaged)
	diffCmd := exec.CommandContext(ctx, "git", "diff", "HEAD")
	diffCmd.Dir = ws.Path
	diffOutput, err := diffCmd.CombinedOutput()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Printf("[commit-generate] git diff failed: %s\n", string(diffOutput))
		json.NewEncoder(w).Encode(map[string]string{"error": "git operation failed"})
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
		fmt.Printf("[commit-generate] %s - not configured\n", req.WorkspaceID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "No commit_message target configured. Select a model in Settings > Code Review."})
		return
	}

	fmt.Printf("[commit-generate] %s - asking %s\n", req.WorkspaceID, targetName)
	start := time.Now()

	timeout := 60 * time.Second
	rawResult, err := oneshot.ExecuteTarget(ctx, s.config, targetName, prompt, schema.LabelCommitMessage, timeout, ws.Path)
	if err != nil {
		fmt.Printf("[commit-generate] %s - failed: %v\n", req.WorkspaceID, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("oneshot failed: %v", err)})
		return
	}

	var result commitmessage.Result
	if err := json.Unmarshal([]byte(rawResult), &result); err != nil {
		fmt.Printf("[commit-generate] %s - failed to parse response: %v\n", req.WorkspaceID, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to parse response: %v", err)})
		return
	}

	fmt.Printf("[commit-generate] %s - completed in %s\n", req.WorkspaceID, time.Since(start))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(CommitMessageResponse{
		Message: strings.TrimSpace(result.Message),
		Files:   files,
	})
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
