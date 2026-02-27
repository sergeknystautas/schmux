package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func main() {
	repoRoot, err := findRepoRoot()
	if err != nil {
		fatalf("failed to locate repo root: %v", err)
	}

	dashboardDir := filepath.Join(repoRoot, "assets", "dashboard")

	npmPath, err := npmExecutable()
	if err != nil {
		fatalf("npm not found on PATH: %v", err)
	}

	// Install deps (shared with dashboard)
	if err := runCmd(dashboardDir, npmPath, "install"); err != nil {
		fatalf("npm install failed: %v", err)
	}

	// Build website using its own vite config
	if err := runCmd(dashboardDir, npmPath, "exec", "--", "vite", "build", "--config", "website/vite.config.ts"); err != nil {
		fatalf("vite build failed: %v", err)
	}

	fmt.Println("Website built successfully → dist/website/")
}

func runCmd(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func npmExecutable() (string, error) {
	npm := "npm"
	if runtime.GOOS == "windows" {
		npm = "npm.cmd"
	}
	return exec.LookPath(npm)
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
			return "", fmt.Errorf("go.mod not found starting from %s", cwd)
		}
		dir = parent
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
