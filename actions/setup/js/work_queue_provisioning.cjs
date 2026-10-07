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
function assignmentInputType(contents) {
  /** @type {unknown} */
  let workflow;
  let parseDocument;
  try {
    ({ parseDocument } = require("./work_queue_yaml.cjs"));
  } catch {
    throw queueError("policy_missing", "worker route provisioning requires the deployed YAML parser");
  }
  try {
    const document = parseDocument(contents, { uniqueKeys: true, merge: true });
    if (!document || document.errors.length || document.warnings.length) throw new Error("invalid workflow YAML");
    workflow = document.toJS({ mapAsMap: true, maxAliasCount: 100 });
  } catch {
    throw queueError("policy_missing", "approved worker route has invalid or unbounded workflow YAML");
  }
  let value = workflow;
  for (const key of ["on", "workflow_dispatch", "inputs", "work_queue_assignment", "type"]) {
    if (!(value instanceof Map)) return undefined;
    value = value.get(key);
  }
  return value;
}

/**
 * @param {{githubClient: RouteClient, owner: string, repo: string, profile: WorkerProfile}} options
 */
async function verifyWorkerRoute({ githubClient, owner, repo, profile }) {
  validateProfile(profile);
  if (/^(?:0{40}|0{64})$/.test(profile.ref)) throw queueError("policy_missing", "constructor placeholder revisions cannot provision a worker route");
  let file;
  try {
    file = (await githubClient.rest.repos.getContent({ owner, repo, path: profile.workflow, ref: profile.ref }))?.data;
  } catch {
    throw queueError("policy_missing", "approved worker route cannot be verified at its immutable revision");
  }
  if (!isRecord(file) || file.type !== "file" || file.path !== profile.workflow || file.encoding !== "base64" || typeof file.content !== "string")
    throw queueError("policy_missing", "approved worker route is not an exact immutable workflow file");
  if (file.content.length > Math.ceil(MAX_WORKFLOW_BYTES / 3) * 4 + 65536) throw queueError("policy_missing", "approved worker route content exceeds the bounded workflow limit");
  const encoded = file.content.replace(/[\r\n]/g, "");
  if (encoded.length > Math.ceil(MAX_WORKFLOW_BYTES / 3) * 4 || !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(encoded))
    throw queueError("policy_missing", "approved worker route requires bounded valid base64 content");
  const bytes = Buffer.from(encoded, "base64");
  if (bytes.length > MAX_WORKFLOW_BYTES) throw queueError("policy_missing", "approved worker route content is oversized");
  let contents;
  try {
    contents = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    throw queueError("policy_missing", "approved worker route contains invalid UTF-8");
  }
  if (assignmentInputType(contents) !== "string") throw queueError("policy_missing", "approved worker route must accept the work_queue_assignment string input");
  let registration;
  try {
    registration = (await githubClient.rest.actions.getWorkflow({ owner, repo, workflow_id: posix.basename(profile.workflow) }))?.data;
  } catch {
    throw queueError("policy_missing", "approved worker route registration cannot be verified");
  }
  if (!isRecord(registration) || registration.path !== profile.workflow || registration.state !== "active") throw queueError("policy_missing", "approved worker route must be active at its exact registered path");
}

/** @param {{githubClient: RouteClient, owner: string, repo: string, policy: ReturnType<typeof import("./work_queue_policy.cjs").defaultPolicy> | null}} options */
async function verifyWorkerRoutes({ githubClient, owner, repo, policy }) {
  if (!policy) throw queueError("policy_missing", "Policy must precede worker routing");
  validatePolicy(policy);
  const seen = new Set();
  for (const pool of Object.values(policy.pools))
    for (const profile of Object.values(pool.profiles)) {
      const key = `${profile.workflow}\n${profile.ref}`;
      if (seen.has(key)) continue;
      await verifyWorkerRoute({ githubClient, owner, repo, profile });
      seen.add(key);
    }
}

module.exports = { MAX_WORKFLOW_BYTES, verifyWorkerRoute, verifyWorkerRoutes };
