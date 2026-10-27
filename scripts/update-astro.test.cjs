"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { test } = require("node:test");

const root = path.join(__dirname, "..");

test("Update Astro uses the supported npm-check-updates upgrade flag", () => {
  const workflow = fs.readFileSync(path.join(root, ".github/workflows/update-astro.md"), "utf8");
  assert.match(workflow, /npx --yes npm-check-updates --upgrade\b/);
  assert.doesNotMatch(workflow, /npm-check-updates --update\b/);
});

test("docs lockfile uses canonical npm tarball URLs rather than environment-specific mirrors", () => {
  const lock = JSON.parse(fs.readFileSync(path.join(root, "docs/package-lock.json"), "utf8"));
  let checked = 0;
  for (const [location, entry] of Object.entries(lock.packages)) {
    if (!entry.resolved || entry.link) continue;
    const name = entry.name || location.split("node_modules/").at(-1);
    const basename = name.split("/").at(-1);
    assert.equal(entry.resolved, `https://registry.npmjs.org/${name}/-/${basename}-${entry.version}.tgz`, location);
    assert.ok(entry.integrity, `${location} must retain its integrity hash`);
    checked++;
  }
  assert.ok(checked > 0, "Expected resolved npm packages in docs/package-lock.json");
});
