package main

import (
	"fmt"
	"net"
)

// ParseIPv6 validates that s is a valid IPv6 address (not CIDR, not v4).
// Returns the parsed net.IP or an error.
func ParseIPv6(s string) (net.IP, error) {
	ip := net.ParseIP(s)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address: %q", s)
	}
	if ip.To4() != nil {
		return nil, fmt.Errorf("expected IPv6, got IPv4: %q", s)
	}
	return ip, nil
}
