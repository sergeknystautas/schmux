//go:build !notunnel

package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/sergeknystautas/schmux/internal/tunnel"
)

func (s *Server) handleRemoteAccessStatus(w http.ResponseWriter, r *http.Request) {
	if s.tunnelManager == nil {
		writeJSONError(w, "Remote access not available", http.StatusServiceUnavailable)
		return
	}

	status := s.tunnelManager.Status()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		s.logger.Error("failed to encode response", "handler", "remote-access-status", "err", err)
	}
}

func (s *Server) handleRemoteAccessOn(w http.ResponseWriter, r *http.Request) {
	if s.tunnelManager == nil {
		writeJSONError(w, "Remote access not available", http.StatusServiceUnavailable)
		return
	}

	if err := s.tunnelManager.Start(); err != nil {
		if !s.config.GetRemoteAccessEnabled() {
			writeJSONError(w, err.Error(), http.StatusForbidden)
		} else {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
		}
		return
	}

	status := s.tunnelManager.Status()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		s.logger.Error("failed to encode response", "handler", "remote-access-on", "err", err)
	}
}

func (s *Server) handleRemoteAccessOff(w http.ResponseWriter, r *http.Request) {
	if s.tunnelManager == nil {
		writeJSONError(w, "Remote access not available", http.StatusServiceUnavailable)
		return
	}

	s.tunnelManager.Stop()

	status := s.tunnelManager.Status()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		s.logger.Error("failed to encode response", "handler", "remote-access-off", "err", err)
	}
}

func (s *Server) handleRemoteAccessTestNotification(w http.ResponseWriter, r *http.Request) {
	ntfyTopic := ""
	if s.config != nil {
		ntfyTopic = s.config.GetRemoteAccessNtfyTopic()
	}
	if ntfyTopic == "" {
		writeJSONError(w, "ntfy topic not configured", http.StatusBadRequest)
		return
	}

	nc := tunnel.NotifyConfig{
		NtfyURL: "https://ntfy.sh/" + ntfyTopic,
	}
	if err := nc.Send("", "Hi from schmux! 👋"); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]bool{"ok": true}); err != nil {
		s.logger.Error("failed to encode response", "handler", "remote-access-test-notification", "err", err)
	}
}
