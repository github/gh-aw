// @ts-check
"use strict";

// Ensures global.core is available when running outside github-script context
require("./shim.cjs");

/**
 * start_mcp_gateway.cjs
 *
 * @safe-outputs-exempt SEC-004: "body" references are local healthcheck response payloads, not user-authored comment bodies
 *
 * Starts the MCP gateway process that proxies MCP servers through a unified HTTP endpoint.
 * Following the MCP Gateway Specification:
 *   https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/mcp-gateway.md
 * Per MCP Gateway Specification v1.0.0: Only container-based execution is supported.
 *
 * This script reads the MCP configuration from stdin and pipes it to the gateway container.
 *
 * Required environment variables:
 * - MCP_GATEWAY_DOCKER_COMMAND: Container image to run (required)
 * - MCP_GATEWAY_AGENT_ID: agent ID for gateway authentication (required for converter scripts)
 * - MCP_GATEWAY_PORT: Port for MCP gateway
 * - MCP_GATEWAY_DOMAIN: Domain for MCP server URLs (e.g., host.docker.internal)
 * - RUNNER_TEMP: GitHub Actions runner temp directory
 * - GITHUB_OUTPUT: Path to GitHub Actions output file
 *
 * Optional:
 * - GH_AW_ENGINE: Engine type (copilot, codex, claude, gemini)
 * - GH_AW_MCP_CLI_SERVERS: JSON array of server names to exclude from agent config
 * - GH_AW_MCP_GATEWAY_CUSTOM_ENV_NAMES: JSON array of custom gateway environment variable names
 */

const { spawn, execFileSync, execSync } = require("child_process");
const fs = require("fs");
const http = require("http");
const os = require("os");
const path = require("path");
const { renderLogToStdout } = require("./render_log_to_stdout.cjs");
const { withRetry } = require("./error_recovery.cjs");
const { lstatGuard } = require("./symlink_guard.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { getSetupTimeoutMs } = require("./child_process_timeouts.cjs");

/** @type {number | null} */
let activeGatewayPid = null;
const customGatewayEnvMarker = "__GH_AW_MCP_GATEWAY_CUSTOM_ENV__";
const customGatewayEnvNamePattern = /^[A-Z_][A-Z0-9_]*$/;
const customGatewayEnvNamesVar = "GH_AW_MCP_GATEWAY_CUSTOM_ENV_NAMES";
const customGatewayEnvTransportPrefix = "GH_AW_MCP_GATEWAY_ENV_";
const customGatewayReservedEnvPrefix = "GH_AW_MCP_GATEWAY_";
const CONTAINER_STATUS_TIMEOUT_MS = getSetupTimeoutMs("mcpContainerStatus");
const CONFIG_CONVERTER_TIMEOUT_MS = getSetupTimeoutMs("mcpConfigConverter");
const DOCKER_CLEANUP_TIMEOUT_MS = getSetupTimeoutMs("mcpDockerCleanup");
const MCP_SERVER_CHECK_TIMEOUT_MS = getSetupTimeoutMs("mcpServerCheck");

/**
 * Default health-check retry parameters. The defaults give the gateway a retry
 * budget that outlasts the MCP backend startup timeout, leaving room for backend
 * cleanup and final server registration before the health check gives up.
 */
const MCP_GATEWAY_HEALTH_RETRY_DEFAULTS = Object.freeze({
  maxTotalAttempts: 150,
  initialDelayMs: 250,
  maxDelayMs: 1000,
  backoffMultiplier: 2,
});

/** Environment variable overrides for each health-check retry parameter. */
const MCP_GATEWAY_HEALTH_RETRY_ENV = Object.freeze({
  maxTotalAttempts: "GH_AW_MCP_GATEWAY_HEALTH_MAX_ATTEMPTS",
  initialDelayMs: "GH_AW_MCP_GATEWAY_HEALTH_INITIAL_DELAY_MS",
  maxDelayMs: "GH_AW_MCP_GATEWAY_HEALTH_MAX_DELAY_MS",
  backoffMultiplier: "GH_AW_MCP_GATEWAY_HEALTH_BACKOFF_MULTIPLIER",
  backendStartupTimeoutMs: "GH_AW_MCP_GATEWAY_BACKEND_STARTUP_TIMEOUT_MS",
});

/** Default MCP backend startup timeout the retry budget must outlast. */
const MCP_GATEWAY_BACKEND_STARTUP_TIMEOUT_DEFAULT_MS = 120_000;

/** Upper bound on configurable health-check attempts, to keep the wait finite. */
const MCP_GATEWAY_HEALTH_MAX_ATTEMPTS_LIMIT = 10_000;

/**
 * Extra time the health check must keep polling after the backend startup
 * timeout so that backend cleanup and final server registration can complete.
 */
const MCP_GATEWAY_HEALTH_REGISTRATION_MARGIN_MS = 25_000;

/**
 * Reads a positive number from the environment, warning when the variable is
 * set to a value that cannot be used.
 *
 * @param {string} envName
 * @param {number} defaultValue
 * @param {NodeJS.ProcessEnv} env
 * @param {{ integer?: boolean }} [options]
 * @returns {number}
 */
function readPositiveEnvNumber(envName, defaultValue, env, options = {}) {
  const raw = env[envName];
  if (raw === undefined || !raw.trim()) {
    return defaultValue;
  }
  const parsed = Number(raw.trim());
  const isValid = Number.isFinite(parsed) && parsed > 0 && (!options.integer || Number.isSafeInteger(parsed));
  if (!isValid) {
    core.warning(`Ignoring invalid ${envName}='${raw}': expected a positive ${options.integer ? "integer" : "number"}, using ${defaultValue}`);
    return defaultValue;
  }
  return parsed;
}

/**
 * Computes the cumulative retry delay withRetry will spend before exhausting
 * its attempts. withRetry applies the backoff multiplier before its first sleep,
 * so the first retry waits initialDelayMs * backoffMultiplier.
 *
 * @param {{ maxTotalAttempts: number, initialDelayMs: number, maxDelayMs: number, backoffMultiplier: number }} config
 * @returns {number}
 */
function computeHealthRetryBudgetMs(config) {
  let delayMs = config.initialDelayMs;
  let budgetMs = 0;
  for (let retry = 1; retry < config.maxTotalAttempts; retry += 1) {
    delayMs = Math.min(delayMs * config.backoffMultiplier, config.maxDelayMs);
    budgetMs += delayMs;
  }
  return budgetMs;
}

/**
 * Resolves the gateway health-check retry parameters from the environment and
 * validates that they are mutually consistent.
 *
 * @param {NodeJS.ProcessEnv} [env]
 * @returns {{ maxTotalAttempts: number, initialDelayMs: number, maxDelayMs: number, backoffMultiplier: number, backendStartupTimeoutMs: number, retryBudgetMs: number }}
 */
function resolveGatewayHealthRetryConfig(env = process.env) {
  let maxTotalAttempts = readPositiveEnvNumber(MCP_GATEWAY_HEALTH_RETRY_ENV.maxTotalAttempts, MCP_GATEWAY_HEALTH_RETRY_DEFAULTS.maxTotalAttempts, env, { integer: true });
  const initialDelayMs = readPositiveEnvNumber(MCP_GATEWAY_HEALTH_RETRY_ENV.initialDelayMs, MCP_GATEWAY_HEALTH_RETRY_DEFAULTS.initialDelayMs, env, { integer: true });
  let maxDelayMs = readPositiveEnvNumber(MCP_GATEWAY_HEALTH_RETRY_ENV.maxDelayMs, MCP_GATEWAY_HEALTH_RETRY_DEFAULTS.maxDelayMs, env, { integer: true });
  let backoffMultiplier = readPositiveEnvNumber(MCP_GATEWAY_HEALTH_RETRY_ENV.backoffMultiplier, MCP_GATEWAY_HEALTH_RETRY_DEFAULTS.backoffMultiplier, env);
  const backendStartupTimeoutMs = readPositiveEnvNumber(MCP_GATEWAY_HEALTH_RETRY_ENV.backendStartupTimeoutMs, MCP_GATEWAY_BACKEND_STARTUP_TIMEOUT_DEFAULT_MS, env, { integer: true });

  if (maxTotalAttempts > MCP_GATEWAY_HEALTH_MAX_ATTEMPTS_LIMIT) {
    core.warning(`${MCP_GATEWAY_HEALTH_RETRY_ENV.maxTotalAttempts}=${maxTotalAttempts} exceeds the supported maximum, using ${MCP_GATEWAY_HEALTH_MAX_ATTEMPTS_LIMIT}`);
    maxTotalAttempts = MCP_GATEWAY_HEALTH_MAX_ATTEMPTS_LIMIT;
  }

  // A backoff multiplier below 1 would shrink the delay on every retry, which
  // contradicts the exponential backoff the retry budget is sized for.
  if (backoffMultiplier < 1) {
    core.warning(`${MCP_GATEWAY_HEALTH_RETRY_ENV.backoffMultiplier}=${backoffMultiplier} is below 1, using 1 (constant delay)`);
    backoffMultiplier = 1;
  }

  // withRetry caps every delay at maxDelayMs, so a smaller cap than the initial
  // delay would silently shorten the first sleep.
  if (maxDelayMs < initialDelayMs) {
    core.warning(`${MCP_GATEWAY_HEALTH_RETRY_ENV.maxDelayMs}=${maxDelayMs} is below ${MCP_GATEWAY_HEALTH_RETRY_ENV.initialDelayMs}=${initialDelayMs}, using ${initialDelayMs}`);
    maxDelayMs = initialDelayMs;
  }

  const config = { maxTotalAttempts, initialDelayMs, maxDelayMs, backoffMultiplier, backendStartupTimeoutMs };
  const retryBudgetMs = computeHealthRetryBudgetMs(config);

  const requiredBudgetMs = backendStartupTimeoutMs + MCP_GATEWAY_HEALTH_REGISTRATION_MARGIN_MS;
  if (retryBudgetMs < requiredBudgetMs) {
    core.warning(
      `MCP gateway health retry budget is ${Math.round(retryBudgetMs / 1000)}s, which is below the ${Math.round(requiredBudgetMs / 1000)}s needed for the ${Math.round(backendStartupTimeoutMs / 1000)}s backend startup timeout plus cleanup and registration; the health check may give up before the gateway is ready`
    );
  }

  return Object.freeze({ ...config, retryBudgetMs });
}

// ---------------------------------------------------------------------------
// Timing helpers
// ---------------------------------------------------------------------------

function nowMs() {
  return Date.now();
}

/**
 * @param {number} startMs
 * @param {string} label
 */
function printTiming(startMs, label) {
  const elapsed = nowMs() - startMs;
  core.info(`⏱️  TIMING: ${label} took ${elapsed}ms`);
}

// ---------------------------------------------------------------------------
// Utility helpers
// ---------------------------------------------------------------------------

/**
 * @param {number} ms
 * @returns {Promise<void>}
 */
function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

/**
 * Resolves the marker file that records a successful gateway startup.
 * print_firewall_logs.sh reads this marker to distinguish "the gateway never
 * completed startup" from "the gateway started but produced no auditable
 * egress traffic" when the Squid access log is missing.
 *
 * @param {NodeJS.ProcessEnv} env
 * @returns {string}
 */
function getGatewayStartupMarkerPath(env = process.env) {
  return env.MCP_GATEWAY_STARTUP_MARKER || "/tmp/gh-aw/mcp-gateway-started";
}

/**
 * Removes any stale startup marker left behind by a previous run.
 * @param {string} markerPath
 */
function clearGatewayStartupMarker(markerPath) {
  try {
    fs.rmSync(markerPath, { force: true });
  } catch (err) {
    core.warning(`Could not remove stale gateway startup marker ${markerPath}: ${getErrorMessage(err)}`);
  }
}

/**
 * Records that the gateway completed startup. Marker creation is best-effort:
 * it only affects the wording of a downstream firewall warning.
 * @param {string} markerPath
 */
function writeGatewayStartupMarker(markerPath) {
  try {
    fs.mkdirSync(path.dirname(markerPath), { recursive: true });
    fs.writeFileSync(markerPath, "", { mode: 0o600 });
  } catch (err) {
    core.warning(`Could not write gateway startup marker ${markerPath}: ${getErrorMessage(err)}`);
  }
}

/**
 * Credential shapes that may appear in gateway stderr (bearer headers and
 * structured `key: value` / `"key": "value"` pairs). They are replaced before
 * the stderr is written to the workflow log.
 */
const gatewayCredentialRedactions = [
  { pattern: /(Bearer\s+)\S+/gi, replacement: "$1[REDACTED]" },
  { pattern: /((?:agent[_-]?id|api[_-]?key|token|secret|password|authorization)"?\s*[:=]\s*"?)[^\s,}"]+/gi, replacement: "$1[REDACTED]" },
];

/**
 * Redacts common credential formats from gateway diagnostics output.
 * @param {string} text
 * @returns {string}
 */
function redactGatewayDiagnostics(text) {
  let redacted = text;
  for (const { pattern, replacement } of gatewayCredentialRedactions) {
    redacted = redacted.replace(pattern, replacement);
  }
  return redacted;
}

/**
 * Creates the file that captures raw gateway stderr. It lives outside the
 * artifact directory and is readable only by the current user, because the
 * unredacted stderr may contain credentials.
 * @returns {string}
 */
function createGatewayStderrPath() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-mcp-gateway-"));
  fs.chmodSync(dir, 0o700);
  process.on("exit", () => {
    try {
      fs.rmSync(dir, { recursive: true, force: true });
    } catch {
      // best-effort cleanup
    }
  });
  return path.join(dir, "stderr.log");
}

/**
 * Prints redacted gateway stderr in a collapsed log group. Gateway stdout is
 * withheld because it carries the gateway configuration, which contains
 * credentials.
 * @param {string} stderrPath
 * @param {string} stdoutPath
 */
function printGatewayStartupDiagnostics(stderrPath, stdoutPath) {
  core.error("Gateway startup diagnostics:");
  let stderrContent = "";
  try {
    stderrContent = fs.readFileSync(stderrPath, "utf8");
  } catch {
    stderrContent = "";
  }
  if (stderrContent.trim()) {
    renderLogToStdout("Gateway stderr", redactGatewayDiagnostics(stderrContent));
  } else {
    core.error("Gateway stderr: (empty)");
  }
  let stdoutSize = 0;
  try {
    stdoutSize = fs.statSync(stdoutPath).size;
  } catch {
    stdoutSize = 0;
  }
  if (stdoutSize > 0) {
    core.error("Gateway stdout was captured but is not displayed because it may contain credentials.");
  } else {
    core.error("Gateway stdout: (empty)");
  }
}

/**
 * Replaces the compiler marker with atomic Docker -e arguments. Runtime values
 * never enter MCP_GATEWAY_DOCKER_COMMAND, preventing Docker argument injection.
 *
 * @param {string[]} args
 * @param {NodeJS.ProcessEnv} env
 * @returns {string[]}
 */
function injectCustomGatewayEnvArgs(args, env = process.env) {
  const markerIndex = args.indexOf(customGatewayEnvMarker);
  if (markerIndex === -1) {
    return args;
  }

  let names;
  try {
    names = JSON.parse(env[customGatewayEnvNamesVar] || "[]");
  } catch (err) {
    throw new Error(`${customGatewayEnvNamesVar} must be valid JSON: ${getErrorMessage(err)}`, { cause: err });
  }
  if (!Array.isArray(names) || !names.every(name => typeof name === "string" && customGatewayEnvNamePattern.test(name))) {
    throw new Error(`${customGatewayEnvNamesVar} must be an array of valid environment variable names`);
  }
  if (names.some(name => name.startsWith(customGatewayReservedEnvPrefix))) {
    throw new Error(`${customGatewayEnvNamesVar} must not contain names reserved by the ${customGatewayReservedEnvPrefix} namespace`);
  }
  if (new Set(names).size !== names.length) {
    throw new Error(`${customGatewayEnvNamesVar} must not contain duplicate environment variable names`);
  }

  // Missing indexed transport values intentionally become empty container env vars.
  // This preserves deterministic NAME→slot mapping and keeps Docker argument injection
  // impossible even if the compiler/runtime metadata ever diverges.
  const customArgs = names.flatMap((name, index) => ["-e", `${name}=${env[`${customGatewayEnvTransportPrefix}${index}`] || ""}`]);
  return [...args.slice(0, markerIndex), ...customArgs, ...args.slice(markerIndex + 1)];
}

/**
 * Builds targeted context for a JSON.parse error so logs can point at the likely key.
 *
 * @param {string} jsonText
 * @param {string} parseErrorMessage
 * @returns {{line: number, column: number, lineText: string, key: string | null} | null}
 */
function getJSONParseErrorContext(jsonText, parseErrorMessage) {
  const posMatch = parseErrorMessage.match(/position\s+(\d+)/i);
  if (!posMatch) {
    return null;
  }

  const pos = Number(posMatch[1]);
  if (!Number.isFinite(pos) || pos < 0) {
    return null;
  }

  const safePos = Math.min(pos, Math.max(0, jsonText.length - 1));
  const before = jsonText.slice(0, safePos);
  const line = before.split("\n").length;
  const lineStart = before.lastIndexOf("\n") + 1;
  const lineEnd = jsonText.indexOf("\n", safePos);
  const resolvedLineEnd = lineEnd === -1 ? jsonText.length : lineEnd;
  const lineText = jsonText.slice(lineStart, resolvedLineEnd);
  const column = safePos - lineStart + 1;
  const keyMatch = lineText.match(/"([^"]+)"\s*:/);
  const key = keyMatch ? keyMatch[1] : null;

  return { line, column, lineText, key };
}

/**
 * Repairs a known double-encoding regression where sink-visibility can be rendered as:
 *   "sink-visibility": ""public""
 * instead of:
 *   "sink-visibility": "public"
 *
 * @param {string} jsonText
 * @returns {string}
 */
function normalizeSinkVisibilityEncoding(jsonText) {
  return jsonText.replace(/("sink-visibility"\s*:\s*)""(public|private|internal)""/g, '$1"$2"');
}

/**
 * Normalizes GH_AW_OTLP_IF_MISSING to a supported mode.
 * @param {string | undefined} value
 * @returns {"error" | "warn" | "ignore"}
 */
function getOTLPIfMissingMode(value) {
  const normalized = (value || "").trim().toLowerCase();
  if (normalized === "warn" || normalized === "ignore") {
    return normalized;
  }
  return "error";
}

/**
 * Returns true when GH_AW_OTLP_IF_MISSING is set to "ignore".
 * @param {string | undefined} value
 * @returns {boolean}
 */
function isOTLPIfMissingIgnore(value) {
  return getOTLPIfMissingMode(value) === "ignore";
}

/**
 * Returns true when OTLP headers contain at least one non-empty value.
 * NOTE: Unexpected primitive values are treated as non-empty so they are
 * preserved for downstream validation/error handling.
 * @param {unknown} headers
 * @returns {boolean}
 */
function hasNonEmptyOTLPHeaders(headers) {
  if (headers == null) {
    return false;
  }
  if (typeof headers === "string") {
    return headers.trim() !== "";
  }
  if (Array.isArray(headers)) {
    return headers.some(item => hasNonEmptyOTLPHeaders(item));
  }
  if (typeof headers === "object") {
    return Object.values(headers).some(value => hasNonEmptyOTLPHeaders(value));
  }
  // For unexpected primitive types, keep headers
  // intact so downstream validation can fail explicitly rather than silently
  // treating them as "missing".
  return true;
}

/**
 * Apply observability.otlp.if-missing: ignore behavior to gateway OTLP config.
 * When enabled, missing endpoint values are downgraded to warnings and OTLP
 * gateway configuration is skipped instead of hard-failing the workflow.
 *
 * @param {Record<string, unknown>} configObj
 */
function applyOTLPIgnoreIfMissing(configObj) {
  const mode = getOTLPIfMissingMode(process.env.GH_AW_OTLP_IF_MISSING);
  const shouldDowngradeMissing = mode === "warn" || mode === "ignore";
  const shouldWarn = mode === "warn";
  if (!shouldDowngradeMissing) {
    return;
  }
  const gw = configObj.gateway;
  if (!gw || typeof gw !== "object" || Array.isArray(gw)) {
    return;
  }
  const gateway = /** @type {Record<string, unknown>} */ gw;
  const otel = gateway["opentelemetry"];
  if (!otel || typeof otel !== "object" || Array.isArray(otel)) {
    return;
  }
  const otelConfig = /** @type {Record<string, unknown>} */ otel;
  const endpoint = typeof otelConfig["endpoint"] === "string" ? otelConfig["endpoint"].trim() : "";
  if (!endpoint) {
    delete gateway["opentelemetry"];
    if (shouldWarn) {
      core.warning("OTLP endpoint is missing/empty and GH_AW_OTLP_IF_MISSING=warn; skipping MCP gateway OTLP configuration.");
    }
    return;
  }
  const headers = otelConfig["headers"];
  if ("headers" in otelConfig && !hasNonEmptyOTLPHeaders(headers)) {
    delete otelConfig["headers"];
    if (shouldWarn) {
      core.warning("OTLP headers are missing/empty and GH_AW_OTLP_IF_MISSING=warn; continuing without MCP gateway OTLP headers.");
    }
  }
}

/**
 * Collect the names of MCP servers declared as non-critical (`required: false`)
 * and remove the flag from the configuration handed to the gateway.
 *
 * The gateway configuration specification does not define a `required` field,
 * and the gateway output does not echo it back, so the flag is extracted here
 * and forwarded to the startup connectivity check instead.
 *
 * @param {Record<string, unknown>} configObj
 * @returns {string[]} names of servers whose startup failure must not abort the workflow
 */
function extractOptionalServerNames(configObj) {
  const servers = configObj && configObj.mcpServers;
  if (!servers || typeof servers !== "object" || Array.isArray(servers)) {
    return [];
  }
  const optional = [];
  for (const [name, value] of Object.entries(/** @type {Record<string, unknown>} */ servers)) {
    if (!value || typeof value !== "object" || Array.isArray(value)) {
      continue;
    }
    const server = /** @type {Record<string, unknown>} */ value;
    if (server.required === false) {
      optional.push(name);
    }
    // The flag is always removed — including when set to true — because the
    // gateway configuration specification does not define a `required` field.
    delete server.required;
  }
  return optional;
}

/**
 * Check whether a process is alive.
 * @param {number} pid
 * @returns {boolean}
 */
function isProcessAlive(pid) {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

/**
 * Best-effort cleanup for a detached gateway process when startup fails.
 * @param {number | null | undefined} pid
 */
function stopGatewayProcess(pid) {
  if (!pid || !isProcessAlive(pid)) {
    return;
  }
  try {
    process.kill(pid);
  } catch {
    // ignore cleanup failures
  }
}

/**
 * HTTP GET helper – returns { statusCode, body }.
 * @param {string} url
 * @param {number} timeoutMs
 * @returns {Promise<{ statusCode: number; body: string }>}
 */
function httpGet(url, timeoutMs) {
  return new Promise((resolve, reject) => {
    const req = http.get(url, { timeout: timeoutMs }, res => {
      let data = "";
      res.on("error", reject);
      res.on("data", chunk => (data += chunk));
      res.on("end", () => resolve({ statusCode: res.statusCode || 0, body: data }));
    });
    req.on("timeout", () => {
      req.destroy();
      reject(new Error("timeout"));
    });
    req.on("error", reject);
  });
}

// ---------------------------------------------------------------------------
// Validation helpers
// ---------------------------------------------------------------------------

/**
 * Validate that a path is not a symlink (symlink attack prevention).
 * @param {string} p
 * @returns {boolean}
 */
function assertNotSymlink(p) {
  try {
    if (lstatGuard(p) === null) {
      core.setFailed(`ERROR: ${p} is a symlink — possible symlink attack, aborting`);
      return false;
    }
  } catch (error) {
    const errorCode = typeof error === "object" && error !== null && "code" in error ? error.code : undefined;
    if (errorCode === "ENOENT") {
      // Path does not exist yet — ignored, that's fine.
      return true;
    }
    core.setFailed(`ERROR: failed to inspect ${p} for symlink safety: ${getErrorMessage(error)}`);
    return false;
  }
  return true;
}

/**
 * Resolves the Copilot CLI config directory and the MCP config file path from
 * the runtime $HOME. The Copilot CLI uses ~/.copilot, which is
 * /home/runner/.copilot on standard GitHub-hosted runners (HOME=/home/runner)
 * but may differ on self-hosted or containerized runners. HOME is a standard
 * POSIX environment variable inherited from the runner's parent process and
 * passed through to shell steps; other generators (copilot_mcp.go,
 * copilot_engine_execution.go) rely on it the same way.
 *
 * Exported for testability; throws Error rather than exiting so tests can
 * exercise the missing-HOME branch.
 *
 * @returns {{ dir: string, file: string }}
 */
function resolveCopilotConfigPaths() {
  const home = process.env.HOME;
  if (!home) {
    throw new Error("HOME environment variable is not set; cannot locate Copilot CLI config directory");
  }
  const dir = path.join(home, ".copilot");
  return { dir, file: path.join(dir, "mcp-config.json") };
}

/**
 * Determines which engine-specific MCP config converter to use.
 * Copilot auto-detection consults HOME only when HOME is available so
 * explicit non-Copilot engines do not fail just because HOME is unset.
 *
 * @param {string} configDir
 * @param {NodeJS.ProcessEnv} [env]
 * @param {(p: string) => boolean} [existsSync]
 * @returns {string}
 */
function detectEngineType(configDir, env = process.env, existsSync = fs.existsSync) {
  if (env.GH_AW_ENGINE) {
    return env.GH_AW_ENGINE;
  }
  if (env.GITHUB_COPILOT_CLI_MODE) {
    return "copilot";
  }
  if (env.HOME && existsSync(path.join(env.HOME, ".copilot"))) {
    return "copilot";
  }
  if (existsSync(path.join(configDir, "config.toml"))) {
    return "codex";
  }
  if (existsSync(path.join(configDir, "mcp-servers.json"))) {
    return "claude";
  }
  return "unknown";
}

/**
 * Validates the mutually exclusive gateway agent identifier forms.
 *
 * @param {Record<string, unknown>} gateway
 * @returns {string | null}
 */
function validateGatewayAgentIdentifiers(gateway) {
  const hasAgentId = Object.prototype.hasOwnProperty.call(gateway, "agentId");
  const hasAgentIds = Object.prototype.hasOwnProperty.call(gateway, "agentIds");

  if (hasAgentId === hasAgentIds) {
    return "ERROR: Gateway configuration must specify exactly one of 'agentId' or 'agentIds'";
  }
  if (hasAgentId) {
    return typeof gateway.agentId === "string" && gateway.agentId.length > 0 ? null : "ERROR: Gateway 'agentId' must be a non-empty string";
  }
  return Array.isArray(gateway.agentIds) && gateway.agentIds.length > 0 && gateway.agentIds.every(agentId => typeof agentId === "string" && agentId.length > 0)
    ? null
    : "ERROR: Gateway 'agentIds' must be a non-empty array of non-empty strings";
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main() {
  activeGatewayPid = null;
  // Restrict default file creation mode to owner-only (rw-------)
  process.umask(0o077);

  const dockerCommand = process.env.MCP_GATEWAY_DOCKER_COMMAND;
  const agentId = process.env.MCP_GATEWAY_AGENT_ID;
  const gatewayPort = process.env.MCP_GATEWAY_PORT;
  const gatewayDomain = process.env.MCP_GATEWAY_DOMAIN;
  const runnerTemp = process.env.RUNNER_TEMP;
  const githubOutput = process.env.GITHUB_OUTPUT;

  // -----------------------------------------------------------------------
  // Validate required env vars
  // -----------------------------------------------------------------------
  if (!dockerCommand) {
    core.setFailed("ERROR: MCP_GATEWAY_DOCKER_COMMAND must be set (command-based execution is not supported per MCP Gateway Specification v1.0.0)");
    return;
  }

  // Validate port is numeric to prevent injection in shell commands and URLs
  if (gatewayPort && !/^\d+$/.test(gatewayPort)) {
    core.setFailed(`ERROR: MCP_GATEWAY_PORT must be a numeric value, got: '${gatewayPort}'`);
    return;
  }

  core.info("=== MCP Gateway Startup ===");
  core.info(`Engine: ${process.env.GH_AW_ENGINE || "(auto-detect)"}`);
  core.info(`Runner temp: ${runnerTemp || "(not set)"}`);
  core.info(`Gateway port: ${gatewayPort || "(not set)"}`);
  core.info(`Gateway domain: ${gatewayDomain || "(not set)"}`);
  core.info("");

  // -----------------------------------------------------------------------
  // Create directories
  // -----------------------------------------------------------------------
  // Config and CLI manifest are stored under ${RUNNER_TEMP}/gh-aw/ to prevent
  // tampering.  RUNNER_TEMP is a per-runner directory that is not
  // world-writable, unlike /tmp.  Logs remain under /tmp/gh-aw/mcp-logs/
  // because the Docker gateway container mounts -v /tmp:/tmp:rw and writes
  // there via MCP_GATEWAY_LOG_DIR.
  const configDir = path.join(runnerTemp || "/tmp", "gh-aw/mcp-config");
  const cliDir = path.join(runnerTemp || "/tmp", "gh-aw/mcp-cli");

  try {
    fs.mkdirSync("/tmp/gh-aw/mcp-logs", { recursive: true });
  } catch (err) {
    throw new Error(`Failed to create directory /tmp/gh-aw/mcp-logs: ${getErrorMessage(err)}`, { cause: err });
  }

  // Symlink attack prevention on the config directory
  if (!assertNotSymlink(configDir)) {
    return;
  }
  try {
    fs.mkdirSync(configDir, { recursive: true });
  } catch (err) {
    throw new Error(`Failed to create directory ${configDir}: ${getErrorMessage(err)}`, { cause: err });
  }
  // Post-creation check
  if (!assertNotSymlink(configDir)) {
    return;
  }
  try {
    fs.chmodSync(configDir, 0o700);
  } catch (err) {
    throw new Error(`Failed to set permissions on ${configDir}: ${getErrorMessage(err)}`, { cause: err });
  }

  // -----------------------------------------------------------------------
  // Validate container syntax
  // -----------------------------------------------------------------------
  if (!/^docker run/.test(dockerCommand)) {
    core.error("ERROR: MCP_GATEWAY_DOCKER_COMMAND has incorrect syntax");
    core.error("Expected: docker run command with image and arguments");
    core.setFailed(`ERROR: MCP_GATEWAY_DOCKER_COMMAND has incorrect syntax — got: ${dockerCommand}`);
    return;
  }
  if (!/-i/.test(dockerCommand)) {
    core.setFailed("ERROR: MCP_GATEWAY_DOCKER_COMMAND must include -i flag for interactive mode");
    return;
  }
  if (!/--rm/.test(dockerCommand)) {
    core.setFailed("ERROR: MCP_GATEWAY_DOCKER_COMMAND must include --rm flag for cleanup");
    return;
  }
  if (!/--network/.test(dockerCommand)) {
    core.setFailed("ERROR: MCP_GATEWAY_DOCKER_COMMAND must include --network flag for networking");
    return;
  }

  // -----------------------------------------------------------------------
  // Read MCP configuration from stdin
  // -----------------------------------------------------------------------
  const scriptStartTime = nowMs();

  core.info("Reading MCP configuration from stdin...");
  const configReadStart = nowMs();
  let mcpConfig;
  try {
    mcpConfig = fs.readFileSync(0, "utf8"); // fd 0 = stdin
  } catch (err) {
    throw new Error(`Failed to read MCP configuration from stdin: ${getErrorMessage(err)}`, { cause: err });
  }
  const normalizedConfig = normalizeSinkVisibilityEncoding(mcpConfig);
  if (normalizedConfig !== mcpConfig) {
    core.warning("Detected double-encoded sink-visibility value in MCP config; applying compatibility normalization.");
    mcpConfig = normalizedConfig;
  }
  printTiming(configReadStart, "Configuration read from stdin");
  core.info("");

  // Log configuration for debugging
  core.info("-------START MCP CONFIG-----------");
  core.info(mcpConfig);
  core.info("-------END MCP CONFIG-----------");
  core.info("");

  // -----------------------------------------------------------------------
  // Validate JSON
  // -----------------------------------------------------------------------
  const configValidationStart = nowMs();
  /** @type {Record<string, unknown>} */
  let configObj;
  try {
    configObj = JSON.parse(mcpConfig);
  } catch (err) {
    core.error("ERROR: Configuration is not valid JSON");
    core.error("");
    core.error("JSON validation error:");
    const parseMessage = getErrorMessage(err);
    core.error(parseMessage);
    const parseContext = getJSONParseErrorContext(mcpConfig, parseMessage);
    if (parseContext) {
      core.error(`Likely offending location: line ${parseContext.line}, column ${parseContext.column}`);
      if (parseContext.key) {
        core.error(`Likely offending key: ${parseContext.key}`);
      }
      core.error(`Context line: ${parseContext.lineText}`);
    }
    core.error("");
    core.error("Configuration content:");
    const lines = mcpConfig.split("\n");
    core.error(lines.slice(0, 50).join("\n"));
    if (lines.length > 50) {
      core.error("... (truncated, showing first 50 lines)");
    }
    core.setFailed("ERROR: Configuration is not valid JSON");
    return;
  }

  // Validate gateway section
  core.info("Validating gateway configuration...");
  const gw = configObj.gateway;
  if (!gw || typeof gw !== "object") {
    core.error("Per MCP Gateway Specification v1.0.0 section 4.1.3, the gateway section is required");
    core.setFailed("ERROR: Configuration is missing required 'gateway' section");
    return;
  }
  if (!("port" in gw) || gw.port == null) {
    core.setFailed("ERROR: Gateway configuration is missing required 'port' field");
    return;
  }
  if (!("domain" in gw) || gw.domain == null) {
    core.setFailed("ERROR: Gateway configuration is missing required 'domain' field");
    return;
  }
  const agentIdentifierError = validateGatewayAgentIdentifiers(gw);
  if (agentIdentifierError) {
    core.setFailed(agentIdentifierError);
    return;
  }

  applyOTLPIgnoreIfMissing(configObj);

  // Servers declared with `required: false` are best-effort: a failed startup
  // connectivity check for them must not abort the whole gateway.
  const optionalServerNames = extractOptionalServerNames(configObj);
  if (optionalServerNames.length > 0) {
    core.info(`Non-critical MCP server(s) (startup failures tolerated): ${optionalServerNames.join(", ")}`);
  }

  core.info("Configuration validated successfully");
  printTiming(configValidationStart, "Configuration validation");
  core.info("");

  // -----------------------------------------------------------------------
  // Start gateway container
  // -----------------------------------------------------------------------
  const logDir = "/tmp/gh-aw/mcp-logs/";
  const outputPath = path.join(configDir, "gateway-output.json");
  const gatewayStartupMarker = getGatewayStartupMarkerPath();
  clearGatewayStartupMarker(gatewayStartupMarker);

  // Clean up any stale gateway container from a previous run on this runner.
  // On persistent self-hosted runners a prior job's gateway container may still
  // be running and holding the host port, causing "bind: address already in use"
  // when we try to start the new one.  Force-removing by the well-known container
  // name is idempotent: docker rm -f exits non-zero when the container doesn't
  // exist, but the trailing || true (and 2>/dev/null) make the command succeed.
  core.info("Cleaning up any stale awmg-mcpg container from a previous run...");
  try {
    execSync("docker rm -f awmg-mcpg 2>/dev/null && echo 'Removed stale awmg-mcpg container' || true", { stdio: "inherit", timeout: DOCKER_CLEANUP_TIMEOUT_MS });
  } catch {
    // Non-fatal: proceed even if the cleanup command itself fails
    core.info("Could not remove stale awmg-mcpg container (may not exist)");
  }
  core.info("");

  core.info("Starting gateway container...");
  core.info("");

  const gatewayStartTime = nowMs();

  // Split docker command into args, respecting simple quoting
  let args = Array.from(dockerCommand.match(/(?:[^\s"']+|"[^"]*"|'[^']*')+/g) || []);
  const cmd = args.shift();
  if (!cmd) {
    core.setFailed("ERROR: MCP_GATEWAY_DOCKER_COMMAND did not contain an executable command");
    return;
  }
  args = injectCustomGatewayEnvArgs(args);

  const outputFd = fs.openSync(outputPath, "w", 0o600);
  const stderrPath = createGatewayStderrPath();
  const stderrFd = fs.openSync(stderrPath, "w", 0o600);

  const child = spawn(cmd, args, {
    stdio: ["pipe", outputFd, stderrFd],
    env: { ...process.env, MCP_GATEWAY_LOG_DIR: logDir },
    detached: true,
  });
  child.on("error", err => {
    activeGatewayPid = null;
    core.error(`ERROR: Failed to launch MCP gateway process: ${getErrorMessage(err)}`);
  });
  activeGatewayPid = child.pid || null;

  // Write configuration to stdin then close
  if (!child.stdin) {
    stopGatewayProcess(activeGatewayPid);
    core.setFailed("ERROR: Gateway process stdin is not available");
    return;
  }
  child.stdin.write(JSON.stringify(configObj));
  child.stdin.end();

  // Allow the child to run independently
  child.unref();

  const gatewayPid = child.pid;
  if (!gatewayPid) {
    core.setFailed("ERROR: Failed to start gateway container");
    return;
  }

  core.info(`Gateway started with PID: ${gatewayPid}`);
  printTiming(gatewayStartTime, "Gateway container launch");
  core.info("Verifying gateway process is running...");

  if (isProcessAlive(gatewayPid)) {
    core.info(`Gateway process confirmed running (PID: ${gatewayPid})`);
  } else {
    core.error("ERROR: Gateway process exited immediately after start");
    core.error("");
    printGatewayStartupDiagnostics(stderrPath, outputPath);
    core.setFailed("ERROR: Gateway process exited immediately after start");
    return;
  }
  core.info("");

  // -----------------------------------------------------------------------
  // Wait for gateway to initialise
  // -----------------------------------------------------------------------
  core.info("Waiting for gateway to initialize...");
  await sleep(5000);
  core.info("Checking if gateway process is still alive after initialization...");

  if (!isProcessAlive(gatewayPid)) {
    core.error(`ERROR: Gateway process (PID: ${gatewayPid}) exited during initialization`);
    core.error("");
    printGatewayStartupDiagnostics(stderrPath, outputPath);
    core.setFailed(`ERROR: Gateway process (PID: ${gatewayPid}) exited during initialization`);
    return;
  }
  core.info(`Gateway process is still running (PID: ${gatewayPid})`);
  core.info("");

  // -----------------------------------------------------------------------
  // Health check loop
  // -----------------------------------------------------------------------
  core.info("Waiting for gateway to be ready...");
  const healthCheckStart = nowMs();
  const healthHost = "localhost";
  const healthUrl = `http://${healthHost}:${gatewayPort}/health`;

  const healthRetryConfig = resolveGatewayHealthRetryConfig();
  const retryBudgetSeconds = Math.round(healthRetryConfig.retryBudgetMs / 1000);
  core.info(`Health endpoint: ${healthUrl}`);
  core.info(`(Note: MCP_GATEWAY_DOMAIN is '${gatewayDomain}' for container access)`);
  core.info(
    `Retrying up to ${healthRetryConfig.maxTotalAttempts} times with exponential backoff (${healthRetryConfig.initialDelayMs}ms initial delay, capped at ${healthRetryConfig.maxDelayMs}ms, ~${retryBudgetSeconds}s cumulative retry delay excluding request time)`
  );
  core.info("");

  const maxTotalAttempts = healthRetryConfig.maxTotalAttempts;
  // withRetry's maxRetries excludes the initial attempt.
  const maxRetryCount = maxTotalAttempts - 1;
  let httpCode = 0;
  let healthBody = "";
  let succeeded = false;
  let attemptsMade = 0;

  core.info("=== Health Check Progress ===");
  try {
    await withRetry(
      async () => {
        // Counts total health-check attempts, including the final successful attempt.
        attemptsMade += 1;
        const elapsedSec = Math.floor((nowMs() - healthCheckStart) / 1000);
        if (attemptsMade % 10 === 1 || attemptsMade === 1) {
          core.info(`Attempt ${attemptsMade}/${maxTotalAttempts} (${elapsedSec}s elapsed)...`);
        }

        const res = await httpGet(healthUrl, 2000);
        httpCode = res.statusCode;
        healthBody = res.body;
        if (httpCode === 200 && healthBody) {
          core.info(`✓ Health check succeeded on attempt ${attemptsMade} (${elapsedSec}s elapsed)`);
          succeeded = true;
          return;
        }
        throw new Error(`Health endpoint not ready (HTTP ${httpCode || 0})`);
      },
      {
        maxRetries: maxRetryCount,
        initialDelayMs: healthRetryConfig.initialDelayMs,
        maxDelayMs: healthRetryConfig.maxDelayMs,
        backoffMultiplier: healthRetryConfig.backoffMultiplier,
        jitterMs: 0,
        // Preserve previous loop behavior: retry any health-check failure until attempts are exhausted.
        shouldRetry: () => true,
      },
      "MCP gateway health check"
    );
  } catch {
    // Retry exhaustion is ignored here; handled below using existing diagnostics.
  }

  core.info("=== End Health Check Progress ===");
  core.info("");

  core.info(`Final HTTP code: ${httpCode}`);
  core.info(`Total attempts: ${attemptsMade}`);
  if (healthBody) {
    core.info(`Health response body: ${healthBody}`);
  } else {
    core.info("Health response body: (empty)");
  }

  if (succeeded) {
    core.info("Gateway is ready!");
    printTiming(healthCheckStart, "Health check wait");
    writeGatewayStartupMarker(gatewayStartupMarker);
  } else {
    core.error("");
    core.error("ERROR: Gateway failed to become ready");
    core.error(`Last HTTP code: ${httpCode}`);
    core.error(`Last health response: ${healthBody || "(empty)"}`);
    core.error("");
    core.error("Checking if gateway process is still alive...");
    if (isProcessAlive(gatewayPid)) {
      core.error(`Gateway process (PID: ${gatewayPid}) is still running`);
    } else {
      core.error(`Gateway process (PID: ${gatewayPid}) has exited`);
    }
    core.error("");
    core.error("Docker container status:");
    try {
      execSync("docker ps -a 2>/dev/null | head -20", { stdio: "inherit", timeout: CONTAINER_STATUS_TIMEOUT_MS });
    } catch {
      core.error("Could not list docker containers");
    }
    core.error("");
    printGatewayStartupDiagnostics(stderrPath, outputPath);
    core.error("");
    core.error("Checking network connectivity to gateway port...");
    try {
      // Validate gatewayPort is numeric to prevent shell injection
      const safePort = String(gatewayPort).replace(/[^0-9]/g, "");
      execSync(`netstat -tlnp 2>/dev/null | grep ":${safePort}" || ss -tlnp 2>/dev/null | grep ":${safePort}" || echo "Port ${safePort} does not appear to be listening"`, { stdio: "inherit", timeout: CONTAINER_STATUS_TIMEOUT_MS });
    } catch {
      // ignore
    }
    try {
      process.kill(gatewayPid);
    } catch {
      // ignore
    }
    core.setFailed("ERROR: Gateway failed to become ready");
    return;
  }
  core.info("");

  // -----------------------------------------------------------------------
  // Wait for gateway output (rewritten configuration)
  // -----------------------------------------------------------------------
  core.info("Reading gateway output configuration...");
  const outputWaitStart = nowMs();
  const waitAttempts = 10;
  for (let i = 0; i < waitAttempts; i++) {
    try {
      const stat = fs.statSync(outputPath);
      if (stat.size > 0) {
        core.info("Gateway output received!");
        break;
      }
    } catch {
      // Output file not ready yet — stat failure is ignored.
    }
    if (i < waitAttempts - 1) {
      await sleep(1000);
    }
  }
  printTiming(outputWaitStart, "Gateway output wait");
  core.info("");

  // Verify output was written
  let outputSize = 0;
  try {
    outputSize = fs.statSync(outputPath).size;
  } catch {
    // File doesn't exist — ignored, treated as empty output.
  }
  if (outputSize === 0) {
    core.error("ERROR: Gateway did not write output configuration");
    core.error("");
    printGatewayStartupDiagnostics(stderrPath, outputPath);
    try {
      process.kill(gatewayPid);
    } catch {
      // ignore
    }
    core.setFailed("ERROR: Gateway did not write output configuration");
    return;
  }

  // Restrict permissions
  try {
    fs.chmodSync(outputPath, 0o600);
  } catch (err) {
    throw new Error(`Failed to set permissions on ${outputPath}: ${getErrorMessage(err)}`, { cause: err });
  }

  // Check for error payload
  let gatewayOutput;
  try {
    gatewayOutput = JSON.parse(fs.readFileSync(outputPath, "utf8"));
  } catch (err) {
    throw new Error("Failed to parse gateway output file " + outputPath + ": " + getErrorMessage(err), { cause: err });
  }
  if (gatewayOutput.error) {
    core.error("ERROR: Gateway returned an error payload instead of configuration");
    core.error("");
    // Only the error field is emitted; the rest of the payload is the gateway
    // configuration, which contains credentials.
    core.error(`Gateway error: ${redactGatewayDiagnostics(typeof gatewayOutput.error === "string" ? gatewayOutput.error : JSON.stringify(gatewayOutput.error))}`);
    printGatewayStartupDiagnostics(stderrPath, outputPath);
    try {
      process.kill(gatewayPid);
    } catch {
      // ignore
    }
    core.setFailed("ERROR: Gateway returned an error payload instead of configuration");
    return;
  }

  // -----------------------------------------------------------------------
  // Convert gateway output to agent-specific format
  // -----------------------------------------------------------------------
  core.info("Converting gateway configuration to agent format...");
  const configConvertStart = nowMs();
  process.env.MCP_GATEWAY_OUTPUT = outputPath;

  // Validate MCP_GATEWAY_AGENT_ID
  if (!agentId) {
    stopGatewayProcess(gatewayPid);
    core.error("This variable should be set in the workflow before calling start_mcp_gateway.cjs");
    core.setFailed("ERROR: MCP_GATEWAY_AGENT_ID environment variable must be set for converter scripts");
    return;
  }

  // Determine engine type
  const engineType = detectEngineType(configDir);

  core.info(`Detected engine type: ${engineType}`);

  const converters = {
    copilot: "convert_gateway_config_copilot.cjs",
    codex: "convert_gateway_config_codex.cjs",
    claude: "convert_gateway_config_claude.cjs",
    gemini: "convert_gateway_config_gemini.cjs",
  };

  // Engines can declare a "mcp.config-adapter" script in their behaviors definition
  // (e.g. .github/workflows/shared/goose.md) instead of a built-in per-engine converter.
  // The compiler writes this script to disk and exports its filename via
  // GH_AW_MCP_CONFIG_ADAPTER before this gateway starts.
  const converterFile = process.env.GH_AW_MCP_CONFIG_ADAPTER || converters[/** @type {keyof typeof converters} */ engineType];
  if (converterFile) {
    core.info(`Using ${engineType} converter...`);
    const converterPath = path.join(runnerTemp || "", "gh-aw/actions", converterFile);
    try {
      execFileSync("node", [converterPath], { stdio: "inherit", env: process.env, timeout: CONFIG_CONVERTER_TIMEOUT_MS });
    } catch (err) {
      stopGatewayProcess(gatewayPid);
      core.setFailed(`ERROR: MCP config converter failed: ${getErrorMessage(err)}`);
      return;
    }
  } else {
    let copilotConfigDir, copilotConfigFile;
    try {
      ({ dir: copilotConfigDir, file: copilotConfigFile } = resolveCopilotConfigPaths());
    } catch (err) {
      stopGatewayProcess(gatewayPid);
      core.setFailed(`ERROR: ${getErrorMessage(err)}`);
      return;
    }
    core.info(`No agent-specific converter found for engine: ${engineType}`);
    core.info("Using gateway output directly");
    // Default fallback – copy to most common location, filtering CLI-mounted servers
    try {
      fs.mkdirSync(copilotConfigDir, { recursive: true });
    } catch (err) {
      throw new Error(`Failed to create directory ${copilotConfigDir}: ${getErrorMessage(err)}`, { cause: err });
    }
    const cliServersRaw = process.env.GH_AW_MCP_CLI_SERVERS;
    if (cliServersRaw) {
      try {
        const cliServers = new Set(JSON.parse(cliServersRaw));
        const filtered = { ...gatewayOutput };
        if (filtered.mcpServers && typeof filtered.mcpServers === "object") {
          const servers = /** @type {Record<string, unknown>} */ filtered.mcpServers;
          for (const key of Object.keys(servers)) {
            if (cliServers.has(key)) {
              delete servers[key];
            }
          }
        }
        fs.writeFileSync(copilotConfigFile, JSON.stringify(filtered, null, 2), { mode: 0o600 });
      } catch {
        core.error("ERROR: Failed to filter CLI-mounted servers from agent MCP config");
        core.info("Falling back to unfiltered config");
        try {
          fs.copyFileSync(outputPath, copilotConfigFile);
        } catch (err) {
          throw new Error(`Failed to copy file ${outputPath} to ${copilotConfigFile}: ${getErrorMessage(err)}`, { cause: err });
        }
      }
    } else {
      try {
        fs.copyFileSync(outputPath, copilotConfigFile);
      } catch (err) {
        throw new Error(`Failed to copy file ${outputPath} to ${copilotConfigFile}: ${getErrorMessage(err)}`, { cause: err });
      }
    }
    let copilotConfigContent;
    try {
      copilotConfigContent = fs.readFileSync(copilotConfigFile, "utf8");
    } catch (err) {
      throw new Error(`Failed to read file ${copilotConfigFile}: ${getErrorMessage(err)}`, { cause: err });
    }
    core.info(copilotConfigContent);
  }
  printTiming(configConvertStart, "Configuration conversion");
  core.info("");

  // -----------------------------------------------------------------------
  // Check MCP server functionality
  // -----------------------------------------------------------------------
  core.info("Checking MCP server functionality...");
  const mcpCheckStart = nowMs();
  const checkScript = path.join(runnerTemp || "", "gh-aw/actions/check_mcp_servers.sh");

  if (fs.existsSync(checkScript)) {
    core.info("Running MCP server checks...");
    // Pass agentId via MCP_GATEWAY_AGENT_ID env var (already set) rather than
    // as a shell argument to avoid shell metacharacter injection risks.
    const safePort = String(gatewayPort).replace(/[^0-9]/g, "");
    try {
      execFileSync("bash", [checkScript, outputPath, `http://localhost:${safePort}`, process.env.MCP_GATEWAY_AGENT_ID || ""], {
        stdio: "inherit",
        env: { ...process.env, GH_AW_MCP_OPTIONAL_SERVERS: optionalServerNames.join(",") },
        timeout: MCP_SERVER_CHECK_TIMEOUT_MS,
      });
    } catch {
      core.error("ERROR: MCP server checks failed - no servers could be connected");
      core.error("Gateway process will be terminated");
      try {
        process.kill(gatewayPid);
      } catch {
        // ignore
      }
      core.setFailed("ERROR: MCP server checks failed - no servers could be connected");
      return;
    }
    printTiming(mcpCheckStart, "MCP server connectivity checks");
  } else {
    core.info(`WARNING: MCP server check script not found at ${checkScript}`);
    core.info("Skipping MCP server functionality checks");
  }
  core.info("");

  // -----------------------------------------------------------------------
  // Save CLI manifest for mount_mcp_as_cli.cjs
  // -----------------------------------------------------------------------
  core.info("Saving MCP CLI manifest...");
  try {
    fs.mkdirSync(cliDir, { recursive: true });
  } catch (err) {
    throw new Error(`Failed to create directory ${cliDir}: ${getErrorMessage(err)}`, { cause: err });
  }

  try {
    const gwOut = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    if (gwOut.mcpServers && typeof gwOut.mcpServers === "object") {
      const allEntries = Object.entries(/** @type {Record<string, Record<string, unknown>>} */ gwOut.mcpServers);
      const servers = allEntries
        .filter(([name, v]) => {
          if (typeof v.url !== "string") {
            core.info(`  Skipping server '${name}' from manifest: missing url`);
            return false;
          }
          // Validate server name — only alphanumeric, hyphen, underscore
          if (!/^[a-zA-Z0-9_-]+$/.test(name) || name.length > 64) {
            core.info(`  Skipping server '${name}' from manifest: invalid name`);
            return false;
          }
          return true;
        })
        .map(([name, v]) => ({ name, url: v.url }));
      const manifest = JSON.stringify({ servers }, null, 2);
      fs.writeFileSync(path.join(cliDir, "manifest.json"), manifest, {
        mode: 0o600,
      });
      core.info(`CLI manifest saved with ${servers.length} server(s): ${servers.map(s => s.name).join(", ")}`);
    } else {
      core.info("WARNING: No mcpServers in gateway output, CLI manifest not created");
    }
  } catch {
    core.info("WARNING: No mcpServers in gateway output, CLI manifest not created");
  }
  core.info("");

  // -----------------------------------------------------------------------
  // Delete gateway configuration file
  // -----------------------------------------------------------------------
  core.info("Cleaning up gateway configuration file...");
  try {
    fs.unlinkSync(outputPath);
    core.info("Gateway configuration file deleted");
  } catch {
    core.info("Gateway configuration file not found (already deleted or never created)");
  }
  core.info("");

  // -----------------------------------------------------------------------
  // Summary
  // -----------------------------------------------------------------------
  core.info("MCP gateway is running:");
  core.info(`  - From host: http://localhost:${gatewayPort}`);
  core.info(`  - From containers: http://${gatewayDomain}:${gatewayPort}`);
  core.info(`Gateway PID: ${gatewayPid}`);

  printTiming(scriptStartTime, "Overall gateway startup");
  core.info("");

  // -----------------------------------------------------------------------
  // Write GitHub Actions step outputs
  // -----------------------------------------------------------------------
  if (githubOutput) {
    // The gateway authenticates with its agent ID; expose the same value under the
    // API-key output name expected by the AWF host handoff.
    const outputs = [`gateway-pid=${gatewayPid}`, `gateway-port=${gatewayPort}`, `gateway-agent-id=${agentId}`, `gateway-api-key=${agentId}`, `gateway-domain=${gatewayDomain}`].join("\n");
    try {
      fs.appendFileSync(githubOutput, outputs + "\n");
    } catch {
      /* ignore */
    }
  }
}

if (require.main === module) {
  main().catch(err => {
    stopGatewayProcess(activeGatewayPid);
    activeGatewayPid = null;
    const message = getErrorMessage(err);
    const stack = err instanceof Error ? err.stack : undefined;
    if (stack) core.error(stack);
    core.setFailed(`FATAL: ${message}`);
  });
}

module.exports = {
  applyOTLPIgnoreIfMissing,
  clearGatewayStartupMarker,
  createGatewayStderrPath,
  customGatewayEnvNamesVar,
  customGatewayEnvTransportPrefix,
  customGatewayReservedEnvPrefix,
  detectEngineType,
  extractOptionalServerNames,
  getOTLPIfMissingMode,
  hasNonEmptyOTLPHeaders,
  isOTLPIfMissingIgnore,
  getJSONParseErrorContext,
  getGatewayStartupMarkerPath,
  injectCustomGatewayEnvArgs,
  computeHealthRetryBudgetMs,
  MCP_GATEWAY_HEALTH_MAX_ATTEMPTS_LIMIT,
  MCP_GATEWAY_HEALTH_RETRY_DEFAULTS,
  MCP_GATEWAY_HEALTH_RETRY_ENV,
  MCP_GATEWAY_HEALTH_REGISTRATION_MARGIN_MS,
  MCP_GATEWAY_BACKEND_STARTUP_TIMEOUT_DEFAULT_MS,
  resolveGatewayHealthRetryConfig,
  normalizeSinkVisibilityEncoding,
  redactGatewayDiagnostics,
  resolveCopilotConfigPaths,
  validateGatewayAgentIdentifiers,
  writeGatewayStartupMarker,
};
