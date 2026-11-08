import { existsSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { readDocumentMetadata } from "./markdown_metadata.cjs";

const root = resolve(import.meta.dirname, "../../..");
const source = readFileSync(resolve(root, ".github/workflows/daily-mcp-concurrency-analysis.md"), "utf8");
const [, frontmatter, prompt] = source.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n([\s\S]*)$/);
const document = readDocumentMetadata(frontmatter, { version: "1.2", uniqueKeys: true });
const workflow = document.value;
const require = createRequire(import.meta.url);
const { buildCopilotSDKPermissionHandler } = require("./copilot_sdk_permissions.cjs");

describe("daily MCP concurrency workflow tool contract", () => {
  it("restricts shell access to bounded implementation history", () => {
    expect(workflow.tools["cli-proxy"]).toBe(false);
    expect(workflow.tools.bash).toEqual(['git log -30 --format="%ai %s" --name-only -- actions/setup/js/*.cjs']);
    expect(workflow["max-tool-denials"]).toBe(3);
    expect(workflow.tools).toHaveProperty("edit");
    expect(prompt).toContain("native `glob`, `rg`, and `view`");
    expect(prompt).toContain("native `apply_patch` or edit tools");
    const shellExamples = [...prompt.matchAll(/```bash\n([\s\S]*?)```/g)].map(match => match[1].trim());
    expect(shellExamples).toEqual(workflow.tools.bash);
    expect(prompt).not.toMatch(/\$\((?:date|cat|jq)\b|cat .* \| jq|TOOL_FILE=|safeoutputs create_issue/);
    expect(prompt).toContain("call `report_incomplete` with the concrete blocker");
    expect(prompt).toContain("Do not port this shell fallback to an engine without shell filters (such as Codex)");
    expect(prompt).toContain("Never execute handler modules");
    expect(prompt).toContain("never call a tool named `mcpscripts`");
  });

  it("discovers real MCP and deferred handler paths instead of guessing filenames", () => {
    for (const file of ["pkg/workflow/js/safe_outputs_tools.json", "actions/setup/js/safe_outputs_tools_loader.cjs", "actions/setup/js/safe_outputs_handlers.cjs", "actions/setup/js/safe_output_handler_manager.cjs"]) {
      expect(existsSync(resolve(root, file))).toBe(true);
      expect(prompt).toContain(file.split("/").at(-1));
    }
    expect(prompt).toContain("Read `pkg/workflow/js/safe_outputs_tools.json` with native `view`");
    expect(prompt).toContain("verify the implementation path with native `glob`");
    expect(prompt).toContain("Do not assume a tool named `<tool_name>` has a file named `<tool_name>.cjs`");
    expect(prompt).not.toContain("~32 tools");
    expect(prompt).toContain("Analyze exactly one tool per run");
    expect(prompt).not.toContain("Continue to next tool");
  });

  it("allows the documented commands with the real SDK matcher but rejects denied pipelines", () => {
    const handler = buildCopilotSDKPermissionHandler({ allowedTools: workflow.tools.bash.map(command => `shell(${command})`) }, () => ({ kind: "approve-once" }));
    const check = command => handler({ kind: "shell", commands: [], fullCommandText: command });
    for (const command of workflow.tools.bash) {
      expect(check(command)).toEqual({ kind: "approve-once" });
    }
    for (const command of [
      "cd /workspace; ls /tmp/gh-aw/cache-memory/",
      "jq -r '.[].name' pkg/workflow/js/safe_outputs_tools.json | sort | tr '\\n' ' '",
      "cat > /tmp/gh-aw/agent/report.md <<'EOF'\nreport\nEOF",
      "safeoutputs create_issue .",
      "sqlite3 -readonly /tmp/gh-aw/ledgers/concurrency-audits/ledger.db '.shell echo unsafe'",
      "sqlite3 -readonly /tmp/gh-aw/ledgers/concurrency-audits/ledger.db 'DELETE FROM records;'",
      'git log -1 --format="%ai" -- actions/setup/js/safe_outputs_tools_loader.cjs',
      'git log -30 --format="%ai %s" --name-only -- pkg/cli/*.go',
      `${workflow.tools.bash[0]} && node actions/setup/js/upload_assets.cjs`,
      "git log -1 --format=%ai -- actions/setup/js/upload_asset.cjs && node actions/setup/js/upload_asset.cjs",
    ]) {
      expect(check(command).kind).not.toBe("approve-once");
    }
  });

  it("preserves cache state, direct completion outputs, and bounded ledger records", () => {
    expect(workflow.tools["cache-memory"]).toBe(true);
    expect(prompt).toContain("Reuse the existing state file and its format");
    expect(prompt).toContain("Read it back with `view`");
    expect(workflow.tools.ledger["concurrency-audits"].schema.required).toEqual(["record_type", "run_id", "analyzed_tool", "status"]);
    expect(workflow["safe-outputs"]).toHaveProperty("create-issue");
    expect(workflow["safe-outputs"]).toHaveProperty("create-agent-session");
    expect(prompt).toContain("call them directly with structured arguments");
    expect(prompt).toContain("Pass this object directly as arguments to `noop`");
    expect(prompt).toContain("If you emitted any actionable safe outputs, do not emit `noop`");
    expect(prompt).toContain("`ledger: concurrency-audits`");
    expect(prompt).toContain("Submit at most once");
    expect(prompt).toContain("queued records are not visible in this run's read-only projection");
    expect(prompt).toContain("do not store code or raw analysis notes");
    expect(prompt).toContain("Complete the cache read-back and ledger query/submission before calling `noop`");
    expect(prompt).toContain("no terminal safe output was emitted");
  });

  it("queries only the fixed ledger with read-only, parameterized SQL and closes it", async () => {
    const { DatabaseSync } = require("node:sqlite");
    const db = new DatabaseSync(":memory:");
    db.exec("CREATE TABLE records (ordinal INTEGER PRIMARY KEY, payload TEXT)");
    const insert = db.prepare("INSERT INTO records (payload) VALUES (?)");
    const record = (run_id, analyzed_tool, status, record_type = "mcp_concurrency_audit") => ({ record_type, run_id, analyzed_tool, status });
    const old = record("1", "noop", "clean");
    const latest = record("2", "noop", "issues_found");
    const current = record("3", "create_issue", "clean");
    for (const value of [old, latest, current, record("4", "noop", "ignored", "other")]) insert.run(JSON.stringify(value));
    let closed = false;
    const scriptRequire = name => {
      expect(name).toBe("node:sqlite");
      return {
        DatabaseSync: class {
          constructor(file, options) {
            expect(file).toBe("/tmp/gh-aw/ledgers/concurrency-audits/ledger.db");
            expect(options).toEqual({ readOnly: true });
          }
          prepare(sql) {
            return db.prepare(sql);
          }
          close() {
            closed = true;
          }
        },
      };
    };
    const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
    const query = new AsyncFunction("require", "process", "analyzed_tool", workflow["mcp-scripts"]["concurrency-audit-state"].script);
    try {
      expect(await query(scriptRequire, { env: { AUDIT_RUN_ID: "3" } }, "noop")).toEqual({ current_run: current, latest_tool: latest });
      expect(await query(scriptRequire, { env: { AUDIT_RUN_ID: "missing" } }, "noop' OR 1=1 --")).toEqual({ current_run: null, latest_tool: null });
      expect(closed).toBe(true);
      db.exec("DROP TABLE records");
      closed = false;
      await expect(query(scriptRequire, { env: { AUDIT_RUN_ID: "3" } }, "noop")).rejects.toThrow("no such table");
      expect(closed).toBe(true);
    } finally {
      db.close();
    }
  });
});
