// @ts-check
/// <reference types="@actions/github-script" />

const { ERR_CONFIG, ERR_VALIDATION } = require("./error_codes.cjs");
const { writeDenialSummary } = require("./pre_activation_summary.cjs");

// Matches a relative time delta such as "+25h", "+3d", "+1w", "+1mo", "+1d12h".
// Mirrors pkg/workflow/time_delta.go's parseTimeDeltaForStopAfter: minutes are not
// supported since the minimum unit for stop-after is hours.
const TIME_DELTA_PATTERN = /(\d+)(mo|w|d|h)/g;

/** @param {string} stopTime */
function isRelativeStopTime(stopTime) {
  return stopTime.startsWith("+");
}

/** @param {string} deltaStr */
function parseTimeDeltaForStopAfter(deltaStr) {
  const rest = deltaStr.slice(1);
  if (!rest) {
    throw new Error("empty time delta after '+'");
  }

  const matches = [...rest.matchAll(TIME_DELTA_PATTERN)];
  if (matches.length === 0) {
    throw new Error(`invalid time delta format: +${rest}. Expected format like +25h, +3d, +1w, +1mo, +1d12h`);
  }

  const consumed = matches.reduce((sum, match) => sum + match[0].length, 0);
  if (consumed !== rest.length) {
    throw new Error(`invalid time delta format: +${rest}. Extra characters detected`);
  }

  const delta = { months: 0, weeks: 0, days: 0, hours: 0 };
  const seenUnits = new Set();
  for (const [, valueStr, unit] of matches) {
    if (seenUnits.has(unit)) {
      throw new Error(`duplicate unit '${unit}' in time delta: +${rest}`);
    }
    seenUnits.add(unit);
    const value = parseInt(valueStr, 10);
    if (unit === "mo") delta.months = value;
    else if (unit === "w") delta.weeks = value;
    else if (unit === "d") delta.days = value;
    else if (unit === "h") delta.hours = value;
  }
  return delta;
}

/**
 * Resolves a relative stop-time delta (e.g. "+48h") to an absolute Date, relative to baseTime.
 * Mirrors pkg/workflow/stop_after.go's resolveStopTime: months and days/weeks are applied
 * together in a single calendar computation (so date-normalization overflow, e.g. Jan 31 + 1mo,
 * is resolved consistently), then hours are added on top.
 * @param {string} deltaStr
 * @param {Date} baseTime
 */
function resolveRelativeStopTime(deltaStr, baseTime) {
  const delta = parseTimeDeltaForStopAfter(deltaStr);
  const totalDays = delta.weeks * 7 + delta.days;
  return new Date(
    Date.UTC(baseTime.getUTCFullYear(), baseTime.getUTCMonth() + delta.months, baseTime.getUTCDate() + totalDays, baseTime.getUTCHours() + delta.hours, baseTime.getUTCMinutes(), baseTime.getUTCSeconds(), baseTime.getUTCMilliseconds())
  );
}

async function main() {
  const stopTime = process.env.GH_AW_STOP_TIME;
  const workflowName = process.env.GH_AW_WORKFLOW_NAME;

  if (!stopTime) {
    core.setFailed(`${ERR_CONFIG}: Configuration error: GH_AW_STOP_TIME not specified.`);
    return;
  }

  if (!workflowName) {
    core.setFailed(`${ERR_CONFIG}: Configuration error: GH_AW_WORKFLOW_NAME not specified.`);
    return;
  }

  core.info(`Checking stop-time limit: ${stopTime}`);

  // Resolve the stop time. A GitHub Actions expression (e.g. "${{ inputs.stop-after }}")
  // is passed through verbatim at compile time and evaluated by the runner before this
  // step runs, so it may still be a relative delta (e.g. "+48h") rather than an already
  // resolved absolute timestamp (format: "YYYY-MM-DD HH:MM:SS").
  let stopTimeDate;
  if (isRelativeStopTime(stopTime)) {
    try {
      stopTimeDate = resolveRelativeStopTime(stopTime, new Date());
    } catch (err) {
      core.setFailed(`${ERR_VALIDATION}: Invalid stop-time format: ${stopTime}. ${err instanceof Error ? err.message : String(err)}`);
      return;
    }
  } else {
    stopTimeDate = new Date(stopTime);
  }

  if (Number.isNaN(stopTimeDate.getTime())) {
    core.setFailed(`${ERR_VALIDATION}: Invalid stop-time format: ${stopTime}. Expected format: YYYY-MM-DD HH:MM:SS`);
    return;
  }

  const currentTime = new Date();
  core.info(`Current time: ${currentTime.toISOString()}`);
  core.info(`Stop time: ${stopTimeDate.toISOString()}`);

  if (currentTime >= stopTimeDate) {
    core.warning(`⏰ Stop time reached. Workflow execution will be prevented by activation job.`);
    core.setOutput("stop_time_ok", "false");
    await writeDenialSummary(`Workflow '${workflowName}' has passed its configured stop-time (${stopTimeDate.toISOString()}).`, "Update or remove `on.stop-after:` in the workflow frontmatter to extend the active window.");
    return;
  }

  core.setOutput("stop_time_ok", "true");
}

module.exports = { main };
