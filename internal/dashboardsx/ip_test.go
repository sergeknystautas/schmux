package dashboardsx

import (
	"net"
	"testing"
)

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		private bool
	}{
		{"10.0.0.1", "10.0.0.1", true},
		{"10.255.255.255", "10.255.255.255", true},
		{"172.16.0.1", "172.16.0.1", true},
		{"172.31.255.255", "172.31.255.255", true},
		{"172.15.255.255", "172.15.255.255", false},
		{"172.32.0.1", "172.32.0.1", false},
		{"192.168.0.1", "192.168.0.1", true},
		{"192.168.1.100", "192.168.1.100", true},
		{"192.169.0.1", "192.169.0.1", false},
		{"8.8.8.8", "8.8.8.8", false},
		{"1.1.1.1", "1.1.1.1", false},
		{"127.0.0.1", "127.0.0.1", false},
		{"0.0.0.0", "0.0.0.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %s", tt.ip)
			}
			got := IsPrivateIP(ip)
			if got != tt.private {
				t.Errorf("IsPrivateIP(%s) = %v, want %v", tt.ip, got, tt.private)
			}
		})
	}
}

func TestIsPrivateIP_IPv6(t *testing.T) {
	// IPv6 addresses should return false (no IPv4 representation)
	ip := net.ParseIP("::1")
	if IsPrivateIP(ip) {
		t.Error("IsPrivateIP(::1) = true, want false")
	}
}

func TestDetectPrivateIPs(t *testing.T) {
	ips, err := DetectPrivateIPs()
	if err != nil {
		t.Fatalf("DetectPrivateIPs() error: %v", err)
	}
	// We can't assert specific IPs since this depends on the machine,
	// but we can verify the function doesn't crash and returns valid IPs
	for _, ip := range ips {
		parsed := net.ParseIP(ip)
		if parsed == nil {
			t.Errorf("DetectPrivateIPs() returned invalid IP: %s", ip)
		}
		if !IsPrivateIP(parsed) {
			t.Errorf("DetectPrivateIPs() returned non-private IP: %s", ip)
		}
	}
}
