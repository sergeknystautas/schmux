// Package schema provides JSON schema generation from Go struct definitions.
// It uses github.com/swaggest/jsonschema-go to generate schemas at runtime,
// ensuring schema and struct definitions stay in sync.
package schema

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/swaggest/jsonschema-go"
)

// Schema labels for registered schemas.
const (
	LabelConflictResolve = "conflict-resolve"
	LabelNudgeNik        = "nudgenik"
	LabelBranchSuggest   = "branch-suggest"
)

// schemaEntry holds a type and optional skip fields for schema generation.
type schemaEntry struct {
	value      any
	skipFields []string
}

var (
	registry      = make(map[string]schemaEntry)
	registryMu    sync.RWMutex
	schemaCache   = make(map[string]string)
	schemaCacheMu sync.RWMutex
)

// Register adds a type to the schema registry.
// The schema will be generated on first access via Get().
func Register(label string, v any, skipFields ...string) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[label] = schemaEntry{value: v, skipFields: skipFields}
}

// Get returns the JSON schema string for a registered label.
// Schemas are cached after first generation.
func Get(label string) (string, error) {
	// Check cache first
	schemaCacheMu.RLock()
	if cached, ok := schemaCache[label]; ok {
		schemaCacheMu.RUnlock()
		return cached, nil
	}
	schemaCacheMu.RUnlock()

	// Get from registry
	registryMu.RLock()
	entry, ok := registry[label]
	registryMu.RUnlock()
	if !ok {
		return "", fmt.Errorf("unknown schema label: %s", label)
	}

	// Generate schema
	schema, err := GenerateJSON(entry.value, entry.skipFields...)
	if err != nil {
		return "", fmt.Errorf("failed to generate schema for %s: %w", label, err)
	}

	// Cache it
	schemaCacheMu.Lock()
	schemaCache[label] = schema
	schemaCacheMu.Unlock()

	return schema, nil
}

// Labels returns all registered schema labels.
func Labels() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	labels := make([]string, 0, len(registry))
	for label := range registry {
		labels = append(labels, label)
	}
	return labels
}

// GenerateJSON generates a JSON schema string from a Go type.
// The type should have struct tags defining required fields and constraints.
// Fields can be excluded by adding them to the skipFields parameter.
func GenerateJSON(v any, skipFields ...string) (string, error) {
	r := jsonschema.Reflector{}

	opts := []func(*jsonschema.ReflectContext){
		jsonschema.InlineRefs,
	}

	// Add field skip interceptor if needed
	if len(skipFields) > 0 {
		skipSet := make(map[string]bool)
		for _, f := range skipFields {
			skipSet[f] = true
		}
		opts = append(opts, jsonschema.InterceptProp(
			func(params jsonschema.InterceptPropParams) error {
				if skipSet[params.Name] {
					return jsonschema.ErrSkipProperty
				}
				return nil
			},
		))
	}

	schema, err := r.Reflect(v, opts...)
	if err != nil {
		return "", err
	}

	bytes, err := json.Marshal(schema)
	if err != nil {
		return "", err
	}

	// Post-process to ensure OpenAI compatibility:
	// When additionalProperties is a schema object, properties must be defined.
	var raw map[string]any
	if err := json.Unmarshal(bytes, &raw); err != nil {
		return "", err
	}

	fixAdditionalProperties(raw)

	bytes, err = json.Marshal(raw)
	if err != nil {
		return "", err
	}

	return string(bytes), nil
}

// fixAdditionalProperties recursively ensures that when additionalProperties is
// a schema object, the properties field is defined (OpenAI requirement).
func fixAdditionalProperties(node map[string]any) {
	// Check if this node has additionalProperties that is a schema object
	if ap, ok := node["additionalProperties"]; ok {
		if apMap, ok := ap.(map[string]any); ok {
			// It's a schema object, ensure properties is defined
			if _, hasProps := node["properties"]; !hasProps {
				node["properties"] = map[string]any{}
			}
			// Recursively fix the nested schema
			fixAdditionalProperties(apMap)
		}
	}

	// Recursively process properties
	if props, ok := node["properties"].(map[string]any); ok {
		for _, prop := range props {
			if propMap, ok := prop.(map[string]any); ok {
				fixAdditionalProperties(propMap)
			}
		}
	}

	// Recursively process items (for arrays)
	if items, ok := node["items"].(map[string]any); ok {
		fixAdditionalProperties(items)
	}
}
