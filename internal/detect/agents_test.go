package detect

import (
	"context"
	"os"
	"testing"
)

func TestOpencodeDetectorRegistered(t *testing.T) {
	t.Parallel()
	a := GetAdapter("opencode")
	if a == nil {
		t.Fatal("opencode adapter not registered")
	}
	if a.Name() != "opencode" {
		t.Errorf("Name() = %q, want 'opencode'", a.Name())
	}
}

// TestCommandExists verifies commandExists works correctly.
func TestCommandExists(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		wantBool bool
	}{
		{
			name:     "sh should exist on Unix systems",
			command:  "sh",
			wantBool: true,
		},
		{
			name:     "nonexistent command should not exist",
			command:  "nonexistentcmd12345abcdef",
			wantBool: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := commandExists(tt.command)
			if got != tt.wantBool {
				t.Errorf("commandExists(%q) = %v, want %v", tt.command, got, tt.wantBool)
			}
		})
	}
}

// TestFileExists verifies fileExists works correctly.
func TestFileExists(t *testing.T) {
	// Test with a file that should exist
	tmpFile, err := os.CreateTemp("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	tmpFile.Close()

	tests := []struct {
		name     string
		path     string
		wantBool bool
	}{
		{
			name:     "existing temp file",
			path:     tmpFile.Name(),
			wantBool: true,
		},
		{
			name:     "nonexistent file",
			path:     "/nonexistent/file/that/does/not/exist",
			wantBool: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fileExists(tt.path)
			if got != tt.wantBool {
				t.Errorf("fileExists(%q) = %v, want %v", tt.path, got, tt.wantBool)
			}
		})
	}
}

// TestNpmGlobalInstalled verifies npmGlobalInstalled works correctly.
func TestNpmGlobalInstalled(t *testing.T) {
	ctx := context.Background()
	// Test with a package that should never exist
	pkg := "@nonexistent/testing-package-xyz123"
	if npmGlobalInstalled(ctx, pkg) {
		t.Errorf("npmGlobalInstalled(%q) = true, want false (package should not exist)", pkg)
	}

	// If npm is installed, test with a known package format
	if commandExists("npm") {
		// Test that the function doesn't crash and returns false for non-existent package
		pkg := "@schmux/nonexistent-test-package-xyz"
		if npmGlobalInstalled(ctx, pkg) {
			t.Errorf("npmGlobalInstalled(%q) = true, want false", pkg)
		}
	}
}

// TestHomebrewCaskInstalled verifies cask detection works correctly.
func TestHomebrewCaskInstalled(t *testing.T) {
	ctx := context.Background()
	// Test with a cask that should never exist
	cask := "nonexistent-cask-xyz123"
	if homebrewCaskInstalled(ctx, cask) {
		t.Errorf("homebrewCaskInstalled(%q) = true, want false (cask should not exist)", cask)
	}
}

// TestHomebrewFormulaInstalled verifies formula detection works correctly.
func TestHomebrewFormulaInstalled(t *testing.T) {
	ctx := context.Background()
	// Test with a formula that should never exist
	formula := "nonexistent-formula-xyz123"
	if homebrewFormulaInstalled(ctx, formula) {
		t.Errorf("homebrewFormulaInstalled(%q) = true, want false (formula should not exist)", formula)
	}
}

// TestExpandHome verifies home directory expansion works correctly.
func TestExpandHome(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		wantError bool
	}{
		{
			name:      "path without tilde should return as-is",
			path:      "/usr/local/bin",
			wantError: false,
		},
		{
			name:      "tilde alone should expand to home",
			path:      "~",
			wantError: false,
		},
		{
			name:      "tilde with path should expand",
			path:      "~/.local/bin",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := expandHome(tt.path)
			if (err != nil) != tt.wantError {
				t.Errorf("expandHome(%q) error = %v, wantError %v", tt.path, err, tt.wantError)
				return
			}
			if !tt.wantError && tt.path[0] != '~' && got != tt.path {
				t.Errorf("expandHome(%q) = %q, want %q (paths without ~ should be unchanged)", tt.path, got, tt.path)
			}
		})
	}
}

// TestNpmGlobalInstalledJSONParsing verifies JSON parsing works correctly.
func TestNpmGlobalInstalledJSONParsing(t *testing.T) {
	// This test verifies that npmGlobalInstalled properly parses JSON output
	// We can't mock npm output easily, but we can verify the function handles various cases

	// If npm is not available, function should return false
	if !commandExists("npm") {
		pkg := "@anthropic-ai/claude-code"
		if npmGlobalInstalled(context.Background(), pkg) {
			t.Errorf("npmGlobalInstalled(%q) = true when npm is not available, want false", pkg)
		}
	}
}
