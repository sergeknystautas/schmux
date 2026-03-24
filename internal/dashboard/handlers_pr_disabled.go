//go:build nogithub

package dashboard

import "net/http"

func (s *Server) handlePRs(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "GitHub integration is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handlePRRefresh(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "GitHub integration is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handlePRCheckout(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "GitHub integration is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleGetGitHubStatus(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "GitHub integration is not available in this build", http.StatusServiceUnavailable)
}
