package dashboard

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/detect"
)

func TestValidateModelSecrets(t *testing.T) {
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
			model:   detect.Model{ID: "test", RequiredSecrets: []string{"API_KEY"}},
			secrets: map[string]string{"API_KEY": "sk-123"},
			wantErr: false,
		},
		{
			name:    "missing secret",
			model:   detect.Model{ID: "test", RequiredSecrets: []string{"API_KEY"}},
			secrets: map[string]string{},
			wantErr: true,
		},
		{
			name:    "empty string secret",
			model:   detect.Model{ID: "test", RequiredSecrets: []string{"API_KEY"}},
			secrets: map[string]string{"API_KEY": ""},
			wantErr: true,
		},
		{
			name:    "whitespace-only secret",
			model:   detect.Model{ID: "test", RequiredSecrets: []string{"API_KEY"}},
			secrets: map[string]string{"API_KEY": "   "},
			wantErr: true,
		},
		{
			name:    "nil secrets map with required",
			model:   detect.Model{ID: "test", RequiredSecrets: []string{"KEY"}},
			secrets: nil,
			wantErr: true,
		},
		{
			name:    "multiple secrets all present",
			model:   detect.Model{ID: "test", RequiredSecrets: []string{"A", "B"}},
			secrets: map[string]string{"A": "v1", "B": "v2"},
			wantErr: false,
		},
		{
			name:    "multiple secrets one missing",
			model:   detect.Model{ID: "test", RequiredSecrets: []string{"A", "B"}},
			secrets: map[string]string{"A": "v1"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateModelSecrets(tt.model, tt.secrets)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateModelSecrets() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
