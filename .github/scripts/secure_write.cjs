// Shared helpers for writing agent-output JSON artifacts to fixed,
// well-known paths (e.g. under /tmp/gh-aw/agent/...) without the
// symlink / hard-link TOCTOU races and predictable-permission issues
// flagged by CodeQL's js/insecure-temporary-file rule.
const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");

/**
 * Create `dir` (and any missing parents) with restrictive permissions and
 * verify the resulting path is a real directory, not a symlink planted by
 * another process sharing the same predictable path.
 * @param {string} dir
 */
function ensureSecureDirectory(dir) {
  fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
  const stat = fs.lstatSync(dir);
  if (!stat.isDirectory()) {
    throw new Error(`Refusing to use non-directory output path: ${dir}`);
  }
}

/**
 * Write `content` to the fixed path `filePath` safely:
 *   1. Create a uniquely-named temp file in the same directory using
 *      O_EXCL | O_NOFOLLOW, so the create step can never collide with or
 *      follow a pre-existing file, symlink, or hard link at that name.
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
