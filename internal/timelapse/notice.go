package timelapse

import (
	"os"
	"path/filepath"
)

const noticeMarker = ".notice-shown"

// ShowFirstRunNotice returns the notice message if it hasn't been shown yet.
// Creates the marker file on first call. Returns empty string on subsequent calls.
func ShowFirstRunNotice(recordingsDir string) string {
	markerPath := filepath.Join(recordingsDir, noticeMarker)
	if _, err := os.Stat(markerPath); err == nil {
		return "" // already shown
	}

	// Create recordings dir and marker
	os.MkdirAll(recordingsDir, 0700)
	os.WriteFile(markerPath, []byte("shown"), 0600)

	return "Timelapse recording is enabled. Terminal output is saved to " + recordingsDir + ". Run 'schmux config' to disable."
}
