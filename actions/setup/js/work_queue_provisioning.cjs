// @ts-check
"use strict";

const { TextDecoder } = require("node:util");
const { posix } = require("node:path");
const { queueError } = require("./work_queue_codec.cjs");
const { validatePolicy, validateProfile } = require("./work_queue_policy.cjs");
const { readQueueMetadata, metadataRequest } = require("./work_queue_read_retry.cjs");
const log = require("./work_queue_logging.cjs").createWorkQueueLogger("provisioning");

const MAX_WORKFLOW_BYTES = 1024 * 1024;

/** @typedef {{rest: {repos: {getContent(options: {owner: string, repo: string, path: string, ref: string, request: object}): Promise<{data: unknown}>}, actions: {getWorkflow(options: {owner: string, repo: string, workflow_id: string, request: object}): Promise<{data: unknown}>}}}} RouteClient */
/** @typedef {ReturnType<typeof import("./work_queue_policy.cjs").defaultPolicy>["pools"]["default"]["profiles"]["default"]} WorkerProfile */

/** @param {unknown} value @returns {value is Record<string, unknown>} */
function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/**
 * @param {{githubClient: RouteClient, owner: string, repo: string, path: string, ref: string, sleepFn?: (delay: number) => Promise<void>}} options
 */
async function readWorkerFile({ githubClient, owner, repo, path, ref, sleepFn }) {
  let file;
  try {
    file = (await readQueueMetadata(() => metadataRequest(request => githubClient.rest.repos.getContent({ owner, repo, path, ref, request })), log, "worker.file.read", sleepFn))?.data;
  } catch (error) {
    if (error?.status !== 404) throw error;
    throw queueError("policy_missing", "approved worker route cannot be verified at its immutable revision");
  }
  if (!isRecord(file) || file.type !== "file" || file.path !== path || file.encoding !== "base64" || typeof file.content !== "string") throw queueError("policy_missing", "approved worker route is not an exact immutable workflow file");
  if (file.content.length > Math.ceil(MAX_WORKFLOW_BYTES / 3) * 4 + 65536) throw queueError("policy_missing", "approved worker route content exceeds the bounded workflow limit");
  const encoded = file.content.replace(/[\r\n]/g, "");
  if (encoded.length > Math.ceil(MAX_WORKFLOW_BYTES / 3) * 4 || !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(encoded))
    throw queueError("policy_missing", "approved worker route requires bounded valid base64 content");
  const bytes = Buffer.from(encoded, "base64");
  if (bytes.length > MAX_WORKFLOW_BYTES) throw queueError("policy_missing", "approved worker route content is oversized");
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    throw queueError("policy_missing", "approved worker route contains invalid UTF-8");
  }
}

/** @param {{githubClient: RouteClient, owner: string, repo: string, profile: WorkerProfile, sleepFn?: (delay: number) => Promise<void>}} options */
async function verifyWorkerRoute(options) {
  const { profile } = options;
  validateProfile(profile);
  if (/^(?:0{40}|0{64})$/.test(profile.ref)) throw queueError("policy_missing", "constructor placeholder revisions cannot provision a worker route");
  await readWorkerFile({ ...options, path: profile.workflow, ref: profile.ref });
  await verifyRegisteredWorker(options);
}

/** @param {{githubClient: RouteClient, owner: string, repo: string, profile: WorkerProfile, sleepFn?: (delay: number) => Promise<void>}} options */
async function verifyRegisteredWorker({ githubClient, owner, repo, profile, sleepFn }) {
  let registration;
  try {
    registration = (await readQueueMetadata(() => metadataRequest(request => githubClient.rest.actions.getWorkflow({ owner, repo, workflow_id: posix.basename(profile.workflow), request })), log, "worker.registration.read", sleepFn))?.data;
  } catch (error) {
    if (error?.status !== 404) throw error;
    throw queueError("policy_missing", "approved worker route registration cannot be verified");
  }

  if (!isRecord(registration) || registration.path !== profile.workflow || registration.state !== "active") throw queueError("policy_missing", "approved worker route must be active at its exact registered path");
}

function workerContract(contents) {
  const prefix = "# gh-aw-manifest: ";
  const manifestLines = contents.match(/^# gh-aw-manifest: .+$/gm) ?? [];
  if (manifestLines.length !== 1) return undefined;
  let manifest;
  try {
    manifest = JSON.parse(manifestLines[0].slice(prefix.length));
  } catch {
    return undefined;
  }
  if (!isRecord(manifest) || !isRecord(manifest.work_queue) || manifest.work_queue.worker !== true) return undefined;
  const contract = manifest.work_queue.logical_contract;
  return typeof contract === "string" && /^[a-f0-9]{64}$/.test(contract) ? contract : undefined;
}

async function verifiedDefaultReference({ githubClient, owner, repo, sleepFn = delay => new Promise(resolve => setTimeout(resolve, delay)) }) {
  const parameters = { owner, repo };
  const repository = (await readQueueMetadata(() => metadataRequest(request => githubClient.rest.repos.get({ ...parameters, request })), log, "deployment.repository.read", sleepFn))?.data;
  const branch = repository?.default_branch;
  if (typeof repository?.full_name !== "string" || repository.full_name.toLowerCase() !== `${owner}/${repo}`.toLowerCase() || typeof branch !== "string" || !/^[A-Za-z0-9][A-Za-z0-9._/-]*$/.test(branch) || branch.includes(".."))
    throw queueError("policy_invalid", "AW deployment requires a verified repository and default worker revision");
  const reference = (await readQueueMetadata(() => metadataRequest(request => githubClient.rest.git.getRef({ ...parameters, ref: `heads/${branch}`, request })), log, "deployment.reference.read", sleepFn))?.data;
  const revision = reference?.object?.sha;
  if (reference?.ref !== `refs/heads/${branch}` || typeof revision !== "string" || !/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(revision) || /^0+$/.test(revision)) throw queueError("policy_invalid", "default worker revision is not immutable");
  return revision;
}

// Ref and contract describe approved code; all authority stays in the installed
// profile. Missing default artifacts retain the current identity but pause it.
async function configuredWorkerDeployment(options, revision) {
  const current = options.profile;
  let profile = current;
  try {
    const contents = await readWorkerFile({ ...options, path: current.workflow, ref: revision });
    const contract = workerContract(contents);
    if (!contract) return { profile, available: false };
    profile = { ...current, ref: revision, logical_contract: contract };
    await verifyRegisteredWorker({ ...options, profile });
    return { profile, available: true };
  } catch (error) {
    if (error?.code !== "policy_missing") throw error;
    return { profile, available: false };
  }
}

/** @param {{githubClient: RouteClient, owner: string, repo: string, policy: ReturnType<typeof import("./work_queue_policy.cjs").defaultPolicy> | null}} options */
async function verifyWorkerRoutes({ githubClient, owner, repo, policy }) {
  if (!policy) throw queueError("policy_missing", "Policy must precede worker routing");
  validatePolicy(policy);
  if (policy.authorization === "aw") return;
  const seen = new Set();
  for (const pool of Object.values(policy.pools))
    for (const profile of Object.values(pool.profiles)) {
      const key = `${profile.workflow}\n${profile.ref}`;
      if (seen.has(key)) continue;
      await verifyWorkerRoute({ githubClient, owner, repo, profile });
      seen.add(key);
    }
}

module.exports = { MAX_WORKFLOW_BYTES, verifyWorkerRoute, verifyWorkerRoutes, workerContract, verifiedDefaultReference, configuredWorkerDeployment };
