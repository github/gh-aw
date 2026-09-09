// @ts-check

"use strict";

const { fetch: undiciFetch, ProxyAgent } = require("undici");

const DEFAULT_MAX_LENGTH = 5_000;
const MAX_MAX_LENGTH = 20_000;
const DEFAULT_TIMEOUT_MS = 30_000;
const MAX_REDIRECTS = 5;
const MAX_RESPONSE_BYTES = 10 * 1024 * 1024;
const REDIRECT_STATUSES = new Set([301, 302, 303, 307, 308]);

/**
 * @param {unknown} value
 * @param {string} name
 * @param {number} defaultValue
 * @param {number} minimum
 * @param {number} maximum
 * @returns {number}
 */
function boundedInteger(value, name, defaultValue, minimum, maximum) {
  if (value === undefined) return defaultValue;
  if (!Number.isSafeInteger(value) || Number(value) < minimum || Number(value) > maximum) {
    throw new Error(`${name} must be an integer between ${minimum} and ${maximum}`);
  }
  return Number(value);
}

/**
 * Validate URL syntax without making destination-policy decisions. AWF owns
 * allow/deny policy for public, private, loopback, and enterprise destinations.
 *
 * @param {unknown} value
 * @param {URL | undefined} [base]
 * @returns {URL}
 */
function parseWebFetchURL(value, base) {
  if (typeof value !== "string" || value.trim() === "") {
    throw new Error("url must be a non-empty string");
  }
  let url;
  try {
    url = base ? new URL(value, base) : new URL(value);
  } catch {
    throw new Error("url must be an absolute HTTP or HTTPS URL");
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new Error("url must use the http or https protocol");
  }
  if (url.username || url.password) {
    throw new Error("url must not contain credentials");
  }
  return url;
}

/**
 * @param {ReadableStream<Uint8Array> | null} body
 * @returns {Promise<string>}
 */
async function readBoundedResponseBody(body) {
  if (!body) return "";
  const reader = body.getReader();
  /** @type {Uint8Array[]} */
  const chunks = [];
  let totalBytes = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      totalBytes += value.byteLength;
      if (totalBytes > MAX_RESPONSE_BYTES) {
        await reader.cancel();
        throw new Error(`response body exceeds ${MAX_RESPONSE_BYTES} bytes`);
      }
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }
  return Buffer.concat(chunks.map(chunk => Buffer.from(chunk))).toString("utf8");
}

/**
 * @param {string} value
 * @returns {string}
 */
function decodeBasicHTMLEntities(value) {
  return value
    .replace(/&nbsp;/gi, " ")
    .replace(/&amp;/gi, "&")
    .replace(/&lt;/gi, "<")
    .replace(/&gt;/gi, ">")
    .replace(/&quot;/gi, '"')
    .replace(/&#(?:39|x27);/gi, "'");
}

/**
 * @param {string} content
 * @param {string} contentType
 * @param {boolean} raw
 * @returns {string}
 */
function simplifyWebContent(content, contentType, raw) {
  if (raw || !contentType.toLowerCase().includes("text/html")) {
    return content;
  }
  return decodeBasicHTMLEntities(
    content
      .replace(/<(script|style|noscript)\b[^>]*>[\s\S]*?<\/\1>/gi, "")
      .replace(/<!--[\s\S]*?-->/g, "")
      .replace(/<\/?(?:p|div|section|article|main|header|footer|nav|aside|h[1-6]|li|ul|ol|table|tr|blockquote|pre)\b[^>]*>/gi, "\n")
      .replace(/<br\s*\/?>/gi, "\n")
      .replace(/<[^>]+>/g, "")
  )
    .replace(/[ \t]+\n/g, "\n")
    .replace(/\n[ \t]+/g, "\n")
    .replace(/\n{3,}/g, "\n\n")
    .trim();
}

/**
 * Use AWF's explicit proxy environment for every destination, including
 * loopback/private names. Do not honor NO_PROXY here: AWF must observe and
 * decide each model-requested destination.
 *
 * @param {{fetchImpl?: typeof fetch, env?: NodeJS.ProcessEnv}} options
 */
function createWebFetchTransport(options) {
  if (options.fetchImpl) {
    return { fetch: options.fetchImpl, close: async () => {} };
  }
  const env = options.env ?? process.env;
  /** @type {Map<string, InstanceType<typeof ProxyAgent>>} */
  const agents = new Map();
  return {
    /**
     * @param {URL} url
     * @param {{method: "GET", redirect: "manual", signal: AbortSignal}} init
     */
    fetch: async (url, init) => {
      const proxyURL = url.protocol === "https:" ? env.HTTPS_PROXY || env.https_proxy || env.HTTP_PROXY || env.http_proxy : env.HTTP_PROXY || env.http_proxy || env.HTTPS_PROXY || env.https_proxy;
      if (!proxyURL) {
        // AWF must observe every web_fetch destination through its proxy. Fail closed
        // instead of silently issuing an unobserved direct request when the proxy
        // environment is missing or misconfigured.
        throw new Error("web_fetch requires an AWF proxy (HTTPS_PROXY/HTTP_PROXY); refusing unobserved direct request");
      }
      let dispatcher = agents.get(proxyURL);
      if (!dispatcher) {
        dispatcher = new ProxyAgent(proxyURL);
        agents.set(proxyURL, dispatcher);
      }
      return undiciFetch(url, { ...init, dispatcher });
    },
    close: async () => {
      await Promise.all([...agents.values()].map(agent => agent.close()));
    },
  };
}

/**
 * @typedef {{
 *   url: string,
 *   raw?: boolean,
 *   max_length?: number,
 *   start_index?: number,
 * }} CopilotSDKWebFetchInput
 */

/**
 * @param {CopilotSDKWebFetchInput} input
 * @param {{
 *   fetchImpl?: typeof fetch,
 *   env?: NodeJS.ProcessEnv,
 *   timeoutMs?: number,
 *   maxRedirects?: number,
 * }} [options]
 * @returns {Promise<string>}
 */
async function executeCopilotSDKWebFetch(input, options = {}) {
  if (!input || typeof input !== "object") {
    throw new Error("web_fetch input must be an object");
  }
  if (options.fetchImpl !== undefined && typeof options.fetchImpl !== "function") {
    throw new Error("web_fetch requires a Fetch API implementation");
  }
  const transport = createWebFetchTransport(options);

  const maxLength = boundedInteger(input.max_length, "max_length", DEFAULT_MAX_LENGTH, 1, MAX_MAX_LENGTH);
  const startIndex = boundedInteger(input.start_index, "start_index", 0, 0, Number.MAX_SAFE_INTEGER);
  const timeoutMs = boundedInteger(options.timeoutMs, "timeoutMs", DEFAULT_TIMEOUT_MS, 1, 5 * 60_000);
  const maxRedirects = boundedInteger(options.maxRedirects, "maxRedirects", MAX_REDIRECTS, 0, 20);
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutMs);
  timeout.unref?.();

  let currentURL = parseWebFetchURL(input.url);
  let response;
  try {
    for (let redirectCount = 0; ; redirectCount++) {
      response = await transport.fetch(currentURL, {
        method: "GET",
        redirect: "manual",
        signal: controller.signal,
      });
      if (!REDIRECT_STATUSES.has(response.status)) break;
      const location = response.headers.get("location");
      if (!location) break;
      if (redirectCount >= maxRedirects) {
        throw new Error(`web_fetch exceeded ${maxRedirects} redirects`);
      }
      await response.body?.cancel();
      currentURL = parseWebFetchURL(location, currentURL);
    }

    const contentType = response.headers.get("content-type") ?? "";
    const body = simplifyWebContent(await readBoundedResponseBody(response.body), contentType, input.raw === true);
    const content = body.slice(startIndex, startIndex + maxLength);
    return JSON.stringify({
      url: currentURL.toString(),
      status: response.status,
      content_type: contentType,
      start_index: startIndex,
      end_index: startIndex + content.length,
      total_length: body.length,
      truncated: startIndex + content.length < body.length,
      content,
    });
  } finally {
    clearTimeout(timeout);
    await transport.close();
  }
}

/**
 * Adapter for the SDK Tool<unknown> handler boundary.
 *
 * @param {any} input
 * @param {{fetchImpl?: typeof fetch, env?: NodeJS.ProcessEnv, timeoutMs?: number, maxRedirects?: number}} options
 * @returns {Promise<string>}
 */
function executeCopilotSDKWebFetchToolHandler(input, options) {
  return executeCopilotSDKWebFetch(input, options);
}

/**
 * Create the compiler-controlled web_fetch tool. @github/copilot-sdk 1.0.11
 * explicitly supports replacing a same-name built-in through
 * overridesBuiltInTool. Assert that defineTool preserves the override contract
 * so future incompatible SDK changes fail during session initialization.
 *
 * @param {typeof import("@github/copilot-sdk").defineTool} defineTool
 * @param {{fetchImpl?: typeof fetch, env?: NodeJS.ProcessEnv, timeoutMs?: number, maxRedirects?: number}} [options]
 * @returns {import("@github/copilot-sdk").Tool<any>}
 */
function createCopilotSDKWebFetchTool(defineTool, options = {}) {
  if (typeof defineTool !== "function") {
    throw new Error("Copilot SDK defineTool is required to register web_fetch");
  }
  const tool = defineTool("web_fetch", {
    description: "Fetch an HTTP or HTTPS URL from inside the AWF-protected agent boundary.",
    parameters: {
      type: "object",
      additionalProperties: false,
      required: ["url"],
      properties: {
        url: { type: "string", description: "Absolute HTTP or HTTPS URL to fetch." },
        raw: { type: "boolean", description: "Return raw HTML instead of simplified text." },
        max_length: { type: "integer", minimum: 1, maximum: MAX_MAX_LENGTH, description: "Maximum response characters to return." },
        start_index: { type: "integer", minimum: 0, description: "Character offset for pagination." },
      },
    },
    overridesBuiltInTool: true,
    defer: "never",
    handler: input => executeCopilotSDKWebFetchToolHandler(input, options),
  });
  if (!tool || tool.name !== "web_fetch" || tool.overridesBuiltInTool !== true) {
    throw new Error("Copilot SDK defineTool did not preserve the required web_fetch override contract");
  }
  return tool;
}

module.exports = {
  DEFAULT_MAX_LENGTH,
  MAX_MAX_LENGTH,
  DEFAULT_TIMEOUT_MS,
  MAX_REDIRECTS,
  MAX_RESPONSE_BYTES,
  boundedInteger,
  parseWebFetchURL,
  readBoundedResponseBody,
  simplifyWebContent,
  createWebFetchTransport,
  executeCopilotSDKWebFetch,
  createCopilotSDKWebFetchTool,
};
