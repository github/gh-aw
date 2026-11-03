#!/bin/bash
set +o histexpand

# Test script to validate the build-release.sh script
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "=== Testing build-release.sh script ==="

# Create a temporary directory for testing
TEST_DIR=$(mktemp -d)
trap "rm -rf $TEST_DIR" EXIT

echo "Test directory: $TEST_DIR"

# Test 1: Script fails without version argument
echo ""
echo "Test 1: Verify script requires version argument"
if $PROJECT_ROOT/scripts/build-release.sh 2>/dev/null; then
  echo "FAIL: Script should fail without version argument"
  exit 1
else
  echo "PASS: Script correctly requires version argument"
fi

# Test 2: Build a single platform with version
echo ""
echo "Test 2: Build single platform with version"
cd "$PROJECT_ROOT"
export GOARCH=amd64
export GOOS=linux

# Temporarily modify the build script to only build linux-amd64
TEMP_SCRIPT="$TEST_DIR/build-test.sh"
cat > "$TEMP_SCRIPT" <<'EOF'
#!/bin/bash
set -e

VERSION="$1"

if [ -z "$VERSION" ]; then
  echo "error: VERSION argument is required" >&2
  exit 1
fi

platforms=(
  linux-amd64
)

echo "Building binaries with version: $VERSION"

mkdir -p dist

IFS=$'\n' read -d '' -r -a supported_platforms < <(go tool dist list) || true

for p in "${platforms[@]}"; do
  goos="${p%-*}"
  goarch="${p#*-}"
  
  if [[ " ${supported_platforms[*]} " != *" ${goos}/${goarch} "* ]]; then
    echo "warning: skipping unsupported platform $p" >&2
    continue
  fi
  
  ext=""
  if [ "$goos" = "windows" ]; then
    ext=".exe"
  fi
  
  echo "Building gh-aw for $p..."
  GOOS="$goos" GOARCH="$goarch" go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.isRelease=true" \
    -o "dist/${p}${ext}" \
    ./cmd/gh-aw
done

echo "Build complete."
EOF

chmod +x "$TEMP_SCRIPT"

# Build with test version
TEST_VERSION="v1.2.3-test"
"$TEMP_SCRIPT" "$TEST_VERSION"

# Check that binary was created
if [ ! -f "dist/linux-amd64" ]; then
  echo "FAIL: gh-aw binary was not created"
  exit 1
fi

# Check that version is embedded in gh-aw binary
BINARY_VERSION=$(./dist/linux-amd64 version 2>&1 | grep -o "v[0-9]\+\.[0-9]\+\.[0-9]\+-test" || echo "")
if [ "$BINARY_VERSION" != "$TEST_VERSION" ]; then
  echo "FAIL: gh-aw binary version is '$BINARY_VERSION', expected '$TEST_VERSION'"
  ./dist/linux-amd64 version
  exit 1
fi

echo "PASS: gh-aw binary built with correct version: $BINARY_VERSION"

# Test 3: Verify version is not "dev"
echo ""
echo "Test 3: Verify version is not 'dev'"
if echo "$BINARY_VERSION" | grep -q "dev"; then
  echo "FAIL: gh-aw binary version should not contain 'dev'"
  exit 1
fi
echo "PASS: Binary version does not contain 'dev'"

# Test 4: Build Windows 386 binary for release
echo ""
echo "Test 4: Verify Windows 386 is included in release builds"
if ! grep -Eq '^[[:space:]]+windows-386$' "$PROJECT_ROOT/scripts/build-release.sh"; then
  echo "FAIL: Windows 386 is not included in the release platform list"
  exit 1
fi

CGO_ENABLED=0 GOOS=windows GOARCH=386 go build \
  -trimpath \
  -ldflags="-s -w -X main.version=${TEST_VERSION} -X main.isRelease=true" \
  -o "$TEST_DIR/windows-386.exe" \
  ./cmd/gh-aw

if [ ! -f "$TEST_DIR/windows-386.exe" ]; then
  echo "FAIL: Windows 386 binary was not created"
  exit 1
fi

echo "PASS: Windows 386 release binary built"

# Clean up dist directory
rm -rf dist

echo ""
echo "=== All tests passed ==="
