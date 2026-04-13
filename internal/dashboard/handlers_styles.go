package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/style"
)

// StyleHandlers groups HTTP handlers for style CRUD operations.
type StyleHandlers struct {
	styleManager *style.Manager
	logger       *log.Logger
}

var validStyleID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$`)

func (h *StyleHandlers) handleListStyles(w http.ResponseWriter, r *http.Request) {
	styles, err := h.styleManager.List()
	if err != nil {
		writeJSONError(w, fmt.Sprintf("failed to list styles: %v", err), http.StatusInternalServerError)
		return
	}
	response := contracts.StyleListResponse{
		Styles: make([]contracts.Style, len(styles)),
	}
	for i, st := range styles {
		response.Styles[i] = styleToContract(st)
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("failed to encode response", "handler", "list-styles", "err", err)
	}
}

func (h *StyleHandlers) handleGetStyle(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	st, err := h.styleManager.Get(id)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("style not found: %s", id), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(styleToContract(st)); err != nil {
		h.logger.Error("failed to encode response", "handler", "get-style", "err", err)
	}
}

func (h *StyleHandlers) handleCreateStyle(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req contracts.StyleCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.ID == "" || req.Name == "" || req.Icon == "" || req.Prompt == "" {
		writeJSONError(w, "id, name, icon, and prompt are required", http.StatusBadRequest)
		return
	}
	if !validStyleID.MatchString(req.ID) {
		writeJSONError(w, "id must be a URL-safe slug (lowercase alphanumeric + hyphens)", http.StatusBadRequest)
		return
	}
	if req.ID == "create" || req.ID == "none" {
		writeJSONError(w, fmt.Sprintf("%q is a reserved ID", req.ID), http.StatusBadRequest)
		return
	}
	st := &style.Style{
		ID: req.ID, Name: req.Name, Icon: req.Icon,
		Tagline: req.Tagline, Prompt: req.Prompt, BuiltIn: false,
	}
	if err := h.styleManager.Create(st); err != nil {
		writeJSONError(w, fmt.Sprintf("failed to create style: %v", err), http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(styleToContract(st)); err != nil {
		h.logger.Error("failed to encode response", "handler", "create-style", "err", err)
	}
}

func (h *StyleHandlers) handleUpdateStyle(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	id := chi.URLParam(r, "id")
	existing, err := h.styleManager.Get(id)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("style not found: %s", id), http.StatusNotFound)
		return
	}
	var req contracts.StyleUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Icon != nil {
		existing.Icon = *req.Icon
	}
	if req.Tagline != nil {
		existing.Tagline = *req.Tagline
	}
	if req.Prompt != nil {
		existing.Prompt = *req.Prompt
	}
	if err := h.styleManager.Update(existing); err != nil {
		writeJSONError(w, fmt.Sprintf("failed to update style: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(styleToContract(existing)); err != nil {
		h.logger.Error("failed to encode response", "handler", "update-style", "err", err)
	}
}

func (h *StyleHandlers) handleDeleteStyle(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := h.styleManager.Get(id)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("style not found: %s", id), http.StatusNotFound)
		return
	}
	if existing.BuiltIn {
		if err := h.styleManager.ResetBuiltIn(id); err != nil {
			writeJSONError(w, fmt.Sprintf("failed to reset style: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		if err := h.styleManager.Delete(id); err != nil {
			writeJSONError(w, fmt.Sprintf("failed to delete style: %v", err), http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func styleToContract(s *style.Style) contracts.Style {
	return contracts.Style{
		ID: s.ID, Name: s.Name, Icon: s.Icon,
		Tagline: s.Tagline, Prompt: s.Prompt, BuiltIn: s.BuiltIn,
	}
}
