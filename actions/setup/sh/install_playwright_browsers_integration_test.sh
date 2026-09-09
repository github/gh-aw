#!/usr/bin/env bash
# Integration tests for install_playwright_browsers.sh.
#
# Unlike the pure unit tests in this directory, these tests exercise the real
# install path: they install @playwright/cli globally, run the script under test
# and then launch the freshly installed browser. Launching is the assertion that
# actually catches the regression this script fixes, because a browser whose OS
# level shared libraries are missing downloads fine but fails to start.
#
# Requires network access, npm and a Linux runner with passwordless sudo (the
# playwright "install-deps" command installs apt packages).
#
# Run: bash actions/setup/sh/install_playwright_browsers_integration_test.sh [browser...]

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT_UNDER_TEST="$SCRIPT_DIR/install_playwright_browsers.sh"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

TESTS_PASSED=0
TESTS_FAILED=0

pass() {
  echo "PASS: $1"
  TESTS_PASSED=$((TESTS_PASSED + 1))
}

fail() {
  echo "FAIL: $1"
  if [ -n "${2:-}" ]; then
    echo "  $2"
  fi
  TESTS_FAILED=$((TESTS_FAILED + 1))
}

browsers=()
for browser in "$@"; do
  case "${browser,,}" in
    chrome | chromium) browsers+=(chromium) ;;
    *) browsers+=("${browser,,}") ;;
  esac
done
if [ "${#browsers[@]}" -eq 0 ]; then
  browsers=(chromium)
fi

# Keep the browser download out of the shared Playwright cache so the test is
# hermetic and observable.
PLAYWRIGHT_BROWSERS_PATH="${PLAYWRIGHT_BROWSERS_PATH:-$(mktemp -d)/playwright-browsers}"
export PLAYWRIGHT_BROWSERS_PATH

echo "Running install_playwright_browsers.sh integration tests..."
echo "  browsers: ${browsers[*]}"
echo "  PLAYWRIGHT_BROWSERS_PATH: ${PLAYWRIGHT_BROWSERS_PATH}"
echo

# Test 1: unsupported browsers are rejected before anything is downloaded.
echo "Test 1: rejects unsupported browser names..."
output=$(bash "$SCRIPT_UNDER_TEST" not-a-browser 2>&1)
status=$?
if [ "$status" -ne 0 ] && echo "$output" | grep -q "Unsupported Playwright browser: not-a-browser"; then
  pass "unsupported browser exits non-zero with an error"
else
  fail "unsupported browser was not rejected (exit ${status})" "$output"
fi

# Test 2: the dependency install fails closed when playwright-cli cannot be
# found, instead of downloading browsers that will not launch.
echo "Test 2: fails closed when playwright-cli is unavailable..."
empty_bin="$(mktemp -d)"
fail_closed_browsers_path="$(mktemp -d)/playwright-browsers"
output=$(PATH="$empty_bin" PLAYWRIGHT_BROWSERS_PATH="$fail_closed_browsers_path" \
  /bin/bash "$SCRIPT_UNDER_TEST" "${browsers[@]}" 2>&1)
status=$?
if [ "$status" -ne 0 ]; then
  pass "missing playwright-cli exits non-zero"
else
  fail "missing playwright-cli did not fail the setup step" "$output"
fi
if echo "$output" | grep -q "::error::playwright-cli is not on PATH"; then
  pass "missing playwright-cli reports an actionable error"
else
  fail "missing playwright-cli did not report the expected error" "$output"
fi
if [ ! -e "$fail_closed_browsers_path" ]; then
  pass "no browsers were downloaded after the dependency install failed"
else
  fail "browsers were downloaded despite the dependency install failing" "$fail_closed_browsers_path"
fi
rm -rf "$empty_bin"

# Install @playwright/cli globally, matching the version the compiler pins.
if ! command -v playwright-cli >/dev/null 2>&1; then
  version="${PLAYWRIGHT_CLI_VERSION:-}"
  constants_file="$REPO_ROOT/pkg/constants/version_constants.go"
  if [ -z "$version" ] && [ -f "$constants_file" ]; then
    version="$(sed -n 's/^const DefaultPlaywrightCLIVersion Version = "\(.*\)"$/\1/p' "$constants_file")"
  fi
  version="${version:-latest}"
  echo "Installing @playwright/cli@${version} globally..."
  if ! npm install -g "@playwright/cli@${version}"; then
    echo "::error::Failed to install @playwright/cli@${version}; cannot run integration tests"
    exit 1
  fi
fi

# Test 3: the script installs system dependencies and browser binaries.
echo "Test 3: installs system dependencies and browser binaries..."
install_log="$(mktemp)"
trap 'rm -f "$install_log"' EXIT
bash "$SCRIPT_UNDER_TEST" "${browsers[@]}" >"$install_log" 2>&1
status=$?
if [ "$status" -eq 0 ]; then
  pass "script exited successfully"
else
  fail "script exited with status ${status}" "$(tail -n 30 "$install_log")"
fi

if grep -q "Installing Playwright system dependencies for: ${browsers[*]}" "$install_log"; then
  pass "system dependency install ran for ${browsers[*]}"
else
  fail "system dependency install did not run" "$(tail -n 30 "$install_log")"
fi

for browser in "${browsers[@]}"; do
  if grep -q "Installing Playwright ${browser} browser" "$install_log"; then
    pass "browser install ran for ${browser}"
  else
    fail "browser install did not run for ${browser}" "$(tail -n 30 "$install_log")"
  fi
done

if [ -d "$PLAYWRIGHT_BROWSERS_PATH" ] && [ -n "$(ls -A "$PLAYWRIGHT_BROWSERS_PATH" 2>/dev/null)" ]; then
  pass "browser binaries were downloaded to PLAYWRIGHT_BROWSERS_PATH"
else
  fail "PLAYWRIGHT_BROWSERS_PATH is empty" "$PLAYWRIGHT_BROWSERS_PATH"
fi

# Test 4: every installed browser actually launches. This is the regression
# test for missing shared libraries: the download above succeeds even when the
# OS level dependencies are absent, but launching does not.
echo "Test 4: installed browsers launch..."
playwright_cli_real="$(readlink -f "$(command -v playwright-cli)")"
playwright_dir="$(node -e '
const path = require("path");
process.stdout.write(path.dirname(require.resolve("playwright/package.json", { paths: [process.argv[1]] })));
' "$(dirname "$playwright_cli_real")" 2>/dev/null || true)"

if [ -z "$playwright_dir" ]; then
  fail "could not resolve the playwright package to launch browsers" "$playwright_cli_real"
else
  for browser in "${browsers[@]}"; do
    # shellcheck disable=SC2016 # the node script uses its own JS template literal
    if launch_output=$(PLAYWRIGHT_DIR="$playwright_dir" PLAYWRIGHT_BROWSER="$browser" node -e '
      const playwright = require(process.env.PLAYWRIGHT_DIR);
      const name = process.env.PLAYWRIGHT_BROWSER;
      (async () => {
        const browser = await playwright[name].launch();
        const page = await browser.newPage();
        await page.setContent("<h1>gh-aw</h1>");
        const text = await page.textContent("h1");
        await browser.close();
        if (text !== "gh-aw") {
          throw new Error(`unexpected page content: ${text}`);
        }
        console.log("launched");
      })().catch((error) => {
        console.error(error.message);
        process.exit(1);
      });
    ' 2>&1); then
      pass "${browser} launched and rendered a page"
    else
      fail "${browser} failed to launch" "$launch_output"
    fi
  done
fi

echo
echo "Tests passed: ${TESTS_PASSED}"
echo "Tests failed: ${TESTS_FAILED}"

if [ "$TESTS_FAILED" -gt 0 ]; then
  exit 1
fi

echo "All install_playwright_browsers.sh integration tests passed"
