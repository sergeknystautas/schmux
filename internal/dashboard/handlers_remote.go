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
		response[i] = RemoteFlavorResponse{
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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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
	added, _ := s.config.GetRemoteFlavor(config.GenerateRemoteFlavorID(req.Flavor))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(RemoteFlavorResponse{
		ID:                    added.ID,
		Flavor:                added.Flavor,
		DisplayName:           added.DisplayName,
		VCS:                   added.VCS,
		WorkspacePath:         added.WorkspacePath,
		ConnectCommand:        added.ConnectCommand,
		ReconnectCommand:      added.ReconnectCommand,
		ProvisionCommand:      added.ProvisionCommand,
		HostnameRegex:         added.HostnameRegex,
		VSCodeCommandTemplate: added.VSCodeCommandTemplate,
	})
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
	json.NewEncoder(w).Encode(RemoteFlavorResponse{
		ID:                    flavor.ID,
		Flavor:                flavor.Flavor,
		DisplayName:           flavor.DisplayName,
		VCS:                   flavor.VCS,
		WorkspacePath:         flavor.WorkspacePath,
		ConnectCommand:        flavor.ConnectCommand,
		ReconnectCommand:      flavor.ReconnectCommand,
		ProvisionCommand:      flavor.ProvisionCommand,
		HostnameRegex:         flavor.HostnameRegex,
		VSCodeCommandTemplate: flavor.VSCodeCommandTemplate,
	})
}

// handleRemoteFlavorUpdate handles PUT /api/config/remote-flavors/{id}
func (s *Server) handleRemoteFlavorUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "Flavor ID required", http.StatusBadRequest)
		return
	}

	// Get existing flavor first (Flavor field is immutable)
	existing, found := s.config.GetRemoteFlavor(id)
	if !found {
		http.Error(w, "Flavor not found", http.StatusNotFound)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req struct {
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
		ID:                    id,
		Flavor:                existing.Flavor, // Keep existing (immutable)
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
	json.NewEncoder(w).Encode(RemoteFlavorResponse{
		ID:                    rf.ID,
		Flavor:                rf.Flavor,
		DisplayName:           rf.DisplayName,
		VCS:                   rf.VCS,
		WorkspacePath:         rf.WorkspacePath,
		ConnectCommand:        rf.ConnectCommand,
		ReconnectCommand:      rf.ReconnectCommand,
		ProvisionCommand:      rf.ProvisionCommand,
		HostnameRegex:         rf.HostnameRegex,
		VSCodeCommandTemplate: rf.VSCodeCommandTemplate,
	})
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
	json.NewEncoder(w).Encode(response)
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

	// Check if already connected
	if conn := s.remoteManager.GetConnectionByFlavorID(req.FlavorID); conn != nil && conn.IsConnected() {
		host := conn.Host()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(RemoteHostResponse{
			ID:          host.ID,
			FlavorID:    host.FlavorID,
			DisplayName: flavor.DisplayName,
			Hostname:    host.Hostname,
			Status:      host.Status,
			VCS:         flavor.VCS,
			ConnectedAt: host.ConnectedAt.Format("2006-01-02T15:04:05Z07:00"),
			ExpiresAt:   host.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
		})
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
	json.NewEncoder(w).Encode(RemoteHostResponse{
		FlavorID:              req.FlavorID,
		DisplayName:           flavor.DisplayName,
		Status:                state.RemoteHostStatusProvisioning,
		VCS:                   flavor.VCS,
		ProvisioningSessionID: provisioningSessionID,
	})
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
	json.NewEncoder(w).Encode(RemoteHostResponse{
		ID:                    hostID,
		FlavorID:              host.FlavorID,
		DisplayName:           displayName,
		Hostname:              host.Hostname,
		Status:                state.RemoteHostStatusReconnecting,
		VCS:                   vcs,
		ProvisioningSessionID: provisioningSessionID,
	})
}

// handleRemoteHostDisconnect handles DELETE /api/remote/hosts/{hostID}
func (s *Server) handleRemoteHostDisconnect(w http.ResponseWriter, r *http.Request) {
	hostID := chi.URLParam(r, "hostID")
	if hostID == "" {
		http.Error(w, "Host ID required", http.StatusBadRequest)
		return
	}

	// Disconnect via remote manager if available
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

	w.WriteHeader(http.StatusNoContent)
}

// RemoteFlavorStatusResponse represents a flavor with its connection status.
type RemoteFlavorStatusResponse struct {
	Flavor    RemoteFlavorResponse `json:"flavor"`
	Connected bool                 `json:"connected"`
	Status    string               `json:"status"` // "provisioning", "connecting", "connected", "disconnected"
	Hostname  string               `json:"hostname,omitempty"`
	HostID    string               `json:"host_id,omitempty"`
}

// handleRemoteFlavorStatuses returns all flavors with their connection status.
func (s *Server) handleRemoteFlavorStatuses(w http.ResponseWriter, r *http.Request) {
	flavors := s.config.GetRemoteFlavors()

	// If remote manager is available, use its real-time connection status
	if s.remoteManager != nil {
		statuses := s.remoteManager.GetFlavorStatuses()
		response := make([]RemoteFlavorStatusResponse, len(statuses))
		for i, fs := range statuses {
			response[i] = RemoteFlavorStatusResponse{
				Flavor: RemoteFlavorResponse{
					ID:               fs.Flavor.ID,
					Flavor:           fs.Flavor.Flavor,
					DisplayName:      fs.Flavor.DisplayName,
					VCS:              fs.Flavor.VCS,
					WorkspacePath:    fs.Flavor.WorkspacePath,
					ConnectCommand:   fs.Flavor.ConnectCommand,
					ReconnectCommand: fs.Flavor.ReconnectCommand,
					ProvisionCommand: fs.Flavor.ProvisionCommand,
				},
				Connected: fs.Connected,
				Status:    fs.Status,
				Hostname:  fs.Hostname,
				HostID:    fs.HostID,
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Fallback: use state-based connection status
	hosts := s.state.GetRemoteHosts()

	// Build a map of flavor ID -> connected host
	flavorToHost := make(map[string]state.RemoteHost)
	for _, h := range hosts {
		if h.Status == state.RemoteHostStatusConnected {
			flavorToHost[h.FlavorID] = h
		}
	}

	response := make([]RemoteFlavorStatusResponse, len(flavors))
	for i, f := range flavors {
		resp := RemoteFlavorStatusResponse{
			Flavor: RemoteFlavorResponse{
				ID:               f.ID,
				Flavor:           f.Flavor,
				DisplayName:      f.DisplayName,
				VCS:              f.VCS,
				WorkspacePath:    f.WorkspacePath,
				ConnectCommand:   f.ConnectCommand,
				ReconnectCommand: f.ReconnectCommand,
				ProvisionCommand: f.ProvisionCommand,
			},
			Connected: false,
			Status:    "disconnected",
		}

		if host, found := flavorToHost[f.ID]; found {
			resp.Connected = true
			resp.Status = host.Status
			resp.Hostname = host.Hostname
			resp.HostID = host.ID
		}

		response[i] = resp
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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

	// Set SSE headers (CORS is handled by the withCORS wrapper at route registration)
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
