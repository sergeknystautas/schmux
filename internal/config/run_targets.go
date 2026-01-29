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
		if target.Type != RunTargetTypePromptable && target.Type != RunTargetTypeCommand {
			return fmt.Errorf("%w: run target %s has invalid type %q", ErrInvalidConfig, name, target.Type)
		}
		source := target.Source
		if source == "" {
			source = RunTargetSourceUser
		}
		if source != RunTargetSourceUser && source != RunTargetSourceDetected && source != RunTargetSourceModel {
			return fmt.Errorf("%w: run target %s has invalid source %q", ErrInvalidConfig, name, source)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("%w: duplicate run target name: %s", ErrInvalidConfig, name)
		}
		if source == RunTargetSourceUser {
			if detect.IsBuiltinToolName(name) {
				return fmt.Errorf("%w: run target name %s collides with detected tool", ErrInvalidConfig, name)
			}
		}
		if source == RunTargetSourceDetected {
			if !detect.IsBuiltinToolName(name) {
				return fmt.Errorf("%w: detected run target %s is not a supported tool", ErrInvalidConfig, name)
			}
			if target.Type != RunTargetTypePromptable {
				return fmt.Errorf("%w: detected run target %s must be promptable", ErrInvalidConfig, name)
			}
		}
		if source == RunTargetSourceModel {
			if !detect.IsModelID(name) {
				return fmt.Errorf("%w: run target name %s is not a valid model ID", ErrInvalidConfig, name)
			}
		}
		seen[name] = struct{}{}
	}
	return nil
}

// IsTargetPromptable returns whether the named target is promptable and whether it exists.
func IsTargetPromptable(cfg *Config, detected []RunTarget, name string) (bool, bool) {
	// Check if it's a model ID or alias
	model, ok := detect.FindModel(name)
	if ok {
		// Check if the model's base tool is detected
		for _, target := range detected {
			if target.Name == model.BaseTool && target.Source == RunTargetSourceDetected {
				return true, true
			}
		}
		// Model exists but base tool not detected
		return true, false
	}
	if detect.IsBuiltinToolName(name) {
		for _, target := range detected {
			if target.Name == name {
				return true, true
			}
		}
		return true, false
	}
	if target, found := cfg.GetRunTarget(name); found {
		return target.Type == RunTargetTypePromptable, true
	}
	return false, false
}

func validateQuickLaunch(presets []QuickLaunch, targets []RunTarget) error {
	seen := make(map[string]struct{})

	for _, preset := range presets {
		name := strings.TrimSpace(preset.Name)
		if name == "" {
			return fmt.Errorf("%w: quick launch name is required", ErrInvalidConfig)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("%w: duplicate quick launch name: %s", ErrInvalidConfig, name)
		}
		targetName := strings.TrimSpace(preset.Target)
		if targetName == "" {
			return fmt.Errorf("%w: quick launch target is required for %s", ErrInvalidConfig, name)
		}

		promptable, ok := quickLaunchTargetPromptable(targetName, targets)
		if !ok {
			return fmt.Errorf("%w: quick launch target not found: %s", ErrInvalidConfig, targetName)
		}

		prompt := ""
		if preset.Prompt != nil {
			prompt = strings.TrimSpace(*preset.Prompt)
		}
		if promptable {
			if prompt == "" {
				return fmt.Errorf("%w: quick launch %s requires prompt", ErrInvalidConfig, name)
			}
		} else if prompt != "" {
			return fmt.Errorf("%w: quick launch %s cannot include prompt for command target", ErrInvalidConfig, name)
		}

		seen[name] = struct{}{}
	}
	return nil
}

func validateNudgenikConfig(nudgenik *NudgenikConfig, targets []RunTarget) error {
	if nudgenik == nil {
		return nil
	}
	targetName := strings.TrimSpace(nudgenik.Target)
	if targetName == "" {
		return nil
	}

	promptable, ok := quickLaunchTargetPromptable(targetName, targets)
	if !ok {
		return fmt.Errorf("%w: nudgenik target not found: %s", ErrInvalidConfig, targetName)
	}
	if !promptable {
		return fmt.Errorf("%w: nudgenik target %s must be promptable", ErrInvalidConfig, targetName)
	}
	return nil
}

func validateQuickLaunchTargets(presets []QuickLaunch, targets []RunTarget) error {
	for _, preset := range presets {
		name := strings.TrimSpace(preset.Name)
		targetName := strings.TrimSpace(preset.Target)
		if name == "" || targetName == "" {
			continue
		}
		if _, ok := quickLaunchTargetPromptable(targetName, targets); !ok {
			return fmt.Errorf("%w: quick launch target not found: %s", ErrInvalidConfig, targetName)
		}
	}
	return nil
}

func validateRunTargetDependencies(targets []RunTarget, quickLaunch []QuickLaunch, nudgenik *NudgenikConfig) error {
	if err := validateQuickLaunchTargets(quickLaunch, targets); err != nil {
		return err
	}
	if err := validateNudgenikConfig(nudgenik, targets); err != nil {
		return err
	}
	return nil
}

func quickLaunchTargetPromptable(targetName string, targets []RunTarget) (bool, bool) {
	if detect.IsModelID(targetName) {
		return true, true
	}
	if detect.IsBuiltinToolName(targetName) {
		return true, true
	}
	for _, target := range targets {
		if target.Name == targetName {
			return target.Type == RunTargetTypePromptable, true
		}
	}
	return false, false
}

func normalizeRunTargets(targets []RunTarget) {
	for i := range targets {
		if targets[i].Source == "" {
			targets[i].Source = RunTargetSourceUser
		}
	}
}

func splitRunTargets(targets []RunTarget) (user []RunTarget, detected []RunTarget) {
	for _, target := range targets {
		source := target.Source
		if source == "" {
			source = RunTargetSourceUser
		}
		if source == RunTargetSourceDetected {
			detected = append(detected, target)
		} else {
			user = append(user, target)
		}
	}
	return user, detected
}

// MergeDetectedRunTargets replaces detected run targets with the latest detected tools,
// preserving user-defined run targets.
func MergeDetectedRunTargets(existing []RunTarget, detectedTools []detect.Tool) []RunTarget {
	user, _ := splitRunTargets(existing)
	merged := make([]RunTarget, 0, len(user)+len(detectedTools))
	merged = append(merged, user...)
	for _, tool := range detectedTools {
		merged = append(merged, RunTarget{
			Name:    tool.Name,
			Type:    RunTargetTypePromptable,
			Command: tool.Command,
			Source:  RunTargetSourceDetected,
		})
	}
	return merged
}
