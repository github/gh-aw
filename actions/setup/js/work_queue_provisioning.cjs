// @ts-check
"use strict";

const { TextDecoder } = require("node:util");
const { posix } = require("node:path");
const { queueError } = require("./work_queue_codec.cjs");
const { validatePolicy, validateProfile } = require("./work_queue_policy.cjs");

const MAX_WORKFLOW_BYTES = 1024 * 1024;

/** @typedef {{rest: {repos: {getContent(options: {owner: string, repo: string, path: string, ref: string}): Promise<{data: unknown}>}, actions: {getWorkflow(options: {owner: string, repo: string, workflow_id: string}): Promise<{data: unknown}>}}}} RouteClient */
/** @typedef {ReturnType<typeof import("./work_queue_policy.cjs").defaultPolicy>["pools"]["default"]["profiles"]["default"]} WorkerProfile */

/** @param {unknown} value @returns {value is Record<string, unknown>} */
function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** @param {string} contents */
function parsedWorkflow(contents) {
  let parseDocument;
  try {
    ({ parseDocument } = require("./work_queue_yaml.cjs"));
  } catch {
    throw queueError("policy_missing", "worker route provisioning requires the deployed YAML parser");
  }
  try {
    const document = parseDocument(contents, { uniqueKeys: true, merge: true });
    if (!document || document.errors.length || document.warnings.length) throw new Error("invalid workflow YAML");
    return document.toJS({ mapAsMap: true, maxAliasCount: 100 });
  } catch {
    throw queueError("policy_missing", "approved worker route has invalid or unbounded workflow YAML");
  }
}

/** @param {string} contents */
function assignmentInputType(contents) {
  let value = parsedWorkflow(contents);
  for (const key of ["on", "workflow_dispatch", "inputs", "work_queue_assignment", "type"]) {
    if (!(value instanceof Map)) return undefined;
    value = value.get(key);
  }
  return value;
}

/**
 * @param {{githubClient: RouteClient, owner: string, repo: string, path: string, ref: string}} options
 */
async function readWorkerFile({ githubClient, owner, repo, path, ref }) {
  let file;
  try {
    file = (await githubClient.rest.repos.getContent({ owner, repo, path, ref }))?.data;
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

/** @param {{githubClient: RouteClient, owner: string, repo: string, profile: WorkerProfile}} options */
async function verifyWorkerRoute(options) {
  const { profile } = options;
  validateProfile(profile);
  if (/^(?:0{40}|0{64})$/.test(profile.ref)) throw queueError("policy_missing", "constructor placeholder revisions cannot provision a worker route");
  const contents = await readWorkerFile({ ...options, path: profile.workflow, ref: profile.ref });
  if (assignmentInputType(contents) !== "string") throw queueError("policy_missing", "approved worker route must accept the work_queue_assignment string input");
  if (profile.logical_contract && workerContract(contents) !== profile.logical_contract) throw queueError("policy_missing", "worker route does not carry its compiler-derived logical contract");
  await verifyRegisteredWorker(options);
}

/** @param {{githubClient: RouteClient, owner: string, repo: string, profile: WorkerProfile}} options */
async function verifyRegisteredWorker({ githubClient, owner, repo, profile }) {
  let registration;
  try {
    registration = (await githubClient.rest.actions.getWorkflow({ owner, repo, workflow_id: posix.basename(profile.workflow) }))?.data;
  } catch (error) {
    if (error?.status !== 404) throw error;
    throw queueError("policy_missing", "approved worker route registration cannot be verified");
  }

  if (!isRecord(registration) || registration.path !== profile.workflow || registration.state !== "active") throw queueError("policy_missing", "approved worker route must be active at its exact registered path");
}

function workerContract(contents) {
  const workflow = parsedWorkflow(contents);
  if (!(workflow instanceof Map)) return undefined;
  const environments = [workflow.get("env")];
  const jobs = workflow.get("jobs");
  if (jobs instanceof Map)
    for (const job of jobs.values()) {
      if (!(job instanceof Map)) continue;
      environments.push(job.get("env"));
      const steps = job.get("steps");
      if (Array.isArray(steps)) for (const step of steps) if (step instanceof Map) environments.push(step.get("env"));
    }
  const contracts = new Set();
  for (const env of environments) {
    if (!(env instanceof Map) || !env.has("GH_AW_WORK_QUEUE_CONTRACT")) continue;
    const stamp = env.get("GH_AW_WORK_QUEUE_CONTRACT");
    if (typeof stamp !== "string" || !/^[a-f0-9]{64}$/.test(stamp)) return undefined;
    contracts.add(stamp);
  }
  return contracts.size === 1 ? [...contracts][0] : undefined;
}

async function verifiedDefaultReference({ githubClient, owner, repo }) {
  const repository = (await githubClient.rest.repos.get({ owner, repo }))?.data;
  const branch = repository?.default_branch;
  if (typeof repository?.full_name !== "string" || repository.full_name.toLowerCase() !== `${owner}/${repo}`.toLowerCase() || typeof branch !== "string" || !/^[A-Za-z0-9][A-Za-z0-9._/-]*$/.test(branch) || branch.includes(".."))
    throw queueError("policy_invalid", "AW deployment requires a verified repository and default worker revision");
  const reference = (await githubClient.rest.git.getRef({ owner, repo, ref: `heads/${branch}` }))?.data;
  const revision = reference?.object?.sha;
  if (reference?.ref !== `refs/heads/${branch}` || typeof revision !== "string" || !/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(revision) || /^0+$/.test(revision)) throw queueError("policy_invalid", "default worker revision is not immutable");
  return revision;
}

function sourceIsWorker(contents) {
  const lines = contents.split("\n");
  if (lines[0]?.trim() !== "---") return false;
  const end = lines.findIndex((line, index) => index > 0 && line.trim() === "---");
  if (end < 0) return false;
  let value = parsedWorkflow(lines.slice(1, end).join("\n"));
  for (const key of ["tools", "work-queue", "worker"]) {
    if (!(value instanceof Map)) return false;
    value = value.get(key);
  }
  return value === true;
}

// Ref and contract describe approved code; all authority stays in the installed
// profile. Missing default artifacts retain the current identity but pause it.
async function configuredWorkerDeployment(options, revision) {
  const current = options.profile;
  let profile = current;
  try {
    const source = current.workflow.replace(/\.lock\.yml$/, ".md");
    if (!sourceIsWorker(await readWorkerFile({ ...options, path: source, ref: revision }))) return { profile, available: false };
    const contents = await readWorkerFile({ ...options, path: current.workflow, ref: revision });
    const contract = workerContract(contents);
    if (assignmentInputType(contents) !== "string" || !contract) return { profile, available: false };
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
