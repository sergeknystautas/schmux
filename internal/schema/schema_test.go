package schema

import (
	"encoding/json"
	"testing"
)

// TestStruct mimics the structure we need for OpenAI
type TestStruct struct {
	Name     string   `json:"name" required:"true"`
	Age      int      `json:"age" required:"true"`
	Tags     []string `json:"tags" required:"true" nullable:"false"`
	Optional string   `json:"optional,omitempty"` // Not required
	_        struct{} `additionalProperties:"false"`
}

func TestGenerateJSON(t *testing.T) {
	schema, err := GenerateJSON(TestStruct{})
	if err != nil {
		t.Fatalf("GenerateJSON failed: %v", err)
	}

	// Parse to verify structure
	var parsed map[string]any
	if err := json.Unmarshal([]byte(schema), &parsed); err != nil {
		t.Fatalf("Generated schema is not valid JSON: %v", err)
	}

	// Check type is object
	if parsed["type"] != "object" {
		t.Errorf("Expected type=object, got %v", parsed["type"])
	}

	// Check additionalProperties is false
	if parsed["additionalProperties"] != false {
		t.Errorf("Expected additionalProperties=false, got %v", parsed["additionalProperties"])
	}

	// Check required array contains expected fields
	required, ok := parsed["required"].([]any)
	if !ok {
		t.Fatalf("Expected required to be an array, got %T", parsed["required"])
	}

	requiredSet := make(map[string]bool)
	for _, r := range required {
		if s, ok := r.(string); ok {
			requiredSet[s] = true
		}
	}

	expectedRequired := []string{"name", "age", "tags"}
	for _, field := range expectedRequired {
		if !requiredSet[field] {
			t.Errorf("Expected %q to be in required array", field)
		}
	}

	// Optional should NOT be required
	if requiredSet["optional"] {
		t.Errorf("Did not expect 'optional' to be in required array")
	}

	// Check properties exist
	props, ok := parsed["properties"].(map[string]any)
	if !ok {
		t.Fatalf("Expected properties to be an object, got %T", parsed["properties"])
	}

	expectedProps := []string{"name", "age", "tags", "optional"}
	for _, prop := range expectedProps {
		if _, exists := props[prop]; !exists {
			t.Errorf("Expected property %q to exist", prop)
		}
	}

	t.Logf("Generated schema:\n%s", schema)
}

// TestNestedStruct tests nested object handling
type NestedChild struct {
	Action      string   `json:"action" required:"true"`
	Description string   `json:"description" required:"true"`
	_           struct{} `additionalProperties:"false"`
}

type NestedParent struct {
	Name  string                 `json:"name" required:"true"`
	Items map[string]NestedChild `json:"items" required:"true" nullable:"false"`
	_     struct{}               `additionalProperties:"false"`
}

func TestNestedStruct(t *testing.T) {
	schema, err := GenerateJSON(NestedParent{})
	if err != nil {
		t.Fatalf("GenerateJSON failed: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(schema), &parsed); err != nil {
		t.Fatalf("Generated schema is not valid JSON: %v", err)
	}

	t.Logf("Nested schema:\n%s", schema)

	// Check that items property exists and has additionalProperties
	props := parsed["properties"].(map[string]any)
	items := props["items"].(map[string]any)

	// Map types should have additionalProperties defining the value type
	if items["additionalProperties"] == nil {
		t.Errorf("Expected items to have additionalProperties for map value type")
	}
}

// TestSkipFields verifies that fields can be excluded from schema generation
type StructWithInternal struct {
	Name     string   `json:"name" required:"true"`
	Internal string   `json:"internal,omitempty"` // Should be skippable
	_        struct{} `additionalProperties:"false"`
}

func TestSkipFields(t *testing.T) {
	// Without skipping
	schemaWithInternal, err := GenerateJSON(StructWithInternal{})
	if err != nil {
		t.Fatalf("GenerateJSON failed: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(schemaWithInternal), &parsed); err != nil {
		t.Fatalf("Generated schema is not valid JSON: %v", err)
	}

	props := parsed["properties"].(map[string]any)
	if _, exists := props["internal"]; !exists {
		t.Errorf("Expected 'internal' property to exist when not skipped")
	}

	// With skipping
	schemaWithoutInternal, err := GenerateJSON(StructWithInternal{}, "internal")
	if err != nil {
		t.Fatalf("GenerateJSON with skip failed: %v", err)
	}

	var parsedSkipped map[string]any
	if err := json.Unmarshal([]byte(schemaWithoutInternal), &parsedSkipped); err != nil {
		t.Fatalf("Generated schema is not valid JSON: %v", err)
	}

	propsSkipped := parsedSkipped["properties"].(map[string]any)
	if _, exists := propsSkipped["internal"]; exists {
		t.Errorf("Expected 'internal' property to be skipped")
	}

	// Name should still exist
	if _, exists := propsSkipped["name"]; !exists {
		t.Errorf("Expected 'name' property to still exist")
	}

	t.Logf("Schema with internal: %s", schemaWithInternal)
	t.Logf("Schema without internal: %s", schemaWithoutInternal)
}

// TestSchemaType is used for Register/Get/Labels tests.
type TestSchemaType struct {
	Value string   `json:"value" required:"true"`
	_     struct{} `additionalProperties:"false"`
}

func TestRegisterAndGet(t *testing.T) {
	label := "test-register-get"
	Register(label, TestSchemaType{})

	schema, err := Get(label)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if schema == "" {
		t.Fatal("Get() returned empty schema")
	}

	// Verify it's valid JSON
	var parsed map[string]any
	if err := json.Unmarshal([]byte(schema), &parsed); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}

	// Second call should return cached result
	schema2, err := Get(label)
	if err != nil {
		t.Fatalf("Get() second call error: %v", err)
	}
	if schema2 != schema {
		t.Error("cached schema should be identical")
	}
}

func TestGet_UnknownLabel(t *testing.T) {
	_, err := Get("nonexistent-label-12345")
	if err == nil {
		t.Error("expected error for unknown label")
	}
}

func TestLabels(t *testing.T) {
	label := "test-labels-check"
	Register(label, TestSchemaType{})

	labels := Labels()
	found := false
	for _, l := range labels {
		if l == label {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Labels() does not contain registered label %q", label)
	}
}

func TestFixAdditionalProperties(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input map[string]any
		check func(t *testing.T, node map[string]any)
	}{
		{
			name: "adds properties when additionalProperties is schema object",
			input: map[string]any{
				"type":                 "object",
				"additionalProperties": map[string]any{"type": "string"},
			},
			check: func(t *testing.T, node map[string]any) {
				if _, ok := node["properties"]; !ok {
					t.Error("expected 'properties' to be added")
				}
			},
		},
		{
			name: "does not add properties when additionalProperties is false",
			input: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
			},
			check: func(t *testing.T, node map[string]any) {
				if _, ok := node["properties"]; ok {
					t.Error("should not add 'properties' when additionalProperties is false")
				}
			},
		},
		{
			name: "no additionalProperties does nothing",
			input: map[string]any{
				"type": "string",
			},
			check: func(t *testing.T, node map[string]any) {
				if _, ok := node["properties"]; ok {
					t.Error("should not add 'properties' when no additionalProperties")
				}
			},
		},
		{
			name: "preserves existing properties",
			input: map[string]any{
				"type":                 "object",
				"additionalProperties": map[string]any{"type": "string"},
				"properties":           map[string]any{"existing": map[string]any{"type": "number"}},
			},
			check: func(t *testing.T, node map[string]any) {
				props := node["properties"].(map[string]any)
				if _, ok := props["existing"]; !ok {
					t.Error("existing property should be preserved")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixAdditionalProperties(tt.input)
			tt.check(t, tt.input)
		})
	}
}
