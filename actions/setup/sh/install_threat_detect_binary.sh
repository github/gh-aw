#!/usr/bin/env bash
set +o histexpand

# Install the threat-detect binary from GitHub Releases with setup-action-pinned SHA256 verification.
# Used when `features: gh-aw-detection: true` is set in the workflow frontmatter to enable
# the external threat-detect binary detection path instead of inline engine execution.
#
# Usage: install_threat_detect_binary.sh VERSION [--sha256-amd64 DIGEST --sha256-arm64 DIGEST] [--rootless]
#
# Arguments:
#   VERSION    - threat-detect version to install (e.g., v0.2.2) or "latest" to
#                install the latest release via GitHub's latest-release download endpoint
#   --sha256-amd64 - Expected SHA256 digest for the Linux amd64 binary (required
#                    when VERSION differs from the version pinned in this action)
#   --sha256-arm64 - Expected SHA256 digest for the Linux arm64 binary (required
#                    when VERSION differs from the version pinned in this action)
#   --rootless     - Install to ~/.local/bin without sudo; appends that directory to
#                    $GITHUB_PATH so subsequent steps find the binary. Use this on
#                    ARC/DinD runners that enforce allowPrivilegeEscalation: false.
#
# Platform support:
#   - Linux (x64, arm64): Downloads pre-built binary
#   - macOS: NOT supported. Agentic workflows require Linux container jobs, and the
#     compiler rejects macOS runner labels (including
#     safe-outputs.threat-detection.runs-on) before a workflow is generated. If this
#     script is ever reached on Darwin it fails fast with an explicit message instead
#     of attempting a download.
#
# Security features:
#   - Downloads directly from GitHub releases
#   - Verifies SHA256 against setup-action-pinned, architecture-specific digests
#   - Fails fast if checksum verification fails

set -euo pipefail

# Configuration
THREAT_DETECT_REPO="github/gh-aw-threat-detection"
THREAT_DETECT_INSTALL_DIR="/usr/local/bin"
THREAT_DETECT_INSTALL_NAME="threat-detect"
MACOS_FAQ_URL="https://github.github.com/gh-aw/reference/faq/#why-are-macos-runners-not-supported"
PINNED_THREAT_DETECT_VERSION="v0.5.1"
PINNED_THREAT_DETECT_SHA256_AMD64="1b27989fb52cbdc401e48137e508bea8b915aad4911468fb0fd9c87c2a7cd31b"
PINNED_THREAT_DETECT_SHA256_ARM64="204ba220229ac3fda80f7603b6e2a816f69e8a5ea2833c0ac534107bbffb827a"

# Parse arguments.
THREAT_DETECT_VERSION=""
THREAT_DETECT_SHA256_AMD64=""
THREAT_DETECT_SHA256_ARM64=""
ROOTLESS=false
while [ "$#" -gt 0 ]; do
  case "$1" in
    --rootless)
      ROOTLESS=true
      shift
      ;;
    --sha256-amd64|--sha256-arm64)
      if [ "$#" -lt 2 ]; then
        echo "ERROR: $1 requires a SHA256 digest" >&2
        exit 1
      fi
      if [ "$1" = "--sha256-amd64" ]; then
        THREAT_DETECT_SHA256_AMD64="$2"
      else
        THREAT_DETECT_SHA256_ARM64="$2"
      fi
      shift 2
      ;;
    --*)
      echo "ERROR: Unknown flag: $1" >&2
      exit 1
      ;;
    *)
      if [ -z "$THREAT_DETECT_VERSION" ]; then
        THREAT_DETECT_VERSION="$1"
      else
        echo "ERROR: Unexpected argument: $1" >&2
        exit 1
      fi
      shift
      ;;
  esac
done

if [ -z "$THREAT_DETECT_VERSION" ]; then
  echo "ERROR: threat-detect version is required"
  echo "Usage: $0 VERSION [--sha256-amd64 DIGEST --sha256-arm64 DIGEST] [--rootless]"
  exit 1
fi

# Use the digests embedded in this immutable action for the compiler-pinned version.
if [ "$THREAT_DETECT_VERSION" = "$PINNED_THREAT_DETECT_VERSION" ]; then
  THREAT_DETECT_SHA256_AMD64="${THREAT_DETECT_SHA256_AMD64:-$PINNED_THREAT_DETECT_SHA256_AMD64}"
  THREAT_DETECT_SHA256_ARM64="${THREAT_DETECT_SHA256_ARM64:-$PINNED_THREAT_DETECT_SHA256_ARM64}"
fi

for digest in "$THREAT_DETECT_SHA256_AMD64" "$THREAT_DETECT_SHA256_ARM64"; do
  if [[ ! "$digest" =~ ^[[:xdigit:]]{64}$ ]]; then
    echo "ERROR: Valid SHA256 digests are required for both supported architectures" >&2
    exit 1
  fi
done

# In rootless mode, install into the user's home directory instead of /usr/local/bin
# so that ARC/DinD runners with allowPrivilegeEscalation: false can run without sudo.
if [ "$ROOTLESS" = "true" ]; then
  THREAT_DETECT_INSTALL_DIR="${HOME}/.local/bin"
fi

# maybe_sudo runs a command with sudo unless --rootless was specified.
# In rootless mode, sudo is not available or needed.
maybe_sudo() {
  if [ "$ROOTLESS" = "true" ]; then
    "$@"
  else
    sudo "$@"
  fi
}

# Rootless mode preflight: create and verify write access to the install directory.
if [ "$ROOTLESS" = "true" ]; then
  if ! { mkdir -p "${THREAT_DETECT_INSTALL_DIR}" && [ -w "${THREAT_DETECT_INSTALL_DIR}" ]; }; then
    echo "ERROR: --rootless could not create a writable install directory at ${THREAT_DETECT_INSTALL_DIR}" >&2
    exit 1
  fi
fi

# Detect OS and architecture
OS="$(uname -s)"
ARCH="$(uname -m)"

echo "Installing threat-detect with checksum verification (version: ${THREAT_DETECT_VERSION}, os: ${OS}, arch: ${ARCH})"

# Fail fast on unsupported platforms before any network access. Only Linux is supported:
# agentic workflows require Linux container jobs, and the compiler rejects macOS runner
# labels (including safe-outputs.threat-detection.runs-on) at compile time.
case "$OS" in
  Linux) ;;
  Darwin)
    echo "ERROR: macOS is not a supported platform for threat-detect."
    echo "  Agentic workflows require Linux container jobs; use a Linux runner instead."
    echo "  See ${MACOS_FAQ_URL} for details."
    exit 1
    ;;
  *)
    echo "ERROR: Unsupported operating system: ${OS}"
    echo "  threat-detect is only published for Linux (x64, arm64)."
    exit 1
    ;;
esac

# Download release assets directly rather than resolving a release through the GitHub API.
if [ "$THREAT_DETECT_VERSION" = "latest" ]; then
  BASE_URL="https://github.com/${THREAT_DETECT_REPO}/releases/latest/download"
else
  BASE_URL="https://github.com/${THREAT_DETECT_REPO}/releases/download/${THREAT_DETECT_VERSION}"
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

verify_checksum() {
  local file="$1"
  local fname="$2"
  local expected_checksum="$3"
  local actual_checksum

  echo "Verifying SHA256 checksum for ${fname}..."
  expected_checksum=$(printf '%s' "$expected_checksum" | tr 'A-F' 'a-f')
  actual_checksum=$(sha256_hash "$file" | tr 'A-F' 'a-f')

  if [ "$expected_checksum" != "$actual_checksum" ]; then
    echo "ERROR: Checksum verification failed!"
    echo "  Expected: $expected_checksum"
    echo "  Got:      $actual_checksum"
    echo "  The downloaded file may be corrupted or tampered with"
    return 1
  fi

  echo "✓ Checksum verification passed for ${fname}"
}

install_linux_binary() {
  # Determine binary name based on architecture
  local binary_name
  local expected_checksum
  case "$ARCH" in
    x86_64|amd64)
      binary_name="threat-detect-linux-amd64"
      expected_checksum="$THREAT_DETECT_SHA256_AMD64"
      ;;
    aarch64|arm64)
      binary_name="threat-detect-linux-arm64"
      expected_checksum="$THREAT_DETECT_SHA256_ARM64"
      ;;
    *) echo "ERROR: Unsupported Linux architecture: ${ARCH}"; exit 1 ;;
  esac

  local binary_url="${BASE_URL}/${binary_name}"
  echo "Downloading binary from \"${binary_url}\"..."
  curl -fsSL --retry 5 --retry-delay 10 --retry-max-time 180 --retry-all-errors -o "${TEMP_DIR}/${binary_name}" "${binary_url}"

  # Verify checksum
  verify_checksum "${TEMP_DIR}/${binary_name}" "${binary_name}" "${expected_checksum}"

  # Make binary executable and install
  chmod +x "${TEMP_DIR}/${binary_name}"
  maybe_sudo mv "${TEMP_DIR}/${binary_name}" "${THREAT_DETECT_INSTALL_DIR}/${THREAT_DETECT_INSTALL_NAME}"
}

install_linux_binary

# In rootless mode, add the install dir to PATH for subsequent steps.
if [ "$ROOTLESS" = "true" ]; then
  if [ -n "${GITHUB_PATH:-}" ]; then
    echo "${THREAT_DETECT_INSTALL_DIR}" >> "${GITHUB_PATH}"
    echo "  Exported ${THREAT_DETECT_INSTALL_DIR} to GITHUB_PATH"
  else
    echo "  GITHUB_PATH not set — binary installed at ${THREAT_DETECT_INSTALL_DIR}/${THREAT_DETECT_INSTALL_NAME}"
  fi
fi

# Verify installation
"${THREAT_DETECT_INSTALL_DIR}/${THREAT_DETECT_INSTALL_NAME}" --version

echo "✓ threat-detect installation complete"
