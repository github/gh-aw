// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "module";

const req = createRequire(import.meta.url);

// ---------------------------------------------------------------------------
// Load otlp.cjs (module under test) and patch its send_otlp_span dependency.
// Both share the same CJS module cache, so we can replace exports on the
// already-loaded send_otlp_span module and the otlp module picks them up.
// ---------------------------------------------------------------------------

const sendOtlpModule = req("./send_otlp_span.cjs");
const otlp = req("./otlp.cjs");

/** 32 lowercase hex chars — valid OTLP trace ID */
const VALID_TRACE_ID = "aabbccdd00112233aabbccdd00112233";
/** 16 lowercase hex chars — valid OTLP span ID */
const VALID_SPAN_ID = "aabbccdd00112233";

// Stable stubs that we swap in before each test
const mockBuildAttr = vi.fn();
const mockBuildOTLPPayload = vi.fn();
const mockSendOTLPSpan = vi.fn();
const mockSendOTLPToAllEndpoints = vi.fn();
const mockSanitizeOTLPPayload = vi.fn();
const mockAppendToOTLPJSONL = vi.fn();
const mockGenerateSpanId = vi.fn();
const mockIsValidTraceId = vi.fn();
const mockIsValidSpanId = vi.fn();
const mockBuildGitHubActionsResourceAttributes = vi.fn();
const mockReadJSONIfExists = vi.fn();

// Capture originals so we can restore them after each test
const PATCHED_KEYS = [
  "buildAttr",
  "buildOTLPPayload",
  "sendOTLPSpan",
  "sendOTLPToAllEndpoints",
  "sanitizeOTLPPayload",
  "appendToOTLPJSONL",
  "generateSpanId",
  "isValidTraceId",
  "isValidSpanId",
  "buildGitHubActionsResourceAttributes",
  "readJSONIfExists",
];
const originals = Object.fromEntries(PATCHED_KEYS.map(k => [k, sendOtlpModule[k]]));

describe("otlp.cjs", () => {
  /** @type {Record<string, string | undefined>} */
  let savedEnv;

  beforeEach(() => {
    vi.clearAllMocks();
    vi.spyOn(console, "warn").mockImplementation(() => {});

    // Re-apply default implementations after clearAllMocks (which resets them)
    mockBuildAttr.mockImplementation((key, value) => ({ key, value }));
    mockBuildOTLPPayload.mockReturnValue({ resourceSpans: [] });
    mockSendOTLPSpan.mockResolvedValue(undefined);
    // sendOTLPToAllEndpoints delegates to mockSendOTLPSpan so existing per-span
    // assertions still work without changing each individual test.
    mockSendOTLPToAllEndpoints.mockImplementation(async (endpoints, payload, opts) => {
      for (const ep of endpoints) {
        await mockSendOTLPSpan(ep.url, payload, { ...opts, headersOverride: ep.headers !== undefined ? ep.headers : "" });
      }
    });
    mockSanitizeOTLPPayload.mockImplementation(p => p);
    mockAppendToOTLPJSONL.mockReturnValue(undefined);
    mockGenerateSpanId.mockReturnValue(VALID_SPAN_ID);
    mockIsValidTraceId.mockImplementation(id => id === VALID_TRACE_ID);
    mockIsValidSpanId.mockImplementation(id => id === VALID_SPAN_ID);
    mockBuildGitHubActionsResourceAttributes.mockReturnValue([{ key: "github.repository", value: "owner/repo" }]);
    // Default: no aw_info.json present (the common env-only path)
    mockReadJSONIfExists.mockReturnValue(null);

    // Patch the shared CJS module exports
    sendOtlpModule.buildAttr = mockBuildAttr;
    sendOtlpModule.buildOTLPPayload = mockBuildOTLPPayload;
    sendOtlpModule.sendOTLPSpan = mockSendOTLPSpan;
    sendOtlpModule.sendOTLPToAllEndpoints = mockSendOTLPToAllEndpoints;
    sendOtlpModule.sanitizeOTLPPayload = mockSanitizeOTLPPayload;
    sendOtlpModule.appendToOTLPJSONL = mockAppendToOTLPJSONL;
    sendOtlpModule.generateSpanId = mockGenerateSpanId;
    sendOtlpModule.isValidTraceId = mockIsValidTraceId;
    sendOtlpModule.isValidSpanId = mockIsValidSpanId;
    sendOtlpModule.buildGitHubActionsResourceAttributes = mockBuildGitHubActionsResourceAttributes;
    sendOtlpModule.readJSONIfExists = mockReadJSONIfExists;
    // Keep SPAN_KIND_CLIENT as-is (it's a constant and does not need a stub)

    savedEnv = {
      GH_AW_OTLP_ENDPOINTS: process.env.GH_AW_OTLP_ENDPOINTS,
      GITHUB_AW_OTEL_TRACE_ID: process.env.GITHUB_AW_OTEL_TRACE_ID,
      GITHUB_AW_OTEL_PARENT_SPAN_ID: process.env.GITHUB_AW_OTEL_PARENT_SPAN_ID,
      GITHUB_REPOSITORY: process.env.GITHUB_REPOSITORY,
      GITHUB_RUN_ID: process.env.GITHUB_RUN_ID,
      GITHUB_RUN_ATTEMPT: process.env.GITHUB_RUN_ATTEMPT,
      GITHUB_EVENT_NAME: process.env.GITHUB_EVENT_NAME,
      GITHUB_REF: process.env.GITHUB_REF,
      GITHUB_REF_NAME: process.env.GITHUB_REF_NAME,
      GITHUB_HEAD_REF: process.env.GITHUB_HEAD_REF,
      GITHUB_SHA: process.env.GITHUB_SHA,
      GITHUB_JOB: process.env.GITHUB_JOB,
      GITHUB_ACTOR_ID: process.env.GITHUB_ACTOR_ID,
      RUNNER_OS: process.env.RUNNER_OS,
      RUNNER_ARCH: process.env.RUNNER_ARCH,
      RUNNER_NAME: process.env.RUNNER_NAME,
      RUNNER_ENVIRONMENT: process.env.RUNNER_ENVIRONMENT,
      GITHUB_WORKFLOW_REF: process.env.GITHUB_WORKFLOW_REF,
      GH_AW_CURRENT_WORKFLOW_REF: process.env.GH_AW_CURRENT_WORKFLOW_REF,
      GH_AW_INFO_STAGED: process.env.GH_AW_INFO_STAGED,
      GH_AW_INFO_VERSION: process.env.GH_AW_INFO_VERSION,
      GH_AW_INFO_CLI_VERSION: process.env.GH_AW_INFO_CLI_VERSION,
      OTEL_SERVICE_NAME: process.env.OTEL_SERVICE_NAME,
      GITHUB_SERVER_URL: process.env.GITHUB_SERVER_URL,
    };

    process.env.GITHUB_AW_OTEL_TRACE_ID = VALID_TRACE_ID;
    process.env.GITHUB_AW_OTEL_PARENT_SPAN_ID = VALID_SPAN_ID;
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://otel.example.com" }]);
    process.env.GITHUB_REPOSITORY = "owner/repo";
    process.env.GITHUB_RUN_ID = "99887766";
    process.env.GITHUB_EVENT_NAME = "push";
    process.env.GH_AW_INFO_VERSION = "v1.2.3";
    delete process.env.GH_AW_INFO_CLI_VERSION;
    delete process.env.OTEL_SERVICE_NAME;
    delete process.env.GH_AW_INFO_STAGED;
  });

  afterEach(() => {
    for (const [k, v] of Object.entries(originals)) {
      sendOtlpModule[k] = v;
    }
    for (const [k, v] of Object.entries(savedEnv)) {
      if (v === undefined) {
        delete process.env[k];
      } else {
        process.env[k] = v;
      }
    }
    vi.restoreAllMocks();
  });

  // ---------------------------------------------------------------------------
  // shim.cjs integration — global.core must be available after require
  // ---------------------------------------------------------------------------

  describe("shim integration", () => {
    it("populates global.core when otlp.cjs is loaded", () => {
      // otlp.cjs requires shim.cjs at module load time; by the time we reach
      // this test global.core must already be set (either by the real
      // github-script runtime or by the shim).
      expect(global.core).toBeDefined();
      expect(typeof global.core.warning).toBe("function");
      expect(typeof global.core.info).toBe("function");
    });
  });

  // ---------------------------------------------------------------------------
  // logSpan — happy path
  // ---------------------------------------------------------------------------

  describe("logSpan", () => {
    it("calls sendOTLPSpan with a payload that includes the canonical gh-aw service name", async () => {
      await otlp.logSpan("my-scanner", { "my-scanner.issues_found": 3 });

      expect(mockSendOTLPSpan).toHaveBeenCalledOnce();
      expect(mockBuildOTLPPayload).toHaveBeenCalledOnce();
      const payloadOpts = mockBuildOTLPPayload.mock.calls[0][0];
      expect(payloadOpts.serviceName).toBe("gh-aw");
      expect(payloadOpts.spanName).toBe("my-scanner.run");
    });

    it("uses OTEL_SERVICE_NAME when set", async () => {
      process.env.OTEL_SERVICE_NAME = "custom-gh-aw";

      await otlp.logSpan("my-scanner", {});

      const payloadOpts = mockBuildOTLPPayload.mock.calls[0][0];
      expect(payloadOpts.serviceName).toBe("custom-gh-aw");
    });

    it("uses the trace ID from GITHUB_AW_OTEL_TRACE_ID", async () => {
      await otlp.logSpan("my-scanner", {});

      const payloadOpts = mockBuildOTLPPayload.mock.calls[0][0];
      expect(payloadOpts.traceId).toBe(VALID_TRACE_ID);
    });

    it("uses the parent span ID from GITHUB_AW_OTEL_PARENT_SPAN_ID", async () => {
      await otlp.logSpan("my-scanner", {});

      const payloadOpts = mockBuildOTLPPayload.mock.calls[0][0];
      expect(payloadOpts.parentSpanId).toBe(VALID_SPAN_ID);
    });

    it("reads the endpoint from GH_AW_OTLP_ENDPOINTS", async () => {
      await otlp.logSpan("my-scanner", {});

      expect(mockSendOTLPSpan).toHaveBeenCalledWith("https://otel.example.com", expect.anything(), expect.objectContaining({ skipJSONL: true }));
    });

    it("converts attributes object to buildAttr calls", async () => {
      await otlp.logSpan("my-scanner", { "my-scanner.count": 5, "my-scanner.ok": true, "my-scanner.label": "x" });

      expect(mockBuildAttr).toHaveBeenCalledWith("my-scanner.count", 5);
      expect(mockBuildAttr).toHaveBeenCalledWith("my-scanner.ok", true);
      expect(mockBuildAttr).toHaveBeenCalledWith("my-scanner.label", "x");
    });

    it("uses statusCode 1 (OK) by default", async () => {
      await otlp.logSpan("my-scanner", {});

      const payloadOpts = mockBuildOTLPPayload.mock.calls[0][0];
      expect(payloadOpts.statusCode).toBe(1);
    });

    it("uses statusCode 2 (ERROR) when isError is true", async () => {
      await otlp.logSpan("my-scanner", {}, { isError: true, errorMessage: "scan failed" });

      const payloadOpts = mockBuildOTLPPayload.mock.calls[0][0];
      expect(payloadOpts.statusCode).toBe(2);
      expect(payloadOpts.statusMessage).toBe("scan failed");
    });

    it("accepts options.traceId override", async () => {
      const customTrace = "ccddee0011223344ccddee0011223344";
      mockIsValidTraceId.mockImplementation(id => id === customTrace);

      await otlp.logSpan("my-scanner", {}, { traceId: customTrace });

      const payloadOpts = mockBuildOTLPPayload.mock.calls[0][0];
      expect(payloadOpts.traceId).toBe(customTrace);
    });

    it("accepts options.endpoint override", async () => {
      await otlp.logSpan("my-scanner", {}, { endpoint: "https://custom.otel.io" });

      expect(mockSendOTLPSpan).toHaveBeenCalledWith("https://custom.otel.io", expect.anything(), { skipJSONL: true });
    });

    it("does not attempt HTTP export when GH_AW_OTLP_ENDPOINTS is not set", async () => {
      delete process.env.GH_AW_OTLP_ENDPOINTS;

      await otlp.logSpan("my-scanner", {});

      expect(mockSendOTLPSpan).not.toHaveBeenCalled();
      expect(mockAppendToOTLPJSONL).toHaveBeenCalledOnce();
    });

    it("sanitizes the payload before writing to the JSONL mirror", async () => {
      const rawPayload = { resourceSpans: ["raw"] };
      const sanitizedPayload = { resourceSpans: ["sanitized"] };
      mockBuildOTLPPayload.mockReturnValue(rawPayload);
      mockSanitizeOTLPPayload.mockReturnValue(sanitizedPayload);

      await otlp.logSpan("my-scanner", {});

      expect(mockSanitizeOTLPPayload).toHaveBeenCalledWith(rawPayload);
      expect(mockAppendToOTLPJSONL).toHaveBeenCalledWith(sanitizedPayload);
      // Wire export still uses the original payload (sendOTLPSpan sanitizes internally)
      expect(mockSendOTLPSpan).toHaveBeenCalledWith(expect.any(String), rawPayload, expect.objectContaining({ skipJSONL: true }));
    });
  });

  // ---------------------------------------------------------------------------
  // logSpan — missing / invalid trace ID
  // ---------------------------------------------------------------------------

  describe("logSpan — missing trace ID", () => {
    it("silently skips the span when GITHUB_AW_OTEL_TRACE_ID is not set", async () => {
      delete process.env.GITHUB_AW_OTEL_TRACE_ID;
      mockIsValidTraceId.mockReturnValue(false);

      await otlp.logSpan("my-scanner", { "my-scanner.count": 1 });

      expect(mockSendOTLPSpan).not.toHaveBeenCalled();
      expect(console.warn).not.toHaveBeenCalled();
    });
  });

  // ---------------------------------------------------------------------------
  // logSpan — error resilience
  // ---------------------------------------------------------------------------

  describe("logSpan — error resilience", () => {
    it("reports exporter failure through a redacted callback when supplied", async () => {
      mockSendOTLPSpan.mockRejectedValue(new Error("private-exporter-url-and-token"));
      const onError = vi.fn();
      await expect(otlp.logSpan("safe-output", {}, { onError })).resolves.toBeUndefined();
      expect(onError).toHaveBeenCalledWith();
      expect(console.warn).not.toHaveBeenCalled();
    });

    it("does not throw when sendOTLPSpan rejects", async () => {
      mockSendOTLPSpan.mockRejectedValue(new Error("network failure"));

      await expect(otlp.logSpan("my-scanner", {})).resolves.toBeUndefined();
      expect(console.warn).toHaveBeenCalledWith(expect.stringContaining("network failure"));
    });

    it("does not throw when an internal helper throws synchronously", async () => {
      mockBuildOTLPPayload.mockImplementation(() => {
        throw new Error("unexpected");
      });

      await expect(otlp.logSpan("my-scanner", {})).resolves.toBeUndefined();
      expect(console.warn).toHaveBeenCalledWith(expect.stringContaining("unexpected"));
    });
  });

  // ---------------------------------------------------------------------------
  // logSpan — omits parentSpanId when invalid
  // ---------------------------------------------------------------------------

  describe("logSpan — parent span ID handling", () => {
    it("omits parentSpanId when GITHUB_AW_OTEL_PARENT_SPAN_ID is not set", async () => {
      delete process.env.GITHUB_AW_OTEL_PARENT_SPAN_ID;
      mockIsValidSpanId.mockReturnValue(false);

      await otlp.logSpan("my-scanner", {});

      const payloadOpts = mockBuildOTLPPayload.mock.calls[0][0];
      expect(payloadOpts.parentSpanId).toBeUndefined();
    });
  });

  // ---------------------------------------------------------------------------
  // logSpan — resource attributes
  // ---------------------------------------------------------------------------

  describe("logSpan — resource attributes", () => {
    it("passes scopeVersion from GH_AW_INFO_VERSION when aw_info.json is absent", async () => {
      process.env.GH_AW_INFO_VERSION = "v2.0.0";

      await otlp.logSpan("my-scanner", {});

      const payloadOpts = mockBuildOTLPPayload.mock.calls[0][0];
      expect(payloadOpts.scopeVersion).toBe("v2.0.0");
    });

    it("falls back to 'unknown' when aw_info.json is absent and GH_AW_INFO_VERSION is not set", async () => {
      delete process.env.GH_AW_INFO_VERSION;

      await otlp.logSpan("my-scanner", {});

      const payloadOpts = mockBuildOTLPPayload.mock.calls[0][0];
      expect(payloadOpts.scopeVersion).toBe("unknown");
    });

    it("passes the result of buildGitHubActionsResourceAttributes to buildOTLPPayload as resourceAttributes", async () => {
      const mockAttrs = [{ key: "github.repository", value: { stringValue: "owner/repo" } }];
      mockBuildGitHubActionsResourceAttributes.mockReturnValue(mockAttrs);

      await otlp.logSpan("my-scanner", {});

      const payloadOpts = mockBuildOTLPPayload.mock.calls[0][0];
      expect(payloadOpts.resourceAttributes).toBe(mockAttrs);
    });

    it("passes GITHUB_REPOSITORY to buildGitHubActionsResourceAttributes", async () => {
      process.env.GITHUB_REPOSITORY = "myorg/myrepo";

      await otlp.logSpan("my-scanner", {});

      expect(mockBuildGitHubActionsResourceAttributes).toHaveBeenCalledWith(expect.objectContaining({ repository: "myorg/myrepo" }));
    });

    it("passes GITHUB_RUN_ID to buildGitHubActionsResourceAttributes", async () => {
      process.env.GITHUB_RUN_ID = "12345678";

      await otlp.logSpan("my-scanner", {});

      expect(mockBuildGitHubActionsResourceAttributes).toHaveBeenCalledWith(expect.objectContaining({ runId: "12345678" }));
    });

    it("passes GITHUB_RUN_ATTEMPT to buildGitHubActionsResourceAttributes", async () => {
      process.env.GITHUB_RUN_ATTEMPT = "3";

      await otlp.logSpan("my-scanner", {});

      expect(mockBuildGitHubActionsResourceAttributes).toHaveBeenCalledWith(expect.objectContaining({ runAttempt: "3" }));
    });

    it("passes '1' for runAttempt to buildGitHubActionsResourceAttributes when GITHUB_RUN_ATTEMPT is not set", async () => {
      delete process.env.GITHUB_RUN_ATTEMPT;

      await otlp.logSpan("my-scanner", {});

      expect(mockBuildGitHubActionsResourceAttributes).toHaveBeenCalledWith(expect.objectContaining({ runAttempt: "1" }));
    });

    it("passes GITHUB_EVENT_NAME to buildGitHubActionsResourceAttributes when set", async () => {
      process.env.GITHUB_EVENT_NAME = "pull_request";

      await otlp.logSpan("my-scanner", {});

      expect(mockBuildGitHubActionsResourceAttributes).toHaveBeenCalledWith(expect.objectContaining({ eventName: "pull_request" }));
    });

    it("passes empty string for eventName to buildGitHubActionsResourceAttributes when not set", async () => {
      delete process.env.GITHUB_EVENT_NAME;

      await otlp.logSpan("my-scanner", {});

      expect(mockBuildGitHubActionsResourceAttributes).toHaveBeenCalledWith(expect.objectContaining({ eventName: "" }));
    });

    it("passes staged=false to buildGitHubActionsResourceAttributes when GH_AW_INFO_STAGED is not set", async () => {
      delete process.env.GH_AW_INFO_STAGED;

      await otlp.logSpan("my-scanner", {});

      expect(mockBuildGitHubActionsResourceAttributes).toHaveBeenCalledWith(expect.objectContaining({ staged: false }));
    });

    it("passes staged=true to buildGitHubActionsResourceAttributes when GH_AW_INFO_STAGED is 'true'", async () => {
      process.env.GH_AW_INFO_STAGED = "true";

      await otlp.logSpan("my-scanner", {});

      expect(mockBuildGitHubActionsResourceAttributes).toHaveBeenCalledWith(expect.objectContaining({ staged: true }));
    });

    it("passes GITHUB_SHA to buildGitHubActionsResourceAttributes when set", async () => {
      process.env.GITHUB_SHA = "abc123def456";

      await otlp.logSpan("my-scanner", {});

      expect(mockBuildGitHubActionsResourceAttributes).toHaveBeenCalledWith(expect.objectContaining({ sha: "abc123def456" }));
    });

    it("passes GITHUB_JOB to buildGitHubActionsResourceAttributes when set", async () => {
      process.env.GITHUB_JOB = "agent";

      await otlp.logSpan("my-scanner", {});

      expect(mockBuildGitHubActionsResourceAttributes).toHaveBeenCalledWith(expect.objectContaining({ job: "agent" }));
    });

    it("passes runner.* and GITHUB_ACTOR_ID to buildGitHubActionsResourceAttributes when set", async () => {
      process.env.GITHUB_ACTOR_ID = "4175913";
      process.env.RUNNER_OS = "Linux";
      process.env.RUNNER_ARCH = "X64";
      process.env.RUNNER_NAME = "GitHub Actions 1187452382";
      process.env.RUNNER_ENVIRONMENT = "github-hosted";

      await otlp.logSpan("my-scanner", {});

      expect(mockBuildGitHubActionsResourceAttributes).toHaveBeenCalledWith(
        expect.objectContaining({
          actorId: "4175913",
          runnerOs: "Linux",
          runnerArch: "X64",
          runnerName: "GitHub Actions 1187452382",
          runnerEnvironment: "github-hosted",
        })
      );
    });
  });

  // ---------------------------------------------------------------------------
  // logSpan — aw_info.json runtime path
  // ---------------------------------------------------------------------------

  describe("logSpan — aw_info.json runtime path", () => {
    it("reads version from aw_info.json when env var is absent", async () => {
      delete process.env.GH_AW_INFO_VERSION;
      mockReadJSONIfExists.mockReturnValue({ version: "v3.1.0" });

      await otlp.logSpan("my-scanner", {});

      const payloadOpts = mockBuildOTLPPayload.mock.calls[0][0];
      expect(payloadOpts.scopeVersion).toBe("v3.1.0");
    });

    it("prefers agent_version over version in aw_info.json", async () => {
      mockReadJSONIfExists.mockReturnValue({ agent_version: "v4.0.0", version: "v3.0.0" });

      await otlp.logSpan("my-scanner", {});

      const payloadOpts = mockBuildOTLPPayload.mock.calls[0][0];
      expect(payloadOpts.scopeVersion).toBe("v4.0.0");
    });

    it("prefers cli_version over engine version fields in aw_info.json", async () => {
      mockReadJSONIfExists.mockReturnValue({ cli_version: "v9.1.0", agent_version: "2.1.168", version: "2.1.168" });

      await otlp.logSpan("my-scanner", {});

      const payloadOpts = mockBuildOTLPPayload.mock.calls[0][0];
      expect(payloadOpts.scopeVersion).toBe("v9.1.0");
    });

    it("reads staged=true from aw_info.json when env var is absent", async () => {
      delete process.env.GH_AW_INFO_STAGED;
      mockReadJSONIfExists.mockReturnValue({ staged: true });

      await otlp.logSpan("my-scanner", {});

      expect(mockBuildGitHubActionsResourceAttributes).toHaveBeenCalledWith(expect.objectContaining({ staged: true }));
    });

    it("reads staged=false from aw_info.json when staged is not set there", async () => {
      delete process.env.GH_AW_INFO_STAGED;
      mockReadJSONIfExists.mockReturnValue({ staged: false });

      await otlp.logSpan("my-scanner", {});

      expect(mockBuildGitHubActionsResourceAttributes).toHaveBeenCalledWith(expect.objectContaining({ staged: false }));
    });

    it("aw_info.json staged takes precedence over GH_AW_INFO_STAGED=false", async () => {
      // awInfo.staged === true wins even when env var would say non-staged
      delete process.env.GH_AW_INFO_STAGED;
      mockReadJSONIfExists.mockReturnValue({ staged: true });

      await otlp.logSpan("my-scanner", {});

      expect(mockBuildGitHubActionsResourceAttributes).toHaveBeenCalledWith(expect.objectContaining({ staged: true }));
    });

    it("falls back to GH_AW_INFO_STAGED when aw_info.json has staged=false", async () => {
      process.env.GH_AW_INFO_STAGED = "true";
      mockReadJSONIfExists.mockReturnValue({ staged: false });

      await otlp.logSpan("my-scanner", {});

      // staged = (false === true) || ("true" === "true") => true
      expect(mockBuildGitHubActionsResourceAttributes).toHaveBeenCalledWith(expect.objectContaining({ staged: true }));
    });

    it("calls readJSONIfExists with the aw_info.json path", async () => {
      await otlp.logSpan("my-scanner", {});

      expect(mockReadJSONIfExists).toHaveBeenCalledWith("/tmp/gh-aw/aw_info.json");
    });

    it("passes awfVersion and awmgVersion from aw_info.json to buildGitHubActionsResourceAttributes", async () => {
      mockReadJSONIfExists.mockReturnValue({ awf_version: "v1.2.3-awf", awmg_version: "v4.5.6-awmg" });

      await otlp.logSpan("my-scanner", {});

      expect(mockBuildGitHubActionsResourceAttributes).toHaveBeenCalledWith(
        expect.objectContaining({
          awfVersion: "v1.2.3-awf",
          awmgVersion: "v4.5.6-awmg",
        })
      );
    });
  });
});
