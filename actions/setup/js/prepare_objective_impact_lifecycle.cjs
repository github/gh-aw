// @ts-check

const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const { getSetupTimeoutMs, MAX_SETUP_TIMEOUT_MS } = require("./child_process_timeouts.cjs");
const { ERR_API, ERR_CONFIG, ERR_PARSE, ERR_SYSTEM, ERR_VALIDATION } = require("./error_codes.cjs");

/** @param {string} filePath */
function readJSON(filePath) {
  let content;
  try {
    content = fs.readFileSync(filePath, "utf8");
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Failed to read impact report input ${filePath}`, { cause: error });
  }
  try {
    return JSON.parse(content);
  } catch (error) {
    throw new Error(`${ERR_PARSE}: Invalid impact report JSON in ${filePath}`, { cause: error });
  }
}

/** @param {string} filePath @param {string} content */
function writeFileAtomic(filePath, content) {
  const temporary = `${filePath}.tmp`;
  try {
    fs.writeFileSync(temporary, content);
    fs.renameSync(temporary, filePath);
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Failed to write impact report output ${filePath}`, { cause: error });
  }
}

/** @param {string} repo @param {string} runsDir @param {number[]} ids */
function collectLifecycleReports(repo, runsDir, ids) {
  const reports = new Map();
  if (ids.length === 0) return reports;
  const timeout = Math.min(MAX_SETUP_TIMEOUT_MS, getSetupTimeoutMs("adoptionGh") * ids.length);
  let raw;
  try {
    raw = execFileSync("gh", ["aw", "logs", "--stdin", "--adoption", "--repo", repo, "--output", runsDir, "--timeout", String(Math.ceil(timeout / 60_000)), "--json"], {
      input: ids.join("\n") + "\n",
      encoding: "utf8",
      stdio: ["pipe", "pipe", "inherit"],
      timeout,
      maxBuffer: 64 * 1024 * 1024,
    });
  } catch (error) {
    throw new Error(`${ERR_API}: Failed to evaluate adoption for selected runs`, { cause: error });
  }
  let logs;
  try {
    logs = JSON.parse(raw);
  } catch (error) {
    throw new Error(`${ERR_PARSE}: Invalid gh aw logs --adoption JSON`, { cause: error });
  }
  if (!Array.isArray(logs?.adoption)) throw new Error(`${ERR_VALIDATION}: Invalid gh aw logs --adoption report`);
  for (const report of logs.adoption) {
    if (!Number.isSafeInteger(report?.run_id) || report.run_id <= 0 || reports.has(report.run_id)) throw new Error(`${ERR_VALIDATION}: Invalid gh aw logs --adoption report`);
    reports.set(report.run_id, report);
  }
  return reports;
}

/** @param {string} dataDir @param {string} repo */
function prepareIssueLifecycle(dataDir, repo) {
  if (!repo) throw new Error(`${ERR_CONFIG}: EXPR_GITHUB_REPOSITORY or GITHUB_REPOSITORY is required`);
  const workflowLogs = readJSON(path.join(dataDir, "workflow-logs.json"));
  if (!workflowLogs || !Array.isArray(workflowLogs.runs)) throw new Error(`${ERR_VALIDATION}: Impact report input must contain a runs array`);
  const runs = new Map();
  for (const run of workflowLogs.runs) {
    const id = Number(run?.run_id ?? run?.id ?? run?.databaseId);
    if (!Number.isSafeInteger(id) || id <= 0) throw new Error(`${ERR_VALIDATION}: Impact report contains an invalid run ID`);
    if (!runs.has(id)) runs.set(id, run);
  }
  const reports = collectLifecycleReports(repo, path.join(dataDir, "safe-output-runs"), [...runs.keys()]);
  if (reports.size !== runs.size || [...reports.keys()].some(id => !runs.has(id))) throw new Error(`${ERR_VALIDATION}: Invalid gh aw logs --adoption report: selected runs do not match`);
  const rows = [];
  const runErrors = [];
  for (const [id, run] of runs) {
    const report = reports.get(id);
    if (
      !report ||
      report.run_id !== id ||
      (report.eval_error !== undefined && typeof report.eval_error !== "string") ||
      !Array.isArray(report.items) ||
      report.items.some(item => !item || typeof item.type !== "string" || !["accepted", "rejected", "pending", "lifecycle", "not_applicable", "unknown"].includes(item.adoption_status)) ||
      (report.eval_error && report.items.length > 0 && report.coverage !== "partial")
    ) {
      throw new Error(`${ERR_VALIDATION}: Invalid gh aw logs --adoption report for run ${id}`);
    }
    if (report.eval_error) {
      runErrors.push({
        run_id: id,
        workflow_name: report.workflow || run.workflow_name || run.workflowName || "",
        workflow_run_url: run.html_url || run.url || "",
        eval_error: report.eval_error,
      });
    }
    for (const item of report.items) {
      if (item.type !== "create_issue" && item.type !== "close_issue") continue;
      rows.push({
        ...item,
        run_id: id,
        workflow_name: report.workflow || run.workflow_name || run.workflowName || "",
        workflow_aic: run.aic ?? null,
        workflow_run_created_at: run.created_at || run.createdAt || "",
        workflow_run_url: run.html_url || run.url || "",
        number: item.object_number ?? null,
        url: item.object_url || "",
        timestamp: item.created_at || "",
      });
    }
  }

  const summary = {
    partial: runErrors.length > 0 || [...reports.values()].some(report => report.coverage === "partial"),
    selected_run_count: runs.size,
    evaluated_run_count: [...reports.values()].filter(report => !report.eval_error || report.items.length > 0).length,
    unavailable_run_count: [...reports.values()].filter(report => report.eval_error && report.items.length === 0).length,
    partial_run_count: [...reports.values()].filter(report => report.coverage === "partial").length,
    run_errors: runErrors,
    total_issue_deliveries: rows.length,
    create_issue_count: rows.filter(row => row.type === "create_issue").length,
    close_issue_count: rows.filter(row => row.type === "close_issue").length,
    accepted_count: rows.filter(row => row.adoption_status === "accepted").length,
    human_adopted_count: rows.filter(row => row.adoption_status === "accepted" && row.adoption?.acceptance_kind === "explicit_human").length,
    automation_adopted_count: rows.filter(row => row.adoption_status === "accepted" && row.adoption?.acceptance_kind === "automation").length,
    delivery_fallback_adopted_count: rows.filter(row => row.adoption_status === "accepted" && row.adoption?.acceptance_kind === "fallback_delivery").length,
    rejected_count: rows.filter(row => row.adoption_status === "rejected").length,
    pending_count: rows.filter(row => row.adoption_status === "pending").length,
    not_applicable_count: rows.filter(row => row.adoption_status === "not_applicable").length,
    unknown_count: rows.filter(row => row.adoption_status === "unknown").length,
    errors_count: rows.filter(row => row.eval_error).length,
    lifecycle_count: rows.filter(row => row.adoption_status === "lifecycle").length,
    distinct_workflows: new Set(rows.map(row => row.workflow_name).filter(Boolean)).size,
    distinct_runs_with_issue_deliveries: new Set(rows.map(row => row.run_id)).size,
  };
  writeFileAtomic(path.join(dataDir, "safe-output-issue-evaluations.jsonl"), rows.map(row => JSON.stringify(row)).join("\n") + (rows.length ? "\n" : ""));
  writeFileAtomic(path.join(dataDir, "safe-output-issue-summary.json"), JSON.stringify(summary, null, 2) + "\n");
}

if (require.main === module) {
  prepareIssueLifecycle("/tmp/gh-aw/agent/objective-impact-report", process.env.EXPR_GITHUB_REPOSITORY || process.env.GITHUB_REPOSITORY || "");
}

module.exports = { prepareIssueLifecycle };
