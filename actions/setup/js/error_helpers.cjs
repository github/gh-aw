// @ts-check

/**
 * Detect whether a string looks like an HTML page rather than a plain error message.
 * Used to sanitize GitHub's "Unicorn" / gateway error HTML responses so they don't
 * pollute CI logs with hundreds of lines of markup.
 *
 * @param {string} str - The string to inspect
 * @returns {boolean} True when the string appears to be an HTML document
 */
function isHtmlContent(str) {
  return /^\s*<!DOCTYPE\s/i.test(str) || /^\s*<html[\s>]/i.test(str);
}

/**
 * Safely extract an error message from an unknown error value.
 * Handles Error instances, objects with message properties, and other values.
 *
 * When the extracted message looks like an HTML page (e.g. GitHub's "Unicorn"
 * 504 error page), it is replaced with a concise human-readable description so
 * that CI logs stay readable.  The HTTP status code is included when available.
 *
 * @param {unknown} error - The error value to extract a message from
 * @returns {string} The error message as a string
 */
function getErrorMessage(error) {
  // prettier-ignore
  const errorAsAny = /** @type {any} */ (error);
  let message;
  if (error instanceof Error) {
    message = error.message;
  } else if (error && typeof error === "object" && "message" in error) {
    message = typeof error.message === "string" ? error.message : String(error.message);
  } else {
    message = String(error);
  }

  if (isHtmlContent(message)) {
    const status = errorAsAny != null && typeof errorAsAny.status === "number" ? errorAsAny.status : null;
    return status != null ? `GitHub returned an unexpected HTML response (HTTP ${status})` : "GitHub returned an unexpected HTML response";
  }

  if (!message.trim()) {
    const status = errorAsAny?.status ?? errorAsAny?.response?.status;
    const requestId = errorAsAny?.response?.headers?.["x-github-request-id"];
    const context = [];
    if (Number.isInteger(status) && status >= 100 && status <= 599) context.push(`HTTP ${status}`);
    if (typeof requestId === "string" && /^[A-Za-z0-9:-]{1,128}$/.test(requestId)) context.push(`request ID ${requestId}`);
    return context.length ? `GitHub API request failed (${context.join("; ")})` : "Unknown error";
  }

  return message;
}

/**
 * Check if an error is due to a locked issue/PR/discussion.
 * GitHub API returns 403 with specific messages for locked resources.
 * This helper is used to determine if an operation should be silently ignored.
 *
 * @param {unknown} error - The error value to check
 * @returns {boolean} True if error is due to locked resource, false otherwise
 */
function isLockedError(error) {
  // Check if the error has a 403 status code
  const is403Error = error && typeof error === "object" && "status" in error && error.status === 403;
  if (!is403Error) {
    return false;
  }

  // Check if the error message mentions "locked"
  const errorMessage = getErrorMessage(error);
  const hasLockedMessage = Boolean(errorMessage && (errorMessage.includes("locked") || errorMessage.includes("Lock conversation")));

  // Only return true if it's BOTH a 403 status code AND mentions locked
  return hasLockedMessage;
}

/**
 * Check if an error is due to a GitHub API rate limit being exceeded.
 * This includes both installation-level and user-level rate limits.
 * Used to determine if a check should fail-open (allow workflow to proceed)
 * rather than hard-failing when the error is transient.
 *
 * @param {unknown} error - The error value to check
 * @returns {boolean} True if error is due to API rate limiting, false otherwise
 */
function isRateLimitError(error) {
  const errorMessage = getErrorMessage(error);
  return /\bapi rate limit\b|\brate limit exceeded\b/i.test(errorMessage);
}

/**
 * Check for an Undici connection timeout by code or message.
 * @param {unknown} error - The error value to check
 * @returns {boolean} True if the error is a connection timeout
 */
function isConnectTimeoutError(error) {
  if (!error || typeof error !== "object") {
    return false;
  }
  return ("code" in error && error.code === "UND_ERR_CONNECT_TIMEOUT") || ("message" in error && error.message === "Connect Timeout Error");
}

module.exports = { getErrorMessage, isHtmlContent, isLockedError, isRateLimitError, isConnectTimeoutError };
