package detect

import (
	"context"
	"sort"
	"sync"
)

// nativeDeps are the non-agent dependencies, each wrapping an existing detector.
var nativeDeps = []Dependency{
	{
		ID: "git", DisplayName: "git", Group: "vcs",
		Description: "Git version control.",
		Unlocks:     []string{"Git workspaces and worktrees"},
		DocsURL:     "https://git-scm.com",
		Install: []InstallMethod{
			{OS: "macos", Label: "Homebrew", Command: "brew install git", Requires: "homebrew"},
			{OS: "linux", Label: "apt", Command: "sudo apt install git"},
		},
		detect: func(ctx context.Context) DetectionResult { return detectVCSNamed("git") },
	},
	{
		ID: "sapling", DisplayName: "sapling", Group: "vcs",
		Description: "Sapling source control.",
		Unlocks:     []string{"Sapling-backed workspaces"},
		DocsURL:     "https://sapling-scm.com",
		Install: []InstallMethod{
			{OS: "macos", Label: "Homebrew", Command: "brew install sapling", Requires: "homebrew"},
			{OS: "linux", Label: "Instructions", URL: "https://sapling-scm.com/docs/introduction/installation"},
		},
		detect: func(ctx context.Context) DetectionResult { return detectVCSNamed("sapling") },
	},
	{
		ID: "tmux", DisplayName: "tmux", Group: "terminal",
		Description: "Terminal multiplexer schmux runs sessions in.",
		Unlocks:     []string{"Spawning any session"},
		DocsURL:     "https://github.com/tmux/tmux/wiki",
		Install: []InstallMethod{
			{OS: "macos", Label: "Homebrew", Command: "brew install tmux", Requires: "homebrew"},
			{OS: "linux", Label: "apt", Command: "sudo apt install tmux"},
		},
		detect: func(ctx context.Context) DetectionResult {
			s := DetectTmux()
			return DetectionResult{Detected: s.Available, Command: s.Path, Source: "PATH"}
		},
	},
	{
		ID: "fence", DisplayName: "fence", Group: "sandbox",
		Description: "Lightweight, container-free sandbox for running commands with network and filesystem restrictions.",
		Unlocks:     []string{"Run agent commands in a container-free sandbox with network and filesystem restrictions"},
		DocsURL:     "https://github.com/fencesandbox/fence",
		Install: []InstallMethod{
			{OS: "macos", Label: "Homebrew", Command: "brew tap fencesandbox/tap && brew install fencesandbox/tap/fence", Requires: "homebrew"},
			{OS: "macos", Label: "Install script", Command: "curl -fsSL https://cli.fencesandbox.com/install.sh | sh"},
			{OS: "linux", Label: "Install script", Command: "curl -fsSL https://cli.fencesandbox.com/install.sh | sh"},
		},
		detect: func(ctx context.Context) DetectionResult {
			if commandExists("fence") {
				return DetectionResult{Detected: true, Command: "fence", Source: "PATH"}
			}
			return DetectionResult{}
		},
	},
	{
		ID: "vscode", DisplayName: "VS Code", Group: "editors",
		Description: "Visual Studio Code editor.",
		Unlocks:     []string{"Open workspaces in VS Code", "VS Code remote server / tunnel"},
		DocsURL:     "https://code.visualstudio.com",
		Install: []InstallMethod{
			{OS: "macos", Label: "Download", URL: "https://code.visualstudio.com/download"},
			{OS: "linux", Label: "Download", URL: "https://code.visualstudio.com/download"},
		},
		detect: func(ctx context.Context) DetectionResult {
			p, ok := ResolveVSCodePath(ctx)
			if !ok {
				return DetectionResult{}
			}
			return DetectionResult{Detected: true, Command: p.Path, Source: p.Source}
		},
	},
	{
		ID: "iterm2", DisplayName: "iTerm2", Group: "editors", Platforms: []string{"macos"},
		Description: "iTerm2 terminal emulator (macOS).",
		Unlocks:     []string{"Open sessions in iTerm2"},
		DocsURL:     "https://iterm2.com",
		Install: []InstallMethod{
			{OS: "macos", Label: "Homebrew", Command: "brew install --cask iterm2", Requires: "homebrew"},
			{OS: "macos", Label: "Download", URL: "https://iterm2.com/downloads.html"},
		},
		detect: func(ctx context.Context) DetectionResult {
			if ITerm2Available() {
				return DetectionResult{Detected: true, Source: "/Applications/iTerm.app"}
			}
			return DetectionResult{}
		},
	},
}

// detectVCSNamed adapts DetectVCS (which returns all detected VCS tools) to a
// single-tool DetectionResult.
func detectVCSNamed(name string) DetectionResult {
	for _, v := range DetectVCS() {
		if v.Name == name {
			return DetectionResult{Detected: true, Command: v.Path, Source: "PATH"}
		}
	}
	return DetectionResult{}
}

// agentDeps holds agent Dependency definitions built from descriptors at
// registration time (see registerAgentDependency).
var (
	agentDeps   []Dependency
	agentDepsMu sync.Mutex
)

// registerAgentDependency records an agent Dependency built from a descriptor.
// Called from RegisterDescriptorAdapters. Idempotent per ID (last wins, so a
// runtime override replaces the builtin).
func registerAgentDependency(d *Descriptor) {
	agentDepsMu.Lock()
	defer agentDepsMu.Unlock()
	dep := Dependency{
		ID: d.Name, DisplayName: d.DisplayName, Group: "agents",
		Description: d.Description, DocsURL: d.DocsURL, Unlocks: d.Unlocks,
		Install: deriveInstall(d),
	}
	if dep.DisplayName == "" {
		dep.DisplayName = d.Name
	}
	for i := range agentDeps {
		if agentDeps[i].ID == dep.ID {
			agentDeps[i] = dep
			return
		}
	}
	agentDeps = append(agentDeps, dep)
}

// AllDependencies returns every dependency definition (agents + native),
// ordered by Groups. Detection is not run.
func AllDependencies() []Dependency {
	agentDepsMu.Lock()
	agents := append([]Dependency(nil), agentDeps...)
	agentDepsMu.Unlock()
	sort.Slice(agents, func(i, j int) bool { return agents[i].ID < agents[j].ID })

	all := append(agents, nativeDeps...)
	order := map[string]int{}
	for i, g := range Groups {
		order[g.ID] = i
	}
	sort.SliceStable(all, func(i, j int) bool { return order[all[i].Group] < order[all[j].Group] })
	return all
}

// DetectDependencies builds the full report. Agents come from the passed-in
// list (models.Manager is the source of truth); native deps run their detect
// func concurrently. Package managers are detected for install tailoring.
func DetectDependencies(ctx context.Context, agents []Tool) DependencyReport {
	agentByID := make(map[string]Tool, len(agents))
	for _, t := range agents {
		agentByID[t.Name] = t
	}

	deps := AllDependencies()
	statuses := make([]DependencyStatus, len(deps))
	var wg sync.WaitGroup
	for i, dep := range deps {
		if !platformAllows(dep.Platforms, currentOS()) {
			statuses[i] = DependencyStatus{Dependency: dep} // omitted later by handler
			continue
		}
		if dep.Group == "agents" {
			t, ok := agentByID[dep.ID]
			statuses[i] = DependencyStatus{Dependency: dep, Detected: ok, Command: t.Command, Source: t.Source}
			continue
		}
		wg.Add(1)
		go func(i int, dep Dependency) {
			defer wg.Done()
			r := dep.detect(ctx)
			statuses[i] = DependencyStatus{Dependency: dep, Detected: r.Detected, Command: r.Command, Source: r.Source}
		}(i, dep)
	}
	wg.Wait()

	// Drop platform-irrelevant deps from the report.
	filtered := statuses[:0]
	for _, s := range statuses {
		if platformAllows(s.Platforms, currentOS()) {
			filtered = append(filtered, s)
		}
	}

	return DependencyReport{
		Statuses: filtered,
		PackageManagers: map[string]bool{
			"homebrew": commandExists("brew"),
			"npm":      commandExists("npm"),
		},
	}
}
