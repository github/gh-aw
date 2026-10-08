// @ts-check
"use strict";
const log = require("./work_queue_logging.cjs").createWorkQueueLogger("native");

const { canonical, closed, parseStrictJSON } = require("./work_queue_codec.cjs");
const { actorFromContext } = require("./work_queue_policy.cjs");

const API_VERSION = "2026-03-10";

function nativeId(value, name = "native ID") {
  if (typeof value === "number" && Number.isSafeInteger(value) && value > 0) return String(value);
  if (typeof value === "string" && value.length <= 256 && /^[1-9][0-9]*$/.test(value)) return value;
  throw new TypeError(`${name} must be a lossless positive canonical integer`);
}

function immutableRef(value) {
  if (typeof value !== "string" || !/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(value)) throw new TypeError("worker ref must be an immutable commit SHA");
  return value;
}

function nativeAttempt(value) {
  const attempt = Number(nativeId(value, "publisher attempt"));
  if (!Number.isSafeInteger(attempt) || attempt > 4096) throw new TypeError("publisher attempt must be a bounded positive integer");
  return attempt;
}

function hasDispatchToken(title, dispatchId) {
  if (typeof title !== "string" || typeof dispatchId !== "string" || !dispatchId.length) return false;
  const escaped = dispatchId.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`(?:^|[^A-Za-z0-9_])${escaped}(?:$|[^A-Za-z0-9_])`, "u").test(title);
}

function resourceURL(value, expected) {
  let url;
  try {
    url = new URL(value);
  } catch {
    throw new TypeError("native resource URL is invalid");
  }
  if (url.protocol !== "https:" || url.username || url.password || url.search || url.hash || url.hostname !== expected.host || url.pathname !== expected.path || url.port) {
    throw new TypeError("native resource URL does not match the expected run");
  }
  return url.href;
}

function dispatchResponse(response, destination) {
  if (response?.status !== 200) throw new TypeError("queue launch requires the API 2026-03-10 HTTP 200 response contract");
  const runId = nativeId(response.data?.workflow_run_id, "workflow_run_id");
  const host = destination.host || "github.com";
  const apiHost = destination.api_host || (host === "github.com" ? "api.github.com" : host);
  const apiPrefix = host === "github.com" ? "" : "/api/v3";
  return {
    run_id: runId,
    run_url: resourceURL(response.data?.run_url, { host: apiHost, path: `${apiPrefix}/repos/${destination.repository}/actions/runs/${runId}` }),
    html_url: resourceURL(response.data?.html_url, { host, path: `/${destination.repository}/actions/runs/${runId}` }),
  };
}

async function fetchNativeRepository(githubClient, repository) {
  log.debug("repository.fetch.start");
  const [owner, repo, extra] = repository.split("/");
  if (!owner || !repo || extra) throw new TypeError("native run repository is invalid");
  if (typeof githubClient?.rest?.repos?.get !== "function") throw new Error("native_repository_api_missing");
  const response = await githubClient.rest.repos.get({ owner, repo, headers: { "X-GitHub-Api-Version": API_VERSION }, request: { retries: 0, timeout: 15000 } });
  if (response.status !== 200 || typeof response.data?.full_name !== "string" || response.data.full_name.toLowerCase() !== repository.toLowerCase()) throw new Error("native_repository_identity_mismatch");
  const verified = { repository: response.data.full_name, repository_id: nativeId(response.data.id, "native repository ID") };
  log.debug("repository.fetch.verified");
  return verified;
}

function canonicalNativeRepository(run, verified) {
  if (typeof run?.repository?.full_name !== "string" || run.repository.full_name.toLowerCase() !== verified.repository.toLowerCase()) throw new Error("run_repository_mismatch");
  if (nativeId(run.repository.id, "run repository ID") !== verified.repository_id) throw new Error("run_repository_identity_mismatch");
  return { ...run, repository: { ...run.repository, full_name: verified.repository } };
}

// An agent's assignment and run name are correlation hints, never proof.
async function fetchNativeRun(githubClient, repository, runId) {
  log.debug("run.fetch.start");
  const verified = await fetchNativeRepository(githubClient, repository);
  const [owner, repo] = verified.repository.split("/");
  const response = await githubClient.rest.actions.getWorkflowRun({ owner, repo, run_id: nativeId(runId), headers: { "X-GitHub-Api-Version": API_VERSION }, request: { retries: 0, timeout: 15000 } });
  if (response.status !== undefined && response.status !== 200) throw new Error("native run API read was not successful");
  const run = canonicalNativeRepository(response.data, verified);
  log.debug("run.fetch.verified");
  return run;
}

async function fetchNativeRunAttempt(githubClient, repository, runId) {
  log.debug("run_attempt.fetch.start");
  const verified = await fetchNativeRepository(githubClient, repository);
  const [owner, repo] = verified.repository.split("/");
  const response = await githubClient.rest.actions.getWorkflowRunAttempt({
    owner,
    repo,
    run_id: nativeId(runId),
    attempt_number: 1,
    headers: { "X-GitHub-Api-Version": API_VERSION },
    request: { retries: 0, timeout: 15000 },
  });
  if (response.status !== undefined && response.status !== 200) throw new Error("native run attempt API read was not successful");
  const run = canonicalNativeRepository(response.data, verified);
  log.debug("run_attempt.fetch.verified");
  return run;
}

function validateNativeRun(run, expected) {
  log.debug("run.validate.start");
  const runId = nativeId(run?.id, "run ID");
  if (expected.run_id !== undefined && runId !== nativeId(expected.run_id)) throw new Error("run_binding_conflict");
  if (nativeId(run?.run_attempt, "run attempt") !== "1") throw new Error("rerun_not_authorized");
  if (run?.repository?.full_name?.toLowerCase() !== expected.repository.toLowerCase()) throw new Error("run_repository_mismatch");
  if (expected.repository_id !== undefined && nativeId(run?.repository?.id, "repository ID") !== nativeId(expected.repository_id)) throw new Error("run_repository_identity_mismatch");
  if (run?.event !== "workflow_dispatch") throw new Error("run_event_mismatch");
  if (run?.head_sha !== immutableRef(expected.ref)) throw new Error("run_ref_mismatch");
  if (expected.workflow_id !== undefined && nativeId(run?.workflow_id, "workflow ID") !== nativeId(expected.workflow_id)) throw new Error("run_workflow_mismatch");
  if (typeof expected.workflow !== "string" || run?.path?.split("@")[0] !== expected.workflow) throw new Error("run_workflow_mismatch");
  if (!expected.principal_id || nativeId(run?.actor?.id, "initiating principal ID") !== nativeId(expected.principal_id)) throw new Error("run_principal_mismatch");
  if (run?.triggering_actor?.id !== undefined && nativeId(run.triggering_actor.id) !== nativeId(expected.principal_id)) throw new Error("run_principal_mismatch");
  if (expected.dispatch_id !== undefined && !hasDispatchToken(run?.display_title, expected.dispatch_id)) throw new Error("run_correlation_mismatch");
  const status = run.status;
  const terminal = status === "completed" && typeof run.conclusion === "string" && run.conclusion.length > 0;
  log.debug("run.validate.complete", { terminal });
  return { run_id: runId, run_attempt: 1, repository: run.repository.full_name, terminal, status, conclusion: terminal ? run.conclusion : null };
}

async function postQueueDispatch(githubClient, destination, inputs) {
  log.debug("dispatch.post.start");
  const [owner, repo] = destination.repository.split("/");
  immutableRef(destination.ref);
  const response = await githubClient.rest.actions.createWorkflowDispatch({
    owner,
    repo,
    workflow_id: destination.workflow,
    ref: destination.ref,
    inputs,
    headers: { "X-GitHub-Api-Version": API_VERSION },
    request: { retries: 0, retryCount: 0, timeout: 30000 },
  });
  const result = dispatchResponse(response, destination);
  log.debug("dispatch.post.accepted");
  return result;
}

function publisherContextForRun(options, run) {
  const context = options.context;
  const repository = `${context?.repo?.owner}/${context?.repo?.repo}`;
  if (!context?.repo?.owner || !context.repo.repo) throw new Error("publisher_repository_context_missing");
  const runId = nativeId(context.runId ?? process.env.GITHUB_RUN_ID, "publisher run ID");
  const attempt = nativeAttempt(context.runAttempt ?? process.env.GITHUB_RUN_ATTEMPT ?? "1");
  if (options.role === "worker" && attempt !== 1) throw new Error("rerun_not_authorized");
  const actualAttempt = nativeAttempt(run?.run_attempt);
  if (actualAttempt !== attempt || (options.role === "worker" && actualAttempt !== 1)) throw new Error("rerun_not_authorized");
  const workflowRef = options.workflowRef ?? process.env.GITHUB_WORKFLOW_REF;
  if (typeof workflowRef !== "string" || workflowRef.slice(0, repository.length).toLowerCase() !== repository.toLowerCase() || !workflowRef.slice(repository.length).startsWith("/.github/workflows/") || !workflowRef.includes("@"))
    throw new Error("publisher_workflow_context_missing");
  const workflow = workflowRef.slice(repository.length + 1).split("@")[0];
  const ref = immutableRef(context.sha ?? process.env.GITHUB_SHA);
  if (
    nativeId(run.id) !== runId ||
    run.repository?.full_name?.toLowerCase() !== repository.toLowerCase() ||
    run.path?.split("@")[0] !== workflow ||
    run.head_sha !== ref ||
    typeof context.eventName !== "string" ||
    run.event !== context.eventName
  )
    throw new Error("publisher_native_context_mismatch");
  if (context.payload?.repository?.id !== undefined && nativeId(run.repository.id) !== nativeId(context.payload.repository.id)) throw new Error("publisher_repository_identity_mismatch");
  const principal = nativeId(run.actor?.id, "publisher principal ID");
  if (context.actorId !== undefined && nativeId(context.actorId) !== principal) throw new Error("publisher_principal_mismatch");
  /** @type {{role: string, principal: string, repository: string, workflow: string, run_id: string, run_attempt: number, dispatch_id?: string, claim_handle?: string}} */
  const actor = { role: options.role, principal, repository: run.repository.full_name, workflow, run_id: runId, run_attempt: attempt };
  if (options.dispatch_id !== undefined) actor.dispatch_id = options.dispatch_id;
  if (options.claim_handle !== undefined) actor.claim_handle = options.claim_handle;
  return { ...actor, authenticated: true, roles: [options.role], ref, event: run.event, created_at: Date.parse(run.created_at), native_run: run };
}

async function authenticatePublisher(options) {
  log.debug("publisher.authenticate.start");
  const context = options.context;
  if (!context?.repo?.owner || !context.repo.repo) throw new Error("publisher_repository_context_missing");
  const attempt = nativeAttempt(context.runAttempt ?? process.env.GITHUB_RUN_ATTEMPT ?? "1");
  if (options.role === "worker" && attempt !== 1) throw new Error("rerun_not_authorized");
  const repository = `${context.repo.owner}/${context.repo.repo}`;
  const runId = nativeId(context.runId ?? process.env.GITHUB_RUN_ID, "publisher run ID");
  const publisher = publisherContextForRun(options, await fetchNativeRun(options.githubClient, repository, runId));
  log.debug("publisher.authenticate.complete");
  return publisher;
}

async function authenticateIntentPublisher(options) {
  const current = await authenticatePublisher(options);
  const raw = Object.hasOwn(options, "intentOrigin") ? options.intentOrigin : process.env.GH_AW_WORK_QUEUE_INTENT_ORIGIN;
  if (raw === undefined) {
    if (current.role !== "worker" && current.run_attempt !== 1) throw new Error("work_queue_intent_origin_required");
    return current;
  }
  if (typeof raw === "string" && Buffer.byteLength(raw, "utf8") > 4096) throw new Error("work_queue_intent_origin_limit");
  const origin = typeof raw === "string" ? parseStrictJSON(raw) : raw;
  closed(origin, ["role", "principal", "repository", "workflow", "run_id", "run_attempt"], ["dispatch_id", "claim_handle"], "trusted intent origin");
  const attempt = nativeAttempt(origin.run_attempt);
  if (attempt > current.run_attempt || origin.role !== current.role || origin.principal !== current.principal || origin.repository !== current.repository || origin.workflow !== current.workflow || origin.run_id !== current.run_id)
    throw new Error("work_queue_intent_origin_mismatch");
  let source = current;
  if (attempt !== current.run_attempt) {
    const [owner, repo] = current.repository.split("/");
    const response = await options.githubClient.rest.actions.getWorkflowRunAttempt({
      owner,
      repo,
      run_id: current.run_id,
      attempt_number: attempt,
      headers: { "X-GitHub-Api-Version": API_VERSION },
      request: { retries: 0, timeout: 15000 },
    });
    if (response.status !== undefined && response.status !== 200) throw new Error("work_queue_intent_origin_read_failed");
    source = publisherContextForRun(
      { ...options, context: { ...options.context, runAttempt: attempt } },
      canonicalNativeRepository(response.data, { repository: current.repository, repository_id: nativeId(current.native_run.repository.id) })
    );
  }
  const actor = actorFromContext(source);
  if (!Object.hasOwn(origin, "claim_handle")) delete actor.claim_handle;
  if (canonical(actor) !== canonical(origin)) throw new Error("work_queue_intent_origin_mismatch");
  return { ...source, publisher_attempt: current.run_attempt };
}

module.exports = {
  API_VERSION,
  nativeId,
  nativeAttempt,
  immutableRef,
  hasDispatchToken,
  resourceURL,
  dispatchResponse,
  fetchNativeRun,
  fetchNativeRunAttempt,
  validateNativeRun,
  postQueueDispatch,
  authenticatePublisher,
  authenticateIntentPublisher,
};
