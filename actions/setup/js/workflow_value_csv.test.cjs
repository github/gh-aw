import { afterEach, describe, expect, it } from "vitest";
import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { generateWorkflowValueReport } = require("./workflow_value_report.cjs");
const { generateWorkflowValueCsv } = require("./workflow_value_csv.cjs");
const { MAX_WORKFLOW_VALUE_CSV_FILE_BYTES, validateWorkflowValueCsvFiles } = require("./workflow_value_csv_format.cjs");
const { verifyWorkflowValueCsv } = require("./workflow_value_csv_verify.cjs");
const temporary = [];

function temp(prefix = "workflow-value-csv-") {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), prefix));
  temporary.push(root);
  return root;
}

function runGit(root, args, date) {
  const result = spawnSync("git", args, {
    cwd: root,
    encoding: "utf8",
    env: {
      ...process.env,
      GIT_AUTHOR_DATE: date || process.env.GIT_AUTHOR_DATE,
      GIT_COMMITTER_DATE: date || process.env.GIT_COMMITTER_DATE,
    },
  });
  if (result.status !== 0) throw new Error(result.stderr || result.stdout);
  return result.stdout.trim();
}

function workflowSource(id) {
  return `---
name: ${id.toUpperCase()} workflow
description: ${id} workflow description
---

Test workflow.
`;
}

function selectedRecord(delivered = 0, acceptedHuman = 0) {
  return {
    safe_outputs_source: "agentic_workflows_logs",
    safe_output_delivery: { coverage: "complete", delivered },
    safe_output_adoption: {
      coverage: "complete",
      accepted: acceptedHuman,
      accepted_human: acceptedHuman,
      accepted_delivery_fallback: 0,
      rejected: 0,
      not_applicable: 0,
      pending: Math.max(0, delivered - acceptedHuman),
      lifecycle: 0,
      unknown: 0,
    },
  };
}

function metrics(ids, timestamp, records = {}) {
  return {
    timestamp,
    period: "daily",
    collection_status: "complete",
    collection_window: {
      start: new Date(Date.parse(timestamp) - 24 * 60 * 60 * 1000).toISOString(),
      end: timestamp,
      coverage_hours: 24,
      logs_batches: 1,
    },
    workflows: Object.fromEntries(ids.map(id => [id, records[id] || selectedRecord()])),
    ecosystem: { total_workflows: ids.length },
  };
}

function retainedCounts(overrides = {}) {
  return {
    delivered: 0,
    accepted_human: 0,
    accepted_delivery_fallback: 0,
    rejected: 0,
    pending: 0,
    lifecycle: 0,
    not_applicable: 0,
    unknown: 0,
    survival_observed: 0,
    survival_reversed: 0,
    survival_unknown: 0,
    ...overrides,
  };
}

function lifecycle(observedAt, options = {}) {
  return {
    schema_version: 2,
    repository: "github/gh-aw",
    observed_at: observedAt,
    scope: "retained_deliveries",
    coverage: options.coverage || "complete",
    discovery_complete: options.coverage !== "partial",
    discovery_completed_until: observedAt,
    unavailable_runs: options.coverage === "partial" ? 1 : 0,
    refreshed: 0,
    deferred: 0,
    errors: [],
    totals: options.totals || retainedCounts(),
    workflows: options.workflows || {},
    ledger: {
      memory_id: "lifecycle",
      branch: "memory/output-lifecycle",
      path: "lifecycle.jsonl",
      schema_version: 7,
      sha256: crypto.createHash("sha256").update("").digest("hex"),
      bytes: 0,
      records: 0,
    },
  };
}

function writeJson(filename, value) {
  fs.mkdirSync(path.dirname(filename), { recursive: true });
  fs.writeFileSync(filename, `${JSON.stringify(value)}\n`);
}

function fixture() {
  const root = temp();
  const workflowsDir = path.join(root, ".github", "workflows");
  const metricsDir = path.join(root, "memory", "metrics");
  const reportDir = path.join(root, "report");
  const ledgerFile = path.join(root, "lifecycle", "lifecycle.jsonl");
  fs.mkdirSync(workflowsDir, { recursive: true });
  fs.mkdirSync(path.join(metricsDir, "daily"), { recursive: true });
  fs.mkdirSync(path.join(metricsDir, "lifecycle", "daily"), { recursive: true });
  fs.mkdirSync(path.dirname(ledgerFile), { recursive: true });
  fs.writeFileSync(ledgerFile, "");

  runGit(root, ["init", "--quiet"]);
  runGit(root, ["config", "user.name", "Workflow Value Test"]);
  runGit(root, ["config", "user.email", "workflow-value@example.com"]);
  runGit(root, ["config", "commit.gpgsign", "false"]);
  fs.writeFileSync(path.join(workflowsDir, "alpha.md"), workflowSource("alpha"));
  fs.writeFileSync(path.join(workflowsDir, "alpha.lock.yml"), "name: alpha\n");
  runGit(root, ["add", ".github/workflows/alpha.md", ".github/workflows/alpha.lock.yml"]);
  runGit(root, ["commit", "--quiet", "--no-gpg-sign", "-m", "Add alpha"], "2026-11-15T01:00:00Z");
  fs.writeFileSync(path.join(workflowsDir, "beta.md"), workflowSource("beta"));
  fs.writeFileSync(path.join(workflowsDir, "beta.lock.yml"), "name: beta\n");
  runGit(root, ["add", ".github/workflows/beta.md", ".github/workflows/beta.lock.yml"]);
  runGit(root, ["commit", "--quiet", "--no-gpg-sign", "-m", "Add beta"], "2026-11-17T12:00:00Z");
  fs.appendFileSync(path.join(workflowsDir, "alpha.md"), "\nRevised.\n");
  runGit(root, ["add", ".github/workflows/alpha.md"]);
  runGit(root, ["commit", "--quiet", "--no-gpg-sign", "-m", "Revise alpha"], "2026-11-18T00:00:00Z");

  writeJson(path.join(metricsDir, "daily", "2026-11-14.json"), { date: "2026-11-14", status: "no-data" });
  writeJson(
    path.join(metricsDir, "daily", "2026-11-16.json"),
    metrics(["alpha"], "2026-11-16T02:00:00Z", {
      alpha: {
        safe_outputs_source: "agentic_workflows_logs",
        safe_output_outcomes: { accepted: 99, rejected: 0, ignored: 0, pending: 0, lifecycle: 0, lifecycle_close: 0 },
        workflow_runs: { total: 1, successful: 1, failed: 0, executed: 1, success_rate: 1 },
      },
    })
  );
  writeJson(
    path.join(metricsDir, "lifecycle", "daily", "2026-11-16.json"),
    lifecycle("2026-11-16T03:00:00Z", {
      coverage: "partial",
      totals: retainedCounts({ delivered: 1, pending: 1, survival_unknown: 1 }),
      workflows: { ".github/workflows/alpha.lock.yml": retainedCounts({ delivered: 1, pending: 1, survival_unknown: 1 }) },
    })
  );
  const currentMetrics = metrics(["alpha", "beta"], "2026-11-18T02:00:00Z", {
    alpha: selectedRecord(2, 1),
    beta: selectedRecord(1, 0),
  });
  const currentLifecycle = lifecycle("2026-11-18T01:00:00Z");
  writeJson(path.join(metricsDir, "latest.json"), currentMetrics);
  writeJson(path.join(metricsDir, "daily", "2026-11-18.json"), currentMetrics);
  writeJson(path.join(metricsDir, "lifecycle.json"), currentLifecycle);
  writeJson(path.join(metricsDir, "lifecycle", "daily", "2026-11-18.json"), currentLifecycle);

  generateWorkflowValueReport({
    repoRoot: root,
    metricsDir,
    lifecycleLedgerFile: ledgerFile,
    outputDir: reportDir,
    repository: "github/gh-aw",
    prune: false,
  });
  return { root, reportDir };
}

afterEach(() => {
  for (const root of temporary.splice(0)) fs.rmSync(root, { recursive: true, force: true });
});

describe("workflow value normalized CSV", () => {
  it("reconstructs every workflow-period scope with full git provenance and explicit evidence gaps", () => {
    const f = fixture();
    const outputDir = path.join(f.reportDir, "csv");
    const result = generateWorkflowValueCsv({
      repoRoot: f.root,
      reportDir: f.reportDir,
      outputDir,
      provenanceRepoRoot: f.root,
    });

    expect(result.manifest.row_counts).toEqual({
      workflows: 2,
      contracts: 2,
      periods: 3,
      delivery_observations: 12,
      adoption_observations: 12,
      attributed_value_observations: 6,
    });
    expect(result.manifest.git_provenance).toMatchObject({ status: "complete", shallow: false });
    expect(result.tables.periods.find(row => row.period_date === "2026-11-16")).toMatchObject({
      session_evidence_source: "agentic_workflows_logs",
      session_reported_workflow_count: 1,
      session_delivery_workflow_count: 0,
      session_adoption_workflow_count: 0,
    });
    expect(result.tables.workflows.find(row => row.workflow_id === "alpha")).toMatchObject({
      name: "ALPHA workflow",
      git_history_status: "complete",
      workflow_introduced_at: "2026-11-15T01:00:00Z",
      workflow_revised_at: "2026-11-18T00:00:00Z",
    });
    expect(result.tables.adoption_observations.find(row => row.period_date === "2026-11-16" && row.workflow_id === "alpha" && row.observation_scope === "selected_window")).toMatchObject({
      workflow_applicability: "applicable",
      coverage: "unavailable",
      accepted: null,
      reason_code: "adoption_evidence_absent",
    });
    expect(result.tables.delivery_observations.find(row => row.period_date === "2026-11-16" && row.workflow_id === "beta" && row.observation_scope === "selected_window")).toMatchObject({
      workflow_applicability: "not_applicable",
      coverage: "unavailable",
      delivered: null,
      reason_code: "workflow_not_introduced",
    });
    expect(result.tables.attributed_value_observations.filter(row => row.period_role === "current").map(row => row.state)).toEqual(["needs_definition", "needs_definition"]);
    expect(result.tables.contracts.every(row => row.contract_basis === "recorded_agent_session_evidence_only" && row.attributed_value_native_unit === null)).toBe(true);
    expect(result.tables.contracts.every(row => row.session_evidence_source === "agentic_workflows_logs")).toBe(true);
    expect(result.tables.contracts.every(row => row.delivery_evidence_status === "available" && row.adoption_evidence_status === "available")).toBe(true);
    expect(result.tables.contracts[0].excluded_proxy_evidence).toContain("safe_output_outcomes");
    expect(verifyWorkflowValueCsv({ repoRoot: f.root, outputDir })).toMatchObject({
      workflow_count: 2,
      period_count: 3,
      row_counts: result.manifest.row_counts,
    });
  });

  it("marks shallow and missing git history instead of fabricating introduction dates", () => {
    const f = fixture();
    const shallow = temp("workflow-value-shallow-");
    fs.rmSync(shallow, { recursive: true, force: true });
    runGit(path.dirname(shallow), ["clone", "--quiet", "--depth=1", `file://${f.root}`, shallow]);
    const shallowResult = generateWorkflowValueCsv({
      repoRoot: f.root,
      reportDir: f.reportDir,
      outputDir: path.join(f.reportDir, "csv-shallow"),
      provenanceRepoRoot: shallow,
    });
    expect(shallowResult.manifest.git_provenance).toMatchObject({ status: "shallow", shallow: true });
    expect(shallowResult.tables.workflows.every(row => row.git_history_status === "shallow" && row.workflow_introduced_at === null)).toBe(true);

    const missingResult = generateWorkflowValueCsv({
      repoRoot: f.root,
      reportDir: f.reportDir,
      outputDir: path.join(f.reportDir, "csv-missing"),
      provenanceRepoRoot: temp("workflow-value-no-git-"),
    });
    expect(missingResult.manifest.git_provenance.status).toBe("missing");
    expect(missingResult.tables.workflows.every(row => row.git_history_status === "missing" && row.workflow_introduced_at === null)).toBe(true);
  });

  it("fails rather than dropping rows or accepting an incomplete dataset", () => {
    expect(() => validateWorkflowValueCsvFiles(new Map([["rows.csv", "x".repeat(MAX_WORKFLOW_VALUE_CSV_FILE_BYTES + 1)]]))).toThrow("without dropping rows");
    const f = fixture();
    const outputDir = path.join(f.reportDir, "csv");
    generateWorkflowValueCsv({ repoRoot: f.root, reportDir: f.reportDir, outputDir });
    fs.unlinkSync(path.join(outputDir, "adoption_observations.csv"));
    expect(() => verifyWorkflowValueCsv({ repoRoot: f.root, outputDir })).toThrow("artifact files differ from the schema");
  });
});
