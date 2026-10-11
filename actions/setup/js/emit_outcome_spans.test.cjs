import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import { createRequire } from "module";

const req = createRequire(import.meta.url);

// Set up global.core mock before loading any module that requires shim.cjs so
// the shim sees it already present and does not overwrite it.
const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
};
global.core = mockCore;

const sendOtlpModule = req("./send_otlp_span.cjs");

const mockGenerateTraceId = vi.fn();
const mockGenerateSpanId = vi.fn();
const mockBuildAttr = vi.fn();
const mockBuildOTLPSpan = vi.fn();
const mockBuildOTLPBatchPayload = vi.fn();
const mockBuildGitHubActionsResourceAttributes = vi.fn();
const mockParseOTLPEndpoints = vi.fn();
const mockSendOTLPToAllEndpoints = vi.fn();
const mockAppendToOTLPJSONL = vi.fn();
const mockReadJSONIfExists = vi.fn();

const PATCHED_KEYS = [
  "generateTraceId",
  "generateSpanId",
  "buildAttr",
  "buildOTLPSpan",
  "buildOTLPBatchPayload",
  "buildGitHubActionsResourceAttributes",
  "parseOTLPEndpoints",
  "sendOTLPToAllEndpoints",
  "appendToOTLPJSONL",
  "readJSONIfExists",
];

const originals = Object.fromEntries(PATCHED_KEYS.map(key => [key, sendOtlpModule[key]]));

const EVALUATIONS_PATH = "/tmp/gh-aw/outcome-evaluations.jsonl";
const AW_INFO_PATH = "/tmp/gh-aw/aw_info.json";
const SUMMARY_PATH = "/tmp/gh-aw/outcome-summary.json";

describe("emit_outcome_spans.cjs", () => {
  /** @type {Record<string, string | undefined>} */
  let savedEnv;
  /** @type {unknown} */
  let currentAwInfo;
  /** @type {unknown} */
  let currentSummary;
  /** @type {{ main: () => Promise<void> }} */
  let moduleUnderTest;
  let spanCounter;

  beforeEach(() => {
    vi.clearAllMocks();
    vi.spyOn(console, "warn").mockImplementation(() => {});

    fs.mkdirSync("/tmp/gh-aw", { recursive: true });
    for (const path of [EVALUATIONS_PATH, AW_INFO_PATH, SUMMARY_PATH]) {
      if (fs.existsSync(path)) {
        fs.unlinkSync(path);
      }
    }

    currentAwInfo = null;
    currentSummary = null;
    spanCounter = 0;

    mockGenerateTraceId.mockReturnValue("trace-generated-1234567890abcdef");
    mockGenerateSpanId.mockImplementation(() => `span-id-${++spanCounter}`);
    mockBuildAttr.mockImplementation((key, value) => ({ key, value }));
    mockBuildOTLPSpan.mockImplementation(opts => ({ ...opts }));
    mockBuildOTLPBatchPayload.mockImplementation(opts => ({ payload: true, ...opts }));
    mockBuildGitHubActionsResourceAttributes.mockImplementation(({ repository, staged }) => [
      { key: "github.repository", value: repository },
      { key: "deployment.environment", value: staged ? "staging" : "production" },
    ]);
    mockParseOTLPEndpoints.mockReturnValue([]);
    mockSendOTLPToAllEndpoints.mockResolvedValue(undefined);
    mockAppendToOTLPJSONL.mockReturnValue(undefined);
    mockReadJSONIfExists.mockImplementation(filePath => {
      if (filePath === AW_INFO_PATH) return currentAwInfo;
      if (filePath === SUMMARY_PATH) return currentSummary;
      return null;
    });

    sendOtlpModule.generateTraceId = mockGenerateTraceId;
    sendOtlpModule.generateSpanId = mockGenerateSpanId;
    sendOtlpModule.buildAttr = mockBuildAttr;
    sendOtlpModule.buildOTLPSpan = mockBuildOTLPSpan;
    sendOtlpModule.buildOTLPBatchPayload = mockBuildOTLPBatchPayload;
    sendOtlpModule.buildGitHubActionsResourceAttributes = mockBuildGitHubActionsResourceAttributes;
    sendOtlpModule.parseOTLPEndpoints = mockParseOTLPEndpoints;
    sendOtlpModule.sendOTLPToAllEndpoints = mockSendOTLPToAllEndpoints;
    sendOtlpModule.appendToOTLPJSONL = mockAppendToOTLPJSONL;
    sendOtlpModule.readJSONIfExists = mockReadJSONIfExists;

    savedEnv = {
      GITHUB_AW_OTEL_TRACE_ID: process.env.GITHUB_AW_OTEL_TRACE_ID,
      GITHUB_AW_OTEL_PARENT_SPAN_ID: process.env.GITHUB_AW_OTEL_PARENT_SPAN_ID,
      GITHUB_REPOSITORY: process.env.GITHUB_REPOSITORY,
      GITHUB_RUN_ID: process.env.GITHUB_RUN_ID,
      GITHUB_RUN_ATTEMPT: process.env.GITHUB_RUN_ATTEMPT,
      GITHUB_EVENT_NAME: process.env.GITHUB_EVENT_NAME,
      GITHUB_REF: process.env.GITHUB_REF,
      GITHUB_REF_NAME: process.env.GITHUB_REF_NAME,
      GITHUB_SHA: process.env.GITHUB_SHA,
      GITHUB_JOB: process.env.GITHUB_JOB,
      GITHUB_WORKFLOW_REF: process.env.GITHUB_WORKFLOW_REF,
      GITHUB_ACTOR_ID: process.env.GITHUB_ACTOR_ID,
      RUNNER_OS: process.env.RUNNER_OS,
      RUNNER_ARCH: process.env.RUNNER_ARCH,
      RUNNER_NAME: process.env.RUNNER_NAME,
      RUNNER_ENVIRONMENT: process.env.RUNNER_ENVIRONMENT,
      GH_AW_INFO_STAGED: process.env.GH_AW_INFO_STAGED,
      GH_AW_INFO_VERSION: process.env.GH_AW_INFO_VERSION,
      GH_AW_INFO_CLI_VERSION: process.env.GH_AW_INFO_CLI_VERSION,
      OTEL_SERVICE_NAME: process.env.OTEL_SERVICE_NAME,
    };

    process.env.GITHUB_AW_OTEL_TRACE_ID = "feedfacefeedfacefeedfacefeedface";
    process.env.GITHUB_AW_OTEL_PARENT_SPAN_ID = "cafebabecafebabe";
    process.env.GITHUB_REPOSITORY = "github/gh-aw";
    process.env.GITHUB_RUN_ID = "12345";
    process.env.GITHUB_RUN_ATTEMPT = "2";
    process.env.GITHUB_EVENT_NAME = "workflow_dispatch";
    process.env.GITHUB_REF = "refs/heads/main";
    process.env.GITHUB_REF_NAME = "main";
    process.env.GITHUB_SHA = "abc123";
    process.env.GITHUB_JOB = "outcome-collector";
    process.env.GITHUB_WORKFLOW_REF = "github/gh-aw/.github/workflows/outcome.yml@refs/heads/main";
    process.env.GITHUB_ACTOR_ID = "1234567";
    process.env.RUNNER_OS = "Linux";
    process.env.RUNNER_ARCH = "X64";
    process.env.RUNNER_NAME = "GitHub Actions 99";
    process.env.RUNNER_ENVIRONMENT = "github-hosted";
    delete process.env.GH_AW_INFO_STAGED;
    delete process.env.GH_AW_INFO_VERSION;
    delete process.env.GH_AW_INFO_CLI_VERSION;
    delete process.env.OTEL_SERVICE_NAME;

    const emitModulePath = req.resolve("./emit_outcome_spans.cjs");
    delete req.cache[emitModulePath];
    moduleUnderTest = req("./emit_outcome_spans.cjs");
  });

  afterEach(() => {
    for (const key of PATCHED_KEYS) {
      sendOtlpModule[key] = originals[key];
    }

    const emitModulePath = req.resolve("./emit_outcome_spans.cjs");
    delete req.cache[emitModulePath];

    for (const [key, value] of Object.entries(savedEnv)) {
      if (value === undefined) {
        delete process.env[key];
      } else {
        process.env[key] = value;
      }
    }

    for (const path of [EVALUATIONS_PATH, AW_INFO_PATH, SUMMARY_PATH]) {
      if (fs.existsSync(path)) {
        fs.unlinkSync(path);
      }
    }

    vi.restoreAllMocks();
  });

  it("no-ops when there are no evaluations and no summary data", async () => {
    await moduleUnderTest.main();

    expect(mockCore.info).toHaveBeenCalledWith("[outcome-otel] No outcome evaluations to export");
    expect(mockBuildGitHubActionsResourceAttributes).not.toHaveBeenCalled();
    expect(mockBuildOTLPBatchPayload).not.toHaveBeenCalled();
    expect(mockAppendToOTLPJSONL).not.toHaveBeenCalled();
    expect(mockSendOTLPToAllEndpoints).not.toHaveBeenCalled();
  });

  it("builds item and summary spans using aw_info staged/version metadata and mirrors locally without endpoints", async () => {
    currentAwInfo = { staged: true, cli_version: "v0.79.1", agent_version: "2.1.168" };
    currentSummary = {
      runs_checked: 3,
      total_outcomes: 2,
      accepted: 1,
      rejected: 1,
      ignored: 0,
      pending: 0,
      unknown: 1,
      errors: 2,
      lifecycle: 3,
      noop: 0,
      accepted_strong: 1,
      accepted_medium: 0,
      accepted_weak: 0,
      fallback_exists_only_count: 1,
      noop_rate: 0,
      zero_touch: 1,
      zero_touch_rate: 1,
      median_resolution_sec: 42,
      acceptance_rate: 0.5,
      waste_rate: 0.5,
      date: "2026-05-13",
    };
    fs.writeFileSync(
      EVALUATIONS_PATH,
      [
        JSON.stringify({
          type: "issue",
          result: "accepted",
          outcome_status: "accepted",
          evidence_strength: "strong",
          signal: "merged",
          detail: "merged",
          workflow: "triage",
          run_id: 101,
          url: "https://github.com/github/gh-aw/issues/1",
          repo: "github/gh-aw",
          timestamp: "2026-05-13T09:00:00Z",
          review_comments: 0,
          changed_files: 3,
          additions: 10,
          deletions: 2,
          reactions_total: 5,
          reactions_positive: 4,
          reactions_negative: 1,
          comments: 0,
          human_comments: 0,
          human_reviews: 0,
          human_edits: 0,
          zero_touch: true,
        }),
        JSON.stringify({
          type: "comment",
          result: "rejected",
          outcome_status: "unknown",
          evidence_strength: "weak",
          signal: "target_exists_only",
          workflow: "triage",
          run_id: 102,
          repo: "github/gh-aw",
        }),
      ].join("\n")
    );

    await moduleUnderTest.main();

    expect(mockBuildGitHubActionsResourceAttributes).toHaveBeenCalledWith(
      expect.objectContaining({
        repository: "github/gh-aw",
        runId: "12345",
        runAttempt: "2",
        actorId: "1234567",
        runnerOs: "Linux",
        runnerArch: "X64",
        runnerName: "GitHub Actions 99",
        runnerEnvironment: "github-hosted",
        staged: true,
      })
    );

    expect(mockBuildOTLPBatchPayload).toHaveBeenCalledWith(
      expect.objectContaining({
        serviceName: "gh-aw",
        scopeVersion: "v0.79.1",
        resourceAttributes: [
          { key: "github.repository", value: "github/gh-aw" },
          { key: "deployment.environment", value: "staging" },
        ],
      })
    );

    const { spans } = mockBuildOTLPBatchPayload.mock.calls[0][0];
    const summarySpan = spans[0];
    expect(spans).toHaveLength(3);
    expect(summarySpan).toEqual(
      expect.objectContaining({
        spanName: "gh-aw.outcome.summary",
        parentSpanId: "cafebabecafebabe",
        statusCode: 1,
      })
    );
    expect(spans[1]).toEqual(
      expect.objectContaining({
        spanName: "gh-aw.outcome.evaluation",
        parentSpanId: summarySpan.spanId,
        statusCode: 1,
      })
    );
    expect(spans[2]).toEqual(
      expect.objectContaining({
        spanName: "gh-aw.outcome.evaluation",
        parentSpanId: summarySpan.spanId,
        statusCode: 0,
      })
    );

    expect(summarySpan.attributes).toContainEqual({ key: "gh-aw.exporter.name", value: "outcome-collector" });
    expect(summarySpan.attributes).toContainEqual({ key: "gh-aw.outcome.date", value: "2026-05-13" });
    expect(summarySpan.attributes).toContainEqual({ key: "gh-aw.outcome.zero_touch_count", value: 1 });
    expect(summarySpan.attributes).toContainEqual({ key: "gh-aw.outcome.accepted_strong", value: 1 });
    expect(summarySpan.attributes).toContainEqual({ key: "gh-aw.outcome.unknown", value: 1 });
    expect(summarySpan.attributes).toContainEqual({ key: "gh-aw.outcome.errors", value: 2 });
    expect(summarySpan.attributes).toContainEqual({ key: "gh-aw.outcome.lifecycle", value: 3 });
    expect(summarySpan.attributes).toContainEqual({ key: "gh-aw.outcome.fallback_exists_only_count", value: 1 });
    expect(spans[1].attributes).toContainEqual({ key: "gh-aw.exporter.name", value: "outcome-collector" });
    expect(spans[1].attributes).toContainEqual({ key: "gh-aw.outcome.url", value: "https://github.com/github/gh-aw/issues/1" });
    expect(spans[1].attributes).toContainEqual({ key: "gh-aw.outcome.detail", value: "merged" });
    expect(spans[1].attributes).toContainEqual({ key: "gh-aw.outcome.outcome_status", value: "accepted" });
    expect(spans[1].attributes).toContainEqual({ key: "gh-aw.outcome.evidence_strength", value: "strong" });
    expect(spans[1].attributes).toContainEqual({ key: "gh-aw.outcome.signal", value: "merged" });
    expect(spans[1].attributes).toContainEqual({ key: "gh-aw.outcome.created_at", value: "2026-05-13T09:00:00Z" });
    expect(spans[1].attributes).toContainEqual({ key: "gh-aw.outcome.review_comments", value: 0 });
    expect(spans[1].attributes).toContainEqual({ key: "gh-aw.outcome.changed_files", value: 3 });
    expect(spans[1].attributes).toContainEqual({ key: "gh-aw.outcome.additions", value: 10 });
    expect(spans[1].attributes).toContainEqual({ key: "gh-aw.outcome.deletions", value: 2 });
    expect(spans[1].attributes).toContainEqual({ key: "gh-aw.outcome.reactions_total", value: 5 });
    expect(spans[1].attributes).toContainEqual({ key: "gh-aw.outcome.reactions_positive", value: 4 });
    expect(spans[1].attributes).toContainEqual({ key: "gh-aw.outcome.reactions_negative", value: 1 });
    expect(spans[1].attributes).toContainEqual({ key: "gh-aw.outcome.comments", value: 0 });
    expect(spans[1].attributes).toContainEqual({ key: "gh-aw.outcome.human_comments", value: 0 });
    expect(spans[1].attributes).toContainEqual({ key: "gh-aw.outcome.human_reviews", value: 0 });
    expect(spans[1].attributes).toContainEqual({ key: "gh-aw.outcome.human_edits", value: 0 });
    expect(spans[1].attributes).toContainEqual({ key: "gh-aw.outcome.zero_touch", value: true });
    expect(spans[2].attributes.find(attr => attr.key === "gh-aw.outcome.review_comments")).toBeUndefined();
    expect(spans[2].attributes.find(attr => attr.key === "gh-aw.outcome.changed_files")).toBeUndefined();
    expect(spans[2].attributes.find(attr => attr.key === "gh-aw.outcome.additions")).toBeUndefined();
    expect(spans[2].attributes.find(attr => attr.key === "gh-aw.outcome.deletions")).toBeUndefined();
    expect(spans[2].attributes.find(attr => attr.key === "gh-aw.outcome.reactions_total")).toBeUndefined();
    expect(spans[2].attributes.find(attr => attr.key === "gh-aw.outcome.reactions_positive")).toBeUndefined();
    expect(spans[2].attributes.find(attr => attr.key === "gh-aw.outcome.reactions_negative")).toBeUndefined();
    expect(spans[2].attributes.find(attr => attr.key === "gh-aw.outcome.comments")).toBeUndefined();
    expect(spans[2].attributes.find(attr => attr.key === "gh-aw.outcome.zero_touch")).toBeUndefined();
    expect(spans[2].attributes).toContainEqual({ key: "gh-aw.outcome.outcome_status", value: "unknown" });
    expect(spans[2].attributes).toContainEqual({ key: "gh-aw.outcome.evidence_strength", value: "weak" });
    expect(spans[2].attributes).toContainEqual({ key: "gh-aw.outcome.signal", value: "target_exists_only" });

    expect(mockAppendToOTLPJSONL).toHaveBeenCalledOnce();
    expect(mockSendOTLPToAllEndpoints).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith("[outcome-otel] No OTLP endpoints configured, writing JSONL mirror only");
  });

  it("falls back to GH_AW_INFO_* env vars and exports to configured endpoints", async () => {
    currentAwInfo = { staged: false };
    currentSummary = { total_outcomes: 1 };
    process.env.GH_AW_INFO_STAGED = "true";
    process.env.GH_AW_INFO_VERSION = "v2.0.0";
    process.env.OTEL_SERVICE_NAME = "custom-gh-aw";
    mockParseOTLPEndpoints.mockReturnValue([{ url: "https://otel.example.com" }]);

    fs.writeFileSync(
      EVALUATIONS_PATH,
      JSON.stringify({
        type: "issue",
        result: "pending",
        workflow: "nightly",
        run_id: 500,
        repo: "github/gh-aw",
      }) + "\n"
    );

    await moduleUnderTest.main();

    expect(mockBuildGitHubActionsResourceAttributes).toHaveBeenCalledWith(expect.objectContaining({ staged: true }));
    expect(mockBuildOTLPBatchPayload).toHaveBeenCalledWith(
      expect.objectContaining({
        serviceName: "custom-gh-aw",
        scopeVersion: "v2.0.0",
      })
    );
    expect(mockAppendToOTLPJSONL).toHaveBeenCalledOnce();
    expect(mockSendOTLPToAllEndpoints).toHaveBeenCalledWith([{ url: "https://otel.example.com" }], expect.any(Object), { skipJSONL: true });
    expect(mockCore.info).toHaveBeenCalledWith("[outcome-otel] Exported to 1 endpoint(s)");
  });
});
