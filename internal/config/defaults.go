package config

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

//go:embed defaults
var defaultsFS embed.FS

func loadBuildDefaults() (map[string]json.RawMessage, error) {
	data, err := defaultsFS.ReadFile("defaults/build_defaults.json")
	if err != nil {
		return nil, nil // no build defaults embedded, not an error
	}
	var defaults map[string]json.RawMessage
	if err := json.Unmarshal(data, &defaults); err != nil {
		return nil, fmt.Errorf("invalid build_defaults.json: %w", err)
	}
	return defaults, nil
}

// applyBuildDefaults overlays embedded build defaults onto a Config struct.
// Build defaults win over Go zero-value defaults but are overridden by user config.
func applyBuildDefaults(cfg *Config) error {
	defaults, err := loadBuildDefaults()
	if err != nil {
		return err
	}
	return overlayDefaults(cfg, defaults)
}

// overlayDefaults overlays a map of JSON values onto a Config struct.
// Each key in defaults corresponds to a top-level JSON field of Config.
func overlayDefaults(cfg *Config, defaults map[string]json.RawMessage) error {
	if defaults == nil {
		return nil
	}

	// Serialize the config to JSON, overlay the defaults, then deserialize back.
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config for defaults overlay: %w", err)
	}

	var cfgMap map[string]json.RawMessage
	if err := json.Unmarshal(cfgJSON, &cfgMap); err != nil {
		return fmt.Errorf("unmarshal config map for defaults overlay: %w", err)
	}

	// Overlay: for each default key, set it on the config map.
	for key, val := range defaults {
		cfgMap[key] = val
	}

	merged, err := json.Marshal(cfgMap)
	if err != nil {
		return fmt.Errorf("marshal merged config: %w", err)
	}

	// Preserve unexported fields by unmarshaling onto the existing struct.
	if err := json.Unmarshal(merged, cfg); err != nil {
		return fmt.Errorf("unmarshal merged config: %w", err)
	}

	return nil
}

// resolveConfigTemplates replaces template variables in serialized config JSON.
// Currently supports ${USER} → current OS user.
func resolveConfigTemplates(configJSON []byte) []byte {
	s := string(configJSON)
	s = strings.ReplaceAll(s, "${USER}", os.Getenv("USER"))
	return []byte(s)
}
