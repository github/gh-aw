// @ts-check

const childProcess = require("child_process");
const crypto = require("crypto");
const fs = require("fs");
const os = require("os");
const path = require("path");

const { getErrorMessage } = require("./error_helpers.cjs");

const DEFAULT_VALIDATION_TIMEOUT_SECONDS = 60;
const MAX_VALIDATION_OUTPUT_BYTES = 12 * 1024;

function removePath(targetPath, options) {
  try {
    fs.rmSync(targetPath, options);
  } catch (error) {
    throw new Error(`Failed to remove ${targetPath}: ${getErrorMessage(error)}`, { cause: error });
  }
}

function makeDirectory(targetPath) {
  try {
    fs.mkdirSync(targetPath, { recursive: true });
  } catch (error) {
    throw new Error(`Failed to create directory ${targetPath}: ${getErrorMessage(error)}`, { cause: error });
  }
}

function writeFile(targetPath, content, options) {
  try {
    fs.writeFileSync(targetPath, content, options);
  } catch (error) {
    throw new Error(`Failed to write ${targetPath}: ${getErrorMessage(error)}`, { cause: error });
  }
}

function readDirectory(targetPath) {
  try {
    return fs.readdirSync(targetPath, { withFileTypes: true });
  } catch (error) {
    throw new Error(`Failed to read directory ${targetPath}: ${getErrorMessage(error)}`, { cause: error });
  }
}

/**
 * @param {string} targetPath
 * @param {BufferEncoding} [encoding]
 * @returns {any}
 */
function readFile(targetPath, encoding) {
  try {
    return encoding ? fs.readFileSync(targetPath, encoding) : fs.readFileSync(targetPath);
  } catch (error) {
    throw new Error(`Failed to read ${targetPath}: ${getErrorMessage(error)}`, { cause: error });
  }
}

function makeTempDirectory() {
  try {
    return fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-memory-validation-"));
  } catch (error) {
    throw new Error(`Failed to create memory validation temporary directory: ${getErrorMessage(error)}`, { cause: error });
  }
}

/**
 * @param {string} value
 */
function sanitizeID(value) {
  return String(value || "default").replace(/[^A-Za-z0-9_.-]/g, "_");
}

/**
 * @param {string} kind
 * @param {string} memoryId
 */
function getValidationMarkerPath(kind, memoryId) {
  return path.join(os.tmpdir(), "gh-aw", "memory-validation", `${sanitizeID(kind)}-${sanitizeID(memoryId)}.ok`);
}

/**
 * @param {string} kind
 * @param {string} memoryId
 */
function clearValidationMarker(kind, memoryId) {
  removePath(getValidationMarkerPath(kind, memoryId), { force: true });
}

/**
 * @param {string} kind
 * @param {string} memoryId
 */
function writeValidationMarker(kind, memoryId) {
  const markerPath = getValidationMarkerPath(kind, memoryId);
  makeDirectory(path.dirname(markerPath));
  writeFile(markerPath, "ok\n", "utf8");
  return markerPath;
}

/**
 * @param {Buffer | string | undefined | null} output
 */
function boundedOutput(output) {
  const text = Buffer.isBuffer(output) ? output.toString("utf8") : String(output || "");
  if (Buffer.byteLength(text, "utf8") <= MAX_VALIDATION_OUTPUT_BYTES) {
    return text;
  }
  return text.slice(0, MAX_VALIDATION_OUTPUT_BYTES) + "\n[output truncated]";
}

/**
 * @param {string} dirPath
 * @param {number} maxFileSize
 */
function formatJSONFiles(dirPath, maxFileSize) {
  if (!fs.existsSync(dirPath)) {
    return [];
  }
  /** @type {string[]} */
  const formattedFiles = [];

  /**
   * @param {string} currentDir
   */
  function visit(currentDir) {
    const entries = readDirectory(currentDir);
    for (const entry of entries) {
      const fullPath = path.join(currentDir, entry.name);
      if (entry.isDirectory()) {
        if (entry.name !== ".git") {
          visit(fullPath);
        }
        continue;
      }
      if (!entry.isFile() || !entry.name.endsWith(".json")) {
        continue;
      }
      const raw = readFile(fullPath, "utf8");
      if (!raw.trim()) {
        continue;
      }
      let parsed;
      try {
        parsed = JSON.parse(raw);
      } catch (_error) {
        continue;
      }
      const formatted = JSON.stringify(parsed, null, 2) + "\n";
      if (raw === formatted) {
        continue;
      }
      const formattedSize = Buffer.byteLength(formatted, "utf8");
      if (formattedSize > maxFileSize) {
        throw new Error(`Formatted JSON exceeds max file size: ${path.relative(dirPath, fullPath)} (${formattedSize} bytes > ${maxFileSize} bytes)`);
      }
      writeFile(fullPath, formatted, "utf8");
      formattedFiles.push(path.relative(dirPath, fullPath).replace(/\\/g, "/"));
    }
  }

  visit(dirPath);
  return formattedFiles;
}

/**
 * @param {Record<string, string | undefined>} sourceEnv
 */
function sanitizedValidationEnv(sourceEnv) {
  const keep = ["PATH", "HOME", "TMPDIR", "TEMP", "TMP", "RUNNER_TEMP", "GITHUB_WORKSPACE", "CI"];
  /** @type {Record<string, string>} */
  const env = {};
  for (const key of keep) {
    const value = sourceEnv[key];
    if (value !== undefined) {
      env[key] = value;
    }
  }
  return env;
}

/**
 * @param {string} dirPath
 */
function memoryTreeDigest(dirPath) {
  const hash = crypto.createHash("sha256");

  /**
   * @param {string} currentDir
   */
  function visit(currentDir) {
    const entries = readDirectory(currentDir).sort((a, b) => a.name.localeCompare(b.name));
    for (const entry of entries) {
      const fullPath = path.join(currentDir, entry.name);
      const relativePath = path.relative(dirPath, fullPath).replace(/\\/g, "/");
      if (entry.isDirectory()) {
        hash.update(`directory\0${relativePath}\0`);
        visit(fullPath);
      } else if (entry.isFile()) {
        hash.update(`file\0${relativePath}\0`);
        hash.update(readFile(fullPath));
      } else if (entry.isSymbolicLink()) {
        hash.update(`symlink\0${relativePath}\0${fs.readlinkSync(fullPath)}\0`);
      } else {
        hash.update(`other\0${relativePath}\0`);
      }
    }
  }

  visit(dirPath);
  return hash.digest("hex");
}

/**
 * @param {{
 *   script?: string,
 *   scriptBase64?: string,
 *   memoryDir: string,
 *   memoryId?: string,
 *   kind: "repo" | "cache" | "drive",
 *   timeoutSeconds?: number,
 * }} options
 */
function runCustomMemoryValidation(options) {
  let script = options.script || "";
  if (!script && options.scriptBase64) {
    script = Buffer.from(options.scriptBase64, "base64").toString("utf8");
  }
  if (!script.trim()) {
    return {
      ok: false,
      exitCode: null,
      timedOut: false,
      stdout: "",
      stderr: "validation.script is configured but empty or missing",
    };
  }

  const rawTimeoutSeconds = options.timeoutSeconds;
  const timeoutSeconds = typeof rawTimeoutSeconds === "number" && Number.isFinite(rawTimeoutSeconds) && rawTimeoutSeconds > 0 ? Math.floor(rawTimeoutSeconds) : DEFAULT_VALIDATION_TIMEOUT_SECONDS;
  const timeoutMs = timeoutSeconds * 1000;
  const memoryId = options.memoryId || "default";
  let beforeDigest;
  try {
    beforeDigest = memoryTreeDigest(options.memoryDir);
  } catch (error) {
    return {
      ok: false,
      exitCode: null,
      timedOut: false,
      stdout: "",
      stderr: `Unable to snapshot memory before custom validation: ${getErrorMessage(error)}`,
    };
  }
  const validationDir = makeTempDirectory();
  const scriptPath = path.join(validationDir, "validator.cjs");
  const wrapper = `"use strict";
const fs = require("fs");
const path = require("path");
const memoryRoot = ${JSON.stringify(options.memoryDir)};
const memoryDir = memoryRoot;
const memoryId = ${JSON.stringify(memoryId)};
const memoryKind = ${JSON.stringify(options.kind)};
process.env.GH_AW_MEMORY_ROOT = memoryRoot;
process.env.GH_AW_MEMORY_DIR = memoryRoot;
process.env.GH_AW_MEMORY_ID = memoryId;
process.env.GH_AW_MEMORY_KIND = memoryKind;
(async () => {
  return await (async () => {
${script}
  })();
})()
  .then(result => {
    if (result === false) {
      console.error("validation.script returned false");
      process.exit(1);
    }
  })
  .catch(error => {
    console.error(error && error.stack ? error.stack : String(error));
    process.exit(1);
  });
`;
  writeFile(scriptPath, wrapper, { encoding: "utf8", mode: 0o600 });
  try {
    const result = childProcess.spawnSync(process.execPath, [scriptPath], {
      cwd: options.memoryDir,
      encoding: "utf8",
      env: sanitizedValidationEnv(process.env),
      timeout: timeoutMs,
      maxBuffer: MAX_VALIDATION_OUTPUT_BYTES * 2,
      windowsHide: true,
    });
    const spawnErrorCode = result.error ? Reflect.get(result.error, "code") : undefined;
    let memoryChanged = false;
    let snapshotError = "";
    try {
      memoryChanged = beforeDigest !== memoryTreeDigest(options.memoryDir);
    } catch (error) {
      snapshotError = `Unable to snapshot memory after custom validation: ${getErrorMessage(error)}`;
    }
    const stderr = boundedOutput(result.stderr || (result.error ? getErrorMessage(result.error) : ""));
    const validationError = memoryChanged ? "Custom validation must not modify memory files" : snapshotError;
    return {
      ok: result.status === 0 && !result.error && !memoryChanged && !snapshotError,
      exitCode: result.status,
      timedOut: spawnErrorCode === "ETIMEDOUT",
      stdout: boundedOutput(result.stdout),
      stderr: validationError ? boundedOutput(`${stderr}${stderr ? "\n" : ""}${validationError}`) : stderr,
    };
  } finally {
    removePath(validationDir, { recursive: true, force: true });
  }
}

module.exports = {
  DEFAULT_VALIDATION_TIMEOUT_SECONDS,
  clearValidationMarker,
  formatJSONFiles,
  getValidationMarkerPath,
  memoryTreeDigest,
  runCustomMemoryValidation,
  writeValidationMarker,
};
