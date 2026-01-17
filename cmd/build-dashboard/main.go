package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func main() {
	var skipInstall bool
	var runTests bool
	var runBuild bool
	var skipTypecheck bool

	flag.BoolVar(&skipInstall, "skip-install", false, "skip npm install step")
	flag.BoolVar(&runTests, "test", false, "run go test ./...")
	flag.BoolVar(&runBuild, "build", false, "run go build ./cmd/schmux")
	flag.BoolVar(&skipTypecheck, "skip-typecheck", false, "skip npm run typecheck in assets/dashboard")
	flag.Parse()

	repoRoot, err := findRepoRoot()
	if err != nil {
		fatalf("failed to locate repo root: %v", err)
	}

	frontendDir := filepath.Join(repoRoot, "assets", "dashboard")

	npmPath, err := npmExecutable()
	if err != nil {
		fatalf("npm not found on PATH: %v", err)
	}

	if !skipInstall {
		if err := runCmd(frontendDir, npmPath, "install"); err != nil {
			fatalf("npm install failed: %v", err)
		}
	}

	if !skipTypecheck {
		if err := runCmd(frontendDir, npmPath, "run", "typecheck"); err != nil {
			fatalf("npm run typecheck failed: %v", err)
		}
	}

	if err := runCmd(frontendDir, npmPath, "run", "build"); err != nil {
		fatalf("npm run build failed: %v", err)
	}

	if runTests {
		if err := runCmd(repoRoot, "go", "test", "./..."); err != nil {
			fatalf("go test failed: %v", err)
		}
	}

	if runBuild {
		if err := runCmd(repoRoot, "go", "build", "./cmd/schmux"); err != nil {
			fatalf("go build failed: %v", err)
		}
	}
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
