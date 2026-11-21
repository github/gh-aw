// @ts-check
"use strict";

const path = require("node:path");
const { createHash } = require("node:crypto");
const { readInboundAwContext } = require("./aw_context.cjs");
const { DefaultArtifactClient } = require("./artifact_client.cjs");
const { restoreFiles } = require("./durable_files.cjs");
const { preparePrompt } = require("./durable_session.cjs");
const { tryRecordDurableSessionEvent } = require("./durable_session_record.cjs");

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

const MAX_CONTINUATION_HINTS_LENGTH = 4000;

/**
 * Reads continuation metadata written by the request_continuation safe output.
 * @param {any} awContext
 * @returns {{depth: number, max: number | undefined, hints: string} | undefined}
 */
function readContinuation(awContext) {
  if (!awContext || !Object.hasOwn(awContext, "continuation")) return undefined;
  const continuation = awContext.continuation;
  if (!continuation || typeof continuation !== "object" || Array.isArray(continuation)) throw new Error("aw_context.continuation must be an object");
  const depth = Number(continuation.depth);
  if (!Number.isInteger(depth) || depth < 1) throw new Error("aw_context.continuation.depth must be a positive integer");
  const max = continuation.max === undefined ? undefined : Number(continuation.max);
  if (max !== undefined && (!Number.isInteger(max) || max < 1)) throw new Error("aw_context.continuation.max must be a positive integer");
  const hints = typeof continuation.hints === "string" ? continuation.hints.slice(0, MAX_CONTINUATION_HINTS_LENGTH) : "";
  return { depth, max, hints };
}

/**
 * Restores only explicit continuation lineage requested through request_continuation,
 * or an earlier attempt of the current run. Other runs always start fresh.
 * Fork runs and other in-progress writers are never continuation sources.
 * @returns {Promise<{continuation: {depth: number, max: number | undefined, hints: string} | undefined}>}
 */
async function selectCheckpoint({ github, context, core }) {
  const workflow = process.env.GH_AW_DURABLE_WORKFLOW;
  if (!workflow || !/^[A-Za-z0-9_-]+$/.test(workflow)) throw new Error("Invalid durable workflow identifier");
  const { owner, repo } = context.repo;
  const { data: current } = await github.rest.actions.getWorkflowRun({ owner, repo, run_id: context.runId });
  if (current.head_repository?.id !== current.repository.id) throw new Error("Durable continuation is not supported for fork workflow runs");
  const scope = createHash("sha256")
    .update(JSON.stringify([current.repository.id, current.workflow_id, current.head_branch]))
    .digest("hex")
    .slice(0, 24);
  const prefix = `durable-${workflow}-${scope}-`;
  core.setOutput("artifact-name", `${prefix}${context.runId}-${process.env.GITHUB_RUN_ATTEMPT || "1"}`);
  const inbound = readInboundAwContext(context.payload);
  const rawContext = context.payload.inputs?.aw_context ?? context.payload.client_payload?.aw_context;
  if (rawContext && !inbound) throw new Error("Durable continuation requires a valid aw_context object");
  const continuation = readContinuation(inbound);
  core.setOutput("continuation-depth", String(continuation?.depth ?? 0));
  async function restoreFrom(run) {
    const artifacts = await github.paginate(github.rest.actions.listWorkflowRunArtifacts, { owner, repo, run_id: run.id, per_page: 100 });
    const artifact = artifacts
      .filter(item => {
        const runPrefix = `${prefix}${run.id}-`;
        if (!item.name.startsWith(runPrefix) || item.expired) return false;
        const attempt = item.name.slice(runPrefix.length);
        if (!/^[1-9]\d*$/.test(attempt) || (run.id === current.id && Number(attempt) >= Number(process.env.GITHUB_RUN_ATTEMPT || "1"))) return false;
        const expiresAt = new Date(item.expires_at).getTime();
        if (!Number.isFinite(expiresAt)) throw new Error("Durable checkpoint artifact has an invalid expiration date");
        return expiresAt > Date.now();
      })
      .sort((a, b) => b.id - a.id)[0];
    if (!artifact) return false;
    core.setOutput("artifact-id", String(artifact.id));
    core.setOutput("run-id", String(run.id));
    core.setOutput("parent-run-id", String(run.id));
    core.info(`Restoring durable checkpoint from run ${run.id}`);
    return true;
  }
  if (Number(process.env.GITHUB_RUN_ATTEMPT || "1") > 1 && (await restoreFrom(current))) return { continuation };
  if (!continuation) {
    core.info("No continuation was requested by a previous run; starting a new session");
    return { continuation };
  }
  const parentRunId = String(inbound?.run_id || "");
  if (!/^[1-9]\d*$/.test(parentRunId) || parentRunId === String(current.id)) throw new Error("aw_context.run_id must identify the run that requested continuation");
  const { data: parent } = await github.rest.actions.getWorkflowRun({ owner, repo, run_id: parentRunId });
  if (parent.workflow_id !== current.workflow_id || parent.repository.id !== current.repository.id || parent.head_repository?.id !== current.repository.id || parent.head_branch !== current.head_branch || parent.status !== "completed") {
    throw new Error("Durable parent must be a completed run of the same workflow and branch in this repository");
  }
  if (await restoreFrom(parent)) return { continuation };
  core.warning(`Continuation parent run ${parentRunId} has no unexpired durable checkpoint; starting a new session with its continuation hints`);
  return { continuation };
}

/** Recheck integrity after host secret redaction, never upload a damaged store. */
module.exports = { selectCheckpoint, restoreCheckpoint, readContinuation };
