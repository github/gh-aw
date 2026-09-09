#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -eq 0 ]; then
  set -- chromium
fi

normalized_browsers=()
for browser in "$@"; do
  case "${browser,,}" in
    chrome|chromium) browser=chromium ;;
    firefox|webkit) ;;
    *)
      echo "::error::Unsupported Playwright browser: ${browser}"
      exit 1
      ;;
  esac
  normalized_browsers+=("$browser")
done

# Install the OS-level shared library dependencies (e.g. libnspr4, libnss3,
# libatk-bridge2.0-0) required for the browser binaries to actually launch.
# Without this step, browser processes can be downloaded successfully but
# fail to start at runtime with missing shared library errors, so a failure to
# resolve or run the dependency installer must fail this step rather than leave
# unusable browsers behind.
playwright_cli_bin="$(command -v playwright-cli || true)"
if [ -z "$playwright_cli_bin" ]; then
  echo "::error::playwright-cli is not on PATH; cannot install Playwright system dependencies"
  exit 1
fi

# The @playwright/cli package bundles its own "playwright" dependency, whose CLI
# is not symlinked into the global npm bin dir. Let Node resolve the package
# from the installed playwright-cli script and read the CLI entry point from the
# package's own "bin" field, so no npm layout is hard-coded here.
playwright_cli_real="$(readlink -f "$playwright_cli_bin")"
playwright_js="$(node -e '
const path = require("path");
const pkgPath = require.resolve("playwright/package.json", { paths: [process.argv[1]] });
const pkg = require(pkgPath);
const bin = typeof pkg.bin === "string" ? pkg.bin : pkg.bin && pkg.bin.playwright;
if (!bin) {
  process.exit(1);
}
process.stdout.write(path.resolve(path.dirname(pkgPath), bin));
' "$(dirname "$playwright_cli_real")" 2>/dev/null || true)"

if [ ! -f "$playwright_js" ]; then
  echo "::error::Could not resolve the playwright CLI bundled with ${playwright_cli_real}; Playwright system dependencies cannot be installed"
  exit 1
fi

echo "Installing Playwright system dependencies for: ${normalized_browsers[*]}"
if ! node "$playwright_js" install-deps "${normalized_browsers[@]}"; then
  echo "::error::Failed to install Playwright system dependencies for: ${normalized_browsers[*]}"
  exit 1
fi

max_attempts=3
for browser in "${normalized_browsers[@]}"; do
  for ((attempt = 1; attempt <= max_attempts; attempt++)); do
    echo "Installing Playwright ${browser} browser (attempt ${attempt}/${max_attempts})"
    if playwright-cli install-browser "$browser"; then
      break
    fi
    if [ "$attempt" -eq "$max_attempts" ]; then
      echo "::error::Failed to install Playwright ${browser} browser after ${max_attempts} attempts"
      exit 1
    fi
    sleep $((attempt * 5))
  done
done
