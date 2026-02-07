#!/usr/bin/env bash
set -euo pipefail

# ClawReach Bridge Uninstall Script

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }

echo ""
echo "ClawReach Bridge Uninstaller"
echo "============================"
echo ""

# Stop and disable systemd service
if command -v systemctl &>/dev/null; then
    if systemctl is-active --quiet clawreachbridge 2>/dev/null; then
        info "Stopping service..."
        sudo systemctl stop clawreachbridge
    fi
    if systemctl is-enabled --quiet clawreachbridge 2>/dev/null; then
        info "Disabling service..."
        sudo systemctl disable clawreachbridge
    fi
    if [ -f /etc/systemd/system/clawreachbridge.service ]; then
        info "Removing service file..."
        sudo rm /etc/systemd/system/clawreachbridge.service
        sudo systemctl daemon-reload
    fi
fi

# Remove binary
if [ -f /usr/local/bin/clawreachbridge ]; then
    info "Removing binary..."
    sudo rm /usr/local/bin/clawreachbridge
fi

# Ask about config
if [ -d /etc/clawreachbridge ]; then
    warn "Config directory /etc/clawreachbridge still exists."
    echo "  Remove it manually if no longer needed:"
    echo "  sudo rm -rf /etc/clawreachbridge"
fi

# Remove service user
if id clawreachbridge &>/dev/null; then
    info "Removing service user..."
    sudo userdel clawreachbridge 2>/dev/null || true
fi

echo ""
info "Uninstall complete."
