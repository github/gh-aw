// @ts-check
"use strict";

const { maskSecret } = require("./actions_secret_masking.cjs");

/**
 * Apply the atomic default OTLP configuration from the intact Actions secret.
 * @returns {boolean} Whether the atomic configuration secret was present
 */
function applyDefaultOTLPConfig() {
  const raw = process.env.GH_AW_DEFAULT_OTLP_CONFIG || "";
  if (!raw) return false;

  maskSecret(raw);
  let endpoint = "";
  let headers = "";
  try {
    const config = JSON.parse(raw);
    if (typeof config.endpoint === "string" && config.endpoint && typeof config.headers === "string" && config.headers) {
      endpoint = config.endpoint;
      headers = config.headers;
    } else {
      global.core?.warning?.("[otlp] invalid default OTLP configuration; skipping export");
    }
  } catch {
    global.core?.warning?.("[otlp] invalid default OTLP configuration; skipping export");
  }

  if (endpoint) maskSecret(endpoint);
  if (headers) maskSecret(headers);
  process.env.OTEL_EXPORTER_OTLP_ENDPOINT = endpoint;
  process.env.OTEL_EXPORTER_OTLP_HEADERS = headers;
  process.env.GH_AW_OTLP_ENDPOINTS = endpoint && headers ? JSON.stringify([{ url: endpoint, headers }]) : "";
  return true;
}

module.exports = { applyDefaultOTLPConfig };
