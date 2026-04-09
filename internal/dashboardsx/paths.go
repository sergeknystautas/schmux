//go:build !nodashboardsx

package dashboardsx

import (
	"os"
	"path/filepath"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

const dirName = "dashboardsx"

// Dir returns the dashboard.sx data directory (<schmuxdir>/dashboardsx/).
func Dir() string {
	return filepath.Join(schmuxdir.Get(), dirName)
}

// InstanceKeyPath returns the path to the instance key file.
func InstanceKeyPath() string {
	return filepath.Join(Dir(), "instance.key")
}

// CertPath returns the path to the TLS certificate.
func CertPath() string {
	return filepath.Join(Dir(), "cert.pem")
}

// KeyPath returns the path to the TLS private key.
func KeyPath() string {
	return filepath.Join(Dir(), "key.pem")
}

// ACMEAccountPath returns the path to the ACME account key.
func ACMEAccountPath() string {
	return filepath.Join(Dir(), "acme-account.key")
}

// EnsureDir creates the dashboardsx directory with 0700 permissions.
func EnsureDir() error {
	return os.MkdirAll(Dir(), 0700)
}
