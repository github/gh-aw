// @ts-check

const crypto = require("crypto");
const fs = require("fs");
const path = require("path");
const { getObjectPackLimits } = require("./object_pack_limits.cjs");

const MAGIC = Buffer.from("GH-AW-OBJECT-PACK 1\n");
const READ_FLAGS = fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK;

/** @param {string} filename @param {ReturnType<typeof getObjectPackLimits>} limits */
function previewHash(filename, limits) {
  const fd = fs.openSync(filename, READ_FLAGS);
  try {
    const stat = fs.fstatSync(fd);
    if (!stat.isFile() || stat.size > limits.maxPreviewBytes) throw new Error("Invalid recovery preview");
    const hash = crypto.createHash("sha256");
    const buffer = Buffer.alloc(64 * 1024);
    let total = 0;
    for (;;) {
      const count = fs.readSync(fd, buffer, 0, buffer.length, null);
      if (!count) break;
      total += count;
      if (total > limits.maxPreviewBytes) throw new Error("Oversized recovery preview");
      hash.update(buffer.subarray(0, count));
    }
    if (total !== stat.size) throw new Error("Recovery preview changed during validation");
    return hash.digest("hex");
  } finally {
    fs.closeSync(fd);
  }
}

/**
 * Binary recovery objects must never outlive a redacted or removed text preview.
 * Ordinary Git bundles are deliberately untouched.
 * @param {string} filename
 * @param {ReturnType<typeof getObjectPackLimits>} [limits]
 * @returns {boolean} Whether a recovery envelope was removed.
 */
function validateEnvelope(filename, limits = getObjectPackLimits()) {
  const fd = fs.openSync(filename, READ_FLAGS);
  let valid = false;
  try {
    const stat = fs.fstatSync(fd);
    if (!stat.isFile()) throw new Error("Invalid recovery envelope");
    const header = Buffer.alloc(Math.min(Math.max(limits.maxHeaderBytes, MAGIC.length), stat.size));
    let length = 0;
    while (length < header.length) {
      const count = fs.readSync(fd, header, length, header.length - length, length);
      if (!count) break;
      length += count;
    }
    if (!header.subarray(0, MAGIC.length).equals(MAGIC)) return false;
    if (stat.size > limits.maxPackBytes + limits.maxHeaderBytes) throw new Error("Oversized recovery envelope");
    const end = header.indexOf(10, MAGIC.length);
    if (end < 0 || end >= length || end >= limits.maxHeaderBytes) throw new Error("Invalid recovery metadata");
    const metadata = JSON.parse(header.subarray(MAGIC.length, end).toString("utf8"));
    if (!/^[0-9a-f]{64}$/.test(metadata.previewHash || "") || !/^[0-9a-f]{64}$/.test(metadata.packHash || "")) throw new Error("Invalid recovery hashes");
    if (!["sha1", "sha256"].includes(metadata.objectFormat)) throw new Error("Invalid recovery object format");
    const pack = Buffer.alloc(stat.size - end - 1);
    let offset = 0;
    while (offset < pack.length) {
      const count = fs.readSync(fd, pack, offset, pack.length - offset, end + 1 + offset);
      if (!count) throw new Error("Recovery envelope changed during validation");
      offset += count;
    }
    if (fs.fstatSync(fd).size !== stat.size || crypto.createHash("sha256").update(pack).digest("hex") !== metadata.packHash) throw new Error("Recovery envelope changed during validation");
    // Both metadata hashes are agent-controlled: reconstruct every packed object
    // using the same bounded parser as publication, not just its claimed digest.
    const { inspectObjectPack } = require("./object_pack_transport.cjs");
    const { preview } = inspectObjectPack(pack, metadata.objectFormat, limits);
    const expectedHash = crypto.createHash("sha256").update(preview).digest("hex");
    valid = expectedHash === metadata.previewHash && previewHash(filename.replace(/\.bundle$/, ".patch"), limits) === expectedHash;
  } catch {
    // Missing, unreadable, malformed and redacted previews all suppress the pack.
    valid = false;
  } finally {
    fs.closeSync(fd);
  }
  if (valid) return false;
  // An unlink failure must fail the step, never permit a best-effort upload.
  fs.unlinkSync(filename);
  return true;
}

/** @param {string} candidate @param {string} base */
function isWithin(candidate, base) {
  return candidate === base || candidate.startsWith(base.endsWith(path.sep) ? base : base + path.sep);
}

/** @param {string} filename */
function canonicalize(filename) {
  try {
    return fs.realpathSync(path.resolve(filename));
  } catch (error) {
    if (error && typeof error === "object" && "code" in error && error.code === "ENOENT") return null;
    throw new Error("Unable to inspect recovery artifacts", { cause: error });
  }
}

/**
 * Run after every masking step, and again after artifact consolidation/copies.
 * Additional roots select subtrees, never expand the trusted artifact boundary.
 * @param {{ roots?: string[], additionalRoots?: string[], core?: {info?: (message: string) => void} }} [options]
 */
function main({ roots = ["/tmp/gh-aw", path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw")], additionalRoots = [], core = globalThis.core } = {}) {
  const limits = getObjectPackLimits();
  if (!Array.isArray(additionalRoots) || additionalRoots.some(root => typeof root !== "string")) throw new Error("Invalid additional recovery artifact roots");
  const baseRoots = roots.filter(Boolean).map(root => path.resolve(root));
  const trustedRoots = baseRoots
    .map(root => {
      const canonical = canonicalize(root);
      if (canonical !== null && fs.lstatSync(root).isSymbolicLink()) throw new Error("Recovery artifact root must not be a symbolic link");
      return canonical;
    })
    .filter(root => root !== null);
  for (const root of additionalRoots) {
    if (!path.isAbsolute(root) || !baseRoots.some(base => isWithin(path.resolve(root), base))) throw new Error("Invalid additional recovery artifact roots");
    const real = canonicalize(root);
    if (real !== null && !trustedRoots.some(base => isWithin(real, base))) throw new Error("Invalid additional recovery artifact roots");
  }
  const scanRoots = [...baseRoots, ...additionalRoots.map(root => path.resolve(root))];
  const visited = new Set();
  let removed = 0;
  let entries = 0;
  /** @param {string} filename */
  function visit(filename) {
    const canonical = canonicalize(filename);
    if (canonical === null) return;
    if (!trustedRoots.some(root => isWithin(canonical, root))) {
      throw new Error("Recovery artifact path escapes trusted roots");
    }
    let stat;
    try {
      stat = fs.statSync(filename);
    } catch (error) {
      if (error && typeof error === "object" && "code" in error && error.code === "ENOENT") return;
      throw new Error("Unable to inspect recovery artifacts", { cause: error });
    }
    if (++entries > limits.maxScanEntries) throw new Error("Recovery artifact scan exceeded its entry limit");
    if (stat.isDirectory()) {
      const identity = `${stat.dev}:${stat.ino}`;
      if (visited.has(identity)) return;
      visited.add(identity);
      for (const entry of fs.readdirSync(filename)) visit(path.join(filename, entry));
    } else if (filename.endsWith(".bundle")) {
      if (validateEnvelope(filename, limits)) removed++;
    }
  }
  for (const root of scanRoots) visit(root);
  core?.info?.(`Removed ${removed} recovery envelope(s) without unchanged previews`);
  return { removed };
}

module.exports = { main, validateEnvelope };
