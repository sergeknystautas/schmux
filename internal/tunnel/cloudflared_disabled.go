//go:build notunnel

package tunnel

import "github.com/charmbracelet/log"

// SetLogger is a no-op when the tunnel module is excluded.
func SetLogger(_ *log.Logger) {}

// FindCloudflared returns ErrModuleDisabled.
func FindCloudflared(_ string) (string, error) {
	return "", ErrModuleDisabled
}

// EnsureCloudflared returns ErrModuleDisabled.
func EnsureCloudflared(_ string) (string, error) {
	return "", ErrModuleDisabled
}
