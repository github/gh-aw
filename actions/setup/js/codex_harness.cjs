// @ts-check

/**
 * OpenAI Codex CLI Harness with Retry Logic
 *
 * Wraps the OpenAI Codex CLI command with retry logic for failures that occur after the
 * session has been partially executed.  Passes all arguments to the codex subprocess,
 * forwarding stdout/stderr; stdin is closed since the prompt is delivered via
 * --prompt-file, not stdin.
 *
 * Retry policy:
 *   - If the process produced any output (hasOutput) and exits with a non-zero code, the
 *     session is considered partially executed.  The driver retries with a fresh run
 *     because Codex does not support a --continue-style session resumption.
 *   - Rate-limit errors (HTTP 429 / "rate_limit_exceeded") and server errors (HTTP 500,
 *     503) are well-known transient failure modes and are logged explicitly, but
 *     any partial-execution failure is retried — not just those specific errors.
 *   - If the process produced no output (failed to start / auth error before any work), the
 *     driver does not retry because there is nothing to resume.
 *   - Retries use exponential backoff: 5s → 10s → 20s (capped at 60s) by default.
 *   - Maximum 3 retry attempts after the initial run by default.
 *   - Override via GH_AW_HARNESS_MAX_RETRIES, GH_AW_HARNESS_INITIAL_DELAY_MS,
 *     GH_AW_HARNESS_BACKOFF_MULTIPLIER, GH_AW_HARNESS_MAX_DELAY_MS.
 *
 * Prompt handling:
 *   - The harness expects a `--prompt-file <path>` argument in the args list.
 *   - It reads the file and appends the content as the last positional argument, which is
 *     where the Codex CLI (`codex exec`) expects the prompt.
 *   - The `--prompt-file` flag is a harness-only argument and is not forwarded to codex.
 *
 * Usage: node codex_harness.cjs <command> [args...]
 * Example: node codex_harness.cjs codex exec --dangerously-bypass-approvals-and-sandbox --skip-git-repo-check --prompt-file /tmp/gh-aw/aw-prompts/prompt.txt
 */

"use strict";

const { getErrorMessage } = require("./error_helpers.cjs");
const fs = require("fs");
const { runProcess, formatDuration, sleep, MIN_POST_RESULT_WATCHDOG_TIMEOUT_MS, DEFAULT_POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS, MAX_POST_RESULT_WATCHDOG_TIMEOUT_MS, resolvePostResultWatchdogIdleTimeoutMs } = require("./process_runner.cjs");
const { runHarnessRetryLoop, shouldSkipForNoopSafeOutputs, shouldStopForNoopSafeOutputs } = require("./harness_retry_runner.cjs");
const {
  AWF_API_PROXY_REFLECT_URL,
  AWF_REFLECT_OUTPUT_PATH,
  AWF_MODELS_URL_TIMEOUT_MS,
  GEMINI_MODEL_NAME_PREFIX,
  enrichReflectModels,
  extractModelIds,
  fetchAWFReflect,
  fetchModelsFromUrl,
  normalizeReflectProviderName,
  REFLECT_PROVIDER_ALIASES,
  resolveProviderEndpointFromReflect,
} = require("./awf_reflect.cjs");
const { emitInfrastructureIncomplete, emitMissingToolPermissionIssue, hasExpectedSafeOutputs, hasTerminalSafeOutput, hasNoopInSafeOutputs } = require("./safeoutputs_cli.cjs");
const { countPermissionDeniedIssues, hasNumerousPermissionDeniedIssues, extractDeniedCommands, buildMissingToolPermissionIssuePayload } = require("./permission_denied_helpers.cjs");
const { detectNonRetryableHarnessGuard, buildSoftTimeoutGuard, emitSoftTimeoutSignal, isAuthenticationFailedError, parseAICreditsExceededProxyRejection } = require("./harness_retry_guard.cjs");
const { MODEL_NOT_SUPPORTED_PATTERN: INVALID_MODEL_ERROR_PATTERN } = require("./detect_agent_errors.cjs");
const { resolveRetryConfig } = require("./harness_retry_config.cjs");
const { applyModelFallback, injectModelFlagAfterExec } = require("./model_fallback.cjs");
const { parseMaxAICreditsExceededFromAuditLog } = require("./ai_credits_context.cjs");
const { calculateWorkingSetFromJSONL } = require("./working_set_metrics.cjs");

// Pattern to detect OpenAI rate-limit errors.
// Matches the JSON error type field ("rate_limit_exceeded"), the HTTP status code
// ("429 Too Many Requests"), the client-side exception class ("RateLimitError"), and
// the human-readable message Codex emits inside "Reconnecting..." / error lines:
// "Rate limit reached for <model> in organization <org> on tokens per min (TPM): ..."
const RATE_LIMIT_ERROR_PATTERN = /rate_limit_exceeded|429 Too Many Requests|RateLimitError|Rate limit reached for [^\s]+(?: in organization [^\s]+)? on tokens per min/i;
const TOKEN_PER_MIN_RATE_LIMIT_PATTERN = /Rate limit reached for [^\s]+(?: in organization [^\s]+)? on tokens per min/i;

// Pattern to detect when Codex's internal stream-reconnect budget is fully spent.
// Codex emits "Reconnecting... N/N (reason)" where both numbers are the same when
// the reconnect is the last allowed attempt.  Seeing this pattern together with a
// rate-limit error means the session cannot make forward progress: every reconnect
// attempt immediately fails with the same rate-limit, and a fresh harness run will
// re-encounter the same limit since the same work pattern consumes the same TPM budget.
//
// The backreference \1 requires the two numeric parts of "N/N" to be identical —
// "5/5" matches (exhausted) but "1/5", "3/5", "4/5" do not (still retrying).
const RECONNECT_EXHAUSTED_PATTERN = /Reconnecting\.\.\.\s+(\d+)\/\1\b/;

// Pattern to detect a missing API key at startup — Codex emits this before making any API
// calls when neither CODEX_API_KEY nor OPENAI_API_KEY is available in the environment.
// Example: "ERROR: Missing environment variable: `OPENAI_API_KEY`"
const MISSING_API_KEY_PATTERN = /Missing environment variable:\s*`?(?:CODEX_API_KEY|OPENAI_API_KEY)\b`?/i;

// Pattern to detect OpenAI server-side errors (HTTP 500, 503).
// These are transient infrastructure failures that may resolve on retry.
const SERVER_ERROR_PATTERN = /InternalServerError|ServiceUnavailableError|500 Internal Server Error|503 Service Unavailable/i;

// Pattern to detect deterministic request-validation failures (HTTP 400 `invalid_request_error`)
// within Codex's outer `turn.failed` event. The provider rejects the serialized request itself
// (e.g. `"code": "empty_array"` on `messages[N].content`), so an identical fresh run produces
// an identical rejection: retrying only re-bills the turns that succeeded before the failure point.
const INVALID_REQUEST_ERROR_PATTERN = /invalid_request_error/i;

// Codex's `turn.failed` event nests the actual provider error as a JSON string inside
// `error.message` (sometimes doubly-nested, e.g. `error.message` -> `{"error": {...}}`).
// This is a specific, common form of "unsupported model" failure: the configured model does
// not support the `custom` tool type Codex uses for its `apply_patch`/freeform tool schema.
// The provider rejects the whole request before any work happens, surfacing as:
//   {"error": {"message": "Invalid value: 'custom'", "type": "invalid_request_error",
//              "param": "tools", "code": "unknown_parameter"}}
// This is a model-capability mismatch, not a malformed request, so it warrants a dedicated,
// more actionable message than the generic invalid_request_error handling below.

/**
 * Unwraps up to a few levels of Codex's nested provider error payload to find the
 * innermost object that carries string `param`/`code` fields.
 * @param {unknown} error
 * @returns {{ param?: string, code?: string } | null}
 */
function extractNestedProviderErrorDetails(error) {
  const candidates = [error];
  for (let visited = 0; visited < 8 && candidates.length > 0; visited++) {
    const current = candidates.shift();
    if (!current || typeof current !== "object") continue;
    /** @type {{ param?: unknown, code?: unknown, error?: unknown, message?: unknown, metadata?: unknown }} */
    const candidate = current;
    if (typeof candidate.param === "string" && typeof candidate.code === "string") {
      return { param: candidate.param, code: candidate.code };
    }
    if (candidate.error && typeof candidate.error === "object") candidates.push(candidate.error);
    if (typeof candidate.message === "string") {
      const parsed = parseJsonOrUndefined(candidate.message);
      if (parsed !== undefined) candidates.push(parsed);
    }
    if (candidate.metadata && typeof candidate.metadata === "object") {
      /** @type {{ raw?: unknown }} */
      const metadata = candidate.metadata;
      if (typeof metadata.raw === "string") {
        const parsed = parseJsonOrUndefined(metadata.raw);
        if (parsed !== undefined) candidates.push(parsed);
      }
    }
  }
  return null;
}

/**
 * @param {string} value
 * @returns {unknown}
 */
function parseJsonOrUndefined(value) {
  try {
    return JSON.parse(value);
  } catch {
    return undefined;
  }
}

// Post-result watchdog: once the agent writes a terminal safe-output the harness
// arms a watchdog timer and kills the Codex process if it is still running after
// POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS of inactivity.  This prevents the step from
// hitting its hard timeout when Codex hangs on exit after completing its work.
// Constants and resolvePostResultWatchdogIdleTimeoutMs are imported from process_runner.cjs.
const POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS = resolvePostResultWatchdogIdleTimeoutMs();

const TOKEN_USAGE_AUDIT_PATH = "/tmp/gh-aw/sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl";
const TOKEN_USAGE_AWF_AUDIT_PATH = "/tmp/gh-aw/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl";
const TOKEN_USAGE_PATH = "/tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl";
const TOKEN_USAGE_PATHS = [TOKEN_USAGE_AUDIT_PATH, TOKEN_USAGE_AWF_AUDIT_PATH, TOKEN_USAGE_PATH];

const DEFAULT_CONTEXT_REBUILD_FACTOR_LIMIT = 25;
const DEFAULT_CONTEXT_REBUILD_MIN_CUMULATIVE_INPUT_TOKENS = 1000000;
const DEFAULT_CONTEXT_REBUILD_POLL_INTERVAL_MS = 15000;
const DEFAULT_CONTEXT_REBUILD_TERM_GRACE_MS = 5000;

/**
 * Return the current byte size of the safe-outputs file, or 0 if the file does not
 * yet exist.  Used as a per-attempt baseline so the watchdog only arms on output
 * appended by the current attempt, not a record left by an earlier retry.
 * @param {string} safeOutputsPath
 * @returns {number}
 */
function getSafeOutputsByteOffset(safeOutputsPath) {
  try {
    return fs.statSync(safeOutputsPath).size;
  } catch {
    return 0;
  }
}

/**
 * Emit a timestamped diagnostic log line to stderr.
 * All driver messages are prefixed with "[codex-harness]" so they are easy to
 * grep out of the combined agent-stdio.log.
 * @param {string} message
 */
function log(message) {
  const ts = new Date().toISOString();
  process.stderr.write(`[codex-harness] ${ts} ${message}\n`);
}

/**
 * Determines if the collected output contains an OpenAI rate-limit error.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isRateLimitError(output) {
  return RATE_LIMIT_ERROR_PATTERN.test(output);
}

/**
 * Determines if the collected output indicates OpenAI token-per-minute exhaustion.
 * This limit is workload-dependent and immediate fresh-run retries can consume
 * additional budget without making progress.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isTokenPerMinuteRateLimitError(output) {
  return TOKEN_PER_MIN_RATE_LIMIT_PATTERN.test(output);
}

/**
 * Determines if the collected output indicates a missing API key at startup.
 * Codex exits before producing any agent output in this case, so retrying is futile.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isMissingApiKeyError(output) {
  return MISSING_API_KEY_PATTERN.test(output);
}

/**
 * Determines if the collected output contains an OpenAI server error.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isServerError(output) {
  return SERVER_ERROR_PATTERN.test(output);
}

/**
 * Determines if the collected output indicates an invalid or unavailable model name.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isInvalidModelError(output) {
  return INVALID_MODEL_ERROR_PATTERN.test(output);
}

/**
 * Determines if Codex emitted a `turn.failed` provider event containing a deterministic
 * request-validation failure (HTTP 400 `invalid_request_error`). Such schema-level rejections
 * can never succeed on a fresh run with the same input, so they are treated as terminal.
 * Tokens in agent transcripts and tool responses are intentionally ignored.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isInvalidRequestError(output) {
  return output.split(/\r?\n/).some(line => {
    try {
      const event = JSON.parse(line);
      return event?.type === "turn.failed" && event.error && INVALID_REQUEST_ERROR_PATTERN.test(JSON.stringify(event.error));
    } catch {
      return false;
    }
  });
}

/**
 * Determines if Codex emitted a `turn.failed` provider event indicating the configured model
 * does not support Codex's required `custom` tool-calling schema (the provider rejects the
 * `tools` request parameter with code `unknown_parameter`). This is a model-capability mismatch
 * — the model itself is valid but incompatible with Codex — so it is surfaced as a dedicated,
 * non-retryable condition with actionable guidance rather than the generic invalid-request message.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isUnsupportedModelToolsError(output) {
  return output.split(/\r?\n/).some(line => {
    try {
      const event = JSON.parse(line);
      if (event?.type !== "turn.failed" || !event.error) return false;
      const details = extractNestedProviderErrorDetails(event.error);
      return !!details && details.param === "tools" && details.code === "unknown_parameter";
    } catch {
      return false;
    }
  });
}

/**
 * Determines if the collected output shows that Codex's internal stream-reconnect
 * retries are exhausted (i.e., the output contains "Reconnecting... N/N" where both
 * numbers are the same, indicating the last reconnect attempt).
 *
 * When this is true together with a rate-limit error, retrying from scratch would
 * immediately encounter the same rate limit and drain the token budget further.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isReconnectExhaustedError(output) {
  return RECONNECT_EXHAUSTED_PATTERN.test(output);
}

/**
 * Resolve --prompt-file arguments for the Codex run.
 * Strips the --prompt-file <path> pair from args and appends the file content
 * as the last positional argument, which is where `codex exec` expects the prompt.
 *
 * @param {string[]} args
 * @returns {string[]} Args with --prompt-file resolved to inline prompt content
 */
function resolveCodexPromptFileArgs(args) {
  /** @type {string[]} */
  const filteredArgs = [];
  /** @type {string|null} */
  let promptContent = null;

  for (let i = 0; i < args.length; i++) {
    if (args[i] !== "--prompt-file") {
      filteredArgs.push(args[i]);
      continue;
    }

    if (i + 1 >= args.length) {
      log("warning: --prompt-file provided without a path; leaving arguments unchanged");
      filteredArgs.push(args[i]);
      continue;
    }

    const promptFile = args[i + 1];
    try {
      const stat = fs.statSync(promptFile);
      log(`resolved --prompt-file: path=${promptFile} size=${stat.size}B`);
      promptContent = fs.readFileSync(promptFile, "utf8");
    } catch (error) {
      const err = /** @type {Error} */ error;
      // An unreadable prompt file means no task instructions can be delivered to Codex.
      // Propagate as a fatal error rather than forwarding the harness-only flag to the
      // codex subprocess (which would fail with an "unknown option" error).
      throw new Error(`--prompt-file '${promptFile}' is not readable: ${err.message}`, { cause: err });
    }
    i++; // Skip the prompt-file path argument
  }

  // Append the prompt content as the last positional argument (codex exec convention).
  if (promptContent !== null) {
    filteredArgs.push(promptContent);
  }

  return filteredArgs;
}

/**
 * Inject `--json` after `exec` in the args list so that Codex streams structured
 * JSON Lines (JSONL) to stdout.  This enables machine-readable output for CI
 * pipelines without changing how stderr progress output works.
 *
 * No-op when the subcommand is not `exec` or when `--json` is already present.
 *
 * @param {string[]} args
 * @returns {string[]}
 */
function injectJsonFlag(args) {
  if (args.length === 0 || args[0] !== "exec") return args;
  if (args.includes("--json")) return args;
  return ["exec", "--json", ...args.slice(1)];
}

function getCodexModelEnvVar(env = process.env) {
  if ("GH_AW_MODEL_EVALS_CODEX" in env) {
    return "GH_AW_MODEL_EVALS_CODEX";
  }
  if ("GH_AW_MODEL_DETECTION_CODEX" in env) {
    return "GH_AW_MODEL_DETECTION_CODEX";
  }
  if ("GH_AW_MODEL_AGENT_CODEX" in env) {
    return "GH_AW_MODEL_AGENT_CODEX";
  }
  return "";
}

/**
 * Build child process environment for Codex execution.
 * Preserve API keys captured at harness startup, even if the parent environment
 * is sanitized later in the run.
 *
 * @param {NodeJS.ProcessEnv} baseEnv
 * @param {string|undefined} codexApiKey
 * @param {string|undefined} openaiApiKey
 * @returns {NodeJS.ProcessEnv}
 */
function buildCodexChildEnv(baseEnv, codexApiKey, openaiApiKey) {
  const childEnv = { ...baseEnv };
  if (codexApiKey) {
    childEnv.CODEX_API_KEY = codexApiKey;
  }
  if (openaiApiKey) {
    childEnv.OPENAI_API_KEY = openaiApiKey;
  }
  return childEnv;
}

/**
 * Extract numeric port from a URL string.
 * @param {string} urlString
 * @returns {number|null}
 */
function extractPortFromURL(urlString) {
  if (!urlString || typeof urlString !== "string") return null;
  try {
    const parsed = new URL(urlString);
    if (!parsed.port) return null;
    return Number(parsed.port);
  } catch {
    return null;
  }
}

/**
 * Read Codex openai-proxy base_url from config.toml content.
 * @param {string} tomlContent
 * @returns {string|null}
 */
function extractOpenAIProxyBaseURLFromToml(tomlContent) {
  if (!tomlContent || typeof tomlContent !== "string") return null;
  const providerSection = tomlContent.match(/\[model_providers\.openai-proxy\]([\s\S]*?)(?:\n\[|$)/);
  if (!providerSection) return null;
  const baseURLMatch = providerSection[1].match(/(?:^|\n)\s*base_url\s*=\s*"([^"]+)"/);
  return baseURLMatch && baseURLMatch[1] ? baseURLMatch[1].trim() : null;
}

/**
 * Determine a configured provider endpoint port from AWF /reflect payload.
 * @param {any} reflectData
 * @param {string} [provider]
 * @returns {number|null}
 */
function getConfiguredProviderPortFromReflect(reflectData, provider = "openai") {
  const resolved = resolveProviderEndpointFromReflect({ provider, reflectData, logger: () => {} });
  const normalizedProvider = normalizeReflectProviderName(provider, "openai");
  const normalizedEndpointProvider = normalizeReflectProviderName(resolved?.endpointProvider);
  const matchingProviders = REFLECT_PROVIDER_ALIASES[normalizedProvider] || new Set([normalizedProvider]);
  if (!matchingProviders.has(normalizedEndpointProvider)) return null;
  return resolved && resolved.port != null ? resolved.port : null;
}

/**
 * Determine whether AWF /reflect advertises at least one configured endpoint for the
 * selected provider. Used to distinguish "no endpoints configured at all" (best-effort,
 * cannot validate) from "endpoints configured but none for the selected provider"
 * (validation should fail strictly rather than silently pass).
 * @param {any} reflectData
 * @param {string} provider
 * @returns {boolean}
 */
function reflectHasMatchingProviderEndpoint(reflectData, provider) {
  const endpoints = Array.isArray(reflectData?.endpoints) ? reflectData.endpoints.filter(ep => ep && ep.configured === true) : [];
  if (endpoints.length === 0) return true;
  const normalizedProvider = normalizeReflectProviderName(provider, "openai");
  const matchingProviders = REFLECT_PROVIDER_ALIASES[normalizedProvider] || new Set([normalizedProvider]);
  return endpoints.some(ep => matchingProviders.has(normalizeReflectProviderName(ep.provider)));
}

/**
 * Validate that Codex openai-proxy base_url matches the configured provider endpoint from /reflect.
 * @param {{
 *   codexConfigPath: string,
 *   reflectPath: string,
 *   provider?: string,
 *   readFileSync?: (path: import("node:fs").PathOrFileDescriptor, options?: import("node:fs").ObjectEncodingOptions & { flag?: string } | BufferEncoding | null) => string
 * }} options
 * @returns {{ ok: boolean, reason?: string }}
 */
function validateCodexOpenAIBaseURLFromReflect(options) {
  const readFileSync = (options && options.readFileSync) || fs.readFileSync;
  const codexConfigPath = options && options.codexConfigPath;
  const reflectPath = options && options.reflectPath;
  const provider = normalizeReflectProviderName(options?.provider || "openai", "openai");
  if (!codexConfigPath || !reflectPath) return { ok: true };

  let tomlContent;
  let reflectContent;
  try {
    tomlContent = readFileSync(codexConfigPath, "utf8");
    reflectContent = readFileSync(reflectPath, "utf8");
  } catch {
    return { ok: true };
  }

  const baseURL = extractOpenAIProxyBaseURLFromToml(tomlContent);
  if (!baseURL) return { ok: true };
  const baseURLPort = extractPortFromURL(baseURL);
  if (baseURLPort == null) return { ok: true };

  let reflectData;
  try {
    reflectData = JSON.parse(reflectContent);
  } catch {
    return { ok: true };
  }
  if (!reflectHasMatchingProviderEndpoint(reflectData, provider)) {
    return {
      ok: false,
      reason: `Codex openai-proxy provider mismatch: /reflect has no configured endpoint for provider "${provider}"`,
    };
  }
  const providerPort = getConfiguredProviderPortFromReflect(reflectData, provider);
  if (providerPort == null) return { ok: true };
  if (providerPort !== baseURLPort) {
    return {
      ok: false,
      reason: `Codex openai-proxy base_url port mismatch: config.toml uses ${baseURLPort}, but /reflect reports ${provider} on port ${providerPort}`,
    };
  }
  return { ok: true };
}

/**
 * Configure Codex openai-proxy base_url from /reflect and return env overrides.
 *
 * @param {{ codexConfigPath?: string, reflectPath?: string, provider?: string }} options
 * @returns {{ env: NodeJS.ProcessEnv, configured: boolean }}
 */
function configureCodexProviderFromReflect(options) {
  const env = { ...process.env };
  const codexConfigPath = options && options.codexConfigPath;
  const reflectPath = (options && options.reflectPath) || AWF_REFLECT_OUTPUT_PATH;
  const provider = normalizeReflectProviderName(options?.provider || process.env.GH_AW_LLM_PROVIDER, "openai");
  if (!reflectPath) return { env, configured: false };
  try {
    const reflectContent = fs.readFileSync(reflectPath, "utf8");
    const reflectData = JSON.parse(reflectContent);
    const resolved = resolveProviderEndpointFromReflect({ provider, reflectData, logger: log });
    if (!resolved || !resolved.baseUrl) return { env, configured: false };
    env.OPENAI_BASE_URL = resolved.baseUrl;
    log(`configured OPENAI_BASE_URL from /reflect for provider=${provider}: ${resolved.baseUrl}`);
    if (codexConfigPath) {
      const tomlContent = fs.readFileSync(codexConfigPath, "utf8");
      const providerSectionPattern = /\[model_providers\.openai-proxy\][\s\S]*?(?:\n\[|$)/;
      const baseLine = `base_url = "${resolved.baseUrl}"`;
      if (providerSectionPattern.test(tomlContent)) {
        const rewritten = tomlContent.replace(providerSectionPattern, section => {
          if (/(?:^|\n)\s*base_url\s*=/.test(section)) {
            return section.replace(/(?:^|\n)\s*base_url\s*=\s*(?:"[^"]*"|'[^']*'|[^\n]+)/, `\n${baseLine}`);
          }
          return `${section.trimEnd()}\n${baseLine}\n`;
        });
        fs.writeFileSync(codexConfigPath, rewritten, "utf8");
      }
    }
    return { env, configured: true };
  } catch (error) {
    const err = /** @type {Error} */ error;
    log(`warning: unable to configure provider endpoint from /reflect: ${err.message}`);
    return { env, configured: false };
  }
}

/**
 * @param {NodeJS.ProcessEnv} [env]
 * @returns {{ enabled: boolean, maxRebuildFactor: number, minCumulativeInputTokens: number, pollIntervalMs: number, termGraceMs: number }}
 */
function resolveContextRebuildCircuitBreakerConfig(env = process.env) {
  const enabledValue = env.GH_AW_CODEX_CONTEXT_REBUILD_CIRCUIT_BREAKER;
  const enabled = enabledValue == null || !/^(0|false|off|no)$/i.test(String(enabledValue).trim());
  const maxRebuildFactorRaw = Number(env.GH_AW_CODEX_MAX_REBUILD_FACTOR);
  const minCumulativeInputTokensRaw = Number(env.GH_AW_CODEX_REBUILD_MIN_CUMULATIVE_INPUT_TOKENS);
  const pollIntervalRaw = Number(env.GH_AW_CODEX_REBUILD_GUARD_POLL_MS);
  const termGraceRaw = Number(env.GH_AW_CODEX_REBUILD_GUARD_TERM_GRACE_MS);
  return {
    enabled,
    // A rebuild factor of exactly 1 means "no rebuild at all", so it is accepted as the
    // most aggressive valid threshold; anything below 1 is not a reachable factor.
    maxRebuildFactor: Number.isFinite(maxRebuildFactorRaw) && maxRebuildFactorRaw >= 1 ? maxRebuildFactorRaw : DEFAULT_CONTEXT_REBUILD_FACTOR_LIMIT,
    minCumulativeInputTokens: Number.isFinite(minCumulativeInputTokensRaw) && Math.floor(minCumulativeInputTokensRaw) >= 1 ? Math.floor(minCumulativeInputTokensRaw) : DEFAULT_CONTEXT_REBUILD_MIN_CUMULATIVE_INPUT_TOKENS,
    pollIntervalMs: Number.isFinite(pollIntervalRaw) && pollIntervalRaw > 0 ? Math.max(1000, Math.floor(pollIntervalRaw)) : DEFAULT_CONTEXT_REBUILD_POLL_INTERVAL_MS,
    termGraceMs: Number.isFinite(termGraceRaw) && termGraceRaw > 0 ? Math.max(250, Math.floor(termGraceRaw)) : DEFAULT_CONTEXT_REBUILD_TERM_GRACE_MS,
  };
}

/**
 * Returns the working set from the most recently written candidate that yields usable
 * measurements. Candidates are ordered by modification time (newest first) so the breaker
 * tracks the active run rather than whichever path happens to be listed first, and
 * candidates that are missing, empty, or unparseable (`measurement_state` of
 * `"unavailable"`) are skipped so a stale or malformed file cannot silently disable it.
 * File access is asynchronous to avoid blocking the driver's event loop while polling.
 * @param {string[]} paths
 * @returns {Promise<ReturnType<typeof calculateWorkingSetFromJSONL>["workingSet"] | null>}
 */
async function readWorkingSetFromTokenUsage(paths = TOKEN_USAGE_PATHS) {
  /** @type {{ path: string, mtimeMs: number }[]} */
  const candidates = [];
  for (const candidate of paths) {
    if (!candidate) continue;
    try {
      const stat = await fs.promises.stat(candidate);
      if (!stat.isFile() || stat.size <= 0) continue;
      candidates.push({ path: candidate, mtimeMs: stat.mtimeMs });
    } catch {
      continue;
    }
  }
  candidates.sort((a, b) => b.mtimeMs - a.mtimeMs);
  for (const candidate of candidates) {
    try {
      const content = await fs.promises.readFile(candidate.path, "utf8");
      if (!content.trim()) continue;
      const workingSet = calculateWorkingSetFromJSONL(content).workingSet;
      if (!workingSet || workingSet.measurement_state === "unavailable") continue;
      return workingSet;
    } catch {
      continue;
    }
  }
  return null;
}

/**
 * @param {ReturnType<typeof calculateWorkingSetFromJSONL>["workingSet"] | null} workingSet
 * @param {{ maxRebuildFactor: number, minCumulativeInputTokens: number }} config
 * @returns {{ terminate: boolean, reason: string }}
 */
function evaluateContextRebuildCircuitBreaker(workingSet, config) {
  if (!workingSet || typeof workingSet !== "object") {
    return { terminate: false, reason: "" };
  }
  const rebuildFactor = typeof workingSet.rebuild_factor === "number" && Number.isFinite(workingSet.rebuild_factor) ? workingSet.rebuild_factor : null;
  const cumulativeInputTokens = Number.isFinite(workingSet.cumulative_input_tokens) ? Number(workingSet.cumulative_input_tokens) : 0;
  if (rebuildFactor == null || rebuildFactor < config.maxRebuildFactor) {
    return { terminate: false, reason: "" };
  }
  if (!Number.isFinite(cumulativeInputTokens) || cumulativeInputTokens < config.minCumulativeInputTokens) {
    return { terminate: false, reason: "" };
  }
  return {
    terminate: true,
    reason: `context-rebuild circuit breaker tripped: rebuild_factor=${rebuildFactor.toFixed(2)} cumulative_input_tokens=${Math.round(cumulativeInputTokens)} thresholds=${config.maxRebuildFactor}/${config.minCumulativeInputTokens}`,
  };
}

/**
 * Evaluate the context-rebuild circuit breaker for a single Codex attempt. Once the
 * agent has emitted a terminal safe-output for this attempt, the breaker must not
 * preempt it with a synthetic report_incomplete; the post-result watchdog handles
 * any slow Codex shutdown separately.
 * @param {ReturnType<typeof calculateWorkingSetFromJSONL>["workingSet"] | null} workingSet
 * @param {{ maxRebuildFactor: number, minCumulativeInputTokens: number }} config
 * @param {{ safeOutputsPath?: string, safeOutputsByteOffset?: number, logger?: (msg: string) => void }=} options
 * @returns {{ terminate: boolean, reason: string }}
 */
function evaluateContextRebuildCircuitBreakerForAttempt(workingSet, config, options) {
  const decision = evaluateContextRebuildCircuitBreaker(workingSet, config);
  if (!decision.terminate) return decision;

  const safeOutputsPath = options && typeof options.safeOutputsPath === "string" ? options.safeOutputsPath : "";
  const safeOutputsByteOffset = options && Number.isFinite(options.safeOutputsByteOffset) ? Number(options.safeOutputsByteOffset) : 0;
  const logger = options && options.logger ? options.logger : () => {};
  if (safeOutputsPath && hasTerminalSafeOutput(safeOutputsPath, { byteOffset: safeOutputsByteOffset, includeMissingData: true, includeReportIncomplete: true, logger })) {
    logger(`context-rebuild circuit breaker threshold exceeded after terminal safe-output was emitted — allowing Codex to exit normally`);
    return { terminate: false, reason: "" };
  }

  return decision;
}

/**
 * Main entry point: run codex with retry logic for transient API failures.
 * Codex does not support --continue session resumption, so all retries are fresh runs.
 */
async function main() {
  const [, , command, ...args] = process.argv;

  if (!command) {
    process.stderr.write("codex-harness: Usage: node codex_harness.cjs <command> [args...]\n");
    process.exit(1);
  }

  const { maxRetries: MAX_RETRIES, initialDelayMs: INITIAL_DELAY_MS, backoffMultiplier: BACKOFF_MULTIPLIER, maxDelayMs: MAX_DELAY_MS } = resolveRetryConfig(process.env, log);
  log(`starting: command=${command} maxRetries=${MAX_RETRIES} initialDelayMs=${INITIAL_DELAY_MS}` + ` backoffMultiplier=${BACKOFF_MULTIPLIER} maxDelayMs=${MAX_DELAY_MS}` + ` nodeVersion=${process.version} platform=${process.platform}`);

  // Pre-flight: skip the agent entirely when a noop has already been written by a prior step.
  // A noop indicates the work is complete or there is nothing to do — starting the agent
  // would be wasteful and potentially harmful.  This check runs before API key validation so
  // that a noop can be honoured even when credentials are absent.
  const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS || "";
  if (shouldSkipForNoopSafeOutputs({ safeOutputsPath, hasNoopInSafeOutputs, log })) {
    process.exit(0);
  }

  // Diagnose API key presence so CI failures can be triaged without exposing secret values.
  const codexApiKey = process.env.CODEX_API_KEY;
  const openaiApiKey = process.env.OPENAI_API_KEY;
  const codexChildEnv = buildCodexChildEnv(process.env, codexApiKey, openaiApiKey);
  log(`secrets: CODEX_API_KEY=${codexApiKey ? `set (length=${codexApiKey.length})` : "not set"}` + ` OPENAI_API_KEY=${openaiApiKey ? `set (length=${openaiApiKey.length})` : "not set"}`);

  // Pre-flight: require at least one API key before spawning codex.
  // Without a key, codex exits immediately with "Missing environment variable" and every
  // retry attempt fails the same way. Failing here avoids burning the retry budget and
  // surfaces a clear, actionable message in CI logs.
  if (!codexApiKey && !openaiApiKey) {
    log("fatal: no API key available - set CODEX_API_KEY or OPENAI_API_KEY and retry");
    process.exit(1);
  }

  // Resolve the prompt for the initial run (reads --prompt-file content).
  // A missing or unreadable prompt file is treated as a fatal startup error.
  let resolvedArgs;
  try {
    resolvedArgs = resolveCodexPromptFileArgs(args);
  } catch (err) {
    const e = /** @type {Error} */ err;
    log(`fatal: ${e.message}`);
    process.exit(1);
  }

  const codexModelEnvVar = getCodexModelEnvVar(process.env);
  const resolvedModel = codexModelEnvVar ? applyModelFallback(process.env, codexModelEnvVar, log) : "";
  resolvedArgs = injectModelFlagAfterExec(resolvedArgs, resolvedModel);

  // Safe arg list for logging: when --prompt-file was present, the last element of
  // resolvedArgs is the resolved prompt content. Replace it with a placeholder so that
  // task instructions are never written to stderr or captured in agent logs.
  const hadPromptFile = args.includes("--prompt-file");
  const safeArgs = hadPromptFile && resolvedArgs.length > 0 ? [...resolvedArgs.slice(0, -1), "<prompt omitted>"] : resolvedArgs;

  // Inject --json after `exec` to stream structured JSONL events to stdout, making
  // Codex output machine-readable in CI without affecting the stderr progress stream.
  resolvedArgs = injectJsonFlag(resolvedArgs);

  // Fetch AWF API proxy reflection data before running the agent to capture initial proxy state.
  // This is best-effort: failures are logged but do not affect the agent run.
  // Skip when AWF_REFLECT_ENABLED is not "1" (e.g. no api-proxy running in sandbox or test mode).
  if (process.env.AWF_REFLECT_ENABLED === "1") {
    await fetchAWFReflect({ logger: log });
  }
  const codexHome = process.env.CODEX_HOME || "";
  let codexEnv = codexChildEnv;
  const providerConfig = configureCodexProviderFromReflect({
    codexConfigPath: codexHome ? `${codexHome}/config.toml` : "",
    reflectPath: AWF_REFLECT_OUTPUT_PATH,
    provider: process.env.GH_AW_LLM_PROVIDER || "openai",
  });
  if (providerConfig.configured) {
    codexEnv = { ...codexEnv, ...providerConfig.env };
  }
  if (codexHome) {
    const validation = validateCodexOpenAIBaseURLFromReflect({
      codexConfigPath: `${codexHome}/config.toml`,
      reflectPath: AWF_REFLECT_OUTPUT_PATH,
      provider: process.env.GH_AW_LLM_PROVIDER || "openai",
    });
    if (!validation.ok) {
      log(`fatal: ${validation.reason}`);
      process.exit(1);
    }
  }

  let lastExitCode = 1;
  const driverStartTime = Date.now();
  // Soft-timeout guard: polled at the top of the retry loop and after each backoff sleep.
  // It does not preempt a running attempt — if a single invocation runs past the soft
  // deadline the guard fires on the next iteration. Individual attempts are expected to
  // complete within the SOFT_TIMEOUT_BUFFER_MS window.
  const softTimeoutGuard = buildSoftTimeoutGuard(driverStartTime);
  const contextRebuildCircuitBreaker = resolveContextRebuildCircuitBreakerConfig(process.env);
  log(
    `context-rebuild circuit breaker: enabled=${contextRebuildCircuitBreaker.enabled}` +
      ` maxRebuildFactor=${contextRebuildCircuitBreaker.maxRebuildFactor}` +
      ` minCumulativeInputTokens=${contextRebuildCircuitBreaker.minCumulativeInputTokens}` +
      ` pollIntervalMs=${contextRebuildCircuitBreaker.pollIntervalMs}`
  );
  const retryRun = await runHarnessRetryLoop({
    maxRetries: MAX_RETRIES,
    initialDelayMs: INITIAL_DELAY_MS,
    backoffMultiplier: BACKOFF_MULTIPLIER,
    maxDelayMs: MAX_DELAY_MS,
    driverStartTime,
    harnessName: "Codex harness",
    log,
    softTimeoutGuard,
    getRetryMode: () => "fresh run",
    runAttempt: async attempt => {
      // Track the file size before this attempt so the watchdog only arms on output
      // written by this attempt, not by a previous retry.
      const safeOutputsByteOffset = safeOutputsPath ? getSafeOutputsByteOffset(safeOutputsPath) : 0;

      const result = await runProcess({
        command,
        args: resolvedArgs,
        attempt,
        log,
        logArgs: safeArgs,
        env: codexEnv,
        runtimeGuard: contextRebuildCircuitBreaker.enabled
          ? {
              pollIntervalMs: contextRebuildCircuitBreaker.pollIntervalMs,
              termGraceMs: contextRebuildCircuitBreaker.termGraceMs,
              shouldTerminate: async () =>
                evaluateContextRebuildCircuitBreakerForAttempt(
                  await readWorkingSetFromTokenUsage(TOKEN_USAGE_PATHS),
                  {
                    maxRebuildFactor: contextRebuildCircuitBreaker.maxRebuildFactor,
                    minCumulativeInputTokens: contextRebuildCircuitBreaker.minCumulativeInputTokens,
                  },
                  { safeOutputsPath, safeOutputsByteOffset, logger: log }
                ),
            }
          : undefined,
        postResultWatchdog: safeOutputsPath
          ? {
              shouldArm: () =>
                hasTerminalSafeOutput(safeOutputsPath, {
                  byteOffset: safeOutputsByteOffset,
                  includeMissingData: true,
                  includeReportIncomplete: true,
                  logger: log,
                }),
              inactivityTimeoutMs: POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS,
            }
          : undefined,
      });
      // A guard-terminated run must never be reported as a success: Codex may handle SIGTERM
      // and exit cleanly, and `runHarnessRetryLoop` short-circuits on exitCode 0 before
      // `handleFailure` runs. Normalize the exit code so the failure handler always sees it.
      if (result.runtimeGuardFired && result.exitCode === 0) {
        log(`attempt ${attempt + 1}: runtime guard fired but process exited 0 — normalizing exit code to 1`);
        return { ...result, exitCode: 1, safeOutputsByteOffset };
      }
      return { ...result, safeOutputsByteOffset };
    },
    handleFailure: ({ attempt, result }) => {
      if (result.runtimeGuardFired) {
        const details = result.runtimeGuardReason || "Codex runtime guard terminated the run after context rebuild thresholds were exceeded.";
        emitInfrastructureIncomplete(details, { logger: log });
        log(`attempt ${attempt + 1}: ${details} — not retrying (circuit breaker)`);
        return { action: "stop" };
      }

      // When the post-result watchdog fired (SIGTERM sent to a hanging Codex process) and the
      // safe-outputs file contains a terminal result written during this attempt, treat the run
      // as a success.  The agent completed its work and wrote its output — the hang on exit is
      // a cosmetic failure, not a task failure.  Check this before logging "attempt failed" so
      // the log stream does not contradict itself for what is ultimately a successful run.
      if (
        result.watchdogFired &&
        safeOutputsPath &&
        hasTerminalSafeOutput(safeOutputsPath, {
          byteOffset: result.safeOutputsByteOffset ?? 0,
          includeMissingData: true,
          includeReportIncomplete: true,
          logger: log,
        })
      ) {
        log(`attempt ${attempt + 1}: post-result watchdog fired after terminal safe-output was emitted — treating as success (late-activity exit suppressed)`);
        return { action: "stop", exitCode: 0 };
      }

      const isRateLimit = isRateLimitError(result.output);
      const isTokenPerMinuteRateLimit = isTokenPerMinuteRateLimitError(result.output);
      const isAuthenticationFailed = isAuthenticationFailedError(result.output);
      const isMissingApiKey = isMissingApiKeyError(result.output);
      const isServer = isServerError(result.output);
      const isInvalidModel = isInvalidModelError(result.output);
      const isUnsupportedModelTools = isUnsupportedModelToolsError(result.output);
      const isInvalidRequest = isInvalidRequestError(result.output);
      const permissionDeniedCount = countPermissionDeniedIssues(result.output);
      const hasNumerousPermissionDenied = hasNumerousPermissionDeniedIssues(result.output);
      log(
        `attempt ${attempt + 1} failed:` +
          ` exitCode=${result.exitCode}` +
          ` watchdogFired=${result.watchdogFired}` +
          ` runtimeGuardFired=${result.runtimeGuardFired}` +
          ` isRateLimitError=${isRateLimit}` +
          ` isTokenPerMinuteRateLimitError=${isTokenPerMinuteRateLimit}` +
          ` isAuthenticationFailedError=${isAuthenticationFailed}` +
          ` isMissingApiKeyError=${isMissingApiKey}` +
          ` isServerError=${isServer}` +
          ` isInvalidModelError=${isInvalidModel}` +
          ` isUnsupportedModelToolsError=${isUnsupportedModelTools}` +
          ` isInvalidRequestError=${isInvalidRequest}` +
          ` permissionDeniedCount=${permissionDeniedCount}` +
          ` hasNumerousPermissionDenied=${hasNumerousPermissionDenied}` +
          ` hasOutput=${result.hasOutput}` +
          ` retriesRemaining=${MAX_RETRIES - attempt}`
      );

      if (shouldStopForNoopSafeOutputs({ attempt, safeOutputsPath, hasNoopInSafeOutputs, log })) {
        return { action: "stop", exitCode: 0 };
      }

      const nonRetryableGuard = detectNonRetryableHarnessGuard(result.output);
      const proxyAICreditsRejection = parseAICreditsExceededProxyRejection(result.output);
      if (proxyAICreditsRejection) {
        log(`attempt ${attempt + 1}: AWF API proxy rejected the request with HTTP 403 max-AI-credits (${proxyAICreditsRejection.aiCredits}/${proxyAICreditsRejection.maxAICredits}) — trusted budget-abort evidence`);
      }
      const trustedAICreditsExceeded = nonRetryableGuard.aiCreditsExceeded && (!!proxyAICreditsRejection || parseMaxAICreditsExceededFromAuditLog());
      if (nonRetryableGuard.aiCreditsExceeded && !trustedAICreditsExceeded) {
        log(`attempt ${attempt + 1}: AI credits marker found in CLI output without trusted firewall audit confirmation — preserving normal failure handling`);
      }
      // Some CLIs surface the proxy's budget rejection as an authentication failure (e.g. Claude Code
      // reports `error: authentication_failed` for "403 Maximum AI credits exceeded"). When the trusted
      // proxy signature is present that veto must not mask intentional budget enforcement.
      const shouldTreatAICreditsExceededAsSuccess = trustedAICreditsExceeded && (!isAuthenticationFailed || !!proxyAICreditsRejection) && !isMissingApiKey;
      if (shouldTreatAICreditsExceededAsSuccess || nonRetryableGuard.awfAPIProxyBlockingRequests || nonRetryableGuard.goalAlreadyActive || nonRetryableGuard.maxRunsExceeded) {
        const reasons = [];
        if (shouldTreatAICreditsExceededAsSuccess) reasons.push("AI credits budget exceeded");
        if (nonRetryableGuard.awfAPIProxyBlockingRequests) reasons.push("AWF API proxy is blocking requests");
        if (nonRetryableGuard.goalAlreadyActive) reasons.push("goal is already active for this thread (use update_goal when the current goal is complete)");
        if (nonRetryableGuard.maxRunsExceeded) reasons.push("maximum LLM invocations exceeded");
        log(`attempt ${attempt + 1}: ${reasons.join(" and ")} — not retrying (non-retryable guard condition)`);
        if (shouldTreatAICreditsExceededAsSuccess) {
          log(`attempt ${attempt + 1}: AI credits budget enforced — exiting 0 (budget control, not an error)`);
          return { action: "stop", exitCode: 0 };
        }
        if (nonRetryableGuard.maxRunsExceeded && safeOutputsPath && hasExpectedSafeOutputs(safeOutputsPath, { logger: log })) {
          log(`attempt ${attempt + 1}: invocation cap saturated but safe-outputs already contain expected output — suppressing terminal verdict (false-red: core work succeeded)`);
          return { action: "stop", exitCode: 0 };
        }
        return { action: "stop" };
      }

      if (attempt === 0 && isAuthenticationFailed) {
        log(`attempt ${attempt + 1}: authentication failed — not retrying (first-attempt auth failure is non-retryable)`);
        return { action: "stop" };
      }

      if (isMissingApiKey) {
        log(`attempt ${attempt + 1}: missing API key — not retrying (configure CODEX_API_KEY or OPENAI_API_KEY)`);
        return { action: "stop" };
      }

      if (isInvalidModel) {
        log(`attempt ${attempt + 1}: invalid/unsupported model configuration — not retrying (specify a valid engine model name in workflow frontmatter)`);
        return { action: "stop" };
      }

      if (isUnsupportedModelTools) {
        log(
          `attempt ${attempt + 1}: configured model does not support Codex's required tool-calling schema` +
            ` ("tools" param rejected with code "unknown_parameter") — not retrying` +
            ` (pick a model documented as compatible with Codex CLI, or remove the \`model:\` override in workflow frontmatter to use the engine default)`
        );
        return { action: "stop" };
      }

      if (isInvalidRequest) {
        log(`attempt ${attempt + 1}: invalid_request_error (HTTP 400) — not retrying (the provider rejected the request payload; an identical fresh run would fail the same way)`);
        return { action: "stop" };
      }

      if (hasNumerousPermissionDenied) {
        if (safeOutputsPath && hasExpectedSafeOutputs(safeOutputsPath, { logger: log })) {
          log(`attempt ${attempt + 1}: detected numerous permission-denied issues but safe-outputs already contain expected output — suppressing terminal verdict (false-red: core work succeeded)`);
          return { action: "stop", exitCode: 0 };
        }
        const deniedCommands = extractDeniedCommands(result.output);
        emitMissingToolPermissionIssue({ deniedCommands, logger: log });
        log(`attempt ${attempt + 1}: detected numerous permission-denied issues — not retrying (classified as missing tool/permission issue)`);
        return { action: "stop" };
      }

      if (isTokenPerMinuteRateLimit) {
        log(`attempt ${attempt + 1}: token-per-minute rate limit detected — not retrying (fresh runs can further drain token budget)`);
        return { action: "stop" };
      }

      if (isRateLimit && isReconnectExhaustedError(result.output)) {
        log(`attempt ${attempt + 1}: rate-limit with exhausted reconnects — not retrying (fresh run would hit the same rate limit)`);
        return { action: "stop" };
      }

      const isTransient = isRateLimit || isServer;
      if (attempt < MAX_RETRIES && (result.hasOutput || isTransient)) {
        const reason = isRateLimit ? "rate_limit_exceeded (transient)" : isServer ? "server_error (transient)" : "partial execution";
        log(`attempt ${attempt + 1}: ${reason} — will retry as fresh run (attempt ${attempt + 2}/${MAX_RETRIES + 1})`);
        return { action: "retry" };
      }

      if (attempt >= MAX_RETRIES) {
        log(`all ${MAX_RETRIES} retries exhausted — giving up (exitCode=${result.exitCode})`);
      } else {
        log(`attempt ${attempt + 1}: no output produced — not retrying` + ` (possible causes: binary not found, permission denied, auth failure, or silent startup crash)`);
      }

      return { action: "stop" };
    },
  });
  lastExitCode = retryRun.exitCode;

  // Fetch AWF API proxy reflection data and persist to disk for post-run step summary.
  // Skip when AWF_REFLECT_ENABLED is not "1" (e.g. no api-proxy running in sandbox or test mode).
  if (process.env.AWF_REFLECT_ENABLED === "1") {
    await fetchAWFReflect({ logger: log });
  }

  log(`done: exitCode=${lastExitCode} totalDuration=${formatDuration(Date.now() - driverStartTime)}`);
  process.exit(lastExitCode);
}

if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    resolveCodexPromptFileArgs,
    injectJsonFlag,
    isRateLimitError,
    isTokenPerMinuteRateLimitError,
    isAuthenticationFailedError,
    isMissingApiKeyError,
    isServerError,
    isInvalidModelError,
    isUnsupportedModelToolsError,
    isInvalidRequestError,
    isReconnectExhaustedError,
    countPermissionDeniedIssues,
    hasNumerousPermissionDeniedIssues,
    extractDeniedCommands,
    buildMissingToolPermissionIssuePayload,
    emitMissingToolPermissionIssue,
    buildCodexChildEnv,
    extractPortFromURL,
    extractOpenAIProxyBaseURLFromToml,
    getConfiguredProviderPortFromReflect,
    validateCodexOpenAIBaseURLFromReflect,
    configureCodexProviderFromReflect,
    resolveContextRebuildCircuitBreakerConfig,
    readWorkingSetFromTokenUsage,
    evaluateContextRebuildCircuitBreaker,
    evaluateContextRebuildCircuitBreakerForAttempt,
    TOKEN_USAGE_PATHS,
    DEFAULT_CONTEXT_REBUILD_FACTOR_LIMIT,
    DEFAULT_CONTEXT_REBUILD_MIN_CUMULATIVE_INPUT_TOKENS,
    DEFAULT_CONTEXT_REBUILD_POLL_INTERVAL_MS,
    DEFAULT_CONTEXT_REBUILD_TERM_GRACE_MS,
    hasNoopInSafeOutputs,
    hasExpectedSafeOutputs,
    resolveRetryConfig,
    applyModelFallback,
    injectModelFlagAfterExec,
    getCodexModelEnvVar,
    resolvePostResultWatchdogIdleTimeoutMs,
    POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS,
    DEFAULT_POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS,
    MIN_POST_RESULT_WATCHDOG_TIMEOUT_MS,
    MAX_POST_RESULT_WATCHDOG_TIMEOUT_MS,
  };
}

if (require.main === module) {
  main().catch(err => {
    log(`unexpected error: ${getErrorMessage(err)}`);
    process.exit(1);
  });
}
