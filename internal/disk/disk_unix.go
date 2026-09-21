//go:build darwin || linux

package disk

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func init() {
	probeFn = availableUnix
}

// availableUnix returns caller-available bytes via statfs.
func availableUnix(path string) (uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("statfs %q: %w", path, err)
	}
	// Bavail is the free space available to non-root callers; Bsize is the
	// fragment size in bytes. Multiplying yields caller-visible free bytes.
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}
