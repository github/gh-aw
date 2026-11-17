#!/usr/bin/env bash
set +o histexpand

# Install or upgrade GitHub CLI (gh)
#
# This script installs or upgrades the GitHub CLI using the official apt
# repository on Debian/Ubuntu systems. It is idempotent: running it on a
# system where gh is already present upgrades it to the latest available
# version from the official repository.
#
# Supported platforms:
#   - Linux (Debian/Ubuntu) x64 and arm64

set -euo pipefail

if command -v gh &>/dev/null; then
  echo "gh CLI is already installed: $(gh --version | head -1), checking for upgrade..."
fi

OS="$(uname -s)"

if [ "$OS" != "Linux" ]; then
  echo "::error::Unsupported operating system: ${OS}. This script only supports Linux (Debian/Ubuntu)."
  exit 1
fi

if ! command -v apt-get &>/dev/null; then
  echo "::error::apt-get is not available. This script requires a Debian/Ubuntu system."
  exit 1
fi

# Update package lists once (also installs curl if missing)
echo "Updating package lists..."
sudo apt-get update -qq

# Install curl if missing (needed to fetch the signing key)
if ! command -v curl &>/dev/null; then
  echo "curl not found, installing..."
  sudo apt-get install -y curl
fi

# Add the GitHub CLI apt repository
KEYRING_PATH="/usr/share/keyrings/githubcli-archive-keyring.gpg"
SOURCE_LIST="/etc/apt/sources.list.d/github-cli.list"

echo "Adding GitHub CLI apt repository..."
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=actions/setup/sh/release_integrity.sh
source "${SCRIPT_DIR}/release_integrity.sh"
key_file="$(mktemp "${GH_AW_RUNTIME_DIR:?}/gh-cli-key.XXXXXX")"
trap 'rm -f "$key_file"' EXIT
curl --proto '=https' --proto-redir '=https' -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
  -o "$key_file"
# Source: https://cli.github.com/packages/githubcli-archive-keyring.gpg
# Review policy recorded 2026-10-20: authenticate key rotation before updating this pin.
verify_release_digest "$key_file" 6084d5d7bd8e288441e0e94fc6275570895da18e6751f70f057485dc2d1a811b
sudo cp "$key_file" "${KEYRING_PATH}"
sudo chmod go+r "${KEYRING_PATH}"

echo "deb [arch=$(dpkg --print-architecture) signed-by=${KEYRING_PATH}] https://cli.github.com/packages stable main" \
  | sudo tee "${SOURCE_LIST}" > /dev/null

sudo apt-get update -qq
sudo apt-get install -y gh

# Verify installation
if command -v gh &>/dev/null; then
  echo "✓ gh CLI installed: $(gh --version | head -1)"
else
  echo "::error::gh CLI installation failed - command not found after install"
  exit 1
fi
