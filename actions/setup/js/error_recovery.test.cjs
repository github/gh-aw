// @ts-check

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";

// Mock @actions/core
global.core = {
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  debug: vi.fn(),
};

import { withRetry, isTransientError, getRetryAfterMs, enhanceError, createValidationError, createOperationError, DEFAULT_RETRY_CONFIG, RATE_LIMIT_RETRY_CONFIG } from "./error_recovery.cjs";

describe("error_recovery", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe("isTransientError", () => {
    it("should identify network errors as transient", () => {
      expect(isTransientError(new Error("Network error occurred"))).toBe(true);
      expect(isTransientError(new Error("ECONNRESET"))).toBe(true);
      expect(isTransientError(new Error("ETIMEDOUT"))).toBe(true);
      expect(isTransientError(new Error("Socket hang up"))).toBe(true);
    });

    it("should identify HTTP timeout errors as transient", () => {
      expect(isTransientError(new Error("502 Bad Gateway"))).toBe(true);
      expect(isTransientError(new Error("503 Service Unavailable"))).toBe(true);
      expect(isTransientError(new Error("504 Gateway Timeout"))).toBe(true);
    });

    it("should identify rate limit errors as transient", () => {
      expect(isTransientError(new Error("Rate limit exceeded"))).toBe(true);
      expect(isTransientError(new Error("Secondary rate limit hit"))).toBe(true);
      expect(isTransientError(new Error("Too Many Requests"))).toBe(true);
      expect(isTransientError(new Error("Abuse detection triggered"))).toBe(true);
    });

    it("should identify GitHub server unavailability as transient", () => {
      expect(isTransientError(new Error("No server is currently available to service your request"))).toBe(true);
      expect(isTransientError(new Error("no server is currently available"))).toBe(true);
    });

    it("should identify HTML responses (GitHub 500 Unicorn page) as transient", () => {
      expect(isTransientError(new Error("<!DOCTYPE html><html><head><title>Unicorn!</title></head></html>"))).toBe(true);
      expect(isTransientError(new Error("<!doctype html>\n<html>..."))).toBe(true);
      // With leading whitespace
      expect(isTransientError(new Error("  <!DOCTYPE html><html>..."))).toBe(true);
    });

    it("should identify status-only 429 errors as transient even with a non-descriptive message", () => {
      // Octokit can produce errors where status=429 but the message contains no rate-limit text.
      expect(isTransientError({ status: 429, message: "Request failed" })).toBe(true);
      expect(isTransientError({ response: { status: 429 }, message: "Request failed" })).toBe(true);
    });

    it.each([408, 425, 500, 502, 503, 504])("should identify HTTP %i as transient without relying on message text", status => {
      expect(isTransientError({ status, message: "Request failed" })).toBe(true);
      expect(isTransientError({ response: { status }, message: "Request failed" })).toBe(true);
    });

    it("should not identify non-transient HTTP statuses as transient", () => {
      expect(isTransientError({ status: 400, message: "Request failed" })).toBe(false);
      expect(isTransientError({ status: 404, message: "Request failed" })).toBe(false);
    });

    it("should not identify validation errors as transient", () => {
      expect(isTransientError(new Error("Invalid input"))).toBe(false);
      expect(isTransientError(new Error("Field is required"))).toBe(false);
      expect(isTransientError(new Error("Not found"))).toBe(false);
    });

    it("should handle non-Error objects", () => {
      expect(isTransientError("network error")).toBe(true);
      expect(isTransientError({ message: "timeout occurred" })).toBe(true);
      expect(isTransientError("validation failed")).toBe(false);
    });
  });

  describe("withRetry", () => {
    it("should succeed on first attempt", async () => {
      const operation = vi.fn().mockResolvedValue("success");
      const result = await withRetry(operation, {}, "test-operation");

      expect(result).toBe("success");
      expect(operation).toHaveBeenCalledTimes(1);
      expect(core.info).not.toHaveBeenCalledWith(expect.stringContaining("Retry attempt"));
    });

    it("should retry transient errors and succeed", async () => {
      const operation = vi.fn().mockRejectedValueOnce(new Error("Network timeout")).mockResolvedValue("success");

      const result = await withRetry(operation, { maxRetries: 2, initialDelayMs: 10 }, "test-operation");

      expect(result).toBe("success");
      expect(operation).toHaveBeenCalledTimes(2);
      expect(core.warning).toHaveBeenCalledWith(expect.stringContaining("test-operation failed (attempt 1/3)"));
      expect(core.info).toHaveBeenCalledWith(expect.stringContaining("Retry attempt 1/2"));
      expect(core.info).toHaveBeenCalledWith(expect.stringContaining("succeeded on retry attempt 1"));
    });

    it("should fail immediately on non-retryable errors", async () => {
      const operation = vi.fn().mockRejectedValue(new Error("Invalid input"));

      await expect(withRetry(operation, { maxRetries: 3, initialDelayMs: 10 }, "test-operation")).rejects.toThrow("Invalid input");

      expect(operation).toHaveBeenCalledTimes(1);
      expect(core.debug).toHaveBeenCalledWith(expect.stringContaining("non-retryable error"));
    });

    it("should exhaust all retries and fail with enhanced error", async () => {
      const operation = vi.fn().mockRejectedValue(new Error("Network timeout"));

      await expect(withRetry(operation, { maxRetries: 2, initialDelayMs: 10 }, "test-operation")).rejects.toThrow("All retry attempts exhausted");

      expect(operation).toHaveBeenCalledTimes(3); // Initial + 2 retries
      expect(core.warning).toHaveBeenCalledWith(expect.stringContaining("failed after 2 retry attempts"));
    });

    it("should emit E010 RATE_LIMIT_EXCEEDED when rate limit persists after 3 retries", async () => {
      const rateLimitError = {
        message: "Too Many Requests",
        response: { status: 429, headers: { "retry-after": "1" } },
      };
      const operation = vi.fn().mockRejectedValue(rateLimitError);

      await expect(withRetry(operation, { maxRetries: 3, initialDelayMs: 1 }, "test-operation")).rejects.toThrow("E010 RATE_LIMIT_EXCEEDED");

      expect(operation).toHaveBeenCalledTimes(4); // Initial + 3 retries
    });

    it("should emit E010 for exhausted retries on a secondary rate limit", async () => {
      const rateLimitError = {
        message: "secondary rate limit",
        response: { status: 403, headers: { "retry-after": "1", "x-ratelimit-remaining": "0" } },
      };
      const operation = vi.fn().mockRejectedValue(rateLimitError);

      await expect(withRetry(operation, { maxRetries: 3, initialDelayMs: 1 }, "test-operation")).rejects.toThrow("E010 RATE_LIMIT_EXCEEDED");
    });

    it("should emit E010 for exhausted retries on abuse-detection message without status", async () => {
      const rateLimitError = new Error("Abuse detection mechanism triggered");
      const operation = vi.fn().mockRejectedValue(rateLimitError);

      await expect(withRetry(operation, { maxRetries: 3, initialDelayMs: 1 }, "test-operation")).rejects.toThrow("E010 RATE_LIMIT_EXCEEDED");
    });

    it("should emit E010 for status-only 429 with non-descriptive message persisting after 3 retries", async () => {
      // Verifies that an Octokit-style error with status=429 and a generic message
      // is retried (isTransientError returns true via isRateLimitError) and emits E010 on exhaustion.
      const rateLimitError = { status: 429, message: "Request failed" };
      const operation = vi.fn().mockRejectedValue(rateLimitError);

      await expect(withRetry(operation, { maxRetries: 3, initialDelayMs: 1 }, "test-operation")).rejects.toThrow("E010 RATE_LIMIT_EXCEEDED");
      expect(operation).toHaveBeenCalledTimes(4); // Initial + 3 retries
    });

    it("should use exponential backoff", async () => {
      const operation = vi.fn().mockRejectedValueOnce(new Error("Network timeout")).mockRejectedValueOnce(new Error("Network timeout")).mockResolvedValue("success");

      const config = {
        maxRetries: 3,
        initialDelayMs: 100,
        backoffMultiplier: 2,
        maxDelayMs: 1000,
        jitterMs: 0,
      };

      await withRetry(operation, config, "test-operation");

      // Verify retry attempts were made
      expect(operation).toHaveBeenCalledTimes(3);
      // First retry: initialDelay * backoffMultiplier = 100 * 2 = 200ms
      expect(core.info).toHaveBeenCalledWith(expect.stringContaining("after 200ms delay"));
      // Second retry: 200 * 2 = 400ms
      expect(core.info).toHaveBeenCalledWith(expect.stringContaining("after 400ms delay"));
    });

    it("should respect max delay limit", async () => {
      const operation = vi.fn().mockRejectedValueOnce(new Error("Network timeout")).mockRejectedValueOnce(new Error("Network timeout")).mockResolvedValue("success");

      const config = {
        maxRetries: 3,
        initialDelayMs: 1000,
        backoffMultiplier: 10,
        maxDelayMs: 2000, // Cap at 2000ms
        jitterMs: 0,
      };

      await withRetry(operation, config, "test-operation");

      // Second delay would be 10000ms without cap, but should be capped at 2000ms
      expect(core.info).toHaveBeenCalledWith(expect.stringContaining("after 2000ms delay"));
    });

    it("should allow custom shouldRetry function", async () => {
      const operation = vi.fn().mockRejectedValue(new Error("Custom retryable error"));
      const shouldRetry = vi.fn().mockReturnValue(false);

      await expect(withRetry(operation, { shouldRetry, maxRetries: 2 }, "test-operation")).rejects.toThrow("Custom retryable error");

      expect(operation).toHaveBeenCalledTimes(1);
      expect(shouldRetry).toHaveBeenCalled();
    });

    it("should add random jitter to delay when jitterMs is configured", async () => {
      const randomSpy = vi.spyOn(Math, "random").mockReturnValue(0.5);
      const operation = vi.fn().mockRejectedValueOnce(new Error("Network timeout")).mockResolvedValue("success");

      const config = {
        maxRetries: 2,
        initialDelayMs: 100,
        backoffMultiplier: 2,
        maxDelayMs: 10000,
        jitterMs: 1000,
      };

      await withRetry(operation, config, "test-operation");

      // Base delay after first failure: 100 * 2 = 200ms
      // Jitter: Math.floor(0.5 * 1000) = 500ms
      // Total: 200 + 500 = 700ms
      expect(core.info).toHaveBeenCalledWith(expect.stringContaining("after 700ms delay"));

      randomSpy.mockRestore();
    });

    it("should not add jitter when jitterMs is 0", async () => {
      const operation = vi.fn().mockRejectedValueOnce(new Error("Network timeout")).mockResolvedValue("success");

      const config = {
        maxRetries: 2,
        initialDelayMs: 100,
        backoffMultiplier: 2,
        maxDelayMs: 10000,
        jitterMs: 0,
      };

      await withRetry(operation, config, "test-operation");

      // Base delay after first failure: 100 * 2 = 200ms, no jitter
      expect(core.info).toHaveBeenCalledWith(expect.stringContaining("after 200ms delay"));
    });

    it("should cap the delay after adding jitter", async () => {
      const randomSpy = vi.spyOn(Math, "random").mockReturnValue(0.99);
      const operation = vi.fn().mockRejectedValueOnce(new Error("Network timeout")).mockResolvedValue("success");

      await withRetry(operation, { maxRetries: 1, initialDelayMs: 1000, backoffMultiplier: 2, maxDelayMs: 2000, jitterMs: 1000 }, "test-operation");

      expect(core.info).toHaveBeenCalledWith(expect.stringContaining("after 2000ms delay"));
      randomSpy.mockRestore();
    });

    it.each([
      ["maxRetries", -1],
      ["initialDelayMs", Number.NaN],
      ["maxDelayMs", Number.POSITIVE_INFINITY],
      ["maxDelayMs", 2 ** 31],
      ["jitterMs", 1.5],
      ["backoffMultiplier", 0],
    ])("should reject invalid %s configuration", async (key, value) => {
      const operation = vi.fn();

      await expect(withRetry(operation, { [key]: value }, "test-operation")).rejects.toThrow(`Retry configuration ${key}`);
      expect(operation).not.toHaveBeenCalled();
    });

    it("should accept the largest supported timer delay", async () => {
      const operation = vi.fn().mockResolvedValue("success");

      await expect(withRetry(operation, { maxDelayMs: 2 ** 31 - 1 }, "test-operation")).resolves.toBe("success");
    });

    it("should reject a non-function retry predicate", async () => {
      // @ts-expect-error Deliberately exercise runtime input validation.
      await expect(withRetry(vi.fn(), { shouldRetry: true }, "test-operation")).rejects.toThrow("Retry configuration shouldRetry must be a function");
    });
  });

  describe("enhanceError", () => {
    it("should enhance error with operation context", () => {
      const originalError = new Error("Original message");
      const context = {
        operation: "create issue",
        attempt: 1,
        retryable: true,
        suggestion: "Check your input",
      };

      const enhanced = enhanceError(originalError, context);

      expect(enhanced.message).toContain("create issue failed");
      expect(enhanced.message).toContain("Original error: Original message");
      expect(enhanced.message).toContain("Retryable: true");
      expect(enhanced.message).toContain("Suggestion: Check your input");
      // @ts-ignore - Checking custom property
      expect(enhanced.originalError).toBe(originalError);
    });

    it("should include retry information when maxRetries is provided", () => {
      const originalError = new Error("Failed");
      const context = {
        operation: "update PR",
        attempt: 3,
        maxRetries: 3,
        retryable: true,
        suggestion: "Try again later",
      };

      const enhanced = enhanceError(originalError, context);

      expect(enhanced.message).toContain("after 3 retry attempts");
    });

    it("should include timestamp in error message", () => {
      const originalError = new Error("Failed");
      const context = {
        operation: "test",
        attempt: 1,
        retryable: false,
        suggestion: "Fix it",
      };

      const enhanced = enhanceError(originalError, context);

      expect(enhanced.message).toMatch(/\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z\]/);
    });
  });

  describe("createValidationError", () => {
    it("should create validation error with context", () => {
      const error = createValidationError("title", "", "cannot be empty", "Provide a non-empty title");

      expect(error.message).toContain("Validation failed for field 'title'");
      expect(error.message).toContain("Reason: cannot be empty");
      expect(error.message).toContain("Suggestion: Provide a non-empty title");
      expect(error.isValidationError).toBe(true);
      expect(error.field).toBe("title");
    });

    it("should truncate long values", () => {
      const longValue = "a".repeat(200);
      const error = createValidationError("body", longValue, "too long");

      expect(error.message).toContain("...");
      expect(error.message.length).toBeLessThan(longValue.length + 200);
    });

    it("should work without suggestion", () => {
      const error = createValidationError("labels", ["invalid"], "not allowed");

      expect(error.message).toContain("Validation failed");
      expect(error.message).not.toContain("Suggestion:");
    });

    it("should include timestamp", () => {
      const error = createValidationError("field", "value", "reason");

      expect(error.message).toMatch(/\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z\]/);
    });
  });

  describe("createOperationError", () => {
    it("should create operation error with full context", () => {
      const cause = new Error("API error");
      const error = createOperationError("update", "issue", cause, 123, "Check permissions");

      expect(error.message).toContain("Failed to update issue #123");
      expect(error.message).toContain("Underlying error: API error");
      expect(error.message).toContain("Suggestion: Check permissions");
      // @ts-ignore - Checking custom property
      expect(error.originalError).toBe(cause);
      // @ts-ignore - Checking custom property
      expect(error.operation).toBe("update");
      // @ts-ignore - Checking custom property
      expect(error.entityType).toBe("issue");
      // @ts-ignore - Checking custom property
      expect(error.entityId).toBe(123);
    });

    it("should work without entity ID", () => {
      const cause = new Error("Network error");
      const error = createOperationError("create", "PR", cause);

      expect(error.message).toContain("Failed to create PR");
      expect(error.message).not.toContain("#");
    });

    it("should provide default suggestion for transient errors", () => {
      const cause = new Error("Network timeout");
      const error = createOperationError("delete", "comment", cause, 456);

      expect(error.message).toContain("This appears to be a transient error");
      expect(error.message).toContain("retried automatically");
    });

    it("should provide default suggestion for non-transient errors", () => {
      const cause = new Error("Not found");
      const error = createOperationError("update", "discussion", cause, 789);

      expect(error.message).toContain("Check that the discussion exists");
      expect(error.message).toContain("necessary permissions");
    });

    it("should include timestamp", () => {
      const cause = new Error("Failed");
      const error = createOperationError("operation", "entity", cause, 1);

      expect(error.message).toMatch(/\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z\]/);
    });
  });

  describe("DEFAULT_RETRY_CONFIG", () => {
    it("should have sensible defaults", () => {
      expect(DEFAULT_RETRY_CONFIG.maxRetries).toBe(3);
      expect(DEFAULT_RETRY_CONFIG.initialDelayMs).toBe(1000);
      expect(DEFAULT_RETRY_CONFIG.maxDelayMs).toBe(10000);
      expect(DEFAULT_RETRY_CONFIG.backoffMultiplier).toBe(2);
      expect(DEFAULT_RETRY_CONFIG.jitterMs).toBe(100);
      expect(DEFAULT_RETRY_CONFIG.shouldRetry).toBe(isTransientError);
    });
  });

  describe("RATE_LIMIT_RETRY_CONFIG", () => {
    it("should have 5 retries", () => {
      expect(RATE_LIMIT_RETRY_CONFIG.maxRetries).toBe(5);
    });

    it("should have initialDelayMs producing 30s first retry sleep", () => {
      // First retry sleep = initialDelayMs * backoffMultiplier
      const firstRetrySleep = RATE_LIMIT_RETRY_CONFIG.initialDelayMs * RATE_LIMIT_RETRY_CONFIG.backoffMultiplier;
      expect(firstRetrySleep).toBe(30000);
    });

    it("should cap delay at 240s", () => {
      expect(RATE_LIMIT_RETRY_CONFIG.maxDelayMs).toBe(240000);
    });

    it("should use isTransientError as shouldRetry", () => {
      expect(RATE_LIMIT_RETRY_CONFIG.shouldRetry).toBe(isTransientError);
    });
  });

  describe("getRetryAfterMs", () => {
    it("should return null when error has no response headers", () => {
      expect(getRetryAfterMs(new Error("Some error"))).toBeNull();
      expect(getRetryAfterMs(null)).toBeNull();
      expect(getRetryAfterMs(undefined)).toBeNull();
    });

    it("should return null when status is not a rate-limit status", () => {
      // 5xx errors should not use Retry-After from x-ratelimit-reset
      const error502 = { response: { status: 502, headers: { "x-ratelimit-reset": String(Math.floor((Date.now() + 120000) / 1000)) } } };
      expect(getRetryAfterMs(error502)).toBeNull();
      // 403 without x-ratelimit-remaining: 0 is not a rate-limit response
      const error403 = { response: { status: 403, headers: { "retry-after": "60", "x-ratelimit-remaining": "100" } } };
      expect(getRetryAfterMs(error403)).toBeNull();
      const secondaryRateLimitWithoutRemaining = { response: { status: 403, headers: { "retry-after": "60" } } };
      expect(getRetryAfterMs(secondaryRateLimitWithoutRemaining)).toBeNull();
    });

    it("should extract retry-after seconds from response headers on 429", () => {
      const error = { response: { status: 429, headers: { "retry-after": "60" } } };
      expect(getRetryAfterMs(error)).toBe(60000);
    });

    it("should extract retry-after seconds from a 403 secondary rate-limit response", () => {
      const error = { response: { status: 403, headers: { "retry-after": "30", "x-ratelimit-remaining": "0" } } };
      expect(getRetryAfterMs(error)).toBe(30000);
    });

    it("should extract retry-after seconds from a 503 response", () => {
      const error = { response: { status: 503, headers: { "retry-after": "15" } } };
      expect(getRetryAfterMs(error)).toBe(15000);
    });

    it("should read case-insensitive headers from a Fetch Headers instance", () => {
      const error = { response: { status: 429, headers: new Headers({ "Retry-After": "20" }) } };
      expect(getRetryAfterMs(error)).toBe(20000);
    });

    it("should extract retry-after seconds from top-level headers on 429", () => {
      const error = { status: 429, headers: { "retry-after": "30" } };
      expect(getRetryAfterMs(error)).toBe(30000);
    });

    it("should prefer response.headers over top-level headers on 429", () => {
      const error = {
        response: { status: 429, headers: { "retry-after": "60" } },
        headers: { "retry-after": "30" },
      };
      expect(getRetryAfterMs(error)).toBe(60000);
    });

    it("should return null for zero or negative retry-after on 429", () => {
      expect(getRetryAfterMs({ response: { status: 429, headers: { "retry-after": "0" } } })).toBeNull();
      expect(getRetryAfterMs({ response: { status: 429, headers: { "retry-after": "-1" } } })).toBeNull();
    });

    it("should fall back to x-ratelimit-reset when retry-after is absent on 429", () => {
      const futureTimestamp = Math.floor((Date.now() + 120000) / 1000); // 2 min from now
      const error = { response: { status: 429, headers: { "x-ratelimit-reset": String(futureTimestamp) } } };
      const result = getRetryAfterMs(error);
      // Should be roughly 120s (allow ±5s for test timing)
      expect(result).toBeGreaterThan(115000);
      expect(result).toBeLessThan(125000);
    });

    it("should use x-ratelimit-reset for 403 with x-ratelimit-remaining: 0 (secondary rate limit)", () => {
      const futureTimestamp = Math.floor((Date.now() + 60000) / 1000); // 1 min from now
      const error = {
        response: {
          status: 403,
          headers: { "x-ratelimit-remaining": "0", "x-ratelimit-reset": String(futureTimestamp) },
        },
      };
      const result = getRetryAfterMs(error);
      expect(result).toBeGreaterThan(55000);
      expect(result).toBeLessThan(65000);
    });

    it("should return null when x-ratelimit-reset is in the past on 429", () => {
      const pastTimestamp = Math.floor((Date.now() - 60000) / 1000);
      const error = { response: { status: 429, headers: { "x-ratelimit-reset": String(pastTimestamp) } } };
      expect(getRetryAfterMs(error)).toBeNull();
    });

    it("should return null for non-numeric retry-after on 429", () => {
      const error = { response: { status: 429, headers: { "retry-after": "not-a-number" } } };
      expect(getRetryAfterMs(error)).toBeNull();
    });

    it("should parse retry-after HTTP-date on 429", () => {
      const retryAt = new Date(Date.now() + 60000).toUTCString();
      const error = { response: { status: 429, headers: { "retry-after": retryAt } } };
      const result = getRetryAfterMs(error);
      expect(result).toBeGreaterThan(55000);
      expect(result).toBeLessThan(65000);
    });
  });

  describe("withRetry with Retry-After header", () => {
    let appendSpy, existsSpy, mkdirSpy;

    beforeEach(() => {
      existsSpy = vi.spyOn(fs, "existsSync").mockReturnValue(true);
      mkdirSpy = vi.spyOn(fs, "mkdirSync").mockImplementation(() => undefined);
      appendSpy = vi.spyOn(fs, "appendFileSync").mockImplementation(() => undefined);
    });

    afterEach(() => {
      existsSpy.mockRestore();
      mkdirSpy.mockRestore();
      appendSpy.mockRestore();
    });

    it("should use Retry-After delay instead of backoff when header is present on 429", async () => {
      const retryAfterError = {
        message: "rate limit exceeded",
        response: { status: 429, headers: { "retry-after": "1" } }, // 1s for test speed
      };
      const operation = vi.fn().mockRejectedValueOnce(retryAfterError).mockResolvedValue("success");

      const result = await withRetry(operation, { maxRetries: 2, initialDelayMs: 10, backoffMultiplier: 2, jitterMs: 0 }, "test-operation");

      expect(result).toBe("success");
      // The delay used should be 1000ms (from Retry-After: 1) rather than 20ms (10 * 2)
      expect(core.info).toHaveBeenCalledWith(expect.stringContaining("Retry-After header detected for test-operation: next retry will wait 1000ms"));
    });

    it("should use Retry-After delay for a status-only 503 response", async () => {
      const retryAfterError = {
        message: "Request failed",
        response: { status: 503, headers: { "retry-after": "1" } },
      };
      const operation = vi.fn().mockRejectedValueOnce(retryAfterError).mockResolvedValue("success");

      const result = await withRetry(operation, { maxRetries: 1, initialDelayMs: 10, backoffMultiplier: 2, jitterMs: 0 }, "test-operation");

      expect(result).toBe("success");
      expect(core.info).toHaveBeenCalledWith(expect.stringContaining("Retry-After header detected for test-operation: next retry will wait 1000ms"));
    });

    it("should fall back to exponential backoff for non-rate-limit errors (502)", async () => {
      const transientError = {
        message: "502 bad gateway",
        response: { status: 502, headers: { "x-ratelimit-reset": String(Math.floor((Date.now() + 120000) / 1000)) } },
      };
      const operation = vi.fn().mockRejectedValueOnce(transientError).mockResolvedValue("success");

      const result = await withRetry(operation, { maxRetries: 2, initialDelayMs: 10, backoffMultiplier: 2, jitterMs: 0 }, "test-operation");

      expect(result).toBe("success");
      // Normal backoff: 10 * 2 = 20ms — NOT the 120s reset header
      expect(core.info).toHaveBeenCalledWith(expect.stringContaining("after 20ms delay"));
      expect(core.info).not.toHaveBeenCalledWith(expect.stringContaining("Retry-After header detected"));
    });

    it("should write a JSONL retry entry to the rate-limit log file on each retry", async () => {
      const error = {
        message: "rate limit exceeded",
        response: {
          status: 429,
          headers: { "x-ratelimit-remaining": "0", "x-ratelimit-limit": "5000", "retry-after": "1" },
        },
      };
      const operation = vi.fn().mockRejectedValueOnce(error).mockResolvedValue("ok");

      await withRetry(operation, { maxRetries: 2, initialDelayMs: 10, backoffMultiplier: 2, jitterMs: 0 }, "test-log-op");

      // appendFileSync should have been called once (one retry)
      expect(appendSpy).toHaveBeenCalledOnce();
      const entry = JSON.parse(appendSpy.mock.calls[0][1].trimEnd());
      expect(entry.source).toBe("retry");
      expect(entry.operation).toBe("test-log-op");
      expect(entry.attempt).toBe(1);
      expect(entry.status).toBe(429);
      expect(entry.remaining).toBe(0);
    });
  });
});
