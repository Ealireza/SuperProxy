// +build !linux

package main

import "syscall"

// setSocketOptions is a no-op on non-Linux platforms.
// The Linux-specific version in sockopt_linux.go sets TCP_NODELAY,
// SO_REUSEADDR, and keepalive options.
func setSocketOptions(network, address string, c syscall.RawConn) error {
	return nil
}
