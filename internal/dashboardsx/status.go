//go:build !nodashboardsx

package dashboardsx

import (
	"math"
	"os"
	"time"
)

// ConfigReader is the interface for reading dashboard.sx config values.
type ConfigReader interface {
	GetDashboardSXEnabled() bool
	GetDashboardSXCode() string
	GetDashboardSXIP() string
	GetDashboardSXHostname() string
}

// Status represents the current state of dashboard.sx configuration.
type Status struct {
	HasInstanceKey  bool
	HasCert         bool
	Code            string
	IP              string
	Hostname        string
	CertExpiry      time.Time
	DaysUntilExpiry int
	Enabled         bool
}

// GetStatus inspects the filesystem and config to return the current dashboard.sx status.
func GetStatus(cfg ConfigReader) (*Status, error) {
	s := &Status{
		Code:     cfg.GetDashboardSXCode(),
		IP:       cfg.GetDashboardSXIP(),
		Hostname: cfg.GetDashboardSXHostname(),
		Enabled:  cfg.GetDashboardSXEnabled(),
	}

	// Check instance key
	if _, err := os.Stat(InstanceKeyPath()); err == nil {
		s.HasInstanceKey = true
	}

	// Check certificate
	if _, err := os.Stat(CertPath()); err == nil {
		s.HasCert = true
		if expiry, err := GetCertExpiry(); err == nil {
			s.CertExpiry = expiry
			s.DaysUntilExpiry = int(math.Ceil(time.Until(expiry).Hours() / 24))
		}
	}

	return s, nil
}

// IsAvailable reports whether the dashboardsx module is included in this build.
func IsAvailable() bool { return true }
