#!/usr/bin/env bash
set -euo pipefail

# ClawReach Bridge Installation Script
# Usage: curl -fsSL https://raw.githubusercontent.com/cortexuvula/clawreachbridge/master/scripts/install.sh | bash

REPO="cortexuvula/clawreachbridge"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/clawreachbridge"
SERVICE_USER="clawreachbridge"
BINARY_NAME="clawreachbridge"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }
die()   { error "$*"; exit 1; }

# Detect OS and architecture
detect_platform() {
    OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
    ARCH="$(uname -m)"

    case "${ARCH}" in
        x86_64|amd64)  ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        armv7l)        ARCH="armv7" ;;
        *)             die "Unsupported architecture: ${ARCH}" ;;
    esac

    case "${OS}" in
        linux|darwin) ;;
        *)            die "Unsupported OS: ${OS}" ;;
    esac

    BINARY="clawreachbridge-${OS}-${ARCH}"
    info "Detected platform: ${OS}/${ARCH}"
}

# Get latest release version from GitHub
get_latest_version() {
    if command -v curl &>/dev/null; then
        VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)"
    elif command -v wget &>/dev/null; then
        VERSION="$(wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)"
    else
        die "Neither curl nor wget found. Please install one."
    fi

    if [ -z "${VERSION}" ]; then
        die "Failed to determine latest version"
    fi
    info "Latest version: ${VERSION}"
}

# Download binary
download_binary() {
    local url="https://github.com/${REPO}/releases/download/${VERSION}/${BINARY}"
    local checksum_url="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"
    local tmp_dir
    tmp_dir="$(mktemp -d)"

    info "Downloading ${BINARY}..."
    if command -v curl &>/dev/null; then
        curl -fsSL -o "${tmp_dir}/${BINARY}" "${url}"
        curl -fsSL -o "${tmp_dir}/checksums.txt" "${checksum_url}" 2>/dev/null || true
    else
        wget -q -O "${tmp_dir}/${BINARY}" "${url}"
        wget -q -O "${tmp_dir}/checksums.txt" "${checksum_url}" 2>/dev/null || true
    fi

    # Verify checksum if available
    if [ -f "${tmp_dir}/checksums.txt" ] && command -v sha256sum &>/dev/null; then
        info "Verifying checksum..."
        (cd "${tmp_dir}" && grep "${BINARY}" checksums.txt | sha256sum -c --quiet) || die "Checksum verification failed"
        info "Checksum verified"
    fi

    # Install binary
    chmod +x "${tmp_dir}/${BINARY}"
    sudo mv "${tmp_dir}/${BINARY}" "${INSTALL_DIR}/${BINARY_NAME}"
    rm -rf "${tmp_dir}"
    info "Installed to ${INSTALL_DIR}/${BINARY_NAME}"
}

# Create config directory and example config
setup_config() {
    if [ ! -d "${CONFIG_DIR}" ]; then
        sudo mkdir -p "${CONFIG_DIR}"
        info "Created config directory: ${CONFIG_DIR}"
    fi

    if [ ! -f "${CONFIG_DIR}/config.yaml" ]; then
        info "No config yet — run 'sudo ${BINARY_NAME} setup' to create one"
    else
        warn "Config already exists at ${CONFIG_DIR}/config.yaml"
    fi
}

# Install systemd service (Linux only)
install_systemd() {
    if [ "${OS}" != "linux" ]; then
        return
    fi

    if ! command -v systemctl &>/dev/null; then
        warn "systemd not found, skipping service installation"
        return
    fi

    # Create service user if it doesn't exist
    if ! id "${SERVICE_USER}" &>/dev/null; then
        sudo useradd --system --no-create-home --shell /usr/sbin/nologin "${SERVICE_USER}" 2>/dev/null || true
        info "Created service user: ${SERVICE_USER}"
    fi

    # Generate and install service file
    "${INSTALL_DIR}/${BINARY_NAME}" systemd --print | sudo tee /etc/systemd/system/clawreachbridge.service >/dev/null
    sudo systemctl daemon-reload
    sudo systemctl enable clawreachbridge
    info "systemd service installed and enabled"
}

# Main
main() {
    echo ""
    echo "ClawReach Bridge Installer"
    echo "========================="
    echo ""

    detect_platform

    if [ "${1:-}" = "--non-interactive" ]; then
        get_latest_version
        download_binary
        setup_config
        install_systemd
    else
        get_latest_version
        download_binary
        setup_config
        install_systemd

        echo ""
        info "Installation complete!"
        "${INSTALL_DIR}/${BINARY_NAME}" version
        echo ""
        echo "Next step:"
        echo "  sudo ${BINARY_NAME} setup"
        echo ""
        echo "The setup wizard will create your config, and start the service."
    fi
}

main "$@"
