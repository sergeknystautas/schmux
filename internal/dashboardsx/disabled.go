//go:build nodashboardsx

package dashboardsx

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/log"
	"github.com/go-acme/lego/v4/challenge"
)

const DefaultServiceURL = "https://dashboard.sx"

type ConfigReader interface {
	GetDashboardSXEnabled() bool
	GetDashboardSXCode() string
	GetDashboardSXIP() string
	GetDashboardSXHostname() string
}

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

type Client struct {
	ServiceURL  string
	InstanceKey string
	Code        string
	OnLog       func(msg string)
}

type StatusFunc func(status, message string)

type ServiceDNSProvider struct {
	OnStatus StatusFunc
}

func SetLogger(_ *log.Logger) {}

func GetStatus(_ ConfigReader) (*Status, error) {
	return &Status{}, nil
}

func EnsureInstanceKey() (string, error) {
	return "", fmt.Errorf("dashboardsx is not available in this build")
}

func NewClient(_, _, _ string) *Client {
	return &Client{}
}

func StartHeartbeat(_ context.Context, _ *Client) {}

func StartAutoRenewal(_ context.Context, _ *Client, _ string) {}

func DetectBindableIPs() ([]string, error) {
	return nil, fmt.Errorf("dashboardsx is not available in this build")
}

func CertPath() (string, error) {
	return "", fmt.Errorf("dashboardsx is not available in this build")
}

func KeyPath() (string, error) {
	return "", fmt.Errorf("dashboardsx is not available in this build")
}

func NewServiceDNSProvider(_ *Client) (*ServiceDNSProvider, error) {
	return nil, fmt.Errorf("dashboardsx is not available in this build")
}

func ProvisionCert(_, _ string, _ bool, _ challenge.Provider, _ StatusFunc) error {
	return fmt.Errorf("dashboardsx is not available in this build")
}

func IsAvailable() bool { return false }
