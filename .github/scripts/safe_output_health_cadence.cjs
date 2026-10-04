const fs = require("node:fs");
const path = require("node:path");

async function checkCadence({ github, context, core, outputPath = "/tmp/gh-aw/agent/safe-output-health-cadence.json" }) {
  const result = { status: "unknown", expected_interval_hours: 24, threshold_hours: 48 };
  let message;
  try {
    const { data: current } = await github.rest.actions.getWorkflowRun({
      ...context.repo,
      run_id: context.runId,
    });
    result.current_run_started_at = current.run_started_at;
    const started = Date.parse(current.run_started_at);
    if (!Number.isFinite(started)) throw new Error("Invalid current run timestamp");
    const runs = await github.paginate(github.rest.actions.listWorkflowRuns, {
      ...context.repo,
      workflow_id: current.workflow_id,
      branch: context.payload.repository.default_branch,
      status: "success",
      per_page: 100,
    });
    const previous = runs.filter(run => run.id !== context.runId && Date.parse(run.run_started_at) <= started).sort((left, right) => Date.parse(right.run_started_at) - Date.parse(left.run_started_at))[0];
    if (previous) {
      const elapsed = (started - Date.parse(previous.run_started_at)) / 3600000;
      if (!Number.isFinite(elapsed) || elapsed < 0) throw new Error("Invalid workflow run timestamps");
      result.previous_successful_run = { id: previous.id, run_started_at: previous.run_started_at, url: previous.html_url };
      result.elapsed_hours = elapsed;
      result.status = elapsed > result.threshold_hours ? "stale" : "healthy";
      message = `Safe Output Health cadence: ${result.status}; ${elapsed.toFixed(1)} hours since the previous successful audit (daily interval; alert above 48 hours).`;
    } else {
      message = "Safe Output Health cadence: unknown; no previous successful default-branch audit found.";
    }
  } catch {
    message = "Safe Output Health cadence: unknown; workflow run history could not be checked.";
  }
  result.message = message;
  fs.mkdirSync(path.dirname(outputPath), { recursive: true });
  fs.writeFileSync(outputPath, `${JSON.stringify(result, null, 2)}\n`);
  if (result.status !== "healthy") core.warning(message);
  core.info(message);
  await core.summary.addHeading("Safe Output Health cadence").addRaw(message).write();
  return result;
}

module.exports = { checkCadence };
