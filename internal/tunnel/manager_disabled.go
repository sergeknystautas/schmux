//go:build notunnel

package tunnel

import (
	"errors"

	"github.com/charmbracelet/log"
)

// ErrModuleDisabled is returned when the tunnel module is excluded from the build.
var ErrModuleDisabled = errors.New("remote access is not available in this build")

// Tunnel states
const (
	StateOff       = "off"
	StateStarting  = "starting"
	StateConnected = "connected"
	StateError     = "error"
)

// TunnelStatus represents the current tunnel state.
type TunnelStatus struct {
	State string `json:"state"`
	URL   string `json:"url,omitempty"`
	Error string `json:"error,omitempty"`
}

// ManagerConfig holds configuration for the tunnel manager.
type ManagerConfig struct {
	Disabled          func() bool
	PasswordHashSet   func() bool
	Port              int
	BindAddress       string
	AllowAutoDownload bool
	SchmuxBinDir      string
	TimeoutMinutes    int
	OnStatusChange    func(TunnelStatus)
}

// Manager is a no-op stub when the tunnel module is excluded.
type Manager struct{}

// NewManager returns a disabled tunnel manager.
func NewManager(_ ManagerConfig, _ *log.Logger) *Manager {
	return &Manager{}
}

// Status returns the off state.
func (m *Manager) Status() TunnelStatus {
	return TunnelStatus{State: StateOff}
}

// Start returns ErrModuleDisabled.
func (m *Manager) Start() error {
	return ErrModuleDisabled
}

// Stop is a no-op.
func (m *Manager) Stop() {}

// IsAvailable reports whether the tunnel module is included in this build.
func IsAvailable() bool { return false }
