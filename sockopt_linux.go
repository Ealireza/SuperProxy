// +build linux

package main

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// setSocketOptions configures TCP performance options on the raw socket fd.
// Called via net.Dialer.Control before connect(2).
func setSocketOptions(network, address string, c syscall.RawConn) error {
	var sysErr error
	err := c.Control(func(fd uintptr) {
		// Allow address reuse for rapid restart
		if e := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); e != nil {
			sysErr = e
			return
		}

		// Disable Nagle's algorithm for lower latency
		if e := unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_NODELAY, 1); e != nil {
			sysErr = e
			return
		}

		// Enable TCP keepalive
		if e := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_KEEPALIVE, 1); e != nil {
			sysErr = e
			return
		}

		// Keepalive idle time: 30 seconds
		if e := unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_KEEPIDLE, 30); e != nil {
			sysErr = e
			return
		}

		// Keepalive interval: 10 seconds
		if e := unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_KEEPINTVL, 10); e != nil {
			sysErr = e
			return
		}

		// Keepalive probes: 3
		if e := unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_KEEPCNT, 3); e != nil {
			sysErr = e
			return
		}
	})
	if err != nil {
		return err
	}
	return sysErr
}
