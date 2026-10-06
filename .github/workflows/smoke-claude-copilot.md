---
private: true
name: Smoke Claude Copilot
description: Canary for Claude native Messages inference and MCP tool round-trips through GitHub Copilot.
intent: Detect CAPI incompatibilities with Claude Code requests, streaming responses, and tool results.
on:
  workflow_dispatch:
    inputs:
      model:
        description: Copilot-hosted Anthropic model ID (without copilot/).
        type: string
        default: claude-haiku-4.5
        required: true
concurrency:
  job-discriminator: ${{ github.run_id }}
permissions:
  contents: read
  copilot-requests: write
engine:
  id: claude
  bare: true
  harness:
    max-retries: 0
model: copilot/${{ inputs.model }}
max-turns: 5
checkout: false
tools:
  github: false
  bash: false
  edit: false
  cli-proxy: false
mcp-scripts:
  capi-probe:
    description: Return the deterministic Copilot smoke-test receipt.
    env:
      SMOKE_RECEIPT: ${{ runner.temp }}/gh-aw/safeoutputs/claude-copilot-receipt.json
    script: |
      const fs = require("node:fs");
      const receipt = { message: "CLAUDE_COPILOT_TOOL_OK" };
      fs.writeFileSync(process.env.SMOKE_RECEIPT, JSON.stringify(receipt), { mode: 0o600 });
      return receipt;
safe-outputs:
  noop:
    max: 1
    report-as-issue: false
  threat-detection: false
sandbox:
  agent:
    model-fallback: false
post-steps:
  - name: Assert Claude Copilot inference evidence
    if: always()
    uses: actions/github-script@v9
    env:
      SMOKE_RECEIPT: ${{ runner.temp }}/gh-aw/safeoutputs/claude-copilot-receipt.json
      SMOKE_OUTPUTS: ${{ steps.set-runtime-paths.outputs.GH_AW_SAFE_OUTPUTS }}
      SMOKE_EXECUTION: ${{ steps.agentic_execution.outcome }}
    with:
      script: |
        const assert = require("node:assert/strict");
        const fs = require("node:fs");
        assert.equal(process.env.SMOKE_EXECUTION, "success",
          "Claude Copilot inference failed; inspect agent-stdio.log and AWF proxy diagnostics for CAPI auth, model, endpoint, beta-header, or request-schema rejection.");
        assert.deepEqual(JSON.parse(fs.readFileSync(process.env.SMOKE_RECEIPT, "utf8")),
          { message: "CLAUDE_COPILOT_TOOL_OK" }, "No successful native MCP tool receipt");
        const outputs = fs.readFileSync(process.env.SMOKE_OUTPUTS, "utf8")
          .split("\n").filter(line => line.trim()).map(line => JSON.parse(line));
        assert.equal(outputs.filter(output => output.type === "noop" &&
          output.message === "CLAUDE_COPILOT_TOOL_OK").length, 1,
          "Claude did not complete inference after receiving the MCP tool result");
        await core.summary.addHeading("Claude Copilot CAPI: PASS", 2)
          .addRaw("Native Messages inference, streaming tool use, and a follow-up safe output completed without model fallback.\n").write();
timeout-minutes: 5
---

# Claude Copilot CAPI Canary

Call the `capi-probe` MCP tool exactly once. After receiving its result, call the
`noop` safe-output tool exactly once with that result's `message`,
`CLAUDE_COPILOT_TOOL_OK`.

Do not inspect the repository, run commands, or perform any other task. Do not
invent a tool result or report success if inference or a tool call fails.
