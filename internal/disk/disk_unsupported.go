//go:build !darwin && !linux && !windows

package disk

import "fmt"

func init() {
	probeFn = unavailable
}

// unavailable returns an explicit unsupported-platform error. Available and
// EnsureAvailable translate this into the standard fail-closed message.
func unavailable(path string) (uint64, error) {
	return 0, fmt.Errorf("disk space probing is not supported on this platform")
}
