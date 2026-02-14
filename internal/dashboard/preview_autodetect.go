package dashboard

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/signal"
)

const previewAutoDetectCooldown = 45 * time.Second

// ListeningPort represents a detected listening port with its host address.
type ListeningPort struct {
	Host string
	Port int
}

// scanExistingSessionsForPreviews checks all local sessions for listening ports
// and creates previews for any web servers found. Called on daemon startup.
func (s *Server) scanExistingSessionsForPreviews() {
	if s.previewManager == nil {
		return
	}

	sessions := s.state.GetSessions()
	for _, sess := range sessions {
		// Skip remote sessions
		if sess.RemoteHostID != "" || sess.Pid <= 0 {
			continue
		}

		ws, found := s.state.GetWorkspace(sess.WorkspaceID)
		if !found || ws.RemoteHostID != "" {
			continue
		}

		// Find ports this session is listening on
		listeningPorts := detectListeningPortsByPID(sess.Pid)
		if len(listeningPorts) == 0 {
			continue
		}

		// Filter out ports we already have previews for
		ports := s.filterExistingPreviews(ws.ID, listeningPorts)
		if len(ports) == 0 {
			continue
		}

		// Filter out our own proxy ports
		ports = s.filterProxyPorts(ports)
		if len(ports) == 0 {
			continue
		}

		// Create previews for found ports
		now := time.Now().UTC()
		for _, lp := range ports {
			key := fmt.Sprintf("%s:%d", ws.ID, lp.Port)
			s.previewDetectMu.Lock()
			s.previewDetect[key] = now
			s.previewDetectMu.Unlock()

			ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
			preview, err := s.previewManager.CreateOrGet(ctx, ws, lp.Host, lp.Port)
			cancel()
			if err != nil {
				fmt.Printf("[preview] failed to create preview for %s:%d: %v\n", lp.Host, lp.Port, err)
				continue
			}
			fmt.Printf("[preview] created preview for workspace %s %s:%d -> proxy %d\n", ws.ID, lp.Host, lp.Port, preview.ProxyPort)
		}
	}
}

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

	// Find http(s):// URLs in terminal output, extract ports
	candidatePorts := detectPortsFromChunk(chunk)
	if len(candidatePorts) == 0 {
		return
	}

	// Verify which ports the session is actually listening on
	listeningPorts := detectListeningPortsByPID(sess.Pid)
	if len(listeningPorts) == 0 {
		return
	}

	// Only keep ports that are actually listening (with host info)
	ports := intersectPorts(candidatePorts, listeningPorts)
	if len(ports) == 0 {
		return
	}

	// Filter out ports we already have previews for
	ports = s.filterExistingPreviews(ws.ID, ports)
	if len(ports) == 0 {
		return
	}

	// Filter out our own proxy ports
	ports = s.filterProxyPorts(ports)
	if len(ports) == 0 {
		return
	}

	// Create previews for verified ports
	now := time.Now().UTC()
	createdAny := false
	for _, lp := range ports {
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
		_, err := s.previewManager.CreateOrGet(ctx, ws, lp.Host, lp.Port)
		cancel()
		if err != nil {
			continue
		}
		createdAny = true
	}

	if createdAny {
		go s.BroadcastSessions()
	}
}

// urlRegex matches http(s)://host[:port] patterns
// Groups: 1=scheme, 2=host, 3=:port (with colon), 4=port (digits only)
var urlRegex = regexp.MustCompile(`(?i)(https?)://([a-zA-Z0-9.\[\]-]+)(:(\d+))?`)

// detectPortsFromChunk finds http(s):// URLs in terminal output and extracts ports.
// Defaults to 80 for http, 443 for https if no port specified.
func detectPortsFromChunk(chunk []byte) []int {
	clean := string(signal.StripANSIBytes(nil, chunk))
	if clean == "" {
		return nil
	}

	seen := make(map[int]bool)
	var ports []int

	for _, match := range urlRegex.FindAllStringSubmatch(clean, -1) {
		if len(match) < 5 {
			continue
		}
		scheme := strings.ToLower(match[1])
		portStr := match[4]

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

		if seen[port] {
			continue
		}
		seen[port] = true
		ports = append(ports, port)
	}

	sort.Ints(ports)
	return ports
}

// filterExistingPreviews removes ports that already have previews for this workspace.
// Returns ListeningPort entries for ports that don't have existing previews.
func (s *Server) filterExistingPreviews(workspaceID string, ports []ListeningPort) []ListeningPort {
	var filtered []ListeningPort
	for _, lp := range ports {
		if _, exists := s.state.FindPreview(workspaceID, lp.Host, lp.Port); exists {
			continue
		}
		// Also check with the other loopback address (in case preview was created with different host)
		otherHost := "::1"
		if lp.Host == "::1" {
			otherHost = "127.0.0.1"
		}
		if _, exists := s.state.FindPreview(workspaceID, otherHost, lp.Port); exists {
			continue
		}
		filtered = append(filtered, lp)
	}
	return filtered
}

// intersectPorts returns listening ports from candidates that are in the listening set.
// Returns ListeningPort entries with host information from the listening set.
func intersectPorts(candidates []int, listening []ListeningPort) []ListeningPort {
	// Build map with IPv4 preference
	listeningMap := make(map[int]ListeningPort)
	for _, lp := range listening {
		// Prefer IPv4 over IPv6 if both exist
		if existing, ok := listeningMap[lp.Port]; !ok || (existing.Host == "::1" && lp.Host == "127.0.0.1") {
			listeningMap[lp.Port] = lp
		}
	}

	// Build candidate set
	candidateSet := make(map[int]bool)
	for _, p := range candidates {
		candidateSet[p] = true
	}

	// Build result from the map (which has IPv4 preference applied)
	var result []ListeningPort
	for port, lp := range listeningMap {
		if candidateSet[port] {
			result = append(result, lp)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Port < result[j].Port
	})
	return result
}

// filterProxyPorts removes ports that are our proxy ports (ephemeral ports we assigned)
func (s *Server) filterProxyPorts(ports []ListeningPort) []ListeningPort {
	proxyPorts := make(map[int]bool)
	for _, preview := range s.state.GetPreviews() {
		if preview.ProxyPort > 0 {
			proxyPorts[preview.ProxyPort] = true
		}
	}

	var filtered []ListeningPort
	for _, lp := range ports {
		if !proxyPorts[lp.Port] {
			filtered = append(filtered, lp)
		}
	}
	return filtered
}

// detectListeningPortsByPID finds TCP ports that a process or any of its descendants are listening on.
// Returns ListeningPort entries with host information (IPv4 or IPv6 loopback).
func detectListeningPortsByPID(pid int) []ListeningPort {
	if pid <= 0 {
		return nil
	}

	// Get all descendant PIDs (children, grandchildren, etc.)
	pids := getDescendantPIDs(pid)
	pids = append(pids, pid) // include the original

	// Collect ports from all PIDs, keyed by port to dedupe
	// Prefer IPv4 over IPv6 when both are available
	portMap := make(map[int]ListeningPort)
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
	result := make([]ListeningPort, 0, len(portMap))
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
// Returns ListeningPort entries with host information derived from the address prefix.
func detectPortsViaSS(pid int) []ListeningPort {
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ss", "-tlnp")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	portMap := map[int]ListeningPort{}
	pidStr := fmt.Sprintf("pid=%d", pid)
	lines := strings.Split(string(out), "\n")

	for _, line := range lines {
		if !strings.Contains(line, pidStr) {
			continue
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
					portMap[port] = ListeningPort{Host: host, Port: port}
				}
			} else {
				portMap[port] = ListeningPort{Host: host, Port: port}
			}
		}
	}

	if len(portMap) == 0 {
		return nil
	}
	result := make([]ListeningPort, 0, len(portMap))
	for _, lp := range portMap {
		result = append(result, lp)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Port < result[j].Port
	})
	return result
}

// detectPortsViaLsof uses lsof (macOS) to find listening TCP ports for a PID.
// Returns ListeningPort entries with host information derived from the address type.
func detectPortsViaLsof(pid int) []ListeningPort {
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "lsof", "-Pan", "-p", strconv.Itoa(pid), "-iTCP", "-sTCP:LISTEN")
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil
	}

	portMap := map[int]ListeningPort{}
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
				portMap[port] = ListeningPort{Host: host, Port: port}
			}
		} else {
			portMap[port] = ListeningPort{Host: host, Port: port}
		}
	}

	if len(portMap) == 0 {
		return nil
	}
	result := make([]ListeningPort, 0, len(portMap))
	for _, lp := range portMap {
		result = append(result, lp)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Port < result[j].Port
	})
	return result
}
