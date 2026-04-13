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

// AutolearnHandlers is a minimal stub when autolearn is compiled out.
type AutolearnHandlers struct{}

// newAutolearnHandlers returns a stub AutolearnHandlers when autolearn is compiled out.
func newAutolearnHandlers(_ *Server) *AutolearnHandlers {
	return &AutolearnHandlers{}
}

func validateAutolearnRepo(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
	})
}

func (h *AutolearnHandlers) getAutolearnWorkspaces(_ string) []autolearnWorkspace { return nil }

func (h *AutolearnHandlers) readAutolearnEntries(_ string, _ interface{}) ([]interface{}, error) {
	return nil, nil
}

func (s *Server) refreshAutolearnExecutor(_ *config.Config) {}

func (h *AutolearnHandlers) handleAutolearnStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (h *AutolearnHandlers) handleAutolearnBatches(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (h *AutolearnHandlers) handleAutolearnBatchGet(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (h *AutolearnHandlers) handleAutolearnBatchDismiss(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (h *AutolearnHandlers) handleAutolearnLearningUpdate(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (h *AutolearnHandlers) handleAutolearnForget(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (h *AutolearnHandlers) handleAutolearnEntries(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (h *AutolearnHandlers) handleAutolearnEntriesClear(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (h *AutolearnHandlers) handleAutolearnCurate(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (h *AutolearnHandlers) handleAutolearnCurationsActive(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (h *AutolearnHandlers) handleAutolearnCurationsList(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (h *AutolearnHandlers) handleAutolearnCurationLog(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (h *AutolearnHandlers) handleAutolearnMerge(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (h *AutolearnHandlers) handleAutolearnPendingMergeGet(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (h *AutolearnHandlers) handleAutolearnPendingMergeDelete(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (h *AutolearnHandlers) handleAutolearnPendingMergePatch(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (h *AutolearnHandlers) handleAutolearnPush(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (h *AutolearnHandlers) handleAutolearnHistory(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}

func (h *AutolearnHandlers) handleAutolearnPromptHistory(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Autolearn is not available in this build", http.StatusServiceUnavailable)
}
