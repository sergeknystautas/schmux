package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/sergeknystautas/schmux/internal/state"
)

type createTabRequest struct {
	Kind     string `json:"kind"`
	Hash     string `json:"hash,omitempty"`
	Filepath string `json:"filepath,omitempty"`
}

func (h *WorkspaceHandlers) handleTabCreate(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceID")

	var req createTabRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	var (
		tab *state.Tab
		err error
	)
	switch req.Kind {
	case "commit":
		if req.Hash == "" {
			writeJSONError(w, "hash is required for commit tabs", http.StatusBadRequest)
			return
		}
		tab, err = h.workspace.OpenCommitTab(workspaceID, req.Hash)
	case "markdown":
		if req.Filepath == "" {
			writeJSONError(w, "filepath is required for markdown tabs", http.StatusBadRequest)
			return
		}
		tab, err = h.workspace.OpenMarkdownTab(workspaceID, req.Filepath)
	default:
		writeJSONError(w, fmt.Sprintf("tab kind %q not supported", req.Kind), http.StatusBadRequest)
		return
	}

	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
		"id":     tab.ID,
		"route":  tab.Route,
		"status": "ok",
	})
}

func (h *WorkspaceHandlers) handleTabDelete(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceID")
	tabID := chi.URLParam(r, "tabID")

	if err := h.workspace.CloseTab(workspaceID, tabID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeJSONError(w, err.Error(), http.StatusNotFound)
		} else if strings.Contains(err.Error(), "not closable") {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
		} else {
			writeJSONError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}
