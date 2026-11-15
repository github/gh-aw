// @ts-check
"use strict";

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { canonicalPendingJSON, inputDigest, parsePendingJSON, assertManifestInputs, integrityError } = require("./pending_writes.cjs");
const { readInput, describeDirectory, sha256 } = require("./pending_writes_files.cjs");
const { withoutCredentials } = require("./pending_writes_plan.cjs");
const { redactBuiltInPatterns } = require("./redact_secrets.cjs");

const SNAPSHOT_SCHEMA = "gh-aw/pending-write-inputs/v1";
const SNAPSHOT_FILENAME = "pending-write-inputs.json";
const MAX_SNAPSHOT_BYTES = 512 * 1024 * 1024;
/** @typedef {{id: string, kind: "value"} | {id: string, kind: "file" | "directory", scope: string, path: string, artifact_path: string}} SnapshotBinding */

function relativePath(value) {
  if (typeof value !== "string" || !value || path.isAbsolute(value) || value.includes("\\") || value.split("/").some(part => !part || part === "." || part === "..")) throw integrityError("snapshot_path_invalid");
  return value;
}

function roots(runnerTemp = process.env.RUNNER_TEMP || os.tmpdir()) {
  if (!path.isAbsolute(runnerTemp)) throw integrityError("snapshot_path_invalid");
  return { gh_aw: "/tmp/gh-aw", runner_temp_gh_aw: path.join(runnerTemp, "gh-aw") };
}

function scopedPath(file, runnerTemp) {
  for (const [scope, root] of Object.entries(roots(runnerTemp))) {
    const relative = path.relative(root, file);
    if (relative && !relative.startsWith("../") && !path.isAbsolute(relative)) return { scope, path: relativePath(relative) };
  }
  throw integrityError("snapshot_path_invalid");
}

function realDirectory(directory) {
  const pending = [];
  let current = path.resolve(directory);
  while (!fs.existsSync(current)) {
    pending.push(current);
    current = path.dirname(current);
  }
  for (let parent = current; ; parent = path.dirname(parent)) {
    if (!fs.lstatSync(parent).isDirectory()) throw integrityError("snapshot_path_invalid");
    if (parent === path.dirname(parent)) break;
  }
  for (const item of pending.reverse()) fs.mkdirSync(item, { mode: 0o700 });
}

function assertNoSnapshotSecrets(bytes) {
  for (const [key, value] of Object.entries(process.env)) {
    if (/(?:TOKEN|SECRET|PASSWORD|PRIVATE_KEY)/.test(key) && value && value.length >= 8 && bytes.includes(Buffer.from(value))) throw integrityError("snapshot_secret");
  }
  const text = bytes.toString("utf8");
  if (redactBuiltInPatterns(text).content !== text) throw integrityError("snapshot_secret");
}

function writeSnapshotFile(file, bytes, mode) {
  assertNoSnapshotSecrets(bytes);
  realDirectory(path.dirname(file));
  const fd = fs.openSync(file, fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_EXCL | fs.constants.O_NOFOLLOW, mode === "100755" ? 0o700 : 0o600);
  try {
    fs.writeFileSync(fd, bytes);
  } finally {
    fs.closeSync(fd);
  }
}

/** Preserve only exact, digest-bound original data, never credentials. */
/** @param {{directory: string, manifest: any, bindings: any[], handlerConfig: any, gateConfig: any, context?: any, runnerTemp?: string, publicationInputs?: any[]}} options */
function writePendingInputSnapshot({ directory, manifest, bindings, handlerConfig, gateConfig, context = undefined, runnerTemp = undefined, publicationInputs = [] }) {
  /** @type {SnapshotBinding[]} */
  const snapshotBindings = [];
  const metadata = {
    schema: SNAPSHOT_SCHEMA,
    manifest_sha256: inputDigest(manifest),
    run: manifest.run,
    handler_config: withoutCredentials(handlerConfig),
    gate_config: gateConfig,
    context: context || bindings.find(binding => binding.id === "context")?.value,
    environment: bindings.find(binding => binding.id === "environment")?.value,
    publication_invocations: bindings.find(binding => binding.id === "publication_invocations")?.value,
    generated_inputs: publicationInputs.filter(input => input.path).map(input => ({ job: input.job, id: input.id, input_id: input.input_id, ...scopedPath(input.path, runnerTemp), value: input.value })),
    bindings: snapshotBindings,
  };
  assertManifestInputs(manifest, bindings);
  let totalBytes = 0;
  const copy = (source, target, mode) => {
    const input = readInput(source);
    totalBytes += input.length;
    if (totalBytes > MAX_SNAPSHOT_BYTES) throw integrityError("snapshot_limit");
    writeSnapshotFile(target, input.bytes, mode);
  };
  bindings.forEach((binding, index) => {
    if (binding.kind === "value") {
      if (!["handler_config", "gate_config", "context", "environment", "publication_invocations"].includes(binding.id)) throw integrityError("snapshot_input_unsupported");
      metadata.bindings.push({ id: binding.id, kind: "value" });
      return;
    }

    const scoped = scopedPath(binding.path, runnerTemp);
    if (binding.kind === "directory" && !path.relative(binding.path, directory).startsWith("..")) throw integrityError("snapshot_path_invalid");
    const artifactPath = `inputs/${index}`;
    metadata.bindings.push({ id: binding.id, kind: binding.kind, ...scoped, artifact_path: artifactPath });
    if (binding.kind === "file") {
      const input = manifest.inputs.find(input => input.id === binding.id);
      copy(binding.path, path.join(directory, artifactPath), input.mode);
    } else if (binding.kind === "directory") {
      realDirectory(path.join(directory, artifactPath));
      for (const file of describeDirectory(binding.path)) copy(path.join(binding.path, relativePath(file.path)), path.join(directory, artifactPath, file.path), file.mode);
    } else throw integrityError("snapshot_input_unsupported");
  });
  assertManifestInputs(manifest, bindings);
  const bytes = Buffer.from(canonicalPendingJSON(metadata));
  writeSnapshotFile(path.join(directory, SNAPSHOT_FILENAME), bytes, "100644");
  writeSnapshotFile(path.join(directory, "pending-writes.json"), Buffer.from(canonicalPendingJSON(manifest)), "100644");
  return { sha256: sha256(bytes), metadata };
}

/** @param {{directory: string, expectedSha256: string | undefined, manifest: any}} options */
function verifyPendingInputSnapshot({ directory, expectedSha256, manifest }) {
  const input = readInput(path.join(directory, SNAPSHOT_FILENAME));
  if (input.sha256 !== expectedSha256) throw integrityError("snapshot_changed");
  const metadata = parsePendingJSON(input.bytes.toString("utf8"));
  if (metadata.schema !== SNAPSHOT_SCHEMA || metadata.manifest_sha256 !== inputDigest(manifest) || inputDigest(metadata.run) !== inputDigest(manifest.run) || !Array.isArray(metadata.bindings)) throw integrityError("snapshot_invalid");
  if (readInput(path.join(directory, "pending-writes.json")).sha256 !== metadata.manifest_sha256) throw integrityError("snapshot_changed");
  if (
    canonicalPendingJSON(metadata) !== input.bytes.toString("utf8") ||
    Object.keys(metadata).some(key => !["schema", "manifest_sha256", "run", "handler_config", "gate_config", "context", "environment", "publication_invocations", "generated_inputs", "bindings"].includes(key))
  )
    throw integrityError("snapshot_invalid");
  const descriptions = metadata.bindings.map(binding => {
    const expected = manifest.inputs.find(input => input.id === binding.id);
    if (!expected || binding.kind !== expected.kind) throw integrityError("snapshot_invalid");
    if (binding.kind === "value") {
      if (!["handler_config", "gate_config", "context", "environment", "publication_invocations"].includes(binding.id)) throw integrityError("snapshot_invalid");
      return { id: binding.id, kind: "value", sha256: inputDigest(metadata[binding.id]) };
    }
    const source = path.join(directory, relativePath(binding.artifact_path));
    if (binding.kind === "file") {
      const { length, sha256 } = readInput(source);
      return { id: binding.id, kind: "file", length, sha256, mode: expected.mode };
    }
    if (binding.kind !== "directory") throw integrityError("snapshot_invalid");
    const files = describeDirectory(source).map(file => ({ ...file, mode: expected.files.find(original => original.path === file.path)?.mode || file.mode }));
    return { id: binding.id, kind: "directory", files };
  });
  if (inputDigest(descriptions) !== inputDigest(manifest.inputs)) throw integrityError("snapshot_changed");
  for (const generated of metadata.generated_inputs || []) {
    const item = manifest.items.find(item => item.target.job === generated.job && item.publication?.id === generated.id);
    if (!item || item.publication.sha256 !== inputDigest(generated.value)) throw integrityError("snapshot_generated_invalid");
  }
  return metadata;
}

/** Restore all inputs only after validating the original run and metadata hash. */
/** @param {{directory: string, expectedSha256: string, manifest: any, runnerTemp?: string}} options */
function restorePendingInputs({ directory, expectedSha256, manifest, runnerTemp = undefined }) {
  const metadata = verifyPendingInputSnapshot({ directory, expectedSha256, manifest });
  const destinations = roots(runnerTemp);
  const ids = new Set();
  const bindings = metadata.bindings.map(binding => {
    if (!binding || typeof binding.id !== "string" || ids.has(binding.id)) throw integrityError("snapshot_invalid");
    ids.add(binding.id);
    if (binding.kind === "value") {
      if (!["handler_config", "gate_config", "context", "environment", "publication_invocations"].includes(binding.id)) throw integrityError("snapshot_invalid");
      return { id: binding.id, kind: "value", value: metadata[binding.id] };
    }
    const root = destinations[binding.scope];
    if (!root || !["file", "directory"].includes(binding.kind)) throw integrityError("snapshot_invalid");
    return { id: binding.id, kind: binding.kind, path: path.join(root, relativePath(binding.path)), artifact_path: relativePath(binding.artifact_path) };
  });
  for (const binding of bindings) {
    if (binding.kind === "value") continue;
    const source = path.join(directory, binding.artifact_path);
    const restoreFile = (from, to, mode) => {
      const input = readInput(from);
      realDirectory(path.dirname(to));
      if (fs.existsSync(to)) {
        const existing = fs.lstatSync(to);
        if (!existing.isFile() || existing.nlink !== 1) throw integrityError("snapshot_path_invalid");
      }
      const fd = fs.openSync(to, fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_TRUNC | fs.constants.O_NOFOLLOW, mode === "100755" ? 0o700 : 0o600);
      try {
        fs.writeFileSync(fd, input.bytes);
        fs.fchmodSync(fd, mode === "100755" ? 0o700 : 0o600);
      } finally {
        fs.closeSync(fd);
      }
    };
    if (binding.kind === "file") {
      restoreFile(source, binding.path, manifest.inputs.find(input => input.id === binding.id).mode);
    } else {
      realDirectory(binding.path);
      // Existing extra files are never silently deleted to force a match.
      const expected = manifest.inputs.find(input => input.id === binding.id);
      for (const file of describeDirectory(source)) restoreFile(path.join(source, file.path), path.join(binding.path, file.path), expected.files.find(original => original.path === file.path).mode);
    }
  }
  assertManifestInputs(manifest, bindings);
  const generatedBindings = (metadata.generated_inputs || []).map(input => {
    const root = destinations[input.scope];
    if (!root) throw integrityError("snapshot_generated_invalid");
    const target = path.join(root, relativePath(input.path));
    realDirectory(path.dirname(target));
    if (fs.existsSync(target)) {
      const existing = fs.lstatSync(target);
      if (!existing.isFile() || existing.nlink !== 1) throw integrityError("snapshot_path_invalid");
    }
    const fd = fs.openSync(target, fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_TRUNC | fs.constants.O_NOFOLLOW, 0o600);
    try {
      fs.writeFileSync(fd, canonicalPendingJSON(input.value));
    } finally {
      fs.closeSync(fd);
    }
    return { id: input.input_id, kind: "file", path: target };
  });
  const invocations = (metadata.publication_invocations || []).map(invocation => {
    const env = { ...invocation.env };
    for (const [name, id] of Object.entries(invocation.path_env || {})) {
      const binding = [...bindings, ...generatedBindings].find(binding => binding.id === id);
      if (!binding?.path) throw integrityError("snapshot_invocation_invalid");
      env[name] = binding.path;
    }
    return { ...invocation, env };
  });
  const agentOutput = bindings.find(binding => binding.id === "agent_output");
  if (!agentOutput || agentOutput.kind !== "file") throw integrityError("snapshot_invalid");
  return {
    handlerConfig: metadata.handler_config,
    gateConfig: metadata.gate_config,
    context: metadata.context,
    environment: metadata.environment,
    publication_invocations: invocations,
    generatedBindings,
    bindings: bindings.map(({ artifact_path, ...binding }) => binding),
    agentOutputPath: agentOutput.path,
  };
}

module.exports = { SNAPSHOT_SCHEMA, SNAPSHOT_FILENAME, writePendingInputSnapshot, verifyPendingInputSnapshot, restorePendingInputs };
