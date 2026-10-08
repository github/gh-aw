"use strict";

const fs = require("node:fs");
const path = require("node:path");

const PROFILES = [
  { name: "history", label: "Retained history" },
  { name: "lifecycle", label: "Worker lifecycles" },
  { name: "saturation", label: "Admission saturation" },
];
const OUTCOMES = { success: "Passed", failure: "Failed", cancelled: "Cancelled", skipped: "Not run" };
const measured = value => typeof value === "number" && Number.isFinite(value) && value >= 0;
const number = (value, digits = 0) => (measured(value) ? value.toLocaleString("en-US", { minimumFractionDigits: digits, maximumFractionDigits: digits }) : "Not measured");
const duration = value => (measured(value) ? (value < 1000 ? `${number(value, 1)} ms` : `${number(value / 1000, 2)} s`) : "Not measured");

function bytes(value) {
  if (!measured(value)) return "Not measured";
  const units = ["B", "KiB", "MiB", "GiB"];
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${number(value, unit ? 2 : 0)} ${units[unit]}`;
}

function text(value) {
  return String(value)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/\|/g, "&#124;")
    .replace(/([\\`*_[\]])/g, "\\$1")
    .replace(/\r?\n/g, "<br>");
}

function readProfile(directory, profile, outcome) {
  const entry = { ...profile, status: OUTCOMES[outcome], report: null, diagnostic: null };
  if (outcome === "skipped") return entry;
  try {
    if (outcome && !Object.hasOwn(OUTCOMES, outcome)) throw new Error(`Unknown step outcome: ${outcome}`);
    const report = JSON.parse(fs.readFileSync(path.join(directory, `${profile.name}.json`), "utf8"));
    if (!report || typeof report.ok !== "boolean") throw new Error("Report must contain a boolean ok status");
    if (report.ok && report.mode !== profile.name) throw new Error(`Expected ${profile.name} metrics, received ${report.mode}`);
    entry.report = report;
    entry.status ||= report.ok ? "Passed" : "Failed";
    if (!report.ok) entry.status = "Failed";
    if (report.error) entry.diagnostic = report.error;
  } catch (error) {
    entry.status = error.code === "ENOENT" && !outcome ? "No report" : entry.status === "Cancelled" ? "Cancelled" : "Failed";
    entry.diagnostic = `Cannot read ${profile.name}.json: ${error.message}`;
  }
  return entry;
}

function workload(entry) {
  const report = entry.report;
  if (!report) return "Not measured";
  if (entry.name === "history") return `${number(report.retained_history_entities)} retained entities (${number(report.seeded_cancelled_work)} Work + ${number(report.seeded_claims)} Claims)`;
  if (entry.name === "lifecycle") return `${number(report.completed)} completed / ${number(report.requested)} requested Work`;
  return `${number(report.submitted)} admitted / ${number(report.requested)} requested Work`;
}

function throughput(entry) {
  const report = entry.report;
  if (!report?.ok || entry.status !== "Passed") return "Not measured";
  if (entry.name === "saturation") return "Not a completion benchmark";
  const count = entry.name === "history" ? report.retained_history_entities : report.completed;
  if (!measured(count) || !measured(report.elapsed_ms) || report.elapsed_ms === 0) return "Not measured";
  return `${number(count / (report.elapsed_ms / 1000), 2)} ${entry.name === "history" ? "entities" : "Work"}/s`;
}

function renderSummary(entries, invariantsOutcome) {
  const passed = entries.filter(entry => entry.status === "Passed").length;
  const failed = entries.filter(entry => ["Failed", "Cancelled"].includes(entry.status)).length;
  const lines = [
    "### Work queue stress",
    "",
    `Profiles: **${passed} passed, ${failed} failed or cancelled, ${entries.length - passed - failed} unavailable**. Simulator invariants: **${OUTCOMES[invariantsOutcome] || "Not reported"}**.`,
    "",
    "| Profile | Status | Measured workload | Elapsed | Throughput | Peak RSS |",
    "| --- | --- | --- | --- | --- | --- |",
    ...entries.map(
      entry => `| ${entry.label} | ${entry.status}${entry.report?.saturation ? " (admission limit reached)" : ""} | ${workload(entry)} | ${duration(entry.report?.elapsed_ms)} | ${throughput(entry)} | ${bytes(entry.report?.max_rss_bytes)} |`
    ),
    "",
    "History throughput measures retained Work + Claim fixture validation, including setup and separate live smoke; it is **not worker completion throughput**. Lifecycle throughput includes Git publication, scheduling, simulated Actions, Result verification and cleanup across bounded branches. Peak RSS includes Node worker threads, excludes Git subprocesses, and is a process-lifetime high-water mark.",
    "",
    "Full JSON metrics, invariant output and this report are in the `work-queue-stress-results` artifact.",
    "",
    "<details>",
    "<summary>Phase timings, Git contention and workload scope</summary>",
    "",
    "| Metric | Retained history | Worker lifecycles | Admission saturation |",
    "| --- | --- | --- | --- |",
  ];
  const sumTiming = (report, key) => {
    if (!Array.isArray(report?.queues) || !report.queues.length || !report.queues.every(queue => measured(queue[key]))) return undefined;
    return report.queues.reduce((sum, queue) => sum + queue[key], 0);
  };
  const largestLedger = report => {
    const maximum = report?.max_queue_ledger_bytes ?? report?.ledger_bytes;
    if (measured(maximum)) return maximum;
    if (!Array.isArray(report?.queues) || !report.queues.length || !report.queues.every(queue => measured(queue.ledger_bytes))) return undefined;
    return report.queues.reduce((largest, queue) => Math.max(largest, queue.ledger_bytes), 0);
  };
  const metrics = [
    ["Workers", report => number(report?.workers)],
    ["Git branches (including history smoke)", report => number(report?.branch_count)],
    ["Ledger bytes across branches", report => bytes(report?.ledger_bytes_total)],
    ["Largest branch ledger", report => bytes(largestLedger(report))],
    ["Cold Git read and replay (sum)", report => duration(report?.cold_git_read_replay_ms ?? sumTiming(report, "cold_replay_ms"))],
    ["Selection (sum)", report => duration(sumTiming(report, "selection_ms"))],
    ["Fixture generation", report => duration(report?.fixture_generation_ms)],
    ["Fixture validation", report => duration(report?.fixture_validation_ms)],
    ["Batch planning", report => (measured(report?.ten_batch_plans_ms) ? `${duration(report.ten_batch_plans_ms)} / ${number(report.batch_plan_count ?? 10)} plans` : "Not measured")],
    ["Canonicalization", report => duration(report?.canonicalization_ms)],
    ["Recovery reserve used", report => bytes(report?.recovery_bytes_used)],
    ["Git commands", report => number(report?.git_api_metrics?.git_commands)],
    ["Simulated API calls", report => number(report?.git_api_metrics?.api_calls)],
    ["CAS conflicts", report => number(report?.git_api_metrics?.cas_conflicts)],
    ["Lost successful responses injected", report => number(report?.git_api_metrics?.lost_success_responses)],
    ["Recovered publications", report => number(report?.publisher_metrics?.recovered_publications)],
    ["Publisher retry sleeps", report => number(report?.publisher_metrics?.retry_sleeps)],
    ["Simulated native POSTs", report => number(report?.git_api_metrics?.native_posts)],
    ["Verified Results", report => number(report?.verified_results)],
  ];
  for (const [label, metric] of metrics) lines.push(`| ${label} | ${entries.map(entry => metric(entry.report)).join(" | ")} |`);
  for (const entry of entries) {
    lines.push("", `### ${entry.label}`, "");
    if (entry.diagnostic) lines.push(`**Diagnostic:** ${text(entry.diagnostic)}`, "");
    const report = entry.report;
    if (!report) continue;
    lines.push(`Seed: ${number(report.seed)}. Requested: ${number(report.requested)}. Scope: ${text(report.scope || "Not reported")}.`, "");
    if (report.counts_scope) lines.push(`**Partial counts:** ${text(report.counts_scope)}.`, "");
    if (report.saturation) {
      const boundary = report.saturation;
      lines.push(`Admission boundary: ${text(boundary.code)}. ${text(boundary.message)}`, "");
      if (entry.name === "saturation")
        lines.push(`Attempted: ${number(boundary.attempted)} Work; refused batch: ${number(boundary.refused_batch)} Work; not attempted: ${number(boundary.not_attempted)} Work. No completions are inferred from admission.`, "");
    }
    if (report.lifecycle_smoke) {
      const smoke = report.lifecycle_smoke;
      lines.push(
        `Separate live smoke: ${number(smoke.completed)} completed Work in ${duration(smoke.elapsed_ms)}, ${number(smoke.git_api_metrics?.cas_conflicts)} CAS conflicts and ${number(smoke.git_api_metrics?.lost_success_responses)} lost-response faults. These are excluded from the history fixture's Git counters.`,
        ""
      );
    }
    if (report.history_worker_verifications?.length) {
      lines.push("| History worker | Cold replay | 10 batch plans | Canonicalization | Total |", "| --- | --- | --- | --- | --- |");
      for (const worker of report.history_worker_verifications)
        lines.push(`| ${number(worker.worker)} | ${duration(worker.cold_replay_ms)} | ${duration(worker.ten_batch_plans_ms)} | ${duration(worker.canonicalization_ms)} | ${duration(worker.elapsed_ms)} |`);
      lines.push("");
    }
  }
  lines.push("</details>", "");
  return lines.join("\n");
}

function writeSummary({ directory = "stress-results", env = process.env } = {}) {
  const entries = PROFILES.map(profile => readProfile(directory, profile, env[`${profile.name.toUpperCase()}_OUTCOME`]));
  const summary = renderSummary(entries, env.INVARIANTS_OUTCOME);
  fs.mkdirSync(directory, { recursive: true });
  fs.writeFileSync(path.join(directory, "summary.md"), summary);
  if (env.GITHUB_STEP_SUMMARY) fs.appendFileSync(env.GITHUB_STEP_SUMMARY, summary);
  return { summary, failed: entries.some(entry => ["Failed", "Cancelled", "No report"].includes(entry.status)) || ["failure", "cancelled"].includes(env.INVARIANTS_OUTCOME) };
}

if (require.main === module) {
  try {
    const result = writeSummary({ directory: process.argv[2] });
    process.stdout.write(result.summary);
    if (result.failed) process.exitCode = 1;
  } catch (error) {
    console.error(`Cannot publish work queue stress summary: ${error.message}`);
    process.exitCode = 1;
  }
}

module.exports = { renderSummary, writeSummary };
