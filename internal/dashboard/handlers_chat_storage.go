package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/chat"
)

// handleChatStorageStatus returns the cached chat storage status. It does
// not rescan the chat tree — every read serves the in-memory snapshot the
// cleaner last replaced.
func (s *Server) handleChatStorageStatus(w http.ResponseWriter, r *http.Request) {
	resp := contracts.ChatStorageStatusResponse{
		RetentionDays:     s.config.GetChatStorageRetentionDays(),
		MaxTotalStorageMB: s.config.GetChatStorageMaxTotalStorageMB(),
	}
	if s.chatStorageCleaner != nil {
		status := s.chatStorageCleaner.Status()
		resp.RetentionDays = status.RetentionDays
		resp.MaxTotalStorageMB = status.MaxTotalStorageMB
		resp.UsedBytes = status.UsedBytes
		resp.ActiveBytes = status.ActiveBytes
		resp.EndedBytes = status.EndedBytes
		resp.SessionDirectories = status.SessionDirectories
		resp.ActiveDirectories = status.ActiveDirectories
		resp.LastCleanup = cleanupResultFromChat(status.LastCleanup)
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("failed to encode chat storage status", "err", err)
	}
}

func cleanupResultFromChat(r *chat.StorageCleanupResult) *contracts.ChatStorageCleanupResult {
	if r == nil {
		return nil
	}
	return &contracts.ChatStorageCleanupResult{
		At:             r.At,
		AgeDeleted:     r.AgeDeleted,
		BudgetDeleted:  r.BudgetDeleted,
		BytesReclaimed: r.BytesReclaimed,
		Errors:         r.Errors,
	}
}
