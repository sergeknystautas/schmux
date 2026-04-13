//go:build notimelapse

package dashboard

import "net/http"

func (s *Server) handleTimelapseList(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Timelapse is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleTimelapseExport(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Timelapse is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleTimelapseDownload(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Timelapse is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleTimelapseDelete(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Timelapse is not available in this build", http.StatusServiceUnavailable)
}
