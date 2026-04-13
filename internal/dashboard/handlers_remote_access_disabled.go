//go:build notunnel

package dashboard

import "net/http"

func (s *Server) handleRemoteAccessStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Remote access is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleRemoteAccessOn(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Remote access is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleRemoteAccessOff(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Remote access is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleRemoteAccessTestNotification(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Remote access is not available in this build", http.StatusServiceUnavailable)
}
