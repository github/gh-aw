// @ts-check
/// <reference types="@actions/github-script" />
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { createHash } = require("node:crypto");
const { canonical } = require("./work_queue_codec.cjs");
const { readWorkQueueLog } = require("./work_queue_store.cjs");

function planFor(current) {
  if (!current.sha || current.transactions.length < 2) return null;
  const plan = { version: 1, branch: current.branch, base_sha: current.sha, tip: current.state.tip };
  return { ...plan, plan_id: createHash("sha256").update(canonical(plan)).digest("hex") };
}

async function main(options = {}) {
  const githubClient = options.githubClient || github;
  const owner = options.owner || context.repo.owner;
  const repo = options.repo || context.repo.repo;
  const planFile = options.planFile || process.env.GH_AW_WORK_QUEUE_COMPACTION_PLAN_FILE;
  if (!planFile) throw new Error("Missing work queue compaction plan path");
  core.setOutput("plan_created", "false");
  const current = await readWorkQueueLog({ githubClient, owner, repo });
  const plan = planFor(current);
  if (!plan) return { status: "skipped" };
  try {
    fs.mkdirSync(path.dirname(planFile), { recursive: true });
    fs.writeFileSync(planFile, `${canonical(plan)}\n`, { mode: 0o600 });
  } catch (error) {
    throw new Error("Unable to write work queue compaction plan", { cause: error });
  }
  core.setOutput("plan_created", "true");
  core.info(`Work queue compaction planned at ${plan.base_sha}`);
  return { status: "planned", plan };
}

if (require.main === module) main().catch(error => core.setFailed(error instanceof Error ? error.message : "Work queue compaction planning failed"));

module.exports = { main, planFor };
