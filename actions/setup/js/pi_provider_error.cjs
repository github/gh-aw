// @ts-check
"use strict";

const MAX_PROVIDER_ERROR_LENGTH = 1000;

/**
 * Reduce provider errors to bounded, single-line diagnostics without exposing credentials.
 *
 * @param {unknown} value
 * @returns {string}
 */
function sanitizeProviderErrorMessage(value) {
  let message;
  if (value === undefined || value === null) {
    message = "";
  } else if (typeof value === "string") {
    message = value;
  } else {
    try {
      message = JSON.stringify(value);
    } catch {
      message = String(value);
    }
  }

  for (const [name, secret] of Object.entries(process.env)) {
    if (/(?:TOKEN|KEY|SECRET|PASSWORD|CREDENTIAL)/i.test(name) && secret && secret.length >= 6) {
      message = message.split(secret).join("[REDACTED]");
    }
  }

  message = message
    .replace(/\bBearer\s+[^\s,;"}]+/gi, "******")
    .replace(/\b(?:gh[pousr]_|ghs_|github_pat_)[A-Za-z0-9._-]+\b/g, "[REDACTED]")
    .replace(/\bsk-(?:proj-|ant-api03-)?[A-Za-z0-9_-]{20,}\b/g, "[REDACTED]")
    .replace(/[\u0000-\u001f\u007f-\u009f]+/g, " ");
  if (message.length > MAX_PROVIDER_ERROR_LENGTH) {
    message = `${message.slice(0, MAX_PROVIDER_ERROR_LENGTH)}…`;
  }
  return message;
}

/**
 * Extract the HTTP status and response message from a Pi provider error.
 *
 * @param {unknown} value
 * @param {number|undefined} responseStatus
 * @returns {{ status?: number, message: string }}
 */
function getProviderErrorDetails(value, responseStatus) {
  const rawMessage = sanitizeProviderErrorMessage(value);
  const match = /^\s*(?:HTTP\s*)?(\d{3})(?::\s*|\s+)([\s\S]*)$/i.exec(rawMessage);
  const parsedStatus = match ? Number(match[1]) : undefined;
  return {
    ...(typeof responseStatus === "number" ? { status: responseStatus } : parsedStatus ? { status: parsedStatus } : {}),
    message: match ? match[2] : rawMessage,
  };
}

module.exports = { sanitizeProviderErrorMessage, getProviderErrorDetails };
