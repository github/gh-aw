// @ts-check

/**
 * Copilot Harness with Retry Logic
 *
 * Wraps the Copilot CLI command (or @github/copilot-sdk session in SDK mode) with retry logic
 * for failures that occur after the session has been partially executed.  Passes all arguments
 * to the copilot subprocess, forwarding stdout/stderr; stdin is closed since the prompt is
 * delivered via CLI argument, not stdin.
 *
 * Retry policy (shared by CLI and SDK modes):
 *   - If the process produced any output (hasOutput) and exits with a non-zero code, the
 *     session is considered partially executed and is retried.
 *     - CLI mode: retries with --continue so the Copilot CLI can continue from on-disk state.
 *     - SDK mode: retries always restart the session fresh (--continue is a CLI concept).
 *   - CAPIError 400 is a well-known transient failure mode and is logged explicitly, but
 *     any partial-execution failure is retried — not just CAPIError 400.
 *   - If the process produced no output (failed to start / auth error before any work), the
 *     driver does not retry because there is nothing to resume.
 *   - "No authentication information found" errors are handled differently depending on context:
 *     - On a `--continue` attempt: the Copilot CLI's on-disk session credential written by the
 *       interrupted run may be incomplete/invalid.  The driver falls back to a single fresh run
 *       (without `--continue`) so env-var auth can succeed.  Mid-stream context is lost but the
 *       job has a recovery path.
 *     - On a fresh run (attempt 0 or after a `--continue`-auth fallback): the env-var token is
 *       genuinely absent or invalid.  All further retries will produce the same failure, so the
 *       driver bails immediately.
 *   - Null-type tool_call errors (400 "Invalid type for '...tool_calls[N].type': ... got null")
 *     poison the conversation history.  Retrying with `--continue` re-injects the same broken
 *     state on every subsequent attempt.  The driver restarts fresh to discard the poisoned
 *     history and permanently disables `--continue` for the remainder of the run so the corrupt
 *     state can never be reloaded.  Once `--continue` is disabled this way it is not re-enabled
 *     even if later retries produce output.
 *   - Exit codes that indicate the CLI subprocess was killed by a fatal OS-level signal
 *     (SIGILL/SIGABRT/SIGBUS/SIGFPE/SIGSEGV/SIGSYS — see harness_crash_signals.cjs) are treated
 *     like the null-type tool_call case: `--continue` is permanently disabled and the next retry
 *     starts a fresh session, since resuming the exact on-disk session risks immediately
 *     reproducing the same crash.
 *   - Retries use exponential backoff: 5s → 10s → 20s (capped at 60s) by default.
 *   - Maximum 3 retry attempts after the initial run by default.
 *
 * Usage: node copilot_harness.cjs <command> [args...]
 * Example: node copilot_harness.cjs copilot --add-dir /tmp/ --prompt-file /tmp/gh-aw/aw-prompts/prompt.txt
 */

"use strict";

require("./shim.cjs");

const { getErrorMessage } = require("./error_helpers.cjs");
const { maskSecret } = require("./actions_secret_masking.cjs");
const fs = require("fs");
const crypto = require("crypto");
const { getPromptPath, renderTemplateFromFile } = require("./messages_core.cjs");
const {
  runProcess,
  formatDuration,
  sleep,
  isCopilotSDKEnabled,
  buildCopilotSDKEnv,
  MIN_POST_RESULT_WATCHDOG_TIMEOUT_MS,
  DEFAULT_POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS,
  resolvePostResultWatchdogIdleTimeoutMs,
} = require("./process_runner.cjs");
const { buildCopilotSDKServerArgs, getCopilotSDKServerPort, startCopilotSDKServer, stopCopilotSDKServer, waitForCopilotSDKServer } = require("./copilot_sdk_sidecar.cjs");
const { resolveRetryConfig: resolveSharedRetryConfig } = require("./harness_retry_config.cjs");
const { runHarnessRetryLoop, shouldSkipForNoopSafeOutputs, shouldStopForNoopSafeOutputs } = require("./harness_retry_runner.cjs");
const {
  AWF_API_PROXY_REFLECT_URL,
  AWF_REFLECT_OUTPUT_PATH,
  AWF_REFLECT_TIMEOUT_MS,
  AWF_MODELS_URL_TIMEOUT_MS,
  GEMINI_MODEL_NAME_PREFIX,
  AWF_PROVIDER_LISTENER_READY_TIMEOUT_MS,
  waitForProviderListenerReady,
  enrichReflectModels,
  extractModelIds,
  fetchAWFReflect,
  fetchModelsFromUrl,
  inferProviderTypeForModel,
  resolveMultiProviderFromReflect,
} = require("./awf_reflect.cjs");
const { runSafeOutputsCLI, buildMissingToolAlternatives, emitMissingToolPermissionIssue, emitInfrastructureIncomplete, hasExpectedSafeOutputs, hasTerminalSafeOutput, hasNoopInSafeOutputs } = require("./safeoutputs_cli.cjs");
const { countPermissionDeniedIssues, hasNumerousPermissionDeniedIssues, extractDeniedCommands, buildMissingToolPermissionIssuePayload } = require("./permission_denied_helpers.cjs");
const { detectNonRetryableHarnessGuard, buildSoftTimeoutGuard, emitSoftTimeoutSignal, isAuthenticationFailedError: isCommonAuthenticationFailedError, parseAICreditsExceededProxyRejection } = require("./harness_retry_guard.cjs");
const { isCrashSignalExitCode, crashSignalNameForExitCode } = require("./harness_crash_signals.cjs");
const { isCAPIQuotaExceededError } = require("./detect_agent_errors.cjs");
const { applyModelFallback } = require("./model_fallback.cjs");
const { loadModelsJson } = require("./model_costs.cjs");
const { resolveConfiguredCopilotModel, ModelAliasResolutionError } = require("./resolve_model_alias.cjs");
const { parseMaxAICreditsExceededFromAuditLog } = require("./ai_credits_context.cjs");

const AWF_CONFIG_PATH = process.env.GH_AW_AWF_CONFIG_PATH || "/tmp/gh-aw/awf-config.json";

// Additional startup retry budget for scheduled and push-triggered runs when Copilot exits with
// code 2 before producing any output (typically transient API interruption at startup).
// Push-triggered runs share the same transient startup-failure mode as scheduled runs; the
// retry budget is equally useful there to avoid a deterministic red on push/main.
const MAX_SCHEDULED_EXIT2_RETRIES = 1;
// If prompt files are larger than this threshold, avoid inlining into argv.
const PROMPT_FILE_INLINE_THRESHOLD_BYTES = 100 * 1024;
const PROMPT_FILE_INLINE_THRESHOLD_LABEL = "100KB";
const MAX_ENV_VAR_PREVIEW_LENGTH = 120;
const OUTPUT_TAIL_MAX_CHARS = 600;
const OUTPUT_TAIL_MAX_LINES = 12;
// Default token count threshold above which a 0-turn failure is classified as "long_run_exit"
// rather than the generic "partial_execution". Corresponds to ~30+ minutes of Copilot
// CLI work where the wrapper exits non-zero after the agent has completed substantial work.
// Override via GH_AW_HARNESS_LONG_RUN_TOKEN_THRESHOLD env var.
const DEFAULT_LONG_RUN_TOKEN_THRESHOLD = 10000;
function resolveLongRunTokenThreshold(env = process.env) {
  const configured = Number(env.GH_AW_HARNESS_LONG_RUN_TOKEN_THRESHOLD);
  if (!Number.isFinite(configured) || configured < 0) {
    return DEFAULT_LONG_RUN_TOKEN_THRESHOLD;
  }
  return configured;
}
const LONG_RUN_TOKEN_THRESHOLD = resolveLongRunTokenThreshold();
const POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS = resolvePostResultWatchdogIdleTimeoutMs();
const COPILOT_REQUESTS_PROXY_AUTH_403_TEMPLATE_NAME = "copilot_requests_proxy_auth_403.md";
// Pattern to detect transient CAPIError 400 in copilot output
const CAPI_ERROR_400_PATTERN = /CAPIError:\s*400/;
// Pattern to detect generic HTTP 400 Bad Request responses emitted by engine CLI / SDK wrappers.
// NOTE: keep in sync with HTTP_400_RESPONSE_ERROR_PATTERN in detect_agent_errors.cjs.
// Also matches "400 400 400 no model endpoints available given user constraints" which is emitted
// by the Copilot SDK when no model endpoints are available for the user's configured constraints.
// Also matches "400 400 400 stream_options: Extra inputs are not permitted" which is emitted when
// the Copilot SDK sends an OpenAI-only field to an Anthropic-type provider.
// The non-first alternatives are anchored to a leading "400" to avoid false positives from unrelated
// diagnostic or informational messages that might contain the phrase.
const HTTP_400_RESPONSE_ERROR_PATTERN =
  /(?:Response status code does not indicate success:\s*400(?:\s*\(Bad Request\))?|400[^\n]*no model endpoints available given user constraints|400[^\n]*stream_options:\s*Extra inputs are not permitted)/i;

// Pattern to detect MCP servers blocked by enterprise/organization policy.
// This is a persistent policy configuration error — retrying will not help.
const MCP_POLICY_BLOCKED_PATTERN = /MCP servers were blocked by policy:/;

// Pattern to detect "model not supported" error (e.g. Copilot Pro/Education users hitting
// a model that is unavailable for their subscription tier).
// Also matches the Copilot SDK driver's policy-enablement error, which is emitted when a model
// (commonly the one requested by a subagent / `task` dispatch) is disabled by the org/repo
// Copilot policy:
//   "[copilot-sdk-driver] [sdk-driver] error: Execution failed: Error: No model available.
//    Check policy enablement under GitHub Settings > Copilot"
// The alternative is anchored to the "policy enablement" phrase so that the generic
// "No model available" wording alone does not produce false positives.
// This is a persistent configuration error — retrying with --continue will not help.
const MODEL_NOT_SUPPORTED_PATTERN = /The requested model is not supported|No model available\b[^\n]*policy enablement/i;

// Pattern to detect missing authentication credentials.
// On a --continue attempt this may indicate that the Copilot CLI's on-disk session
// credential (written by a mid-stream interrupted run) is incomplete or invalid.  In that
// case the driver falls back to a fresh run (without --continue) to re-do env-var auth.
// On a fresh run the token is genuinely absent — retrying will not help.
const NO_AUTH_INFO_PATTERN = /No authentication information found|Session was not created with authentication info or custom provider/;
// Pattern to detect authentication failures returned by Copilot API.
// After a first-attempt auth failure, retrying is futile because the entrypoint unsets
// COPILOT_GITHUB_TOKEN between attempts.
//
// This pattern covers the Copilot CAPI 400 response emitted when the supplied token is a
// Personal Access Token (classic or fine-grained):
//   "400 400 checking third-party user token: bad request: Personal Access Tokens
//    are not supported for this endpoint"
// PAT rejection is a persistent credential-type problem — retrying with the same
// token always produces the same 400.  Treating it as an auth failure short-circuits
// the retry loop instead of burning all 4 attempts.
// Common auth failure signals ("Authentication failed", "authentication_failed", "not logged in")
// are handled by isCommonAuthenticationFailedError from harness_retry_guard.cjs.
const COPILOT_PAT_AUTH_FAILED_PATTERN = /checking third-party user token:[^\n]*Personal Access Tokens are not supported/i;
// Pattern: Copilot CLI inference access denied
const INFERENCE_ACCESS_ERROR_PATTERN = /Access denied by policy settings|invalid access to inference/;
// Pattern: Agentic engine process killed by signal (timeout)
const AGENTIC_ENGINE_TIMEOUT_PATTERN = /signal=SIG(?:TERM|KILL|INT)/;
// Pattern: Copilot SDK driver timed out waiting for the session to become idle.
const SDK_SESSION_IDLE_TIMEOUT_PATTERN = /Timeout after \d+ms waiting for session\.idle/;
// Pattern: MCP gateway shutdown surfaced in agent output.
// Anchored to the JSON "message" key emitted by the MCP gateway driver to
// avoid false positives from any process that logs "Gateway shutdown initiated"
// as plain text.
const MCP_GATEWAY_SHUTDOWN_PATTERN = /"message"\s*:\s*"Gateway shutdown initiated"/;
const CONNECTION_REFUSED_ERROR_PATTERN = /connection refused|ECONNREFUSED/i;
const FIRST_CONNECTION_REFUSED_RETRY_DELAY_MS = 1000;

// Pattern to detect null-type tool_call error that poisons conversation history.
// Matches the Copilot API 400 error:
//   "Invalid type for '...tool_calls[N].type': expected one of 'function', ..., but got null instead."
// The model emitted a malformed tool call with type: null.  Retrying with --continue
// re-injects the same broken history, producing the same 400 on every subsequent attempt.
// A fresh restart is required to discard the poisoned history.
const NULL_TYPE_TOOL_CALL_PATTERN = /tool_calls\[.*?\]\.type.*null/;
/**
 * Emit a diagnostic log line to stderr.
 * All driver messages are prefixed with "[copilot-harness]" so they are easy to
 * grep out of the combined agent-stdio.log.
 * @param {string} message
 */
function log(message) {
  process.stderr.write(`[copilot-harness] ${message}\n`);
}

/**
 * Format an inference endpoint for diagnostics without exposing URL credentials,
 * query parameters, or fragments.
 * @param {unknown} endpoint
 * @returns {string}
 */
function formatInferenceEndpointForLog(endpoint) {
  if (typeof endpoint !== "string" || !endpoint.trim()) {
    return "(not set)";
  }
  try {
    const parsed = new URL(endpoint);
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      return "(invalid endpoint)";
    }
    return `${parsed.origin}${parsed.pathname}`;
  } catch {
    return "(invalid endpoint)";
  }
}

/**
 * Log the resolved Copilot inference routing without logging authentication values.
 * @param {{
 *   copilotSDKMode: boolean,
 *   configuredModel: string,
 *   resolvedModel: string,
 *   primaryProviderName: string,
 *   providers: Array<{ name: string, type: string, baseUrl: string, wireApi?: string }>,
 *   models: Array<{ id: string, provider: string }>,
 *   logger?: (message: string) => void,
 * }} options
 */
function logCopilotInferenceConfiguration(options) {
  const logger = options.logger ?? log;
  const configuredModel = options.configuredModel || "(not set)";

  if (!options.copilotSDKMode) {
    logger(`inference routing: mode=cli configuredModel=${JSON.stringify(configuredModel)} endpoint=managed-by-copilot-cli`);
    return;
  }

  const resolvedModel = options.resolvedModel || "(not resolved)";
  const primaryProvider = options.providers.find(provider => provider.name === options.primaryProviderName) ?? options.providers[0];
  logger(
    `inference routing: mode=sdk-byok source=awf-reflect configuredModel=${JSON.stringify(configuredModel)}` +
      ` resolvedModel=${JSON.stringify(resolvedModel)} providerCount=${options.providers.length}` +
      ` modelRouteCount=${options.models.length}`
  );

  for (const [index, provider] of options.providers.entries()) {
    const modelCount = options.models.filter(model => model.provider === provider.name).length;
    logger(
      `inference provider ${index + 1}/${options.providers.length}: name=${JSON.stringify(provider.name)}` +
        ` type=${JSON.stringify(provider.type)} wireApi=${JSON.stringify(provider.wireApi || "(default)")}` +
        ` endpoint=${JSON.stringify(formatInferenceEndpointForLog(provider.baseUrl))}` +
        ` modelCount=${modelCount} selected=${provider === primaryProvider}`
    );
  }

  logger(
    `inference endpoint selected: model=${JSON.stringify(resolvedModel)}` +
      ` provider=${JSON.stringify(primaryProvider?.name || "(unresolved)")}` +
      ` type=${JSON.stringify(primaryProvider?.type || "(unknown)")}` +
      ` wireApi=${JSON.stringify(primaryProvider?.wireApi || "(default)")}` +
      ` endpoint=${JSON.stringify(formatInferenceEndpointForLog(primaryProvider?.baseUrl))}`
  );
  logger("inference handoff: SDK driver receives all reflected provider routes; Copilot sidecar receives the selected primary route; authentication values are omitted");
}

/**
 * @param {NodeJS.ProcessEnv} [env]
 * @param {(message: string) => void} [logger]
 * @returns {{maxRetries: number, initialDelayMs: number, backoffMultiplier: number, maxDelayMs: number}}
 */
function resolveRetryConfig(env = process.env, logger = log) {
  return resolveSharedRetryConfig(env, logger);
}

/**
 * Generate a per-run connection token for Copilot SDK headless authentication.
 * Produces 32 random bytes encoded as a 64-character hexadecimal string.
 * @param {{ randomBytes?: (size: number) => Buffer }} [options]
 * @returns {string} 64-character hexadecimal token (32 random bytes).
 */
function generateCopilotConnectionToken(options) {
  // randomBytes injection exists only for unit tests; production uses crypto.randomBytes.
  const randomBytes = options?.randomBytes ?? crypto.randomBytes;
  return randomBytes(32).toString("hex");
}

/**
 * Determines if the collected output contains a transient CAPIError 400
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isTransientCAPIError(output) {
  return CAPI_ERROR_400_PATTERN.test(output);
}

/**
 * Determines if the collected output contains a generic HTTP 400 response failure.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isHTTP400ResponseError(output) {
  return HTTP_400_RESPONSE_ERROR_PATTERN.test(output);
}

/**
 * Determines if the collected output indicates MCP servers were blocked by policy.
 * This is a persistent configuration error that cannot be resolved by retrying.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isMCPPolicyError(output) {
  return MCP_POLICY_BLOCKED_PATTERN.test(output);
}

/**
 * Determines if the collected output indicates the requested model is not supported.
 * This occurs when a Copilot Pro/Education user attempts to use a model that is not
 * available for their subscription tier.  Retrying will not help.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isModelNotSupportedError(output) {
  return MODEL_NOT_SUPPORTED_PATTERN.test(output);
}

/**
 * Determine whether the current run phase is threat detection.
 * @param {string | undefined | null} phase
 * @returns {boolean}
 */
function isDetectionPhase(phase) {
  return (
    String(phase || "")
      .trim()
      .toLowerCase() === "detection"
  );
}

/**
 * Returns true when the given GitHub event name is eligible for the startup
 * retry budget (exit code 2, no output — Turns=0 driver-handoff failure).
 * Eligible events: "schedule" and "push".
 * @param {string|undefined} eventName
 * @returns {boolean}
 */
function computeStartupRetryEligible(eventName) {
  return eventName === "schedule" || eventName === "push";
}

/**
 * Returns true when a failed attempt qualifies for the startup no-output retry budget.
 * @param {{exitCode: number, hasOutput: boolean}} result
 * @returns {boolean}
 */
function isStartupNoOutputRetryCandidate(result) {
  return !result.hasOutput && result.exitCode === 2;
}

/**
 * Read AWF config written by the compiler before the agent runs.
 * @returns {any|null}
 */
function loadAwfConfigData() {
  try {
    return JSON.parse(fs.readFileSync(AWF_CONFIG_PATH, "utf8"));
  } catch (err) {
    const errAny = /** @type {any} */ err;
    if (errAny?.code !== "ENOENT") {
      log(`awf-config load error: ${getErrorMessage(err)}`);
    }
    return null;
  }
}

/**
 * Resolve gh-aw model aliases (e.g. "small") to concrete Copilot CLI model ids.
 *
 * When the configured model is a known alias but the awf-reflect model catalog is
 * unavailable (e.g. a transient models-endpoint 429/503), a single bounded catalog
 * refresh is attempted via `refetchReflectData` before giving up. If the refresh still
 * cannot produce a catalog, this stops the process before Copilot is spawned rather than
 * forwarding the unresolved alias — the API proxy would otherwise reject it with a
 * misleading "no AI credits pricing" error instead of the actual root cause
 * (see https://github.com/github/gh-aw/issues/52782).
 *
 * @param {{
 *   awfReflectData: object|null,
 *   logger?: (msg: string) => void,
 *   refetchReflectData?: () => Promise<object|null>,
 * }} options
 * @returns {Promise<string>}
 */
async function applyCopilotModelAliasResolution(options) {
  const logger = options.logger || log;
  const configuredModel = typeof process.env.COPILOT_MODEL === "string" ? process.env.COPILOT_MODEL.trim() : "";
  if (!configuredModel) {
    return configuredModel;
  }

  const awfConfig = loadAwfConfigData();
  const aliasMap = awfConfig?.apiProxy?.models;

  const tryResolve = (/** @type {object|null} */ reflectData) =>
    resolveConfiguredCopilotModel({
      configuredModel,
      aliasMap,
      reflectData,
      logger,
    });

  let resolvedModel;
  try {
    resolvedModel = tryResolve(options.awfReflectData);
  } catch (err) {
    if (!(err instanceof ModelAliasResolutionError)) {
      throw err;
    }
    logger(`copilot model alias resolution: retrying awf-reflect model-catalog fetch once before failing for '${configuredModel}'`);
    const refreshedReflectData = options.refetchReflectData ? await options.refetchReflectData() : null;
    try {
      resolvedModel = tryResolve(refreshedReflectData);
    } catch (retryErr) {
      if (!(retryErr instanceof ModelAliasResolutionError)) {
        throw retryErr;
      }
      logger(`copilot model alias resolution failed: model-catalog retrieval prevented alias resolution for '${configuredModel}' after a bounded refresh — refusing to start Copilot with an unresolved alias`);
      process.exit(1);
      return configuredModel; // unreachable, keeps TypeScript control-flow analysis happy
    }
  }

  if (resolvedModel && resolvedModel !== configuredModel) {
    process.env.COPILOT_MODEL = resolvedModel;
  }
  return resolvedModel || configuredModel;
}

/**
 * Auto-configure COPILOT_PROVIDER_WIRE_API based on the resolved COPILOT_MODEL.
 *
 * Skips configuration when COPILOT_PROVIDER_WIRE_API is already set so that
 * explicit engine.env values always take precedence. Looks up the wire_api for
 * the current COPILOT_MODEL in the github-copilot provider section of models.json.
 *
 * @param {{
 *   modelsJson: Record<string, unknown> | null,
 *   logger?: (msg: string) => void,
 * }} options
 */
function applyCopilotWireAPI({ modelsJson, logger = log }) {
  if (process.env.COPILOT_PROVIDER_WIRE_API) {
    logger(`COPILOT_PROVIDER_WIRE_API already set to ${process.env.COPILOT_PROVIDER_WIRE_API} — skipping auto-configure`);
    return; // User override wins — do not auto-configure.
  }
  const modelName = typeof process.env.COPILOT_MODEL === "string" ? process.env.COPILOT_MODEL.trim() : "";
  if (!modelName) return;

  // Look up wire_api for the resolved model in the github-copilot provider catalog.
  const providers = modelsJson !== null && typeof modelsJson === "object" && "providers" in modelsJson ? modelsJson.providers : null;
  const githubCopilotData = providers !== null && typeof providers === "object" && "github-copilot" in providers ? providers["github-copilot"] : null;
  const models = githubCopilotData !== null && typeof githubCopilotData === "object" && "models" in githubCopilotData ? githubCopilotData.models : null;
  if (!models || typeof models !== "object") return;

  // Strip query parameters before catalog lookup (e.g. "gpt-5-mini?effort=high" → "gpt-5-mini").
  const baseModelName = modelName.split("?")[0];
  // Case-insensitive lookup.
  const normalizedModelName = baseModelName.toLowerCase();
  for (const [key, value] of Object.entries(models)) {
    if (key.toLowerCase() === normalizedModelName) {
      const wireApi = value !== null && typeof value === "object" && "wire_api" in value ? value.wire_api : null;
      if (wireApi && typeof wireApi === "string") {
        logger(`auto-configuring COPILOT_PROVIDER_WIRE_API=${wireApi} for model ${modelName}`);
        process.env.COPILOT_PROVIDER_WIRE_API = wireApi;
      }
      return;
    }
  }
}

/**
 * Check whether a model is present in AWF /reflect endpoint data.
 * @param {string} model
 * @param {unknown} reflectData
 * @returns {boolean}
 */
function isModelAvailableInReflectData(model, reflectData) {
  const normalizedModel = typeof model === "string" ? model.trim() : "";
  if (!normalizedModel) return false;
  if (!reflectData || typeof reflectData !== "object") return false;

  // TypeScript needs explicit 'in' check or cast before property access on narrowed object type
  const endpoints = "endpoints" in reflectData && Array.isArray(reflectData.endpoints) ? reflectData.endpoints : [];
  for (const endpoint of endpoints) {
    if (!endpoint || endpoint.configured !== true || !Array.isArray(endpoint.models)) {
      continue;
    }
    if (endpoint.models.includes(normalizedModel)) {
      return true;
    }
  }
  return false;
}

/**
 * Load saved AWF /reflect data and check whether a model is present.
 * @param {string} model
 * @param {{
 *   reflectPath?: string,
 *   readFileSync?: (path: string, encoding: string) => string,
 *   logger?: (msg: string) => void,
 * }} [options]
 * @returns {boolean}
 */
function isModelAvailableInReflectFile(model, options) {
  const normalizedModel = typeof model === "string" ? model.trim() : "";
  const reflectPath = (options && options.reflectPath) || AWF_REFLECT_OUTPUT_PATH;
  const readFile = (options && options.readFileSync) || fs.readFileSync;
  const logger = (options && options.logger) || log;
  if (!normalizedModel) {
    logger("awf-reflect: model availability check skipped (model is empty)");
    return false;
  }

  try {
    const raw = readFile(reflectPath, "utf8");
    const reflectData = JSON.parse(raw);
    return isModelAvailableInReflectData(normalizedModel, reflectData);
  } catch (error) {
    const err = /** @type {Error} */ error;
    logger(`awf-reflect: unable to read model availability from ${reflectPath}: ${err.message}`);
    return false;
  }
}

/**
 * Determines if the collected output contains a "No authentication information found" error.
 * This means no auth token (COPILOT_GITHUB_TOKEN, GH_TOKEN, or GITHUB_TOKEN) is available
 * in the environment.  Retrying will not help because the absent token will remain absent.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isNoAuthInfoError(output) {
  return NO_AUTH_INFO_PATTERN.test(output);
}

/**
 * Determines if the collected output contains an authentication failed error.
 * Covers both the common signals (from harness_retry_guard) and the Copilot-specific
 * PAT rejection error.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isAuthenticationFailedError(output) {
  return isCommonAuthenticationFailedError(output) || COPILOT_PAT_AUTH_FAILED_PATTERN.test(output);
}

/**
 * Determines if the collected output contains a Copilot SDK session.idle timeout.
 * @param {string} output
 * @returns {boolean}
 */
function isSDKSessionIdleTimeoutError(output) {
  return SDK_SESSION_IDLE_TIMEOUT_PATTERN.test(output);
}

/**
 * Determines if the collected output contains an MCP gateway shutdown message.
 * @param {string} output
 * @returns {boolean}
 */
function isMCPGatewayShutdownError(output) {
  return MCP_GATEWAY_SHUTDOWN_PATTERN.test(output);
}

/**
 * Determine whether output contains a connection-refused signal.
 * @param {string} output
 * @returns {boolean}
 */
function isConnectionRefusedError(output) {
  return CONNECTION_REFUSED_ERROR_PATTERN.test(output);
}

/**
 * Decide whether a failed attempt qualifies for the one-shot connection-refused retry.
 *
 * This path only exists to cover the narrow window in which the api-proxy provider listener
 * dies between the SDK-mode readiness probe and the first request. It is therefore restricted
 * to SDK mode (CLI failures keep the generic retry policy, which may reuse `--continue`), to
 * the very first attempt, and to runs that still have a retry budget.
 *
 * @param {{ copilotSDKMode: boolean, attempt: number, isConnectionRefused: boolean, maxRetries: number }} params
 * @returns {boolean}
 */
function shouldRetryFirstConnectionRefused({ copilotSDKMode, attempt, isConnectionRefused, maxRetries }) {
  return Boolean(copilotSDKMode) && attempt === 0 && Boolean(isConnectionRefused) && maxRetries > 0;
}

/**
 * Extract a compact tail preview from combined process output for failure logs.
 * @param {string} output
 * @param {{ maxChars?: number, maxLines?: number }} [options]
 * @returns {string}
 */
function extractOutputTail(output, options) {
  if (typeof output !== "string" || !output) return "";
  const maxChars = options?.maxChars ?? OUTPUT_TAIL_MAX_CHARS;
  const maxLines = options?.maxLines ?? OUTPUT_TAIL_MAX_LINES;
  const normalized = output.replace(/\0/g, "").replace(/\r\n/g, "\n").replace(/\r/g, "\n").trim();
  if (!normalized) return "";
  // filter(Boolean) removes empty strings from blank lines after trimEnd(); maxLines therefore counts non-empty lines.
  const tailLines = normalized
    .split("\n")
    .map(line => line.trimEnd())
    .filter(Boolean)
    .slice(-maxLines);
  if (tailLines.length === 0) return "";
  let tail = tailLines.join("\n");
  if (tail.length > maxChars) {
    const keep = maxChars - 1;
    tail = keep > 0 ? `…${tail.slice(-keep)}` : "…";
  }
  return tail;
}

/**
 * Sum all `"total_tokens"` values emitted in debug JSON response blocks in CLI output.
 * The Copilot CLI emits JSON blocks containing token usage data in its stdout/stderr.
 * Returns 0 when no token data is present (e.g., startup failures that produced no output).
 * @param {string} output - Combined stdout/stderr from the Copilot CLI
 * @returns {number}
 */
function extractTokenCountFromOutput(output) {
  if (!output) return 0;
  let total = 0;
  const pattern = /"total_tokens"\s*:\s*(\d+)/g;
  let match;
  while ((match = pattern.exec(output)) !== null) {
    total += parseInt(match[1], 10);
  }
  return total;
}

/**
 * Classify a failed Copilot attempt into a short, named failure class.
 * @param {{
 *   hasOutput: boolean,
 *   isAuthErr?: boolean,
 *   isAuthenticationFailed?: boolean,
 *   isTransientCAPIError?: boolean,
 *   isMCPGatewayShutdown?: boolean,
 *   isMCPPolicy?: boolean,
 *   isModelNotSupported?: boolean,
 *   isHTTP400ResponseError?: boolean,
 *   isInvocationCapExceeded?: boolean,
 *   isNullTypeToolCall?: boolean,
 *   isQuotaExceeded?: boolean,
 *   isSDKSessionIdleTimeout?: boolean,
 *   hasNumerousPermissionDenied?: boolean,
 *   tokenCount?: number,
 * }} detection
 * @returns {string}
 */
function classifyCopilotFailure(detection) {
  if (detection.isInvocationCapExceeded) return "invocation_cap_exceeded";
  if (detection.isQuotaExceeded) return "capi_quota_exceeded";
  if (detection.isMCPPolicy) return "mcp_policy_blocked";
  if (detection.isModelNotSupported) return "model_not_supported";
  if (detection.isHTTP400ResponseError) return "http_400_response_error";
  if (detection.isNullTypeToolCall) return "null_type_tool_call";
  if (detection.isAuthErr) return "no_auth_info";
  if (detection.isAuthenticationFailed) return "authentication_failed";
  if (detection.isSDKSessionIdleTimeout) return "sdk_session_idle_timeout";
  if (detection.isMCPGatewayShutdown) return "mcp_gateway_shutdown";
  if (detection.hasNumerousPermissionDenied) return "permission_denied";
  if (detection.isTransientCAPIError) return "capi_error_400";
  if (detection.hasOutput && (detection.tokenCount ?? 0) > LONG_RUN_TOKEN_THRESHOLD) return "long_run_exit";
  return detection.hasOutput ? "partial_execution" : "no_output";
}

/**
 * Shared retry predicate for the generic partial-execution branch.
 * Used by the runtime loop and unit tests to avoid divergence.
 * @param {{ exitCode: number, hasOutput: boolean, output: string, attempt: number, maxRetries: number }} params
 *   output must be the combined stdout/stderr text for the failed attempt.
 * @returns {boolean}
 */
function shouldRetryFailedExecution(params) {
  if (params.exitCode === 0) return false;
  if (hasNumerousPermissionDeniedIssues(params.output)) return false;
  if (isCAPIQuotaExceededError(params.output)) return false;
  if (detectNonRetryableHarnessGuard(params.output).maxRunsExceeded) return false;
  return params.attempt < params.maxRetries && params.hasOutput;
}

/**
 * Extract provider auth failure details from Copilot output when available.
 * @param {string} output
 * @returns {{ providerUrl: string, statusCode: string } | null}
 */
function parseProviderAuthFailure(output) {
  const match = output.match(/Authentication failed with provider at (\S+) \(HTTP (\d+)\)\.?/i);
  if (!match) {
    return null;
  }
  return {
    providerUrl: match[1],
    statusCode: match[2],
  };
}

/**
 * Determine whether a provider URL likely points at the gh-aw API proxy sidecar.
 * @param {string} providerUrl
 * @returns {boolean}
 */
function isLikelyAWFAPIProxyURL(providerUrl) {
  try {
    const { hostname, port } = new URL(providerUrl);
    const normalizedHostname = hostname.toLowerCase();
    if (port !== "10002") {
      return false;
    }
    return (
      normalizedHostname === "api-proxy" ||
      normalizedHostname === "host.docker.internal" ||
      normalizedHostname === "localhost" ||
      /^127(?:\.\d{1,3}){3}$/.test(normalizedHostname) ||
      /^10(?:\.\d{1,3}){3}$/.test(normalizedHostname) ||
      /^192\.168(?:\.\d{1,3}){2}$/.test(normalizedHostname) ||
      /^172\.(?:1[6-9]|2\d|3[01])(?:\.\d{1,3}){2}$/.test(normalizedHostname)
    );
  } catch {
    return false;
  }
}

/**
 * Infer which Copilot auth stage failed without exposing secrets.
 * @param {string} output
 * @returns {string}
 */
function detectCopilotAuthFailureStage(output) {
  if (/\b(?:validating|validate|validation)\b[\s\S]{0,40}\b(?:token|auth|authentication)\b/i.test(output)) {
    return "validating the token";
  }
  if (/\b(?:list|listing)\b[\s\S]{0,40}\bmodels?\b/i.test(output) || /\/models\b/i.test(output)) {
    return "listing models";
  }
  return "starting the Copilot CLI request";
}

/**
 * @param {string | undefined} value
 * @returns {boolean}
 */
function envFlagEnabled(value) {
  if (typeof value !== "string") {
    return false;
  }
  const normalized = value.trim().toLowerCase();
  return normalized === "true" || normalized === "1" || normalized === "yes";
}

/**
 * Build a more actionable Copilot auth diagnostic when a 401/403 came from the gh-aw API proxy.
 * @param {string} output
 * @param {NodeJS.ProcessEnv} [env]
 * @param {{ renderTemplateFromFile?: typeof renderTemplateFromFile }} [options]
 * @returns {string}
 */
function buildCopilotProxyAuthFailureDiagnostic(output, env = process.env, options = {}) {
  const authFailure = parseProviderAuthFailure(output);
  if (!authFailure || !isLikelyAWFAPIProxyURL(authFailure.providerUrl)) {
    return "";
  }

  const selectedModel = typeof env.COPILOT_MODEL === "string" && env.COPILOT_MODEL.trim() ? env.COPILOT_MODEL.trim() : "(unset)";
  const stage = detectCopilotAuthFailureStage(output);
  if (authFailure.statusCode === "403" && envFlagEnabled(env.S2STOKENS)) {
    const render = options.renderTemplateFromFile || renderTemplateFromFile;
    return render(getPromptPath(COPILOT_REQUESTS_PROXY_AUTH_403_TEMPLATE_NAME), {
      selected_model: selectedModel,
      stage,
    });
  }
  if (authFailure.statusCode !== "401") {
    return "";
  }
  return (
    `Copilot authentication failed through the gh-aw API proxy (HTTP 401, model=${selectedModel}, stage=${stage}). ` +
    "Check that COPILOT_GITHUB_TOKEN is present, unexpired, and authorized for the selected COPILOT_MODEL. " +
    "If you configured GH_AW_MODEL_AGENT_COPILOT or GH_AW_DEFAULT_MODEL_COPILOT, verify that the token has access to that model."
  );
}

/**
 * Determine whether an authentication_failed error came from the gh-aw API proxy after
 * partial execution, making a one-time fresh-run retry worthwhile.
 * @param {string} output
 * @param {boolean} hasOutput
 * @returns {boolean}
 */
function isRetryableProxyAuthenticationFailure(output, hasOutput) {
  if (!hasOutput || !isAuthenticationFailedError(output)) {
    return false;
  }
  const authFailure = parseProviderAuthFailure(output);
  return Boolean(authFailure && isLikelyAWFAPIProxyURL(authFailure.providerUrl));
}

/**
 * Detect known Copilot error patterns for workflow outputs.
 * @param {string} output
 * @returns {{ inferenceAccessError: boolean, mcpPolicyError: boolean, agenticEngineTimeout: boolean, modelNotSupportedError: boolean, http400ResponseError: boolean }}
 */
function detectCopilotErrors(output) {
  return {
    inferenceAccessError: INFERENCE_ACCESS_ERROR_PATTERN.test(output),
    mcpPolicyError: isMCPPolicyError(output),
    agenticEngineTimeout: AGENTIC_ENGINE_TIMEOUT_PATTERN.test(output),
    modelNotSupportedError: isModelNotSupportedError(output),
    http400ResponseError: isHTTP400ResponseError(output),
  };
}

/**
 * Build child-process environment additions for Copilot SDK mode.
 *
 * When `multiProviderJson` is set, the driver will use multi-provider BYOK.
 * `COPILOT_PROVIDER_*` env vars are still populated from the primary provider
 * for the headless sidecar (sub-agent sessions).
 *
 * @param {{
 *   sdkEnv: NodeJS.ProcessEnv,
 *   copilotSDKMode: boolean,
 *   copilotConnectionToken: string,
 *   providerBaseUrl: string,
 *   providerType: string,
 *   providerWireApi: string,
 *   resolvedModel: string,
 *   multiProviderJson?: string,
 * }} options
 * @returns {NodeJS.ProcessEnv}
 */
function buildCopilotSDKChildEnv({ sdkEnv, copilotSDKMode, copilotConnectionToken, providerBaseUrl, providerType, providerWireApi, resolvedModel, multiProviderJson }) {
  if (!copilotSDKMode) {
    return sdkEnv;
  }
  return {
    ...sdkEnv,
    COPILOT_CONNECTION_TOKEN: copilotConnectionToken,
    ...(multiProviderJson ? { GH_AW_COPILOT_SDK_MULTI_PROVIDER_JSON: multiProviderJson } : {}),
    COPILOT_MODEL: resolvedModel,
    // Native Copilot CLI BYOK env vars — consumed by the headless sidecar for all sessions.
    COPILOT_PROVIDER_BASE_URL: providerBaseUrl,
    COPILOT_PROVIDER_TYPE: providerType,
    ...(providerWireApi ? { COPILOT_PROVIDER_WIRE_API: providerWireApi } : {}),
  };
}

/**
 * Write Copilot detection outputs to $GITHUB_OUTPUT.
 * @param {{ inferenceAccessError: boolean, mcpPolicyError: boolean, agenticEngineTimeout: boolean, modelNotSupportedError: boolean, http400ResponseError: boolean }} results
 */
function writeCopilotOutputs(results) {
  const outputFile = process.env.GITHUB_OUTPUT;
  if (!outputFile) {
    log("GITHUB_OUTPUT not set — skipping copilot error outputs");
    return;
  }

  const lines = [
    `inference_access_error=${results.inferenceAccessError}`,
    `mcp_policy_error=${results.mcpPolicyError}`,
    `agentic_engine_timeout=${results.agenticEngineTimeout}`,
    `model_not_supported_error=${results.modelNotSupportedError}`,
    `http_400_response_error=${results.http400ResponseError}`,
  ];
  try {
    fs.appendFileSync(outputFile, lines.join("\n") + "\n");
  } catch {
    /* ignore */
  }
}

/**
 * Determines if the collected output contains a null-type tool_call error.
 * This error occurs when the model emits a malformed tool call with type: null.
 * The Copilot API rejects it with a 400, and retrying with --continue will re-inject
 * the same broken history, causing the same failure on every subsequent attempt.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isNullTypeToolCallError(output) {
  return NULL_TYPE_TOOL_CALL_PATTERN.test(output);
}

/**
 * Build a structured report_incomplete payload for infrastructure failures.
 * @param {string} details
 * @returns {string}
 */
function buildInfrastructureIncompletePayload(details) {
  return JSON.stringify({
    type: "report_incomplete",
    reason: "infrastructure_error",
    details,
  });
}

/**
 * Append one safe-output entry line.
 * @param {(path: import("node:fs").PathOrFileDescriptor, data: string | Uint8Array, options?: import("node:fs").WriteFileOptions) => void} appendFileSync
 * @param {string} safeOutputsPath
 * @param {string} payload
 */
function appendSafeOutputLine(appendFileSync, safeOutputsPath, payload) {
  appendFileSync(safeOutputsPath, payload + "\n", { encoding: "utf8" });
}

/**
 * Check whether a command path is accessible and executable, logging the result.
 * Returns true if the command is usable, false otherwise.
 * @param {string} command - Absolute or relative path to the executable
 * @returns {Promise<boolean>}
 */
async function checkCommandAccessible(command) {
  try {
    await fs.promises.access(command, fs.constants.F_OK);
  } catch {
    log(`pre-flight: command not found: ${command} (F_OK check failed — binary does not exist at this path)`);
    return false;
  }
  try {
    await fs.promises.access(command, fs.constants.X_OK);
    log(`pre-flight: command is accessible and executable: ${command}`);
    return true;
  } catch {
    log(`pre-flight: command exists but is not executable: ${command} (X_OK check failed — permission denied)`);
    return false;
  }
}

/**
 * Parse GH_AW_COPILOT_SDK_SERVER_ARGS for SDK driver mode.
 * Returns [] when unset or invalid so sidecar defaults remain available.
 *
 * @param {string | undefined} serverArgsEnv
 * @param {{ logger?: (msg: string) => void }} [options]
 * @returns {string[]}
 */
function parseCopilotSDKServerArgsFromEnv(serverArgsEnv, options) {
  const logger = options?.logger ?? log;
  if (!serverArgsEnv) {
    logger("copilot-sdk driver mode: GH_AW_COPILOT_SDK_SERVER_ARGS is not set; using sidecar default args");
    return [];
  }

  try {
    const parsed = JSON.parse(serverArgsEnv);
    if (!Array.isArray(parsed) || parsed.some(arg => typeof arg !== "string")) {
      logger("copilot-sdk driver mode: GH_AW_COPILOT_SDK_SERVER_ARGS must be a JSON string array; using sidecar default args");
      return [];
    }
    logger(`copilot-sdk driver mode: parsed ${parsed.length} sidecar args from GH_AW_COPILOT_SDK_SERVER_ARGS`);
    return parsed;
  } catch (parseErr) {
    const preview = serverArgsEnv.length > MAX_ENV_VAR_PREVIEW_LENGTH ? serverArgsEnv.slice(0, MAX_ENV_VAR_PREVIEW_LENGTH) + "…" : serverArgsEnv;
    logger(`copilot-sdk driver mode: failed to parse GH_AW_COPILOT_SDK_SERVER_ARGS: ${getErrorMessage(parseErr)} (value: ${preview})`);
    return [];
  }
}

/**
 * Build a compact fallback prompt that asks the agent to read instructions from disk.
 * @param {string} promptFile
 * @returns {string}
 */
function buildPromptFileFallbackInstruction(promptFile) {
  return `Read the full instructions from ${promptFile} and execute them exactly as written.`;
}

/**
 * Replace --prompt-file arguments with -p prompt text to support older Copilot CLIs.
 * For files over 100KB, emit a compact fallback prompt that instructs the agent to
 * read and execute the full prompt file from disk.
 * @param {string[]} args
 * @returns {string[]}
 */
function resolvePromptFileArgs(args) {
  /** @type {string[]} */
  const resolvedArgs = [];

  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (arg !== "--prompt-file") {
      resolvedArgs.push(arg);
      continue;
    }

    if (i + 1 >= args.length) {
      log("warning: --prompt-file provided without a path; leaving arguments unchanged");
      resolvedArgs.push(arg);
      continue;
    }
    const promptFile = args[i + 1];

    try {
      const stat = fs.statSync(promptFile);
      log(`resolved --prompt-file: path=${promptFile} size=${stat.size}B`);

      if (stat.size > PROMPT_FILE_INLINE_THRESHOLD_BYTES) {
        log(`prompt file exceeds ${PROMPT_FILE_INLINE_THRESHOLD_LABEL}; using compact fallback prompt`);
        resolvedArgs.push("-p", buildPromptFileFallbackInstruction(promptFile));
      } else {
        const promptText = fs.readFileSync(promptFile, "utf8");
        resolvedArgs.push("-p", promptText);
      }
      i++; // Skip the prompt-file path argument
    } catch (error) {
      const err = /** @type {Error} */ error;
      log(`warning: failed to resolve --prompt-file ${promptFile}: ${err.message}; leaving arguments unchanged`);
      resolvedArgs.push(arg, promptFile);
      i++; // Skip the prompt-file path argument
    }
  }

  return resolvedArgs;
}

/**
 * Main entry point: run copilot with retry logic for partially-executed sessions.
 */
async function main() {
  const [, , command, ...args] = process.argv;
  const retryConfig = resolveRetryConfig(process.env, log);
  const { maxRetries, initialDelayMs, backoffMultiplier, maxDelayMs } = retryConfig;

  if (!command) {
    process.stderr.write("copilot-harness: Usage: node copilot_harness.cjs <command> [args...]\n");
    process.exit(1);
  }

  log(`starting: command=${command} maxRetries=${maxRetries} initialDelayMs=${initialDelayMs}` + ` backoffMultiplier=${backoffMultiplier} maxDelayMs=${maxDelayMs}` + ` nodeVersion=${process.version} platform=${process.platform}`);

  await checkCommandAccessible(command);

  // Build SDK env additions. When COPILOT_SDK_URI is set the harness will start a separate
  // headless Copilot CLI sidecar and this helper merges COPILOT_SDK_URI into the child
  // process env so that every started process (including retry attempts) inherits the
  // correct SDK endpoint URI.
  const sdkEnv = buildCopilotSDKEnv();
  const copilotSDKMode = isCopilotSDKEnabled();
  let copilotConnectionToken = "";
  if (copilotSDKMode) {
    // The harness always generates the connection token when SDK mode is active.
    // The token is injected into the driver subprocess env so the harness-managed
    // sidecar and the driver's SDK client share the same token.
    copilotConnectionToken = generateCopilotConnectionToken();
    maskSecret(copilotConnectionToken);
    log("copilot-sdk mode active: generated per-run COPILOT_CONNECTION_TOKEN");
    log(`copilot-sdk mode active: COPILOT_SDK_URI=${sdkEnv.COPILOT_SDK_URI || "(not set)"}`);
  }

  // In driver mode the args are the driver command + copilot binary path; no stdin payload.
  // In CLI mode, args are resolved to inline prompt text.
  let resolvedArgs;
  if (copilotSDKMode) {
    resolvedArgs = args;
  } else {
    resolvedArgs = resolvePromptFileArgs(args);
  }

  // Fetch AWF API proxy reflection data before running the agent.
  // In SDK/BYOK mode the live data is used immediately to resolve the custom provider
  // configuration that is injected into the driver subprocess environment.
  // Skip when AWF_REFLECT_ENABLED is not "1" (e.g. sandbox.agent: false — no api-proxy running).
  /** @type {any} */
  let awfReflectData = null;
  if (process.env.AWF_REFLECT_ENABLED === "1") {
    const reflectResult = await fetchAWFReflect({ logger: log });
    if (reflectResult.ok && reflectResult.reflectData) {
      awfReflectData = reflectResult.reflectData;
    }
  }

  applyModelFallback(process.env, "COPILOT_MODEL", log);
  await applyCopilotModelAliasResolution({
    awfReflectData,
    logger: log,
    refetchReflectData: async () => {
      if (process.env.AWF_REFLECT_ENABLED !== "1") {
        return null;
      }
      const refreshed = await fetchAWFReflect({ logger: log });
      return refreshed.ok && refreshed.reflectData ? refreshed.reflectData : null;
    },
  });
  applyCopilotWireAPI({ modelsJson: loadModelsJson(), logger: log });

  // Pre-flight: skip the agent entirely when a noop has already been written by a prior step.
  // A noop indicates the work is complete or there is nothing to do — starting the agent
  // (and, in SDK mode, probing provider listener readiness below) would be wasteful and
  // potentially harmful. This must run before the provider-listener readiness gate so a
  // legitimate noop exit is never turned into an infrastructure-incomplete failure by an
  // unrelated listener being unavailable.
  const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS || "";
  if (shouldSkipForNoopSafeOutputs({ safeOutputsPath, hasNoopInSafeOutputs, log })) {
    process.exit(0);
  }

  // Resolve BYOK provider from live reflect data (SDK mode only).
  // Multi-provider BYOK is the only supported mode — fail immediately if the
  // provider cannot be resolved so retries are not wasted on a misconfigured environment.
  let providerBaseUrl = "";
  let providerType = "openai";
  let providerWireApi = "";
  let resolvedModel = "";
  let multiProviderJson = "";
  let primaryProviderName = "";
  if (copilotSDKMode) {
    const configuredModel = process.env.COPILOT_MODEL || "";
    const modelsJson = loadModelsJson();

    const multiProvider = resolveMultiProviderFromReflect({ model: configuredModel, reflectData: awfReflectData, modelsJson, logger: log });
    if (!multiProvider) {
      log("copilot-sdk driver mode: BYOK provider is required but could not be resolved from awf-reflect data — aborting");
      process.exit(1);
    }
    resolvedModel = multiProvider.model;
    multiProviderJson = JSON.stringify({ model: multiProvider.model, providers: multiProvider.providers, models: multiProvider.models });
    // Set the primary provider's details as COPILOT_PROVIDER_* env vars for the headless sidecar
    // (which still reads those to configure its own sub-agent sessions).
    primaryProviderName = multiProvider.models.find(m => m.id === resolvedModel)?.provider ?? multiProvider.providers[0]?.name ?? "";
    const primaryProvider = multiProvider.providers.find(p => p.name === primaryProviderName) ?? multiProvider.providers[0];
    providerBaseUrl = primaryProvider?.baseUrl ?? "";
    providerType = primaryProvider?.type ?? "openai";
    providerWireApi = primaryProvider?.wireApi ?? "";

    // For BYOK copilot providers, prefix the model with "copilot/" so subagents treat it as BYOK.
    // The headless sidecar reads COPILOT_MODEL to configure sub-agent sessions spawned via the task tool,
    // and the "copilot/" prefix signals to use the custom provider config from COPILOT_PROVIDER_* env vars.
    const isCopilotProvider = primaryProviderName && (primaryProviderName.toLowerCase().includes("copilot") || primaryProviderName.toLowerCase().includes("github-copilot"));
    if (isCopilotProvider && resolvedModel && !resolvedModel.includes("/")) {
      resolvedModel = `copilot/${resolvedModel}`;
    }

    log(`copilot-sdk driver mode: multi-provider config resolved (${multiProvider.providers.length} providers, ${multiProvider.models.length} models, model=${resolvedModel})`);
    logCopilotInferenceConfiguration({
      copilotSDKMode,
      configuredModel,
      resolvedModel,
      primaryProviderName,
      providers: multiProvider.providers,
      models: multiProvider.models,
    });

    const uniqueProviderBaseUrls = [...new Set(multiProvider.providers.map(provider => String(provider.baseUrl || "").trim()).filter(Boolean))];
    if (uniqueProviderBaseUrls.length === 0) {
      log("copilot-sdk driver mode: no provider baseUrls to probe — skipping listener readiness check");
    }
    for (const providerBaseUrlToProbe of uniqueProviderBaseUrls) {
      const readiness = await waitForProviderListenerReady({
        baseUrl: providerBaseUrlToProbe,
        timeoutMs: AWF_PROVIDER_LISTENER_READY_TIMEOUT_MS,
        logger: log,
      });
      if (!readiness.ok) {
        emitInfrastructureIncomplete(`api-proxy provider listener was not ready at ${providerBaseUrlToProbe} before first Copilot SDK request (${readiness.error}).`);
        log(`copilot-sdk driver mode: provider listener readiness probe failed for ${providerBaseUrlToProbe}: ${readiness.error}`);
        process.exit(1);
      }
    }
  } else {
    logCopilotInferenceConfiguration({
      copilotSDKMode,
      configuredModel: process.env.COPILOT_MODEL || "",
      resolvedModel,
      primaryProviderName,
      providers: [],
      models: [],
    });
  }

  // Merge SDK env additions into the child process env only when the SDK helper
  // returned at least one variable; otherwise leave the env undefined so that
  // runProcess inherits the full process.env (the common case).
  // sdkEnv already contains SDK-mode variables (e.g. COPILOT_SDK_URI) when enabled.
  // Always attach the generated per-run COPILOT_CONNECTION_TOKEN so both the sidecar
  // (started by the harness) and the SDK client share the same token.
  //
  // Forward BYOK config as native Copilot CLI COPILOT_PROVIDER_* env vars so
  // the headless sidecar propagates the same provider to sub-agent sessions spawned via the
  // task tool. Sub-agents do not inherit the SDK session-level `providers` config; the headless
  // server instead reads COPILOT_PROVIDER_* from its own process env to configure each
  // sub-agent session's inference backend.
  const sdkChildEnv = buildCopilotSDKChildEnv({
    sdkEnv,
    copilotSDKMode,
    copilotConnectionToken,
    providerBaseUrl,
    providerType,
    providerWireApi,
    resolvedModel,
    multiProviderJson,
  });
  const childEnv = Object.keys(sdkChildEnv).length > 0 ? { ...process.env, ...sdkChildEnv } : undefined;

  let delay = initialDelayMs;
  let lastExitCode = 1;
  let lastHasOutput = false;
  const isScheduledRun = process.env.GITHUB_EVENT_NAME === "schedule";
  // Push-triggered runs are also eligible for the startup retry budget (exit code 2, no output).
  // The Tidy workflow was previously push-triggered and showed deterministic Turns=0 failures
  // at startup; extending the retry to push prevents the same pattern from recurring if a
  // push trigger is (re)added to any agentic workflow.
  const isStartupRetryEligible = computeStartupRetryEligible(process.env.GITHUB_EVENT_NAME);
  let scheduledExit2Retries = 0;
  let scheduledExit2RetryAttempted = false;
  let useContinueOnRetry = false;
  let modelNotSupportedReflectRetryAttempted = false;
  // Once set to true, --continue is never re-enabled for the remainder of this run.
  // This prevents a broken --continue recovery from resurrecting --continue on the next attempt.
  let continueDisabledPermanently = false;
  const driverStartTime = Date.now();
  // Soft-timeout guard: polled at the top of the retry loop and after each backoff sleep.
  // It does not preempt a running attempt — if a single invocation runs past the soft
  // deadline the guard fires on the next iteration. Individual attempts are expected to
  // complete within the SOFT_TIMEOUT_BUFFER_MS window.
  const softTimeoutGuard = buildSoftTimeoutGuard(driverStartTime);
  const detectedCopilotErrors = {
    inferenceAccessError: false,
    mcpPolicyError: false,
    agenticEngineTimeout: false,
    modelNotSupportedError: false,
    http400ResponseError: false,
  };
  /** @type {Awaited<ReturnType<typeof startCopilotSDKServer>>} */
  let copilotSDKServer = null;
  try {
    if (copilotSDKMode) {
      // Driver mode: the harness starts the sidecar; the driver subprocess only opens a client.
      // Server args are provided via GH_AW_COPILOT_SDK_SERVER_ARGS (JSON-encoded CLI arg list
      // generated by the Go engine).  The copilot binary is args[1] in the driver command:
      //   node copilot_harness.cjs $GH_AW_NODE_EXEC copilot_sdk_driver.cjs <copilot-binary>
      const copilotBin = args[1];
      if (!copilotBin) {
        log("copilot-sdk driver mode: missing copilot binary path in args[1]");
        lastExitCode = 1;
      } else {
        let driverServerArgs = parseCopilotSDKServerArgsFromEnv(process.env.GH_AW_COPILOT_SDK_SERVER_ARGS, { logger: log });
        if (process.env.GITHUB_WORKSPACE) {
          driverServerArgs = [...driverServerArgs, "--add-dir", process.env.GITHUB_WORKSPACE];
          log(`copilot-sdk driver mode: appended workspace --add-dir ${process.env.GITHUB_WORKSPACE}`);
        }
        log(`copilot-sdk driver mode: starting sidecar command=${copilotBin} args=${driverServerArgs.length}`);
        copilotSDKServer = await startCopilotSDKServer({
          command: copilotBin,
          env: childEnv ?? process.env,
          serverArgs: driverServerArgs.length > 0 ? driverServerArgs : undefined,
          logger: log,
        });
      }
    }

    // CLI mode always enters the retry loop.
    // Driver mode always enters when the sidecar started successfully.
    if (!copilotSDKMode || copilotSDKServer) {
      // Unified retry loop for CLI and driver modes.
      // --continue is a CLI concept; in SDK mode retries always restart the session fresh.
      const retryRun = await runHarnessRetryLoop({
        maxRetries,
        initialDelayMs,
        backoffMultiplier,
        maxDelayMs,
        driverStartTime,
        harnessName: "Copilot harness",
        log,
        softTimeoutGuard,
        getRetryMode: () => (!copilotSDKMode && useContinueOnRetry ? "--continue" : "fresh run"),
        runAttempt: async attempt => {
          // Add --continue flag on CLI retries so the copilot session continues from where it left off
          const currentArgs = !copilotSDKMode && attempt > 0 && useContinueOnRetry ? [...resolvedArgs, "--continue"] : resolvedArgs;

          // Redact --prompt / -p value from logs to avoid leaking prompt content
          const safeArgs = currentArgs.map((arg, i) => (currentArgs[i - 1] === "--prompt" || currentArgs[i - 1] === "-p" ? "<redacted>" : arg));
          // Driver mode: run copilot_sdk_driver.cjs as a normal subprocess. The harness has
          // already started the sidecar; the driver only opens an SDK client connection.
          const result = await runProcess({
            command,
            args: currentArgs,
            attempt,
            log,
            logArgs: safeArgs,
            env: childEnv,
            postResultWatchdog: safeOutputsPath
              ? {
                  shouldArm: () => hasTerminalSafeOutput(safeOutputsPath),
                  inactivityTimeoutMs: POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS,
                }
              : undefined,
          });
          lastHasOutput = result.hasOutput;
          const attemptDetections = detectCopilotErrors(result.output);
          detectedCopilotErrors.inferenceAccessError ||= attemptDetections.inferenceAccessError;
          detectedCopilotErrors.mcpPolicyError ||= attemptDetections.mcpPolicyError;
          detectedCopilotErrors.agenticEngineTimeout ||= attemptDetections.agenticEngineTimeout;
          detectedCopilotErrors.modelNotSupportedError ||= attemptDetections.modelNotSupportedError;
          detectedCopilotErrors.http400ResponseError ||= attemptDetections.http400ResponseError;
          return result;
        },
        handleFailure: async ({ attempt, result }) => {
          // Determine whether to retry.
          // Retry whenever the session was partially executed (hasOutput).
          //   - CLI mode: retry with --continue so the Copilot CLI can continue from on-disk state.
          //   - SDK mode: retry always restarts fresh — there is no CLI on-disk state to resume.
          // CAPIError 400 is the well-known transient case, but any partial-execution failure is
          // eligible for a retry.
          // Exceptions:
          //   - MCP policy errors and model-not-supported errors are persistent configuration issues.
          //   - Auth errors trigger a one-time fallback to a fresh run; after that --continue is
          //     permanently disabled.
          //   - Null-type tool_call 400 errors poison conversation history — always restart fresh and
          //     permanently disable --continue so the corrupt state is never reloaded.
          const isCAPIError = isTransientCAPIError(result.output);
          const isQuotaExceeded = isCAPIQuotaExceededError(result.output);
          const isMCPPolicy = isMCPPolicyError(result.output);
          const isModelNotSupported = isModelNotSupportedError(result.output);
          const hasHTTP400ResponseError = isHTTP400ResponseError(result.output);
          const isAuthErr = isNoAuthInfoError(result.output);
          const isAuthenticationFailed = isAuthenticationFailedError(result.output);
          const proxyAuthDiagnostic = buildCopilotProxyAuthFailureDiagnostic(result.output, process.env);
          const retryableProxyAuthenticationFailure = isRetryableProxyAuthenticationFailure(result.output, result.hasOutput);
          const isNullTypeToolCall = isNullTypeToolCallError(result.output);
          const isSDKSessionIdleTimeout = isSDKSessionIdleTimeoutError(result.output);
          const isMCPGatewayShutdown = isMCPGatewayShutdownError(result.output);
          const isConnectionRefused = isConnectionRefusedError(result.output);
          const permissionDeniedCount = countPermissionDeniedIssues(result.output);
          const hasNumerousPermissionDenied = hasNumerousPermissionDeniedIssues(result.output);
          const nonRetryableGuard = detectNonRetryableHarnessGuard(result.output);
          const isInvocationCapExceeded = nonRetryableGuard.maxRunsExceeded;
          const tokenCount = extractTokenCountFromOutput(result.output);
          const attemptDurationMs = result.durationMs ?? 0;
          const failureClass = classifyCopilotFailure({
            hasOutput: result.hasOutput,
            isAuthErr,
            isAuthenticationFailed,
            isTransientCAPIError: isCAPIError,
            isMCPGatewayShutdown,
            isMCPPolicy,
            isModelNotSupported,
            isHTTP400ResponseError: hasHTTP400ResponseError,
            isInvocationCapExceeded,
            isNullTypeToolCall,
            isQuotaExceeded,
            isSDKSessionIdleTimeout,
            hasNumerousPermissionDenied,
            tokenCount,
          });
          const outputTail = extractOutputTail(result.output);
          log(
            `attempt ${attempt + 1} failed:` +
              ` exitCode=${result.exitCode}` +
              ` failureClass=${failureClass}` +
              ` isCAPIError400=${isCAPIError}` +
              ` isCAPIQuotaExceededError=${isQuotaExceeded}` +
              ` isInvocationCapExceeded=${isInvocationCapExceeded}` +
              ` isMCPPolicyError=${isMCPPolicy}` +
              ` isModelNotSupportedError=${isModelNotSupported}` +
              ` isHTTP400ResponseError=${hasHTTP400ResponseError}` +
              ` isNullTypeToolCallError=${isNullTypeToolCall}` +
              ` isSDKSessionIdleTimeoutError=${isSDKSessionIdleTimeout}` +
              ` isMCPGatewayShutdownError=${isMCPGatewayShutdown}` +
              ` isConnectionRefusedError=${isConnectionRefused}` +
              ` isAuthError=${isAuthErr}` +
              ` isAuthenticationFailedError=${isAuthenticationFailed}` +
              ` permissionDeniedCount=${permissionDeniedCount}` +
              ` hasNumerousPermissionDenied=${hasNumerousPermissionDenied}` +
              ` hasOutput=${result.hasOutput}` +
              ` tokenCount=${tokenCount}` +
              ` attemptDurationMs=${attemptDurationMs}` +
              ` retriesRemaining=${maxRetries - attempt}`
          );
          if (outputTail) {
            log(`attempt ${attempt + 1}: outputTail=${JSON.stringify(outputTail)}`);
          }
          // Driver-handoff diagnostic: when no output was produced, the agent never ran a turn.
          // This is the Turns=0 signature.  Log a named diagnostic to make it clearly visible
          // in the run log for cross-engine triage.
          if (!result.hasOutput) {
            log(
              `attempt ${attempt + 1}: driver-handoff-turns0 exitCode=${result.exitCode}` +
                ` eventName=${process.env.GITHUB_EVENT_NAME ?? "unknown"}` +
                ` startupRetryEligible=${isStartupRetryEligible}` +
                ` startupRetries=${scheduledExit2Retries}/${MAX_SCHEDULED_EXIT2_RETRIES}`
            );
          }

          if (shouldStopForNoopSafeOutputs({ attempt, safeOutputsPath, hasNoopInSafeOutputs, log })) {
            return { action: "stop", exitCode: 0 };
          }

          // When the run fails with partial_execution and the safe-outputs file already contains a
          // terminal result, treat the run as a success-with-late-activity rather than a
          // retryable failure.  This covers the common case where the post-result watchdog fires
          // because the agent kept running exploratory commands after its deliverable was ready
          // (watchdogFired=true), as well as any other partial_execution failure that occurs
          // after the primary task output was already produced.  Retrying would reproduce the
          // same pattern and exhaust the retry budget without ever posting a final safe-output.
          // The no_output + watchdogFired case is also handled here: the post-result watchdog is
          // only armed after hasTerminalSafeOutput is true, so watchdogFired on a no-stdio-output
          // run means the agent completed its task (wrote safe-output) but produced no console
          // output before the watchdog terminated the idle process.
          const isExpectedLateExit = failureClass === "partial_execution" || failureClass === "long_run_exit" || (failureClass === "no_output" && result.watchdogFired) || (failureClass === "authentication_failed" && result.watchdogFired);
          if (isExpectedLateExit && safeOutputsPath && hasTerminalSafeOutput(safeOutputsPath)) {
            const reason = result.watchdogFired ? "post-result watchdog fired after terminal safe-output was emitted" : "partial execution after terminal safe-output was already produced";
            log(`attempt ${attempt + 1}: ${reason} — treating as success (late-activity exit suppressed)`);
            return { action: "stop", exitCode: 0 };
          }

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
          const shouldTreatAICreditsExceededAsSuccess = trustedAICreditsExceeded && (!isAuthenticationFailed || !!proxyAICreditsRejection);
          if (shouldTreatAICreditsExceededAsSuccess || isInvocationCapExceeded) {
            const reasons = [];
            if (shouldTreatAICreditsExceededAsSuccess) reasons.push("AI credits budget exceeded");
            if (isInvocationCapExceeded) {
              reasons.push("LLM invocation cap saturated — the pooled per-run budget is fully exhausted; retries cannot make progress");
            }
            log(`attempt ${attempt + 1}: ${reasons.join(" and ")} — not retrying (non-retryable guard condition)`);
            if (shouldTreatAICreditsExceededAsSuccess) {
              log(`attempt ${attempt + 1}: AI credits budget enforced — exiting 0 (budget control, not an error)`);
              return { action: "stop", exitCode: 0 };
            }
            if (isInvocationCapExceeded && safeOutputsPath && hasTerminalSafeOutput(safeOutputsPath)) {
              log(`attempt ${attempt + 1}: invocation cap saturated but safe-outputs already contain expected output — suppressing terminal verdict (false-red: core work succeeded)`);
              return { action: "stop", exitCode: 0 };
            }
            return { action: "stop" };
          }

          // attempt === 0 makes this a one-time fresh-run recovery path.
          if (attempt === 0 && retryableProxyAuthenticationFailure) {
            useContinueOnRetry = false;
            continueDisabledPermanently = true;
            log(`attempt ${attempt + 1}: provider authentication failed after partial execution - will retry once as fresh run to avoid losing completed agent work`);
            return { action: "retry" };
          }

          if (isAuthenticationFailed) {
            if (proxyAuthDiagnostic) {
              log(`attempt ${attempt + 1}: ${proxyAuthDiagnostic} — not retrying`);
            } else {
              log(`attempt ${attempt + 1}: authentication failed — not retrying`);
            }
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

          if (isMCPPolicy) {
            log(`attempt ${attempt + 1}: MCP servers blocked by policy — not retrying (this is a policy configuration issue, not a transient error)`);
            return { action: "stop" };
          }

          if (isModelNotSupported) {
            if (!modelNotSupportedReflectRetryAttempted && attempt < maxRetries && isDetectionPhase(process.env.GH_AW_PHASE) && process.env.AWF_REFLECT_ENABLED === "1") {
              const configuredModel = process.env.COPILOT_MODEL || "";
              modelNotSupportedReflectRetryAttempted = true;
              log(`attempt ${attempt + 1}: model not supported during detection — refreshing awf-reflect to rule out startup registry race`);
              await fetchAWFReflect({ logger: log });
              if (isModelAvailableInReflectFile(configuredModel, { logger: log })) {
                useContinueOnRetry = false;
                continueDisabledPermanently = true;
                log(`attempt ${attempt + 1}: refreshed awf-reflect now includes model '${configuredModel}' — retrying once as fresh run`);
                return { action: "retry" };
              }
              log(`attempt ${attempt + 1}: refreshed awf-reflect does not include model '${configuredModel || "(none)"}' — treating as non-retryable`);
            }
            log(`attempt ${attempt + 1}: model not supported — not retrying (the requested model is unavailable for this subscription tier; specify a supported model in the workflow frontmatter)`);
            return { action: "stop" };
          }

          if (hasHTTP400ResponseError) {
            if (attempt < maxRetries && result.hasOutput && useContinueOnRetry) {
              useContinueOnRetry = false;
              continueDisabledPermanently = true;
              log(`attempt ${attempt + 1}: HTTP 400 response error on --continue — retrying once as fresh run (request/state may be stale; --continue disabled permanently)`);
              return { action: "retry" };
            }
            log(`attempt ${attempt + 1}: HTTP 400 response error — not retrying (persistent request validation/state failure)`);
            return { action: "stop" };
          }

          if (isAuthErr) {
            if (useContinueOnRetry && attempt < maxRetries) {
              useContinueOnRetry = false;
              continueDisabledPermanently = true;
              log(`attempt ${attempt + 1}: auth error on --continue — retrying as fresh run (session credential may be corrupted; context will be lost)`);
              return { action: "retry" };
            }
            log(`attempt ${attempt + 1}: no authentication information found — not retrying (COPILOT_GITHUB_TOKEN, GH_TOKEN, and GITHUB_TOKEN are all absent or invalid)`);
            return { action: "stop" };
          }

          if (isNullTypeToolCall) {
            if (attempt < maxRetries && result.hasOutput) {
              const priorMode = attempt > 0 && useContinueOnRetry ? "--continue" : "fresh run";
              useContinueOnRetry = false;
              continueDisabledPermanently = true;
              log(`attempt ${attempt + 1}: null-type tool_call error (${priorMode}) — restarting fresh (poisoned history discarded; --continue disabled permanently)`);
              return { action: "retry" };
            }
          }

          // The listener readiness probe above ensures the api-proxy provider listener is
          // accepting connections before attempt 0 is sent in SDK mode. A ECONNREFUSED here
          // means the provider died in the narrow window between the probe and the first
          // request — retry once as a fresh run. CLI mode and later attempts (attempt > 0) fall
          // through to the generic retry handling below instead of taking this one-shot path.
          if (shouldRetryFirstConnectionRefused({ copilotSDKMode, attempt, isConnectionRefused, maxRetries })) {
            useContinueOnRetry = false;
            log(`attempt ${attempt + 1}: connection refused on first request path — retrying as fresh run with short backoff (${FIRST_CONNECTION_REFUSED_RETRY_DELAY_MS}ms) (attempt ${attempt + 2}/${maxRetries + 1})`);
            return { action: "retry", nextDelayMs: FIRST_CONNECTION_REFUSED_RETRY_DELAY_MS };
          }

          if (isStartupRetryEligible && isStartupNoOutputRetryCandidate(result) && scheduledExit2Retries < MAX_SCHEDULED_EXIT2_RETRIES && attempt < maxRetries) {
            scheduledExit2Retries += 1;
            scheduledExit2RetryAttempted = true;
            useContinueOnRetry = false;
            const triggerLabel = isScheduledRun ? "scheduled" : "push";
            log(`attempt ${attempt + 1}: ${triggerLabel} startup interruption (exit code 2, no output — driver-handoff Turns=0)` + ` — retrying once as fresh run (startupRetry=${scheduledExit2Retries}/${MAX_SCHEDULED_EXIT2_RETRIES})`);
            return { action: "retry" };
          }
          if (isStartupRetryEligible && isStartupNoOutputRetryCandidate(result) && scheduledExit2Retries < MAX_SCHEDULED_EXIT2_RETRIES && attempt >= maxRetries) {
            log(`attempt ${attempt + 1}: startup interruption detected (driver-handoff Turns=0) but retry budget exhausted — no attempts remain`);
          }

          if (isQuotaExceeded) {
            log(`attempt ${attempt + 1}: Copilot quota exceeded — not retrying`);
            return { action: "stop" };
          }

          if (shouldRetryFailedExecution({ ...result, attempt, maxRetries })) {
            const reason = isCAPIError ? "CAPIError 400 (transient)" : "partial execution";
            const isCrashSignal = isCrashSignalExitCode(result.exitCode);
            const crashSignalName = crashSignalNameForExitCode(result.exitCode);
            if (isCrashSignal) {
              continueDisabledPermanently = true;
            }
            // --continue is only meaningful in CLI mode; SDK mode always restarts fresh.
            useContinueOnRetry = !copilotSDKMode && !continueDisabledPermanently;
            const retryMode = useContinueOnRetry ? "--continue" : copilotSDKMode ? "fresh run" : "fresh run (--continue permanently disabled)";
            const crashSuffix = isCrashSignal ? ` crashSignal=${crashSignalName}` : "";
            log(`attempt ${attempt + 1}: ${reason} — will retry with ${retryMode} (attempt ${attempt + 2}/${maxRetries + 1})${crashSuffix}`);
            return { action: "retry" };
          }

          if (attempt >= maxRetries) {
            log(`all ${maxRetries} retries exhausted — giving up (exitCode=${result.exitCode})`);
          } else {
            log(`attempt ${attempt + 1}: no output produced — not retrying` + ` (possible causes: binary not found, permission denied, auth failure, or silent startup crash)`);
          }

          // Non-retryable error or retries exhausted — propagate exit code
          return { action: "stop" };
        },
      });
      lastExitCode = retryRun.exitCode;

      if (isStartupRetryEligible && scheduledExit2RetryAttempted && isStartupNoOutputRetryCandidate({ exitCode: lastExitCode, hasOutput: lastHasOutput })) {
        const triggerLabel = isScheduledRun ? "scheduled" : "push";
        emitInfrastructureIncomplete(
          `Copilot API interruption (exit code 2) persisted after automatic retry in ${triggerLabel} workflow run. ` + "This is the Turns=0 driver-handoff failure signature. Check the agent-stdio.log for startup diagnostics."
        );
      }
    }

    // Fetch AWF API proxy reflection data and persist to disk for post-run step summary.
    // This is best-effort: failures are logged but do not affect the agent exit code.
    // Skip when AWF_REFLECT_ENABLED is not "1" (e.g. sandbox.agent: false — no api-proxy running).
    if (process.env.AWF_REFLECT_ENABLED === "1") {
      await fetchAWFReflect({ logger: log });
    }
  } finally {
    await stopCopilotSDKServer(copilotSDKServer, { logger: log });
  }
  log(`done: exitCode=${lastExitCode} totalDuration=${formatDuration(Date.now() - driverStartTime)}`);
  process.exit(lastExitCode);
}

if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    AWF_API_PROXY_REFLECT_URL,
    AWF_REFLECT_OUTPUT_PATH,
    AWF_REFLECT_TIMEOUT_MS,
    AWF_MODELS_URL_TIMEOUT_MS,
    GEMINI_MODEL_NAME_PREFIX,
    PROMPT_FILE_INLINE_THRESHOLD_BYTES,
    appendSafeOutputLine,
    buildMissingToolAlternatives,
    buildPromptFileFallbackInstruction,
    buildInfrastructureIncompletePayload,
    emitInfrastructureIncomplete,
    emitMissingToolPermissionIssue,
    enrichReflectModels,
    extractModelIds,
    extractDeniedCommands,
    fetchAWFReflect,
    fetchModelsFromUrl,
    buildCopilotProxyAuthFailureDiagnostic,
    buildCopilotSDKChildEnv,
    envFlagEnabled,
    generateCopilotConnectionToken,
    buildCopilotSDKServerArgs,
    getCopilotSDKServerPort,
    hasNoopInSafeOutputs,
    hasExpectedSafeOutputs,
    isDetectionPhase,
    computeStartupRetryEligible,
    isHTTP400ResponseError,
    isModelAvailableInReflectData,
    isModelAvailableInReflectFile,
    resolveMultiProviderFromReflect,
    inferProviderTypeForModel,
    countPermissionDeniedIssues,
    detectCopilotErrors,
    classifyCopilotFailure,
    extractTokenCountFromOutput,
    shouldRetryFailedExecution,
    isCrashSignalExitCode,
    crashSignalNameForExitCode,
    extractOutputTail,
    isRetryableProxyAuthenticationFailure,
    hasNumerousPermissionDeniedIssues,
    INFERENCE_ACCESS_ERROR_PATTERN,
    AGENTIC_ENGINE_TIMEOUT_PATTERN,
    buildMissingToolPermissionIssuePayload,
    isAuthenticationFailedError,
    isConnectionRefusedError,
    shouldRetryFirstConnectionRefused,
    FIRST_CONNECTION_REFUSED_RETRY_DELAY_MS,
    isMCPGatewayShutdownError,
    isSDKSessionIdleTimeoutError,
    startCopilotSDKServer,
    stopCopilotSDKServer,
    waitForCopilotSDKServer,
    writeCopilotOutputs,
    resolvePromptFileArgs,
    resolveRetryConfig,
    parseCopilotSDKServerArgsFromEnv,
    isCAPIQuotaExceededError,
    hasTerminalSafeOutput,
    applyModelFallback,
    applyCopilotModelAliasResolution,
    applyCopilotWireAPI,
    formatInferenceEndpointForLog,
    logCopilotInferenceConfiguration,
    loadAwfConfigData,
    resolveLongRunTokenThreshold,
  };
}

if (require.main === module) {
  main().catch(err => {
    log(`unexpected error: ${getErrorMessage(err)}`);
    process.exit(1);
  });
}
