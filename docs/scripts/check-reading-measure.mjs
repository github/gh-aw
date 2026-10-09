#!/usr/bin/env node
// Fails the docs build if the reading measure or content gutters change.
// The values and the reasons for them live in .github/skills/docs-design/SKILL.md;
// change them there first, with the designer's agreement, then update this file.

import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const docsDir = fileURLToPath(new URL("..", import.meta.url));
const tokensFile = "src/styles/custom.css";

const expected = {
  "--aw-prose-width": "38.75rem", // 620px, ~80 characters per line
  "--aw-content-gutter": "3.75rem", // 60px
  "--sl-content-width": "var(--aw-prose-width)",
};

function walk(dir, files = []) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) walk(path, files);
    else if (/\.(css|astro|mdx?|[cm]?[jt]sx?)$/.test(name)) files.push(path);
  }
  return files;
}

const errors = [];
const files = [...walk(join(docsDir, "src")), join(docsDir, "astro.config.mjs")];
const declaration = new RegExp(`(${Object.keys(expected).join("|")})\\s*:\\s*([^;}"'\`]+)`, "g");

for (const file of files) {
  const rel = relative(docsDir, file);
  for (const [, name, rawValue] of readFileSync(file, "utf8").matchAll(declaration)) {
    const value = rawValue.replace(/\/\*.*?\*\//g, "").trim();
    if (rel !== tokensFile) {
      errors.push(`${rel}: sets ${name}: ${value}. The reading measure is defined only in ${tokensFile}.`);
    } else if (value !== expected[name]) {
      errors.push(`${rel}: ${name} is ${value}, expected ${expected[name]}.`);
    }
  }
}

const tokens = readFileSync(join(docsDir, tokensFile), "utf8");
for (const name of Object.keys(expected)) {
  if (!new RegExp(`${name}\\s*:`).test(tokens)) errors.push(`${tokensFile}: ${name} is missing.`);
}

// The tokens only matter if chrome.css still applies them
const chrome = readFileSync(join(docsDir, "src/styles/chrome.css"), "utf8");
if (!/max-width:\s*var\(--aw-prose-width\)/.test(chrome)) {
  errors.push("src/styles/chrome.css: docs content is no longer capped at max-width: var(--aw-prose-width).");
}
if (!/padding-inline:\s*var\(--aw-content-gutter\)/.test(chrome)) {
  errors.push("src/styles/chrome.css: docs content no longer has padding-inline: var(--aw-content-gutter) on both sides.");
}

if (errors.length) {
  console.error(`Reading measure check failed:\n\n  ${errors.join("\n  ")}

Docs prose is capped at 620px (~80 characters per line) because longer lines
are measurably harder to read: the eye loses its place returning to the start
of each line. Tables, diagrams and images may already run wider. See
.github/skills/docs-design/SKILL.md for the reasoning and how to change a rule.
`);
  process.exit(1);
}

console.log("Reading measure check passed (620px prose, 60px gutters).");
