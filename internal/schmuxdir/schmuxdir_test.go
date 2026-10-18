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
	got := FenceLaunchDir("repo-001", "repo-001-ab12cd34")
	want := filepath.Join("/tmp/schmux-home", "fence", "repo-001", "repo-001-ab12cd34")
	if got != want {
		t.Errorf("FenceLaunchDir = %q, want %q", got, want)
	}
}

func TestFenceSharedCacheDir(t *testing.T) {
	Set("/tmp/schmux-home")
	t.Cleanup(func() { Set("") })
	got := FenceSharedCacheDir()
	want := filepath.Join("/tmp/schmux-home", ".cache", "schmux-fence")
	if got != want {
		t.Errorf("FenceSharedCacheDir = %q, want %q", got, want)
	}
}

func TestFenceWorkspaceDir(t *testing.T) {
	Set("/tmp/schmux-home")
	t.Cleanup(func() { Set("") })
	got := FenceWorkspaceDir("repo-001")
	want := filepath.Join("/tmp/schmux-home", "fence", "repo-001")
	if got != want {
		t.Errorf("FenceWorkspaceDir = %q, want %q", got, want)
	}
}

func TestChatWorkspaceDir(t *testing.T) {
	Set("/tmp/schmux-home")
	t.Cleanup(func() { Set("") })
	got := ChatWorkspaceDir("repo-001")
	want := filepath.Join("/tmp/schmux-home", "chat", "repo-001")
	if got != want {
		t.Errorf("ChatWorkspaceDir = %q, want %q", got, want)
	}
	if session := ChatSessionDir("repo-001", "repo-001-ab12cd34"); filepath.Dir(session) != got {
		t.Errorf("ChatSessionDir parent = %q, want %q", filepath.Dir(session), got)
	}
}

func TestFlightsPath(t *testing.T) {
	Set("/tmp/schmux-home")
	t.Cleanup(func() { Set("") })
	got := FlightsPath()
	want := filepath.Join("/tmp/schmux-home", "flights.json")
	if got != want {
		t.Errorf("FlightsPath = %q, want %q", got, want)
	}
}
