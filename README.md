# SuperProxy

High-performance, zero-copy **SOCKS5 proxy pool** with dedicated IPv6 outbound addresses.  
Each proxy listener binds to its own port and routes all traffic through a specific IPv6. Missing addresses are auto-provisioned on the NIC at startup.

---

## Features

| Feature | Detail |
|---------|--------|
| **Multi-listener SOCKS5** | One SOCKS5 proxy per IPv6+port pair, all from a single binary |
| **Auto IPv6 provisioning** | Adds missing `<ipv6>/128` to your NIC via `ip addr add` at startup |
| **Zero-copy relay** | Linux `splice(2)` — data moves kernel-to-kernel, never touches userspace |
| **Zero allocations** | `sync.Pool` buffers + stack-allocated SOCKS5 handshake, no GC pressure |
| **No CGo, no deps** | Hand-rolled SOCKS5 (RFC 1928 CONNECT), pure Go, static binary |
| **TCP tuning** | `TCP_NODELAY`, `SO_KEEPALIVE`, `SO_REUSEADDR` via raw syscalls |
| **Async / non-blocking** | Go epoll netpoller handles thousands of concurrent connections |
| **Config test mode** | `superproxy -t` validates config without starting (like `nginx -t`) |
| **Graceful shutdown** | Clean `SIGINT`/`SIGTERM` handling |
| **systemd ready** | Hardened unit file with `CAP_NET_ADMIN`, `LimitNOFILE=1M` |

---

## Quick Install (RHEL 9 / Ubuntu 22.04+)

```bash
git clone https://github.com/your-org/go-proxy-ipv6-pool.git
cd go-proxy-ipv6-pool
chmod +x install.sh
sudo ./install.sh
```

### What `install.sh` does

1. **Detects OS** — RHEL / CentOS / AlmaLinux / Rocky / Ubuntu / Debian
2. **Installs dependencies** — `gcc`, `make`, `wget`, `iproute` / `iproute2`
3. **Installs Go** — downloads Go 1.23 if missing or outdated (supports `amd64` + `arm64`)
4. **Compiles** — `CGO_ENABLED=0 go build -ldflags="-s -w" -trimpath` (static, stripped)
5. **Installs binary** → `/usr/superproxy/superproxy`
6. **Installs config** → `/etc/superproxy/config.yaml`
7. **Installs & enables systemd service** → `/etc/systemd/system/superproxy.service`

> [!NOTE]
> If `/etc/superproxy/config.yaml` already exists, it is **not overwritten**. The new example is saved as `config.yaml.new`.

---

## Configuration

Edit `/etc/superproxy/config.yaml`:

```yaml
# Network interface for IPv6 address assignment
interface: eth0

proxies:
  # Each entry = one SOCKS5 listener
  # Listens on 0.0.0.0:<port>, outbound via <ipv6>
  - ipv6: "2001:db8::1"
    port: 10001

  - ipv6: "2001:db8::2"
    port: 10002

  - ipv6: "2001:db8::3"
    port: 10003

  - ipv6: "2001:db8::4"
    port: 10004
```

### Config fields

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `interface` | string | ✅ | NIC name where IPv6 addresses are assigned (e.g. `eth0`, `ens3`) |
| `proxies` | list | ✅ | One or more proxy entries |
| `proxies[].ipv6` | string | ✅ | IPv6 address for outbound (auto-added to NIC if missing) |
| `proxies[].port` | int | ✅ | Listen port, range 1–65535 |

### Validation rules

- IPv6 must be valid and not IPv4
- Ports must be unique
- IPv6 addresses must be unique
- Interface name must be non-empty

---

## CLI Reference

```
superproxy [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `-config <path>` | `config.yaml` | Path to YAML configuration file |
| `-t` | — | Test configuration and exit (like `nginx -t`) |

### Examples

```bash
# Start the proxy
superproxy -config /etc/superproxy/config.yaml

# Test configuration without starting
superproxy -t -config /etc/superproxy/config.yaml
# Output:
#   configuration file /etc/superproxy/config.yaml test OK
#     interface: eth0
#     proxies:   4
#       socks5://0.0.0.0:10001 → 2001:db8::1
#       socks5://0.0.0.0:10002 → 2001:db8::2
#       socks5://0.0.0.0:10003 → 2001:db8::3
#       socks5://0.0.0.0:10004 → 2001:db8::4

# Test with bad config (exits with code 1)
superproxy -t -config broken.yaml
# Output:
#   configuration test FAILED: config: proxies[2]: invalid IP address "bad"
```

---

## Service Management

```bash
# Start / stop / restart
sudo systemctl start superproxy
sudo systemctl stop superproxy
sudo systemctl restart superproxy

# Status
sudo systemctl status superproxy

# Live logs
journalctl -u superproxy -f

# Logs since last boot
journalctl -u superproxy -b
```

### Service hardening (built-in)

The systemd unit includes:

| Setting | Value |
|---------|-------|
| `Restart` | `always` (3s delay, max 5/min) |
| `LimitNOFILE` | `1048576` (1M open files) |
| `LimitNPROC` | `65535` |
| `AmbientCapabilities` | `CAP_NET_BIND_SERVICE` + `CAP_NET_ADMIN` |
| `ProtectSystem` | `strict` |
| `ProtectHome` | `true` |
| `NoNewPrivileges` | `true` |
| `PrivateTmp` | `true` |

---

## Testing a Proxy

```bash
# Test proxy on port 10001 — should return the first IPv6 address
curl -x socks5://127.0.0.1:10001 http://ifconfig.co

# Test proxy on port 10002 — should return the second IPv6 address
curl -x socks5://127.0.0.1:10002 http://ifconfig.co

# HTTPS also works (SOCKS5 CONNECT)
curl -x socks5h://127.0.0.1:10001 https://ifconfig.co
```

---

## Architecture

```
                          ┌─────────────────────────────────────────┐
                          │            SuperProxy Process           │
                          │                                         │
  Client ──► port:10001 ──┤  SOCKS5 handshake (stack-allocated)     │
  Client ──► port:10002 ──┤         │                               │
  Client ──► port:10003 ──┤     splice(2) relay ← zero-copy        │
  Client ──► port:10004 ──┤         │                               │
                          │  net.Dialer{LocalAddr: IPv6_N}          │
                          │  TCP_NODELAY │ SO_KEEPALIVE              │
                          └──────┬──────────────────────────────────┘
                                 │
                    ┌────────────┼────────────┐
                    ▼            ▼            ▼
              2001:db8::1  2001:db8::2  2001:db8::3  ...
                    │            │            │
                    └────────────┼────────────┘
                                 │
                            Internet
```

### Performance design

| Aspect | Implementation |
|--------|----------------|
| **I/O model** | Go netpoller (`epoll` on Linux) — fully async, non-blocking |
| **Data relay** | `splice(2)` via `io.Copy` on `*net.TCPConn` — zero userspace copy |
| **Buffers** | `sync.Pool` of 32 KiB — lock-free, no GC pressure |
| **Concurrency** | One goroutine per connection, no shared locks on hot path |
| **SOCKS5** | Hand-rolled RFC 1928 CONNECT, fixed-size stack buffers |

---

## Manual Build

```bash
# Requirements: Go 1.21+
go build -ldflags="-s -w" -trimpath -o superproxy .

# Run directly
./superproxy -config config.yaml

# Test config
./superproxy -t -config config.yaml
```

### Cross-compile for Linux (from any OS)

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -trimpath -o superproxy .
```

---

## Uninstall

```bash
chmod +x uninstall.sh
sudo ./uninstall.sh
```

Stops the service, removes the binary from `/usr/superproxy/`, removes the systemd unit.  
Config at `/etc/superproxy/` is **preserved** — delete manually if no longer needed.

---

## File Structure

```
go-proxy-ipv6-pool/
├── main.go            # Entrypoint, CLI flags, graceful shutdown
├── config.go          # YAML config loader + validation
├── proxy.go           # SOCKS5 server + zero-copy relay
├── ipv6.go            # IPv6 parsing utilities
├── netif.go           # Auto IPv6/128 provisioning on NIC
├── sockopt_linux.go   # Linux TCP socket options (TCP_NODELAY, keepalive)
├── sockopt_other.go   # No-op stub for non-Linux builds
├── config.yaml        # Example configuration
├── install.sh         # Build + install + systemd setup script
├── uninstall.sh       # Clean uninstall script
├── go.mod             # Go module (Go 1.21, yaml.v3, x/sys)
├── README.md          # This file
└── LICENSE            # MIT
```

---

## Requirements

| Component | Minimum |
|-----------|---------|
| **OS** | RHEL 9 / CentOS 9 / AlmaLinux 9 / Rocky 9 / Ubuntu 22.04+ |
| **Kernel** | 5.14+ (for splice optimization) |
| **Go** | 1.21+ (auto-installed by `install.sh`) |
| **Privileges** | Root or `CAP_NET_ADMIN` (for `ip addr add`) |

---

## License

MIT License — see [LICENSE](LICENSE)
