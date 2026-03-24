//go:build nogithub

package dashboard

import "net/http"

func (s *Server) authRedirectURI() (string, error) {
	return "", nil
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "GitHub auth is not available in this build", http.StatusNotFound)
}

func (s *Server) handleAuthCallback(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "GitHub auth is not available in this build", http.StatusNotFound)
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "GitHub auth is not available in this build", http.StatusNotFound)
}

func (s *Server) handleAuthMe(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "GitHub auth is not available in this build", http.StatusNotFound)
}
