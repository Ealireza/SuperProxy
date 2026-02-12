#!/usr/bin/env bash
#
# SuperProxy — Uninstall Script
#
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
NC='\033[0m'

info()  { echo -e "${CYAN}[INFO]${NC}  $*"; }
ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
fail()  { echo -e "${RED}[FAIL]${NC}  $*"; exit 1; }

if [[ $EUID -ne 0 ]]; then
    fail "This script must be run as root (sudo ./uninstall.sh)"
fi

SERVICE_NAME="superproxy"
INSTALL_DIR="/usr/superproxy"
CONFIG_DIR="/etc/superproxy"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

info "Stopping service..."
systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
systemctl disable "${SERVICE_NAME}" 2>/dev/null || true

info "Removing service file..."
rm -f "${SERVICE_FILE}"
systemctl daemon-reload

info "Removing binary..."
rm -rf "${INSTALL_DIR}"

echo ""
echo -e "${GREEN}SuperProxy uninstalled.${NC}"
echo -e "Config preserved at: ${CONFIG_DIR}"
echo -e "Remove manually if no longer needed: rm -rf ${CONFIG_DIR}"
