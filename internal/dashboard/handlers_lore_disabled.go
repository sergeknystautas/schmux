//go:build nolore

package dashboard

import (
	"net/http"

	"github.com/sergeknystautas/schmux/internal/config"
)

type loreWorkspace struct {
	Path string
	ID   string
}

type mergeApplyRequest struct {
	Layer   string `json:"layer"`
	Content string `json:"content"`
}

func (s *Server) getLoreWorkspaces(_ string) []loreWorkspace { return nil }

func (s *Server) refreshLoreExecutor(_ *config.Config) {}

func (s *Server) handleLoreStatus(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Lore is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleLoreProposals(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Lore is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleLoreProposalGet(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Lore is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleLoreDismiss(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Lore is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleLoreRuleUpdate(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Lore is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleLoreApplyMerge(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Lore is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleLoreEntries(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Lore is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleLoreEntriesClear(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Lore is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleLoreCurate(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Lore is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleLoreCurationsActive(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Lore is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleLoreCurationsList(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Lore is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleLoreCurationLog(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Lore is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleLoreUnifiedMerge(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Lore is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleLorePendingMergeGet(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Lore is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleLorePendingMergeDelete(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Lore is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleLorePendingMergePatch(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Lore is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleLorePush(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Lore is not available in this build", http.StatusServiceUnavailable)
}

func validateLoreRepo(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Lore is not available in this build", http.StatusServiceUnavailable)
	})
}
