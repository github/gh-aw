// Shared helper for writing agent-output JSON artifacts to fixed,
// well-known paths (e.g. under /tmp/gh-aw/agent/...) without the
// symlink / hard-link TOCTOU races and predictable-permission issues
// flagged by CodeQL's js/insecure-temporary-file rule.
import { constants as fsConstants, closeSync, lstatSync, mkdirSync, openSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { dirname, basename, join } from "node:path";
import { randomBytes } from "node:crypto";

/**
 * Create `dir` (and any missing parents) with restrictive permissions and
 * verify the resulting path is a real directory, not a symlink planted by
 * another process sharing the same predictable path.
 * @param {string} dir
 */
export function ensureSecureDirectory(dir) {
  mkdirSync(dir, { recursive: true, mode: 0o700 });
  const stat = lstatSync(dir);
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
export function writeFileSecure(filePath, content) {
  const dir = dirname(filePath);
  ensureSecureDirectory(dir);
  const tmpPath = join(dir, `.${basename(filePath)}.${process.pid}.${randomBytes(8).toString("hex")}.tmp`);
  const flags = fsConstants.O_WRONLY | fsConstants.O_CREAT | fsConstants.O_EXCL | fsConstants.O_NOFOLLOW;
  const fd = openSync(tmpPath, flags, 0o600);
  try {
    writeFileSync(fd, content);
  } finally {
    closeSync(fd);
  }
  try {
    renameSync(tmpPath, filePath);
  } catch (error) {
    rmSync(tmpPath, { force: true });
    throw error;
  }
}
