package dashboard

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/state"
)

const maxWorkspaceAttachmentSize = 50 << 20

// handleWorkspaceAttachment saves one raw file body for the chat composer's
// Attach action. A unique directory preserves the filename without collisions.
func (h *WorkspaceHandlers) handleWorkspaceAttachment(w http.ResponseWriter, r *http.Request) {
	ws, found := h.state.GetWorkspace(chi.URLParam(r, "workspaceID"))
	if !found {
		writeJSONError(w, "workspace not found", http.StatusNotFound)
		return
	}
	if ws.RemoteHostID != "" {
		writeJSONError(w, "file attachments are not supported for remote workspaces", http.StatusBadRequest)
		return
	}
	if ws.Status == state.WorkspaceStatusDisposing || h.workspace.IsWorkspaceLocked(ws.ID) {
		writeJSONError(w, "workspace is busy", http.StatusConflict)
		return
	}
	name := r.URL.Query().Get("filename")
	if name == "" || name == "." || name == ".." || len(name) > 255 || strings.ContainsAny(name, `/\`) || strings.ContainsFunc(name, unicode.IsControl) {
		writeJSONError(w, "invalid filename", http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxWorkspaceAttachmentSize)
	defer r.Body.Close()

	// Anchor every filesystem operation to the workspace. String-prefix path
	// checks alone would allow .schmux symlinks to redirect writes outside it.
	root, err := os.OpenRoot(ws.Path)
	if err != nil {
		h.logger.Warn("open attachment workspace", "workspace", ws.ID, "err", err)
		writeJSONError(w, "cannot open workspace", http.StatusInternalServerError)
		return
	}
	defer root.Close()
	parent := filepath.Join(state.SchmuxDataDirRelative(ws.VCS), "attachments")
	if err := root.MkdirAll(parent, 0o700); err != nil {
		h.logger.Warn("create attachments directory", "workspace", ws.ID, "err", err)
		writeJSONError(w, "cannot create attachments directory", http.StatusInternalServerError)
		return
	}
	dir := filepath.Join(parent, uuid.NewString())
	if err := root.Mkdir(dir, 0o700); err != nil {
		h.logger.Warn("create attachment directory", "workspace", ws.ID, "err", err)
		writeJSONError(w, "cannot create attachment directory", http.StatusInternalServerError)
		return
	}
	saved := false
	defer func() {
		if !saved {
			if err := root.RemoveAll(dir); err != nil {
				h.logger.Warn("clean up failed attachment", "workspace", ws.ID, "err", err)
			}
		}
	}()
	upload, err := root.OpenRoot(dir)
	if err != nil {
		writeJSONError(w, "cannot open attachment directory", http.StatusInternalServerError)
		return
	}
	defer upload.Close()
	f, err := upload.OpenFile(".upload", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		writeJSONError(w, "cannot create attachment", http.StatusInternalServerError)
		return
	}
	_, copyErr := io.Copy(f, r.Body)
	closeErr := f.Close()
	if copyErr != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(copyErr, &tooLarge) {
			writeJSONError(w, "file exceeds 50 MiB", http.StatusRequestEntityTooLarge)
		} else {
			h.logger.Warn("receive attachment", "workspace", ws.ID, "err", copyErr)
			writeJSONError(w, "file upload failed", http.StatusBadRequest)
		}
		return
	}
	if closeErr != nil {
		h.logger.Warn("close attachment", "workspace", ws.ID, "err", closeErr)
		writeJSONError(w, "cannot save attachment", http.StatusInternalServerError)
		return
	}
	if err := upload.Rename(".upload", name); err != nil {
		h.logger.Warn("publish attachment", "workspace", ws.ID, "err", err)
		writeJSONError(w, "cannot save attachment", http.StatusInternalServerError)
		return
	}
	saved = true
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(contracts.WorkspaceAttachment{
		Name: name,
		Path: filepath.Join(ws.Path, dir, name),
	})
}
