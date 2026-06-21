package contracts

// DependencyInstallMethod is one OS-specific way to install a dependency.
type DependencyInstallMethod struct {
	OS       string `json:"os"`
	Label    string `json:"label"`
	Command  string `json:"command,omitempty"`
	URL      string `json:"url,omitempty"`
	Requires string `json:"requires,omitempty"`
}

// Dependency is a detected (or missing) tool with its metadata.
type Dependency struct {
	ID          string                    `json:"id"`
	DisplayName string                    `json:"display_name"`
	Description string                    `json:"description"`
	Unlocks     []string                  `json:"unlocks,omitempty"`
	DocsURL     string                    `json:"docs_url,omitempty"`
	Detected    bool                      `json:"detected"`
	Command     string                    `json:"command,omitempty"`
	Source      string                    `json:"source,omitempty"`
	Install     []DependencyInstallMethod `json:"install,omitempty"`
}

// DependencyGroup is a functional bucket with its member dependencies.
type DependencyGroup struct {
	ID           string       `json:"id"`
	DisplayName  string       `json:"display_name"`
	Description  string       `json:"description"`
	Dependencies []Dependency `json:"dependencies"`
}

// DependenciesResponse is the GET /api/dependencies payload.
type DependenciesResponse struct {
	OS     string            `json:"os"` // "macos" | "linux"
	Groups []DependencyGroup `json:"groups"`
}
