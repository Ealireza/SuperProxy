package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to YAML config file")
	testConfig := flag.Bool("t", false, "test configuration and exit")
	flag.Parse()

	// Load configuration
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		if *testConfig {
			fmt.Fprintf(os.Stderr, "configuration test FAILED: %v\n", err)
			os.Exit(1)
		}
		log.Fatalf("[main] %v", err)
	}

	// Config test mode: validate and exit
	if *testConfig {
		fmt.Printf("configuration file %s test OK\n", *configPath)
		fmt.Printf("  interface: %s\n", cfg.Interface)
		fmt.Printf("  proxies:   %d\n", len(cfg.Proxies))
		for _, entry := range cfg.Proxies {
			fmt.Printf("    socks5://0.0.0.0:%-5d → %s\n", entry.Port, entry.IPv6)
		}
		os.Exit(0)
	}

	log.Printf("[main] loaded %d proxy entries from %s", len(cfg.Proxies), *configPath)
	log.Printf("[main] interface: %s", cfg.Interface)
	log.Printf("[main] GOMAXPROCS: %d", runtime.GOMAXPROCS(0))

	// Auto-assign IPv6 addresses to the network interface
	if runtime.GOOS == "linux" {
		if err := EnsureIPv6Addresses(cfg.Interface, cfg.Proxies); err != nil {
			log.Fatalf("[main] failed to ensure IPv6 addresses: %v", err)
		}
	} else {
		log.Printf("[main] skipping IPv6 address assignment (not Linux)")
	}

	// Start all proxy listeners
	errCh := make(chan error, len(cfg.Proxies))
	for _, entry := range cfg.Proxies {
		entry := entry // capture for goroutine
		go func() {
			if err := StartProxy(entry); err != nil {
				errCh <- fmt.Errorf("proxy %s:%d: %w", entry.IPv6, entry.Port, err)
			}
		}()
	}

	// Print startup summary
	log.Println("[main] ─────────────────────────────────────")
	for _, entry := range cfg.Proxies {
		log.Printf("[main]   socks5://0.0.0.0:%-5d → %s", entry.Port, entry.IPv6)
	}
	log.Println("[main] ─────────────────────────────────────")
	log.Println("[main] all proxies running. Press Ctrl+C to stop.")

	// Wait for shutdown signal or fatal error
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("[main] received signal %s, shutting down...", sig)
	case err := <-errCh:
		log.Fatalf("[main] fatal: %v", err)
	}
}
