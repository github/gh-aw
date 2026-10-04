// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";

// Mock core global (needed by github_rate_limit_logger.cjs)
const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
};

global.core = mockCore;

describe("rate_limit_helpers", () => {
  let mockGithub;

  beforeEach(() => {
    vi.clearAllMocks();
    mockGithub = {
      rest: {
        rateLimit: {
          get: vi.fn().mockResolvedValue({
            data: {
              rate: { remaining: 5000, limit: 5000, used: 0 },
              resources: {},
            },
          }),
        },
      },
    };
  });

  describe("getRateLimitRemaining", () => {
    it("should return remaining rate limit", async () => {
      const { getRateLimitRemaining } = await import("./rate_limit_helpers.cjs");
      const remaining = await getRateLimitRemaining(mockGithub, "test");
      expect(remaining).toBe(5000);
    });

    it("should use resources.core when rate is absent", async () => {
      const { getRateLimitRemaining } = await import("./rate_limit_helpers.cjs");
      mockGithub.rest.rateLimit.get.mockResolvedValue({
        data: { resources: { core: { remaining: 3200, limit: 5000, used: 1800, reset: 1700000000 } } },
      });
      expect(await getRateLimitRemaining(mockGithub, "test")).toBe(3200);
    });

    it("should return -1 on error", async () => {
      const { getRateLimitRemaining } = await import("./rate_limit_helpers.cjs");
      mockGithub.rest.rateLimit.get.mockRejectedValueOnce(new Error("API error")).mockRejectedValueOnce(new Error("API error"));
      const remaining = await getRateLimitRemaining(mockGithub, "test");
      expect(remaining).toBe(-1);
    });
  });

  describe("checkRateLimit", () => {
    it("should return ok when rate limit is sufficient", async () => {
      const { checkRateLimit } = await import("./rate_limit_helpers.cjs");
      const result = await checkRateLimit(mockGithub, "test");
      expect(result.ok).toBe(true);
      expect(result.remaining).toBe(5000);
    });

    it("should return not ok when rate limit is too low", async () => {
      const { checkRateLimit } = await import("./rate_limit_helpers.cjs");
      mockGithub.rest.rateLimit.get.mockResolvedValue({
        data: {
          rate: { remaining: 50, limit: 5000, used: 4950 },
          resources: {},
        },
      });
      const result = await checkRateLimit(mockGithub, "test");
      expect(result.ok).toBe(false);
      expect(result.remaining).toBe(50);
    });

    it("should return ok when rate limit check fails", async () => {
      const { checkRateLimit } = await import("./rate_limit_helpers.cjs");
      mockGithub.rest.rateLimit.get.mockRejectedValue(new Error("API error"));
      const result = await checkRateLimit(mockGithub, "test");
      expect(result.ok).toBe(true);
      expect(result.remaining).toBe(-1);
    });
  });

  describe("MIN_RATE_LIMIT_REMAINING", () => {
    it("should be 100", async () => {
      const { MIN_RATE_LIMIT_REMAINING } = await import("./rate_limit_helpers.cjs");
      expect(MIN_RATE_LIMIT_REMAINING).toBe(100);
    });
  });

  describe("LOW_RATE_LIMIT_THRESHOLD_PERCENT", () => {
    it("should be 20", async () => {
      const { LOW_RATE_LIMIT_THRESHOLD_PERCENT } = await import("./rate_limit_helpers.cjs");
      expect(LOW_RATE_LIMIT_THRESHOLD_PERCENT).toBe(20);
    });
  });

  describe("checkRateLimitHeadroom", () => {
    it("should return remaining, limit, and percentRemaining", async () => {
      const { checkRateLimitHeadroom } = await import("./rate_limit_helpers.cjs");
      mockGithub.rest.rateLimit.get.mockResolvedValue({
        data: {
          rate: { remaining: 4000, limit: 5000, used: 1000 },
          resources: {},
        },
      });
      const result = await checkRateLimitHeadroom(mockGithub, "test");
      expect(result.remaining).toBe(4000);
      expect(result.limit).toBe(5000);
      expect(result.percentRemaining).toBe(80);
    });

    it("should use resources.core without warning when rate is absent", async () => {
      const { checkRateLimitHeadroom } = await import("./rate_limit_helpers.cjs");
      mockGithub.rest.rateLimit.get.mockResolvedValue({
        data: { resources: { core: { remaining: 4000, limit: 5000, used: 1000 } } },
      });
      expect(await checkRateLimitHeadroom(mockGithub, "test")).toEqual({ remaining: 4000, limit: 5000, percentRemaining: 80 });
      expect(mockCore.warning).not.toHaveBeenCalled();
    });

    it("should report an unavailable core quota rather than succeeding", async () => {
      const { checkRateLimitHeadroom } = await import("./rate_limit_helpers.cjs");
      mockGithub.rest.rateLimit.get.mockResolvedValue({ data: { resources: {} } });
      expect(await checkRateLimitHeadroom(mockGithub, "test")).toEqual({ remaining: -1, limit: -1, percentRemaining: -1 });
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("no core quota"));
    });

    it("should log info when headroom is above threshold", async () => {
      const { checkRateLimitHeadroom } = await import("./rate_limit_helpers.cjs");
      mockGithub.rest.rateLimit.get.mockResolvedValue({
        data: {
          rate: { remaining: 4000, limit: 5000, used: 1000 },
          resources: {},
        },
      });
      await checkRateLimitHeadroom(mockGithub, "test");
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Rate-limit headroom: 4000/5000"));
      expect(mockCore.warning).not.toHaveBeenCalled();
    });

    it("should emit warning when headroom is below threshold", async () => {
      const { checkRateLimitHeadroom } = await import("./rate_limit_helpers.cjs");
      mockGithub.rest.rateLimit.get.mockResolvedValue({
        data: {
          rate: { remaining: 500, limit: 5000, used: 4500 },
          resources: {},
        },
      });
      await checkRateLimitHeadroom(mockGithub, "test");
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Rate-limit headroom low: 500/5000"));
    });

    it("should return -1 values and warn with error details on error", async () => {
      const { checkRateLimitHeadroom } = await import("./rate_limit_helpers.cjs");
      mockGithub.rest.rateLimit.get.mockRejectedValue(new Error("API error"));
      const result = await checkRateLimitHeadroom(mockGithub, "test");
      expect(result.remaining).toBe(-1);
      expect(result.limit).toBe(-1);
      expect(result.percentRemaining).toBe(-1);
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Could not check rate-limit headroom"));
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("API error"));
    });

    it("should use Math.floor so 19.9% triggers warning (not Math.round which would give 20%)", async () => {
      const { checkRateLimitHeadroom } = await import("./rate_limit_helpers.cjs");
      // 199/1000 = 19.9% — Math.floor gives 19, Math.round would give 20 (and skip the warning)
      mockGithub.rest.rateLimit.get.mockResolvedValue({
        data: {
          rate: { remaining: 199, limit: 1000, used: 801 },
          resources: {},
        },
      });
      await checkRateLimitHeadroom(mockGithub, "test");
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Rate-limit headroom low:"));
    });
    it("should write the quota snapshot to the JSONL log file when response has rate-limit headers", async () => {
      const { checkRateLimitHeadroom } = await import("./rate_limit_helpers.cjs");

      // Provide rate-limit headers so logRateLimitFromResponse can write an entry
      mockGithub.rest.rateLimit.get.mockResolvedValue({
        data: {
          rate: { remaining: 3000, limit: 5000, used: 2000 },
          resources: {},
        },
        headers: {
          "x-ratelimit-limit": "5000",
          "x-ratelimit-remaining": "3000",
          "x-ratelimit-reset": "1700000000",
          "x-ratelimit-resource": "core",
        },
      });

      const existsSpy = vi.spyOn(fs, "existsSync").mockReturnValue(true);
      const mkdirSpy = vi.spyOn(fs, "mkdirSync").mockImplementation(() => undefined);
      const appendSpy = vi.spyOn(fs, "appendFileSync").mockImplementation(() => undefined);

      try {
        await checkRateLimitHeadroom(mockGithub, "pre-flight-check");

        expect(appendSpy).toHaveBeenCalledOnce();
        const entry = JSON.parse(appendSpy.mock.calls[0][1].trimEnd());
        expect(entry.source).toBe("response_headers");
        expect(entry.operation).toBe("pre-flight-check");
        expect(entry.remaining).toBe(3000);
        expect(entry.limit).toBe(5000);
        expect(entry.resource).toBe("core");
      } finally {
        existsSpy.mockRestore();
        mkdirSpy.mockRestore();
        appendSpy.mockRestore();
      }
    });
  });
});
