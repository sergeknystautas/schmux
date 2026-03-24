//go:build nodashboardsx

package dashboard

import "net/http"

func (s *Server) handleDashboardSXProvisionStatus(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "dashboardsx is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleDashboardSXCallback(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "dashboardsx is not available in this build", http.StatusServiceUnavailable)
}
