package lore

import (
	"strings"
	"testing"
)

func TestBuildMergePrompt(t *testing.T) {
	currentContent := "# Project\n\n## Build\ngo build\n"
	rules := []Rule{
		{Text: "Use go run ./cmd/build-dashboard instead of npm", Category: "build"},
		{Text: "Run tests with --race flag", Category: "testing"},
	}

	prompt := BuildMergePrompt(currentContent, rules)

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
	// Should request tagged output format
	if !strings.Contains(prompt, "<MERGED>") {
		t.Error("prompt should request MERGED tag in output format")
	}
	if !strings.Contains(prompt, "<SUMMARY>") {
		t.Error("prompt should request SUMMARY tag in output format")
	}
}

func TestParseMergeResponse(t *testing.T) {
	response := "<SUMMARY>Added build dashboard rule</SUMMARY>\n<MERGED>\n# Project\n\n## Build\ngo build\n\nAlways use go run ./cmd/build-dashboard.\n</MERGED>"

	result, err := ParseMergeResponse(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.MergedContent, "go run ./cmd/build-dashboard") {
		t.Error("merged content should contain the new rule")
	}
	if result.Summary != "Added build dashboard rule" {
		t.Errorf("unexpected summary: %s", result.Summary)
	}
}

func TestParseMergeResponse_WithCodeBlocks(t *testing.T) {
	// Merged content containing markdown code blocks — backticks must survive parsing
	response := "<SUMMARY>Added build section</SUMMARY>\n<MERGED>\n# Project\n\n## Build\n\n```bash\ngo build ./cmd/schmux\n```\n\n## Rules\n- Always use X\n</MERGED>"

	result, err := ParseMergeResponse(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.MergedContent, "```bash") {
		t.Error("merged content should preserve code block opening")
	}
	if !strings.Contains(result.MergedContent, "go build ./cmd/schmux") {
		t.Error("merged content should preserve code block content")
	}
	if !strings.Contains(result.MergedContent, "Always use X") {
		t.Error("merged content should contain the new rule")
	}
}

func TestParseMergeResponse_WithPreamble(t *testing.T) {
	// LLM adds text before the tags
	response := "Here is the merged file:\n\n<SUMMARY>Added testing rule</SUMMARY>\n<MERGED>\n# Project\n- Run with --race\n</MERGED>\n\nLet me know if you need changes."

	result, err := ParseMergeResponse(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Summary != "Added testing rule" {
		t.Errorf("unexpected summary: %s", result.Summary)
	}
	if !strings.Contains(result.MergedContent, "--race") {
		t.Error("merged content should contain the rule")
	}
}

func TestParseMergeResponse_Invalid(t *testing.T) {
	_, err := ParseMergeResponse("no tags here")
	if err == nil {
		t.Error("expected error for missing tags")
	}
}

func TestParseMergeResponse_MissingSummary(t *testing.T) {
	_, err := ParseMergeResponse("<MERGED>\ncontent\n</MERGED>")
	if err == nil {
		t.Error("expected error for missing SUMMARY")
	}
}

func TestParseMergeResponse_MissingMerged(t *testing.T) {
	_, err := ParseMergeResponse("<SUMMARY>test</SUMMARY>")
	if err == nil {
		t.Error("expected error for missing MERGED")
	}
}
