"use strict";

const assert = require("node:assert/strict");
const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { test } = require("node:test");
const { readDocumentMetadata } = require("./markdown_metadata_test_adapter.cjs");

const root = path.join(__dirname, "..");

test("Update Astro fails the preliminary check when npm-check-updates fails", () => {
  const source = fs.readFileSync(path.join(root, ".github/workflows/update-astro.md"), "utf8");
  const [, frontmatter] = source.match(/^---\r?\n([\s\S]*?)\r?\n---/);
  const workflow = readDocumentMetadata(frontmatter).value;
  const script = workflow.jobs.check_updates.steps.find(step => step.id === "check").run;
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "update-astro-test-"));
  try {
    const npx = path.join(dir, "npx");
    const output = path.join(dir, "output");
    fs.writeFileSync(npx, '#!/bin/sh\nprintf \'%s\\n\' "$NCU_OUTPUT"\nexit "$NCU_EXIT"\n');
    fs.chmodSync(npx, 0o755);
    for (const [status, result, expected, expectedOutput] of [
      ["0", "{}", 0, "has_updates=false\n"],
      ["0", '{"astro":"7.4.13"}', 0, "has_updates=true\nupdates_summary=astro: 7.4.13\n"],
      ["1", "", 1, ""],
    ]) {
      fs.writeFileSync(output, "");
      const run = spawnSync("bash", ["-e", "-o", "pipefail", "-c", script], {
        cwd: dir,
        env: { ...process.env, PATH: `${dir}:${process.env.PATH}`, GITHUB_OUTPUT: output, NCU_EXIT: status, NCU_OUTPUT: result },
      });
      assert.equal(run.status, expected);
      assert.equal(fs.readFileSync(output, "utf8"), expectedOutput);
    }
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test("Update Astro distinguishes no updates from blocked or unverified work", () => {
  const source = fs.readFileSync(path.join(root, ".github/workflows/update-astro.md"), "utf8");
  for (const text of [
    "`noop` only when no dependency changes are needed",
    "`report_incomplete` with the concrete blocker",
    "it is diagnostic only and does not replace `report_incomplete`",
    "Do not create a PR or use `noop` in this case",
    "Do not create a pull request if the build is failing",
  ]) {
    assert.ok(source.includes(text), text);
  }
});

test("Update Astro preserves TLS verification when npm downloads fail", () => {
  const source = fs.readFileSync(path.join(root, ".github/workflows/update-astro.md"), "utf8");
  for (const text of ["NODE_USE_SYSTEM_CA=1", "Keep TLS verification enabled", "never disable it or switch to an unapproved registry"]) {
    assert.ok(source.includes(text), text);
  }
});
