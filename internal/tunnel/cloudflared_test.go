package tunnel

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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

// makeTgz creates a tar.gz archive containing a single file named "cloudflared"
// with the given content.
func makeTgz(t *testing.T, content []byte) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	if err := tw.WriteHeader(&tar.Header{
		Name: "cloudflared",
		Mode: 0755,
		Size: int64(len(content)),
	}); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("tar write: %v", err)
	}
	tw.Close()
	gw.Close()
	return &buf
}

func TestExtractTgz_NormalArchive(t *testing.T) {
	content := []byte("#!/bin/sh\necho hello\n")
	archive := makeTgz(t, content)

	destPath := filepath.Join(t.TempDir(), "cloudflared")
	if err := extractTgz(archive, destPath); err != nil {
		t.Fatalf("extractTgz failed: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("reading extracted file: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
}

func TestExtractTgz_RejectsOversizedEntry(t *testing.T) {
	// Create a tar.gz with an entry whose header claims a size exceeding
	// maxCloudflaredSize. The actual content is small (we only write a few bytes),
	// but the tar header's Size field is what extractTgz should check against.
	// This simulates a decompression bomb where the decompressed output could
	// be much larger than the compressed input.
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Header claims file is maxCloudflaredSize + 1 byte
	oversized := int64(maxCloudflaredSize + 1)
	if err := tw.WriteHeader(&tar.Header{
		Name: "cloudflared",
		Mode: 0755,
		Size: oversized,
	}); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	// Write a small amount of actual data — just enough that extractTgz
	// doesn't hit io.EOF before the size limit kicks in.
	// We write zeros up to just past the limit to trigger the limiter.
	smallChunk := make([]byte, 1024)
	for written := int64(0); written < oversized; written += int64(len(smallChunk)) {
		remaining := oversized - written
		if remaining < int64(len(smallChunk)) {
			smallChunk = smallChunk[:remaining]
		}
		tw.Write(smallChunk)
	}
	tw.Close()
	gw.Close()

	destPath := filepath.Join(t.TempDir(), "cloudflared")
	err := extractTgz(&buf, destPath)
	if err == nil {
		t.Fatal("expected error for oversized tar entry, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("expected 'exceeds maximum' in error, got: %v", err)
	}

	// Extracted file should not be larger than maxCloudflaredSize
	if fi, statErr := os.Stat(destPath); statErr == nil {
		if fi.Size() > maxCloudflaredSize {
			t.Errorf("extracted file is %d bytes, exceeds limit of %d", fi.Size(), maxCloudflaredSize)
		}
	}
}
