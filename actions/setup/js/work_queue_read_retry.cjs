// @ts-check
"use strict";

const TRANSIENT_CODES = new Set(["ETIMEDOUT", "ECONNRESET", "UND_ERR_SOCKET", "UND_ERR_CONNECT_TIMEOUT", "UND_ERR_HEADERS_TIMEOUT", "UND_ERR_BODY_TIMEOUT"]);

async function metadataRequest(read) {
  // Octokit's fetch transport uses AbortSignal, not the legacy timeout option.
  const request = { retries: 0, retryCount: 0, signal: AbortSignal.timeout(15000) };
  const response = await read(request);
  // Octokit can swallow a body-read abort and return an empty successful payload.
  if (request.signal.aborted) throw Object.assign(new Error("work queue metadata request timed out", { cause: request.signal.reason }), { code: "ETIMEDOUT" });
  return response;
}

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

function statusFromQueueReadError(error) {
  try {
    return error && typeof error === "object" ? Object.getOwnPropertyDescriptor(error, "status")?.value : undefined;
  } catch {
    return undefined;
  }
}

// Call only around an idempotent metadata GET, never around validation or writes.
/** @param {{attemptBudget?: {remaining: number}, expectedStatus?: number}} [options] */
async function readQueueMetadata(read, log, event, sleepFn = delay => new Promise(resolve => setTimeout(resolve, delay)), options = {}) {
  const { attemptBudget, expectedStatus } = options;
  const maxAttempts = Math.min(3, attemptBudget?.remaining ?? 3);
  if (maxAttempts < 1) throw new Error("queue metadata retry budget exhausted");
  for (let attempt = 1; ; attempt++) {
    if (attemptBudget) attemptBudget.remaining--;
    try {
      return await read();
    } catch (error) {
      const retry = attempt < maxAttempts && isTransientQueueReadError(error);
      if (retry || expectedStatus === undefined || statusFromQueueReadError(error) !== expectedStatus) log.failure(`${event}.${retry ? "retry" : "failed"}`, error, true);
      else log.debug?.(`${event}.expected_absence`, { status: expectedStatus });
      log.diagnostic?.(`${event}.attempt`, { attempt, max_attempts: maxAttempts, retry, delay_ms: retry ? 1000 : 0 });
      if (!retry) throw error;
      await sleepFn(1000);
    }
  }
}

module.exports = { isTransientQueueReadError, readQueueMetadata, metadataRequest };
