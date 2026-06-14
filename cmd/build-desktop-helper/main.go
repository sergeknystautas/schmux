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
