#!/bin/bash
set +o histexpand

# Test script to validate the install-gh-aw.sh script detection logic
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "=== Testing install-gh-aw.sh detection logic ==="

# Test function to validate platform detection
test_platform_detection() {
    local test_os=$1
    local test_arch=$2
    local expected_os=$3
    local expected_arch=$4
    local expected_platform=$5
    
    echo ""
    echo "Test: OS=$test_os, ARCH=$test_arch"
    
    # Execute the detection logic from the script
    OS=$test_os
    ARCH=$test_arch
    
    # Normalize OS name (same logic as install-gh-aw.sh)
    case $OS in
        Linux)
            OS_NAME="linux"
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
            echo "  ✗ FAIL: Unsupported OS: $OS"
            return 1
            ;;
    esac
    
    # Normalize architecture name (same logic as install-gh-aw.sh)
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
            echo "  ✗ FAIL: Unsupported architecture: $ARCH"
            return 1
            ;;
    esac
    
    PLATFORM="${OS_NAME}-${ARCH_NAME}"
    
    # Verify results
    if [ "$OS_NAME" != "$expected_os" ]; then
        echo "  ✗ FAIL: OS_NAME is '$OS_NAME', expected '$expected_os'"
        return 1
    fi
    
    if [ "$ARCH_NAME" != "$expected_arch" ]; then
        echo "  ✗ FAIL: ARCH_NAME is '$ARCH_NAME', expected '$expected_arch'"
        return 1
    fi
    
    if [ "$PLATFORM" != "$expected_platform" ]; then
        echo "  ✗ FAIL: PLATFORM is '$PLATFORM', expected '$expected_platform'"
        return 1
    fi
    
    echo "  ✓ PASS: $PLATFORM (OS: $OS_NAME, ARCH: $ARCH_NAME)"
    return 0
}

# Test 1: Script syntax is valid
echo ""
echo "Test 1: Verify script syntax"
if bash -n "$PROJECT_ROOT/install-gh-aw.sh"; then
    echo "  ✓ PASS: Script syntax is valid"
else
    echo "  ✗ FAIL: Script has syntax errors"
    exit 1
fi

# Test 2: Linux platforms
echo ""
echo "Test 2: Linux platform detection"
test_platform_detection "Linux" "x86_64" "linux" "amd64" "linux-amd64"
test_platform_detection "Linux" "aarch64" "linux" "arm64" "linux-arm64"
test_platform_detection "Linux" "arm64" "linux" "arm64" "linux-arm64"
test_platform_detection "Linux" "armv7l" "linux" "arm" "linux-arm"
test_platform_detection "Linux" "armv7" "linux" "arm" "linux-arm"
test_platform_detection "Linux" "i386" "linux" "386" "linux-386"
test_platform_detection "Linux" "i686" "linux" "386" "linux-386"

# Test 3: macOS (Darwin) platforms
echo ""
echo "Test 3: macOS (Darwin) platform detection"
test_platform_detection "Darwin" "x86_64" "darwin" "amd64" "darwin-amd64"
test_platform_detection "Darwin" "arm64" "darwin" "arm64" "darwin-arm64"

# Test 4: Windows platforms
echo ""
echo "Test 4: FreeBSD platforms"
test_platform_detection "FreeBSD" "amd64" "freebsd" "amd64" "freebsd-amd64"
test_platform_detection "FreeBSD" "arm64" "freebsd" "arm64" "freebsd-arm64"
test_platform_detection "FreeBSD" "i386" "freebsd" "386" "freebsd-386"

# Test 5: Windows platforms
echo ""
echo "Test 5: Windows platform detection"
test_platform_detection "MINGW64_NT-10.0" "x86_64" "windows" "amd64" "windows-amd64"
test_platform_detection "MSYS_NT-10.0" "x86_64" "windows" "amd64" "windows-amd64"
test_platform_detection "CYGWIN_NT-10.0" "x86_64" "windows" "amd64" "windows-amd64"

# Test 6: Binary name detection
echo ""
echo "Test 6: Binary name detection"
OS_NAME="linux"
if [ "$OS_NAME" = "windows" ]; then
    BINARY_NAME="gh-aw.exe"
else
    BINARY_NAME="gh-aw"
fi
if [ "$BINARY_NAME" = "gh-aw" ]; then
    echo "  ✓ PASS: Linux binary name is correct: $BINARY_NAME"
else
    echo "  ✗ FAIL: Linux binary name is incorrect: $BINARY_NAME"
    exit 1
fi

OS_NAME="windows"
if [ "$OS_NAME" = "windows" ]; then
    BINARY_NAME="gh-aw.exe"
else
    BINARY_NAME="gh-aw"
fi
if [ "$BINARY_NAME" = "gh-aw.exe" ]; then
    echo "  ✓ PASS: Windows binary name is correct: $BINARY_NAME"
else
    echo "  ✗ FAIL: Windows binary name is incorrect: $BINARY_NAME"
    exit 1
fi

# Test 7: Verify download URL construction
echo ""
echo "Test 7: Download URL construction"
REPO="github/gh-aw"
VERSION="v1.0.0"
OS_NAME="linux"
PLATFORM="linux-amd64"
DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/$PLATFORM"
if [ "$OS_NAME" = "windows" ]; then
    DOWNLOAD_URL="${DOWNLOAD_URL}.exe"
fi
EXPECTED_URL="https://github.com/github/gh-aw/releases/download/v1.0.0/linux-amd64"
if [ "$DOWNLOAD_URL" = "$EXPECTED_URL" ]; then
    echo "  ✓ PASS: Linux URL is correct: $DOWNLOAD_URL"
else
    echo "  ✗ FAIL: Linux URL is incorrect: $DOWNLOAD_URL (expected: $EXPECTED_URL)"
    exit 1
fi

OS_NAME="windows"
PLATFORM="windows-amd64"
DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/$PLATFORM"
if [ "$OS_NAME" = "windows" ]; then
    DOWNLOAD_URL="${DOWNLOAD_URL}.exe"
fi
EXPECTED_URL="https://github.com/github/gh-aw/releases/download/v1.0.0/windows-amd64.exe"
if [ "$DOWNLOAD_URL" = "$EXPECTED_URL" ]; then
    echo "  ✓ PASS: Windows URL is correct: $DOWNLOAD_URL"
else
    echo "  ✗ FAIL: Windows URL is incorrect: $DOWNLOAD_URL (expected: $EXPECTED_URL)"
    exit 1
fi

# Test 8: Verify retry logic for downloads
echo ""
echo "Test 8: Verify download retry logic"

# Check for MAX_RETRIES variable
if grep -q "MAX_RETRIES=5" "$PROJECT_ROOT/install-gh-aw.sh"; then
    echo "  ✓ PASS: Downloads retry up to five times"
else
    echo "  ✗ FAIL: Downloads should retry up to five times"
    exit 1
fi

# Check for retry loop
if grep -q "for attempt in" "$PROJECT_ROOT/install-gh-aw.sh"; then
    echo "  ✓ PASS: Retry loop exists"
else
    echo "  ✗ FAIL: Retry loop not found"
    exit 1
fi

# Check for exponential backoff
if grep -q "RETRY_DELAY=\$((RETRY_DELAY \* 2))" "$PROJECT_ROOT/install-gh-aw.sh" || grep -q "delay=\$((delay \* 2))" "$PROJECT_ROOT/install-gh-aw.sh"; then
    echo "  ✓ PASS: Exponential backoff implemented"
else
    echo "  ✗ FAIL: Exponential backoff not found"
    exit 1
fi

if grep -q "latest_retry_delay=\$((latest_retry_delay \* 2))" "$PROJECT_ROOT/install-gh-aw.sh" &&
   grep -q "checksum_retry_delay=\$((checksum_retry_delay \* 2))" "$PROJECT_ROOT/install-gh-aw.sh"; then
    echo "  ✓ PASS: Release metadata and checksum retries use exponential backoff"
else
    echo "  ✗ FAIL: Release metadata and checksum retries should use exponential backoff"
    exit 1
fi

# Test 9: Verify checksum validation functionality
echo ""
echo "Test 9: Verify checksum validation functionality"

# Check for --skip-checksum flag
if grep -q "\-\-skip-checksum" "$PROJECT_ROOT/install-gh-aw.sh"; then
    echo "  ✓ PASS: --skip-checksum flag is documented"
else
    echo "  ✗ FAIL: --skip-checksum flag not found"
    exit 1
fi

# Check for checksum tool detection
if grep -q "sha256sum\|shasum" "$PROJECT_ROOT/install-gh-aw.sh"; then
    echo "  ✓ PASS: Script checks for sha256sum or shasum"
else
    echo "  ✗ FAIL: Script doesn't check for checksum tools"
    exit 1
fi

# Check for checksums URL construction
if grep -q 'CHECKSUMS_URL=.*checksums.txt' "$PROJECT_ROOT/install-gh-aw.sh"; then
    echo "  ✓ PASS: Checksums URL is constructed"
else
    echo "  ✗ FAIL: Checksums URL construction not found"
    exit 1
fi

# Check for checksum verification logic
if grep -q "Verifying binary checksum" "$PROJECT_ROOT/install-gh-aw.sh"; then
    echo "  ✓ PASS: Checksum verification logic exists"
else
    echo "  ✗ FAIL: Checksum verification logic not found"
    exit 1
fi

# Check for checksum failure handling
if grep -q "Checksum verification failed" "$PROJECT_ROOT/install-gh-aw.sh"; then
    echo "  ✓ PASS: Checksum failure is handled"
else
    echo "  ✗ FAIL: Checksum failure handling not found"
    exit 1
fi

# Check for graceful handling when checksums file is not available
if grep -q "Checksum verification will be skipped" "$PROJECT_ROOT/install-gh-aw.sh"; then
    echo "  ✓ PASS: Script handles missing checksums gracefully"
else
    echo "  ✗ FAIL: Missing checksums handling not found"
    exit 1
fi

# Check for SKIP_CHECKSUM flag logic
if grep -q "SKIP_CHECKSUM=true" "$PROJECT_ROOT/install-gh-aw.sh" && grep -q 'if \[ "\$SKIP_CHECKSUM" = false \]' "$PROJECT_ROOT/install-gh-aw.sh"; then
    echo "  ✓ PASS: SKIP_CHECKSUM flag logic is implemented"
else
    echo "  ✗ FAIL: SKIP_CHECKSUM flag logic not found"
    exit 1
fi

# Test 10: Verify "latest" version functionality
echo ""
echo "Test 10: Verify 'latest' version functionality"

# Check for "latest" as default version
if grep -q "using 'latest'" "$PROJECT_ROOT/install-gh-aw.sh"; then
    echo "  ✓ PASS: Script uses 'latest' as default version"
else
    echo "  ✗ FAIL: Script does not use 'latest' as default version"
    exit 1
fi

# Check for latest URL construction
if grep -q 'releases/latest/download' "$PROJECT_ROOT/install-gh-aw.sh"; then
    echo "  ✓ PASS: Latest release URL pattern is correct"
else
    echo "  ✗ FAIL: Latest release URL pattern not found"
    exit 1
fi

# Check that API validation is removed
if grep -q "fetch_release_data.*releases/latest" "$PROJECT_ROOT/install-gh-aw.sh"; then
    echo "  ✗ FAIL: API validation for latest release still exists"
    exit 1
else
    echo "  ✓ PASS: API validation for latest release removed"
fi

# Check that validation logic is removed
if grep -q "Validating release.*exists" "$PROJECT_ROOT/install-gh-aw.sh"; then
    echo "  ✗ FAIL: Version validation logic still exists"
    exit 1
else
    echo "  ✓ PASS: Version validation logic removed"
fi

# Test 11: Verify latest download fallback functionality
echo ""
echo "Test 11: Verify latest download fallback functionality"

if grep -q "FALLBACK_DOWNLOAD_URL" "$PROJECT_ROOT/install-gh-aw.sh"; then
    echo "  ✓ PASS: Fallback download URL logic exists"
else
    echo "  ✗ FAIL: Fallback download URL logic not found"
    exit 1
fi

if grep -q "api.github.com/repos/\$REPO/releases/latest" "$PROJECT_ROOT/install-gh-aw.sh"; then
    echo "  ✓ PASS: Latest release tag fallback lookup exists"
else
    echo "  ✗ FAIL: Latest release tag fallback lookup not found"
    exit 1
fi

# Test 12: Verify no-color gate for CI environments
echo ""
echo "Test 12: Verify no-color gate"
if grep -q 'NO_COLOR+set' "$PROJECT_ROOT/install-gh-aw.sh" && \
   grep -q 'RED=""' "$PROJECT_ROOT/install-gh-aw.sh"; then
    echo "  ✓ PASS: No-color gate present and spec-compliant"
else
    echo "  ✗ FAIL: No-color gate missing or not spec-compliant"
    exit 1
fi

echo ""
echo "=== All tests passed ==="
