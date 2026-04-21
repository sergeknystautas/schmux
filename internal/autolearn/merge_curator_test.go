package autolearn

import (
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/schema"
)

func TestBuildMergePrompt(t *testing.T) {
	currentContent := "# Project\n\n## Build\ngo build\n"
	learnings := []Learning{
		{Title: "Use go run ./cmd/build-dashboard instead of npm", Category: "build"},
		{Title: "Run tests with --race flag", Category: "testing"},
	}

	prompt := BuildMergePrompt(currentContent, learnings)

	// Should contain current file content
	if !strings.Contains(prompt, "# Project") {
		t.Error("prompt should contain current file content")
	}
	if !strings.Contains(prompt, "go build") {
		t.Error("prompt should contain existing build section")
	}
	// Should contain rules to merge
	if !strings.Contains(prompt, "Use go run ./cmd/build-dashboard") {
		t.Error("prompt should contain first rule")
	}
	if !strings.Contains(prompt, "Run tests with --race flag") {
		t.Error("prompt should contain second rule")
	}
	// Should contain categories
	if !strings.Contains(prompt, "[build]") {
		t.Error("prompt should contain rule category")
	}
	// Should request JSON output format
	if !strings.Contains(prompt, `"merged_content"`) {
		t.Error("prompt should request merged_content field in JSON output")
	}
	if !strings.Contains(prompt, `"summary"`) {
		t.Error("prompt should request summary field in JSON output")
	}
}

func TestMergeCuratorSchemaRegistered(t *testing.T) {
	s, err := schema.Get(schema.LabelAutolearnMerge)
	if err != nil {
		t.Fatalf("LabelAutolearnMerge schema should be registered: %v", err)
	}
	if !strings.Contains(s, "merged_content") || !strings.Contains(s, "summary") {
		t.Fatalf("schema missing required fields: %s", s)
	}
}
