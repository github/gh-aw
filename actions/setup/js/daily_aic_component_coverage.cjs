// @ts-check

const fs = require("fs");
const path = require("path");
const { sumAICFromUsageJSONLFiles } = require("./daily_aic_workflow_helpers.cjs");
const { API_READ_OPERATIONS } = require("./daily_aic_api_budget.cjs");
const { EVALS_STATUS_FILENAME } = require("./evals_constants.cjs");
const { EVALS_REASON_CODES, readEvalsNotEvaluatedStatus } = require("./evals_status.cjs");

// These are the compiler-owned jobs and collect_usage_artifact_files.sh paths.
// Raw firewall accounting is preferred to the overlapping engine summary.
const COMPONENT_FILES = {
  agent: [["agent", "token_usage.jsonl"], ["agent_usage.jsonl"], ["agent_usage.json"]],
  detection: [["detection", "token_usage.jsonl"], ["detection_usage.jsonl"]],
  evals: [["evals", "token_usage.jsonl"]],
};

// Generated jobs display as "Agent", "Detection" and "Evaluations".
const COMPONENT_DISPLAY_NAMES = { evaluations: "evals" };

function componentKey(job, jobNames) {
  for (const candidate of [job.job_id, job.name]) {
    if (typeof candidate !== "string") continue;
    const separator = candidate.lastIndexOf(" / ");
    if (separator !== -1) {
      // Caller names are arbitrary. Require the compiler's sibling jobs rather
      // than treating every reusable workflow with an "agent" job as gh-aw.
      const prefix = candidate.slice(0, separator + 3);
      if (!jobNames.has(`${prefix}activation`) || !jobNames.has(`${prefix}conclusion`)) continue;
    }
    const lower = (candidate.split(" / ").pop() ?? "").toLowerCase();
    const key = COMPONENT_DISPLAY_NAMES[lower] ?? lower;
    if (Object.hasOwn(COMPONENT_FILES, key)) return key;
  }
  return "";
}

async function loadBillableJobs({ github, budget }, owner, repo, run) {
  const components = new Map();
  const jobsByComponent = new Map();
  const allJobs = [];
  let complete = false;
  for (let page = 1; page <= 10; page++) {
    const response = await budget.request(
      () =>
        github.rest.actions.listJobsForWorkflowRun({
          owner,
          repo,
          run_id: run.id,
          filter: "all",
          per_page: 100,
          page,
        }),
      true,
      API_READ_OPERATIONS.RUN_JOBS
    );
    const jobs = response.data.jobs;
    if (!Array.isArray(jobs)) throw new Error("Incomplete daily AIC job metadata");
    allJobs.push(...jobs);
    if (jobs.length < 100) {
      complete = true;
      break;
    }
  }
  if (!complete) throw new Error("Cannot prove complete billable-component coverage");
  const jobNames = new Set(allJobs.flatMap(job => [job.job_id, job.name]));
  for (const job of allJobs) {
    const name = componentKey(job, jobNames);
    if (!name) continue;
    if (!Number.isSafeInteger(job.run_attempt) || job.run_attempt < 1 || job.run_attempt > run.run_attempt || job.status !== "completed" || !job.conclusion) {
      throw new Error("Incomplete daily AIC component attempt metadata");
    }
    if (!jobsByComponent.has(name)) jobsByComponent.set(name, []);
    jobsByComponent.get(name).push(job);
    const prior = components.get(name);
    const notStarted = job.conclusion === "skipped" || provesJobExecutionNotStarted(job);
    const priorNotStarted = prior && (prior.conclusion === "skipped" || provesJobExecutionNotStarted(prior));
    if (!prior || (priorNotStarted && !notStarted) || (notStarted === priorNotStarted && job.run_attempt > prior.run_attempt)) {
      components.set(name, job);
    }
  }
  // A complete empty job list proves the run ended before any billable job existed.
  if (components.size > 0 && !components.has("agent")) throw new Error("Cannot prove complete billable-component coverage");
  const priorJobs = new Map();
  for (const [name, selected] of components) {
    const selectedAttemptJobs = jobsByComponent.get(name).filter(job => job.run_attempt === selected.run_attempt);
    if (new Set(selectedAttemptJobs.map(job => job.id)).size > 1) {
      throw new Error("Ambiguous daily AIC component jobs");
    }
    if (selected.conclusion === "cancelled") {
      priorJobs.set(
        name,
        jobsByComponent
          .get(name)
          .filter(job => job.conclusion !== "skipped" && job.run_attempt < selected.run_attempt)
          .sort((a, b) => b.run_attempt - a.run_attempt)
      );
    }
  }
  return { components, priorJobs };
}

function allBillableJobsNotStarted(components) {
  return [...components.values()].every(job => job.conclusion === "skipped" || provesJobExecutionNotStarted(job));
}

function allBillableJobsSkipped(components) {
  return [...components.values()].every(job => job.conclusion === "skipped");
}

function provesJobExecutionNotStarted(job) {
  return (
    (job.conclusion === "failure" || job.conclusion === "cancelled") &&
    Object.hasOwn(job, "runner_id") &&
    (job.runner_id === null || job.runner_id === 0) &&
    Object.hasOwn(job, "runner_name") &&
    job.runner_name === null &&
    Object.hasOwn(job, "steps") &&
    Array.isArray(job.steps) &&
    job.steps.length === 0
  );
}

function provesExecutionNotStarted(directory, name, runId, runAttempt) {
  const evidenceFile = path.join(directory, name, "execution.json");
  if (!fs.existsSync(evidenceFile)) return false;
  try {
    const evidence = JSON.parse(fs.readFileSync(evidenceFile, "utf8"));
    return evidence?.version === 1 && evidence.component === name && evidence.run_id === runId && evidence.run_attempt === runAttempt && evidence.state === "not_started";
  } catch {
    return false;
  }
}

function hasFailedDecisionsEvals(directory, runId, runAttempt) {
  const status = readEvalsNotEvaluatedStatus(path.join(directory, "evals", EVALS_STATUS_FILENAME));
  return (
    status?.engine === "decisions" &&
    status.reason_code === EVALS_REASON_CODES.JUDGE_FAILED &&
    status.execution_outcome === "failure" &&
    status.execution_state === "started" &&
    status.run_id === String(runId) &&
    status.run_attempt === String(runAttempt)
  );
}

function hasUnavailableDecisionsModel(directory, runId, runAttempt) {
  const status = readEvalsNotEvaluatedStatus(path.join(directory, "evals", EVALS_STATUS_FILENAME));
  return (
    status?.engine === "decisions" &&
    status.reason_code === EVALS_REASON_CODES.MODEL_UNAVAILABLE &&
    status.execution_outcome === "success" &&
    status.execution_state === EVALS_REASON_CODES.MODEL_UNAVAILABLE &&
    status.run_id === String(runId) &&
    status.run_attempt === String(runAttempt) &&
    ["evals.jsonl", path.join("evals", "evals.jsonl")].every(file => {
      const state = inspectAccountingFile(directory, path.join(directory, file)).state;
      return state === "missing" || state === "empty";
    })
  );
}

function inspectAccountingFile(directory, file) {
  const relativeFile = path.relative(directory, file);
  if (!fs.existsSync(file)) return { file: relativeFile, state: "missing" };
  try {
    return { file: relativeFile, state: fs.readFileSync(file, "utf8").trim() ? "non-empty" : "empty" };
  } catch (error) {
    return { file: relativeFile, state: `unreadable (${error instanceof Error ? error.message : String(error)})` };
  }
}

function logComponentAIC(runId, name, job, aic, reason, details = {}) {
  core.info(
    `[daily-workflow-aic] Computed component AIC: ${JSON.stringify({
      runId,
      component: name,
      jobId: job.id,
      runAttempt: job.run_attempt,
      conclusion: job.conclusion,
      aic,
      reason,
      ...details,
    })}`
  );
}

/**
 * @param {{ artifactInspected: boolean, preHarnessFailure: boolean, sampleReplay: boolean } | null} legacyAgentEvidence
 */
function sumCoveredComponents(directory, components, artifactCreatedAt, artifacts, usageArtifactName, attempt, runId, legacyAgentEvidence = null, priorJobs = new Map()) {
  let total = 0;
  for (const [name, selectedJob] of components) {
    let job = selectedJob;
    if (job.conclusion === "skipped") {
      logComponentAIC(runId, name, job, 0, "job_skipped");
      continue;
    }
    if (provesJobExecutionNotStarted(job)) {
      logComponentAIC(runId, name, job, 0, "runner_not_assigned");
      continue;
    }
    // Failed-only reruns can retain successful jobs from earlier attempts. Such
    // usage remains valid, but an artifact predating any executed job does not.
    const started = Date.parse(job.started_at);
    const completed = Date.parse(job.completed_at);
    if (!Number.isFinite(started) || !Number.isFinite(completed) || started > completed || completed > artifactCreatedAt) {
      throw new Error(`Usage artifact does not cover the ${name} component attempt`);
    }
    if (attempt > 1) {
      // The conclusion job can repack an older producer artifact after a failed
      // rerun. Check the original producer, not only the new aggregate timestamp.
      const producerName = usageArtifactName.slice(0, -"usage".length) + name;
      const producer = artifacts.find(artifact => artifact.name === producerName);
      const produced = producer?.createdAt?.getTime();
      if (!producer?.id || producer.expired || !Number.isFinite(produced) || produced < started || produced >= completed + 1000) {
        const earlier = priorJobs.get(name) || [];
        const prior = earlier[0];
        const priorStarted = Date.parse(prior?.started_at);
        const priorCompleted = Date.parse(prior?.completed_at);
        if (job.conclusion === "cancelled" && prior && earlier.some(item => item.run_attempt === prior.run_attempt && item.id !== prior.id)) {
          throw new Error("Ambiguous daily AIC component jobs");
        }
        if (
          job.conclusion !== "cancelled" ||
          !prior ||
          !producer?.id ||
          producer.expired ||
          !Number.isFinite(produced) ||
          !Number.isFinite(priorStarted) ||
          !Number.isFinite(priorCompleted) ||
          priorStarted > priorCompleted ||
          produced < priorStarted ||
          produced >= priorCompleted + 1000 ||
          produced >= started
        ) {
          throw new Error(`Cannot verify the ${name} producer artifact for its job attempt`);
        }
        core.warning(
          `[daily-workflow-aic] Cancelled ${name} job ${job.id} in run ${runId} has no verifiable producer artifact; counting carried-forward usage from attempt ${prior.run_attempt}. Usage from the cancelled attempt may be unaccounted for.`
        );
        job = prior;
      }
    }
    const candidates = COMPONENT_FILES[name].map(parts => path.join(directory, ...parts));
    const candidateStates = candidates.map(file => inspectAccountingFile(directory, file));
    core.info(
      `[daily-workflow-aic] Inspected component accounting: ${JSON.stringify({
        runId,
        component: name,
        jobId: job.id,
        runAttempt: job.run_attempt,
        conclusion: job.conclusion,
        candidates: candidateStates,
      })}`
    );
    // An empty raw detection/token_usage.jsonl is authoritative proof that
    // detection ran but produced no firewall-observed usage (e.g. threat
    // detection was skipped internally). Unlike every other component, this
    // deliberately overrides the "first non-empty candidate wins" fallback
    // below: the legacy detection_usage.jsonl summary is not consulted, so a
    // stale or unrelated fallback value cannot resurrect nonzero AIC for a
    // component whose primary source proves zero usage.
    if (name === "detection" && candidateStates[0].state === "empty") {
      logComponentAIC(runId, name, job, 0, "empty_detection_accounting", {
        source: candidateStates[0].file,
      });
      continue;
    }
    const selectedIndex = candidateStates.findIndex(candidate => candidate.state === "non-empty");
    const unreadableIndex = candidateStates.findIndex(candidate => candidate.state.startsWith("unreadable"));
    const selected = selectedIndex >= 0 ? candidates[selectedIndex] : "";
    const candidateSummary = candidateStates.map(candidate => `${candidate.file} is ${candidate.state}`).join("; ");
    if (unreadableIndex >= 0 && (selectedIndex < 0 || unreadableIndex < selectedIndex)) {
      throw new Error(`Missing accounting for executed ${name} component in run ${runId} (attempt ${job.run_attempt}, job ${job.id}, conclusion ${job.conclusion}): ${candidateSummary}`);
    }
    // Failed agent requests can be rejected before the provider returns usage
    // (for example, an unsupported model). An empty authoritative firewall
    // file proves that no billable response was observed.
    if (!selected && name === "agent" && job.conclusion === "failure" && candidateStates[0].state === "empty") {
      logComponentAIC(runId, name, job, 0, "failed_before_accounting", {
        source: candidateStates[0].file,
      });
      continue;
    }
    if (!selected && name === "agent" && job.conclusion === "failure" && legacyAgentEvidence?.preHarnessFailure) {
      logComponentAIC(runId, name, job, 0, "legacy_pre_harness_failure");
      continue;
    }
    if (!selected && name === "agent" && job.conclusion === "success" && candidateStates[0].state === "empty" && legacyAgentEvidence?.sampleReplay) {
      logComponentAIC(runId, name, job, 0, "legacy_sample_replay", {
        source: candidateStates[0].file,
      });
      continue;
    }
    if (!selected) {
      if (provesExecutionNotStarted(directory, name, runId, job.run_attempt)) {
        logComponentAIC(runId, name, job, 0, "execution_not_started");
        continue;
      }
      if (name === "agent" && job.conclusion === "failure" && candidateStates[0].state === "missing" && !fs.existsSync(path.join(directory, name, "execution.json")) && !legacyAgentEvidence?.artifactInspected) {
        logComponentAIC(runId, name, job, 0, "failed_before_accounting", {
          source: candidateStates[0].file,
        });
        continue;
      }
      // Only a run-bound rejected-model marker without answer records covers a
      // graceful Decisions skip; arbitrary not-evaluated markers do not.
      if (name === "evals" && job.conclusion === "success" && hasUnavailableDecisionsModel(directory, runId, job.run_attempt)) {
        core.warning(`[daily-workflow-aic] Counting unavailable Decisions model without response usage as zero for run ${runId}, attempt ${job.run_attempt}; provider billing remains unverified.`);
        logComponentAIC(runId, name, job, 0, EVALS_REASON_CODES.MODEL_UNAVAILABLE, {
          source: candidateStates[0].file,
        });
        continue;
      }
      // Decisions is advisory: continue-on-error can leave the job successful
      // even though the run-bound judge outcome records a failed request.
      if (name === "evals" && (job.conclusion === "failure" || (job.conclusion === "success" && hasFailedDecisionsEvals(directory, runId, job.run_attempt)))) {
        core.warning(`[daily-workflow-aic] Counting failed evals without response usage as zero under the failed-evaluation accounting policy for run ${runId}, attempt ${job.run_attempt}; provider billing remains unverified.`);
        logComponentAIC(runId, name, job, 0, "failed_before_accounting", {
          source: candidateStates[0].file,
        });
        continue;
      }
      throw new Error(`Missing accounting for executed ${name} component in run ${runId} (attempt ${job.run_attempt}, job ${job.id}, conclusion ${job.conclusion}): ${candidateSummary}`);
    }
    const componentAIC = sumAICFromUsageJSONLFiles([selected], { strict: true });
    logComponentAIC(runId, name, job, componentAIC, "accounting_file", {
      source: candidateStates[selectedIndex].file,
    });
    total += componentAIC;
  }
  if (!Number.isFinite(total)) throw new Error("Daily AIC component total is not finite");
  core.info(`[daily-workflow-aic] Computed covered component total: ${JSON.stringify({ runId, aic: total })}`);
  return total;
}

module.exports = { loadBillableJobs, allBillableJobsNotStarted, allBillableJobsSkipped, sumCoveredComponents };
