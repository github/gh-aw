// @ts-check
"use strict";

const TRANSIENT_CODES = new Set(["ETIMEDOUT", "ECONNRESET", "UND_ERR_SOCKET", "UND_ERR_CONNECT_TIMEOUT", "UND_ERR_HEADERS_TIMEOUT", "UND_ERR_BODY_TIMEOUT"]);

function isTransientQueueReadError(error) {
  let transient = false;
  for (let depth = 0; depth < 5 && error && typeof error === "object"; depth++, error = error.cause) {
    const status = error.status ?? error.response?.status;
    if (error.code === "ERR_TRANSPORT_INTEGRITY" || error.response?.headers?.["retry-after"] !== undefined) return false;
    if (status !== undefined) {
      if (!Number.isInteger(status) || status < 500 || status > 599) return false;
      transient = true;
    }
    if (TRANSIENT_CODES.has(error.code)) transient = true;
  }
  return transient && !(error && typeof error === "object");
}

// Call only around an idempotent metadata GET, never around validation or writes.
async function readQueueMetadata(read, log, event, sleepFn = delay => new Promise(resolve => setTimeout(resolve, delay))) {
  for (let attempt = 1; ; attempt++) {
    try {
      return await read();
    } catch (error) {
      const retry = attempt < 3 && isTransientQueueReadError(error);
      log.failure(`${event}.${retry ? "retry" : "failed"}`, error, true);
      log.diagnostic?.(`${event}.attempt`, { attempt, max_attempts: 3, retry, delay_ms: retry ? 1000 : 0 });
      if (!retry) throw error;
      await sleepFn(1000);
    }
  }
}

module.exports = { isTransientQueueReadError, readQueueMetadata };
