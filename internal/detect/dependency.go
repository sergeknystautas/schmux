package detect

import (
	"context"
	"runtime"
	"sort"
)

// InstallMethod is one way to install a dependency on one platform.
// OS is "macos" | "linux" | "any" ("linux" covers WSL2). Requires names a
// package manager ("homebrew" | "npm" | "") the method depends on.
type InstallMethod struct {
	OS       string `yaml:"os"`
	Label    string `yaml:"label"`
	Command  string `yaml:"command"`
	URL      string `yaml:"url"`
	Requires string `yaml:"requires"`
}

// DepGroup is a functional bucket of dependencies (organizational only).
type DepGroup struct {
	ID          string
	DisplayName string
	Description string
}

// Groups is the ordered set of functional groups.
var Groups = []DepGroup{
	{ID: "agents", DisplayName: "AI agents", Description: "Coding agents schmux orchestrates."},
	{ID: "vcs", DisplayName: "Version control", Description: "Back workspaces with a VCS."},
	{ID: "terminal", DisplayName: "Terminal", Description: "Run agent sessions."},
	{ID: "sandbox", DisplayName: "Sandbox", Description: "Restrict what agent commands can touch."},
	{ID: "editors", DisplayName: "Editor integrations", Description: "Open workspaces in your tools."},
}

// DetectionResult is the outcome of detecting one dependency.
type DetectionResult struct {
	Detected bool
	Command  string
	Source   string
}

// Dependency is the static definition of a detectable tool.
type Dependency struct {
	ID          string
	DisplayName string
	Group       string
	Description string
	DocsURL     string
	Unlocks     []string
	Platforms   []string // OS where relevant; empty = all
	Install     []InstallMethod
	detect      func(ctx context.Context) DetectionResult // nil for agents
}

// DependencyStatus is a Dependency plus its detection result.
type DependencyStatus struct {
	Dependency
	Detected bool
	Command  string
	Source   string
}

// DependencyReport is the full detection output.
type DependencyReport struct {
	Statuses        []DependencyStatus
	PackageManagers map[string]bool // "homebrew","npm" -> present
}

// Status returns the dependency status with the given ID.
func (r DependencyReport) Status(id string) (DependencyStatus, bool) {
	for _, s := range r.Statuses {
		if s.ID == id {
			return s, true
		}
	}
	return DependencyStatus{}, false
}

// normalizeOS maps runtime.GOOS to an install-method OS tag.
func normalizeOS(goos string) string {
	if goos == "darwin" {
		return "macos"
	}
	return goos // "linux"
}

// platformAllows reports whether a dependency with the given Platforms list is
// relevant on goos (empty list = all platforms).
func platformAllows(platforms []string, goos string) bool {
	if len(platforms) == 0 {
		return true
	}
	want := normalizeOS(goos)
	for _, p := range platforms {
		if p == want {
			return true
		}
	}
	return false
}

// InstallForOS returns the install methods relevant to goos (matching OS or
// "any"), ordered so methods whose Requires package manager is present (or
// empty) come first. Stable: original order preserved within each bucket.
func InstallForOS(methods []InstallMethod, goos string, have map[string]bool) []InstallMethod {
	want := normalizeOS(goos)
	var out []InstallMethod
	for _, m := range methods {
		if m.OS == want || m.OS == "any" {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return installReady(out[i], have) && !installReady(out[j], have)
	})
	return out
}

func installReady(m InstallMethod, have map[string]bool) bool {
	return m.Requires == "" || have[m.Requires]
}

// deriveInstall builds install methods implied by a descriptor's detect entries
// (homebrew cask/formula -> brew; npm_global -> npm), then appends its explicit
// Install block.
func deriveInstall(d *Descriptor) []InstallMethod {
	var out []InstallMethod
	for _, e := range d.Detect {
		switch e.Type {
		case "homebrew_cask":
			out = append(out, InstallMethod{OS: "macos", Label: "Homebrew", Command: "brew install --cask " + e.Name, Requires: "homebrew"})
		case "homebrew_formula":
			out = append(out, InstallMethod{OS: "macos", Label: "Homebrew", Command: "brew install " + e.Name, Requires: "homebrew"})
		case "npm_global":
			out = append(out, InstallMethod{OS: "any", Label: "npm", Command: "npm i -g " + e.Package, Requires: "npm"})
		}
	}
	out = append(out, d.Install...)
	return out
}

// currentOS is the install-method OS tag for this host.
func currentOS() string { return normalizeOS(runtime.GOOS) }
