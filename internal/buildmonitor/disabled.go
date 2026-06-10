//go:build nobuildmonitor

package buildmonitor

// IsAvailable reports whether the build monitor feature is available.
func IsAvailable() bool { return false }
