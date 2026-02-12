package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// SOCKS5 constants (RFC 1928)
const (
	socks5Version = 0x05

	authNone = 0x00
	authNoAcceptable = 0xFF

	cmdConnect = 0x01

	atypIPv4   = 0x01
	atypDomain = 0x03
	atypIPv6   = 0x04

	repSuccess             = 0x00
	repGeneralFailure      = 0x01
	repConnectionNotAllowed = 0x02
	repNetworkUnreachable  = 0x03
	repHostUnreachable     = 0x04
	repConnectionRefused   = 0x05
	repCommandNotSupported = 0x07
	repAddrTypeNotSupported = 0x08
)

// bufPool is a lock-free pool of 32 KiB buffers for relay.
// On Linux with two *net.TCPConn, io.Copy uses splice(2) and this pool
// is only the fallback path.
var bufPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 32*1024)
		return &buf
	},
}

// StartProxy starts a SOCKS5 listener on the given port, using outboundIP
// for all outgoing connections. Blocks until the listener is closed.
func StartProxy(entry ProxyEntry) error {
	outboundIP, err := ParseIPv6(entry.IPv6)
	if err != nil {
		return fmt.Errorf("proxy %d: %w", entry.Port, err)
	}

	listenAddr := fmt.Sprintf(":%d", entry.Port)
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", listenAddr, err)
	}
	defer ln.Close()

	log.Printf("[socks5] listening on %s → outbound %s", listenAddr, outboundIP)

	for {
		conn, err := ln.Accept()
		if err != nil {
			// Check if listener was closed (graceful shutdown)
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			log.Printf("[socks5:%d] accept error: %v", entry.Port, err)
			continue
		}
		go handleConnection(conn, outboundIP, entry.Port)
	}
}

// handleConnection handles a single SOCKS5 client connection.
// All buffers are stack-allocated or pooled; no per-connection heap allocations
// on the hot path.
func handleConnection(client net.Conn, outboundIP net.IP, port int) {
	defer client.Close()

	// Set a deadline for the handshake phase only
	client.SetDeadline(time.Now().Add(10 * time.Second))

	// --- Auth negotiation ---
	// Read: VER | NMETHODS | METHODS...
	var hdr [2]byte
	if _, err := io.ReadFull(client, hdr[:]); err != nil {
		return
	}
	if hdr[0] != socks5Version {
		return
	}

	nmethods := int(hdr[1])
	if nmethods == 0 || nmethods > 255 {
		return
	}

	// Read methods into a stack buffer (max 255 bytes)
	var methodsBuf [255]byte
	methods := methodsBuf[:nmethods]
	if _, err := io.ReadFull(client, methods); err != nil {
		return
	}

	// Check if NO AUTH (0x00) is offered
	hasNoAuth := false
	for _, m := range methods {
		if m == authNone {
			hasNoAuth = true
			break
		}
	}

	if !hasNoAuth {
		// Reject: no acceptable auth method
		client.Write([]byte{socks5Version, authNoAcceptable})
		return
	}

	// Accept NO AUTH
	if _, err := client.Write([]byte{socks5Version, authNone}); err != nil {
		return
	}

	// --- Request ---
	// Read: VER | CMD | RSV | ATYP
	var reqHdr [4]byte
	if _, err := io.ReadFull(client, reqHdr[:]); err != nil {
		return
	}
	if reqHdr[0] != socks5Version {
		return
	}

	// Only CONNECT is supported
	if reqHdr[1] != cmdConnect {
		sendReply(client, repCommandNotSupported, nil, 0)
		return
	}

	// Parse destination address
	atyp := reqHdr[3]
	var destAddr string

	switch atyp {
	case atypIPv4:
		var addr [4]byte
		if _, err := io.ReadFull(client, addr[:]); err != nil {
			return
		}
		destAddr = net.IP(addr[:]).String()

	case atypDomain:
		var domainLen [1]byte
		if _, err := io.ReadFull(client, domainLen[:]); err != nil {
			return
		}
		if domainLen[0] == 0 {
			sendReply(client, repGeneralFailure, nil, 0)
			return
		}
		var domainBuf [255]byte
		domain := domainBuf[:domainLen[0]]
		if _, err := io.ReadFull(client, domain); err != nil {
			return
		}
		destAddr = string(domain)

	case atypIPv6:
		var addr [16]byte
		if _, err := io.ReadFull(client, addr[:]); err != nil {
			return
		}
		destAddr = net.IP(addr[:]).String()

	default:
		sendReply(client, repAddrTypeNotSupported, nil, 0)
		return
	}

	// Read destination port (2 bytes, big-endian)
	var portBuf [2]byte
	if _, err := io.ReadFull(client, portBuf[:]); err != nil {
		return
	}
	destPort := binary.BigEndian.Uint16(portBuf[:])

	target := net.JoinHostPort(destAddr, strconv.Itoa(int(destPort)))

	// --- Dial outbound ---
	dialer := net.Dialer{
		LocalAddr: &net.TCPAddr{IP: outboundIP},
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   setSocketOptions,
	}

	remote, err := dialer.Dial("tcp", target)
	if err != nil {
		rep := repGeneralFailure
		if errors.Is(err, syscall.ECONNREFUSED) {
			rep = repConnectionRefused
		} else if errors.Is(err, syscall.ENETUNREACH) {
			rep = repNetworkUnreachable
		} else if errors.Is(err, syscall.EHOSTUNREACH) {
			rep = repHostUnreachable
		}
		sendReply(client, byte(rep), nil, 0)
		return
	}
	defer remote.Close()

	// Get the bound address for the reply
	boundAddr := remote.LocalAddr().(*net.TCPAddr)
	sendReply(client, repSuccess, boundAddr.IP, uint16(boundAddr.Port))

	// Clear deadlines for the relay phase
	client.SetDeadline(time.Time{})
	remote.SetDeadline(time.Time{})

	// --- Relay (zero-copy on Linux via splice) ---
	relay(client, remote)
}

// sendReply sends a SOCKS5 reply to the client.
func sendReply(conn net.Conn, rep byte, bindIP net.IP, bindPort uint16) {
	// VER | REP | RSV | ATYP | BND.ADDR | BND.PORT
	var buf [22]byte // max: 4 + 16 (IPv6) + 2 = 22
	buf[0] = socks5Version
	buf[1] = rep
	buf[2] = 0x00 // RSV

	n := 4
	if bindIP != nil {
		if v4 := bindIP.To4(); v4 != nil {
			buf[3] = atypIPv4
			copy(buf[4:8], v4)
			n = 8
		} else {
			buf[3] = atypIPv6
			copy(buf[4:20], bindIP.To16())
			n = 20
		}
	} else {
		buf[3] = atypIPv4
		// 0.0.0.0
		n = 8
	}
	binary.BigEndian.PutUint16(buf[n:n+2], bindPort)
	n += 2

	conn.Write(buf[:n])
}

// relay copies data bidirectionally between client and remote.
// On Linux, when both sides are *net.TCPConn, Go's io.Copy uses splice(2)
// for zero-copy kernel-to-kernel data transfer.
func relay(client, remote net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	// client → remote
	go func() {
		defer wg.Done()
		copyAndClose(remote, client)
	}()

	// remote → client
	go func() {
		defer wg.Done()
		copyAndClose(client, remote)
	}()

	wg.Wait()
}

// copyAndClose copies from src to dst, then signals write-done via CloseWrite.
// Uses pooled buffers as fallback when splice is not available.
func copyAndClose(dst, src net.Conn) {
	bufp := bufPool.Get().(*[]byte)
	defer bufPool.Put(bufp)

	io.CopyBuffer(dst, src, *bufp)

	// Graceful half-close: signal that no more data will be written
	if tc, ok := dst.(*net.TCPConn); ok {
		tc.CloseWrite()
	}
	if tc, ok := src.(*net.TCPConn); ok {
		tc.CloseRead()
	}
}
