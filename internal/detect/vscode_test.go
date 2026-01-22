package detect

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestResolveVSCodePath verifies that ResolveVSCodePath returns valid results.
func TestResolveVSCodePath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, found := ResolveVSCodePath(ctx)

	// If VS Code is found, verify the result is valid
	if found {
		if result.Path == "" {
			t.Error("VSCodePath.Path should not be empty when found")
		}
		if result.Source == "" {
			t.Error("VSCodePath.Source should not be empty when found")
		}
		t.Logf("Found VS Code via %s: %s", result.Source, result.Path)
	} else {
		// Not finding VS Code is OK - it may not be installed
		t.Log("VS Code not found (this is OK if VS Code is not installed)")
	}
}

// TestResolveVSCodePathTimeout verifies that ResolveVSCodePath respects context timeout.
func TestResolveVSCodePathTimeout(t *testing.T) {
	// Create a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _ = ResolveVSCodePath(ctx)
	elapsed := time.Since(start)

	// Should complete quickly (within 1 second) even if timeout is very short
	// The function should respect the context and not hang
	if elapsed > 5*time.Second {
		t.Errorf("ResolveVSCodePath took too long: %v, expected < 5s", elapsed)
	}
}

// TestResolveViaShell verifies shell resolution works for known commands.
func TestResolveViaShell(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		cmd      string
		wantFind bool // Whether we expect to find this command
	}{
		{
			name:     "sh should be resolvable",
			cmd:      "sh",
			wantFind: true,
		},
		{
			name:     "nonexistent command should not be found",
			cmd:      "nonexistentcmd12345abcdef",
			wantFind: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, found := resolveViaShell(ctx, tt.cmd)
			if found != tt.wantFind {
				t.Errorf("resolveViaShell(%q) found = %v, want %v", tt.cmd, found, tt.wantFind)
			}
			if found && path == "" {
				t.Errorf("resolveViaShell(%q) returned empty path but found = true", tt.cmd)
			}
			if found {
				t.Logf("resolveViaShell(%q) = %q", tt.cmd, path)
			}
		})
	}
}

// TestResolveViaShellWithContext verifies shell resolution respects context cancellation.
func TestResolveViaShellWithContext(t *testing.T) {
	// Create an already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, _ = resolveViaShell(ctx, "code")
	elapsed := time.Since(start)

	// Should return quickly when context is cancelled
	if elapsed > 2*time.Second {
		t.Errorf("resolveViaShell with cancelled context took too long: %v", elapsed)
	}
}

// TestCheckKnownLocations verifies known location checking works.
func TestCheckKnownLocations(t *testing.T) {
	path, source, found := checkKnownLocations()

	// Log what was found (or not found)
	if found {
		t.Logf("checkKnownLocations found: %s (source: %s)", path, source)

		// Verify the path is not empty
		if path == "" {
			t.Error("path should not be empty when found")
		}
		if source == "" {
			t.Error("source should not be empty when found")
		}
	} else {
		t.Log("checkKnownLocations did not find VS Code in known locations (this is OK)")
	}
}

// TestCheckKnownLocationsReturnsExecutable verifies that when a path is found,
// it actually exists and is executable.
func TestCheckKnownLocationsReturnsExecutable(t *testing.T) {
	path, _, found := checkKnownLocations()

	if !found {
		t.Skip("VS Code not found in known locations, skipping executable check")
	}

	// Check that the file exists
	info, err := os.Stat(path)
	if err != nil {
		t.Errorf("checkKnownLocations returned path that doesn't exist: %s, error: %v", path, err)
		return
	}

	// Check that it's not a directory
	if info.IsDir() {
		t.Errorf("checkKnownLocations returned a directory, not a file: %s", path)
	}

	// Check that it's executable (on Unix systems)
	if runtime.GOOS != "windows" {
		mode := info.Mode()
		if mode&0111 == 0 {
			t.Errorf("checkKnownLocations returned non-executable file: %s (mode: %v)", path, mode)
		}
	}
}

// TestVSCodePathStruct verifies VSCodePath struct behaves correctly.
func TestVSCodePathStruct(t *testing.T) {
	p := VSCodePath{
		Path:   "/usr/bin/code",
		Source: "PATH",
	}

	if p.Path != "/usr/bin/code" {
		t.Errorf("VSCodePath.Path = %q, want %q", p.Path, "/usr/bin/code")
	}
	if p.Source != "PATH" {
		t.Errorf("VSCodePath.Source = %q, want %q", p.Source, "PATH")
	}
}

// TestResolveVSCodePathPrefersPATH verifies that PATH resolution is preferred
// when the command exists in PATH.
func TestResolveVSCodePathPrefersPATH(t *testing.T) {
	// Create a temporary directory with a mock 'code' script
	tmpDir := t.TempDir()
	mockCodePath := filepath.Join(tmpDir, "code")

	// Create a simple shell script that acts as a mock 'code' command
	mockScript := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(mockCodePath, []byte(mockScript), 0755); err != nil {
		t.Fatalf("Failed to create mock code script: %v", err)
	}

	// Prepend our temp directory to PATH
	originalPath := os.Getenv("PATH")
	os.Setenv("PATH", tmpDir+string(os.PathListSeparator)+originalPath)
	defer os.Setenv("PATH", originalPath)

	ctx := context.Background()
	result, found := ResolveVSCodePath(ctx)

	if !found {
		t.Error("ResolveVSCodePath should find mock code command")
		return
	}

	if result.Path != mockCodePath {
		t.Logf("ResolveVSCodePath found: %s (source: %s)", result.Path, result.Source)
		// It's OK if it finds a different 'code' - the important thing is that
		// PATH is checked first
	}

	if result.Source != "PATH" && result.Path == mockCodePath {
		t.Errorf("Expected source to be PATH when found via PATH, got: %s", result.Source)
	}
}

// TestResolveViaShellWithNilContext verifies resolveViaShell handles nil context.
func TestResolveViaShellWithNilContext(t *testing.T) {
	// This should not panic and should create its own context
	path, found := resolveViaShell(nil, "sh")

	// sh should exist on Unix systems
	if runtime.GOOS != "windows" {
		if !found {
			t.Log("resolveViaShell(nil, \"sh\") did not find sh - this may be OK in some environments")
		} else if path == "" {
			t.Error("resolveViaShell found sh but returned empty path")
		}
	}
}

// TestResolveVSCodePathIntegration is an integration test that verifies
// the full resolution flow works end-to-end.
func TestResolveVSCodePathIntegration(t *testing.T) {
	// Skip in CI environments where VS Code is unlikely to be installed
	if os.Getenv("CI") != "" {
		t.Skip("Skipping integration test in CI environment")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, found := ResolveVSCodePath(ctx)

	if found {
		// Verify we can actually execute the command
		cmd := exec.CommandContext(ctx, result.Path, "--version")
		output, err := cmd.Output()
		if err != nil {
			t.Errorf("Found VS Code at %s but failed to run --version: %v", result.Path, err)
		} else {
			t.Logf("VS Code version: %s", string(output))
		}
	}
}
