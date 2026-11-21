#!/usr/bin/env bash
set -euo pipefail

variant="${1:-}"
expected_digest="${2:-}"
newest="${3:-}"
case "$variant" in
  node|typescript) ;;
  *) echo "::error::Unknown Copilot SDK npm project" >&2; exit 1 ;;
esac

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
project="${script_dir}/copilot-sdk/${variant}"
target="${RUNNER_TEMP:?RUNNER_TEMP must be set}"

# npm ci does not apply the release-age filter to a complete lockfile.
node - "$project" "$expected_digest" "$newest" <<'NODE'
const { createHash } = require("node:crypto");
const { readFileSync } = require("node:fs");
const { join } = require("node:path");
const [project, expectedDigest, newest] = process.argv.slice(2);
const days = Number(process.env.NPM_CONFIG_MIN_RELEASE_AGE);
const published = Date.parse(newest);
if (!Number.isInteger(days) || days < 0 || !Number.isFinite(published) ||
    (days > 0 && Date.now() - published < days * 86400000)) {
  console.error("::error::Locked Copilot SDK dependencies do not satisfy engine.cooldown");
  process.exit(1);
}
const actual = createHash("sha256")
  .update(readFileSync(join(project, "package.json")))
  .update("\0")
  .update(readFileSync(join(project, "package-lock.json")))
  .digest("hex");
if (actual !== expectedDigest) {
  console.error("::error::Copilot SDK npm project does not match the compiled workflow");
  process.exit(1);
}
NODE

for component in gh-aw engine-cli copilot-sdk; do
  target="$target/$component"
  if [[ -L "$target" ]]; then
    echo "::error::Unexpected symlink in the Copilot SDK install directory" >&2
    exit 1
  fi
  mkdir -p "$target"
done
cd "$target"
if [[ -e npm-shrinkwrap.json || -L npm-shrinkwrap.json || -L package.json || -L package-lock.json ]]; then
  echo "::error::Unexpected package-file override in the Copilot SDK install directory" >&2
  exit 1
fi
cp "$project/package.json" "$project/package-lock.json" .
npm ci --prefix "$target" --ignore-scripts --no-audit --no-fund
