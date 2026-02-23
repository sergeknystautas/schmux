package preview

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"
	"github.com/sergeknystautas/schmux/internal/state"
)

const (
	StatusReady    = "ready"
	StatusDegraded = "degraded"
	staleGrace     = 3 * time.Second  // how long a degraded preview sticks around before removal
	touchDebounce  = 30 * time.Second // minimum interval between LastUsedAt state updates
)

var (
	ErrTargetHostNotAllowed = errors.New("target host must be loopback (127.0.0.1, ::1, or localhost)")
	ErrRemoteUnsupported    = errors.New("remote workspace previews are not supported in phase 1")
)

type Manager struct {
	state           state.StateStore
	maxPerWorkspace int
	maxGlobal       int
	networkAccess   bool   // if true, bind to 0.0.0.0 for external access
	portBase        int    // base port for stable block allocation (e.g. 53000)
	blockSize       int    // ports per workspace block (e.g. 10)
	tlsEnabled      bool   // if true, serve HTTPS on proxy ports
	tlsCertPath     string // path to TLS certificate
	tlsKeyPath      string // path to TLS key
	logger          *log.Logger

	mu       sync.Mutex
	entries  map[string]*entry // preview_id -> listener entry
	stopCh   chan struct{}
	stopOnce sync.Once
	doneCh   chan struct{}
}

type entry struct {
	workspaceID string
	listener    net.Listener
	server      *http.Server
}

func NewManager(st state.StateStore, maxPerWorkspace, maxGlobal int, networkAccess bool, portBase, blockSize int, tlsEnabled bool, tlsCertPath, tlsKeyPath string, logger *log.Logger) *Manager {
	if maxPerWorkspace <= 0 {
		maxPerWorkspace = 3
	}
	if maxGlobal <= 0 {
		maxGlobal = 20
	}
	if portBase <= 0 {
		portBase = 53000
	}
	if blockSize <= 0 {
		blockSize = 10
	}
	m := &Manager{
		state:           st,
		maxPerWorkspace: maxPerWorkspace,
		maxGlobal:       maxGlobal,
		networkAccess:   networkAccess,
		portBase:        portBase,
		blockSize:       blockSize,
		tlsEnabled:      tlsEnabled,
		tlsCertPath:     tlsCertPath,
		tlsKeyPath:      tlsKeyPath,
		logger:          logger,
		entries:         map[string]*entry{},
		stopCh:          make(chan struct{}),
		doneCh:          make(chan struct{}),
	}
	go m.cleanupLoop()
	return m
}

func (m *Manager) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})
	<-m.doneCh
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range m.entries {
		m.stopEntryLocked(id)
	}
}

func (m *Manager) CreateOrGet(ctx context.Context, ws state.Workspace, targetHost string, targetPort int) (state.WorkspacePreview, error) {
	if ws.RemoteHostID != "" {
		return state.WorkspacePreview{}, ErrRemoteUnsupported
	}
	host, err := normalizeTargetHost(targetHost)
	if err != nil {
		return state.WorkspacePreview{}, err
	}
	if targetPort <= 0 || targetPort > 65535 {
		return state.WorkspacePreview{}, fmt.Errorf("target port must be between 1 and 65535")
	}

	if existing, ok := m.state.FindPreview(ws.ID, host, targetPort); ok {
		preview, err := m.ensureListener(ctx, existing)
		if err != nil {
			return state.WorkspacePreview{}, err
		}
		return preview, nil
	}

	// Hold m.mu across cap check, port assignment, and state upsert to prevent
	// TOCTOU races where concurrent calls could pick the same port slot or both
	// pass the cap check. Lock is released before ensureListener to avoid
	// holding it during net.Listen.
	m.mu.Lock()
	if err := m.enforceCaps(ws.ID); err != nil {
		m.mu.Unlock()
		return state.WorkspacePreview{}, err
	}

	proxyPort, err := m.pickStablePortLocked(ws.ID)
	if err != nil {
		m.mu.Unlock()
		return state.WorkspacePreview{}, err
	}

	now := time.Now().UTC()
	preview := state.WorkspacePreview{
		ID:          fmt.Sprintf("prev_%s", uuid.NewString()[:8]),
		WorkspaceID: ws.ID,
		TargetHost:  host,
		TargetPort:  targetPort,
		ProxyPort:   proxyPort,
		CreatedAt:   now,
		LastUsedAt:  now,
	}
	// Reserve the slot in state before releasing the lock so subsequent
	// cap checks and port picks see this preview counted.
	if err := m.state.UpsertPreview(preview); err != nil {
		m.mu.Unlock()
		return state.WorkspacePreview{}, err
	}
	if err := m.state.Save(); err != nil {
		if removeErr := m.state.RemovePreview(preview.ID); removeErr != nil {
			if m.logger != nil {
				m.logger.Error("failed to roll back preview reservation", "preview_id", preview.ID, "err", removeErr)
			}
		}
		m.mu.Unlock()
		return state.WorkspacePreview{}, err
	}
	m.mu.Unlock()

	result, err := m.ensureListener(ctx, preview)
	if err != nil {
		// Roll back the reservation on listener failure.
		_ = m.state.RemovePreview(preview.ID)
		_ = m.state.Save()
		return state.WorkspacePreview{}, err
	}
	return result, nil
}

func (m *Manager) List(ctx context.Context, workspaceID string) ([]state.WorkspacePreview, error) {
	previews := m.state.GetWorkspacePreviews(workspaceID)
	for i := range previews {
		updated, err := m.ensureListener(ctx, previews[i])
		if err != nil {
			return nil, err
		}
		previews[i] = updated
	}
	sort.Slice(previews, func(i, j int) bool {
		if previews[i].TargetPort == previews[j].TargetPort {
			return previews[i].ID < previews[j].ID
		}
		return previews[i].TargetPort < previews[j].TargetPort
	})
	return previews, nil
}

func (m *Manager) Delete(workspaceID, previewID string) error {
	preview, ok := m.state.GetPreview(previewID)
	if !ok {
		return nil
	}
	if preview.WorkspaceID != workspaceID {
		return fmt.Errorf("preview not found in workspace")
	}

	m.mu.Lock()
	m.stopEntryLocked(previewID)
	m.mu.Unlock()

	if err := m.state.RemovePreview(previewID); err != nil {
		return err
	}
	return m.state.Save()
}

func (m *Manager) DeleteWorkspace(workspaceID string) error {
	m.mu.Lock()
	for previewID, e := range m.entries {
		if e != nil && e.workspaceID == workspaceID {
			m.stopEntryLocked(previewID)
		}
	}
	m.mu.Unlock()

	removed := m.state.RemoveWorkspacePreviews(workspaceID)
	if removed > 0 {
		return m.state.Save()
	}
	return nil
}

// ReconcileWorkspace updates preview health and removes stale previews for one workspace.
// Returns true when preview set changed (updated/deleted).
func (m *Manager) ReconcileWorkspace(workspaceID string) (bool, error) {
	previews := m.state.GetWorkspacePreviews(workspaceID)
	if len(previews) == 0 {
		return false, nil
	}
	changed := false
	now := time.Now().UTC()
	for _, preview := range previews {
		// If proxy listener died, remove mapping.
		if !isProxyAlive(preview.ProxyPort) {
			if err := m.Delete(workspaceID, preview.ID); err != nil {
				return changed, err
			}
			changed = true
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		ready, err := checkUpstream(ctx, preview.TargetHost, preview.TargetPort)
		cancel()
		if ready {
			if preview.Status != StatusReady || preview.LastHealthyAt.IsZero() {
				preview.Status = StatusReady
				preview.LastError = ""
				preview.LastHealthyAt = now
				if err := m.state.UpsertPreview(preview); err != nil {
					return changed, err
				}
				changed = true
			}
			continue
		}

		// If this preview was healthy before and has been unreachable for a grace period,
		// clean it up so dead server tabs disappear.
		if !preview.LastHealthyAt.IsZero() && now.Sub(preview.LastHealthyAt) > staleGrace {
			if err := m.Delete(workspaceID, preview.ID); err != nil {
				return changed, err
			}
			changed = true
			continue
		}

		// Only update if status or error actually changed
		newError := ""
		if err != nil {
			newError = err.Error()
		}
		if preview.Status != StatusDegraded || preview.LastError != newError {
			preview.Status = StatusDegraded
			preview.LastError = newError
			if err := m.state.UpsertPreview(preview); err != nil {
				return changed, err
			}
			changed = true
		}
	}
	if changed {
		if err := m.state.Save(); err != nil {
			return changed, err
		}
	}
	return changed, nil
}

func (m *Manager) enforceCaps(workspaceID string) error {
	if len(m.state.GetPreviews()) >= m.maxGlobal {
		return fmt.Errorf("global preview limit reached (%d)", m.maxGlobal)
	}
	if len(m.state.GetWorkspacePreviews(workspaceID)) >= m.maxPerWorkspace {
		return fmt.Errorf("workspace preview limit reached (%d)", m.maxPerWorkspace)
	}
	return nil
}

func (m *Manager) ensureListener(ctx context.Context, preview state.WorkspacePreview) (state.WorkspacePreview, error) {
	m.mu.Lock()
	currentEntry, hasEntry := m.entries[preview.ID]
	if hasEntry && currentEntry != nil && isProxyAlive(preview.ProxyPort) {
		m.mu.Unlock()
		return m.updateStatus(ctx, preview, false)
	}
	if hasEntry {
		m.stopEntryLocked(preview.ID)
	}
	m.mu.Unlock()

	targetURL, err := url.Parse(fmt.Sprintf("http://%s:%d", preview.TargetHost, preview.TargetPort))
	if err != nil {
		return state.WorkspacePreview{}, fmt.Errorf("invalid target URL: %w", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.touch(preview.ID)
		if isWebSocketUpgrade(r) {
			tunnelWebSocket(w, r, preview.TargetHost, preview.TargetPort)
			return
		}
		proxy.ServeHTTP(w, r)
	})

	// Bind to the stable port. Follow bind_address: 0.0.0.0 in network-access
	// mode so remote clients can reach the proxied server.
	bindAddr := fmt.Sprintf("127.0.0.1:%d", preview.ProxyPort)
	if m.networkAccess {
		bindAddr = fmt.Sprintf("0.0.0.0:%d", preview.ProxyPort)
	}
	listener, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return state.WorkspacePreview{}, fmt.Errorf("failed to bind proxy listener on port %d: %w", preview.ProxyPort, err)
	}

	server := &http.Server{Handler: proxyHandler}
	go func() {
		var err error
		if m.tlsEnabled {
			err = server.ServeTLS(listener, m.tlsCertPath, m.tlsKeyPath)
		} else {
			err = server.Serve(listener)
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			if m.logger != nil {
				m.logger.Error("listener stopped unexpectedly", "preview_id", preview.ID, "err", err)
			}
		}
	}()

	preview.LastUsedAt = time.Now().UTC()

	m.mu.Lock()
	m.entries[preview.ID] = &entry{workspaceID: preview.WorkspaceID, listener: listener, server: server}
	m.mu.Unlock()

	return m.updateStatus(ctx, preview, true)
}

func (m *Manager) updateStatus(ctx context.Context, preview state.WorkspacePreview, touched bool) (state.WorkspacePreview, error) {
	ready, err := checkUpstream(ctx, preview.TargetHost, preview.TargetPort)
	now := time.Now().UTC()
	if ready {
		preview.Status = StatusReady
		preview.LastError = ""
		preview.LastHealthyAt = now
	} else {
		preview.Status = StatusDegraded
		if err != nil {
			preview.LastError = err.Error()
		} else {
			preview.LastError = "target unreachable"
		}
	}
	if touched {
		preview.LastUsedAt = now
	}
	if err := m.state.UpsertPreview(preview); err != nil {
		return state.WorkspacePreview{}, err
	}
	if err := m.state.Save(); err != nil {
		return state.WorkspacePreview{}, err
	}
	return preview, nil
}

func (m *Manager) touch(previewID string) {
	preview, ok := m.state.GetPreview(previewID)
	if !ok {
		return
	}
	now := time.Now().UTC()
	// Debounce: only update state if more than 30 seconds since last touch
	if now.Sub(preview.LastUsedAt) < touchDebounce {
		return
	}
	preview.LastUsedAt = now
	if preview.Status == StatusDegraded {
		ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
		ready, err := checkUpstream(ctx, preview.TargetHost, preview.TargetPort)
		cancel()
		if ready {
			preview.Status = StatusReady
			preview.LastError = ""
			preview.LastHealthyAt = now
		} else if err != nil {
			preview.LastError = err.Error()
		}
	}
	_ = m.state.UpsertPreview(preview)
	// LastUsedAt is low-priority; skip Save() here to avoid write amplification
	// on every proxied request. It will be persisted on the next status change.
}

func (m *Manager) cleanupLoop() {
	defer close(m.doneCh)
	<-m.stopCh
}

func (m *Manager) stopEntryLocked(previewID string) {
	e := m.entries[previewID]
	if e == nil {
		delete(m.entries, previewID)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_ = e.server.Shutdown(ctx)
	_ = e.listener.Close()
	delete(m.entries, previewID)
}

func isProxyAlive(port int) bool {
	if port <= 0 {
		return false
	}
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func checkUpstream(ctx context.Context, host string, port int) (bool, error) {
	dialer := net.Dialer{Timeout: 400 * time.Millisecond}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	if err != nil {
		return false, fmt.Errorf("target unreachable: %w", err)
	}
	_ = conn.Close()
	return true, nil
}

// assignPortBlockLocked returns the port block for a workspace, assigning one
// if not yet set. Must be called with m.mu held.
func (m *Manager) assignPortBlockLocked(workspaceID string) (int, error) {
	ws, ok := m.state.GetWorkspace(workspaceID)
	if !ok {
		return 0, fmt.Errorf("workspace not found: %s", workspaceID)
	}
	if ws.PortBlock != 0 {
		return ws.PortBlock, nil
	}

	// Derive next block from max across all workspaces (blocks are 1-indexed).
	next := 1
	for _, w := range m.state.GetWorkspaces() {
		if w.PortBlock >= next {
			next = w.PortBlock + 1
		}
	}

	ws.PortBlock = next
	if err := m.state.UpdateWorkspace(ws); err != nil {
		return 0, fmt.Errorf("failed to assign port block: %w", err)
	}
	// Save is deferred to the caller which will save the preview record too.
	return next, nil
}

// pickStablePortLocked selects the lowest available port slot in the workspace's
// block for a new preview. Skips ports already used by existing previews or
// occupied by an external process. Must be called with m.mu held.
func (m *Manager) pickStablePortLocked(workspaceID string) (int, error) {
	portBlock, err := m.assignPortBlockLocked(workspaceID)
	if err != nil {
		return 0, err
	}

	used := make(map[int]bool)
	for _, p := range m.state.GetWorkspacePreviews(workspaceID) {
		if p.ProxyPort > 0 {
			used[p.ProxyPort] = true
		}
	}

	blockBase := m.portBase + (portBlock-1)*m.blockSize
	for slot := 0; slot < m.blockSize; slot++ {
		port := blockBase + slot
		if used[port] {
			continue
		}
		if !m.isPortFree(port) {
			if m.logger != nil {
				m.logger.Debug("port in use by external process, trying next slot", "port", port)
			}
			continue
		}
		return port, nil
	}
	return 0, fmt.Errorf("all %d ports in workspace block exhausted", m.blockSize)
}

// isPortFree is a best-effort check for whether a port is available to bind.
// There is an inherent TOCTOU race: the port could be claimed between this check
// and the actual bind in ensureListener. The caller (ensureListener) handles bind
// failures gracefully, so this check only serves to skip obviously-occupied ports
// during allocation without wasting a round-trip.
func (m *Manager) isPortFree(port int) bool {
	bindAddr := fmt.Sprintf("127.0.0.1:%d", port)
	if m.networkAccess {
		bindAddr = fmt.Sprintf("0.0.0.0:%d", port)
	}
	ln, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// tunnelWebSocket handles WebSocket upgrade requests by hijacking the client
// connection and opening a raw TCP tunnel to the upstream. This preserves the
// full HTTP upgrade handshake that httputil.ReverseProxy would otherwise strip.
func tunnelWebSocket(w http.ResponseWriter, r *http.Request, targetHost string, targetPort int) {
	upstream, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", targetHost, targetPort), 5*time.Second)
	if err != nil {
		http.Error(w, "upstream unreachable", http.StatusBadGateway)
		return
	}
	defer upstream.Close()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "connection hijacking not supported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	// Forward the original upgrade request to the upstream.
	if err := r.Write(upstream); err != nil {
		return
	}

	// Bidirectional copy until either side closes.
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		io.Copy(dst, src) //nolint:errcheck
		done <- struct{}{}
	}
	go cp(upstream, clientConn)
	go cp(clientConn, upstream)
	<-done
	// One direction finished — close both to unblock the other goroutine.
	upstream.Close()
	clientConn.Close()
	<-done
}

func normalizeTargetHost(host string) (string, error) {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		host = "127.0.0.1"
	}
	// The switch-case already restricts to known loopback values, so a DNS
	// lookup is redundant and adds unnecessary latency / failure modes.
	switch host {
	case "127.0.0.1", "::1", "localhost":
	default:
		return "", ErrTargetHostNotAllowed
	}
	return host, nil
}
