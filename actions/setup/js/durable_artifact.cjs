// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { createHash } = require("node:crypto");
const { assertNoSecrets, SOURCE, VERSION } = require("./pi_durable_checkpoint.cjs");
const { readInboundAwContext } = require("./aw_context.cjs");
const { DefaultArtifactClient } = require("./artifact_client.cjs");
const { restoreFiles } = require("./durable_files.cjs");
const { preparePrompt } = require("./durable_session.cjs");

/** Select and restore a checkpoint before exposing continuation to the engine. */
async function restoreCheckpoint({ github, context, core, token, artifactClient = new DefaultArtifactClient(), root = "/tmp/gh-aw/agent", promptFile }) {
  const outputs = new Map();
  await selectCheckpoint({
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
  if (!outputs.has("artifact-id")) return;
  await artifactClient.downloadArtifact(Number(outputs.get("artifact-id")), {
    path: path.join(root, "durable/bundle"),
    findBy: { token, repositoryOwner: context.repo.owner, repositoryName: context.repo.repo },
  });
  restoreFiles({ core: { ...core, setOutput() {} }, root });
  if (["copilot", "claude", "codex", "gemini", "agy"].includes(process.env.GH_AW_DURABLE_ENGINE || "")) {
    preparePrompt({ env: { ...process.env, GH_AW_DURABLE_RESUMED: "true" }, promptFile });
  }
  core.setOutput("restored", "true");
}

/**
 * Explicit aw_context lineage wins over the latest successful same-branch run.
 * Fork runs and other in-progress writers are never continuation sources.
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
  const explicitParent = String(inbound?.run_id || "") !== String(current.id) && inbound?.run_id ? inbound.run_id : inbound?.parent_run_id || undefined;
  if (explicitParent !== undefined && !/^[1-9]\d*$/.test(String(explicitParent))) throw new Error("aw_context parent run ID must be a positive integer");
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
  if (Number(process.env.GITHUB_RUN_ATTEMPT || "1") > 1 && (await restoreFrom(current))) return;
  let parent;
  if (explicitParent) {
    ({ data: parent } = await github.rest.actions.getWorkflowRun({ owner, repo, run_id: String(explicitParent) }));
  } else {
    if (!current.head_branch) throw new Error("Durable continuation requires a branch or an explicit aw_context parent run");
    const {
      data: { workflow_runs: runs },
    } = await github.rest.actions.listWorkflowRuns({
      owner,
      repo,
      workflow_id: current.workflow_id,
      per_page: 100,
      branch: current.head_branch,
      status: "success",
    });
    parent = runs.find(
      run =>
        run.id !== current.id && run.run_number < current.run_number && run.workflow_id === current.workflow_id && run.head_branch === current.head_branch && run.head_repository?.id === current.repository.id && run.conclusion === "success"
    );
  }
  if (parent) {
    if (
      parent.workflow_id !== current.workflow_id ||
      parent.repository.id !== current.repository.id ||
      parent.head_repository?.id !== current.repository.id ||
      parent.head_branch !== current.head_branch ||
      (parent.id !== current.id && parent.status !== "completed")
    ) {
      throw new Error("Durable parent must be a completed run of the same workflow and branch in this repository");
    }
    if (await restoreFrom(parent)) return;
    if (explicitParent) throw new Error("The explicit durable parent run has no unexpired checkpoint artifact");
  }
  core.info("The last successful same-branch workflow run has no unexpired durable checkpoint; starting a new session");
}

/** Recheck integrity after host secret redaction, never upload a damaged store. */
function validateCheckpoint({ core }) {
  const file = process.env.GH_AW_DURABLE_CHECKPOINT;
  if (!file || path.dirname(file) !== "/tmp/gh-aw/agent/durable" || !/^pi-durable-[A-Za-z0-9_-]+\.json$/.test(path.basename(file))) {
    throw new Error("Invalid durable artifact checkpoint path");
  }
  if (!fs.existsSync(file)) {
    core.info("The engine did not produce a durable checkpoint");
    return;
  }
  if (fs.lstatSync("/tmp/gh-aw/agent/durable").isSymbolicLink() || !fs.lstatSync(file).isFile()) throw new Error("Durable checkpoints must be regular files");
  const checkpoint = JSON.parse(fs.readFileSync(file, "utf8"));
  assertNoSecrets(checkpoint);
  if (process.env.GH_AW_DURABLE_ENGINE === "pi") {
    if (
      checkpoint.source !== SOURCE ||
      checkpoint.packageVersion !== VERSION ||
      checkpoint.contractVersion !== 1 ||
      checkpoint.storage !== "artifact" ||
      checkpoint.scope !== path.basename(file, ".json") ||
      !checkpoint.files ||
      !Object.hasOwn(checkpoint.files, "main.jsonl") ||
      !checkpoint.hashes ||
      JSON.stringify(Object.keys(checkpoint.files).sort()) !== JSON.stringify(Object.keys(checkpoint.hashes).sort())
    ) {
      throw new Error("Invalid Pi durable checkpoint manifest");
    }
    for (const [name, content] of Object.entries(checkpoint.files)) {
      if (!/^(?:main|(?:doc|task)-(?:0|[1-9]\d*))\.jsonl$/.test(name) || typeof content !== "string" || checkpoint.hashes[name] !== createHash("sha256").update(content).digest("hex")) {
        throw new Error("Pi durable checkpoint was damaged or redacted; refusing publication");
      }
    }
  }
  core.setOutput("present", "true");
}

module.exports = { selectCheckpoint, restoreCheckpoint, validateCheckpoint };
