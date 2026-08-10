package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/vcs"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// stripNullByteContent blanks content that looks binary (a NUL byte in the
// first 8KB), matching the behavior of the old commit-detail getFileAtCommit.
func stripNullByteContent(s string) string {
	checkLen := len(s)
	if checkLen > 8192 {
		checkLen = 8192
	}
	for i := 0; i < checkLen; i++ {
		if s[i] == 0 {
			return ""
		}
	}
	return s
}

// buildDiffFileContent fetches one file's old/new content.
//
// commit == "": old side = ShowFile(oldPath||path, "HEAD"), new side =
// working-tree read via readFile. commit != "": old side =
// ShowFile(oldPath||path, commit+"^"), new side = ShowFile(path, commit),
// both null-byte-sniffed (working-tree binary files are filtered client-side
// via the list's is_binary flag, so no sniff is needed there).
//
// A side that errors yields empty content by design — added files have no
// old side, deleted files no new side; the client interprets emptiness via
// the status it already has from the list.
func buildDiffFileContent(run vcsRunFunc, readFile readFileFunc, cb vcs.CommandBuilder, path, oldPath, commit string) (string, string) {
	oldSide := oldPath
	if oldSide == "" {
		oldSide = path
	}
	oldRev := "HEAD"
	if commit != "" {
		oldRev = commit + "^"
	}

	oldContent := ""
	if oldSide != "" {
		oldContent, _ = run(cb.ShowFile(oldSide, oldRev))
	}

	newContent := ""
	if path != "" {
		if commit != "" {
			newContent, _ = run(cb.ShowFile(path, commit))
		} else {
			newContent = readFile(path)
		}
	}

	if commit != "" {
		oldContent = stripNullByteContent(oldContent)
		newContent = stripNullByteContent(newContent)
	}
	return capContent(oldContent), capContent(newContent)
}

// handleDiffFile handles GET /api/diff-file/{workspaceId}?path=&old_path=&commit=.
// It serves one file's old/new content for the diff and commit-detail pages;
// the list endpoints (/api/diff, commit-detail) carry only metadata.
func (h *GitHandlers) handleDiffFile(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "*")
	if workspaceID == "" {
		writeJSONError(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	q := r.URL.Query()
	path := q.Get("path")
	oldPath := q.Get("old_path")
	commit := q.Get("commit")

	if path == "" && oldPath == "" {
		writeJSONError(w, "path or old_path is required", http.StatusBadRequest)
		return
	}
	if commit != "" {
		if err := workspace.ValidateCommitHash(commit); err != nil {
			writeJSONError(w, "invalid commit hash", http.StatusBadRequest)
			return
		}
	}

	ws, found := h.state.GetWorkspace(workspaceID)
	if !found {
		writeJSONError(w, "workspace not found", http.StatusNotFound)
		return
	}

	// Block path traversal on both sides. For local workspaces also verify
	// the resolved path stays inside the workspace root.
	for _, p := range []string{path, oldPath} {
		if p == "" {
			continue
		}
		if strings.Contains(p, "..") {
			writeJSONError(w, "invalid file path", http.StatusForbidden)
			return
		}
		if ws.RemoteHostID == "" && !isPathWithinDir(filepath.Join(ws.Path, p), ws.Path) {
			writeJSONError(w, "invalid file path", http.StatusForbidden)
			return
		}
	}

	if ws.RemoteHostID != "" {
		h.handleRemoteDiffFile(w, r, ws, path, oldPath, commit)
		return
	}

	cb := vcs.NewCommandBuilder(h.vcsTypeForWorkspace(ws))
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(h.config.GetGitStatusTimeoutMs())*time.Millisecond)
	defer cancel()
	run := localShellRun(ctx, ws.Path)
	readFile := func(p string) string { return readWorkingFile(ws.Path, p) }

	oldContent, newContent := buildDiffFileContent(run, readFile, cb, path, oldPath, commit)
	writeDiffFileResponse(w, h, ws, path, oldPath, oldContent, newContent)
}

// handleRemoteDiffFile fetches both sides in a single RunCommand (each
// RunCommand is a ~1.5s tmux round-trip).
func (h *GitHandlers) handleRemoteDiffFile(w http.ResponseWriter, r *http.Request, ws state.Workspace, path, oldPath, commit string) {
	if h.remoteManager == nil {
		writeJSONError(w, "remote manager not available", http.StatusServiceUnavailable)
		return
	}
	conn := h.remoteManager.GetConnection(ws.RemoteHostID)
	if conn == nil || !conn.IsConnected() {
		writeJSONError(w, "remote host not connected", http.StatusServiceUnavailable)
		return
	}

	cb := vcs.NewCommandBuilder(h.vcsTypeForWorkspace(ws))

	oldSide := oldPath
	if oldSide == "" {
		oldSide = path
	}
	oldRev := "HEAD"
	if commit != "" {
		oldRev = commit + "^"
	}

	var parts []string
	hasOld := oldSide != ""
	hasNew := path != ""
	if hasOld {
		parts = append(parts, cb.ShowFile(oldSide, oldRev))
	}
	if hasNew {
		if commit != "" {
			parts = append(parts, cb.ShowFile(path, commit))
		} else {
			parts = append(parts, cb.FileContent(path))
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	out, err := conn.RunCommand(ctx, ws.RemotePath, strings.Join(parts, "; echo "+remoteDiffListDelim+"; "))
	if err != nil {
		h.logger.Error("remote diff-file failed", "err", err)
		writeJSONError(w, `{"error":"remote diff-file failed"}`, http.StatusInternalServerError)
		return
	}

	sections := strings.Split(out, remoteDiffListDelim)
	oldContent, newContent := "", ""
	idx := 0
	if hasOld {
		if idx < len(sections) {
			oldContent = strings.TrimSpace(sections[idx])
		}
		idx++
	}
	if hasNew {
		if idx < len(sections) {
			newContent = strings.TrimSpace(sections[idx])
		}
	}
	if commit != "" {
		oldContent = stripNullByteContent(oldContent)
		newContent = stripNullByteContent(newContent)
	}
	writeDiffFileResponse(w, h, ws, path, oldPath, capContent(oldContent), capContent(newContent))
}

// writeDiffFileResponse writes the JSON response for both local and remote
// diff-file handlers. The echoed path is the new-side path, falling back to
// old_path for deleted files.
func writeDiffFileResponse(w http.ResponseWriter, h *GitHandlers, ws state.Workspace, path, oldPath, oldContent, newContent string) {
	echoPath := path
	if echoPath == "" {
		echoPath = oldPath
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(contracts.DiffFileContentResponse{
		WorkspaceID: ws.ID,
		Path:        echoPath,
		OldContent:  oldContent,
		NewContent:  newContent,
	}); err != nil {
		h.logger.Error("failed to encode response", "handler", "diff-file", "err", err)
	}
}
