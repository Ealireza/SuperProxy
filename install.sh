#!/usr/bin/env bash
#
# SuperProxy — Build, Install & Service Setup Script
# Supports: RHEL 9 / CentOS 9 / AlmaLinux 9 / Rocky Linux 9 / Ubuntu 22.04+
#
# Usage:
#   chmod +x install.sh
#   sudo ./install.sh
#
set -euo pipefail

# ─── Configuration ────────────────────────────────────────────────────────────
BINARY_NAME="superproxy"
INSTALL_DIR="/usr/superproxy"
CONFIG_DIR="/etc/superproxy"
CONFIG_FILE="${CONFIG_DIR}/config.yaml"
SERVICE_NAME="superproxy"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
GO_MIN_VERSION="1.21"
GO_INSTALL_VERSION="1.23.6"  # version to install if Go is missing or too old

# ─── Colors ───────────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

info()  { echo -e "${CYAN}[INFO]${NC}  $*"; }
ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
fail()  { echo -e "${RED}[FAIL]${NC}  $*"; exit 1; }

# ─── Root check ───────────────────────────────────────────────────────────────
if [[ $EUID -ne 0 ]]; then
    fail "This script must be run as root (sudo ./install.sh)"
fi

# ─── Detect OS ────────────────────────────────────────────────────────────────
detect_os() {
    if [[ -f /etc/os-release ]]; then
        . /etc/os-release
        OS_ID="${ID}"
        OS_VERSION="${VERSION_ID}"
        OS_NAME="${PRETTY_NAME}"
    else
        fail "Cannot detect OS: /etc/os-release not found"
    fi

    case "${OS_ID}" in
        rhel|centos|almalinux|rocky|ol)
            OS_FAMILY="rhel"
            ;;
        ubuntu|debian)
            OS_FAMILY="debian"
            ;;
        *)
            fail "Unsupported OS: ${OS_NAME} (${OS_ID}). Supported: RHEL/CentOS/AlmaLinux/Rocky/Ubuntu/Debian"
            ;;
    esac

    info "Detected OS: ${OS_NAME} (family: ${OS_FAMILY})"
}

# ─── Install system dependencies ─────────────────────────────────────────────
install_deps() {
    info "Installing build dependencies..."

    case "${OS_FAMILY}" in
        rhel)
            dnf install -y gcc make wget tar gzip iproute >/dev/null 2>&1 || \
            yum install -y gcc make wget tar gzip iproute >/dev/null 2>&1
            ;;
        debian)
            export DEBIAN_FRONTEND=noninteractive
            apt-get update -qq >/dev/null 2>&1
            apt-get install -y -qq gcc make wget tar gzip iproute2 >/dev/null 2>&1
            ;;
    esac

    ok "System dependencies installed"
}

# ─── Go version check / install ──────────────────────────────────────────────
version_ge() {
    # Returns 0 if $1 >= $2 (semantic version compare)
    printf '%s\n%s' "$2" "$1" | sort -V -C
}

ensure_go() {
    local go_bin=""

    # Check if Go is already installed
    if command -v go &>/dev/null; then
        go_bin="$(command -v go)"
        local current_version
        current_version="$(go version | grep -oP 'go(\d+\.\d+(\.\d+)?)' | sed 's/go//')"
        if version_ge "${current_version}" "${GO_MIN_VERSION}"; then
            ok "Go ${current_version} found at ${go_bin} (>= ${GO_MIN_VERSION})"
            return
        else
            warn "Go ${current_version} is too old (need >= ${GO_MIN_VERSION}), installing ${GO_INSTALL_VERSION}..."
        fi
    else
        info "Go not found, installing ${GO_INSTALL_VERSION}..."
    fi

    # Detect architecture
    local arch
    case "$(uname -m)" in
        x86_64)  arch="amd64" ;;
        aarch64) arch="arm64" ;;
        *)       fail "Unsupported architecture: $(uname -m)" ;;
    esac

    local go_tarball="go${GO_INSTALL_VERSION}.linux-${arch}.tar.gz"
    local go_url="https://go.dev/dl/${go_tarball}"

    info "Downloading Go ${GO_INSTALL_VERSION} for linux/${arch}..."
    wget -q -O "/tmp/${go_tarball}" "${go_url}" || fail "Failed to download Go"

    # Remove old Go installation if exists
    rm -rf /usr/local/go

    info "Extracting Go to /usr/local/go..."
    tar -C /usr/local -xzf "/tmp/${go_tarball}"
    rm -f "/tmp/${go_tarball}"

    # Add to PATH for this session
    export PATH="/usr/local/go/bin:${PATH}"

    # Ensure it persists
    if ! grep -q '/usr/local/go/bin' /etc/profile.d/go.sh 2>/dev/null; then
        echo 'export PATH="/usr/local/go/bin:${PATH}"' > /etc/profile.d/go.sh
        chmod +x /etc/profile.d/go.sh
    fi

    ok "Go ${GO_INSTALL_VERSION} installed at /usr/local/go/bin/go"
}

# ─── Build ────────────────────────────────────────────────────────────────────
build_binary() {
    local src_dir
    src_dir="$(cd "$(dirname "$0")" && pwd)"

    info "Building ${BINARY_NAME} from ${src_dir}..."

    cd "${src_dir}"

    # Ensure modules are tidy
    go mod tidy

    # Build with optimizations
    CGO_ENABLED=0 go build \
        -ldflags="-s -w" \
        -trimpath \
        -o "${BINARY_NAME}" \
        .

    ok "Built ${BINARY_NAME} ($(du -h ${BINARY_NAME} | cut -f1) stripped)"
}

# ─── Install ──────────────────────────────────────────────────────────────────
install_binary() {
    info "Installing to ${INSTALL_DIR}..."

    mkdir -p "${INSTALL_DIR}"
    cp -f "${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
    chmod 755 "${INSTALL_DIR}/${BINARY_NAME}"

    ok "Binary installed: ${INSTALL_DIR}/${BINARY_NAME}"
}

install_config() {
    mkdir -p "${CONFIG_DIR}"

    if [[ -f "${CONFIG_FILE}" ]]; then
        warn "Config already exists at ${CONFIG_FILE}, not overwriting"
        warn "New example saved to ${CONFIG_FILE}.new"
        cp -f config.yaml "${CONFIG_FILE}.new"
    else
        cp -f config.yaml "${CONFIG_FILE}"
        chmod 644 "${CONFIG_FILE}"
        ok "Config installed: ${CONFIG_FILE}"
    fi
}

install_service() {
    info "Installing systemd service..."

    cat > "${SERVICE_FILE}" << 'UNIT'
[Unit]
Description=SuperProxy — High-Performance IPv6 SOCKS5 Proxy Pool
Documentation=https://github.com/your-org/go-proxy-ipv6-pool
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/superproxy/superproxy -config /etc/superproxy/config.yaml
WorkingDirectory=/usr/superproxy

# Restart policy
Restart=always
RestartSec=3
StartLimitIntervalSec=60
StartLimitBurst=5

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadOnlyPaths=/etc/superproxy
PrivateTmp=true

# Capabilities: bind low ports + manage network interfaces (ip addr add)
AmbientCapabilities=CAP_NET_BIND_SERVICE CAP_NET_ADMIN
CapabilityBoundingSet=CAP_NET_BIND_SERVICE CAP_NET_ADMIN

# Performance: allow lots of connections
LimitNOFILE=1048576
LimitNPROC=65535

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=superproxy

[Install]
WantedBy=multi-user.target
UNIT

    # Reload systemd
    systemctl daemon-reload

    ok "Service installed: ${SERVICE_FILE}"
}

# ─── Enable & start ──────────────────────────────────────────────────────────
enable_service() {
    info "Enabling and starting ${SERVICE_NAME}..."

    systemctl enable "${SERVICE_NAME}" 2>/dev/null
    ok "Service enabled (will start on boot)"

    echo ""
    echo -e "${CYAN}═══════════════════════════════════════════════════════════════${NC}"
    echo -e "${GREEN} SuperProxy installed successfully!${NC}"
    echo -e "${CYAN}═══════════════════════════════════════════════════════════════${NC}"
    echo ""
    echo -e "  Binary:   ${INSTALL_DIR}/${BINARY_NAME}"
    echo -e "  Config:   ${CONFIG_FILE}"
    echo -e "  Service:  ${SERVICE_FILE}"
    echo ""
    echo -e "  ${YELLOW}Edit your config first:${NC}"
    echo -e "    nano ${CONFIG_FILE}"
    echo ""
    echo -e "  ${YELLOW}Then start the service:${NC}"
    echo -e "    systemctl start ${SERVICE_NAME}"
    echo -e "    systemctl status ${SERVICE_NAME}"
    echo -e "    journalctl -u ${SERVICE_NAME} -f"
    echo ""
    echo -e "  ${YELLOW}Test a proxy:${NC}"
    echo -e "    curl -x socks5://127.0.0.1:<PORT> http://ifconfig.co"
    echo ""
}

# ─── Main ─────────────────────────────────────────────────────────────────────
main() {
    echo ""
    echo -e "${CYAN}╔═══════════════════════════════════════════════════════════╗${NC}"
    echo -e "${CYAN}║     SuperProxy — Build & Install Script                  ║${NC}"
    echo -e "${CYAN}║     High-Performance IPv6 SOCKS5 Proxy Pool              ║${NC}"
    echo -e "${CYAN}╚═══════════════════════════════════════════════════════════╝${NC}"
    echo ""

    detect_os
    install_deps
    ensure_go
    build_binary
    install_binary
    install_config
    install_service
    enable_service
}

main "$@"
