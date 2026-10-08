// @ts-check
"use strict";

const { createLogger } = require("./mcp_logger.cjs");

/** @param {string} namespace */
function debugEnabled(namespace) {
  if (process.env.ACTIONS_RUNNER_DEBUG === "true" || process.env.RUNNER_DEBUG === "1") return true;
  let enabled = false;
  for (const token of (process.env.DEBUG || "").split(/[\s,]+/).filter(Boolean)) {
    const excluded = token.startsWith("-");
    const pattern = excluded ? token.slice(1) : token;
    const expression = pattern
      .split("*")
      .map(part => part.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"))
      .join(".*");
    if (!new RegExp("^" + expression + "$").test(namespace)) continue;
    if (excluded) return false;
    enabled = true;
  }
  return enabled;
}

/**
 * Events and field names must be static developer-authored labels. Values are
 * counts/flags only: never pass identities, payloads, credentials or raw errors.
 * @param {string} component
 */
function createWorkQueueLogger(component) {
  const namespace = `work-queue:${component}`;
  const logger = createLogger(namespace);
  /** @param {string} event @param {Record<string, unknown>} [fields] */
  function debug(event, fields = {}) {
    if (!debugEnabled(namespace)) return;
    const metadata = Object.entries(fields).filter(([, value]) => typeof value === "boolean" || (typeof value === "number" && Number.isSafeInteger(value) && value >= 0));
    logger.debug([event, ...metadata.map(([key, value]) => `${key}=${value}`)].join(" "));
  }
  /** @param {string} event @param {unknown} error */
  function failure(event, error) {
    if (!debugEnabled(namespace)) return;
    // Do not use debugError: API errors may contain request bodies and tokens.
    const status = error && typeof error === "object" ? Object.getOwnPropertyDescriptor(error, "status")?.value : undefined;
    debug(event, { failed: true, ...(Number.isInteger(status) && status >= 100 && status <= 599 ? { http_status: status } : {}) });
  }
  return { debug, failure };
}

module.exports = { createWorkQueueLogger, debugEnabled };
