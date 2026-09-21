// Package disk reports available free disk space on the host filesystem.
// Available wraps platform-specific stat calls and resolves the nearest
// existing ancestor of a path so a not-yet-created workspace directory can
// be checked before any write occurs.
package disk

import (
	"fmt"
	"os"
	"path/filepath"
)

// BytesPerMiB is the conversion factor between mebibytes and bytes.
const BytesPerMiB uint64 = 1 << 20

// Units in binary-prefixed increments. 1024 bytes = 1 KiB; 1024 KiB = 1 MiB; etc.
const (
	bytesPerKiB = 1 << 10
	bytesPerMiB = 1 << 20
	bytesPerGiB = 1 << 30
	bytesPerTiB = 1 << 40
)

// probeFn is the platform-specific implementation injected by disk_unix.go
// or disk_windows.go. Tests can override it to simulate failures.
var probeFn func(path string) (uint64, error)

// Available returns the bytes available to the calling user on the
// filesystem that backs the given path. The path does not need to exist;
// the nearest existing ancestor is used.
func Available(path string) (uint64, error) {
	cleaned := filepath.Clean(path)
	resolved, err := nearestExistingPath(cleaned)
	if err != nil {
		return 0, err
	}
	return probeFn(resolved)
}

// nearestExistingPath walks up from path until it finds a directory that
// exists. It propagates non-NotExist errors (so an unreadable or
// inaccessible path fails closed rather than silently jumping to a parent
// volume) and returns the cleaned absolute path of the first existing
// ancestor.
func nearestExistingPath(path string) (string, error) {
	current := path
	for {
		info, err := os.Stat(current)
		if err == nil {
			if !info.IsDir() {
				return "", fmt.Errorf("path %q is not a directory", current)
			}
			return current, nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("stat %q: %w", current, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			// Reached the filesystem root without finding anything existing.
			return "", fmt.Errorf("no existing ancestor found for %q", path)
		}
		current = parent
	}
}

// EnsureAvailable returns nil if the disk containing path has at least
// requiredBytes available to the calling user. role is a human-readable
// label included in the error message (e.g. "workspace directory").
// requiredBytes of 0 always returns nil. When the configured threshold
// is greater than zero and free space cannot be determined, EnsureAvailable
// fails closed. probe is the platform-specific free-space lookup; callers
// pass Available in production and a stub in tests.
func EnsureAvailable(probe func(path string) (uint64, error), path string, role string, requiredBytes uint64) error {
	if requiredBytes == 0 {
		return nil
	}
	cleaned := filepath.Clean(path)
	resolved, err := nearestExistingPath(cleaned)
	if err != nil {
		return fmt.Errorf("unable to check disk space for %s: %s: %w", role, cleaned, err)
	}
	avail, err := probe(resolved)
	if err != nil {
		return fmt.Errorf("unable to check disk space for %s: %s: %w", role, cleaned, err)
	}
	if avail >= requiredBytes {
		return nil
	}
	return fmt.Errorf(
		"insufficient disk space: %s available, %s required (%s: %s)",
		FormatBytes(avail),
		FormatBytes(requiredBytes),
		role,
		cleaned,
	)
}

// FormatBytes formats bytes using IEC binary units (KiB, MiB, GiB, TiB) and
// one decimal place above bytes. Zero is reported as "0 B".
func FormatBytes(b uint64) string {
	switch {
	case b < bytesPerKiB:
		return fmt.Sprintf("%d B", b)
	case b < bytesPerMiB:
		return fmt.Sprintf("%.1f KiB", float64(b)/float64(bytesPerKiB))
	case b < bytesPerGiB:
		return fmt.Sprintf("%.1f MiB", float64(b)/float64(bytesPerMiB))
	case b < bytesPerTiB:
		return fmt.Sprintf("%.1f GiB", float64(b)/float64(bytesPerGiB))
	default:
		return fmt.Sprintf("%.1f TiB", float64(b)/float64(bytesPerTiB))
	}
}
