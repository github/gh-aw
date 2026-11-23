"use strict";

const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");
const { test } = require("node:test");
const { buildCopilotSDKPermissionHandler } = require("../actions/setup/js/copilot_sdk_permissions.cjs");
const { parseToolArgs } = require("../actions/setup/js/mcp_cli_bridge.cjs");
const { hasTerminalSafeOutput } = require("../actions/setup/js/safeoutputs_cli.cjs");
const { queryArguments } = require("../.github/workflows/shared/graft/query.cjs");

function workflowSource(name) {
  return fs.readFileSync(path.join(__dirname, "..", ".github", "workflows", `${name}.md`), "utf8");
}

function instructionPrefix(source) {
  const marker = "<!-- instruction-prefix-end -->";
  const end = source.indexOf(marker);
  assert.notEqual(end, -1, "Missing self-contained instruction prefix");
  assert.ok(Buffer.byteLength(source.slice(0, end + marker.length)) <= 7500);
  return source.slice(0, end);
}

function workflowLock(name) {
  const lockPath = path.join(__dirname, "..", ".github", "workflows", `${name}.lock.yml`);
  return JSON.parse(execFileSync("yq", ["-o=json", lockPath], { encoding: "utf8" }));
}

function compiledMCPServer(lock, serverName) {
  const gatewayStep = lock.jobs.agent.steps.find(step => step.name === "Start MCP Gateway");
  assert.ok(gatewayStep, "Missing MCP gateway step");

  const heredoc = gatewayStep.run.match(/cat << (GH_AW_MCP_CONFIG_[a-f0-9]{16}_EOF) \|[^\n]*\n/);
  assert.ok(heredoc, "Missing compiled MCP configuration heredoc");
  const bodyStart = heredoc.index + heredoc[0].length;
  const bodyEnd = gatewayStep.run.indexOf(`\n${heredoc[1]}`, bodyStart);
  assert.notEqual(bodyEnd, -1, "Missing compiled MCP configuration heredoc terminator");

  const configText = gatewayStep.run.slice(bodyStart, bodyEnd).replace(/\\\$/g, "$");
  const gatewayStart = configText.indexOf('\n  "gateway":');
  assert.notEqual(gatewayStart, -1, "Missing compiled gateway configuration");
  const mcpServersText = configText.slice(0, gatewayStart).replace(/,\s*$/, "") + "\n}";
  return JSON.parse(mcpServersText).mcpServers[serverName];
}

function workflowPermissions(name) {
  const lock = workflowLock(name);
  const config = lock.jobs.agent.steps.find(step => step.name === "Execute GitHub Copilot CLI")?.env?.GH_AW_COPILOT_SDK_TOOL_CONFIG;
  assert.ok(config, `Missing compiler-owned SDK permissions for ${name}`);
  return JSON.parse(config).permissions;
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
  const handler = permissionHandler("code-scanning-fixer-worker");
  for (const command of ["safeoutputs --help", "github --help", "git status --short", "git diff --binary --no-ext-diff | wc -c", `work-queue work_queue_claim_finish '{"claim_handle":"h1","outcome":"cancelled"}'`]) {
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
  const prompt = fs.readFileSync(path.join(__dirname, "..", ".github", "workflows", "code-scanning-fixer-worker.md"), "utf8");
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

test("Daily Graft uses full history and fails closed on unusable graph analysis", () => {
  const workflows = path.join(__dirname, "..", ".github", "workflows");
  const source = fs.readFileSync(path.join(workflows, "daily-graft-intelligence.md"), "utf8");
  const graft = fs.readFileSync(path.join(workflows, "shared", "graft.md"), "utf8");

  assert.match(source, /checkout:\s*\n\s+fetch-depth: 0/);
  assert.doesNotMatch(source, /graft-(?:recent-activity|changed-files)\.txt \|\| true/);
  assert.match(graft, /check "\$GITHUB_WORKSPACE" --json/);
  assert.match(graft, /report\.graph\?\.ok === true/);
  assert.match(graft, /Graft wiring graph is missing or stale/);
  assert.match(source, /optional deep layer may report `NO GRAPH`/);
  assert.match(source, /safeoutputs report_incomplete/);
});

test("Daily Graft runs the native CLI against a read-only graph without a Graft MCP backend", () => {
  const lock = workflowLock("daily-graft-intelligence");
  assert.equal(compiledMCPServer(lock, "graft"), undefined);
  const steps = lock.jobs.agent.steps;
  const install = steps.find(step => step.name === "Install Graft CLI");
  const build = steps.find(step => step.name === "Build Graft context graph");
  const execute = steps.find(step => step.name === "Execute GitHub Copilot CLI");
  assert.ok(steps.indexOf(install) < steps.indexOf(build));
  assert.match(install.run, /shared\/graft\/package-lock\.json/);
  assert.match(install.run, /npm ci --prefix "\$GRAFT_CLI_DIR"/);
  assert.match(install.run, /query\.cjs.*\$GRAFT_CLI_DIR\/bin\/graft/);
  assert.match(install.run, /%s\/bin.*GITHUB_PATH/);
  assert.match(build.run, /node_modules\/\.bin\/graft --dir "\$GRAFT_DIR" build "\$GITHUB_WORKSPACE"/);
  assert.match(execute.run, /--mount.*\/tmp\/gh-aw\/graft:\/tmp\/gh-aw\/graft:ro/);
  assert.match(execute.run, /--mount.*\/tmp\/gh-aw\/graft-cli:\/tmp\/gh-aw\/graft-cli:ro/);
});

test("Daily Graft query wrapper rejects effective graph-directory overrides", () => {
  const graph = "--dir=/tmp/gh-aw/graft";
  assert.deepEqual(queryArguments([graph, "ask", "compiler", "--source"]), [graph, "ask", "compiler", "--source"]);
  for (const args of [
    [graph, "ask", "compiler", "--dir=/tmp/alternate-graph"],
    [graph, "map", "--dir", "/tmp/alternate-graph"],
    ["--dir=/tmp/alternate-graph", "map"],
    [graph, "check"],
    [graph, "build"],
    [graph, "mcp"],
  ]) {
    assert.throws(() => queryArguments(args), /pre-built graph directory|Use graft/);
  }
});

test("Daily Graft permits native queries but rejects duplicate checks and graph mutations", () => {
  const handler = permissionHandler("daily-graft-intelligence");
  for (const command of [
    "graft --help",
    "graft --dir=/tmp/gh-aw/graft map",
    "graft --dir=/tmp/gh-aw/graft map --max-dirs 5",
    'graft --dir=/tmp/gh-aw/graft ask "compiler" --source',
    'graft --dir=/tmp/gh-aw/graft callers "Compile" --depth 2',
    'graft --dir=/tmp/gh-aw/graft skeleton "pkg/workflow/compiler.go"',
    'graft --dir=/tmp/gh-aw/graft grep "Compile"',
  ]) {
    checkCommand(handler, command, "approve-once");
  }
  for (const command of [
    "graft --dir=/tmp/gh-aw/graft check",
    "graft --dir=/tmp/gh-aw/graft build",
    "graft --dir=/tmp/gh-aw/graft mcp",
    "graft graft_check",
    "graft graft_map",
    "graft map",
    "graft --dir=/tmp/other-graph map",
    "npx -y @nanonets/graft@0.8.0 check",
  ]) {
    checkCommand(handler, command, "reject");
  }
});

test("Dependabot missing-read diagnostics require a terminal incomplete outcome in every experiment", () => {
  const source = workflowSource("dependabot-go-checker");
  const prefix = instructionPrefix(source);
  assert.ok(source.indexOf("{{#if experiments.prompt_style") > prefix.length);
  assert.match(prefix, /`github` CLI bridge if it is\s+listed in the runtime's available CLI transports/);
  assert.match(prefix, /issue search or another\s+required read is unavailable, stop analysis/);
  assert.match(prefix, /`report_missing_tool` diagnostic is\s+nonterminal/);
  assert.match(prefix, /never use noop for\s+blocked or unfinished analysis/);

  const example = prefix.match(/safeoutputs report_incomplete '([^']+)'/);
  assert.ok(example, "Missing supported blocked-outcome invocation");
  const { args } = parseToolArgs([example[1]]);
  const tool = require("../actions/setup/js/safe_outputs_tools.json").find(tool => tool.name === "report_incomplete");
  for (const key of tool.inputSchema.required) assert.equal(typeof args[key], tool.inputSchema.properties[key].type);
  assert.deepEqual(Object.keys(args), ["reason"]);

  const diagnostic = { type: "report_missing_tool", tool: "GitHub issue search", reason: "Required read unavailable" };
  const terminal = records =>
    hasTerminalSafeOutput("fixture.jsonl", {
      readFileSync: () => records.map(record => JSON.stringify(record)).join("\n"),
      includeReportIncomplete: true,
      logger: () => {},
    });
  assert.equal(terminal([diagnostic]), false);
  assert.equal(terminal([diagnostic, { type: "report_incomplete", ...args }]), true);
});

test("API report collection cannot stand in for reporting or a submitted safe output", () => {
  const source = workflowSource("api-consumption-report");
  const prefix = instructionPrefix(source);
  assert.match(prefix, /pagination\s+allowance ends collection only, not the reporting task/);
  assert.match(prefix, /complete\s+\*\*Steps 2–6\*\*/);
  assert.match(prefix, /`safeoutputs create_discussion`.*JSON payload\s+\(`title` and `body`\) and wait for its response/);
  assert.match(prefix, /final text response is not evidence/);
  assert.match(prefix, /Use noop only when no reporting action is needed, never for an unfinished report/);
  assert.match(prefix, /safe-output transport itself is unavailable or denied, stop/);
  assert.doesNotMatch(source, /Do NOT call the CLI directly/);
  assert.match(source, /`agenticworkflows logs` CLI bridge when MCP calls are not exposed/);

  const examples = [...source.matchAll(/safeoutputs (report_incomplete|noop) '([^']+)'/g)];
  assert.equal(examples.length, 2);
  for (const [, name, payload] of examples) {
    const { args } = parseToolArgs([payload]);
    const tool = require("../actions/setup/js/safe_outputs_tools.json").find(tool => tool.name === name);
    for (const key of tool.inputSchema.required) assert.equal(typeof args[key], tool.inputSchema.properties[key].type);
    for (const key of Object.keys(args)) assert.ok(Object.hasOwn(tool.inputSchema.properties, key), `${name}: unknown field ${key}`);
  }
});
