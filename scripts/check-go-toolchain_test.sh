#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TEMP_DIR"' EXIT
cat >"$TEMP_DIR/go" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$TEST_GO_VERSION"
EOF
chmod +x "$TEMP_DIR/go"
for version in go1.27.4 go1.28.0; do
  TEST_GO_VERSION="$version" PATH="$TEMP_DIR:$PATH" bash "$SCRIPT_DIR/check-go-toolchain.sh" >/dev/null
done
for version in go1.25.20 go1.26.9 go1.26.10 go1.26.11 go1.27.0 go1.27.1 go1.27.2 go1.27.3 go1.28rc1 devel; do
  if TEST_GO_VERSION="$version" PATH="$TEMP_DIR:$PATH" bash "$SCRIPT_DIR/check-go-toolchain.sh" >"$TEMP_DIR/output" 2>&1; then
    echo "FAIL: accepted unpatched or nonstable toolchain $version" >&2
    exit 1
  fi
  grep -q 'ERROR:' "$TEMP_DIR/output"
done
echo "PASS: release toolchain floors and stable-version checks"
