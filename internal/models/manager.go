// Package models provides a single owner for model catalog, availability,
// enablement, and model→tool resolution. All consumers ask the Manager
// rather than reaching into detect + config independently.
package models

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
)

// Manager owns the model catalog, availability, enablement, and resolution.
// It merges three sources: registry, user-defined, and default_* models.
type Manager struct {
	mu            sync.RWMutex
	config        *config.Config
	detectedTools []detect.Tool
	schmuxDir     string
	// Callback for catalog updates (e.g., to broadcast to WebSocket)
	onCatalogUpdated func()
	// Catalog sources
	registryModels []detect.Model
	userModels     []detect.Model // stored as detect.Model for catalog
	userModelsOrig []UserModel    // stored as UserModel for API returns
	defaultModels  []detect.Model // stored to keep mergedIndex pointers valid
	// Registry metadata (ID -> RegistryModel) for cost/context info
	registryMeta map[string]RegistryModel
	// Merged catalog (rebuilt on any source change)
	merged      []detect.Model
	mergedIndex map[string]*detect.Model
}

// New creates a ModelManager backed by the given config.
func New(cfg *config.Config, detectedTools []detect.Tool, schmuxDir string) *Manager {
	m := &Manager{
		config:         cfg,
		detectedTools:  detectedTools,
		schmuxDir:      schmuxDir,
		registryModels: nil,
		userModels:     nil,
	}
	// Initialize catalog with defaults only
	m.rebuildCatalog()
	return m
}

// rebuildCatalog merges three sources via index (last write wins on ID collision):
// registry, then user-defined, then default_* models.
func (m *Manager) rebuildCatalog() {
	index := make(map[string]*detect.Model)

	// Source 1: registry (lowest priority)
	for i := range m.registryModels {
		index[m.registryModels[i].ID] = &m.registryModels[i]
	}

	// Source 2: user-defined (overrides registry)
	for i := range m.userModels {
		index[m.userModels[i].ID] = &m.userModels[i]
	}

	// Source 3: default_* models (always present, override everything)
	m.defaultModels = detect.GetDefaultModels()
	for i := range m.defaultModels {
		index[m.defaultModels[i].ID] = &m.defaultModels[i]
	}

	// Build sorted list from index (single entry per ID, no duplicates)
	merged := make([]detect.Model, 0, len(index))
	for _, modelPtr := range index {
		merged = append(merged, *modelPtr)
	}
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].ID < merged[j].ID
	})

	m.merged = merged
	m.mergedIndex = index
}

// SetRegistryModels sets the registry models layer and rebuilds the catalog.
func (m *Manager) SetRegistryModels(models []detect.Model) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registryModels = models
	m.rebuildCatalog()
}

// SetRegistryMeta stores the registry metadata (cost, context window, etc.) for catalog display.
func (m *Manager) SetRegistryMeta(meta map[string]RegistryModel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registryMeta = meta
}

// GetRegistryMeta returns the registry metadata for a model, or zero values if not found.
func (m *Manager) GetRegistryMeta(modelID string) RegistryModel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if meta, ok := m.registryMeta[modelID]; ok {
		return meta
	}
	return RegistryModel{}
}

// SetOnCatalogUpdated sets the callback for catalog updates.
func (m *Manager) SetOnCatalogUpdated(callback func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onCatalogUpdated = callback
}

// buildRegistryMeta creates a map from model ID to registry metadata.
func (m *Manager) buildRegistryMeta(registryModels []RegistryModel) {
	m.registryMeta = make(map[string]RegistryModel, len(registryModels))
	for _, rm := range registryModels {
		m.registryMeta[rm.ID] = rm
	}
}

// StartBackgroundFetch begins the async registry fetch loop.
// It loads cache immediately, then fetches fresh data.
// Subsequent fetches happen every 24 hours.
func (m *Manager) StartBackgroundFetch(ctx context.Context) {
	if m.schmuxDir == "" {
		log.Println("models: schmuxDir not set, skipping registry fetch")
		return
	}

	// Load from cache synchronously (fast)
	if data, err := LoadCache(m.schmuxDir); err == nil && data != nil {
		if models, err := ParseRegistry(data, RegistryCutoff()); err == nil {
			m.mu.Lock()
			m.registryModels = BuildDetectModels(models)
			m.buildRegistryMeta(models)
			m.rebuildCatalog()
			m.mu.Unlock()
			log.Printf("models: loaded %d models from cache", len(models))
		}
	}

	m.mu.RLock()
	registryCount := len(m.registryModels)
	m.mu.RUnlock()
	if registryCount == 0 {
		log.Println("models: no cached registry data, catalog contains only default models")
	}

	// Fetch fresh data in background
	go m.fetchLoop(ctx)
}

func (m *Manager) fetchLoop(ctx context.Context) {
	// Fetch immediately on startup
	m.fetchAndUpdate()

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.fetchAndUpdate()
		}
	}
}

func (m *Manager) fetchAndUpdate() {
	// Use HTTP client with timeout
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(RegistryURL)
	if err != nil {
		log.Printf("models: failed to fetch registry: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("models: registry fetch returned status %d", resp.StatusCode)
		return
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("models: failed to read registry response: %v", err)
		return
	}

	models, err := ParseRegistry(data, RegistryCutoff())
	if err != nil {
		log.Printf("models: failed to parse registry: %v", err)
		return
	}

	// Save to cache
	if err := SaveCache(m.schmuxDir, data); err != nil {
		log.Printf("models: failed to save cache: %v", err)
	}

	// Update catalog
	m.mu.Lock()
	m.registryModels = BuildDetectModels(models)
	m.buildRegistryMeta(models)
	m.rebuildCatalog()
	m.mu.Unlock()

	log.Printf("models: updated catalog with %d registry models", len(models))

	// Notify dashboard of catalog change
	m.mu.RLock()
	callback := m.onCatalogUpdated
	m.mu.RUnlock()
	if callback != nil {
		callback()
	}
}

// SetUserModels sets the user models layer and rebuilds the catalog.
func (m *Manager) SetUserModels(models []detect.Model) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.userModels = models
	m.rebuildCatalog()
}

// SetUserModelsOrig stores the original UserModel slice (for API returns).
func (m *Manager) SetUserModelsOrig(models []UserModel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.userModelsOrig = models
}

// LoadUserModels loads user-defined models from disk and updates the catalog.
func (m *Manager) LoadUserModels(path string) error {
	models, err := LoadUserModels(path)
	if err != nil {
		return err
	}
	detectModels := UserModelsToDetect(models)
	m.mu.Lock()
	m.userModels = detectModels
	m.userModelsOrig = models
	m.mu.Unlock()
	m.rebuildCatalog()
	return nil
}

// CatalogResult holds the models and top-level runner info returned by GetCatalog.
type CatalogResult struct {
	Models  []contracts.Model
	Runners map[string]contracts.RunnerInfo
}

// GetCatalog returns all models that have at least one available (detected) runner,
// with full metadata and configuration status, plus top-level runner info.
// This is the single source of truth for the dashboard model list.
func (m *Manager) GetCatalog() (*CatalogResult, error) {
	m.mu.RLock()
	allModels := m.merged
	m.mu.RUnlock()

	detectedTools := m.detectedTools
	detected := make(map[string]bool, len(detectedTools))
	for _, t := range detectedTools {
		detected[t.Name] = true
	}

	// Build top-level runner info (one entry per tool)
	topRunners := make(map[string]contracts.RunnerInfo)
	for _, adapter := range detect.AllAdapters() {
		name := adapter.Name()
		topRunners[name] = contracts.RunnerInfo{
			Available:    detected[name],
			Capabilities: adapter.Capabilities(),
		}
	}

	models := make([]contracts.Model, 0, len(allModels))
	for _, model := range allModels {
		anyConfigured := false
		anyAvailable := false
		runnerNames := make([]string, 0, len(model.Runners))

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
			runnerNames = append(runnerNames, toolName)
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

		// Sort runner names for deterministic output
		sort.Strings(runnerNames)

		// Get registry metadata if available
		m.mu.RLock()
		meta, hasMeta := m.registryMeta[model.ID]
		m.mu.RUnlock()

		contractModel := contracts.Model{
			ID:              model.ID,
			DisplayName:     model.DisplayName,
			Provider:        model.Provider,
			Configured:      anyConfigured,
			Runners:         runnerNames,
			RequiredSecrets: model.FirstRunnerRequiredSecrets(),
			IsDefault:       detect.IsDefaultModel(model.ID),
			IsUserDefined:   isUserDefinedModel(model.ID, m.userModels),
		}

		// Populate registry metadata if available
		if hasMeta {
			contractModel.ContextWindow = meta.ContextWindow
			contractModel.MaxOutput = meta.MaxOutput
			contractModel.CostInputPerMTok = meta.CostInput
			contractModel.CostOutputPerMTok = meta.CostOutput
			contractModel.Reasoning = meta.Reasoning
			contractModel.ReleaseDate = meta.ReleaseDate
		}

		models = append(models, contractModel)
	}
	return &CatalogResult{Models: models, Runners: topRunners}, nil
}

// FindModel looks up a model by ID (or legacy alias).
func (m *Manager) FindModel(id string) (detect.Model, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	model, ok := m.mergedIndex[id]
	if !ok {
		// Fall back to legacy migration
		migrated := detect.MigrateModelID(id)
		model, ok = m.mergedIndex[migrated]
	}
	if !ok {
		return detect.Model{}, false
	}
	return *model, true
}

// IsModelID returns true if the given string is a known model ID.
func (m *Manager) IsModelID(id string) bool {
	_, ok := m.FindModel(id)
	return ok
}

// ResolveTargetToTool returns the tool name for a target.
// If the target is a tool name, returns it directly.
// If it's a model ID, returns the first runner key.
func (m *Manager) ResolveTargetToTool(targetName string) string {
	if detect.IsBuiltinToolName(targetName) {
		return targetName
	}
	model, ok := m.FindModel(targetName)
	if !ok {
		return ""
	}
	return model.FirstRunnerKey()
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
	model, ok := m.FindModel(modelID)
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
	if model, ok := m.FindModel(targetName); ok {
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
		if model, ok := m.FindModel(preset.Target); ok && model.ID == canonicalName {
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
		if err == nil {
			return true, true
		}
		// Model exists in catalog but can't be resolved (e.g., no detected tool).
		// Fall through to check if it's also a builtin tool name.
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

// DetectedToolNames returns just the names of detected tools.
func (m *Manager) DetectedToolNames() []string {
	names := make([]string, len(m.detectedTools))
	for i, t := range m.detectedTools {
		names[i] = t.Name
	}
	return names
}

// GetUserModels returns the user-defined models.
func (m *Manager) GetUserModels() []UserModel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]UserModel, len(m.userModelsOrig))
	copy(out, m.userModelsOrig)
	return out
}

// SaveUserModels saves user-defined models to disk and updates the catalog.
func (m *Manager) SaveUserModels(models []UserModel, path string) error {
	// Validate first
	if err := ValidateUserModels(models, m.DetectedToolNames()); err != nil {
		return err
	}

	// Save to disk
	if err := SaveUserModels(path, models); err != nil {
		return err
	}

	// Update catalog
	detectModels := UserModelsToDetect(models)
	m.mu.Lock()
	m.userModels = detectModels
	m.userModelsOrig = models
	m.mu.Unlock()
	m.rebuildCatalog()
	return nil
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

// isUserDefinedModel checks if a model ID is from the user-defined layer.
func isUserDefinedModel(id string, userModels []detect.Model) bool {
	for _, m := range userModels {
		if m.ID == id {
			return true
		}
	}
	return false
}
