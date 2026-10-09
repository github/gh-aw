// @ts-check
/// <reference types="@actions/github-script" />
"use strict";

const fs = require("node:fs");
const { createHash } = require("node:crypto");
const { canonical, parseStrictJSON, queueError } = require("./work_queue_codec.cjs");
const { compactTransactions, replayTransactions } = require("./work_queue_replay.cjs");
const { readWorkQueueLog } = require("./work_queue_store.cjs");
const { actorFromContext, validateTrustedContext } = require("./work_queue_policy.cjs");
const { authenticatePublisher } = require("./work_queue_native.cjs");

const MAX_ATTEMPTS = 3;
const PLAN_MAX_BYTES = 4096;

function readPlan(file) {
  let contents;
  try {
    if (fs.statSync(file).size > PLAN_MAX_BYTES) throw queueError("checkpoint_invalid", "work queue compaction plan exceeds size limit");
    contents = fs.readFileSync(file, "utf8");
  } catch (error) {
    throw new Error("Unable to read work queue compaction plan", { cause: error });
  }
  const plan = parseStrictJSON(contents);
  if (
    !plan ||
    Object.keys(plan).sort().join(",") !== "base_sha,branch,plan_id,tip,version" ||
    plan.version !== 1 ||
    plan.branch !== "work-queue" ||
    !/^[a-f0-9]{40}$|^[a-f0-9]{64}$/.test(plan.base_sha) ||
    typeof plan.tip !== "string" ||
    !/^[a-f0-9]{64}$/.test(plan.plan_id)
  )
    throw queueError("checkpoint_invalid", "invalid work queue compaction plan");
  const { plan_id, ...base } = plan;
  if (createHash("sha256").update(canonical(base)).digest("hex") !== plan_id) throw queueError("checkpoint_invalid", "work queue compaction plan identity mismatch");
  return plan;
}

function isRetryablePublicationError(error) {
  if (!error || typeof error !== "object") return false;
  const status = "status" in error ? error.status : undefined;
  const message = error instanceof Error ? error.message : "";
  return status === 409 || (status === 422 && /not a fast.forward|reference update failed/i.test(message)) || status === 408 || status === 429 || (typeof status === "number" && status >= 500 && status < 600);
}

async function main(options = {}) {
  const context = options.context || global.context;
  const githubClient = options.githubClient || github;
  const owner = options.owner || context.repo.owner;
  const repo = options.repo || context.repo.repo;
  const file = options.planFile || process.env.GH_AW_WORK_QUEUE_COMPACTION_PLAN_FILE;
  if (!file) throw new Error("Missing work queue compaction plan path");
  const plan = readPlan(file);
  const trustedContext = await authenticatePublisher({ ...options, githubClient, context, owner, repo, role: "administrator" });
  const actor = actorFromContext(trustedContext);
  validateTrustedContext(trustedContext, actor);
  if (options.actor !== undefined) validateTrustedContext(trustedContext, options.actor);
  const sleep = options.sleep || (delay => new Promise(resolve => setTimeout(resolve, delay)));
  core.setOutput("result", "deferred");
  for (let attempt = 0; attempt < MAX_ATTEMPTS; attempt++) {
    const current = await readWorkQueueLog({ githubClient, owner, repo, branch: plan.branch });
    if (current.sha !== plan.base_sha || current.state.tip !== plan.tip) {
      core.info("Work queue compaction deferred: queue changed since planning");
      return { status: "deferred" };
    }
    const candidate = compactTransactions(current.transactions, current.sha, actor);
    replayTransactions(candidate);
    const content = candidate.map(commit => canonical(commit)).join("\n") + "\n";
    try {
      const blob = await githubClient.rest.git.createBlob({ owner, repo, content, encoding: "utf-8" });
      const tree = await githubClient.rest.git.createTree({
        owner,
        repo,
        base_tree: current.treeSha,
        tree: [{ path: current.logPath, mode: "100644", type: "blob", sha: blob.data.sha }],
      });
      const commit = await githubClient.rest.git.createCommit({
        owner,
        repo,
        message: "Checkpoint work queue",
        tree: tree.data.sha,
        parents: [current.sha],
      });
      await githubClient.rest.git.updateRef({ owner, repo, ref: `heads/${plan.branch}`, sha: commit.data.sha, force: false });
      core.setOutput("result", "applied");
      core.info("Work queue checkpoint committed");
      return { status: "applied" };
    } catch (error) {
      if (!isRetryablePublicationError(error)) throw error;
      const latest = await readWorkQueueLog({ githubClient, owner, repo, branch: plan.branch });
      if (latest.transactions[0]?.operations[0]?.kind === "Checkpoint" && latest.transactions[0].operations[0].prior_git_sha === plan.base_sha) {
        core.setOutput("result", "applied");
        return { status: "applied" };
      }
      if (latest.sha !== plan.base_sha || latest.state.tip !== plan.tip) {
        core.info("Work queue compaction deferred after concurrent publication");
        return { status: "deferred" };
      }
      if (attempt === MAX_ATTEMPTS - 1) {
        core.warning("Work queue compaction deferred after bounded publication retries");
        return { status: "deferred" };
      }
      await sleep((attempt + 1) * 250);
    }
  }
  return { status: "deferred" };
}

if (require.main === module) main().catch(error => core.setFailed(error instanceof Error ? error.message : "Work queue compaction failed"));

module.exports = { isRetryablePublicationError, main, readPlan };
