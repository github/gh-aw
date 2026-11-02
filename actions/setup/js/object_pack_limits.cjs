// @ts-check

const { constants } = require("buffer");

/**
 * @typedef {Object} ObjectPackLimits
 * @property {number} maxObjects
 * @property {number} maxUncompressedBytes
 * @property {number} maxPackBytes
 * @property {number} maxPreviewBytes
 * @property {number} maxHeaderBytes
 * @property {number} maxTreeEntries
 * @property {number} maxScanEntries
 * @property {number} maxHydrationBytes
 * @property {number} commandTimeoutMs
 * @property {number} networkTimeoutMs
 * @property {number} maxCommandOutputBytes
 * @property {number} maxGitOutputBytes
 * @property {number} fetchBatchSize
 * @property {readonly number[]} fetchDepths
 */

/** @type {Readonly<ObjectPackLimits>} */
const DEFAULT_OBJECT_PACK_LIMITS = Object.freeze({
  maxObjects: 100000,
  maxUncompressedBytes: 64 * 1024 * 1024,
  maxPackBytes: 80 * 1024 * 1024,
  maxPreviewBytes: 128 * 1024 * 1024,
  maxHeaderBytes: 4096,
  maxTreeEntries: 1000000,
  maxScanEntries: 1000000,
  maxHydrationBytes: 64 * 1024 * 1024,
  commandTimeoutMs: 60000,
  networkTimeoutMs: 180000,
  maxCommandOutputBytes: 1024 * 1024,
  maxGitOutputBytes: 100 * 1024 * 1024,
  fetchBatchSize: 256,
  fetchDepths: Object.freeze([1, 16, 64, 256, 1024, 4096, 16384, 65536]),
});

const OBJECT_PACK_LIMIT_ENV = Object.freeze({
  maxUncompressedBytes: "GH_AW_OBJECT_PACK_MAX_BYTES",
  maxObjects: "GH_AW_OBJECT_PACK_MAX_OBJECTS",
  commandTimeoutMs: "GH_AW_OBJECT_PACK_TIMEOUT_MS",
  fetchDepths: "GH_AW_OBJECT_PACK_FETCH_DEPTHS",
});

/** @param {string} value @param {string} name @param {number} maximum */
function positiveInteger(value, name, maximum) {
  const parsed = Number(value);
  if (!/^[0-9]+$/.test(value) || !Number.isSafeInteger(parsed) || parsed < 1 || parsed > maximum) {
    throw new Error(`Invalid ${name}: expected a positive decimal integer no greater than ${maximum}`);
  }
  return parsed;
}

/**
 * Limits come from the current job's repository-variable bindings, never uploads.
 * Empty bindings preserve defaults; invalid settings fail closed without values.
 * @param {NodeJS.ProcessEnv} [env]
 */
function getObjectPackLimits(env = process.env) {
  const limits = { ...DEFAULT_OBJECT_PACK_LIMITS };
  const bufferMaximum = Math.min(constants.MAX_LENGTH - 1, constants.MAX_STRING_LENGTH);
  for (const key of Object.keys(OBJECT_PACK_LIMIT_ENV)) {
    const name = OBJECT_PACK_LIMIT_ENV[key];
    const value = env[name];
    if (value == null || value === "") continue;
    if (key === "fetchDepths") {
      const depths = value.split(",").map(depth => positiveInteger(depth.trim(), name, 2147483647));
      if (depths.some((depth, index) => index > 0 && depth <= depths[index - 1])) {
        throw new Error(`Invalid ${name}: expected strictly increasing positive depths`);
      }
      limits.fetchDepths = Object.freeze(depths);
    } else {
      const maximum = key === "commandTimeoutMs" ? Math.floor(2147483647 / 3) : key === "maxUncompressedBytes" ? Math.floor(bufferMaximum / 2) : 0xffffffff;
      limits[key] = positiveInteger(value, name, maximum);
    }
  }
  const bytes = limits.maxUncompressedBytes;
  limits.maxPackBytes = Math.ceil((bytes * 5) / 4);
  limits.maxPreviewBytes = bytes * 2;
  limits.maxHeaderBytes = Math.max(1, Math.floor(bytes / 16384));
  limits.maxHydrationBytes = bytes;
  limits.maxCommandOutputBytes = Math.max(1, Math.floor(bytes / 64));
  limits.maxGitOutputBytes = Math.ceil((bytes * 25) / 16);
  limits.maxTreeEntries = limits.maxObjects * 10;
  limits.maxScanEntries = limits.maxObjects * 10;
  limits.fetchBatchSize = Math.max(1, Math.ceil((limits.maxObjects * 256) / 100000));
  limits.networkTimeoutMs = limits.commandTimeoutMs * 3;
  return Object.freeze(limits);
}

module.exports = { DEFAULT_OBJECT_PACK_LIMITS, OBJECT_PACK_LIMIT_ENV, getObjectPackLimits };
