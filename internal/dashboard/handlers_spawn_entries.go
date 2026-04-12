package dashboard

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/spawn"
)

// extractSkillDescription pulls the description field from skill file YAML frontmatter.
func extractSkillDescription(skillContent string) string {
	// Frontmatter is between first and second "---" lines.
	if !strings.HasPrefix(skillContent, "---\n") {
		return ""
	}
	end := strings.Index(skillContent[4:], "\n---")
	if end < 0 {
		return ""
	}
	frontmatter := skillContent[4 : 4+end]
	for _, line := range strings.Split(frontmatter, "\n") {
		if strings.HasPrefix(line, "description: ") {
			return strings.TrimPrefix(line, "description: ")
		}
	}
	return ""
}

// validateSpawnRepo is middleware that validates the repo URL parameter.
func validateSpawnRepo(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		repo := chi.URLParam(r, "repo")
		if repo == "" || strings.ContainsAny(repo, "/\\.\x00") || len(repo) > 128 {
			http.Error(w, "invalid repo name", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleListSpawnEntries returns pinned spawn entries for a repo, sorted by use_count.
func (s *Server) handleListSpawnEntries(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")
	if s.spawnStore == nil {
		http.Error(w, "emergence system not initialized", http.StatusServiceUnavailable)
		return
	}

	entries, err := s.spawnStore.List(repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if entries == nil {
		entries = []contracts.SpawnEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(contracts.SpawnEntriesResponse{Entries: entries})
}

// handleListAllSpawnEntries returns all spawn entries regardless of state.
func (s *Server) handleListAllSpawnEntries(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")
	if s.spawnStore == nil {
		http.Error(w, "emergence system not initialized", http.StatusServiceUnavailable)
		return
	}

	entries, err := s.spawnStore.ListAll(repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if entries == nil {
		entries = []contracts.SpawnEntry{}
	}

	// Enrich skill entries with metadata.
	if s.spawnMetadataStore != nil {
		for i := range entries {
			if entries[i].SkillRef != "" {
				if meta, ok, _ := s.spawnMetadataStore.Get(repo, entries[i].SkillRef); ok {
					if entries[i].Description == "" {
						entries[i].Description = extractSkillDescription(meta.SkillContent)
					}
					entries[i].Metadata = &meta
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(contracts.SpawnEntriesResponse{Entries: entries})
}

// handleCreateSpawnEntry creates a new manual spawn entry.
func (s *Server) handleCreateSpawnEntry(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")
	if s.spawnStore == nil {
		http.Error(w, "emergence system not initialized", http.StatusServiceUnavailable)
		return
	}

	var req contracts.CreateSpawnEntryRequest
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

	entry, err := s.spawnStore.Create(repo, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(entry)
}

// handleUpdateSpawnEntry updates an existing spawn entry.
func (s *Server) handleUpdateSpawnEntry(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")
	id := chi.URLParam(r, "id")
	if s.spawnStore == nil {
		http.Error(w, "emergence system not initialized", http.StatusServiceUnavailable)
		return
	}

	var req contracts.UpdateSpawnEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.spawnStore.Update(repo, id, req); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	entry, _, _ := s.spawnStore.Get(repo, id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entry)
}

// handleDeleteSpawnEntry deletes a spawn entry.
func (s *Server) handleDeleteSpawnEntry(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")
	id := chi.URLParam(r, "id")
	if s.spawnStore == nil {
		http.Error(w, "emergence system not initialized", http.StatusServiceUnavailable)
		return
	}

	if err := s.spawnStore.Delete(repo, id); err != nil {
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

// handlePinSpawnEntry transitions a proposed spawn entry to pinned and injects the skill into workspaces.
func (s *Server) handlePinSpawnEntry(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")
	id := chi.URLParam(r, "id")
	if s.spawnStore == nil {
		http.Error(w, "emergence system not initialized", http.StatusServiceUnavailable)
		return
	}

	// Get entry before pinning to check type
	entry, found, _ := s.spawnStore.Get(repo, id)
	if !found {
		http.Error(w, "spawn entry not found", http.StatusNotFound)
		return
	}

	if err := s.spawnStore.Pin(repo, id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}

	// If this is a skill entry, inject into all workspaces for this repo
	if entry.Type == contracts.SpawnEntrySkill && entry.SkillRef != "" && s.spawnMetadataStore != nil {
		meta, ok, _ := s.spawnMetadataStore.Get(repo, entry.SkillRef)
		if ok && meta.SkillContent != "" {
			skillModule := detect.SkillModule{
				Name:    entry.SkillRef,
				Content: meta.SkillContent,
			}
			for _, ws := range s.getAutolearnWorkspaces(repo) {
				for _, adapter := range detect.AllAdapters() {
					if err := adapter.InjectSkill(ws.Path, skillModule); err != nil {
						s.logger.Warn("failed to inject skill on pin",
							"repo", repo, "skill", entry.SkillRef, "workspace", ws.Path, "err", err)
					}
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "pinned"})
}

// handleDismissSpawnEntry transitions a spawn entry to dismissed.
func (s *Server) handleDismissSpawnEntry(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")
	id := chi.URLParam(r, "id")
	if s.spawnStore == nil {
		http.Error(w, "emergence system not initialized", http.StatusServiceUnavailable)
		return
	}

	if err := s.spawnStore.Dismiss(repo, id); err != nil {
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

// handleRecordSpawnEntryUse records a usage of a spawn entry.
func (s *Server) handleRecordSpawnEntryUse(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")
	id := chi.URLParam(r, "id")
	if s.spawnStore == nil {
		http.Error(w, "emergence system not initialized", http.StatusServiceUnavailable)
		return
	}

	if err := s.spawnStore.RecordUse(repo, id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "recorded"})
}

// handlePromptHistory returns prompt autocomplete data: pinned spawn entries
// and raw prompt history from event files.
func (s *Server) handlePromptHistory(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")
	if s.spawnStore == nil {
		http.Error(w, "emergence system not initialized", http.StatusServiceUnavailable)
		return
	}

	// Collect prompt history from workspace event files
	var wsPaths []string
	for _, ws := range s.getAutolearnWorkspaces(repo) {
		wsPaths = append(wsPaths, ws.Path)
	}

	prompts := spawn.CollectPromptHistory(wsPaths, 50)
	if prompts == nil {
		prompts = []contracts.PromptHistoryEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(contracts.PromptHistoryResponse{
		Prompts: prompts,
	})
}
