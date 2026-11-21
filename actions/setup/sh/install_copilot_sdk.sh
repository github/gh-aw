#!/usr/bin/env bash
set -euo pipefail

node -e 'const days = Number(process.env.NPM_CONFIG_MIN_RELEASE_AGE); const published = Date.parse(process.argv[1]); if (!Number.isInteger(days) || days < 0 || !Number.isFinite(published) || (days > 0 && Date.now() - published < days * 86400000)) { console.error("Locked Copilot SDK dependencies do not satisfy engine.cooldown"); process.exit(1); }' "$1"

target="${RUNNER_TEMP:?RUNNER_TEMP must be set}"
for component in gh-aw engine-cli copilot-sdk; do
  target="$target/$component"
  if [[ -L "$target" ]]; then
    echo "Unexpected symlink in the Copilot SDK install directory: $target" >&2
    exit 1
  fi
  mkdir -p "$target"
done
cd "$target"
if [[ -e npm-shrinkwrap.json || -L npm-shrinkwrap.json ]]; then
  echo "Unexpected npm-shrinkwrap.json in the Copilot SDK install directory" >&2
  exit 1
fi
if [[ -L package.json || -L package-lock.json ]]; then
  echo "Unexpected symlink in the Copilot SDK install directory" >&2
  exit 1
fi
node -e 'const fs = require("node:fs"); const project = JSON.parse(fs.readFileSync(0, "utf8")); fs.writeFileSync("package.json", JSON.stringify(project.manifest)); fs.writeFileSync("package-lock.json", JSON.stringify(project.lock));'
npm ci --prefix "$target" --ignore-scripts --no-audit --no-fund
