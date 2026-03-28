package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// templateRepoDir is a pre-built git repo with one commit on main.
// Tests copy this instead of running git init + git add + git commit each time.
var templateRepoDir string

func TestMain(m *testing.M) {
	tempHome, err := os.MkdirTemp("", "schmux-workspace-tests-home")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tempHome)

	if err := os.Setenv("HOME", tempHome); err != nil {
		panic(err)
	}

	// Create global gitconfig so tests don't need per-repo git config calls.
	// Local git config (used by "Other" user tests) still overrides this.
	gitConfig := filepath.Join(tempHome, ".gitconfig")
	if err := os.WriteFile(gitConfig, []byte("[user]\n\temail = test@test.com\n\tname = Test User\n"), 0644); err != nil {
		panic(err)
	}

	// Build a template repo once — tests copy it instead of creating from scratch.
	templateRepoDir, err = os.MkdirTemp("", "schmux-template-repo")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(templateRepoDir)

	mustRun(templateRepoDir, "git", "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(templateRepoDir, "README.md"), []byte("test repo"), 0644); err != nil {
		panic(err)
	}
	mustRun(templateRepoDir, "git", "add", ".")
	mustRun(templateRepoDir, "git", "commit", "-m", "initial commit")

	os.Exit(m.Run())
}

func mustRun(dir, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		panic(name + " " + args[0] + ": " + string(out))
	}
}
