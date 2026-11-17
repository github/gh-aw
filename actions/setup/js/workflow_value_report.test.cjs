import { afterEach, describe, expect, it } from "vitest";
import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const repoRoot = path.resolve(import.meta.dirname, "../../..");
const { generateWorkflowValueReport, loadWorkflowInventory, pruneUploadHistory, validateArtifactFiles } = require("./workflow_value_report.cjs");
const { verifyWorkflowValueReport } = require("./workflow_value_report_verify.cjs");
const temporary = [];

function temp() {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "workflow-value-report-"));
  temporary.push(root);
  return root;
}

function workflowSource(id, operationalValue = false) {
  return `---
name: ${id}
${
  operationalValue
    ? `graders:
  operational-value:
    name: Recorded effect
    description: Recorded effect
    unit: boolean
    direction: higher_is_better
    script: |
      return [{id: "recorded", value: 1}];
`
    : ""
}---

Test workflow.
`;
}

function fixture(ids = ["alpha", "beta"], operational = []) {
  const root = temp();
  const workflows = path.join(root, ".github", "workflows");
  const metricsDir = path.join(root, "memory", "metrics");
  const outputDir = path.join(root, "report");
  const ledgerFile = path.join(root, "lifecycle", "lifecycle.jsonl");
  fs.mkdirSync(workflows, { recursive: true });
  fs.mkdirSync(path.join(metricsDir, "daily"), { recursive: true });
  fs.mkdirSync(path.join(metricsDir, "lifecycle", "daily"), { recursive: true });
  fs.mkdirSync(path.dirname(ledgerFile), { recursive: true });
  for (const id of ids) {
    fs.writeFileSync(path.join(workflows, `${id}.md`), workflowSource(id, operational.includes(id)));
    fs.writeFileSync(path.join(workflows, `${id}.lock.yml`), `name: ${id}\n`);
  }
  fs.writeFileSync(ledgerFile, "");
  return { root, metricsDir, outputDir, ledgerFile, ids };
}

function selectedRecord(delivered = 0, acceptedHuman = 0, fallback = 0) {
  return {
    safe_outputs: { issues_created: 0, prs_created: 0, comments_added: 0, discussions_created: 0 },
    safe_outputs_by_type: {},
    safe_output_delivery: { delivered, coverage: "complete" },
    safe_output_adoption: {
      coverage: "complete",
      accepted: acceptedHuman + fallback,
      accepted_human: acceptedHuman,
      accepted_delivery_fallback: fallback,
      rejected: 0,
      not_applicable: 0,
      pending: Math.max(0, delivered - acceptedHuman - fallback),
      lifecycle: 0,
      unknown: 0,
    },
    workflow_runs: { total: 0, successful: 0, failed: 0, skipped: 0, action_required: 0, executed: 0, success_rate: null },
  };
}

function metrics(ids, options = {}) {
  const timestamp = options.timestamp || "2026-11-17T02:00:00Z";
  const records = Object.fromEntries(ids.map(id => [id, selectedRecord()]));
  for (const [id, value] of Object.entries(options.records || {})) records[id] = value;
  return {
    timestamp,
    period: "daily",
    collection_status: options.collectionStatus || "complete",
    collection_note: "",
    unclassified_conclusions: [],
    collection_window: {
      start: new Date(Date.parse(timestamp) - 24 * 60 * 60 * 1000).toISOString(),
      end: timestamp,
      coverage_hours: 24,
      logs_batches: 1,
    },
    workflows: records,
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

function lifecycle(options = {}) {
  return {
    schema_version: 2,
    repository: "github/gh-aw",
    observed_at: options.observedAt || "2026-11-17T01:00:00Z",
    scope: "retained_deliveries",
    coverage: options.coverage || "complete",
    discovery_complete: options.coverage !== "partial",
    discovery_completed_until: options.observedAt || "2026-11-17T01:00:00Z",
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
      sha256: options.ledger?.sha256 || crypto.createHash("sha256").update("").digest("hex"),
      bytes: options.ledger?.bytes || 0,
      records: options.ledger?.records || 0,
    },
  };
}

function writeJSON(filename, value) {
  fs.mkdirSync(path.dirname(filename), { recursive: true });
  fs.writeFileSync(filename, JSON.stringify(value) + "\n");
}

function writeCurrent(f, snapshot = metrics(f.ids), retained = lifecycle()) {
  const day = snapshot.timestamp.slice(0, 10);
  writeJSON(path.join(f.metricsDir, "latest.json"), snapshot);
  writeJSON(path.join(f.metricsDir, "daily", `${day}.json`), snapshot);
  writeJSON(path.join(f.metricsDir, "lifecycle.json"), retained);
  writeJSON(path.join(f.metricsDir, "lifecycle", "daily", `${retained.observed_at.slice(0, 10)}.json`), retained);
}

function run(f, options = {}) {
  return generateWorkflowValueReport({
    repoRoot: f.root,
    metricsDir: f.metricsDir,
    lifecycleLedgerFile: f.ledgerFile,
    outputDir: f.outputDir,
    repository: "github/gh-aw",
    stepSummaryPath: options.stepSummaryPath,
    prune: options.prune ?? false,
  });
}

function ledgerRecord(id, workflow, overrides = {}) {
  return {
    schema_version: 7,
    kind: "output_lifecycle",
    lifecycle: {
      id,
      host: "github.com",
      repository: "github/gh-aw",
      workflow,
      run_id: 42,
      run_attempt: 1,
      invocation_id: "invocation",
      evidence_id: "evidence",
      type: "create_issue",
      target_url: "https://github.com/github/gh-aw/issues/1",
      target_repository: "github/gh-aw",
      delivery: { state: "delivered", signal: "effect_applied", at: "2026-11-16T03:00:00Z", observed_at: "2026-11-16T03:01:00Z", source: "safe-output" },
      adoption: {
        state: "accepted",
        signal: "closed",
        at: "2026-11-16T05:00:00Z",
        observed_at: "2026-11-16T05:01:00Z",
        source: "github-api",
        rule_version: "gh-aw-adoption-v1",
        acceptance_kind: "explicit_human",
        human_verified: true,
        coverage: "available",
      },
      survival: { state: "observed", signal: "resource_still_accepted", observed_at: "2026-11-17T00:00:00Z", coverage: "snapshot" },
      ...overrides,
    },
  };
}

afterEach(() => {
  for (const root of temporary.splice(0)) fs.rmSync(root, { recursive: true, force: true });
});

describe("workflow value portfolio", () => {
  it("derives the complete repository inventory without a hard-coded workflow count", () => {
    const inventory = loadWorkflowInventory(repoRoot);
    const directory = path.join(repoRoot, ".github", "workflows");
    const sources = fs.readdirSync(directory).filter(name => name.endsWith(".md"));
    const locks = fs.readdirSync(directory).filter(name => name.endsWith(".lock.yml"));
    expect(inventory.count).toBe(sources.length);
    expect(inventory.count).toBe(locks.length);
    expect(inventory.workflows.map(workflow => workflow.id)).toEqual([...inventory.workflows.map(workflow => workflow.id)].sort());
    expect(inventory.workflows.filter(workflow => workflow.operational_value.state === "declared_run_scoped")).toEqual([]);
  });

  it("covers every workflow while keeping legacy outcomes and run success out of attributed value", () => {
    const f = fixture();
    const legacy = metrics(f.ids, {
      timestamp: "2026-11-16T02:00:00Z",
      records: Object.fromEntries(
        f.ids.map(id => [
          id,
          {
            safe_output_outcomes: { accepted: 99, rejected: 0, ignored: 0, pending: 0, lifecycle: 0, lifecycle_close: 0 },
            workflow_runs: { total: 1, successful: 1, failed: 0, skipped: 0, action_required: 0, executed: 1, success_rate: 1 },
          },
        ])
      ),
    });
    writeJSON(path.join(f.metricsDir, "daily", "2026-11-16.json"), legacy);
    writeJSON(path.join(f.metricsDir, "daily", "2026-01-06.json"), { date: "2026-01-06", status: "no-data" });
    writeCurrent(f);
    const summary = path.join(f.root, "step-summary.md");
    const result = run(f, { stepSummaryPath: summary });
    expect(result.portfolio.inventory.count).toBe(2);
    expect(result.portfolio.workflows).toHaveLength(2);
    expect(result.portfolio.periods.find(period => period.date === "2026-01-06")).toMatchObject({
      collection_status: "legacy_unavailable",
      selected_window: { aggregate: { coverage: "unavailable", delivered: null, accepted: null } },
    });
    expect(result.portfolio.periods.find(period => period.date === "2026-11-16")).toMatchObject({
      selected_window: { aggregate: { coverage: "unavailable", delivered: null, accepted: null } },
      attributed_value: { status_coverage: "unavailable", measurement_coverage: "unavailable" },
    });
    for (const workflow of result.portfolio.workflows) {
      expect(workflow.current.attributed_value).toEqual({
        state: "needs_definition",
        reason_code: "reviewed_longitudinal_definition_missing",
        unit: null,
        value: null,
      });
    }
    expect(fs.readFileSync(path.join(f.outputDir, "workflow-value-report.md"), "utf8")).toContain("Legacy `safe_output_outcomes`");
    const portfolio = JSON.parse(fs.readFileSync(path.join(f.outputDir, "workflow-value-portfolio.json"), "utf8"));
    const history = JSON.parse(fs.readFileSync(path.join(f.outputDir, "workflow-value-history.json"), "utf8"));
    expect(portfolio.workflows).toHaveLength(2);
    expect(portfolio.history).toMatchObject({ path: "workflow-value-history.json", period_count: 3 });
    expect(history.periods).toHaveLength(3);
    expect(history.reconstruction.workflow_history).toContain("layer.overrides");
    expect(fs.readFileSync(summary, "utf8")).not.toContain("(plots/");
    expect(fs.readFileSync(summary, "utf8")).toContain("see the `workflow-value-report` artifact");
    expect(verifyWorkflowValueReport({ repoRoot: f.root, metricsDir: f.metricsDir, outputDir: f.outputDir })).toMatchObject({
      workflow_count: 2,
      period_count: 3,
    });
    expect(fs.readdirSync(path.join(f.outputDir, "plots")).sort()).toEqual(["inventory-coverage.svg", "retained-lifecycle.svg", "selected-lifecycle.svg", "value-evidence.svg"]);
  });

  it("fails when current metrics omit or add a repository workflow", () => {
    const f = fixture();
    writeCurrent(f, metrics(["alpha"]));
    expect(() => run(f)).toThrow("missing: beta");
    writeCurrent(f, metrics(["alpha", "beta", "extra"]));
    expect(() => run(f)).toThrow("extra: extra");
  });

  it("rejects missing pairs and duplicate case-insensitive workflow identities", () => {
    const missing = fixture(["alpha"]);
    fs.unlinkSync(path.join(missing.root, ".github/workflows/alpha.lock.yml"));
    expect(() => loadWorkflowInventory(missing.root)).toThrow("missing locks: alpha");

    const duplicate = fixture(["alpha", "Alpha"]);
    expect(() => loadWorkflowInventory(duplicate.root)).toThrow("Duplicate case-insensitive workflow identity");
  });

  it("selects the latest same-day metrics observation before applying the canonical current ledger cohort", () => {
    const f = fixture();
    const older = metrics(f.ids, {
      timestamp: "2026-11-17T01:00:00Z",
      records: { alpha: selectedRecord(1, 0, 1), beta: selectedRecord() },
    });
    const latest = metrics(f.ids, {
      timestamp: "2026-11-17T02:00:00Z",
      records: { alpha: selectedRecord(3, 2, 1), beta: selectedRecord() },
    });
    writeJSON(path.join(f.metricsDir, "daily", "2026-11-17.json"), older);
    writeJSON(path.join(f.metricsDir, "latest.json"), latest);
    const retained = lifecycle();
    writeJSON(path.join(f.metricsDir, "lifecycle.json"), retained);
    writeJSON(path.join(f.metricsDir, "lifecycle/daily/2026-11-17.json"), retained);
    const result = run(f);
    expect(result.portfolio.periods.find(period => period.date === "2026-11-17")).toMatchObject({
      timestamp: "2026-11-17T02:00:00Z",
      metrics_source: "metrics/latest.json",
      selected_window_source: "canonical_retained_lifecycle_ledger",
      selected_window: { aggregate: { coverage: "complete", delivered: 0 } },
    });
  });

  it("keeps absent retained workflows unknown under partial coverage and zero only under complete coverage", () => {
    const partial = fixture();
    writeCurrent(
      partial,
      metrics(partial.ids),
      lifecycle({
        coverage: "partial",
        totals: retainedCounts({ delivered: 1, pending: 1, survival_unknown: 1 }),
        workflows: { ".github/workflows/alpha.lock.yml": retainedCounts({ delivered: 1, pending: 1, survival_unknown: 1 }) },
      })
    );
    let result = run(partial);
    expect(result.portfolio.workflows.find(workflow => workflow.id === "alpha").current.retained_population).toMatchObject({ coverage: "partial", delivered: 1 });
    expect(result.portfolio.workflows.find(workflow => workflow.id === "beta").current.retained_population).toMatchObject({ coverage: "unavailable", delivered: null });

    const complete = fixture();
    writeCurrent(complete);
    result = run(complete);
    expect(result.portfolio.workflows.find(workflow => workflow.id === "beta").current.retained_population).toMatchObject({ coverage: "complete", delivered: 0 });
  });

  it("preserves canonical ledger identity and clocks by digest and workflow selector", () => {
    const f = fixture();
    const ledger = JSON.stringify(ledgerRecord("delivery-sha256-test", ".github/workflows/alpha.lock.yml")) + "\n";
    writeCurrent(
      f,
      metrics(f.ids),
      lifecycle({
        totals: retainedCounts({ delivered: 1, accepted_human: 1, survival_observed: 1 }),
        workflows: { ".github/workflows/alpha.lock.yml": retainedCounts({ delivered: 1, accepted_human: 1, survival_observed: 1 }) },
        ledger: { sha256: crypto.createHash("sha256").update(ledger).digest("hex"), bytes: Buffer.byteLength(ledger), records: 1 },
      })
    );
    fs.writeFileSync(f.ledgerFile, ledger);
    const result = run(f);
    expect(result.portfolio.sources.retained_lifecycle.ledger).toMatchObject({
      records: 1,
      identity_fields: expect.arrayContaining(["run_id", "run_attempt", "evidence_id"]),
      clock_fields: expect.arrayContaining(["delivery.at", "adoption.at", "survival.observed_at"]),
    });
    expect(result.portfolio.workflows.find(workflow => workflow.id === "alpha").lifecycle_evidence).toMatchObject({
      ledger_selector: { workflow: ".github/workflows/alpha.lock.yml" },
      delivery_identities: 1,
      producer_runs: 1,
      source_repositories: ["github/gh-aw"],
      target_repositories: ["github/gh-aw"],
      clocks: {
        first_delivery_at: "2026-11-16T03:00:00.000Z",
        last_decision_at: "2026-11-16T05:00:00.000Z",
        last_survival_observed_at: "2026-11-17T00:00:00.000Z",
      },
    });
    expect(result.portfolio.current.selected_window.aggregate).toMatchObject({ coverage: "complete", delivered: 1, accepted_human: 1 });
  });

  it("rejects a lifecycle projection that does not match the restored canonical ledger", () => {
    const f = fixture();
    writeCurrent(f);
    fs.writeFileSync(f.ledgerFile, JSON.stringify(ledgerRecord("delivery-sha256-test", ".github/workflows/alpha.lock.yml")) + "\n");
    expect(() => run(f)).toThrow("does not reference the restored canonical ledger");
  });

  it("keeps run-scoped operational value separate from longitudinal attributed value", () => {
    const f = fixture(["alpha"], ["alpha"]);
    writeCurrent(f);
    const result = run(f);
    expect(result.portfolio.workflows[0].operational_value).toMatchObject({ state: "declared_run_scoped", scope: "run_scoped_optional" });
    expect(result.portfolio.workflows[0].current.attributed_value).toMatchObject({ state: "needs_definition", value: null });
  });

  it("renders full retained history before deterministically pruning the upload window", () => {
    const f = fixture();
    writeCurrent(f);
    writeJSON(path.join(f.metricsDir, "daily", "2026-09-01.json"), { date: "2026-09-01", status: "no-data" });
    const result = run(f, { prune: true });
    expect(result.portfolio.periods.map(period => period.date)).toContain("2026-09-01");
    expect(fs.existsSync(path.join(f.metricsDir, "daily", "2026-09-01.json"))).toBe(false);
    expect(result.retention).toMatchObject({ retained_days: 30, removed_files: 1 });
  });

  it("fails explicitly instead of truncating an oversized complete artifact", () => {
    expect(() => validateArtifactFiles(new Map([["portfolio.json", "x".repeat(512 * 1024 + 1)]]))).toThrow("without truncation");
  });

  it("fails verification when a generated plot or machine output is missing", () => {
    const f = fixture();
    writeCurrent(f);
    run(f);
    fs.unlinkSync(path.join(f.outputDir, "plots/value-evidence.svg"));
    expect(() => verifyWorkflowValueReport({ repoRoot: f.root, metricsDir: f.metricsDir, outputDir: f.outputDir })).toThrow("Missing workflow value artifact file");
  });

  it("prunes both selected-window and lifecycle uploads by date without using mtimes", () => {
    const f = fixture();
    for (const directory of [path.join(f.metricsDir, "daily"), path.join(f.metricsDir, "lifecycle", "daily")]) {
      writeJSON(path.join(directory, "2026-10-01.json"), {});
      writeJSON(path.join(directory, "2026-11-17.json"), {});
    }
    expect(pruneUploadHistory(f.metricsDir, "2026-11-17")).toMatchObject({ oldest_day: "2026-10-19", removed_files: 2 });
    expect(fs.existsSync(path.join(f.metricsDir, "daily", "2026-11-17.json"))).toBe(true);
    expect(fs.existsSync(path.join(f.metricsDir, "lifecycle/daily/2026-11-17.json"))).toBe(true);
  });
});
