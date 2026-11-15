// @ts-check
"use strict";

const { parsePendingJSON, prepare, verify, readAuthorizedManifest, assertPendingPublication, integrityError } = require("./pending_writes.cjs");
const { readInput, describeDirectory } = require("./pending_writes_files.cjs");

function optionsFromEnv() {
  const spec = parsePendingJSON(process.env.GH_AW_PENDING_WRITES_SPEC || "{}");
  return {
    ...spec,
    agentOutputPath: process.env.GH_AW_AGENT_OUTPUT,
    manifestPath: process.env.GH_AW_PENDING_WRITES,
    handlerConfig: parsePendingJSON(process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG || "{}"),
    context,
    readToken: process.env.GITHUB_TOKEN,
    staged: process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true",
    workflowId: process.env.GH_AW_WORKFLOW_ID,
    run: {
      repository: process.env.GITHUB_REPOSITORY,
      workflow_ref: process.env.GITHUB_WORKFLOW_REF,
      workflow_sha: process.env.GITHUB_WORKFLOW_SHA,
      run_id: process.env.GITHUB_RUN_ID,
      run_attempt: process.env.GITHUB_RUN_ATTEMPT,
      event: process.env.GITHUB_EVENT_NAME,
    },
    detection: {
      enabled: spec.detectionEnabled === true,
      mode: spec.detectionMode,
      conclusion: process.env.GH_AW_DETECTION_CONCLUSION,
      reason: process.env.GH_AW_DETECTION_REASON,
      threatKinds: process.env.GH_AW_DETECTION_THREAT_KINDS,
    },
  };
}

async function main() {
  const options = optionsFromEnv();
  try {
    const result = await prepare(options);
    core.setOutput("sha256", result.sha256);
    core.setOutput("complete", String(result.manifest.complete));
    core.setOutput("bindings", JSON.stringify(result.bindings.map(({ id, kind, path }) => ({ id, kind, ...(path ? { path } : {}) }))));
    core.exportVariable("GH_AW_PENDING_WRITES", options.manifestPath);
    core.exportVariable("GH_AW_AGENT_OUTPUT", options.agentOutputPath);
    if (!result.manifest.complete) core.warning("Pending writes include an unsupported or incomplete plan; writes will be refused");
  } catch (cause) {
    core.setOutput("complete", "false");
    core.setFailed("Pending-write inputs could not be prepared; writes will be refused");
    return;
  }
}

async function verifyFromEnv() {
  return verify(process.env.GH_AW_PENDING_WRITES_SHA256, optionsFromEnv());
}

/** Read the supplied data using the same shape the compiler-owned plan bound. */
function publicationSupplied(spec) {
  if (["ledger", "sarif"].includes(spec.kind)) return parsePendingJSON(readInput(spec.path).bytes.toString("utf8"));
  if (spec.kind === "directory") return { id: spec.binding_id, kind: "directory", files: describeDirectory(spec.path) };
  if (spec.kind === "file") {
    const { length, sha256, mode } = readInput(spec.path);
    return { id: spec.binding_id, kind: "file", length, sha256, mode };
  }
  throw integrityError("unsupported_publication");
}

function checkPublication(spec) {
  const manifest = readAuthorizedManifest();
  const item = manifest.items.find(item => item.target.job === spec.job && item.publication?.id === spec.id);
  if (!item || (spec.repository && spec.repository !== item.target.repository) || (spec.branch && spec.branch !== item.target.branch)) throw integrityError("target_changed");
  const supplied = publicationSupplied(spec);
  assertPendingPublication(spec.job, spec.id, supplied);
  return supplied;
}

function checkPublicationFromEnv() {
  try {
    checkPublication(parsePendingJSON(process.env.GH_AW_PENDING_PUBLICATION || "{}"));
  } catch (cause) {
    core.setFailed("Pending-write publication inputs changed or are incomplete; no writes are authorized");
    return;
  }
}

module.exports = { main, optionsFromEnv, verifyFromEnv, publicationSupplied, checkPublication, checkPublicationFromEnv };
