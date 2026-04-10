package dashboard

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/preview"
)

const previewAutoDetectCooldown = 45 * time.Second

// pidFieldRegex matches the pid= field in ss output.
var pidFieldRegex = regexp.MustCompile(`pid=(\d+)`)

func (s *Server) handleSessionOutputChunk(sessionID string, chunk []byte) {
	if s.previewManager == nil || len(chunk) == 0 {
		return
	}
	sess, found := s.state.GetSession(sessionID)
	if !found || sess.RemoteHostID != "" {
		return
	}
	ws, found := s.state.GetWorkspace(sess.WorkspaceID)
	if !found || ws.RemoteHostID != "" {
		return
	}

	// Find http(s):// URLs in terminal output, extract ports with host info
	candidatePorts := detectPortsFromChunk(chunk)
	if len(candidatePorts) == 0 {
		return
	}

	// Filter out ports we already have previews for
	ports := s.filterExistingPreviews(ws.ID, candidatePorts)
	if len(ports) == 0 {
		return
	}

	// Filter out our own proxy ports
	ports = s.filterProxyPorts(ports)
	if len(ports) == 0 {
		return
	}

	// Filter out the daemon's own listening port
	ports = s.filterDaemonPort(ports)
	if len(ports) == 0 {
		return
	}

	// Filter out ports that don't speak HTTP
	ports = filterNonHTTPPorts(ports)
	if len(ports) == 0 {
		return
	}

	// Build PID tree for ownership lookup (includes session PID itself)
	descendantPIDs := make(map[int]bool)
	descendantPIDs[sess.Pid] = true
	for _, dpid := range getDescendantPIDs(sess.Pid) {
		descendantPIDs[dpid] = true
	}

	// Create previews for verified ports
	previewLog := logging.Sub(s.logger, "preview")
	now := time.Now().UTC()
	var createdPreview *string // ID of first created preview for navigation
	for _, lp := range ports {
		// Look up which PID owns this port
		ownerPID, err := preview.LookupPortOwner(lp.Port)
		if err != nil {
			continue
		}

		// Check PID tree first, then fall back to PID file match
		trigger := "autodetect"
		if !descendantPIDs[ownerPID] {
			if !matchesBrainstormPIDFile(ws.Path, ownerPID) {
				previewLog.Debug("pid file scan no match", "port", lp.Port, "owner", ownerPID, "workspace", ws.Path)
				continue
			}
			trigger = "pid-file"
		}

		key := fmt.Sprintf("%s:%d", ws.ID, lp.Port)
		s.previewDetectMu.Lock()
		last, hasLast := s.previewDetect[key]
		if hasLast && now.Sub(last) < previewAutoDetectCooldown {
			s.previewDetectMu.Unlock()
			continue
		}
		s.previewDetect[key] = now
		s.previewDetectMu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		result, wasCreated, err := s.previewManager.CreateOrGet(ctx, ws, lp.Host, lp.Port, sess.ID, ownerPID)
		cancel()
		if err != nil {
			continue
		}
		if wasCreated {
			previewLog.Info("created", "host", lp.Host, "port", lp.Port, "session", sess.ID, "server_pid", ownerPID, "trigger", trigger)
			if createdPreview == nil {
				createdPreview = &result.ID
			}
		}
	}

	if createdPreview != nil {
		go s.BroadcastSessions()
		go s.BroadcastPendingNavigation("preview", ws.ID, *createdPreview)
	}
}

// urlRegex matches http(s)://host[:port] patterns
// Groups: 1=scheme, 2=host, 3=:port (with colon), 4=port (digits only)
var urlRegex = regexp.MustCompile(`(?i)(https?)://([a-zA-Z0-9.\[\]-]+)(:(\d+))?`)

// ansiRegex matches ANSI escape sequences (CSI, OSC, and single-character escapes).
var ansiRegex = regexp.MustCompile(`\x1b(?:\[[0-9;?]*[a-zA-Z@]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[^[\]])`)

// detectPortsFromChunk finds http(s):// URLs in terminal output and extracts ports.
// Only accepts loopback hosts (localhost, 127.0.0.1, ::1). Defaults to 80 for http,
// 443 for https if no port specified. Deduplicates by host+port pair.
func detectPortsFromChunk(chunk []byte) []preview.ListeningPort {
	clean := ansiRegex.ReplaceAllString(string(chunk), "")
	if clean == "" {
		return nil
	}

	type hostPort struct {
		host string
		port int
	}
	seen := make(map[hostPort]bool)
	var ports []preview.ListeningPort

	for _, match := range urlRegex.FindAllStringSubmatch(clean, -1) {
		if len(match) < 5 {
			continue
		}
		scheme := strings.ToLower(match[1])
		rawHost := match[2]
		portStr := match[4]

		// Validate host is loopback
		host := strings.TrimSpace(strings.ToLower(rawHost))
		switch host {
		case "localhost", "127.0.0.1", "::1":
			// valid loopback — keep as-is
		default:
			continue
		}

		var port int
		if portStr != "" {
			p, err := strconv.Atoi(portStr)
			if err != nil || p <= 0 || p > 65535 {
				continue
			}
			port = p
		} else {
			// Default ports based on scheme
			if scheme == "https" {
				port = 443
			} else {
				port = 80
			}
		}

		key := hostPort{host: host, port: port}
		if seen[key] {
			continue
		}
		seen[key] = true
		ports = append(ports, preview.ListeningPort{Host: host, Port: port, OwnerPID: 0})
	}

	sort.Slice(ports, func(i, j int) bool {
		if ports[i].Port != ports[j].Port {
			return ports[i].Port < ports[j].Port
		}
		return ports[i].Host < ports[j].Host
	})
	return ports
}

// filterExistingPreviews removes ports that already have previews for this workspace.
// Returns preview.ListeningPort entries for ports that don't have existing previews.
func (s *Server) filterExistingPreviews(workspaceID string, ports []preview.ListeningPort) []preview.ListeningPort {
	var filtered []preview.ListeningPort
	for _, lp := range ports {
		if _, exists := s.state.FindPreview(workspaceID, lp.Host, lp.Port); exists {
			continue
		}
		filtered = append(filtered, lp)
	}
	return filtered
}

// filterProxyPorts removes ports that are our proxy ports (ephemeral ports we assigned)
func (s *Server) filterProxyPorts(ports []preview.ListeningPort) []preview.ListeningPort {
	proxyPorts := make(map[int]bool)
	for _, p := range s.state.GetPreviews() {
		if p.ProxyPort > 0 {
			proxyPorts[p.ProxyPort] = true
		}
	}

	var filtered []preview.ListeningPort
	for _, lp := range ports {
		if !proxyPorts[lp.Port] {
			filtered = append(filtered, lp)
		}
	}
	return filtered
}

// matchesBrainstormPIDFile checks if the given PID matches any
// .superpowers/brainstorm/*/state/server.pid file in the workspace.
func matchesBrainstormPIDFile(workspacePath string, pid int) bool {
	pattern := filepath.Join(workspacePath, ".superpowers", "brainstorm", "*", "state", "server.pid")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return false
	}
	pidStr := strconv.Itoa(pid)
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(data)) == pidStr {
			return true
		}
	}
	return false
}

// filterNonHTTPPorts removes ports that don't speak HTTP by sending a HEAD request
// to each. Returns only ports that respond with a valid HTTP response.
func filterNonHTTPPorts(ports []preview.ListeningPort) []preview.ListeningPort {
	timeout := 1 * time.Second
	var filtered []preview.ListeningPort
	for _, lp := range ports {
		addr := lp.Host
		if strings.Contains(lp.Host, ":") {
			addr = "[" + lp.Host + "]"
		}
		url := fmt.Sprintf("http://%s:%d/", addr, lp.Port)

		client := &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{Timeout: timeout}).DialContext,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}

		req, err := http.NewRequest("HEAD", url, nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		filtered = append(filtered, lp)
	}
	return filtered
}

// filterDaemonPort removes the daemon's own listening port to prevent
// creating a preview for schmux's web UI during dev mode.
func (s *Server) filterDaemonPort(ports []preview.ListeningPort) []preview.ListeningPort {
	daemonPort := s.config.GetPort()
	if daemonPort <= 0 {
		return ports
	}
	var filtered []preview.ListeningPort
	for _, lp := range ports {
		if lp.Port != daemonPort {
			filtered = append(filtered, lp)
		}
	}
	return filtered
}

// detectListeningPortsByPID finds TCP ports that a process or any of its descendants are listening on.
// Returns preview.ListeningPort entries with host information (IPv4 or IPv6 loopback).
func detectListeningPortsByPID(pid int) []preview.ListeningPort {
	if pid <= 0 {
		return nil
	}

	// Get all descendant PIDs (children, grandchildren, etc.)
	pids := getDescendantPIDs(pid)
	pids = append(pids, pid) // include the original

	// Collect ports from all PIDs, keyed by port to dedupe
	// Prefer IPv4 over IPv6 when both are available
	portMap := make(map[int]preview.ListeningPort)
	for _, p := range pids {
		// Try ss first (Linux)
		for _, lp := range detectPortsViaSS(p) {
			if existing, ok := portMap[lp.Port]; !ok || (existing.Host == "::1" && lp.Host == "127.0.0.1") {
				portMap[lp.Port] = lp
			}
		}
		// Try lsof (macOS)
		for _, lp := range detectPortsViaLsof(p) {
			if existing, ok := portMap[lp.Port]; !ok || (existing.Host == "::1" && lp.Host == "127.0.0.1") {
				portMap[lp.Port] = lp
			}
		}
	}

	if len(portMap) == 0 {
		return nil
	}
	result := make([]preview.ListeningPort, 0, len(portMap))
	for _, lp := range portMap {
		result = append(result, lp)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Port < result[j].Port
	})
	return result
}

// getDescendantPIDs returns all descendant PIDs of a process (children, grandchildren, etc.)
func getDescendantPIDs(pid int) []int {
	var descendants []int
	children := getChildPIDs(pid)
	for _, child := range children {
		descendants = append(descendants, child)
		descendants = append(descendants, getDescendantPIDs(child)...)
	}
	return descendants
}

// getChildPIDs returns direct child PIDs of a process
func getChildPIDs(pid int) []int {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// pgrep -P <pid> returns direct children
	cmd := exec.CommandContext(ctx, "pgrep", "-P", strconv.Itoa(pid))
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil
	}

	var children []int
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		childPID, err := strconv.Atoi(line)
		if err != nil || childPID <= 0 {
			continue
		}
		children = append(children, childPID)
	}
	return children
}

// detectPortsViaSS uses ss (Linux) to find listening TCP ports for a PID.
// Returns preview.ListeningPort entries with host information derived from the address prefix.
func detectPortsViaSS(pid int) []preview.ListeningPort {
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ss", "-tlnp")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	portMap := map[int]preview.ListeningPort{}
	pidStr := fmt.Sprintf("pid=%d", pid)
	lines := strings.Split(string(out), "\n")

	for _, line := range lines {
		if !strings.Contains(line, pidStr) {
			continue
		}

		// Extract ownerPID from pid= field in line
		var ownerPID int
		if pidMatch := pidFieldRegex.FindStringSubmatch(line); len(pidMatch) >= 2 {
			ownerPID, _ = strconv.Atoi(pidMatch[1])
		}

		fields := strings.Fields(line)
		for _, field := range fields {
			var host string
			var isWildcard bool

			switch {
			case strings.HasPrefix(field, "[::]:"):
				host = "::1"
				isWildcard = true
			case strings.HasPrefix(field, "[::1]:"):
				host = "::1"
			case strings.HasPrefix(field, "0.0.0.0:"):
				host = "127.0.0.1"
				isWildcard = true
			case strings.HasPrefix(field, "127.0.0.1:"):
				host = "127.0.0.1"
			case strings.HasPrefix(field, "*:"):
				host = "127.0.0.1"
				isWildcard = true
			default:
				continue
			}

			idx := strings.LastIndex(field, ":")
			if idx < 0 || idx+1 >= len(field) {
				continue
			}
			portField := field[idx+1:]
			if strings.HasSuffix(portField, "]") {
				continue
			}
			port, err := strconv.Atoi(portField)
			if err != nil || port <= 0 || port > 65535 {
				continue
			}

			// For wildcard listeners, prefer IPv4 (127.0.0.1)
			// If we already have a non-wildcard entry, keep it
			existing, hasExisting := portMap[port]
			if hasExisting && !isWildcard {
				continue
			}
			// For wildcards, only update if we don't have an entry or current is IPv6
			if isWildcard {
				if !hasExisting || existing.Host == "::1" {
					portMap[port] = preview.ListeningPort{Host: host, Port: port, OwnerPID: ownerPID}
				}
			} else {
				portMap[port] = preview.ListeningPort{Host: host, Port: port, OwnerPID: ownerPID}
			}
		}
	}

	if len(portMap) == 0 {
		return nil
	}
	result := make([]preview.ListeningPort, 0, len(portMap))
	for _, lp := range portMap {
		result = append(result, lp)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Port < result[j].Port
	})
	return result
}

// detectPortsForPIDFunc is the function used to find listening TCP ports for a PID via lsof.
// Tests can replace this with a lightweight implementation to avoid lsof.
var detectPortsForPIDFunc = defaultDetectPortsViaLsof

// detectPortsViaLsof delegates to the pluggable function.
func detectPortsViaLsof(pid int) []preview.ListeningPort {
	return detectPortsForPIDFunc(pid)
}

// defaultDetectPortsViaLsof uses lsof (macOS) to find listening TCP ports for a PID.
func defaultDetectPortsViaLsof(pid int) []preview.ListeningPort {
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "lsof", "-Pan", "-p", strconv.Itoa(pid), "-iTCP", "-sTCP:LISTEN")
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil
	}

	portMap := map[int]preview.ListeningPort{}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if !strings.Contains(line, "TCP") {
			continue
		}

		// Determine IPv4 vs IPv6 from the line
		// lsof output has columns including TYPE which shows "IPv4" or "IPv6"
		var host string
		isWildcard := false

		if strings.Contains(line, "IPv6") {
			host = "::1"
		} else if strings.Contains(line, "IPv4") {
			host = "127.0.0.1"
		} else {
			// Fallback: can't determine, assume IPv4
			host = "127.0.0.1"
		}

		// Check if it's a wildcard listener (*:port)
		if strings.Contains(line, "*:") {
			isWildcard = true
		}

		idx := strings.LastIndex(line, ":")
		if idx < 0 || idx+1 >= len(line) {
			continue
		}
		portField := line[idx+1:]
		for i, r := range portField {
			if r < '0' || r > '9' {
				portField = portField[:i]
				break
			}
		}
		port, err := strconv.Atoi(portField)
		if err != nil || port <= 0 || port > 65535 {
			continue
		}

		// For wildcard listeners, prefer IPv4 (127.0.0.1)
		// If we already have a non-wildcard entry, keep it
		existing, hasExisting := portMap[port]
		if hasExisting && !isWildcard {
			continue
		}
		// For wildcards, only update if we don't have an entry or current is IPv6
		if isWildcard {
			if !hasExisting || existing.Host == "::1" {
				portMap[port] = preview.ListeningPort{Host: host, Port: port, OwnerPID: pid}
			}
		} else {
			portMap[port] = preview.ListeningPort{Host: host, Port: port, OwnerPID: pid}
		}
	}

	if len(portMap) == 0 {
		return nil
	}
	result := make([]preview.ListeningPort, 0, len(portMap))
	for _, lp := range portMap {
		result = append(result, lp)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Port < result[j].Port
	})
	return result
}
