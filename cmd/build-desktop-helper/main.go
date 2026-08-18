package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Stable, reused self-signed identity (Task 0). Reusing it across rebuilds keeps
// the TCC designated requirement stable — that's the property the spike tests.
const signingIdentity = "schmux-desktop-dev"

func main() {
	root, err := findRepoRoot()
	if err != nil {
		fatalf("locate repo root: %v", err)
	}
	pkgDir := filepath.Join(root, "native", "desktop-macos")

	if err := syncWebRTCFramework(root, pkgDir); err != nil {
		fatalf("sync WebRTC.xcframework: %v", err)
	}

	if err := run(root, "swift", "build", "-c", "release", "--package-path", pkgDir); err != nil {
		fatalf("swift build failed: %v", err)
	}

	binDir, err := swiftBinDir(root, pkgDir)
	if err != nil {
		fatalf("resolve build dir: %v", err)
	}

	// Sign any embedded framework first, then the executable. Path-sensitive: sign in place.
	matches, _ := filepath.Glob(filepath.Join(binDir, "*.framework"))
	for _, fw := range matches {
		if err := run(root, "codesign", "--force", "--sign", signingIdentity, fw); err != nil {
			fatalf("codesign framework %s: %v", fw, err)
		}
	}

	bin := filepath.Join(binDir, "schmux-desktop-macos")
	if err := run(root, "codesign", "--force", "--sign", signingIdentity, bin); err != nil {
		fatalf("codesign binary: %v", err)
	}

	fmt.Printf("signed helper: %s\n", bin)
}

func swiftBinDir(root, pkgDir string) (string, error) {
	cmd := exec.Command("swift", "build", "-c", "release", "--package-path", pkgDir, "--show-bin-path")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func run(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// syncWebRTCFramework copies our built WebRTC.xcframework (from native/webrtc-build,
// on the external drive) into native/desktop-macos/Frameworks/ so the helper's
// Package.swift local binary target resolves. Source overridable via
// SCHMUX_WEBRTC_XCFRAMEWORK; defaults to the native/webrtc-build output path.
func syncWebRTCFramework(root, pkgDir string) error {
	src := os.Getenv("SCHMUX_WEBRTC_XCFRAMEWORK")
	if src == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		src = filepath.Join(home, "dev-ext", "webrtc-build", "build", "_build",
			"macos_arm64", "release", "webrtc", "WebRTC.xcframework")
	}
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("not found at %s (build it via native/webrtc-build, or set SCHMUX_WEBRTC_XCFRAMEWORK): %w", src, err)
	}
	fwDir := filepath.Join(pkgDir, "Frameworks")
	dst := filepath.Join(fwDir, "WebRTC.xcframework")
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.MkdirAll(fwDir, 0o755); err != nil {
		return err
	}
	// rsync for fast incremental copies on repeat builds; cp -R fallback.
	if _, err := exec.LookPath("rsync"); err == nil {
		if err := run(root, "rsync", "-a", "--delete", src+"/", dst+"/"); err != nil {
			return err
		}
	} else if err := run(root, "cp", "-R", src, dst); err != nil {
		return err
	}
	fmt.Printf("synced WebRTC.xcframework <- %s\n", src)
	return nil
}

func findRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", cwd)
		}
		dir = parent
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
