// @ts-check

import { describe, it, expect, beforeEach, vi } from "vitest";

// Mock @actions/core
global.core = {
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  debug: vi.fn(),
};

import { withRetry, isTransientError, enhanceError, createValidationError, createOperationError, DEFAULT_RETRY_CONFIG } from "./error_recovery.cjs";

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
      expect(isTransientError(new Error("Abuse detection triggered"))).toBe(true);
    });

    it("should identify GitHub server unavailability as transient", () => {
      expect(isTransientError(new Error("No server is currently available to service your request"))).toBe(true);
      expect(isTransientError(new Error("no server is currently available"))).toBe(true);
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

    it("should use exponential backoff", async () => {
      const operation = vi.fn().mockRejectedValueOnce(new Error("Network timeout")).mockRejectedValueOnce(new Error("Network timeout")).mockResolvedValue("success");

      const config = {
        maxRetries: 3,
        initialDelayMs: 100,
        backoffMultiplier: 2,
        maxDelayMs: 1000,
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
      expect(DEFAULT_RETRY_CONFIG.shouldRetry).toBe(isTransientError);
    });
  });
});
