//go:build notunnel

package tunnel

// NotifyConfig holds notification configuration (stub).
type NotifyConfig struct {
	NtfyURL   string
	Command   string
	TunnelURL string
}

// Send returns ErrModuleDisabled.
func (nc *NotifyConfig) Send(_ string, _ string) error {
	return ErrModuleDisabled
}
