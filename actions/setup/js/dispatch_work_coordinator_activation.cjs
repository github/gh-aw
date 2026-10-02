// @ts-check
"use strict";

const fs = require("node:fs");
const { DispatchWorkCoordinator, coordinatorIdentity } = require("./dispatch_work_coordinator_branch.cjs");
const { createDispatchWorkCoordinatorGitHubClient } = require("./dispatch_work_coordinator_github_client.cjs");

async function runDispatchWorkCoordinatorActivation(env = process.env) {
  const [owner, repo] = (env.GITHUB_REPOSITORY || "").split("/");
  const workflowRef = env.GITHUB_WORKFLOW_REF || "";
  const schemaJSON = env.GH_AW_DISPATCH_WORK_COORDINATOR_SCHEMA;
  if (!owner || !repo || !env.GITHUB_RUN_ID || !workflowRef || !env.GITHUB_OUTPUT || typeof schemaJSON !== "string" || Buffer.byteLength(schemaJSON, "utf8") > 16 * 1024) {
    throw new TypeError("Dispatch Work Coordinator activation configuration is incomplete");
  }
  let workSchema;
  try {
    workSchema = JSON.parse(schemaJSON);
  } catch (error) {
    throw new TypeError("Dispatch Work Coordinator Work schema is invalid", { cause: error });
  }
  if (!workSchema || typeof workSchema !== "object" || Array.isArray(workSchema) || workSchema.type !== "object") {
    throw new TypeError("Dispatch Work Coordinator Work schema is invalid");
  }
  const coordinator = new DispatchWorkCoordinator({
    githubClient: createDispatchWorkCoordinatorGitHubClient(env.GH_AW_DISPATCH_WORK_COORDINATOR_TOKEN, env.GITHUB_API_URL),
    owner,
    repo,
    identity: coordinatorIdentity({ owner, repo, workflowRef, coordinatorId: env.GH_AW_DISPATCH_WORK_COORDINATOR_ID }),
    runId: env.GITHUB_RUN_ID,
    workflowId: workflowRef,
    workSchema,
  });
  const assignment = await coordinator.claimNext();
  if (env.GH_AW_DISPATCH_WORK_COORDINATOR_REQUIRE_ASSIGNMENT === "true" && !assignment?.assigned) {
    throw new Error("Dispatch Work Coordinator worker has no available assignment");
  }
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
