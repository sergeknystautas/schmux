package dashboardsx

import (
	"os"
	"path/filepath"
)

const dirName = "dashboardsx"

// Dir returns the dashboard.sx data directory (~/.schmux/dashboardsx/).
func Dir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".schmux", dirName), nil
}

// InstanceKeyPath returns the path to the instance key file.
func InstanceKeyPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "instance.key"), nil
}

// CertPath returns the path to the TLS certificate.
func CertPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cert.pem"), nil
}

// KeyPath returns the path to the TLS private key.
func KeyPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "key.pem"), nil
}

// ACMEAccountPath returns the path to the ACME account key.
func ACMEAccountPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "acme-account.key"), nil
}

// EnsureDir creates the dashboardsx directory with 0700 permissions.
func EnsureDir() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0700)
}
