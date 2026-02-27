package config

import (
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/detect"
)

func TestValidateRunTargets_SourceValidation(t *testing.T) {
	tests := []struct {
		name         string
		targets      []RunTarget
		wantErr      bool
		wantContains string
	}{
		{
			name: "invalid source",
			targets: []RunTarget{
				{Name: "agent", Type: RunTargetTypePromptable, Command: "echo hi", Source: "bogus"},
			},
			wantErr:      true,
			wantContains: "invalid source",
		},
		{
			name: "user source that collides with builtin tool",
			targets: []RunTarget{
				{Name: "claude", Type: RunTargetTypePromptable, Command: "echo hi", Source: RunTargetSourceUser},
			},
			wantErr:      true,
			wantContains: "collides with detected tool",
		},
		{
			name: "detected source for non-builtin tool",
			targets: []RunTarget{
				{Name: "my-custom-tool", Type: RunTargetTypePromptable, Command: "echo hi", Source: RunTargetSourceDetected},
			},
			wantErr:      true,
			wantContains: "not a supported tool",
		},
		{
			name: "detected source must be promptable",
			targets: []RunTarget{
				{Name: "claude", Type: RunTargetTypeCommand, Command: "claude", Source: RunTargetSourceDetected},
			},
			wantErr:      true,
			wantContains: "must be promptable",
		},
		{
			name: "valid detected source",
			targets: []RunTarget{
				{Name: "claude", Type: RunTargetTypePromptable, Command: "claude", Source: RunTargetSourceDetected},
			},
			wantErr: false,
		},
		{
			name: "valid user source with non-builtin name",
			targets: []RunTarget{
				{Name: "my-agent", Type: RunTargetTypePromptable, Command: "echo hi", Source: RunTargetSourceUser},
			},
			wantErr: false,
		},
		{
			name: "empty source defaults to user (valid)",
			targets: []RunTarget{
				{Name: "my-agent", Type: RunTargetTypePromptable, Command: "echo hi"},
			},
			wantErr: false,
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

func TestValidateRunTargets_ModelSource(t *testing.T) {
	models := detect.GetBuiltinModels()
	if len(models) == 0 {
		t.Skip("no builtin models available")
	}
	validModelID := models[0].ID

	tests := []struct {
		name         string
		targets      []RunTarget
		wantErr      bool
		wantContains string
	}{
		{
			name: "model source with invalid model ID",
			targets: []RunTarget{
				{Name: "not-a-real-model", Type: RunTargetTypePromptable, Command: "echo hi", Source: RunTargetSourceModel},
			},
			wantErr:      true,
			wantContains: "not a valid model ID",
		},
		{
			name: "model source with valid model ID",
			targets: []RunTarget{
				{Name: validModelID, Type: RunTargetTypePromptable, Command: "model-cmd", Source: RunTargetSourceModel},
			},
			wantErr: false,
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

func TestValidateQuickLaunch_CommandTargetWithPrompt(t *testing.T) {
	prompt := "do something"
	tests := []struct {
		name         string
		targets      []RunTarget
		presets      []QuickLaunch
		wantErr      bool
		wantContains string
	}{
		{
			name: "command target with prompt fails",
			targets: []RunTarget{
				{Name: "script", Type: RunTargetTypeCommand, Command: "bash run.sh"},
			},
			presets: []QuickLaunch{
				{Name: "preset", Target: "script", Prompt: &prompt},
			},
			wantErr:      true,
			wantContains: "cannot include prompt for command target",
		},
		{
			name: "command target without prompt succeeds",
			targets: []RunTarget{
				{Name: "script", Type: RunTargetTypeCommand, Command: "bash run.sh"},
			},
			presets: []QuickLaunch{
				{Name: "preset", Target: "script"},
			},
			wantErr: false,
		},
		{
			name: "builtin tool target resolves as promptable",
			targets: []RunTarget{
				{Name: "claude", Type: RunTargetTypePromptable, Command: "claude", Source: RunTargetSourceDetected},
			},
			presets: []QuickLaunch{
				{Name: "claude-task", Target: "claude", Prompt: &prompt},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateQuickLaunch(tt.presets, tt.targets)
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

func TestValidateRunTargetDependencies(t *testing.T) {
	prompt := "check things"
	tests := []struct {
		name         string
		targets      []RunTarget
		quickLaunch  []QuickLaunch
		nudgenik     *NudgenikConfig
		compound     *CompoundConfig
		wantErr      bool
		wantContains string
	}{
		{
			name: "nudgenik references nonexistent target",
			targets: []RunTarget{
				{Name: "agent", Type: RunTargetTypePromptable, Command: "echo"},
			},
			nudgenik:     &NudgenikConfig{Target: "missing"},
			wantErr:      true,
			wantContains: "nudgenik target not found",
		},
		{
			name: "nudgenik references non-promptable target",
			targets: []RunTarget{
				{Name: "script", Type: RunTargetTypeCommand, Command: "bash"},
			},
			nudgenik:     &NudgenikConfig{Target: "script"},
			wantErr:      true,
			wantContains: "must be promptable",
		},
		{
			name: "compound references nonexistent target",
			targets: []RunTarget{
				{Name: "agent", Type: RunTargetTypePromptable, Command: "echo"},
			},
			compound:     &CompoundConfig{Target: "missing"},
			wantErr:      true,
			wantContains: "compound target not found",
		},
		{
			name: "compound references non-promptable target",
			targets: []RunTarget{
				{Name: "script", Type: RunTargetTypeCommand, Command: "bash"},
			},
			compound:     &CompoundConfig{Target: "script"},
			wantErr:      true,
			wantContains: "must be promptable",
		},
		{
			name: "quick launch references nonexistent target in dependencies",
			targets: []RunTarget{
				{Name: "agent", Type: RunTargetTypePromptable, Command: "echo"},
			},
			quickLaunch: []QuickLaunch{
				{Name: "preset", Target: "deleted-target", Prompt: &prompt},
			},
			wantErr:      true,
			wantContains: "target not found",
		},
		{
			name: "all valid dependencies pass",
			targets: []RunTarget{
				{Name: "agent", Type: RunTargetTypePromptable, Command: "echo"},
			},
			quickLaunch: []QuickLaunch{
				{Name: "preset", Target: "agent", Prompt: &prompt},
			},
			nudgenik: &NudgenikConfig{Target: "agent"},
			compound: &CompoundConfig{Target: "agent"},
			wantErr:  false,
		},
		{
			name:    "nil nudgenik and compound pass",
			targets: []RunTarget{{Name: "a", Type: RunTargetTypePromptable, Command: "c"}},
			wantErr: false,
		},
		{
			name: "empty nudgenik target passes",
			targets: []RunTarget{
				{Name: "agent", Type: RunTargetTypePromptable, Command: "echo"},
			},
			nudgenik: &NudgenikConfig{Target: ""},
			wantErr:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRunTargetDependencies(tt.targets, tt.quickLaunch, tt.nudgenik, tt.compound)
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

func TestSplitRunTargets(t *testing.T) {
	targets := []RunTarget{
		{Name: "user1", Source: RunTargetSourceUser},
		{Name: "detected1", Source: RunTargetSourceDetected},
		{Name: "model1", Source: RunTargetSourceModel},
		{Name: "default-source"}, // empty source defaults to user
	}
	user, detected := splitRunTargets(targets)
	if len(user) != 3 {
		t.Errorf("expected 3 user targets (user + model + default), got %d", len(user))
	}
	if len(detected) != 1 {
		t.Errorf("expected 1 detected target, got %d", len(detected))
	}
	if detected[0].Name != "detected1" {
		t.Errorf("detected target should be 'detected1', got %q", detected[0].Name)
	}
}

func TestNormalizeRunTargets(t *testing.T) {
	targets := []RunTarget{
		{Name: "a", Source: ""},
		{Name: "b", Source: RunTargetSourceDetected},
	}
	normalizeRunTargets(targets)
	if targets[0].Source != RunTargetSourceUser {
		t.Errorf("empty source should be normalized to %q, got %q", RunTargetSourceUser, targets[0].Source)
	}
	if targets[1].Source != RunTargetSourceDetected {
		t.Errorf("non-empty source should be preserved, got %q", targets[1].Source)
	}
}
