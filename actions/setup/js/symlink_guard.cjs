// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Symlink Guard Helper
 *
 * Centralises the lstatSync + isSymbolicLink security check that prevents
 * symlink-traversal attacks across staging and gateway code paths.
 *
 * All callers that need to reject symbolic links should call lstatGuard()
 * instead of calling fs.lstatSync() directly and checking isSymbolicLink()
 * themselves.  This ensures any future hardening (e.g., rejecting device
 * nodes or hard-links) can be applied in one place.
 */

const fs = require("fs");
const path = require("path");
const { ERR_VALIDATION } = require("./error_codes.cjs");

/** @param {string} message */
function assetValidationError(message) {
  return Object.assign(new Error(`${ERR_VALIDATION}: ${message}`), { code: ERR_VALIDATION });
}

/**
 * Calls `fs.lstatSync(filePath)` and returns the Stats object unless the path
 * is a symbolic link, in which case `null` is returned.
 *
 * By using `lstatSync` (not `statSync`) the symlink itself is inspected rather
 * than the target it points to, preventing symlink-traversal attacks.
 *
 * Throws if `filePath` does not exist — callers that need to tolerate
 * non-existent paths should wrap the call in a try/catch.
 *
 * @param {string} filePath
 * @returns {import('fs').Stats | null} Stats when not a symlink, null when it is
 */
function lstatGuard(filePath) {
  const stat = fs.lstatSync(filePath);
  return stat.isSymbolicLink() ? null : stat;
}

/**
 * Resolve only platform-owned aliases, not agent-controlled symlinks.
 * @param {string} filename
 */
function normalizeGuardedPath(filename) {
  const absolute = path.resolve(filename);
  if (process.platform === "darwin") {
    for (const alias of ["/tmp", "/var"]) {
      if (absolute === alias || absolute.startsWith(`${alias}/`)) {
        return fs.realpathSync(alias) + absolute.slice(alias.length);
      }
    }
  }
  return absolute;
}

/**
 * Pin each directory while walking, rejecting links before opening children.
 * Linux uses descriptor-relative paths so a renamed/replaced ancestor cannot
 * redirect the next open. Fail closed elsewhere: absolute-path revalidation
 * cannot detect an ancestor swapped and restored during the operation, and
 * POSIX no-follow flags do not provide Windows junction protection.
 * @template T
 * @param {string} directory
 * @param {boolean} create
 * @param {(directoryPath: string) => T} operation
 * @returns {T}
 */
function withGuardedDirectory(directory, create, operation) {
  if (process.platform !== "linux") {
    throw assetValidationError("Guarded asset access requires Linux descriptor-relative paths; macOS and Windows are unsupported");
  }
  if (fs.constants.O_NOFOLLOW === undefined || fs.constants.O_DIRECTORY === undefined) {
    throw assetValidationError("Cannot safely access assets: no-follow directory opens are unavailable");
  }
  const absolute = normalizeGuardedPath(directory);
  const root = path.parse(absolute).root;
  /** @type {Array<{fd: number, filename: string, stat: import('fs').Stats}>} */
  const directories = [];
  let current = root;
  try {
    for (const component of [root, ...absolute.slice(root.length).split(path.sep).filter(Boolean)]) {
      if (component !== root) {
        const parent = directories[directories.length - 1];
        current = `/proc/self/fd/${parent.fd}/${component}`;
      }
      let stat;
      try {
        stat = lstatGuard(current);
      } catch (error) {
        if (!create || !error || typeof error !== "object" || !("code" in error) || error.code !== "ENOENT") throw error;
        try {
          fs.mkdirSync(current, { mode: 0o700 });
        } catch (mkdirError) {
          if (!mkdirError || typeof mkdirError !== "object" || !("code" in mkdirError) || mkdirError.code !== "EEXIST") throw mkdirError;
        }
        stat = lstatGuard(current);
      }
      if (!stat) throw assetValidationError("Asset symlinks are not allowed, including parent directories");
      if (!stat.isDirectory()) throw assetValidationError("Asset parent must be a directory");
      const fd = fs.openSync(current, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_DIRECTORY);
      directories.push({ fd, filename: current, stat });
      const opened = fs.fstatSync(fd);
      if (opened.dev !== stat.dev || opened.ino !== stat.ino) throw assetValidationError("Asset directory changed while opening");
    }
    const verify = () => {
      for (const directory of directories) {
        const stat = lstatGuard(directory.filename);
        if (!stat || stat.dev !== directory.stat.dev || stat.ino !== directory.stat.ino) throw assetValidationError("Asset directory changed during access");
      }
    };
    verify();
    const final = directories[directories.length - 1];
    const result = operation(`/proc/self/fd/${final.fd}`);
    verify();
    return result;
  } catch (error) {
    if (error && typeof error === "object" && "code" in error && (error.code === "ELOOP" || error.code === "ENOTDIR")) {
      throw assetValidationError("Asset symlinks or non-directory parents are not allowed");
    }
    throw error;
  } finally {
    for (const directory of directories.reverse()) fs.closeSync(directory.fd);
  }
}

/**
 * Read one isolated regular-file snapshot; never reopen the source for copying.
 * @param {string} filename
 * @param {number} [maximumBytes]
 * @returns {Buffer}
 */
function readGuardedFile(filename, maximumBytes = 10 * 1024 * 1024) {
  if (!Number.isSafeInteger(maximumBytes) || maximumBytes < 0) throw assetValidationError("Invalid asset maximum size");
  return withGuardedDirectory(path.dirname(filename), false, directory => {
    const source = path.join(directory, path.basename(filename));
    const expected = lstatGuard(source);
    if (!expected) throw assetValidationError("Asset symlinks are not allowed");
    if (!expected.isFile() || expected.nlink !== 1) throw assetValidationError("Asset must be an isolated regular file");
    const fd = fs.openSync(source, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
    try {
      const opened = fs.fstatSync(fd);
      if (!opened.isFile() || opened.nlink !== 1 || opened.dev !== expected.dev || opened.ino !== expected.ino) throw assetValidationError("Asset file changed while opening");
      const current = lstatGuard(source);
      if (!current || current.dev !== opened.dev || current.ino !== opened.ino) throw assetValidationError("Asset file changed while opening");
      if (opened.size > maximumBytes) throw assetValidationError(`Asset exceeds maximum allowed size ${maximumBytes / 1024} KB`);
      const content = Buffer.alloc(opened.size);
      let offset = 0;
      while (offset < content.length) {
        const count = fs.readSync(fd, content, offset, content.length - offset, offset);
        if (!count) throw assetValidationError("Asset file changed while reading");
        offset += count;
      }
      const after = fs.fstatSync(fd);
      if (after.nlink !== 1 || after.size !== opened.size || after.mtimeMs !== opened.mtimeMs || after.ctimeMs !== opened.ctimeMs) throw assetValidationError("Asset file changed while reading");
      return content;
    } finally {
      fs.closeSync(fd);
    }
  });
}

/**
 * Exclusively create a staged/published file without following destination links.
 * @param {string} filename
 * @param {Buffer} content
 */
function writeGuardedFile(filename, content) {
  return withGuardedDirectory(path.dirname(filename), true, directory => {
    const target = path.join(directory, path.basename(filename));
    const fd = fs.openSync(target, fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_EXCL | fs.constants.O_NOFOLLOW, 0o600);
    try {
      fs.writeFileSync(fd, content);
    } finally {
      fs.closeSync(fd);
    }
  });
}

module.exports = { lstatGuard, normalizeGuardedPath, readGuardedFile, writeGuardedFile };
