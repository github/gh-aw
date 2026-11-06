#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
sandbox=$(mktemp -d)
trap 'rm -rf "$sandbox"' EXIT
mkdir -p "$sandbox/bin" "$sandbox/.github/scripts" "$sandbox/actions/setup/sh" "$sandbox/fixtures/jre/bin"
installer="$sandbox/.github/scripts/install_tla_runtime.sh"
cp "$SCRIPT_DIR/../../actions/setup/sh/release_integrity.sh" "$sandbox/actions/setup/sh/"
printf 'fixture jar\n' > "$sandbox/fixtures/tla2tools.jar"
cat > "$sandbox/fixtures/jre/bin/java" <<'JAVA'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$EXECUTION_LOG"
if [ "${MODE:-}" = java-failure ]; then exit 1; fi
JAVA
chmod +x "$sandbox/fixtures/jre/bin/java"
tar -czf "$sandbox/fixtures/jre.tar.gz" -C "$sandbox/fixtures" jre
printf 'not an archive\n' > "$sandbox/fixtures/bad.tar.gz"

digest() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}
jar_sha=$(digest "$sandbox/fixtures/tla2tools.jar")
jre_sha=$(digest "$sandbox/fixtures/jre.tar.gz")
bad_archive_sha=$(digest "$sandbox/fixtures/bad.tar.gz")
fixture_pins() {
  sed \
    -e "s/936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88/$jar_sha/" \
    -e "s/2413149700df0f7d440500a84a8f764c535f21e5a5e87d38328b64eec2c5b500/${1:-$jre_sha}/" \
    "$SCRIPT_DIR/install_tla_runtime.sh" > "$installer"
}
fixture_pins
cat > "$sandbox/bin/uname" <<'UNAME'
#!/usr/bin/env bash
case "$1" in
  -s) echo "${FAKE_OS:-Linux}" ;;
  -m) echo "${FAKE_ARCH:-x86_64}" ;;
esac
UNAME
cat > "$sandbox/bin/curl" <<'CURL'
#!/usr/bin/env bash
set -euo pipefail
file=""
https=false
redirects=false
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) file="$2"; shift 2 ;;
    --proto) [ "$2" = '=https' ]; https=true; shift 2 ;;
    --proto-redir) [ "$2" = '=https' ]; redirects=true; shift 2 ;;
    https://*) url="$1"; shift ;;
    *) shift ;;
  esac
done
[ "$https" = true ] && [ "$redirects" = true ]
case "$url" in
  https://github.com/tlaplus/tlaplus/releases/download/v1.7.4/tla2tools.jar) kind=jar; payload="$FIXTURES/tla2tools.jar" ;;
  https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.12.1%2B1/OpenJDK21U-jre_x64_linux_hotspot_21.0.12.1_1.tar.gz) kind=jre; payload="$FIXTURES/jre.tar.gz" ;;
  *) echo "Unexpected download: $url" >&2; exit 90 ;;
esac
echo "$kind" >> "$DOWNLOAD_LOG"
cp "$payload" "$file"
case "$MODE" in
  "$kind-download") echo "fixture download failure" >&2; exit 22 ;;
  "$kind-mismatch") printf tampered >> "$file" ;;
  extraction) if [ "$kind" = jre ]; then cp "$FIXTURES/bad.tar.gz" "$file"; fi ;;
esac
CURL
chmod +x "$sandbox/bin/"*
export PATH="$sandbox/bin:$PATH" FIXTURES="$sandbox/fixtures"
export EXECUTION_LOG="$sandbox/executions" DOWNLOAD_LOG="$sandbox/downloads"
export MODE=success

run_case() {
  local name="$1" expected="$2"
  shift 2
  local destination="$sandbox/$name/runtime" status=0
  mkdir -p "$(dirname "$destination")"
  : > "$EXECUTION_LOG"
  : > "$DOWNLOAD_LOG"
  bash "$installer" "$destination" "$@" > "$sandbox/output" 2>&1 || status=$?
  if [ "$expected" = success ]; then
    [ "$status" -eq 0 ] || { cat "$sandbox/output"; exit 1; }
    cmp "$FIXTURES/tla2tools.jar" "$destination/tla2tools.jar"
    if [ "${1:-}" = --jar-only ]; then
      [ ! -e "$destination/jre" ]
      [ ! -s "$EXECUTION_LOG" ]
      [ "$(cat "$DOWNLOAD_LOG")" = jar ]
    else
      [ -x "$destination/jre/bin/java" ]
      grep -q -- '-version' "$EXECUTION_LOG"
      grep -q -- 'tlc2.TLC -help' "$EXECUTION_LOG"
    fi
  else
    [ "$status" -ne 0 ]
    [ ! -e "$destination" ]
    grep -q 'ERROR:' "$sandbox/output"
    if [ "$MODE" != java-failure ]; then [ ! -s "$EXECUTION_LOG" ]; fi
  fi
  [ -z "$(find "$(dirname "$destination")" -name 'runtime.install.*' -print)" ]
  echo "PASS: $name"
}

run_case full success
run_case jar-only success --jar-only
for MODE in jar-mismatch jar-download jre-mismatch jre-download; do
  export MODE
  run_case "$MODE" failure
done
MODE=extraction
fixture_pins "$bad_archive_sha"
run_case extraction failure
fixture_pins
MODE=java-failure
run_case java-failure failure
MODE=success
FAKE_ARCH=aarch64 run_case unsupported failure
run_case invalid-flag failure --unknown
mkdir "$sandbox/existing"
printf preserved > "$sandbox/existing/sentinel"
if bash "$installer" "$sandbox/existing" > "$sandbox/output" 2>&1; then exit 1; fi
[ "$(cat "$sandbox/existing/sentinel")" = preserved ]
if bash "$installer" / > "$sandbox/output" 2>&1; then exit 1; fi
if bash "$installer" relative > "$sandbox/output" 2>&1; then exit 1; fi
echo "PASS: existing and invalid destinations"

[ ! -e "$SCRIPT_DIR/../../actions/setup/sh/install_tla_runtime.sh" ]
[ ! -e "$SCRIPT_DIR/../../actions/setup/sh/install_tla_runtime_test.sh" ]
echo "PASS: TLA+ installer and tests are repository-owned, not setup action payloads"
