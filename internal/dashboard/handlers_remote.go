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

// RemoteProfileResponse represents a remote profile in API responses.
type RemoteProfileResponse struct {
	ID                    string                       `json:"id"`
	DisplayName           string                       `json:"display_name"`
	VCS                   string                       `json:"vcs"`
	WorkspacePath         string                       `json:"workspace_path"`
	ConnectCommand        string                       `json:"connect_command,omitempty"`
	ReconnectCommand      string                       `json:"reconnect_command,omitempty"`
	ProvisionCommand      string                       `json:"provision_command,omitempty"`
	HostnameRegex         string                       `json:"hostname_regex,omitempty"`
	VSCodeCommandTemplate string                       `json:"vscode_command_template,omitempty"`
	Flavors               []config.RemoteProfileFlavor `json:"flavors"`
}

// RemoteFlavorResponse represents a remote flavor in API responses.
// DEPRECATED: kept for backward compatibility with existing API consumers.
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

// toProfileResponse converts a config.RemoteProfile to a RemoteProfileResponse.
func toProfileResponse(p config.RemoteProfile) RemoteProfileResponse {
	flavors := p.Flavors
	if flavors == nil {
		flavors = []config.RemoteProfileFlavor{}
	}
	return RemoteProfileResponse{
		ID:                    p.ID,
		DisplayName:           p.DisplayName,
		VCS:                   p.VCS,
		WorkspacePath:         p.WorkspacePath,
		ConnectCommand:        p.ConnectCommand,
		ReconnectCommand:      p.ReconnectCommand,
		ProvisionCommand:      p.ProvisionCommand,
		HostnameRegex:         p.HostnameRegex,
		VSCodeCommandTemplate: p.VSCodeCommandTemplate,
		Flavors:               flavors,
	}
}

// RemoteHostResponse represents a remote host in API responses.
type RemoteHostResponse struct {
	ID                    string `json:"id"`
	ProfileID             string `json:"profile_id"`
	Flavor                string `json:"flavor"`
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

// handleGetRemoteProfiles returns all configured remote profiles.
func (s *Server) handleGetRemoteProfiles(w http.ResponseWriter, r *http.Request) {
	profiles := s.config.GetRemoteProfiles()
	response := make([]RemoteProfileResponse, len(profiles))
	for i, p := range profiles {
		response[i] = toProfileResponse(p)
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("failed to encode response", "handler", "remote-profiles", "err", err)
	}
}

// handleCreateRemoteProfile creates a new remote profile.
func (s *Server) handleCreateRemoteProfile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req struct {
		DisplayName           string                       `json:"display_name"`
		VCS                   string                       `json:"vcs"`
		WorkspacePath         string                       `json:"workspace_path"`
		ConnectCommand        string                       `json:"connect_command"`
		ReconnectCommand      string                       `json:"reconnect_command"`
		ProvisionCommand      string                       `json:"provision_command"`
		HostnameRegex         string                       `json:"hostname_regex"`
		VSCodeCommandTemplate string                       `json:"vscode_command_template"`
		Flavors               []config.RemoteProfileFlavor `json:"flavors"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	rp := config.RemoteProfile{
		DisplayName:           req.DisplayName,
		VCS:                   req.VCS,
		WorkspacePath:         req.WorkspacePath,
		ConnectCommand:        req.ConnectCommand,
		ReconnectCommand:      req.ReconnectCommand,
		ProvisionCommand:      req.ProvisionCommand,
		HostnameRegex:         req.HostnameRegex,
		VSCodeCommandTemplate: req.VSCodeCommandTemplate,
		Flavors:               req.Flavors,
	}

	if err := s.config.AddRemoteProfile(rp); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.config.Save(); err != nil {
		writeJSONError(w, "Failed to save config", http.StatusInternalServerError)
		return
	}

	// Find the added profile to get the generated ID
	profiles := s.config.GetRemoteProfiles()
	var addedProfile config.RemoteProfile
	found := false
	for _, p := range profiles {
		if p.DisplayName == req.DisplayName {
			addedProfile = p
			found = true
			break
		}
	}
	if !found {
		writeJSONError(w, "failed to retrieve created profile", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(toProfileResponse(addedProfile)); err != nil {
		s.logger.Error("failed to encode response", "handler", "create-remote-profile", "err", err)
	}
}

// handleRemoteProfileGet handles GET /api/config/remote-profiles/{id}
func (s *Server) handleRemoteProfileGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONError(w, "Profile ID required", http.StatusBadRequest)
		return
	}

	profile, found := s.config.GetRemoteProfile(id)
	if !found {
		writeJSONError(w, "Profile not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toProfileResponse(profile))
}

// handleRemoteProfileUpdate handles PUT /api/config/remote-profiles/{id}
func (s *Server) handleRemoteProfileUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONError(w, "Profile ID required", http.StatusBadRequest)
		return
	}

	_, found := s.config.GetRemoteProfile(id)
	if !found {
		writeJSONError(w, "Profile not found", http.StatusNotFound)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req struct {
		DisplayName           string                       `json:"display_name"`
		VCS                   string                       `json:"vcs"`
		WorkspacePath         string                       `json:"workspace_path"`
		ConnectCommand        string                       `json:"connect_command"`
		ReconnectCommand      string                       `json:"reconnect_command"`
		ProvisionCommand      string                       `json:"provision_command"`
		HostnameRegex         string                       `json:"hostname_regex"`
		VSCodeCommandTemplate string                       `json:"vscode_command_template"`
		Flavors               []config.RemoteProfileFlavor `json:"flavors"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	rp := config.RemoteProfile{
		ID:                    id,
		DisplayName:           req.DisplayName,
		VCS:                   req.VCS,
		WorkspacePath:         req.WorkspacePath,
		ConnectCommand:        req.ConnectCommand,
		ReconnectCommand:      req.ReconnectCommand,
		ProvisionCommand:      req.ProvisionCommand,
		HostnameRegex:         req.HostnameRegex,
		VSCodeCommandTemplate: req.VSCodeCommandTemplate,
		Flavors:               req.Flavors,
	}

	if err := s.config.UpdateRemoteProfile(rp); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.config.Save(); err != nil {
		writeJSONError(w, "Failed to save config", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toProfileResponse(rp))
}

// handleRemoteProfileDelete handles DELETE /api/config/remote-profiles/{id}
func (s *Server) handleRemoteProfileDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONError(w, "Profile ID required", http.StatusBadRequest)
		return
	}

	if err := s.config.RemoveRemoteProfile(id); err != nil {
		writeJSONError(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := s.config.Save(); err != nil {
		writeJSONError(w, "Failed to save config", http.StatusInternalServerError)
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

		if profile, found := s.config.GetRemoteProfile(h.ProfileID); found {
			if resolved, err := config.ResolveProfileFlavor(profile, h.Flavor); err == nil {
				displayName = resolved.FlavorDisplayName
				vcs = resolved.VCS
			} else {
				displayName = profile.DisplayName
				vcs = profile.VCS
			}
		}

		// Get provisioning session ID if available
		if s.remoteManager != nil {
			if conn := s.remoteManager.GetConnection(h.ID); conn != nil {
				provisioningSessionID = conn.ProvisioningSessionID()
			}
		}

		response[i] = RemoteHostResponse{
			ID:                    h.ID,
			ProfileID:             h.ProfileID,
			Flavor:                h.Flavor,
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
		writeJSONError(w, "Rate limit exceeded. Max 3 connection attempts per minute.",
			http.StatusTooManyRequests)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req struct {
		ProfileID string `json:"profile_id"`
		Flavor    string `json:"flavor"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ProfileID == "" {
		writeJSONError(w, "profile_id is required", http.StatusBadRequest)
		return
	}
	if req.Flavor == "" {
		writeJSONError(w, "flavor is required", http.StatusBadRequest)
		return
	}

	if s.remoteManager == nil {
		writeJSONError(w, "Remote workspace support not enabled", http.StatusServiceUnavailable)
		return
	}

	// Check if profile exists and resolve flavor
	profile, found := s.config.GetRemoteProfile(req.ProfileID)
	if !found {
		writeJSONError(w, fmt.Sprintf("Profile not found: %s", req.ProfileID), http.StatusNotFound)
		return
	}
	resolved, err := config.ResolveProfileFlavor(profile, req.Flavor)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Start connection (returns immediately with provisioning session ID)
	provisioningSessionID, err := s.remoteManager.StartConnect(req.ProfileID, req.Flavor)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to start connection: %v", err), http.StatusInternalServerError)
		return
	}

	// Return immediately with provisioning status
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(RemoteHostResponse{
		ProfileID:             req.ProfileID,
		Flavor:                req.Flavor,
		DisplayName:           resolved.FlavorDisplayName,
		Status:                state.RemoteHostStatusProvisioning,
		VCS:                   resolved.VCS,
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
		writeJSONError(w, "Host ID required", http.StatusBadRequest)
		return
	}

	if s.remoteManager == nil {
		writeJSONError(w, "Remote workspace support not enabled", http.StatusServiceUnavailable)
		return
	}

	host, found := s.state.GetRemoteHost(hostID)
	if !found {
		writeJSONError(w, "Host not found", http.StatusNotFound)
		return
	}

	displayName := ""
	vcs := ""
	if profile, found := s.config.GetRemoteProfile(host.ProfileID); found {
		if resolved, err := config.ResolveProfileFlavor(profile, host.Flavor); err == nil {
			displayName = resolved.FlavorDisplayName
			vcs = resolved.VCS
		} else {
			displayName = profile.DisplayName
			vcs = profile.VCS
		}
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
		writeJSONError(w, fmt.Sprintf("Failed to start reconnection: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(RemoteHostResponse{
		ID:                    hostID,
		ProfileID:             host.ProfileID,
		Flavor:                host.Flavor,
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
		writeJSONError(w, "Host ID required", http.StatusBadRequest)
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
			writeJSONError(w, "Failed to save state", http.StatusInternalServerError)
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
				writeJSONError(w, fmt.Sprintf("Failed to update host: %v", err), http.StatusInternalServerError)
				return
			}
			if err := s.state.Save(); err != nil {
				writeJSONError(w, "Failed to save state", http.StatusInternalServerError)
				return
			}
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// RemoteProfileStatusResponse represents a profile with the status of all its hosts.
type RemoteProfileStatusResponse struct {
	Profile     RemoteProfileResponse   `json:"profile"`
	FlavorHosts []RemoteFlavorHostGroup `json:"flavor_hosts"`
}

// RemoteFlavorHostGroup groups hosts by flavor within a profile status response.
type RemoteFlavorHostGroup struct {
	Flavor string                 `json:"flavor"`
	Hosts  []RemoteHostStatusItem `json:"hosts"`
}

// RemoteFlavorStatusResponse is kept for backward compatibility.
// DEPRECATED: Use RemoteProfileStatusResponse instead.
type RemoteFlavorStatusResponse = RemoteProfileStatusResponse

// RemoteHostStatusItem represents the status of a single remote host within a flavor.
type RemoteHostStatusItem struct {
	HostID    string `json:"host_id"`
	Hostname  string `json:"hostname"`
	Status    string `json:"status"`
	Connected bool   `json:"connected"`
}

// handleRemoteProfileStatuses returns all profiles with their connection status.
func (s *Server) handleRemoteProfileStatuses(w http.ResponseWriter, r *http.Request) {
	profiles := s.config.GetRemoteProfiles()

	// If remote manager is available, use its real-time connection status
	if s.remoteManager != nil {
		statuses := s.remoteManager.GetProfileStatuses()
		response := make([]RemoteProfileStatusResponse, len(statuses))
		for i, ps := range statuses {
			resp := RemoteProfileStatusResponse{
				Profile:     toProfileResponse(ps.Profile),
				FlavorHosts: []RemoteFlavorHostGroup{},
			}
			for _, fg := range ps.FlavorHosts {
				group := RemoteFlavorHostGroup{
					Flavor: fg.Flavor,
					Hosts:  []RemoteHostStatusItem{},
				}
				for _, h := range fg.Hosts {
					group.Hosts = append(group.Hosts, RemoteHostStatusItem{
						HostID:    h.HostID,
						Hostname:  h.Hostname,
						Status:    h.Status,
						Connected: h.Status == "connected",
					})
				}
				resp.FlavorHosts = append(resp.FlavorHosts, group)
			}
			response[i] = resp
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			s.logger.Error("failed to encode response", "handler", "remote-profile-statuses", "err", err)
		}
		return
	}

	// Fallback: use state-based connection status
	hosts := s.state.GetRemoteHosts()

	// Build a map of profileID -> flavor -> hosts
	type profileFlavorKey struct {
		profileID string
		flavor    string
	}
	pfToHosts := make(map[profileFlavorKey][]state.RemoteHost)
	for _, h := range hosts {
		key := profileFlavorKey{h.ProfileID, h.Flavor}
		pfToHosts[key] = append(pfToHosts[key], h)
	}

	response := make([]RemoteProfileStatusResponse, len(profiles))
	for i, p := range profiles {
		resp := RemoteProfileStatusResponse{
			Profile:     toProfileResponse(p),
			FlavorHosts: []RemoteFlavorHostGroup{},
		}
		for _, pf := range p.Flavors {
			group := RemoteFlavorHostGroup{
				Flavor: pf.Flavor,
				Hosts:  []RemoteHostStatusItem{},
			}
			key := profileFlavorKey{p.ID, pf.Flavor}
			for _, host := range pfToHosts[key] {
				group.Hosts = append(group.Hosts, RemoteHostStatusItem{
					HostID:    host.ID,
					Hostname:  host.Hostname,
					Status:    host.Status,
					Connected: host.Status == state.RemoteHostStatusConnected,
				})
			}
			resp.FlavorHosts = append(resp.FlavorHosts, group)
		}
		response[i] = resp
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("failed to encode response", "handler", "remote-profile-statuses", "err", err)
	}
}

// handleRemoteConnectStream handles GET /api/remote/hosts/connect/stream
// This streams provisioning progress via Server-Sent Events (SSE).
func (s *Server) handleRemoteConnectStream(w http.ResponseWriter, r *http.Request) {
	profileID := r.URL.Query().Get("profile_id")
	flavorStr := r.URL.Query().Get("flavor")
	if profileID == "" || flavorStr == "" {
		writeJSONError(w, "profile_id and flavor required", http.StatusBadRequest)
		return
	}

	if s.remoteManager == nil {
		writeJSONError(w, "Remote workspace support not enabled", http.StatusServiceUnavailable)
		return
	}

	// Set SSE headers (CORS is handled by corsMiddleware)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, "Streaming not supported", http.StatusInternalServerError)
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

		_, err := s.remoteManager.ConnectWithProgress(ctx, profileID, flavorStr, progressCh)
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
