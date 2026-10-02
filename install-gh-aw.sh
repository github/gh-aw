#!/bin/bash
set +o histexpand

# Script sync note: install-gh-aw.sh is canonical. actions/setup-cli/install.sh is copied from install-gh-aw.sh.

# Script to download and install gh-aw binary for the current OS and architecture
# Supports: Linux, macOS (Darwin), FreeBSD, Windows (Git Bash/MSYS/Cygwin)
# If no version is specified, it will use "latest"
# SHA256 checksum validation is performed by default to ensure binary integrity.
# 
# Usage: ./install.sh [version] [options]
#
# Examples:
#   ./install.sh                           # Install latest version
#   ./install.sh v1.0.0                    # Install specific version
#   ./install.sh --skip-checksum           # Skip checksum validation
#
# Options:
#   --skip-checksum                   Skip checksum verification
#   --gh-install                      Try gh extension install first

set -e  # Exit on any error

# Parse arguments
SKIP_CHECKSUM=false  # Checksum verification is enabled by default
TRY_GH_INSTALL=false  # Whether to try gh extension install first
VERSION=""

# Check if INPUT_VERSION is set (GitHub Actions context)
if [ -n "$INPUT_VERSION" ]; then
    VERSION="$INPUT_VERSION"
    TRY_GH_INSTALL=true  # In GitHub Actions, try gh install first
fi

for arg in "$@"; do
    case $arg in
        --skip-checksum)
            SKIP_CHECKSUM=true
            ;;
        --gh-install)
            TRY_GH_INSTALL=true
            ;;
        *)
            if [ -z "$VERSION" ]; then
                VERSION="$arg"
            fi
            ;;
    esac
done

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Disable colors when output is captured or color is explicitly disabled.
# NO_COLOR is the no-color.org standard; NO_COLORS covers tools that use the non-standard variant.
# Per the spec, NO_COLOR disables colors even when set to an empty string, so we test with +set.
if [ -n "${CI:-}" ] || [ "${NO_COLOR+set}" = "set" ] || [ "${NO_COLORS+set}" = "set" ] || [ ! -t 1 ] || [ "${TERM:-}" = "dumb" ]; then
    RED=""
    GREEN=""
    YELLOW=""
    BLUE=""
    NC=""
fi

# Function to print colored output
print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if HOME is set
if [ -z "$HOME" ]; then
    print_error "HOME environment variable is not set. Cannot determine installation directory."
    exit 1
fi

# Check if curl is available
if ! command -v curl &> /dev/null; then
    print_error "curl is required but not installed. Please install curl first."
    exit 1
fi

# Check if sha256sum or shasum is available (for checksum verification)
HAS_CHECKSUM_TOOL=false
CHECKSUM_CMD=""
if command -v sha256sum &> /dev/null; then
    HAS_CHECKSUM_TOOL=true
    CHECKSUM_CMD="sha256sum"
elif command -v shasum &> /dev/null; then
    HAS_CHECKSUM_TOOL=true
    CHECKSUM_CMD="shasum -a 256"
fi

if [ "$SKIP_CHECKSUM" = false ] && [ "$HAS_CHECKSUM_TOOL" = false ]; then
    print_warning "Neither sha256sum nor shasum is available. Checksum verification will be skipped."
    print_warning "To suppress this warning, use --skip-checksum flag."
    SKIP_CHECKSUM=true
fi

# Determine OS and architecture
OS=$(uname -s)
ARCH=$(uname -m)

# Normalize OS name
case $OS in
    Linux)
        if [ -n "$ANDROID_ROOT" ]; then
            OS_NAME="android"
        else
            OS_NAME="linux"
        fi
        ;;
    Darwin)
        OS_NAME="darwin"
        ;;
    FreeBSD)
        OS_NAME="freebsd"
        ;;
    MINGW*|MSYS*|CYGWIN*)
        OS_NAME="windows"
        ;;
    *)
        print_error "Unsupported operating system: $OS"
        print_info "Supported operating systems: Linux, macOS (Darwin), FreeBSD, Windows, Android (Termux)"
        exit 1
        ;;
esac

# Normalize architecture name
case $ARCH in
    x86_64|amd64)
        ARCH_NAME="amd64"
        ;;
    aarch64|arm64)
        ARCH_NAME="arm64"
        ;;
    armv7l|armv7)
        ARCH_NAME="arm"
        ;;
    i386|i686)
        ARCH_NAME="386"
        ;;
    *)
        print_error "Unsupported architecture: $ARCH"
        print_info "Supported architectures: x86_64/amd64, aarch64/arm64, armv7l/arm, i386/i686"
        exit 1
        ;;
esac

# Construct platform string
PLATFORM="${OS_NAME}-${ARCH_NAME}"

# Add .exe extension for Windows
if [ "$OS_NAME" = "windows" ]; then
    BINARY_NAME="gh-aw.exe"
else
    BINARY_NAME="gh-aw"
fi

print_info "Detected OS: $OS -> $OS_NAME"
print_info "Detected architecture: $ARCH -> $ARCH_NAME"
print_info "Platform: $PLATFORM"

# Get version (use provided version or default to "latest")
# VERSION is already set from argument parsing
REPO="github/gh-aw"

if [ -z "$VERSION" ]; then
    print_info "No version specified, using 'latest'..."
    VERSION="latest"
else
    print_info "Using specified version: $VERSION"
fi

compare_semver() {
    local candidate="$1"
    local current="$2"
    local candidate_part current_part
    local candidate_parts current_parts
    local i

    IFS=. read -r -a candidate_parts <<< "${candidate#v}"
    IFS=. read -r -a current_parts <<< "${current#v}"
    candidate_parts[2]="${candidate_parts[2]%%+*}"
    current_parts[2]="${current_parts[2]%%+*}"
    for i in 0 1 2; do
        candidate_part="${candidate_parts[$i]}"
        current_part="${current_parts[$i]}"
        if [ "${#candidate_part}" -gt "${#current_part}" ]; then
            return 0
        elif [ "${#candidate_part}" -lt "${#current_part}" ]; then
            return 1
        elif [[ "$candidate_part" > "$current_part" ]]; then
            return 0
        elif [[ "$candidate_part" < "$current_part" ]]; then
            return 1
        fi
    done
    return 1
}

if [[ "$VERSION" =~ ^v(0|[1-9][0-9]*)$ ]]; then
    CHANNEL_MAJOR="${BASH_REMATCH[1]}"
    BEST_TAG=""
    PAGE=1
    PER_PAGE=100

    while :; do
        RELEASES_URL="https://api.github.com/repos/$REPO/releases?per_page=$PER_PAGE&page=$PAGE"
        RELEASES_CURL_ARGS=(-sLf --connect-timeout 15 --max-time 30 -H "Accept: application/vnd.github+json" -H "User-Agent: gh-aw-install-gh-aw.sh" -H "X-GitHub-Api-Version: 2022-11-28")
        if [ -n "${GH_TOKEN:-}" ]; then
            AUTHORIZATION_HEADER="Authorization: Bearer "
            AUTHORIZATION_HEADER+="$GH_TOKEN"
            RELEASES_CURL_ARGS+=(-H "$AUTHORIZATION_HEADER")
        fi
        if ! RELEASES_RESPONSE=$(curl "${RELEASES_CURL_ARGS[@]}" "$RELEASES_URL"); then
            print_error "Failed to resolve stable releases for version channel $VERSION from GitHub API."
            exit 1
        fi

        RELEASE_RECORDS=$(printf '%s\n' "$RELEASES_RESPONSE" | awk '
            function parse_field(    value) {
                value = field
                if (match(value, /^[[:space:]]*"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"/)) {
                    value = substr(value, 1, RLENGTH)
                    sub(/^[[:space:]]*"tag_name"[[:space:]]*:[[:space:]]*"/, "", value)
                    sub(/"$/, "", value)
                    release_tag = value
                } else if (match(value, /^[[:space:]]*"draft"[[:space:]]*:[[:space:]]*(true|false)/)) {
                    sub(/^[[:space:]]*"draft"[[:space:]]*:[[:space:]]*/, "", value)
                    sub(/[[:space:]]+$/, "", value)
                    release_draft = value
                } else if (match(value, /^[[:space:]]*"prerelease"[[:space:]]*:[[:space:]]*(true|false)/)) {
                    sub(/^[[:space:]]*"prerelease"[[:space:]]*:[[:space:]]*/, "", value)
                    sub(/[[:space:]]+$/, "", value)
                    release_prerelease = value
                }
            }
            function emit_release() {
                if (release_tag != "" && release_draft != "" && release_prerelease != "") {
                    print release_tag, release_draft, release_prerelease
                }
            }
            {
                for (i = 1; i <= length($0); i++) {
                    char = substr($0, i, 1)
                    if (in_string) {
                        field = field char
                        if (escaped) {
                            escaped = 0
                        } else if (char == "\\") {
                            escaped = 1
                        } else if (char == "\"") {
                            in_string = 0
                        }
                        continue
                    }

                    if (char == "\"" && depth > 0) {
                        field = field char
                        in_string = 1
                        continue
                    }

                    if (depth == 1 && array_depth == object_array_depth && char == ",") {
                        parse_field()
                        field = ""
                        continue
                    }
                    if (depth == 1 && array_depth == object_array_depth && char == "}") {
                        parse_field()
                        emit_release()
                        depth--
                        field = ""
                        release_tag = ""
                        release_draft = ""
                        release_prerelease = ""
                        continue
                    }

                    if (depth > 0) {
                        field = field char
                    }
                    if (char == "{") {
                        if (depth == 0) {
                            object_array_depth = array_depth
                            field = ""
                            release_tag = ""
                            release_draft = ""
                            release_prerelease = ""
                        }
                        depth++
                    } else if (char == "}") {
                        depth--
                    } else if (char == "[") {
                        array_depth++
                    } else if (char == "]") {
                        array_depth--
                    }
                }
            }
        ')
        PAGE_RELEASE_COUNT=$(printf '%s\n' "$RELEASE_RECORDS" | awk 'NF { count++ } END { print count+0 }')

        while IFS=' ' read -r release_tag draft prerelease; do
            [ -n "$release_tag" ] || continue
            if [ "$draft" != false ] || [ "$prerelease" != false ]; then
                continue
            fi
            if [[ "$release_tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$ ]] &&
                [ "${BASH_REMATCH[1]}" = "$CHANNEL_MAJOR" ]; then
                if [ -z "$BEST_TAG" ] || compare_semver "$release_tag" "$BEST_TAG"; then
                    BEST_TAG="$release_tag"
                fi
            fi
        done <<< "$RELEASE_RECORDS"

        if [ "$PAGE_RELEASE_COUNT" -lt "$PER_PAGE" ]; then
            break
        fi
        PAGE=$((PAGE + 1))
    done

    if [ -z "$BEST_TAG" ]; then
        print_error "No stable release found for version channel $VERSION."
        exit 1
    fi
    print_info "Resolved version channel $VERSION to $BEST_TAG"
    VERSION="$BEST_TAG"
fi

# Try gh extension install if requested (and gh is available)
if [ "$TRY_GH_INSTALL" = true ] && command -v gh &> /dev/null; then
    print_info "Attempting to install gh-aw using 'gh extension install'..."

    # On Windows, `gh extension install` has been observed to hang for the full
    # job timeout (~10 min) without producing any output.  Root-cause hypothesis:
    # the gh CLI executes the newly downloaded gh-aw.exe for verification, and
    # Windows Defender Real-Time Protection blocks that execution while scanning
    # the new binary.  Apply a generous-but-bounded timeout so we fall through to
    # the direct-download path when this happens instead of hanging indefinitely.
    GH_INSTALL_CMD_TIMEOUT=""
    if [[ "$OS_NAME" == "windows" ]] && command -v timeout &>/dev/null; then
        GH_INSTALL_CMD_TIMEOUT="timeout 90"
        print_info "Windows detected: wrapping gh extension install with a 90s timeout"
    fi

    # Call gh extension install directly to avoid command injection
    install_result=0
    if [ -n "$VERSION" ] && [ "$VERSION" != "latest" ]; then
        # shellcheck disable=SC2086  # GH_INSTALL_CMD_TIMEOUT is intentionally unquoted
        $GH_INSTALL_CMD_TIMEOUT gh extension install "$REPO" --force --pin "$VERSION" 2>&1 | tee /tmp/gh-install.log
        install_result=${PIPESTATUS[0]}
    else
        # shellcheck disable=SC2086  # GH_INSTALL_CMD_TIMEOUT is intentionally unquoted
        $GH_INSTALL_CMD_TIMEOUT gh extension install "$REPO" --force 2>&1 | tee /tmp/gh-install.log
        install_result=${PIPESTATUS[0]}
    fi

    # Exit code 124 means the timeout command fired; treat it the same as a
    # failed install so we fall through to the direct-download path below.
    if [ $install_result -eq 124 ]; then
        print_warning "gh extension install timed out (90s) — falling back to manual installation."
        print_warning "This is a known issue on Windows where Defender scans the new binary."
        install_result=1
    fi
    
    if [ $install_result -eq 0 ]; then
        # Verify the installation succeeded
        if gh aw version &> /dev/null; then
            INSTALLED_VERSION=$(gh aw version 2>&1 | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1)
            
            # Ensure we could parse an installed version; empty means verification failed
            if [ -z "$INSTALLED_VERSION" ]; then
                print_warning "gh extension install completed but the installed gh-aw version could not be determined"
                print_info "Falling back to manual installation..."
            else
                # Verify the installed version matches the requested version (if specific version was requested)
                REQUESTED_VERSION="${VERSION%%+*}"
                if [ "$VERSION" != "latest" ] && [ "$INSTALLED_VERSION" != "$REQUESTED_VERSION" ]; then
                    print_warning "Version mismatch: requested $VERSION but gh extension install installed $INSTALLED_VERSION"
                    print_info "Falling back to manual installation to install the correct version..."
                else
                    print_success "Successfully installed gh-aw using gh extension install"
                    print_info "Installed version: $INSTALLED_VERSION"
                    
                    # Set output for GitHub Actions
                    if [ -n "${GITHUB_OUTPUT}" ]; then
                        echo "installed_version=${INSTALLED_VERSION}" >> "${GITHUB_OUTPUT}"
                    fi
                    
                    exit 0
                fi
            fi
        else
            print_warning "gh extension install completed but verification failed"
            print_info "Falling back to manual installation..."
        fi
    else
        print_warning "gh extension install failed, falling back to manual installation..."
        if [ -f /tmp/gh-install.log ]; then
            cat /tmp/gh-install.log
        fi
    fi
elif [ "$TRY_GH_INSTALL" = true ]; then
    print_info "gh CLI not available, proceeding with manual installation..."
fi

# Construct download URL and paths
if [ "$VERSION" = "latest" ]; then
    DOWNLOAD_URL="https://github.com/$REPO/releases/latest/download/$PLATFORM"
    CHECKSUMS_URL="https://github.com/$REPO/releases/latest/download/checksums.txt"
else
    DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/$PLATFORM"
    CHECKSUMS_URL="https://github.com/$REPO/releases/download/$VERSION/checksums.txt"
fi
if [ "$OS_NAME" = "windows" ]; then
    DOWNLOAD_URL="${DOWNLOAD_URL}.exe"
fi

# For latest installs, keep a versioned fallback URL in case the latest redirect
# endpoint intermittently returns gateway errors.
LATEST_TAG=""
FALLBACK_DOWNLOAD_URL=""
FALLBACK_CHECKSUMS_URL=""
if [ "$VERSION" = "latest" ]; then
    if LATEST_RELEASE_RESPONSE=$(curl -sLf --connect-timeout 15 --max-time 30 "https://api.github.com/repos/$REPO/releases/latest"); then
        LATEST_TAG=$(printf '%s' "$LATEST_RELEASE_RESPONSE" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
    else
        print_warning "Failed to resolve latest release tag from GitHub API."
    fi
    if [ -n "$LATEST_TAG" ]; then
        FALLBACK_DOWNLOAD_URL="https://github.com/$REPO/releases/download/$LATEST_TAG/$PLATFORM"
        FALLBACK_CHECKSUMS_URL="https://github.com/$REPO/releases/download/$LATEST_TAG/checksums.txt"
        if [ "$OS_NAME" = "windows" ]; then
            FALLBACK_DOWNLOAD_URL="${FALLBACK_DOWNLOAD_URL}.exe"
        fi
        print_info "Prepared latest fallback URL for resolved tag: $LATEST_TAG"
    else
        print_warning "Could not resolve latest release tag; install will use latest redirect URL only."
    fi
fi

INSTALL_DIR="$HOME/.local/share/gh/extensions/gh-aw"
BINARY_PATH="$INSTALL_DIR/$BINARY_NAME"

print_info "Download URL: $DOWNLOAD_URL"
print_info "Installation directory: $INSTALL_DIR"

# Create the installation directory if it doesn't exist
if [ ! -d "$INSTALL_DIR" ]; then
    print_info "Creating installation directory..."
    mkdir -p "$INSTALL_DIR"
fi

# Check if binary already exists
if [ -e "$BINARY_PATH" ] || [ -L "$BINARY_PATH" ]; then
    print_warning "Binary '$BINARY_PATH' already exists. It will be replaced after verification."
fi

if ! STAGING_DIR=$(mktemp -d "$INSTALL_DIR/.gh-aw-install.XXXXXX"); then
    print_error "Failed to create temporary installation directory in '$INSTALL_DIR'."
    exit 1
fi
STAGED_BINARY_PATH="$STAGING_DIR/$BINARY_NAME"
CHECKSUMS_PATH="$STAGING_DIR/checksums.txt"
trap 'rm -rf -- "$STAGING_DIR"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Download the binary with retry logic
print_info "Downloading gh-aw binary..."
MAX_RETRIES=3
RETRY_DELAY=2
download_binary_with_retry() {
    local url="$1"
    local delay="$RETRY_DELAY"

    for attempt in $(seq 1 $MAX_RETRIES); do
        if curl -L -f --connect-timeout 15 --max-time 120 -o "$STAGED_BINARY_PATH" "$url"; then
            print_success "Binary downloaded successfully"
            return 0
        fi

        if [ "$attempt" -lt "$MAX_RETRIES" ]; then
            print_warning "Download attempt $attempt failed. Retrying in ${delay}s..."
            sleep $delay
            delay=$((delay * 2))
        fi
    done

    return 1
}

if ! download_binary_with_retry "$DOWNLOAD_URL"; then
    if [ -n "$FALLBACK_DOWNLOAD_URL" ]; then
        print_warning "Failed to download from latest redirect URL. Retrying with resolved tag URL ($LATEST_TAG)."
        DOWNLOAD_URL="$FALLBACK_DOWNLOAD_URL"
        CHECKSUMS_URL="$FALLBACK_CHECKSUMS_URL"
        if ! download_binary_with_retry "$DOWNLOAD_URL"; then
            print_error "Failed to download binary from $DOWNLOAD_URL after $MAX_RETRIES attempts"
            print_info "Please check if the version and platform combination exists in the releases."
            exit 1
        fi
    else
        print_error "Failed to download binary from $DOWNLOAD_URL after $MAX_RETRIES attempts"
        print_info "Please check if the version and platform combination exists in the releases."
        exit 1
    fi
fi

# Download and verify checksums if not skipped
if [ "$SKIP_CHECKSUM" = false ]; then
    print_info "Downloading checksums file..."
    CHECKSUMS_DOWNLOADED=false
    
    for attempt in $(seq 1 $MAX_RETRIES); do
        if curl -L -f --connect-timeout 15 --max-time 60 -o "$CHECKSUMS_PATH" "$CHECKSUMS_URL" 2>/dev/null; then
            CHECKSUMS_DOWNLOADED=true
            print_success "Checksums file downloaded successfully"
            break
        else
            if [ "$attempt" -eq "$MAX_RETRIES" ]; then
                print_warning "Failed to download checksums file after $MAX_RETRIES attempts"
                print_warning "Checksum verification will be skipped for this version."
                print_info "This may occur for older releases that don't include checksums."
                break
            else
                print_warning "Checksum download attempt $attempt failed. Retrying in 2s..."
                sleep 2
            fi
        fi
    done
    
    # Verify checksum if we downloaded it successfully
    if [ "$CHECKSUMS_DOWNLOADED" = true ]; then
        print_info "Verifying binary checksum..."
        
        # Determine the expected filename in the checksums file
        EXPECTED_FILENAME="$PLATFORM"
        if [ "$OS_NAME" = "windows" ]; then
            EXPECTED_FILENAME="${PLATFORM}.exe"
        fi
        
        # Extract the expected checksum from the checksums file (exact filename match on field 2)
        EXPECTED_CHECKSUM=$(awk -v f="$EXPECTED_FILENAME" '$2 == f {print $1}' "$CHECKSUMS_PATH")
        
        if [ -z "$EXPECTED_CHECKSUM" ]; then
            print_warning "Checksum for $EXPECTED_FILENAME not found in checksums file"
            print_warning "Checksum verification will be skipped."
        else
            # Compute the actual checksum of the downloaded binary
            ACTUAL_CHECKSUM=$($CHECKSUM_CMD "$STAGED_BINARY_PATH" | awk '{print $1}')
            
            if [ "$ACTUAL_CHECKSUM" = "$EXPECTED_CHECKSUM" ]; then
                print_success "Checksum verification passed!"
                print_info "Expected: $EXPECTED_CHECKSUM"
                print_info "Actual:   $ACTUAL_CHECKSUM"
            else
                print_error "Checksum verification failed!"
                print_error "Expected: $EXPECTED_CHECKSUM"
                print_error "Actual:   $ACTUAL_CHECKSUM"
                print_error "The downloaded binary may be corrupted or tampered with."
                print_info "To skip checksum verification, use: ./install-gh-aw.sh $VERSION --skip-checksum"
                exit 1
            fi
        fi
        
        # Clean up checksums file
        rm -f "$CHECKSUMS_PATH"
    fi
else
    print_warning "Checksum verification skipped (--skip-checksum flag used)"
fi

# Make it executable
print_info "Making binary executable..."
chmod +x "$STAGED_BINARY_PATH"

# On Windows, executing a freshly downloaded binary may stall while Windows Defender
# scans it.  Wrap verification calls with a timeout so the script doesn't hang.
BINARY_EXEC_TIMEOUT=""
if [ "$OS_NAME" = "windows" ] && command -v timeout &>/dev/null; then
    BINARY_EXEC_TIMEOUT="timeout 30"
    print_info "Windows detected: wrapping binary verification with a 30s timeout"
fi

# Verify the binary
print_info "Verifying binary..."
# shellcheck disable=SC2086
if $BINARY_EXEC_TIMEOUT "$STAGED_BINARY_PATH" --help > /dev/null 2>&1; then
    print_success "Binary is working correctly!"
else
    if [ "$OS_NAME" = "windows" ]; then
        print_warning "Binary verification timed out — Windows Defender may still be scanning the binary."
        print_warning "After installation completes, verify manually with: '$BINARY_PATH' --help"
    else
        print_error "Binary verification failed. The downloaded file may be corrupted or incompatible."
        exit 1
    fi
fi

if [ -d "$BINARY_PATH" ]; then
    print_error "Cannot install gh-aw: '$BINARY_PATH' is a directory."
    exit 1
fi

if ! mv -f -- "$STAGED_BINARY_PATH" "$BINARY_PATH"; then
    print_error "Failed to replace gh-aw at '$BINARY_PATH'."
    exit 1
fi

# Show file info
FILE_SIZE=$(ls -lh "$BINARY_PATH" | awk '{print $5}')
print_success "Installation complete!"
print_info "Binary location: $BINARY_PATH"
print_info "Binary size: $FILE_SIZE"
print_info "Version: $VERSION"

# Show usage info
print_info ""
print_info "You can now use gh-aw with the gh CLI:"
print_info "  gh aw --help"
print_info "  gh aw version"

# Show version
print_info ""
print_info "Running gh-aw version check..."
# shellcheck disable=SC2086
$BINARY_EXEC_TIMEOUT "$BINARY_PATH" version || print_warning "Version check timed out (Windows Defender may still be scanning the binary)."

# Set output for GitHub Actions
if [ -n "${GITHUB_OUTPUT}" ]; then
    echo "installed_version=${VERSION}" >> "${GITHUB_OUTPUT}"
fi
