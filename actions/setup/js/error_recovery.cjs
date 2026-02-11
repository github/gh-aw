// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Error recovery utilities for safe output operations
 * Provides retry logic with exponential backoff for transient failures
 */

const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Configuration for retry behavior
 * @typedef {Object} RetryConfig
 * @property {number} maxRetries - Maximum number of retry attempts (default: 3)
 * @property {number} initialDelayMs - Initial delay in milliseconds (default: 1000)
 * @property {number} maxDelayMs - Maximum delay in milliseconds (default: 10000)
 * @property {number} backoffMultiplier - Backoff multiplier for exponential backoff (default: 2)
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
  shouldRetry: isTransientError,
};

/**
 * Determine if an error is transient and worth retrying
 * @param {any} error - The error to check
 * @returns {boolean} True if the error is transient and should be retried
 */
function isTransientError(error) {
  const errorMsg = getErrorMessage(error).toLowerCase();

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
    "rate limit", // GitHub API rate limiting
    "secondary rate limit", // GitHub secondary rate limits
    "abuse detection", // GitHub abuse detection
    "temporarily unavailable",
    "no server is currently available", // GitHub API server unavailability
  ];

  return transientPatterns.some(pattern => errorMsg.includes(pattern));
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
  let lastError;
  let delay = fullConfig.initialDelayMs;

  for (let attempt = 0; attempt <= fullConfig.maxRetries; attempt++) {
    try {
      if (attempt > 0) {
        core.info(`Retry attempt ${attempt}/${fullConfig.maxRetries} for ${operationName} after ${delay}ms delay`);
        await sleep(delay);
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

      // Calculate next delay with exponential backoff
      delay = Math.min(delay * fullConfig.backoffMultiplier, fullConfig.maxDelayMs);
    }
  }

  // This should never be reached, but TypeScript needs it
  throw lastError;
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
 * @returns {Error} Enhanced error with context
 */
function enhanceError(error, context) {
  const originalMessage = getErrorMessage(error);
  const timestamp = new Date().toISOString();

  let enhancedMessage = `[${timestamp}] ${context.operation} failed`;

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
  enhanceError,
  createValidationError,
  createOperationError,
  DEFAULT_RETRY_CONFIG,
};
