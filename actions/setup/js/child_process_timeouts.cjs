// @ts-check
"use strict";

/**
 * Largest timeout Node.js can represent as a timer delay (2^31 - 1 ms, ~24.8 days).
 * Larger delays overflow and are silently converted to 1 ms, so overrides above
 * this bound are clamped to it.
 */
const MAX_SETUP_TIMEOUT_MS = 2_147_483_647;

/**
 * Setup/runtime command timeout defaults, in milliseconds. Each timeout can be
 * overridden with its environment variable by setting a positive integer value.
 */
const SETUP_TIMEOUTS = Object.freeze({
  applySamplesFetch: { env: "GH_AW_APPLY_SAMPLES_FETCH_TIMEOUT_MS", defaultMs: 120_000 },
  applySamplesGit: { env: "GH_AW_APPLY_SAMPLES_GIT_TIMEOUT_MS", defaultMs: 120_000 },
  artifactArchive: { env: "GH_AW_ARTIFACT_ARCHIVE_TIMEOUT_MS", defaultMs: 300_000 },
  artifactArchiveProbe: { env: "GH_AW_ARTIFACT_ARCHIVE_PROBE_TIMEOUT_MS", defaultMs: 15_000 },
  artifactFetch: { env: "GH_AW_ARTIFACT_FETCH_TIMEOUT_MS", defaultMs: 120_000 },
  artifactTransfer: { env: "GH_AW_ARTIFACT_TRANSFER_TIMEOUT_MS", defaultMs: 300_000 },
  gitBranch: { env: "GH_AW_GIT_BRANCH_TIMEOUT_MS", defaultMs: 15_000 },
  importGit: { env: "GH_AW_IMPORT_GIT_TIMEOUT_MS", defaultMs: 300_000 },
  mcpConfigConverter: { env: "GH_AW_MCP_CONFIG_CONVERTER_TIMEOUT_MS", defaultMs: 120_000 },
  mcpContainerStatus: { env: "GH_AW_MCP_CONTAINER_STATUS_TIMEOUT_MS", defaultMs: 15_000 },
  mcpDockerCleanup: { env: "GH_AW_MCP_DOCKER_CLEANUP_TIMEOUT_MS", defaultMs: 30_000 },
  mcpServerCheck: { env: "GH_AW_MCP_SERVER_CHECK_TIMEOUT_MS", defaultMs: 120_000 },
  outcomeGh: { env: "GH_AW_OUTCOME_GH_TIMEOUT_MS", defaultMs: 300_000 },
  safeoutputsCli: { env: "GH_AW_SAFEOUTPUTS_CLI_TIMEOUT_MS", defaultMs: 120_000 },
});

/**
 * @param {string} envName
 * @param {number} defaultMs
 * @param {NodeJS.ProcessEnv} [env]
 * @returns {number}
 */
function getPositiveEnvIntOrDefault(envName, defaultMs, env = process.env) {
  const raw = env[envName];
  if (!raw || !raw.trim()) {
    return defaultMs;
  }
  const trimmed = raw.trim();
  if (!/^\d+$/.test(trimmed)) {
    return defaultMs;
  }
  const parsed = Number(trimmed);
  if (!Number.isSafeInteger(parsed) || parsed <= 0) {
    return defaultMs;
  }
  return Math.min(parsed, MAX_SETUP_TIMEOUT_MS);
}

/**
 * @param {keyof typeof SETUP_TIMEOUTS} name
 * @param {NodeJS.ProcessEnv} [env]
 * @returns {number}
 */
function getSetupTimeoutMs(name, env = process.env) {
  const timeout = SETUP_TIMEOUTS[name];
  if (!timeout) {
    throw new Error(`Unknown setup timeout: ${String(name)}`);
  }
  return getPositiveEnvIntOrDefault(timeout.env, timeout.defaultMs, env);
}

module.exports = {
  MAX_SETUP_TIMEOUT_MS,
  SETUP_TIMEOUTS,
  getPositiveEnvIntOrDefault,
  getSetupTimeoutMs,
};
