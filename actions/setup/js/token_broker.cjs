// @ts-check
"use strict";

const { readBoundedBody } = require("./admission_protocol.cjs");

const SKEW_MS = 30000;
const TIMEOUT_MS = 10000;
const MESSAGES = Object.freeze({
  CONFIGURATION: "Token broker configuration is invalid.",
  OIDC: "Token broker identity acquisition failed.",
  UNAVAILABLE: "Token broker is unavailable.",
  UNAUTHORIZED: "Token broker authorization failed.",
  UNSUPPORTED: "Token broker capability is unsupported.",
  MALFORMED_RESPONSE: "Token broker response is invalid.",
});

/** @param {keyof typeof MESSAGES} kind */
function failure(kind) {
  return Object.assign(new Error(MESSAGES[kind]), { code: `ERR_TOKEN_BROKER_${kind}` });
}

/** @param {NodeJS.ProcessEnv} [env] */
function isTokenBrokerEnabled(env = process.env) {
  try {
    const config = JSON.parse(env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG || "");
    return config !== null && typeof config === "object" && !Array.isArray(config) && config._token_broker === true;
  } catch {
    return false;
  }
}

/** @param {unknown} value */
function isCredential(value) {
  return typeof value === "string" && /^[\x21-\x7e]+$/.test(value);
}

/** @param {unknown} value @param {keyof typeof MESSAGES} kind */
function httpsUrl(value, kind) {
  try {
    if (typeof value !== "string" || !/^https:\/\//i.test(value) || /[\s\\#]/u.test(value) || /^https:\/\/[^/?]*@/i.test(value)) throw failure(kind);
    const url = new URL(value);
    if (url.protocol !== "https:" || !url.hostname || url.username || url.password) throw failure(kind);
    return url;
  } catch {
    throw failure(kind);
  }
}

/** @param {Response} response */
function discard(response) {
  void response.body?.cancel().catch(() => {
    // Cancellation is best-effort; raw transport diagnostics must remain private.
  });
}

/** @param {unknown} value */
function expiration(value) {
  if (typeof value !== "string") throw failure("MALFORMED_RESPONSE");
  const parts = /^(\d{4})-(\d{2})-(\d{2})[Tt](\d{2}):(\d{2}):(\d{2})(?:\.\d+)?([Zz]|[+-]\d{2}:\d{2})$/.exec(value);
  if (!parts) throw failure("MALFORMED_RESPONSE");
  const [, year, month, day, hour, minute, second, offset] = parts;
  const leap = Number(year) % 4 === 0 && (Number(year) % 100 !== 0 || Number(year) % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (
    Number(month) < 1 ||
    Number(month) > 12 ||
    Number(day) < 1 ||
    Number(day) > days[Number(month) - 1] ||
    Number(hour) > 23 ||
    Number(minute) > 59 ||
    Number(second) > 59 ||
    (offset.length > 1 && (Number(offset.slice(1, 3)) > 23 || Number(offset.slice(4)) > 59))
  )
    throw failure("MALFORMED_RESPONSE");
  const result = Date.parse(value);
  if (!Number.isFinite(result)) throw failure("MALFORMED_RESPONSE");
  return result;
}

/** @typedef {{env?: NodeJS.ProcessEnv, fetchImpl?: typeof fetch, now?: () => number, mask?: (token: string) => void | Promise<void>}} BrokerOptions */

/**
 * Tokens exist only in this client's memory. Only inference and task capabilities are supported;
 * HTTP 400, 404, 422 and 501 mean unsupported, with no fallback or retry.
 * @param {BrokerOptions} [options]
 */
function createTokenBrokerClient({ env = process.env, fetchImpl = fetch, now = Date.now, mask = async token => (await import("@actions/core")).setSecret(token) } = {}) {
  /** @type {Map<string, {token: string, expiresAt: number}>} */
  const cache = new Map();
  /** @type {Map<string, Promise<string>>} */
  const pending = new Map();

  /** @param {string} tokenType @returns {Promise<string>} */
  async function getToken(tokenType) {
    if (tokenType !== "copilot-request" && tokenType !== "copilot-task") throw failure("UNSUPPORTED");
    if (!Object.hasOwn(env, "GH_AW_TOKEN_BROKER_URL")) throw failure("CONFIGURATION");
    const snapshot = { ...env };
    const endpoint = snapshot.GH_AW_TOKEN_BROKER_URL || "";
    httpsUrl(endpoint, "CONFIGURATION");
    const key = JSON.stringify([snapshot.GITHUB_RUN_ID, snapshot.GITHUB_RUN_ATTEMPT, snapshot.GITHUB_JOB, snapshot.GITHUB_REPOSITORY, endpoint, tokenType]);
    const cached = cache.get(key);
    if (cached && cached.expiresAt > now() + SKEW_MS) return cached.token;
    cache.delete(key);
    const existing = pending.get(key);
    if (existing) return existing;

    const controller = new AbortController();
    /** @type {ReturnType<typeof setTimeout> | undefined} */
    let timer;
    /** @type {Promise<never>} */
    const timeout = new Promise((_, reject) => {
      timer = setTimeout(() => {
        reject(failure("UNAVAILABLE"));
        controller.abort();
      }, TIMEOUT_MS);
    });

    const exchange = async () => {
      const oidcUrl = httpsUrl(snapshot.ACTIONS_ID_TOKEN_REQUEST_URL, "OIDC");
      const bootstrap = snapshot.ACTIONS_ID_TOKEN_REQUEST_TOKEN;
      if (!isCredential(bootstrap)) throw failure("OIDC");
      oidcUrl.searchParams.set("audience", endpoint || "");
      /** @type {string} */
      let oidcToken;
      try {
        const response = await fetchImpl(oidcUrl, { headers: { authorization: `bearer ${bootstrap}`, accept: "application/json" }, redirect: "manual", signal: controller.signal });
        if (response.status !== 200 || response.redirected) {
          discard(response);
          throw failure("OIDC");
        }
        const result = JSON.parse(await readBoundedBody(response, 65536));
        if (typeof result?.value !== "string") throw failure("OIDC");
        await mask(result.value);
        if (!/^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/.test(result.value)) throw failure("OIDC");
        const payload = JSON.parse(Buffer.from(result.value.split(".")[1], "base64url").toString("utf8"));
        if (typeof payload?.exp !== "number" || !Number.isFinite(payload.exp) || payload.exp * 1000 <= now()) throw failure("OIDC");
        oidcToken = result.value;
      } catch {
        throw failure("OIDC");
      }
      if (controller.signal.aborted) throw failure("UNAVAILABLE");
      let response;
      try {
        response = await fetchImpl(endpoint, {
          method: "POST",
          headers: { "content-type": "application/json", accept: "application/json", "cache-control": "no-store" },
          body: JSON.stringify({ token_type: tokenType, oidc_token: oidcToken }),
          redirect: "manual",
          signal: controller.signal,
        });
      } catch {
        throw failure("UNAVAILABLE");
      }
      if (response.status !== 200 || response.redirected) {
        discard(response);
        if (response.status === 401 || response.status === 403) throw failure("UNAUTHORIZED");
        if ([400, 404, 422, 501].includes(response.status)) throw failure("UNSUPPORTED");
        throw failure("UNAVAILABLE");
      }
      try {
        const text = await readBoundedBody(response, 65536);
        const result = JSON.parse(text);
        if (typeof result?.token === "string") await mask(result.token);
        const string = '"(?:[^"\\\\\\u0000-\\u001f]|\\\\(?:["\\\\/bfnrt]|u[0-9a-fA-F]{4}))*"';
        // eslint-disable-next-line gh-aw-custom/require-escaped-regexp-interpolation -- All fragments are fixed JSON grammar, never response data.
        if (!new RegExp(`^\\s*\\{\\s*${string}\\s*:\\s*${string}\\s*,\\s*${string}\\s*:\\s*${string}\\s*\\}\\s*$`).test(text)) throw failure("MALFORMED_RESPONSE");
        if (!result || typeof result !== "object" || Array.isArray(result) || Object.keys(result).length !== 2 || !Object.hasOwn(result, "token") || !Object.hasOwn(result, "expires_at") || !isCredential(result.token))
          throw failure("MALFORMED_RESPONSE");
        const expiresAt = expiration(result.expires_at);
        if (expiresAt <= now() + SKEW_MS) throw failure("MALFORMED_RESPONSE");
        if (controller.signal.aborted) throw failure("UNAVAILABLE");
        cache.set(key, { token: result.token, expiresAt });
        return result.token;
      } catch {
        throw failure("MALFORMED_RESPONSE");
      }
    };
    const request = Promise.race([exchange(), timeout]).finally(() => {
      clearTimeout(timer);
      pending.delete(key);
    });
    pending.set(key, request);
    return request;
  }
  return { getToken };
}

/** @type {ReturnType<typeof createTokenBrokerClient> | undefined} */
let defaultClient;
/** @type {WeakMap<BrokerOptions, ReturnType<typeof createTokenBrokerClient>>} */
const optionClients = new WeakMap();

/** @param {string} tokenType @param {BrokerOptions} [options] @returns {Promise<string>} */
function getBrokerToken(tokenType, options) {
  if (!options) {
    defaultClient ||= createTokenBrokerClient();
    return defaultClient.getToken(tokenType);
  }
  let client = optionClients.get(options);
  if (!client) {
    client = createTokenBrokerClient(options);
    optionClients.set(options, client);
  }
  return client.getToken(tokenType);
}

module.exports = { createTokenBrokerClient, getBrokerToken, isTokenBrokerEnabled };
