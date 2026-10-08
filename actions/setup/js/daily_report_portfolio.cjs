// @ts-check
"use strict";

const { DEFAULT_LIMITS } = require("./work_queue_limits.cjs");
const { validatePolicy } = require("./work_queue_policy.cjs");
const { validateDeliveryContract } = require("./work_queue_delivery.cjs");

const DAILY_REPORTS = Object.freeze([
  "daily-compiler-quality",
  "daily-evals-report",
  "daily-firewall-report",
  "daily-issues-report",
  "daily-observability-report",
  "daily-regulatory",
  "daily-repo-chronicle",
  "daily-secrets-analysis",
  "daily-team-evolution-insights",
  "daily-token-consumption-report",
]);
const REPORTS_PER_DAY = 3;
const REPORT_POOL = "daily-reports";

/** @param {string} date */
function reportDay(date) {
  if (typeof date !== "string" || !/^[0-9]{4}-[0-9]{2}-[0-9]{2}$/.test(date)) throw new Error("Report date must be YYYY-MM-DD in UTC");
  const timestamp = Date.parse(`${date}T00:00:00.000Z`);
  if (!Number.isFinite(timestamp) || timestamp < 0 || new Date(timestamp).toISOString().slice(0, 10) !== date) throw new Error("Report date must be a valid UTC date on or after 1970-01-01");
  return timestamp / 86400000;
}

/** @param {string} date */
function reportsForDay(date) {
  const offset = (reportDay(date) * REPORTS_PER_DAY) % DAILY_REPORTS.length;
  return Array.from({ length: REPORTS_PER_DAY }, (_, index) => DAILY_REPORTS[(offset + index) % DAILY_REPORTS.length]);
}

/** @param {string} repository */
function assertRepository(repository) {
  if (typeof repository !== "string" || !/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository)) throw new Error("Report repository must be owner/name");
}

/** @param {string} profile */
function reportContract(profile) {
  const outputs = [{ type: "create_discussion", min: 1, max: 1 }, ...["noop", "report_incomplete", "missing_tool", "missing_data"].map(type => ({ type, min: 0, max: 1 }))];
  if (["daily-firewall-report", "daily-repo-chronicle"].includes(profile)) outputs.push({ type: "upload_asset", min: 0, max: 3 });
  if (profile === "daily-regulatory") outputs.push({ type: "close_discussion", min: 0, max: 1 });
  return validateDeliveryContract({ version: 1, outputs });
}

/** @param {{date: string, repository: string, repositoryId: string}} options */
function buildDailyReportPlan({ date, repository, repositoryId }) {
  assertRepository(repository);
  if (typeof repositoryId !== "string" || !/^[1-9][0-9]{0,255}$/.test(repositoryId)) throw new Error("Report repository ID must be a verified positive decimal identity");
  const selected = reportsForDay(date);
  return {
    version: 1,
    date,
    selected,
    nodes: selected.map(profile => ({
      graph_id: `daily-report-cohort:${date}`,
      node_key: profile,
      pool: REPORT_POOL,
      priority: 3,
      fairness_key: profile,
      worker_profile: profile,
      payload: {
        plan: "Run this workflow's existing discussion-report mission for the immutable report date. Publish at most one discussion; do not dispatch other reports.",
        report_date: date,
        report_profile: profile,
        resource_scope: { version: 1, resources: [{ host: "github.com", repository, repository_id: repositoryId }] },
        effect_contract: reportContract(profile),
      },
      depends_on: [],
    })),
    dispatch: { pool: REPORT_POOL, max_claims: REPORTS_PER_DAY, max_dispatches: REPORTS_PER_DAY },
  };
}

/** @param {{repository: string, ref: string, producerPrincipal: string, workerPrincipal: string}} options */
function buildDailyReportPolicy({ repository, ref, producerPrincipal, workerPrincipal }) {
  assertRepository(repository);
  const policy = {
    mode: "weighted-priority",
    class_weights: [8, 4, 2, 1, 1],
    accounting_weights: Object.fromEntries([["", 1], ...DAILY_REPORTS.map(profile => [profile, 1])]),
    producers: { [producerPrincipal]: { pools: [REPORT_POOL], priorities: [3], fairness_keys: [...DAILY_REPORTS] } },
    pools: {
      [REPORT_POOL]: {
        default_profile: DAILY_REPORTS[0],
        profiles: Object.fromEntries(
          DAILY_REPORTS.map(profile => [
            profile,
            { workflow: `.github/workflows/${profile}.lock.yml`, ref, principal: workerPrincipal, trust_domain: profile, credential_scope: "repository", effect_scope: repository, max_claims: 1, share_keys: false },
          ])
        ),
        logical_limit: REPORTS_PER_DAY,
        native_limit: REPORTS_PER_DAY,
        per_account_limit: 1,
        allowed_repositories: [repository],
        max_observation_age_ms: 60000,
        retry: { max_attempts: 1, backoff_ms: 1000 },
        reconciliation: { max_attempts: 5, deadline_ms: 300000 },
      },
    },
    limits: { ...DEFAULT_LIMITS, graph_nodes: REPORTS_PER_DAY, pending_nodes: 30, operations: 32, payload_bytes: 8192 },
  };
  validatePolicy(policy);
  return policy;
}

module.exports = { DAILY_REPORTS, REPORTS_PER_DAY, REPORT_POOL, reportsForDay, buildDailyReportPlan, buildDailyReportPolicy };

if (require.main === module) {
  const [command, repository, ref, producerPrincipal, workerPrincipal, ...extra] = process.argv.slice(2);
  if (command !== "policy" || extra.length || !repository || !ref || !producerPrincipal || !workerPrincipal) throw new Error("Usage: node daily_report_portfolio.cjs policy OWNER/REPO IMMUTABLE_SHA VERIFIED_PRODUCER_ID VERIFIED_WORKER_ID");
  process.stdout.write(JSON.stringify(buildDailyReportPolicy({ repository, ref, producerPrincipal, workerPrincipal }), null, 2) + "\n");
}
