package config

import (
	"fmt"
	"strings"

	"github.com/sergeknystautas/schmux/internal/detect"
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
		if detect.IsBuiltinToolName(name) {
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

func validateNudgenikConfig(nudgenik *NudgenikConfig) error {
	if nudgenik == nil {
		return nil
	}
	targetName := strings.TrimSpace(nudgenik.Target)
	if targetName == "" {
		return nil
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
