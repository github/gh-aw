// @ts-check
"use strict";
const log = require("./work_queue_logging.cjs").createWorkQueueLogger("native");
const { posix } = require("node:path");

const { dispatchWorkflow } = require("./dispatch_workflow_transport.cjs");

const API_VERSION = "2026-03-10";

function nativeId(value, name = "native ID") {
  if (typeof value === "number" && Number.isSafeInteger(value) && value > 0) return String(value);
  if (typeof value === "string" && value.length <= 256 && /^[1-9][0-9]*$/.test(value)) return value;
  throw new TypeError(`${name} must be a lossless positive canonical integer`);
}

/**
 * POST one worker launch as workflow_dispatch at `ref` (a branch or commit ref; no tag
 * pinning). Any successful response is an accepted launch; errors propagate.
 * @param {any} githubClient
 * @param {{owner: string, repo: string, workflow: string, ref: string}} destination
 * @param {Record<string, string>} inputs
 */
async function postQueueDispatch(githubClient, { owner, repo, workflow, ref }, inputs) {
  if (typeof ref !== "string" || !ref) throw new Error("work_queue_dispatch_ref_missing");
  log.debug("dispatch.post.start");
  const response = await dispatchWorkflow(githubClient, {
    owner,
    repo,
    workflow_id: posix.basename(workflow),
    ref,
    inputs,
    headers: { "X-GitHub-Api-Version": API_VERSION },
    request: { retries: 0, retryCount: 0, timeout: 30000 },
  });
  log.debug("dispatch.post.accepted");
  return response;
}

/**
 * Receipt reason for a failed launch POST.
 * @param {unknown} error
 * @returns {"http_4xx" | "http_5xx" | "transport"}
 */
function launchFailureReason(error) {
  const status = error && typeof error === "object" && "status" in error ? error.status : undefined;
  if (typeof status === "number" && status >= 400 && status < 500) return "http_4xx";
  if (typeof status === "number" && status >= 500 && status < 600) return "http_5xx";
  return "transport";
}

module.exports = { API_VERSION, launchFailureReason, nativeId, postQueueDispatch };
