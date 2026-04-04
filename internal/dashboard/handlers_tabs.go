package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sergeknystautas/schmux/internal/state"
)

// allowedClientTabKinds lists the tab kinds that clients are permitted to create.
var allowedClientTabKinds = map[string]bool{
	"markdown": true,
	"commit":   true,
}

type createTabRequest struct {
	Kind     string            `json:"kind"`
	Label    string            `json:"label"`
	Route    string            `json:"route"`
	Closable bool              `json:"closable"`
	Meta     map[string]string `json:"meta,omitempty"`
}

func (s *Server) handleTabCreate(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceID")

	var req createTabRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if !allowedClientTabKinds[req.Kind] {
		http.Error(w, fmt.Sprintf("tab kind %q not allowed for client creation", req.Kind), http.StatusBadRequest)
		return
	}

	tab := state.Tab{
		ID:        uuid.NewString(),
		Kind:      req.Kind,
		Label:     req.Label,
		Route:     req.Route,
		Closable:  req.Closable,
		Meta:      req.Meta,
		CreatedAt: time.Now(),
	}

	if err := s.state.AddTab(workspaceID, tab); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	s.state.Save() //nolint:errcheck
	go s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": tab.ID, "status": "ok"}) //nolint:errcheck
}

func (s *Server) handleTabDelete(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceID")
	tabID := chi.URLParam(r, "tabID")

	// Check tab exists and is closable.
	tabs := s.state.GetWorkspaceTabs(workspaceID)
	var found *state.Tab
	for i := range tabs {
		if tabs[i].ID == tabID {
			found = &tabs[i]
			break
		}
	}
	if found == nil {
		http.Error(w, "tab not found", http.StatusNotFound)
		return
	}
	closable := found.Closable
	if found.Kind == "resolve-conflict" {
		hash := found.Meta["hash"]
		if hash != "" {
			// Use the in-memory state as source of truth: it's updated by
			// Finish() before the persisted record, so it always reflects the
			// current goroutine status.  If no in-memory state exists (daemon
			// restarted, or goroutine already completed and was cleaned up),
			// allow closing — there is no running goroutine to protect.
			if crState := s.getLinearSyncResolveConflictState(workspaceID); crState != nil {
				snapshot := crState.Snapshot()
				if snapshot.Hash == hash {
					closable = snapshot.Status != "in_progress"
				}
				// in-memory state exists but for a different hash — allow close
			}
			// no in-memory state — allow close (no running goroutine)
		}
	}
	if !closable {
		http.Error(w, "tab is not closable", http.StatusBadRequest)
		return
	}

	// For preview tabs, cascade to preview manager.
	if found.Kind == "preview" && found.Meta["preview_id"] != "" && s.previewManager != nil {
		s.previewManager.Delete(workspaceID, found.Meta["preview_id"]) //nolint:errcheck
	}

	if err := s.state.RemoveTab(workspaceID, tabID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if found.Kind == "resolve-conflict" && found.Meta["hash"] != "" {
		if err := s.state.RemoveResolveConflict(workspaceID, found.Meta["hash"]); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if current := s.getLinearSyncResolveConflictState(workspaceID); current != nil &&
			current.Hash == found.Meta["hash"] &&
			current.Status != "in_progress" {
			s.deleteLinearSyncResolveConflictState(workspaceID)
		}
	}

	s.state.Save() //nolint:errcheck
	go s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}
