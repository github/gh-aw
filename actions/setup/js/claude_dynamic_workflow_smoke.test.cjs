import { describe, expect, it } from "vitest";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { parseLogEntries } from "./log_parser_shared.cjs";
import { normalizeClaudeSession } from "./claude_session.cjs";
import { normalizeUnifiedSessionEvent } from "./unified_session_payload.cjs";
import { dynamicWorkflow } from "./fixtures/claude_dynamic_workflow.cjs";

const root = path.resolve(__dirname, "../../..");
const source = fs.readFileSync(path.join(root, ".github/workflows/smoke-claude-dynamic.md"), "utf8");
const verifier = source.match(/node <<'NODE'\n([\s\S]*?)\n      NODE/)[1];

function verify(records) {
  const env = { RUNNER_TEMP: "/runner", GITHUB_WORKSPACE: "/workspace", SMOKE_RUN_ID: "123", GITHUB_STEP_SUMMARY: "/summary" };
  const files = new Map();
  for (const file of [".claude/workflows/smoke-claude-dynamic.js", ".claude/workflows/references/smoke-claude-dynamic/.context"]) {
    const bytes = fs.readFileSync(path.join(root, file));
    files.set(path.join(env.GITHUB_WORKSPACE, file), bytes);
    files.set(path.join("/tmp/gh-aw/base", file), bytes);
  }
  files.set("/tmp/gh-aw/agent/claude-dynamic-smoke.json", JSON.stringify({ status: "PASS", workflow: "smoke-claude-dynamic", token: "gh-aw-claude-dynamic-fixture-v1", runId: env.SMOKE_RUN_ID }));
  files.set("/tmp/gh-aw/agent-stdio.log", records.map(record => JSON.stringify(record)).join("\n"));
  let summary = "";
  const modules = new Map([
    ["node:assert/strict", assert],
    ["node:path", path],
    [
      "node:fs",
      {
        readFileSync(file) {
          if (!files.has(file)) throw new Error(`Unexpected smoke verifier read: ${file}`);
          return files.get(file);
        },
        appendFileSync(file, text) {
          assert.equal(file, env.GITHUB_STEP_SUMMARY);
          summary += text;
        },
      },
    ],
    [path.join(env.RUNNER_TEMP, "gh-aw/actions/log_parser_shared.cjs"), { parseLogEntries }],
    [path.join(env.RUNNER_TEMP, "gh-aw/actions/claude_session.cjs"), { normalizeClaudeSession }],
    [path.join(env.RUNNER_TEMP, "gh-aw/actions/unified_session_payload.cjs"), { normalizeUnifiedSessionEvent }],
  ]);
  vm.runInNewContext(
    verifier,
    {
      process: { env },
      require(name) {
        if (!modules.has(name)) throw new Error(`Unexpected smoke verifier module: ${name}`);
        return modules.get(name);
      },
    },
    { timeout: 1000 }
  );
  return summary;
}

describe("Claude dynamic workflow smoke verifier", () => {
  it("accepts a packaged local launch with matching normalized completion evidence", () => {
    expect(verify(dynamicWorkflow)).toContain("Claude dynamic workflow smoke: PASS");
  });

  it.each(["embedded-error", "remote-launch", "missing-completion", "wrong-task", "wrong-tool", "failed-task"])("rejects %s even when the agent writes a passing result", scenario => {
    let records = structuredClone(dynamicWorkflow);
    if (scenario === "missing-completion") records = records.filter(record => record.subtype !== "task_notification");
    if (scenario === "embedded-error") records.find(record => record.tool_use_result).tool_use_result.error = "Workflow syntax check failed";
    if (scenario === "remote-launch") records.find(record => record.tool_use_result).tool_use_result.status = "remote_launched";
    if (scenario === "wrong-task") records.find(record => record.subtype === "task_notification").task_id = "other-task";
    if (scenario === "wrong-tool") records.find(record => record.subtype === "task_notification").tool_use_id = "other-tool";
    if (scenario === "failed-task") records.find(record => record.subtype === "task_notification").status = "failed";
    expect(() => verify(records)).toThrow("The native Workflow tool must launch locally without errors and its correlated task must complete");
  });
});
