package preview

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"
	"github.com/sergeknystautas/schmux/internal/state"
)

// Compiled regexes for ss output parsing.
var ssPIDRegex = regexp.MustCompile(`pid=(\d+)`)

const (
	StatusReady   = "ready"
	touchDebounce = 30 * time.Second // minimum interval between LastUsedAt state updates
)

// ListeningPort represents a detected listening port with its host address.
type ListeningPort struct {
	Host     string
	Port     int
	OwnerPID int
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

func (m *Manager) CreateOrGet(ctx context.Context, ws state.Workspace, targetHost string, targetPort int, sourceSessionID string, serverPID int) (state.WorkspacePreview, bool, error) {
	if ws.RemoteHostID != "" {
		return state.WorkspacePreview{}, false, ErrRemoteUnsupported
	}
	host, err := NormalizeTargetHost(targetHost)
	if err != nil {
		return state.WorkspacePreview{}, false, err
	}
	if targetPort <= 0 || targetPort > 65535 {
		return state.WorkspacePreview{}, false, fmt.Errorf("target port must be between 1 and 65535")
	}

	if existing, ok := m.state.FindPreview(ws.ID, host, targetPort); ok {
		preview, err := m.ensureListener(ctx, existing)
		if err != nil {
			return state.WorkspacePreview{}, false, err
		}
		return preview, false, nil
	}

	// Hold m.mu across cap check, port assignment, and state upsert to prevent
	// TOCTOU races where concurrent calls could pick the same port slot or both
	// pass the cap check. Lock is released before ensureListener to avoid
	// holding it during net.Listen.
	m.mu.Lock()
	if err := m.enforceCaps(ws.ID); err != nil {
		m.mu.Unlock()
		return state.WorkspacePreview{}, false, err
	}

	proxyPort, err := m.pickStablePortLocked(ws.ID)
	if err != nil {
		m.mu.Unlock()
		return state.WorkspacePreview{}, false, err
	}

	now := time.Now().UTC()
	preview := state.WorkspacePreview{
		ID:              fmt.Sprintf("prev_%s", uuid.NewString()[:8]),
		WorkspaceID:     ws.ID,
		TargetHost:      host,
		TargetPort:      targetPort,
		ProxyPort:       proxyPort,
		SourceSessionID: sourceSessionID,
		ServerPID:       serverPID,
		CreatedAt:       now,
		LastUsedAt:      now,
	}
	// Reserve the slot in state before releasing the lock so subsequent
	// cap checks and port picks see this preview counted.
	if err := m.state.UpsertPreview(preview); err != nil {
		m.mu.Unlock()
		return state.WorkspacePreview{}, false, err
	}
	if err := m.state.Save(); err != nil {
		if removeErr := m.state.RemovePreview(preview.ID); removeErr != nil {
			if m.logger != nil {
				m.logger.Error("failed to roll back preview reservation", "preview_id", preview.ID, "err", removeErr)
			}
		}
		m.mu.Unlock()
		return state.WorkspacePreview{}, false, err
	}
	m.mu.Unlock()

	result, err := m.ensureListener(ctx, preview)
	if err != nil {
		// Roll back the reservation on listener failure.
		_ = m.state.RemovePreview(preview.ID)
		_ = m.state.Save()
		return state.WorkspacePreview{}, false, err
	}

	// Create a corresponding accessory tab for this preview.
	previewTab := state.Tab{
		ID:        "sys-preview-" + preview.ID,
		Kind:      "preview",
		Label:     fmt.Sprintf("web:%d", preview.TargetPort),
		Route:     fmt.Sprintf("/preview/%s/%s", ws.ID, preview.ID),
		Closable:  true,
		Meta:      map[string]string{"preview_id": preview.ID},
		CreatedAt: preview.CreatedAt,
	}
	_ = m.state.AddTab(ws.ID, previewTab)

	return result, true, nil
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
	// Remove the corresponding accessory tab.
	_ = m.state.RemoveTab(workspaceID, "sys-preview-"+previewID)
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

func (m *Manager) ReconcileWorkspaceWithCache(workspaceID string, cache PortOwnerCache) (bool, error) {
	previews := m.state.GetWorkspacePreviews(workspaceID)
	if len(previews) == 0 {
		return false, nil
	}
	changed := false
	for _, p := range previews {
		// Step 1: Session check
		var sess state.Session
		var hasSess bool
		if p.SourceSessionID != "" {
			sess, hasSess = m.state.GetSession(p.SourceSessionID)
			if !hasSess || sess.Pid <= 0 {
				if m.logger != nil {
					m.logger.Info("deleted", "id", p.ID, "host", p.TargetHost, "port", p.TargetPort, "reason", "session-gone", "session", p.SourceSessionID)
				}
				if err := m.Delete(workspaceID, p.ID); err != nil {
					return changed, err
				}
				changed = true
				continue
			}
		}

		// Step 2: ServerPID alive check
		if p.ServerPID > 0 && !isProcessAlive(p.ServerPID) {
			if m.logger != nil {
				m.logger.Info("deleted", "id", p.ID, "host", p.TargetHost, "port", p.TargetPort, "reason", "server-pid-dead", "server_pid", p.ServerPID)
			}
			if err := m.Delete(workspaceID, p.ID); err != nil {
				return changed, err
			}
			changed = true
			continue
		}

		// Step 3: PID tree check (session-bound only, non-terminal)
		if p.SourceSessionID != "" && hasSess {
			ownsPort := false
			if m.portDetector != nil {
				for _, lp := range m.portDetector(sess.Pid) {
					if lp.Port == p.TargetPort {
						ownsPort = true
						break
					}
				}
			}
			if ownsPort {
				m.mu.Lock()
				_, hasEntry := m.entries[p.ID]
				m.mu.Unlock()
				if !hasEntry {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					if _, err := m.ensureListener(ctx, p); err != nil {
						if m.logger != nil {
							m.logger.Warn("failed to recreate listener", "preview_id", p.ID, "err", err)
						}
					}
					cancel()
					changed = true
				}
				continue
			}
			// Fall through to step 4 (non-terminal)
		}

		// Step 4: Port ownership (keeps POST API previews alive)
		if p.ServerPID > 0 {
			var currentOwner int
			var found bool
			if cache != nil {
				currentOwner, found = cache[p.TargetPort]
			} else {
				var err error
				currentOwner, err = LookupPortOwner(p.TargetPort)
				found = err == nil
			}
			if found && currentOwner == p.ServerPID {
				m.mu.Lock()
				_, hasEntry := m.entries[p.ID]
				m.mu.Unlock()
				if !hasEntry {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					if _, err := m.ensureListener(ctx, p); err != nil {
						if m.logger != nil {
							m.logger.Warn("failed to recreate listener", "preview_id", p.ID, "err", err)
						}
					}
					cancel()
					changed = true
				}
				continue
			}
			if m.logger != nil && found {
				m.logger.Info("deleted", "id", p.ID, "host", p.TargetHost, "port", p.TargetPort, "reason", "port-owner-changed", "server_pid", p.ServerPID, "current_owner", currentOwner)
			}
		}

		// Step 5: All checks failed
		if m.logger != nil {
			m.logger.Info("deleted", "id", p.ID, "host", p.TargetHost, "port", p.TargetPort, "reason", "all-checks-failed")
		}
		if err := m.Delete(workspaceID, p.ID); err != nil {
			return changed, err
		}
		changed = true
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

func NormalizeTargetHost(host string) (string, error) {
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

// LookupPortOwner finds which PID is listening on a TCP port.
// Uses lsof on macOS, ss on Linux. Prefers IPv4, lowest PID for ties.
func LookupPortOwner(port int) (int, error) {
	pid, err := lookupPortOwnerViaLsof(port)
	if err == nil {
		return pid, nil
	}
	return lookupPortOwnerViaSS(port)
}

func lookupPortOwnerViaLsof(port int) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "lsof", "-Pan", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN")
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return 0, fmt.Errorf("no listener on port %d", port)
	}

	type candidate struct {
		pid  int
		ipv4 bool
	}
	var candidates []candidate
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		// NAME is second-to-last field, (LISTEN) is last
		if len(fields) < 2 || fields[len(fields)-1] != "(LISTEN)" {
			continue
		}
		pid, err := strconv.Atoi(fields[1])
		if err != nil || pid <= 0 {
			continue
		}
		isIPv4 := strings.Contains(line, "IPv4")
		candidates = append(candidates, candidate{pid: pid, ipv4: isIPv4})
	}

	if len(candidates) == 0 {
		return 0, fmt.Errorf("no listener on port %d", port)
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].ipv4 != candidates[j].ipv4 {
			return candidates[i].ipv4
		}
		return candidates[i].pid < candidates[j].pid
	})
	return candidates[0].pid, nil
}

func lookupPortOwnerViaSS(port int) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ss", "-tlnp", "sport", "=", strconv.Itoa(port))
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return 0, fmt.Errorf("no listener on port %d", port)
	}

	type candidate struct {
		pid  int
		ipv4 bool
	}
	var candidates []candidate
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "LISTEN") {
			continue
		}
		pidMatch := ssPIDRegex.FindStringSubmatch(line)
		if len(pidMatch) < 2 {
			continue
		}
		pid, err := strconv.Atoi(pidMatch[1])
		if err != nil || pid <= 0 {
			continue
		}
		isIPv4 := !strings.Contains(line, "[::]") && !strings.Contains(line, ":::")
		candidates = append(candidates, candidate{pid: pid, ipv4: isIPv4})
	}
	if len(candidates) == 0 {
		return 0, fmt.Errorf("no listener on port %d", port)
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].ipv4 != candidates[j].ipv4 {
			return candidates[i].ipv4
		}
		return candidates[i].pid < candidates[j].pid
	})
	return candidates[0].pid, nil
}

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// PortOwnerCache maps port numbers to their owning PIDs.
// Built once per reconciliation tick via a batch lsof/ss call.
type PortOwnerCache map[int]int

// BuildPortOwnerCache runs a single lsof/ss call to snapshot all TCP LISTEN ports.
func BuildPortOwnerCache() PortOwnerCache {
	cache := make(PortOwnerCache)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "lsof", "-Pan", "-iTCP", "-sTCP:LISTEN")
	out, err := cmd.Output()
	if err == nil {
		parseLsofCacheOutput(out, cache)
		return cache
	}

	cmd = exec.CommandContext(ctx, "ss", "-tlnp")
	out, err = cmd.Output()
	if err == nil {
		parseSSCacheOutput(out, cache)
	}
	return cache
}

func parseLsofCacheOutput(out []byte, cache PortOwnerCache) {
	type entry struct {
		pid  int
		ipv4 bool
	}
	portEntries := make(map[int][]entry)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		// NAME is second-to-last field, (LISTEN) is last
		if len(fields) < 2 || fields[len(fields)-1] != "(LISTEN)" {
			continue
		}
		pid, err := strconv.Atoi(fields[1])
		if err != nil || pid <= 0 {
			continue
		}
		// NAME field: e.g. "*:7337" or "[::1]:9323" or "127.0.0.1:3000"
		name := fields[len(fields)-2]
		colonIdx := strings.LastIndex(name, ":")
		if colonIdx < 0 {
			continue
		}
		port, err := strconv.Atoi(name[colonIdx+1:])
		if err != nil || port <= 0 {
			continue
		}
		isIPv4 := strings.Contains(line, "IPv4")
		portEntries[port] = append(portEntries[port], entry{pid: pid, ipv4: isIPv4})
	}
	for port, entries := range portEntries {
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].ipv4 != entries[j].ipv4 {
				return entries[i].ipv4
			}
			return entries[i].pid < entries[j].pid
		})
		cache[port] = entries[0].pid
	}
}

func parseSSCacheOutput(out []byte, cache PortOwnerCache) {
	type entry struct {
		pid  int
		ipv4 bool
	}
	portEntries := make(map[int][]entry)
	portRe := regexp.MustCompile(`:(\d+)\s`)
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "LISTEN") {
			continue
		}
		portMatch := portRe.FindStringSubmatch(line)
		pidMatch := ssPIDRegex.FindStringSubmatch(line)
		if len(portMatch) < 2 || len(pidMatch) < 2 {
			continue
		}
		port, _ := strconv.Atoi(portMatch[1])
		pid, _ := strconv.Atoi(pidMatch[1])
		if port > 0 && pid > 0 {
			isIPv4 := !strings.Contains(line, "[::]") && !strings.Contains(line, ":::")
			portEntries[port] = append(portEntries[port], entry{pid: pid, ipv4: isIPv4})
		}
	}
	for port, entries := range portEntries {
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].ipv4 != entries[j].ipv4 {
				return entries[i].ipv4
			}
			return entries[i].pid < entries[j].pid
		})
		cache[port] = entries[0].pid
	}
}
