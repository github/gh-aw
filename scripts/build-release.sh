#!/bin/bash
set +o histexpand

# Custom build script for gh-extension-precompile to set version correctly
# This script is called during the release process to build binaries with proper version info
set -e

VERSION="$1"

if [ -z "$VERSION" ]; then
  echo "error: VERSION argument is required" >&2
  exit 1
fi

platforms=(
  darwin-amd64
  darwin-arm64
  freebsd-386
  freebsd-amd64
  freebsd-arm64
  linux-386
  linux-amd64
  linux-arm
  linux-arm64
  android-arm64
  windows-386
  windows-amd64
  windows-arm64
)

echo "Building binaries with version: $VERSION"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_TOOLCHAIN="$(bash "${SCRIPT_DIR}/check-go-toolchain.sh")"

# Create dist directory if it doesn't exist
mkdir -p dist
printf '%s\n' "$BUILD_TOOLCHAIN" > dist/build-toolchain.txt

IFS=$'\n' read -d '' -r -a supported_platforms < <(go tool dist list) || true

for p in "${platforms[@]}"; do
  goos="${p%-*}"
  goarch="${p#*-}"
  
  # Check if platform is supported
  if [[ " ${supported_platforms[*]} " != *" ${goos}/${goarch} "* ]]; then
    echo "warning: skipping unsupported platform $p" >&2
    continue
  fi
  
  ext=""
  if [ "$goos" = "windows" ]; then
    ext=".exe"
  fi
  
  echo "Building gh-aw for $p..."
  # CGO_ENABLED=0 creates a statically-linked binary that works in Alpine containers
  # (Alpine uses musl libc, not glibc, so dynamically-linked binaries fail)
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.isRelease=true" \
    -o "dist/${p}${ext}" \
    ./cmd/gh-aw

  binary_toolchain="$(go version -m "dist/${p}${ext}" | head -1)"
  if [[ "$binary_toolchain" != *": ${BUILD_TOOLCHAIN}" ]]; then
    echo "ERROR: Unexpected build toolchain for ${p}: ${binary_toolchain}" >&2
    exit 1
  fi
done

# Build WebAssembly binary
echo ""
echo "Building gh-aw WebAssembly binary..."
GOOS=js GOARCH=wasm go build \
  -trimpath \
  -ldflags="-s -w" \
  -o "dist/gh-aw.wasm" \
  ./cmd/gh-aw-wasm

# Run wasm-opt if available
if command -v wasm-opt &> /dev/null; then
  echo "Running wasm-opt -Oz (size optimization)..."
  BEFORE=$(wc -c < dist/gh-aw.wasm)
  wasm-opt -Oz --enable-bulk-memory dist/gh-aw.wasm -o dist/gh-aw.opt.wasm && \
    mv dist/gh-aw.opt.wasm dist/gh-aw.wasm
  AFTER=$(wc -c < dist/gh-aw.wasm)
  echo "wasm-opt: $BEFORE -> $AFTER bytes"
fi

# Bundle wasm_exec.js (required Go runtime for loading the wasm binary)
WASM_EXEC_SRC="$(go env GOROOT)/lib/wasm/wasm_exec.js"
if [ ! -f "$WASM_EXEC_SRC" ]; then
  WASM_EXEC_SRC="$(go env GOROOT)/misc/wasm/wasm_exec.js"
fi
if [ ! -f "$WASM_EXEC_SRC" ]; then
  echo "error: wasm_exec.js not found in Go installation" >&2
  exit 1
fi
cp "$WASM_EXEC_SRC" dist/wasm_exec.js
echo "Bundled wasm_exec.js from $WASM_EXEC_SRC"

# Create WASM bundle archive
echo "Creating WASM bundle archive..."
tar -czf "dist/gh-aw-wasm-${VERSION}.tar.gz" -C dist gh-aw.wasm wasm_exec.js

# Remove individual WASM files from dist (they're now in the archive)
rm dist/gh-aw.wasm dist/wasm_exec.js

echo "✓ Created dist/gh-aw-wasm-${VERSION}.tar.gz"

echo ""
echo "Build complete. Binaries:"
ls -lh dist/

# Generate checksums file
echo ""
echo "Generating checksums..."
cd dist
# Use sha256sum if available (Linux), otherwise use shasum (macOS)
if command -v sha256sum &> /dev/null; then
  sha256sum * > checksums.txt
elif command -v shasum &> /dev/null; then
  shasum -a 256 * > checksums.txt
else
  echo "error: neither sha256sum nor shasum is available" >&2
  exit 1
fi
cd ..

echo "Checksums generated:"
cat dist/checksums.txt
