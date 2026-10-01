// @ts-check
"use strict";

const fs = require("node:fs");
const { DispatchWorkCoordinator } = require("./dispatch_work_coordinator_branch.cjs");
const { createDispatchWorkCoordinatorGitHubClient } = require("./dispatch_work_coordinator_github_client.cjs");

async function runDispatchWorkCoordinatorActivation(env = process.env) {
  const [owner, repo] = (env.GITHUB_REPOSITORY || "").split("/");
  const workflowRef = env.GITHUB_WORKFLOW_REF || "";
  if (!owner || !repo || !env.GITHUB_RUN_ID || !workflowRef || !env.GITHUB_OUTPUT) {
    throw new TypeError("Dispatch Work Coordinator activation configuration is incomplete");
  }
  const coordinator = new DispatchWorkCoordinator({
    githubClient: createDispatchWorkCoordinatorGitHubClient(env.GH_AW_DISPATCH_WORK_COORDINATOR_TOKEN, env.GITHUB_API_URL),
    owner,
    repo,
    identity: workflowRef.split("@", 1)[0],
    runId: env.GITHUB_RUN_ID,
    workflowId: workflowRef,
  });
  const assignment = await coordinator.claimNext();
  const trustedAssignment = assignment?.assigned ? { work_id: assignment.work_id, claim_id: assignment.claim_id, work: assignment.work } : null;
  fs.appendFileSync(env.GITHUB_OUTPUT, `assignment=${JSON.stringify(trustedAssignment)}\n`, { encoding: "utf8", mode: 0o600 });
  return trustedAssignment;
}

if (require.main === module) {
  runDispatchWorkCoordinatorActivation().catch(() => {
    console.error("Dispatch Work Coordinator activation failed; agent execution is blocked.");
    process.exitCode = 1;
  });
}

module.exports = { runDispatchWorkCoordinatorActivation };
