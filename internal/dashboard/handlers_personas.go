package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/persona"
)

// PersonaHandlers groups HTTP handlers for persona CRUD operations.
type PersonaHandlers struct {
	personaManager *persona.Manager
	logger         *log.Logger
}

// validPersonaID matches URL-safe slugs: lowercase alphanumeric + hyphens.
var validPersonaID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$`)

// handleListPersonas returns all personas.
func (h *PersonaHandlers) handleListPersonas(w http.ResponseWriter, r *http.Request) {
	personas, err := h.personaManager.List()
	if err != nil {
		writeJSONError(w, fmt.Sprintf("failed to list personas: %v", err), http.StatusInternalServerError)
		return
	}

	response := contracts.PersonaListResponse{
		Personas: make([]contracts.Persona, len(personas)),
	}
	for i, p := range personas {
		response.Personas[i] = personaToContract(p)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("failed to encode response", "handler", "list-personas", "err", err)
	}
}

// handleGetPersona returns a single persona by ID.
func (h *PersonaHandlers) handleGetPersona(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := h.personaManager.Get(id)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("persona not found: %s", id), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(personaToContract(p)); err != nil {
		h.logger.Error("failed to encode response", "handler", "get-persona", "err", err)
	}
}

// handleCreatePersona creates a new persona.
func (h *PersonaHandlers) handleCreatePersona(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

	var req contracts.PersonaCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.ID == "" || req.Name == "" || req.Icon == "" || req.Color == "" || req.Prompt == "" {
		writeJSONError(w, "id, name, icon, color, and prompt are required", http.StatusBadRequest)
		return
	}

	// Validate ID format (prevent path traversal)
	if !validPersonaID.MatchString(req.ID) {
		writeJSONError(w, "id must be a URL-safe slug (lowercase alphanumeric + hyphens)", http.StatusBadRequest)
		return
	}

	// "create" is reserved for the dashboard route (/personas/create)
	if req.ID == "create" {
		writeJSONError(w, `"create" is a reserved ID`, http.StatusBadRequest)
		return
	}

	p := &persona.Persona{
		ID:           req.ID,
		Name:         req.Name,
		Icon:         req.Icon,
		Color:        req.Color,
		Prompt:       req.Prompt,
		Expectations: req.Expectations,
		BuiltIn:      false,
	}

	if err := h.personaManager.Create(p); err != nil {
		writeJSONError(w, fmt.Sprintf("failed to create persona: %v", err), http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(personaToContract(p)); err != nil {
		h.logger.Error("failed to encode response", "handler", "create-persona", "err", err)
	}
}

// handleUpdatePersona updates an existing persona.
func (h *PersonaHandlers) handleUpdatePersona(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	id := chi.URLParam(r, "id")

	existing, err := h.personaManager.Get(id)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("persona not found: %s", id), http.StatusNotFound)
		return
	}

	var req contracts.PersonaUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Apply non-nil fields
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Icon != nil {
		existing.Icon = *req.Icon
	}
	if req.Color != nil {
		existing.Color = *req.Color
	}
	if req.Prompt != nil {
		existing.Prompt = *req.Prompt
	}
	if req.Expectations != nil {
		existing.Expectations = *req.Expectations
	}

	if err := h.personaManager.Update(existing); err != nil {
		writeJSONError(w, fmt.Sprintf("failed to update persona: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(personaToContract(existing)); err != nil {
		h.logger.Error("failed to encode response", "handler", "update-persona", "err", err)
	}
}

// handleDeletePersona deletes a persona or resets a built-in to default.
func (h *PersonaHandlers) handleDeletePersona(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	existing, err := h.personaManager.Get(id)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("persona not found: %s", id), http.StatusNotFound)
		return
	}

	if existing.BuiltIn {
		// Reset built-in to default
		if err := h.personaManager.ResetBuiltIn(id); err != nil {
			writeJSONError(w, fmt.Sprintf("failed to reset persona: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		if err := h.personaManager.Delete(id); err != nil {
			writeJSONError(w, fmt.Sprintf("failed to delete persona: %v", err), http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// personaToContract converts a persona.Persona to a contracts.Persona.
func personaToContract(p *persona.Persona) contracts.Persona {
	return contracts.Persona{
		ID:           p.ID,
		Name:         p.Name,
		Icon:         p.Icon,
		Color:        p.Color,
		Prompt:       p.Prompt,
		Expectations: p.Expectations,
		BuiltIn:      p.BuiltIn,
	}
}
