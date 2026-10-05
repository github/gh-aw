// @ts-check

/**
 * AWF API proxy /reflect endpoint helpers shared by harnesses.
 *
 * Fetches the api-proxy sidecar's /reflect endpoint and persists the response to disk
 * so that the post-run step summary (awf_reflect_summary.cjs) can include provider and
 * model information without needing the containers to still be running.
 *
 * Provides model-ID parsing helpers that understand the response formats used by all
 * supported providers (OpenAI / Anthropic / Copilot and Gemini).
 *
 * Exported by: copilot_harness.cjs, claude_harness.cjs (and any future agent harnesses)
 */

"use strict";

require("./shim.cjs");

const fs = require("fs");
const path = require("path");
const net = require("net");
const tls = require("tls");
const { withRetry, sleep } = require("./error_recovery.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { AWF_REFLECT_OUTPUT_PATH } = require("./awf_reflect_paths.cjs");

function parseReflectTimeoutMs(value) {
  const rawValue = String(value || "").trim();
  if (!/^\d+$/.test(rawValue)) {
    return 60000;
  }
  const timeoutMs = Number(rawValue);
  return Number.isSafeInteger(timeoutMs) ? timeoutMs : 60000;
}

// AWF API proxy management endpoint for discovering configured LLM providers and available models.
// The api-proxy sidecar exposes /reflect on its management port (port 10000) inside the AWF
// Docker network. From the agent container, the proxy is reachable via the "api-proxy" hostname.
const AWF_API_PROXY_REFLECT_URL = "http://api-proxy:10000/reflect";
// Milliseconds to wait for the /reflect endpoint before giving up.
const AWF_REFLECT_TIMEOUT_MS = parseReflectTimeoutMs(process.env.GH_AW_REFLECT_TIMEOUT_MS);
// Milliseconds to wait for each models_url fallback fetch (shorter than the main reflect timeout).
const AWF_MODELS_URL_TIMEOUT_MS = 3000;
// Milliseconds to wait for an api-proxy provider listener to accept a real TCP connection.
const AWF_PROVIDER_LISTENER_READY_TIMEOUT_MS = 15000;
// Delay between provider-listener readiness probes.
const AWF_PROVIDER_LISTENER_READY_RETRY_MS = 250;
// Per-attempt connect timeout while probing provider listener readiness.
const AWF_PROVIDER_LISTENER_READY_PROBE_TIMEOUT_MS = 2000;
// Maximum attempts for models_url fallback fetches when the proxy is not yet ready.
const AWF_MODELS_URL_MAX_ATTEMPTS = 5;
// HTTP statuses treated as transient and worth retrying for models_url fallback fetches.
// 503 covers the api-proxy not yet being ready; 429 covers transient model-catalog throttling
// (see https://github.com/github/gh-aw/issues/52782 — an unresolved 429 previously caused
// alias resolution to be skipped and an unresolved alias to reach the API proxy).
const AWF_MODELS_URL_RETRYABLE_STATUSES = new Set([503, 429]);
// Base delay between models_url fallback retries. Uses exponential backoff.
const AWF_MODELS_URL_RETRY_BASE_MS = 250;
// Cap for exponential backoff delay between retries.
const AWF_MODELS_URL_RETRY_MAX_MS = 2000;
// Delay before the first models_url probe when using GitHub OIDC auth with the local api-proxy.
// This reduces startup-race 503s while the proxy completes OIDC token exchange.
const AWF_MODELS_URL_OIDC_INITIAL_DELAY_MS_DEFAULT = 5000;
// Gemini model name prefix stripped from model IDs in the Gemini models API response.
// Example: { name: "models/gemini-1.5-pro" } → "gemini-1.5-pro"
const GEMINI_MODEL_NAME_PREFIX = "models/";
const REFLECT_PROVIDER_GITHUB = "github";
const REFLECT_PROVIDER_OPENAI = "openai";
const REFLECT_PROVIDER_ANTHROPIC = "anthropic";

/**
 * @typedef {{
 *   configured?: boolean,
 *   models_url?: string | null,
 *   port?: number | null,
 *   provider?: string,
 * }} ReflectEndpoint
 */

/**
 * @typedef {{
 *   endpoints?: ReflectEndpoint[],
 * }} ReflectData
 */

const REFLECT_PROVIDER_ALIASES = {
  github: new Set(["github", "copilot", "github-copilot", "github_models"]),
  openai: new Set(["openai"]),
  anthropic: new Set(["anthropic"]),
  google: new Set(["google", "gemini"]),
};

const DEFAULT_API_PROXY_HOST_BRIDGE = "host.docker.internal";

/**
 * Detect the sbx HOSTALIASES mapping that makes `api-proxy` resolve to localhost.
 * In that topology AWF creates a localhost bridge only for the management
 * /reflect port, so provider traffic for ports such as 10002 must use the
 * host-side Docker gateway name instead.
 *
 * @param {NodeJS.ProcessEnv} [env]
 * @param {(path: string, encoding: BufferEncoding) => string} [readFileSync]
 * @returns {boolean}
 */
function hasAPIProxyLocalhostAlias(env = process.env, readFileSync = fs.readFileSync) {
  const hostAliasesPath = env.HOSTALIASES;
  if (!hostAliasesPath) return false;
  try {
    const aliases = readFileSync(hostAliasesPath, "utf8");
    return aliases.split(/\r?\n/).some(line => {
      const trimmed = line.replace(/#.*/, "").trim();
      if (!trimmed) return false;
      const parts = trimmed.split(/\s+/);
      return parts[0] === "api-proxy" && (parts[1] === "localhost" || parts[1] === "127.0.0.1");
    });
  } catch {
    return false;
  }
}

/**
 * Rewrite api-proxy URLs for sbx HOSTALIASES bridge mode.
 *
 * @param {string} url
 * @param {NodeJS.ProcessEnv} [env]
 * @param {(path: string, encoding: BufferEncoding) => string} [readFileSync]
 * @returns {string}
 */
function rewriteAPIProxyURLForHostBridge(url, env = process.env, readFileSync = fs.readFileSync) {
  if (!hasAPIProxyLocalhostAlias(env, readFileSync)) return url;
  const bridgeHost = env.GH_AW_API_PROXY_HOST_BRIDGE || DEFAULT_API_PROXY_HOST_BRIDGE;
  return url.replace(/^(https?:\/\/)api-proxy(?=[:/]|$)/i, `$1${bridgeHost}`);
}

// Default logger used by fetchAWFReflect when no logger is provided via options.
// All lines are prefixed with "[awf-reflect]" for easy grepping in combined logs.
// prettier-ignore
const DEFAULT_REFLECT_LOGGER = /** @type {(msg: string) => void} */ (msg => process.stderr.write(`[awf-reflect] ${new Date().toISOString()} ${msg}\n`));

/**
 * Normalize provider IDs used in reflect/provider resolution.
 *
 * @param {unknown} provider
 * @param {string} [fallback]
 * @returns {string}
 */
function normalizeReflectProviderName(provider, fallback = "") {
  const normalized = String(provider || "")
    .toLowerCase()
    .trim();
  return normalized || fallback;
}

/**
 * Extract model IDs from a provider API response body.
 *
 * Handles:
 *   - OpenAI / Anthropic / Copilot format: { data: [{ id: "..." }, ...] }
 *   - Gemini format: { models: [{ name: "models/gemini-1.5-pro" }, ...] }
 *
 * @param {any|null} json - Parsed API response
 * @returns {string[]|null} Sorted array of model IDs, or null if unavailable
 */
function extractModelIds(json) {
  if (!json || typeof json !== "object") return null;

  // OpenAI / Anthropic / Copilot format: { data: [{ id: "..." }, ...] }
  if (Array.isArray(json.data)) {
    const ids = json.data.map(m => m && (m.id || m.name)).filter(Boolean);
    return ids.length > 0 ? ids.sort() : null;
  }

  // Gemini format: { models: [{ name: "models/gemini-1.5-pro", ... }, ...] }
  if (Array.isArray(json.models)) {
    const ids = json.models
      .map(m => {
        if (!m) return null;
        const name = m.name || null;
        if (!name) return null;
        return name.startsWith(GEMINI_MODEL_NAME_PREFIX) ? name.slice(GEMINI_MODEL_NAME_PREFIX.length) : name;
      })
      .filter(Boolean);
    return ids.length > 0 ? ids.sort() : null;
  }

  return null;
}

/**
 * Extract a `retry-after` header (if present) from a fetch Response so it can be attached
 * to a retryable error for `withRetry`'s Retry-After handling (see `getRetryAfterMs` in
 * error_recovery.cjs). Only the `retry-after` header is needed: `withRetry` only consults
 * it for HTTP 429 responses, which is the only retryable status here that carries one.
 *
 * @param {{ headers?: { get?: (name: string) => string|null } }} res - fetch Response-like object
 * @returns {Record<string, string>|undefined}
 */
function extractRetryAfterHeader(res) {
  try {
    const retryAfter = res?.headers?.get?.("retry-after");
    return retryAfter != null ? { "retry-after": retryAfter } : undefined;
  } catch {
    return undefined;
  }
}

/**
 * Fetch model IDs from a single models_url endpoint via HTTP GET.
 * Used as a fallback when the api-proxy's startup model-fetch returned null.
 * The api-proxy injects the correct auth headers when forwarding the request.
 *
 * @param {string} modelsUrl - URL of the models endpoint on the api-proxy
 * @param {number} timeoutMs - Request timeout in milliseconds
 * @param {(msg: string) => void} logger
 * @returns {Promise<string[]|null>}
 */
async function fetchModelsFromUrl(modelsUrl, timeoutMs, logger) {
  const requestUrl = rewriteAPIProxyURLForHostBridge(modelsUrl);
  let isInitialProbeDelayed = false;
  try {
    const modelsHost = new URL(modelsUrl).hostname.toLowerCase();
    isInitialProbeDelayed = process.env.AWF_AUTH_TYPE === "github-oidc" && modelsHost === "api-proxy";
  } catch {
    // Ignore invalid URL parsing and proceed without startup delay.
  }
  if (isInitialProbeDelayed) {
    const configuredDelay = Number.parseInt(process.env.AWF_MODELS_URL_OIDC_INITIAL_DELAY_MS || "", 10);
    const initialProbeDelay = Number.isFinite(configuredDelay) && configuredDelay >= 0 ? configuredDelay : AWF_MODELS_URL_OIDC_INITIAL_DELAY_MS_DEFAULT;
    if (initialProbeDelay > 0) {
      logger(`awf-reflect: delaying initial models probe for ${modelsUrl} by ${initialProbeDelay}ms (AWF_AUTH_TYPE=github-oidc)`);
      await sleep(initialProbeDelay);
    }
  }

  let attemptCounter = 0;
  const retryConfig = {
    maxRetries: AWF_MODELS_URL_MAX_ATTEMPTS - 1,
    // withRetry multiplies delay before the next attempt, so divide by 2 here
    // to preserve the intended first backoff of AWF_MODELS_URL_RETRY_BASE_MS.
    initialDelayMs: Math.ceil(AWF_MODELS_URL_RETRY_BASE_MS / 2),
    maxDelayMs: AWF_MODELS_URL_RETRY_MAX_MS,
    backoffMultiplier: 2,
    jitterMs: 0,
    shouldRetry: error => {
      const original = error?.originalError || error;
      const status = original?.status ?? original?.response?.status ?? null;
      const shouldRetry = AWF_MODELS_URL_RETRYABLE_STATUSES.has(status);
      if (shouldRetry && attemptCounter < AWF_MODELS_URL_MAX_ATTEMPTS) {
        logger(`awf-reflect: models fetch returned ${status} for ${modelsUrl}; retrying (attempt ${attemptCounter + 1}/${AWF_MODELS_URL_MAX_ATTEMPTS})`);
      }
      return shouldRetry;
    },
  };

  try {
    return await withRetry(
      async () => {
        attemptCounter += 1;
        const ac = new AbortController();
        const timer = setTimeout(() => {
          logger(`awf-reflect: models fetch timed out for ${modelsUrl}`);
          ac.abort();
        }, timeoutMs);
        try {
          const res = await fetch(requestUrl, { signal: ac.signal });
          if (!res.ok) {
            if (AWF_MODELS_URL_RETRYABLE_STATUSES.has(res.status)) {
              const err = Object.assign(new Error(`models fetch returned ${res.status} for ${modelsUrl}`), {
                status: res.status,
                headers: extractRetryAfterHeader(res),
              });
              throw err;
            }
            // Permanent 4xx/5xx responses (e.g. 400, 401, 403) are not retried — the
            // caller falls back to treating this endpoint as having no models.
            logger(`awf-reflect: models fetch returned ${res.status} for ${modelsUrl}`);
            return null;
          }
          const json = await res.json();
          const models = extractModelIds(json);
          if (models) {
            logger(`awf-reflect: fetched ${models.length} model(s) from ${modelsUrl}`);
          }
          return models;
        } catch (err) {
          if (err instanceof Error && err.name === "AbortError") {
            return null; // already logged above
          }
          /** @type {any} */
          const e = err;
          const status = e?.status ?? e?.response?.status ?? null;
          if (AWF_MODELS_URL_RETRYABLE_STATUSES.has(status)) {
            throw e;
          }
          logger(`awf-reflect: models fetch error for ${modelsUrl}: ${getErrorMessage(err)}`);
          return null;
        } finally {
          clearTimeout(timer);
        }
      },
      retryConfig,
      `awf-reflect models fetch for ${modelsUrl}`
    );
  } catch (err) {
    /** @type {any} */
    const e = err;
    const original = e?.originalError || e;
    const status = original?.status ?? original?.response?.status ?? null;
    if (AWF_MODELS_URL_RETRYABLE_STATUSES.has(status)) {
      logger(`awf-reflect: models fetch returned ${status} for ${modelsUrl}`);
      return null;
    }
    logger(`awf-reflect: models fetch error for ${modelsUrl}: ${getErrorMessage(err)}`);
    return null;
  }
}

/**
 * Enrich a reflect response by fetching models for configured endpoints where
 * the api-proxy's startup fetch left models as null.
 *
 * This is a best-effort fallback: failures are logged but do not throw.
 *
 * @param {any} reflectData - Parsed /reflect response (mutated in-place)
 * @param {number} timeoutMs - Per-request timeout for models_url fetches
 * @param {(msg: string) => void} logger
 * @returns {Promise<void>}
 */
async function enrichReflectModels(reflectData, timeoutMs, logger) {
  const endpoints = Array.isArray(reflectData.endpoints) ? reflectData.endpoints : [];
  const fetches = endpoints
    .filter(ep => ep && ep.configured && ep.models === null && ep.models_url)
    .map(async ep => {
      const models = await fetchModelsFromUrl(ep.models_url, timeoutMs, logger);
      if (models) {
        ep.models = models;
      }
    });
  if (fetches.length > 0) {
    await Promise.allSettled(fetches);
  }
}

/**
 * Fetch the AWF API proxy /reflect endpoint and persist the response to disk.
 *
 * The /reflect endpoint is exposed by the api-proxy sidecar on each started provider port.
 * The active provider's gateway port should be used rather than a hardcoded port, since
 * port 10000 (the OpenAI sidecar) is only started when OpenAI credentials are configured.
 * This information is saved to AWF_REFLECT_OUTPUT_PATH so the post-run GitHub Actions step
 * (awf_reflect_summary.cjs) can include it in the step summary without requiring the
 * containers to still be running.
 *
 * When the api-proxy's startup model-fetch produced null models for a configured provider
 * (e.g. due to a transient upstream error), the function makes a best-effort fallback fetch
 * directly to each endpoint's models_url. The api-proxy injects the correct auth headers
 * when forwarding these requests, giving us a second chance at getting model data.
 *
 * The function is best-effort: any network or parse error is logged but does not abort
 * the agent run.
 *
 * @param {{
 *   reflectUrl?: string,
 *   outputPath?: string,
 *   timeoutMs?: number,
 *   modelsTimeoutMs?: number,
 *   logger?: (msg: string) => void,
 *   writeFileSync?: (path: string, data: string, options: object) => void,
 * }=} options
 * @returns {Promise<{
 *   ok: boolean,
 *   reflectUrl: string,
 *   outputPath: string,
 *   bytesWritten?: number,
 *   reflectData?: object,
 *   reason?: "disabled"|"unexpected_status"|"timeout"|"request_failed",
 *   status?: number,
 *   error?: string,
 * }>}
 */
async function fetchAWFReflect(options) {
  const reflectUrl = (options && options.reflectUrl) || AWF_API_PROXY_REFLECT_URL;
  const outputPath = (options && options.outputPath) || AWF_REFLECT_OUTPUT_PATH;
  const timeoutMs = options && options.timeoutMs != null ? options.timeoutMs : AWF_REFLECT_TIMEOUT_MS;
  const modelsTimeoutMs = options && options.modelsTimeoutMs != null ? options.modelsTimeoutMs : AWF_MODELS_URL_TIMEOUT_MS;
  const logger = (options && options.logger) || DEFAULT_REFLECT_LOGGER;
  const writeFile = (options && options.writeFileSync) || fs.writeFileSync;

  if (process.env.GH_AW_SKIP_REFLECT === "true") {
    logger("awf-reflect: disabled by GH_AW_SKIP_REFLECT");
    return { ok: false, reflectUrl, outputPath, reason: "disabled" };
  }

  logger(`awf-reflect: fetching ${reflectUrl} (timeout=${timeoutMs}ms)`);

  const ac = new AbortController();
  let timedOut = false;
  const timer = setTimeout(() => {
    timedOut = true;
    logger(`awf-reflect: request timed out after ${timeoutMs}ms`);
    ac.abort();
  }, timeoutMs);

  try {
    const res = await fetch(reflectUrl, { signal: ac.signal });
    if (!res.ok) {
      logger(`awf-reflect: unexpected status ${res.status}, skipping`);
      return {
        ok: false,
        reflectUrl,
        outputPath,
        reason: "unexpected_status",
        status: res.status,
      };
    }

    /** @type {any} */
    const reflectData = await res.json();
    // Attempt to fill in null models for configured providers by fetching directly
    // from each endpoint's models_url. The api-proxy injects auth headers when
    // forwarding these requests, so this succeeds without needing the raw API keys.
    await enrichReflectModels(reflectData, modelsTimeoutMs, logger);
    const enrichedBody = JSON.stringify(reflectData);
    let bytesWritten;
    try {
      fs.mkdirSync(path.dirname(outputPath), { recursive: true });
      writeFile(outputPath, enrichedBody, { encoding: "utf8" });
      bytesWritten = enrichedBody.length;
      logger(`awf-reflect: saved ${enrichedBody.length}B to ${outputPath}`);
    } catch (persistErr) {
      const persistError = /** @type {Error} */ persistErr;
      logger(`awf-reflect: unable to persist reflect payload to ${outputPath}: ${persistError.message}`);
    }
    return {
      ok: true,
      reflectUrl,
      outputPath,
      ...(bytesWritten != null ? { bytesWritten } : {}),
      reflectData,
    };
  } catch (err) {
    const e = /** @type {Error} */ err;
    if (e.name === "AbortError") {
      return {
        ok: false,
        reflectUrl,
        outputPath,
        reason: "timeout",
        error: timedOut ? `request timed out after ${timeoutMs}ms` : e.message,
      };
    }
    logger(`awf-reflect: request failed: ${e.message}`);
    return {
      ok: false,
      reflectUrl,
      outputPath,
      reason: "request_failed",
      error: e.message,
    };
  } finally {
    clearTimeout(timer);
  }
}

/**
 * Wait until a provider listener (e.g. http://api-proxy:10002) accepts a real TCP
 * connection, or time out.
 *
 * This guards against startup races where /reflect is available but a per-provider
 * listener has not yet bound/started accepting connections.
 *
 * For "https:" baseUrls, the probe performs a full TLS handshake (via `tls.connect`) rather
 * than a bare TCP connect, since a raw TCP accept can succeed well before the TLS listener
 * is actually able to negotiate a secure session and serve requests.
 *
 * @param {{
 *   baseUrl: string,
 *   timeoutMs?: number,
 *   retryDelayMs?: number,
 *   perAttemptTimeoutMs?: number,
 *   logger?: (msg: string) => void,
 *   connectImpl?: (opts: { host: string, port: number }) => import("net").Socket,
 * }} options - `connectImpl`, when provided, overrides the default connect implementation for
 *   both http:// and https:// baseUrls (test-only hook). The readiness event awaited is still
 *   derived from the baseUrl's protocol: for `https:` baseUrls the returned socket must emit
 *   `"secureConnect"` (not `"connect"`) to be treated as ready, matching the real `tls.connect`
 *   behavior; for `http:` baseUrls it must emit `"connect"`.
 * @returns {Promise<{ ok: true } | { ok: false, reason: "invalid_base_url" | "timeout", error: string }>}
 */
async function waitForProviderListenerReady(options) {
  const logger = options?.logger ?? DEFAULT_REFLECT_LOGGER;
  const timeoutMs = options?.timeoutMs ?? AWF_PROVIDER_LISTENER_READY_TIMEOUT_MS;
  const retryDelayMs = options?.retryDelayMs ?? AWF_PROVIDER_LISTENER_READY_RETRY_MS;
  const perAttemptTimeoutMsRaw = options?.perAttemptTimeoutMs ?? AWF_PROVIDER_LISTENER_READY_PROBE_TIMEOUT_MS;
  const perAttemptTimeoutMs = Number.isFinite(perAttemptTimeoutMsRaw) && perAttemptTimeoutMsRaw > 0 ? perAttemptTimeoutMsRaw : AWF_PROVIDER_LISTENER_READY_PROBE_TIMEOUT_MS;
  const baseUrl = String(options?.baseUrl ?? "").trim();
  if (!baseUrl) {
    return { ok: false, reason: "invalid_base_url", error: "baseUrl is empty" };
  }

  let parsed;
  try {
    parsed = new URL(baseUrl);
  } catch {
    return { ok: false, reason: "invalid_base_url", error: `invalid baseUrl: ${baseUrl}` };
  }
  const host = parsed.hostname;
  const port = parsed.port ? Number.parseInt(parsed.port, 10) : parsed.protocol === "https:" ? 443 : 80;
  if (!host || !Number.isFinite(port) || port <= 0) {
    return { ok: false, reason: "invalid_base_url", error: `baseUrl missing host/port: ${baseUrl}` };
  }
  // For https:// providers, a bare TCP accept does not prove the listener can complete a TLS
  // handshake. Probe with tls.connect and wait for "secureConnect" so the readiness gate lines
  // up with the actual failure mode (handshake/startup errors), not just an open port.
  const isHttps = parsed.protocol === "https:";
  const readyEvent = isHttps ? "secureConnect" : "connect";
  const connectImpl = options?.connectImpl ?? (isHttps ? opts => tls.connect({ ...opts, servername: opts.host }) : opts => net.connect(opts));

  logger(`awf-reflect: waiting for provider listener readiness at ${host}:${port} (timeout=${timeoutMs}ms)`);
  const startedAt = Date.now();
  let lastError = "connection not ready";
  while (Date.now() - startedAt < timeoutMs) {
    const remainingBudgetMs = timeoutMs - (Date.now() - startedAt);
    const attemptTimeoutMs = Math.max(1, Math.min(perAttemptTimeoutMs, remainingBudgetMs));
    const ready = await new Promise(resolve => {
      const socket = connectImpl({ host, port });
      let settled = false;
      const settle = value => {
        if (settled) return;
        settled = true;
        resolve(value);
      };
      let timer;
      const clear = () => clearTimeout(timer);
      timer = setTimeout(() => {
        clear();
        lastError = `connect attempt timed out after ${attemptTimeoutMs}ms`;
        settle(false);
        socket.destroy();
      }, attemptTimeoutMs);
      socket.once(readyEvent, () => {
        clear();
        // Settle as ready before tearing down the socket, and keep the "error" listener
        // installed: destroy() can surface a late/trailing error (e.g. an abrupt RST), and an
        // EventEmitter with no "error" listener would throw and terminate the process. The
        // handler below ignores errors once the probe is settled.
        settle(true);
        socket.destroy();
      });
      socket.once("error", err => {
        if (settled) return;
        clear();
        lastError = getErrorMessage(err);
        socket.destroy();
        settle(false);
      });
    });
    if (ready) {
      logger(`awf-reflect: provider listener is accepting connections at ${host}:${port}`);
      return { ok: true };
    }
    const remainingAfterAttemptMs = timeoutMs - (Date.now() - startedAt);
    if (remainingAfterAttemptMs <= 0) {
      break;
    }
    await sleep(Math.min(retryDelayMs, remainingAfterAttemptMs));
  }
  logger(`awf-reflect: provider listener readiness timed out for ${host}:${port} (${lastError})`);
  return { ok: false, reason: "timeout", error: lastError };
}

/**
 * Returns true when the model name matches well-known Anthropic naming patterns:
 * "claude-*" prefix, or "-opus", "-haiku", or "-sonnet" as a segment or suffix.
 *
 * @param {string} model - Lower-cased, trimmed model name.
 * @returns {boolean}
 */
function isAnthropicModelName(model) {
  return model.startsWith("claude-") || model.includes("-opus-") || model.endsWith("-opus") || model.includes("-haiku-") || model.endsWith("-haiku") || model.includes("-sonnet-") || model.endsWith("-sonnet");
}

/**
 * Returns true when the model name matches well-known OpenAI naming patterns:
 * "gpt-*" prefix, or o1/o3/o4 reasoning models.
 *
 * @param {string} model - Lower-cased, trimmed model name.
 * @returns {boolean}
 */
function isOpenAIModelName(model) {
  return model.startsWith("gpt-") || /^o[134][-.]/.test(model) || model === "o1" || model === "o3" || model === "o4";
}

/**
 * Look up a model entry in the models.json catalog, case-insensitively.
 * Exact model IDs take precedence over the base-model fallback for `-utility` variants.
 *
 * @param {any} modelsJson
 * @param {string} modelName
 * @param {string | null | undefined} [providerName]
 * @returns {any | null}
 */
function getCatalogModelEntry(modelsJson, modelName, providerName) {
  const model = String(modelName || "")
    .split("?")[0]
    .toLowerCase()
    .trim();
  const provider = String(providerName || "")
    .toLowerCase()
    .trim();
  if (!model || modelsJson == null || typeof modelsJson !== "object" || Array.isArray(modelsJson)) {
    return null;
  }
  const providers = modelsJson.providers;
  if (!providers || typeof providers !== "object" || Array.isArray(providers)) {
    return null;
  }
  const providerEntries = provider
    ? Object.entries(providers).filter(
        ([name]) =>
          String(name || "")
            .toLowerCase()
            .trim() === provider
      )
    : Object.entries(providers);
  const modelEntriesByProvider = [];
  for (const [, providerData] of providerEntries) {
    const models = providerData && typeof providerData === "object" ? providerData.models : null;
    if (!models || typeof models !== "object" || Array.isArray(models)) continue;
    const modelEntriesByName = new Map();
    for (const [catalogModel, catalogEntry] of Object.entries(models)) {
      const normalizedCatalogModel = String(catalogModel || "")
        .toLowerCase()
        .trim();
      if (!modelEntriesByName.has(normalizedCatalogModel)) modelEntriesByName.set(normalizedCatalogModel, catalogEntry);
    }
    modelEntriesByProvider.push(modelEntriesByName);
  }
  const lookupModelNames = [model];
  if (model.endsWith("-utility")) lookupModelNames.push(model.slice(0, -"-utility".length));
  for (const lookupModelName of lookupModelNames) {
    for (const modelEntriesByName of modelEntriesByProvider) {
      if (!modelEntriesByName.has(lookupModelName)) continue;
      const catalogEntry = modelEntriesByName.get(lookupModelName);
      return catalogEntry && typeof catalogEntry === "object" && !Array.isArray(catalogEntry) ? catalogEntry : null;
    }
  }
  return null;
}

/**
 * Infer the Copilot SDK provider type for a given endpoint provider name and model name.
 *
 * The SDK's `ProviderConfig.type` field determines which API format the SDK uses when
 * communicating with the `baseUrl` endpoint:
 *   - "anthropic" — Anthropic Messages API (required for direct Anthropic API endpoints)
 *   - "azure"     — Azure OpenAI API
 *   - "openai"    — OpenAI-compatible API (default; used for Copilot, OpenAI, Gemini, etc.)
 *
 * Resolution order:
 *   1. Endpoint provider name mapping (most authoritative for BYOK endpoints).
 *   2. Model catalog lookup via the `modelsJson` catalog (explicit `provider_type` field).
 *   3. Well-known model name heuristics (e.g. "claude-*" → "anthropic", "gpt-*" → "openai").
 *   4. Default: "openai".
 *
 * @param {string} endpointProvider - The `provider` field from the AWF reflect endpoint entry.
 * @param {string} modelName - The resolved model name to use for heuristic fallback.
 * @param {any} catalogEntryOrModelsJson - Matching models.json catalog entry or full catalog (optional).
 * @returns {"openai" | "azure" | "anthropic"}
 */
function inferProviderTypeForModel(endpointProvider, modelName, catalogEntryOrModelsJson) {
  // 1. Endpoint provider name mapping.
  const ep = String(endpointProvider || "")
    .toLowerCase()
    .trim();
  if (ep === "anthropic") return "anthropic";
  if (ep === "azure" || ep === "azure-openai" || ep === "azure_openai") return "azure";
  if (ep === "openai") return "openai";
  // GitHub Copilot provider is a multi-model proxy that always uses OpenAI wire protocol.
  if (ep === "copilot" || ep === "github-copilot") return "openai";
  // For unknown providers, fall through to model-based lookup.

  const model = String(modelName || "")
    .toLowerCase()
    .trim();
  const catalogEntry =
    catalogEntryOrModelsJson && typeof catalogEntryOrModelsJson === "object" && !Array.isArray(catalogEntryOrModelsJson) && "providers" in catalogEntryOrModelsJson
      ? getCatalogModelEntry(catalogEntryOrModelsJson, model)
      : catalogEntryOrModelsJson;

  // 2. Model catalog lookup.
  if (model) {
    const pt = catalogEntry && typeof catalogEntry.provider_type === "string" ? catalogEntry.provider_type.trim() : "";
    if (pt === "anthropic" || pt === "azure" || pt === "openai") return /** @type {"openai" | "azure" | "anthropic"} */ pt;
  }

  // 3. Well-known model name heuristics.
  if (model) {
    if (isAnthropicModelName(model)) return "anthropic";
    if (isOpenAIModelName(model)) return "openai";
  }

  // 4. Default.
  return "openai";
}

/**
 * Infer the SDK wire API for a model.
 *
 * Resolution order:
 *   1. For Anthropic provider types: undefined (wireApi ignored by SDK).
 *   2. `models.json` explicit `wire_api`/`wireApi`.
 *   3. Heuristic default for GPT-5+ models: "responses".
 *   4. Default for other OpenAI/Azure-compatible models: "completions".
 *
 * @param {"openai" | "azure" | "anthropic"} providerType
 * @param {string} modelName
 * @param {any} catalogEntryOrModelsJson
 * @returns {"completions" | "responses" | undefined}
 */
function inferWireApiForModel(providerType, modelName, catalogEntryOrModelsJson) {
  if (providerType === "anthropic") {
    return undefined;
  }
  const model = String(modelName || "").trim();
  if (!model) return undefined;
  const catalogEntry =
    catalogEntryOrModelsJson && typeof catalogEntryOrModelsJson === "object" && !Array.isArray(catalogEntryOrModelsJson) && "providers" in catalogEntryOrModelsJson
      ? getCatalogModelEntry(catalogEntryOrModelsJson, model)
      : catalogEntryOrModelsJson;
  // Keep the camelCase fallback for defensive compatibility with injected catalog
  // objects that bypass the normalized models.json pipeline.
  const rawWireApi = typeof catalogEntry?.wire_api === "string" ? catalogEntry.wire_api : typeof catalogEntry?.wireApi === "string" ? catalogEntry.wireApi : "";
  const normalizedWireApi = String(rawWireApi || "")
    .toLowerCase()
    .trim();
  if (normalizedWireApi === "responses" || normalizedWireApi === "completions") {
    return /** @type {"responses" | "completions"} */ normalizedWireApi;
  }
  if (/^gpt-(?:[5-9]|\d{2,})(?:[.-]|$)/i.test(model.split("?")[0])) {
    return "responses";
  }
  return "completions";
}

/**
 * Derive a base URL string from an endpoint object.
 * Prefers the origin of `models_url`; falls back to `http://api-proxy:<port>`.
 * Returns an empty string when neither is available.
 *
 * @param {{ models_url?: string | null, port?: number | null }} endpoint
 * @returns {string}
 */
function endpointBaseUrl(endpoint) {
  if (typeof endpoint.models_url === "string" && endpoint.models_url) {
    try {
      return rewriteAPIProxyURLForHostBridge(new URL(endpoint.models_url).origin);
    } catch {
      // fall through to port-based construction
    }
  }
  if (endpoint.port != null) {
    return rewriteAPIProxyURLForHostBridge(`http://api-proxy:${String(endpoint.port)}`);
  }
  return "";
}

/**
 * Derive a base URL (origin + path prefix, with any trailing `/models` segment
 * stripped) from a `models_url` value, applying the same api-proxy ->
 * host.docker.internal HOSTALIASES bridge rewrite as `endpointBaseUrl`.
 *
 * Harnesses that need a base URL for chat-completions requests (rather than
 * the models-listing endpoint) should use this instead of deriving the
 * base URL from `models_url` inline, so the api-proxy hostname rewrite is
 * never accidentally skipped.
 *
 * @param {string} modelsUrl
 * @param {NodeJS.ProcessEnv} [env]
 * @param {(path: string, encoding: BufferEncoding) => string} [readFileSync]
 * @returns {string}
 */
function deriveBaseUrlFromModelsURL(modelsUrl, env = process.env, readFileSync = fs.readFileSync) {
  let parsed;
  try {
    parsed = new URL(modelsUrl);
  } catch (error) {
    throw new Error(`Invalid models URL: ${modelsUrl}`, { cause: error });
  }
  const basePath = parsed.pathname.replace(/\/models\/?$/i, "");
  return rewriteAPIProxyURLForHostBridge(`${parsed.origin}${basePath}`, env, readFileSync);
}

/**
 * Resolve a configured provider endpoint from AWF /reflect data.
 *
 * @param {{
 *   provider?: string,
 *   reflectData: ReflectData | null | undefined,
 *   logger?: (msg: string) => void,
 * }} options
 * @returns {{ provider: string, endpointProvider: string, port: number|null, baseUrl: string } | null}
 */
function resolveProviderEndpointFromReflect(options) {
  const logger = (options && options.logger) || DEFAULT_REFLECT_LOGGER;
  const provider = normalizeReflectProviderName(options?.provider, "openai");
  const reflectData = options?.reflectData;
  /** @type {{ endpoints?: unknown } | null} */
  const reflectRecord = reflectData && typeof reflectData === "object" ? reflectData : null;
  const endpointCandidates = Array.isArray(reflectRecord?.endpoints) ? reflectRecord.endpoints : [];
  const endpoints = endpointCandidates.filter(/** @param {any} ep */ ep => ep && ep.configured === true);
  if (endpoints.length === 0) {
    logger(`awf-reflect: no configured endpoints available while resolving provider=${provider}`);
    return null;
  }

  /** @param {string} endpointProvider */
  const endpointProviderMatches = endpointProvider => {
    // Keep aliases aligned with pkg/workflow/llm_provider.go (llmProviderAliases).
    // If alias handling changes, update both places in the same PR.
    const normalized = normalizeReflectProviderName(endpointProvider);
    if (!normalized) return false;
    if (provider === REFLECT_PROVIDER_GITHUB) {
      return REFLECT_PROVIDER_ALIASES.github.has(normalized);
    }
    if (provider === REFLECT_PROVIDER_OPENAI) {
      return REFLECT_PROVIDER_ALIASES.openai.has(normalized);
    }
    if (provider === REFLECT_PROVIDER_ANTHROPIC) {
      return REFLECT_PROVIDER_ALIASES.anthropic.has(normalized);
    }
    return normalized === provider;
  };

  const matched = endpoints.find(ep => typeof ep?.provider === "string" && endpointProviderMatches(ep.provider)) || endpoints[0];
  const baseUrl = endpointBaseUrl(matched);
  if (!baseUrl) {
    logger(`awf-reflect: matched provider=${provider} but could not derive baseUrl`);
    return null;
  }
  const endpointProvider = String(matched.provider || "unknown");
  const parsedPort = matched.port == null ? null : Number(matched.port);
  const port = Number.isFinite(parsedPort) ? parsedPort : null;
  logger(`awf-reflect: provider=${provider} mapped to endpoint provider=${endpointProvider} baseUrl=${baseUrl}`);
  return { provider, endpointProvider, port, baseUrl };
}

/**
 * Resolve the OpenAI-compatible chat endpoint for a configured provider.
 *
 * Goose requires the endpoint origin and request path in separate environment
 * variables. The models URL is the authoritative source for provider-specific
 * path prefixes such as OpenAI's `/v1` and Copilot's versionless API.
 *
 * @param {{
 *   provider?: string,
 *   reflectData: ReflectData | null | undefined,
 *   logger?: (msg: string) => void,
 *   env?: NodeJS.ProcessEnv,
 *   readFileSync?: (path: string, encoding: BufferEncoding) => string,
 * }} options
 * @returns {{ provider: string, endpointProvider: string, host: string, basePath: string } | null}
 */
function resolveOpenAICompatibleEndpointFromReflect(options) {
  const logger = (options && options.logger) || DEFAULT_REFLECT_LOGGER;
  const provider = normalizeReflectProviderName(options?.provider);
  if (!provider) {
    logger("awf-reflect: provider is required for OpenAI-compatible endpoint resolution");
    return null;
  }

  const endpointCandidates = Array.isArray(options?.reflectData?.endpoints) ? options.reflectData.endpoints : [];
  const aliases = REFLECT_PROVIDER_ALIASES[provider] || new Set([provider]);
  const endpoint = endpointCandidates.find(ep => {
    if (!ep || ep.configured !== true || typeof ep.provider !== "string") return false;
    return aliases.has(normalizeReflectProviderName(ep.provider));
  });
  if (!endpoint) {
    logger(`awf-reflect: no configured endpoint found for provider=${provider}`);
    return null;
  }

  const endpointURL = endpoint.models_url;
  if (typeof endpointURL !== "string" || !endpointURL) {
    logger(`awf-reflect: configured provider=${provider} has no models URL`);
    return null;
  }

  try {
    const parsed = new URL(endpointURL);
    let path = parsed.pathname.replace(/\/+$/, "");
    if (!/\/models$/i.test(path)) {
      logger(`awf-reflect: models URL for provider=${provider} does not end in /models`);
      return null;
    }
    path = path.replace(/\/models$/i, "/chat/completions");
    const basePath = path.replace(/^\/+/, "");
    const endpointProvider = String(endpoint.provider);
    const host = rewriteAPIProxyURLForHostBridge(parsed.origin, options?.env, options?.readFileSync);
    logger(`awf-reflect: provider=${provider} mapped to endpoint provider=${endpointProvider} host=${host} basePath=${basePath}`);
    return { provider, endpointProvider, host, basePath };
  } catch {
    logger(`awf-reflect: invalid endpoint URL for provider=${provider}`);
    return null;
  }
}

/**
 * Resolve multi-provider BYOK configuration from AWF /reflect data.
 *
 * Returns `null` when no configured endpoints are present or the data is
 * unavailable.
 *
 * Each endpoint becomes a `NamedProviderConfig` (using the endpoint's `provider`
 * field as the stable name) and every model advertised by that endpoint becomes a
 * `ProviderModelConfig` tuple `{ id, provider }` referencing it.  Callers can
 * derive provider-qualified selection ids as `"<providerName>/<modelId>"` if needed.
 *
 * The primary model is the first model that matches `options.model` (if set),
 * otherwise the first model across all providers.
 * A valid `options.wireApi` overrides inference for the primary provider only,
 * except for Anthropic providers, which do not use an OpenAI wire API.
 *
 * @param {{
 *   model?: string,
 *   wireApi?: string,
 *   reflectData: ReflectData | null | undefined,
 *   modelsJson?: object | null,
 *   logger?: (msg: string) => void,
 * }} [options]
 * @returns {{
 *   model: string,
 *   providers: Array<{ name: string, type: "openai" | "azure" | "anthropic", baseUrl: string, wireApi?: "completions" | "responses" }>,
 *   models: Array<{ id: string, provider: string }>,
 * } | null}
 */
function resolveMultiProviderFromReflect(options) {
  const configuredModel = typeof options?.model === "string" ? options.model.trim() : "";
  const logger = (options && options.logger) || DEFAULT_REFLECT_LOGGER;

  const reflectData = options?.reflectData;
  if (reflectData == null) {
    logger("sdk-mode(multi): no reflect data provided; cannot resolve multi-provider config");
    return null;
  }

  /** @type {any} */
  const rd = reflectData;
  const endpoints = Array.isArray(rd?.endpoints) ? rd.endpoints.filter(ep => ep && ep.configured === true) : [];

  if (endpoints.length === 0) {
    logger(`sdk-mode(multi): no configured endpoints in awf-reflect data; cannot build multi-provider config`);
    return null;
  }

  /** @type {Array<{ name: string, type: "openai" | "azure" | "anthropic", baseUrl: string, wireApi?: "completions" | "responses" }>} */
  const providers = [];
  /** @type {Array<{ id: string, provider: string }>} */
  const models = [];

  // Track used provider names to avoid duplicates when multiple endpoints share the same
  // provider label (e.g. two "copilot" entries at different ports).
  /** @type {Map<string, number>} */
  const providerNameCount = new Map();

  for (const endpoint of endpoints) {
    const baseUrl = endpointBaseUrl(endpoint);
    if (!baseUrl) {
      logger(`sdk-mode(multi): skipping endpoint with no resolvable baseUrl (provider=${String(endpoint.provider || "unknown")})`);
      continue;
    }

    const rawProviderName = String(endpoint.provider || "").trim();
    if (!rawProviderName) {
      logger("sdk-mode(multi): skipping endpoint with no provider name");
      continue;
    }

    // Ensure unique provider names by appending a suffix when the same name appears twice.
    const existing = providerNameCount.get(rawProviderName) ?? 0;
    providerNameCount.set(rawProviderName, existing + 1);
    const providerName = existing === 0 ? rawProviderName : `${rawProviderName}-${existing}`;

    const endpointModels = Array.isArray(endpoint.models) ? endpoint.models.filter(m => typeof m === "string" && m.trim().length > 0) : [];

    // Infer provider type and wire API using the configured model if available,
    // otherwise fall back to the first model.
    // For multi-model providers (e.g. Copilot), different models may have different wire APIs,
    // so we prefer the configured model to ensure the correct wireApi is selected.
    const firstModel = endpointModels.length > 0 ? endpointModels[0] : "";
    const modelForInference = configuredModel && endpointModels.includes(configuredModel) ? configuredModel : firstModel;
    const catalogProviderName = rawProviderName.toLowerCase() === "copilot" ? "github-copilot" : rawProviderName;
    const catalogEntry = modelForInference ? getCatalogModelEntry(options?.modelsJson ?? null, modelForInference, catalogProviderName) : null;
    const providerType = inferProviderTypeForModel(rawProviderName, modelForInference, catalogEntry);
    const wireApi = inferWireApiForModel(providerType, modelForInference, catalogEntry);

    logger(
      `sdk-mode(multi): resolved provider="${providerName}" (raw="${rawProviderName}") type="${providerType}" wireApi="${wireApi || "(none)"}" ` +
        `inferredFrom="${modelForInference}" modelCount=${endpointModels.length} baseUrl="${baseUrl}"`
    );

    providers.push({
      name: providerName,
      type: providerType,
      baseUrl,
      ...(wireApi ? { wireApi } : {}),
    });

    for (const modelId of endpointModels) {
      models.push({ id: modelId, provider: providerName });
    }
  }

  if (providers.length === 0) {
    logger("sdk-mode(multi): no providers resolved from awf-reflect data; cannot build multi-provider config");
    return null;
  }

  // Determine the primary model: prefer the configured model if it appears in the model list;
  // otherwise fall back to the first model across all providers.
  let primaryModel = "";
  if (configuredModel) {
    const match = models.find(m => m.id === configuredModel);
    if (match) primaryModel = match.id;
  }
  if (!primaryModel && models.length > 0) {
    primaryModel = models[0].id;
  }

  if (!primaryModel) {
    logger("sdk-mode(multi): no models found in awf-reflect endpoints; cannot build multi-provider config");
    return null;
  }

  const primaryProviderName = models.find(m => m.id === primaryModel)?.provider;
  const primaryProvider = providers.find(p => p.name === primaryProviderName);
  const wireApi = String(options?.wireApi || "")
    .toLowerCase()
    .trim();
  if (primaryProvider && primaryProvider.type !== "anthropic" && (wireApi === "responses" || wireApi === "completions")) {
    primaryProvider.wireApi = wireApi;
    logger(`sdk-mode(multi): primary provider="${primaryProvider.name}" wireApi="${wireApi}" selected from configured wire API`);
  }

  logger(`sdk-mode(multi): resolved ${providers.length} providers, ${models.length} models (primary model: ${primaryModel})`);
  return { model: primaryModel, providers, models };
}

if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    AWF_API_PROXY_REFLECT_URL,
    AWF_REFLECT_OUTPUT_PATH,
    AWF_REFLECT_TIMEOUT_MS,
    AWF_MODELS_URL_TIMEOUT_MS,
    AWF_MODELS_URL_MAX_ATTEMPTS,
    AWF_MODELS_URL_RETRYABLE_STATUSES,
    AWF_MODELS_URL_RETRY_BASE_MS,
    AWF_MODELS_URL_RETRY_MAX_MS,
    AWF_PROVIDER_LISTENER_READY_TIMEOUT_MS,
    AWF_PROVIDER_LISTENER_READY_RETRY_MS,
    AWF_PROVIDER_LISTENER_READY_PROBE_TIMEOUT_MS,
    DEFAULT_API_PROXY_HOST_BRIDGE,
    GEMINI_MODEL_NAME_PREFIX,
    parseReflectTimeoutMs,
    enrichReflectModels,
    extractModelIds,
    fetchAWFReflect,
    fetchModelsFromUrl,
    waitForProviderListenerReady,
    getCatalogModelEntry,
    hasAPIProxyLocalhostAlias,
    inferProviderTypeForModel,
    inferWireApiForModel,
    deriveBaseUrlFromModelsURL,
    normalizeReflectProviderName,
    REFLECT_PROVIDER_ALIASES,
    resolveOpenAICompatibleEndpointFromReflect,
    resolveProviderEndpointFromReflect,
    resolveMultiProviderFromReflect,
    rewriteAPIProxyURLForHostBridge,
  };
}
