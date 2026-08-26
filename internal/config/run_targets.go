package config

import (
	"fmt"
	"strings"

	"github.com/sergeknystautas/schmux/internal/types"
)

func validateRunTargets(targets []RunTarget) error {
	seen := make(map[string]struct{})
	for _, target := range targets {
		name := strings.TrimSpace(target.Name)
		if name == "" {
			return fmt.Errorf("%w: run target name is required", ErrInvalidConfig)
		}
		if target.Command == "" {
			return fmt.Errorf("%w: run target command is required for %s", ErrInvalidConfig, name)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("%w: duplicate run target name: %s", ErrInvalidConfig, name)
		}
		if types.IsBuiltinToolName(name) {
			return fmt.Errorf("%w: run target name %s collides with detected tool", ErrInvalidConfig, name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func validateQuickLaunch(presets []QuickLaunch) error {
	seen := make(map[string]struct{})

	for _, preset := range presets {
		name := strings.TrimSpace(preset.Name)
		if name == "" {
			return fmt.Errorf("%w: quick launch name is required", ErrInvalidConfig)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("%w: duplicate quick launch name: %s", ErrInvalidConfig, name)
		}
		hasTarget := strings.TrimSpace(preset.Target) != ""
		hasCommand := strings.TrimSpace(preset.Command) != ""
		if !hasTarget && !hasCommand {
			return fmt.Errorf("%w: quick launch target or command is required for %s", ErrInvalidConfig, name)
		}

		seen[name] = struct{}{}
	}
	return nil
}

// CleanTargets trims entries and drops blanks, preserving order. Returns nil
// when nothing remains.
func CleanTargets(in []string) []string {
	out := make([]string, 0, len(in))
	for _, t := range in {
		if v := strings.TrimSpace(t); v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func validateNudgenikConfig(nudgenik *NudgenikConfig) error {
	if nudgenik == nil {
		return nil
	}
	for _, t := range nudgenik.Targets {
		if strings.TrimSpace(t) == "" {
			return fmt.Errorf("%w: nudgenik targets must be non-empty", ErrInvalidConfig)
		}
	}
	return nil
}

func validateBranchSuggestConfig(bs *BranchSuggestConfig) error {
	if bs == nil {
		return nil
	}
	for _, t := range bs.Targets {
		if strings.TrimSpace(t) == "" {
			return fmt.Errorf("%w: branch_suggest targets must be non-empty", ErrInvalidConfig)
		}
	}
	return nil
}

func validateCompoundConfig(compound *CompoundConfig) error {
	if compound == nil {
		return nil
	}
	targetName := strings.TrimSpace(compound.Target)
	if targetName == "" {
		return nil
	}
	return nil
}
