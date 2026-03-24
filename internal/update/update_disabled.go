//go:build noupdate

package update

import (
	"errors"

	"github.com/charmbracelet/log"
)

var errDisabled = errors.New("self-update is not available in this build")

// SetLogger is a no-op when the update module is excluded.
func SetLogger(_ *log.Logger) {}

// Update returns errDisabled when the update module is excluded.
func Update() error { return errDisabled }

// CheckForUpdate returns no update available when the module is excluded.
func CheckForUpdate() (string, bool, error) { return "", false, nil }

// GetLatestVersion returns errDisabled when the update module is excluded.
func GetLatestVersion() (string, error) { return "", errDisabled }

// IsAvailable reports whether the update module is included in this build.
func IsAvailable() bool { return false }
