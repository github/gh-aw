#!/usr/bin/env bash
set -euo pipefail

variant="${1:?missing SDK variant}"
newest="${2:?missing publication timestamp}"
manifest_hash="${3:?missing manifest hash}"
lock_hash="${4:?missing lock hash}"
[[ "$variant" == node || "$variant" == typescript ]] || { echo "Invalid SDK variant" >&2; exit 1; }

node -e 'const days = Number(process.env.NPM_CONFIG_MIN_RELEASE_AGE); const published = Date.parse(process.argv[1]); if (!Number.isInteger(days) || days < 0 || !Number.isFinite(published) || (days > 0 && Date.now() - published < days * 86400000)) { console.error("Locked Copilot SDK dependencies do not satisfy engine.cooldown"); process.exit(1); }' "$newest"

source_dir="${RUNNER_TEMP}/gh-aw/actions"
target="${RUNNER_TEMP}/gh-aw/engine-cli/copilot-sdk"
for dir in "${RUNNER_TEMP}/gh-aw" "${RUNNER_TEMP}/gh-aw/engine-cli" "$target"; do
  if [[ -L "$dir" ]]; then echo "Unexpected symlink in Copilot SDK install path: $dir" >&2; exit 1; fi
  mkdir -p "$dir"
done
cd "$target"
if [[ -e npm-shrinkwrap.json || -L npm-shrinkwrap.json ]]; then echo "Unexpected npm-shrinkwrap.json in the Copilot SDK install directory" >&2; exit 1; fi
if [[ -L package.json || -L package-lock.json ]]; then echo "Unexpected symlink in the Copilot SDK install directory" >&2; exit 1; fi
node -e 'const fs = require("node:fs"); const crypto = require("node:crypto"); const files = [[process.argv[1], "package.json", process.argv[3]], [process.argv[2], "package-lock.json", process.argv[4]]].map(([source, target, expected]) => { const data = fs.readFileSync(source); if (crypto.createHash("sha256").update(data).digest("hex") !== expected) throw new Error("Copilot SDK asset hash mismatch: " + source); return [target, data]; }); for (const [target, data] of files) fs.writeFileSync(target, data);' \
  "$source_dir/copilot-sdk-$variant-package.json" "$source_dir/copilot-sdk-$variant-package-lock.json" "$manifest_hash" "$lock_hash"
npm ci --prefix "$target" --ignore-scripts --no-audit --no-fund
