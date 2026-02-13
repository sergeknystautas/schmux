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
		for _, port := range ports {
			key := fmt.Sprintf("%s:%d", ws.ID, port)
			s.previewDetectMu.Lock()
			s.previewDetect[key] = now
			s.previewDetectMu.Unlock()

			ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
			preview, err := s.previewManager.CreateOrGet(ctx, ws, "127.0.0.1", port)
			cancel()
			if err != nil {
				fmt.Printf("[preview] failed to create preview for port %d: %v\n", port, err)
				continue
			}
			fmt.Printf("[preview] created preview for workspace %s port %d -> proxy %d\n", ws.ID, port, preview.ProxyPort)
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

	// Filter out ports we already have previews for
	candidatePorts = s.filterExistingPreviews(ws.ID, candidatePorts)
	if len(candidatePorts) == 0 {
		return
	}

	// Verify which ports the session is actually listening on
	listeningPorts := detectListeningPortsByPID(sess.Pid)
	if len(listeningPorts) == 0 {
		return
	}

	// Only keep ports that are actually listening
	ports := intersectPorts(candidatePorts, listeningPorts)
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
	for _, port := range ports {
		key := fmt.Sprintf("%s:%d", ws.ID, port)
		s.previewDetectMu.Lock()
		last, hasLast := s.previewDetect[key]
		if hasLast && now.Sub(last) < previewAutoDetectCooldown {
			s.previewDetectMu.Unlock()
			continue
		}
		s.previewDetect[key] = now
		s.previewDetectMu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		_, err := s.previewManager.CreateOrGet(ctx, ws, "127.0.0.1", port)
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

// filterExistingPreviews removes ports that already have previews for this workspace
func (s *Server) filterExistingPreviews(workspaceID string, ports []int) []int {
	var filtered []int
	for _, port := range ports {
		if _, exists := s.state.FindPreview(workspaceID, "127.0.0.1", port); exists {
			continue
		}
		filtered = append(filtered, port)
	}
	return filtered
}

// intersectPorts returns ports from candidates that are in the listening set
func intersectPorts(candidates, listening []int) []int {
	listeningSet := make(map[int]bool)
	for _, p := range listening {
		listeningSet[p] = true
	}

	seen := make(map[int]bool)
	var result []int
	for _, p := range candidates {
		if listeningSet[p] && !seen[p] {
			seen[p] = true
			result = append(result, p)
		}
	}
	sort.Ints(result)
	return result
}

// filterProxyPorts removes ports that are our proxy ports (ephemeral ports we assigned)
func (s *Server) filterProxyPorts(ports []int) []int {
	proxyPorts := make(map[int]bool)
	for _, preview := range s.state.GetPreviews() {
		if preview.ProxyPort > 0 {
			proxyPorts[preview.ProxyPort] = true
		}
	}

	var filtered []int
	for _, port := range ports {
		if !proxyPorts[port] {
			filtered = append(filtered, port)
		}
	}
	return filtered
}

// detectListeningPortsByPID finds TCP ports that a process or any of its descendants are listening on
func detectListeningPortsByPID(pid int) []int {
	if pid <= 0 {
		return nil
	}

	// Get all descendant PIDs (children, grandchildren, etc.)
	pids := getDescendantPIDs(pid)
	pids = append(pids, pid) // include the original

	// Collect ports from all PIDs
	allPorts := map[int]bool{}
	for _, p := range pids {
		// Try ss first (Linux)
		for _, port := range detectPortsViaSS(p) {
			allPorts[port] = true
		}
		// Try lsof (macOS)
		for _, port := range detectPortsViaLsof(p) {
			allPorts[port] = true
		}
	}

	if len(allPorts) == 0 {
		return nil
	}
	result := make([]int, 0, len(allPorts))
	for port := range allPorts {
		result = append(result, port)
	}
	sort.Ints(result)
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
func detectPortsViaSS(pid int) []int {
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ss", "-tlnp")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	ports := map[int]bool{}
	pidStr := fmt.Sprintf("pid=%d", pid)
	lines := strings.Split(string(out), "\n")

	for _, line := range lines {
		if !strings.Contains(line, pidStr) {
			continue
		}
		fields := strings.Fields(line)
		for _, field := range fields {
			if strings.HasPrefix(field, "*:") || strings.HasPrefix(field, "0.0.0.0:") ||
				strings.HasPrefix(field, "127.0.0.1:") || strings.HasPrefix(field, "[::]:") ||
				strings.HasPrefix(field, "[::1]:") {
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
				ports[port] = true
			}
		}
	}

	if len(ports) == 0 {
		return nil
	}
	result := make([]int, 0, len(ports))
	for port := range ports {
		result = append(result, port)
	}
	sort.Ints(result)
	return result
}

// detectPortsViaLsof uses lsof (macOS) to find listening TCP ports for a PID.
func detectPortsViaLsof(pid int) []int {
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "lsof", "-Pan", "-p", strconv.Itoa(pid), "-iTCP", "-sTCP:LISTEN")
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil
	}

	ports := map[int]bool{}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if !strings.Contains(line, "TCP") {
			continue
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
		ports[port] = true
	}

	if len(ports) == 0 {
		return nil
	}
	result := make([]int, 0, len(ports))
	for port := range ports {
		result = append(result, port)
	}
	sort.Ints(result)
	return result
}
