---
import-schema:
  engine-id:
    type: string
    required: true
network:
  allowed: []
runtimes:
  node:
    version: "24"
tools:
  github: false
  edit:
  bash:
    - "*"
safe-outputs:
  staged: true
  threat-detection: false
pre-agent-steps:
  - name: Prepare engine conformance fixtures
    env:
      CONFORMANCE_ENGINE: ${{ github.aw.import-inputs.engine-id }}
      CONFORMANCE_ROOT: /tmp/gh-aw/agent/engine-conformance
      CONFORMANCE_STATE: ${{ runner.temp }}/engine-conformance
    run: |
      set -euo pipefail
      node <<'JS'
      const crypto = require("node:crypto");
      const fs = require("node:fs");
      const path = require("node:path");
      const engine = process.env.CONFORMANCE_ENGINE;
      if (!/^[a-z][a-z0-9-]*$/.test(engine || "")) throw new Error("Invalid conformance engine ID");
      const root = process.env.CONFORMANCE_ROOT;
      const host = process.env.CONFORMANCE_STATE;
      if (!root || !host) throw new Error("Conformance paths are missing");
      fs.mkdirSync(root, { recursive: true });
      fs.mkdirSync(host, { recursive: true, mode: 0o700 });
      const fixture = {
        fileNonce: crypto.randomBytes(24).toString("hex"),
        left: crypto.randomInt(100, 1000),
        right: crypto.randomInt(100, 1000),
      };
      const expected = {
        engine,
        ...fixture,
        engineEnv: `conformance-${engine}`,
        shellDigest: crypto.createHash("sha256").update(fixture.fileNonce).digest("hex"),
      };
      fs.writeFileSync(path.join(host, "expected.json"), JSON.stringify(expected), { mode: 0o600, flag: "wx" });
      fs.writeFileSync(path.join(root, "input.json"), JSON.stringify(fixture), { mode: 0o644, flag: "wx" });
      fs.writeFileSync(path.join(root, "shell-probe.cjs"), [
        'const crypto = require("node:crypto");',
        'const fs = require("node:fs");',
        'const path = require("node:path");',
        'const input = JSON.parse(fs.readFileSync(path.join(__dirname, "input.json"), "utf8"));',
        'const shellDigest = crypto.createHash("sha256").update(input.fileNonce).digest("hex");',
        'const result = { shellDigest, engineEnv: process.env.ENGINE_CONFORMANCE_SENTINEL };',
        'if (!result.engineEnv) throw new Error("ENGINE_CONFORMANCE_SENTINEL is missing");',
        'fs.writeFileSync(path.join(__dirname, "shell.json"), JSON.stringify(result));',
        'console.log(JSON.stringify(result));',
        "",
      ].join("\n"), { mode: 0o644, flag: "wx" });
      JS
mcp-scripts:
  conformance-challenge:
    description: "Return a fresh tool nonce and record a receipt for the engine conformance suite."
    inputs:
      file_nonce:
        type: string
        required: true
        description: "The fileNonce read from engine-conformance/input.json."
    env:
      CONFORMANCE_STATE: ${{ runner.temp }}/engine-conformance
    script: |
      const crypto = require("node:crypto");
      const fs = require("node:fs");
      const path = require("node:path");
      const host = process.env.CONFORMANCE_STATE;
      if (!host) throw new Error("CONFORMANCE_STATE is missing");
      const expected = JSON.parse(fs.readFileSync(path.join(host, "expected.json"), "utf8"));
      if (file_nonce !== expected.fileNonce) throw new Error("The fixture nonce does not match this run");
      const receipt = { fileNonce: file_nonce, toolNonce: crypto.randomBytes(24).toString("hex") };
      fs.writeFileSync(path.join(host, "receipt.json"), JSON.stringify(receipt), { mode: 0o600 });
      return { toolNonce: receipt.toolNonce };
post-steps:
  - name: Assert engine conformance
    if: always()
    env:
      CONFORMANCE_ENGINE: ${{ github.aw.import-inputs.engine-id }}
      CONFORMANCE_EXECUTION: ${{ steps.agentic_execution.outcome }}
      CONFORMANCE_ROOT: /tmp/gh-aw/agent/engine-conformance
      CONFORMANCE_STATE: ${{ runner.temp }}/engine-conformance
    run: |
      set -euo pipefail
      node <<'JS'
      const assert = require("node:assert/strict");
      const fs = require("node:fs");
      const path = require("node:path");
      const root = process.env.CONFORMANCE_ROOT;
      const host = process.env.CONFORMANCE_STATE;
      if (!root || !host) throw new Error("Conformance paths are missing");
      const checks = [];
      const check = (name, run) => {
        try {
          run();
          checks.push({ name, status: "passed" });
        } catch (error) {
          checks.push({ name, status: "failed", error: error.message });
        }
      };
      const read = (directory, name) => {
        const directoryStat = fs.lstatSync(directory);
        assert.ok(directoryStat.isDirectory() && !directoryStat.isSymbolicLink(), "Evidence directory must not be a symlink");
        const file = path.join(directory, name);
        const stat = fs.lstatSync(file);
        assert.ok(stat.isFile() && !stat.isSymbolicLink(), `${name} must be a regular file`);
        assert.ok(stat.size <= 16384, `${name} exceeds 16 KiB`);
        return JSON.parse(fs.readFileSync(file, "utf8"));
      };
      let expected, result, receipt, shell;
      check("agent-execution", () => assert.equal(process.env.CONFORMANCE_EXECUTION, "success"));
      check("fixtures", () => {
        expected = read(host, "expected.json");
        assert.equal(expected.engine, process.env.CONFORMANCE_ENGINE);
        assert.deepEqual(read(root, "input.json"), {
          fileNonce: expected.fileNonce, left: expected.left, right: expected.right,
        });
      });
      check("result-schema", () => {
        result = read(root, "result.json");
        assert.deepEqual(Object.keys(result).sort(), [
          "engineEnv", "fileNonce", "shellDigest", "sum", "toolNonce",
        ]);
        assert.equal(typeof result.sum, "number");
        for (const key of ["engineEnv", "fileNonce", "shellDigest", "toolNonce"]) {
          assert.equal(typeof result[key], "string", `${key} must be a string`);
        }
      });
      check("file-read", () => assert.equal(result.fileNonce, expected.fileNonce));
      check("inference", () => assert.equal(result.sum, expected.left + expected.right));
      check("shell-and-environment", () => {
        shell = read(root, "shell.json");
        assert.deepEqual(shell, { shellDigest: expected.shellDigest, engineEnv: expected.engineEnv });
        assert.equal(result.shellDigest, shell.shellDigest);
        assert.equal(result.engineEnv, shell.engineEnv);
      });
      check("tool-round-trip", () => {
        receipt = read(host, "receipt.json");
        assert.equal(receipt.fileNonce, expected.fileNonce);
        assert.match(receipt.toolNonce, /^[a-f0-9]{48}$/);
        assert.equal(result.toolNonce, receipt.toolNonce);
      });
      const passed = checks.every(item => item.status === "passed");
      const report = { engine: process.env.CONFORMANCE_ENGINE, status: passed ? "passed" : "failed", checks };
      fs.mkdirSync(host, { recursive: true, mode: 0o700 });
      fs.writeFileSync(path.join(host, "report.json"), JSON.stringify(report, null, 2), { mode: 0o600, flag: "wx" });
      const summary = [
        `## Engine conformance: ${report.engine}`,
        "",
        "| Probe | Result |",
        "|---|---|",
        ...checks.map(item => `| ${item.name} | ${item.status} |`),
        "",
        `Overall: **${report.status.toUpperCase()}**`,
        "",
      ].join("\n");
      if (process.env.GITHUB_STEP_SUMMARY) fs.appendFileSync(process.env.GITHUB_STEP_SUMMARY, summary);
      console.log(summary);
      for (const item of checks.filter(item => item.status === "failed")) {
        const message = `${item.name}: ${item.error}`.replace(/%/g, "%25").replace(/\r/g, "%0D").replace(/\n/g, "%0A");
        console.error(`::error::${message}`);
      }
      if (!passed) process.exitCode = 1;
      JS
  - name: Upload engine conformance evidence
    if: always()
    uses: actions/upload-artifact@v7.0.1
    with:
      name: engine-conformance-${{ github.aw.import-inputs.engine-id }}
      path: ${{ runner.temp }}/engine-conformance/report.json
      retention-days: 2
      if-no-files-found: error
---

# Engine configuration conformance

This is a small configuration test, not a repository task. Do not change repository
files, install dependencies, or publish GitHub resources. Do not edit fixtures,
the shell probe, or the checker. Stop immediately if a required operation fails;
do not claim success or substitute a guessed value.

1. Read `/tmp/gh-aw/agent/engine-conformance/input.json` with your file-reading
   capability. Retain `fileNonce`, `left`, and `right`.
2. Use your shell capability to execute exactly:
   `node /tmp/gh-aw/agent/engine-conformance/shell-probe.cjs`.
   Retain the returned `shellDigest` and `engineEnv`.
3. Use the mounted MCP CLI through your shell capability:
   `mcpscripts conformance-challenge --file_nonce "<fileNonce>"`.
   Pass the actual fixture nonce, then retain the returned `toolNonce`.
   This tests the production MCP gateway/CLI transport, including for engines
   without a native MCP client.
4. Use your file-editing capability to create
   `/tmp/gh-aw/agent/engine-conformance/result.json` with exactly these fields:
   `fileNonce` (string), `sum` (number, `left + right`), `shellDigest` (string),
   `engineEnv` (string), and `toolNonce` (string).

After writing the result, call `safeoutputs noop --message "Conformance probes completed"`.
Safe outputs are staged; do not create an issue or comment. Your final reply
should be one short line. The JavaScript post-step, not your reply, decides
whether the suite passed.
