// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Error recovery utilities for safe output operations
 * Provides retry logic with exponential backoff for transient failures
 */

const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_API, SAFE_OUTPUT_E010, RATE_LIMIT_EXCEEDED } = require("./error_codes.cjs");
const { logRetryEvent } = require("./github_rate_limit_logger.cjs");

/**
 * Configuration for retry behavior
 * @typedef {Object} RetryConfig
 * @property {number} maxRetries - Maximum number of retry attempts (default: 3)
 * @property {number} initialDelayMs - Initial delay in milliseconds (default: 1000)
 * @property {number} maxDelayMs - Maximum delay in milliseconds (default: 10000)
 * @property {number} backoffMultiplier - Backoff multiplier for exponential backoff (default: 2)
 * @property {number} jitterMs - Maximum random jitter in milliseconds added to each retry delay (default: 100)
 * @property {(error: any) => boolean} shouldRetry - Function to determine if error is retryable
 */

/**
 * Default configuration for retry behavior
 * @type {RetryConfig}
 */
const DEFAULT_RETRY_CONFIG = {
  maxRetries: 3,
  initialDelayMs: 1000,
  maxDelayMs: 10000,
  backoffMultiplier: 2,
  jitterMs: 100,
  shouldRetry: isTransientError,
};

/**
 * Retry configuration for GitHub API rate-limit scenarios.
 * Uses longer delays to handle installation token exhaustion during burst windows.
 *
 * Backoff sequence (approximate — jitter of up to 5 s is added per retry):
 *   ~30 s → ~60 s → ~120 s → ~240 s → ~240 s (capped)
 *
 * Note: The first actual retry sleep = initialDelayMs * backoffMultiplier = 15 000 * 2 = 30 000 ms.
 * @type {RetryConfig}
 */
const RATE_LIMIT_RETRY_CONFIG = {
  maxRetries: 5,
  initialDelayMs: 15000, // 15 s × backoffMultiplier(2) = 30 s first retry
  maxDelayMs: 240000, // 4-minute cap
  backoffMultiplier: 2,
  jitterMs: 5000, // Up to 5 s of jitter to spread concurrent retries
  shouldRetry: isTransientError,
};

/**
 * Message indicators used to detect GitHub API rate-limit errors.
 * Matched case-insensitively against lower-cased error messages.
 * Entries are kept lower-case because we lower-case input messages before substring matching.
 * @type {string[]}
 */
const RATE_LIMIT_INDICATORS = ["rate limit", "secondary rate limit", "abuse detection", "too many requests"];
const TRANSIENT_HTTP_STATUSES = new Set([408, 425, 429, 500, 502, 503, 504]);
const MAX_TIMER_DELAY_MS = 2 ** 31 - 1;

/**
 * @param {string} messageLower - Lower-cased error message
 * @returns {boolean} True when message indicates GitHub rate limiting
 */
function hasRateLimitIndicator(messageLower) {
  return RATE_LIMIT_INDICATORS.some(pattern => messageLower.includes(pattern));
}

/**
 * Determine if an error is transient and worth retrying
 * @param {any} error - The error to check
 * @returns {boolean} True if the error is transient and should be retried
 */
function isTransientError(error) {
  const errorMsg = getErrorMessage(error);
  const errorMsgLower = errorMsg.trimStart().toLowerCase();
  const status = error?.response?.status ?? error?.status ?? null;

  // GitHub REST APIs may crash and return an HTML error page (e.g. the "Unicorn!"
  // 500 page) instead of JSON. Detect this by checking for an HTML doctype at the
  // start of the error message and treat it as a transient server error.
  if (errorMsgLower.startsWith("<!doctype html") || errorMsgLower.includes("unexpected html response")) {
    return true;
  }

  // Status-based rate-limit detection handles cases where the message text is
  // not descriptive enough (e.g. Octokit errors with status 429 and a generic
  // "Request failed" message). Must be checked before the text patterns.
  if (isRateLimitError(error)) {
    return true;
  }

  // Fetch and Octokit errors do not consistently include the status text in
  // their message, so classify standard transient HTTP statuses directly.
  if (TRANSIENT_HTTP_STATUSES.has(Number(status))) {
    return true;
  }

  // Network-related errors that are likely transient
  const transientPatterns = [
    "network",
    "timeout",
    "econnreset",
    "enotfound",
    "etimedout",
    "econnrefused",
    "socket hang up",
    "502 bad gateway",
    "503 service unavailable",
    "504 gateway timeout",
    ...RATE_LIMIT_INDICATORS, // GitHub rate limiting signals (including HTTP 429 text)
    "temporarily unavailable",
    "no server is currently available", // GitHub API server unavailability
  ];

  return transientPatterns.some(pattern => errorMsgLower.includes(pattern));
}

/**
 * Sleep for a specified duration
 * @param {number} ms - Duration in milliseconds
 * @returns {Promise<void>}
 */
function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

/**
 * Read a response header from either a Fetch Headers instance or a plain object.
 * @param {Headers|Record<string, any>|null|undefined} headers
 * @param {string} name
 * @returns {any}
 */
function getHeader(headers, name) {
  if (!headers) return null;
  if (typeof headers.get === "function") {
    return headers.get(name);
  }
  const matchingKey = Object.keys(headers).find(key => key.toLowerCase() === name);
  return matchingKey === undefined ? null : headers[matchingKey];
}

/**
 * Determine whether an error response represents a GitHub rate-limit condition.
 * @param {any} error - The error to classify
 * @returns {{headers: Headers|Record<string, any>|null, isRateLimit: boolean}}
 */
function getRateLimitErrorDetails(error) {
  const status = error?.response?.status ?? error?.status ?? null;
  const headers = error?.response?.headers ?? error?.headers ?? null;
  const remainingHeader = getHeader(headers, "x-ratelimit-remaining");
  const remainingExhausted = remainingHeader != null && parseInt(remainingHeader, 10) === 0;
  return { headers, isRateLimit: status === 429 || (status === 403 && remainingExhausted) };
}

/**
 * Extract the Retry-After delay in milliseconds from a retryable HTTP error.
 *
 * Applies when the response status indicates a rate-limit condition or service
 * unavailability:
 *   - HTTP 429 (Too Many Requests)
 *   - HTTP 403 with `x-ratelimit-remaining: 0` (GitHub secondary rate limit)
 *   - HTTP 503 (Service Unavailable)
 *
 * In those cases GitHub returns one of two headers:
 *   - `retry-after`       – integer seconds OR HTTP-date to wait until
 *   - `x-ratelimit-reset` – Unix timestamp (seconds) when the quota resets
 *
 * For any other status returns null so normal exponential backoff applies.
 *
 * @param {any} error - The error object from a failed GitHub API call
 * @returns {number|null} Milliseconds to wait, or null if not a rate-limit response
 */
function getRetryAfterMs(error) {
  // Octokit surfaces response headers via error.response.headers or error.headers
  const status = error?.response?.status ?? error?.status ?? null;
  const { headers, isRateLimit } = getRateLimitErrorDetails(error);
  if (!headers) return null;

  // Only honour rate-limit headers for genuine rate-limit responses.
  // GitHub uses 429 for primary rate limits and 403 for secondary rate limits
  // (the latter always sets x-ratelimit-remaining to "0").
  const retryAfter = getHeader(headers, "retry-after");
  const supportsRetryAfter = isRateLimit || status === 503;

  if (!supportsRetryAfter) return null;

  // retry-after: number of seconds OR HTTP-date (highest priority)
  if (retryAfter != null) {
    const seconds = parseInt(retryAfter, 10);
    if (!Number.isNaN(seconds) && seconds > 0) {
      return seconds * 1000;
    }
    const retryAtMs = Date.parse(String(retryAfter));
    if (!Number.isNaN(retryAtMs)) {
      const waitMs = retryAtMs - Date.now();
      if (waitMs > 0) {
        return waitMs;
      }
    }
  }

  // x-ratelimit-reset: Unix timestamp — derive wait time from clock delta
  const resetAt = getHeader(headers, "x-ratelimit-reset");
  if (isRateLimit && resetAt != null) {
    const resetTimestampMs = parseInt(resetAt, 10) * 1000;
    if (!Number.isNaN(resetTimestampMs)) {
      const waitMs = resetTimestampMs - Date.now();
      if (waitMs > 0) {
        return waitMs;
      }
    }
  }

  return null;
}

/**
 * Determine whether an error is specifically a GitHub API rate-limit error.
 * @param {any} error - The error to classify
 * @returns {boolean} True when the error indicates primary or secondary rate limiting
 */
function isRateLimitError(error) {
  if (getRateLimitErrorDetails(error).isRateLimit) {
    return true;
  }

  const errorMsg = getErrorMessage(error).toLowerCase();
  return hasRateLimitIndicator(errorMsg);
}

/**
 * Execute an operation with retry logic and exponential backoff
 * @template T
 * @param {() => Promise<T>} operation - The async operation to execute
 * @param {Partial<RetryConfig>} [config] - Retry configuration (optional)
 * @param {string} [operationName] - Name of the operation for logging
 * @returns {Promise<T>} The result of the operation
 * @throws {Error} If all retry attempts fail
 */
async function withRetry(operation, config = {}, operationName = "operation") {
  const fullConfig = { ...DEFAULT_RETRY_CONFIG, ...config };
  validateRetryConfig(operation, fullConfig);
  let lastError;
  let delay = fullConfig.initialDelayMs;

  for (let attempt = 0; attempt <= fullConfig.maxRetries; attempt++) {
    try {
      if (attempt > 0) {
        const jitter = fullConfig.jitterMs > 0 ? Math.floor(Math.random() * fullConfig.jitterMs) : 0;
        const delayWithJitter = Math.min(delay + jitter, fullConfig.maxDelayMs);
        core.info(`Retry attempt ${attempt}/${fullConfig.maxRetries} for ${operationName} after ${delayWithJitter}ms delay`);
        logRetryEvent(lastError, operationName, attempt, delayWithJitter);
        await sleep(delayWithJitter);
      }

      const result = await operation();

      if (attempt > 0) {
        core.info(`✓ ${operationName} succeeded on retry attempt ${attempt}`);
      }

      return result;
    } catch (error) {
      lastError = error;
      const errorMsg = getErrorMessage(error);

      // Check if this error should be retried
      if (!fullConfig.shouldRetry(error)) {
        core.debug(`${operationName} failed with non-retryable error: ${errorMsg}`);
        throw enhanceError(error, {
          operation: operationName,
          attempt: attempt + 1,
          retryable: false,
          suggestion: "This error cannot be resolved by retrying. Please check the error details and fix the underlying issue.",
        });
      }

      // If this was the last attempt, throw the enhanced error
      if (attempt === fullConfig.maxRetries) {
        core.warning(`${operationName} failed after ${fullConfig.maxRetries} retry attempts: ${errorMsg}`);
        // E010 applies only when retry budget is at least 3 retries.
        const hasEnoughRetriesForE010 = fullConfig.maxRetries >= 3;
        if (hasEnoughRetriesForE010 && isRateLimitError(error)) {
          throw enhanceError(error, {
            operation: operationName,
            attempt: attempt + 1,
            maxRetries: fullConfig.maxRetries,
            retryable: true,
            code: `${SAFE_OUTPUT_E010} ${RATE_LIMIT_EXCEEDED}`,
            suggestion: "RATE_LIMIT_EXCEEDED: GitHub API rate limit persisted after 3 retries.",
          });
        }
        throw enhanceError(error, {
          operation: operationName,
          attempt: attempt + 1,
          maxRetries: fullConfig.maxRetries,
          retryable: true,
          suggestion: "All retry attempts exhausted. This may be a persistent issue. Check GitHub status or try again later.",
        });
      }

      // Log the retry attempt
      core.warning(`${operationName} failed (attempt ${attempt + 1}/${fullConfig.maxRetries + 1}): ${errorMsg}`);

      // Calculate next delay: honour Retry-After header when present, otherwise
      // use exponential backoff.  Either way the result is capped at maxDelayMs.
      const retryAfterMs = getRetryAfterMs(error);
      if (retryAfterMs !== null) {
        const cappedDelay = Math.min(retryAfterMs, fullConfig.maxDelayMs);
        if (cappedDelay < retryAfterMs) {
          core.info(`Retry-After header detected for ${operationName}: server requested ${retryAfterMs}ms wait, capped to ${cappedDelay}ms (maxDelayMs)`);
        } else {
          core.info(`Retry-After header detected for ${operationName}: next retry will wait ${cappedDelay}ms`);
        }
        delay = cappedDelay;
      } else {
        // Calculate next delay with exponential backoff
        delay = Math.min(delay * fullConfig.backoffMultiplier, fullConfig.maxDelayMs);
      }
    }
  }

  // This should never be reached, but TypeScript needs it
  throw lastError;
}

/**
 * Reject invalid retry configuration before starting an operation.
 * @param {unknown} operation
 * @param {RetryConfig} config
 */
function validateRetryConfig(operation, config) {
  if (typeof operation !== "function") {
    throw new TypeError("Retry operation must be a function");
  }

  for (const key of ["maxRetries", "initialDelayMs", "maxDelayMs", "jitterMs"]) {
    const value = config[key];
    if (!Number.isSafeInteger(value) || value < 0) {
      throw new RangeError(`Retry configuration ${key} must be a non-negative safe integer`);
    }
  }
  for (const key of ["initialDelayMs", "maxDelayMs", "jitterMs"]) {
    if (config[key] > MAX_TIMER_DELAY_MS) {
      throw new RangeError(`Retry configuration ${key} must not exceed ${MAX_TIMER_DELAY_MS}`);
    }
  }
  if (!Number.isFinite(config.backoffMultiplier) || config.backoffMultiplier < 1) {
    throw new RangeError("Retry configuration backoffMultiplier must be a finite number greater than or equal to 1");
  }
  if (typeof config.shouldRetry !== "function") {
    throw new TypeError("Retry configuration shouldRetry must be a function");
  }
}

/**
 * Enhance an error with additional context for better debugging
 * @param {any} error - The original error
 * @param {Object} context - Additional context to add
 * @param {string} context.operation - Name of the operation that failed
 * @param {number} context.attempt - Current attempt number
 * @param {number} [context.maxRetries] - Maximum retry attempts
 * @param {boolean} context.retryable - Whether the error is retryable
 * @param {string} context.suggestion - Suggestion for resolving the error
 * @param {string} [context.code] - Optional standardized error code (e.g., ERR_API)
 * @returns {Error} Enhanced error with context
 */
function enhanceError(error, context) {
  const originalMessage = getErrorMessage(error);
  const timestamp = new Date().toISOString();

  let enhancedMessage = `${context.code || ERR_API}: [${timestamp}] ${context.operation} failed`;

  if (context.maxRetries !== undefined) {
    enhancedMessage += ` after ${context.maxRetries} retry attempts`;
  } else {
    enhancedMessage += ` (attempt ${context.attempt})`;
  }

  enhancedMessage += `\n\nOriginal error: ${originalMessage}`;
  enhancedMessage += `\nRetryable: ${context.retryable}`;
  enhancedMessage += `\nSuggestion: ${context.suggestion}`;

  const enhancedError = new Error(enhancedMessage);
  // @ts-ignore - Adding custom properties to Error
  enhancedError.originalError = error;
  // @ts-ignore - Adding custom properties to Error
  enhancedError.context = context;

  return enhancedError;
}

/**
 * Create a validation error with helpful context
 * @param {string} field - The field that failed validation
 * @param {any} value - The invalid value (will be truncated if too long)
 * @param {string} reason - Why the validation failed
 * @param {string} [suggestion] - Optional suggestion for fixing the issue
 * @returns {Error} Validation error with context
 */
function createValidationError(field, value, reason, suggestion) {
  const timestamp = new Date().toISOString();
  const truncatedValue = String(value).length > 100 ? String(value).substring(0, 97) + "..." : String(value);

  let message = `[${timestamp}] Validation failed for field '${field}'`;
  message += `\n\nValue: ${truncatedValue}`;
  message += `\nReason: ${reason}`;

  if (suggestion) {
    message += `\nSuggestion: ${suggestion}`;
  }

  const error = new Error(message);
  // @ts-ignore - Adding custom properties to Error
  error.isValidationError = true;
  // @ts-ignore - Adding custom properties to Error
  error.field = field;
  // @ts-ignore - Adding custom properties to Error
  error.value = value;

  return error;
}

/**
 * Create an operation error with context about what was being attempted
 * @param {string} operation - Description of the operation
 * @param {string} entityType - Type of entity being operated on (e.g., "issue", "PR")
 * @param {any} cause - The underlying error
 * @param {string|number} [entityId] - ID of the entity (optional)
 * @param {string} [suggestion] - Optional suggestion for resolution
 * @returns {Error} Operation error with context
 */
function createOperationError(operation, entityType, cause, entityId, suggestion) {
  const timestamp = new Date().toISOString();
  const causeMsg = getErrorMessage(cause);

  let message = `[${timestamp}] Failed to ${operation} ${entityType}`;

  if (entityId !== undefined) {
    message += ` #${entityId}`;
  }

  message += `\n\nUnderlying error: ${causeMsg}`;

  if (suggestion) {
    message += `\nSuggestion: ${suggestion}`;
  } else {
    // Provide default suggestions based on error type
    if (isTransientError(cause)) {
      message += `\nSuggestion: This appears to be a transient error. The operation will be retried automatically.`;
    } else {
      message += `\nSuggestion: Check that the ${entityType} exists and you have the necessary permissions.`;
    }
  }

  const error = new Error(message);
  // @ts-ignore - Adding custom properties to Error
  error.originalError = cause;
  // @ts-ignore - Adding custom properties to Error
  error.operation = operation;
  // @ts-ignore - Adding custom properties to Error
  error.entityType = entityType;
  // @ts-ignore - Adding custom properties to Error
  error.entityId = entityId;

  return error;
}

module.exports = {
  withRetry,
  sleep,
  isTransientError,
  getRetryAfterMs,
  enhanceError,
  createValidationError,
  createOperationError,
  DEFAULT_RETRY_CONFIG,
  RATE_LIMIT_RETRY_CONFIG,
};
