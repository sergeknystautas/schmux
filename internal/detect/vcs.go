package detect

import (
	"os/exec"
)

// VCSTool represents a detected version control system.
type VCSTool struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// TmuxStatus represents the detected tmux availability.
type TmuxStatus struct {
	Available bool   `json:"available"`
	Path      string `json:"path,omitempty"`
}

// DetectVCS checks PATH for known VCS binaries and returns detected tools.
func DetectVCS() []VCSTool {
	candidates := []struct {
		name   string
		binary string
	}{
		{"git", "git"},
		{"sapling", "sl"},
	}

	var tools []VCSTool
	for _, c := range candidates {
		if !commandExists(c.binary) {
			continue
		}
		path, _ := exec.LookPath(c.binary)
		tools = append(tools, VCSTool{
			Name: c.name,
			Path: path,
		})
	}
	return tools
}

// DetectTmux checks if tmux is available.
func DetectTmux() TmuxStatus {
	if !commandExists("tmux") {
		return TmuxStatus{Available: false}
	}
	path, _ := exec.LookPath("tmux")
	return TmuxStatus{
		Available: true,
		Path:      path,
	}
}
