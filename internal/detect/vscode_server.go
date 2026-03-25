package detect

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// VSCodeServerInfo describes VS Code server availability on this machine.
type VSCodeServerInfo struct {
	// Hostname of this machine (for SSH remote URIs).
	Hostname string `json:"hostname,omitempty"`

	// WebServerRunning is true if "code serve-web" is detected.
	WebServerRunning bool `json:"web_server_running,omitempty"`
	// WebServerPort is the port the web server is listening on (0 if unknown).
	WebServerPort int `json:"web_server_port,omitempty"`

	// TunnelRunning is true if "code tunnel" is detected.
	TunnelRunning bool `json:"tunnel_running,omitempty"`

	// HasVSCodeServer is true if ~/.vscode-server exists (SSH Remote was used before).
	HasVSCodeServer bool `json:"has_vscode_server,omitempty"`
}

// DetectVSCodeServer checks for VS Code server processes running on this machine.
func DetectVSCodeServer() VSCodeServerInfo {
	info := VSCodeServerInfo{}

	// Get hostname
	if h, err := os.Hostname(); err == nil {
		info.Hostname = h
	}

	// Check for code serve-web process
	if processContains("code", "serve-web") {
		info.WebServerRunning = true
		info.WebServerPort = probeWebServerPort()
	}

	// Check for code tunnel process
	if processContains("code", "tunnel") {
		info.TunnelRunning = true
	}

	// Check for ~/.vscode-server directory (indicates SSH Remote was used)
	if home := homeDirOrTilde(); home != "~" {
		if fi, err := os.Stat(home + "/.vscode-server"); err == nil && fi.IsDir() {
			info.HasVSCodeServer = true
		}
	}

	return info
}

// BuildVSCodeRemoteURI builds a vscode:// URI for opening VS Code with SSH Remote.
// Returns a URI like: vscode://vscode-remote/ssh-remote+hostname/path
func BuildVSCodeRemoteURI(hostname, path string) string {
	// Ensure path starts with /
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return fmt.Sprintf("vscode://vscode-remote/ssh-remote+%s%s", hostname, path)
}

// processContains checks if any running process command line contains all the given substrings.
func processContains(substrings ...string) bool {
	// Use ps aux to list all processes with full command lines
	cmd := exec.Command("ps", "aux")
	out, err := cmd.Output()
	if err != nil {
		return false
	}

	for _, line := range strings.Split(string(out), "\n") {
		matchAll := true
		for _, sub := range substrings {
			if !strings.Contains(line, sub) {
				matchAll = false
				break
			}
		}
		if matchAll {
			return true
		}
	}
	return false
}

// probeWebServerPort tries common VS Code serve-web ports and returns the first one that's listening.
func probeWebServerPort() int {
	for _, port := range []int{8000, 8080, 8443} {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
		if err == nil {
			conn.Close()
			return port
		}
	}
	return 0
}
