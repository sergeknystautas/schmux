package models

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
)

func TestValidateSecrets(t *testing.T) {
	mm := New(&config.Config{}, nil)

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
			mm := New(tt.cfg, nil)
			got := mm.IsTargetInUse(tt.targetName)
			if got != tt.want {
				t.Errorf("IsTargetInUse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetCatalogIncludesCapabilities(t *testing.T) {
	// Create a manager with claude as a detected tool
	mm := New(&config.Config{}, []detect.Tool{{Name: "claude", Command: "claude"}})

	catalog, err := mm.GetCatalog()
	if err != nil {
		t.Fatalf("GetCatalog() error: %v", err)
	}

	// Find any model that has a claude runner
	found := false
	for _, model := range catalog {
		if ri, ok := model.Runners["claude"]; ok {
			found = true
			if len(ri.Capabilities) == 0 {
				t.Errorf("model %s: claude runner has empty Capabilities", model.ID)
			}
			// Claude adapter should report interactive, oneshot, streaming
			want := map[string]bool{"interactive": true, "oneshot": true, "streaming": true}
			got := make(map[string]bool, len(ri.Capabilities))
			for _, c := range ri.Capabilities {
				got[c] = true
			}
			for cap := range want {
				if !got[cap] {
					t.Errorf("model %s: claude runner missing capability %q, got %v", model.ID, cap, ri.Capabilities)
				}
			}
			break // one model is enough to verify
		}
	}
	if !found {
		t.Fatal("no model with a claude runner found in catalog")
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
