package config

import (
	"sort"

	"github.com/sergeknystautas/schmux/internal/detect"
)

// GetVariantConfigs returns the variant configs.
func (c *Config) GetVariantConfigs() []VariantConfig {
	return c.Variants
}

// GetMergedVariants returns the built-in variants merged with config overrides.
func (c *Config) GetMergedVariants() []detect.Variant {
	overrides := c.variantOverrides()
	var out []detect.Variant
	for _, v := range detect.GetBuiltinVariants() {
		if cfg, ok := overrides[v.Name]; ok {
			if cfg.Enabled != nil && !*cfg.Enabled {
				continue
			}
			v.Env = mergeEnvOverrides(v.Env, cfg.Env)
		}
		out = append(out, v)
	}
	return out
}

// GetAvailableVariants returns merged variants whose base tool is detected.
func (c *Config) GetAvailableVariants(detected []detect.Tool) []detect.Variant {
	overrides := c.variantOverrides()
	available := detect.GetAvailableVariants(detected)
	out := make([]detect.Variant, 0, len(available))
	for _, v := range available {
		if cfg, ok := overrides[v.Name]; ok {
			if cfg.Enabled != nil && !*cfg.Enabled {
				continue
			}
			v.Env = mergeEnvOverrides(v.Env, cfg.Env)
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (c *Config) variantOverrides() map[string]VariantConfig {
	overrides := make(map[string]VariantConfig, len(c.Variants))
	for _, v := range c.Variants {
		overrides[v.Name] = v
	}
	return overrides
}

func mergeEnvOverrides(base, overrides map[string]string) map[string]string {
	if base == nil && overrides == nil {
		return nil
	}
	out := make(map[string]string, len(base))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overrides {
		out[k] = v
	}
	return out
}
