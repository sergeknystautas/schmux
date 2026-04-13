//go:build noautolearn

package dashboard

import (
	"net/http"

	"github.com/sergeknystautas/schmux/internal/config"
)

type autolearnWorkspace struct {
	Path string
	ID   string
}

func validateAutolearnRepo(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
	})
}

func (s *Server) getAutolearnWorkspaces(_ string) []autolearnWorkspace { return nil }

func (s *Server) readAutolearnEntries(_ string, _ interface{}) ([]interface{}, error) {
	return nil, nil
}

func (s *Server) refreshAutolearnExecutor(_ *config.Config) {}

func (s *Server) TriggerAutolearnCuration(_ string) {}

func (s *Server) handleAutolearnStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleAutolearnBatches(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleAutolearnBatchGet(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleAutolearnBatchDismiss(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleAutolearnLearningUpdate(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleAutolearnForget(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleAutolearnEntries(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleAutolearnEntriesClear(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleAutolearnCurate(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleAutolearnCurationsActive(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleAutolearnCurationsList(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleAutolearnCurationLog(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleAutolearnMerge(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleAutolearnPendingMergeGet(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleAutolearnPendingMergeDelete(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleAutolearnPendingMergePatch(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleAutolearnPush(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleAutolearnHistory(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleAutolearnPromptHistory(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}
