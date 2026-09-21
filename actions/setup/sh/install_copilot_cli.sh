#!/usr/bin/env bash
set +o histexpand

# Install GitHub Copilot CLI with SHA256 checksum verification
# Usage: install_copilot_cli.sh [VERSION] [--rootless]
#
# This script downloads and installs the GitHub Copilot CLI directly from GitHub
# releases with SHA256 checksum verification, following the secure pattern from
# install_awf_binary.sh to avoid executing unverified downloaded scripts.
#
# Arguments:
#   VERSION    - Optional Copilot CLI version to install. When omitted, the script
#                resolves the version via compat.json (using GH_AW_COMPILED_VERSION) and
#                falls back to DEFAULT_COPILOT_VERSION baked into this script.
#   --rootless - Install to ~/.local/bin without sudo; appends that directory to
#                $GITHUB_PATH so subsequent steps find the binary.  Use this on
#                ARC/DinD runners that enforce allowPrivilegeEscalation: false.
#
# Environment overrides (testing):
#   COPILOT_INSTALL_DIR - Override the install directory (default: /usr/local/bin).
#
# Security features:
#   - Downloads binary directly from GitHub releases (no installer script execution)
#   - Verifies SHA256 checksum against official SHA256SUMS.txt
#   - Fails fast if checksum verification fails

set -euo pipefail

# Configuration
SECONDS_PER_DAY=86400
COPILOT_REPO="github/copilot-cli"
INSTALL_DIR="${COPILOT_INSTALL_DIR:-/usr/local/bin}"
COPILOT_DIR="${HOME}/.copilot"
COPILOT_TOOLCACHE_MAX_DEPTH=4
# DEFAULT_COPILOT_VERSION is the baked-in fallback used when neither an explicit version
# argument nor a GH_AW_COMPILED_VERSION-backed compat.json lookup is available.
# It is the last resort (priority 3) after engine.version (priority 1) and
# compat.json toolcache lookup (priority 2).
DEFAULT_COPILOT_VERSION="1.0.83"
COMPAT_URL="${COPILOT_COMPAT_URL:-https://raw.githubusercontent.com/github/gh-aw-actions/main/.github/aw/compat.json}"
COMPILED_GH_AW_VERSION="${GH_AW_COMPILED_VERSION:-}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
COMPAT_BUNDLED_PATH="${COPILOT_COMPAT_BUNDLED_PATH:-${REPO_ROOT}/.github/aw/compat.json}"
COMPAT_MATCHED_MIN_AGENT=""
COMPAT_MATCHED_MAX_AGENT=""
COMPAT_CACHE_TTL_DAYS=""

# Parse arguments: treat the first non-flag argument as VERSION, all --<flag> arguments as flags.
VERSION=""
ROOTLESS=false
for arg in "$@"; do
  case "$arg" in
    --rootless) ROOTLESS=true ;;
    --*) echo "WARNING: Unknown flag: $arg" >&2 ;;
    *)
      if [ -z "$VERSION" ]; then
        VERSION="$arg"
      fi
      ;;
  esac
done

OS="$(uname -s)"
IS_WINDOWS=false
case "$OS" in
  MINGW*|MSYS*|CYGWIN*)
    IS_WINDOWS=true
    ROOTLESS=true
    INSTALL_DIR="${HOME}/.local/bin"
    ;;
esac

# In rootless mode, install into the user's home directory instead of /usr/local/bin
# so that ARC/DinD runners with allowPrivilegeEscalation: false can run without sudo.
if [ "$ROOTLESS" = "true" ]; then
  INSTALL_DIR="${HOME}/.local/bin"
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
  if ! { mkdir -p "${INSTALL_DIR}" && [ -w "${INSTALL_DIR}" ]; }; then
    echo "ERROR: --rootless could not create a writable install directory at ${INSTALL_DIR}" >&2
    exit 1
  fi
fi

# Fix directory ownership before installation
# This is needed because a previous AWF run on the same runner may have used
# `sudo -E awf --enable-chroot ...`, which creates the .copilot directory with
# root ownership. The Copilot CLI (running as the runner user) then fails when
# trying to create subdirectories. See: https://github.com/github/gh-aw/issues/12066
echo "Ensuring correct ownership of $COPILOT_DIR..."
mkdir -p "$COPILOT_DIR"
maybe_sudo chown -R "$(id -u):$(id -g)" "$COPILOT_DIR"

# Clean up any stale AWF chroot directories left by previous runs.
# When AWF ran with `sudo -E awf --enable-host-access`, it created
# /tmp/awf-*-chroot-home and /tmp/awf-chroot-* directories with root-owned
# files. These cause EACCES failures in the Copilot CLI cleanup path
# (rimrafSync) or AWF writeConfigs on the same or subsequent runs, which
# reports as "engine terminated unexpectedly" or fatal EACCES errors.
# Remove them here before the agent starts so the runner is in a clean state.
echo "Cleaning up stale AWF chroot directories..."
maybe_sudo find /tmp -maxdepth 1 -name 'awf-*-chroot-home' -type d -exec rm -rf -- {} + 2>/dev/null || true
maybe_sudo find /tmp -maxdepth 1 -name 'awf-chroot-*' -type d -exec rm -rf -- {} + 2>/dev/null || true

# Detect architecture
ARCH="$(uname -m)"

# Map architecture to Copilot CLI naming
case "$ARCH" in
  x86_64|amd64) ARCH_NAME="x64" ;;
  aarch64|arm64) ARCH_NAME="arm64" ;;
  *) echo "ERROR: Unsupported architecture: ${ARCH}"; exit 1 ;;
esac

# Map OS to Copilot CLI naming
case "$OS" in
  Linux) PLATFORM="linux" ;;
  Darwin) PLATFORM="darwin" ;;
  MINGW*|MSYS*|CYGWIN*) PLATFORM="win32" ;;
  *) echo "ERROR: Unsupported operating system: ${OS}"; exit 1 ;;
esac

if [ "$IS_WINDOWS" = "true" ]; then
  TARBALL_NAME="copilot-${PLATFORM}-${ARCH_NAME}.zip"
else
  TARBALL_NAME="copilot-${PLATFORM}-${ARCH_NAME}.tar.gz"
fi
REQUESTED_VERSION="${VERSION:-latest}"

echo "Installing GitHub Copilot CLI${VERSION:+ version $VERSION} (os: ${OS}, arch: ${ARCH})..."

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

# Normalize Copilot versions so toolcache lookups can match both v-prefixed and bare versions.
normalize_version() {
  local version="${1:-}"
  printf '%s\n' "${version#v}"
}

# Check if a version string contains only numeric parts separated by dots.
# Returns success (0) if version is purely numeric (e.g., "1.2.3"), failure (1) otherwise.
version_is_numeric() {
  local version="${1:-}"
  local parts=()
  local part=""
  
  IFS='.' read -r -a parts <<< "$version"
  
  for part in "${parts[@]}"; do
    # Check if part is empty or contains non-digits
    if [[ ! "$part" =~ ^[0-9]+$ ]]; then
      return 1
    fi
  done
  
  return 0
}

# Compare dotted numeric versions without relying on GNU-specific sort -V.
# Returns success only when the left version is strictly greater than the right version.
version_is_greater() {
  local left="${1:-0}"
  local right="${2:-0}"
  local left_parts=()
  local right_parts=()
  local max_parts=0
  local i=0
  local left_part=0
  local right_part=0

  IFS='.' read -r -a left_parts <<< "$left"
  IFS='.' read -r -a right_parts <<< "$right"

  if [ "${#left_parts[@]}" -gt "${#right_parts[@]}" ]; then
    max_parts="${#left_parts[@]}"
  else
    max_parts="${#right_parts[@]}"
  fi

  for ((i = 0; i < max_parts; i++)); do
    left_part="${left_parts[i]:-0}"
    right_part="${right_parts[i]:-0}"

    # Use base-10 parsing so values like 08 are never treated as invalid octal literals.
    if ((10#$left_part > 10#$right_part)); then
      return 0
    fi
    if ((10#$left_part < 10#$right_part)); then
      return 1
    fi
  done

  return 1
}

# Download compatibility matrix with bundled fallback.
download_compat_json() {
  local compat_file="$1"
  local source_file="$2"

  echo "Attempting to download compatibility matrix from ${COMPAT_URL}..." >&2
  if curl -fsSL --retry 3 --retry-delay 5 --retry-all-errors -o "$compat_file" "$COMPAT_URL"; then
    echo "$COMPAT_URL" > "$source_file"
    echo "Successfully downloaded compatibility matrix from ${COMPAT_URL}" >&2
    return 0
  fi
  echo "Compatibility matrix download failed from ${COMPAT_URL}" >&2

  if [ -f "$COMPAT_BUNDLED_PATH" ]; then
    echo "::warning::Compatibility matrix network fetch failed; using bundled fallback at ${COMPAT_BUNDLED_PATH}"
    echo "Falling back to bundled compatibility matrix at ${COMPAT_BUNDLED_PATH}" >&2
    cp "$COMPAT_BUNDLED_PATH" "$compat_file"
    echo "bundled:${COMPAT_BUNDLED_PATH}" > "$source_file"
    return 0
  fi

  echo "Bundled compatibility matrix not found at ${COMPAT_BUNDLED_PATH}" >&2
  return 1
}

# Resolve compat using jq.
# Returns: "max_agent|row_index|min_aw|max_aw|min_agent|max_agent|cache_ttl_days"
resolve_compat_with_jq() {
  local compat_file="$1"
  local compiled_version="$2"
  local compiled_no_v="${compiled_version#v}"
  
  jq -r --arg compiled "$compiled_no_v" --arg compiled_version "$compiled_version" '
    # Semver comparison: returns -1 if a<b, 0 if equal, 1 if a>b
    def semver_cmp(a; b):
      (a | split(".") | map(tonumber)) as $a_parts |
      (b | split(".") | map(tonumber)) as $b_parts |
      if ($a_parts[0] < $b_parts[0]) then -1
      elif ($a_parts[0] > $b_parts[0]) then 1
      elif ($a_parts[1] < $b_parts[1]) then -1
      elif ($a_parts[1] > $b_parts[1]) then 1
      elif ($a_parts[2] < $b_parts[2]) then -1
      elif ($a_parts[2] > $b_parts[2]) then 1
      else 0 end;
    
    .["agent-compat-v1"] as $compat |
    ($compat["cache-ttl-days"] // "") as $cache_ttl |
    ($compat.copilot // []) as $rows |
    
    # Find first matching row
    $rows | to_entries | map(
      .value as $row |
      .key as $idx |
      $row["min-gh-aw"] as $min_aw |
      $row["max-gh-aw"] as $max_aw |
      $row["min-agent"] as $min_agent |
      $row["max-agent"] as $max_agent |
      
      # Use the open row for development builds, which do not have a semver release tag.
      if (($compiled_version == "dev") and ($row.open == true)) or
         (($compiled_version != "dev") and
          (semver_cmp($compiled; $min_aw) >= 0) and
          (($max_aw == "*") or (semver_cmp($compiled; $max_aw) <= 0))) then
        "\($max_agent)|\($idx)|\($min_aw)|\($max_aw)|\($min_agent)|\($max_agent)|\($cache_ttl)"
      else empty end
    ) | first // ""
  ' "$compat_file"
}

# Resolve Copilot version from compat matrix using GH_AW_COMPILED_VERSION.
resolve_version_from_compat() {
  local compiled_version="${1:-}"
  local compat_file="$2"
  local resolved_info=""
  local compat_source=""

  if [ -z "$compiled_version" ]; then
    echo "No GH_AW_COMPILED_VERSION provided, skipping compatibility matrix resolution." >&2
    return 1
  fi

  if [[ ! "$compiled_version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ && "$compiled_version" != "dev" ]]; then
    echo "GH_AW_COMPILED_VERSION '${compiled_version}' is not in vMAJOR.MINOR.PATCH format; skipping compatibility matrix resolution." >&2
    return 1
  fi

  compat_source="${compat_file}.source"
  if ! download_compat_json "$compat_file" "$compat_source"; then
    echo "Could not resolve compatibility matrix from network or bundled fallback." >&2
    return 1
  fi

  if ! command -v jq >/dev/null 2>&1; then
    echo "ERROR: jq is required for compatibility matrix resolution." >&2
    echo "ERROR: Install jq from https://jqlang.github.io/jq/download/ or pass an explicit Copilot CLI version to bypass compat resolution." >&2
    return 1
  fi

  if ! resolved_info="$(resolve_compat_with_jq "$compat_file" "$compiled_version" 2>&1)"; then
    if [ -n "$resolved_info" ]; then
      echo "ERROR: Compatibility matrix resolution failed: ${resolved_info}" >&2
    else
      echo "ERROR: Compatibility matrix resolution failed." >&2
    fi
    return 1
  fi

  if [ -z "$resolved_info" ]; then
    echo "Compatibility matrix lookup found no matching copilot window for gh-aw ${compiled_version}." >&2
    return 1
  fi

  IFS='|' read -r resolved_version row_index row_min_aw row_max_aw row_min_agent row_max_agent cache_ttl_days <<< "$resolved_info"
  echo "Compatibility matrix source: $(cat "$compat_source")" >&2
  echo "Compatibility matrix matched row ${row_index}: gh-aw ${row_min_aw}..${row_max_aw}, copilot ${row_min_agent}..${row_max_agent}" >&2
  echo "Resolved Copilot CLI version from compatibility matrix: ${resolved_version}" >&2
  if [ -n "$cache_ttl_days" ]; then
    echo "Cache TTL: ${cache_ttl_days} days" >&2
  fi
  printf '%s|%s|%s|%s\n' "$resolved_version" "$row_min_agent" "$row_max_agent" "$cache_ttl_days"
  return 0
}

# Check if a cached binary has exceeded the cache TTL (in days).
# Returns 0 (expired) or 1 (not expired).
is_cache_expired() {
  local cached_binary="$1"
  local ttl_days="$2"
  local now_epoch=""
  local file_epoch=""
  local age_days=""
  
  # If TTL is not set or not numeric, consider cache as not expired
  if [ -z "$ttl_days" ] || ! [[ "$ttl_days" =~ ^[0-9]+$ ]]; then
    return 1
  fi
  
  # Get current time and file modification time as epoch seconds
  now_epoch="$(date +%s)"
  
  # Try to get file modification time (platform-portable)
  if file_epoch="$(stat -c %Y "$cached_binary" 2>/dev/null)"; then
    : # Linux stat format worked
  elif file_epoch="$(stat -f %m "$cached_binary" 2>/dev/null)"; then
    : # macOS stat format worked
  else
    # Cannot determine file age, consider not expired
    return 1
  fi
  
  # Calculate age in days (integer division truncates partial days, e.g., 1.9 days → 1 day)
  age_days=$(( (now_epoch - file_epoch) / SECONDS_PER_DAY ))
  
  if [ "$age_days" -ge "$ttl_days" ]; then
    echo "  Cache age: ${age_days} days (exceeds TTL of ${ttl_days} days)" >&2
    return 0  # Expired
  else
    echo "  Cache age: ${age_days} days (within TTL of ${ttl_days} days)" >&2
    return 1  # Not expired
  fi
}

# Look up a compatible Copilot CLI from the Actions toolcache before downloading a release tarball.
find_cached_copilot_bin() {
  local requested_version="${1:-latest}"
  local min_version="${2:-}"
  local max_version="${3:-}"
  local cache_ttl_days="${4:-}"
  local requested_version_normalized=""
  local tool_cache_root=""
  local candidate=""
  local candidate_dir=""
  local candidate_arch=""
  local candidate_version=""
  local candidate_version_normalized=""
  local best_candidate=""
  local best_version=""

  echo "Searching toolcache for GitHub Copilot CLI (requested: ${requested_version}, arch: ${ARCH_NAME}, range: ${min_version:-none}..${max_version:-none})..." >&2
  if [ -n "$cache_ttl_days" ]; then
    echo "  Cache TTL enabled: ${cache_ttl_days} days" >&2
  fi

  if [ "$requested_version" != "latest" ]; then
    requested_version_normalized="$(normalize_version "$requested_version")"
  fi

  local -a tool_cache_roots

  if [ -n "${RUNNER_TOOL_CACHE:-}" ]; then
    tool_cache_roots=("${RUNNER_TOOL_CACHE}")
  else
    tool_cache_roots=(
      /opt/hostedtoolcache
      /home/runner/work/_tool
    )
  fi

  for tool_cache_root in "${tool_cache_roots[@]}"; do
    if [ ! -d "${tool_cache_root}/copilot-cli" ]; then
      echo "  Toolcache root ${tool_cache_root}/copilot-cli not found, skipping" >&2
      continue
    fi

    echo "  Scanning toolcache root: ${tool_cache_root}/copilot-cli" >&2

    while IFS= read -r candidate; do
      candidate_dir="$(dirname "$candidate")"
      candidate_arch="$(basename "$(dirname "$candidate_dir")")"
      candidate_version="$(basename "$(dirname "$(dirname "$candidate_dir")")")"
      candidate_version_normalized="$(normalize_version "$candidate_version")"

      echo "  Found candidate: ${candidate} (version: ${candidate_version_normalized}, arch: ${candidate_arch})" >&2

      # Skip non-numeric versions (e.g., "1.2.3-beta.1") to prevent arithmetic expansion errors
      if ! version_is_numeric "$candidate_version_normalized"; then
        echo "  Skipping candidate (non-numeric version: ${candidate_version_normalized})" >&2
        continue
      fi

      if [ "$candidate_arch" != "$ARCH_NAME" ]; then
        echo "  Skipping candidate (arch mismatch: want ${ARCH_NAME}, got ${candidate_arch})" >&2
        continue
      fi

      if [ -n "$requested_version_normalized" ]; then
        if [ "$candidate_version_normalized" = "$requested_version_normalized" ]; then
          echo "  Exact version match found: ${candidate}" >&2
          printf '%s\n' "$candidate"
          return 0
        fi
        echo "  Skipping candidate (version mismatch: want ${requested_version_normalized}, got ${candidate_version_normalized})" >&2
        continue
      fi

      if [ -n "$min_version" ] && version_is_greater "$min_version" "$candidate_version_normalized"; then
        echo "  Skipping candidate (below compat minimum: ${candidate_version_normalized} < ${min_version})" >&2
        continue
      fi

      if [ -n "$max_version" ] && version_is_greater "$candidate_version_normalized" "$max_version"; then
        echo "  Skipping candidate (above compat maximum: ${candidate_version_normalized} > ${max_version})" >&2
        continue
      fi

      # Apply cache TTL expiry check UNLESS:
      # 1. Cached version equals max-agent (already latest in compat window), OR
      # 2. Explicit version was requested (requested_version != "latest")
      if [ -n "$cache_ttl_days" ] && [ "$requested_version" = "latest" ] && [ -n "$max_version" ]; then
        # Check if candidate version equals max-agent
        if [ "$candidate_version_normalized" = "$max_version" ]; then
          echo "  Cache TTL skipped (candidate equals max-agent: ${candidate_version_normalized})" >&2
        else
          # Candidate is not max-agent, apply TTL check
          if is_cache_expired "$candidate" "$cache_ttl_days"; then
            echo "  Skipping candidate (cache expired and not max-agent: ${candidate_version_normalized} != ${max_version})" >&2
            continue
          fi
        fi
      fi

      if [ -z "$best_candidate" ] || version_is_greater "$candidate_version_normalized" "$best_version"; then
        echo "  New best candidate: ${candidate} (${candidate_version_normalized} > ${best_version:-none})" >&2
        best_candidate="$candidate"
        best_version="$candidate_version_normalized"
      fi
    done < <(find "${tool_cache_root}/copilot-cli" -maxdepth "${COPILOT_TOOLCACHE_MAX_DEPTH}" -type f -path '*/bin/copilot' 2>/dev/null)
  done

  if [ -n "$best_candidate" ]; then
    echo "  Selected best cached version: ${best_version} at ${best_candidate}" >&2
    printf '%s\n' "$best_candidate"
    return 0
  fi

  echo "  No compatible toolcache entry found" >&2
  return 1
}

# Make a cached Copilot CLI available both to the current shell and later GitHub Actions steps.
activate_cached_copilot_bin() {
  local cached_copilot_bin="$1"
  local cached_copilot_dir=""
  local wrapper_path=""

  cached_copilot_dir="$(dirname "$cached_copilot_bin")"
  echo "Activating cached Copilot CLI from ${cached_copilot_bin}..."
  export PATH="${cached_copilot_dir}:$PATH"
  echo "  Prepended ${cached_copilot_dir} to PATH"

  if [ -n "${GITHUB_PATH:-}" ]; then
    echo "  Exporting ${cached_copilot_dir} to GITHUB_PATH (${GITHUB_PATH})"
    echo "$cached_copilot_dir" >> "${GITHUB_PATH}"
  else
    echo "  GITHUB_PATH not set — relying on ${INSTALL_DIR}/copilot"
  fi

  # The agent is launched with the hardcoded absolute path ${INSTALL_DIR}/copilot, which
  # PATH additions cannot satisfy, so always materialize that path. A small wrapper is used
  # instead of symlinking or copying the cached script to avoid broken relative paths.
  if [ "$cached_copilot_bin" = "${INSTALL_DIR}/copilot" ]; then
    echo "  Cached binary already lives at ${INSTALL_DIR}/copilot — no wrapper needed"
    return 0
  fi

  echo "  Installing wrapper at ${INSTALL_DIR}/copilot"
  wrapper_path="${TEMP_DIR}/copilot"
  cat > "$wrapper_path" <<EOF
#!/usr/bin/env bash
exec "$cached_copilot_bin" "\$@"
EOF
  maybe_sudo install -m 0755 "$wrapper_path" "${INSTALL_DIR}/copilot"
  echo "  Wrapper installed at ${INSTALL_DIR}/copilot"
}

# Create temp directory with cleanup on exit
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

# Version resolution follows this priority order:
# 1. Explicit VERSION argument (from engine.version in the workflow)
# 2. Compat.json toolcache lookup (requires GH_AW_COMPILED_VERSION to select the right window)
# 3. DEFAULT_COPILOT_VERSION baked into this script
if [ -z "$VERSION" ]; then
  echo "No explicit Copilot CLI version requested. Attempting compat-driven version resolution..."
  if RESOLVED_COMPAT_INFO="$(resolve_version_from_compat "$COMPILED_GH_AW_VERSION" "${TEMP_DIR}/compat.json")"; then
    IFS='|' read -r RESOLVED_COMPAT_VERSION COMPAT_MATCHED_MIN_AGENT COMPAT_MATCHED_MAX_AGENT COMPAT_CACHE_TTL_DAYS <<< "$RESOLVED_COMPAT_INFO"
    VERSION="$RESOLVED_COMPAT_VERSION"
    REQUESTED_VERSION="latest"
    echo "Using compat-resolved Copilot CLI window: ${COMPAT_MATCHED_MIN_AGENT}..${COMPAT_MATCHED_MAX_AGENT}"
    echo "Will install compat max-agent ${VERSION} if no cached version satisfies the window."
  else
    echo "Compat resolution unavailable; falling back to baked-in default version: ${DEFAULT_COPILOT_VERSION}" >&2
    VERSION="$DEFAULT_COPILOT_VERSION"
    REQUESTED_VERSION="$DEFAULT_COPILOT_VERSION"
  fi
else
  echo "Explicit Copilot CLI version argument provided (${VERSION}); skipping compat matrix resolution."
fi

# Prefer the runner toolcache when a compatible Copilot CLI is already available.
if CACHED_COPILOT_BIN="$(find_cached_copilot_bin "$REQUESTED_VERSION" "${COMPAT_MATCHED_MIN_AGENT}" "${COMPAT_MATCHED_MAX_AGENT}" "${COMPAT_CACHE_TTL_DAYS}")"; then
  echo "Using cached GitHub Copilot CLI from ${CACHED_COPILOT_BIN}"
  activate_cached_copilot_bin "$CACHED_COPILOT_BIN"

  echo "Verifying cached Copilot CLI installation..."
  # The agent is spawned with the absolute path ${INSTALL_DIR}/copilot, so that path must
  # exist even when the CLI itself came from the toolcache.
  if [ ! -x "${INSTALL_DIR}/copilot" ]; then
    echo "ERROR: Cached Copilot CLI activation failed - ${INSTALL_DIR}/copilot is missing or not executable"
    exit 1
  fi

  RESOLVED_COPILOT="$(command -v copilot 2>/dev/null || true)"
  if [ -n "$RESOLVED_COPILOT" ]; then
    echo "  Resolved copilot binary: ${RESOLVED_COPILOT}"
    echo "  Canonical install path: ${INSTALL_DIR}/copilot"
    "$RESOLVED_COPILOT" --version
    echo "✓ Copilot CLI installation complete (cached)"
    exit 0
  fi

  echo "ERROR: Cached Copilot CLI activation failed - command not found"
  exit 1
fi

# Build download URLs
if [ -z "$VERSION" ] || [ "$VERSION" = "latest" ]; then
  BASE_URL="https://github.com/${COPILOT_REPO}/releases/latest/download"
else
  # Prefix version with 'v' if not already present
  case "$VERSION" in
    v*) ;;
    *) VERSION="v$VERSION" ;;
  esac
  BASE_URL="https://github.com/${COPILOT_REPO}/releases/download/${VERSION}"
fi

TARBALL_URL="${BASE_URL}/${TARBALL_NAME}"
CHECKSUMS_URL="${BASE_URL}/SHA256SUMS.txt"

# Download checksums
echo "Downloading checksums from ${CHECKSUMS_URL}..."
curl -fsSL --retry 5 --retry-delay 2 --retry-max-time 60 --retry-all-errors -o "${TEMP_DIR}/SHA256SUMS.txt" "${CHECKSUMS_URL}"

# Download binary tarball
echo "Downloading binary from ${TARBALL_URL}..."
curl -fsSL --retry 5 --retry-delay 2 --retry-max-time 60 --retry-all-errors -o "${TEMP_DIR}/${TARBALL_NAME}" "${TARBALL_URL}"

# Verify checksum
echo "Verifying SHA256 checksum for ${TARBALL_NAME}..."
EXPECTED_CHECKSUM=$(awk -v fname="${TARBALL_NAME}" '$2 == fname {print $1; exit}' "${TEMP_DIR}/SHA256SUMS.txt" | tr 'A-F' 'a-f')

if [ -z "$EXPECTED_CHECKSUM" ]; then
  echo "ERROR: Could not find checksum for ${TARBALL_NAME} in SHA256SUMS.txt"
  exit 1
fi

ACTUAL_CHECKSUM=$(sha256_hash "${TEMP_DIR}/${TARBALL_NAME}" | tr 'A-F' 'a-f')

if [ "$EXPECTED_CHECKSUM" != "$ACTUAL_CHECKSUM" ]; then
  echo "ERROR: Checksum verification failed!"
  echo "  Expected: $EXPECTED_CHECKSUM"
  echo "  Got:      $ACTUAL_CHECKSUM"
  echo "  The downloaded file may be corrupted or tampered with"
  exit 1
fi

echo "✓ Checksum verification passed for ${TARBALL_NAME}"

# Extract and install binary
echo "Installing binary to ${INSTALL_DIR}..."
if [ "$IS_WINDOWS" = "true" ]; then
  if command -v unzip >/dev/null 2>&1; then
    unzip -qo "${TEMP_DIR}/${TARBALL_NAME}" -d "${INSTALL_DIR}"
  elif command -v 7z >/dev/null 2>&1; then
    7z x -y "-o${INSTALL_DIR}" "${TEMP_DIR}/${TARBALL_NAME}" >/dev/null
  else
    echo "ERROR: unzip or 7z is required for Windows installation"
    exit 1
  fi
  cat > "${INSTALL_DIR}/copilot" <<EOF
#!/usr/bin/env bash
exec "${INSTALL_DIR}/copilot.exe" "\$@"
EOF
  chmod +x "${INSTALL_DIR}/copilot"
else
  maybe_sudo tar -xz -C "${INSTALL_DIR}" -f "${TEMP_DIR}/${TARBALL_NAME}"
  maybe_sudo chmod +x "${INSTALL_DIR}/copilot"
fi

# In rootless mode, add the install dir to PATH for subsequent steps.
# $GITHUB_PATH is the mechanism for persisting PATH additions across steps in GitHub Actions.
if [ "$ROOTLESS" = "true" ]; then
  if [ -n "${GITHUB_PATH:-}" ]; then
    echo "${INSTALL_DIR}" >> "${GITHUB_PATH}"
    echo "  Exported ${INSTALL_DIR} to GITHUB_PATH"
  else
    echo "WARNING: --rootless install complete but \$GITHUB_PATH is unset; add ${INSTALL_DIR} to PATH manually" >&2
  fi
fi

# Verify installation
echo "Verifying Copilot CLI installation..."
if [ "$ROOTLESS" = "true" ]; then
  if [ -x "${INSTALL_DIR}/copilot" ]; then
    "${INSTALL_DIR}/copilot" --version
    echo "✓ Copilot CLI installation complete"
  else
    echo "ERROR: Copilot CLI installation failed - binary not found at ${INSTALL_DIR}/copilot"
    exit 1
  fi
elif command -v copilot >/dev/null 2>&1; then
  copilot --version
  echo "✓ Copilot CLI installation complete"
else
  echo "ERROR: Copilot CLI installation failed - command not found"
  exit 1
fi
