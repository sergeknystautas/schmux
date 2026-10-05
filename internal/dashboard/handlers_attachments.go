package dashboard

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/attachment"
	"github.com/sergeknystautas/schmux/internal/state"
)

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
	if !attachment.ValidName(name) {
		writeJSONError(w, "invalid filename", http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, attachment.MaxSize)
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
	rel, err := attachment.Save(root, filepath.Join(state.SchmuxDataDirRelative(ws.VCS), "attachments"), name, r.Body)
	if err != nil {
		writeAttachmentSaveError(w, h.logger, ws.ID, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(contracts.WorkspaceAttachment{
		Name: name,
		Path: filepath.Join(ws.Path, rel),
	})
}

// writeAttachmentSaveError maps an attachment.Save failure to the responses
// both attachment endpoints share. owner names the workspace or staging area.
func writeAttachmentSaveError(w http.ResponseWriter, logger *log.Logger, owner string, err error) {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		writeJSONError(w, "file exceeds 50 MiB", http.StatusRequestEntityTooLarge)
	case errors.Is(err, attachment.ErrReceive):
		logger.Warn("receive attachment", "owner", owner, "err", err)
		writeJSONError(w, "file upload failed", http.StatusBadRequest)
	default:
		logger.Warn("save attachment", "owner", owner, "err", err)
		writeJSONError(w, "cannot save attachment", http.StatusInternalServerError)
	}
}
