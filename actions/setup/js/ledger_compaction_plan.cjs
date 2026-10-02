// @ts-check
/// <reference types="@actions/github-script" />
"use strict";

// Untrusted Agentic Maintenance planning step for one ledger. It runs with read-only repository
// access, uses built-in segment selection, and writes only a compaction plan
// artifact. It never modifies the ledger branch.

const fs = require("node:fs");
const path = require("node:path");
const { fetchLedgerBranch } = require("./create_ledger_projection.cjs");
const { execGitSync } = require("./git_helpers.cjs");
const { canonicalJSON } = require("./ledger_store.cjs");
const { createPlan, isCompactionDue, loadSegments, materializeSnapshot, parseCompactionConfig, parseTrigger, readState, selectSources } = require("./ledger_compaction.cjs");

/**
 * @param {{config?: string, trigger?: string, planFile?: string, githubClient?: any, owner?: string, repo?: string, serverHost?: string, token?: string, now?: Date}} [options]
 */
async function main(options = {}) {
  const config = parseCompactionConfig(options.config);
  const trigger = parseTrigger(options.trigger);
  const planFile = options.planFile || process.env.GH_AW_LEDGER_COMPACTION_PLAN_FILE;
  if (!planFile) throw new TypeError("Missing ledger compaction plan path");
  const owner = options.owner || context.repo.owner;
  const repo = options.repo || context.repo.repo;
  const serverHost = options.serverHost || new URL(process.env.GITHUB_SERVER_URL || "https://github.com").host;
  const token = options.token || process.env.GH_TOKEN;
  const scratch = fs.mkdtempSync(path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw-ledger-compaction-plan-"));
  /** @type {Record<string, string | number>} */
  const summary = { ledger: config.name, trigger, schedule: config.schedule, result: "skipped" };
  core.setOutput("plan_created", "false");
  try {
    const workspaceDir = path.join(scratch, "repo");
    const sourceDir = path.join(scratch, "source");
    execGitSync(["init", "-q", workspaceDir], { stdio: "pipe" });
    const refName = `refs/gh-aw/ledger-compaction/${config.name}`;
    const exists = await fetchLedgerBranch({ githubClient: options.githubClient || github, owner, repo, branchName: config.branch, refName, workspaceDir, serverHost, token });
    if (!exists) {
      summary.reason = `ledger branch ${config.branch} does not exist`;
      return { status: "skipped", summary };
    }
    const baseCommit = execGitSync(["rev-parse", refName], { cwd: workspaceDir, stdio: "pipe" }).trim();
    const snapshot = materializeSnapshot({ workspaceDir, refName, sourceDir, config });
    const due = isCompactionDue({ config, trigger, state: readState(sourceDir), now: options.now });
    if (!due.due) {
      summary.reason = due.reason;
      return { status: "skipped", summary };
    }
    const loaded = loadSegments(sourceDir, config);
    summary.segments = loaded.segments.size;
    summary.invalid_segments = loaded.invalid;
    summary.oversized_segments = snapshot.skippedOversized;
    const selection = selectSources(loaded, config);
    if (!selection.sources) {
      summary.reason = selection.reason;
      return { status: "skipped", summary };
    }
    const plan = createPlan({ loaded, sources: selection.sources, config, trigger, baseCommit, now: options.now });
    if (!plan) {
      summary.reason = "selected segments are already compacted";
      return { status: "skipped", summary };
    }
    fs.mkdirSync(path.dirname(planFile), { recursive: true });
    fs.writeFileSync(planFile, `${canonicalJSON(plan)}\n`, { mode: 0o600 });
    Object.assign(summary, {
      result: "planned",
      plan_id: plan.plan_id,
      source_segments: plan.sources.length,
      source_records: plan.sources.reduce((sum, source) => sum + source.records.length, 0),
      replacement_records: plan.replacement.records.length,
      bytes_before: plan.sources.reduce((sum, source) => sum + source.bytes, 0),
      bytes_after: plan.replacement.bytes,
    });
    core.setOutput("plan_created", "true");
    core.setOutput("plan_id", plan.plan_id);
    return { status: "planned", plan, summary };
  } catch (error) {
    summary.result = "failed";
    summary.reason = error instanceof Error ? error.message : "planning failed";
    throw error;
  } finally {
    fs.rmSync(scratch, { recursive: true, force: true });
    await writeSummary("Ledger compaction plan", summary);
  }
}

/**
 * Write non-sensitive transaction metadata to the step summary and log. Ledger payloads are never logged.
 * @param {string} title
 * @param {Record<string, string | number>} summary
 */
async function writeSummary(title, summary) {
  const entries = Object.entries(summary);
  core.info(`${title}: ${entries.map(([key, value]) => `${key}=${value}`).join(" ")}`);
  if (core.summary && typeof core.summary.addRaw === "function" && typeof core.summary.write === "function") {
    const rows = entries.map(([key, value]) => `| ${key} | ${String(value).replace(/[|\r\n]/g, " ")} |`);
    await core.summary.addRaw([`### ${title}`, "", "| Field | Value |", "| --- | --- |", ...rows, ""].join("\n")).write();
  }
}

if (require.main === module) {
  main().catch(error => {
    core.setFailed(error instanceof Error ? error.message : "Ledger compaction planning failed");
  });
}

module.exports = { main, writeSummary };
