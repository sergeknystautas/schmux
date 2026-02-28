package config

import (
	"testing"
)

func TestGetCompoundTarget_FallbackChain(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{
			name: "explicit compound target used",
			cfg: &Config{
				Compound: &CompoundConfig{Target: "compound-model"},
				Nudgenik: &NudgenikConfig{Target: "nudgenik-model"},
			},
			want: "compound-model",
		},
		{
			name: "falls back to nudgenik when compound target empty",
			cfg: &Config{
				Compound: &CompoundConfig{Target: ""},
				Nudgenik: &NudgenikConfig{Target: "nudgenik-model"},
			},
			want: "nudgenik-model",
		},
		{
			name: "falls back to nudgenik when compound nil",
			cfg: &Config{
				Nudgenik: &NudgenikConfig{Target: "nudgenik-model"},
			},
			want: "nudgenik-model",
		},
		{
			name: "returns empty when both nil",
			cfg:  &Config{},
			want: "",
		},
		{
			name: "returns empty when nudgenik also empty",
			cfg: &Config{
				Compound: &CompoundConfig{Target: ""},
				Nudgenik: &NudgenikConfig{Target: ""},
			},
			want: "",
		},
		{
			name: "trims whitespace from compound target",
			cfg: &Config{
				Compound: &CompoundConfig{Target: "  model-x  "},
			},
			want: "model-x",
		},
		{
			name: "whitespace-only compound falls back to nudgenik",
			cfg: &Config{
				Compound: &CompoundConfig{Target: "   "},
				Nudgenik: &NudgenikConfig{Target: "nudge"},
			},
			want: "nudge",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.GetCompoundTarget()
			if got != tt.want {
				t.Errorf("GetCompoundTarget() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetLoreTarget_FallbackChain(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{
			name: "explicit lore target used",
			cfg: &Config{
				Lore:     &LoreConfig{Target: "lore-model"},
				Compound: &CompoundConfig{Target: "compound-model"},
				Nudgenik: &NudgenikConfig{Target: "nudgenik-model"},
			},
			want: "lore-model",
		},
		{
			name: "falls back to compound when lore target empty",
			cfg: &Config{
				Lore:     &LoreConfig{Target: ""},
				Compound: &CompoundConfig{Target: "compound-model"},
				Nudgenik: &NudgenikConfig{Target: "nudgenik-model"},
			},
			want: "compound-model",
		},
		{
			name: "falls back through compound to nudgenik",
			cfg: &Config{
				Lore:     &LoreConfig{Target: ""},
				Compound: &CompoundConfig{Target: ""},
				Nudgenik: &NudgenikConfig{Target: "nudgenik-model"},
			},
			want: "nudgenik-model",
		},
		{
			name: "falls back to nudgenik when lore and compound nil",
			cfg: &Config{
				Nudgenik: &NudgenikConfig{Target: "nudgenik-model"},
			},
			want: "nudgenik-model",
		},
		{
			name: "returns empty when entire chain is nil",
			cfg:  &Config{},
			want: "",
		},
		{
			name: "lore nil falls back to compound",
			cfg: &Config{
				Compound: &CompoundConfig{Target: "compound-model"},
			},
			want: "compound-model",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.GetLoreTarget()
			if got != tt.want {
				t.Errorf("GetLoreTarget() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetRemoteAccessEnabled_BackwardCompat(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name string
		cfg  *Config
		want bool
	}{
		{
			name: "nil RemoteAccess defaults to false",
			cfg:  &Config{},
			want: false,
		},
		{
			name: "empty RemoteAccess defaults to false",
			cfg:  &Config{RemoteAccess: &RemoteAccessConfig{}},
			want: false,
		},
		{
			name: "Enabled true",
			cfg:  &Config{RemoteAccess: &RemoteAccessConfig{Enabled: &trueVal}},
			want: true,
		},
		{
			name: "Enabled false",
			cfg:  &Config{RemoteAccess: &RemoteAccessConfig{Enabled: &falseVal}},
			want: false,
		},
		{
			name: "deprecated Disabled true inverts to enabled=false",
			cfg:  &Config{RemoteAccess: &RemoteAccessConfig{Disabled: &trueVal}},
			want: false,
		},
		{
			name: "deprecated Disabled false inverts to enabled=true",
			cfg:  &Config{RemoteAccess: &RemoteAccessConfig{Disabled: &falseVal}},
			want: true,
		},
		{
			name: "Enabled takes precedence over Disabled",
			cfg:  &Config{RemoteAccess: &RemoteAccessConfig{Enabled: &trueVal, Disabled: &trueVal}},
			want: true,
		},
		{
			name: "Enabled false takes precedence over Disabled false",
			cfg:  &Config{RemoteAccess: &RemoteAccessConfig{Enabled: &falseVal, Disabled: &falseVal}},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.GetRemoteAccessEnabled()
			if got != tt.want {
				t.Errorf("GetRemoteAccessEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
