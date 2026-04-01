package timelapse

import (
	"path/filepath"
	"testing"
)

func TestFirstRunNotice(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "recordings")

	// First call should return the notice
	msg := ShowFirstRunNotice(dir)
	if msg == "" {
		t.Error("first call should return notice message")
	}

	// Second call should return empty (marker exists)
	msg = ShowFirstRunNotice(dir)
	if msg != "" {
		t.Errorf("second call should return empty, got %q", msg)
	}
}
