package disk

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		name string
		in   uint64
		want string
	}{
		{"zero bytes", 0, "0 B"},
		{"single byte", 1, "1 B"},
		{"just under 1 KiB", 1023, "1023 B"},
		{"exact KiB", 1024, "1.0 KiB"},
		{"fractional KiB", 1024 + 512, "1.5 KiB"},
		{"MiB boundary", 1 << 20, "1.0 MiB"},
		{"GiB boundary", 1 << 30, "1.0 GiB"},
		{"GiB 5", 5 * (1 << 30), "5.0 GiB"},
		// 1.8 GiB rounded: 1932735283 bytes == 1.8 GiB under binary formatting.
		{"1.8 GiB", 1932735283, "1.8 GiB"},
		{"TiB boundary", 1 << 40, "1.0 TiB"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatBytes(tc.in)
			if got != tc.want {
				t.Errorf("FormatBytes(%d) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNearestExistingPath_ReturnsTempDir(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "not-yet-created", "child")
	got, err := nearestExistingPath(child)
	if err != nil {
		t.Fatalf("nearestExistingPath: %v", err)
	}
	if got != dir {
		t.Errorf("nearestExistingPath(%q) = %q, want %q", child, got, dir)
	}
}

func TestNearestExistingPath_NoExistingAncestor(t *testing.T) {
	// An absolute path that cannot exist on the current system (a single
	// segment inside an unknown volume) should still resolve to the
	// OS-reported root if the root is the only existing ancestor. We
	// assert that the resolver returns an existing path on every platform
	// rather than testing the exact root path.
	got, err := nearestExistingPath("/this/path/definitely/does/not/exist/anywhere")
	if err != nil {
		t.Fatalf("nearestExistingPath: %v", err)
	}
	if got == "" {
		t.Error("nearestExistingPath returned empty path")
	}
}

func TestAvailable_ReturnsValue(t *testing.T) {
	dir := t.TempDir()
	got, err := Available(dir)
	if err != nil {
		t.Fatalf("Available(%q): %v", dir, err)
	}
	if got == 0 {
		t.Errorf("Available(%q) = 0, expected positive value", dir)
	}
}

func TestEnsureAvailable_AtThreshold(t *testing.T) {
	dir := t.TempDir()
	const avail = uint64(5 * (1 << 30))
	probe := func(string) (uint64, error) { return avail, nil }
	if err := EnsureAvailable(probe, dir, "workspace directory", avail); err != nil {
		t.Errorf("EnsureAvailable at threshold: %v", err)
	}
}

func TestEnsureAvailable_BelowThreshold(t *testing.T) {
	dir := t.TempDir()
	const avail = uint64(5 * (1 << 30))
	probe := func(string) (uint64, error) { return avail, nil }
	// Require one byte more than is available to force the rejection.
	required := avail + 1
	err := EnsureAvailable(probe, dir, "workspace directory", required)
	if err == nil {
		t.Fatal("expected insufficient-space error, got nil")
	}
	msg := err.Error()
	wantParts := []string{
		"insufficient disk space:",
		" available,",
		" required",
		"workspace directory",
		dir,
	}
	for _, want := range wantParts {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q\nfull message: %s", want, msg)
		}
	}
}

func TestEnsureAvailable_ProbeFailsClosed(t *testing.T) {
	probe := func(string) (uint64, error) {
		return 0, errors.New("simulated stat failure")
	}

	err := EnsureAvailable(probe, "/nonexistent", "workspace directory", 1)
	if err == nil {
		t.Fatal("expected fail-closed error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "unable to check disk space") {
		t.Errorf("error message missing fail-closed prefix: %s", msg)
	}
	if !strings.Contains(msg, "simulated stat failure") {
		t.Errorf("error message missing underlying cause: %s", msg)
	}
}

func TestEnsureAvailable_NonExistentErrorIsNotPropagated(t *testing.T) {
	// EnsureAvailable must not propagate os.IsNotExist errors for the leaf
	// path because the workspace directory does not exist yet. It should
	// fall back to the nearest existing ancestor's available space.
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing/leaf")

	// The production probe resolves the ancestor itself, so this should
	// succeed without ever inspecting the missing leaf path.
	if err := EnsureAvailable(Available, missing, "workspace directory", 1); err != nil {
		t.Fatalf("expected nil error using ancestor, got: %v", err)
	}
}
