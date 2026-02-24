package dashboardsx

import (
	"net"
)

// IsPrivateIP checks if an IP address is in a private network range.
// Private ranges: 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16.
func IsPrivateIP(ip net.IP) bool {
	ip = ip.To4()
	if ip == nil {
		return false
	}

	// 10.0.0.0/8
	if ip[0] == 10 {
		return true
	}
	// 172.16.0.0/12
	if ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31 {
		return true
	}
	// 192.168.0.0/16
	if ip[0] == 192 && ip[1] == 168 {
		return true
	}
	return false
}

// DetectPrivateIPs enumerates network interfaces and returns all private IPv4 addresses.
func DetectPrivateIPs() ([]string, error) {
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
			if IsPrivateIP(ip) {
				ips = append(ips, ip.String())
			}
		}
	}

	return ips, nil
}
