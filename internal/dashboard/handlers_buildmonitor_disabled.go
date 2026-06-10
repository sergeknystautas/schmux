//go:build nobuildmonitor || nogithub

package dashboard

import "net/http"

func (s *Server) handleBuildMonitorGet(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Build Monitor feature is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleBuildMonitorCheck(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Build Monitor feature is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleBuildMonitorIdentities(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Build Monitor feature is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleBuildMonitorConnectIdentity(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Build Monitor feature is not available in this build", http.StatusServiceUnavailable)
}
