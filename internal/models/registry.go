//go:build !nomodelregistry

package models

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/detect"
)

const (
	RegistryURL   = "https://models.dev/api.json"
	cacheFileName = "models-dev.json"
	recencyMonths = 12
	schemaVersion = 1
)

// RegistryModel is a model parsed from models.dev with bonus metadata.
type RegistryModel struct {
	ID            string // models.dev model ID
	DisplayName   string
	Provider      string // models.dev provider key (e.g., "moonshotai")
	ContextWindow int
	MaxOutput     int
	CostInput     float64 // $/million tokens
	CostOutput    float64
	Reasoning     bool
	ReleaseDate   string
}

// registryJSON mirrors models.dev/api.json structure for parsing.
type registryJSON map[string]registryProvider

type registryProvider struct {
	Name   string                       `json:"name"`
	API    string                       `json:"api"`
	Env    []string                     `json:"env"`
	Models map[string]registryModelJSON `json:"models"`
}

type registryModelJSON struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	ToolCall    bool             `json:"tool_call"`
	Reasoning   bool             `json:"reasoning"`
	ReleaseDate string           `json:"release_date"`
	Modalities  registryModality `json:"modalities"`
	Limit       registryLimit    `json:"limit"`
	Cost        registryCost     `json:"cost"`
}

type registryModality struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

type registryLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

type registryCost struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

// ParseRegistry parses models.dev JSON and returns filtered models.
// cutoff is the date before which models are considered too old.
func ParseRegistry(data []byte, cutoff time.Time) ([]RegistryModel, error) {
	var reg registryJSON
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}

	cutoffStr := cutoff.Format("2006-01-02")
	var result []RegistryModel

	for providerKey, provider := range reg {
		if _, ok := GetProviderProfile(providerKey); !ok {
			continue // skip unsupported providers
		}

		for _, m := range provider.Models {
			if !m.ToolCall {
				continue
			}
			if !hasTextOutput(m.Modalities) {
				continue
			}
			if m.ReleaseDate == "" || m.ReleaseDate < cutoffStr {
				continue
			}

			result = append(result, RegistryModel{
				ID:            m.ID,
				DisplayName:   m.Name,
				Provider:      providerKey,
				ContextWindow: m.Limit.Context,
				MaxOutput:     m.Limit.Output,
				CostInput:     m.Cost.Input,
				CostOutput:    m.Cost.Output,
				Reasoning:     m.Reasoning,
				ReleaseDate:   m.ReleaseDate,
			})
		}
	}

	result = deduplicateModels(result)
	return result, nil
}

func hasTextOutput(m registryModality) bool {
	for _, o := range m.Output {
		if o == "text" {
			return true
		}
	}
	return false
}

type registryCache struct {
	SchemaVersion int             `json:"schema_version"`
	FetchedAt     string          `json:"fetched_at"`
	Data          json.RawMessage `json:"data"`
}

// CachePath returns the path to the registry cache file.
func CachePath(schmuxDir string) string {
	return filepath.Join(schmuxDir, "cache", cacheFileName)
}

// SaveCache writes registry data to cache file.
func SaveCache(schmuxDir string, data []byte) error {
	cachePath := CachePath(schmuxDir)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		return err
	}

	cache := registryCache{
		SchemaVersion: schemaVersion,
		FetchedAt:     time.Now().UTC().Format(time.RFC3339),
		Data:          data,
	}
	encoded, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	return os.WriteFile(cachePath, encoded, 0644)
}

// LoadCache reads registry data from cache file.
// Returns nil, nil if cache is missing or corrupt.
func LoadCache(schmuxDir string) ([]byte, error) {
	cachePath := CachePath(schmuxDir)
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, nil // missing cache is not an error
	}

	var cache registryCache
	if err := json.Unmarshal(raw, &cache); err != nil {
		return nil, nil // corrupt cache is not an error
	}
	if cache.SchemaVersion != schemaVersion {
		return nil, nil // wrong version, treat as missing
	}
	return cache.Data, nil
}

// RegistryCutoff returns the cutoff date for filtering (12 months before now).
func RegistryCutoff() time.Time {
	return time.Now().AddDate(0, -recencyMonths, 0)
}

// NormalizeProvider normalizes a provider string for lookup.
// Handles "anthropic" -> "anthropic", etc.
func NormalizeProvider(provider string) string {
	// First check if it's a known models.dev provider
	if _, ok := GetProviderProfile(provider); ok {
		return provider
	}
	// Try to find by canonical name
	provider = strings.ToLower(provider)
	for k, p := range providerProfiles {
		if strings.ToLower(p.SchmuxProvider) == provider || strings.ToLower(p.OpencodePrefix) == provider {
			return k
		}
	}
	return provider
}

// BuildDetectModels converts registry models to detect.Model using provider profiles.
func BuildDetectModels(registry []RegistryModel) []detect.Model {
	var result []detect.Model
	for _, rm := range registry {
		profile, ok := GetProviderProfile(rm.Provider)
		if !ok {
			continue
		}

		runners := map[string]detect.RunnerSpec{
			profile.Runner: {
				ModelValue:      rm.ID,
				Endpoint:        profile.Endpoint,
				RequiredSecrets: profile.RequiredSecrets,
			},
			"opencode": {
				ModelValue: profile.OpencodePrefix + "/" + rm.ID,
			},
		}

		result = append(result, detect.Model{
			ID:          rm.ID,
			DisplayName: rm.DisplayName,
			Provider:    profile.CanonicalProvider(),
			UsageURL:    profile.UsageURL,
			Category:    profile.Category,
			Runners:     runners,
		})
	}
	return result
}

// deduplicateModels removes alias/dated duplicates from registry results.
// Two rules:
//  1. Skip IDs matching provider SkipIDPatterns (e.g., openai's -chat-latest)
//  2. Skip dated variants when a shorter alias exists (e.g., claude-opus-4-1-20250805
//     is deduped because claude-opus-4-1 exists). Only matches when the suffix is a
//     date (8+ digits), so claude-opus-4-1 is NOT deduped by claude-opus-4 existing.
//     Also handles the -0 convention (claude-opus-4-20250514 deduped because
//     claude-opus-4-0 exists).
//  3. Skip -latest IDs when a dated variant exists (e.g., claude-3-5-haiku-latest
//     deduped because claude-3-5-haiku-20241022 exists).
func deduplicateModels(models []RegistryModel) []RegistryModel {
	// First pass: skip provider-specific patterns
	var filtered []RegistryModel
	for _, m := range models {
		profile, _ := GetProviderProfile(m.Provider)
		skip := false
		for _, pattern := range profile.SkipIDPatterns {
			if strings.HasSuffix(m.ID, pattern) {
				skip = true
				break
			}
		}
		// Skip models with "(latest)" in display name — these are floating aliases
		if !skip && strings.Contains(m.DisplayName, "(latest)") {
			skip = true
		}

		if !skip {
			filtered = append(filtered, m)
		}
	}

	// Build ID set for prefix checks
	idSet := make(map[string]bool, len(filtered))
	for _, m := range filtered {
		idSet[m.ID] = true
	}

	// Second pass: skip dated variants and -latest aliases
	var out []RegistryModel
	for _, m := range filtered {
		skip := false

		// Rule 2: skip dated variants when the alias exists.
		// Only triggers when the suffix after the dash is a date (8+ digits).
		// This prevents "claude-opus-4-1" from being deduped by "claude-opus-4".
		for i := len(m.ID) - 1; i > 0; i-- {
			if m.ID[i] == '-' {
				suffix := m.ID[i+1:]
				if len(suffix) >= 8 && isAllDigits(suffix) {
					prefix := m.ID[:i]
					if idSet[prefix] || idSet[prefix+"-0"] {
						skip = true
						break
					}
				}
			}
		}

		// Rule 3: skip -latest IDs when a dated variant exists.
		if !skip && strings.HasSuffix(m.ID, "-latest") {
			base := strings.TrimSuffix(m.ID, "-latest")
			for otherID := range idSet {
				if otherID != m.ID && strings.HasPrefix(otherID, base+"-") {
					skip = true
					break
				}
			}
		}

		if !skip {
			out = append(out, m)
		}
	}
	return out
}

// IsAvailable reports whether the model registry module is included in this build.
func IsAvailable() bool { return true }

func isAllDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
