//go:build windows

package disk

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func init() {
	probeFn = availableWindows
}

// availableWindows returns caller-available bytes via GetDiskFreeSpaceEx.
func availableWindows(path string) (uint64, error) {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("utf16 %q: %w", path, err)
	}
	var freeBytesAvailableToCaller, totalBytes, totalFreeBytes uint64
	if err := windows.GetDiskFreeSpaceEx(ptr, &freeBytesAvailableToCaller, &totalBytes, &totalFreeBytes); err != nil {
		return 0, fmt.Errorf("getdiskfreespaceex %q: %w", path, err)
	}
	return freeBytesAvailableToCaller, nil
}
