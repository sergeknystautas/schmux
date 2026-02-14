package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/compound"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// handleOverlays returns overlay information for all repos.
func (s *Server) handleOverlays(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type PathInfo struct {
		Path   string `json:"path"`
		Source string `json:"source"` // "builtin", "global", or "repo"
		Status string `json:"status"` // "synced" or "pending"
	}

	type OverlayInfo struct {
		RepoName       string     `json:"repo_name"`
		Path           string     `json:"path"`
		Exists         bool       `json:"exists"`
		FileCount      int        `json:"file_count"`
		DeclaredPaths  []PathInfo `json:"declared_paths"`
		NudgeDismissed bool       `json:"nudge_dismissed"`
	}

	type Response struct {
		Overlays []OverlayInfo `json:"overlays"`
	}

	repos := s.config.GetRepos()
	overlays := make([]OverlayInfo, 0, len(repos))

	// Build lookup sets for source classification
	builtinSet := make(map[string]bool)
	for _, p := range config.DefaultOverlayPaths {
		builtinSet[p] = true
	}
	globalSet := make(map[string]bool)
	if s.config.Overlay != nil {
		for _, p := range s.config.Overlay.Paths {
			globalSet[p] = true
		}
	}

	for _, repo := range repos {
		overlayDir, err := workspace.OverlayDir(repo.Name)
		if err != nil {
			fmt.Printf("[workspace] failed to get overlay directory for %s: %v\n", repo.Name, err)
			continue
		}

		// Check if overlay directory exists
		exists := true
		if _, err := os.Stat(overlayDir); os.IsNotExist(err) {
			exists = false
		}

		// Count files if directory exists
		fileCount := 0
		if exists {
			files, err := workspace.ListOverlayFiles(repo.Name)
			if err != nil {
				fmt.Printf("[workspace] failed to list overlay files for %s: %v\n", repo.Name, err)
			} else {
				fileCount = len(files)
			}
		}

		// Get all declared paths for this repo and classify them
		declaredPaths := s.config.GetOverlayPaths(repo.Name)
		pathInfos := make([]PathInfo, 0, len(declaredPaths))
		for _, p := range declaredPaths {
			// Determine source (check in priority order)
			source := "repo"
			if builtinSet[p] {
				source = "builtin"
			} else if globalSet[p] {
				source = "global"
			}

			// Determine status
			status := "pending"
			if exists {
				filePath := filepath.Join(overlayDir, p)
				if _, err := os.Stat(filePath); err == nil {
					status = "synced"
				}
			}

			pathInfos = append(pathInfos, PathInfo{
				Path:   p,
				Source: source,
				Status: status,
			})
		}

		overlays = append(overlays, OverlayInfo{
			RepoName:       repo.Name,
			Path:           overlayDir,
			Exists:         exists,
			FileCount:      fileCount,
			DeclaredPaths:  pathInfos,
			NudgeDismissed: repo.OverlayNudgeDismissed,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(Response{Overlays: overlays})
}

// handleRefreshOverlay handles POST requests to refresh overlay files for a workspace.
func (s *Server) handleRefreshOverlay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract workspace ID from URL: /api/workspaces/:id/refresh-overlay
	workspaceID := strings.TrimPrefix(r.URL.Path, "/api/workspaces/")
	workspaceID = strings.TrimSuffix(workspaceID, "/refresh-overlay")
	if workspaceID == "" {
		http.Error(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermOperationTimeoutMs())*time.Millisecond)
	defer cancel()

	if err := s.workspace.RefreshOverlay(ctx, workspaceID); err != nil {
		fmt.Printf("[workspace] refresh-overlay error: workspace_id=%s error=%v\n", workspaceID, err)
		w.Header().Set("Content-Type", "application/json")
		// Return 400 for client errors (active sessions, not found)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	fmt.Printf("[workspace] refresh-overlay success: workspace_id=%s\n", workspaceID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleOverlayScan scans a workspace for gitignored files that could be added to the overlay.
func (s *Server) handleOverlayScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		WorkspaceID string `json:"workspace_id"`
		RepoName    string `json:"repo_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	ws, found := s.state.GetWorkspace(req.WorkspaceID)
	if !found {
		http.Error(w, "Workspace not found", http.StatusNotFound)
		return
	}

	// List gitignored files using git
	cmd := exec.CommandContext(r.Context(), "git", "ls-files", "--others", "--ignored", "--exclude-standard")
	cmd.Dir = ws.Path
	output, err := cmd.Output()
	if err != nil {
		http.Error(w, "Failed to scan workspace", http.StatusInternalServerError)
		return
	}

	// Well-known patterns for auto-detection
	wellKnown := map[string]bool{
		".env": true, ".env.local": true, ".envrc": true,
		".tool-versions": true, ".nvmrc": true, ".node-version": true,
		".python-version": true, ".ruby-version": true,
	}

	type Candidate struct {
		Path     string `json:"path"`
		Size     int64  `json:"size"`
		Detected bool   `json:"detected"`
	}

	var candidates []Candidate
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		info, err := os.Stat(filepath.Join(ws.Path, line))
		if err != nil || info.IsDir() {
			continue
		}
		candidates = append(candidates, Candidate{
			Path:     line,
			Size:     info.Size(),
			Detected: wellKnown[filepath.Base(line)] || wellKnown[line],
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"candidates": candidates})
}

// handleOverlayAdd copies files from a workspace to the overlay directory and updates config.
func (s *Server) handleOverlayAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		WorkspaceID string   `json:"workspace_id"`
		RepoName    string   `json:"repo_name"`
		Paths       []string `json:"paths"`
		CustomPaths []string `json:"custom_paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	ws, found := s.state.GetWorkspace(req.WorkspaceID)
	if !found {
		http.Error(w, "Workspace not found", http.StatusNotFound)
		return
	}

	// Validate repo name against configured repos
	repoFound := false
	for _, repo := range s.config.GetRepos() {
		if repo.Name == req.RepoName {
			repoFound = true
			break
		}
	}
	if !repoFound {
		http.Error(w, "Unknown repo", http.StatusBadRequest)
		return
	}

	overlayDir, err := workspace.OverlayDir(req.RepoName)
	if err != nil {
		http.Error(w, "Failed to resolve overlay dir", http.StatusInternalServerError)
		return
	}

	// Copy selected files from workspace to overlay dir
	var copied []string
	for _, relPath := range req.Paths {
		if err := compound.ValidateRelPath(relPath); err != nil {
			continue
		}
		srcPath := filepath.Join(ws.Path, relPath)
		dstPath := filepath.Join(overlayDir, relPath)
		os.MkdirAll(filepath.Dir(dstPath), 0755)

		content, err := os.ReadFile(srcPath)
		if err != nil {
			continue
		}
		if err := os.WriteFile(dstPath, content, 0644); err != nil {
			continue
		}
		copied = append(copied, relPath)
	}

	// Validate custom paths for path traversal
	var validCustom []string
	for _, p := range req.CustomPaths {
		if err := compound.ValidateRelPath(p); err != nil {
			continue
		}
		validCustom = append(validCustom, p)
	}

	// Add all paths (copied + custom) to repo config
	allNewPaths := make([]string, 0, len(copied)+len(validCustom))
	allNewPaths = append(allNewPaths, copied...)
	allNewPaths = append(allNewPaths, validCustom...)
	if len(allNewPaths) > 0 {
		s.config.AddRepoOverlayPaths(req.RepoName, allNewPaths)
		s.config.Save()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success":    true,
		"copied":     copied,
		"registered": allNewPaths,
	})
}
