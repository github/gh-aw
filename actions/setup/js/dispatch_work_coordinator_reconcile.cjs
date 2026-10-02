// @ts-check
"use strict";

const { DispatchWorkCoordinator, resolveCoordinatorIdentity } = require("./dispatch_work_coordinator_branch.cjs");
const { createDispatchWorkCoordinatorGitHubClient } = require("./dispatch_work_coordinator_github_client.cjs");
const { deriveWorkId, validateTransaction } = require("./dispatch_work_coordinator.cjs");

function parseTrustedAssignment(raw) {
  if (typeof raw !== "string" || !raw.trim() || raw.trim() === "null") return null;
  let assignment;
  try {
    assignment = JSON.parse(raw);
  } catch {
    throw new TypeError("Trusted Dispatch Work Coordinator context is invalid");
  }
  if (!assignment || typeof assignment !== "object" || Array.isArray(assignment)) {
    throw new TypeError("Trusted Dispatch Work Coordinator context is invalid");
  }
  const keys = Object.keys(assignment).sort();
  if (
    keys.join(",") !== "claim_id,work,work_id" ||
    typeof assignment.claim_id !== "string" ||
    !/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(assignment.claim_id) ||
    typeof assignment.work_id !== "string" ||
    !assignment.work ||
    typeof assignment.work !== "object" ||
    Array.isArray(assignment.work)
  ) {
    throw new TypeError("Trusted Dispatch Work Coordinator context is invalid");
  }
  if (deriveWorkId(assignment.work) !== assignment.work_id) {
    throw new TypeError("Trusted Dispatch Work Coordinator context does not match its Work payload");
  }
  return assignment;
}

function createCoordinator(env) {
  const [owner, repo] = (env.GITHUB_REPOSITORY || "").split("/");
  const workflowRef = env.GITHUB_WORKFLOW_REF || "";
  const schemaJSON = env.GH_AW_DISPATCH_WORK_COORDINATOR_SCHEMA;
  if (!owner || !repo || !workflowRef || !env.GITHUB_RUN_ID || typeof schemaJSON !== "string" || Buffer.byteLength(schemaJSON, "utf8") > 16 * 1024) {
    throw new TypeError("Dispatch Work Coordinator safe-output configuration is incomplete");
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
  return new DispatchWorkCoordinator({
    githubClient: createDispatchWorkCoordinatorGitHubClient(env.GH_AW_DISPATCH_WORK_COORDINATOR_TOKEN, env.GITHUB_API_URL),
    owner,
    repo,
    identity: resolveCoordinatorIdentity(workflowRef, env.GH_AW_DISPATCH_WORK_COORDINATOR_ID),
    runId: env.GITHUB_RUN_ID,
    workflowId: workflowRef,
    workSchema,
  });
}

async function reconcileDispatchWorkCoordinator({ messages, env = process.env, coordinatorFactory = createCoordinator }) {
  const finishes = messages.filter(message => message?.type === "dispatch_claim_finish");
  if (finishes.length > 1) throw new TypeError("Only one dispatch_claim_finish intent is allowed");
  const assignment = parseTrustedAssignment(env.GH_AW_DISPATCH_WORK_COORDINATOR_CONTEXT);
  if (!assignment) {
    if (finishes.length > 0) throw new TypeError("dispatch_claim_finish requires a trusted assigned Claim");
    return { forceStaged: false, finishAuthorized: false };
  }
  if (finishes.length > 0) {
    const finish = finishes[0];
    if (Object.keys(finish).some(key => key !== "type" && key !== "outcome")) {
      throw new TypeError("dispatch_claim_finish cannot contain Claim authority fields");
    }
    validateTransaction({ type: "Completion", claim_id: assignment.claim_id, ...(Object.hasOwn(finish, "outcome") ? { outcome: finish.outcome } : {}) });
  }

  const coordinator = coordinatorFactory(env);
  const workflowRef = env.GITHUB_WORKFLOW_REF;
  const snapshot = await coordinator.status();
  const work = snapshot.works.find(item => item.work_id === assignment.work_id);
  const claim = work?.claims.find(item => item.claim_id === assignment.claim_id);
  if (!claim || !work || claim.run_id !== env.GITHUB_RUN_ID || claim.workflow_id !== workflowRef || deriveWorkId(assignment.work) !== assignment.work_id) {
    throw new TypeError("Trusted Dispatch Work Coordinator Claim provenance could not be verified");
  }
  if (work.state !== "claimed" || work.effective_claim_id !== assignment.claim_id || claim.state !== "effective") {
    return { forceStaged: true, finishAuthorized: false, reason: "Claim is no longer effective" };
  }

  if (finishes.length === 0) {
    await coordinator.cancelClaim(assignment.claim_id);
    return { forceStaged: true, finishAuthorized: false, reason: "No dispatch_claim_finish intent was provided" };
  }

  await coordinator.finishClaim(assignment.claim_id, finishes[0].outcome);
  return { forceStaged: false, finishAuthorized: true };
}

module.exports = { parseTrustedAssignment, reconcileDispatchWorkCoordinator };
