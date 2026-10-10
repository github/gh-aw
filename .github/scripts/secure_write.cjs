// Shared helpers for writing agent-output JSON artifacts to fixed,
// well-known paths (e.g. under /tmp/gh-aw/agent/...) without the
// symlink / hard-link TOCTOU races and predictable-permission issues
// flagged by CodeQL's js/insecure-temporary-file rule.
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const crypto = require("node:crypto");

const STICKY_BIT = 0o1000;
const GROUP_OTHER_WRITE = 0o022;
const OTHER_WRITE = 0o002;

/**
 * Decide whether a pre-existing directory is safe to reuse as-is, without
 * ever chmod/chown-ing it (which would just as easily apply to a symlink
 * planted at that path). A directory is trusted only if:
 *   - we own it and it grants no group/other write access, or
 *   - it is not ours but follows the standard shared `/tmp` model: sticky
 *     bit set and world-writable (entries are protected per-owner even
 *     though the directory itself is shared across users/jobs).
 * @param {fs.Stats} stat
 */
function isTrustedExistingDirectory(stat) {
  if (!stat.isDirectory()) return false;
  if (typeof process.getuid !== "function") return true; // no POSIX uid model (e.g. Windows): best effort
  if (stat.uid === process.getuid()) return (stat.mode & GROUP_OTHER_WRITE) === 0;
  return (stat.mode & STICKY_BIT) !== 0 && (stat.mode & OTHER_WRITE) !== 0;
}

/**
 * Create `dir` securely by validating every path component below a trust
 * boundary, rather than trusting a single end-state lstat of the final
 * path (which would miss a symlinked or insecurely-permissioned ancestor).
 *
 * The trust boundary is `os.tmpdir()` when `dir` resolves under it (the
 * common case for these scripts' fixed `/tmp/gh-aw/agent/...` paths) —
 * that directory is provided by the runner before any job-controlled code
 * executes, so it is trusted without a component-by-component walk above
 * it. Every component at or below the boundary is:
 *   - rejected if it is a symlink (never followed, never "fixed" with a
 *     chmod that could itself land on a symlink target), and
 *   - created by us with mode 0700 if missing, or validated against
 *     {@link isTrustedExistingDirectory} if it already exists.
 * @param {string} dir
 */
function ensureSecureDirectory(dir) {
  const resolved = path.resolve(dir);
  const fsRoot = path.parse(resolved).root;
  const tmpRoot = path.resolve(os.tmpdir());
  const relativeToTmp = path.relative(tmpRoot, resolved);
  const underTmp = relativeToTmp === "" || (!relativeToTmp.startsWith("..") && !path.isAbsolute(relativeToTmp));
  const boundary = underTmp ? tmpRoot : fsRoot;

  if (boundary !== fsRoot) {
    const stat = fs.lstatSync(boundary);
    if (stat.isSymbolicLink() || !isTrustedExistingDirectory(stat)) {
      throw new Error(`Refusing to use untrusted directory as a trust boundary: ${boundary}`);
    }
  }

  const segments = path.relative(boundary, resolved).split(path.sep).filter(Boolean);
  let current = boundary;
  for (const segment of segments) {
    current = path.join(current, segment);
    let stat;
    try {
      stat = fs.lstatSync(current);
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
      fs.mkdirSync(current, { mode: 0o700 });
      stat = fs.lstatSync(current);
    }
    if (stat.isSymbolicLink()) {
      throw new Error(`Refusing to use path with a symlinked component: ${current}`);
    }
    if (!isTrustedExistingDirectory(stat)) {
      throw new Error(`Refusing to use untrusted or insecurely-permissioned directory: ${current}`);
    }
  }
}

/**
 * Write `content` to the fixed path `filePath` safely:
 *   1. Create a uniquely-named temp file in the same (now validated)
 *      directory using O_EXCL | O_NOFOLLOW, so the create step can never
 *      collide with or follow a pre-existing file, symlink, or hard link
 *      at that name.
 *   2. Atomically rename() the temp file onto `filePath`. rename() replaces
 *      whatever directory entry previously existed there without
 *      dereferencing it, so publishing to the fixed, consumer-visible path
 *      is always safe regardless of what (if anything) was there before.
 * @param {string} filePath
 * @param {string} content
 */
function writeFileSecure(filePath, content) {
  const dir = path.dirname(filePath);
  ensureSecureDirectory(dir);
  const tmpPath = path.join(dir, `.${path.basename(filePath)}.${process.pid}.${crypto.randomBytes(8).toString("hex")}.tmp`);
  const flags = fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_EXCL | fs.constants.O_NOFOLLOW;
  const fd = fs.openSync(tmpPath, flags, 0o600);
  try {
    fs.writeFileSync(fd, content);
  } finally {
    fs.closeSync(fd);
  }
  try {
    fs.renameSync(tmpPath, filePath);
  } catch (error) {
    fs.rmSync(tmpPath, { force: true });
    throw error;
  }
}

module.exports = { ensureSecureDirectory, writeFileSecure };
