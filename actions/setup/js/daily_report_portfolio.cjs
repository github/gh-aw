// @ts-check
"use strict";

const { buildAWPolicy } = require("./work_queue_settings.cjs");
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
const FIXED_DAILY_REPORTS = Object.freeze(["deep-report"]);
// Report dates precede activation dates: Saturday/Sunday run on Sunday/Monday.
const WEEKLY_REPORTS = Object.freeze([Object.freeze({ profile: "artifacts-summary", reportWeekday: 6 }), Object.freeze({ profile: "repo-tree-map", reportWeekday: 0 })]);
const REPORT_PROFILES = Object.freeze([...DAILY_REPORTS, ...FIXED_DAILY_REPORTS, ...WEEKLY_REPORTS.map(report => report.profile)]);

/** @param {string} date */
function reportDay(date) {
  if (typeof date !== "string" || !/^[0-9]{4}-[0-9]{2}-[0-9]{2}$/.test(date)) throw new Error("Report date must be YYYY-MM-DD in UTC");
  const timestamp = Date.parse(`${date}T00:00:00.000Z`);
  if (!Number.isFinite(timestamp) || timestamp < 0 || new Date(timestamp).toISOString().slice(0, 10) !== date) throw new Error("Report date must be a valid UTC date on or after 1970-01-01");
  return timestamp / 86400000;
}

/** @param {string} date */
function reportsForDay(date) {
  const day = reportDay(date);
  const weekly = WEEKLY_REPORTS.filter(report => report.reportWeekday === (day + 4) % 7).map(report => report.profile);
  const priorWeeklySlots = WEEKLY_REPORTS.reduce((count, report) => {
    const firstDay = (report.reportWeekday - 4 + 7) % 7;
    return count + Math.floor((day + 6 - firstDay) / 7);
  }, 0);
  const rotatingSlots = REPORTS_PER_DAY - FIXED_DAILY_REPORTS.length;
  const offset = (day * rotatingSlots - priorWeeklySlots) % DAILY_REPORTS.length;
  return [...Array.from({ length: rotatingSlots - weekly.length }, (_, index) => DAILY_REPORTS[(offset + index) % DAILY_REPORTS.length]), ...weekly, ...FIXED_DAILY_REPORTS];
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
  if (profile === "deep-report") outputs.push({ type: "create_issue", min: 0, max: 7 }, { type: "add_comment", min: 0, max: 3 }, { type: "upload_artifact", min: 0, max: 3 });
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

/** @param {{repository: string, ref: string, settings?: object}} options */
function buildDailyReportPolicy({ repository, ref, settings }) {
  assertRepository(repository);
  return buildAWPolicy({ repository, ref, workflows: [...REPORT_PROFILES], settings });
}

module.exports = { DAILY_REPORTS, FIXED_DAILY_REPORTS, WEEKLY_REPORTS, REPORT_PROFILES, REPORTS_PER_DAY, REPORT_POOL, reportsForDay, buildDailyReportPlan, buildDailyReportPolicy };

if (require.main === module) {
  const [command, repository, ref, ...extra] = process.argv.slice(2);
  if (command !== "policy" || extra.length || !repository || !ref) throw new Error("Usage: node daily_report_portfolio.cjs policy OWNER/REPO IMMUTABLE_SHA");
  const { readPortfolioSettings } = require("./work_queue_portfolio_config.cjs");
  process.stdout.write(JSON.stringify(buildDailyReportPolicy({ repository, ref, settings: readPortfolioSettings() }), null, 2) + "\n");
}
