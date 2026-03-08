package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/emergence"
)

// validateEmergenceRepo is middleware that validates the repo URL parameter.
func validateEmergenceRepo(next http.Handler) http.Handler {
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
	if s.emergenceStore == nil {
		http.Error(w, "emergence system not initialized", http.StatusServiceUnavailable)
		return
	}

	entries, err := s.emergenceStore.List(repo)
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
	if s.emergenceStore == nil {
		http.Error(w, "emergence system not initialized", http.StatusServiceUnavailable)
		return
	}

	entries, err := s.emergenceStore.ListAll(repo)
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

// handleCreateSpawnEntry creates a new manual spawn entry.
func (s *Server) handleCreateSpawnEntry(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")
	if s.emergenceStore == nil {
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

	entry, err := s.emergenceStore.Create(repo, req)
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
	if s.emergenceStore == nil {
		http.Error(w, "emergence system not initialized", http.StatusServiceUnavailable)
		return
	}

	var req contracts.UpdateSpawnEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.emergenceStore.Update(repo, id, req); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	entry, _, _ := s.emergenceStore.Get(repo, id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entry)
}

// handleDeleteSpawnEntry deletes a spawn entry.
func (s *Server) handleDeleteSpawnEntry(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")
	id := chi.URLParam(r, "id")
	if s.emergenceStore == nil {
		http.Error(w, "emergence system not initialized", http.StatusServiceUnavailable)
		return
	}

	if err := s.emergenceStore.Delete(repo, id); err != nil {
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
	if s.emergenceStore == nil {
		http.Error(w, "emergence system not initialized", http.StatusServiceUnavailable)
		return
	}

	// Get entry before pinning to check type
	entry, found, _ := s.emergenceStore.Get(repo, id)
	if !found {
		http.Error(w, "spawn entry not found", http.StatusNotFound)
		return
	}

	if err := s.emergenceStore.Pin(repo, id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}

	// If this is a skill entry, inject into all workspaces for this repo
	if entry.Type == contracts.SpawnEntrySkill && entry.SkillRef != "" && s.emergenceMetadataStore != nil {
		meta, ok, _ := s.emergenceMetadataStore.Get(repo, entry.SkillRef)
		if ok && meta.SkillContent != "" {
			skillModule := detect.SkillModule{
				Name:    entry.SkillRef,
				Content: meta.SkillContent,
			}
			for _, ws := range s.getLoreWorkspaces(repo) {
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
	if s.emergenceStore == nil {
		http.Error(w, "emergence system not initialized", http.StatusServiceUnavailable)
		return
	}

	if err := s.emergenceStore.Dismiss(repo, id); err != nil {
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
	if s.emergenceStore == nil {
		http.Error(w, "emergence system not initialized", http.StatusServiceUnavailable)
		return
	}

	if err := s.emergenceStore.RecordUse(repo, id); err != nil {
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
	if s.emergenceStore == nil {
		http.Error(w, "emergence system not initialized", http.StatusServiceUnavailable)
		return
	}

	// Collect prompt history from workspace event files
	var wsPaths []string
	for _, ws := range s.getLoreWorkspaces(repo) {
		wsPaths = append(wsPaths, ws.Path)
	}

	prompts := emergence.CollectPromptHistory(wsPaths, 50)
	if prompts == nil {
		prompts = []contracts.PromptHistoryEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(contracts.PromptHistoryResponse{
		Prompts: prompts,
	})
}

// handleEmergenceCurate triggers emergence curation: collects intent signals,
// calls the LLM curator, and creates proposed spawn entries.
// Returns 202 immediately; work runs in the background.
func (s *Server) handleEmergenceCurate(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")
	if s.emergenceStore == nil {
		http.Error(w, "emergence system not initialized", http.StatusServiceUnavailable)
		return
	}
	if s.loreExecutor == nil {
		http.Error(w, "no LLM target configured", http.StatusServiceUnavailable)
		return
	}

	// Collect workspace paths for this repo
	var wsPaths []string
	for _, ws := range s.getLoreWorkspaces(repo) {
		wsPaths = append(wsPaths, ws.Path)
	}

	// Collect intent signals
	signals, err := emergence.CollectIntentSignals(wsPaths)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to collect signals: %v", err), http.StatusInternalServerError)
		return
	}
	if len(signals) == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "no_signals"})
		return
	}

	// Get existing skills (builtins)
	builtins, _ := emergence.ListBuiltins()

	// Build curator prompt
	prompt := emergence.BuildEmergencePrompt(signals, builtins, repo)

	// Return 202 immediately
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})

	// Run curation in background
	go s.runEmergenceCuration(repo, prompt)
}

// runEmergenceCuration runs the emergence curation in the background.
func (s *Server) runEmergenceCuration(repo, prompt string) {
	ctx, cancel := context.WithTimeout(s.shutdownCtx, 5*time.Minute)
	defer cancel()

	s.logger.Info("starting emergence curation", "repo", repo)

	response, err := s.loreExecutor(ctx, prompt, 5*time.Minute)
	if err != nil {
		s.logger.Error("emergence curation LLM call failed", "repo", repo, "err", err)
		return
	}

	result, err := emergence.ParseEmergenceResponse(response)
	if err != nil {
		s.logger.Error("emergence curation parse failed", "repo", repo, "err", err)
		return
	}

	now := time.Now().UTC()

	// Process new skills
	for _, proposal := range result.NewSkills {
		skillContent := emergence.GenerateSkillFile(proposal)
		entry := contracts.SpawnEntry{
			ID:       s.emergenceStore.GenerateID(),
			Name:     proposal.Name,
			Type:     contracts.SpawnEntrySkill,
			SkillRef: proposal.Name,
		}
		if err := s.emergenceStore.AddProposed(repo, []contracts.SpawnEntry{entry}); err != nil {
			s.logger.Error("failed to add proposed spawn entry", "repo", repo, "skill", proposal.Name, "err", err)
			continue
		}

		// Save metadata
		if s.emergenceMetadataStore != nil {
			s.emergenceMetadataStore.Save(repo, contracts.EmergenceMetadata{
				SkillName:     proposal.Name,
				SkillContent:  skillContent,
				Confidence:    proposal.Confidence,
				EvidenceCount: len(proposal.Evidence),
				Evidence:      proposal.Evidence,
				EmergedAt:     now,
				LastCurated:   now,
			})
		}

		s.logger.Info("proposed new skill", "repo", repo, "skill", proposal.Name, "entry_id", entry.ID, "content_len", len(skillContent))
	}

	// Process updated skills
	for _, proposal := range result.UpdatedSkills {
		if s.emergenceMetadataStore != nil {
			s.emergenceMetadataStore.Save(repo, contracts.EmergenceMetadata{
				SkillName:     proposal.Name,
				Confidence:    proposal.Confidence,
				EvidenceCount: len(proposal.Evidence),
				Evidence:      proposal.Evidence,
				EmergedAt:     now,
				LastCurated:   now,
			})
		}
		s.logger.Info("updated skill metadata", "repo", repo, "skill", proposal.Name)
	}

	s.logger.Info("emergence curation complete",
		"repo", repo,
		"new_skills", len(result.NewSkills),
		"updated_skills", len(result.UpdatedSkills),
		"discarded", len(result.DiscardedSignals),
	)
}

// TriggerEmergenceCuration triggers emergence curation for a repo in the background.
// Called by the daemon auto-curation callback on session dispose.
func (s *Server) TriggerEmergenceCuration(repo string) {
	if s.emergenceStore == nil || s.loreExecutor == nil {
		return
	}

	var wsPaths []string
	for _, ws := range s.getLoreWorkspaces(repo) {
		wsPaths = append(wsPaths, ws.Path)
	}

	signals, err := emergence.CollectIntentSignals(wsPaths)
	if err != nil || len(signals) == 0 {
		return
	}

	builtins, _ := emergence.ListBuiltins()
	prompt := emergence.BuildEmergencePrompt(signals, builtins, repo)

	go s.runEmergenceCuration(repo, prompt)
}
