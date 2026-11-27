// @ts-check
"use strict";
const log = require("./work_queue_logging.cjs").createWorkQueueLogger("native");
const { posix } = require("node:path");

const { dispatchWorkflow } = require("./dispatch_workflow_transport.cjs");
const { queueError } = require("./work_queue_codec.cjs");
const { LAUNCH_REVISION } = require("./work_queue_replay.cjs");

const API_VERSION = "2026-03-10";
const LAUNCH_UNVERIFIED = "work_queue_launch_unverified";

function nativeId(value, name = "native ID") {
  if (typeof value === "number" && Number.isSafeInteger(value) && value > 0) return String(value);
  if (typeof value === "string" && value.length <= 256 && /^[1-9][0-9]*$/.test(value)) return value;
  throw new TypeError(`${name} must be a lossless positive canonical integer`);
}

/**
 * POST one worker launch as workflow_dispatch at `ref` (a branch or commit ref; no tag
 * pinning), asking GitHub to return the created run. Errors propagate.
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
    return_run_details: true,
    headers: { "X-GitHub-Api-Version": API_VERSION },
    request: { retries: 0, retryCount: 0, timeout: 30000 },
  });
  log.debug("dispatch.post.accepted");
  return response;
}

/**
 * Read one workflow run of the repository through the Actions API.
 * @param {any} githubClient
 * @param {{owner: string, repo: string, run_id: string}} target
 */
async function readWorkflowRun(githubClient, { owner, repo, run_id }) {
  const response = await githubClient.rest.actions.getWorkflowRun({ owner, repo, run_id: Number(nativeId(run_id, "workflow run ID")), headers: { "X-GitHub-Api-Version": API_VERSION }, request: { timeout: 15000 } });
  return response?.data;
}

/**
 * Check a run read from the Actions API against launch provenance: the exact run, in this
 * repository, of `workflow`, started by workflow_dispatch at a full commit revision (and,
 * when given, at exactly `revision`). Throws work_queue_launch_unverified on any mismatch.
 * @param {any} run
 * @param {{owner: string, repo: string, run_id: string, workflow: string, revision?: string}} expected
 * @returns {string} the run's head revision
 */
function verifyLaunchedRun(run, { owner, repo, run_id, workflow, revision = undefined }) {
  const mismatch = (/** @type {string} */ field) => {
    log.debug("launch.provenance.mismatch", { [`${field}_mismatch`]: true });
    return queueError(LAUNCH_UNVERIFIED, `the workflow run's ${field} does not match the dispatcher's launch`);
  };
  if (!run || typeof run !== "object" || String(run.id) !== run_id) throw mismatch("identity");
  if (typeof run.repository?.full_name !== "string" || run.repository.full_name.toLowerCase() !== `${owner}/${repo}`.toLowerCase()) throw mismatch("repository");
  if (typeof run.path !== "string" || run.path.split("@")[0] !== workflow) throw mismatch("workflow");
  if (run.event !== "workflow_dispatch") throw mismatch("event");
  if (typeof run.head_sha !== "string" || !LAUNCH_REVISION.test(run.head_sha) || (revision !== undefined && run.head_sha !== revision)) throw mismatch("revision");
  return run.head_sha;
}

/**
 * Dispatcher-side launch evidence for an accepted POST: the run GitHub reported creating,
 * read back and verified as a workflow_dispatch run of the launched workflow. Without a
 * run ID (e.g. return_run_details unsupported) or a verified read there is no evidence.
 * @param {any} githubClient
 * @param {{owner: string, repo: string, workflow: string}} destination
 * @param {any} response the createWorkflowDispatch response
 * @returns {Promise<{run_id: string, revision: string}>}
 */
async function launchedRunEvidence(githubClient, { owner, repo, workflow }, response) {
  let runId;
  try {
    runId = nativeId(response?.data?.workflow_run_id, "workflow_run_id");
  } catch {
    throw queueError(LAUNCH_UNVERIFIED, "the launch response did not identify the created run");
  }
  let run;
  try {
    run = await readWorkflowRun(githubClient, { owner, repo, run_id: runId });
  } catch (error) {
    log.failure("launch.run.read_failed", error, true);
    throw queueError(LAUNCH_UNVERIFIED, "the launched run could not be read back");
  }
  return { run_id: runId, revision: verifyLaunchedRun(run, { owner, repo, run_id: runId, workflow }) };
}

/**
 * Receipt reason for a failed launch.
 * @param {unknown} error
 * @returns {"unverified" | "http_4xx" | "http_5xx" | "transport"}
 */
function launchFailureReason(error) {
  if (error && typeof error === "object" && "code" in error && error.code === LAUNCH_UNVERIFIED) return "unverified";
  const status = error && typeof error === "object" && "status" in error ? error.status : undefined;
  if (typeof status === "number" && status >= 400 && status < 500) return "http_4xx";
  if (typeof status === "number" && status >= 500 && status < 600) return "http_5xx";
  return "transport";
}

module.exports = { API_VERSION, LAUNCH_UNVERIFIED, launchFailureReason, launchedRunEvidence, nativeId, postQueueDispatch, readWorkflowRun, verifyLaunchedRun };
