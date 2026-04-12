//go:build nofloormanager

package dashboard

import "net/http"

func (s *Server) handleGetFloorManager(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Floor Manager is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleEndShift(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Floor Manager is not available in this build", http.StatusServiceUnavailable)
}
