//go:build !nodashboardsx

package dashboardsx

import (
	"net"
)

// IsUsableIP checks if an IP address is usable for dashboard binding.
// Excludes loopback (127.x.x.x) and link-local (169.254.x.x) addresses.
func IsUsableIP(ip net.IP) bool {
	ip = ip.To4()
	if ip == nil {
		return false
	}

	// Exclude loopback 127.0.0.0/8
	if ip[0] == 127 {
		return false
	}
	// Exclude link-local 169.254.0.0/16
	if ip[0] == 169 && ip[1] == 254 {
		return false
	}
	return true
}

// DetectBindableIPs enumerates network interfaces and returns all usable IPv4 addresses.
func DetectBindableIPs() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	var ips []string
	for _, iface := range ifaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.To4() == nil {
				continue
			}
			if IsUsableIP(ip) {
				ips = append(ips, ip.String())
			}
		}
	}

	return ips, nil
}
