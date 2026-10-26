// @ts-check

/**
 * Copilot Harness with Retry Logic
 *
 * Wraps the Copilot CLI command (or @github/copilot-sdk session in SDK mode) with retry logic
 * for failures that occur after the session has been partially executed.  Passes all arguments
 * to the copilot subprocess, forwarding stdout/stderr. Small prompt files are delivered as
 * CLI arguments for compatibility; large prompt files are streamed through stdin.
 *
 * Retry policy (shared by CLI and SDK modes):
 *   - If the process produced any output (hasOutput) and exits with a non-zero code, the
 *     session is considered partially executed and is retried.
 *     - CLI mode: retries with --continue so the Copilot CLI can continue from on-disk state.
 *     - SDK mode: retries always restart the session fresh (--continue is a CLI concept).
 *   - CAPIError 400 is a well-known transient failure mode and is logged explicitly, but
 *     any partial-execution failure is retried — not just CAPIError 400.
 *   - If the process produced no output (failed to start / auth error before any work), the
 *     driver does not retry because there is nothing to resume, except for retry-exhausted CAPI
 *     5xx errors, which may recover during harness backoff and restart fresh without --continue.
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
 *   - Tool-call ID schema errors (400 "Invalid 'input[N].id': 'ctc_call_...'.
 *     Expected an ID that begins with 'fc'") are non-transient. Stop rather than
 *     resuming poisoned history or restarting after partial execution.
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

const { iterateJsonl } = require("./jsonl_helpers.cjs");
require("./shim.cjs");

const { getErrorMessage } = require("./error_helpers.cjs");
const { addCopilotURLPermissions } = require("./copilot_url_permissions.cjs");
const { maskSecret } = require("./actions_secret_masking.cjs");
const fs = require("fs");
const os = require("os");
const path = require("path");
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
const { captureExecutionEvidenceConfiguration, resolveHarnessRetryTelemetryPath, runHarnessRetryLoop, shouldSkipForNoopSafeOutputs, shouldStopForNoopSafeOutputs, stripExecutionEvidenceEnv } = require("./harness_retry_runner.cjs");
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
  normalizeReflectProviderName,
  REFLECT_PROVIDER_ALIASES,
} = require("./awf_reflect.cjs");
const { runSafeOutputsCLI, buildMissingToolAlternatives, emitMissingToolPermissionIssue, emitInfrastructureIncomplete, hasExpectedSafeOutputs, hasTerminalSafeOutput, hasNoopInSafeOutputs } = require("./safeoutputs_cli.cjs");
const { countPermissionDeniedIssues, hasNumerousPermissionDeniedIssues, extractDeniedCommands, buildMissingToolPermissionIssuePayload } = require("./permission_denied_helpers.cjs");
const { detectNonRetryableHarnessGuard, buildSoftTimeoutGuard, emitSoftTimeoutSignal, isAuthenticationFailedError: isCommonAuthenticationFailedError, parseAICreditsExceededProxyRejection } = require("./harness_retry_guard.cjs");
const { isCrashSignalExitCode, crashSignalNameForExitCode } = require("./harness_crash_signals.cjs");
const { isCAPIQuotaExceededError, isCAPIServerError } = require("./detect_agent_errors.cjs");
const { applyModelFallback } = require("./model_fallback.cjs");
const { parseMultiProviderJson, qualifyModelForMultiProvider, isCopilotProviderName } = require("./copilot_sdk_multi_provider.cjs");
const { copilotSessionContext, getCopilotMessageRefusal } = require("./copilot_session.cjs");
const { isRoutingReasoningEffort } = require("./copilot_routing_effort.cjs");
const { resolveAWFModelRoutingSelection, isModelAvailableInReflectData, getAWFModelRoutingPolicy, getAWFModelRoutingFailureCode } = require("./awf_model_routing.cjs");
const { recordAWFModelRoutingOutcome } = require("./awf_model_routing.cjs");
const COPILOT_ROUTING_POLICY = getAWFModelRoutingPolicy("copilot");
const { loadModelsJson } = require("./model_costs.cjs");
const { applyCopilotWireAPI: configureCopilotWireAPI, warnCopilotSubagentEndpoints, recordModelEndpointMismatch, getSupportedEndpoints, MODEL_ENDPOINT_FIX } = require("./copilot_wire_api.cjs");
const { loadModelAliasMap, resolveEngineModel, normalizeForCopilotCLI, ModelAliasResolutionError, recordModelResolution, splitModelIdentifier } = require("./resolve_model_alias.cjs");
const { COPILOT_MODEL_VERIFICATION_EXIT_CODE, createCopilotModelObserver, readCopilotModelSubstitution, recordCopilotModelSelection } = require("./copilot_model_selection.cjs");
const { getEngineSubAgentTarget } = require("./extract_inline_sub_agents.cjs");
const { inlineArtifactTarget } = require("./inline_artifact_target.cjs");
const MODEL_NOT_SUPPORTED_PATTERN = /The requested model is not supported|No model available\b[^\n]*policy enablement/i;
const { parseAICreditsErrorInfoFromAuditLog, parseMaxAICreditsFromAuditLog, parseMaxAICreditsExceededFromAuditLog, parseAPIProxyGuardRejectionFromEventLog, formatAPIProxyGuardRejection } = require("./ai_credits_context.cjs");

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
// Also matches a standalone "400 Bad Request" line emitted by the Copilot CLI, and
// "400 400 400 no model endpoints available given user constraints" which is emitted
// by the Copilot SDK when no model endpoints are available for the user's configured constraints.
// Also matches "400 400 400 stream_options: Extra inputs are not permitted" which is emitted when
// the Copilot SDK sends an OpenAI-only field to an Anthropic-type provider.
// The non-first alternatives are anchored to a leading "400" to avoid false positives from unrelated
// diagnostic or informational messages that might contain the phrase.
const HTTP_400_RESPONSE_ERROR_PATTERN =
  /(?:Response status code does not indicate success:\s*400(?:\s*\(Bad Request\))?|(?:^|\r?\n)[ \t]*400 Bad Request[ \t]*(?=\r?\n|$)|400[^\n]*no model endpoints available given user constraints|400[^\n]*stream_options:\s*Extra inputs are not permitted)/i;

// Pattern to detect MCP servers blocked by enterprise/organization policy.
// This is a persistent policy configuration error — retrying will not help.
const MCP_POLICY_BLOCKED_PATTERN = /MCP servers were blocked by policy:/;

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
const TOOL_CALL_ID_SCHEMA_ERROR_PATTERN = /400\s+Invalid ['"]input\[\d+\]\.id['"]:\s*['"]ctc_call_[^'"\s]+['"]\.\s*Expected an ID that begins with ['"]fc['"]/i;
// A truncated completion stream must not be resumed with incomplete tool-call history.
const INCOMPLETE_TOOL_CALL_STREAM_PATTERN = /Failed to finalize chat-completions stream:\s*missing choices\[\d+\]\.tool_calls\[\d+\]\.id\b/;
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

function isToolCallIdSchemaError(output) {
  return TOOL_CALL_ID_SCHEMA_ERROR_PATTERN.test(output);
}

/** @param {string} output @returns {boolean} */
function isIncompleteToolCallStreamError(output) {
  return INCOMPLETE_TOOL_CALL_STREAM_PATTERN.test(output);
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
 * Resolve native command-line model overrides before alias and endpoint inference.
 * @param {string[]} args
 * @param {string} defaultModel
 */
function resolveCopilotInvocationModel(args, defaultModel) {
  let model = defaultModel;
  let hasOverride = false;
  for (let i = 0; i < args.length && args[i] !== "--"; i++) {
    if (args[i] === "--prompt" || args[i] === "-p") {
      i++;
      continue;
    }
    if (args[i] !== "--model" && !args[i].startsWith("--model=")) continue;
    const value = args[i] === "--model" ? args[++i] : args[i].slice("--model=".length);
    if (!value?.trim() || value.startsWith("-")) throw new Error("Copilot --model requires a non-empty model value");
    if (hasOverride && model !== value.trim()) throw new Error("Conflicting Copilot --model arguments; specify one invocation model");
    model = value.trim();
    hasOverride = true;
  }
  return { model, hasOverride };
}

/** @param {string[]} args @param {string} resolvedModel */
function applyCopilotInvocationModel(args, resolvedModel) {
  const result = [];
  let options = true;
  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--") options = false;
    if (options && (args[i] === "--prompt" || args[i] === "-p")) {
      result.push(args[i], args[++i]);
      continue;
    }
    if (options && args[i] === "--model") {
      result.push("--model", resolvedModel);
      i++;
    } else result.push(options && args[i].startsWith("--model=") ? `--model=${resolvedModel}` : args[i]);
  }
  return result;
}

/** Bind native observation and retries to an exact CLI session, never shared-HOME recency.
 * The pinned CLI's --session-id resumes existing UUIDs and creates only missing UUIDs.
 * @param {string[]} args
 * @param {string | undefined} continuedSessionId
 */
function prepareCopilotModelSession(args, continuedSessionId) {
  let sessionId = "";
  let selector = false;
  for (let i = 0; i < args.length && args[i] !== "--"; i++) {
    if (args[i] === "--prompt" || args[i] === "-p") {
      i++;
      continue;
    }
    if (args[i] === "--continue" || args[i] === "--connect" || args[i].startsWith("--connect=")) {
      throw new Error("Copilot model verification requires an exact --session-id UUID instead of --continue or --connect");
    }
    if (["--session-id", "--resume", "-r"].includes(args[i]) || /^(?:--session-id|--resume)=/.test(args[i])) {
      if (selector) throw new Error("Conflicting Copilot session selectors; specify one exact --session-id UUID");
      selector = true;
      sessionId = args[i].includes("=") ? args[i].slice(args[i].indexOf("=") + 1) : args[++i];
      if (!/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i.test(sessionId || "")) {
        throw new Error("Copilot model verification requires an exact session UUID; resume names and ID prefixes are ambiguous");
      }
    }
  }
  if (selector) return { args, sessionId };
  sessionId = continuedSessionId || require("crypto").randomUUID();
  const delimiter = args.indexOf("--");
  const position = delimiter < 0 ? args.length : delimiter;
  return { args: [...args.slice(0, position), "--session-id", sessionId, ...args.slice(position)], sessionId };
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

  let resolvedModel;
  try {
    const result = await resolveEngineModel({
      engine: "copilot",
      role: "main",
      configuredModel,
      aliasMap: aliasMap ?? loadModelAliasMap({ reflectData: options.awfReflectData }),
      reflectData: options.awfReflectData,
      refetchReflectData: options.refetchReflectData,
      logPrefix: "copilot model alias resolution",
      normalize: r => normalizeForCopilotCLI(r.resolvedModel),
      logger,
    });
    resolvedModel = result.engineModel;
    if (result.alias && resolvedModel !== configuredModel) {
      logger(`copilot model alias resolution: '${configuredModel}' -> '${resolvedModel}'`);
    }
  } catch (failure) {
    if (!(failure instanceof ModelAliasResolutionError)) {
      throw failure;
    }
    logger(`copilot model alias resolution failed: ${failure.message} after a bounded refresh — refusing to start Copilot with an unresolved alias`);
    if (failure.reason === "model_unavailable") {
      recordCopilotModelSelection({ status: "unavailable", configured_model: configuredModel, resolved_model: "", effective_model: "" });
    }
    process.exit(1);
    return configuredModel; // unreachable, keeps TypeScript control-flow analysis happy
  }

  if (resolvedModel && resolvedModel !== configuredModel) {
    process.env.COPILOT_MODEL = resolvedModel;
  }
  return resolvedModel || configuredModel;
}

/**
 * The AWF /reflect provider names a `<provider>/` model prefix refers to, or null for a bare
 * model id. Copilot prefixes, including the SDK's wire-API-qualified names
 * (`copilot-responses/…`, `copilot-completions/…`), refer to the GitHub provider aliases.
 * @param {string} prefix
 * @returns {Set<string>|null}
 */
function reflectProvidersForModelPrefix(prefix) {
  const normalized = normalizeReflectProviderName(prefix);
  if (!normalized) return null;
  if (isCopilotProviderName(normalized) || REFLECT_PROVIDER_ALIASES.github.has(normalized)) return REFLECT_PROVIDER_ALIASES.github;
  const unsuffixed = normalized.replace(/-\d+$/, "");
  const known = [normalized, unsuffixed].find(name => Object.prototype.hasOwnProperty.call(REFLECT_PROVIDER_ALIASES, name));
  return known ? REFLECT_PROVIDER_ALIASES[known] : new Set([normalized, unsuffixed]);
}

/**
 * Split a configured model into its model id and the /reflect providers it is scoped to.
 * Model parameters (`?effort=…`) are ignored.
 * @param {string} model
 * @returns {{ id: string, prefix: string, providers: Set<string>|null }}
 */
function scopeModelToReflectProviders(model) {
  const { base } = splitModelIdentifier(model);
  const slash = base.indexOf("/");
  if (slash <= 0) return { id: base, prefix: "", providers: null };
  const prefix = base.slice(0, slash);
  return { id: base.slice(slash + 1), prefix, providers: reflectProvidersForModelPrefix(prefix) };
}

/**
 * Whether a model is listed on a configured AWF /reflect endpoint. A bare id may be listed by
 * any provider; a `<provider>/`-prefixed id only by that provider.
 * @param {string} model
 * @param {any} reflectData
 * @returns {boolean}
 */
function isModelListedOnConfiguredReflectEndpoint(model, reflectData) {
  const { id, providers } = scopeModelToReflectProviders(model);
  return Boolean(id) && isModelAvailableInReflectData(id, reflectData, providers);
}

/**
 * Classify why a configured model is not listed in AWF /reflect data: `catalog_unavailable`
 * when discovery is missing or incomplete for the model's providers (no /reflect data, no
 * configured endpoint, `models_fetch_complete: false`, or a relevant endpoint with a missing
 * or empty model list); otherwise `model_unavailable`.
 * @param {string} model
 * @param {any} reflectData
 * @returns {"catalog_unavailable" | "model_unavailable"}
 */
function classifyUnlistedReflectModel(model, reflectData) {
  const { providers } = scopeModelToReflectProviders(model);
  const configuredEndpoints = (Array.isArray(reflectData?.endpoints) ? reflectData.endpoints : []).filter(endpoint => endpoint?.configured === true);
  const relevantEndpoints = configuredEndpoints.filter(endpoint => !providers || providers.has(normalizeReflectProviderName(endpoint.provider)));
  const incomplete = !reflectData || reflectData.models_fetch_complete === false || configuredEndpoints.length === 0 || relevantEndpoints.some(endpoint => !Array.isArray(endpoint.models) || endpoint.models.length === 0);
  return incomplete ? "catalog_unavailable" : "model_unavailable";
}

/**
 * Resolve the Copilot SDK multi-provider configuration from AWF /reflect data.
 *
 * Without a router selection, a configured model must be listed on a configured /reflect
 * endpoint (scoped to its provider when prefixed). When it is not, or no catalogue is
 * available, one bounded /reflect refresh is attempted; if the model is still absent, this
 * records the failure and stops the process before the SDK starts rather than running a
 * different model (see https://github.com/github/gh-aw/issues/70317). A `model_unavailable`
 * result from a complete initial catalogue wins over a degraded refresh, as in
 * `resolveConfiguredModelWithRefresh`.
 *
 * @param {{
 *   configuredModel: string,
 *   requestedModel?: string,
 *   routed?: boolean,
 *   wireApi?: string,
 *   reflectData: any,
 *   modelsJson?: object | null,
 *   refetchReflectData?: () => Promise<object|null>,
 *   logger?: (msg: string) => void,
 * }} options `configuredModel` is the model after alias resolution; `requestedModel` is the
 *   model as configured before alias resolution (used in the failure record and message).
 * @returns {Promise<ReturnType<typeof resolveMultiProviderFromReflect>>}
 */
async function resolveCopilotSDKMultiProvider(options) {
  const logger = options.logger || log;
  const configuredModel = String(options.configuredModel || "").trim();
  const resolve = reflectData => resolveMultiProviderFromReflect({ model: configuredModel, wireApi: options.wireApi, reflectData, modelsJson: options.modelsJson, logger });
  const isUnavailable = (multiProvider, reflectData) => (!multiProvider || Boolean(multiProvider.configuredModelNotFound)) && !isModelListedOnConfiguredReflectEndpoint(configuredModel, reflectData);
  const initialReflectData = options.reflectData;
  let multiProvider = resolve(initialReflectData);
  if (options.routed || !configuredModel || !isUnavailable(multiProvider, initialReflectData)) {
    return multiProvider;
  }

  logger(`copilot-sdk driver mode: configured model '${configuredModel}' is not listed on any configured awf-reflect endpoint; refreshing awf-reflect once`);
  const refreshed = options.refetchReflectData ? await options.refetchReflectData() : null;
  if (refreshed) {
    multiProvider = resolve(refreshed);
    if (!isUnavailable(multiProvider, refreshed)) {
      return multiProvider;
    }
  }

  const initialReason = classifyUnlistedReflectModel(configuredModel, initialReflectData);
  const refreshedReason = refreshed ? classifyUnlistedReflectModel(configuredModel, refreshed) : initialReason;
  const reason = initialReason === "model_unavailable" ? initialReason : refreshedReason;
  // Report the catalogue the failure reason came from: the latest one, unless only the initial one was complete.
  const reflectData = refreshed && (reason === "catalog_unavailable" || refreshedReason === "model_unavailable") ? refreshed : initialReflectData;
  const requestedModel = String(options.requestedModel || configuredModel).trim();
  const requestedScope = scopeModelToReflectProviders(requestedModel);
  const failure = new ModelAliasResolutionError(requestedModel, reason, { provider: requestedScope.providers === REFLECT_PROVIDER_ALIASES.github ? "copilot" : requestedScope.prefix });
  const checked = (Array.isArray(reflectData?.endpoints) ? reflectData.endpoints : [])
    .filter(endpoint => endpoint?.configured === true)
    .map(endpoint => `${String(endpoint.provider || "unknown")} (${formatInferenceEndpointForLog(endpoint.base_url || endpoint.models_url)}, ${Array.isArray(endpoint.models) ? `${endpoint.models.length} models` : "no model list"})`)
    .join(", ");
  const resolvedNote = configuredModel !== requestedModel ? ` (resolved to '${configuredModel}')` : "";
  logger(
    `copilot-sdk driver mode: ${failure.message} Configured model '${requestedModel}'${resolvedNote} is not listed on the configured awf-reflect endpoints checked: ${checked || "(none)"}. Refusing to start the Copilot SDK with a different model.`
  );
  recordModelResolution({ engine: "copilot", role: "main", configured_model: requestedModel, resolved_model: null, provider: failure.provider || null, status: "failed", cause: failure.reason });
  if (failure.reason === "model_unavailable") {
    recordCopilotModelSelection({ status: "unavailable", configured_model: requestedModel, resolved_model: "", effective_model: "" });
  }
  process.exit(1);
  return null; // unreachable, keeps TypeScript control-flow analysis happy
}

/**
 * Read the Copilot custom agent files in one directory, one entry per regular `.agent.md` file.
 * The name is the frontmatter `name` or the file name without `.agent.md`;
 * `model` is the trimmed frontmatter `model` string, when declared.
 * @param {string} directory
 * @param {(msg: string) => void} [logger]
 * @returns {Array<{ name: string, model?: string, path: string }>}
 */
function listCopilotAgentFiles(directory, logger = log) {
  const { extractFrontmatterAndBody } = require("./frontmatter_hash_pure.cjs");
  const { parseDocument } = require("./work_queue_yaml.cjs");
  let entries;
  try {
    entries = fs.readdirSync(directory, { withFileTypes: true });
  } catch (error) {
    if (!(error instanceof Error && "code" in error && error.code === "ENOENT")) logger(`warning: cannot read custom agents in ${directory}: ${getErrorMessage(error)}`);
    return [];
  }
  const agents = [];
  for (const entry of entries.sort((a, b) => a.name.localeCompare(b.name))) {
    if (!entry.isFile() || !entry.name.endsWith(".agent.md")) continue;
    const agentPath = path.join(directory, entry.name);
    try {
      const { frontmatterText } = extractFrontmatterAndBody(fs.readFileSync(agentPath, "utf8"));
      /** @type {any} */
      const document = parseDocument(frontmatterText);
      if (!document || document.errors.length) throw new Error("cannot parse agent frontmatter");
      const frontmatter = document.toJS() || {};
      if (typeof frontmatter !== "object" || Array.isArray(frontmatter)) throw new Error("agent frontmatter must be a mapping");
      const name = typeof frontmatter.name === "string" && frontmatter.name.trim() ? frontmatter.name.trim() : entry.name.slice(0, -".agent.md".length);
      agents.push({ name, path: agentPath, ...(typeof frontmatter.model === "string" ? { model: frontmatter.model.trim() } : {}) });
    } catch (error) {
      logger(`warning: cannot read custom agent ${agentPath}: ${getErrorMessage(error)}`);
    }
  }
  return agents;
}

/**
 * List the Copilot sub-agents of a run. Repository agents come from
 * `<workingDirectory>/.github/agents`; workflow-declared inline sub-agents come from the
 * directory they are staged in under `agentsBaseDir` (the one `loadCopilotSDKCustomAgents`
 * reads). An inline sub-agent overrides a repository agent with the same name and is declared.
 * @param {string} workingDirectory
 * @param {string} agentsBaseDir
 * @param {(msg: string) => void} [logger]
 * @returns {Array<{ name: string, model?: string, path: string, declared: boolean }>}
 */
function listCopilotSubagents(workingDirectory, agentsBaseDir, logger = log) {
  const declaredDir = path.resolve(agentsBaseDir, inlineArtifactTarget("SUB_AGENT", getEngineSubAgentTarget("copilot")).dir);
  const repositoryDir = path.resolve(workingDirectory, ".github", "agents");
  const agents = new Map();
  if (repositoryDir !== declaredDir) {
    for (const agent of listCopilotAgentFiles(repositoryDir, logger)) agents.set(agent.name, { ...agent, declared: false });
  }
  for (const agent of listCopilotAgentFiles(declaredDir, logger)) agents.set(agent.name, { ...agent, declared: true });
  return [...agents.values()];
}

/**
 * Locate the top-level YAML model scalar in a Copilot agent file.
 * @param {string} content
 * @returns {{ value: string, source: string, start: number, end: number }}
 */
function getCopilotAgentModelScalar(content) {
  const { extractFrontmatterAndBody } = require("./frontmatter_hash_pure.cjs");
  const { parseDocument } = require("./work_queue_yaml.cjs");
  const { frontmatterText } = extractFrontmatterAndBody(content);
  /** @type {any} */
  const document = parseDocument(frontmatterText);
  if (!document || document.errors.length || !document.contents || !Array.isArray(document.contents.items)) {
    throw new Error("cannot parse agent frontmatter");
  }
  const pairs = document.contents.items.filter(pair => pair?.key?.value === "model");
  if (pairs.length !== 1 || typeof pairs[0].value?.value !== "string" || !Array.isArray(pairs[0].value.range)) {
    throw new Error("expected exactly one top-level string 'model:' scalar in the frontmatter");
  }
  const frontmatterStart = content.indexOf("\n") + 1;
  const start = frontmatterStart + pairs[0].value.range[0];
  const end = frontmatterStart + pairs[0].value.range[1];
  return { value: pairs[0].value.value, source: content.slice(start, end), start, end };
}

/**
 * Replace the complete YAML model scalar, including any block-scalar continuation lines.
 * @param {string} content
 * @param {string} model
 * @returns {string}
 */
function replaceCopilotAgentFileModelContent(content, model) {
  const scalar = getCopilotAgentModelScalar(content);
  const lineEnding = scalar.source.match(/(?:\r?\n)+$/)?.[0] || "";
  return content.slice(0, scalar.start) + JSON.stringify(model) + lineEnding + content.slice(scalar.end);
}

/**
 * Write a copy of a Copilot agent file to Copilot CLI's personal agents directory with only
 * the frontmatter `model:` scalar replaced. The copy keeps the file name, which Copilot CLI
 * uses as the agent ID, so it overrides the repository agent with the same ID. The source
 * file is never modified.
 * @param {string} agentPath
 * @param {string} personalAgentsDir
 * @param {string} model
 * @returns {string} The path of the copy.
 */
function writeCopilotPersonalAgentCopy(agentPath, personalAgentsDir, model) {
  let content;
  try {
    content = fs.readFileSync(agentPath, "utf8");
  } catch (error) {
    throw new Error(`cannot read agent file ${agentPath}: ${getErrorMessage(error)}`, { cause: error });
  }
  const updated = replaceCopilotAgentFileModelContent(content, model);
  const target = path.join(personalAgentsDir, path.basename(agentPath));
  try {
    fs.mkdirSync(personalAgentsDir, { recursive: true });
    fs.writeFileSync(target, updated);
  } catch (error) {
    throw new Error(`cannot write personal agent ${target}: ${getErrorMessage(error)}`, { cause: error });
  }
  return target;
}

/**
 * Resolve the declared models of Copilot sub-agents before Copilot starts, in CLI and SDK
 * mode and on routed runs. Each `model:` goes through the shared resolver (Copilot provider
 * scope, the workflow alias map, `normalizeForCopilotCLI`) and the live-catalogue check that
 * the main SDK model uses. With `providerConfig` (SDK mode), the resolved id must also qualify
 * against the SDK provider models. A sub-agent without `model:` inherits the main model and is
 * not recorded.
 *
 * Every sub-agent with a model gets a `model.resolution` record (role = agent name), so
 * several sub-agents sharing one dead model each get a `failed` record. Workflow-declared
 * sub-agents (the default) fail closed: when any of them fails, the first failure is thrown
 * as a ModelAliasResolutionError after all records are written; callers must not start
 * Copilot. A repository agent (`declared: false`) that fails gets a warning and a `failed`
 * record, is left out of the result, and does not stop the run. `/reflect` is refreshed at
 * most once.
 *
 * @param {{
 *   agents: Array<{ name: string, model?: string, path?: string, declared?: boolean }>,
 *   reflectData: any,
 *   refetchReflectData?: () => Promise<any>,
 *   providerConfig?: { providers: Array<{name: string}>, models: Array<{id: string, provider: string}> } | null,
 *   refreshProviderConfig?: (reflectData: any) => Promise<{ providers: Array<{name: string}>, models: Array<{id: string, provider: string}> } | null>,
 *   aliasMap?: Record<string, string[]> | null,
 *   repositoryFallback?: string,
 *   env?: NodeJS.ProcessEnv,
 *   logger?: (msg: string) => void,
 * }} options `repositoryFallback` describes what happens to a repository agent whose model fails.
 * @returns {Promise<Array<{ name: string, configuredModel: string, resolvedModel: string, model: string }>>}
 *   `resolvedModel` is the resolved Copilot id; `model` is the id to hand to Copilot (qualified in SDK mode).
 */
async function resolveCopilotSubagentModels(options) {
  const logger = options.logger || log;
  const env = options.env || process.env;
  const declared = options.agents.filter(agent => typeof agent.model === "string" && agent.model.trim() && agent.model.trim().toLowerCase() !== "inherited");
  if (declared.length === 0) return [];
  const aliasMap = options.aliasMap !== undefined ? options.aliasMap : (loadAwfConfigData()?.apiProxy?.models ?? loadModelAliasMap({ reflectData: options.reflectData, env }));
  let reflectData = options.reflectData;
  let providerConfig = options.providerConfig;
  /** @type {Promise<any> | null} */
  let refresh = null;
  const refreshOnce = () => {
    refresh ??= (options.refetchReflectData ? options.refetchReflectData() : Promise.resolve(null)).then(refreshed => {
      if (refreshed) {
        reflectData = refreshed;
        if (options.refreshProviderConfig) {
          return options.refreshProviderConfig(refreshed).then(config => {
            if (config) providerConfig = config;
            return refreshed;
          });
        }
      }
      return refreshed || null;
    });
    return refresh;
  };
  const liveCheck = env.AWF_REFLECT_ENABLED === "1" || Boolean(options.reflectData);
  /** @type {InstanceType<typeof ModelAliasResolutionError>[]} */
  const failures = [];
  const resolutions = [];
  for (const agent of declared) {
    const configuredModel = String(agent.model).trim();
    const repositoryAgent = agent.declared === false;
    // A repository agent the workflow may never use must not make the run look like an
    // unsupported-model failure, so its resolver failure is reported by the warning below.
    const agentLogger = repositoryAgent ? (/** @type {string} */ message) => (MODEL_NOT_SUPPORTED_PATTERN.test(message) ? undefined : logger(message)) : logger;
    const scope = scopeModelToReflectProviders(configuredModel);
    const fail = (/** @type {InstanceType<typeof ModelAliasResolutionError>} */ failure, /** @type {string} */ detail) => {
      recordModelResolution({ engine: "copilot", role: agent.name, configured_model: configuredModel, resolved_model: null, provider: failure.provider || null, status: "failed", cause: failure.reason }, env);
      if (repositoryAgent) {
        logger(
          `warning: repository agent '${agent.name}' (${agent.path || "unknown file"}) declares model '${configuredModel}', which could not be resolved (cause: ${failure.reason}); ${options.repositoryFallback || "the agent is left unchanged"}.`
        );
        return;
      }
      logger(`copilot sub-agent '${agent.name}': ${failure.message}${detail ? ` ${detail}` : ""} Refusing to start Copilot with a different sub-agent model.`);
      failures.push(failure);
    };
    let result;
    try {
      result = await resolveEngineModel({
        engine: "copilot",
        role: agent.name,
        configuredModel,
        aliasMap,
        reflectData,
        provider: "copilot",
        refetchReflectData: refreshOnce,
        logPrefix: `copilot sub-agent '${agent.name}' model alias resolution`,
        normalize: r => normalizeForCopilotCLI(r.resolvedModel),
        env,
        logger: agentLogger,
      });
    } catch (failure) {
      if (!(failure instanceof ModelAliasResolutionError)) throw failure;
      fail(failure, "");
      continue;
    }
    const resolvedModel = result.engineModel;
    const resolvedNote = resolvedModel !== configuredModel ? ` (resolved to '${resolvedModel}')` : "";
    const failureProvider = scope.providers && scope.providers !== REFLECT_PROVIDER_ALIASES.github ? scope.prefix : "copilot";
    const scopedResolvedModel = scope.prefix ? `${scope.prefix}/${resolvedModel}` : resolvedModel;
    if (liveCheck && !isModelListedOnConfiguredReflectEndpoint(scopedResolvedModel, reflectData)) {
      const initialReason = classifyUnlistedReflectModel(scopedResolvedModel, reflectData);
      const refreshed = await refreshOnce();
      if (!refreshed || !isModelListedOnConfiguredReflectEndpoint(scopedResolvedModel, refreshed)) {
        const reason = initialReason === "model_unavailable" || !refreshed ? initialReason : classifyUnlistedReflectModel(scopedResolvedModel, refreshed);
        fail(new ModelAliasResolutionError(configuredModel, reason, { provider: failureProvider }), `Sub-agent model '${configuredModel}'${resolvedNote} is not listed on the configured awf-reflect endpoints.`);
        continue;
      }
    }
    let model = resolvedModel;
    if (providerConfig) {
      let qualified;
      try {
        const qualificationModel = scope.providers === REFLECT_PROVIDER_ALIASES.github ? `copilot/${resolvedModel}` : scope.prefix ? `${scope.prefix}/${resolvedModel}` : resolvedModel;
        qualified = qualifyModelForMultiProvider(qualificationModel, providerConfig);
      } catch (error) {
        fail(
          new ModelAliasResolutionError(configuredModel, "model_unavailable", { provider: failureProvider }),
          `Sub-agent model '${configuredModel}'${resolvedNote} could not be qualified against the Copilot SDK provider models: ${getErrorMessage(error)}`
        );
        continue;
      }
      if (!qualified) {
        fail(
          new ModelAliasResolutionError(configuredModel, "model_unavailable", { provider: failureProvider }),
          `Sub-agent model '${configuredModel}'${resolvedNote} is not in the Copilot SDK provider models (models without a supported wire API are skipped).`
        );
        continue;
      }
      model = qualified;
    }
    logger(`copilot sub-agent '${agent.name}': model '${configuredModel}' -> '${model}'`);
    resolutions.push({ name: agent.name, configuredModel, resolvedModel, model });
  }
  if (failures.length > 0) throw failures[0];
  return resolutions;
}

/**
 * CLI mode: resolve the sub-agent models of the agents Copilot CLI loads (repository agents
 * in the engine working directory and workflow-declared inline sub-agents) and, for each
 * resolved id that differs from the declared value, write a copy of the agent file with the
 * resolved `model:` to Copilot CLI's personal agents directory
 * (`${COPILOT_HOME:-$HOME/.copilot}/agents`). Files in the checkout are never modified.
 * Stops the process before Copilot starts when a declared sub-agent model cannot be resolved
 * or its copy cannot be written; repository agents only produce warnings.
 *
 * @param {{
 *   reflectData: any,
 *   refetchReflectData?: () => Promise<any>,
 *   workingDirectory?: string,
 *   agentsBaseDir?: string,
 *   personalAgentsDir?: string,
 *   env?: NodeJS.ProcessEnv,
 *   logger?: (msg: string) => void,
 * }} options
 * @returns {Promise<Array<{ name: string, configuredModel: string, resolvedModel: string, model: string }>>}
 */
async function applyCopilotCLISubagentModels(options) {
  const logger = options.logger || log;
  const env = options.env || process.env;
  const workingDirectory = options.workingDirectory || path.resolve(env.GITHUB_WORKSPACE || process.cwd(), env.GH_AW_ENGINE_CWD || ".");
  const agentsBaseDir = options.agentsBaseDir || path.join(os.tmpdir(), "gh-aw");
  const personalAgentsDir = options.personalAgentsDir || path.join(env.COPILOT_HOME || path.join(env.HOME || os.homedir(), ".copilot"), "agents");
  const agents = listCopilotSubagents(workingDirectory, agentsBaseDir, logger);
  let resolutions;
  try {
    resolutions = await resolveCopilotSubagentModels({
      agents,
      reflectData: options.reflectData,
      refetchReflectData: options.refetchReflectData,
      repositoryFallback: "Copilot CLI uses the agent file unchanged",
      env,
      logger,
    });
  } catch (failure) {
    if (!(failure instanceof ModelAliasResolutionError)) throw failure;
    process.exit(1);
    return []; // unreachable, keeps TypeScript control-flow analysis happy
  }
  for (const resolution of resolutions) {
    if (resolution.model === resolution.configuredModel) continue;
    const agent = agents.find(candidate => candidate.name === resolution.name);
    if (!agent) continue;
    try {
      const target = writeCopilotPersonalAgentCopy(agent.path, personalAgentsDir, resolution.model);
      logger(`copilot sub-agent '${resolution.name}': wrote model '${resolution.model}' to personal agent ${target}`);
    } catch (error) {
      if (!agent.declared) {
        logger(
          `warning: repository agent '${resolution.name}' (${agent.path}): cannot write personal agent copy with model '${resolution.model}' to ${personalAgentsDir}: ${getErrorMessage(error)}; Copilot CLI uses the agent file unchanged.`
        );
        continue;
      }
      logger(`copilot sub-agent '${resolution.name}': cannot write personal agent copy of ${agent.path} to ${personalAgentsDir}: ${getErrorMessage(error)}. Refusing to start Copilot with an unresolved sub-agent model.`);
      process.exit(1);
      return []; // unreachable, keeps TypeScript control-flow analysis happy
    }
  }
  return resolutions;
}

/**
 * Configure and validate COPILOT_PROVIDER_WIRE_API using AWF endpoint metadata.
 *
 * @param {{
 *   modelsJson: Record<string, unknown> | null,
 *   awfReflectData?: any,
 *   configuredModel?: string,
 *   overrideSource?: string,
 *   logger?: (msg: string) => void,
 * }} options
 */
function applyCopilotWireAPI(options) {
  return configureCopilotWireAPI({ logger: log, ...options });
}

/**
 * Apply the model and API endpoint selected by AWF, bypassing compile-time model resolution.
 * @param {{wire_model: string, effort: string|null, endpoint: string, selected_endpoint: string}} selection
 * @param {(msg: string) => void} [logger]
 */
function applyCopilotRoutingSelection(selection, logger = log) {
  const wireApi = selection.endpoint === "/responses" ? "responses" : "completions";
  process.env.GH_AW_MODEL_ROUTING = "1";
  process.env.COPILOT_MODEL = selection.wire_model;
  process.env.COPILOT_PROVIDER_WIRE_API = wireApi;
  if (selection.effort) {
    process.env.GH_AW_COPILOT_ROUTING_EFFORT = selection.effort;
  } else {
    delete process.env.GH_AW_COPILOT_ROUTING_EFFORT;
  }
  recordAWFModelRoutingOutcome({
    status: "selected",
    wire_model: selection.wire_model,
    endpoint: selection.endpoint,
    selected_endpoint: selection.selected_endpoint,
    effort: selection.effort,
    applied_effort: selection.effort,
  });
  logger(`inference routing: mode=awf-routed model=${selection.wire_model} effort=${selection.effort || "(unset)"} wire_api=${wireApi}`);
}

/**
 * Resolve and record the Copilot routing decision used by the harness.
 * @param {any} reflectData
 * @param {boolean} routingRequired
 * @param {(msg: string) => void} [logger]
 * @returns {{selection: any, error: string|null}}
 */
function resolveCopilotModelRouting(reflectData, routingRequired, logger = log) {
  const result = resolveAWFModelRoutingSelection(reflectData, routingRequired, COPILOT_ROUTING_POLICY.endpoints, COPILOT_ROUTING_POLICY.allowEndpointOverride);
  if (result.error) {
    recordAWFModelRoutingOutcome({
      status: reflectData?.routing?.status === "failed" ? "failed" : "rejected",
      failure_code: reflectData?.routing?.failure_code || getAWFModelRoutingFailureCode(result.error),
      detail: result.error,
    });
    return result;
  }
  if (result.selection) applyCopilotRoutingSelection(result.selection, logger);
  return result;
}

/**
 * Remove caller model/effort overrides and apply the AWF selection to a CLI invocation.
 * @param {string[]} args
 * @param {{wire_model: string, effort: string|null}} selection
 * @returns {string[]}
 */
function applyCopilotRoutingArgs(args, selection) {
  const routedArgs = [];
  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (arg === "--model" || arg === "--reasoning-effort") {
      i++;
      continue;
    }
    if (arg.startsWith("--model=") || arg.startsWith("--reasoning-effort=")) {
      continue;
    }
    routedArgs.push(arg);
  }
  routedArgs.push("--model", selection.wire_model);
  if (selection.effort) routedArgs.push("--reasoning-effort", selection.effort);
  return routedArgs;
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

/** @param {string} output @returns {boolean} */
function isCopilotContentFilterError(output) {
  for (const event of iterateJsonl(output)) {
    if (event?.type !== "assistant.message") continue;
    const context = copilotSessionContext(event);
    if (context.agentId === undefined && !context.parentToolUseId && getCopilotMessageRefusal(event.data)?.reason === "content_filter") return true;
  }
  return false;
}

/**
 * Classify a failed Copilot attempt into a short, named failure class.
 * @param {{
 *   hasOutput: boolean,
 *   isContentFiltered?: boolean,
 *   isAuthErr?: boolean,
 *   isAuthenticationFailed?: boolean,
 *   isTransientCAPIError?: boolean,
 *   isMCPGatewayShutdown?: boolean,
 *   isMCPPolicy?: boolean,
 *   isModelNotSupported?: boolean,
 *   isHTTP400ResponseError?: boolean,
 *   isToolCallIdSchemaError?: boolean,
 *   isIncompleteToolCallStream?: boolean,
 *   isInvocationCapExceeded?: boolean,
 *   isAPIProxyGuardRejected?: boolean,
 *   isNullTypeToolCall?: boolean,
 *   isQuotaExceeded?: boolean,
 *   isCAPIServerError?: boolean,
 *   isTrustedAICreditsBudgetExhausted?: boolean,
 *   isSDKSessionIdleTimeout?: boolean,
 *   isModelRoutingFailure?: boolean,
 *   isModelEndpointMismatch?: boolean,
 *   hasNumerousPermissionDenied?: boolean,
 *   tokenCount?: number,
 * }} detection
 * @returns {string}
 */
function classifyCopilotFailure(detection) {
  if (detection.isContentFiltered) return "content_filter";
  if (detection.isModelEndpointMismatch) return "model_endpoint_mismatch";
  if (detection.isModelRoutingFailure) return "model_routing_failed";
  if (detection.isInvocationCapExceeded) return "invocation_cap_exceeded";
  if (detection.isTrustedAICreditsBudgetExhausted) return "ai_credits_exhausted";
  // An AWF API proxy guardrail rejection is a policy outcome, not a credential failure: it must
  // outrank the authentication classes because the Copilot CLI reports the proxy's HTTP 403 as
  // "Authentication failed with provider ...".
  if (detection.isAPIProxyGuardRejected) return "api_proxy_guard_rejected";
  if (detection.isQuotaExceeded) return "capi_quota_exceeded";
  if (detection.isCAPIServerError) return "capi_server_error";
  if (detection.isMCPPolicy) return "mcp_policy_blocked";
  if (detection.isModelNotSupported) return "model_not_supported";
  if (detection.isToolCallIdSchemaError) return "tool_call_id_schema_error";
  if (detection.isIncompleteToolCallStream) return "incomplete_tool_call_stream";
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
 * @param {{ exitCode: number, hasOutput: boolean, output: string, attempt: number, maxRetries: number, isModelRoutingFailure?: boolean }} params
 *   output must be the combined stdout/stderr text for the failed attempt.
 * @returns {boolean}
 */
function shouldRetryFailedExecution(params) {
  if (params.exitCode === 0) return false;
  if (isCopilotContentFilterError(params.output)) return false;
  if (params.isModelRoutingFailure) return false;
  if (hasNumerousPermissionDeniedIssues(params.output)) return false;
  if (isCAPIQuotaExceededError(params.output)) return false;
  const nonRetryableGuard = detectNonRetryableHarnessGuard(params.output);
  if (nonRetryableGuard.modelEndpointMismatch) return false;
  if (nonRetryableGuard.maxRunsExceeded) return false;
  if (nonRetryableGuard.apiProxyGuardRejection) return false;
  return params.attempt < params.maxRetries && (params.hasOutput || isCAPIServerError(params.output));
}

function shouldContinueCopilotSessionAfterFailure({ copilotSDKMode, continueDisabledPermanently, hasOutput, hasCAPIServerError }) {
  return !copilotSDKMode && !continueDisabledPermanently && (hasOutput || !hasCAPIServerError);
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
 *   routingEffort?: string|null,
 *   multiProviderJson?: string,
 *   subagentModelsJson?: string,
 * }} options `subagentModelsJson` maps custom agent names to their resolved, qualified models.
 * @returns {NodeJS.ProcessEnv}
 */
function buildCopilotSDKChildEnv({ sdkEnv, copilotSDKMode, copilotConnectionToken, providerBaseUrl, providerType, providerWireApi, resolvedModel, routingEffort, multiProviderJson, subagentModelsJson }) {
  if (!copilotSDKMode) {
    return sdkEnv;
  }
  const multiProvider = parseMultiProviderJson(multiProviderJson);
  const qualifiedModel = qualifyModelForMultiProvider(resolvedModel || multiProvider?.model, multiProvider);
  if (multiProvider && !qualifiedModel) {
    throw new Error(`Copilot SDK model '${resolvedModel || multiProvider.model}' is not available in the configured BYOK provider catalog`);
  }
  return {
    ...sdkEnv,
    COPILOT_CONNECTION_TOKEN: copilotConnectionToken,
    ...(multiProviderJson ? { GH_AW_COPILOT_SDK_MULTI_PROVIDER_JSON: multiProviderJson } : {}),
    ...(subagentModelsJson ? { GH_AW_COPILOT_SDK_SUBAGENT_MODELS: subagentModelsJson } : {}),
    COPILOT_MODEL: qualifiedModel || resolvedModel,
    ...(routingEffort ? { COPILOT_REASONING_EFFORT: routingEffort } : {}),
    // Native Copilot CLI BYOK env vars — consumed by the headless sidecar for all sessions.
    COPILOT_PROVIDER_BASE_URL: providerBaseUrl,
    COPILOT_PROVIDER_TYPE: providerType,
    ...(providerWireApi ? { COPILOT_PROVIDER_WIRE_API: providerWireApi } : {}),
  };
}

/**
 * @param {NodeJS.ProcessEnv} baseEnv
 * @param {NodeJS.ProcessEnv} sdkEnv
 * @returns {NodeJS.ProcessEnv}
 */
function buildCopilotEngineChildEnv(baseEnv, sdkEnv) {
  return stripExecutionEvidenceEnv({ ...baseEnv, ...sdkEnv });
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
 * Return whether the argument is an explicit Copilot prompt option that takes
 * the prompt from argv instead of stdin.
 * @param {string} arg
 * @returns {boolean}
 */
function isPromptOption(arg) {
  return arg === "-p" || arg === "--prompt";
}

/**
 * Return whether the argument is an explicit Copilot prompt option with an
 * inline value.
 * @param {string} arg
 * @returns {boolean}
 */
function isInlinePromptOption(arg) {
  return arg.startsWith("--prompt=") || arg.startsWith("-p=");
}

/**
 * Remove explicit prompt options so Copilot reads the streamed prompt from stdin.
 * Dash-prefixed tokens after -p/--prompt are preserved as likely Copilot options;
 * the harness does not parse Copilot's full option grammar, so dash-prefixed
 * prompt values are treated as malformed conflicting prompt args.
 * @param {string[]} args
 * @returns {string[]}
 */
function removeExplicitPromptOptions(args) {
  /** @type {string[]} */
  const filteredArgs = [];
  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (isPromptOption(arg)) {
      if (i + 1 < args.length && !args[i + 1].startsWith("-")) {
        i++;
      }
      continue;
    }
    if (isInlinePromptOption(arg)) {
      continue;
    }
    filteredArgs.push(arg);
  }
  return filteredArgs;
}

/**
 * Resolve --prompt-file arguments for the Copilot CLI.
 * Small files are inlined as -p prompt text for compatibility with older Copilot CLIs.
 * Larger files are removed from the argument list and returned as stdin data so the full
 * prompt reaches Copilot without being constrained by the operating system's argv limit.
 * @param {string[]} args
 * @returns {{args: string[], stdin?: Buffer}}
 */
function resolvePromptFileInput(args) {
  /** @type {string[]} */
  const resolvedArgs = [];
  /** @type {Buffer | undefined} */
  let promptStdin;

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
        log(`prompt file exceeds ${PROMPT_FILE_INLINE_THRESHOLD_LABEL}; streaming prompt via stdin`);
        promptStdin = fs.readFileSync(promptFile);
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

  const finalArgs = promptStdin ? removeExplicitPromptOptions(resolvedArgs) : resolvedArgs;
  return { args: finalArgs, stdin: promptStdin };
}

/**
 * Main entry point: run copilot with retry logic for partially-executed sessions.
 */
async function main() {
  const executionEvidence = captureExecutionEvidenceConfiguration(process.env);
  stripExecutionEvidenceEnv(process.env);
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
  // In CLI mode, prompt-file resolution may return either inline prompt text or stdin data.
  let resolvedArgs;
  /** @type {Buffer | undefined} */
  let promptStdin;
  if (copilotSDKMode) {
    resolvedArgs = args;
  } else {
    const resolvedPrompt = resolvePromptFileInput(args);
    const fallbackConfigPath = !process.env.GH_AW_AWF_CONFIG_PATH && process.env.RUNNER_TEMP ? path.join(process.env.RUNNER_TEMP, "gh-aw", "awf-config.json") : undefined;
    const configPath = process.env.GH_AW_AWF_CONFIG_PATH || AWF_CONFIG_PATH;
    resolvedArgs = addCopilotURLPermissions(resolvedPrompt.args, { sdkMode: copilotSDKMode, configPath, fallbackConfigPath });
    promptStdin = resolvedPrompt.stdin;
  }

  // Fetch AWF API proxy reflection data before running the agent.
  // In SDK/BYOK mode the live data is used immediately to resolve the custom provider
  // configuration that is injected into the driver subprocess environment.
  // Skip when AWF_REFLECT_ENABLED is not "1" (e.g. sandbox.agent: false — no api-proxy running).
  /** @type {any} */
  let awfReflectData = null;
  const invocationModel = copilotSDKMode ? null : resolveCopilotInvocationModel(resolvedArgs, process.env.COPILOT_MODEL || "");
  if (invocationModel?.hasOverride) process.env.COPILOT_MODEL = invocationModel.model;
  const configuredCopilotModel = process.env.COPILOT_MODEL || "";
  const configuredWireAPI = process.env.COPILOT_PROVIDER_WIRE_API;
  const modelRoutingRequired = process.env.GH_AW_MODEL_ROUTING === "1";
  /** @type {any} */
  let modelRoutingSelection = null;
  if (process.env.AWF_REFLECT_ENABLED === "1" || modelRoutingRequired) {
    const reflectResult = await fetchAWFReflect({ logger: log });
    if (reflectResult.ok && reflectResult.reflectData) {
      awfReflectData = reflectResult.reflectData;
    }
  }

  // One bounded /reflect refresh, shared by alias resolution and the SDK model liveness check.
  const refetchReflectData = async () => {
    if (process.env.AWF_REFLECT_ENABLED !== "1") {
      return null;
    }
    const refreshed = await fetchAWFReflect({ logger: log });
    if (refreshed.ok && refreshed.reflectData) awfReflectData = refreshed.reflectData;
    return refreshed.ok && refreshed.reflectData ? refreshed.reflectData : null;
  };

  const routingResult = resolveCopilotModelRouting(awfReflectData, modelRoutingRequired, log);
  if (routingResult.error) {
    log(`unexpected error: AWF model routing failed: ${routingResult.error}; refusing to start Copilot`);
    process.exit(1);
    return;
  }
  modelRoutingSelection = routingResult.selection;
  // Model-selection evidence records the requested identifier: `auto` for routed runs,
  // otherwise the configured model or alias (or the fallback) before alias resolution.
  let selectionConfiguredModel = configuredCopilotModel;
  if (modelRoutingSelection) {
    selectionConfiguredModel = "auto";
    if (!copilotSDKMode) {
      resolvedArgs = applyCopilotRoutingArgs(resolvedArgs, modelRoutingSelection);
      if (configuredWireAPI) process.env.COPILOT_PROVIDER_WIRE_API = configuredWireAPI;
    }
  } else {
    selectionConfiguredModel = applyModelFallback(process.env, "COPILOT_MODEL", log) || configuredCopilotModel;
    await applyCopilotModelAliasResolution({
      awfReflectData,
      logger: log,
      refetchReflectData,
    });
    if (invocationModel?.hasOverride) resolvedArgs = applyCopilotInvocationModel(resolvedArgs, process.env.COPILOT_MODEL || "");
  }
  if (!copilotSDKMode) {
    try {
      applyCopilotWireAPI({
        modelsJson: loadModelsJson(),
        awfReflectData,
        configuredModel: configuredCopilotModel,
        overrideSource: modelRoutingSelection && !configuredWireAPI ? "routing" : "override",
        logger: log,
      });
    } catch (err) {
      log(getErrorMessage(err));
      process.exit(1);
      return;
    }
    warnCopilotSubagentEndpoints({ awfReflectData, logger: log });
  }

  // Pre-flight: skip the agent entirely when a noop has already been written by a prior step.
  // A noop indicates the work is complete or there is nothing to do — starting the agent
  // (and, in SDK mode, probing provider listener readiness below) would be wasteful and
  // potentially harmful. This must run before the provider-listener readiness gate so a
  // legitimate noop exit is never turned into an infrastructure-incomplete failure by an
  // unrelated listener being unavailable.
  const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS || "";
  if (shouldSkipForNoopSafeOutputs({ safeOutputsPath, hasNoopInSafeOutputs, log, executionEvidence })) {
    process.exit(0);
  }

  // Sub-agent models are resolved on routed runs too, but a pre-existing noop must not
  // make startup depend on sub-agent model availability. Threat detection uses no
  // sub-agents, so it neither reads, resolves, records nor writes agent files.
  const resolveSubagentModels = !isDetectionPhase(process.env.GH_AW_PHASE);
  if (!copilotSDKMode && resolveSubagentModels) {
    await applyCopilotCLISubagentModels({
      reflectData: awfReflectData,
      refetchReflectData,
      logger: log,
    });
  }

  // Resolve BYOK provider from live reflect data (SDK mode only).
  // Multi-provider BYOK is the only supported mode — fail immediately if the
  // provider cannot be resolved so retries are not wasted on a misconfigured environment.
  let providerBaseUrl = "";
  let providerType = "openai";
  /** @type {"responses" | "completions" | ""} */
  let providerWireApi = "";
  let resolvedModel = "";
  let multiProviderJson = "";
  let subagentModelsJson = "";
  let primaryProviderName = "";
  if (copilotSDKMode) {
    const configuredModel = modelRoutingSelection?.wire_model || process.env.COPILOT_MODEL || "";
    const modelsJson = loadModelsJson();

    let multiProvider = await resolveCopilotSDKMultiProvider({
      configuredModel,
      requestedModel: selectionConfiguredModel,
      routed: Boolean(modelRoutingSelection),
      wireApi: process.env.COPILOT_PROVIDER_WIRE_API,
      reflectData: awfReflectData,
      modelsJson,
      refetchReflectData,
      logger: log,
    });
    if (!multiProvider || !multiProvider.model) {
      log(
        multiProvider
          ? `copilot-sdk driver mode: configured model '${configuredModel}' could not be matched to a provider model in awf-reflect data — aborting`
          : "copilot-sdk driver mode: BYOK provider is required but could not be resolved from awf-reflect data — aborting"
      );
      process.exit(1);
    }
    resolvedModel = multiProvider.model;
    // Set the primary provider's details as COPILOT_PROVIDER_* env vars for the headless sidecar
    // (which still reads those to configure its own sub-agent sessions).
    primaryProviderName = multiProvider.models.find(m => `${m.provider}/${m.id}` === resolvedModel)?.provider ?? multiProvider.providers[0]?.name ?? "";
    const primaryProvider = multiProvider.providers.find(p => p.name === primaryProviderName) ?? multiProvider.providers[0];
    providerBaseUrl = primaryProvider?.baseUrl ?? "";
    providerType = primaryProvider?.type ?? "openai";
    providerWireApi = providerWireApi || primaryProvider?.wireApi || "";
    multiProviderJson = JSON.stringify({ model: multiProvider.model, providers: multiProvider.providers, models: multiProvider.models });

    log(`copilot-sdk driver mode: multi-provider config resolved (${multiProvider.providers.length} providers, ${multiProvider.models.length} models, model=${resolvedModel})`);
    logCopilotInferenceConfiguration({
      copilotSDKMode,
      configuredModel,
      resolvedModel,
      primaryProviderName,
      providers: multiProvider.providers,
      models: multiProvider.models,
    });

    // The same custom agents the SDK driver loads: workflow definitions override repository ones.
    const sdkWorkingDirectory = path.resolve(process.env.GITHUB_WORKSPACE || process.cwd(), process.env.GH_AW_ENGINE_CWD || ".");
    const agents = resolveSubagentModels ? listCopilotSubagents(sdkWorkingDirectory, path.join(os.tmpdir(), "gh-aw"), log) : [];
    try {
      const resolutions = await resolveCopilotSubagentModels({
        agents,
        repositoryFallback: "the SDK session gives it the session model",
        reflectData: awfReflectData,
        refetchReflectData,
        providerConfig: multiProvider,
        refreshProviderConfig: async reflectData => {
          multiProvider = resolveMultiProviderFromReflect({
            model: configuredModel,
            wireApi: process.env.COPILOT_PROVIDER_WIRE_API,
            reflectData,
            modelsJson,
            logger: log,
          }) || { model: "", providers: [], models: [] };
          return multiProvider;
        },
        logger: log,
      });
      subagentModelsJson = JSON.stringify(Object.fromEntries(resolutions.map(resolution => [resolution.name, resolution.model])));
    } catch (failure) {
      if (!(failure instanceof ModelAliasResolutionError)) throw failure;
      process.exit(1);
    }

    resolvedModel = multiProvider.model;
    primaryProviderName = multiProvider.models.find(m => `${m.provider}/${m.id}` === resolvedModel)?.provider ?? multiProvider.providers[0]?.name ?? "";
    const refreshedPrimaryProvider = multiProvider.providers.find(p => p.name === primaryProviderName) ?? multiProvider.providers[0];
    providerBaseUrl = refreshedPrimaryProvider?.baseUrl ?? "";
    providerType = refreshedPrimaryProvider?.type ?? "openai";
    const configuredProviderWireApi = process.env.COPILOT_PROVIDER_WIRE_API;
    providerWireApi = configuredProviderWireApi === "responses" || configuredProviderWireApi === "completions" ? configuredProviderWireApi : refreshedPrimaryProvider?.wireApi || "";
    multiProviderJson = JSON.stringify({ model: multiProvider.model, providers: multiProvider.providers, models: multiProvider.models });

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
    routingEffort: modelRoutingSelection?.effort ?? null,
    multiProviderJson,
    subagentModelsJson,
  });
  // The SDK driver records model-selection evidence against the requested model (`auto` when routed).
  if (copilotSDKMode) sdkChildEnv.GH_AW_COPILOT_CONFIGURED_MODEL = selectionConfiguredModel;
  const childEnv = Object.keys(sdkChildEnv).length > 0 ? buildCopilotEngineChildEnv(process.env, sdkChildEnv) : undefined;

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
  /** @type {string | undefined} */
  let nativeModelSessionId;
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
        if (driverServerArgs.length === 0) driverServerArgs = buildCopilotSDKServerArgs(childEnv ?? process.env);
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
        telemetryPath: resolveHarnessRetryTelemetryPath(),
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
          // Verified native retries resume the exact UUID; other CLI paths retain --continue.
          let currentArgs = !copilotSDKMode && attempt > 0 && useContinueOnRetry ? [...resolvedArgs, "--continue"] : resolvedArgs;
          const verifyModel = !copilotSDKMode && (!process.env.GH_AW_PHASE || process.env.GH_AW_PHASE === "agent") && Boolean(process.env.COPILOT_MODEL);
          let sessionId;
          if (verifyModel) {
            const session = prepareCopilotModelSession(resolvedArgs, attempt > 0 && useContinueOnRetry ? nativeModelSessionId : undefined);
            currentArgs = session.args;
            sessionId = session.sessionId;
            nativeModelSessionId = sessionId;
          }

          // Redact --prompt / -p value from logs to avoid leaking prompt content
          const safeArgs = currentArgs.map((arg, i) => (currentArgs[i - 1] === "--prompt" || currentArgs[i - 1] === "-p" ? "<redacted>" : arg));
          const modelObserver = !verifyModel ? { check: () => "" } : createCopilotModelObserver({ configuredModel: selectionConfiguredModel, resolvedModel: process.env.COPILOT_MODEL || "", sessionId, logger: log });
          // Driver mode: run copilot_sdk_driver.cjs as a normal subprocess. The harness has
          // already started the sidecar; the driver only opens an SDK client connection.
          const result = await runProcess({
            command,
            args: currentArgs,
            attempt,
            log,
            logArgs: safeArgs,
            env: childEnv,
            stdin: promptStdin,
            runtimeGuard: { shouldTerminate: () => ({ terminate: Boolean(modelObserver.check()), reason: "copilot_model_verification" }) },
            postResultWatchdog: safeOutputsPath
              ? {
                  shouldArm: () => hasTerminalSafeOutput(safeOutputsPath),
                  inactivityTimeoutMs: POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS,
                }
              : undefined,
          });
          // The SDK driver observes its main session, records any substitution it refused and
          // exits with COPILOT_MODEL_VERIFICATION_EXIT_CODE on any verification failure.
          const modelSubstitution = copilotSDKMode ? readCopilotModelSubstitution() : modelObserver.check(true);
          if (modelSubstitution || (copilotSDKMode && result.exitCode === COPILOT_MODEL_VERIFICATION_EXIT_CODE)) {
            result.exitCode = 1;
            result.runtimeGuardReason = "copilot_model_verification";
            if (modelSubstitution) result.output += `\n${modelSubstitution}`;
          }
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
          if (result.runtimeGuardReason === "copilot_model_verification") {
            result.failureClasses = ["copilot_model_verification"];
            return { action: "stop" };
          }
          // Determine whether to retry.
          // Retry whenever the session was partially executed (hasOutput).
          //   - CLI mode: retry with --continue so the Copilot CLI can continue from on-disk state.
          //   - SDK mode: retry always restarts fresh — there is no CLI on-disk state to resume.
          // CAPIError 400 is the well-known transient case, but any partial-execution failure is
          // eligible for a retry.
          // Exceptions:
          //   - MCP policy errors and model-not-supported errors cannot be fixed by continuing the same session.
          //   - Auth errors trigger a one-time fallback to a fresh run; after that --continue is
          //     permanently disabled.
          //   - Null-type tool_call 400 errors poison conversation history — always restart fresh and
          //     permanently disable --continue so the corrupt state is never reloaded.
          const isCAPIError = isTransientCAPIError(result.output);
          const isModelRoutingFailure = modelRoutingRequired && result.exitCode === 78;
          const isQuotaExceeded = isCAPIQuotaExceededError(result.output);
          const hasCAPIServerError = isCAPIServerError(result.output);
          const isMCPPolicy = isMCPPolicyError(result.output);
          const isModelNotSupported = isModelNotSupportedError(result.output);
          const hasHTTP400ResponseError = isHTTP400ResponseError(result.output);
          const hasToolCallIdSchemaError = isToolCallIdSchemaError(result.output);
          const hasIncompleteToolCallStream = isIncompleteToolCallStreamError(result.output);
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
          const isContentFiltered = isCopilotContentFilterError(result.output);
          const isInvocationCapExceeded = nonRetryableGuard.maxRunsExceeded;
          const tokenCount = extractTokenCountFromOutput(result.output);
          const attemptDurationMs = result.durationMs ?? 0;
          const proxyAICreditsRejection = parseAICreditsExceededProxyRejection(result.output);
          const providerAuthFailure = parseProviderAuthFailure(result.output);
          const isProxyHTTP403AuthFailure = !!providerAuthFailure && providerAuthFailure.statusCode === "403" && isLikelyAWFAPIProxyURL(providerAuthFailure.providerUrl);
          const shouldCheckAuditForAICreditsExceeded = nonRetryableGuard.aiCreditsExceeded || isAuthenticationFailed || isProxyHTTP403AuthFailure;
          const auditAICreditsExceeded = shouldCheckAuditForAICreditsExceeded ? parseMaxAICreditsExceededFromAuditLog() : false;
          const trustedAICreditsExceeded = !!proxyAICreditsRejection || auditAICreditsExceeded;
          const isTrustedAICreditsBudgetExhausted = trustedAICreditsExceeded && (!isAuthenticationFailed || !!proxyAICreditsRejection || isProxyHTTP403AuthFailure);
          // The Copilot CLI discards the api-proxy's structured 403 body and prints a generic
          // "Authentication failed with provider ... (HTTP 403)" line, so a proxy guardrail
          // rejection is indistinguishable from a credential failure in the CLI text. The proxy's
          // own structured log carries the guard by name; consult it whenever the proxy answered
          // with a 403/auth failure so the attempt is classified (and reported) correctly.
          const shouldCheckEventLogForAPIProxyGuard = !nonRetryableGuard.apiProxyGuardRejection && !isTrustedAICreditsBudgetExhausted && (isProxyHTTP403AuthFailure || isAuthenticationFailed);
          const apiProxyGuardRejection = nonRetryableGuard.apiProxyGuardRejection || (shouldCheckEventLogForAPIProxyGuard ? parseAPIProxyGuardRejectionFromEventLog() : null);
          const failureClass = classifyCopilotFailure({
            isContentFiltered,
            hasOutput: result.hasOutput,
            isModelRoutingFailure,
            isModelEndpointMismatch: !!nonRetryableGuard.modelEndpointMismatch,
            isAuthErr,
            isAuthenticationFailed,
            isTransientCAPIError: isCAPIError,
            isMCPGatewayShutdown,
            isMCPPolicy,
            isModelNotSupported,
            isHTTP400ResponseError: hasHTTP400ResponseError,
            isToolCallIdSchemaError: hasToolCallIdSchemaError,
            isIncompleteToolCallStream: hasIncompleteToolCallStream,
            isInvocationCapExceeded,
            isAPIProxyGuardRejected: !!apiProxyGuardRejection,
            isNullTypeToolCall,
            isQuotaExceeded,
            isCAPIServerError: hasCAPIServerError,
            isTrustedAICreditsBudgetExhausted,
            isSDKSessionIdleTimeout,
            hasNumerousPermissionDenied,
            tokenCount,
          });
          const outputTail = extractOutputTail(result.output);
          result.failureClasses = [failureClass];
          log(
            `attempt ${attempt + 1} failed:` +
              ` exitCode=${result.exitCode}` +
              ` failureClass=${failureClass}` +
              ` isCAPIError400=${isCAPIError}` +
              ` isModelRoutingFailure=${isModelRoutingFailure}` +
              ` isCAPIQuotaExceededError=${isQuotaExceeded}` +
              ` isInvocationCapExceeded=${isInvocationCapExceeded}` +
              ` apiProxyGuardRejection=${apiProxyGuardRejection ? formatAPIProxyGuardRejection(apiProxyGuardRejection) : "none"}` +
              ` isMCPPolicyError=${isMCPPolicy}` +
              ` isModelNotSupportedError=${isModelNotSupported}` +
              ` isHTTP400ResponseError=${hasHTTP400ResponseError}` +
              ` isToolCallIdSchemaError=${hasToolCallIdSchemaError}` +
              ` isIncompleteToolCallStream=${hasIncompleteToolCallStream}` +
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
          if (isModelRoutingFailure) {
            log("unexpected error: AWF model routing failed with terminal exit code 78; inspect the AWF routing failure_code and router diagnostics");
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

          if (isContentFiltered) {
            log(`attempt ${attempt + 1}: model response blocked by content filtering — not retrying; task completion could not be confirmed`);
            return { action: "stop" };
          }

          if (nonRetryableGuard.modelEndpointMismatch) {
            const mismatch = nonRetryableGuard.modelEndpointMismatch;
            recordModelEndpointMismatch({
              ...mismatch,
              phase: "runtime",
              configured_model: configuredCopilotModel,
              resolved_model: childEnv?.COPILOT_MODEL || process.env.COPILOT_MODEL || "",
              wire_api: childEnv?.COPILOT_PROVIDER_WIRE_API || process.env.COPILOT_PROVIDER_WIRE_API || "completions",
              wire_api_source: configuredWireAPI ? "override" : "inferred",
              supported_endpoints: getSupportedEndpoints(awfReflectData, mismatch.model || childEnv?.COPILOT_MODEL || process.env.COPILOT_MODEL) || [],
            });
            log(`model endpoint mismatch — not retrying: ${mismatch.detail} ${MODEL_ENDPOINT_FIX}`);
            return { action: "stop" };
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

          if (proxyAICreditsRejection) {
            log(`attempt ${attempt + 1}: AWF API proxy rejected the request with HTTP 403 max-AI-credits (${proxyAICreditsRejection.aiCredits}/${proxyAICreditsRejection.maxAICredits}) — trusted budget-abort evidence`);
          }
          if (nonRetryableGuard.aiCreditsExceeded && !trustedAICreditsExceeded) {
            log(`attempt ${attempt + 1}: AI credits marker found in CLI output without trusted firewall audit confirmation — preserving normal failure handling`);
          }
          // Some CLIs surface the proxy's budget rejection as an authentication failure (e.g. Claude Code
          // reports `error: authentication_failed` for "403 Maximum AI credits exceeded"). When the trusted
          // proxy signature is present that veto must not mask intentional budget enforcement.
          const shouldTreatAICreditsExceededAsSuccess = isTrustedAICreditsBudgetExhausted;
          if (shouldTreatAICreditsExceededAsSuccess || isInvocationCapExceeded) {
            const reasons = [];
            if (shouldTreatAICreditsExceededAsSuccess) {
              /** @type {string} */
              let budgetUsageDetails = "";
              if (proxyAICreditsRejection) {
                budgetUsageDetails = ` (configured cap=${proxyAICreditsRejection.maxAICredits}, observed usage=${proxyAICreditsRejection.aiCredits})`;
              } else {
                const { aiCredits: observedAICredits } = parseAICreditsErrorInfoFromAuditLog();
                const configuredMaxAICredits = parseMaxAICreditsFromAuditLog();
                if (configuredMaxAICredits && observedAICredits) {
                  budgetUsageDetails = ` (configured cap=${configuredMaxAICredits}, observed usage=${observedAICredits})`;
                } else if (configuredMaxAICredits) {
                  budgetUsageDetails = ` (configured cap=${configuredMaxAICredits})`;
                } else if (observedAICredits) {
                  budgetUsageDetails = ` (observed usage=${observedAICredits})`;
                }
              }
              reasons.push(`AI credits budget exceeded${budgetUsageDetails}`);
            }
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

          // An AWF API proxy guardrail rejection (HTTP 403) is a terminal policy outcome: the
          // proxy-side counter is not reset by a fresh attempt, so retrying burns the retry budget
          // against a spent proxy. Surface the guard name and counters so the run log states what
          // was enforced instead of reporting a misleading credential failure.
          if (apiProxyGuardRejection) {
            log(`attempt ${attempt + 1}: AWF API proxy guardrail rejected the request: ${formatAPIProxyGuardRejection(apiProxyGuardRejection)} — not retrying (proxy guardrail, not an authentication failure)`);
            if (safeOutputsPath && hasTerminalSafeOutput(safeOutputsPath)) {
              log(`attempt ${attempt + 1}: proxy guardrail fired but safe-outputs already contain expected output — suppressing terminal verdict (false-red: core work succeeded)`);
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
            log(`attempt ${attempt + 1}: model not supported — not retrying (the model is unavailable to this token/context; check the model catalog and provider access)`);
            return { action: "stop" };
          }

          if (hasToolCallIdSchemaError) {
            log(`attempt ${attempt + 1}: tool-call ID schema error — not retrying (conversation state cannot be resumed)`);
            return { action: "stop" };
          }

          if (hasIncompleteToolCallStream) {
            if (attempt < maxRetries && result.hasOutput) {
              useContinueOnRetry = false;
              continueDisabledPermanently = true;
              log(`attempt ${attempt + 1}: incomplete tool-call stream — restarting fresh (--continue disabled permanently)`);
              return { action: "retry" };
            }
            log(`attempt ${attempt + 1}: incomplete tool-call stream — no retry budget remains; detection or task completion is unverified`);
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

          if (shouldRetryFailedExecution({ ...result, isModelRoutingFailure, attempt, maxRetries })) {
            const reason = hasCAPIServerError ? "CAPI server error (transient)" : isCAPIError ? "CAPIError 400 (transient)" : "partial execution";
            const isCrashSignal = isCrashSignalExitCode(result.exitCode);
            const crashSignalName = crashSignalNameForExitCode(result.exitCode);
            if (isCrashSignal) {
              continueDisabledPermanently = true;
            }
            // --continue is only meaningful in CLI mode; SDK mode always restarts fresh.
            useContinueOnRetry = shouldContinueCopilotSessionAfterFailure({
              copilotSDKMode,
              continueDisabledPermanently,
              hasOutput: result.hasOutput,
              hasCAPIServerError,
            });
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
    buildCopilotEngineChildEnv,
    envFlagEnabled,
    generateCopilotConnectionToken,
    buildCopilotSDKServerArgs,
    getCopilotSDKServerPort,
    hasNoopInSafeOutputs,
    hasExpectedSafeOutputs,
    isDetectionPhase,
    isModelNotSupportedError,
    resolveCopilotInvocationModel,
    applyCopilotInvocationModel,
    prepareCopilotModelSession,
    computeStartupRetryEligible,
    isHTTP400ResponseError,
    isToolCallIdSchemaError,
    isIncompleteToolCallStreamError,
    isModelAvailableInReflectData,
    isModelAvailableInReflectFile,
    resolveMultiProviderFromReflect,
    inferProviderTypeForModel,
    countPermissionDeniedIssues,
    detectCopilotErrors,
    classifyCopilotFailure,
    isCopilotContentFilterError,
    extractTokenCountFromOutput,
    shouldRetryFailedExecution,
    shouldContinueCopilotSessionAfterFailure,
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
    resolvePromptFileInput,
    resolveRetryConfig,
    parseCopilotSDKServerArgsFromEnv,
    isCAPIQuotaExceededError,
    isCAPIServerError,
    hasTerminalSafeOutput,
    applyModelFallback,
    applyCopilotModelAliasResolution,
    resolveCopilotSDKMultiProvider,
    resolveCopilotSubagentModels,
    applyCopilotCLISubagentModels,
    listCopilotAgentFiles,
    listCopilotSubagents,
    writeCopilotPersonalAgentCopy,
    applyCopilotWireAPI,
    warnCopilotSubagentEndpoints,
    resolveAWFModelRoutingSelection,
    resolveCopilotModelRouting,
    applyCopilotRoutingSelection,
    applyCopilotRoutingArgs,
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
