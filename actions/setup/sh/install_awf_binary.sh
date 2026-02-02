#!/usr/bin/env bash
# Install AWF (Agentic Workflow Firewall) binary with SHA256 checksum verification
# Usage: install_awf_binary.sh VERSION
#
# This script downloads the AWF binary directly from GitHub releases and verifies
# its SHA256 checksum before installation to protect against supply chain attacks.
#
# Arguments:
#   VERSION - AWF version to install (e.g., v0.10.0)
#
# Security features:
#   - Downloads binary directly from GitHub releases
#   - Verifies SHA256 checksum against official checksums.txt
#   - Fails fast if checksum verification fails
#   - Eliminates trust dependency on installer scripts

set -euo pipefail

# Configuration
AWF_VERSION="${1:-}"
AWF_REPO="github/gh-aw-firewall"
AWF_BINARY="awf-linux-x64"
AWF_INSTALL_DIR="/usr/local/bin"
AWF_INSTALL_NAME="awf"

if [ -z "$AWF_VERSION" ]; then
  echo "ERROR: AWF version is required"
  echo "Usage: $0 VERSION"
  exit 1
fi

echo "Installing awf binary with checksum verification (version: ${AWF_VERSION})"

# Download URLs
BASE_URL="https://github.com/${AWF_REPO}/releases/download/${AWF_VERSION}"
BINARY_URL="${BASE_URL}/${AWF_BINARY}"
CHECKSUMS_URL="${BASE_URL}/checksums.txt"

# Create temp directory
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

# Download binary and checksums
echo "Downloading binary from ${BINARY_URL}..."
curl -fsSL -o "${TEMP_DIR}/${AWF_BINARY}" "${BINARY_URL}"

echo "Downloading checksums from ${CHECKSUMS_URL}..."
curl -fsSL -o "${TEMP_DIR}/checksums.txt" "${CHECKSUMS_URL}"

# Verify checksum
echo "Verifying SHA256 checksum..."
cd "${TEMP_DIR}"
EXPECTED_CHECKSUM=$(awk -v fname="${AWF_BINARY}" '$2 == fname {print $1; exit}' checksums.txt | tr 'A-F' 'a-f')

if [ -z "$EXPECTED_CHECKSUM" ]; then
  echo "ERROR: Could not find checksum for ${AWF_BINARY} in checksums.txt"
  exit 1
fi

ACTUAL_CHECKSUM=$(sha256sum "${AWF_BINARY}" | awk '{print $1}' | tr 'A-F' 'a-f')

if [ "$EXPECTED_CHECKSUM" != "$ACTUAL_CHECKSUM" ]; then
  echo "ERROR: Checksum verification failed!"
  echo "  Expected: $EXPECTED_CHECKSUM"
  echo "  Got:      $ACTUAL_CHECKSUM"
  echo "  The downloaded file may be corrupted or tampered with"
  exit 1
fi

echo "✓ Checksum verification passed"

# Make binary executable and install
chmod +x "${AWF_BINARY}"
sudo mv "${AWF_BINARY}" "${AWF_INSTALL_DIR}/${AWF_INSTALL_NAME}"

# Verify installation
which awf
awf --version

echo "✓ AWF installation complete"
