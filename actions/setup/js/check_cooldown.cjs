// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { fetchAndLogRateLimit } = require("./github_rate_limit_logger.cjs");

const MAX_WORKFLOW_RUN_PAGES = 5;
const MAX_AGENT_JOB_LOOKUPS = 50;

async function resolveWorkflowId(githubClient, owner, repo, runId) {
  const currentRun = await githubClient.rest.actions.getWorkflowRun({
    owner,
    repo,
    run_id: runId,
  });
  const workflowId = currentRun.data.workflow_id;
  if (!workflowId) {
    throw new Error(`Cannot resolve workflow id for run ${runId}`);
  }
  return workflowId;
}

function parseRunCompletedAt(run) {
  // Prefer run_completed_at because it is the terminal timestamp; updated_at is
  // only a compatibility fallback for API responses that do not expose it yet.
  const completedAtMs = Date.parse(run.run_completed_at ?? run.updated_at ?? "");
  if (Number.isNaN(completedAtMs)) {
    return null;
  }
  return { completedAtMs };
}

function agentJobStarted(job) {
  return job?.name === "agent" && job.conclusion !== "skipped" && job.started_at;
}

async function main() {
  const {
    repo: { owner, repo },
    runId,
  } = context;
  const cooldownSeconds = Number(process.env.GH_AW_COOLDOWN_SECONDS);
  if (!Number.isFinite(cooldownSeconds) || !Number.isSafeInteger(cooldownSeconds) || cooldownSeconds < 300) {
    throw new Error("Workflow cooldown must be an integer of at least 300 seconds");
  }

  const threshold = Date.now() - cooldownSeconds * 1000;

  try {
    await fetchAndLogRateLimit(github, "check_cooldown_start");
    const workflowId = await resolveWorkflowId(github, owner, repo, runId);
    core.info(`Checking ${cooldownSeconds}-second cooldown for workflow '${workflowId}'`);

    let page = 1;
    const perPage = 100;
    let agentJobLookups = 0;
    let hitPageLimit = false;

    while (true) {
      if (page > MAX_WORKFLOW_RUN_PAGES) {
        hitPageLimit = true;
        break;
      }
      const response = await github.rest.actions.listWorkflowRuns({
        owner,
        repo,
        workflow_id: workflowId,
        status: "completed",
        per_page: perPage,
        page,
      });
      const runs = response.data.workflow_runs || [];
      const candidateRuns = [];

      for (const run of runs) {
        if (run.id === runId) {
          continue;
        }

        const parsedCompletion = parseRunCompletedAt(run);
        if (!parsedCompletion) {
          core.warning(`Skipping run ${run.id} with an invalid completion time`);
          continue;
        }
        if (parsedCompletion.completedAtMs <= threshold) {
          continue;
        }
        candidateRuns.push({ run, ...parsedCompletion });
      }

      // listWorkflowRuns is ordered by creation time, not completion time. Sort
      // in-window runs by run_completed_at so the first executed agent run found
      // is the newest completion among the inspected history.
      candidateRuns.sort((left, right) => right.completedAtMs - left.completedAtMs);
      for (const { run, completedAtMs } of candidateRuns) {
        if (agentJobLookups >= MAX_AGENT_JOB_LOOKUPS) {
          core.warning(`Cooldown check reached the ${MAX_AGENT_JOB_LOOKUPS}-run job lookup budget before confirming no agent execution within the cooldown`);
          core.setOutput("cooldown_ok", "true");
          return;
        }
        agentJobLookups++;
        const jobs = await github.paginate(github.rest.actions.listJobsForWorkflowRun, {
          owner,
          repo,
          run_id: run.id,
          filter: "latest",
          per_page: 100,
        });
        const agentExecuted = jobs.some(agentJobStarted);
        if (!agentExecuted) {
          continue;
        }

        const remainingSeconds = Math.max(0, Math.ceil((completedAtMs + cooldownSeconds * 1000 - Date.now()) / 1000));
        core.warning(`Skipping agent execution because run ${run.id} completed within the cooldown period (${remainingSeconds} seconds remaining)`);
        core.setOutput("cooldown_ok", "false");
        return;
      }

      // Do not stop just because every run on this page is older than the
      // threshold: listWorkflowRuns is not ordered by run_completed_at, so a
      // later-created page can still contain a newer completion.
      if (runs.length < perPage) {
        break;
      }
      page++;
    }

    if (hitPageLimit) {
      core.warning(`Cooldown check stopped after scanning ${MAX_WORKFLOW_RUN_PAGES} workflow run pages`);
    }
    core.info("Cooldown passed; no recent completed run executed the agent job");
    core.setOutput("cooldown_ok", "true");
  } catch (error) {
    core.warning(`Cooldown check failed: ${getErrorMessage(error)}`);
    core.warning("Allowing agent execution because workflow run history could not be checked");
    core.setOutput("cooldown_ok", "true");
  }
}

module.exports = { agentJobStarted, main, parseRunCompletedAt, resolveWorkflowId };
