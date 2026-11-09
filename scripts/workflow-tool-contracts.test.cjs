"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { test } = require("node:test");
const { buildCopilotSDKPermissionHandler } = require("../actions/setup/js/copilot_sdk_permissions.cjs");

const cjsWorkflow = fs.readFileSync(path.join(__dirname, "..", ".github", "workflows", "cjs.yml"), "utf8");
const nvxScript = cjsWorkflow.match(/- name: Select NVX musl integration[\s\S]*?script: \|\n([\s\S]*?)(?=      - name: Cache repository checkout)/)?.[1];
assert.ok(nvxScript, "NVX path selector must be present");
const selectNVX = new (Object.getPrototypeOf(async function () {}).constructor)("context", "github", "core", nvxScript);

async function nvxSelection(files, eventName = "pull_request", apiError) {
  let output;
  let calls = 0;
  await selectNVX(
    { eventName, repo: { owner: "github", repo: "gh-aw" }, payload: { pull_request: { number: 123 } } },
    {
      rest: { pulls: { listFiles: "listFiles" } },
      paginate: async (method, options) => {
        calls++;
        assert.equal(method, "listFiles");
        assert.deepEqual(options, { owner: "github", repo: "gh-aw", pull_number: 123, per_page: 100 });
        if (apiError) throw apiError;
        return files;
      },
    },
    {
      setOutput: (name, value) => {
        assert.equal(name, "run");
        output = value;
      },
      warning: () => {},
    }
  );
  return { output, calls };
}

test("NVX musl integration skips unrelated PR changes", async () => {
  for (const filename of ["actions/setup/js/github_api_check.cjs", "actions/setup/md/report.md", "pkg/cli/data/models.json", "Makefile", ".github/workflows/ci.yml"]) {
    assert.equal((await nvxSelection([{ filename }])).output, "false", filename);
  }
});

test("NVX musl integration covers its runtime, tests, version and dependencies", async () => {
  for (const filename of [
    "actions/setup/js/nvx_assets.cjs",
    "actions/setup/js/nvx_assets.test.cjs",
    "actions/setup/js/copilot_sdk_mcp_config_path.cjs",
    "actions/setup/js/copilot_sdk_mcp_config_path.test.cjs",
    "pkg/workflow/nvx_copilot_musl_integration_test.go",
    "pkg/constants/version_constants.go",
    "go.mod",
    "go.sum",
    ".github/workflows/cjs.yml",
    "scripts/workflow-tool-contracts.test.cjs",
  ]) {
    assert.equal((await nvxSelection([{ filename }])).output, "true", filename);
  }
});

test("NVX selection covers renames and relevant files beyond the first API page", async () => {
  assert.equal((await nvxSelection([{ filename: "renamed.cjs", previous_filename: "actions/setup/js/nvx_assets.cjs" }])).output, "true");
  const files = Array.from({ length: 100 }, () => ({ filename: "unrelated.cjs" }));
  files.push({ filename: "go.mod" });
  assert.equal((await nvxSelection(files)).output, "true");
});

test("NVX selection preserves non-PR runs and fails open on missing file data", async () => {
  for (const event of ["push", "workflow_dispatch"]) {
    assert.deepEqual(await nvxSelection([], event), { output: "true", calls: 0 });
  }
  assert.equal((await nvxSelection([], "pull_request", new Error("API unavailable"))).output, "true");
  assert.equal((await nvxSelection(Array.from({ length: 3000 }, () => ({ filename: "unrelated.cjs" })))).output, "true");
});

test("CJS wires the NVX selector at job level without filtering other jobs", () => {
  assert.match(cjsWorkflow, /nvx-musl: \$\{\{ steps\.nvx-paths\.outputs\.run \}\}/);
  assert.match(cjsWorkflow, /nvx-musl-integration:\n[\s\S]*?if: github\.event_name != 'pull_request' \|\| needs\.checkout-cache\.outputs\.nvx-musl == 'true'/);
  assert.equal((cjsWorkflow.match(/needs\.checkout-cache\.outputs\.nvx-musl/g) || []).length, 1);
  const triggers = cjsWorkflow.slice(0, cjsWorkflow.indexOf("\njobs:"));
  for (const filename of ["pkg/workflow/nvx_copilot_musl_integration_test.go", "pkg/constants/version_constants.go", "go.mod", "go.sum", "scripts/workflow-tool-contracts.test.cjs"]) {
    assert.equal(triggers.split(`- '${filename}'`).length - 1, 2, `${filename} must trigger push and PR coverage`);
  }
  assert.match(cjsWorkflow, /cleanup-cache:[\s\S]*?- nvx-musl-integration[\s\S]*?if: always\(\)/);
});

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
