package dashboard

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/models"
)

func TestValidateModelSecrets(t *testing.T) {
	mm := models.New(&config.Config{}, nil)

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

func TestTargetInUseByNudgenikOrQuickLaunch(t *testing.T) {
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
			mm := models.New(tt.cfg, nil)
			got := mm.IsTargetInUse(tt.targetName)
			if got != tt.want {
				t.Errorf("IsTargetInUse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildTLS(t *testing.T) {
	t.Run("nil when no TLS configured", func(t *testing.T) {
		cfg := &config.Config{}
		result := buildTLS(cfg)
		if result != nil {
			t.Errorf("expected nil TLS, got %+v", result)
		}
	})
}

func TestValidPersonaIDRegex(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"a", true},
		{"abc", true},
		{"my-persona", true},
		{"test-123", true},
		{"a1", true},
		{"1a", true},
		{"0", true},
		{"", false},
		{"-invalid", false},
		{"invalid-", false},
		{"UPPERCASE", false},
		{"has space", false},
		{"has.dot", false},
		{"has_underscore", false},
		{"../traversal", false},
		{"create", true}, // regex allows it; handler rejects it separately
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got := validPersonaID.MatchString(tt.id)
			if got != tt.want {
				t.Errorf("validPersonaID.MatchString(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}
