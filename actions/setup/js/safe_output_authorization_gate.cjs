// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
const { parseStrictJSON } = require("./work_queue_codec.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { canonicalDigest, buildAuthorizationCaller, prepareInvocation, authorizePreparedInvocation, assertAuthorizationBatchFresh, recordAuthorizationOutcome, authorizationEvidencePath } = require("./safe_output_authorization.cjs");

function handlerAuthorizationRequired(config = {}) {
  const state = require("./safe_output_execution.cjs").currentSafeOutputInvocation();
  if (!state?.requirement && !config.authorization) return false;
  if (!state?.requirement || !state.invocation) throw new Error("Protected safe-output handlers require their trusted prepared invocation context");
  return true;
}

function recordExecutorOutcome(result, operationStatus, coreApi = global.core) {
  let evidenceRecorded = true;
  for (const authorization of result?.decisions || []) {
    const outcome = recordAuthorizationOutcome({ evidencePath: result.evidencePath, authorization, operationStatus, core: coreApi });
    if (!outcome.evidenceRecorded) evidenceRecorded = false;
  }
  return { evidenceRecorded };
}

/**
 * Complete handler-specific preparation at the live effect boundary. The
 * invocation context is compiler/manager-owned, never taken from message fields.
 *
 * @param {{inputs: unknown, scope?: {binding: string, refs: string[]}, config?: any}} options
 */
async function authorizeHandlerInvocation({ inputs, scope = { binding: "invocation", refs: [] }, config = {} }) {
  if (!handlerAuthorizationRequired(config)) return { state: "not_configured" };
  return requireAuthorization({ inputs, scope });
}

/** @param {{inputs: unknown, scope?: {binding: string, refs: string[]}}} options */
async function requireAuthorization({ inputs, scope = { binding: "invocation", refs: [] } }) {
  const state = require("./safe_output_execution.cjs").currentSafeOutputInvocation();
  if (!state?.requirement || !state.invocation) throw new Error("Protected safe-output handlers require their trusted prepared invocation context");
  const invocation = prepareInvocation({
    caller: state.invocation.caller,
    output: state.invocation.output,
    normalizedInputs: inputs,
    batchDigest: state.batchDigest,
    recordIndex: state.recordIndex,
    scope,
    context: state.invocation.context,
  });
  const evidencePath = authorizationEvidencePath();
  fs.mkdirSync(path.dirname(evidencePath), { recursive: true, mode: 0o700 });
  try {
    state.authorization = await authorizePreparedInvocation({ core: global.core, requirement: state.requirement, invocation, evidencePath });
    assertAuthorizationBatchFresh({ authorizations: [state.authorization] });
    return state.authorization;
  } catch (error) {
    if (error.authorization) state.authorization = error.authorization;
    throw error;
  }
}

/**
 * Hash only regular files in the executor's trusted staging directory.
 * The caller must pass these same files to the executor, not re-expand a glob.
 *
 * @param {string} root
 * @param {string[]} files
 * @returns {{path: string, digest: string, size: number}[]}
 */
function digestArtifactFiles(root, files) {
  const canonicalRoot = fs.realpathSync(root);
  if (!Array.isArray(files) || files.length > 1024) throw new Error("Safe-output authorization requires a bounded artifact list");
  return files.map(filename => {
    const declared = path.isAbsolute(filename) ? path.relative(path.resolve(root), filename) : filename;
    const absolute = path.resolve(canonicalRoot, declared);
    const relative = path.relative(canonicalRoot, absolute);
    if (!relative || relative.startsWith(`..${path.sep}`) || relative === ".." || path.isAbsolute(relative) || fs.realpathSync(absolute) !== absolute) {
      throw new Error("Safe-output authorization artifact escapes its trusted staging directory");
    }
    const fd = fs.openSync(absolute, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
    try {
      const before = fs.fstatSync(fd);
      if (!before.isFile()) throw new Error("Safe-output authorization artifact must be a regular file");
      const hash = crypto.createHash("sha256");
      const buffer = Buffer.alloc(64 * 1024);
      let size = 0;
      let count;
      while ((count = fs.readSync(fd, buffer, 0, buffer.length, null)) !== 0) {
        size += count;
        if (size > before.size) throw new Error("Safe-output authorization artifact changed during preparation");
        hash.update(buffer.subarray(0, count));
      }
      const after = fs.fstatSync(fd);
      if (size !== before.size || before.size !== after.size || before.mtimeMs !== after.mtimeMs || before.ctimeMs !== after.ctimeMs) {
        throw new Error("Safe-output authorization artifact changed during preparation");
      }
      return { path: relative.split(path.sep).join("/"), digest: hash.digest("hex"), size };
    } finally {
      fs.closeSync(fd);
    }
  });
}

/**
 * Collect trusted source ordinals independently of the payload supplied to an
 * external action. Identical records remain distinct logical invocations.
 *
 * @param {Record<string, unknown>[]} batch
 * @param {string} type
 */
function executorRecords(batch, type) {
  const normalize = require("./safe_output_names.cjs").normalizeSafeOutputName;
  return batch.flatMap((record, index) => (typeof record.type === "string" && normalize(record.type) === normalize(type) ? [{ index, inputs: record }] : []));
}

/**
 * @param {Record<string, any>} options
 */
async function main(options = {}) {
  const coreApi = options.core || global.core;
  const env = options.env || process.env;
  const descriptor = options.descriptor || parseStrictJSON(process.env.GH_AW_SAFE_OUTPUT_AUTHORIZATION_GATE || "{}");
  if (!descriptor || typeof descriptor !== "object" || Array.isArray(descriptor) || typeof descriptor.type !== "string") {
    throw new Error("Safe-output authorization gate requires a trusted executor descriptor");
  }
  const staged = isStagedMode(descriptor);
  // Author-provided setup, action hooks and script factories never run to preview
  // a protected external executor.
  if (staged) {
    coreApi.debug?.("Safe-output authorization gate: skipped_staged before loading invocations or Claims");
    coreApi.info(`Staged safe-output executor preview: ${descriptor.type}`);
    coreApi.setOutput("authorized", "false");
    return { state: "skipped_staged", decisions: [] };
  }
  const batch =
    options.batch ||
    (() => {
      const output = require("./load_agent_output.cjs").loadAgentOutput({ partitioning: true });
      if (!output.success) throw new Error("Safe-output authorization gate cannot load the validated invocation batch");
      return output.items;
    })();
  if (!Array.isArray(batch) || batch.length > 1024) throw new Error("Safe-output authorization gate requires a bounded invocation batch");
  let records = options.records || executorRecords(batch, descriptor.type);
  const normalize = require("./safe_output_names.cjs").normalizeSafeOutputName;
  const indices = new Set();
  for (const record of records) {
    if (!Number.isSafeInteger(record.index) || record.index < 0 || !batch[record.index] || normalize(batch[record.index].type) !== normalize(descriptor.type) || indices.has(record.index)) {
      throw new Error("Safe-output authorization executor record has no unique source invocation");
    }
    indices.add(record.index);
  }
  if (descriptor.type === "call_workflow" && String(descriptor.instance || "").startsWith("call_workflow:launch:")) {
    const sourceIndex = env.GH_AW_AUTHORIZATION_SOURCE_RECORD_INDEX;
    if (typeof sourceIndex !== "string" || !/^(?:0|[1-9][0-9]{0,3})$/.test(sourceIndex) || !indices.has(Number(sourceIndex))) {
      throw new Error("Safe-output authorization launch requires its trusted source invocation");
    }
    records = records.filter(record => record.index === Number(sourceIndex));
  }
  const claimHandle = env.GH_AW_AUTHORIZATION_CLAIM_HANDLE;
  if (String(descriptor.instance || "").endsWith(":prepare_claim_adapter") && !claimHandle) {
    throw new Error("Safe-output authorization preparation requires a trusted Claim identity");
  }
  const claim = claimHandle ? options.claimIdentity || require("./work_queue_claim_scope.cjs").claimIdentity(claimHandle) : null;
  if (claim) {
    if (claim.claim_handle !== claimHandle) throw new Error("Safe-output authorization preparation has a foreign Claim identity");
    records = records.filter(record => record.inputs.claim_handle === claimHandle);
  }
  const payload = options.payload ?? process.env.GH_AW_AUTHORIZATION_PAYLOAD;
  if (payload !== undefined) {
    if (records.length !== 1) throw new Error("Safe-output authorization action payload must bind exactly one invocation");
    let inputs = payload;
    if (typeof payload === "string") {
      const envelope = parseStrictJSON(`{"payload":${payload}}`, { finiteNumbersIn: "payload" });
      if (Object.keys(envelope).length !== 1) throw new Error("Safe-output authorization action payload must be one JSON value");
      inputs = envelope.payload;
    }
    records = [{ index: records[0].index, inputs }];
  }
  if (!records.length) {
    coreApi.debug?.("Safe-output authorization gate: not_requested");
    coreApi.setOutput("authorized", "false");
    return { state: "not_requested", decisions: [] };
  }
  const batchDigest = canonicalDigest(batch);
  coreApi.debug?.(`Safe-output authorization gate: preparing records=${records.length} batch_digest=${batchDigest}`);
  const decisions = [];
  const evidencePath = options.evidencePath || (options.authorize ? undefined : authorizationEvidencePath(env));
  if (!options.authorize) {
    if (!env.RUNNER_TEMP || !descriptor.authorization) throw new Error("Safe-output authorization gate requires trusted configuration and evidence storage");
    fs.mkdirSync(path.dirname(evidencePath), { recursive: true, mode: 0o700 });
  }
  const authorize =
    options.authorize ||
    (async ({ record }) => {
      const root = descriptor.artifact_root || env.GH_AW_AUTHORIZATION_ARTIFACT_ROOT;
      let artifacts = record.artifacts || [];
      if (root && !record.artifacts) {
        const inputs = record.inputs;
        const filename = typeof inputs.file === "string" ? inputs.file : typeof inputs.path === "string" ? `${crypto.createHash("sha256").update(inputs.path).digest("hex")}${path.extname(inputs.path).toLowerCase()}` : inputs.fileName;
        if (typeof filename !== "string" || !filename) throw new Error("Safe-output authorization artifact has no prepared identity");
        artifacts = digestArtifactFiles(root, [filename]);
      }
      const invocation = prepareInvocation({
        caller: buildAuthorizationCaller(env),
        output: {
          type: descriptor.type,
          name: descriptor.instance || descriptor.type,
          executor: descriptor.executor,
          implementation_digest: canonicalDigest(descriptor.implementation),
        },
        normalizedInputs: { inputs: record.inputs, artifacts },
        batchDigest,
        recordIndex: record.index,
        scope: { binding: "invocation", refs: artifacts.length ? [`sha256:${canonicalDigest(artifacts)}`] : [] },
        context: {
          policy_digest: canonicalDigest(descriptor),
          ...(claim ? { claim_id: claim.claim_id, delivery_id: claim.dispatch_id } : {}),
        },
      });
      return authorizePreparedInvocation({ core: coreApi, requirement: descriptor.authorization, invocation, evidencePath, env, ...(options.fetchImpl ? { fetchImpl: options.fetchImpl } : {}) });
    });
  try {
    for (const record of records) {
      const result = await authorize({ descriptor, batchDigest, record, staged: false });
      decisions.push(result);
    }
    if (options.authorize) {
      for (const decision of decisions) {
        if (decision.state !== "allowed" || Date.parse(decision.expires_at) - Date.now() < 2000) throw new Error("Safe-output authorization batch has no fresh decision for every invocation");
      }
    } else {
      assertAuthorizationBatchFresh({ authorizations: decisions });
    }
  } catch (error) {
    if (!options.authorize) recordExecutorOutcome({ decisions, evidencePath }, "skipped", coreApi);
    throw error;
  }
  coreApi.setOutput("authorized", "true");
  coreApi.debug?.(`Safe-output authorization gate: released records=${decisions.length}`);
  return { state: "allowed", decisions, evidencePath };
}

module.exports = { digestArtifactFiles, executorRecords, handlerAuthorizationRequired, requireAuthorization, authorizeHandlerInvocation, recordExecutorOutcome, main };
