#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
UPDATER="$SCRIPT_DIR/update-threat-detect-pins.sh"
TESTS_PASSED=0
TESTS_FAILED=0

pass() { echo "PASS: $1"; TESTS_PASSED=$((TESTS_PASSED + 1)); }
fail() { echo "FAIL: $1"; echo "  $2"; TESTS_FAILED=$((TESTS_FAILED + 1)); }

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

ASSETS=(
  threat-detect-linux-amd64
  threat-detect-linux-arm64
  threat-detect-darwin-x64
  threat-detect-darwin-arm64
)

create_release() {
  local dir="$1"
  mkdir -p "$dir"
  : >"$dir/checksums.txt"
  for asset in "${ASSETS[@]}"; do
    printf 'verified payload for %s\n' "$asset" >"$dir/$asset"
    printf '%s  %s\n' "$(sha256sum "$dir/$asset" | awk '{print $1}')" "$asset" >>"$dir/checksums.txt"
  done
}

create_constants() {
  local file="$1"
  cat >"$file" <<'EOF'
package constants

const DefaultThreatDetectVersion Version = "v0.0.1"

var DefaultThreatDetectSHA256 = map[string]string{
	"threat-detect-linux-amd64":  "old",
	"threat-detect-linux-arm64":  "old",
	"threat-detect-darwin-x64":   "old",
	"threat-detect-darwin-arm64": "old",
}

const AfterPins = "preserved"
EOF
}

create_constants_test() {
  local file="$1"
  cat >"$file" <<'EOF'
package constants

func reviewPins() {
	{"Threat Detect", DefaultThreatDetectVersion, "v0.0.1"},
	if DefaultThreatDetectVersion != "v0.0.1" {
		t.Fatalf("DefaultThreatDetectVersion = %q, want v0.0.1; update the version and reviewed digest table together", DefaultThreatDetectVersion)
	}
	expectedDigests := map[string]string{
		"threat-detect-linux-amd64":  "old",
		"threat-detect-linux-arm64":  "old",
		"threat-detect-darwin-x64":   "old",
		"threat-detect-darwin-arm64": "old",
	}
	for asset, want := range expectedDigests {
		if got := DefaultThreatDetectSHA256[asset]; got != want {
			t.Errorf("DefaultThreatDetectSHA256[%q] = %q, want the reviewed v0.0.1 digest %q; update the version and reviewed digest table together", asset, got, want)
		}
	}
}
EOF
}

create_curl_stub() {
  local bin_dir="$1"
  mkdir -p "$bin_dir"
  cat >"$bin_dir/curl" <<'EOF'
#!/usr/bin/env bash
out=""
url=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-o" ]; then
    out="$arg"
  elif [[ "$arg" != -* ]]; then
    url="$arg"
  fi
  prev="$arg"
done
name="${url##*/}"
echo "$url" >>"$CURL_LOG"
cp "$FAKE_RELEASE_DIR/$name" "$out"
EOF
  chmod +x "$bin_dir/curl"
}

run_updater() {
  local fixture="$1"
  local release="$2"
  local output="$3"
  local constants_test="${fixture%.go}_test.go"
  shift 3
  set +e
  env PATH="$TMP_ROOT/bin:$PATH" \
    CURL_LOG="$TMP_ROOT/curl.log" \
    FAKE_RELEASE_DIR="$release" \
    THREAT_DETECT_CONSTANTS_FILE="$fixture" \
    THREAT_DETECT_CONSTANTS_TEST_FILE="$constants_test" \
    THREAT_DETECT_RELEASE_BASE_URL="https://releases.example.test/download" \
    bash "$UPDATER" "$@" >"$output" 2>&1
  RUN_STATUS=$?
  set -e
}

create_curl_stub "$TMP_ROOT/bin"

echo "Test 1: verifies all assets and atomically updates the complete pin set..."
release="$TMP_ROOT/release-success"
constants="$TMP_ROOT/constants-success.go"
output="$TMP_ROOT/success.out"
create_release "$release"
create_constants "$constants"
create_constants_test "${constants%.go}_test.go"
: >"$TMP_ROOT/curl.log"
run_updater "$constants" "$release" "$output" v1.2.3
if [ "$RUN_STATUS" -ne 0 ]; then
  fail "valid release updates pins" "$(cat "$output")"
elif [ "$(wc -l <"$TMP_ROOT/curl.log")" -ne 5 ]; then
  fail "valid release downloads manifest and four assets" "$(cat "$TMP_ROOT/curl.log")"
elif ! grep -q 'DefaultThreatDetectVersion Version = "v1.2.3"' "$constants"; then
  fail "valid release updates version" "$(cat "$constants")"
elif grep -q '"old"' "$constants" || ! grep -q 'const AfterPins = "preserved"' "$constants"; then
  fail "valid release atomically replaces only complete pin unit" "$(cat "$constants")"
elif grep -q 'v0.0.1\\|"old"' "${constants%.go}_test.go"; then
  fail "valid release updates reviewed-literal assertions" "$(cat "${constants%.go}_test.go")"
else
  pass "valid release verifies and updates complete pin unit and assertions"
fi

assert_rejected_unchanged() {
  local description="$1"
  local release_dir="$2"
  local expected="$3"
  local fixture="$TMP_ROOT/${description// /-}.go"
  local before="$TMP_ROOT/${description// /-}.before"
  local test_fixture="${fixture%.go}_test.go"
  local test_before="$TMP_ROOT/${description// /-}-test.before"
  local out="$TMP_ROOT/${description// /-}.out"
  create_constants "$fixture"
  create_constants_test "$test_fixture"
  cp "$fixture" "$before"
  cp "$test_fixture" "$test_before"
  : >"$TMP_ROOT/curl.log"
  run_updater "$fixture" "$release_dir" "$out" v1.2.3
  if [ "$RUN_STATUS" -eq 0 ]; then
    fail "$description" "updater unexpectedly succeeded"
  elif ! grep -qF "$expected" "$out"; then
    fail "$description" "expected '$expected' in: $(cat "$out")"
  elif ! cmp -s "$fixture" "$before"; then
    fail "$description" "constants changed after validation failure"
  elif ! cmp -s "$test_fixture" "$test_before"; then
    fail "$description" "constants test changed after validation failure"
  else
    pass "$description"
  fi
}

echo "Test 2: rejects duplicate manifest entries..."
release="$TMP_ROOT/release-duplicate"
create_release "$release"
cat "$release/checksums.txt" >>"$release/checksums.txt.tmp"
head -n 1 "$release/checksums.txt" >>"$release/checksums.txt.tmp"
mv "$release/checksums.txt.tmp" "$release/checksums.txt"
assert_rejected_unchanged "duplicate manifest entry is rejected" "$release" "duplicate checksums.txt entry"

echo "Test 3: rejects incomplete manifests..."
release="$TMP_ROOT/release-missing"
create_release "$release"
sed -i '/threat-detect-darwin-arm64/d' "$release/checksums.txt"
assert_rejected_unchanged "missing manifest entry is rejected" "$release" "checksums.txt is missing threat-detect-darwin-arm64"

echo "Test 4: rejects unexpected manifest assets..."
release="$TMP_ROOT/release-extra"
create_release "$release"
printf '%064d  unexpected-asset\n' 0 >>"$release/checksums.txt"
assert_rejected_unchanged "unexpected manifest entry is rejected" "$release" "unexpected asset unexpected-asset"

echo "Test 5: rejects malformed manifest digests..."
release="$TMP_ROOT/release-malformed"
create_release "$release"
sed -i '1s/^[0-9a-f]*/ABC/' "$release/checksums.txt"
assert_rejected_unchanged "malformed manifest entry is rejected" "$release" "malformed checksums.txt entry"

echo "Test 6: rejects downloaded bytes that do not match the manifest..."
release="$TMP_ROOT/release-mismatch"
create_release "$release"
printf 'tampered\n' >"$release/threat-detect-linux-arm64"
assert_rejected_unchanged "asset digest mismatch is rejected" "$release" "SHA-256 mismatch"

echo "Test 7: validates both source files before replacing either..."
release="$TMP_ROOT/release-source-shape"
constants="$TMP_ROOT/constants-source-shape.go"
test_constants="${constants%.go}_test.go"
output="$TMP_ROOT/source-shape.out"
create_release "$release"
create_constants "$constants"
printf 'package constants\n' >"$test_constants"
cp "$constants" "$constants.before"
cp "$test_constants" "$test_constants.before"
run_updater "$constants" "$release" "$output" v1.2.3
if [ "$RUN_STATUS" -eq 0 ]; then
  fail "invalid source shape is rejected atomically" "updater unexpectedly succeeded"
elif ! grep -q "could not uniquely locate the threat-detect review assertions" "$output"; then
  fail "invalid source shape is rejected atomically" "unexpected output: $(cat "$output")"
elif ! cmp -s "$constants" "$constants.before" || ! cmp -s "$test_constants" "$test_constants.before"; then
  fail "invalid source shape is rejected atomically" "a source file changed before both replacements were prepared"
else
  pass "invalid source shape is rejected atomically"
fi

echo
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"
if [ "$TESTS_FAILED" -gt 0 ]; then
  exit 1
fi
