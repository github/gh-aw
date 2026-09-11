// @ts-check

const fs = require("fs");
const path = require("path");
const { sumAICFromUsageJSONLFiles } = require("./daily_aic_workflow_helpers.cjs");

// These are the compiler-owned jobs and collect_usage_artifact_files.sh paths.
// Raw firewall accounting is preferred to the overlapping engine summary.
const COMPONENT_FILES = {
  agent: [["agent", "token_usage.jsonl"], ["agent_usage.jsonl"]],
  detection: [["detection", "token_usage.jsonl"], ["detection_usage.jsonl"]],
  evals: [["evals", "token_usage.jsonl"]],
};

async function loadBillableJobs({ github, budget }, owner, repo, run) {
  const components = new Map();
  let complete = false;
  for (let page = 1; page <= 10; page++) {
    const response = await github.rest.actions.listJobsForWorkflowRun({
      owner,
      repo,
      run_id: run.id,
      filter: "all",
      per_page: 100,
      page,
    });
    budget.observe(response);
    const jobs = response.data.jobs;
    if (!Array.isArray(jobs)) throw new Error("Incomplete daily AIC job metadata");
    for (const job of jobs) {
      if (!Object.hasOwn(COMPONENT_FILES, job.name)) continue;
      if (!Number.isSafeInteger(job.run_attempt) || job.run_attempt < 1 || job.run_attempt > run.run_attempt || job.status !== "completed" || !job.conclusion) {
        throw new Error("Incomplete daily AIC component attempt metadata");
      }
      const prior = components.get(job.name);
      if (prior && job.run_attempt === prior.run_attempt && job.id !== prior.id) {
        throw new Error("Ambiguous daily AIC component jobs");
      }
      if (!prior || (prior.conclusion === "skipped" && job.conclusion !== "skipped") || (job.conclusion !== "skipped" && job.run_attempt > prior.run_attempt) || (prior.conclusion === "skipped" && job.run_attempt > prior.run_attempt)) {
        components.set(job.name, job);
      }
    }
    if (jobs.length < 100) {
      complete = true;
      break;
    }
  }
  if (!complete || !components.has("agent")) throw new Error("Cannot prove complete billable-component coverage");
  return components;
}

function allBillableJobsSkipped(components) {
  return [...components.values()].every(job => job.conclusion === "skipped");
}

function sumCoveredComponents(directory, components, artifactCreatedAt, artifacts, usageArtifactName, attempt) {
  let total = 0;
  for (const [name, job] of components) {
    if (job.conclusion === "skipped") continue;
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
        throw new Error(`Cannot verify the ${name} producer artifact for its job attempt`);
      }
    }
    const candidates = COMPONENT_FILES[name].map(parts => path.join(directory, ...parts));
    const selected = candidates.find(file => fs.existsSync(file) && fs.readFileSync(file, "utf8").trim());
    if (!selected) throw new Error(`Missing accounting for executed ${name} component`);
    total += sumAICFromUsageJSONLFiles([selected], { strict: true });
  }
  if (!Number.isFinite(total)) throw new Error("Daily AIC component total is not finite");
  return total;
}

module.exports = { loadBillableJobs, allBillableJobsSkipped, sumCoveredComponents };
