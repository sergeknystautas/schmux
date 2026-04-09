package schmuxdir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetDefault(t *testing.T) {
	Reset()
	got := Get()
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".schmux")
	if got != want {
		t.Errorf("Get() = %q, want %q", got, want)
	}
}

func TestSetOverrides(t *testing.T) {
	Reset()
	defer Reset()
	Set("/tmp/my-schmux")
	if got := Get(); got != "/tmp/my-schmux" {
		t.Errorf("Get() = %q, want /tmp/my-schmux", got)
	}
}

func TestResetClearsOverride(t *testing.T) {
	Set("/tmp/override")
	Reset()
	got := Get()
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".schmux")
	if got != want {
		t.Errorf("after Reset(), Get() = %q, want %q", got, want)
	}
}
