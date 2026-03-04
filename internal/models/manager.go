// Package models provides a single owner for model catalog, availability,
// enablement, and model→tool resolution. All consumers ask the Manager
// rather than reaching into detect + config independently.
package models

import (
	"fmt"
	"strings"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
)

// Manager owns the model catalog, availability, enablement, and resolution.
type Manager struct {
	config        *config.Config
	detectedTools []detect.Tool
}

// New creates a ModelManager backed by the given config.
func New(cfg *config.Config, detectedTools []detect.Tool) *Manager {
	return &Manager{config: cfg, detectedTools: detectedTools}
}

// GetCatalog returns all models that have at least one available (detected) runner,
// with full metadata and configuration status. This is the single source of truth
// for the dashboard model list.
func (m *Manager) GetCatalog() ([]contracts.Model, error) {
	allModels := detect.GetBuiltinModels()
	detectedTools := m.detectedTools
	detected := make(map[string]bool, len(detectedTools))
	for _, t := range detectedTools {
		detected[t.Name] = true
	}

	resp := make([]contracts.Model, 0, len(allModels))
	for _, model := range allModels {
		runners := make(map[string]contracts.RunnerInfo, len(model.Runners))
		anyConfigured := false
		anyAvailable := false

		for toolName, spec := range model.Runners {
			available := detected[toolName]
			configured := true // no secrets needed = configured
			if len(spec.RequiredSecrets) > 0 {
				configured = false
				secrets, err := config.GetEffectiveModelSecrets(model)
				if err == nil {
					configured = true
					for _, key := range spec.RequiredSecrets {
						if strings.TrimSpace(secrets[key]) == "" {
							configured = false
							break
						}
					}
				}
			}
			var capabilities []string
			if adapter := detect.GetAdapter(toolName); adapter != nil {
				capabilities = adapter.Capabilities()
			}
			runners[toolName] = contracts.RunnerInfo{
				Available:       available,
				Configured:      configured,
				RequiredSecrets: spec.RequiredSecrets,
				Capabilities:    capabilities,
			}
			if available && configured {
				anyConfigured = true
			}
			if available {
				anyAvailable = true
			}
		}

		// Only include models that have at least one available runner
		if !anyAvailable {
			continue
		}

		preferredTool := m.config.PreferredTool(model.ID)

		resp = append(resp, contracts.Model{
			ID:            model.ID,
			DisplayName:   model.DisplayName,
			Provider:      model.Provider,
			Category:      model.Category,
			UsageURL:      model.UsageURL,
			Configured:    anyConfigured,
			Runners:       runners,
			PreferredTool: preferredTool,
		})
	}
	return resp, nil
}

// FindModel looks up a model by ID (or legacy alias). Delegates to detect.FindModel.
func (m *Manager) FindModel(id string) (detect.Model, bool) {
	return detect.FindModel(id)
}

// IsModelID returns true if the given string is a known model ID. Delegates to detect.IsModelID.
func (m *Manager) IsModelID(id string) bool {
	return detect.IsModelID(id)
}

// ResolvedModel holds everything needed to spawn a session or run a oneshot
// with a specific model. Returned by ResolveModel.
type ResolvedModel struct {
	Model    detect.Model
	ToolName string
	Command  string
	Env      map[string]string
}

// ResolveModel resolves a model ID to a tool, command, and environment.
// This is the unified resolution logic previously duplicated across
// session/manager.go and oneshot/oneshot.go.
func (m *Manager) ResolveModel(modelID string) (*ResolvedModel, error) {
	model, ok := detect.FindModel(modelID)
	if !ok {
		return nil, fmt.Errorf("model not found: %s", modelID)
	}

	toolName := m.ResolveToolForModel(model)
	if toolName == "" {
		return nil, fmt.Errorf("no available runner for model %s", model.ID)
	}

	spec, _ := model.RunnerFor(toolName)

	// Verify the tool is detected and get its command
	toolCommand := ""
	for _, t := range m.detectedTools {
		if t.Name == toolName {
			toolCommand = t.Command
			break
		}
	}
	if toolCommand == "" {
		return nil, fmt.Errorf("model %s requires tool %s which is not available", model.ID, toolName)
	}

	// Load secrets and verify required ones are present
	secrets, err := config.GetEffectiveModelSecrets(model)
	if err != nil {
		return nil, fmt.Errorf("failed to load secrets for model %s: %w", model.ID, err)
	}
	for _, key := range spec.RequiredSecrets {
		if strings.TrimSpace(secrets[key]) == "" {
			return nil, fmt.Errorf("model %s requires secret %s for tool %s", model.ID, key, toolName)
		}
	}

	// Build env using the adapter
	adapter := detect.GetAdapter(toolName)
	var env map[string]string
	if adapter != nil {
		env = mergeEnvMaps(adapter.BuildRunnerEnv(spec), secrets)
	} else {
		env = secrets
	}

	return &ResolvedModel{
		Model:    model,
		ToolName: toolName,
		Command:  toolCommand,
		Env:      env,
	}, nil
}

// ResolveToolForModel picks which tool to use for a model.
// Checks user preference first, falls back to first detected runner.
func (m *Manager) ResolveToolForModel(model detect.Model) string {
	// 1. Check user preference
	if preferred := m.config.PreferredTool(model.ID); preferred != "" {
		if _, ok := model.RunnerFor(preferred); ok {
			return preferred
		}
	}

	// 2. Fall back to first detected runner
	detectedTools := m.detectedTools
	detected := make(map[string]bool, len(detectedTools))
	for _, t := range detectedTools {
		detected[t.Name] = true
	}

	for _, toolName := range detect.SortedRunnerKeys(model.Runners) {
		if detected[toolName] {
			return toolName
		}
	}
	return ""
}

// IsConfigured returns true if the model has at least one runner
// whose required secrets are all present.
func (m *Manager) IsConfigured(model detect.Model) (bool, error) {
	for _, spec := range model.Runners {
		if len(spec.RequiredSecrets) == 0 {
			return true, nil // No secrets needed for at least one runner
		}
	}
	// Check if any runner has its secrets configured
	secrets, err := config.GetEffectiveModelSecrets(model)
	if err != nil {
		return false, err
	}
	for _, spec := range model.Runners {
		allPresent := true
		for _, key := range spec.RequiredSecrets {
			if strings.TrimSpace(secrets[key]) == "" {
				allPresent = false
				break
			}
		}
		if allPresent {
			return true, nil
		}
	}
	return false, nil
}

// ValidateSecrets checks that all required secrets for a model's runners are present.
func (m *Manager) ValidateSecrets(model detect.Model, secrets map[string]string) error {
	for _, spec := range model.Runners {
		for _, key := range spec.RequiredSecrets {
			val := strings.TrimSpace(secrets[key])
			if val == "" {
				return fmt.Errorf("missing required secret %s", key)
			}
		}
	}
	return nil
}

// GetEnabledModels returns the enabled models map from config.
func (m *Manager) GetEnabledModels() map[string]string {
	return m.config.GetEnabledModels()
}

// IsTargetInUse returns true if the target (by name or model ID) is referenced
// by nudgenik or quick launch configuration.
func (m *Manager) IsTargetInUse(targetName string) bool {
	if m.config == nil || targetName == "" {
		return false
	}

	// Normalize to canonical model ID if targetName is a model or alias
	canonicalName := targetName
	if model, ok := detect.FindModel(targetName); ok {
		canonicalName = model.ID
	}

	if m.config.GetNudgenikTarget() == canonicalName {
		return true
	}
	for _, preset := range m.config.GetQuickLaunch() {
		if preset.Target == canonicalName {
			return true
		}
		// Also check if preset.Target is an alias that resolves to this model
		if model, ok := detect.FindModel(preset.Target); ok && model.ID == canonicalName {
			return true
		}
	}
	return false
}

// IsModel returns whether the named target is a model (or detected tool) that
// accepts prompts, and whether it exists at all. Models and detected tools are
// promptable; user-defined command targets are not.
func (m *Manager) IsModel(name string) (promptable bool, found bool) {
	if m.IsModelID(name) {
		_, err := m.ResolveModel(name)
		return true, err == nil
	}
	if detect.IsBuiltinToolName(name) {
		return true, true
	}
	if _, ok := m.config.GetRunTarget(name); ok {
		return false, true
	}
	return false, false
}

// GetDetectedTools returns the detected tools passed at construction time.
func (m *Manager) GetDetectedTools() []detect.Tool {
	return m.detectedTools
}

// mergeEnvMaps merges two env maps, with overrides taking precedence.
func mergeEnvMaps(base, overrides map[string]string) map[string]string {
	if base == nil && overrides == nil {
		return nil
	}
	out := make(map[string]string, len(base)+len(overrides))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overrides {
		out[k] = v
	}
	return out
}
