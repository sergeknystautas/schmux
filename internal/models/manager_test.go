package models

import (
	"io"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
)

var testLogger = log.NewWithOptions(io.Discard, log.Options{})

func TestValidateSecrets(t *testing.T) {
	mm := New(&config.Config{}, nil, "", testLogger)

	tests := []struct {
		name    string
		model   detect.Model
		secrets map[string]string
		wantErr bool
	}{
		{
			name:    "no required secrets",
			model:   detect.Model{ID: "test"},
			secrets: nil,
			wantErr: false,
		},
		{
			name:    "all secrets present",
			model:   detect.Model{ID: "test", Runners: map[string]detect.RunnerSpec{"tool": {RequiredSecrets: []string{"API_KEY"}}}},
			secrets: map[string]string{"API_KEY": "sk-123"},
			wantErr: false,
		},
		{
			name:    "missing secret",
			model:   detect.Model{ID: "test", Runners: map[string]detect.RunnerSpec{"tool": {RequiredSecrets: []string{"API_KEY"}}}},
			secrets: map[string]string{},
			wantErr: true,
		},
		{
			name:    "empty string secret",
			model:   detect.Model{ID: "test", Runners: map[string]detect.RunnerSpec{"tool": {RequiredSecrets: []string{"API_KEY"}}}},
			secrets: map[string]string{"API_KEY": ""},
			wantErr: true,
		},
		{
			name:    "whitespace-only secret",
			model:   detect.Model{ID: "test", Runners: map[string]detect.RunnerSpec{"tool": {RequiredSecrets: []string{"API_KEY"}}}},
			secrets: map[string]string{"API_KEY": "   "},
			wantErr: true,
		},
		{
			name:    "nil secrets map with required",
			model:   detect.Model{ID: "test", Runners: map[string]detect.RunnerSpec{"tool": {RequiredSecrets: []string{"KEY"}}}},
			secrets: nil,
			wantErr: true,
		},
		{
			name:    "multiple secrets all present",
			model:   detect.Model{ID: "test", Runners: map[string]detect.RunnerSpec{"tool": {RequiredSecrets: []string{"A", "B"}}}},
			secrets: map[string]string{"A": "v1", "B": "v2"},
			wantErr: false,
		},
		{
			name:    "multiple secrets one missing",
			model:   detect.Model{ID: "test", Runners: map[string]detect.RunnerSpec{"tool": {RequiredSecrets: []string{"A", "B"}}}},
			secrets: map[string]string{"A": "v1"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mm.ValidateSecrets(tt.model, tt.secrets)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSecrets() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIsTargetInUse(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *config.Config
		targetName string
		want       bool
	}{
		{
			name:       "nil config",
			cfg:        nil,
			targetName: "claude-sonnet",
			want:       false,
		},
		{
			name:       "empty target name",
			cfg:        &config.Config{},
			targetName: "",
			want:       false,
		},
		{
			name:       "target not in use",
			cfg:        &config.Config{},
			targetName: "claude-sonnet",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm := New(tt.cfg, nil, "", testLogger)
			got := mm.IsTargetInUse(tt.targetName)
			if got != tt.want {
				t.Errorf("IsTargetInUse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetCatalogStructure(t *testing.T) {
	mm := New(&config.Config{}, []detect.Tool{{Name: "claude", Command: "claude"}}, "", testLogger)

	catalog, err := mm.GetCatalog()
	if err != nil {
		t.Fatalf("GetCatalog() error: %v", err)
	}

	// Top-level runners should have capabilities
	ri, ok := catalog.Runners["claude"]
	if !ok {
		t.Fatal("top-level runners missing claude")
	}
	if !ri.Available {
		t.Error("claude runner should be available")
	}
	want := map[string]bool{"interactive": true, "oneshot": true}
	got := make(map[string]bool, len(ri.Capabilities))
	for _, c := range ri.Capabilities {
		got[c] = true
	}
	for cap := range want {
		if !got[cap] {
			t.Errorf("claude runner missing capability %q, got %v", cap, ri.Capabilities)
		}
	}

	// Models should have runners as string slices
	found := false
	for _, model := range catalog.Models {
		for _, r := range model.Runners {
			if r == "claude" {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatal("no model with claude runner found in catalog")
	}
}

func TestRebuildCatalogThreeSources(t *testing.T) {
	mm := New(&config.Config{}, []detect.Tool{{Name: "claude", Command: "claude"}}, "", testLogger)

	// With no registry and no user models, only default models should be present
	catalog, err := mm.GetCatalog()
	if err != nil {
		t.Fatalf("GetCatalog: %v", err)
	}
	for _, m := range catalog.Models {
		if !m.IsDefault {
			t.Errorf("unexpected non-default model %q with no registry or user models", m.ID)
		}
	}
	if _, ok := mm.FindModel("claude"); !ok {
		t.Error("claude not found in catalog")
	}
}

func TestMergePrecedence(t *testing.T) {
	mm := New(&config.Config{}, []detect.Tool{{Name: "claude", Command: "claude"}}, "", testLogger)

	mm.SetRegistryModels([]detect.Model{{
		ID: "test-model", DisplayName: "Registry Version", Provider: "anthropic",
		Runners: map[string]detect.RunnerSpec{"claude": {ModelValue: "registry-value"}},
	}})
	mm.SetUserModels([]detect.Model{{
		ID: "test-model", DisplayName: "User Version", Provider: "anthropic",
		Runners: map[string]detect.RunnerSpec{"claude": {ModelValue: "user-value"}},
	}})

	model, ok := mm.FindModel("test-model")
	if !ok {
		t.Fatal("model not found")
	}
	spec, _ := model.RunnerFor("claude")
	if spec.ModelValue != "user-value" {
		t.Errorf("expected user-defined to win, got ModelValue=%q", spec.ModelValue)
	}
}

func TestDefaultModelsAlwaysPresent(t *testing.T) {
	mm := New(&config.Config{}, []detect.Tool{
		{Name: "claude", Command: "claude"},
		{Name: "codex", Command: "codex"},
	}, "", testLogger)

	mm.SetRegistryModels([]detect.Model{{
		ID: "some-model", Provider: "anthropic",
		Runners: map[string]detect.RunnerSpec{"claude": {ModelValue: "v"}},
	}})

	for _, defaultID := range []string{"claude", "codex"} {
		if _, ok := mm.FindModel(defaultID); !ok {
			t.Errorf("default model %q not found", defaultID)
		}
	}
}

func TestDefaultModelsNotDuplicated(t *testing.T) {
	mm := New(&config.Config{}, []detect.Tool{{Name: "claude", Command: "claude"}}, "", testLogger)

	catalog, err := mm.GetCatalog()
	if err != nil {
		t.Fatalf("GetCatalog: %v", err)
	}
	seen := map[string]int{}
	for _, m := range catalog.Models {
		seen[m.ID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("model %q appears %d times in catalog", id, count)
		}
	}
}

func TestFindModelWithMigration(t *testing.T) {
	mm := New(&config.Config{}, []detect.Tool{{Name: "claude", Command: "claude"}}, "", testLogger)

	mm.SetRegistryModels([]detect.Model{{
		ID: "claude-opus-4-6", Provider: "anthropic",
		Runners: map[string]detect.RunnerSpec{"claude": {ModelValue: "claude-opus-4-6"}},
	}})

	// Legacy alias should resolve via migration
	model, ok := mm.FindModel("opus")
	if !ok {
		t.Fatal("legacy alias 'opus' should resolve")
	}
	if model.ID != "claude-opus-4-6" {
		t.Errorf("got ID=%q, want claude-opus-4-6", model.ID)
	}
}

func TestEmptyCatalogOnlyDefaults(t *testing.T) {
	mm := New(&config.Config{}, []detect.Tool{{Name: "claude", Command: "claude"}}, "", testLogger)

	// No registry, no user models — should be only defaults
	catalog, err := mm.GetCatalog()
	if err != nil {
		t.Fatalf("GetCatalog: %v", err)
	}
	if len(catalog.Models) == 0 {
		t.Fatal("expected at least default models")
	}
	for _, m := range catalog.Models {
		if !m.IsDefault {
			t.Errorf("non-default model %q found with empty registry", m.ID)
		}
	}
}

func TestResolveTargetToTool(t *testing.T) {
	mm := New(&config.Config{}, []detect.Tool{{Name: "claude", Command: "claude"}}, "", testLogger)
	mm.SetRegistryModels([]detect.Model{{
		ID: "claude-opus-4-6", Provider: "anthropic",
		Runners: map[string]detect.RunnerSpec{"claude": {ModelValue: "claude-opus-4-6"}},
	}})

	tests := []struct {
		target string
		want   string
	}{
		{"claude", "claude"},          // tool name passed through
		{"claude-opus-4-6", "claude"}, // model resolved to first runner
		{"opus", "claude"},            // legacy alias resolved
		{"nonexistent", ""},           // unknown returns empty
	}
	for _, tt := range tests {
		got := mm.ResolveTargetToTool(tt.target)
		if got != tt.want {
			t.Errorf("ResolveTargetToTool(%q) = %q, want %q", tt.target, got, tt.want)
		}
	}
}

func TestConcurrentCatalogAccess(t *testing.T) {
	mm := New(&config.Config{}, []detect.Tool{{Name: "claude", Command: "claude"}}, "", testLogger)

	// Start concurrent readers
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- true }()
			for j := 0; j < 100; j++ {
				mm.FindModel("claude")
				mm.IsModelID("nonexistent")
				mm.GetCatalog()
			}
		}()
	}

	// Concurrent writer — swap registry models repeatedly
	go func() {
		defer func() { done <- true }()
		for j := 0; j < 50; j++ {
			mm.SetRegistryModels([]detect.Model{{
				ID: "test-model", Provider: "anthropic",
				Runners: map[string]detect.RunnerSpec{"claude": {ModelValue: "v"}},
			}})
		}
	}()

	// Wait for all goroutines
	for i := 0; i < 11; i++ {
		<-done
	}
}

func TestMergeEnvMaps(t *testing.T) {
	tests := []struct {
		name      string
		base      map[string]string
		overrides map[string]string
		want      map[string]string
	}{
		{
			name:      "both nil",
			base:      nil,
			overrides: nil,
			want:      nil,
		},
		{
			name:      "base only",
			base:      map[string]string{"A": "1"},
			overrides: nil,
			want:      map[string]string{"A": "1"},
		},
		{
			name:      "overrides only",
			base:      nil,
			overrides: map[string]string{"B": "2"},
			want:      map[string]string{"B": "2"},
		},
		{
			name:      "override wins",
			base:      map[string]string{"A": "1"},
			overrides: map[string]string{"A": "2"},
			want:      map[string]string{"A": "2"},
		},
		{
			name:      "merge both",
			base:      map[string]string{"A": "1"},
			overrides: map[string]string{"B": "2"},
			want:      map[string]string{"A": "1", "B": "2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeEnvMaps(tt.base, tt.overrides)
			if tt.want == nil {
				if got != nil {
					t.Errorf("mergeEnvMaps() = %v, want nil", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("mergeEnvMaps() has %d entries, want %d", len(got), len(tt.want))
				return
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("mergeEnvMaps()[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}
