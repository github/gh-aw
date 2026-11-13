"use strict";

const fs = require("node:fs");
const { frontDoor, validateCatalog, SUITE_IDS } = require("./ci-front-door.cjs");

async function evaluate(catalog, input) {
  validateCatalog(catalog);
  if (!Array.isArray(input?.records) || input.records.length > 10000) throw new Error("Expected at most 10000 cached records");
  const report = {
    evaluation_kind: "recorded-response-replay",
    records: 0,
    missing_response_records: 0,
    full_run_records: 0,
    selected_suite_count: 0,
    estimated_test_minutes_saved: 0,
    missing_duration_records: 0,
    missed_regressions: 0,
    missing_outcome_records: 0,
    decision_api_requests: 0,
    other_github_api_requests: 0,
    missing_api_count_records: 0,
    evaluation_api_requests: 0,
    results: [],
  };
  const seen = new Set();
  for (const record of input.records) {
    if (typeof record.id !== "string" || !record.id || seen.has(record.id)) throw new Error("Missing or duplicate record ID");
    seen.add(record.id);
    const missingResponse = !Object.hasOwn(record, "response") || record.response === undefined;
    if (missingResponse) report.missing_response_records++;
    if (
      !Array.isArray(record.diff_summary?.files) ||
      record.diff_summary.files.some(file => typeof file !== "string") ||
      typeof record.diff_summary.patch !== "string" ||
      typeof record.diff_summary.unavailable !== "boolean" ||
      typeof record.diff_summary.truncated !== "boolean"
    )
      throw new Error("Invalid cached diff summary");
    const selection = await frontDoor({
      catalog,
      diff: record.diff_summary,
      prompt: "",
      endpoint: "",
      decide: async () => record.response,
    });
    const omitted = SUITE_IDS.filter(id => !selection.suite_ids.includes(id));
    const durations = omitted.map(id => record.durations_seconds?.[id] ?? catalog.suites.find(suite => suite.id === id).historical_duration_seconds);
    const saved = !missingResponse && durations.every(duration => Number.isFinite(duration) && duration >= 0) ? durations.reduce((sum, duration) => sum + duration, 0) / 60 : null;
    if (saved === null) report.missing_duration_records++;
    else report.estimated_test_minutes_saved += saved;
    let missed = null;
    if (!missingResponse && record.full_run === true && Array.isArray(record.failed_suite_ids) && record.failed_suite_ids.every(id => SUITE_IDS.includes(id))) {
      report.full_run_records++;
      missed = [...new Set(record.failed_suite_ids)].filter(id => omitted.includes(id));
      report.missed_regressions += missed.length;
    } else {
      report.missing_outcome_records++;
    }
    if (Number.isSafeInteger(record.api_requests?.decision) && record.api_requests.decision >= 0 && Number.isSafeInteger(record.api_requests.github) && record.api_requests.github >= 0) {
      report.decision_api_requests += record.api_requests.decision;
      report.other_github_api_requests += record.api_requests.github;
    } else {
      report.missing_api_count_records++;
    }
    report.records++;
    report.selected_suite_count += selection.suite_ids.length;
    report.results.push({ id: record.id, suite_ids: missingResponse ? null : selection.suite_ids, fallback: missingResponse ? null : selection.fallback, estimated_test_minutes_saved: saved, missed_regressions: missed });
  }
  // Partial evidence is not evidence of zero misses or zero requests.
  if (report.full_run_records === 0 || report.missing_response_records > 0) report.missed_regressions = null;
  if (report.missing_response_records > 0) report.selected_suite_count = null;
  if (report.missing_api_count_records > 0 || report.records === 0) {
    report.decision_api_requests = null;
    report.other_github_api_requests = null;
  }
  if (report.missing_duration_records > 0 || report.records === 0) report.estimated_test_minutes_saved = null;
  return report;
}

module.exports = { evaluate };
if (require.main === module) {
  const [catalogFile, recordsFile] = process.argv.slice(2);
  Promise.resolve()
    .then(() => {
      for (const file of [catalogFile, recordsFile]) {
        if (!file || fs.statSync(file).size > 16 * 1024 * 1024) throw new Error("Missing or oversized local evaluation file");
      }
      return evaluate(JSON.parse(fs.readFileSync(catalogFile, "utf8")), JSON.parse(fs.readFileSync(recordsFile, "utf8")));
    })
    .then(report => process.stdout.write(JSON.stringify(report) + "\n"))
    .catch(() => {
      process.stderr.write("Invalid local Front Door evaluation evidence.\n");
      process.exitCode = 1;
    });
}
