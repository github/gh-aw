// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Runtime for the generated agentic-continuation-relay.yml relay.
 *
 * The relay is the only component that dispatches a continuation run. Its inputs
 * (parent_run_id, parent_run_attempt) are untrusted: every decision is derived from
 * the GitHub API, the parent's compiled lock-file metadata at its head SHA, and the
 * parent's continuation request artifact. Any doubt fails closed.
 */

const fs = require("node:fs");
const path = require("node:path");
const { DefaultArtifactClient } = require("./artifact_client.cjs");
const { dispatchWorkflow } = require("./dispatch_workflow_transport.cjs");
const { parseLockFileHeaders } = require("./lock_file_headers.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const {
  GITHUB_ACTIONS_BOT,
  checkpointArtifactPrefix,
  downloadContinuationRequest,
  findCheckpointArtifact,
  findRequestArtifact,
  getDefaultBranch,
  relayRunTitle,
  requestArtifactName,
  validateContinuationRequest,
  validateRelayRun,
} = require("./continuation_request.cjs");

const POSITIVE_INTEGER = /^[1-9]\d*$/;
const WORKFLOW_ID = /^[A-Za-z0-9_-]+$/;
const ENVIRONMENT_NAME = /^[A-Za-z0-9_.-]{1,255}$/;
const LOCK_FILE_PATH = /^\.github\/workflows\/[A-Za-z0-9_.-]+\.lock\.yml$/;
const SUPPORTED_POLICIES = ["workflow-dispatch"];
/** Artifact uploaded by the dispatch job after the child run was dispatched. */
const DISPATCHED_MARKER_ARTIFACT = "continuation-dispatched";

/** @param {string} message */
function fail(message) {
  return new Error(`${ERR_VALIDATION}: ${message}`);
}

/**
 * @param {NodeJS.ProcessEnv} env
 * @returns {{parentRunId: string, parentRunAttempt: string}}
 */
function readInputs(env = process.env) {
  const parentRunId = String(env.GH_AW_PARENT_RUN_ID ?? "").trim();
  const parentRunAttempt = String(env.GH_AW_PARENT_RUN_ATTEMPT ?? "").trim();
  if (!POSITIVE_INTEGER.test(parentRunId)) throw fail("parent_run_id must be a positive integer");
  if (!POSITIVE_INTEGER.test(parentRunAttempt)) throw fail("parent_run_attempt must be a positive integer");
  return { parentRunId, parentRunAttempt };
}

/**
 * Validates the continuation metadata the compiler wrote into the parent's lock file.
 * @param {unknown} metadata
 * @returns {{workflow: string, max: number, policy: string, approval: string}}
 */
function readContinuationMetadata(metadata) {
  /** @type {any} */
  const header = metadata;
  const continuation = header?.continuation;
  if (!continuation || typeof continuation !== "object" || Array.isArray(continuation)) {
    throw fail("the parent workflow does not enable safe-outputs.request-continuation; the relay only continues durable workflows");
  }
  const { workflow, max, policy, approval = "" } = continuation;
  if (typeof workflow !== "string" || !WORKFLOW_ID.test(workflow)) throw fail("the parent continuation metadata has an invalid workflow identifier");
  if (!Number.isInteger(max) || max < 1 || max > 100) throw fail("the parent continuation metadata has an invalid max");
  if (!SUPPORTED_POLICIES.includes(policy)) throw fail(`the parent continuation policy ${JSON.stringify(policy)} is not supported by this relay (supported: ${SUPPORTED_POLICIES.join(", ")})`);
  if (approval !== "" && (typeof approval !== "string" || !ENVIRONMENT_NAME.test(approval))) throw fail("the parent continuation metadata has an invalid approval environment");
  return { workflow, max, policy, approval };
}

/**
 * Reads a file at an exact commit.
 * @param {any} github
 * @param {{owner: string, repo: string, path: string, ref: string}} params
 * @returns {Promise<string>}
 */
async function readFileAtRef(github, params) {
  const { data } = await github.rest.repos.getContent({ ...params, mediaType: { format: "raw" } });
  if (typeof data === "string") return data;
  if (data && typeof data === "object" && !Array.isArray(data) && typeof data.content === "string" && data.encoding === "base64") {
    return Buffer.from(data.content, "base64").toString("utf8");
  }
  throw fail(`could not read ${params.path} at ${params.ref}`);
}

/** Repository permissions allowed to dispatch or rerun the relay by hand (maintain reports as write). */
const DISPATCHER_PERMISSIONS = ["admin", "write"];

/**
 * Guards against workflow_dispatch abuse of the relay itself. The relay must be a
 * workflow_dispatch run of the default-branch relay (a write-access user could otherwise run
 * a modified copy from another branch), and every actor that started or reran it must be
 * GITHUB_TOKEN (the parent's conclusion job) or a human with write access. Other bots and
 * read-only users are rejected even though GitHub already requires write to dispatch.
 * @param {{github: any, context: any, core: any, relay: any, parentRunId: string, parentRunAttempt: string}} options
 */
async function assertTrustedDispatch({ github, context, core, relay, parentRunId, parentRunAttempt }) {
  if (context.eventName !== "workflow_dispatch") throw fail(`the relay only runs on workflow_dispatch, not ${JSON.stringify(context.eventName)}`);
  const { owner, repo } = context.repo;
  if (relay?.repository?.full_name?.toLowerCase() !== `${owner}/${repo}`.toLowerCase()) throw fail("the relay run must belong to this repository");
  try {
    validateRelayRun(relay, { repositoryId: relay.repository.id, defaultBranch: await getDefaultBranch(github, context), parentRunId, parentRunAttempt });
  } catch (error) {
    throw fail(getErrorMessage(error));
  }
  const actors = [...new Set([relay.actor?.login, relay.triggering_actor?.login, context.actor].filter(login => typeof login === "string" && login))];
  if (actors.length === 0) throw fail("could not identify who dispatched the relay");
  for (const actor of actors) {
    if (actor === GITHUB_ACTIONS_BOT) continue;
    if (actor.endsWith("[bot]")) throw fail(`bot ${JSON.stringify(actor)} is not allowed to dispatch the relay; only ${GITHUB_ACTIONS_BOT} or a user with write access can`);
    let permission = "";
    try {
      ({
        data: { permission },
      } = await github.rest.repos.getCollaboratorPermissionLevel({ owner, repo, username: actor }));
    } catch (error) {
      throw fail(`could not verify the repository permission of ${JSON.stringify(actor)}: ${getErrorMessage(error)}`);
    }
    if (!DISPATCHER_PERMISSIONS.includes(String(permission))) throw fail(`${JSON.stringify(actor)} needs write access to dispatch the relay (has ${JSON.stringify(permission || "none")})`);
    core.info(`Relay dispatcher ${actor} has ${permission} access`);
  }
}

/**
 * Rejects the relay when another relay for the same parent attempt is pending or
 * succeeded, or when this relay run already dispatched a child in an earlier attempt.
 * @param {{github: any, context: any, relay: any, parentRunId: string, parentRunAttempt: string, parentCreatedAt: string}} options
 */
async function assertNotRelayed({ github, context, relay, parentRunId, parentRunAttempt, parentCreatedAt }) {
  const { owner, repo } = context.repo;
  const title = relayRunTitle(parentRunId, parentRunAttempt);
  const runs = await github.paginate(github.rest.actions.listWorkflowRuns, { owner, repo, workflow_id: relay.workflow_id, event: "workflow_dispatch", created: `>=${parentCreatedAt}`, per_page: 100 });
  const duplicate = runs.find(run => String(run.id) !== String(context.runId) && run.display_title === title && (run.status !== "completed" || run.conclusion === "success"));
  if (duplicate) throw fail(`continuation for run ${parentRunId} attempt ${parentRunAttempt} is already handled by relay run ${duplicate.id}`);
  const own = await github.paginate(github.rest.actions.listWorkflowRunArtifacts, { owner, repo, run_id: context.runId, per_page: 100 });
  if (own.some(artifact => artifact.name === DISPATCHED_MARKER_ARTIFACT)) throw fail(`relay run ${context.runId} already dispatched this continuation`);
}

/**
 * Validates the parent run, its compiled policy, and its continuation artifacts.
 * @param {{github: any, context: any, core: any, token: string, artifactClient?: any, tempDir?: string, env?: NodeJS.ProcessEnv}} options
 */
async function loadContinuation({ github, context, core, token, artifactClient = new DefaultArtifactClient(), tempDir = path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "continuation-relay"), env = process.env }) {
  const { parentRunId, parentRunAttempt } = readInputs(env);
  const { owner, repo } = context.repo;
  if (parentRunId === String(context.runId)) throw fail("the relay cannot continue itself");
  const { data: relay } = await github.rest.actions.getWorkflowRun({ owner, repo, run_id: context.runId });
  await assertTrustedDispatch({ github, context, core, relay, parentRunId, parentRunAttempt });
  const { data: parent } = await github.rest.actions.getWorkflowRun({ owner, repo, run_id: parentRunId });
  if (!parent?.repository?.id || parent.repository.full_name?.toLowerCase() !== `${owner}/${repo}`.toLowerCase()) throw fail("the parent run must belong to this repository");
  if (parent.head_repository?.id !== parent.repository.id) throw fail("fork workflow runs cannot be continued");
  if (Number(parentRunAttempt) > Number(parent.run_attempt || 1)) throw fail(`run ${parentRunId} has no attempt ${parentRunAttempt}`);
  if (typeof parent.head_branch !== "string" || !parent.head_branch) throw fail("the parent run has no branch to continue on");
  if (typeof parent.path !== "string" || !LOCK_FILE_PATH.test(parent.path)) throw fail("the parent run is not a compiled agentic workflow");
  if (!/^[0-9a-f]{40,64}$/.test(String(parent.head_sha))) throw fail("the parent run has an invalid head SHA");

  const lock = await readFileAtRef(github, { owner, repo, path: parent.path, ref: parent.head_sha });
  const metadata = readContinuationMetadata(parseLockFileHeaders(lock.split("\n", 3)).metadata);

  const artifacts = await github.paginate(github.rest.actions.listWorkflowRunArtifacts, { owner, repo, run_id: parent.id, per_page: 100 });
  const found = findRequestArtifact(artifacts, parentRunAttempt);
  if (!found) throw fail(`run ${parentRunId} attempt ${parentRunAttempt} has no unexpired ${requestArtifactName(parentRunAttempt)} artifact; it did not request continuation or the request expired`);
  const checkpoint = findCheckpointArtifact(artifacts, checkpointArtifactPrefix(metadata.workflow, parent), parent.id, parentRunAttempt);
  if (!checkpoint) throw fail(`run ${parentRunId} attempt ${parentRunAttempt} has no unexpired durable checkpoint; checkpoints are kept for a limited time, so start a new run instead`);

  const raw = await downloadContinuationRequest({ artifactClient, artifactId: found.artifact.id, token, owner, repo, directory: path.join(tempDir, "request") });
  const request = validateContinuationRequest(raw, { parentRunId, parentRunAttempt, max: metadata.max });
  if (request.max !== metadata.max) core.info(`Using the compiled maximum of ${metadata.max} continuation(s) instead of the requested ${request.max}`);

  // The child runs the branch head, which may have changed since the parent ran. Never
  // dispatch into a workflow that no longer opts into continuation or has a lower limit.
  let targetLock;
  try {
    targetLock = await readFileAtRef(github, { owner, repo, path: parent.path, ref: `refs/heads/${parent.head_branch}` });
  } catch (error) {
    throw fail(`could not read ${parent.path} on branch ${parent.head_branch}: ${getErrorMessage(error)}`);
  }
  const target = readContinuationMetadata(parseLockFileHeaders(targetLock.split("\n", 3)).metadata);
  if (target.workflow !== metadata.workflow) throw fail(`${parent.path} on branch ${parent.head_branch} is now workflow ${JSON.stringify(target.workflow)}, not ${JSON.stringify(metadata.workflow)}`);
  if (request.depth > target.max) throw fail(`continuation depth ${request.depth} exceeds the maximum of ${target.max} now compiled on branch ${parent.head_branch}`);
  const max = Math.min(metadata.max, target.max);
  if (metadata.approval && target.approval && metadata.approval !== target.approval) {
    throw fail(`the approval environment changed on branch ${parent.head_branch} from ${JSON.stringify(metadata.approval)} to ${JSON.stringify(target.approval)}; start a new run`);
  }
  const approval = metadata.approval || target.approval;
  return { parentRunId, parentRunAttempt, parent, relay, metadata: { ...metadata, max, approval }, request, targetWorkflow: path.posix.basename(parent.path) };
}

/**
 * Fails closed unless the approval environment exists and requires reviewers. A missing
 * environment would otherwise be created on demand without protection.
 * @param {any} github
 * @param {{owner: string, repo: string}} repo
 * @param {string} environment
 */
async function assertProtectedEnvironment(github, { owner, repo }, environment) {
  let data;
  try {
    ({ data } = await github.rest.repos.getEnvironment({ owner, repo, environment_name: environment }));
  } catch (error) {
    const status = error && typeof error === "object" && "status" in error ? error.status : undefined;
    if (status === 404) throw fail(`approval environment ${JSON.stringify(environment)} does not exist; create it with required reviewers`);
    throw error;
  }
  const rules = Array.isArray(data?.protection_rules) ? data.protection_rules : [];
  const reviewers = rules.some(rule => rule?.type === "required_reviewers" && Array.isArray(rule.reviewers) && rule.reviewers.length > 0);
  if (!reviewers) throw fail(`approval environment ${JSON.stringify(environment)} has no required reviewers; add reviewers so continuation is manually approved`);
}

/** @param {unknown} value */
function escapeHTML(value) {
  return String(value).replace(/[<>&"]/g, c => ({ "<": "&lt;", ">": "&gt;", "&": "&amp;", '"': "&quot;" })[c] || c);
}

/**
 * @param {any} core
 * @param {string} heading
 * @param {Awaited<ReturnType<typeof loadContinuation>>} continuation
 */
async function writeSummary(core, heading, { parentRunId, parentRunAttempt, parent, metadata, request, targetWorkflow }) {
  await core.summary
    .addHeading(heading, 3)
    .addTable([
      [
        { data: "Parent run", header: true },
        { data: "Workflow", header: true },
        { data: "Branch", header: true },
        { data: "Depth", header: true },
        { data: "Approval", header: true },
      ],
      [`<a href="${escapeHTML(parent.html_url)}">${parentRunId}</a> (attempt ${parentRunAttempt})`, escapeHTML(targetWorkflow), escapeHTML(parent.head_branch), `${request.depth} of ${metadata.max}`, escapeHTML(metadata.approval || "none")],
    ])
    .addRaw("Continuation hints written by the parent agent (untrusted):", true)
    .addRaw(`<pre><code>${escapeHTML(request.hints)}</code></pre>`, true)
    .write();
}

/**
 * Relay job `resolve`: validates the request and decides whether approval is needed.
 * @param {{github?: any, context?: any, core?: any, token?: string, artifactClient?: any, env?: NodeJS.ProcessEnv}} [options]
 */
async function resolve({ github = global.github, context = global.context, core = global.core, token = process.env.GH_AW_RELAY_TOKEN || "", artifactClient, env = process.env } = {}) {
  const continuation = await loadContinuation({ github, context, core, token, artifactClient, env });
  await assertNotRelayed({ github, context, relay: continuation.relay, parentRunId: continuation.parentRunId, parentRunAttempt: continuation.parentRunAttempt, parentCreatedAt: continuation.parent.created_at });
  if (continuation.metadata.approval) await assertProtectedEnvironment(github, context.repo, continuation.metadata.approval);
  core.setOutput("target_workflow", continuation.targetWorkflow);
  core.setOutput("ref", continuation.parent.head_branch);
  core.setOutput("depth", String(continuation.request.depth));
  core.setOutput("max", String(continuation.metadata.max));
  core.setOutput("approval_environment", continuation.metadata.approval);
  await writeSummary(core, continuation.metadata.approval ? `Continuation awaiting approval in ${continuation.metadata.approval}` : "Continuation validated", continuation);
}

/**
 * Relay job `dispatch`: revalidates everything and dispatches the continuation run.
 * @param {{github?: any, context?: any, core?: any, token?: string, artifactClient?: any, env?: NodeJS.ProcessEnv, markerFile?: string}} [options]
 */
async function dispatch({
  github = global.github,
  context = global.context,
  core = global.core,
  token = process.env.GH_AW_RELAY_TOKEN || "",
  artifactClient,
  env = process.env,
  markerFile = path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "continuation-relay", "dispatched.json"),
} = {}) {
  const continuation = await loadContinuation({ github, context, core, token, artifactClient, env });
  const { parentRunId, parentRunAttempt, parent, relay, metadata, request, targetWorkflow } = continuation;
  if (metadata.approval && env.GH_AW_APPROVAL_RESULT !== "success") throw fail(`continuation requires approval in ${JSON.stringify(metadata.approval)}`);
  await assertNotRelayed({ github, context, relay, parentRunId, parentRunAttempt, parentCreatedAt: parent.created_at });
  const { owner, repo } = context.repo;
  /** @type {any} */
  const inherited = request.aw_context.continuation && typeof request.aw_context.continuation === "object" ? request.aw_context.continuation : {};
  const { hints: _hints, ...requestContinuation } = inherited;
  const awContext = {
    ...request.aw_context,
    repo: `${owner}/${repo}`,
    run_id: parentRunId,
    run_attempt: parentRunAttempt,
    continuation: { ...requestContinuation, depth: request.depth, max: metadata.max },
    relay_run_id: String(context.runId),
  };
  const ref = `refs/heads/${parent.head_branch}`;
  core.info(`Dispatching continuation ${request.depth} of ${metadata.max}: ${targetWorkflow} (${ref})`);
  const response = await dispatchWorkflow(github, { owner, repo, workflow_id: targetWorkflow, ref, inputs: { aw_context: JSON.stringify(awContext) }, return_run_details: true }, { allowLegacyFallback: true, coreApi: core });
  const childRunId = response?.data?.workflow_run_id;
  try {
    fs.mkdirSync(path.dirname(markerFile), { recursive: true });
    fs.writeFileSync(markerFile, JSON.stringify({ parent_run_id: parentRunId, parent_run_attempt: parentRunAttempt, child_run_id: childRunId ? String(childRunId) : null }) + "\n", { mode: 0o600 });
  } catch (error) {
    throw new Error(`${ERR_VALIDATION}: failed to write continuation dispatch marker ${markerFile}: ${getErrorMessage(error)}`, { cause: error });
  }
  if (childRunId) core.setOutput("child_run_id", String(childRunId));
  core.info(childRunId ? `✓ Dispatched continuation run ${childRunId}` : "✓ Dispatched continuation run");
  await writeSummary(core, childRunId ? `Continuation dispatched as run ${childRunId}` : "Continuation dispatched", continuation);
}

module.exports = { resolve, dispatch, relayRunTitle, readContinuationMetadata, DISPATCHED_MARKER_ARTIFACT };
