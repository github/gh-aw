#!/usr/bin/env bash
# Install GitHub Copilot CLI with retry logic
# Usage: install_copilot_cli.sh [VERSION]
#
# This script downloads and installs the GitHub Copilot CLI from the official
# installer script with retry logic to handle transient network failures.
#
# Arguments:
#   VERSION - Optional Copilot CLI version to install (default: latest from installer)
#
# Features:
#   - Retries download up to 3 times with exponential backoff
#   - Verifies installation after completion
#   - Downloads to temp file for security
#   - Cleans up temp files after installation

set -euo pipefail

# Configuration
VERSION="${1:-}"
INSTALLER_URL="https://raw.githubusercontent.com/github/copilot-cli/main/install.sh"
INSTALLER_TEMP="/tmp/copilot-install.sh"
MAX_ATTEMPTS=3
COPILOT_DIR="/home/runner/.copilot"

# Fix directory ownership before installation
# This is needed because a previous AWF run on the same runner may have used
# `sudo -E awf --enable-chroot ...`, which creates the .copilot directory with
# root ownership. The Copilot CLI (running as the runner user) then fails when
# trying to create subdirectories. See: https://github.com/github/gh-aw/issues/12066
echo "Ensuring correct ownership of $COPILOT_DIR..."
mkdir -p "$COPILOT_DIR"
sudo chown -R runner:runner "$COPILOT_DIR"

# Function to download installer with retry logic
download_installer_with_retry() {
  local attempt=1
  local wait_time=5
  
  while [ $attempt -le $MAX_ATTEMPTS ]; do
    echo "Attempt $attempt of $MAX_ATTEMPTS: Downloading Copilot CLI installer..."
    
    if curl -fsSL "$INSTALLER_URL" -o "$INSTALLER_TEMP" 2>&1; then
      echo "Successfully downloaded installer"
      return 0
    fi
    
    if [ $attempt -lt $MAX_ATTEMPTS ]; then
      echo "Failed to download installer. Retrying in ${wait_time}s..."
      sleep $wait_time
      wait_time=$((wait_time * 2))  # Exponential backoff
    else
      echo "ERROR: Failed to download installer after $MAX_ATTEMPTS attempts"
      return 1
    fi
    attempt=$((attempt + 1))
  done
}

# Main installation flow
echo "Installing GitHub Copilot CLI${VERSION:+ version $VERSION}..."

# Download installer with retry logic
if ! download_installer_with_retry; then
  echo "ERROR: Could not download Copilot CLI installer"
  exit 1
fi

# Execute the installer with the specified version
# Pass VERSION directly to sudo to ensure it's available to the installer script
if [ -n "$VERSION" ]; then
  echo "Installing Copilot CLI version $VERSION..."
  sudo VERSION="$VERSION" bash "$INSTALLER_TEMP"
else
  echo "Installing latest Copilot CLI version..."
  sudo bash "$INSTALLER_TEMP"
fi

# Cleanup temp file
rm -f "$INSTALLER_TEMP"

# Verify installation
echo "Verifying Copilot CLI installation..."
if command -v copilot >/dev/null 2>&1; then
  copilot --version
  echo "✓ Copilot CLI installation complete"
else
  echo "ERROR: Copilot CLI installation failed - command not found"
  exit 1
fi
