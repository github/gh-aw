// @ts-check

const childProcess = require("child_process");
const crypto = require("crypto");
const fs = require("fs");
const os = require("os");
const path = require("path");
const zlib = require("zlib");
const { execLocalGitSync } = require("./git_helpers.cjs");
const { buildGitAuthEnv } = require("./git_auth_env.cjs");
const { getPatchPathForBranchInRepo } = require("./git_patch_utils.cjs");
const { getExcludedFiles, WORKFLOW_COMPILATION_NOTICE } = require("./workflow_file_exclusions.cjs");
const { currentClaimAssignment, currentClaimHandle } = require("./work_queue_claim_scope.cjs");

const MAGIC = Buffer.from("GH-AW-OBJECT-PACK 1\n");
const MAX_BYTES = 64 * 1024 * 1024;
const MAX_OBJECTS = 100000;
const MAX_BUFFER = MAX_BYTES + 16 * 1024 * 1024;
const oidPattern = /^(?:[0-9a-f]{40}|[0-9a-f]{64})$/;
const hash = bytes => crypto.createHash("sha256").update(bytes).digest("hex");

/** @param {"recovery-empty" | "credential-resolution" | "cleanup-failed" | "offline-result" | "target-resolution" | "head-authorization" | "patch-resolution" | "queue-resolution" | "queued" | "queue-failed" | "export-start" | "inventory-ready" | "export-ready" | "export-failed" | "recovery-start" | "preview-verified" | "fetch-start" | "fetch-ready" | "import-ready" | "baseline-resolved" | "regeneration-ready" | "recovery-ready" | "recovery-rejected"} event
 * @param {Record<string, boolean | number>} [fields]
 */
function traceObjectPack(event, fields = {}) {
  const allowed = new Set(["configured", "success", "eligible", "pinned", "staged", "incremental", "scoped", "credential", "dispatch", "objects", "bytes", "previewBytes"]);
  const details = Object.entries(fields)
    .filter(([key, value]) => allowed.has(key) && (typeof value === "boolean" || (typeof value === "number" && Number.isSafeInteger(value) && value >= 0)))
    .map(([key, value]) => `${key}=${value}`);
  globalThis.core?.debug?.(`[object_pack_transport] ${event}${details.length ? ` ${details.join(" ")}` : ""}`);
}

function cleanupRecovery(directory, files = []) {
  let failed = false;
  for (const file of files) {
    try {
      fs.unlinkSync(file);
    } catch (error) {
      if (!error || typeof error !== "object" || !("code" in error) || error.code !== "ENOENT") failed = true;
    }
  }
  try {
    if (directory) fs.rmSync(directory, { recursive: true, force: true });
  } catch {
    failed = true;
  }
  if (failed) {
    traceObjectPack("cleanup-failed");
    throw new Error("Unable to clean temporary object-pack recovery data. Publication must not proceed.");
  }
}

function isObjectPack(bytes) {
  return bytes.subarray(0, MAGIC.length).equals(MAGIC);
}

function canRecoverPatch(result) {
  const eligible =
    !result.success &&
    (["missing_base", "missing_pr_head", "shallow_history"].includes(result.errorCode) || /^Failed to generate (?:filtered )?patch:/.test(result.error || "") || /^Pinned commit failed to generate patch:/.test(result.error || ""));
  traceObjectPack("patch-resolution", { eligible });
  return eligible;
}

async function recoveryToken(client, configuredToken) {
  try {
    let token = configuredToken;
    if (!token && typeof client.auth === "function") {
      const authentication = await client.auth();
      if (typeof authentication?.token === "string") token = authentication.token;
    }
    token ??= process.env.GITHUB_TOKEN;
    traceObjectPack("credential-resolution", { configured: !!configuredToken, credential: !!token });
    return token;
  } catch {
    traceObjectPack("credential-resolution", { configured: !!configuredToken, credential: false });
    throw new Error("Unable to resolve publisher credentials for object-pack recovery");
  }
}

function localObjects(cwd) {
  const listing = execLocalGitSync(["--no-replace-objects", "cat-file", "--batch-all-objects", "--batch-check=%(objectname) %(objectsize)"], { cwd, maxBuffer: MAX_BUFFER });
  const records = listing.trim().split("\n").filter(Boolean);
  if (records.length > MAX_OBJECTS) throw new Error("Object-pack recovery exceeds the local object-count limit. Provision checkout history instead.");
  let total = 0;
  const ids = records.map(record => {
    const [oid, size] = record.split(" ");
    if (!oidPattern.test(oid) || !/^\d+$/.test(size)) throw new Error("Invalid local object inventory");
    total += Number(size);
    if (!Number.isSafeInteger(total) || total > MAX_BYTES) throw new Error("Object-pack recovery exceeds the 64 MiB uncompressed limit. Provision checkout history instead.");
    return oid;
  });
  traceObjectPack("inventory-ready", { objects: ids.length, bytes: total });
  return ids.sort();
}

function inspectObjectPack(pack, objectFormat) {
  const algorithm = objectFormat === "sha256" ? "sha256" : "sha1";
  const trailerLength = objectFormat === "sha256" ? 32 : 20;
  const end = pack.length - trailerLength;
  if (pack.length < 12 + trailerLength || !pack.subarray(0, 4).equals(Buffer.from("PACK")) || ![2, 3].includes(pack.readUInt32BE(4))) throw new Error("Invalid recovery pack header");
  if (!crypto.createHash(algorithm).update(pack.subarray(0, end)).digest().equals(pack.subarray(end))) throw new Error("Invalid recovery pack checksum");
  const count = pack.readUInt32BE(8);
  if (!count || count > MAX_OBJECTS) throw new Error("Invalid recovery object count");
  let offset = 12;
  let total = 0;
  const objects = new Map();
  for (let index = 0; index < count; index++) {
    let byte = pack[offset++];
    const type = ["", "commit", "tree", "blob", "tag"][(byte >> 4) & 7];
    if (!type) throw new Error("Recovery packs must contain full objects, not deltas");
    let size = byte & 15;
    let factor = 16;
    while (byte & 128) {
      if (offset >= end || factor > MAX_BYTES) throw new Error("Invalid recovery object size");
      byte = pack[offset++];
      size += (byte & 127) * factor;
      factor *= 128;
    }
    total += size;
    if (!Number.isSafeInteger(total) || total > MAX_BYTES || offset >= end) throw new Error("Recovery pack exceeds the uncompressed limit");
    /** @type {unknown} */
    const inflated = zlib.inflateSync(pack.subarray(offset, end), { info: true, maxOutputLength: size + 1 });
    if (
      !inflated ||
      typeof inflated !== "object" ||
      !("buffer" in inflated) ||
      !Buffer.isBuffer(inflated.buffer) ||
      !("engine" in inflated) ||
      !inflated.engine ||
      typeof inflated.engine !== "object" ||
      !("bytesWritten" in inflated.engine) ||
      typeof inflated.engine.bytesWritten !== "number"
    )
      throw new Error("Unable to validate recovery object compression");
    const bytes = inflated.buffer;
    if (bytes.length !== size) throw new Error("Invalid recovery object length");
    offset += inflated.engine.bytesWritten;
    const oid = crypto.createHash(algorithm).update(`${type} ${size}\0`).update(bytes).digest("hex");
    if (objects.has(oid)) throw new Error("Duplicate recovery object");
    objects.set(oid, { type, bytes });
  }
  if (offset !== end) throw new Error("Unexpected recovery pack data");
  const chunks = [Buffer.from("GH-AW object-pack recovery preview v1\n")];
  for (const oid of [...objects.keys()].sort()) {
    const { type, bytes } = objects.get(oid);
    chunks.push(Buffer.from(`\nObject ${oid} ${type} ${bytes.length}\n`), bytes);
  }
  return { preview: Buffer.from(Buffer.concat(chunks).toString("utf8")), objects };
}

/**
 * Export only local object bytes, never repository configuration or credentials.
 * The existing patch upload carries a conservative preview of every packed object
 * so detection/redaction cannot silently miss content hidden in the binary pack.
 */
function createObjectPack(branch, repo, tip, options = {}) {
  traceObjectPack("export-start", { pinned: oidPattern.test(tip || ""), scoped: !!currentClaimHandle() });
  if (!oidPattern.test(tip)) throw new Error("Object-pack recovery requires a pinned commit");
  const workspace = path.resolve(process.env.GITHUB_WORKSPACE || process.cwd());
  const workspacePath = options.workspacePath == null ? "" : String(options.workspacePath);
  const cwd = workspacePath.trim() ? path.resolve(workspace, workspacePath) : options.cwd || workspace;
  const relative = path.relative(workspace, cwd);
  if (workspacePath.trim() && (relative.startsWith("..") || path.isAbsolute(relative))) throw new Error("Object-pack workspace must stay under GITHUB_WORKSPACE");
  const patchPath = getPatchPathForBranchInRepo(branch, repo);
  const bundlePath = patchPath.replace(/\.patch$/, ".bundle");
  let directory = "";
  try {
    fs.mkdirSync(path.dirname(patchPath), { recursive: true });
    directory = fs.mkdtempSync(path.join(path.dirname(patchPath), "object-pack-"));
    const ids = localObjects(cwd);
    if (!ids.includes(tip)) throw new Error("Pinned commit is not available locally");
    const packId = execLocalGitSync(["--no-replace-objects", "pack-objects", "--window=0", "--no-reuse-delta", "--no-reuse-object", path.join(directory, "objects")], { cwd, input: `${ids.join("\n")}\n`, maxBuffer: MAX_BUFFER }).trim();
    if (!oidPattern.test(packId)) throw new Error("Invalid local pack identity");
    const pack = fs.readFileSync(path.join(directory, `objects-${packId}.pack`));
    if (pack.length > MAX_BUFFER) throw new Error("Object-pack exceeds the transfer limit");
    const objectFormat = execLocalGitSync(["rev-parse", "--show-object-format"], { cwd }).trim();
    const { preview } = inspectObjectPack(pack, objectFormat);
    const metadata = { tip, objectFormat, previewHash: hash(preview), packHash: hash(pack) };
    const envelope = Buffer.concat([MAGIC, Buffer.from(`${JSON.stringify(metadata)}\n`), pack]);
    const stagedPatch = path.join(directory, "transport.patch");
    const stagedBundle = path.join(directory, "transport.bundle");
    fs.writeFileSync(stagedPatch, preview);
    fs.writeFileSync(stagedBundle, envelope);
    fs.renameSync(stagedPatch, patchPath);
    fs.renameSync(stagedBundle, bundlePath);
    traceObjectPack("export-ready", { objects: ids.length, bytes: envelope.length, previewBytes: preview.length });
    return { patchPath, bundlePath, size: envelope.length };
  } catch {
    traceObjectPack("export-failed");
    throw new Error("Unable to package local objects for safe-outputs recovery. Ensure the pinned commit is local and the checkout fits the 64 MiB / 100000-object recovery limits.");
  } finally {
    cleanupRecovery(directory);
  }
}

/** Git in the recovery directory must not inherit config, alternates or hooks. */
function recoveryEnvironment(directory) {
  const env = Object.fromEntries(Object.entries(process.env).map(([key, value]) => [key, key.startsWith("GIT_") ? undefined : value]));
  return { ...env, HOME: directory, XDG_CONFIG_HOME: directory, GIT_CONFIG_NOSYSTEM: "1", GIT_CONFIG_GLOBAL: "/dev/null", GIT_NO_REPLACE_OBJECTS: "1", GIT_ALLOW_PROTOCOL: "", GIT_NO_LAZY_FETCH: "1" };
}

function trustedRemote(repo) {
  if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repo) || repo.split("/").some(part => part === "." || part === "..")) throw new Error("Invalid authorized recovery repository");
  const server = new URL(process.env.GITHUB_SERVER_URL || "https://github.com");
  if (server.protocol !== "https:" || server.username || server.password || server.pathname !== "/" || server.search || server.hash) throw new Error("Recovery requires a trusted HTTPS GitHub server");
  return `${server.origin}/${repo}.git`;
}

/**
 * Reconstruct before ordinary publisher policy checks. All options here are
 * resolved by the publisher, never taken from the uploaded pack's metadata.
 * Captured inputs remain immutable; only private transport snapshots change.
 */
async function recoverObjectPack(transport, paths, options) {
  try {
    return await recoverObjectPackChecked(transport, paths, options);
  } catch (error) {
    traceObjectPack("recovery-rejected");
    throw error;
  }
}

async function recoverObjectPackChecked(transport, paths, options) {
  if (!paths.bundlePath) return false;
  const envelope = transport.read(paths.bundlePath);
  if (!isObjectPack(envelope)) return false;
  traceObjectPack("recovery-start", { incremental: options.mode === "incremental", staged: !!options.staged, scoped: !!currentClaimHandle() });
  if (!paths.patchPath || envelope.length > MAX_BUFFER + 8 * 1024 * 1024) throw new Error("Invalid object-pack recovery artifact");
  if (options.staged) throw new Error("Object-pack recovery requires the non-staged safe-outputs publisher");
  const boundary = envelope.indexOf(10, MAGIC.length);
  if (boundary < 0 || boundary > MAGIC.length + 8 * 1024 * 1024) throw new Error("Invalid object-pack recovery metadata");
  let metadata;
  try {
    metadata = JSON.parse(envelope.subarray(MAGIC.length, boundary).toString("utf8"));
  } catch {
    throw new Error("Invalid object-pack recovery metadata");
  }
  if (!metadata || typeof metadata.tip !== "string" || !oidPattern.test(metadata.tip) || !["sha1", "sha256"].includes(metadata.objectFormat)) throw new Error("Invalid object-pack recovery identity");
  const pack = envelope.subarray(boundary + 1);
  if (pack.length > MAX_BUFFER || !pack.subarray(0, 4).equals(Buffer.from("PACK")) || hash(pack) !== metadata.packHash) throw new Error("Invalid object-pack recovery bytes");
  const preview = transport.read(paths.patchPath);
  if (hash(preview) !== metadata.previewHash) throw new Error("Recovery preview changed after packaging; refusing unreviewed object bytes");
  const inspection = inspectObjectPack(pack, metadata.objectFormat);
  if (!inspection.preview.equals(preview) || inspection.objects.get(metadata.tip)?.type !== "commit") throw new Error("Recovery preview does not match packed objects");
  traceObjectPack("preview-verified", { objects: inspection.objects.size, bytes: pack.length, previewBytes: preview.length });
  if (options.mode === "incremental" && !oidPattern.test(options.baseline || "")) {
    throw new Error("Object-pack recovery requires a trusted pre-agent PR-head SHA. Dynamic PR updates without a bound baseline must provision checkout data before agent execution.");
  }
  let directory;
  try {
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-object-recovery-"));
  } catch {
    throw new Error("Unable to initialize the isolated object-pack recovery directory");
  }
  const env = recoveryEnvironment(directory);
  const cwd = path.join(directory, "repo");
  const generated = [];
  try {
    const remote = trustedRemote(options.repo);
    execLocalGitSync(["init", "--bare", "--template=", `--object-format=${metadata.objectFormat}`, cwd], { cwd: directory, env });
    // This is the only network-capable command. It operates before importing any
    // untrusted objects, in a new bare repository with no copied configuration.
    const ref = options.mode === "incremental" ? options.baseline : `refs/heads/${options.baseBranch}`;
    const checkoutSha = options.mode === "full" && oidPattern.test(options.checkoutSha || "") ? options.checkoutSha : undefined;
    const refspecs = [`${ref}:refs/heads/recovery-base`];
    if (checkoutSha) refspecs.push(`${checkoutSha}:refs/heads/recovery-checkout`);
    if (options.mode !== "incremental" && !/^[A-Za-z0-9_][A-Za-z0-9_./-]*$/.test(options.baseBranch)) throw new Error("Invalid authorized recovery base branch");
    const auth = options.token ? buildGitAuthEnv(options.token) : {};
    traceObjectPack("fetch-start", { incremental: options.mode === "incremental", credential: !!options.token, dispatch: !!checkoutSha });
    const fetched = childProcess.spawnSync(
      "git",
      ["-c", "credential.helper=", "-c", "core.hooksPath=/dev/null", "-c", "http.followRedirects=false", "-c", "fetch.fsckObjects=true", "fetch", "--no-tags", "--no-recurse-submodules", "--depth=1000", "--", remote, ...refspecs],
      { cwd, env: { ...env, ...auth, GIT_ALLOW_PROTOCOL: "https", GIT_TERMINAL_PROMPT: "0", GIT_ASKPASS: "/bin/false" }, encoding: "utf8", maxBuffer: 1024 * 1024, timeout: 60000, killSignal: "SIGKILL" }
    );
    if (fetched.status !== 0 || fetched.error) throw new Error("Trusted object recovery fetch failed; provision checkout data or verify publisher credentials");
    traceObjectPack("fetch-ready");
    // index-pack verifies object hashes and links against the trusted base.
    const imported = execLocalGitSync(["index-pack", "--strict", "--max-input-size=83886080", "--stdin"], { cwd, env, input: pack, maxBuffer: MAX_BUFFER }).trim().split(/\s+/).pop();
    if (!imported || !oidPattern.test(imported)) throw new Error("Invalid imported object pack");
    execLocalGitSync(["cat-file", "-e", `${metadata.tip}^{commit}`], { cwd, env });
    execLocalGitSync(["fsck", "--strict", "--no-reflogs", metadata.tip], { cwd, env, maxBuffer: MAX_BUFFER });
    traceObjectPack("import-ready");
    const branch = `recovery-${crypto.randomBytes(12).toString("hex")}`;
    execLocalGitSync(["update-ref", `refs/heads/${branch}`, metadata.tip], { cwd, env });
    const isAncestor = (ancestor, descendant) => {
      const result = childProcess.spawnSync("git", ["merge-base", "--is-ancestor", ancestor, descendant], { cwd, env, timeout: 60000, maxBuffer: 1024 * 1024, killSignal: "SIGKILL" });
      if (result.error || (result.status !== 0 && result.status !== 1)) throw new Error("Unable to validate the trusted checkout baseline");
      return result.status === 0;
    };
    const useCheckout = checkoutSha && checkoutSha !== metadata.tip && isAncestor(checkoutSha, metadata.tip) && !isAncestor(checkoutSha, "refs/heads/recovery-base");
    const base = options.mode === "incremental" ? options.baseline : useCheckout ? checkoutSha : execLocalGitSync(["merge-base", "refs/heads/recovery-base", metadata.tip], { cwd, env }).trim();
    traceObjectPack("baseline-resolved", { incremental: options.mode === "incremental", dispatch: !!useCheckout });
    const generationOptions = { cwd, repoSlug: options.repo, mode: "incremental", pinnedSha: metadata.tip, incrementalBaseSha: base, excludedFiles: getExcludedFiles(options.config) };
    const generatedPatch = getPatchPathForBranchInRepo(branch, options.repo);
    generated.push(generatedPatch, generatedPatch.replace(/\.patch$/, ".bundle"));
    // A separate process keeps the clean Git environment local to reconstruction;
    // generators and their helpers cannot accidentally inherit the agent's Git env.
    const regeneration = childProcess.spawnSync(
      process.execPath,
      [
        "-e",
        `
      global.core = { debug() {}, info() {}, warning() {}, error() {} };
      const request = JSON.parse(require("fs").readFileSync(0, "utf8"));
      const regenerate = async () => {
        const patch = await require(process.argv[1]).generateGitPatch(request.branch, "recovery-base", request.options);
        const bundle = patch.success ? await require(process.argv[2]).generateGitBundle(request.branch, "recovery-base", request.options) : null;
        process.stdout.write(JSON.stringify({ patch, bundle }));
      };
      Promise.resolve().then(() => request.claim
        ? require(process.argv[3]).withClaimExecution(request.claim, regenerate)
        : regenerate()).catch(() => { process.exitCode = 1; });
    `,
        require.resolve("./generate_git_patch.cjs"),
        require.resolve("./generate_git_bundle.cjs"),
        require.resolve("./work_queue_claim_scope.cjs"),
      ],
      {
        cwd,
        env,
        input: JSON.stringify({ branch, options: generationOptions, claim: currentClaimHandle() ? { claim_handle: currentClaimHandle(), assignment: currentClaimAssignment() } : undefined }),
        encoding: "utf8",
        maxBuffer: 1024 * 1024,
        timeout: 60000,
        killSignal: "SIGKILL",
      }
    );
    if (regeneration.status !== 0 || regeneration.error) throw new Error("Unable to regenerate recovered transport");
    const { patch, bundle } = JSON.parse(regeneration.stdout);
    const noChanges = !patch.success && ["no_changes", "no_new_commits", "workflow_exclusions"].includes(patch.errorCode);
    if (!patch.success && !noChanges) throw new Error("Recovered checkout cannot generate the requested patch");
    if (patch.excludedWorkflowFiles?.length || patch.errorCode === "workflow_exclusions") {
      const field = options.mode === "incremental" ? "message" : "body";
      const content = options.message[field] || "";
      if (!content.includes(WORKFLOW_COMPILATION_NOTICE)) options.message[field] = `${WORKFLOW_COMPILATION_NOTICE}\n\n${content}`;
    }
    if (noChanges) {
      transport.transform(paths.patchPath, () => Buffer.alloc(0));
      transport.transform(paths.bundlePath, () => Buffer.alloc(0));
      traceObjectPack("recovery-empty");
      return true;
    }
    if (!bundle?.success) throw new Error("Recovered checkout cannot generate the requested bundle");
    traceObjectPack("regeneration-ready");
    const publishBranch = options.branch || branch;
    execLocalGitSync(["check-ref-format", `refs/heads/${publishBranch}`], { cwd, env });
    const bundleBytes = fs.readFileSync(bundle.bundlePath);
    const headerEnd = bundleBytes.indexOf("\n\n");
    if (headerEnd < 0) throw new Error("Invalid regenerated bundle header");
    const header = bundleBytes.subarray(0, headerEnd).toString("utf8");
    const sourceRef = new RegExp(`^([0-9a-f]{40,64}) (?:(?:refs/heads/)?${branch}|HEAD)$`, "m");
    if (!sourceRef.test(header)) throw new Error("Missing regenerated bundle reference");
    const renamed = header.replace(sourceRef, (_, oid) => `${oid} refs/heads/${publishBranch}`);
    transport.transform(paths.patchPath, () => fs.readFileSync(patch.patchPath));
    transport.transform(paths.bundlePath, () => Buffer.concat([Buffer.from(renamed), bundleBytes.subarray(headerEnd)]));
    options.message.base_commit = patch.baseCommit;
    if (typeof patch.diffSize === "number") options.message.diff_size = patch.diffSize;
    traceObjectPack("recovery-ready");
    return true;
  } catch {
    throw new Error("Safe-outputs object-pack recovery failed. Verify publisher credentials and provision the exact baseline, required history and blobs; no changes were published.");
  } finally {
    cleanupRecovery(directory, generated);
  }
}

module.exports = { createObjectPack, recoverObjectPack, canRecoverPatch, isObjectPack, recoveryToken, traceObjectPack };
