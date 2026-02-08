package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// configState tracks the last known state of a workspace's config file
type configState struct {
	mtime   time.Time
	existed bool
}

// LoadRepoConfig reads the .schmux/config.json file from a workspace directory.
// Returns the config and any error (returns nil, nil for missing files; returns nil, error for read/parse failures).
func LoadRepoConfig(workspacePath string) (*contracts.RepoConfig, error) {
	configPath := filepath.Join(workspacePath, ".schmux", "config.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist - not an error, just no config
			return nil, nil
		}
		// Other read errors (permissions, IO issues) should surface
		return nil, fmt.Errorf("failed to read %s: %w", configPath, err)
	}

	var repoConfig contracts.RepoConfig
	if err := json.Unmarshal(data, &repoConfig); err != nil {
		// Invalid JSON - return error so caller can log it
		return nil, fmt.Errorf("failed to parse %s: %w", configPath, err)
	}

	return &repoConfig, nil
}

// RefreshWorkspaceConfig refreshes the cached workspace config for a single workspace.
// Only logs when the config file changes (by mtime).
func (m *Manager) RefreshWorkspaceConfig(w state.Workspace) {
	// Skip remote workspaces - their config is on the remote host
	if w.IsRemoteWorkspace() {
		return
	}

	configPath := filepath.Join(w.Path, ".schmux", "config.json")

	// Check if file has changed since last read
	var currentMtime time.Time
	var fileExists bool
	if info, err := os.Stat(configPath); err == nil {
		currentMtime = info.ModTime()
		fileExists = true
	} else if !os.IsNotExist(err) {
		// Log unexpected stat errors (permissions, IO issues) but don't evict cache
		fmt.Printf("[workspace] warning: unexpected stat error for %s: %v\n", configPath, err)
		return
	}

	m.configStatesMu.Lock()
	lastState, hasLastState := m.configStates[w.Path]
	fileChanged := !hasLastState || lastState.mtime != currentMtime || lastState.existed != fileExists
	if fileChanged {
		m.configStates[w.Path] = configState{mtime: currentMtime, existed: fileExists}
	}
	m.configStatesMu.Unlock()

	// If file hasn't changed, skip processing entirely
	if !fileChanged {
		return
	}

	repoCfg, err := LoadRepoConfig(w.Path)

	// Log on change: error or success
	if err != nil {
		fmt.Printf("[workspace] warning: %v\n", err)
		return
	}
	if repoCfg != nil {
		fmt.Printf("[workspace] loaded config from %s\n", configPath)
	}

	validQuickLaunch := validateWorkspaceQuickLaunch(configPath, repoCfg, m.config)
	if repoCfg == nil || len(validQuickLaunch) == 0 {
		m.workspaceConfigsMu.Lock()
		delete(m.workspaceConfigs, w.ID)
		m.workspaceConfigsMu.Unlock()
		return
	}

	m.workspaceConfigsMu.Lock()
	m.workspaceConfigs[w.ID] = &contracts.RepoConfig{QuickLaunch: validQuickLaunch}
	m.workspaceConfigsMu.Unlock()
}

// GetWorkspaceConfig returns the cached workspace config for the given workspace ID.
func (m *Manager) GetWorkspaceConfig(workspaceID string) *contracts.RepoConfig {
	m.workspaceConfigsMu.RLock()
	cfg := m.workspaceConfigs[workspaceID]
	m.workspaceConfigsMu.RUnlock()
	if cfg == nil {
		return nil
	}
	copyCfg := &contracts.RepoConfig{QuickLaunch: make([]contracts.QuickLaunch, len(cfg.QuickLaunch))}
	copy(copyCfg.QuickLaunch, cfg.QuickLaunch)
	return copyCfg
}

func validateWorkspaceQuickLaunch(configPath string, repoCfg *contracts.RepoConfig, cfg *config.Config) []contracts.QuickLaunch {
	if repoCfg == nil {
		return nil
	}
	presets := repoCfg.QuickLaunch
	if len(presets) == 0 {
		return nil
	}
	valid := make([]contracts.QuickLaunch, 0, len(presets))
	seen := make(map[string]bool)
	detected := cfg.GetDetectedRunTargets()

	for _, preset := range presets {
		name := strings.TrimSpace(preset.Name)
		if name == "" {
			fmt.Printf("[workspace] parse error: %s: quick_launch entry missing name\n", configPath)
			continue
		}
		if seen[name] {
			fmt.Printf("[workspace] parse error: %s: quick_launch %q is duplicated\n", configPath, name)
			continue
		}
		command := strings.TrimSpace(preset.Command)
		target := strings.TrimSpace(preset.Target)
		hasCommand := command != ""
		hasTarget := target != ""
		if hasCommand == hasTarget {
			fmt.Printf("[workspace] parse error: %s: quick_launch %q must set either command or target\n", configPath, name)
			continue
		}
		if hasCommand {
			if preset.Prompt != nil && strings.TrimSpace(*preset.Prompt) != "" {
				fmt.Printf("[workspace] parse error: %s: quick_launch %q cannot include prompt for command\n", configPath, name)
				continue
			}
			preset.Name = name
			preset.Command = command
			preset.Target = ""
			preset.Prompt = nil
			valid = append(valid, preset)
			seen[name] = true
			continue
		}

		promptable, found := config.IsTargetPromptable(cfg, detected, target)
		if !found {
			fmt.Printf("[workspace] parse error: %s: quick_launch %q target not found: %s\n", configPath, name, target)
			continue
		}
		prompt := ""
		if preset.Prompt != nil {
			prompt = strings.TrimSpace(*preset.Prompt)
		}
		if promptable && prompt == "" {
			fmt.Printf("[workspace] parse error: %s: quick_launch %q requires prompt\n", configPath, name)
			continue
		}
		if !promptable && prompt != "" {
			fmt.Printf("[workspace] parse error: %s: quick_launch %q cannot include prompt for command target\n", configPath, name)
			continue
		}
		preset.Name = name
		preset.Command = ""
		preset.Target = target
		if preset.Prompt != nil && prompt == "" {
			preset.Prompt = nil
		}
		valid = append(valid, preset)
		seen[name] = true
	}
	return valid
}
