---
max-turn-cache-misses: 12
mcp-servers:
  agy-native:
    type: http
    url: "http://host.docker.internal:${{ steps.mcp-scripts-start.outputs.port }}"
    headers:
      Authorization: "${{ steps.mcp-scripts-start.outputs.api_key }}"
    allowed:
      - native_challenge
mcp-scripts:
  native-challenge:
    description: Return a fresh nonce and record evidence for the native Agy MCP transport.
    inputs:
      file_nonce:
        type: string
        required: true
        description: The fileNonce from the conformance fixture.
    env:
      CONFORMANCE_STATE: ${{ runner.temp }}/engine-conformance
    script: |
      const crypto = require("node:crypto");
      const fs = require("node:fs");
      const path = require("node:path");
      const host = process.env.CONFORMANCE_STATE;
      if (!host) throw new Error("CONFORMANCE_STATE is missing");
      const expected = JSON.parse(fs.readFileSync(path.join(host, "expected.json"), "utf8"));
      if (file_nonce !== expected.fileNonce) throw new Error("The fixture nonce does not match");
      const receipt = { fileNonce: file_nonce, toolNonce: crypto.randomBytes(24).toString("hex") };
      fs.writeFileSync(path.join(host, "native-receipt.json"), JSON.stringify(receipt), { mode: 0o600 });
      return { toolNonce: receipt.toolNonce };
  native-infrastructure-challenge:
    description: Return a fresh nonce and record evidence for native MCP scripts access.
    inputs:
      file_nonce:
        type: string
        required: true
        description: The fileNonce from the conformance fixture.
    env:
      CONFORMANCE_STATE: ${{ runner.temp }}/engine-conformance
    script: |
      const crypto = require("node:crypto");
      const fs = require("node:fs");
      const path = require("node:path");
      const host = process.env.CONFORMANCE_STATE;
      if (!host) throw new Error("CONFORMANCE_STATE is missing");
      const expected = JSON.parse(fs.readFileSync(path.join(host, "expected.json"), "utf8"));
      if (file_nonce !== expected.fileNonce) throw new Error("The fixture nonce does not match");
      const receipt = { fileNonce: file_nonce, toolNonce: crypto.randomBytes(24).toString("hex") };
      fs.writeFileSync(path.join(host, "native-infrastructure-receipt.json"), JSON.stringify(receipt), { mode: 0o600 });
      return { toolNonce: receipt.toolNonce };
post-steps:
  - name: Assert native Agy MCP transport and inference accounting
    if: always()
    env:
      CONFORMANCE_STATE: ${{ runner.temp }}/engine-conformance
      CONFORMANCE_SAFE_OUTPUTS: ${{ steps.set-runtime-paths.outputs.GH_AW_SAFE_OUTPUTS }}
    run: |
      set -euo pipefail
      node <<'JS'
      const assert = require("node:assert/strict");
      const fs = require("node:fs");
      const path = require("node:path");
      const host = process.env.CONFORMANCE_STATE;
      const receipt = JSON.parse(fs.readFileSync(path.join(host, "native-receipt.json"), "utf8"));
      const expected = JSON.parse(fs.readFileSync(path.join(host, "expected.json"), "utf8"));
      assert.equal(receipt.fileNonce, expected.fileNonce);
      assert.match(receipt.toolNonce, /^[a-f0-9]{48}$/);
      const entries = fs.readFileSync("/tmp/gh-aw/agent-stdio.log", "utf8").split(/\r?\n/).flatMap(line => {
        try { return [JSON.parse(line)]; } catch { return []; }
      });
      const tools = entries.filter(entry => entry.event === "step_update" && entry.step_update?.step_type === "tool");
      const completedCall = (server, tool, args) => tools.find(entry => {
        const step = entry.step_update;
        const info = step.tool_info;
        return step.state === "DONE" && info?.name === "call_mcp_tool" &&
          info.parameters?.ServerName === server &&
          info.parameters?.ToolName === tool &&
          Object.entries(args).every(([key, value]) => info.parameters?.Arguments?.[key] === value);
      })?.step_update.tool_info;
      const native = completedCall("agy-native", "native_challenge", { file_nonce: expected.fileNonce });
      assert.ok(native,
        "The challenge must be called through the native MCP client, not its CLI wrapper");
      assert.equal(native.error, undefined, "The native MCP challenge must complete without an error");
      assert.deepEqual(JSON.parse(native.output), { toolNonce: receipt.toolNonce });
      const infrastructureReceipt = JSON.parse(fs.readFileSync(path.join(host, "native-infrastructure-receipt.json"), "utf8"));
      assert.equal(infrastructureReceipt.fileNonce, expected.fileNonce);
      assert.match(infrastructureReceipt.toolNonce, /^[a-f0-9]{48}$/);
      const infrastructure = completedCall("mcpscripts", "native_infrastructure_challenge", { file_nonce: expected.fileNonce });
      assert.ok(infrastructure, "MCP scripts must be called through the native MCP client");
      assert.equal(infrastructure.error, undefined, "The native MCP scripts challenge must complete without an error");
      assert.deepEqual(JSON.parse(infrastructure.output), { toolNonce: infrastructureReceipt.toolNonce });
      const noop = completedCall("safeoutputs", "noop", { message: "Conformance probes completed" });
      assert.ok(noop, "The final noop must be called through the native safeoutputs MCP server");
      assert.equal(noop.error, undefined, "The native safe-output call must complete without an error");
      const terminal = entries.filter(entry => entry.event === "result").at(-1)?.result;
      assert.equal(terminal?.status, "SUCCESS");
      assert.ok(Number.isSafeInteger(terminal?.num_turns) && terminal.num_turns > 0);
      assert.ok(terminal?.usage?.input_tokens > 0);
      assert.ok(terminal?.usage?.output_tokens > 0);
      assert.ok(entries.some(entry => entry.event === "init" && entry.init?.model === "gemini-3.8-flash-medium"));
      const outputs = process.env.CONFORMANCE_SAFE_OUTPUTS;
      assert.ok(outputs, "Safe-output evidence path is required");
      const outputStat = fs.lstatSync(outputs);
      assert.ok(outputStat.isFile() && !outputStat.isSymbolicLink() && outputStat.size <= 16384);
      assert.deepEqual(JSON.parse(fs.readFileSync(outputs, "utf8")), {
        type: "noop", message: "Conformance probes completed",
      });
      const usage = {};
      for (const name of ["input_tokens", "output_tokens", "thinking_tokens", "cache_read_tokens", "total_tokens"]) {
        const count = terminal.usage[name];
        if (count !== undefined) {
          assert.ok(Number.isSafeInteger(count) && count >= 0, `Invalid ${name}`);
          usage[name] = count;
        }
      }
      fs.writeFileSync(path.join(host, "native-report.json"),
        JSON.stringify({ status: "passed", nativeMCP: true, nativeInfrastructureMCP: true, nativeSafeOutputs: true, inference: true, stagedSafeOutputs: true, usage }, null, 2),
        { mode: 0o600 });
      JS
  - name: Upload native Agy conformance evidence
    if: always()
    uses: actions/upload-artifact@v7.0.2
    with:
      name: agy-production-native-conformance
      path: ${{ runner.temp }}/engine-conformance/native-report.json
      retention-days: 2
      if-no-files-found: error
imports:
  - uses: shared/engine-conformance.md
    with:
      engine-id: agy
---

Execute the imported engine configuration conformance suite once.

Also call the `native_challenge` tool through the native MCP server `agy-native`,
using the actual fixture `fileNonce`. Do not call `mcpscripts native-challenge`
or substitute a shell/HTTP request; the checker requires a native MCP tool event.
Complete this native challenge before the shared suite's final safe-output call.
Also call `native_infrastructure_challenge` through the native MCP server
`mcpscripts`, using the same fixture `fileNonce`. Do not use its CLI wrapper.
For this suite, emit the final noop exactly once through the native MCP server
`safeoutputs`, with message `"Conformance probes completed"`, after all probes.
This replaces the shared suite's default CLI completion call; do not also run
`safeoutputs noop` or emit another completion message.
