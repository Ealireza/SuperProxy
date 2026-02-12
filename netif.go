package main

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"strings"
)

// EnsureIPv6Addresses checks each proxy's IPv6 against the network interface.
// If an address is not assigned, it adds it with /128 prefix using "ip addr add".
// This function is idempotent — already-assigned addresses are silently skipped.
func EnsureIPv6Addresses(iface string, entries []ProxyEntry) error {
	// Verify interface exists
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return fmt.Errorf("interface %q: %w", iface, err)
	}

	// Get currently assigned addresses
	addrs, err := ifi.Addrs()
	if err != nil {
		return fmt.Errorf("list addresses on %q: %w", iface, err)
	}

	// Build a set of existing IPs (normalized strings)
	existing := make(map[string]struct{}, len(addrs))
	for _, a := range addrs {
		// a.String() returns "ip/mask" for *net.IPNet or "ip" for *net.IPAddr
		ipStr := a.String()
		if idx := strings.IndexByte(ipStr, '/'); idx != -1 {
			ipStr = ipStr[:idx]
		}
		ip := net.ParseIP(ipStr)
		if ip != nil {
			existing[ip.String()] = struct{}{}
		}
	}

	for _, entry := range entries {
		ip, err := ParseIPv6(entry.IPv6)
		if err != nil {
			return fmt.Errorf("invalid IPv6 %q: %w", entry.IPv6, err)
		}

		normalized := ip.String()
		if _, ok := existing[normalized]; ok {
			log.Printf("[netif] %s already assigned on %s, skipping", normalized, iface)
			continue
		}

		// Add the address with /128
		addr := normalized + "/128"
		cmd := exec.Command("ip", "addr", "add", addr, "dev", iface)
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Check if the error is "already exists" (race condition)
			if strings.Contains(string(output), "RTNETLINK answers: File exists") {
				log.Printf("[netif] %s already exists on %s (concurrent add), skipping", normalized, iface)
				continue
			}
			return fmt.Errorf("ip addr add %s dev %s: %s: %w", addr, iface, strings.TrimSpace(string(output)), err)
		}

		log.Printf("[netif] added %s to %s", addr, iface)
	}

	return nil
}
