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

	"github.com/google/uuid"
	"github.com/sergeknystautas/schmux/internal/state"
)

const (
	StatusReady    = "ready"
	StatusDegraded = "degraded"
	staleGrace     = 3 * time.Second // how long a degraded preview sticks around
)

var (
	ErrTargetHostNotAllowed = errors.New("target host must be loopback (127.0.0.1, ::1, or localhost)")
	ErrRemoteUnsupported    = errors.New("remote workspace previews are not supported in phase 1")
)

type Manager struct {
	state           state.StateStore
	maxPerWorkspace int
	maxGlobal       int
	idleTimeout     time.Duration

	mu      sync.Mutex
	entries map[string]*entry // preview_id -> listener entry
	stopCh  chan struct{}
}

type entry struct {
	workspaceID string
	listener    net.Listener
	server      *http.Server
}

func NewManager(st state.StateStore, maxPerWorkspace, maxGlobal int, idleTimeout time.Duration) *Manager {
	if maxPerWorkspace <= 0 {
		maxPerWorkspace = 3
	}
	if maxGlobal <= 0 {
		maxGlobal = 20
	}
	if idleTimeout <= 0 {
		idleTimeout = 60 * time.Minute
	}
	m := &Manager{
		state:           st,
		maxPerWorkspace: maxPerWorkspace,
		maxGlobal:       maxGlobal,
		idleTimeout:     idleTimeout,
		entries:         map[string]*entry{},
		stopCh:          make(chan struct{}),
	}
	go m.cleanupLoop()
	return m
}

func (m *Manager) Stop() {
	close(m.stopCh)
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

	// Hold m.mu across the cap check and state upsert to prevent a TOCTOU race
	// where concurrent CreateOrGet calls could both pass the cap check before
	// either registers its preview. We release the lock before ensureListener
	// to avoid holding it during blocking network I/O (net.Listen).
	m.mu.Lock()
	if err := m.enforceCaps(ws.ID); err != nil {
		m.mu.Unlock()
		return state.WorkspacePreview{}, err
	}

	now := time.Now().UTC()
	preview := state.WorkspacePreview{
		ID:          fmt.Sprintf("prev_%s", uuid.NewString()[:8]),
		WorkspaceID: ws.ID,
		TargetHost:  host,
		TargetPort:  targetPort,
		CreatedAt:   now,
		LastUsedAt:  now,
	}
	// Reserve the slot in state before releasing the lock so subsequent
	// cap checks see this preview counted.
	if err := m.state.UpsertPreview(preview); err != nil {
		m.mu.Unlock()
		return state.WorkspacePreview{}, err
	}
	m.mu.Unlock()

	result, err := m.ensureListener(ctx, preview)
	if err != nil {
		// Roll back the reservation on listener failure.
		_ = m.state.RemovePreview(preview.ID)
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
	proxy.Transport = http.DefaultTransport
	proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.touch(preview.ID)
		proxy.ServeHTTP(w, r)
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return state.WorkspacePreview{}, fmt.Errorf("failed to allocate proxy listener: %w", err)
	}

	server := &http.Server{Handler: proxyHandler}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("[preview] listener stopped unexpectedly: preview_id=%s error=%v\n", preview.ID, err)
		}
	}()

	addr, _ := listener.Addr().(*net.TCPAddr)
	if addr == nil {
		listener.Close()
		return state.WorkspacePreview{}, fmt.Errorf("failed to resolve listener address")
	}

	preview.ProxyPort = addr.Port
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
	if now.Sub(preview.LastUsedAt) < 30*time.Second {
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
	// No Save() needed — previews are ephemeral (json:"-") and not persisted to disk.
}

func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.cleanupIdle()
		case <-m.stopCh:
			return
		}
	}
}

func (m *Manager) cleanupIdle() {
	previews := m.state.GetPreviews()
	now := time.Now().UTC()
	for _, preview := range previews {
		if preview.LastUsedAt.IsZero() {
			continue
		}
		if now.Sub(preview.LastUsedAt) < m.idleTimeout {
			continue
		}
		_ = m.Delete(preview.WorkspaceID, preview.ID)
	}
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
