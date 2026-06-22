package schmuxdir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetDefault(t *testing.T) {
	old := dir
	dir = ""
	defer func() { dir = old }()

	got := Get()
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".schmux")
	if got != want {
		t.Errorf("Get() = %q, want %q", got, want)
	}
}

func TestSetOverrides(t *testing.T) {
	old := dir
	defer func() { dir = old }()

	Set("/tmp/my-schmux")
	if got := Get(); got != "/tmp/my-schmux" {
		t.Errorf("Get() = %q, want /tmp/my-schmux", got)
	}
}

func TestFenceLaunchDir(t *testing.T) {
	Set("/tmp/schmux-home")
	t.Cleanup(func() { Set("") })
	got := FenceLaunchDir("repo-001-ab12cd34")
	want := filepath.Join("/tmp/schmux-home", "fence", "repo-001-ab12cd34")
	if got != want {
		t.Errorf("FenceLaunchDir = %q, want %q", got, want)
	}
}
