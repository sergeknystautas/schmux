package remote

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// Manager manages multiple remote host connections.
type Manager struct {
	config *config.Config
	state  *state.State
	logger *log.Logger

	connections map[string]*Connection // hostID -> connection
	mu          sync.RWMutex

	// Callback for state updates
	onStateChange func()
}

// NewManager creates a new remote host manager.
func NewManager(cfg *config.Config, st *state.State, logger *log.Logger) *Manager {
	return &Manager{
		config:      cfg,
		state:       st,
		logger:      logger,
		connections: make(map[string]*Connection),
	}
}

// SetStateChangeCallback sets a callback for when remote host state changes.
func (m *Manager) SetStateChangeCallback(cb func()) {
	m.mu.Lock()
	m.onStateChange = cb
	m.mu.Unlock()
}

// Connect connects to a remote host by flavor ID.
// Each call creates a new connection (multiple hosts per flavor are allowed).
func (m *Manager) Connect(ctx context.Context, flavorID string) (*Connection, error) {
	return m.connectInternal(ctx, flavorID, nil)
}

// StartConnect begins connecting to a remote host and returns immediately.
// Returns the provisioning session ID for WebSocket terminal streaming.
// The connection runs in the background; poll /api/remote/hosts for status updates.
func (m *Manager) StartConnect(flavorID string) (provisioningSessionID string, err error) {
	flavor, found := m.config.GetRemoteFlavor(flavorID)
	if !found {
		return "", fmt.Errorf("remote flavor not found: %s", flavorID)
	}

	// Create new connection (session ID is generated immediately in NewConnection)
	cfg := ConnectionConfigFromFlavor(flavor)
	cfg.OnStatusChange = m.handleStatusChange
	cfg.Logger = m.logger
	conn := NewConnection(cfg)

	// Register in map immediately so WebSocket handler can find it.
	m.mu.Lock()
	m.connections[conn.host.ID] = conn
	m.mu.Unlock()

	// Add to state (shows provisioning status in UI)
	m.state.AddRemoteHost(conn.Host())

	// Create workspace immediately so the host appears on the home page and in
	// WebSocket broadcasts as soon as it exists (not deferred to first spawn).
	// Host:Workspace is 1:1 — each remote host gets its own workspace.
	m.ensureWorkspaceForHost(conn.Host(), flavor)
	if err := m.state.Save(); err != nil {
		m.mu.Lock()
		delete(m.connections, conn.host.ID)
		m.mu.Unlock()
		return "", fmt.Errorf("failed to persist state: %w", err)
	}
	m.notifyStateChange()

	sessionID := conn.ProvisioningSessionID()

	if m.logger != nil {
		m.logger.Info("StartConnect", "host_id", conn.host.ID, "flavor", flavorID, "session_id", sessionID)
	}

	// Connect in background with a hard timeout on provisioning.
	// 5 minutes covers slow SSH auth, MFA prompts, and initial connection setup.
	// If provisioning exceeds this, the connection transitions to "failed".
	// The context is also canceled when conn.Close() is called (e.g., during daemon shutdown)
	// so the goroutine doesn't block for 5 minutes.
	//
	// IMPORTANT: Create the context and set the cancel BEFORE starting the goroutine.
	// If Close() is called before the goroutine starts, SetConnectCancel inside the
	// goroutine would race with Close()'s connectCancel() call and the cancel would
	// never fire, leaving the goroutine blocked for the full 5-minute timeout.
	connectCtx, connectCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	conn.SetConnectCancel(connectCancel)

	go func() {
		ctx := connectCtx
		defer connectCancel()

		if err := conn.Connect(ctx); err != nil {
			if m.logger != nil {
				m.logger.Error("connection failed", "flavor", flavorID, "err", err)
			}
			// Keep connection in map so provisioning_session_id remains available
			// for the frontend ConnectionProgressModal polling to detect failure.
			conn.mu.Lock()
			conn.host.Status = state.RemoteHostStatusFailed
			conn.mu.Unlock()
			m.state.UpdateRemoteHostStatus(conn.host.ID, state.RemoteHostStatusFailed)
			m.state.SaveBatched()
			m.notifyStateChange()
			return
		}

		// Run provisioning if needed
		host := conn.Host()
		if !host.Provisioned && flavor.ProvisionCommand != "" {
			if m.logger != nil {
				m.logger.Info("running provision command", "host_id", host.ID)
			}
			// Revert status to "provisioning" so the ConnectionProgressModal stays
			// open with a spinner while tools are being installed. Without this,
			// the modal sees "connected" and closes, leaving the user with no
			// feedback during a potentially long provision command.
			conn.mu.Lock()
			conn.host.Status = state.RemoteHostStatusProvisioning
			conn.mu.Unlock()
			m.state.UpdateRemoteHostStatus(conn.host.ID, state.RemoteHostStatusProvisioning)
			m.notifyStateChange()

			if err := conn.Provision(ctx, flavor.ProvisionCommand); err != nil {
				if m.logger != nil {
					m.logger.Error("provision failed", "err", err)
				}
			} else {
				m.state.UpdateRemoteHostProvisioned(host.ID, true)
			}

			// Restore connected status now that provisioning is complete
			conn.mu.Lock()
			conn.host.Status = state.RemoteHostStatusConnected
			conn.mu.Unlock()
			m.state.UpdateRemoteHostStatus(conn.host.ID, state.RemoteHostStatusConnected)
			m.notifyStateChange()
		}

		// Update state with final host info (hostname may have been extracted)
		finalHost := conn.Host()
		m.state.UpdateRemoteHost(finalHost)

		// Update workspace branch to hostname now that it's known
		if finalHost.Hostname != "" {
			workspaceID := fmt.Sprintf("remote-%s", finalHost.ID)
			if ws, found := m.state.GetWorkspace(workspaceID); found && ws.Branch != finalHost.Hostname {
				ws.Branch = finalHost.Hostname
				m.state.UpdateWorkspace(ws)
			}
		}

		if err := m.state.Save(); err != nil {
			if m.logger != nil {
				m.logger.Error("failed to persist final state", "err", err)
			}
		}
		m.notifyStateChange()
	}()

	return sessionID, nil
}

// ConnectWithProgress connects to a remote host and streams progress updates.
// Progress messages are sent to the provided channel.
func (m *Manager) ConnectWithProgress(ctx context.Context, flavorID string, progressCh chan<- string) (*Connection, error) {
	// Progress callback to forward messages to channel
	onProgress := func(msg string) {
		// Non-blocking send to prevent panic if channel is closed or full
		select {
		case progressCh <- msg:
		default:
			// Drop if channel is closed or full - client may have disconnected
		}
	}
	return m.connectInternal(ctx, flavorID, onProgress)
}

// connectInternal is the shared implementation for Connect and ConnectWithProgress.
// The onProgress callback is optional - if nil, progress messages are not sent.
func (m *Manager) connectInternal(ctx context.Context, flavorID string, onProgress func(string)) (*Connection, error) {
	// Look up flavor configuration
	flavor, found := m.config.GetRemoteFlavor(flavorID)
	if !found {
		return nil, fmt.Errorf("remote flavor not found: %s", flavorID)
	}

	// Create new connection
	if onProgress != nil {
		onProgress("provisioning new host")
	}

	cfg := ConnectionConfigFromFlavor(flavor)
	cfg.OnStatusChange = m.handleStatusChange
	cfg.OnProgress = onProgress
	cfg.Logger = m.logger
	conn := NewConnection(cfg)

	// Add to state before connecting (shows provisioning status)
	m.state.AddRemoteHost(conn.Host())
	m.ensureWorkspaceForHost(conn.Host(), flavor)
	if err := m.state.Save(); err != nil {
		return nil, fmt.Errorf("failed to persist state: %w", err)
	}
	m.notifyStateChange()

	// Connect
	if onProgress != nil {
		onProgress("connecting to remote host")
	}
	if err := conn.Connect(ctx); err != nil {
		// Update status to disconnected (batched save on error path)
		m.state.UpdateRemoteHostStatus(conn.host.ID, state.RemoteHostStatusDisconnected)
		m.state.SaveBatched()
		m.notifyStateChange()
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	// Run provisioning if needed (first connection only)
	host := conn.Host()
	if !host.Provisioned && flavor.ProvisionCommand != "" {
		if m.logger != nil {
			m.logger.Info("running provision command", "host_id", host.ID)
		}
		if err := conn.Provision(ctx, flavor.ProvisionCommand); err != nil {
			if m.logger != nil {
				m.logger.Error("provision failed", "err", err)
			}
			// Don't fail the connection, but log the error
			// User can manually re-provision or fix the command
		} else {
			// Mark as provisioned (batched save for status update)
			m.state.UpdateRemoteHostProvisioned(host.ID, true)
			m.state.SaveBatched()
			m.notifyStateChange()
		}
	}

	// Store connection
	m.mu.Lock()
	m.connections[conn.host.ID] = conn
	m.mu.Unlock()

	// Update state with final host info (hostname may have been extracted)
	finalHost := conn.Host()
	m.state.UpdateRemoteHost(finalHost)

	// Update workspace branch to hostname now that it's known
	if finalHost.Hostname != "" {
		workspaceID := fmt.Sprintf("remote-%s", finalHost.ID)
		if ws, found := m.state.GetWorkspace(workspaceID); found && ws.Branch != finalHost.Hostname {
			ws.Branch = finalHost.Hostname
			m.state.UpdateWorkspace(ws)
		}
	}

	if err := m.state.Save(); err != nil {
		return nil, fmt.Errorf("failed to persist state: %w", err)
	}
	m.notifyStateChange()

	if onProgress != nil {
		onProgress("connection established")
	}
	return conn, nil
}

// Reconnect reconnects to an existing host by ID.
func (m *Manager) Reconnect(ctx context.Context, hostID string) (*Connection, error) {
	// Get host from state
	host, found := m.state.GetRemoteHost(hostID)
	if !found {
		return nil, fmt.Errorf("remote host not found: %s", hostID)
	}

	// If hostname is missing from state, try the live connection
	if host.Hostname == "" {
		m.mu.RLock()
		conn, exists := m.connections[hostID]
		m.mu.RUnlock()
		if exists {
			if liveHostname := conn.Hostname(); liveHostname != "" {
				host.Hostname = liveHostname
				m.state.UpdateRemoteHost(conn.Host())
				if err := m.state.Save(); err != nil {
					if m.logger != nil {
						m.logger.Error("failed to save state", "err", err)
					}
				}
			}
		}
	}

	if host.Hostname == "" {
		return nil, fmt.Errorf("remote host has no hostname: %s", hostID)
	}

	// Get flavor configuration
	flavor, found := m.config.GetRemoteFlavor(host.FlavorID)
	if !found {
		return nil, fmt.Errorf("remote flavor not found: %s", host.FlavorID)
	}

	// Create new connection for reconnection
	cfg := ConnectionConfigFromFlavor(flavor)
	cfg.OnStatusChange = m.handleStatusChange
	cfg.Logger = m.logger
	conn := NewConnection(cfg)

	// Use existing host ID
	conn.host.ID = host.ID
	conn.host.Hostname = host.Hostname
	conn.host.UUID = host.UUID

	// Reconnect
	if err := conn.Reconnect(ctx, host.Hostname); err != nil {
		m.state.UpdateRemoteHostStatus(hostID, state.RemoteHostStatusDisconnected)
		if err := m.state.Save(); err != nil {
			if m.logger != nil {
				m.logger.Error("failed to save state", "err", err)
			}
		}
		m.notifyStateChange()
		return nil, fmt.Errorf("reconnection failed: %w", err)
	}

	// Store connection
	m.mu.Lock()
	// Close any existing connection
	if existing, exists := m.connections[hostID]; exists {
		existing.Close()
	}
	m.connections[hostID] = conn
	m.mu.Unlock()

	// Reconcile sessions with discovered windows
	if err := m.reconcileSessions(ctx, conn); err != nil {
		if m.logger != nil {
			m.logger.Warn("failed to reconcile sessions", "err", err)
		}
		// Don't fail reconnection if reconciliation fails
	}

	// Update state
	m.state.UpdateRemoteHost(conn.Host())
	if err := m.state.Save(); err != nil {
		return nil, fmt.Errorf("failed to persist state: %w", err)
	}
	m.notifyStateChange()

	return conn, nil
}

// GetConnection returns the connection for a host ID.
// Returns nil if not connected.
func (m *Manager) GetConnection(hostID string) *Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connections[hostID]
}

// RunCommand executes a command on a remote host and returns its output.
func (m *Manager) RunCommand(ctx context.Context, hostID, workdir, command string) (string, error) {
	conn := m.GetConnection(hostID)
	if conn == nil {
		return "", fmt.Errorf("no connection for host: %s", hostID)
	}
	return conn.RunCommand(ctx, workdir, command)
}

// GetConnectionsByFlavorID returns all connections for a flavor (may be empty).
func (m *Manager) GetConnectionsByFlavorID(flavorID string) []*Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var conns []*Connection
	for _, conn := range m.connections {
		if conn.flavor.ID == flavorID {
			conns = append(conns, conn)
		}
	}
	return conns
}

// IsConnected checks if a host is connected.
func (m *Manager) IsConnected(hostID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	conn, exists := m.connections[hostID]
	return exists && conn.IsConnected()
}

// IsFlavorConnected checks if a flavor has at least one active connection
// across all hosts provisioned for that flavor.
func (m *Manager) IsFlavorConnected(flavorID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, conn := range m.connections {
		if conn.flavor.ID == flavorID && conn.IsConnected() {
			return true
		}
	}
	return false
}

// Disconnect closes a connection by host ID.
func (m *Manager) Disconnect(hostID string) error {
	m.mu.Lock()
	conn, exists := m.connections[hostID]
	if exists {
		delete(m.connections, hostID)
	}
	m.mu.Unlock()

	if !exists {
		return nil
	}

	err := conn.Close()

	// Update state
	m.state.UpdateRemoteHostStatus(hostID, state.RemoteHostStatusDisconnected)
	if err := m.state.Save(); err != nil {
		if m.logger != nil {
			m.logger.Error("failed to save state", "err", err)
		}
	}
	m.notifyStateChange()

	return err
}

// DisconnectAll closes all connections.
func (m *Manager) DisconnectAll() {
	m.mu.Lock()
	connections := make([]*Connection, 0, len(m.connections))
	for _, conn := range m.connections {
		connections = append(connections, conn)
	}
	m.connections = make(map[string]*Connection)
	m.mu.Unlock()

	for _, conn := range connections {
		conn.Close()
		m.state.UpdateRemoteHostStatus(conn.host.ID, state.RemoteHostStatusDisconnected)
	}
	if err := m.state.Save(); err != nil {
		if m.logger != nil {
			m.logger.Error("failed to save state", "err", err)
		}
	}
	m.notifyStateChange()
}

// GetActiveConnections returns all active connections.
func (m *Manager) GetActiveConnections() []*Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Connection, 0, len(m.connections))
	for _, conn := range m.connections {
		if conn.IsConnected() {
			result = append(result, conn)
		}
	}
	return result
}

// handleStatusChange is called when a connection's status changes.
func (m *Manager) handleStatusChange(hostID, status string) {
	// Try to persist the full host state (including hostname) from the live
	// connection, not just the status. This ensures that fields set on the
	// connection object (e.g., hostname extracted during provisioning) are
	// persisted to state as soon as a status change occurs.
	m.mu.RLock()
	conn, exists := m.connections[hostID]
	m.mu.RUnlock()
	if exists {
		m.state.UpdateRemoteHost(conn.Host())
	} else {
		m.state.UpdateRemoteHostStatus(hostID, status)
	}
	if err := m.state.Save(); err != nil {
		if m.logger != nil {
			m.logger.Error("failed to save state", "err", err)
		}
	}
	m.notifyStateChange()
}

// ensureWorkspaceForHost creates a workspace for a remote host if one doesn't
// already exist. This is called immediately when a host is created (not deferred
// to SpawnRemote) so the workspace appears on the home page right away.
func (m *Manager) ensureWorkspaceForHost(host state.RemoteHost, flavor config.RemoteFlavor) {
	workspaceID := fmt.Sprintf("remote-%s", host.ID)
	if _, found := m.state.GetWorkspace(workspaceID); found {
		return // Already exists
	}

	branch := host.Hostname
	if branch == "" {
		branch = flavor.DisplayName
	}

	ws := state.Workspace{
		ID:           workspaceID,
		Repo:         flavor.DisplayName,
		Branch:       branch,
		Path:         flavor.WorkspacePath,
		RemoteHostID: host.ID,
		RemotePath:   flavor.WorkspacePath,
	}
	m.state.AddWorkspace(ws)
}

// notifyStateChange calls the state change callback if set.
func (m *Manager) notifyStateChange() {
	m.mu.RLock()
	cb := m.onStateChange
	m.mu.RUnlock()
	if cb != nil {
		cb()
	}
}

// PruneExpiredHosts removes hosts that have expired from state.
func (m *Manager) PruneExpiredHosts() {
	now := time.Now()
	hosts := m.state.GetRemoteHosts()

	pruned := 0
	for _, host := range hosts {
		if host.ExpiresAt.Before(now) {
			// Disconnect if connected
			m.mu.Lock()
			if conn, exists := m.connections[host.ID]; exists {
				if m.logger != nil {
					m.logger.Info("expiring host", "host_id", host.ID, "hostname", host.Hostname,
						"connected_at", host.ConnectedAt.Format(time.RFC3339),
						"expired_at", host.ExpiresAt.Format(time.RFC3339))
				}
				conn.Close()
				delete(m.connections, host.ID)
				pruned++
			}
			m.mu.Unlock()

			// Update status to expired
			host.Status = state.RemoteHostStatusExpired
			m.state.UpdateRemoteHost(host)
		}
	}

	if pruned > 0 {
		if m.logger != nil {
			m.logger.Info("pruned expired hosts", "count", pruned)
		}
		if err := m.state.Save(); err != nil {
			if m.logger != nil {
				m.logger.Error("failed to save state", "err", err)
			}
		}
		m.notifyStateChange()
	}
}

// GetHostForSession finds the connection for a session by its remote host ID.
func (m *Manager) GetHostForSession(sess state.Session) *Connection {
	if sess.RemoteHostID == "" {
		return nil
	}
	return m.GetConnection(sess.RemoteHostID)
}

// StartReconnect begins reconnecting to a remote host and returns immediately.
// Returns the provisioning session ID for WebSocket terminal streaming.
// The onFail callback is called if reconnection fails (for cleanup).
func (m *Manager) StartReconnect(hostID string, onFail func(hostID string)) (provisioningSessionID string, err error) {
	// Get host from state
	host, found := m.state.GetRemoteHost(hostID)
	if !found {
		return "", fmt.Errorf("remote host not found: %s", hostID)
	}

	// If hostname is missing from state, try the live connection
	if host.Hostname == "" {
		m.mu.RLock()
		conn, exists := m.connections[hostID]
		m.mu.RUnlock()
		if exists {
			if liveHostname := conn.Hostname(); liveHostname != "" {
				host.Hostname = liveHostname
				m.state.UpdateRemoteHost(conn.Host())
				if err := m.state.Save(); err != nil {
					if m.logger != nil {
						m.logger.Error("failed to save state", "err", err)
					}
				}
			}
		}
	}

	if host.Hostname == "" {
		return "", fmt.Errorf("remote host has no hostname: %s", hostID)
	}

	// Get flavor configuration
	flavor, found := m.config.GetRemoteFlavor(host.FlavorID)
	if !found {
		return "", fmt.Errorf("remote flavor not found: %s", host.FlavorID)
	}

	// Check if already reconnecting or connected
	m.mu.RLock()
	if conn, exists := m.connections[hostID]; exists {
		status := conn.Status()
		if conn.IsConnected() || status == state.RemoteHostStatusReconnecting || status == state.RemoteHostStatusConnecting {
			sid := conn.ProvisioningSessionID()
			m.mu.RUnlock()
			return sid, nil
		}
	}
	m.mu.RUnlock()

	// Create new connection for reconnection
	cfg := ConnectionConfigFromFlavor(flavor)
	cfg.OnStatusChange = m.handleStatusChange
	cfg.Logger = m.logger
	conn := NewConnection(cfg)

	// Use existing host ID and provisioning session ID pattern
	conn.host.ID = host.ID
	conn.host.Hostname = host.Hostname
	conn.host.UUID = host.UUID
	conn.provisioningSessionID = fmt.Sprintf("provision-%s", host.ID)

	// Register in map immediately so WebSocket handler can find it
	m.mu.Lock()
	// Close any existing connection
	if existing, exists := m.connections[hostID]; exists {
		existing.Close()
	}
	m.connections[hostID] = conn
	m.mu.Unlock()

	// Update state to reconnecting
	m.state.UpdateRemoteHostStatus(hostID, state.RemoteHostStatusReconnecting)
	if err := m.state.Save(); err != nil {
		if m.logger != nil {
			m.logger.Error("failed to persist reconnecting state", "err", err)
		}
	}
	m.notifyStateChange()

	sessionID := conn.ProvisioningSessionID()

	if m.logger != nil {
		m.logger.Info("StartReconnect", "host_id", hostID, "hostname", host.Hostname, "session_id", sessionID)
	}

	// Reconnect in background
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		if err := conn.Reconnect(ctx, host.Hostname); err != nil {
			if m.logger != nil {
				m.logger.Error("reconnection failed", "host_id", hostID, "hostname", host.Hostname, "err", err)
			}
			// Keep connection in map so provisioning_session_id remains available
			// for the frontend ConnectionProgressModal polling to detect failure.
			conn.mu.Lock()
			conn.host.Status = state.RemoteHostStatusDisconnected
			conn.mu.Unlock()
			m.state.UpdateRemoteHostStatus(hostID, state.RemoteHostStatusDisconnected)
			m.state.SaveBatched()
			m.notifyStateChange()

			// Call failure callback for cleanup
			if onFail != nil {
				onFail(hostID)
			}
			return
		}

		// Re-run provisioning on reconnect: Xvfb is a process that dies on
		// host reboot, and ephemeral hosts may lose installed packages.
		// The provision command should be idempotent (e.g., dnf install -y is a no-op
		// if already installed, pgrep Xvfb || Xvfb :99 & only starts if not running).
		if flavor.ProvisionCommand != "" {
			if m.logger != nil {
				m.logger.Info("re-running provision command on reconnect", "host_id", hostID)
			}
			if err := conn.Provision(ctx, flavor.ProvisionCommand); err != nil {
				if m.logger != nil {
					m.logger.Error("provision on reconnect failed", "err", err)
				}
			}
		}

		// Reconcile sessions with discovered windows
		if err := m.reconcileSessions(ctx, conn); err != nil {
			if m.logger != nil {
				m.logger.Warn("failed to reconcile sessions after reconnect", "err", err)
			}
		}

		// Update state with final host info
		m.state.UpdateRemoteHost(conn.Host())
		if err := m.state.Save(); err != nil {
			if m.logger != nil {
				m.logger.Error("failed to persist final state", "err", err)
			}
		}
		m.notifyStateChange()
	}()

	return sessionID, nil
}

// MarkStaleHostsDisconnected marks all hosts that were "connected" in state as
// "disconnected" at daemon startup. After a daemon restart, SSH/ET processes are
// gone so these hosts are stale. We don't auto-reconnect because reconnection
// typically requires interactive authentication (e.g., Yubikey touch) that can
// only happen when the user explicitly clicks "Reconnect" in the dashboard.
// Returns the number of hosts marked as disconnected.
func (m *Manager) MarkStaleHostsDisconnected() int {
	hosts := m.state.GetRemoteHosts()
	count := 0

	for _, host := range hosts {
		if host.Status != state.RemoteHostStatusConnected || host.Hostname == "" {
			continue
		}

		// Skip expired hosts (handled separately by PruneExpiredHosts)
		if host.ExpiresAt.Before(time.Now()) {
			if m.logger != nil {
				m.logger.Debug("skipping expired host", "host_id", host.ID, "hostname", host.Hostname)
			}
			continue
		}

		if m.logger != nil {
			m.logger.Info("marking stale host as disconnected", "host_id", host.ID, "hostname", host.Hostname)
		}
		m.state.UpdateRemoteHostStatus(host.ID, state.RemoteHostStatusDisconnected)
		count++
	}

	if count > 0 {
		if err := m.state.Save(); err != nil {
			if m.logger != nil {
				m.logger.Error("failed to save state", "err", err)
			}
		}
		m.notifyStateChange()
	}

	return count
}

// HostStatus represents the status of a single remote host within a flavor.
type HostStatus struct {
	HostID   string `json:"host_id"`
	Hostname string `json:"hostname"`
	Status   string `json:"status"`
}

// FlavorStatus represents a flavor with the status of all its hosts.
type FlavorStatus struct {
	Flavor config.RemoteFlavor `json:"flavor"`
	Hosts  []HostStatus        `json:"hosts"`
}

// GetFlavorStatuses returns all configured flavors with the status of all their hosts.
func (m *Manager) GetFlavorStatuses() []FlavorStatus {
	flavors := m.config.GetRemoteFlavors()
	result := make([]FlavorStatus, len(flavors))

	m.mu.RLock()
	defer m.mu.RUnlock()

	for i, flavor := range flavors {
		status := FlavorStatus{
			Flavor: flavor,
			Hosts:  []HostStatus{},
		}

		// Collect all hosts for this flavor
		for _, conn := range m.connections {
			if conn.flavor.ID == flavor.ID {
				status.Hosts = append(status.Hosts, HostStatus{
					HostID:   conn.host.ID,
					Hostname: conn.Hostname(),
					Status:   conn.Status(),
				})
			}
		}

		result[i] = status
	}

	return result
}

// GetHostConnectionStatus returns the live connection status for a remote host.
// Returns the status string and whether the host has a live connection object.
// This should be used instead of reading state directly, since state can be stale after restarts.
func (m *Manager) GetHostConnectionStatus(hostID string) (status string, hasConnection bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, exists := m.connections[hostID]
	if !exists {
		return state.RemoteHostStatusDisconnected, false
	}
	return conn.Status(), true
}

// reconcileSessions reconciles state sessions with windows discovered on the remote host.
// This is called after reconnection to restore session window/pane IDs.
func (m *Manager) reconcileSessions(ctx context.Context, conn *Connection) error {
	// List windows on the remote host
	windows, err := conn.ListSessions(ctx)
	if err != nil {
		return fmt.Errorf("failed to list windows: %w", err)
	}

	if len(windows) == 0 {
		if m.logger != nil {
			m.logger.Info("no windows found on host", "host_id", conn.host.ID)
		}
		return nil
	}

	// Get all sessions for this host from state
	sessions := m.state.GetSessions()
	reconciledCount := 0
	disconnectedCount := 0

	for _, sess := range sessions {
		if sess.RemoteHostID != conn.host.ID {
			continue
		}

		// Match sessions with windows using IDs ONLY (strict matching per Issue 4 fix)
		// Priority: window ID > pane ID
		// DO NOT fall back to window name matching - names can change and cause wrong matches
		var matched bool
		for _, w := range windows {
			// Try to match by window ID (most reliable)
			if sess.RemoteWindow != "" && w.WindowID == sess.RemoteWindow {
				matched = true
			} else if sess.RemotePaneID != "" && w.PaneID == sess.RemotePaneID {
				// Fallback: match by pane ID
				matched = true
			}

			if matched {
				// Session still exists! Update pane and window IDs
				sess.RemotePaneID = w.PaneID
				sess.RemoteWindow = w.WindowID
				sess.Status = "running" // Ensure status is running
				m.state.UpdateSession(sess)
				reconciledCount++
				if m.logger != nil {
					m.logger.Info("rediscovered session", "session_id", sess.ID, "window", w.WindowID, "pane", w.PaneID)
				}
				break
			}
		}

		// If no match found by ID, mark session as disconnected
		if !matched && sess.Status != "disconnected" {
			sess.Status = "disconnected"
			m.state.UpdateSession(sess)
			disconnectedCount++
			if m.logger != nil {
				m.logger.Warn("could not reconcile session (no ID match), marked as disconnected", "session_id", sess.ID)
			}
		}
	}

	if reconciledCount > 0 || disconnectedCount > 0 {
		if m.logger != nil {
			m.logger.Info("reconciled sessions", "reconciled", reconciledCount, "disconnected", disconnectedCount, "host_id", conn.host.ID)
		}
		if err := m.state.Save(); err != nil {
			if m.logger != nil {
				m.logger.Error("failed to save state", "err", err)
			}
		}
	}

	return nil
}
