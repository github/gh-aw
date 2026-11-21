// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { readInboundAwContext } = require("./aw_context.cjs");
const { DefaultArtifactClient } = require("./artifact_client.cjs");
const { restoreFiles } = require("./durable_files.cjs");
const { preparePrompt } = require("./durable_session.cjs");
const { tryRecordDurableSessionEvent } = require("./durable_session_record.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const {
  GITHUB_ACTIONS_BOT,
  RELAY_DISPATCH_MARKER_ARTIFACT,
  RELAY_WORKFLOW_PATH,
  checkpointArtifactPrefix,
  downloadContinuationRequest,
  findCheckpointArtifact,
  findRequestArtifact,
  getDefaultBranch,
  isUnexpired,
  normalizeWorkflowRunPath,
  validateContinuationRequest,
  validateRelayRun,
} = require("./continuation_request.cjs");

const CONTINUATION_CONTEXT_PREFIX = "continuation-context-";
const DISPATCH_EVIDENCE_WAIT_MS = 30000;
const DISPATCH_EVIDENCE_POLL_MS = 2000;

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
      readDispatchEvidence: artifactId =>
        downloadArtifactJSON({
          artifactClient,
          artifactId,
          token,
          owner: context.repo.owner,
          repo: context.repo.repo,
          directory: path.join(root, "durable/relay-evidence"),
          fileName: "dispatched.json",
        }),
      readContinuationContext: artifactId =>
        downloadArtifactJSON({
          artifactClient,
          artifactId,
          token,
          owner: context.repo.owner,
          repo: context.repo.repo,
          directory: path.join(root, "durable/continuation-context-read"),
          fileName: "continuation-context.json",
        }),
      persistContinuationContext: async continuationContext => {
        const directory = path.join(root, "durable/continuation-context-upload");
        fs.rmSync(directory, { recursive: true, force: true });
        fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
        const file = path.join(directory, "continuation-context.json");
        fs.writeFileSync(file, JSON.stringify(continuationContext) + "\n", { mode: 0o600 });
        const attempt = process.env.GITHUB_RUN_ATTEMPT || "1";
        await artifactClient.uploadArtifact(`continuation-context-${attempt}`, [file], directory, { retentionDays: 90 });
      },
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
 * @returns {{depth: number, max: number | undefined, parentRunAttempt: string | undefined, checkpointAttempt: string | undefined, relayRunId: string | undefined} | undefined}
 */
function readContinuation(awContext) {
  if (!awContext || !Object.hasOwn(awContext, "continuation")) return undefined;
  const continuation = awContext.continuation;
  if (!continuation || typeof continuation !== "object" || Array.isArray(continuation)) throw new Error("aw_context.continuation must be an object");
  const depth = Number(continuation.depth);
  if (!Number.isInteger(depth) || depth < 1) throw new Error("aw_context.continuation.depth must be a positive integer");
  const max = continuation.max === undefined ? undefined : Number(continuation.max);
  if (max !== undefined && (!Number.isInteger(max) || max < 1 || max > 100)) throw new Error("aw_context.continuation.max must be an integer from 1 to 100");
  const parentRunAttempt = awContext.run_attempt === undefined || awContext.run_attempt === "" ? undefined : String(awContext.run_attempt);
  if (parentRunAttempt !== undefined && !/^[1-9]\d*$/.test(parentRunAttempt)) throw new Error("aw_context.run_attempt must be a positive integer");
  const checkpointAttempt = awContext.checkpoint_attempt === undefined || awContext.checkpoint_attempt === "" ? undefined : String(awContext.checkpoint_attempt);
  if (checkpointAttempt !== undefined && !/^[1-9]\d*$/.test(checkpointAttempt)) throw new Error("aw_context.checkpoint_attempt must be a positive integer");
  const relayRunId = awContext.relay_run_id === undefined || awContext.relay_run_id === "" ? undefined : String(awContext.relay_run_id);
  if (relayRunId !== undefined && !/^[1-9]\d*$/.test(relayRunId)) throw new Error("aw_context.relay_run_id must be a positive integer");
  return { depth, max, parentRunAttempt, checkpointAttempt, relayRunId };
}

async function downloadArtifactJSON({ artifactClient, artifactId, token, owner, repo, directory, fileName }) {
  try {
    fs.rmSync(directory, { recursive: true, force: true });
  } catch (error) {
    throw new Error(`Failed to prepare durable artifact directory ${directory}: ${getErrorMessage(error)}`, { cause: error });
  }
  try {
    await artifactClient.downloadArtifact(artifactId, { path: directory, findBy: { token, repositoryOwner: owner, repositoryName: repo } });
    let content;
    try {
      content = fs.readFileSync(path.join(directory, fileName), "utf8");
    } catch (error) {
      throw new Error(`Failed to read durable artifact ${fileName}: ${getErrorMessage(error)}`, { cause: error });
    }
    try {
      return JSON.parse(content);
    } catch (error) {
      throw new Error(`Failed to parse durable artifact ${fileName}: ${getErrorMessage(error)}`, { cause: error });
    }
  } finally {
    try {
      fs.rmSync(directory, { recursive: true, force: true });
    } catch (error) {
      throw new Error(`Failed to remove durable artifact directory ${directory}: ${getErrorMessage(error)}`, { cause: error });
    }
  }
}

/**
 * Restores only explicit continuation lineage requested through request_continuation,
 * or an earlier attempt of the current run. Other runs always start fresh.
 * The parent may still be finishing its conclusion job when the relay dispatches
 * this run, so the parent is accepted once its continuation request artifact exists;
 * that artifact is uploaded only after threat detection accepted the parent's outputs.
 * Fork runs are never continuation sources.
 * @param {{github: any, context: any, core: any, readRequest?: (artifactId: number) => Promise<unknown>, readDispatchEvidence?: (artifactId: number) => Promise<unknown>, readContinuationContext?: (artifactId: number) => Promise<unknown>, persistContinuationContext?: (context: any) => Promise<void>, dispatchEvidenceWaitMs?: number}} options
 * @returns {Promise<{continuation: {depth: number, max: number | undefined, hints: string} | undefined}>}
 */
async function selectCheckpoint({ github, context, core, readRequest, readDispatchEvidence, readContinuationContext, persistContinuationContext, dispatchEvidenceWaitMs = DISPATCH_EVIDENCE_WAIT_MS }) {
  const workflow = process.env.GH_AW_DURABLE_WORKFLOW;
  if (!workflow || !/^[A-Za-z0-9_-]+$/.test(workflow)) throw new Error("Invalid durable workflow identifier");
  const { owner, repo } = context.repo;
  const { data: current } = await github.rest.actions.getWorkflowRun({ owner, repo, run_id: context.runId });
  if (current.head_repository?.id !== current.repository.id) throw new Error("Durable continuation is not supported for fork workflow runs");
  const prefix = checkpointArtifactPrefix(workflow, current);
  const currentAttempt = process.env.GITHUB_RUN_ATTEMPT || "1";
  core.setOutput("artifact-name", `${prefix}${context.runId}-${currentAttempt}`);
  core.setOutput("checkpoint-attempt", currentAttempt);
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
  let resumedContext;
  if (lineage && Number(currentAttempt) > 1) {
    const savedArtifacts = (await listArtifacts(current.id))
      .filter(item => {
        if (typeof item?.name !== "string" || !item.name.startsWith(CONTINUATION_CONTEXT_PREFIX)) return false;
        const attempt = item.name.slice(CONTINUATION_CONTEXT_PREFIX.length);
        return /^[1-9]\d*$/.test(attempt) && Number(attempt) < Number(currentAttempt) && isUnexpired(item);
      })
      .sort((a, b) => b.id - a.id);
    if (savedArtifacts.length > 0) {
      if (!readContinuationContext) throw new Error("Durable continuation reruns require access to the saved continuation context");
      /** @type {any} */
      const saved = await readContinuationContext(Number(savedArtifacts[0].id));
      if (
        !saved ||
        typeof saved !== "object" ||
        saved.version !== 1 ||
        String(saved.run_id) !== String(current.id) ||
        String(saved.parent_run_id) !== String(inbound?.run_id || "") ||
        String(saved.parent_run_attempt) !== String(lineage.parentRunAttempt || "") ||
        String(saved.checkpoint_run_attempt) !== String(lineage.checkpointAttempt || "") ||
        String(saved.relay_run_id) !== String(lineage.relayRunId || "") ||
        !/^[1-9]\d*$/.test(String(saved.parent_run_attempt)) ||
        !/^[1-9]\d*$/.test(String(saved.checkpoint_run_attempt)) ||
        !/^[1-9]\d*$/.test(String(saved.relay_run_id)) ||
        Number(saved.depth) !== lineage.depth ||
        !Number.isInteger(Number(saved.depth)) ||
        !Number.isInteger(Number(saved.max)) ||
        Number(saved.max) < 1 ||
        Number(saved.max) > 100 ||
        (lineage.max !== undefined && Number(saved.max) !== lineage.max) ||
        typeof saved.hints !== "string" ||
        !saved.hints.trim() ||
        saved.hints.length > 4000
      ) {
        throw new Error("Saved continuation context does not match the authenticated workflow lineage");
      }
      resumedContext = {
        depth: Number(saved.depth),
        max: Number(saved.max),
        hints: saved.hints,
      };
      parentRunId = String(saved.parent_run_id);
      parentAttempt = String(saved.parent_run_attempt);
      core.setOutput("relay-run-id", String(saved.relay_run_id));
    }
  }
  if (lineage && !resumedContext) {
    parentRunId = String(inbound?.run_id || "");
    if (!/^[1-9]\d*$/.test(parentRunId) || parentRunId === String(current.id)) throw new Error("aw_context.run_id must identify the run that requested continuation");
    const { data: parent } = await github.rest.actions.getWorkflowRun({ owner, repo, run_id: parentRunId });
    if (parent.workflow_id !== current.workflow_id || parent.repository.id !== current.repository.id || parent.head_repository?.id !== current.repository.id || parent.head_branch !== current.head_branch) {
      throw new Error("Durable parent must be a run of the same workflow and branch in this repository");
    }
    // Continuation lineage in aw_context is attacker-controllable by anyone who can dispatch
    // this workflow. Accept it only from an authentic relay run bound to this parent attempt.
    if (current.event !== "workflow_dispatch") throw new Error(`Continuation runs must be workflow_dispatch runs dispatched by ${RELAY_WORKFLOW_PATH}, not ${JSON.stringify(current.event)}`);
    if (!lineage.parentRunAttempt || !lineage.checkpointAttempt) throw new Error("Continuation lineage requires request and checkpoint producer attempts");
    if (!lineage.relayRunId) throw new Error(`Continuation lineage requires aw_context.relay_run_id; continuation runs are dispatched only by ${RELAY_WORKFLOW_PATH}`);
    if (current.actor?.login !== GITHUB_ACTIONS_BOT) throw new Error(`Continuation runs must be dispatched by the continuation relay as ${GITHUB_ACTIONS_BOT}, not ${JSON.stringify(current.actor?.login ?? "unknown")}`);
    const { data: relay } = await github.rest.actions.getWorkflowRun({ owner, repo, run_id: lineage.relayRunId });
    validateRelayRun(relay, { repositoryId: current.repository.id, defaultBranch: await getDefaultBranch(github, context), parentRunId, parentRunAttempt: lineage.parentRunAttempt });
    const deadline = Date.now() + dispatchEvidenceWaitMs;
    let dispatchMarker;
    do {
      const relayArtifacts = await listArtifacts(lineage.relayRunId);
      dispatchMarker = relayArtifacts.find(artifact => artifact.name === RELAY_DISPATCH_MARKER_ARTIFACT && isUnexpired(artifact));
      if (dispatchMarker || Date.now() >= deadline) break;
      await new Promise(resolve => setTimeout(resolve, Math.min(DISPATCH_EVIDENCE_POLL_MS, deadline - Date.now())));
    } while (Date.now() < deadline);
    if (!dispatchMarker) throw new Error(`Continuation relay run ${lineage.relayRunId} has not published dispatch evidence for child run ${current.id}`);
    if (!readDispatchEvidence) throw new Error("Durable continuation requires access to relay dispatch evidence");
    /** @type {any} */
    const evidence = await readDispatchEvidence(Number(dispatchMarker.id));
    if (
      !evidence ||
      typeof evidence !== "object" ||
      String(evidence.parent_run_id) !== parentRunId ||
      String(evidence.parent_run_attempt) !== lineage.parentRunAttempt ||
      String(evidence.checkpoint_run_attempt) !== String(lineage.checkpointAttempt || "") ||
      String(evidence.relay_run_id) !== lineage.relayRunId ||
      String(evidence.child_run_id) !== String(current.id)
    ) {
      throw new Error("Continuation relay dispatch evidence does not authorize this child run");
    }
    if (!readRequest) throw new Error("Durable continuation requires access to the parent continuation request");
    const artifacts = await listArtifacts(parent.id);
    parentArtifacts = artifacts;
    const found = findRequestArtifact(artifacts, lineage.parentRunAttempt);
    if (!found) throw new Error(`Continuation parent run ${parentRunId} has no unexpired continuation request; it did not request continuation or the request expired`);
    parentAttempt = found.attempt;
    const request = validateContinuationRequest(await readRequest(Number(found.artifact.id)), { parentRunId, parentRunAttempt: parentAttempt, max: lineage.max });
    if (request.depth !== lineage.depth) throw new Error(`Continuation depth ${lineage.depth} does not match the parent request depth ${request.depth}`);
    continuation = { depth: lineage.depth, max: lineage.max ?? request.max, hints: request.hints };
    if (!persistContinuationContext) throw new Error("Durable continuation requires persistence of authenticated hints for child reruns");
    await persistContinuationContext({
      version: 1,
      run_id: String(current.id),
      parent_run_id: parentRunId,
      parent_run_attempt: parentAttempt,
      checkpoint_run_attempt: String(lineage.checkpointAttempt),
      relay_run_id: lineage.relayRunId,
      depth: continuation.depth,
      max: continuation.max,
      hints: continuation.hints,
    });
  } else if (resumedContext) {
    continuation = resumedContext;
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
  if (!continuation) {
    core.info("No continuation was requested by a previous run; starting a new session");
    return { continuation };
  }
  if (!parentArtifacts || !lineage) return { continuation };
  if (!lineage.checkpointAttempt) throw new Error("Continuation lineage requires a checkpoint producer attempt");
  const checkpoint = findCheckpointArtifact(parentArtifacts, prefix, parentRunId, lineage.checkpointAttempt);
  if (checkpoint) {
    useCheckpoint(checkpoint, parentRunId);
    return { continuation };
  }
  core.warning(`Continuation parent run ${parentRunId} has no unexpired durable checkpoint; starting a new session with its continuation hints`);
  return { continuation };
}

/** Recheck integrity after host secret redaction, never upload a damaged store. */
module.exports = { selectCheckpoint, restoreCheckpoint, readContinuation };
