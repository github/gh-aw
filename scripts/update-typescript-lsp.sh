#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" != 2 || ! "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ || ! "$2" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Usage: $0 TYPESCRIPT_VERSION LANGUAGE_SERVER_VERSION (exact X.Y.Z releases)" >&2
  exit 1
fi

project="$(cd "$(dirname "${BASH_SOURCE[0]}")/../actions/setup/typescript-lsp" && pwd)"
work="$(mktemp -d "${TMPDIR:-/tmp}/gh-aw-typescript-lsp.XXXXXX")"
trap 'rm -rf -- "$work"' EXIT
cp "$project"/{.npmrc,package.json,package-lock.json} "$work/"

(
  cd "$work"
  registry="$(npm config get registry)"
  npm install --package-lock-only --save-exact --ignore-scripts --no-audit --no-fund \
    "typescript@$1" "typescript-language-server@$2"

  # npm can write mirror URLs for newly resolved packages; retain only canonical
  # npmjs URLs so the checked-in lock works with the runner's selected registry.
  node - "$registry" "$1" "$2" "$project/package-lock.json" <<'NODE'
const { execFileSync } = require("node:child_process");
const fs = require("node:fs");
const [registry, ts, server, previousLock] = process.argv.slice(2);
const canonical = "https://registry.npmjs.org/";
const mirror = new URL(registry);
if (mirror.protocol !== "https:" || mirror.username || mirror.password || mirror.search || mirror.hash) {
  throw new Error("An HTTPS npm registry without embedded credentials is required");
}
const prefix = mirror.href.replace(/\/?$/, "/");
const manifest = JSON.parse(fs.readFileSync("package.json", "utf8"));
const lock = JSON.parse(fs.readFileSync("package-lock.json", "utf8"));
const previous = JSON.parse(fs.readFileSync(previousLock, "utf8")).packages;
const sha512 = /^sha512-[A-Za-z0-9+/]{86}==$/;
if (lock.lockfileVersion !== 3 || !lock.packages?.[""]) {
  throw new Error("Expected an npm v3 lockfile");
}
for (const [name, version] of Object.entries({ typescript: ts, "typescript-language-server": server })) {
  if (manifest.dependencies?.[name] !== version ||
      lock.packages[""].dependencies?.[name] !== version ||
      lock.packages[`node_modules/${name}`]?.version !== version) {
    throw new Error(`Expected an exact ${name}@${version} pin in the manifest and lockfile`);
  }
}
for (const [name, pkg] of Object.entries(lock.packages)) {
  if (!name) continue;
  const resolved = pkg.resolved ?? "";
  const source = resolved.startsWith(canonical) ? canonical : prefix;
  const path = resolved.slice(source.length);
  if (pkg.link || !resolved.startsWith(source) ||
      !/^(?:@[\w.-]+\/)?[\w.-]+\/-\/[\w.+-]+\.tgz$/.test(path) ||
      new URL(canonical + path).href !== canonical + path) {
    throw new Error(`Expected a registry tarball for ${name}`);
  }
  const old = previous[name];
  if (old?.resolved === canonical + path && sha512.test(old.integrity)) {
    if (sha512.test(pkg.integrity) && pkg.integrity !== old.integrity) {
      throw new Error(`Registry checksum changed for the existing pin ${name}`);
    }
    pkg.integrity = old.integrity;
  } else if (/^sha1-[A-Za-z0-9+/]{27}=$/.test(pkg.integrity)) {
    // Some mirrors publish only SHA-1 metadata. Verify it against the archive
    // npm fetched before recording the stronger digest computed by npm pack.
    const [{ shasum, integrity }] = JSON.parse(execFileSync("npm",
      ["pack", "--dry-run", "--json", "--ignore-scripts", prefix + path],
      { encoding: "utf8" }));
    if (`sha1-${Buffer.from(shasum, "hex").toString("base64")}` !== pkg.integrity) {
      throw new Error(`Registry checksum does not match the archive for ${name}`);
    }
    pkg.integrity = integrity;
  }
  if (!sha512.test(pkg.integrity)) {
    throw new Error(`Expected SHA-512 integrity for ${name}`);
  }
  pkg.resolved = canonical + path;
}
fs.writeFileSync("package-lock.json", JSON.stringify(lock, null, 2) + "\n");
NODE

  npm ci --ignore-scripts --replace-registry-host=npmjs --no-audit --no-fund
)

cp "$work"/{package.json,package-lock.json} "$project/"
printf 'Pinned TypeScript %s and TypeScript Language Server %s. Review both npm files before committing.\n' "$1" "$2"
