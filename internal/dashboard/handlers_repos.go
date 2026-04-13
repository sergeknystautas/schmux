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

	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// LocalRepo represents a repository discovered on the local filesystem.
type LocalRepo struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	VCS       string `json:"vcs"` // "git" or "sapling"
	RemoteURL string `json:"remote_url,omitempty"`
}

// skipDirs are directory names to skip at any depth during scanning.
var skipDirs = map[string]bool{
	// OS-managed
	"Library":      true,
	"Applications": true,
	"Music":        true,
	"Pictures":     true,
	"Movies":       true,
	// Build artifacts
	"node_modules": true,
	"vendor":       true,
	"target":       true,
	"build":        true,
	"dist":         true,
}

// scanLocalRepos walks homeDir to depth 2, discovering git and sapling repos.
// It respects context cancellation and returns whatever was found so far.
func scanLocalRepos(ctx context.Context, homeDir string) []LocalRepo {
	var repos []LocalRepo

	// Read depth-0 children (depth 1 entries)
	depth1Entries, err := os.ReadDir(homeDir)
	if err != nil {
		return repos
	}

	for _, d1 := range depth1Entries {
		if ctx.Err() != nil {
			return repos
		}

		name := d1.Name()
		if shouldSkip(name) {
			continue
		}

		// At depth 1, follow symlinks to resolve the entry type
		d1Path := filepath.Join(homeDir, name)
		info, err := os.Stat(d1Path) // Stat follows symlinks
		if err != nil || !info.IsDir() {
			continue
		}

		// Check if this depth-1 entry is itself a repo
		if vcs := detectVCS(d1Path); vcs != "" {
			repos = append(repos, makeLocalRepo(ctx, name, d1Path, vcs))
			continue // Do not recurse into repos
		}

		// Read depth-1 children (depth 2 entries)
		depth2Entries, err := os.ReadDir(d1Path)
		if err != nil {
			continue
		}

		for _, d2 := range depth2Entries {
			if ctx.Err() != nil {
				return repos
			}

			d2Name := d2.Name()
			if shouldSkip(d2Name) {
				continue
			}

			// At depth 2, do NOT follow symlinks
			if d2.Type()&os.ModeSymlink != 0 {
				continue
			}
			if !d2.IsDir() {
				continue
			}

			d2Path := filepath.Join(d1Path, d2Name)
			if vcs := detectVCS(d2Path); vcs != "" {
				repos = append(repos, makeLocalRepo(ctx, d2Name, d2Path, vcs))
			}
		}
	}

	return repos
}

// shouldSkip returns true if the directory name should be skipped.
func shouldSkip(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	return skipDirs[name]
}

// detectVCS checks if the directory contains VCS markers.
// Sapling uses .sl/ in newer versions and .hg/ for historical compatibility.
// os.Stat follows symlinks, so .hg -> symlinked dir works correctly.
func detectVCS(dir string) string {
	if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil && info.IsDir() {
		return "git"
	}
	for _, marker := range []string{".sl", ".hg"} {
		if info, err := os.Stat(filepath.Join(dir, marker)); err == nil && info.IsDir() {
			return "sapling"
		}
	}
	return ""
}

// makeLocalRepo creates a LocalRepo, extracting the remote URL for git repos.
func makeLocalRepo(ctx context.Context, name, path, vcs string) LocalRepo {
	repo := LocalRepo{
		Name: name,
		Path: path,
		VCS:  vcs,
	}

	if vcs == "git" {
		// Best-effort remote URL extraction — don't block on errors
		cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		cmd := exec.CommandContext(cmdCtx, "git", "-C", path, "remote", "get-url", "origin")
		if out, err := cmd.Output(); err == nil {
			repo.RemoteURL = strings.TrimSpace(string(out))
		}
	}

	return repo
}

// handleWorkspacesScan scans the workspace directory and reconciles with state.
func (h *WorkspaceHandlers) handleWorkspacesScan(w http.ResponseWriter, r *http.Request) {
	result, err := h.workspace.Scan()
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to scan workspaces: %v", err), http.StatusInternalServerError)
		return
	}
	if h.previewManager != nil {
		previewLog := logging.Sub(h.logger, "preview")
		for _, removed := range result.Removed {
			if err := h.previewManager.DeleteWorkspace(removed.ID); err != nil {
				previewLog.Warn("scan cleanup failed", "workspace_id", removed.ID, "err", err)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		h.logger.Error("failed to encode response", "handler", "workspaces-scan", "err", err)
	}
}

// handleScanRepos scans the user's home directory for local repositories.
func (h *WorkspaceHandlers) handleScanRepos(w http.ResponseWriter, r *http.Request) {
	home, err := os.UserHomeDir()
	if err != nil {
		writeJSONError(w, "Failed to determine home directory", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	repos := scanLocalRepos(ctx, home)
	if repos == nil {
		repos = []LocalRepo{} // Return [] not null
	}
	writeJSON(w, repos)
}

// handleProbeRepo probes a repository URL for accessibility.
func (h *WorkspaceHandlers) handleProbeRepo(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.URL == "" {
		writeJSONError(w, "url is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	result := workspace.ProbeRepoAccess(ctx, req.URL)
	writeJSON(w, result)
}
