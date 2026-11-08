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

test("MCP access-control spec validation uses full history and matching documented pins", () => {
  const workflow = fs.readFileSync(path.join(__dirname, "..", ".github", "workflows", "ci.yml"), "utf8");
  const job = workflow.split("  validate-mcp-access-control-spec-pin:\n")[1]?.split("\n  test-race:")[0];
  assert.ok(job, "Missing MCP access-control spec-pin validation job");
  assert.match(job, /with:\n\s+fetch-depth: 0\n\s+persist-credentials: false/);
  const pin = job.match(/pinned="([a-f0-9]{40})"/)?.[1];
  assert.ok(pin, "Missing specification commit pin");
  const readme = fs.readFileSync(path.join(__dirname, "..", "specs", "github-mcp-access-control-compliance", "README.md"), "utf8");
  assert.ok(readme.includes(`**Spec version pinned at commit \`${pin}\`.**`));
  assert.ok(readme.includes(`PINNED="${pin}"`));
});

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
