#!/usr/bin/env node
// Fails the docs build if a redirect in astro.config.mjs points at a docs page
// that does not exist, so moved or renamed pages cannot silently 404 old links.

import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const docsDir = fileURLToPath(new URL("..", import.meta.url));
const contentDir = join(docsDir, "src/content/docs");
const base = "/gh-aw/";

const config = readFileSync(join(docsDir, "astro.config.mjs"), "utf8");
const block = config.match(/^ {2}redirects: \{\n([\s\S]*?)^ {2}\},$/m);
if (!block) {
  console.error("Could not find the redirects map in astro.config.mjs.");
  process.exit(1);
}

const redirects = [];
const errors = [];
for (const line of block[1].split("\n")) {
  const trimmed = line.trim();
  if (!trimmed || trimmed.startsWith("//")) continue;
  const entry = trimmed.match(/^"([^"]+)":\s*"([^"]+)",?$/);
  if (entry) redirects.push([entry[1], entry[2]]);
  else errors.push(`Unrecognised redirect entry: ${trimmed}`);
}

for (const [from, to] of redirects) {
  if (/^https?:\/\//.test(to)) continue;
  const path = to.split("#")[0];
  if (!path.startsWith(base)) {
    errors.push(`${from} -> ${to}: internal targets must start with ${base}`);
    continue;
  }
  const slug = path.slice(base.length).replace(/\/$/, "");
  const stem = join(contentDir, slug || "index");
  const candidates = [`${stem}.md`, `${stem}.mdx`, join(stem, "index.md"), join(stem, "index.mdx")];
  if (!candidates.some(existsSync)) errors.push(`${from} -> ${to}: no page at src/content/docs/${slug || "index"}.md(x)`);
}

if (errors.length) {
  console.error(`Redirect check failed:\n  ${errors.join("\n  ")}`);
  process.exit(1);
}
console.log(`Redirect check passed (${redirects.length} redirects).`);
