package config

import (
	"strings"
	"testing"
)

func TestValidateRunTargets(t *testing.T) {
	tests := []struct {
		name         string
		targets      []RunTarget
		wantErr      bool
		wantContains string
	}{
		{
			name: "valid command target",
			targets: []RunTarget{
				{Name: "my-script", Command: "bash run.sh"},
			},
			wantErr: false,
		},
		{
			name: "empty name",
			targets: []RunTarget{
				{Name: "", Command: "echo hi"},
			},
			wantErr:      true,
			wantContains: "name is required",
		},
		{
			name: "empty command",
			targets: []RunTarget{
				{Name: "my-agent", Command: ""},
			},
			wantErr:      true,
			wantContains: "command is required",
		},
		{
			name: "duplicate names",
			targets: []RunTarget{
				{Name: "agent", Command: "echo a"},
				{Name: "agent", Command: "echo b"},
			},
			wantErr:      true,
			wantContains: "duplicate run target name",
		},
		{
			name: "name collides with builtin tool",
			targets: []RunTarget{
				{Name: "claude", Command: "echo hi"},
			},
			wantErr:      true,
			wantContains: "collides with detected tool",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRunTargets(tt.targets)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantContains)
				}
				if !strings.Contains(err.Error(), tt.wantContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantContains)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidateQuickLaunch(t *testing.T) {
	prompt := "do something"
	tests := []struct {
		name         string
		presets      []QuickLaunch
		wantErr      bool
		wantContains string
	}{
		{
			name: "valid quick launch",
			presets: []QuickLaunch{
				{Name: "preset", Target: "claude", Prompt: &prompt},
			},
			wantErr: false,
		},
		{
			name: "valid quick launch with command only",
			presets: []QuickLaunch{
				{Name: "Run website", Command: "npm run dev"},
			},
			wantErr: false,
		},
		{
			name: "empty name",
			presets: []QuickLaunch{
				{Name: "", Target: "claude", Prompt: &prompt},
			},
			wantErr:      true,
			wantContains: "name is required",
		},
		{
			name: "duplicate names",
			presets: []QuickLaunch{
				{Name: "preset", Target: "claude", Prompt: &prompt},
				{Name: "preset", Target: "codex", Prompt: &prompt},
			},
			wantErr:      true,
			wantContains: "duplicate quick launch name",
		},
		{
			name: "no target or command",
			presets: []QuickLaunch{
				{Name: "preset", Target: "", Prompt: &prompt},
			},
			wantErr:      true,
			wantContains: "target or command is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateQuickLaunch(tt.presets)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantContains)
				}
				if !strings.Contains(err.Error(), tt.wantContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantContains)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}
