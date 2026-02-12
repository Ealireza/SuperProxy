package main

import (
	"fmt"
	"net"
	"os"

	"gopkg.in/yaml.v3"
)

// ProxyEntry defines a single SOCKS5 listener with a fixed outbound IPv6.
type ProxyEntry struct {
	IPv6 string `yaml:"ipv6"`
	Port int    `yaml:"port"`
}

// Config is the top-level YAML configuration.
type Config struct {
	Interface string       `yaml:"interface"`
	Proxies   []ProxyEntry `yaml:"proxies"`
}

// LoadConfig reads and validates the YAML configuration file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.Interface == "" {
		return nil, fmt.Errorf("config: 'interface' is required (e.g. eth0)")
	}

	if len(cfg.Proxies) == 0 {
		return nil, fmt.Errorf("config: at least one proxy entry is required")
	}

	seen := make(map[string]struct{}, len(cfg.Proxies))
	seenPorts := make(map[int]struct{}, len(cfg.Proxies))

	for i, p := range cfg.Proxies {
		// Validate IPv6
		ip := net.ParseIP(p.IPv6)
		if ip == nil {
			return nil, fmt.Errorf("config: proxies[%d]: invalid IP address %q", i, p.IPv6)
		}
		if ip.To4() != nil {
			return nil, fmt.Errorf("config: proxies[%d]: %q is IPv4, only IPv6 is supported", i, p.IPv6)
		}

		// Normalize the IPv6 string
		cfg.Proxies[i].IPv6 = ip.String()

		// Validate port
		if p.Port < 1 || p.Port > 65535 {
			return nil, fmt.Errorf("config: proxies[%d]: port %d out of range (1-65535)", i, p.Port)
		}

		// Check duplicate IPv6
		if _, ok := seen[cfg.Proxies[i].IPv6]; ok {
			return nil, fmt.Errorf("config: proxies[%d]: duplicate IPv6 %q", i, p.IPv6)
		}
		seen[cfg.Proxies[i].IPv6] = struct{}{}

		// Check duplicate port
		if _, ok := seenPorts[p.Port]; ok {
			return nil, fmt.Errorf("config: proxies[%d]: duplicate port %d", i, p.Port)
		}
		seenPorts[p.Port] = struct{}{}
	}

	return &cfg, nil
}
