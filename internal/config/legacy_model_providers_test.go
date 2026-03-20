package config

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/detect"
)

func TestLegacyModelProvidersCompleteness(t *testing.T) {
	// Every legacy alias must be present in the map
	for oldID := range detect.LegacyIDMigrations() {
		if _, ok := legacyModelProviders[oldID]; !ok {
			t.Errorf("legacy alias %q missing from legacyModelProviders", oldID)
		}
	}

	// Every entry must have a non-empty provider
	for modelID, provider := range legacyModelProviders {
		if provider == "" {
			t.Errorf("model %q has empty provider in legacyModelProviders", modelID)
		}
	}
}
