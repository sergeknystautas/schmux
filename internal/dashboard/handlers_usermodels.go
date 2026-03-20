package dashboard

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/sergeknystautas/schmux/internal/models"
)

// handleGetUserModels returns the list of user-defined models.
func (s *Server) handleGetUserModels(w http.ResponseWriter, r *http.Request) {
	userModels := s.models.GetUserModels()
	json.NewEncoder(w).Encode(map[string]any{"models": userModels})
}

// handleSetUserModels saves user-defined models.
func (s *Server) handleSetUserModels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Models []models.UserModel `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	toolNames := s.models.DetectedToolNames()
	if err := models.ValidateUserModels(req.Models, toolNames); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Save to disk
	homeDir, _ := os.UserHomeDir()
	userModelsPath := filepath.Join(homeDir, ".schmux", "user-models.json")
	if err := models.SaveUserModels(userModelsPath, req.Models); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
