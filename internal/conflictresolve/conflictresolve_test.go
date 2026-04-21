package conflictresolve

import (
	"errors"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/oneshot"
)

func TestValidateAndNormalize_BasicSuccess(t *testing.T) {
	input := OneshotResult{
		AllResolved: true,
		Confidence:  "high",
		Summary:     "ok",
		Files:       map[string]FileAction{"a.go": {Action: "modified", Description: "did stuff"}},
	}
	result, err := validateAndNormalize(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Confidence != "high" {
		t.Fatalf("want high, got %q", result.Confidence)
	}
}

func TestValidateAndNormalize_EmptyConfidenceRejected(t *testing.T) {
	input := OneshotResult{AllResolved: true, Confidence: "", Summary: "ok"}
	_, err := validateAndNormalize(input)
	if err == nil {
		t.Fatal("expected error for empty confidence")
	}
	if !errors.Is(err, oneshot.ErrInvalidResponse) {
		t.Fatalf("want ErrInvalidResponse, got %v", err)
	}
}

func TestValidateAndNormalize_LiteralNewlineInSummary(t *testing.T) {
	input := OneshotResult{Confidence: "high", Summary: `line1\nline2`}
	result, err := validateAndNormalize(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Summary != "line1\nline2" {
		t.Fatalf("want real newline in summary, got %q", result.Summary)
	}
}

func TestValidateAndNormalize_LiteralNewlineInDescription(t *testing.T) {
	input := OneshotResult{
		Confidence: "high",
		Files: map[string]FileAction{
			"a.go": {Action: "modified", Description: `line1\nline2`},
		},
	}
	result, err := validateAndNormalize(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Files["a.go"].Description != "line1\nline2" {
		t.Fatalf("want real newline in description, got %q", result.Files["a.go"].Description)
	}
}

func TestBuildPrompt(t *testing.T) {
	prompt := BuildPrompt("/tmp/workspace", "abc123", "def456", "Add feature X", []string{
		"internal/foo.go",
	})

	checks := []string{
		"/tmp/workspace",
		"abc123",
		"def456",
		"Add feature X",
		"internal/foo.go",
		"all_resolved",
		"confidence",
		"modified",
		"deleted",
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("prompt missing expected content: %q", check)
		}
	}

	// Should NOT contain file contents - only paths
	if strings.Contains(prompt, "<<<<<<< HEAD") {
		t.Error("prompt should not contain file contents, only file paths")
	}
}
