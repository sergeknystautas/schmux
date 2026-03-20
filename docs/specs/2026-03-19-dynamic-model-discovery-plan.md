# Dynamic Model Discovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace hardcoded model catalog with dynamic discovery from models.dev, layered with user-defined models and built-in fallbacks.

**Architecture:** Three-layer catalog (user-defined > registry > built-in) merged in `internal/models/manager.go`, which becomes the single source of truth. Provider profiles map models.dev providers to schmux runners. Registry fetched non-blocking on startup and daily.

**Tech Stack:** Go (backend), React/TypeScript (frontend), models.dev JSON API (external registry)

**Spec:** `docs/specs/2026-03-19-dynamic-model-discovery-design.md`

---

## File Map

### New Files

- `internal/models/registry.go` — fetching, parsing, filtering models.dev data
- `internal/models/registry_test.go` — tests for registry fetch/parse/filter
- `internal/models/profiles.go` — provider profile definitions
- `internal/models/profiles_test.go` — provider profile tests
- `internal/models/userdefined.go` — loading/saving user-defined models
- `internal/models/userdefined_test.go` — user-defined model tests
- `internal/dashboard/handlers_usermodels.go` — CRUD endpoints for user models
- `internal/dashboard/handlers_usermodels_test.go` — user model endpoint tests
- `assets/dashboard/src/routes/config/UserModels.tsx` — user model editor component

### Modified Files

- `internal/detect/models.go` — builtinModels shrinks to fallback + default\_\* models; legacyIDMigrations expanded; static helpers removed
- `internal/detect/models_test.go` — updated for new migrations and removed helpers
- `internal/detect/adapter.go` — no changes (interface stable)
- `internal/detect/adapter_claude.go:21-31` — handle empty ModelValue in InteractiveArgs
- `internal/detect/adapter_codex.go` — handle empty ModelValue
- `internal/detect/adapter_gemini.go` — handle empty ModelValue
- `internal/detect/adapter_opencode.go` — handle empty ModelValue
- `internal/models/manager.go` — rewrite: single source of truth, RWMutex, merge logic, delegate from detect helpers
- `internal/models/manager_test.go` — new tests for merge, hot-swap, stale retention
- `internal/config/secrets.go:17-21,205-272` — provider-keyed storage, migration
- `internal/config/secrets_test.go` — updated for provider-keyed model
- `internal/api/contracts/config.go:51-59` — Model struct gains new fields
- `internal/dashboard/handlers_models.go:115-120` — use provider-keyed secrets
- `internal/dashboard/server.go` — register new user model routes
- `internal/daemon/daemon.go:489-536` — startup sequence, fetch trigger, ticker
- `assets/dashboard/src/routes/SessionDetailPage.tsx:850-1000` — remove branch/status, add context window/pricing
- `assets/dashboard/src/routes/config/ModelCatalog.tsx` — context window display, defaults group
- `docs/api.md` — document new endpoints and model struct changes

---

## Task 1: Provider Profiles

**Files:**

- Create: `internal/models/profiles.go`
- Create: `internal/models/profiles_test.go`

- [ ] **Step 1: Write failing test for provider profile lookup**

```go
// internal/models/profiles_test.go
package models

import "testing"

func TestGetProfile_Anthropic(t *testing.T) {
	p, ok := GetProviderProfile("anthropic")
	if !ok {
		t.Fatal("anthropic profile not found")
	}
	if p.Runner != "claude" {
		t.Errorf("expected runner 'claude', got %q", p.Runner)
	}
	if p.Category != "native" {
		t.Errorf("expected category 'native', got %q", p.Category)
	}
	if p.Endpoint != "" {
		t.Errorf("expected empty endpoint, got %q", p.Endpoint)
	}
}

func TestGetProfile_Moonshotai(t *testing.T) {
	p, ok := GetProviderProfile("moonshotai")
	if !ok {
		t.Fatal("moonshotai profile not found")
	}
	if p.Runner != "claude" {
		t.Errorf("expected runner 'claude', got %q", p.Runner)
	}
	if p.SchmuxProvider != "moonshot" {
		t.Errorf("expected schmux_provider 'moonshot', got %q", p.SchmuxProvider)
	}
	if p.OpencodePrefix != "moonshot" {
		t.Errorf("expected opencode_prefix 'moonshot', got %q", p.OpencodePrefix)
	}
	if p.Endpoint != "https://api.moonshot.ai/anthropic" {
		t.Errorf("wrong endpoint: %q", p.Endpoint)
	}
	if len(p.RequiredSecrets) != 1 || p.RequiredSecrets[0] != "ANTHROPIC_AUTH_TOKEN" {
		t.Errorf("wrong required secrets: %v", p.RequiredSecrets)
	}
}

func TestGetProfile_Unknown(t *testing.T) {
	_, ok := GetProviderProfile("nonexistent")
	if ok {
		t.Error("expected false for unknown provider")
	}
}

func TestGetProfile_AllProviders(t *testing.T) {
	expected := []string{"anthropic", "openai", "google", "moonshotai", "zai", "minimax"}
	for _, name := range expected {
		if _, ok := GetProviderProfile(name); !ok {
			t.Errorf("missing profile for %q", name)
		}
	}
}

func TestCanonicalProvider(t *testing.T) {
	tests := []struct {
		modelsDevProvider string
		want              string
	}{
		{"anthropic", "anthropic"},
		{"moonshotai", "moonshot"},
		{"zai", "zai"},
		{"minimax", "minimax"},
	}
	for _, tt := range tests {
		p, _ := GetProviderProfile(tt.modelsDevProvider)
		got := p.CanonicalProvider()
		if got != tt.want {
			t.Errorf("CanonicalProvider(%q) = %q, want %q", tt.modelsDevProvider, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/models/ -run TestGetProfile -v`
Expected: FAIL — `GetProviderProfile` undefined

- [ ] **Step 3: Implement provider profiles**

```go
// internal/models/profiles.go
package models

// ProviderProfile maps a models.dev provider to schmux runner config.
type ProviderProfile struct {
	Runner          string   // schmux runner name (claude, codex, gemini, opencode)
	Endpoint        string   // API endpoint override (empty = runner's default)
	RequiredSecrets []string // secrets needed for this provider
	SchmuxProvider  string   // internal provider name if different from models.dev name
	OpencodePrefix  string   // prefix for opencode runner (e.g., "zhipu" for zai)
	UsageURL        string   // signup/pricing page
	Category        string   // "native" or "third-party"
}

// CanonicalProvider returns the schmux-internal provider name.
func (p ProviderProfile) CanonicalProvider() string {
	if p.SchmuxProvider != "" {
		return p.SchmuxProvider
	}
	return p.OpencodePrefix // for native providers, opencode prefix == provider name
}

var providerProfiles = map[string]ProviderProfile{
	"anthropic": {
		Runner:         "claude",
		Category:       "native",
		OpencodePrefix: "anthropic",
	},
	"openai": {
		Runner:         "codex",
		Category:       "native",
		OpencodePrefix: "openai",
	},
	"google": {
		Runner:         "gemini",
		Category:       "native",
		OpencodePrefix: "google",
	},
	"moonshotai": {
		Runner:          "claude",
		Endpoint:        "https://api.moonshot.ai/anthropic",
		RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
		SchmuxProvider:  "moonshot",
		OpencodePrefix:  "moonshot",
		UsageURL:        "https://platform.moonshot.ai/console/account",
		Category:        "third-party",
	},
	"zai": {
		Runner:          "claude",
		Endpoint:        "https://api.z.ai/api/anthropic",
		RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
		SchmuxProvider:  "zai",
		OpencodePrefix:  "zhipu",
		UsageURL:        "https://z.ai/manage-apikey/subscription",
		Category:        "third-party",
	},
	"minimax": {
		Runner:          "claude",
		Endpoint:        "https://api.minimax.io/anthropic",
		RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
		OpencodePrefix:  "minimax",
		UsageURL:        "https://platform.minimax.io/user-center/payment/coding-plan",
		Category:        "third-party",
	},
}

// GetProviderProfile returns the profile for a models.dev provider name.
func GetProviderProfile(modelsDevProvider string) (ProviderProfile, bool) {
	p, ok := providerProfiles[modelsDevProvider]
	return p, ok
}

// SupportedProviders returns the list of models.dev provider names we support.
func SupportedProviders() []string {
	out := make([]string, 0, len(providerProfiles))
	for k := range providerProfiles {
		out = append(out, k)
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/models/ -run TestGetProfile -v && go test ./internal/models/ -run TestCanonical -v`
Expected: PASS

- [ ] **Step 5: Commit**

---

## Task 2: Registry Fetch, Parse, and Filter

**Files:**

- Create: `internal/models/registry.go`
- Create: `internal/models/registry_test.go`

- [ ] **Step 1: Write failing test for parsing models.dev JSON**

The test uses a small embedded JSON fixture mimicking models.dev's structure. Tests parsing, provider filtering, and model filtering (tool_call, text output, recency).

```go
// internal/models/registry_test.go
package models

import (
	"testing"
	"time"
)

const testRegistryJSON = `{
	"anthropic": {
		"name": "Anthropic",
		"env": ["ANTHROPIC_API_KEY"],
		"models": {
			"claude-opus-4-6": {
				"id": "claude-opus-4-6",
				"name": "Claude Opus 4.6",
				"tool_call": true,
				"release_date": "2026-02-05",
				"modalities": {"input": ["text", "image"], "output": ["text"]},
				"limit": {"context": 1000000, "output": 128000},
				"cost": {"input": 5, "output": 25},
				"reasoning": true
			},
			"old-model": {
				"id": "old-model",
				"name": "Old Model",
				"tool_call": true,
				"release_date": "2023-01-01",
				"modalities": {"input": ["text"], "output": ["text"]}
			}
		}
	},
	"moonshotai": {
		"name": "Moonshot AI",
		"api": "https://api.moonshot.ai/v1",
		"env": ["MOONSHOT_API_KEY"],
		"models": {
			"kimi-k2-thinking": {
				"id": "kimi-k2-thinking",
				"name": "Kimi K2 Thinking",
				"tool_call": true,
				"release_date": "2025-11-06",
				"modalities": {"input": ["text"], "output": ["text"]},
				"limit": {"context": 262144, "output": 262144},
				"cost": {"input": 0.6, "output": 2.5}
			}
		}
	},
	"unknown_provider": {
		"models": {
			"some-model": {
				"id": "some-model",
				"name": "Some Model",
				"tool_call": true,
				"release_date": "2026-01-01",
				"modalities": {"input": ["text"], "output": ["text"]}
			}
		}
	},
	"anthropic_embed": {
		"models": {
			"embed-model": {
				"id": "embed-model",
				"name": "Embedding",
				"tool_call": false,
				"release_date": "2026-01-01",
				"modalities": {"input": ["text"], "output": ["text"]}
			}
		}
	}
}`

func TestParseRegistry(t *testing.T) {
	models, err := ParseRegistry([]byte(testRegistryJSON), time.Date(2025, 3, 19, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ParseRegistry: %v", err)
	}
	// Should include claude-opus-4-6 (recent, tool_call, text output, known provider)
	// Should include kimi-k2-thinking (recent, tool_call, text output, known provider)
	// Should exclude old-model (too old)
	// Should exclude some-model (unknown provider)
	// Should exclude embed-model (tool_call false)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}

	var claude, kimi *RegistryModel
	for i := range models {
		switch models[i].ID {
		case "claude-opus-4-6":
			claude = &models[i]
		case "kimi-k2-thinking":
			kimi = &models[i]
		}
	}

	if claude == nil {
		t.Fatal("claude-opus-4-6 not found")
	}
	if claude.DisplayName != "Claude Opus 4.6" {
		t.Errorf("wrong display name: %q", claude.DisplayName)
	}
	if claude.Provider != "anthropic" {
		t.Errorf("wrong provider: %q", claude.Provider)
	}
	if claude.ContextWindow != 1000000 {
		t.Errorf("wrong context window: %d", claude.ContextWindow)
	}
	if claude.CostInput != 5 {
		t.Errorf("wrong cost input: %f", claude.CostInput)
	}
	if !claude.Reasoning {
		t.Error("expected reasoning=true")
	}

	if kimi == nil {
		t.Fatal("kimi-k2-thinking not found")
	}
	if kimi.Provider != "moonshotai" {
		t.Errorf("wrong provider: %q", kimi.Provider)
	}
}

func TestParseRegistry_InvalidJSON(t *testing.T) {
	_, err := ParseRegistry([]byte("not json"), time.Now())
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseRegistry_MissingFields(t *testing.T) {
	// Model with no release_date should be skipped
	json := `{"anthropic": {"models": {"m1": {"id": "m1", "name": "M1", "tool_call": true, "modalities": {"output": ["text"]}}}}}`
	models, err := ParseRegistry([]byte(json), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 0 {
		t.Errorf("expected 0 models (missing release_date), got %d", len(models))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/models/ -run TestParseRegistry -v`
Expected: FAIL — `ParseRegistry` undefined

- [ ] **Step 3: Implement registry parsing and filtering**

```go
// internal/models/registry.go
package models

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	registryURL    = "https://models.dev/api.json"
	cacheFileName  = "models-dev.json"
	recencyMonths  = 12
	schemaVersion  = 1
)

// RegistryModel is a model parsed from models.dev with bonus metadata.
type RegistryModel struct {
	ID            string  // models.dev model ID
	DisplayName   string
	Provider      string  // models.dev provider key (e.g., "moonshotai")
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
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	ToolCall    bool              `json:"tool_call"`
	Reasoning   bool              `json:"reasoning"`
	ReleaseDate string            `json:"release_date"`
	Modalities  registryModality  `json:"modalities"`
	Limit       registryLimit     `json:"limit"`
	Cost        registryCost      `json:"cost"`
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

// FetchRegistry fetches models.dev/api.json over HTTP.
func FetchRegistry() ([]byte, error) {
	resp, err := http.Get(registryURL)
	if err != nil {
		return nil, fmt.Errorf("fetch registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch registry: status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

type registryCache struct {
	SchemaVersion int    `json:"schema_version"`
	FetchedAt     string `json:"fetched_at"`
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/models/ -run TestParseRegistry -v`
Expected: PASS

- [ ] **Step 5: Write test for cache round-trip**

```go
func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`{"test": true}`)

	if err := SaveCache(dir, data); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}

	loaded, err := LoadCache(dir)
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if string(loaded) != string(data) {
		t.Errorf("got %q, want %q", loaded, data)
	}
}

func TestLoadCache_Missing(t *testing.T) {
	data, err := LoadCache(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data != nil {
		t.Error("expected nil for missing cache")
	}
}

func TestLoadCache_Corrupt(t *testing.T) {
	dir := t.TempDir()
	cachePath := CachePath(dir)
	os.MkdirAll(filepath.Dir(cachePath), 0755)
	os.WriteFile(cachePath, []byte("not json"), 0644)

	data, err := LoadCache(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data != nil {
		t.Error("expected nil for corrupt cache")
	}
}
```

- [ ] **Step 6: Run tests, verify pass**

Run: `go test ./internal/models/ -run TestCache -v`
Expected: PASS

- [ ] **Step 7: Commit**

---

## Task 3: Convert Registry Models to detect.Model

**Files:**

- Modify: `internal/models/registry.go`
- Modify: `internal/models/registry_test.go`

This task adds a function that converts `RegistryModel` entries into `detect.Model` entries using provider profiles.

- [ ] **Step 1: Write failing test**

```go
func TestBuildDetectModels(t *testing.T) {
	registry := []RegistryModel{
		{
			ID:            "claude-opus-4-6",
			DisplayName:   "Claude Opus 4.6",
			Provider:      "anthropic",
			ContextWindow: 1000000,
			CostInput:     5,
			CostOutput:    25,
			Reasoning:     true,
			ReleaseDate:   "2026-02-05",
		},
		{
			ID:            "kimi-k2-thinking",
			DisplayName:   "Kimi K2 Thinking",
			Provider:      "moonshotai",
			ContextWindow: 262144,
			CostInput:     0.6,
			CostOutput:    2.5,
			ReleaseDate:   "2025-11-06",
		},
	}

	models := BuildDetectModels(registry)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}

	// Claude model
	claude := models[0]
	if claude.Provider != "anthropic" {
		t.Errorf("provider: got %q, want 'anthropic'", claude.Provider)
	}
	if claude.Category != "native" {
		t.Errorf("category: got %q, want 'native'", claude.Category)
	}
	// Should have claude + opencode runners
	claudeRunner, ok := claude.RunnerFor("claude")
	if !ok {
		t.Fatal("missing claude runner")
	}
	if claudeRunner.ModelValue != "claude-opus-4-6" {
		t.Errorf("claude ModelValue: got %q", claudeRunner.ModelValue)
	}
	ocRunner, ok := claude.RunnerFor("opencode")
	if !ok {
		t.Fatal("missing opencode runner")
	}
	if ocRunner.ModelValue != "anthropic/claude-opus-4-6" {
		t.Errorf("opencode ModelValue: got %q", ocRunner.ModelValue)
	}

	// Kimi model
	kimi := models[1]
	if kimi.Provider != "moonshot" {
		t.Errorf("provider: got %q, want 'moonshot'", kimi.Provider)
	}
	if kimi.Category != "third-party" {
		t.Errorf("category: got %q, want 'third-party'", kimi.Category)
	}
	kimiRunner, ok := kimi.RunnerFor("claude")
	if !ok {
		t.Fatal("missing claude runner for kimi")
	}
	if kimiRunner.Endpoint != "https://api.moonshot.ai/anthropic" {
		t.Errorf("endpoint: got %q", kimiRunner.Endpoint)
	}
	if len(kimiRunner.RequiredSecrets) != 1 || kimiRunner.RequiredSecrets[0] != "ANTHROPIC_AUTH_TOKEN" {
		t.Errorf("secrets: got %v", kimiRunner.RequiredSecrets)
	}
	kimiOC, ok := kimi.RunnerFor("opencode")
	if !ok {
		t.Fatal("missing opencode runner for kimi")
	}
	if kimiOC.ModelValue != "moonshot/kimi-k2-thinking" {
		t.Errorf("opencode ModelValue: got %q", kimiOC.ModelValue)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/models/ -run TestBuildDetectModels -v`
Expected: FAIL — `BuildDetectModels` undefined

- [ ] **Step 3: Implement BuildDetectModels**

Add to `internal/models/registry.go`:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/models/ -run TestBuildDetectModels -v`
Expected: PASS

- [ ] **Step 5: Commit**

---

## Task 4: Default Tool Models

**Files:**

- Modify: `internal/detect/models.go`
- Modify: `internal/detect/models_test.go`
- Modify: `internal/detect/adapter_claude.go:21-31`
- Modify: `internal/detect/adapter_codex.go`
- Modify: `internal/detect/adapter_gemini.go`
- Modify: `internal/detect/adapter_opencode.go`

- [ ] **Step 1: Write failing test for default models**

```go
// Add to internal/detect/models_test.go
func TestDefaultModels(t *testing.T) {
	defaults := GetDefaultModels()
	if len(defaults) != 4 {
		t.Fatalf("expected 4 default models, got %d", len(defaults))
	}

	expectedIDs := map[string]string{
		"default_claude":   "claude",
		"default_codex":    "codex",
		"default_gemini":   "gemini",
		"default_opencode": "opencode",
	}

	for _, m := range defaults {
		expectedRunner, ok := expectedIDs[m.ID]
		if !ok {
			t.Errorf("unexpected default model: %s", m.ID)
			continue
		}
		if _, hasRunner := m.RunnerFor(expectedRunner); !hasRunner {
			t.Errorf("%s: missing runner %q", m.ID, expectedRunner)
		}
		spec, _ := m.RunnerFor(expectedRunner)
		if spec.ModelValue != "" {
			t.Errorf("%s: expected empty ModelValue, got %q", m.ID, spec.ModelValue)
		}
		// Should only have one runner
		if len(m.Runners) != 1 {
			t.Errorf("%s: expected 1 runner, got %d", m.ID, len(m.Runners))
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/detect/ -run TestDefaultModels -v`
Expected: FAIL — `GetDefaultModels` undefined

- [ ] **Step 3: Implement default models in detect/models.go**

Add to `internal/detect/models.go`:

```go
// defaultModels are synthetic models that use each runner's built-in default.
// They pass no --model flag, letting the harness use whatever it defaults to.
var defaultModels = []Model{
	{
		ID:          "default_claude",
		DisplayName: "Claude (default)",
		Provider:    "anthropic",
		Category:    "native",
		Runners:     map[string]RunnerSpec{"claude": {ModelValue: ""}},
	},
	{
		ID:          "default_codex",
		DisplayName: "Codex (default)",
		Provider:    "openai",
		Category:    "native",
		Runners:     map[string]RunnerSpec{"codex": {ModelValue: ""}},
	},
	{
		ID:          "default_gemini",
		DisplayName: "Gemini (default)",
		Provider:    "google",
		Category:    "native",
		Runners:     map[string]RunnerSpec{"gemini": {ModelValue: ""}},
	},
	{
		ID:          "default_opencode",
		DisplayName: "OpenCode (default)",
		Provider:    "opencode",
		Category:    "native",
		Runners:     map[string]RunnerSpec{"opencode": {ModelValue: ""}},
	},
}

// GetDefaultModels returns the synthetic default models.
func GetDefaultModels() []Model {
	out := make([]Model, len(defaultModels))
	copy(out, defaultModels)
	return out
}

// IsDefaultModel returns true if the model ID is a default_* model.
func IsDefaultModel(id string) bool {
	for _, m := range defaultModels {
		if m.ID == id {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/detect/ -run TestDefaultModels -v`
Expected: PASS

- [ ] **Step 5: Write failing test for adapter empty ModelValue**

```go
// Add to internal/detect/adapter_claude_test.go (or create it)
func TestClaudeAdapter_InteractiveArgs_EmptyModelValue(t *testing.T) {
	a := &ClaudeAdapter{}
	m := &Model{
		ID: "default_claude",
		Runners: map[string]RunnerSpec{
			"claude": {ModelValue: ""},
		},
	}
	args := a.InteractiveArgs(m, false)
	for _, arg := range args {
		if arg == "--model" {
			t.Error("should not pass --model for empty ModelValue")
		}
	}
}
```

- [ ] **Step 6: Run test to verify behavior — check if it already passes or fails**

Run: `go test ./internal/detect/ -run TestClaudeAdapter_InteractiveArgs_Empty -v`

- [ ] **Step 7: Update each adapter's InteractiveArgs/OneshotArgs/StreamingArgs to skip --model when ModelValue is empty**

In `adapter_claude.go`, the `InteractiveArgs` method (line 21-31) currently does:

```go
if model != nil {
    spec, _ := model.RunnerFor(a.Name())
    args = append(args, "--model", spec.ModelValue)
}
```

Change to:

```go
if model != nil {
    spec, _ := model.RunnerFor(a.Name())
    if spec.ModelValue != "" {
        args = append(args, "--model", spec.ModelValue)
    }
}
```

Apply the same pattern to `OneshotArgs` and `StreamingArgs` in `adapter_claude.go`, and to `InteractiveArgs`/`OneshotArgs` in `adapter_codex.go`, `adapter_gemini.go`, and `adapter_opencode.go`. Each adapter has a similar conditional — find where `ModelValue` or `ModelFlag()` is used and guard with `!= ""`.

- [ ] **Step 8: Run all adapter tests**

Run: `go test ./internal/detect/ -v`
Expected: PASS

- [ ] **Step 9: Commit**

---

## Task 5: Legacy ID Migrations and builtinModels Cleanup

**Files:**

- Modify: `internal/detect/models.go:521-530` — expand legacyIDMigrations
- Modify: `internal/detect/models_test.go`

- [ ] **Step 1: Write failing test for new migrations**

```go
func TestMigrateModelID_NewMigrations(t *testing.T) {
	tests := []struct {
		old, want string
	}{
		{"kimi-thinking", "kimi-k2-thinking"},
		{"minimax-m2.1", "MiniMax-M2.1"},
		{"minimax-2.5", "MiniMax-M2.5"},
		{"minimax-2.7", "MiniMax-M2.7"},
		// Existing chains should resolve transitively
		{"minimax", "MiniMax-M2.1"},
	}
	for _, tt := range tests {
		got := MigrateModelID(tt.old)
		if got != tt.want {
			t.Errorf("MigrateModelID(%q) = %q, want %q", tt.old, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/detect/ -run TestMigrateModelID_New -v`
Expected: FAIL — old IDs not yet in migration map

- [ ] **Step 3: Update legacyIDMigrations**

In `internal/detect/models.go`, replace the `legacyIDMigrations` map (lines 521-530) with:

```go
var legacyIDMigrations = map[string]string{
	"claude-opus":   "claude-opus-4-6",
	"claude-sonnet": "claude-sonnet-4-6",
	"claude-haiku":  "claude-haiku-4-5",
	"opus":          "claude-opus-4-6",
	"sonnet":        "claude-sonnet-4-6",
	"haiku":         "claude-haiku-4-5",
	// Model ID normalization to models.dev IDs
	"kimi-thinking": "kimi-k2-thinking",
	"minimax":       "MiniMax-M2.1",
	"minimax-m2.1":  "MiniMax-M2.1",
	"minimax-2.5":   "MiniMax-M2.5",
	"minimax-2.7":   "MiniMax-M2.7",
}
```

Also update `MigrateModelID` to resolve chains transitively (follow until no more mappings):

```go
func MigrateModelID(id string) string {
	for i := 0; i < 10; i++ { // max depth to prevent infinite loops
		next, ok := legacyIDMigrations[id]
		if !ok {
			return id
		}
		id = next
	}
	return id
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/detect/ -run TestMigrateModelID -v`
Expected: PASS

- [ ] **Step 5: Run full detect test suite**

Run: `go test ./internal/detect/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

---

## Task 6: Provider-Keyed Secrets

**Files:**

- Modify: `internal/config/secrets.go`
- Modify: `internal/config/secrets_test.go`

- [ ] **Step 1: Write failing test for provider-keyed secrets**

```go
func TestProviderKeyedSecrets(t *testing.T) {
	dir := t.TempDir()
	// Write a secrets file with provider-keyed format
	secretsJSON := `{
		"providers": {
			"moonshot": {"ANTHROPIC_AUTH_TOKEN": "sk-moon-123"}
		}
	}`
	os.WriteFile(filepath.Join(dir, "secrets.json"), []byte(secretsJSON), 0644)

	// GetProviderSecrets should find moonshot secrets
	secrets := GetProviderSecrets("moonshot")
	if secrets["ANTHROPIC_AUTH_TOKEN"] != "sk-moon-123" {
		t.Errorf("expected moonshot token, got %v", secrets)
	}
}

func TestSecretsModelToProviderMigration(t *testing.T) {
	dir := t.TempDir()
	// Old format: model-keyed
	secretsJSON := `{
		"models": {
			"kimi-thinking": {"ANTHROPIC_AUTH_TOKEN": "sk-moon-old"}
		}
	}`
	os.WriteFile(filepath.Join(dir, "secrets.json"), []byte(secretsJSON), 0644)

	// After loading, should migrate to provider-keyed
	// kimi-thinking has provider "moonshot"
	secrets := LoadSecretsFile()
	// Provider secrets should be populated
	provSecrets := secrets.Providers["moonshot"]
	if provSecrets["ANTHROPIC_AUTH_TOKEN"] != "sk-moon-old" {
		t.Errorf("expected migrated moonshot token, got %v", provSecrets)
	}
}
```

Note: These tests will need adjustment based on the actual test patterns in `secrets_test.go`. Read the existing test file to match the setup pattern (some tests may use a test helper to override the schmux directory).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestProviderKeyed -v`
Expected: FAIL

- [ ] **Step 3: Implement provider-keyed secrets**

Update `SecretsFile` struct to add a `Providers` field:

```go
type SecretsFile struct {
	Models    ModelSecrets            `json:"models,omitempty"`
	Variants  ModelSecrets            `json:"variants,omitempty"` // deprecated
	Providers map[string]map[string]string `json:"providers,omitempty"`
	Auth      AuthSecrets             `json:"auth,omitempty"`
}
```

Update `LoadSecretsFile()` to migrate model-keyed secrets to provider-keyed. This requires knowing which provider each model belongs to — use `detect.FindModel()` to look up the provider, then group secrets by provider.

Update `GetProviderSecrets(provider)` to read from `Providers` map directly instead of scanning models.

Update `GetEffectiveModelSecrets(model)` to read from `Providers[model.Provider]` and overlay with model-specific secrets.

Update `DeleteProviderSecrets(provider)` to delete from `Providers` map.

Update `SaveModelSecrets` to also update the `Providers` map when the model's provider is known.

- [ ] **Step 4: Run secrets tests**

Run: `go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

---

## Task 7: contracts.Model Expansion and Type Generation

**Files:**

- Modify: `internal/api/contracts/config.go:51-59`
- Regenerate: `assets/dashboard/src/lib/types.generated.ts`

- [ ] **Step 1: Update contracts.Model struct**

Add new fields to `internal/api/contracts/config.go`:

```go
type Model struct {
	ID              string   `json:"id"`
	DisplayName     string   `json:"display_name"`
	Provider        string   `json:"provider"`
	Configured      bool     `json:"configured"`
	Runners         []string `json:"runners"`
	RequiredSecrets []string `json:"required_secrets,omitempty"`
	ContextWindow   int      `json:"context_window,omitempty"`
	MaxOutput       int      `json:"max_output,omitempty"`
	CostInputPerMTok  float64 `json:"cost_input_per_mtok,omitempty"`
	CostOutputPerMTok float64 `json:"cost_output_per_mtok,omitempty"`
	Reasoning       bool     `json:"reasoning,omitempty"`
	ReleaseDate     string   `json:"release_date,omitempty"`
	IsDefault       bool     `json:"is_default,omitempty"`
	IsUserDefined   bool     `json:"is_user_defined,omitempty"`
}
```

- [ ] **Step 2: Regenerate TypeScript types**

Run: `go run ./cmd/gen-types`
Expected: `assets/dashboard/src/lib/types.generated.ts` updated with new fields

- [ ] **Step 3: Build to verify no type errors**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 4: Commit**

---

## Task 8: User-Defined Models

**Files:**

- Create: `internal/models/userdefined.go`
- Create: `internal/models/userdefined_test.go`

- [ ] **Step 1: Write failing test for loading user models**

```go
// internal/models/userdefined_test.go
package models

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUserModels(t *testing.T) {
	dir := t.TempDir()
	modelsJSON := `{
		"models": [
			{
				"id": "my-model",
				"display_name": "My Model",
				"provider": "internal",
				"runner": "claude",
				"endpoint": "https://llm.internal.corp/anthropic",
				"required_secrets": ["ANTHROPIC_AUTH_TOKEN"]
			}
		]
	}`
	os.WriteFile(filepath.Join(dir, "models.json"), []byte(modelsJSON), 0644)

	models, err := LoadUserModels(filepath.Join(dir, "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].ID != "my-model" {
		t.Errorf("wrong ID: %q", models[0].ID)
	}
}

func TestLoadUserModels_Missing(t *testing.T) {
	models, err := LoadUserModels("/nonexistent/models.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 0 {
		t.Error("expected empty list for missing file")
	}
}

func TestValidateUserModels_ReservedPrefix(t *testing.T) {
	models := []UserModel{{ID: "default_claude", Runner: "claude"}}
	err := ValidateUserModels(models, []string{"claude"})
	if err == nil {
		t.Error("expected error for reserved prefix")
	}
}

func TestValidateUserModels_InvalidRunner(t *testing.T) {
	models := []UserModel{{ID: "my-model", Runner: "nonexistent"}}
	err := ValidateUserModels(models, []string{"claude", "codex"})
	if err == nil {
		t.Error("expected error for invalid runner")
	}
}

func TestValidateUserModels_DuplicateID(t *testing.T) {
	models := []UserModel{
		{ID: "my-model", Runner: "claude"},
		{ID: "my-model", Runner: "codex"},
	}
	err := ValidateUserModels(models, []string{"claude", "codex"})
	if err == nil {
		t.Error("expected error for duplicate ID")
	}
}

func TestSaveUserModels(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")

	models := []UserModel{{
		ID:          "test-model",
		DisplayName: "Test",
		Provider:    "custom",
		Runner:      "claude",
	}}

	if err := SaveUserModels(path, models); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadUserModels(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].ID != "test-model" {
		t.Errorf("round-trip failed: %+v", loaded)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/models/ -run TestLoadUserModels -v`
Expected: FAIL

- [ ] **Step 3: Implement user-defined models**

```go
// internal/models/userdefined.go
package models

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/sergeknystautas/schmux/internal/detect"
)

// UserModel is a user-defined model entry.
type UserModel struct {
	ID              string   `json:"id"`
	DisplayName     string   `json:"display_name,omitempty"`
	Provider        string   `json:"provider,omitempty"`
	Runner          string   `json:"runner"`
	Endpoint        string   `json:"endpoint,omitempty"`
	RequiredSecrets []string `json:"required_secrets,omitempty"`
}

type userModelsFile struct {
	Models []UserModel `json:"models"`
}

// LoadUserModels loads user-defined models from a JSON file.
// Returns empty slice if file doesn't exist.
func LoadUserModels(path string) ([]UserModel, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var f userModelsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse user models: %w", err)
	}
	return f.Models, nil
}

// SaveUserModels writes user-defined models to a JSON file.
func SaveUserModels(path string, models []UserModel) error {
	f := userModelsFile{Models: models}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// ValidateUserModels checks user models for validity.
func ValidateUserModels(models []UserModel, detectedTools []string) error {
	toolSet := make(map[string]bool)
	for _, t := range detectedTools {
		toolSet[t] = true
	}

	seen := make(map[string]bool)
	for _, m := range models {
		if m.ID == "" {
			return fmt.Errorf("model ID is required")
		}
		if strings.HasPrefix(m.ID, "default_") {
			return fmt.Errorf("model ID %q uses reserved prefix 'default_'", m.ID)
		}
		if seen[m.ID] {
			return fmt.Errorf("duplicate model ID: %q", m.ID)
		}
		seen[m.ID] = true

		if m.Runner == "" {
			return fmt.Errorf("model %q: runner is required", m.ID)
		}
		if !toolSet[m.Runner] {
			return fmt.Errorf("model %q: unknown runner %q (available: %v)", m.ID, m.Runner, detectedTools)
		}
		if m.Endpoint != "" {
			if _, err := url.ParseRequestURI(m.Endpoint); err != nil {
				return fmt.Errorf("model %q: invalid endpoint URL: %w", m.ID, err)
			}
		}
	}
	return nil
}

// UserModelsToDetect converts user models to detect.Model entries.
func UserModelsToDetect(models []UserModel) []detect.Model {
	var result []detect.Model
	for _, um := range models {
		provider := um.Provider
		if provider == "" {
			provider = "custom"
		}
		displayName := um.DisplayName
		if displayName == "" {
			displayName = um.ID
		}

		runners := map[string]detect.RunnerSpec{
			um.Runner: {
				ModelValue:      um.ID,
				Endpoint:        um.Endpoint,
				RequiredSecrets: um.RequiredSecrets,
			},
			"opencode": {
				ModelValue: provider + "/" + um.ID,
			},
		}

		result = append(result, detect.Model{
			ID:          um.ID,
			DisplayName: displayName,
			Provider:    provider,
			Category:    "third-party",
			Runners:     runners,
		})
	}
	return result
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/models/ -run "TestLoadUserModels|TestValidate|TestSaveUserModels" -v`
Expected: PASS

- [ ] **Step 5: Commit**

---

## Task 9: Manager Rewrite — Three-Layer Merge

**Files:**

- Modify: `internal/models/manager.go` — major rewrite
- Modify: `internal/models/manager_test.go`

This is the core task. The manager becomes the single source of truth, merging user-defined > registry > built-in models.

- [ ] **Step 1: Write failing test for three-layer merge**

```go
func TestManager_ThreeLayerMerge(t *testing.T) {
	// Built-in has model A and B
	// Registry has model B (different display name) and C
	// User has model C (different runner)
	// Expected: A from built-in, B from registry (overrides), C from user (overrides)

	// This test will exercise the merge logic.
	// The exact setup depends on the Manager API — likely:
	// mm := NewManager(cfg, detectedTools)
	// mm.SetBuiltinModels(builtins)
	// mm.SetRegistryModels(registry)
	// mm.SetUserModels(userDefined)
	// catalog := mm.GetCatalog()
	// Verify priorities.
}
```

Note: The exact test structure depends on how the Manager is refactored. The key assertions are:

1. User-defined model overrides registry model with same ID
2. Registry model overrides built-in model with same ID
3. Default models are always present
4. Models are grouped by provider correctly
5. Stale models (old but referenced in config) are retained

- [ ] **Step 2: Implement Manager rewrite**

Key changes to `internal/models/manager.go`:

1. Add `sync.RWMutex` and catalog fields:

   ```go
   type Manager struct {
       mu             sync.RWMutex
       config         *config.Config
       detectedTools  []detect.Tool
       schmuxDir      string
       // Catalog layers
       builtinModels  []detect.Model
       registryModels []detect.Model
       registryMeta   map[string]RegistryModel // ID → metadata (cost, context, etc.)
       userModels     []detect.Model
       // Merged catalog (rebuilt on any layer change)
       merged         []detect.Model
       mergedIndex    map[string]*detect.Model
   }
   ```

2. Add `rebuildCatalog()` — merges three layers with priority, adds default models
3. Refactor `GetCatalog()` — reads from merged catalog under RLock
4. Refactor `FindModel()` — reads from mergedIndex under RLock (replaces delegation to detect package)
5. Refactor `IsModelID()` — reads from mergedIndex under RLock
6. Add `SetRegistryModels()` — called after fetch, triggers rebuildCatalog under Lock
7. Add `LoadUserModels()` — reads from disk, triggers rebuildCatalog

The `rebuildCatalog()` function:

```go
func (m *Manager) rebuildCatalog() {
    index := make(map[string]*detect.Model)
    var merged []detect.Model

    // Layer 1: built-in (lowest priority)
    for i := range m.builtinModels {
        model := m.builtinModels[i]
        index[model.ID] = &model
    }
    // Layer 2: registry (overrides built-in)
    for i := range m.registryModels {
        model := m.registryModels[i]
        index[model.ID] = &model
    }
    // Layer 3: user-defined (overrides everything)
    for i := range m.userModels {
        model := m.userModels[i]
        index[model.ID] = &model
    }
    // Always add default models (can't be overridden)
    for i := range defaultModels {
        model := defaultModels[i]
        index[model.ID] = &model
    }

    // Stale model retention: keep models referenced in enabledModels
    for modelID := range m.config.GetEnabledModels() {
        migrated := detect.MigrateModelID(modelID)
        if _, exists := index[migrated]; !exists {
            // Try to find in built-in as stale
            if model, ok := detect.FindModel(migrated); ok {
                index[migrated] = &model
            }
        }
    }

    for _, model := range index {
        merged = append(merged, *model)
    }

    m.merged = merged
    m.mergedIndex = index
}
```

- [ ] **Step 3: Update all callers of detect.FindModel, detect.IsModelID**

Grep for all callers and redirect to `manager.FindModel()` / `manager.IsModelID()`. Key locations:

- `internal/dashboard/handlers_models.go` — already uses `s.models.FindModel()`
- `internal/dashboard/handlers_spawn.go` — already uses `s.models.IsModel()`
- `internal/config/secrets.go` — uses `detect.GetBuiltinModels()` in `GetProviderSecrets` and `DeleteProviderSecrets`. After Task 6 (provider-keyed secrets), these no longer scan models, so no change needed here.

- [ ] **Step 4: Run all tests**

Run: `go test ./internal/models/ -v && go test ./internal/detect/ -v && go test ./... -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

---

## Task 10: Daemon Integration — Startup and Background Fetch

**Files:**

- Modify: `internal/daemon/daemon.go:489-536`
- Modify: `internal/models/manager.go`

- [ ] **Step 1: Add StartBackgroundFetch to Manager**

```go
// StartBackgroundFetch begins the async registry fetch loop.
// It loads cache immediately, then fetches fresh data.
// Subsequent fetches happen every 24 hours.
func (m *Manager) StartBackgroundFetch(ctx context.Context) {
    // Load from cache synchronously (fast)
    if data, _ := LoadCache(m.schmuxDir); data != nil {
        if models, err := ParseRegistry(data, RegistryCutoff()); err == nil {
            m.mu.Lock()
            m.registryModels = BuildDetectModels(models)
            m.buildRegistryMeta(models)
            m.rebuildCatalog()
            m.mu.Unlock()
        }
    }

    // Fetch fresh data in background
    go m.fetchLoop(ctx)
}

func (m *Manager) fetchLoop(ctx context.Context) {
    // Fetch immediately
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
    data, err := FetchRegistry()
    if err != nil {
        // Log error, keep using cached/built-in
        return
    }

    models, err := ParseRegistry(data, RegistryCutoff())
    if err != nil {
        return
    }

    // Save to cache
    SaveCache(m.schmuxDir, data)

    // Update catalog
    m.mu.Lock()
    m.registryModels = BuildDetectModels(models)
    m.buildRegistryMeta(models)
    m.rebuildCatalog()
    m.mu.Unlock()

    // Notify dashboard of catalog change
    if m.onCatalogUpdated != nil {
        m.onCatalogUpdated()
    }
}
```

- [ ] **Step 2: Wire into daemon startup**

In `internal/daemon/daemon.go`, after `models.New()` (line 499):

```go
mm := models.New(cfg, detectedTargets, schmuxDir)
mm.StartBackgroundFetch(ctx)
```

Also wire the `onCatalogUpdated` callback to broadcast on the dashboard WebSocket.

- [ ] **Step 3: Run quick tests to verify no regressions**

Run: `./test.sh --quick`
Expected: PASS

- [ ] **Step 4: Commit**

---

## Task 11: User Model CRUD Endpoints

**Files:**

- Create: `internal/dashboard/handlers_usermodels.go`
- Create: `internal/dashboard/handlers_usermodels_test.go`
- Modify: `internal/dashboard/server.go` — register routes

- [ ] **Step 1: Write failing test for GET /api/user-models**

```go
func TestHandleGetUserModels(t *testing.T) {
	// Setup test server with model manager
	// GET /api/user-models
	// Assert 200, empty models list
}

func TestHandleSetUserModels_Valid(t *testing.T) {
	// PUT /api/user-models with valid body
	// Assert 200
	// GET /api/user-models
	// Assert the model appears
}

func TestHandleSetUserModels_ReservedPrefix(t *testing.T) {
	// PUT /api/user-models with id "default_claude"
	// Assert 400 with error message
}

func TestHandleSetUserModels_InvalidRunner(t *testing.T) {
	// PUT /api/user-models with runner "nonexistent"
	// Assert 400
}
```

- [ ] **Step 2: Implement handlers**

```go
// internal/dashboard/handlers_usermodels.go
package dashboard

func (s *Server) handleGetUserModels(w http.ResponseWriter, r *http.Request) {
    models := s.models.GetUserModels()
    json.NewEncoder(w).Encode(map[string]any{"models": models})
}

func (s *Server) handleSetUserModels(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Models []models.UserModel `json:"models"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, err.Error(), 400)
        return
    }

    toolNames := s.models.DetectedToolNames()
    if err := models.ValidateUserModels(req.Models, toolNames); err != nil {
        http.Error(w, err.Error(), 400)
        return
    }

    if err := s.models.SaveUserModels(req.Models); err != nil {
        http.Error(w, err.Error(), 500)
        return
    }

    json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```

- [ ] **Step 3: Register routes in server.go**

Add to route registration:

```go
r.Get("/api/user-models", s.handleGetUserModels)
r.Put("/api/user-models", s.handleSetUserModels)
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/dashboard/ -run TestHandleUserModels -v`
Expected: PASS

- [ ] **Step 5: Commit**

---

## Task 12: Session Detail Page UI Changes

**Files:**

- Modify: `assets/dashboard/src/routes/SessionDetailPage.tsx:850-1000`

- [ ] **Step 1: Remove Branch field (lines 862-867)**

Delete the branch metadata-field div. Branch is already shown in the page header.

- [ ] **Step 2: Remove Status field (lines 977-985)**

Delete the status metadata-field div.

- [ ] **Step 3: Add Context Window and Pricing fields below Target**

After the Target field (line 872), add:

```tsx
{
  sessionModel?.context_window ? (
    <div className="metadata-field">
      <span className="metadata-field__label">Context Window</span>
      <span className="metadata-field__value">
        {(sessionModel.context_window / 1000).toFixed(0)}K tokens
      </span>
    </div>
  ) : null;
}
{
  sessionModel?.cost_input_per_mtok || sessionModel?.cost_output_per_mtok ? (
    <div className="metadata-field">
      <span className="metadata-field__label">Pricing</span>
      <span className="metadata-field__value">
        ${sessionModel.cost_input_per_mtok} / ${sessionModel.cost_output_per_mtok} per MTok
      </span>
    </div>
  ) : null;
}
```

This requires looking up the session's model from the config context. Add logic to find the model by matching `sessionData.target` against the models list from `ConfigContext`.

- [ ] **Step 4: Run frontend tests**

Run: `./test.sh --quick`
Expected: PASS (may need to update existing SessionDetailPage tests for removed fields)

- [ ] **Step 5: Commit**

---

## Task 13: Model Catalog UI Changes

**Files:**

- Modify: `assets/dashboard/src/routes/config/ModelCatalog.tsx`

- [ ] **Step 1: Add context window display**

In the model row rendering, add context window after the model name:

```tsx
{
  model.context_window ? (
    <span className="model-context">{(model.context_window / 1000).toFixed(0)}K</span>
  ) : null;
}
```

- [ ] **Step 2: Add "Defaults" group**

Update `groupByProvider` to separate default models (where `is_default === true`) into their own group shown at the top.

- [ ] **Step 3: Run frontend tests**

Run: `./test.sh --quick`
Expected: PASS

- [ ] **Step 4: Commit**

---

## Task 14: User Model Editor UI

**Files:**

- Create: `assets/dashboard/src/routes/config/UserModels.tsx`
- Modify: `assets/dashboard/src/routes/config/` — integrate into settings page

- [ ] **Step 1: Create UserModels component**

Simple form-based editor:

- List of user-defined models with edit/delete buttons
- "Add model" button opens inline form with fields: ID, Display Name, Provider, Runner (dropdown), Endpoint, Required Secrets
- Save button calls `PUT /api/user-models`
- Error display for validation failures

- [ ] **Step 2: Integrate into settings/config page**

Add as a new section in the config page, after the model catalog.

- [ ] **Step 3: Write component test**

Test that:

- Renders empty state
- Add model form appears on button click
- Validation errors are displayed

- [ ] **Step 4: Run frontend tests**

Run: `./test.sh --quick`
Expected: PASS

- [ ] **Step 5: Commit**

---

## Task 15: Manager GetCatalog Populates New Fields

**Files:**

- Modify: `internal/models/manager.go` — `GetCatalog()` method

- [ ] **Step 1: Update GetCatalog to populate bonus fields**

The `GetCatalog()` method builds `contracts.Model` entries. Update it to populate the new fields from `registryMeta`:

```go
func (m *Manager) GetCatalog() CatalogResult {
    m.mu.RLock()
    defer m.mu.RUnlock()

    // ... existing logic to build contracts.Model entries ...

    // For each model, if we have registry metadata, populate bonus fields
    if meta, ok := m.registryMeta[model.ID]; ok {
        cm.ContextWindow = meta.ContextWindow
        cm.MaxOutput = meta.MaxOutput
        cm.CostInputPerMTok = meta.CostInput
        cm.CostOutputPerMTok = meta.CostOutput
        cm.Reasoning = meta.Reasoning
        cm.ReleaseDate = meta.ReleaseDate
    }
    cm.IsDefault = detect.IsDefaultModel(model.ID)
    // IsUserDefined: check if model came from user layer
}
```

- [ ] **Step 2: Write test**

Verify that a model from the registry layer has its bonus fields populated in the catalog response.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/models/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

---

## Task 16: WebSocket catalog_updated Event

**Files:**

- Modify: `internal/dashboard/` — broadcast logic

- [ ] **Step 1: Add catalog_updated broadcast**

When the manager's `onCatalogUpdated` callback fires, broadcast a `catalog_updated` message on the `/ws/dashboard` WebSocket. Follow the existing pattern for session/workspace broadcasts.

- [ ] **Step 2: Add frontend handler**

In the dashboard WebSocket handler (likely in `SessionsContext` or similar), listen for `catalog_updated` and trigger a re-fetch of `/api/config`.

- [ ] **Step 3: Run tests**

Run: `./test.sh --quick`
Expected: PASS

- [ ] **Step 4: Commit**

---

## Task 17: API Documentation and Final Verification

**Files:**

- Modify: `docs/api.md`

- [ ] **Step 1: Update docs/api.md**

Document:

- New `GET /api/user-models` endpoint
- New `PUT /api/user-models` endpoint with request/response schema
- Updated `Model` struct fields in API responses
- `catalog_updated` WebSocket event

- [ ] **Step 2: Run the CI doc check**

Run: `./scripts/check-api-docs.sh`
Expected: PASS

- [ ] **Step 3: Run full test suite**

Run: `./test.sh --quick`
Expected: All tests pass

- [ ] **Step 4: Final commit**

---

## Task Order and Dependencies

```
Task 1 (Provider Profiles) ──────────┐
Task 2 (Registry Fetch/Parse) ───┐   │
Task 3 (Registry → detect.Model) ┘───┤  (Task 3 depends on Task 1 and 2)
Task 4 (Default Models + Adapters) ──┤
Task 5 (Legacy ID Migrations) ───────┼──→ Task 9 (Manager Rewrite) ──→ Task 9b (Shrink builtinModels) ──→ Task 10 (Daemon Integration) ──→ Task 16 (WebSocket)
Task 6 (Provider-Keyed Secrets) ─────┤                                                                                                       │
Task 7 (Contracts + Gen Types) ──────┤                                                                                                       ├──→ Task 17 (Docs + Verify)
Task 8 (User-Defined Models) ────────┘──→ Task 11 (User Model API) ──→ Task 14 (User Model UI) ─────────────────────────────────────────────┤
                                                                                                                                              │
                                         Task 15 (Catalog Bonus Fields) ──→ Task 12 (Session Detail UI) ────────────────────────────────────┤
                                                                           Task 13 (Model Catalog UI) ───────────────────────────────────────┘
```

Tasks 1, 2, 4, 5, 6, 7, 8 can be parallelized. Task 3 depends on Tasks 1 and 2. Task 9 depends on all of 1-8. Task 17 is last.

## Implementation Notes (from review)

These notes apply across tasks — the implementer should reference them:

1. **Import path:** The Go module is `github.com/sergeknystautas/schmux`. All internal imports use this prefix.
2. **Secrets test pattern:** Existing tests in `secrets_test.go` use `setupSecretsHome(t)` to redirect `$HOME`. Match this pattern in Task 6.
3. **Adapter empty ModelValue:** The existing adapters already guard against empty `ModelValue` (e.g., `adapter_claude.go:26` checks `ok && spec.ModelValue != ""`). Task 4's adapter steps should verify existing behavior with regression tests, not implement new guards.
4. **Stale model lookup in `rebuildCatalog`:** Search `m.builtinModels` directly, not `detect.FindModel()`, to avoid circular dependency with the manager.
5. **Manager `New()` signature:** Changes from `New(cfg, detectedTools)` to `New(cfg, detectedTools, schmuxDir)`. The `schmuxDir` comes from the daemon's config directory (same as `~/.schmux` or whatever is configured).
6. **Commit convention:** Use `/commit` per project CLAUDE.md, never `git commit` directly.
7. **Config.go caller:** `internal/config/config.go:588` calls `detect.GetBuiltinModels()` — this must be updated in Task 9 to use the manager or documented as intentional fallback.
8. **enabledModels migration:** At config load time, iterate `enabledModels` keys and run `MigrateModelID()` on each. Add as a step in Task 5 or Task 9.
9. **state.json migration:** Session target references in state.json need `MigrateModelID()` applied at startup. Add as a step in Task 9 or Task 10.
10. **Drop dashscope:** Remove `qwen3-coder-plus` from `builtinModels` as part of Task 9b (shrink builtinModels).
11. **Hot-swap concurrency test:** Task 9 should include a test that runs concurrent `GetCatalog()` reads while `SetRegistryModels()` writes, verifying no panics or partial data.
