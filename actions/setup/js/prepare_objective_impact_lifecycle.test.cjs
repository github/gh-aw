import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";

const req = createRequire(import.meta.url);
const childProcess = req("node:child_process");
const scriptPath = req.resolve("./prepare_objective_impact_lifecycle.cjs");
let root;
let exec;
let prepareIssueLifecycle;

beforeEach(() => {
  vi.stubEnv("GH_AW_ADOPTION_GH_TIMEOUT_MS", "300000");
  root = fs.mkdtempSync(path.join(path.dirname(scriptPath), ".impact-lifecycle-test-"));
  exec = vi.spyOn(childProcess, "execFileSync");
  delete req.cache[scriptPath];
  ({ prepareIssueLifecycle } = req(scriptPath));
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  delete req.cache[scriptPath];
  fs.rmSync(root, { recursive: true, force: true });
});

function writeRuns(runs) {
  fs.writeFileSync(path.join(root, "workflow-logs.json"), JSON.stringify({ runs }));
}

function report(items, runId = 1) {
  return JSON.stringify({ adoption: [{ run_id: runId, workflow: "producer", items }] });
}

describe("CLI-backed impact report lifecycle", () => {
  it("uses gh aw logs --adoption and preserves its evidence and objective attribution without reclassification", () => {
    writeRuns([{ run_id: 1, workflow_name: "old-name", aic: 4, url: "https://github.com/acme/repo/actions/runs/1" }]);
    exec.mockReturnValue(
      report([
        {
          type: "create_issue",
          object_number: 42,
          object_url: "https://github.com/acme/repo/issues/42",
          repo: "acme/repo",
          adoption_status: "pending",
          evidence_strength: "medium",
          signal: "open",
          detail: "created successfully",
          objective_value: 3,
          zero_touch: false,
        },
        { type: "close_issue", object_number: 43, adoption_status: "lifecycle", evidence_strength: "strong", signal: "bot_closed" },
        { type: "add_comment", adoption_status: "accepted", evidence_strength: "medium", signal: "engaged" },
      ])
    );
    prepareIssueLifecycle(root, "acme/repo");
    expect(exec).toHaveBeenCalledExactlyOnceWith(
      "gh",
      ["aw", "logs", "--stdin", "--adoption", "--repo", "acme/repo", "--output", path.join(root, "safe-output-runs"), "--timeout", "5", "--json"],
      expect.objectContaining({ encoding: "utf8", input: "1\n" })
    );
    const rows = fs.readFileSync(path.join(root, "safe-output-issue-evaluations.jsonl"), "utf8").trim().split("\n").map(JSON.parse);
    expect(rows).toHaveLength(2);
    expect(rows[0]).toMatchObject({ run_id: 1, workflow_name: "producer", workflow_aic: 4, number: 42, adoption_status: "pending", signal: "open", detail: "created successfully", objective_value: 3, zero_touch: false });
    expect(rows[1]).toMatchObject({ adoption_status: "lifecycle", signal: "bot_closed" });
    expect(JSON.parse(fs.readFileSync(path.join(root, "safe-output-issue-summary.json"), "utf8"))).toMatchObject({ total_issue_deliveries: 2, accepted_count: 0, pending_count: 1, lifecycle_count: 1 });
  });

  it("supports GitHub API and gh run list inputs and deduplicates source run IDs", () => {
    writeRuns([{ id: 1 }, { databaseId: 2 }, { run_id: 1 }]);
    exec.mockReturnValue(
      JSON.stringify({
        adoption: [
          { run_id: 2, items: [{ type: "close_issue", adoption_status: "accepted" }] },
          { run_id: 1, items: [{ type: "create_issue", adoption_status: "pending" }] },
        ],
      })
    );
    prepareIssueLifecycle(root, "acme/repo");
    expect(exec).toHaveBeenCalledExactlyOnceWith(
      "gh",
      ["aw", "logs", "--stdin", "--adoption", "--repo", "acme/repo", "--output", path.join(root, "safe-output-runs"), "--timeout", "10", "--json"],
      expect.objectContaining({ input: "1\n2\n", timeout: 600000, maxBuffer: 64 * 1024 * 1024 })
    );
    const rows = fs.readFileSync(path.join(root, "safe-output-issue-evaluations.jsonl"), "utf8").trim().split("\n").map(JSON.parse);
    expect(rows.map(row => row.run_id)).toEqual([1, 2]);
  });

  it("reevaluates source runs on later reports instead of caching adoption classifications", () => {
    writeRuns([{ run_id: 1 }]);
    exec.mockReturnValueOnce(report([{ type: "create_issue", adoption_status: "pending", signal: "open" }]));
    prepareIssueLifecycle(root, "acme/repo");
    exec.mockReturnValueOnce(report([{ type: "create_issue", adoption_status: "accepted", signal: "completed" }]));
    prepareIssueLifecycle(root, "acme/repo");
    expect(exec).toHaveBeenCalledTimes(2);
    expect(JSON.parse(fs.readFileSync(path.join(root, "safe-output-issue-evaluations.jsonl"), "utf8"))).toMatchObject({ adoption_status: "accepted", signal: "completed" });
  });

  it("surfaces CLI failures without publishing a successful empty report", () => {
    writeRuns([{ run_id: 1 }]);
    exec.mockImplementation(() => {
      throw new Error("unavailable");
    });
    expect(() => prepareIssueLifecycle(root, "acme/repo")).toThrow("Failed to evaluate adoption for selected runs");
    expect(fs.existsSync(path.join(root, "safe-output-issue-summary.json"))).toBe(false);
  });

  it("retains available observations and explicit missing evidence in a partial summary", () => {
    writeRuns([{ run_id: 1 }, { run_id: 2, workflow_name: "missing", url: "https://github.com/acme/repo/actions/runs/2" }]);
    exec.mockReturnValue(
      JSON.stringify({
        adoption: [
          { run_id: 1, items: [{ type: "create_issue", adoption_status: "accepted" }] },
          { run_id: 2, items: [], eval_error: "safe-output manifest unavailable; adoption coverage is unknown" },
        ],
      })
    );
    prepareIssueLifecycle(root, "acme/repo");
    expect(JSON.parse(fs.readFileSync(path.join(root, "safe-output-issue-evaluations.jsonl"), "utf8"))).toMatchObject({ run_id: 1, adoption_status: "accepted" });
    expect(JSON.parse(fs.readFileSync(path.join(root, "safe-output-issue-summary.json"), "utf8"))).toMatchObject({
      partial: true,
      selected_run_count: 2,
      evaluated_run_count: 1,
      unavailable_run_count: 1,
      total_issue_deliveries: 1,
      accepted_count: 1,
      run_errors: [{ run_id: 2, workflow_name: "missing", workflow_run_url: "https://github.com/acme/repo/actions/runs/2", eval_error: "safe-output manifest unavailable; adoption coverage is unknown" }],
    });
  });

  it.each([
    ["all unavailable", "safe-output manifest unavailable; adoption coverage is unknown", true, 0, 1],
    ["verified empty", undefined, false, 1, 0],
  ])("distinguishes %s runs from no published issue deliveries", (_label, evalError, partial, evaluated, unavailable) => {
    writeRuns([{ run_id: 1 }]);
    exec.mockReturnValue(JSON.stringify({ adoption: [{ run_id: 1, items: [], eval_error: evalError }] }));
    prepareIssueLifecycle(root, "acme/repo");
    expect(fs.readFileSync(path.join(root, "safe-output-issue-evaluations.jsonl"), "utf8")).toBe("");
    const summary = JSON.parse(fs.readFileSync(path.join(root, "safe-output-issue-summary.json"), "utf8"));
    expect(summary).toMatchObject({ partial, selected_run_count: 1, evaluated_run_count: evaluated, unavailable_run_count: unavailable, total_issue_deliveries: 0 });
    expect(summary.run_errors).toHaveLength(unavailable);
    if (evalError) expect(summary.run_errors[0]).toMatchObject({ run_id: 1, eval_error: evalError });
  });

  it("preserves valid rows when another effect in the same run has incomplete evidence", () => {
    writeRuns([{ run_id: 1 }]);
    exec.mockReturnValue(
      JSON.stringify({
        adoption: [
          {
            run_id: 1,
            coverage: "partial",
            eval_error: "another effect has no delivery clock",
            items: [
              { type: "create_issue", adoption_status: "accepted", adoption: { acceptance_kind: "explicit_human" } },
              { type: "create_issue", adoption_status: "accepted", adoption: { acceptance_kind: "automation", actor: { login: "stale[bot]", type: "Bot" } } },
            ],
          },
        ],
      })
    );
    prepareIssueLifecycle(root, "acme/repo");
    const summary = JSON.parse(fs.readFileSync(path.join(root, "safe-output-issue-summary.json"), "utf8"));
    expect(summary).toMatchObject({
      partial: true,
      partial_run_count: 1,
      evaluated_run_count: 1,
      unavailable_run_count: 0,
      total_issue_deliveries: 2,
      accepted_count: 2,
      human_adopted_count: 1,
      automation_adopted_count: 1,
      delivery_fallback_adopted_count: 0,
    });
    expect(fs.readFileSync(path.join(root, "safe-output-issue-evaluations.jsonl"), "utf8")).toContain('"adoption_status":"accepted"');
  });

  it.each([
    "invalid JSON",
    '{"run_id":1}',
    '{"adoption":[{"run_id":2,"items":[]}]}',
    '{"adoption":[{"run_id":1,"items":[null]}]}',
    '{"adoption":[{"run_id":1,"items":[{"type":"close_issue","adoption_status":"lifecycle_close"}]}]}',
    '{"adoption":[]}',
    '{"adoption":[{"run_id":1,"items":[]},{"run_id":2,"items":[]}]}',
    '{"adoption":[{"run_id":1,"items":[],"eval_error":true}]}',
    '{"adoption":[{"run_id":1,"eval_error":"missing manifest"}]}',
    '{"adoption":[{"run_id":1,"items":[{"type":"create_issue","adoption_status":"accepted"}],"eval_error":"missing manifest"}]}',
    '{"adoption":[null]}',
    '{"adoption":[{"run_id":1,"items":[]},{"run_id":1,"items":[]}]}',
  ])("rejects malformed CLI output: %s", output => {
    writeRuns([{ run_id: 1 }]);
    exec.mockReturnValue(output);
    expect(() => prepareIssueLifecycle(root, "acme/repo")).toThrow(/Invalid gh aw logs --adoption/);
    expect(fs.existsSync(path.join(root, "safe-output-issue-summary.json"))).toBe(false);
  });

  it("does not invoke the CLI for an empty source run list", () => {
    writeRuns([]);
    prepareIssueLifecycle(root, "acme/repo");
    expect(exec).not.toHaveBeenCalled();
    expect(fs.readFileSync(path.join(root, "safe-output-issue-evaluations.jsonl"), "utf8")).toBe("");
  });

  it("clamps the total batch timeout to the subprocess limit", () => {
    vi.stubEnv("GH_AW_ADOPTION_GH_TIMEOUT_MS", "2147483647");
    writeRuns([{ run_id: 1 }, { run_id: 2 }]);
    exec.mockReturnValue(
      JSON.stringify({
        adoption: [
          { run_id: 1, items: [] },
          { run_id: 2, items: [] },
        ],
      })
    );
    prepareIssueLifecycle(root, "acme/repo");
    expect(exec).toHaveBeenCalledWith("gh", expect.any(Array), expect.objectContaining({ timeout: 2147483647 }));
  });

  it("validates every source ID before issuing API requests", () => {
    writeRuns([{ run_id: 1 }, { run_id: -2 }]);
    expect(() => prepareIssueLifecycle(root, "acme/repo")).toThrow("invalid run ID");
    expect(exec).not.toHaveBeenCalled();
  });

  it("rejects missing or invalid inputs rather than publishing no delivery evidence", () => {
    expect(() => prepareIssueLifecycle(root, "")).toThrow("GITHUB_REPOSITORY is required");
    expect(() => prepareIssueLifecycle(root, "acme/repo")).toThrow("Failed to read impact report input");
    writeRuns([{ run_id: -1 }]);
    expect(() => prepareIssueLifecycle(root, "acme/repo")).toThrow("invalid run ID");
    expect(exec).not.toHaveBeenCalled();
  });
});
