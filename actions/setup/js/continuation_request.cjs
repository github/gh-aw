// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { createHash } = require("node:crypto");
const { getErrorMessage } = require("./error_helpers.cjs");

/** Artifact name prefix for continuation requests; the parent run attempt is appended. */
const CONTINUATION_REQUEST_ARTIFACT_PREFIX = "continuation-request-";
/** File name inside the continuation request artifact. */
const CONTINUATION_REQUEST_FILE_NAME = "continuation-request.json";
const CONTINUATION_REQUEST_VERSION = 1;
const MAX_CONTINUATION_HINTS_LENGTH = 4000;
const POSITIVE_INTEGER = /^[1-9]\d*$/;

/**
 * @param {string | number} attempt
 * @returns {string}
 */
function requestArtifactName(attempt) {
  return `${CONTINUATION_REQUEST_ARTIFACT_PREFIX}${attempt}`;
}

/**
 * Returns whether an Actions artifact is still downloadable.
 * @param {{expired?: boolean, expires_at?: string | null}} artifact
 * @param {number} [now]
 * @returns {boolean}
 */
function isUnexpired(artifact, now = Date.now()) {
  if (!artifact || artifact.expired) return false;
  if (!artifact.expires_at) return true;
  const expiresAt = new Date(artifact.expires_at).getTime();
  if (!Number.isFinite(expiresAt)) throw new Error("Continuation artifact has an invalid expiration date");
  return expiresAt > now;
}

/**
 * Finds the unexpired continuation request of a parent run attempt. Without an
 * explicit attempt, the latest attempt that recorded a request is selected.
 * @param {Array<any>} artifacts
 * @param {string | number | undefined} [attempt]
 * @returns {{artifact: any, attempt: string} | undefined}
 */
function findRequestArtifact(artifacts, attempt) {
  const candidates = artifacts
    .filter(item => typeof item?.name === "string" && item.name.startsWith(CONTINUATION_REQUEST_ARTIFACT_PREFIX))
    .map(item => ({ artifact: item, attempt: item.name.slice(CONTINUATION_REQUEST_ARTIFACT_PREFIX.length) }))
    .filter(item => POSITIVE_INTEGER.test(item.attempt) && (attempt === undefined || item.attempt === String(attempt)) && isUnexpired(item.artifact))
    .sort((a, b) => Number(b.attempt) - Number(a.attempt) || b.artifact.id - a.artifact.id);
  return candidates[0];
}

/**
 * Computes the checkpoint artifact name prefix shared by every run of a durable
 * workflow on one branch: durable-<workflow>-<scope>-.
 * @param {string} workflow
 * @param {{repository: {id: number}, workflow_id: number, head_branch: string | null}} run
 * @returns {string}
 */
function checkpointArtifactPrefix(workflow, run) {
  if (!workflow || !/^[A-Za-z0-9_-]+$/.test(workflow)) throw new Error("Invalid durable workflow identifier");
  const scope = createHash("sha256")
    .update(JSON.stringify([run.repository.id, run.workflow_id, run.head_branch]))
    .digest("hex")
    .slice(0, 24);
  return `durable-${workflow}-${scope}-`;
}

/**
 * Finds the unexpired checkpoint uploaded by one attempt of a run.
 * @param {Array<any>} artifacts
 * @param {string} prefix
 * @param {string | number} runId
 * @param {string | number} attempt
 */
function findCheckpointArtifact(artifacts, prefix, runId, attempt) {
  const name = `${prefix}${runId}-${attempt}`;
  return artifacts.filter(item => item?.name === name && isUnexpired(item)).sort((a, b) => b.id - a.id)[0];
}

/**
 * Validates a continuation request recorded by the request_continuation handler.
 * @param {unknown} raw
 * @param {{parentRunId: string, parentRunAttempt: string, max?: number}} expected
 * @returns {{version: number, hints: string, depth: number, max: number, parent_run_id: string, parent_run_attempt: string, aw_context: Record<string, unknown>}}
 */
function validateContinuationRequest(raw, { parentRunId, parentRunAttempt, max }) {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) throw new Error("Continuation request must be a JSON object");
  /** @type {Record<string, any>} */
  const request = Object.assign({}, raw);
  if (request.version !== CONTINUATION_REQUEST_VERSION) throw new Error(`Unsupported continuation request version ${JSON.stringify(request.version)}`);
  if (String(request.parent_run_id) !== String(parentRunId) || String(request.parent_run_attempt) !== String(parentRunAttempt)) {
    throw new Error("Continuation request does not belong to the parent run attempt");
  }
  if (typeof request.hints !== "string" || !request.hints.trim() || request.hints.length > MAX_CONTINUATION_HINTS_LENGTH) {
    throw new Error(`Continuation request hints must be a non-empty string of at most ${MAX_CONTINUATION_HINTS_LENGTH} characters`);
  }
  const depth = Number(request.depth);
  const requestMax = Number(request.max);
  if (!Number.isInteger(depth) || depth < 1) throw new Error("Continuation request depth must be a positive integer");
  if (!Number.isInteger(requestMax) || requestMax < 1) throw new Error("Continuation request max must be a positive integer");
  const limit = max ?? requestMax;
  if (depth > limit) throw new Error(`Continuation depth ${depth} exceeds the configured maximum of ${limit}`);
  const awContext = request.aw_context && typeof request.aw_context === "object" && !Array.isArray(request.aw_context) ? request.aw_context : {};
  return { version: CONTINUATION_REQUEST_VERSION, hints: request.hints, depth, max: requestMax, parent_run_id: String(parentRunId), parent_run_attempt: String(parentRunAttempt), aw_context: awContext };
}

/**
 * Downloads and parses a continuation request artifact.
 * @param {{artifactClient: any, artifactId: number, token: string, owner: string, repo: string, directory: string}} options
 * @returns {Promise<unknown>}
 */
async function downloadContinuationRequest({ artifactClient, artifactId, token, owner, repo, directory }) {
  removeDirectory(directory);
  try {
    await artifactClient.downloadArtifact(Number(artifactId), { path: directory, findBy: { token, repositoryOwner: owner, repositoryName: repo } });
    const file = path.join(directory, CONTINUATION_REQUEST_FILE_NAME);
    try {
      return JSON.parse(fs.readFileSync(file, "utf8"));
    } catch (error) {
      throw new Error(`Failed to read continuation request ${file}: ${getErrorMessage(error)}`, { cause: error });
    }
  } finally {
    removeDirectory(directory);
  }
}

/**
 * @param {string} directory
 */
function removeDirectory(directory) {
  try {
    fs.rmSync(directory, { recursive: true, force: true });
  } catch (error) {
    throw new Error(`Failed to remove ${directory}: ${getErrorMessage(error)}`, { cause: error });
  }
}

/** Workflow path of the generated continuation relay; the only trusted continuation dispatcher. */
const RELAY_WORKFLOW_PATH = ".github/workflows/agentic-continuation-relay.yml";
/** Identity of GITHUB_TOKEN; the conclusion job dispatches the relay and the relay dispatches the child with it. */
const GITHUB_ACTIONS_BOT = "github-actions[bot]";

/**
 * Builds the deterministic relay run name; the generated workflow uses the same format.
 * @param {string | number} parentRunId
 * @param {string | number} parentRunAttempt
 */
function relayRunTitle(parentRunId, parentRunAttempt) {
  return `Continuation relay ${parentRunId}/${parentRunAttempt}`;
}

/**
 * Resolves the repository default branch from the event payload, falling back to the API.
 * @param {any} github
 * @param {any} context
 * @returns {Promise<string>}
 */
async function getDefaultBranch(github, context) {
  const fromPayload = context.payload?.repository?.default_branch;
  if (typeof fromPayload === "string" && fromPayload) return fromPayload;
  const { data } = await github.rest.repos.get({ owner: context.repo.owner, repo: context.repo.repo });
  if (typeof data?.default_branch !== "string" || !data.default_branch) throw new Error("Could not resolve the repository default branch");
  return data.default_branch;
}

/**
 * Verifies that a workflow run is an authentic continuation relay run: a workflow_dispatch
 * run of the generated relay on the default branch of this repository, bound to the given
 * parent run attempt through its deterministic run name. This rejects relay copies run
 * from other branches or forks and lineage that names an unrelated run.
 * @param {any} relay
 * @param {{repositoryId: number, defaultBranch: string, parentRunId: string | number, parentRunAttempt: string | number}} expected
 */
function validateRelayRun(relay, { repositoryId, defaultBranch, parentRunId, parentRunAttempt }) {
  if (!relay || relay.repository?.id !== repositoryId || relay.head_repository?.id !== repositoryId) throw new Error("The continuation relay run must belong to this repository and not a fork");
  if (relay.event !== "workflow_dispatch") throw new Error(`The continuation relay run must be a workflow_dispatch run, not ${JSON.stringify(relay.event)}`);
  if (relay.path !== RELAY_WORKFLOW_PATH) throw new Error(`The continuation relay run must come from ${RELAY_WORKFLOW_PATH}`);
  if (relay.head_branch !== defaultBranch) throw new Error(`The continuation relay must run from the default branch ${JSON.stringify(defaultBranch)}, not ${JSON.stringify(relay.head_branch)}`);
  if (relay.display_title !== relayRunTitle(parentRunId, parentRunAttempt)) throw new Error(`The continuation relay run is not bound to run ${parentRunId} attempt ${parentRunAttempt}`);
}

module.exports = {
  RELAY_WORKFLOW_PATH,
  GITHUB_ACTIONS_BOT,
  relayRunTitle,
  getDefaultBranch,
  validateRelayRun,
  CONTINUATION_REQUEST_ARTIFACT_PREFIX,
  CONTINUATION_REQUEST_FILE_NAME,
  CONTINUATION_REQUEST_VERSION,
  MAX_CONTINUATION_HINTS_LENGTH,
  requestArtifactName,
  isUnexpired,
  findRequestArtifact,
  checkpointArtifactPrefix,
  findCheckpointArtifact,
  validateContinuationRequest,
  downloadContinuationRequest,
};
