package remote

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// Manager manages multiple remote host connections.
type Manager struct {
	config *config.Config
	state  *state.State

	connections map[string]*Connection // hostID -> connection
	mu          sync.RWMutex
	connectMu   sync.Mutex // Serializes connection creation

	// Callback for state updates
	onStateChange func()
}

// NewManager creates a new remote host manager.
func NewManager(cfg *config.Config, st *state.State) *Manager {
	return &Manager{
		config:      cfg,
		state:       st,
		connections: make(map[string]*Connection),
	}
}

// SetStateChangeCallback sets a callback for when remote host state changes.
func (m *Manager) SetStateChangeCallback(cb func()) {
	m.onStateChange = cb
}

// Connect connects to a remote host by flavor ID.
// If already connected, returns the existing connection.
// If the host is cached but disconnected, attempts to reconnect.
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

	// Check existing connections
	m.mu.RLock()
	for _, conn := range m.connections {
		if conn.flavor.ID == flavorID {
			status := conn.Status()
			if conn.IsConnected() || status == state.RemoteHostStatusProvisioning || status == state.RemoteHostStatusAuthenticating {
				sid := conn.ProvisioningSessionID()
				m.mu.RUnlock()
				return sid, nil
			}
		}
	}
	m.mu.RUnlock()

	// Acquire creation lock to prevent duplicate connections
	m.connectMu.Lock()
	defer m.connectMu.Unlock()

	// Double-check after acquiring lock
	m.mu.RLock()
	for _, conn := range m.connections {
		if conn.flavor.ID == flavorID {
			status := conn.Status()
			if conn.IsConnected() || status == state.RemoteHostStatusProvisioning || status == state.RemoteHostStatusAuthenticating {
				sid := conn.ProvisioningSessionID()
				m.mu.RUnlock()
				return sid, nil
			}
		}
	}
	m.mu.RUnlock()

	// Create new connection (session ID is generated immediately in NewConnection)
	conn := NewConnection(ConnectionConfig{
		FlavorID:         flavorID,
		Flavor:           flavor.Flavor,
		DisplayName:      flavor.DisplayName,
		WorkspacePath:    flavor.WorkspacePath,
		VCS:              flavor.VCS,
		ConnectCommand:   flavor.ConnectCommand,
		ReconnectCommand: flavor.ReconnectCommand,
		ProvisionCommand: flavor.ProvisionCommand,
		HostnameRegex:    flavor.HostnameRegex,
		OnStatusChange:   m.handleStatusChange,
	})

	// Register in map immediately so WebSocket handler can find it
	m.mu.Lock()
	m.connections[conn.host.ID] = conn
	m.mu.Unlock()

	// Add to state (shows provisioning status in UI)
	m.state.AddRemoteHost(conn.Host())
	if err := m.state.Save(); err != nil {
		m.mu.Lock()
		delete(m.connections, conn.host.ID)
		m.mu.Unlock()
		return "", fmt.Errorf("failed to persist state: %w", err)
	}
	m.notifyStateChange()

	sessionID := conn.ProvisioningSessionID()

	fmt.Printf("[remote] StartConnect: host=%s flavor=%s sessionID=%s\n", conn.host.ID, flavorID, sessionID)

	// Connect in background
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		if err := conn.Connect(ctx); err != nil {
			fmt.Printf("[remote] connection to %s failed: %v\n", flavorID, err)
			m.mu.Lock()
			delete(m.connections, conn.host.ID)
			m.mu.Unlock()
			m.state.UpdateRemoteHostStatus(conn.host.ID, state.RemoteHostStatusDisconnected)
			m.state.SaveBatched()
			m.notifyStateChange()
			return
		}

		// Run provisioning if needed
		host := conn.Host()
		if !host.Provisioned && flavor.ProvisionCommand != "" {
			fmt.Printf("[remote] running provision command for host %s\n", host.ID)
			if err := conn.Provision(ctx, flavor.ProvisionCommand); err != nil {
				fmt.Printf("[remote] provision failed: %v\n", err)
			} else {
				m.state.UpdateRemoteHostProvisioned(host.ID, true)
				m.state.SaveBatched()
				m.notifyStateChange()
			}
		}

		// Update state with final host info
		m.state.UpdateRemoteHost(conn.Host())
		if err := m.state.Save(); err != nil {
			fmt.Printf("[remote] failed to persist final state: %v\n", err)
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

	// First check with read lock
	m.mu.RLock()
	for _, conn := range m.connections {
		if conn.flavor.ID == flavorID {
			status := conn.Status()
			// Return existing if connected or in progress
			if conn.IsConnected() || status == state.RemoteHostStatusProvisioning || status == state.RemoteHostStatusAuthenticating {
				m.mu.RUnlock()
				if onProgress != nil {
					if conn.IsConnected() {
						onProgress("already connected")
					} else {
						onProgress("connection in progress")
					}
				}
				return conn, nil
			}
		}
	}
	m.mu.RUnlock()

	// Not found - acquire creation lock to prevent race
	m.connectMu.Lock()
	defer m.connectMu.Unlock()

	// Double-check after acquiring lock (another goroutine may have created it)
	m.mu.RLock()
	for _, conn := range m.connections {
		if conn.flavor.ID == flavorID {
			status := conn.Status()
			// Return existing if connected or in progress
			if conn.IsConnected() || status == state.RemoteHostStatusProvisioning || status == state.RemoteHostStatusAuthenticating {
				m.mu.RUnlock()
				if onProgress != nil {
					if conn.IsConnected() {
						onProgress("already connected")
					} else {
						onProgress("connection in progress")
					}
				}
				return conn, nil
			}
		}
	}
	m.mu.RUnlock()

	// Still not found - proceed with connection creation

	// Check if we have a cached host in state that we can reconnect to
	host, found := m.state.GetRemoteHostByFlavorID(flavorID)
	if found && host.Hostname != "" && !host.ExpiresAt.Before(time.Now()) {
		if onProgress != nil {
			onProgress(fmt.Sprintf("reconnecting to %s", host.Hostname))
		}

		// Try to reconnect to existing host
		conn := NewConnection(ConnectionConfig{
			FlavorID:         flavorID,
			Flavor:           flavor.Flavor,
			DisplayName:      flavor.DisplayName,
			WorkspacePath:    flavor.WorkspacePath,
			VCS:              flavor.VCS,
			ConnectCommand:   flavor.ConnectCommand,
			ReconnectCommand: flavor.ReconnectCommand,
			ProvisionCommand: flavor.ProvisionCommand,
			HostnameRegex:    flavor.HostnameRegex,
			OnStatusChange:   m.handleStatusChange,
			OnProgress:       onProgress,
		})

		// Update existing host ID
		conn.host.ID = host.ID
		conn.host.Hostname = host.Hostname
		conn.host.UUID = host.UUID

		if err := conn.Reconnect(ctx, host.Hostname); err != nil {
			// Reconnection failed, fall through to new connection
			if onProgress != nil {
				onProgress(fmt.Sprintf("reconnection failed: %v, provisioning new host", err))
			} else {
				fmt.Printf("[remote] reconnection to %s failed: %v, provisioning new host\n", host.Hostname, err)
			}
		} else {
			// Reconnection successful
			m.mu.Lock()
			m.connections[conn.host.ID] = conn
			m.mu.Unlock()

			// Update state (use batched save for status updates)
			m.state.UpdateRemoteHost(conn.Host())
			m.state.SaveBatched()
			m.notifyStateChange()

			if onProgress != nil {
				onProgress("reconnected successfully")
			}
			return conn, nil
		}
	}

	// Create new connection
	if onProgress != nil {
		onProgress("provisioning new host")
	}

	conn := NewConnection(ConnectionConfig{
		FlavorID:         flavorID,
		Flavor:           flavor.Flavor,
		DisplayName:      flavor.DisplayName,
		WorkspacePath:    flavor.WorkspacePath,
		VCS:              flavor.VCS,
		ConnectCommand:   flavor.ConnectCommand,
		ReconnectCommand: flavor.ReconnectCommand,
		ProvisionCommand: flavor.ProvisionCommand,
		HostnameRegex:    flavor.HostnameRegex,
		OnStatusChange:   m.handleStatusChange,
		OnProgress:       onProgress,
	})

	// Add to state before connecting (shows provisioning status)
	m.state.AddRemoteHost(conn.Host())
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
	host = conn.Host()
	if !host.Provisioned && flavor.ProvisionCommand != "" {
		fmt.Printf("[remote] running provision command for host %s\n", host.ID)
		if err := conn.Provision(ctx, flavor.ProvisionCommand); err != nil {
			fmt.Printf("[remote] provision failed: %v\n", err)
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

	// Update state with final host info
	m.state.UpdateRemoteHost(conn.Host())
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

	if host.Hostname == "" {
		return nil, fmt.Errorf("remote host has no hostname: %s", hostID)
	}

	// Get flavor configuration
	flavor, found := m.config.GetRemoteFlavor(host.FlavorID)
	if !found {
		return nil, fmt.Errorf("remote flavor not found: %s", host.FlavorID)
	}

	// Create new connection for reconnection
	conn := NewConnection(ConnectionConfig{
		FlavorID:         flavor.ID,
		Flavor:           flavor.Flavor,
		DisplayName:      flavor.DisplayName,
		WorkspacePath:    flavor.WorkspacePath,
		VCS:              flavor.VCS,
		ConnectCommand:   flavor.ConnectCommand,
		ReconnectCommand: flavor.ReconnectCommand,
		ProvisionCommand: flavor.ProvisionCommand,
		HostnameRegex:    flavor.HostnameRegex,
		OnStatusChange:   m.handleStatusChange,
	})

	// Use existing host ID
	conn.host.ID = host.ID
	conn.host.Hostname = host.Hostname
	conn.host.UUID = host.UUID

	// Reconnect
	if err := conn.Reconnect(ctx, host.Hostname); err != nil {
		m.state.UpdateRemoteHostStatus(hostID, state.RemoteHostStatusDisconnected)
		m.state.Save() // Best effort on error path
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
		fmt.Printf("[remote] warning: failed to reconcile sessions: %v\n", err)
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

// GetConnectionByFlavorID returns the connection for a flavor ID.
// Returns nil if not connected.
func (m *Manager) GetConnectionByFlavorID(flavorID string) *Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, conn := range m.connections {
		if conn.flavor.ID == flavorID && conn.IsConnected() {
			return conn
		}
	}
	return nil
}

// IsConnected checks if a host is connected.
func (m *Manager) IsConnected(hostID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	conn, exists := m.connections[hostID]
	return exists && conn.IsConnected()
}

// IsFlavorConnected checks if a flavor has an active connection.
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
	m.state.Save()
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
	m.state.Save()
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
	m.state.UpdateRemoteHostStatus(hostID, status)
	m.state.Save()
	m.notifyStateChange()
}

// notifyStateChange calls the state change callback if set.
func (m *Manager) notifyStateChange() {
	if m.onStateChange != nil {
		m.onStateChange()
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
				fmt.Printf("[remote] expiring host %s (%s), connected at %s, expired at %s\n",
					host.ID, host.Hostname, host.ConnectedAt.Format(time.RFC3339),
					host.ExpiresAt.Format(time.RFC3339))
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
		fmt.Printf("[remote] pruned %d expired host(s)\n", pruned)
		m.state.Save()
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

// GetFlavors returns all configured remote flavors with their connection status.
type FlavorStatus struct {
	Flavor    config.RemoteFlavor
	Connected bool
	Status    string // "provisioning", "authenticating", "connected", "disconnected"
	Hostname  string
	HostID    string
}

// GetFlavorStatuses returns all configured flavors with their connection status.
func (m *Manager) GetFlavorStatuses() []FlavorStatus {
	flavors := m.config.GetRemoteFlavors()
	result := make([]FlavorStatus, len(flavors))

	m.mu.RLock()
	defer m.mu.RUnlock()

	for i, flavor := range flavors {
		status := FlavorStatus{
			Flavor: flavor,
			Status: "disconnected", // Default status
		}

		// Check if connected and get status
		for _, conn := range m.connections {
			if conn.flavor.ID == flavor.ID {
				status.Connected = conn.IsConnected()
				status.Status = conn.Status()
				status.Hostname = conn.Hostname()
				status.HostID = conn.host.ID
				break
			}
		}

		result[i] = status
	}

	return result
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
		fmt.Printf("[remote] no windows found on host %s\n", conn.host.ID)
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
				fmt.Printf("[remote] rediscovered session %s (window=%s, pane=%s)\n",
					sess.ID, w.WindowID, w.PaneID)
				break
			}
		}

		// If no match found by ID, mark session as disconnected
		if !matched && sess.Status != "disconnected" {
			sess.Status = "disconnected"
			m.state.UpdateSession(sess)
			disconnectedCount++
			fmt.Printf("[remote] warning: could not reconcile session %s (no ID match), marked as disconnected\n", sess.ID)
		}
	}

	if reconciledCount > 0 || disconnectedCount > 0 {
		fmt.Printf("[remote] reconciled %d session(s), %d marked disconnected on host %s\n",
			reconciledCount, disconnectedCount, conn.host.ID)
		m.state.Save()
	}

	return nil
}
