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
		if source != RunTargetSourceUser && source != RunTargetSourceDetected {
			return fmt.Errorf("%w: run target %s has invalid source %q", ErrInvalidConfig, name, source)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("%w: duplicate run target name: %s", ErrInvalidConfig, name)
		}
		if source == RunTargetSourceUser {
			if detect.IsBuiltinToolName(name) {
				return fmt.Errorf("%w: run target name %s collides with detected tool", ErrInvalidConfig, name)
			}
			if detect.IsVariantName(name) {
				return fmt.Errorf("%w: run target name %s collides with variant", ErrInvalidConfig, name)
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
		seen[name] = struct{}{}
	}
	return nil
}

func validateVariantConfigs(variants []VariantConfig) error {
	seen := make(map[string]struct{})
	for _, v := range variants {
		name := strings.TrimSpace(v.Name)
		if name == "" {
			return fmt.Errorf("%w: variant name is required", ErrInvalidConfig)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("%w: duplicate variant config: %s", ErrInvalidConfig, name)
		}
		if !detect.IsVariantName(name) {
			return fmt.Errorf("%w: unknown variant: %s", ErrInvalidConfig, name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func validateQuickLaunch(presets []QuickLaunch, targets []RunTarget, variants []VariantConfig) error {
	seen := make(map[string]struct{})
	variantEnabled := make(map[string]bool)
	for _, v := range variants {
		if v.Name == "" {
			return fmt.Errorf("%w: variant name is required", ErrInvalidConfig)
		}
		if !detect.IsVariantName(v.Name) {
			return fmt.Errorf("%w: unknown variant: %s", ErrInvalidConfig, v.Name)
		}
		enabled := true
		if v.Enabled != nil {
			enabled = *v.Enabled
		}
		variantEnabled[v.Name] = enabled
	}

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

		promptable, ok := quickLaunchTargetPromptable(targetName, targets, variantEnabled)
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

func validateNudgenikConfig(nudgenik *NudgenikConfig, targets []RunTarget, variants []VariantConfig) error {
	if nudgenik == nil {
		return nil
	}
	targetName := strings.TrimSpace(nudgenik.Target)
	if targetName == "" {
		return nil
	}

	variantEnabled := make(map[string]bool)
	for _, v := range variants {
		enabled := true
		if v.Enabled != nil {
			enabled = *v.Enabled
		}
		variantEnabled[v.Name] = enabled
	}

	promptable, ok := quickLaunchTargetPromptable(targetName, targets, variantEnabled)
	if !ok {
		return fmt.Errorf("%w: nudgenik target not found: %s", ErrInvalidConfig, targetName)
	}
	if !promptable {
		return fmt.Errorf("%w: nudgenik target %s must be promptable", ErrInvalidConfig, targetName)
	}
	return nil
}

func validateQuickLaunchTargets(presets []QuickLaunch, targets []RunTarget, variants []VariantConfig) error {
	variantEnabled := make(map[string]bool)
	for _, v := range variants {
		enabled := true
		if v.Enabled != nil {
			enabled = *v.Enabled
		}
		variantEnabled[v.Name] = enabled
	}

	for _, preset := range presets {
		name := strings.TrimSpace(preset.Name)
		targetName := strings.TrimSpace(preset.Target)
		if name == "" || targetName == "" {
			continue
		}
		if _, ok := quickLaunchTargetPromptable(targetName, targets, variantEnabled); !ok {
			return fmt.Errorf("%w: quick launch target not found: %s", ErrInvalidConfig, targetName)
		}
	}
	return nil
}

func validateRunTargetDependencies(targets []RunTarget, variants []VariantConfig, quickLaunch []QuickLaunch, nudgenik *NudgenikConfig) error {
	if err := validateQuickLaunchTargets(quickLaunch, targets, variants); err != nil {
		return err
	}
	if err := validateNudgenikConfig(nudgenik, targets, variants); err != nil {
		return err
	}
	return nil
}

func quickLaunchTargetPromptable(targetName string, targets []RunTarget, variantEnabled map[string]bool) (bool, bool) {
	if detect.IsVariantName(targetName) {
		if enabled, ok := variantEnabled[targetName]; ok && !enabled {
			return false, false
		}
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
