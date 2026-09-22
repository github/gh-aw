#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

# Downloads and verifies a complete gh-aw-threat-detection release, then atomically
# updates the compiler-owned version and SHA-256 matrix.
#
# Usage: scripts/update-threat-detect-pins.sh VERSION

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
RELEASE_BASE_URL="${THREAT_DETECT_RELEASE_BASE_URL:-https://github.com/github/gh-aw-threat-detection/releases/download}"
CONSTANTS_FILE="${THREAT_DETECT_CONSTANTS_FILE:-$REPO_ROOT/pkg/constants/version_constants.go}"
CONSTANTS_TEST_FILE="${THREAT_DETECT_CONSTANTS_TEST_FILE:-$REPO_ROOT/pkg/constants/version_constants_test.go}"
ASSETS=(
  threat-detect-linux-amd64
  threat-detect-linux-arm64
  threat-detect-darwin-x64
  threat-detect-darwin-arm64
)

if [ "$#" -ne 1 ]; then
  echo "Usage: $0 VERSION" >&2
  exit 1
fi

VERSION="$1"
if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "error: VERSION must be a semantic release tag such as v0.5.2" >&2
  exit 1
fi
if [[ ! "$RELEASE_BASE_URL" =~ ^https://[^[:space:]]+$ ]]; then
  echo "error: release base URL must use HTTPS" >&2
  exit 1
fi
if [ ! -f "$CONSTANTS_FILE" ]; then
  echo "error: constants file not found: $CONSTANTS_FILE" >&2
  exit 1
fi
if [ ! -f "$CONSTANTS_TEST_FILE" ]; then
  echo "error: constants test file not found: $CONSTANTS_TEST_FILE" >&2
  exit 1
fi

sha256_hash() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
  else
    echo "error: neither sha256sum nor shasum is available" >&2
    exit 1
  fi
}

TEMP_DIR="$(mktemp -d)"
UPDATED_FILE="$(mktemp "${CONSTANTS_FILE}.XXXXXX")"
UPDATED_TEST_FILE="$(mktemp "${CONSTANTS_TEST_FILE}.XXXXXX")"
cleanup() {
  rm -rf "$TEMP_DIR"
  rm -f "$UPDATED_FILE"
  rm -f "$UPDATED_TEST_FILE"
}
trap cleanup EXIT

RELEASE_URL="${RELEASE_BASE_URL%/}/${VERSION}"
MANIFEST="$TEMP_DIR/checksums.txt"
echo "Downloading ${RELEASE_URL}/checksums.txt"
curl -fsSL --retry 5 --retry-delay 2 --retry-all-errors \
  -o "$MANIFEST" "${RELEASE_URL}/checksums.txt"

declare -A EXPECTED_DIGESTS=()
while IFS= read -r line || [ -n "$line" ]; do
  line="${line%$'\r'}"
  [ -z "$line" ] && continue

  if [[ ! "$line" =~ ^([0-9a-f]{64})[[:space:]]+(\*?)([^[:space:]]+)$ ]]; then
    echo "error: malformed checksums.txt entry: $line" >&2
    exit 1
  fi
  digest="${BASH_REMATCH[1]}"
  asset="${BASH_REMATCH[3]}"
  if [ -n "${EXPECTED_DIGESTS[$asset]+set}" ]; then
    echo "error: duplicate checksums.txt entry for $asset" >&2
    exit 1
  fi
  EXPECTED_DIGESTS["$asset"]="$digest"
done <"$MANIFEST"

for asset in "${!EXPECTED_DIGESTS[@]}"; do
  supported=false
  for required_asset in "${ASSETS[@]}"; do
    if [ "$asset" = "$required_asset" ]; then
      supported=true
      break
    fi
  done
  if [ "$supported" != "true" ]; then
    echo "error: checksums.txt contains unexpected asset $asset" >&2
    exit 1
  fi
done
for asset in "${ASSETS[@]}"; do
  if [ -z "${EXPECTED_DIGESTS[$asset]+set}" ]; then
    echo "error: checksums.txt is missing $asset" >&2
    exit 1
  fi
done
if [ "${#EXPECTED_DIGESTS[@]}" -ne "${#ASSETS[@]}" ]; then
  echo "error: checksums.txt must contain exactly ${#ASSETS[@]} release assets" >&2
  exit 1
fi

for asset in "${ASSETS[@]}"; do
  echo "Downloading and verifying $asset"
  curl -fsSL --retry 5 --retry-delay 2 --retry-all-errors \
    -o "$TEMP_DIR/$asset" "${RELEASE_URL}/${asset}"
  actual_digest="$(sha256_hash "$TEMP_DIR/$asset")"
  if [ "$actual_digest" != "${EXPECTED_DIGESTS[$asset]}" ]; then
    echo "error: SHA-256 mismatch for $asset" >&2
    echo "  manifest: ${EXPECTED_DIGESTS[$asset]}" >&2
    echo "  download: $actual_digest" >&2
    exit 1
  fi
done

awk \
  -v version="$VERSION" \
  -v linux_amd64="${EXPECTED_DIGESTS[threat-detect-linux-amd64]}" \
  -v linux_arm64="${EXPECTED_DIGESTS[threat-detect-linux-arm64]}" \
  -v darwin_x64="${EXPECTED_DIGESTS[threat-detect-darwin-x64]}" \
  -v darwin_arm64="${EXPECTED_DIGESTS[threat-detect-darwin-arm64]}" '
  BEGIN { version_count = 0; matrix_count = 0; in_matrix = 0 }
  /^const DefaultThreatDetectVersion Version = / {
    print "const DefaultThreatDetectVersion Version = \"" version "\""
    version_count++
    next
  }
  /^var DefaultThreatDetectSHA256 = map\[string\]string\{/ {
    print "var DefaultThreatDetectSHA256 = map[string]string{"
    print "\t\"threat-detect-linux-amd64\":  \"" linux_amd64 "\","
    print "\t\"threat-detect-linux-arm64\":  \"" linux_arm64 "\","
    print "\t\"threat-detect-darwin-x64\":   \"" darwin_x64 "\","
    print "\t\"threat-detect-darwin-arm64\": \"" darwin_arm64 "\","
    print "}"
    matrix_count++
    in_matrix = 1
    next
  }
  in_matrix {
    if ($0 == "}") {
      in_matrix = 0
    }
    next
  }
  { print }
  END {
    if (in_matrix || version_count != 1 || matrix_count != 1) {
      exit 2
    }
  }
' "$CONSTANTS_FILE" >"$UPDATED_FILE" || {
  echo "error: could not uniquely locate the threat-detect version and digest matrix" >&2
  exit 1
}

awk \
  -v version="$VERSION" \
  -v linux_amd64="${EXPECTED_DIGESTS[threat-detect-linux-amd64]}" \
  -v linux_arm64="${EXPECTED_DIGESTS[threat-detect-linux-arm64]}" \
  -v darwin_x64="${EXPECTED_DIGESTS[threat-detect-darwin-x64]}" \
  -v darwin_arm64="${EXPECTED_DIGESTS[threat-detect-darwin-arm64]}" '
  BEGIN {
    table_version_count = 0
    assertion_version_count = 0
    matrix_count = 0
    message_count = 0
    in_matrix = 0
  }
  /^[[:space:]]*\{"Threat Detect", DefaultThreatDetectVersion, / {
    print "\t\t{\"Threat Detect\", DefaultThreatDetectVersion, \"" version "\"},"
    table_version_count++
    next
  }
  /^[[:space:]]*if DefaultThreatDetectVersion != / {
    print "\tif DefaultThreatDetectVersion != \"" version "\" {"
    assertion_version_count++
    next
  }
  /^[[:space:]]*t\.Fatalf\("DefaultThreatDetectVersion = / {
    print "\t\tt.Fatalf(\"DefaultThreatDetectVersion = %q, want " version "; update the version and reviewed digest table together\", DefaultThreatDetectVersion)"
    next
  }
  /^[[:space:]]*expectedDigests := map\[string\]string\{/ {
    print "\texpectedDigests := map[string]string{"
    print "\t\t\"threat-detect-linux-amd64\":  \"" linux_amd64 "\","
    print "\t\t\"threat-detect-linux-arm64\":  \"" linux_arm64 "\","
    print "\t\t\"threat-detect-darwin-x64\":   \"" darwin_x64 "\","
    print "\t\t\"threat-detect-darwin-arm64\": \"" darwin_arm64 "\","
    print "\t}"
    matrix_count++
    in_matrix = 1
    next
  }
  in_matrix {
    if ($0 == "\t}") {
      in_matrix = 0
    }
    next
  }
  /^[[:space:]]*t\.Errorf\("DefaultThreatDetectSHA256\[%q\] = / {
    print "\t\t\tt.Errorf(\"DefaultThreatDetectSHA256[%q] = %q, want the reviewed " version " digest %q; update the version and reviewed digest table together\", asset, got, want)"
    message_count++
    next
  }
  { print }
  END {
    if (in_matrix || table_version_count != 1 || assertion_version_count != 1 || matrix_count != 1 || message_count != 1) {
      exit 2
    }
  }
' "$CONSTANTS_TEST_FILE" >"$UPDATED_TEST_FILE" || {
  echo "error: could not uniquely locate the threat-detect review assertions" >&2
  exit 1
}

chmod 0644 "$UPDATED_FILE"
chmod 0644 "$UPDATED_TEST_FILE"
mv "$UPDATED_FILE" "$CONSTANTS_FILE"
mv "$UPDATED_TEST_FILE" "$CONSTANTS_TEST_FILE"
echo "Updated compiler pins and review assertions to verified threat-detect release $VERSION"
echo "Run 'make fmt && make recompile' and review the resulting diff."
