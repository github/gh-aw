"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { test } = require("node:test");
const { buildCopilotSDKPermissionHandler } = require("../actions/setup/js/copilot_sdk_permissions.cjs");

function workflowPermissions(name) {
  const lock = fs.readFileSync(path.join(__dirname, "..", ".github", "workflows", `${name}.lock.yml`), "utf8");
  const config = lock.match(/GH_AW_COPILOT_SDK_TOOL_CONFIG: '([^\n]+)'/);
  assert.ok(config, `Missing compiler-owned SDK permissions for ${name}`);
  return JSON.parse(config[1]).permissions;
}

function permissionHandler(name) {
  return buildCopilotSDKPermissionHandler(workflowPermissions(name), () => {
    throw new Error("Restricted workflows must not use blanket approval");
  });
}

function checkCommand(handler, command, expected) {
  assert.equal(handler({ kind: "shell", fullCommandText: command }).kind, expected, command);
}

test("Fixer metadata alternatives succeed while the observed denied commands remain restricted", () => {
  const handler = permissionHandler("code-scanning-fixer");
  for (const command of ["safeoutputs --help", "github --help", "git status --short", "git diff --binary --no-ext-diff | wc -c"]) {
    checkCommand(handler, command, "approve-once");
  }
  for (const command of [
    "safeoutputs --help | head -40; github --help | head -60; git status --short; git log --oneline -1",
    'grep -rn "test.mjs" Makefile package.json | head; node --version',
    "node --test scripts/pr-sous-chef.test.mjs",
  ]) {
    checkCommand(handler, command, "reject");
  }
});

test("Fixer prompt does not advertise an ungranted branch-inspection command", () => {
  const prompt = fs.readFileSync(path.join(__dirname, "..", ".github", "workflows", "code-scanning-fixer.md"), "utf8");
  assert.doesNotMatch(prompt, /git branch --show-current/);
});

test("Tidy separates permitted formatting from exact permitted status inspection", () => {
  const handler = permissionHandler("tidy");
  for (const command of ["make fmt", "make lint", "make test", "git status"]) {
    checkCommand(handler, command, "approve-once");
  }
  for (const command of ["make fmt | tail -5; git status --porcelain | head", "git status --porcelain"]) {
    checkCommand(handler, command, "reject");
  }
});

test("Daily Graft uses full history and exposes the built wiring graph to its MCP server", () => {
  const workflows = path.join(__dirname, "..", ".github", "workflows");
  const source = fs.readFileSync(path.join(workflows, "daily-graft-intelligence.md"), "utf8");
  const graft = fs.readFileSync(path.join(workflows, "shared", "mcp", "graft.md"), "utf8");
  const lock = fs.readFileSync(path.join(workflows, "daily-graft-intelligence.lock.yml"), "utf8");

  assert.match(source, /checkout:\s*\n\s+fetch-depth: 0/);
  assert.doesNotMatch(source, /graft-(?:recent-activity|changed-files)\.txt \|\| true/);
  assert.match(graft, /test -s "\$GRAFT_DIR\/\.graph\/wiring\.json"/);
  assert.match(graft, /"--dir=\/tmp\/gh-aw\/graft"/);
  assert.match(graft, /\\\$\{GITHUB_WORKSPACE\}:\\\$\{GITHUB_WORKSPACE\}:ro/);
  assert.match(graft, /\/tmp\/gh-aw\/graft-empty-git:\\\$\{GITHUB_WORKSPACE\}\/\.git:ro/);
  assert.match(graft, /\/tmp\/gh-aw\/graft:\/tmp\/gh-aw\/graft:ro/);
  assert.match(lock, /fetch-depth: 0/);
  assert.match(lock, /--dir=\/tmp\/gh-aw\/graft/);
  assert.match(lock, /\/tmp\/gh-aw\/graft:\/tmp\/gh-aw\/graft:ro/);
  assert.match(lock, /graft-empty-git:.*GITHUB_WORKSPACE.*\/\.git:ro/);
  assert.match(source, /optional deep layer may report `NO GRAPH`/);
  assert.match(source, /safeoutputs report_incomplete/);
});
