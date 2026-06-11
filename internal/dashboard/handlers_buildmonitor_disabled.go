//go:build nobuildmonitor || nogithub

package dashboard

import (
	"context"
	"net/http"
)

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

// RunBuildMonitorCheck is a no-op when the build monitor is excluded from the build.
func (s *Server) RunBuildMonitorCheck(_ context.Context) {}

// BroadcastBuildMonitor is a no-op when the build monitor is excluded from the build.
func (s *Server) BroadcastBuildMonitor() {}
