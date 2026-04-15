package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/preview"
	"github.com/sergeknystautas/schmux/internal/remote"
	"github.com/sergeknystautas/schmux/internal/state"
)

// RemoteHandlers groups all remote host handler methods.
type RemoteHandlers struct {
	config              *config.Config
	state               state.StateStore
	remoteManager       *remote.Manager
	previewManager      *preview.Manager
	logger              *log.Logger
	connectLimiter      *RateLimiter
	broadcastSessions   func()
	normalizeRateKey    func(r *http.Request) string
	authenticateRequest func(r *http.Request) (*authSession, error)
	authEnabled         func() bool
}

// Type aliases for contracts types used throughout this file.
type RemoteProfileResponse = contracts.RemoteProfileResponse
type RemoteFlavorResponse = contracts.RemoteFlavorResponse
type RemoteHostResponse = contracts.RemoteHostResponse
type RemoteProfileStatusResponse = contracts.RemoteProfileStatusResponse
type RemoteFlavorHostGroup = contracts.RemoteFlavorHostGroup
type RemoteFlavorStatusResponse = contracts.RemoteFlavorStatusResponse
type RemoteHostStatusItem = contracts.RemoteHostStatusItem

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
		HostType:              p.HostType,
	}
}

// handleGetRemoteProfiles returns all configured remote profiles.
func (h *RemoteHandlers) handleGetRemoteProfiles(w http.ResponseWriter, r *http.Request) {
	profiles := h.config.GetRemoteProfiles()
	response := make([]RemoteProfileResponse, len(profiles))
	for i, p := range profiles {
		response[i] = toProfileResponse(p)
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("failed to encode response", "handler", "remote-profiles", "err", err)
	}
}

// handleCreateRemoteProfile creates a new remote profile.
func (h *RemoteHandlers) handleCreateRemoteProfile(w http.ResponseWriter, r *http.Request) {
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

	if err := h.config.AddRemoteProfile(rp); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.config.Save(); err != nil {
		writeJSONError(w, "Failed to save config", http.StatusInternalServerError)
		return
	}

	// Find the added profile to get the generated ID
	profiles := h.config.GetRemoteProfiles()
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
		h.logger.Error("failed to encode response", "handler", "create-remote-profile", "err", err)
	}
}

// handleRemoteProfileGet handles GET /api/config/remote-profiles/{id}
func (h *RemoteHandlers) handleRemoteProfileGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONError(w, "Profile ID required", http.StatusBadRequest)
		return
	}

	profile, found := h.config.GetRemoteProfile(id)
	if !found {
		writeJSONError(w, "Profile not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toProfileResponse(profile))
}

// handleRemoteProfileUpdate handles PUT /api/config/remote-profiles/{id}
func (h *RemoteHandlers) handleRemoteProfileUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONError(w, "Profile ID required", http.StatusBadRequest)
		return
	}

	_, found := h.config.GetRemoteProfile(id)
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

	if err := h.config.UpdateRemoteProfile(rp); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.config.Save(); err != nil {
		writeJSONError(w, "Failed to save config", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toProfileResponse(rp))
}

// handleRemoteProfileDelete handles DELETE /api/config/remote-profiles/{id}
func (h *RemoteHandlers) handleRemoteProfileDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONError(w, "Profile ID required", http.StatusBadRequest)
		return
	}

	if err := h.config.RemoveRemoteProfile(id); err != nil {
		writeJSONError(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := h.config.Save(); err != nil {
		writeJSONError(w, "Failed to save config", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleRemoteHosts handles GET /api/remote/hosts
func (h *RemoteHandlers) handleRemoteHosts(w http.ResponseWriter, r *http.Request) {
	hosts := h.state.GetRemoteHosts()
	response := make([]RemoteHostResponse, len(hosts))

	for i, rh := range hosts {
		displayName := ""
		vcs := ""
		provisioningSessionID := ""

		if profile, found := h.config.GetRemoteProfile(rh.ProfileID); found {
			if resolved, err := config.ResolveProfileFlavor(profile, rh.Flavor); err == nil {
				displayName = resolved.FlavorDisplayName
				vcs = resolved.VCS
			} else {
				displayName = profile.DisplayName
				vcs = profile.VCS
			}
		}

		// Get provisioning session ID if available
		if h.remoteManager != nil {
			if conn := h.remoteManager.GetConnection(rh.ID); conn != nil {
				provisioningSessionID = conn.ProvisioningSessionID()
			}
		}

		response[i] = RemoteHostResponse{
			ID:                    rh.ID,
			ProfileID:             rh.ProfileID,
			Flavor:                rh.Flavor,
			DisplayName:           displayName,
			Hostname:              rh.Hostname,
			UUID:                  rh.UUID,
			Status:                rh.Status,
			Provisioned:           rh.Provisioned,
			VCS:                   vcs,
			ConnectedAt:           rh.ConnectedAt.Format("2006-01-02T15:04:05Z07:00"),
			ExpiresAt:             rh.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
			ProvisioningSessionID: provisioningSessionID,
			HostType:              rh.HostType,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("failed to encode response", "handler", "remote-hosts", "err", err)
	}
}

// handleRemoteHostConnect handles POST /api/remote/hosts/connect
// This starts a connection asynchronously and returns immediately.
// The client should poll /api/remote/hosts for status updates.
func (h *RemoteHandlers) handleRemoteHostConnect(w http.ResponseWriter, r *http.Request) {
	// Rate limiting by user (if auth enabled) or IP (without port)
	rateLimitKey := h.normalizeRateKey(r)
	if h.authEnabled() {
		if user, err := h.authenticateRequest(r); err == nil && user != nil {
			rateLimitKey = user.Login
		}
	}

	if !h.connectLimiter.Allow(rateLimitKey) {
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

	if h.remoteManager == nil {
		writeJSONError(w, "Remote workspace support not enabled", http.StatusServiceUnavailable)
		return
	}

	// Check if profile exists and resolve flavor
	profile, found := h.config.GetRemoteProfile(req.ProfileID)
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
	provisioningSessionID, err := h.remoteManager.StartConnect(req.ProfileID, req.Flavor)
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
		h.logger.Error("failed to encode response", "handler", "remote-host-connect", "err", err)
	}
}

// handleRemoteHostReconnect handles POST /api/remote/hosts/{hostID}/reconnect
// This starts reconnection asynchronously and returns immediately with a provisioning session ID.
// The client should open a WebSocket to /ws/provision/{provisioningSessionId} for interactive auth.
func (h *RemoteHandlers) handleRemoteHostReconnect(w http.ResponseWriter, r *http.Request) {
	hostID := chi.URLParam(r, "hostID")
	if hostID == "" {
		writeJSONError(w, "Host ID required", http.StatusBadRequest)
		return
	}

	if h.remoteManager == nil {
		writeJSONError(w, "Remote workspace support not enabled", http.StatusServiceUnavailable)
		return
	}

	host, found := h.state.GetRemoteHost(hostID)
	if !found {
		writeJSONError(w, "Host not found", http.StatusNotFound)
		return
	}

	displayName := ""
	vcs := ""
	if profile, found := h.config.GetRemoteProfile(host.ProfileID); found {
		if resolved, err := config.ResolveProfileFlavor(profile, host.Flavor); err == nil {
			displayName = resolved.FlavorDisplayName
			vcs = resolved.VCS
		} else {
			displayName = profile.DisplayName
			vcs = profile.VCS
		}
	}

	// Start reconnection asynchronously (returns provisioning session ID for WebSocket terminal)
	provisioningSessionID, err := h.remoteManager.StartReconnect(hostID, func(failedHostID string) {
		// Cleanup on failure
		remoteLog := logging.Sub(h.logger, "remote")
		remoteLog.Info("cleaning up failed reconnection", "host_id", failedHostID)
		for _, sess := range h.state.GetSessionsByRemoteHostID(failedHostID) {
			h.state.RemoveSession(sess.ID)
		}
		for _, ws := range h.state.GetWorkspacesByRemoteHostID(failedHostID) {
			h.state.RemoveWorkspace(ws.ID)
			if h.previewManager != nil {
				if err := h.previewManager.DeleteWorkspace(ws.ID); err != nil {
					previewLog := logging.Sub(h.logger, "preview")
					previewLog.Warn("remote cleanup failed", "workspace_id", ws.ID, "err", err)
				}
			}
		}
		h.state.RemoveRemoteHost(failedHostID)
		if err := h.state.Save(); err != nil {
			remoteLog.Error("failed to save state after cleanup", "err", err)
		}
		h.broadcastSessions()
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
		h.logger.Error("failed to encode response", "handler", "remote-host-reconnect", "err", err)
	}
}

// handleRemoteHostDisconnect handles DELETE /api/remote/hosts/{hostID}
func (h *RemoteHandlers) handleRemoteHostDisconnect(w http.ResponseWriter, r *http.Request) {
	hostID := chi.URLParam(r, "hostID")
	if hostID == "" {
		writeJSONError(w, "Host ID required", http.StatusBadRequest)
		return
	}

	dismiss := r.URL.Query().Get("dismiss") == "true"

	if dismiss {
		// Dismiss: remove all associated sessions, workspaces, and the host itself
		for _, sess := range h.state.GetSessionsByRemoteHostID(hostID) {
			h.state.RemoveSession(sess.ID)
		}
		for _, ws := range h.state.GetWorkspacesByRemoteHostID(hostID) {
			h.state.RemoveWorkspace(ws.ID)
		}
		h.state.RemoveRemoteHost(hostID)
		if h.remoteManager != nil {
			h.remoteManager.Disconnect(hostID)
		}
		if err := h.state.Save(); err != nil {
			writeJSONError(w, "Failed to save state", http.StatusInternalServerError)
			return
		}
	} else {
		// Default: disconnect only
		if h.remoteManager != nil {
			if err := h.remoteManager.Disconnect(hostID); err != nil {
				remoteLog := logging.Sub(h.logger, "remote")
				remoteLog.Warn("disconnect failed", "err", err)
			}
		} else {
			// Fallback: just update state
			if err := h.state.UpdateRemoteHostStatus(hostID, state.RemoteHostStatusDisconnected); err != nil {
				writeJSONError(w, fmt.Sprintf("Failed to update host: %v", err), http.StatusInternalServerError)
				return
			}
			if err := h.state.Save(); err != nil {
				writeJSONError(w, "Failed to save state", http.StatusInternalServerError)
				return
			}
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleRemoteProfileStatuses returns all profiles with their connection status.
func (h *RemoteHandlers) handleRemoteProfileStatuses(w http.ResponseWriter, r *http.Request) {
	profiles := h.config.GetRemoteProfiles()

	// If remote manager is available, use its real-time connection status
	if h.remoteManager != nil {
		statuses := h.remoteManager.GetProfileStatuses()
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
				for _, fh := range fg.Hosts {
					group.Hosts = append(group.Hosts, RemoteHostStatusItem{
						HostID:    fh.HostID,
						Hostname:  fh.Hostname,
						Status:    fh.Status,
						Connected: fh.Status == "connected",
					})
				}
				resp.FlavorHosts = append(resp.FlavorHosts, group)
			}
			response[i] = resp
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			h.logger.Error("failed to encode response", "handler", "remote-profile-statuses", "err", err)
		}
		return
	}

	// Fallback: use state-based connection status
	hosts := h.state.GetRemoteHosts()

	// Build a map of profileID -> flavor -> hosts
	type profileFlavorKey struct {
		profileID string
		flavor    string
	}
	pfToHosts := make(map[profileFlavorKey][]state.RemoteHost)
	for _, rh := range hosts {
		key := profileFlavorKey{rh.ProfileID, rh.Flavor}
		pfToHosts[key] = append(pfToHosts[key], rh)
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
		h.logger.Error("failed to encode response", "handler", "remote-profile-statuses", "err", err)
	}
}

// handleRemoteConnectStream handles GET /api/remote/hosts/connect/stream
// This streams provisioning progress via Server-Sent Events (SSE).
func (h *RemoteHandlers) handleRemoteConnectStream(w http.ResponseWriter, r *http.Request) {
	profileID := r.URL.Query().Get("profile_id")
	flavorStr := r.URL.Query().Get("flavor")
	if profileID == "" || flavorStr == "" {
		writeJSONError(w, "profile_id and flavor required", http.StatusBadRequest)
		return
	}

	if h.remoteManager == nil {
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

		_, err := h.remoteManager.ConnectWithProgress(ctx, profileID, flavorStr, progressCh)
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
