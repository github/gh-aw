// @ts-check

/**
 * Pi Provider Extension for gh-aw
 *
 * Registers Pi providers from the AWF-injected environment and calls the AWF
 * API proxy /reflect endpoint at session start to dynamically discover the
 * open LLM inference paths configured for this run. This gives operators
 * runtime visibility into which provider/model combination is active and
 * verifies that the expected gateway port is reachable before the agent starts
 * working.
 *
 * When the model uses provider/model format (e.g. "copilot/claude-sonnet-4"),
 * the extension logs the matched endpoint so failures can be diagnosed without
 * inspecting container internals.
 *
 * The extension is automatically added to every Pi agent invocation by the
 * gh-aw compiler alongside pi_steering_extension.cjs.  No workflow frontmatter
 * configuration is required.
 *
 * Configuration (read from environment variables):
 *   GH_AW_PI_MODEL   The original engine.model value; may be "provider/model"
 *                    or bare "model". Preferred over PI_MODEL so gh-aw can pass
 *                    model context to extensions without changing Pi CLI behavior.
 *   PI_MODEL         Legacy fallback used when GH_AW_PI_MODEL is not set.
 *   GH_AW_PI_SUBAGENT_CHILD / GH_AW_PI_SUBAGENT_NAME
 *                    Set by pi_subagent_extension.cjs in a sub-agent process. Each
 *                    Pi process sends its own AWF caller declaration
 *                    (x-awf-caller: main or subagent/<name>) on provider requests.
 */

"use strict";

const { fetchAWFReflect, AWF_API_PROXY_REFLECT_URL, AWF_REFLECT_OUTPUT_PATH, AWF_REFLECT_TIMEOUT_MS, AWF_MODELS_URL_TIMEOUT_MS } = require("./awf_reflect.cjs");
const { emitInfrastructureIncomplete } = require("./safeoutputs_cli.cjs");
const fs = require("fs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { getProviderErrorDetails } = require("./pi_provider_error.cjs");

// Default logger: prefixed with "[gh-aw/pi-provider]" for easy grepping.
// prettier-ignore
const DEFAULT_LOGGER = /** @type {(msg: string) => void} */ (msg => process.stderr.write(`[gh-aw/pi-provider] ${new Date().toISOString()} ${msg}\n`));

/**
 * Return the workflow-configured model string exposed to Pi extensions.
 * GH_AW_PI_MODEL takes precedence because gh-aw sets it explicitly for extensions
 * while continuing to pass the CLI model via --model. PI_MODEL remains a legacy
 * fallback for older callers.
 *
 * @returns {string}
 */
function getConfiguredModel() {
  return process.env.GH_AW_PI_MODEL || process.env.PI_MODEL || "";
}

/**
 * Extract the provider prefix from a "provider/model" string.
 * Returns an empty string when no slash is present (bare model name).
 *
 * @param {string} model
 * @returns {string}
 */
function extractProviderFromModel(model) {
  if (!model) return "";
  const slashIdx = model.indexOf("/");
  if (slashIdx <= 0) return "";
  return model.slice(0, slashIdx).toLowerCase();
}

/**
 * Resolve the expected LLM gateway base URL for a given provider prefix.
 * Returns null when the provider is not one of the well-known AWF sidecar providers.
 *
 * Uses the "api-proxy" Docker service hostname so the URL reflects the actual
 * address used by Pi's models.json routing within the AWF Docker network.
 *
 * @param {string} provider - Lowercase provider prefix (e.g. "copilot", "anthropic").
 * @returns {string|null}
 */
function resolveGatewayUrl(provider) {
  const GATEWAY_PORTS = /** @type {Record<string, number>} */ {
    copilot: 10002,
    anthropic: 10001,
    openai: 10000,
    codex: 10000,
    google: 10003,
  };
  const port = GATEWAY_PORTS[provider];
  if (!port) return null;
  return `http://api-proxy:${port}`;
}

const AWF_CALLER_HEADER = "x-awf-caller";
const AWF_CALLER_MAX_LENGTH = 128;
const AWF_CALLER_PATTERN = /^(?:main|subagent\/[A-Za-z0-9_.:/-]+)$/;

/**
 * Resolve this Pi process's AWF caller declaration. The parent process is the
 * main agent; a sub-agent process (launched by pi_subagent_extension.cjs with
 * its own environment) declares its agent definition name. Returns null when
 * the value would be invalid, so the request is sent without the header rather
 * than with a truncated or misleading caller.
 *
 * @param {NodeJS.ProcessEnv} [env]
 * @returns {string|null}
 */
function resolveAwfCaller(env = process.env) {
  if (env.GH_AW_PI_SUBAGENT_CHILD !== "1") return "main";
  const caller = `subagent/${env.GH_AW_PI_SUBAGENT_NAME || ""}`;
  return caller.length <= AWF_CALLER_MAX_LENGTH && AWF_CALLER_PATTERN.test(caller) ? caller : null;
}

/**
 * Set the AWF caller header on mutable Pi provider headers, replacing any
 * differently cased copy so exactly one value is sent.
 *
 * @param {Record<string, string|null>|undefined|null} headers
 * @param {string|null} caller
 * @returns {void}
 */
function applyAwfCallerHeader(headers, caller) {
  if (!headers || typeof headers !== "object") return;
  for (const name of Object.keys(headers)) {
    if (name.toLowerCase() === AWF_CALLER_HEADER) delete headers[name];
  }
  if (caller) headers[AWF_CALLER_HEADER] = caller;
}

/**
 * Join a base URL and relative API path without duplicating slashes.
 *
 * @param {string} baseUrl
 * @param {string} apiPath
 * @returns {string}
 */
function joinApiUrl(baseUrl, apiPath) {
  return `${baseUrl.replace(/\/+$/, "")}${apiPath}`;
}

/**
 * Resolve the Pi model's inferred provider request target for logging.
 *
 * @param {any} model
 * @returns {{ api: string, method: string, url: string }}
 */
function resolveProviderRequestTarget(model) {
  const api = typeof model?.api === "string" && model.api ? model.api : "(unknown api)";
  const method = "POST";
  const baseUrl = typeof model?.baseUrl === "string" && model.baseUrl ? model.baseUrl : "";

  if (!baseUrl) {
    return { api, method, url: "(baseUrl unavailable)" };
  }

  switch (api) {
    case "openai-completions":
      return { api, method, url: joinApiUrl(baseUrl, "/chat/completions") };
    case "openai-responses":
    case "azure-openai-responses":
    case "openai-codex-responses":
      return { api, method, url: joinApiUrl(baseUrl, "/responses") };
    case "anthropic":
      return { api, method, url: joinApiUrl(baseUrl, "/messages") };
    case "anthropic-messages":
      return { api, method, url: joinApiUrl(baseUrl, "/v1/messages") };
    case "mistral-conversations":
      return { api, method, url: joinApiUrl(baseUrl, "/conversations") };
    default:
      return { api, method, url: baseUrl };
  }
}

/**
 * Format response header names for logs without printing sensitive values.
 *
 * @param {Record<string, string>|undefined|null} headers
 * @returns {string}
 */
function formatResponseHeaderNames(headers) {
  const names = Object.keys(headers || {})
    .map(name => String(name).toLowerCase())
    .sort();
  return names.length > 0 ? names.join(",") : "none";
}

/**
 * Adapt only provider-facing tool schemas. Pi and MCP retain the original
 * schemas for argument validation; strict inference rejects uniqueItems.
 *
 * @param {any} payload
 * @param {string} api
 * @param {(msg: string) => void} logger
 * @returns {any}
 */
function adaptProviderToolSchemas(payload, api, logger) {
  if (!["openai-completions", "openai-responses", "azure-openai-responses", "openai-codex-responses"].includes(api) || !payload || typeof payload !== "object") return payload;
  let omitted = 0;
  const schemaMaps = new Set(["properties", "patternProperties", "$defs", "definitions", "dependentSchemas", "dependencies"]);
  const schemaNodes = new Set(["items", "additionalProperties", "additionalItems", "contains", "not", "if", "then", "else", "unevaluatedProperties", "unevaluatedItems", "propertyNames"]);
  const schemaArrays = new Set(["allOf", "anyOf", "oneOf", "prefixItems"]);
  /** @param {any} schema @returns {any} */
  function adaptSchema(schema) {
    if (!schema || typeof schema !== "object") return schema;
    if (Array.isArray(schema)) return schema.map(adaptSchema);
    return Object.fromEntries(
      Object.entries(schema).flatMap(([key, value]) => {
        if (key === "uniqueItems") {
          omitted += 1;
          return [];
        }
        if (schemaMaps.has(key) && value && typeof value === "object" && !Array.isArray(value)) {
          return [[key, Object.fromEntries(Object.entries(value).map(([name, child]) => [name, adaptSchema(child)]))]];
        }
        return [[key, schemaNodes.has(key) || schemaArrays.has(key) ? adaptSchema(value) : value]];
      })
    );
  }
  /** @param {any} tool @returns {any} */
  function adaptTool(tool) {
    if (tool?.type === "namespace" && Array.isArray(tool.tools)) return { ...tool, tools: tool.tools.map(adaptTool) };
    if (tool?.type !== "function") return tool;
    if (tool.function) return { ...tool, function: { ...tool.function, parameters: adaptSchema(tool.function.parameters) } };
    return { ...tool, parameters: adaptSchema(tool.parameters) };
  }
  const adapted = {
    ...payload,
    ...(Array.isArray(payload.tools) ? { tools: payload.tools.map(adaptTool) } : {}),
    ...(Array.isArray(payload.input) ? { input: payload.input.map(item => (item?.type === "tool_search_output" && Array.isArray(item.tools) ? { ...item, tools: item.tools.map(adaptTool) } : item)) } : {}),
  };
  if (omitted) logger(`provider_schema_compat omitted_unique_items=${omitted} argument_validation=unchanged`);
  return omitted ? adapted : payload;
}

/**
 * Emit a report_incomplete safe output when provider infrastructure fails
 * before any safe outputs have been recorded.
 *
 * This Pi extension runs inside the AWF agent sandbox, where the directory
 * backing GH_AW_SAFE_OUTPUTS is mounted read-only (writes fail with EROFS).
 * Emission is therefore delegated to the `safeoutputs` CLI (see
 * safeoutputs_cli.cjs), which forwards the call to the MCP gateway process
 * that owns write access to the real outputs file — the same channel used
 * by every other safe-output tool call the agent makes.
 *
 * @param {string} details
 * @param {(msg: string) => void} logger
 * @returns {void}
 */
function emitInfrastructureIncompleteIfNoSafeOutputs(details, logger) {
  const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS || "";
  if (!safeOutputsPath) {
    logger("report_incomplete skipped: GH_AW_SAFE_OUTPUTS is not set");
    return;
  }

  try {
    const existing = fs.existsSync(safeOutputsPath) ? fs.readFileSync(safeOutputsPath, "utf8").trim() : "";
    if (existing) {
      logger(`report_incomplete skipped: safe outputs already recorded at ${safeOutputsPath}`);
      return;
    }
  } catch (error) {
    logger(`report_incomplete pre-check failed, proceeding with emission: ${getErrorMessage(error)}`);
  }

  emitInfrastructureIncomplete(details, { safeOutputsPath, logger });
}

/**
 * Log extra context when the AWF /reflect call does not produce a snapshot.
 *
 * @param {{
 *   phase: string,
 *   provider: string,
 *   model: string,
 *   result: {
 *     ok: boolean,
 *     reflectUrl: string,
 *     outputPath: string,
 *     reason?: string,
 *     status?: number,
 *     error?: string,
 *   },
 *   logger: (msg: string) => void,
 * }} params
 * @returns {void}
 */
function logReflectFailure(params) {
  const { phase, provider, model, result, logger } = params;
  if (!result || result.ok) {
    return;
  }

  const status = typeof result.status === "number" ? ` status=${result.status}` : "";
  const error = result.error ? ` error=${JSON.stringify(result.error)}` : "";
  logger(`reflect_failure phase=${phase} provider=${provider || "(no provider prefix)"} model=${model || "(not set)"} url=${result.reflectUrl} output=${result.outputPath} reason=${result.reason || "unknown"}${status}${error}`);
}

/**
 * Register a Pi provider and any aliases.
 *
 * @param {any} pi
 * @param {string[]} names
 * @param {Record<string, any>} config
 * @param {(msg: string) => void} logger
 */
function registerProviderAliases(pi, names, config, logger) {
  for (const name of names) {
    pi.registerProvider(name, config);
    logger(`registered provider=${name}`);
  }
}

/**
 * Register all supported Pi providers discovered from the environment.
 *
 * @param {any} pi
 * @param {(msg: string) => void} logger
 * @returns {number}
 */
function registerConfiguredProviders(pi, logger) {
  let registeredCount = 0;

  const copilotToken = process.env.COPILOT_GITHUB_TOKEN || process.env.GITHUB_TOKEN;
  if (copilotToken) {
    registerProviderAliases(
      pi,
      ["github-copilot", "copilot"],
      {
        apiKey: copilotToken,
        api: "openai-completions",
        ...(process.env.GITHUB_COPILOT_BASE_URL ? { baseUrl: process.env.GITHUB_COPILOT_BASE_URL } : {}),
      },
      logger
    );
    registeredCount += 2;
  }

  if (process.env.ANTHROPIC_API_KEY) {
    registerProviderAliases(
      pi,
      ["anthropic"],
      {
        apiKey: process.env.ANTHROPIC_API_KEY,
        api: "anthropic-messages",
        ...(process.env.ANTHROPIC_BASE_URL ? { baseUrl: process.env.ANTHROPIC_BASE_URL } : {}),
      },
      logger
    );
    registeredCount += 1;
  }

  const openAIKey = process.env.CODEX_API_KEY || process.env.OPENAI_API_KEY;
  if (openAIKey) {
    registerProviderAliases(
      pi,
      ["openai", "codex"],
      {
        apiKey: openAIKey,
        // Real OpenAI models are only published under "openai-responses" in Pi's
        // upstream model catalog. OpenAI's Chat Completions endpoint rejects function
        // tools whenever reasoning_effort is anything other than "none", so routing
        // through /responses keeps tool calling working for reasoning-capable models.
        // See: https://developers.openai.com/api/docs/guides/responses-vs-chat-completions
        api: "openai-responses",
        ...(process.env.OPENAI_BASE_URL ? { baseUrl: process.env.OPENAI_BASE_URL } : {}),
      },
      logger
    );
    registeredCount += 2;
  }

  if (registeredCount === 0) {
    logger("no provider credentials detected for Pi provider registration");
  }

  return registeredCount;
}

/**
 * Pi provider extension for gh-aw.
 *
 * Registers providers immediately, then subscribes to the `agent_start` and `agent_settled`
 * Pi SDK events and calls the AWF /reflect endpoint to discover and log the open LLM
 * inference paths before the agent begins its first turn and again after it finishes.
 * The post-run fetch is the authoritative snapshot used by the step summary; the pre-run
 * fetch captures the initial proxy state for diagnostics in case the session exits
 * unexpectedly before reaching `agent_settled`.
 * Both calls are best-effort: any network or parse error is logged but does not abort the
 * agent session.
 *
 * @param {any} pi - Pi ExtensionAPI instance
 * @returns {void}
 */
function piProviderExtension(pi) {
  const log = DEFAULT_LOGGER;
  /** @type {{ api: string, method: string, url: string }|null} */
  let lastProviderRequest = null;
  /** @type {{ status: number, responseHeaders: string, succeeded: boolean }|null} */
  let lastProviderResponse = null;
  /** @type {{ status?: number, message: string }|null} */
  let lastProviderFailure = null;
  let providerRequestCount = 0;
  let successfulProviderResponseCount = 0;
  registerConfiguredProviders(pi, log);

  // Process isolation is the attribution boundary: the parent and every
  // sub-agent run as separate Pi processes that each load this extension.
  // Only send the header to the AWF api-proxy, never directly to a provider
  // (e.g. sandbox.agent: false — no api-proxy running).
  if (process.env.AWF_REFLECT_ENABLED === "1") {
    const awfCaller = resolveAwfCaller();
    if (!awfCaller) log("x-awf-caller omitted: sub-agent name is not a valid AWF caller");
    pi.on("before_provider_headers", event => applyAwfCallerHeader(event?.headers, awfCaller));
  }

  pi.on("before_provider_request", (event, ctx) => {
    providerRequestCount += 1;
    lastProviderRequest = resolveProviderRequestTarget(ctx && ctx.model);
    lastProviderResponse = null;
    const provider = ctx?.model?.provider || "(unknown provider)";
    const model = ctx?.model?.id || getConfiguredModel() || "(unknown model)";
    log(`provider_request provider=${provider} model=${model} api=${lastProviderRequest.api} method=${lastProviderRequest.method} url=${lastProviderRequest.url}`);
    return adaptProviderToolSchemas(event?.payload, lastProviderRequest.api, log);
  });

  pi.on("after_provider_response", (event, ctx) => {
    const succeeded = event.status >= 200 && event.status < 300;
    if (succeeded) {
      successfulProviderResponseCount += 1;
    }
    const request = lastProviderRequest || resolveProviderRequestTarget(ctx && ctx.model);
    lastProviderResponse = {
      status: event.status,
      responseHeaders: formatResponseHeaderNames(event.headers),
      succeeded,
    };
    if (!succeeded) {
      const responseBody = event.body ?? event.responseBody;
      lastProviderFailure = getProviderErrorDetails(responseBody, event.status);
    }
    const provider = ctx?.model?.provider || "(unknown provider)";
    const model = ctx?.model?.id || getConfiguredModel() || "(unknown model)";
    log(`provider_response provider=${provider} model=${model} status=${event.status} method=${request.method} url=${request.url} response_headers=${lastProviderResponse.responseHeaders}`);
  });

  pi.on("message_end", event => {
    const message = event && event.message;
    if (message?.role !== "assistant" || message?.stopReason !== "error" || !message?.errorMessage) {
      return;
    }
    const request = lastProviderRequest || { api: message.api || "(unknown api)", method: "POST", url: "(request unavailable)" };
    lastProviderFailure = getProviderErrorDetails(message.errorMessage, lastProviderResponse?.status);
    const status = typeof lastProviderFailure.status === "number" ? String(lastProviderFailure.status) : "no-response";
    const responseHeaders = lastProviderResponse ? lastProviderResponse.responseHeaders : "none";
    log(
      `provider_error provider=${message.provider || "(unknown provider)"} model=${message.model || "(unknown model)"} api=${request.api} status=${status} method=${request.method} url=${request.url} response_headers=${responseHeaders} error=${JSON.stringify(lastProviderFailure.message)}`
    );
    if (lastProviderResponse?.succeeded) {
      successfulProviderResponseCount -= 1;
      lastProviderResponse.succeeded = false;
    }
  });

  pi.on("agent_start", async () => {
    const model = getConfiguredModel();
    const provider = extractProviderFromModel(model);

    if (provider) {
      const gatewayUrl = resolveGatewayUrl(provider);
      if (gatewayUrl) {
        log(`provider=${provider} model=${model} gateway=${gatewayUrl}`);
      } else {
        log(`provider=${provider} model=${model} (no known AWF gateway port for this provider)`);
      }
    } else {
      log(`model=${model || "(not set)"} (no provider prefix — defaulting to Copilot gateway)`);
    }

    // Fetch AWF API proxy reflection data before the agent runs to capture initial proxy state.
    // This is best-effort: failures are logged but do not affect the agent session.
    // Skip when AWF_REFLECT_ENABLED is not "1" (e.g. sandbox.agent: false — no api-proxy running).
    if (process.env.AWF_REFLECT_ENABLED === "1") {
      const result = await fetchAWFReflect({
        reflectUrl: AWF_API_PROXY_REFLECT_URL,
        outputPath: AWF_REFLECT_OUTPUT_PATH,
        timeoutMs: AWF_REFLECT_TIMEOUT_MS,
        modelsTimeoutMs: AWF_MODELS_URL_TIMEOUT_MS,
        logger: log,
      });
      logReflectFailure({ phase: "agent_start", provider, model, result, logger: log });
    }
  });

  pi.on("agent_settled", async () => {
    // Fetch AWF API proxy reflection data after the agent finishes for the post-run step summary.
    // This is best-effort: failures are logged but do not affect the agent exit code.
    // Skip when AWF_REFLECT_ENABLED is not "1" (e.g. sandbox.agent: false — no api-proxy running).
    if (process.env.AWF_REFLECT_ENABLED === "1") {
      const model = getConfiguredModel();
      const provider = extractProviderFromModel(model);
      const result = await fetchAWFReflect({
        reflectUrl: AWF_API_PROXY_REFLECT_URL,
        outputPath: AWF_REFLECT_OUTPUT_PATH,
        timeoutMs: AWF_REFLECT_TIMEOUT_MS,
        modelsTimeoutMs: AWF_MODELS_URL_TIMEOUT_MS,
        logger: log,
      });
      logReflectFailure({ phase: "agent_settled", provider, model, result, logger: log });
    }

    if (providerRequestCount > 0 && successfulProviderResponseCount === 0) {
      const providerFailureDetails = lastProviderFailure
        ? ` Last provider failure: ${typeof lastProviderFailure.status === "number" ? `HTTP ${lastProviderFailure.status}; ` : ""}message=${JSON.stringify(lastProviderFailure.message)}.`
        : "";
      emitInfrastructureIncompleteIfNoSafeOutputs(`All ${providerRequestCount} Pi provider requests failed before safe outputs were emitted.${providerFailureDetails}`, log);
      process.exitCode = 1;
    }
  });
}

module.exports = piProviderExtension;
/** @type {any} */
const _piExports = module.exports;
_piExports.getConfiguredModel = getConfiguredModel;
_piExports.extractProviderFromModel = extractProviderFromModel;
_piExports.resolveGatewayUrl = resolveGatewayUrl;
_piExports.registerConfiguredProviders = registerConfiguredProviders;
_piExports.resolveProviderRequestTarget = resolveProviderRequestTarget;
_piExports.formatResponseHeaderNames = formatResponseHeaderNames;
_piExports.emitInfrastructureIncompleteIfNoSafeOutputs = emitInfrastructureIncompleteIfNoSafeOutputs;
_piExports.logReflectFailure = logReflectFailure;
_piExports.resolveAwfCaller = resolveAwfCaller;
_piExports.applyAwfCallerHeader = applyAwfCallerHeader;
