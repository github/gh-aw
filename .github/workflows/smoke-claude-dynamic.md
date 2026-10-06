---
name: Smoke Claude Dynamic Workflow
description: Verify Claude can invoke a saved dynamic workflow restored from the activation artifact, including a nested hidden fixture
intent: Detect regressions that prevent maintainers from running packaged Claude dynamic workflows in agentic workflows.
private: true
on:
  workflow_dispatch:
  push:
    branches: [main, "*-claude-dynamic-smoke"]
    paths:
      - .github/workflows/smoke-claude-dynamic.md
      - .github/workflows/smoke-claude-dynamic.lock.yml
      - .claude/workflows/smoke-claude-dynamic.js
      - .claude/workflows/references/smoke-claude-dynamic/**
permissions:
  contents: read
concurrency:
  job-discriminator: "${{ github.run_id }}"
engine:
  id: claude
  bare: false
  dynamic-workflows: true
model: claude-sonnet-4-6
max-turns: 20
timeout-minutes: 10
checkout:
  pull-request: false
network:
  allowed: [defaults]
tools:
  github: false
  edit: false
  bash:
    - "mkdir *"
    - "node *"
    - "cat *"
safe-outputs:
  noop:
post-steps:
  - name: Verify packaged Claude dynamic workflow evidence
    if: always()
    env:
      SMOKE_RUN_ID: ${{ github.run_id }}
    run: |
      node <<'NODE'
      const assert = require("node:assert/strict");
      const fs = require("node:fs");
      const path = require("node:path");
      const { parseLogEntries } = require(path.join(process.env.RUNNER_TEMP, "gh-aw", "actions", "log_parser_shared.cjs"));
      const script = ".claude/workflows/smoke-claude-dynamic.js";
      const fixture = ".claude/workflows/references/smoke-claude-dynamic/.context";
      for (const file of [script, fixture]) {
        assert.deepEqual(
          fs.readFileSync(path.join(process.env.GITHUB_WORKSPACE, file)),
          fs.readFileSync(path.join("/tmp/gh-aw/base", file)),
          `${file} must match the trusted activation artifact`
        );
      }
      const result = JSON.parse(fs.readFileSync("/tmp/gh-aw/agent/claude-dynamic-smoke.json", "utf8"));
      assert.deepEqual(result, {
        status: "PASS",
        workflow: "smoke-claude-dynamic",
        token: "gh-aw-claude-dynamic-fixture-v1",
        runId: process.env.SMOKE_RUN_ID
      }, "The saved workflow must return the expected fixture and invocation arguments");
      const events = parseLogEntries(fs.readFileSync("/tmp/gh-aw/agent-stdio.log", "utf8"));
      const launches = events.filter(event =>
        event.type === "tool.execution_start" &&
        event.data?.toolName === "Workflow" &&
        JSON.stringify(event.data.input).includes("smoke-claude-dynamic")
      );
      assert.ok(launches.length > 0, "Claude must invoke the saved script through the native Workflow tool");
      assert.ok(launches.some(launch => events.some(event =>
        event.type === "tool.execution_complete" &&
        event.data?.toolCallId === launch.data.toolCallId &&
        event.data.success !== false &&
        event.data.is_error !== true
      )), "The native Workflow tool must launch successfully");
      fs.appendFileSync(process.env.GITHUB_STEP_SUMMARY,
        "## Claude dynamic workflow smoke: PASS\n\n" +
        "Verified trusted activation packaging, recursive hidden-file restoration, " +
        "native Workflow invocation, fixture contents, and run-ID argument propagation.\n");
      NODE
---

# Claude Dynamic Workflow Smoke Test

Run the saved dynamic workflow `/smoke-claude-dynamic` using the native **Workflow**
tool, with structured arguments `{"runId": "${{ github.run_id }}"}`.
Its script is `.claude/workflows/smoke-claude-dynamic.js`.
Do not create, rewrite, or substitute a workflow script, and do not perform the
fixture-reading task yourself.

Wait for the workflow to finish and use its returned JSON object, not the
background-launch acknowledgement. It must contain `status: "PASS"`,
`workflow: "smoke-claude-dynamic"`, the fixture token, and this run's ID.
Write that returned object as JSON to
`/tmp/gh-aw/agent/claude-dynamic-smoke.json` using Bash and Node.

If the saved workflow is missing, the Workflow tool is unavailable, or execution
fails, report the exact error; do not synthesize a passing result. The deterministic
post-step fails on missing or incorrect evidence. After successful completion,
call the `noop` safe-output tool with a brief success summary; this smoke test
must not create issues, comments, or repository changes.
