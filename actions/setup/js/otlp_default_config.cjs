// @ts-check
"use strict";

const { maskSecret, getOTLPRunnerMaskValues } = require("./actions_secret_masking.cjs");

/**
 * Resolve defaults only inside a step, never in Actions expressions: the runner
 * initially knows only the complete JSON secret, not its individual credentials.
 * A present atomic configuration must never borrow fields from legacy defaults.
 *
 * @param {(key: string, value: string) => void} [exportEnv]
 */
function resolveOTLPDefaultConfig(exportEnv) {
  const env = process.env;
  if (env.GH_AW_OTLP_ENDPOINTS !== undefined || !["GH_AW_DEFAULT_OTLP_CONFIG", "GH_AW_DEFAULT_OTLP_ENDPOINT", "GH_AW_DEFAULT_OTLP_HEADERS"].some(key => env[key] !== undefined)) {
    return;
  }

  let endpoint = "";
  let headers = "";
  const raw = env.GH_AW_DEFAULT_OTLP_CONFIG || "";
  if (env.OTEL_EXPORTER_OTLP_ENDPOINT !== undefined) {
    endpoint = env.OTEL_EXPORTER_OTLP_ENDPOINT;
    headers = env.OTEL_EXPORTER_OTLP_HEADERS || "";
  } else if (raw) {
    try {
      const config = JSON.parse(raw);
      if (config && typeof config.endpoint === "string") endpoint = config.endpoint;
      if (config && typeof config.headers === "string") headers = config.headers;
    } catch {
      // Do not expose JSON parser diagnostics: they can contain secret fragments.
      global.core?.warning?.("Invalid GH_AW_DEFAULT_OTLP_CONFIG JSON; OTLP export is disabled");
    }
    headers = env.OTEL_EXPORTER_OTLP_HEADERS ?? headers;
  } else {
    endpoint = env.GH_AW_DEFAULT_OTLP_ENDPOINT || "";
    headers = env.OTEL_EXPORTER_OTLP_HEADERS ?? env.GH_AW_DEFAULT_OTLP_HEADERS ?? "";
  }

  maskSecret(endpoint);
  maskSecret(headers);
  // Reject file-command injection, without logging any of the rejected data.
  if (/[\r\n]/.test(endpoint) || /[\r\n]/.test(headers)) {
    endpoint = "";
    headers = "";
    global.core?.warning?.("OTLP defaults contain line breaks; OTLP export is disabled");
  }
  env.OTEL_EXPORTER_OTLP_ENDPOINT = endpoint;
  env.OTEL_EXPORTER_OTLP_HEADERS = headers;
  for (const secret of getOTLPRunnerMaskValues()) maskSecret(secret);
  const endpoints = JSON.stringify(endpoint && headers ? [{ url: endpoint, headers }] : []);
  if (endpoint && headers) maskSecret(endpoints);
  env.GH_AW_OTLP_ENDPOINTS = endpoints;
  for (const key of ["OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_HEADERS", "GH_AW_OTLP_ENDPOINTS"]) {
    exportEnv?.(key, env[key] || "");
  }
}

module.exports = { resolveOTLPDefaultConfig };
