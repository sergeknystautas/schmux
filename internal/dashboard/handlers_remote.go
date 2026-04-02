package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/state"
)

// RemoteFlavorResponse represents a remote flavor in API responses.
type RemoteFlavorResponse struct {
	ID                    string `json:"id"`
	Flavor                string `json:"flavor"`
	DisplayName           string `json:"display_name"`
	VCS                   string `json:"vcs"`
	WorkspacePath         string `json:"workspace_path"`
	ConnectCommand        string `json:"connect_command,omitempty"`
	ReconnectCommand      string `json:"reconnect_command,omitempty"`
	ProvisionCommand      string `json:"provision_command,omitempty"`
	HostnameRegex         string `json:"hostname_regex,omitempty"`
	VSCodeCommandTemplate string `json:"vscode_command_template,omitempty"`
}

// toFlavorResponse converts a config.RemoteFlavor to a RemoteFlavorResponse.
func toFlavorResponse(f config.RemoteFlavor) RemoteFlavorResponse {
	return RemoteFlavorResponse{
		ID:                    f.ID,
		Flavor:                f.Flavor,
		DisplayName:           f.DisplayName,
		VCS:                   f.VCS,
		WorkspacePath:         f.WorkspacePath,
		ConnectCommand:        f.ConnectCommand,
		ReconnectCommand:      f.ReconnectCommand,
		ProvisionCommand:      f.ProvisionCommand,
		HostnameRegex:         f.HostnameRegex,
		VSCodeCommandTemplate: f.VSCodeCommandTemplate,
	}
}

// RemoteHostResponse represents a remote host in API responses.
type RemoteHostResponse struct {
	ID                    string `json:"id"`
	FlavorID              string `json:"flavor_id"`
	DisplayName           string `json:"display_name,omitempty"`
	Hostname              string `json:"hostname"`
	UUID                  string `json:"uuid,omitempty"`
	Status                string `json:"status"`
	Provisioned           bool   `json:"provisioned"`
	VCS                   string `json:"vcs,omitempty"`
	ConnectedAt           string `json:"connected_at,omitempty"`
	ExpiresAt             string `json:"expires_at,omitempty"`
	ProvisioningSessionID string `json:"provisioning_session_id,omitempty"` // Local tmux session for interactive provisioning terminal
}

// handleGetRemoteFlavors returns all configured remote flavors.
func (s *Server) handleGetRemoteFlavors(w http.ResponseWriter, r *http.Request) {
	flavors := s.config.GetRemoteFlavors()
	response := make([]RemoteFlavorResponse, len(flavors))
	for i, f := range flavors {
		response[i] = toFlavorResponse(f)
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("failed to encode response", "handler", "remote-flavors", "err", err)
	}
}

// handleCreateRemoteFlavor creates a new remote flavor.
func (s *Server) handleCreateRemoteFlavor(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req struct {
		Flavor                string `json:"flavor"`
		DisplayName           string `json:"display_name"`
		VCS                   string `json:"vcs"`
		WorkspacePath         string `json:"workspace_path"`
		ConnectCommand        string `json:"connect_command"`
		ReconnectCommand      string `json:"reconnect_command"`
		ProvisionCommand      string `json:"provision_command"`
		HostnameRegex         string `json:"hostname_regex"`
		VSCodeCommandTemplate string `json:"vscode_command_template"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	rf := config.RemoteFlavor{
		Flavor:                req.Flavor,
		DisplayName:           req.DisplayName,
		VCS:                   req.VCS,
		WorkspacePath:         req.WorkspacePath,
		ConnectCommand:        req.ConnectCommand,
		ReconnectCommand:      req.ReconnectCommand,
		ProvisionCommand:      req.ProvisionCommand,
		HostnameRegex:         req.HostnameRegex,
		VSCodeCommandTemplate: req.VSCodeCommandTemplate,
	}

	if err := s.config.AddRemoteFlavor(rf); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.config.Save(); err != nil {
		http.Error(w, "Failed to save config", http.StatusInternalServerError)
		return
	}

	// Find the added flavor to get the generated ID
	added, found := s.config.GetRemoteFlavor(config.GenerateRemoteFlavorID(req.Flavor))
	if !found {
		writeJSONError(w, "failed to retrieve created flavor", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(toFlavorResponse(added)); err != nil {
		s.logger.Error("failed to encode response", "handler", "create-remote-flavor", "err", err)
	}
}

// handleRemoteFlavorGet handles GET /api/config/remote-flavors/{id}
func (s *Server) handleRemoteFlavorGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "Flavor ID required", http.StatusBadRequest)
		return
	}

	flavor, found := s.config.GetRemoteFlavor(id)
	if !found {
		http.Error(w, "Flavor not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toFlavorResponse(flavor))
}

// handleRemoteFlavorUpdate handles PUT /api/config/remote-flavors/{id}
func (s *Server) handleRemoteFlavorUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "Flavor ID required", http.StatusBadRequest)
		return
	}

	existing, found := s.config.GetRemoteFlavor(id)
	if !found {
		http.Error(w, "Flavor not found", http.StatusNotFound)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req struct {
		Flavor                string `json:"flavor"`
		DisplayName           string `json:"display_name"`
		VCS                   string `json:"vcs"`
		WorkspacePath         string `json:"workspace_path"`
		ConnectCommand        string `json:"connect_command"`
		ReconnectCommand      string `json:"reconnect_command"`
		ProvisionCommand      string `json:"provision_command"`
		HostnameRegex         string `json:"hostname_regex"`
		VSCodeCommandTemplate string `json:"vscode_command_template"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// If flavor not provided in update, keep the existing value
	flavor := req.Flavor
	if flavor == "" {
		flavor = existing.Flavor
	}

	rf := config.RemoteFlavor{
		ID:                    id,
		Flavor:                flavor,
		DisplayName:           req.DisplayName,
		VCS:                   req.VCS,
		WorkspacePath:         req.WorkspacePath,
		ConnectCommand:        req.ConnectCommand,
		ReconnectCommand:      req.ReconnectCommand,
		ProvisionCommand:      req.ProvisionCommand,
		HostnameRegex:         req.HostnameRegex,
		VSCodeCommandTemplate: req.VSCodeCommandTemplate,
	}

	if err := s.config.UpdateRemoteFlavor(rf); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.config.Save(); err != nil {
		http.Error(w, "Failed to save config", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toFlavorResponse(rf))
}

// handleRemoteFlavorDelete handles DELETE /api/config/remote-flavors/{id}
func (s *Server) handleRemoteFlavorDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "Flavor ID required", http.StatusBadRequest)
		return
	}

	if err := s.config.RemoveRemoteFlavor(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := s.config.Save(); err != nil {
		http.Error(w, "Failed to save config", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleRemoteHosts handles GET /api/remote/hosts
func (s *Server) handleRemoteHosts(w http.ResponseWriter, r *http.Request) {
	hosts := s.state.GetRemoteHosts()
	response := make([]RemoteHostResponse, len(hosts))

	for i, h := range hosts {
		displayName := ""
		vcs := ""
		provisioningSessionID := ""

		if flavor, found := s.config.GetRemoteFlavor(h.FlavorID); found {
			displayName = flavor.DisplayName
			vcs = flavor.VCS
		}

		// Get provisioning session ID if available
		if s.remoteManager != nil {
			if conn := s.remoteManager.GetConnection(h.ID); conn != nil {
				provisioningSessionID = conn.ProvisioningSessionID()
			}
		}

		response[i] = RemoteHostResponse{
			ID:                    h.ID,
			FlavorID:              h.FlavorID,
			DisplayName:           displayName,
			Hostname:              h.Hostname,
			UUID:                  h.UUID,
			Status:                h.Status,
			Provisioned:           h.Provisioned,
			VCS:                   vcs,
			ConnectedAt:           h.ConnectedAt.Format("2006-01-02T15:04:05Z07:00"),
			ExpiresAt:             h.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
			ProvisioningSessionID: provisioningSessionID,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("failed to encode response", "handler", "remote-hosts", "err", err)
	}
}

// handleRemoteHostConnect handles POST /api/remote/hosts/connect
// This starts a connection asynchronously and returns immediately.
// The client should poll /api/remote/hosts for status updates.
func (s *Server) handleRemoteHostConnect(w http.ResponseWriter, r *http.Request) {
	// Rate limiting by user (if auth enabled) or IP (without port)
	rateLimitKey := s.normalizeIPForRateLimit(r)
	if s.config.GetAuthEnabled() {
		if user, err := s.authenticateRequest(r); err == nil && user != nil {
			rateLimitKey = user.Login
		}
	}

	if !s.connectLimiter.Allow(rateLimitKey) {
		http.Error(w, "Rate limit exceeded. Max 3 connection attempts per minute.",
			http.StatusTooManyRequests)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req struct {
		FlavorID string `json:"flavor_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.FlavorID == "" {
		http.Error(w, "flavor_id is required", http.StatusBadRequest)
		return
	}

	if s.remoteManager == nil {
		http.Error(w, "Remote workspace support not enabled", http.StatusServiceUnavailable)
		return
	}

	// Check if flavor exists
	flavor, found := s.config.GetRemoteFlavor(req.FlavorID)
	if !found {
		http.Error(w, fmt.Sprintf("Flavor not found: %s", req.FlavorID), http.StatusNotFound)
		return
	}

	// Start connection (returns immediately with provisioning session ID)
	provisioningSessionID, err := s.remoteManager.StartConnect(req.FlavorID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to start connection: %v", err), http.StatusInternalServerError)
		return
	}

	// Return immediately with provisioning status
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(RemoteHostResponse{
		FlavorID:              req.FlavorID,
		DisplayName:           flavor.DisplayName,
		Status:                state.RemoteHostStatusProvisioning,
		VCS:                   flavor.VCS,
		ProvisioningSessionID: provisioningSessionID,
	}); err != nil {
		s.logger.Error("failed to encode response", "handler", "remote-host-connect", "err", err)
	}
}

// handleRemoteHostReconnect handles POST /api/remote/hosts/{hostID}/reconnect
// This starts reconnection asynchronously and returns immediately with a provisioning session ID.
// The client should open a WebSocket to /ws/provision/{provisioningSessionId} for interactive auth.
func (s *Server) handleRemoteHostReconnect(w http.ResponseWriter, r *http.Request) {
	hostID := chi.URLParam(r, "hostID")
	if hostID == "" {
		http.Error(w, "Host ID required", http.StatusBadRequest)
		return
	}

	if s.remoteManager == nil {
		http.Error(w, "Remote workspace support not enabled", http.StatusServiceUnavailable)
		return
	}

	host, found := s.state.GetRemoteHost(hostID)
	if !found {
		http.Error(w, "Host not found", http.StatusNotFound)
		return
	}

	displayName := ""
	vcs := ""
	if flavor, found := s.config.GetRemoteFlavor(host.FlavorID); found {
		displayName = flavor.DisplayName
		vcs = flavor.VCS
	}

	// Start reconnection asynchronously (returns provisioning session ID for WebSocket terminal)
	provisioningSessionID, err := s.remoteManager.StartReconnect(hostID, func(failedHostID string) {
		// Cleanup on failure
		remoteLog := logging.Sub(s.logger, "remote")
		remoteLog.Info("cleaning up failed reconnection", "host_id", failedHostID)
		for _, sess := range s.state.GetSessionsByRemoteHostID(failedHostID) {
			s.state.RemoveSession(sess.ID)
		}
		for _, ws := range s.state.GetWorkspacesByRemoteHostID(failedHostID) {
			s.state.RemoveWorkspace(ws.ID)
			if s.previewManager != nil {
				if err := s.previewManager.DeleteWorkspace(ws.ID); err != nil {
					previewLog := logging.Sub(s.logger, "preview")
					previewLog.Warn("remote cleanup failed", "workspace_id", ws.ID, "err", err)
				}
			}
		}
		s.state.RemoveRemoteHost(failedHostID)
		if err := s.state.Save(); err != nil {
			remoteLog.Error("failed to save state after cleanup", "err", err)
		}
		s.BroadcastSessions()
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to start reconnection: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(RemoteHostResponse{
		ID:                    hostID,
		FlavorID:              host.FlavorID,
		DisplayName:           displayName,
		Hostname:              host.Hostname,
		Status:                state.RemoteHostStatusReconnecting,
		VCS:                   vcs,
		ProvisioningSessionID: provisioningSessionID,
	}); err != nil {
		s.logger.Error("failed to encode response", "handler", "remote-host-reconnect", "err", err)
	}
}

// handleRemoteHostDisconnect handles DELETE /api/remote/hosts/{hostID}
func (s *Server) handleRemoteHostDisconnect(w http.ResponseWriter, r *http.Request) {
	hostID := chi.URLParam(r, "hostID")
	if hostID == "" {
		http.Error(w, "Host ID required", http.StatusBadRequest)
		return
	}

	dismiss := r.URL.Query().Get("dismiss") == "true"

	if dismiss {
		// Dismiss: remove all associated sessions, workspaces, and the host itself
		for _, sess := range s.state.GetSessionsByRemoteHostID(hostID) {
			s.state.RemoveSession(sess.ID)
		}
		for _, ws := range s.state.GetWorkspacesByRemoteHostID(hostID) {
			s.state.RemoveWorkspace(ws.ID)
		}
		s.state.RemoveRemoteHost(hostID)
		if s.remoteManager != nil {
			s.remoteManager.Disconnect(hostID)
		}
		if err := s.state.Save(); err != nil {
			http.Error(w, "Failed to save state", http.StatusInternalServerError)
			return
		}
	} else {
		// Default: disconnect only
		if s.remoteManager != nil {
			if err := s.remoteManager.Disconnect(hostID); err != nil {
				remoteLog := logging.Sub(s.logger, "remote")
				remoteLog.Warn("disconnect failed", "err", err)
			}
		} else {
			// Fallback: just update state
			if err := s.state.UpdateRemoteHostStatus(hostID, state.RemoteHostStatusDisconnected); err != nil {
				http.Error(w, fmt.Sprintf("Failed to update host: %v", err), http.StatusInternalServerError)
				return
			}
			if err := s.state.Save(); err != nil {
				http.Error(w, "Failed to save state", http.StatusInternalServerError)
				return
			}
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// RemoteFlavorStatusResponse represents a flavor with the status of all its hosts.
type RemoteFlavorStatusResponse struct {
	Flavor RemoteFlavorResponse   `json:"flavor"`
	Hosts  []RemoteHostStatusItem `json:"hosts"`
}

// RemoteHostStatusItem represents the status of a single remote host within a flavor.
type RemoteHostStatusItem struct {
	HostID    string `json:"host_id"`
	Hostname  string `json:"hostname"`
	Status    string `json:"status"`
	Connected bool   `json:"connected"`
}

// handleRemoteFlavorStatuses returns all flavors with their connection status.
func (s *Server) handleRemoteFlavorStatuses(w http.ResponseWriter, r *http.Request) {
	flavors := s.config.GetRemoteFlavors()

	// If remote manager is available, use its real-time connection status
	if s.remoteManager != nil {
		statuses := s.remoteManager.GetFlavorStatuses()
		response := make([]RemoteFlavorStatusResponse, len(statuses))
		for i, fs := range statuses {
			resp := RemoteFlavorStatusResponse{
				Flavor: toFlavorResponse(fs.Flavor),
			}
			for _, h := range fs.Hosts {
				resp.Hosts = append(resp.Hosts, RemoteHostStatusItem{
					HostID:    h.HostID,
					Hostname:  h.Hostname,
					Status:    h.Status,
					Connected: h.Status == "connected",
				})
			}
			response[i] = resp
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			s.logger.Error("failed to encode response", "handler", "remote-flavor-statuses", "err", err)
		}
		return
	}

	// Fallback: use state-based connection status
	hosts := s.state.GetRemoteHosts()

	// Build a map of flavor ID -> all hosts
	flavorToHosts := make(map[string][]state.RemoteHost)
	for _, h := range hosts {
		flavorToHosts[h.FlavorID] = append(flavorToHosts[h.FlavorID], h)
	}

	response := make([]RemoteFlavorStatusResponse, len(flavors))
	for i, f := range flavors {
		resp := RemoteFlavorStatusResponse{
			Flavor: toFlavorResponse(f),
		}
		for _, host := range flavorToHosts[f.ID] {
			resp.Hosts = append(resp.Hosts, RemoteHostStatusItem{
				HostID:    host.ID,
				Hostname:  host.Hostname,
				Status:    host.Status,
				Connected: host.Status == state.RemoteHostStatusConnected,
			})
		}
		response[i] = resp
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("failed to encode response", "handler", "remote-flavor-statuses", "err", err)
	}
}

// handleRemoteConnectStream handles GET /api/remote/hosts/connect/stream
// This streams provisioning progress via Server-Sent Events (SSE).
func (s *Server) handleRemoteConnectStream(w http.ResponseWriter, r *http.Request) {
	flavorID := r.URL.Query().Get("flavor_id")
	if flavorID == "" {
		http.Error(w, "flavor_id required", http.StatusBadRequest)
		return
	}

	if s.remoteManager == nil {
		http.Error(w, "Remote workspace support not enabled", http.StatusServiceUnavailable)
		return
	}

	// Set SSE headers (CORS is handled by corsMiddleware)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Create progress channel and cleanup coordination
	progressCh := make(chan string, 10)
	doneCh := make(chan struct{})
	cleanupOnce := sync.Once{}

	// Cleanup function to drain progressCh and signal goroutine to stop
	cleanup := func() {
		cleanupOnce.Do(func() {
			// Drain any buffered progress messages to prevent goroutine blocking
			go func() {
				for range progressCh {
					// Discard
				}
			}()
			close(doneCh) // Signal goroutine to stop
		})
	}
	defer cleanup()

	// Start connection with progress callback
	go func() {
		// Use request context so we stop if client disconnects
		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()

		_, err := s.remoteManager.ConnectWithProgress(ctx, flavorID, progressCh)
		if err != nil {
			// Try to send error, but don't panic if channel is closed or nobody listening
			select {
			case progressCh <- fmt.Sprintf("error: %v", err):
			case <-doneCh:
				// Cleanup was called (client disconnected), stop
				return
			default:
			}
		} else {
			select {
			case progressCh <- "connected":
			case <-doneCh:
				// Cleanup was called (client disconnected), stop
				return
			default:
			}
		}
		close(progressCh) // Close channel to signal completion
	}()

	// Stream progress events to client
	timeout := time.NewTimer(125 * time.Second) // Slightly longer than connection timeout
	defer timeout.Stop()

	for {
		select {
		case msg, ok := <-progressCh:
			if !ok {
				// Channel closed by goroutine, connection complete
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()

		case <-timeout.C:
			// Safety timeout
			fmt.Fprintf(w, "data: error: connection timeout\n\n")
			flusher.Flush()
			return

		case <-r.Context().Done():
			// Client disconnected - cleanup() will be called by defer
			return
		}
	}
}
