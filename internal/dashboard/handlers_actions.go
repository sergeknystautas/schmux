package dashboard

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/sergeknystautas/schmux/internal/actions"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// validateActionRepo is middleware that validates the repo URL parameter.
func validateActionRepo(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		repo := chi.URLParam(r, "repo")
		if repo == "" || strings.ContainsAny(repo, "/\\.\x00") || len(repo) > 128 {
			http.Error(w, "invalid repo name", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleListActions returns pinned actions for a repo.
func (s *Server) handleListActions(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")
	registry := s.getOrCreateRegistry(repo)

	actions := registry.List(contracts.ActionStatePinned)
	if actions == nil {
		actions = []contracts.Action{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(contracts.ActionRegistryResponse{Actions: actions})
}

// handleCreateAction creates a new manual action.
func (s *Server) handleCreateAction(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")
	registry := s.getOrCreateRegistry(repo)

	var req contracts.CreateActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.Type == "" {
		http.Error(w, "type is required", http.StatusBadRequest)
		return
	}

	action, err := registry.Create(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(action)
}

// handleUpdateAction updates an existing action.
func (s *Server) handleUpdateAction(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")
	id := chi.URLParam(r, "id")
	registry := s.getOrCreateRegistry(repo)

	var req contracts.UpdateActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := registry.Update(id, req); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	action, _ := registry.Get(id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(action)
}

// handleDeleteAction hard-deletes an action.
func (s *Server) handleDeleteAction(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")
	id := chi.URLParam(r, "id")
	registry := s.getOrCreateRegistry(repo)

	if err := registry.Delete(id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// handlePinAction transitions a proposed action to pinned.
func (s *Server) handlePinAction(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")
	id := chi.URLParam(r, "id")
	registry := s.getOrCreateRegistry(repo)

	if err := registry.Pin(id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "pinned"})
}

// handleDismissAction transitions an action to dismissed.
func (s *Server) handleDismissAction(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")
	id := chi.URLParam(r, "id")
	registry := s.getOrCreateRegistry(repo)

	if err := registry.Dismiss(id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "dismissed"})
}

// handleListProposed returns proposed actions for a repo (for the lore page).
func (s *Server) handleListProposed(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")
	registry := s.getOrCreateRegistry(repo)

	actions := registry.List(contracts.ActionStateProposed)
	if actions == nil {
		actions = []contracts.Action{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(contracts.ActionRegistryResponse{Actions: actions})
}

// handlePromptHistory returns past prompts from event JSONL files.
func (s *Server) handlePromptHistory(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")

	// Collect workspace paths for this repo.
	var paths []string
	for _, ws := range s.state.GetWorkspaces() {
		if ws.Repo == repo && ws.Path != "" {
			paths = append(paths, ws.Path)
		}
	}

	prompts := actions.CollectPromptHistory(paths, 100)
	if prompts == nil {
		prompts = []contracts.PromptHistoryEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(contracts.PromptHistoryResponse{Prompts: prompts})
}
