package preview

import (
	"context"
	"errors"
	"fmt"
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
	StatusReady   = "ready"
	touchDebounce = 30 * time.Second // minimum interval between LastUsedAt state updates
)

// ListeningPort represents a detected listening port with its host address.
type ListeningPort struct {
	Host string
	Port int
}

// PortDetector returns the listening ports for a process and its descendants.
type PortDetector func(pid int) []ListeningPort

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
	portDetector    PortDetector

	mu      sync.Mutex
	entries map[string]*entry // preview_id -> listener entry
}

type entry struct {
	workspaceID string
	listener    net.Listener
	server      *http.Server
}

func NewManager(st state.StateStore, maxPerWorkspace, maxGlobal int, networkAccess bool, portBase, blockSize int, tlsEnabled bool, tlsCertPath, tlsKeyPath string, logger *log.Logger, portDetector PortDetector) *Manager {
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
		portDetector:    portDetector,
		entries:         map[string]*entry{},
	}
	return m
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range m.entries {
		m.stopEntryLocked(id)
	}
}

func (m *Manager) CreateOrGet(ctx context.Context, ws state.Workspace, targetHost string, targetPort int, sourceSessionID string) (state.WorkspacePreview, error) {
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
		ID:              fmt.Sprintf("prev_%s", uuid.NewString()[:8]),
		WorkspaceID:     ws.ID,
		TargetHost:      host,
		TargetPort:      targetPort,
		ProxyPort:       proxyPort,
		SourceSessionID: sourceSessionID,
		CreatedAt:       now,
		LastUsedAt:      now,
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

// DeleteBySession removes all previews created by the given session and stops their listeners.
func (m *Manager) DeleteBySession(sessionID string) (int, error) {
	previews := m.state.GetPreviews()
	deleted := 0
	for _, preview := range previews {
		if preview.SourceSessionID != sessionID {
			continue
		}
		if err := m.Delete(preview.WorkspaceID, preview.ID); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
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

// ReconcileWorkspace verifies previews for a workspace are still valid by checking
// whether the source session's PID tree still owns the target port.
// Returns true when preview set changed.
func (m *Manager) ReconcileWorkspace(workspaceID string) (bool, error) {
	previews := m.state.GetWorkspacePreviews(workspaceID)
	if len(previews) == 0 {
		return false, nil
	}
	changed := false
	for _, preview := range previews {
		// Look up source session
		sess, hasSess := m.state.GetSession(preview.SourceSessionID)

		// No source session or PID not set → delete
		if !hasSess || sess.Pid <= 0 {
			if err := m.Delete(workspaceID, preview.ID); err != nil {
				return changed, err
			}
			changed = true
			continue
		}

		// Check if session's PID tree still owns the target port
		ownsPort := false
		if m.portDetector != nil {
			for _, lp := range m.portDetector(sess.Pid) {
				if lp.Port == preview.TargetPort {
					ownsPort = true
					break
				}
			}
		}

		if !ownsPort {
			if err := m.Delete(workspaceID, preview.ID); err != nil {
				return changed, err
			}
			changed = true
			continue
		}

		// Port still owned — ensure proxy listener is running
		m.mu.Lock()
		_, hasEntry := m.entries[preview.ID]
		m.mu.Unlock()
		if !hasEntry {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			if _, err := m.ensureListener(ctx, preview); err != nil {
				if m.logger != nil {
					m.logger.Warn("failed to recreate listener", "preview_id", preview.ID, "err", err)
				}
			}
			cancel()
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
	if hasEntry && currentEntry != nil {
		m.mu.Unlock()
		// Listener already running — return current state as-is.
		return preview, nil
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

	// Strip sensitive headers before forwarding to the upstream service.
	// Without this, schmux session cookies, CSRF tokens, and any Authorization
	// headers would leak to the proxied dev server.
	defaultDirector := proxy.Director
	proxy.Director = func(r *http.Request) {
		defaultDirector(r)
		r.Header.Del("Cookie")
		r.Header.Del("Authorization")
		r.Header.Del("X-CSRF-Token")
	}

	proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.touch(preview.ID)
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

	now := time.Now().UTC()
	preview.Status = StatusReady
	preview.LastError = ""
	preview.LastHealthyAt = now
	preview.LastUsedAt = now

	m.mu.Lock()
	m.entries[preview.ID] = &entry{workspaceID: preview.WorkspaceID, listener: listener, server: server}
	m.mu.Unlock()

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
	_ = m.state.UpsertPreview(preview)
	// LastUsedAt is low-priority; skip Save() here to avoid write amplification
	// on every proxied request. It will be persisted on the next status change.
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
