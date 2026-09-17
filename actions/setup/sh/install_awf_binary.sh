#!/usr/bin/env bash
set +o histexpand

# Install AWF (Agentic Workflow Firewall) with SHA256 checksum verification
# Usage: install_awf_binary.sh VERSION
#
# This script downloads the AWF bundle or binary from GitHub releases and verifies
# its SHA256 checksum before installation to protect against supply chain attacks.
#
# Arguments:
#   VERSION - AWF version to install (e.g., v0.25.10)
#
# Install strategy:
#   1. If Node.js >= 20 is available, download the lightweight awf-bundle.js (~357KB)
#   2. Otherwise, fall back to platform-specific pkg binary (~50MB)
#
# Platform support (fallback binary):
#   - Linux (x64, arm64): Downloads pre-built binary
#   - macOS (x64, arm64): Downloads pre-built binary
#
# Security features:
#   - Downloads directly from GitHub releases
#   - Verifies SHA256 checksum against official checksums.txt
#   - Fails fast if checksum verification fails
#   - Eliminates trust dependency on installer scripts

set -euo pipefail

# Configuration
AWF_VERSION="${1:-}"
AWF_REPO="github/gh-aw-firewall"
AWF_INSTALL_DIR="/usr/local/bin"
AWF_INSTALL_NAME="awf"
AWF_LIB_DIR="/usr/local/lib/awf"

# Parse flags from remaining arguments
ROOTLESS=false
for arg in "${@:2}"; do
  case "$arg" in
    --rootless) ROOTLESS=true ;;
    *) echo "WARNING: Unknown argument: $arg" >&2 ;;
  esac
done

# In rootless mode, install into the user's home directory instead of /usr/local
# so that standard GitHub-hosted runners (where /usr/local is root-owned) work
# without requiring any pre-chowning or sudo.
if [ "$ROOTLESS" = "true" ]; then
  AWF_USER_PREFIX="${HOME}/.local"
  AWF_INSTALL_DIR="${AWF_USER_PREFIX}/bin"
  AWF_LIB_DIR="${AWF_USER_PREFIX}/lib/awf"
fi

# maybe_sudo runs a command with sudo unless --rootless was specified.
# In network-isolation mode, AWF runs rootless so sudo is not available or needed.
maybe_sudo() {
  if [ "$ROOTLESS" = "true" ]; then
    "$@"
  else
    sudo "$@"
  fi
}

if [ -z "$AWF_VERSION" ]; then
  echo "ERROR: AWF version is required"
  echo "Usage: $0 VERSION [--rootless]"
  exit 1
fi

# Detect OS and architecture
OS="$(uname -s)"
ARCH="$(uname -m)"

echo "Installing awf with checksum verification (version: ${AWF_VERSION}, os: ${OS}, arch: ${ARCH})"

# Rootless mode preflight: create and verify write access to install directories
if [ "$ROOTLESS" = "true" ]; then
  if ! { mkdir -p "${AWF_INSTALL_DIR}" && [ -w "${AWF_INSTALL_DIR}" ]; }; then
    echo "ERROR: --rootless could not create a writable install directory at ${AWF_INSTALL_DIR}" >&2
    exit 1
  fi
  if ! { mkdir -p "${AWF_LIB_DIR}" && [ -w "${AWF_LIB_DIR}" ]; }; then
    echo "ERROR: --rootless could not create a writable lib directory at ${AWF_LIB_DIR}" >&2
    exit 1
  fi
fi

# Clean up any stale AWF chroot directories left by previous runs before we
# execute awf. Rootless AWF can fail its writeConfigs startup path with EACCES
# when stale /tmp/awf-*-chroot-home or /tmp/awf-chroot-* directories remain
# from earlier jobs on the same runner. Limit cleanup to directories owned by
# root or the current runner user so we do not delete unrelated /tmp entries
# that happen to match the name pattern.
echo "Cleaning up stale AWF chroot directories..."
if command -v sudo >/dev/null 2>&1; then
  sudo find /tmp -maxdepth 1 -name 'awf-*-chroot-home' -type d \( -user root -o -user "$(id -un)" \) -exec rm -rf -- {} + || true
  sudo find /tmp -maxdepth 1 -name 'awf-chroot-*' -type d \( -user root -o -user "$(id -un)" \) -exec rm -rf -- {} + || true
else
  echo "Warning: sudo is unavailable; skipping stale AWF chroot cleanup" >&2
fi

# Download URLs
BASE_URL="https://github.com/${AWF_REPO}/releases/download/${AWF_VERSION}"
CHECKSUMS_URL="${BASE_URL}/checksums.txt"
CURL_RETRY_OPTS=(--retry 5 --retry-delay 10 --retry-max-time 180)
# --retry-all-errors was added in curl 7.71, so it must not be passed to older curl
# builds found on some self-hosted runners. Probe the help output instead of parsing
# `curl --version`, which reports vendor-patched version strings that do not reliably
# indicate option availability. Help categories (`curl --help all`) only exist since
# curl 7.73, so fall back to plain `curl --help`, which lists every option on
# older builds.
if curl --help all 2>/dev/null | grep -q -- '--retry-all-errors' ||
  curl --help 2>/dev/null | grep -q -- '--retry-all-errors'; then
  CURL_RETRY_OPTS+=(--retry-all-errors)
fi

# Platform-portable SHA256 function
sha256_hash() {
  local file="$1"
  if command -v sha256sum &>/dev/null; then
    sha256sum "$file" | awk '{print $1}'
  elif command -v shasum &>/dev/null; then
    shasum -a 256 "$file" | awk '{print $1}'
  else
    echo "ERROR: No sha256sum or shasum found" >&2
    exit 1
  fi
}

# Create temp directory
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

# Download checksums
echo "Downloading checksums from ${CHECKSUMS_URL@Q}..."
curl -fsSL "${CURL_RETRY_OPTS[@]}" -o "${TEMP_DIR}/checksums.txt" "${CHECKSUMS_URL}"

verify_checksum() {
  local file="$1"
  local fname="$2"

  echo "Verifying SHA256 checksum for ${fname}..."
  EXPECTED_CHECKSUM=$(awk -v fname="${fname}" '$2 == fname {print $1; exit}' "${TEMP_DIR}/checksums.txt" | tr 'A-F' 'a-f')

  if [ -z "$EXPECTED_CHECKSUM" ]; then
    echo "ERROR: Could not find checksum for ${fname} in checksums.txt"
    return 1
  fi

  ACTUAL_CHECKSUM=$(sha256_hash "$file" | tr 'A-F' 'a-f')

  if [ "$EXPECTED_CHECKSUM" != "$ACTUAL_CHECKSUM" ]; then
    echo "ERROR: Checksum verification failed!"
    echo "  Expected: $EXPECTED_CHECKSUM"
    echo "  Got:      $ACTUAL_CHECKSUM"
    echo "  The downloaded file may be corrupted or tampered with"
    return 1
  fi

  echo "✓ Checksum verification passed for ${fname}"
}

# Check if Node.js >= 20 is available
has_node_20() {
  if ! command -v node &>/dev/null; then
    return 1
  fi
  local node_major
  node_major=$(node --version | sed 's/^v//' | cut -d. -f1)
  if [ "$node_major" -ge 20 ] 2>/dev/null; then
    return 0
  fi
  return 1
}

install_bundle() {
  local bundle_name="awf-bundle.js"
  local bundle_url="${BASE_URL}/${bundle_name}"

  # Capture the absolute path to node so the wrapper works correctly when
  # invoked via sudo (where PATH may not include the setup-node install dir,
  # e.g. ~/.nvm/versions/node/v24.x.x/bin).
  local node_bin
  node_bin=$(command -v node)

  echo "Node.js >= 20 detected ($(node --version)), using lightweight bundle..."
  echo "Downloading bundle from ${bundle_url@Q}..."
  if ! curl -fsSL "${CURL_RETRY_OPTS[@]}" -o "${TEMP_DIR}/${bundle_name}" "${bundle_url}"; then
    echo "⚠ Bundle download failed (asset may not exist for this version)"
    return 1
  fi

  # Verify checksum
  if ! verify_checksum "${TEMP_DIR}/${bundle_name}" "${bundle_name}"; then
    echo "⚠ Bundle checksum verification failed"
    return 1
  fi

  # Install bundle to lib directory
  maybe_sudo mkdir -p "${AWF_LIB_DIR}"
  maybe_sudo cp "${TEMP_DIR}/${bundle_name}" "${AWF_LIB_DIR}/${bundle_name}"

  # Create wrapper script using the absolute path to node.
  # Using an unquoted heredoc (<<WRAPPER) so that ${node_bin} is expanded
  # at wrapper-creation time, while \$@ is left as the literal $@ for
  # runtime argument forwarding.
  maybe_sudo tee "${AWF_INSTALL_DIR}/${AWF_INSTALL_NAME}" > /dev/null <<WRAPPER
#!/bin/bash
exec "${node_bin}" "${AWF_LIB_DIR}/awf-bundle.js" "\$@"
WRAPPER
  maybe_sudo chmod +x "${AWF_INSTALL_DIR}/${AWF_INSTALL_NAME}"

  echo "✓ Installed awf bundle to ${AWF_LIB_DIR}/${bundle_name}"
}

install_linux_binary() {
  # Determine binary name based on architecture
  local awf_binary
  case "$ARCH" in
    x86_64|amd64) awf_binary="awf-linux-x64" ;;
    aarch64|arm64) awf_binary="awf-linux-arm64" ;;
    *) echo "ERROR: Unsupported Linux architecture: ${ARCH}"; exit 1 ;;
  esac

  local binary_url="${BASE_URL}/${awf_binary}"
  echo "Downloading binary from ${binary_url@Q}..."
  curl -fsSL "${CURL_RETRY_OPTS[@]}" -o "${TEMP_DIR}/${awf_binary}" "${binary_url}"

  # Verify checksum
  verify_checksum "${TEMP_DIR}/${awf_binary}" "${awf_binary}"

  # Make binary executable and install
  chmod +x "${TEMP_DIR}/${awf_binary}"
  maybe_sudo mv "${TEMP_DIR}/${awf_binary}" "${AWF_INSTALL_DIR}/${AWF_INSTALL_NAME}"
}

install_darwin_binary() {
  # Determine binary name based on architecture
  local awf_binary
  case "$ARCH" in
    x86_64) awf_binary="awf-darwin-x64" ;;
    arm64) awf_binary="awf-darwin-arm64" ;;
    *) echo "ERROR: Unsupported macOS architecture: ${ARCH}"; exit 1 ;;
  esac

  echo "Note: AWF uses iptables for network firewalling, which is not available on macOS."
  echo "      The AWF CLI will be installed but container-based firewalling will not work natively."
  echo ""

  local binary_url="${BASE_URL}/${awf_binary}"
  echo "Downloading binary from ${binary_url@Q}..."
  curl -fsSL "${CURL_RETRY_OPTS[@]}" -o "${TEMP_DIR}/${awf_binary}" "${binary_url}"

  # Verify checksum
  verify_checksum "${TEMP_DIR}/${awf_binary}" "${awf_binary}"

  # Make binary executable and install
  chmod +x "${TEMP_DIR}/${awf_binary}"
  maybe_sudo mv "${TEMP_DIR}/${awf_binary}" "${AWF_INSTALL_DIR}/${AWF_INSTALL_NAME}"
}

install_platform_binary() {
  case "$OS" in
    Linux)
      install_linux_binary
      ;;
    Darwin)
      install_darwin_binary
      ;;
    *)
      echo "ERROR: Unsupported operating system: ${OS}"
      exit 1
      ;;
  esac
}

# Try lightweight bundle first, fall back to platform binary
if has_node_20; then
  if ! install_bundle; then
    echo "⚠ Bundle install failed, falling back to platform binary..."
    install_platform_binary
  fi
else
  echo "Node.js >= 20 not available, falling back to platform binary..."
  install_platform_binary
fi

# In rootless mode, add the install dir to PATH for subsequent steps.
# $GITHUB_PATH is the mechanism for persisting PATH additions across steps in GitHub Actions.
if [ "$ROOTLESS" = "true" ]; then
  if [ -n "${GITHUB_PATH:-}" ]; then
    echo "${AWF_INSTALL_DIR}" >> "${GITHUB_PATH}"
  else
    echo "WARNING: --rootless install complete but \$GITHUB_PATH is unset; add ${AWF_INSTALL_DIR} to PATH manually" >&2
  fi
fi

# Verify installation by running --version.
# In legacy (non-rootless) mode, run with sudo (without -E, since we explicitly unset
# environment variables below). On GPU runners (e.g. aw-gpu-runner-T4), /usr/local/bin
# may be inaccessible to the current non-root user due to filesystem or security
# policy restrictions, so running the version check without sudo would fail with
# "Permission denied".
# In rootless (network-isolation) mode, run without sudo since AWF is rootless.
# A successful run prints the version string (e.g. "0.25.13") to stdout.
# Also clear DIFC (Data Integrity and Filtering Controls) proxy env vars
# set by start_difc_proxy.sh. When the DIFC proxy is active, GITHUB_API_URL
# and GITHUB_GRAPHQL_URL point to localhost:18443 and GH_HOST is overridden.
# The AWF bundle may try to reach these endpoints on startup, causing the
# version check to fail with a connection error if the proxy rejects the request.
maybe_sudo env -u GITHUB_API_URL -u GITHUB_GRAPHQL_URL -u GH_HOST \
    "${AWF_INSTALL_DIR}/${AWF_INSTALL_NAME}" --version

echo "✓ AWF installation complete"
