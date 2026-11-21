// @ts-check
"use strict";

const path = require("node:path");
const { readInboundAwContext } = require("./aw_context.cjs");
const { DefaultArtifactClient } = require("./artifact_client.cjs");
const { restoreFiles } = require("./durable_files.cjs");
const { preparePrompt } = require("./durable_session.cjs");
const { tryRecordDurableSessionEvent } = require("./durable_session_record.cjs");
const {
  GITHUB_ACTIONS_BOT,
  RELAY_WORKFLOW_PATH,
  checkpointArtifactPrefix,
  downloadContinuationRequest,
  findCheckpointArtifact,
  findRequestArtifact,
  getDefaultBranch,
  isUnexpired,
  validateContinuationRequest,
  validateRelayRun,
} = require("./continuation_request.cjs");

/** Select and restore a checkpoint before exposing continuation to the engine. */
async function restoreCheckpoint({ github, context, core, token, artifactClient = new DefaultArtifactClient(), root = "/tmp/gh-aw/agent", promptFile = "/tmp/gh-aw/aw-prompts/prompt.txt" }) {
  const outputs = new Map();
  const record = status =>
    tryRecordDurableSessionEvent(
      "durable.restore",
      {
        status,
        parentRunId: outputs.get("parent-run-id"),
        artifactId: outputs.get("artifact-id"),
        relayRunId: outputs.get("relay-run-id"),
      },
      { core, root, env: { ...process.env, GITHUB_RUN_ID: String(context.runId) } }
    );
  try {
    const { continuation } = await selectCheckpoint({
      github,
      context,
      core: {
        ...core,
        setOutput(name, value) {
          outputs.set(name, value);
          core.setOutput(name, value);
        },
      },
      readRequest: artifactId => downloadContinuationRequest({ artifactClient, artifactId, token, owner: context.repo.owner, repo: context.repo.repo, directory: path.join(root, "durable/request") }),
    });
    const restored = outputs.has("artifact-id");
    if (restored) {
      await artifactClient.downloadArtifact(Number(outputs.get("artifact-id")), {
        path: path.join(root, "durable/bundle"),
        findBy: { token, repositoryOwner: context.repo.owner, repositoryName: context.repo.repo },
      });
      restoreFiles({ core: { ...core, setOutput() {} }, root });
    }
    if ((restored || continuation) && ["copilot", "claude", "codex", "gemini", "agy", "pi"].includes(process.env.GH_AW_DURABLE_ENGINE || "")) {
      const currentPrompt = process.env.GH_AW_DURABLE_ENGINE === "pi" ? process.env.GH_AW_PI_USER_PROMPT || path.join(path.dirname(promptFile), "user.txt") : promptFile;
      preparePrompt({ env: { ...process.env, GH_AW_DURABLE_RESUMED: restored ? "true" : "false" }, promptFile: currentPrompt, continuation });
    }
    if (!restored) {
      record("fresh");
      return;
    }
    record("restored");
    core.setOutput("restored", "true");
  } catch (error) {
    record("failed");
    throw error;
  }
}

/**
 * Reads continuation lineage written into aw_context by the continuation relay.
 * Hints never travel in aw_context; they are read from the parent's request artifact.
 * @param {any} awContext
 * @returns {{depth: number, max: number | undefined, parentRunAttempt: string | undefined, relayRunId: string | undefined} | undefined}
 */
function readContinuation(awContext) {
  if (!awContext || !Object.hasOwn(awContext, "continuation")) return undefined;
  const continuation = awContext.continuation;
  if (!continuation || typeof continuation !== "object" || Array.isArray(continuation)) throw new Error("aw_context.continuation must be an object");
  const depth = Number(continuation.depth);
  if (!Number.isInteger(depth) || depth < 1) throw new Error("aw_context.continuation.depth must be a positive integer");
  const max = continuation.max === undefined ? undefined : Number(continuation.max);
  if (max !== undefined && (!Number.isInteger(max) || max < 1)) throw new Error("aw_context.continuation.max must be a positive integer");
  const parentRunAttempt = awContext.run_attempt === undefined || awContext.run_attempt === "" ? undefined : String(awContext.run_attempt);
  if (parentRunAttempt !== undefined && !/^[1-9]\d*$/.test(parentRunAttempt)) throw new Error("aw_context.run_attempt must be a positive integer");
  const relayRunId = awContext.relay_run_id === undefined || awContext.relay_run_id === "" ? undefined : String(awContext.relay_run_id);
  if (relayRunId !== undefined && !/^[1-9]\d*$/.test(relayRunId)) throw new Error("aw_context.relay_run_id must be a positive integer");
  return { depth, max, parentRunAttempt, relayRunId };
}

/**
 * Restores only explicit continuation lineage requested through request_continuation,
 * or an earlier attempt of the current run. Other runs always start fresh.
 * The parent may still be finishing its conclusion job when the relay dispatches
 * this run, so the parent is accepted once its continuation request artifact exists;
 * that artifact is uploaded only after threat detection accepted the parent's outputs.
 * Fork runs are never continuation sources.
 * @param {{github: any, context: any, core: any, readRequest?: (artifactId: number) => Promise<unknown>}} options
 * @returns {Promise<{continuation: {depth: number, max: number | undefined, hints: string} | undefined}>}
 */
async function selectCheckpoint({ github, context, core, readRequest }) {
  const workflow = process.env.GH_AW_DURABLE_WORKFLOW;
  if (!workflow || !/^[A-Za-z0-9_-]+$/.test(workflow)) throw new Error("Invalid durable workflow identifier");
  const { owner, repo } = context.repo;
  const { data: current } = await github.rest.actions.getWorkflowRun({ owner, repo, run_id: context.runId });
  if (current.head_repository?.id !== current.repository.id) throw new Error("Durable continuation is not supported for fork workflow runs");
  const prefix = checkpointArtifactPrefix(workflow, current);
  const currentAttempt = process.env.GITHUB_RUN_ATTEMPT || "1";
  core.setOutput("artifact-name", `${prefix}${context.runId}-${currentAttempt}`);
  const inbound = readInboundAwContext(context.payload);
  const rawContext = context.payload.inputs?.aw_context ?? context.payload.client_payload?.aw_context;
  if (rawContext && !inbound) throw new Error("Durable continuation requires a valid aw_context object");
  const lineage = readContinuation(inbound);
  core.setOutput("continuation-depth", String(lineage?.depth ?? 0));
  if (lineage?.relayRunId) core.setOutput("relay-run-id", lineage.relayRunId);
  const listArtifacts = run_id => github.paginate(github.rest.actions.listWorkflowRunArtifacts, { owner, repo, run_id, per_page: 100 });
  /** @param {any} artifact @param {string | number} runId */
  function useCheckpoint(artifact, runId) {
    core.setOutput("artifact-id", String(artifact.id));
    core.setOutput("run-id", String(runId));
    core.setOutput("parent-run-id", String(runId));
    core.info(`Restoring durable checkpoint from run ${runId}`);
  }
  /** @type {{depth: number, max: number | undefined, hints: string} | undefined} */
  let continuation;
  /** @type {any[] | undefined} */
  let parentArtifacts;
  let parentRunId = "";
  let parentAttempt = "";
  if (lineage) {
    parentRunId = String(inbound?.run_id || "");
    if (!/^[1-9]\d*$/.test(parentRunId) || parentRunId === String(current.id)) throw new Error("aw_context.run_id must identify the run that requested continuation");
    const { data: parent } = await github.rest.actions.getWorkflowRun({ owner, repo, run_id: parentRunId });
    if (parent.workflow_id !== current.workflow_id || parent.repository.id !== current.repository.id || parent.head_repository?.id !== current.repository.id || parent.head_branch !== current.head_branch) {
      throw new Error("Durable parent must be a run of the same workflow and branch in this repository");
    }
    const artifacts = await listArtifacts(parent.id);
    parentArtifacts = artifacts;
    const found = findRequestArtifact(artifacts, lineage.parentRunAttempt);
    if (!found) throw new Error(`Continuation parent run ${parentRunId} has no unexpired continuation request; it did not request continuation or the request expired`);
    // Continuation lineage in aw_context is attacker-controllable by anyone who can dispatch
    // this workflow. Accept it only from an authentic relay run bound to this parent attempt.
    if (current.event !== "workflow_dispatch") throw new Error(`Continuation runs must be workflow_dispatch runs dispatched by ${RELAY_WORKFLOW_PATH}, not ${JSON.stringify(current.event)}`);
    if (!lineage.relayRunId) throw new Error(`Continuation lineage requires aw_context.relay_run_id; continuation runs are dispatched only by ${RELAY_WORKFLOW_PATH}`);
    if (current.actor?.login !== GITHUB_ACTIONS_BOT) throw new Error(`Continuation runs must be dispatched by the continuation relay as ${GITHUB_ACTIONS_BOT}, not ${JSON.stringify(current.actor?.login ?? "unknown")}`);
    const { data: relay } = await github.rest.actions.getWorkflowRun({ owner, repo, run_id: lineage.relayRunId });
    validateRelayRun(relay, { repositoryId: current.repository.id, defaultBranch: await getDefaultBranch(github, context), parentRunId, parentRunAttempt: found.attempt });
    if (!readRequest) throw new Error("Durable continuation requires access to the parent continuation request");
    parentAttempt = found.attempt;
    const request = validateContinuationRequest(await readRequest(Number(found.artifact.id)), { parentRunId, parentRunAttempt: parentAttempt, max: lineage.max });
    if (request.depth !== lineage.depth) throw new Error(`Continuation depth ${lineage.depth} does not match the parent request depth ${request.depth}`);
    continuation = { depth: lineage.depth, max: lineage.max ?? request.max, hints: request.hints };
  }
  if (Number(currentAttempt) > 1) {
    const artifact = (await listArtifacts(current.id))
      .filter(item => {
        const runPrefix = `${prefix}${current.id}-`;
        if (!item.name.startsWith(runPrefix)) return false;
        const attempt = item.name.slice(runPrefix.length);
        return /^[1-9]\d*$/.test(attempt) && Number(attempt) < Number(currentAttempt) && isUnexpired(item);
      })
      .sort((a, b) => b.id - a.id)[0];
    if (artifact) {
      useCheckpoint(artifact, current.id);
      return { continuation };
    }
  }
  if (!continuation || !parentArtifacts) {
    core.info("No continuation was requested by a previous run; starting a new session");
    return { continuation };
  }
  const checkpoint = findCheckpointArtifact(parentArtifacts, prefix, parentRunId, parentAttempt);
  if (checkpoint) {
    useCheckpoint(checkpoint, parentRunId);
    return { continuation };
  }
  core.warning(`Continuation parent run ${parentRunId} has no unexpired durable checkpoint; starting a new session with its continuation hints`);
  return { continuation };
}

/** Recheck integrity after host secret redaction, never upload a damaged store. */
module.exports = { selectCheckpoint, restoreCheckpoint, readContinuation };
