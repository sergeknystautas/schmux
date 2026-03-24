//go:build !nodashboardsx

package dashboardsx

import (
	"net"
	"testing"
)

func TestIsUsableIP(t *testing.T) {
	tests := []struct {
		name   string
		ip     string
		usable bool
	}{
		// Should be usable
		{"10.0.0.1", "10.0.0.1", true},
		{"172.16.0.1", "172.16.0.1", true},
		{"192.168.0.1", "192.168.0.1", true},
		{"100.64.0.1", "100.64.0.1", true},           // CGNAT (Tailscale)
		{"100.100.100.100", "100.100.100.100", true}, // Tailscale
		{"8.8.8.8", "8.8.8.8", true},                 // Public IP
		{"1.1.1.1", "1.1.1.1", true},                 // Public IP
		// Should not be usable
		{"127.0.0.1", "127.0.0.1", false},             // Loopback
		{"127.255.255.255", "127.255.255.255", false}, // Loopback
		{"169.254.0.1", "169.254.0.1", false},         // Link-local
		{"169.254.255.255", "169.254.255.255", false}, // Link-local
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %s", tt.ip)
			}
			got := IsUsableIP(ip)
			if got != tt.usable {
				t.Errorf("IsUsableIP(%s) = %v, want %v", tt.ip, got, tt.usable)
			}
		})
	}
}

func TestIsUsableIP_IPv6(t *testing.T) {
	// IPv6 addresses should return false (no IPv4 representation)
	ip := net.ParseIP("::1")
	if IsUsableIP(ip) {
		t.Error("IsUsableIP(::1) = true, want false")
	}
}

func TestDetectBindableIPs(t *testing.T) {
	ips, err := DetectBindableIPs()
	if err != nil {
		t.Fatalf("DetectBindableIPs() error: %v", err)
	}
	// We can't assert specific IPs since this depends on the machine,
	// but we can verify the function doesn't crash and returns valid IPs
	for _, ip := range ips {
		parsed := net.ParseIP(ip)
		if parsed == nil {
			t.Errorf("DetectBindableIPs() returned invalid IP: %s", ip)
		}
		if !IsUsableIP(parsed) {
			t.Errorf("DetectBindableIPs() returned non-usable IP: %s", ip)
		}
	}
}
