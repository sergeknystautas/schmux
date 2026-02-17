package tunnel

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCloudflaredDownloadURL(t *testing.T) {
	tests := []struct {
		goos   string
		goarch string
		want   string
	}{
		{"darwin", "arm64", "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-darwin-arm64.tgz"},
		{"darwin", "amd64", "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-darwin-amd64.tgz"},
		{"linux", "amd64", "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64"},
		{"linux", "arm64", "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-arm64"},
	}

	for _, tt := range tests {
		t.Run(tt.goos+"/"+tt.goarch, func(t *testing.T) {
			got := cloudflaredDownloadURL(tt.goos, tt.goarch)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFindCloudflared_OnPATH(t *testing.T) {
	path, err := exec.LookPath("cloudflared")
	if err != nil {
		t.Skip("cloudflared not on PATH, skipping")
	}

	result, err := FindCloudflared("")
	if err != nil {
		t.Fatalf("FindCloudflared() error: %v", err)
	}
	if result != path {
		t.Errorf("got %q, want %q", result, path)
	}
}

func TestFindCloudflared_InSchmuxBin(t *testing.T) {
	tmpDir := t.TempDir()
	fakeBin := filepath.Join(tmpDir, "cloudflared")
	if runtime.GOOS == "windows" {
		fakeBin += ".exe"
	}
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("failed to create fake binary: %v", err)
	}

	// Override PATH so cloudflared won't be found there,
	// forcing FindCloudflared to fall through to the schmux bin dir check.
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", t.TempDir()) // empty temp dir with no binaries
	defer os.Setenv("PATH", origPath)

	result, err := FindCloudflared(tmpDir)
	if err != nil {
		t.Fatalf("FindCloudflared() error: %v", err)
	}
	if result != fakeBin {
		t.Errorf("got %q, want %q", result, fakeBin)
	}
}

func TestFindCloudflared_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	origPath := os.Getenv("PATH")
	os.Setenv("PATH", tmpDir)
	defer os.Setenv("PATH", origPath)

	_, err := FindCloudflared(tmpDir)
	if err == nil {
		t.Fatal("expected error when cloudflared not found")
	}
}

func TestInstallSuggestion(t *testing.T) {
	tests := []struct {
		goos string
		want string
	}{
		{"darwin", "brew install cloudflared"},
		{"linux", "apt install cloudflared"},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			got := installSuggestion(tt.goos)
			if !strings.Contains(got, tt.want) {
				t.Errorf("installSuggestion(%q) = %q, want contains %q", tt.goos, got, tt.want)
			}
		})
	}
}

func TestVerifyCodesign_UnsignedBinary(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("codesign only available on macOS")
	}

	// Create a fake unsigned binary
	tmpDir := t.TempDir()
	fakeBin := filepath.Join(tmpDir, "fakecloudflared")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\necho hello\n"), 0755); err != nil {
		t.Fatalf("failed to create fake binary: %v", err)
	}

	err := verifyCodesign(fakeBin)
	if err == nil {
		t.Fatal("expected error for unsigned binary, got nil")
	}
	if !strings.Contains(err.Error(), "codesign verification failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVerifyCodesign_RealCloudflared(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("codesign only available on macOS")
	}

	// Only run if cloudflared is installed (e.g., via brew)
	path, err := exec.LookPath("cloudflared")
	if err != nil {
		t.Skip("cloudflared not installed, skipping signature verification test")
	}

	if err := verifyCodesign(path); err != nil {
		t.Errorf("verifyCodesign(%q) failed: %v", path, err)
	}
}

func TestVerifyCloudflaredSignature_NonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("test is for non-darwin platforms")
	}

	// On non-darwin, should succeed with a warning (no verification available)
	err := verifyCloudflaredSignature("/nonexistent/path")
	if err != nil {
		t.Errorf("expected nil error on non-darwin, got: %v", err)
	}
}
