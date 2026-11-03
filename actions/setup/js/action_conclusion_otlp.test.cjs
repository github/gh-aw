// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { createRequire } from "module";

// Use CJS require so we share the same module cache as action_conclusion_otlp.cjs
const req = createRequire(import.meta.url);

// Set up global.core mock before loading the module under test so shim.cjs
// sees it already present and does not overwrite it with its stderr-based shim.
const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
};
global.core = mockCore;

// Load the real send_otlp_span module and capture the original function
const sendOtlpModule = req("./send_otlp_span.cjs");
const originalSendJobConclusionSpan = sendOtlpModule.sendJobConclusionSpan;
const sessionLogs = req("./send_otlp_session_logs.cjs");

// Load the module under test — it holds a reference to the same sendOtlpModule object
const { run, buildSpanName, parseJobStartMs } = req("./action_conclusion_otlp.cjs");

// Shared mock function — patched onto the module exports in beforeEach
const mockSendJobConclusionSpan = vi.fn();

/** Env vars read by this module — cleared before each test */
const MANAGED_ENV_VARS = ["GH_AW_OTLP_ENDPOINTS", "INPUT_JOB_NAME", "INPUT_JOB-NAME", "GITHUB_AW_OTEL_JOB_START_MS"];

describe("action_conclusion_otlp.cjs", () => {
  /** @type {Record<string, string | undefined>} */
  let originalEnv = {};

  beforeEach(() => {
    vi.clearAllMocks();
    vi.spyOn(sessionLogs, "sendSessionLogs").mockResolvedValue(undefined);
    mockSendJobConclusionSpan.mockResolvedValue(undefined);
    // Patch the shared CJS exports object — run() accesses this at call time
    sendOtlpModule.sendJobConclusionSpan = mockSendJobConclusionSpan;
    originalEnv = Object.fromEntries(MANAGED_ENV_VARS.map(key => [key, process.env[key]]));
    MANAGED_ENV_VARS.forEach(key => delete process.env[key]);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    sendOtlpModule.sendJobConclusionSpan = originalSendJobConclusionSpan;
    MANAGED_ENV_VARS.forEach(key => {
      const value = originalEnv[key];
      if (value === undefined) {
        delete process.env[key];
      } else {
        process.env[key] = value;
      }
    });
  });

  it("should export run as a function", () => {
    expect(typeof run).toBe("function");
  });

  it("exports session logs only from the conclusion job", async () => {
    process.env.INPUT_JOB_NAME = "agent";
    await run();
    expect(sessionLogs.sendSessionLogs).not.toHaveBeenCalled();
    process.env.INPUT_JOB_NAME = "conclusion";
    await run();
    expect(sessionLogs.sendSessionLogs).toHaveBeenCalledOnce();
    expect(mockSendJobConclusionSpan.mock.invocationCallOrder[1]).toBeLessThan(sessionLogs.sendSessionLogs.mock.invocationCallOrder[0]);
  });

  describe("when GH_AW_OTLP_ENDPOINTS is not set", () => {
    it("should log that OTLP export is skipped and JSONL mirror will be attempted", async () => {
      await run();

      expect(mockCore.info).toHaveBeenCalledWith("[otlp] GH_AW_OTLP_ENDPOINTS not set, skipping OTLP export (will attempt JSONL mirror)");
    });

    it("should still call sendJobConclusionSpan for JSONL mirror", async () => {
      await run();

      expect(mockSendJobConclusionSpan).toHaveBeenCalledOnce();
      expect(mockSendJobConclusionSpan).toHaveBeenCalledWith("gh-aw.job.conclusion", { startMs: undefined });
    });
  });

  describe("when GH_AW_OTLP_ENDPOINTS is set", () => {
    beforeEach(() => {
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "http://localhost:4318" }]);
    });

    it("should call sendJobConclusionSpan once", async () => {
      await run();

      expect(mockSendJobConclusionSpan).toHaveBeenCalledOnce();
    });

    it("should log the conclusion span export as attempted", async () => {
      await run();

      expect(mockCore.info).toHaveBeenCalledWith("[otlp] conclusion span export attempted");
    });

    it("should log the endpoint URL in the sending message", async () => {
      await run();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("configured endpoints"));
    });

    describe("span name construction", () => {
      it("should use default span name 'gh-aw.job.conclusion' when INPUT_JOB_NAME is not set", async () => {
        await run();

        expect(mockSendJobConclusionSpan).toHaveBeenCalledWith("gh-aw.job.conclusion", { startMs: undefined });
      });

      it("should use job name from INPUT_JOB_NAME when set", async () => {
        process.env.INPUT_JOB_NAME = "agent";

        await run();

        expect(mockSendJobConclusionSpan).toHaveBeenCalledWith("gh-aw.agent.conclusion", { startMs: undefined });
      });

      it("should use job name from INPUT_JOB-NAME (hyphen form) when INPUT_JOB_NAME is not set", async () => {
        delete process.env.INPUT_JOB_NAME;
        process.env["INPUT_JOB-NAME"] = "agent";

        await run();

        expect(mockSendJobConclusionSpan).toHaveBeenCalledWith("gh-aw.agent.conclusion", { startMs: undefined });
      });

      it("should log the full span name in the sending message", async () => {
        process.env.INPUT_JOB_NAME = "setup";

        await run();

        expect(mockCore.info).toHaveBeenCalledWith('[otlp] sending conclusion span "gh-aw.setup.conclusion" to configured endpoints');
      });

      it("should handle different job names correctly", async () => {
        process.env.INPUT_JOB_NAME = "activation";

        await run();

        expect(mockSendJobConclusionSpan).toHaveBeenCalledWith("gh-aw.activation.conclusion", { startMs: undefined });
      });
    });

    describe("startMs propagation from GITHUB_AW_OTEL_JOB_START_MS", () => {
      it("should pass startMs when GITHUB_AW_OTEL_JOB_START_MS is set to a valid timestamp", async () => {
        const jobStartMs = Date.now() - 60_000; // 1 minute ago
        process.env.GITHUB_AW_OTEL_JOB_START_MS = String(jobStartMs);

        await run();

        expect(mockSendJobConclusionSpan).toHaveBeenCalledWith("gh-aw.job.conclusion", { startMs: jobStartMs });
      });

      it("should pass startMs: undefined when GITHUB_AW_OTEL_JOB_START_MS is not set", async () => {
        await run();

        expect(mockSendJobConclusionSpan).toHaveBeenCalledWith("gh-aw.job.conclusion", { startMs: undefined });
      });

      it("should pass startMs: undefined when GITHUB_AW_OTEL_JOB_START_MS is '0'", async () => {
        process.env.GITHUB_AW_OTEL_JOB_START_MS = "0";

        await run();

        expect(mockSendJobConclusionSpan).toHaveBeenCalledWith("gh-aw.job.conclusion", { startMs: undefined });
      });

      it("should pass startMs: undefined when GITHUB_AW_OTEL_JOB_START_MS is not a number", async () => {
        process.env.GITHUB_AW_OTEL_JOB_START_MS = "not-a-number";

        await run();

        expect(mockSendJobConclusionSpan).toHaveBeenCalledWith("gh-aw.job.conclusion", { startMs: undefined });
      });

      it("should pass startMs: undefined when GITHUB_AW_OTEL_JOB_START_MS is a negative number", async () => {
        process.env.GITHUB_AW_OTEL_JOB_START_MS = "-1000";

        await run();

        expect(mockSendJobConclusionSpan).toHaveBeenCalledWith("gh-aw.job.conclusion", { startMs: undefined });
      });

      it("should pass startMs: undefined when GITHUB_AW_OTEL_JOB_START_MS is Infinity", async () => {
        process.env.GITHUB_AW_OTEL_JOB_START_MS = "Infinity";

        await run();

        expect(mockSendJobConclusionSpan).toHaveBeenCalledWith("gh-aw.job.conclusion", { startMs: undefined });
      });
    });
  });

  describe("error handling", () => {
    it("should propagate errors from sendJobConclusionSpan when endpoint is configured", async () => {
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "http://localhost:4318" }]);
      mockSendJobConclusionSpan.mockRejectedValueOnce(new Error("Network error"));

      // run() propagates the error; callers swallow it via .catch(() => {})
      await expect(run()).rejects.toThrow("Network error");
    });

    it("should propagate errors from sendJobConclusionSpan even without endpoint (JSONL mirror path)", async () => {
      mockSendJobConclusionSpan.mockRejectedValueOnce(new Error("JSONL write error"));

      await expect(run()).rejects.toThrow("JSONL write error");
    });

    it("should not log 'conclusion span export attempted' when an error occurs", async () => {
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "http://localhost:4318" }]);
      mockSendJobConclusionSpan.mockRejectedValueOnce(new Error("fail"));

      await expect(run()).rejects.toThrow("fail");
      expect(mockCore.info).not.toHaveBeenCalledWith("[otlp] conclusion span export attempted");
    });
  });
});

describe("buildSpanName", () => {
  it("returns default span name when jobName is undefined", () => {
    expect(buildSpanName(undefined)).toBe("gh-aw.job.conclusion");
  });

  it("returns default span name when jobName is empty string", () => {
    expect(buildSpanName("")).toBe("gh-aw.job.conclusion");
  });

  it("returns namespaced span name when jobName is provided", () => {
    expect(buildSpanName("agent")).toBe("gh-aw.agent.conclusion");
  });

  it("handles job names with hyphens", () => {
    expect(buildSpanName("my-job")).toBe("gh-aw.my-job.conclusion");
  });

  it("handles job names with dots", () => {
    expect(buildSpanName("setup.v2")).toBe("gh-aw.setup.v2.conclusion");
  });
});

describe("parseJobStartMs", () => {
  it("returns undefined when input is undefined", () => {
    expect(parseJobStartMs(undefined)).toBeUndefined();
  });

  it("returns undefined when input is empty string", () => {
    expect(parseJobStartMs("")).toBeUndefined();
  });

  it("returns undefined when input is '0'", () => {
    expect(parseJobStartMs("0")).toBeUndefined();
  });

  it("returns undefined when input is negative", () => {
    expect(parseJobStartMs("-1000")).toBeUndefined();
  });

  it("returns undefined when input is non-numeric", () => {
    expect(parseJobStartMs("not-a-number")).toBeUndefined();
  });

  it("returns undefined when input is 'Infinity'", () => {
    expect(parseJobStartMs("Infinity")).toBeUndefined();
  });

  it("returns the numeric value for a valid timestamp string", () => {
    expect(parseJobStartMs("1700000000000")).toBe(1700000000000);
  });

  it("returns the numeric value for a small positive number", () => {
    expect(parseJobStartMs("1")).toBe(1);
  });
});
