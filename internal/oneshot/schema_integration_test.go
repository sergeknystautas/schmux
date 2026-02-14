package oneshot_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/sergeknystautas/schmux/internal/schema"

	// Import domain packages to trigger their init() which registers schemas
	_ "github.com/sergeknystautas/schmux/internal/branchsuggest"
	_ "github.com/sergeknystautas/schmux/internal/conflictresolve"
	_ "github.com/sergeknystautas/schmux/internal/nudgenik"
)

// TestSchemaRegistry validates that all registered schemas meet OpenAI requirements:
// - All required keys must exist in properties
// - When additionalProperties is a schema, properties must be defined
func TestSchemaRegistry(t *testing.T) {
	labels := schema.Labels()
	if len(labels) == 0 {
		t.Fatal("no schemas registered")
	}

	for _, label := range labels {
		schemaJSON, err := schema.Get(label)
		if err != nil {
			t.Fatalf("failed to get schema %q: %v", label, err)
		}
		if err := validateSchemaRequired(schemaJSON); err != nil {
			t.Fatalf("schema %q invalid: %v", label, err)
		}
	}
}

func validateSchemaRequired(raw string) error {
	var node map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return err
	}
	return walkSchema(node)
}

func walkSchema(node map[string]interface{}) error {
	// Validate that all required keys exist in properties (if properties exist).
	// Note: Not all properties need to be required - some can be optional.
	if propsRaw, ok := node["properties"]; ok {
		props, ok := propsRaw.(map[string]interface{})
		if ok {
			requiredSet := map[string]struct{}{}
			if reqRaw, ok := node["required"]; ok {
				if reqList, ok := reqRaw.([]interface{}); ok {
					for _, item := range reqList {
						if s, ok := item.(string); ok {
							requiredSet[s] = struct{}{}
						}
					}
				}
			}
			// Validate that all required keys actually exist in properties
			for key := range requiredSet {
				if _, ok := props[key]; !ok {
					return fmt.Errorf("required key %q not found in properties", key)
				}
			}
			for _, child := range props {
				if childMap, ok := child.(map[string]interface{}); ok {
					if err := walkSchema(childMap); err != nil {
						return err
					}
				}
			}
		}
	}

	// Validate additionalProperties if it is a schema object.
	// OpenAI requires that when additionalProperties is a schema, properties must be
	// explicitly defined (even if empty). See: https://platform.openai.com/docs/guides/structured-outputs
	if apRaw, ok := node["additionalProperties"]; ok {
		if apMap, ok := apRaw.(map[string]interface{}); ok {
			// Check that properties is defined (OpenAI requirement)
			if _, hasProps := node["properties"]; !hasProps {
				return fmt.Errorf("properties must be defined when additionalProperties is a schema (can be empty: {})")
			}
			if err := walkSchema(apMap); err != nil {
				return err
			}
		}
	}

	return nil
}
