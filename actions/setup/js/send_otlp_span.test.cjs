import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import childProcess from "child_process";
import fs from "fs";

// ---------------------------------------------------------------------------
// Module import
// ---------------------------------------------------------------------------

const {
  isValidTraceId,
  isValidSpanId,
  generateTraceId,
  generateSpanId,
  toNanoString,
  buildAttr,
  buildDoubleAttr,
  buildGitHubActionsResourceAttributes,
  buildOTLPSpan,
  buildOTLPBatchPayload,
  buildOTLPBatchPayloads,
  buildOTLPPayload,
  sanitizeOTLPPayload,
  parseOTLPHeaders,
  parseOTLPEndpoints,
  sendOTLPSpan,
  sendOTLPToAllEndpoints,
  sendJobSetupSpan,
  sendJobConclusionSpan,
  readLastRateLimitEntry,
  GITHUB_RATE_LIMITS_JSONL_PATH,
  OTEL_JSONL_PATH,
  appendToOTLPJSONL,
  SPAN_KIND_INTERNAL,
  SPAN_KIND_SERVER,
  buildCurrentWorkflowCallId,
  buildEpisodeAttributesFromContext,
  buildExperimentAttributes,
  buildGraderTelemetry,
  buildEvalTelemetry,
  hasProxyConfigured,
  resolveEngineId,
  parseOTLPCustomAttributes,
  buildCustomOTLPAttributes,
  FAILURE_CATEGORIES_PATH,
} = await import("./send_otlp_span.cjs");

const { readExperimentAssignments, EXPERIMENT_ASSIGNMENTS_PATH } = await import("./experiment_helpers.cjs");

// ---------------------------------------------------------------------------
// isValidTraceId
// ---------------------------------------------------------------------------

describe("isValidTraceId", () => {
  it("accepts a valid 32-character lowercase hex trace ID", () => {
    expect(isValidTraceId("a".repeat(32))).toBe(true);
    expect(isValidTraceId("0123456789abcdef0123456789abcdef")).toBe(true);
  });

  it("rejects uppercase hex characters", () => {
    expect(isValidTraceId("A".repeat(32))).toBe(false);
  });

  it("rejects strings that are too short or too long", () => {
    expect(isValidTraceId("a".repeat(31))).toBe(false);
    expect(isValidTraceId("a".repeat(33))).toBe(false);
  });

  it("rejects empty string", () => {
    expect(isValidTraceId("")).toBe(false);
  });

  it("rejects non-hex characters", () => {
    expect(isValidTraceId("z".repeat(32))).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// isValidSpanId
// ---------------------------------------------------------------------------

describe("isValidSpanId", () => {
  it("accepts a valid 16-character lowercase hex span ID", () => {
    expect(isValidSpanId("b".repeat(16))).toBe(true);
    expect(isValidSpanId("0123456789abcdef")).toBe(true);
  });

  it("rejects uppercase hex characters", () => {
    expect(isValidSpanId("B".repeat(16))).toBe(false);
  });

  it("rejects strings that are too short or too long", () => {
    expect(isValidSpanId("b".repeat(15))).toBe(false);
    expect(isValidSpanId("b".repeat(17))).toBe(false);
  });

  it("rejects empty string", () => {
    expect(isValidSpanId("")).toBe(false);
  });

  it("rejects non-hex characters", () => {
    expect(isValidSpanId("z".repeat(16))).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// generateTraceId
// ---------------------------------------------------------------------------

describe("generateTraceId", () => {
  it("returns a 32-character hex string", () => {
    const id = generateTraceId();
    expect(id).toMatch(/^[0-9a-f]{32}$/);
  });

  it("returns a unique value on each call", () => {
    expect(generateTraceId()).not.toBe(generateTraceId());
  });
});

// ---------------------------------------------------------------------------
// generateSpanId
// ---------------------------------------------------------------------------

describe("generateSpanId", () => {
  it("returns a 16-character hex string", () => {
    const id = generateSpanId();
    expect(id).toMatch(/^[0-9a-f]{16}$/);
  });

  it("returns a unique value on each call", () => {
    expect(generateSpanId()).not.toBe(generateSpanId());
  });
});

// ---------------------------------------------------------------------------
// toNanoString
// ---------------------------------------------------------------------------

describe("toNanoString", () => {
  it("converts milliseconds to nanoseconds string", () => {
    expect(toNanoString(1000)).toBe("1000000000");
  });

  it("handles zero", () => {
    expect(toNanoString(0)).toBe("0");
  });

  it("handles a realistic GitHub Actions timestamp without precision loss", () => {
    const ms = 1700000000000; // 2023-11-14T22:13:20Z
    const nanos = toNanoString(ms);
    expect(nanos).toBe("1700000000000000000");
  });

  it("truncates fractional milliseconds", () => {
    // 1500.9 ms should truncate to 1500
    expect(toNanoString(1500.9)).toBe("1500000000");
  });
});

// ---------------------------------------------------------------------------
// buildAttr
// ---------------------------------------------------------------------------

describe("buildAttr", () => {
  it("returns stringValue for string input", () => {
    expect(buildAttr("k", "v")).toEqual({ key: "k", value: { stringValue: "v" } });
  });

  it("returns intValue for number input", () => {
    expect(buildAttr("k", 42)).toEqual({ key: "k", value: { intValue: 42 } });
  });

  it("returns doubleValue for non-integer numbers", () => {
    expect(buildAttr("k", 1.25)).toEqual({ key: "k", value: { doubleValue: 1.25 } });
  });

  it("returns boolValue for boolean input", () => {
    expect(buildAttr("k", true)).toEqual({ key: "k", value: { boolValue: true } });
    expect(buildAttr("k", false)).toEqual({ key: "k", value: { boolValue: false } });
  });

  it("coerces non-string non-number non-boolean to stringValue", () => {
    // @ts-expect-error intentional type violation for coverage
    expect(buildAttr("k", null).value).toHaveProperty("stringValue");
  });
});

describe("buildDoubleAttr", () => {
  it("returns doubleValue for integer 0", () => {
    expect(buildDoubleAttr("k", 0)).toEqual({ key: "k", value: { doubleValue: 0 } });
  });

  it("returns doubleValue for positive float", () => {
    expect(buildDoubleAttr("k", 1.25)).toEqual({ key: "k", value: { doubleValue: 1.25 } });
  });

  it("returns doubleValue for positive integer", () => {
    expect(buildDoubleAttr("k", 42)).toEqual({ key: "k", value: { doubleValue: 42 } });
  });

  it("falls back to doubleValue 0 for non-finite values", () => {
    expect(buildDoubleAttr("k", Infinity).value).toEqual({ doubleValue: 0 });
    expect(buildDoubleAttr("k", NaN).value).toEqual({ doubleValue: 0 });
  });
});

describe("buildCurrentWorkflowCallId", () => {
  it("includes workflow ref so reusable workflow invocations in one run stay distinct", () => {
    expect(buildCurrentWorkflowCallId("12345", "2", "owner/repo/.github/workflows/test.yml@refs/heads/main")).toBe("12345-2:owner/repo/.github/workflows/test.yml@refs/heads/main");
  });

  it("defaults the attempt to 1 when omitted", () => {
    expect(buildCurrentWorkflowCallId("12345", "", "owner/repo/.github/workflows/test.yml@refs/heads/main")).toBe("12345-1:owner/repo/.github/workflows/test.yml@refs/heads/main");
  });

  it("returns empty string when run id is unavailable", () => {
    expect(buildCurrentWorkflowCallId("", "2")).toBe("");
  });
});

describe("buildEpisodeAttributesFromContext", () => {
  it("prefers canonical lineage fields and keeps workflow_call aliases for compatibility", () => {
    process.env.GITHUB_WORKFLOW_REF = "owner/repo/.github/workflows/child.yml@refs/heads/main";
    const attrs = buildEpisodeAttributesFromContext(
      {
        context: {
          episode_id: "episode-42",
          hop_id: "200-3:owner/repo/.github/workflows/parent.yml@refs/heads/main",
          parent_hop_id: "199-1:owner/repo/.github/workflows/root.yml@refs/heads/main",
          origin_event: "workflow_run",
          root_repo: "owner/repo",
          root_workflow_id: "owner/repo/.github/workflows/root.yml@refs/heads/main",
          workflow_call_id: "200-3:owner/repo/.github/workflows/parent.yml@refs/heads/main",
        },
      },
      "200",
      "3"
    );
    expect(attrs).toContainEqual({ key: "gh-aw.episode.id", value: { stringValue: "episode-42" } });
    expect(attrs).toContainEqual({ key: "gh-aw.episode.kind", value: { stringValue: "workflow_call" } });
    expect(attrs).toContainEqual({ key: "gh-aw.hop.id", value: { stringValue: "200-3:owner/repo/.github/workflows/child.yml@refs/heads/main" } });
    expect(attrs).toContainEqual({ key: "gh-aw.hop.parent_id", value: { stringValue: "199-1:owner/repo/.github/workflows/root.yml@refs/heads/main" } });
    expect(attrs).toContainEqual({ key: "gh-aw.origin.event", value: { stringValue: "workflow_run" } });
    expect(attrs).toContainEqual({ key: "gh-aw.root.repo", value: { stringValue: "owner/repo" } });
    expect(attrs).toContainEqual({ key: "gh-aw.root.workflow_id", value: { stringValue: "owner/repo/.github/workflows/root.yml@refs/heads/main" } });
    expect(attrs).toContainEqual({ key: "gh-aw.workflow_call.id", value: { stringValue: "200-3:owner/repo/.github/workflows/child.yml@refs/heads/main" } });
    expect(attrs).toContainEqual({ key: "gh-aw.workflow_call.parent_id", value: { stringValue: "199-1:owner/repo/.github/workflows/root.yml@refs/heads/main" } });
  });

  it("falls back to legacy workflow_call_id when canonical lineage fields are absent", () => {
    process.env.GITHUB_WORKFLOW_REF = "owner/repo/.github/workflows/child.yml@refs/heads/main";
    const attrs = buildEpisodeAttributesFromContext({ context: { workflow_call_id: "200-3:owner/repo/.github/workflows/parent.yml@refs/heads/main" } }, "200", "3");
    expect(attrs).toContainEqual({ key: "gh-aw.episode.id", value: { stringValue: "200-3:owner/repo/.github/workflows/parent.yml@refs/heads/main" } });
    expect(attrs).toContainEqual({ key: "gh-aw.hop.id", value: { stringValue: "200-3:owner/repo/.github/workflows/child.yml@refs/heads/main" } });
    expect(attrs).toContainEqual({ key: "gh-aw.hop.parent_id", value: { stringValue: "200-3:owner/repo/.github/workflows/parent.yml@refs/heads/main" } });
  });

  it("falls back to the current run when no inherited lineage exists", () => {
    process.env.GITHUB_WORKFLOW_REF = "owner/repo/.github/workflows/root.yml@refs/heads/main";
    const attrs = buildEpisodeAttributesFromContext({}, "300", "4");
    expect(attrs).toContainEqual({ key: "gh-aw.episode.id", value: { stringValue: "300-4:owner/repo/.github/workflows/root.yml@refs/heads/main" } });
    expect(attrs).toContainEqual({ key: "gh-aw.episode.kind", value: { stringValue: "run" } });
    expect(attrs).toContainEqual({ key: "gh-aw.hop.id", value: { stringValue: "300-4:owner/repo/.github/workflows/root.yml@refs/heads/main" } });
    expect(attrs).toContainEqual({ key: "gh-aw.workflow_call.id", value: { stringValue: "300-4:owner/repo/.github/workflows/root.yml@refs/heads/main" } });
    const keys = attrs.map(attr => attr.key);
    expect(keys).not.toContain("gh-aw.workflow_call.parent_id");
  });
});

// ---------------------------------------------------------------------------
// buildOTLPPayload
// ---------------------------------------------------------------------------

describe("buildOTLPPayload", () => {
  it("produces a valid OTLP resourceSpans structure", () => {
    const traceId = "a".repeat(32);
    const spanId = "b".repeat(16);
    const payload = buildOTLPPayload({
      traceId,
      spanId,
      spanName: "gh-aw.job.setup",
      startMs: 1000,
      endMs: 2000,
      serviceName: "gh-aw",
      scopeVersion: "v1.2.3",
      attributes: [buildAttr("foo", "bar")],
    });

    expect(payload.resourceSpans).toHaveLength(1);
    const rs = payload.resourceSpans[0];

    // Resource
    expect(rs.resource.attributes).toContainEqual({ key: "service.name", value: { stringValue: "gh-aw" } });
    expect(rs.resource.attributes).toContainEqual({ key: "service.version", value: { stringValue: "v1.2.3" } });

    // Scope — name is always "gh-aw"; version comes from scopeVersion
    expect(rs.scopeSpans).toHaveLength(1);
    expect(rs.scopeSpans[0].scope.name).toBe("gh-aw");
    expect(rs.scopeSpans[0].scope.version).toBe("v1.2.3");

    // Span
    const span = rs.scopeSpans[0].spans[0];
    expect(span.traceId).toBe(traceId);
    expect(span.spanId).toBe(spanId);
    expect(span.name).toBe("gh-aw.job.setup");
    expect(span.kind).toBe(SPAN_KIND_INTERNAL);
    expect(span.startTimeUnixNano).toBe(toNanoString(1000));
    expect(span.endTimeUnixNano).toBe(toNanoString(2000));
    expect(span.status.code).toBe(1);
    expect(span.attributes).toContainEqual({ key: "foo", value: { stringValue: "bar" } });
  });

  it("uses 'unknown' as scope version when scopeVersion is omitted", () => {
    const payload = buildOTLPPayload({
      traceId: "a".repeat(32),
      spanId: "b".repeat(16),
      spanName: "test",
      startMs: 0,
      endMs: 1,
      serviceName: "gh-aw",
      attributes: [],
    });
    expect(payload.resourceSpans[0].scopeSpans[0].scope.version).toBe("unknown");
  });

  it("omits service.version from resource attributes when scopeVersion is 'unknown'", () => {
    const payload = buildOTLPPayload({
      traceId: "a".repeat(32),
      spanId: "b".repeat(16),
      spanName: "test",
      startMs: 0,
      endMs: 1,
      serviceName: "gh-aw",
      scopeVersion: "unknown",
      attributes: [],
    });
    const resourceKeys = payload.resourceSpans[0].resource.attributes.map(a => a.key);
    expect(resourceKeys).not.toContain("service.version");
  });

  it("omits service.version from resource attributes when scopeVersion is omitted", () => {
    const payload = buildOTLPPayload({
      traceId: "a".repeat(32),
      spanId: "b".repeat(16),
      spanName: "test",
      startMs: 0,
      endMs: 1,
      serviceName: "gh-aw",
      attributes: [],
    });
    const resourceKeys = payload.resourceSpans[0].resource.attributes.map(a => a.key);
    expect(resourceKeys).not.toContain("service.version");
  });

  it("merges caller-supplied resourceAttributes into the resource block", () => {
    const payload = buildOTLPPayload({
      traceId: "a".repeat(32),
      spanId: "b".repeat(16),
      spanName: "test",
      startMs: 0,
      endMs: 1,
      serviceName: "gh-aw",
      scopeVersion: "v1.0.0",
      attributes: [],
      resourceAttributes: [buildAttr("github.repository", "owner/repo"), buildAttr("github.run_id", "123")],
    });
    const rs = payload.resourceSpans[0];
    expect(rs.resource.attributes).toContainEqual({ key: "github.repository", value: { stringValue: "owner/repo" } });
    expect(rs.resource.attributes).toContainEqual({ key: "github.run_id", value: { stringValue: "123" } });
    expect(rs.resource.attributes).toContainEqual({ key: "service.version", value: { stringValue: "v1.0.0" } });
  });

  it("includes parentSpanId in span when provided", () => {
    const payload = buildOTLPPayload({
      traceId: "a".repeat(32),
      spanId: "b".repeat(16),
      parentSpanId: "c".repeat(16),
      spanName: "test",
      startMs: 0,
      endMs: 1,
      serviceName: "gh-aw",
      attributes: [],
    });
    const span = payload.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.parentSpanId).toBe("c".repeat(16));
  });

  it("omits parentSpanId from span when not provided", () => {
    const payload = buildOTLPPayload({
      traceId: "a".repeat(32),
      spanId: "b".repeat(16),
      spanName: "test",
      startMs: 0,
      endMs: 1,
      serviceName: "gh-aw",
      attributes: [],
    });
    const span = payload.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.parentSpanId).toBeUndefined();
  });

  it("uses SPAN_KIND_INTERNAL (1) by default when kind is not specified", () => {
    const payload = buildOTLPPayload({
      traceId: "a".repeat(32),
      spanId: "b".repeat(16),
      spanName: "test",
      startMs: 0,
      endMs: 1,
      serviceName: "gh-aw",
      attributes: [],
    });
    const span = payload.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.kind).toBe(SPAN_KIND_INTERNAL);
  });

  it("uses the caller-supplied kind when specified (e.g. SPAN_KIND_SERVER)", () => {
    const payload = buildOTLPPayload({
      traceId: "a".repeat(32),
      spanId: "b".repeat(16),
      spanName: "test",
      startMs: 0,
      endMs: 1,
      serviceName: "gh-aw",
      attributes: [],
      kind: SPAN_KIND_SERVER,
    });
    const span = payload.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.kind).toBe(SPAN_KIND_SERVER);
  });

  it("includes events array in span when events are provided", () => {
    const events = [
      {
        timeUnixNano: toNanoString(1000),
        name: "exception",
        attributes: [buildAttr("exception.message", "something failed")],
      },
    ];
    const payload = buildOTLPPayload({
      traceId: "a".repeat(32),
      spanId: "b".repeat(16),
      spanName: "test",
      startMs: 0,
      endMs: 1,
      serviceName: "gh-aw",
      attributes: [],
      events,
    });
    const span = payload.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.events).toHaveLength(1);
    expect(span.events[0].name).toBe("exception");
    expect(span.events[0].attributes).toContainEqual({ key: "exception.message", value: { stringValue: "something failed" } });
  });

  it("includes multiple events when provided", () => {
    const events = [
      { timeUnixNano: toNanoString(1000), name: "exception", attributes: [buildAttr("exception.message", "error A")] },
      { timeUnixNano: toNanoString(1000), name: "exception", attributes: [buildAttr("exception.message", "error B")] },
    ];
    const payload = buildOTLPPayload({
      traceId: "a".repeat(32),
      spanId: "b".repeat(16),
      spanName: "test",
      startMs: 0,
      endMs: 1,
      serviceName: "gh-aw",
      attributes: [],
      events,
    });
    const span = payload.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.events).toHaveLength(2);
    expect(span.events[0].attributes[0].value.stringValue).toBe("error A");
    expect(span.events[1].attributes[0].value.stringValue).toBe("error B");
  });

  it("omits events from span when events array is empty", () => {
    const payload = buildOTLPPayload({
      traceId: "a".repeat(32),
      spanId: "b".repeat(16),
      spanName: "test",
      startMs: 0,
      endMs: 1,
      serviceName: "gh-aw",
      attributes: [],
      events: [],
    });
    const span = payload.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.events).toBeUndefined();
  });

  it("omits events from span when events is not provided", () => {
    const payload = buildOTLPPayload({
      traceId: "a".repeat(32),
      spanId: "b".repeat(16),
      spanName: "test",
      startMs: 0,
      endMs: 1,
      serviceName: "gh-aw",
      attributes: [],
    });
    const span = payload.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.events).toBeUndefined();
  });
});

describe("buildOTLPBatchPayload", () => {
  it("wraps multiple spans in a single OTLP payload", () => {
    const spans = [
      buildOTLPSpan({
        traceId: "a".repeat(32),
        spanId: "b".repeat(16),
        spanName: "span.one",
        startMs: 1000,
        endMs: 1001,
        attributes: [buildAttr("k1", "v1")],
      }),
      buildOTLPSpan({
        traceId: "a".repeat(32),
        spanId: "c".repeat(16),
        parentSpanId: "b".repeat(16),
        spanName: "span.two",
        startMs: 1002,
        endMs: 1003,
        attributes: [buildAttr("k2", "v2")],
      }),
    ];

    const payload = buildOTLPBatchPayload({
      serviceName: "gh-aw-batch",
      scopeVersion: "v1.0.0",
      resourceAttributes: [buildAttr("github.repository", "owner/repo")],
      spans,
    });

    expect(payload.resourceSpans).toHaveLength(1);
    expect(payload.resourceSpans[0].scopeSpans).toHaveLength(1);
    expect(payload.resourceSpans[0].scopeSpans[0].spans).toHaveLength(2);
    expect(payload.resourceSpans[0].scopeSpans[0].spans[1].parentSpanId).toBe("b".repeat(16));
    expect(payload.resourceSpans[0].resource.attributes).toContainEqual({
      key: "github.repository",
      value: { stringValue: "owner/repo" },
    });
  });
});

describe("buildOTLPBatchPayloads", () => {
  it("chunks spans into multiple payloads when maxSpansPerPayload is exceeded", () => {
    const spans = Array.from({ length: 5 }, (_, index) =>
      buildOTLPSpan({
        traceId: "d".repeat(32),
        spanId: `${index + 1}`.padStart(16, "0"),
        spanName: `span.${index + 1}`,
        startMs: 1000 + index,
        endMs: 1001 + index,
        attributes: [],
      })
    );

    const payloads = buildOTLPBatchPayloads({
      serviceName: "gh-aw-batch",
      spans,
      maxSpansPerPayload: 2,
    });

    expect(payloads).toHaveLength(3);
    expect(payloads[0].resourceSpans[0].scopeSpans[0].spans).toHaveLength(2);
    expect(payloads[1].resourceSpans[0].scopeSpans[0].spans).toHaveLength(2);
    expect(payloads[2].resourceSpans[0].scopeSpans[0].spans).toHaveLength(1);
  });
});

// ---------------------------------------------------------------------------
// sanitizeOTLPPayload
// ---------------------------------------------------------------------------

describe("sanitizeOTLPPayload", () => {
  /** Build a minimal OTLP payload with the given span and resource attributes. */
  function makePayload(spanAttrs = [], resourceAttrs = []) {
    return buildOTLPPayload({
      traceId: "a".repeat(32),
      spanId: "b".repeat(16),
      spanName: "test",
      startMs: 0,
      endMs: 1,
      serviceName: "gh-aw",
      attributes: spanAttrs,
      resourceAttributes: resourceAttrs,
    });
  }

  it("redacts span attribute values whose keys match sensitive patterns", () => {
    const payload = makePayload([buildAttr("gh.auth_token", "super-secret"), buildAttr("safe.label", "value")]);
    const sanitized = sanitizeOTLPPayload(payload);
    const attrs = sanitized.resourceSpans[0].scopeSpans[0].spans[0].attributes;
    const tokenAttr = attrs.find(a => a.key === "gh.auth_token");
    expect(tokenAttr.value.stringValue, "sensitive attribute should be redacted").toBe("[REDACTED]");
    const safeAttr = attrs.find(a => a.key === "safe.label");
    expect(safeAttr.value.stringValue, "non-sensitive attribute should be unchanged").toBe("value");
  });

  it("redacts span attributes matching all sensitive key patterns", () => {
    const sensitiveKeys = ["token", "secret", "password", "passwd", "key", "api_key", "auth", "credential", "access_key"];
    const attrs = sensitiveKeys.map(k => buildAttr(k, "should-be-redacted"));
    const payload = makePayload(attrs);
    const sanitized = sanitizeOTLPPayload(payload);
    const spanAttrs = sanitized.resourceSpans[0].scopeSpans[0].spans[0].attributes;
    for (const k of sensitiveKeys) {
      const attr = spanAttrs.find(a => a.key === k);
      expect(attr.value.stringValue, `${k} should be redacted`).toBe("[REDACTED]");
    }
  });

  it("does not redact non-sensitive compound keys containing 'key' after underscore", () => {
    const nonSensitiveKeys = ["sort_key", "cache_key", "primary_key", "partition_key"];
    const attrs = nonSensitiveKeys.map(k => buildAttr(k, "safe-value"));
    const payload = makePayload(attrs);
    const sanitized = sanitizeOTLPPayload(payload);
    const spanAttrs = sanitized.resourceSpans[0].scopeSpans[0].spans[0].attributes;
    for (const k of nonSensitiveKeys) {
      const attr = spanAttrs.find(a => a.key === k);
      expect(attr.value.stringValue, `${k} should not be redacted`).toBe("safe-value");
    }
  });

  it("redacts resource attribute values whose keys match sensitive patterns", () => {
    const payload = makePayload([], [buildAttr("db.password", "hunter2"), buildAttr("service.name", "gh-aw")]);
    const sanitized = sanitizeOTLPPayload(payload);
    const resourceAttrs = sanitized.resourceSpans[0].resource.attributes;
    const pwAttr = resourceAttrs.find(a => a.key === "db.password");
    expect(pwAttr.value.stringValue, "sensitive resource attribute should be redacted").toBe("[REDACTED]");
    const svcAttr = resourceAttrs.find(a => a.key === "service.name");
    expect(svcAttr.value.stringValue, "service.name should be unchanged").toBe("gh-aw");
  });

  it("truncates string values exceeding 1024 characters", () => {
    const longValue = "x".repeat(2000);
    const payload = makePayload([buildAttr("large.output", longValue)]);
    const sanitized = sanitizeOTLPPayload(payload);
    const attr = sanitized.resourceSpans[0].scopeSpans[0].spans[0].attributes.find(a => a.key === "large.output");
    expect(attr.value.stringValue, "value should be truncated to 1024 chars").toBe(longValue.slice(0, 1024));
  });

  it("does not redact non-string sensitive attribute values (e.g. intValue, boolValue)", () => {
    const intAttr = { key: "auth_count", value: { intValue: 42 } };
    const boolAttr = { key: "token_valid", value: { boolValue: true } };
    const payload = makePayload([intAttr, boolAttr]);
    const sanitized = sanitizeOTLPPayload(payload);
    const spanAttrs = sanitized.resourceSpans[0].scopeSpans[0].spans[0].attributes;
    const sanitizedInt = spanAttrs.find(a => a.key === "auth_count");
    expect(sanitizedInt.value.intValue, "intValue sensitive attribute should not be redacted").toBe(42);
    const sanitizedBool = spanAttrs.find(a => a.key === "token_valid");
    expect(sanitizedBool.value.boolValue, "boolValue sensitive attribute should not be redacted").toBe(true);
  });

  it("does not mutate the original payload", () => {
    const payload = makePayload([buildAttr("auth_token", "secret-value")]);
    const originalAttr = payload.resourceSpans[0].scopeSpans[0].spans[0].attributes[0];
    sanitizeOTLPPayload(payload);
    expect(originalAttr.value.stringValue, "original payload should not be mutated").toBe("secret-value");
  });

  it("returns the payload unchanged when resourceSpans is absent", () => {
    const payload = { custom: "data" };
    expect(sanitizeOTLPPayload(payload), "payload without resourceSpans should be returned as-is").toBe(payload);
  });

  it("redacts sensitive keys in span event attributes", () => {
    const payload = makePayload([]);
    // Manually add events with sensitive attributes to the span
    const span = payload.resourceSpans[0].scopeSpans[0].spans[0];
    span.events = [
      {
        timeUnixNano: "1000000000",
        name: "exception",
        attributes: [buildAttr("exception.message", "safe message"), buildAttr("auth_token", "super-secret-token")],
      },
    ];
    const sanitized = sanitizeOTLPPayload(payload);
    const events = sanitized.resourceSpans[0].scopeSpans[0].spans[0].events;
    expect(events).toHaveLength(1);
    const msgAttr = events[0].attributes.find(a => a.key === "exception.message");
    expect(msgAttr.value.stringValue, "non-sensitive event attribute should be unchanged").toBe("safe message");
    const tokenAttr = events[0].attributes.find(a => a.key === "auth_token");
    expect(tokenAttr.value.stringValue, "sensitive event attribute should be redacted").toBe("[REDACTED]");
  });

  it("truncates long string values in span event attributes", () => {
    const payload = makePayload([]);
    const span = payload.resourceSpans[0].scopeSpans[0].spans[0];
    const longValue = "y".repeat(2000);
    span.events = [
      {
        timeUnixNano: "1000000000",
        name: "exception",
        attributes: [buildAttr("exception.message", longValue)],
      },
    ];
    const sanitized = sanitizeOTLPPayload(payload);
    const events = sanitized.resourceSpans[0].scopeSpans[0].spans[0].events;
    const msgAttr = events[0].attributes.find(a => a.key === "exception.message");
    expect(msgAttr.value.stringValue.length, "long event attribute should be truncated").toBe(1024);
  });

  it("preserves span events without attributes unchanged", () => {
    const payload = makePayload([]);
    const span = payload.resourceSpans[0].scopeSpans[0].spans[0];
    span.events = [{ timeUnixNano: "1000000000", name: "custom-event" }];
    const sanitized = sanitizeOTLPPayload(payload);
    const events = sanitized.resourceSpans[0].scopeSpans[0].spans[0].events;
    expect(events).toHaveLength(1);
    expect(events[0].name).toBe("custom-event");
  });
});

// ---------------------------------------------------------------------------
// sendOTLPSpan
// ---------------------------------------------------------------------------

describe("sendOTLPSpan", () => {
  let mkdirSpy, appendSpy, spawnSyncSpy;

  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
    mkdirSpy = vi.spyOn(fs, "mkdirSync").mockImplementation(() => {});
    appendSpy = vi.spyOn(fs, "appendFileSync").mockImplementation(() => {});
    spawnSyncSpy = vi.spyOn(childProcess, "spawnSync");
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    mkdirSpy.mockRestore();
    appendSpy.mockRestore();
    spawnSyncSpy.mockRestore();
    delete process.env.HTTPS_PROXY;
    delete process.env.https_proxy;
    delete process.env.HTTP_PROXY;
    delete process.env.http_proxy;
    delete process.env.ALL_PROXY;
    delete process.env.all_proxy;
  });

  it("POSTs JSON payload to endpoint/v1/traces", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    const payload = { resourceSpans: [] };
    await sendOTLPSpan("https://traces.example.com:4317", payload);

    expect(mockFetch).toHaveBeenCalledOnce();
    const [url, init] = mockFetch.mock.calls[0];
    expect(url).toBe("https://traces.example.com:4317/v1/traces");
    expect(init.method).toBe("POST");
    expect(init.headers["Content-Type"]).toBe("application/json");
    expect(JSON.parse(init.body)).toEqual(payload);
  });

  it("strips trailing slash from endpoint before appending /v1/traces", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    await sendOTLPSpan("https://traces.example.com/", {});
    const [url] = mockFetch.mock.calls[0];
    expect(url).toBe("https://traces.example.com/v1/traces");
  });

  it("uses curl when a proxy is configured", async () => {
    process.env.HTTPS_PROXY = "http://proxy.internal:3128";
    spawnSyncSpy.mockReturnValue({ error: undefined, status: 0, stdout: "200", stderr: "" });

    await sendOTLPSpan("https://traces.example.com", { resourceSpans: [] });

    expect(fetch).not.toHaveBeenCalled();
    expect(spawnSyncSpy).toHaveBeenCalledOnce();
    const [command, args, options] = spawnSyncSpy.mock.calls[0];
    expect(command).toBe("curl");
    expect(args).toContain("https://traces.example.com/v1/traces");
    expect(options.input).toBe(JSON.stringify({ resourceSpans: [] }));
  });

  it("warns (does not throw) when server returns non-2xx status on all retries", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: false, status: 400, statusText: "Bad Request" });
    vi.stubGlobal("fetch", mockFetch);
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    const writeSpy = vi.spyOn(fs, "writeFileSync").mockImplementation(() => {});

    // Should not throw
    await expect(sendOTLPSpan("https://traces.example.com", {}, { maxRetries: 1, baseDelayMs: 1 })).resolves.toBeUndefined();

    // Two attempts (1 initial + 1 retry)
    expect(mockFetch).toHaveBeenCalledTimes(2);
    expect(warnSpy).toHaveBeenCalledTimes(2);
    expect(warnSpy.mock.calls[0][0]).toContain("attempt 1/2 failed");
    expect(warnSpy.mock.calls[1][0]).toContain("failed after 2 attempts");
    expect(writeSpy).toHaveBeenCalled();

    writeSpy.mockRestore();
    warnSpy.mockRestore();
  });

  it("records OTLP export failure host, status, and reason details", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: false, status: 401, statusText: "Unauthorized" });
    vi.stubGlobal("fetch", mockFetch);
    const appendSpy = vi.spyOn(fs, "appendFileSync").mockImplementation(() => {});

    await expect(sendOTLPSpan("https://collector.example.com:4318", {}, { maxRetries: 0, skipJSONL: true })).resolves.toBeUndefined();

    expect(appendSpy).toHaveBeenCalledWith("/tmp/gh-aw/otlp-export-errors.jsonl", `${JSON.stringify({ host: "collector.example.com:4318", status: 401, reason: "Unauthorized" })}\n`);

    appendSpy.mockRestore();
  });

  it("falls back to HTTP status when curl reports non-2xx with statusText OK", async () => {
    process.env.HTTPS_PROXY = "http://proxy.internal:3128";
    spawnSyncSpy.mockReturnValue({ error: undefined, status: 0, stdout: "401", stderr: "" });
    const appendSpy = vi.spyOn(fs, "appendFileSync").mockImplementation(() => {});

    await expect(sendOTLPSpan("https://collector.example.com:4318", {}, { maxRetries: 0, skipJSONL: true })).resolves.toBeUndefined();

    expect(appendSpy).toHaveBeenCalledWith("/tmp/gh-aw/otlp-export-errors.jsonl", `${JSON.stringify({ host: "collector.example.com:4318", status: 401, reason: "HTTP 401" })}\n`);

    appendSpy.mockRestore();
  });

  it("sanitizes OTLP export failure reasons before persisting them", async () => {
    const mockFetch = vi.fn().mockRejectedValue(new Error("Failed to parse URL from https://user:secret@collector.example.com/v1/traces?token=abc"));
    vi.stubGlobal("fetch", mockFetch);
    const appendSpy = vi.spyOn(fs, "appendFileSync").mockImplementation(() => {});

    await expect(sendOTLPSpan("https://user:secret@collector.example.com:4318?token=abc", {}, { maxRetries: 0, skipJSONL: true })).resolves.toBeUndefined();

    expect(appendSpy).toHaveBeenCalledWith("/tmp/gh-aw/otlp-export-errors.jsonl", `${JSON.stringify({ host: "collector.example.com:4318", reason: "Failed to parse URL from [REDACTED]" })}\n`);

    appendSpy.mockRestore();
  });

  it("retries on failure and succeeds on second attempt", async () => {
    const mockFetch = vi.fn().mockResolvedValueOnce({ ok: false, status: 503, statusText: "Service Unavailable" }).mockResolvedValueOnce({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});

    await sendOTLPSpan("https://traces.example.com", {}, { maxRetries: 2, baseDelayMs: 1 });

    expect(mockFetch).toHaveBeenCalledTimes(2);
    expect(warnSpy).toHaveBeenCalledTimes(1);
    expect(warnSpy.mock.calls[0][0]).toContain("attempt 1/3 failed");

    warnSpy.mockRestore();
  });

  it("warns (does not throw) when fetch rejects on all retries", async () => {
    const mockFetch = vi.fn().mockRejectedValue(new Error("network error"));
    vi.stubGlobal("fetch", mockFetch);
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    const writeSpy = vi.spyOn(fs, "writeFileSync").mockImplementation(() => {});

    await expect(sendOTLPSpan("https://traces.example.com", {}, { maxRetries: 1, baseDelayMs: 1 })).resolves.toBeUndefined();

    expect(mockFetch).toHaveBeenCalledTimes(2);
    expect(warnSpy.mock.calls[1][0]).toContain("error after 2 attempts");
    expect(writeSpy).toHaveBeenCalled();
    expect(writeSpy.mock.calls[0][0]).toBe("/tmp/gh-aw/otlp-export-errors.count");

    writeSpy.mockRestore();
    warnSpy.mockRestore();
  });
});

describe("hasProxyConfigured", () => {
  afterEach(() => {
    delete process.env.HTTPS_PROXY;
    delete process.env.https_proxy;
    delete process.env.HTTP_PROXY;
    delete process.env.http_proxy;
    delete process.env.ALL_PROXY;
    delete process.env.all_proxy;
  });

  it("returns false when no proxy environment is set", () => {
    expect(hasProxyConfigured("https://traces.example.com")).toBe(false);
  });

  it("detects HTTPS proxy settings for HTTPS endpoints", () => {
    process.env.HTTPS_PROXY = "http://proxy.internal:3128";
    expect(hasProxyConfigured("https://traces.example.com")).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// readLastRateLimitEntry
// ---------------------------------------------------------------------------

describe("readLastRateLimitEntry", () => {
  let readFileSpy;

  beforeEach(() => {
    readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });
  });

  afterEach(() => {
    readFileSpy.mockRestore();
  });

  it("returns null when the file does not exist", () => {
    expect(readLastRateLimitEntry()).toBeNull();
  });

  it("returns null when the file is empty", () => {
    readFileSpy.mockImplementation(filePath => {
      if (filePath === GITHUB_RATE_LIMITS_JSONL_PATH) return "";
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });
    expect(readLastRateLimitEntry()).toBeNull();
  });

  it("returns null when the file contains only blank lines", () => {
    readFileSpy.mockImplementation(filePath => {
      if (filePath === GITHUB_RATE_LIMITS_JSONL_PATH) return "\n\n  \n";
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });
    expect(readLastRateLimitEntry()).toBeNull();
  });

  it("returns the parsed entry for a single-line file", () => {
    const entry = { resource: "core", limit: 5000, remaining: 4823, used: 177 };
    readFileSpy.mockImplementation(filePath => {
      if (filePath === GITHUB_RATE_LIMITS_JSONL_PATH) return JSON.stringify(entry) + "\n";
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });
    expect(readLastRateLimitEntry()).toEqual(entry);
  });

  it("returns the last entry for a multi-line file", () => {
    const first = { resource: "core", remaining: 4900 };
    const last = { resource: "core", remaining: 4500 };
    readFileSpy.mockImplementation(filePath => {
      if (filePath === GITHUB_RATE_LIMITS_JSONL_PATH) {
        return JSON.stringify(first) + "\n" + JSON.stringify(last) + "\n";
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });
    expect(readLastRateLimitEntry()).toEqual(last);
  });

  it("returns null when the last line is invalid JSON", () => {
    readFileSpy.mockImplementation(filePath => {
      if (filePath === GITHUB_RATE_LIMITS_JSONL_PATH) return "not valid json\n";
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });
    expect(readLastRateLimitEntry()).toBeNull();
  });

  it("returns the last valid entry when the final line is malformed (truncated write)", () => {
    const valid = { resource: "core", remaining: 4999 };
    readFileSpy.mockImplementation(filePath => {
      if (filePath === GITHUB_RATE_LIMITS_JSONL_PATH) {
        return JSON.stringify(valid) + "\ntruncated-";
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });
    // parseJsonlContent drops the malformed trailing line; the last valid entry is returned.
    expect(readLastRateLimitEntry()).toEqual(valid);
  });
});

// ---------------------------------------------------------------------------
// appendToOTLPJSONL
// ---------------------------------------------------------------------------

describe("appendToOTLPJSONL", () => {
  let mkdirSpy, appendSpy;

  beforeEach(() => {
    mkdirSpy = vi.spyOn(fs, "mkdirSync").mockImplementation(() => {});
    appendSpy = vi.spyOn(fs, "appendFileSync").mockImplementation(() => {});
  });

  afterEach(() => {
    mkdirSpy.mockRestore();
    appendSpy.mockRestore();
  });

  it("writes payload as a JSON line to OTEL_JSONL_PATH", () => {
    const payload = { resourceSpans: [{ spans: [] }] };
    appendToOTLPJSONL(payload);

    expect(appendSpy).toHaveBeenCalledOnce();
    const [filePath, content] = appendSpy.mock.calls[0];
    expect(filePath).toBe(OTEL_JSONL_PATH);
    expect(content).toBe(JSON.stringify(payload) + "\n");
  });

  it("ensures /tmp/gh-aw directory exists before writing", () => {
    appendToOTLPJSONL({});

    expect(mkdirSpy).toHaveBeenCalledWith("/tmp/gh-aw", { recursive: true });
  });

  it("does not throw when appendFileSync fails", () => {
    appendSpy.mockImplementation(() => {
      throw new Error("disk full");
    });

    expect(() => appendToOTLPJSONL({ spans: [] })).not.toThrow();
  });
});

// ---------------------------------------------------------------------------
// sendOTLPSpan – JSONL mirror
// ---------------------------------------------------------------------------

describe("sendOTLPSpan JSONL mirror", () => {
  let mkdirSpy, appendSpy;

  beforeEach(() => {
    mkdirSpy = vi.spyOn(fs, "mkdirSync").mockImplementation(() => {});
    appendSpy = vi.spyOn(fs, "appendFileSync").mockImplementation(() => {});
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" }));
  });

  afterEach(() => {
    mkdirSpy.mockRestore();
    appendSpy.mockRestore();
    vi.unstubAllGlobals();
  });

  it("mirrors the payload to otel.jsonl even when fetch succeeds", async () => {
    const payload = { resourceSpans: [] };
    await sendOTLPSpan("https://traces.example.com", payload);

    expect(appendSpy).toHaveBeenCalledOnce();
    const [filePath, content] = appendSpy.mock.calls[0];
    expect(filePath).toBe(OTEL_JSONL_PATH);
    expect(content).toBe(JSON.stringify(payload) + "\n");
  });

  it("mirrors the payload to otel.jsonl even when fetch fails all retries", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, status: 503, statusText: "Unavailable" }));
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});

    const payload = { resourceSpans: [{ note: "retry-test" }] };
    await sendOTLPSpan("https://traces.example.com", payload, { maxRetries: 1, baseDelayMs: 1 });

    const otelCall = appendSpy.mock.calls.find(([filePath]) => filePath === OTEL_JSONL_PATH);
    expect(otelCall).toBeTruthy();
    expect(otelCall[1]).toBe(JSON.stringify(payload) + "\n");

    warnSpy.mockRestore();
  });

  it("skips JSONL mirror when skipJSONL is true", async () => {
    const payload = { resourceSpans: [{ note: "skip-test" }] };
    await sendOTLPSpan("https://traces.example.com", payload, { skipJSONL: true });

    expect(appendSpy).not.toHaveBeenCalled();
    expect(fetch).toHaveBeenCalledOnce();
  });
});

// ---------------------------------------------------------------------------
// parseOTLPHeaders
// ---------------------------------------------------------------------------

describe("parseOTLPHeaders", () => {
  it("returns empty object for empty/null/whitespace input", () => {
    expect(parseOTLPHeaders("")).toEqual({});
    expect(parseOTLPHeaders("   ")).toEqual({});
  });

  it("parses a single key=value pair", () => {
    expect(parseOTLPHeaders("Authorization=Bearer mytoken")).toEqual({ Authorization: "Bearer mytoken" });
  });

  it("parses multiple comma-separated key=value pairs", () => {
    expect(parseOTLPHeaders("X-Tenant=acme,X-Region=us-east-1")).toEqual({
      "X-Tenant": "acme",
      "X-Region": "us-east-1",
    });
  });

  it("handles percent-encoded values", () => {
    expect(parseOTLPHeaders("Authorization=Bearer%20tok%3Dvalue")).toEqual({ Authorization: "Bearer tok=value" });
  });

  it("decodes before trimming so encoded whitespace at edges is preserved", () => {
    // %20 at start/end of value should survive: decode first, then trim removes nothing
    expect(parseOTLPHeaders("X-Token=abc%20def")).toEqual({ "X-Token": "abc def" });
  });

  it("handles values containing = signs (only first = is delimiter)", () => {
    expect(parseOTLPHeaders("Authorization=Bearer base64==")).toEqual({ Authorization: "Bearer base64==" });
  });

  it("parses Sentry OTLP header format (value contains space and embedded = sign)", () => {
    // Sentry's OTLP auth header: x-sentry-auth: Sentry sentry_key=<key>
    // The value "Sentry sentry_key=abc123" contains both a space and an embedded =.
    expect(parseOTLPHeaders("x-sentry-auth=Sentry sentry_key=abc123def456")).toEqual({
      "x-sentry-auth": "Sentry sentry_key=abc123def456",
    });
  });

  it("parses Sentry header combined with another header", () => {
    expect(parseOTLPHeaders("x-sentry-auth=Sentry sentry_key=mykey,x-custom=value")).toEqual({
      "x-sentry-auth": "Sentry sentry_key=mykey",
      "x-custom": "value",
    });
  });

  it("skips malformed pairs with no =", () => {
    const result = parseOTLPHeaders("Valid=value,malformedNoEquals");
    expect(result).toEqual({ Valid: "value" });
  });

  it("skips pairs with empty key", () => {
    const result = parseOTLPHeaders("=value,Good=ok");
    expect(result).toEqual({ Good: "ok" });
  });
});

// ---------------------------------------------------------------------------
// sendOTLPSpan headers
// ---------------------------------------------------------------------------

describe("sendOTLPSpan with OTEL_EXPORTER_OTLP_HEADERS", () => {
  const savedHeaders = process.env.OTEL_EXPORTER_OTLP_HEADERS;
  let mkdirSpy, appendSpy;

  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
    delete process.env.OTEL_EXPORTER_OTLP_HEADERS;
    mkdirSpy = vi.spyOn(fs, "mkdirSync").mockImplementation(() => {});
    appendSpy = vi.spyOn(fs, "appendFileSync").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    mkdirSpy.mockRestore();
    appendSpy.mockRestore();
    if (savedHeaders !== undefined) {
      process.env.OTEL_EXPORTER_OTLP_HEADERS = savedHeaders;
    } else {
      delete process.env.OTEL_EXPORTER_OTLP_HEADERS;
    }
  });

  it("includes custom headers when OTEL_EXPORTER_OTLP_HEADERS is set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.OTEL_EXPORTER_OTLP_HEADERS = "Authorization=Bearer mytoken,X-Tenant=acme";
    await sendOTLPSpan("https://traces.example.com", {});

    const [, init] = mockFetch.mock.calls[0];
    expect(init.headers["Authorization"]).toBe("Bearer mytoken");
    expect(init.headers["X-Tenant"]).toBe("acme");
    expect(init.headers["Content-Type"]).toBe("application/json");
  });

  it("does not add extra headers when OTEL_EXPORTER_OTLP_HEADERS is absent", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    await sendOTLPSpan("https://traces.example.com", {});

    const [, init] = mockFetch.mock.calls[0];
    expect(Object.keys(init.headers)).toEqual(["Content-Type"]);
  });
});

// ---------------------------------------------------------------------------
// buildGitHubActionsResourceAttributes
// ---------------------------------------------------------------------------

describe("buildGitHubActionsResourceAttributes", () => {
  const savedOTELResourceAttributes = process.env.OTEL_RESOURCE_ATTRIBUTES;

  afterEach(() => {
    if (savedOTELResourceAttributes === undefined) {
      delete process.env.OTEL_RESOURCE_ATTRIBUTES;
    } else {
      process.env.OTEL_RESOURCE_ATTRIBUTES = savedOTELResourceAttributes;
    }
  });

  it("parses escaped commas, equals signs, and backslashes in OTEL_RESOURCE_ATTRIBUTES", () => {
    process.env.OTEL_RESOURCE_ATTRIBUTES = "gh\\=aw.workflow.name=Daily\\\\ Token\\, Report,service.version=1.2.3";

    const attrs = Object.fromEntries(
      buildGitHubActionsResourceAttributes({
        repository: "owner/repo",
        runId: "987654321",
        staged: false,
      }).map(attr => [attr.key, attr.value.stringValue])
    );

    expect(attrs["gh=aw.workflow.name"]).toBe("Daily\\ Token, Report");
    expect(attrs["service.version"]).toBe("1.2.3");
  });

  it("parses percent-encoded OTEL_RESOURCE_ATTRIBUTES values", () => {
    process.env.OTEL_RESOURCE_ATTRIBUTES = "gh-aw.workflow.name=Daily%20Token%2C%20Report,gh-aw.engine.id=claude%3Dsonnet%5C4,service.version=1.2.3";

    const attrs = Object.fromEntries(
      buildGitHubActionsResourceAttributes({
        repository: "owner/repo",
        runId: "987654321",
        staged: false,
      }).map(attr => [attr.key, attr.value.stringValue])
    );

    expect(attrs["gh-aw.workflow.name"]).toBe("Daily Token, Report");
    expect(attrs["gh-aw.engine.id"]).toBe("claude=sonnet\\4");
    expect(attrs["service.version"]).toBe("1.2.3");
  });
});

// ---------------------------------------------------------------------------
// sendJobSetupSpan
// ---------------------------------------------------------------------------

describe("sendJobSetupSpan", () => {
  /** @type {Record<string, string | undefined>} */
  const savedEnv = {};
  const envKeys = [
    "GH_AW_OTLP_ENDPOINTS",
    "OTEL_SERVICE_NAME",
    "INPUT_JOB_NAME",
    "INPUT_TRACE_ID",
    "GH_AW_SETUP_WORKFLOW_NAME",
    "GH_AW_CURRENT_WORKFLOW_REF",
    "GH_AW_SETUP_AW_CONTEXT",
    "GH_AW_INFO_WORKFLOW_NAME",
    "GH_AW_INFO_ENGINE_ID",
    "GITHUB_RUN_ID",
    "GITHUB_RUN_ATTEMPT",
    "GITHUB_ACTOR",
    "GITHUB_REPOSITORY",
    "GITHUB_SERVER_URL",
    "GITHUB_EVENT_NAME",
    "GITHUB_REF",
    "GITHUB_REF_NAME",
    "GITHUB_HEAD_REF",
    "GITHUB_SHA",
    "GITHUB_JOB",
    "GITHUB_ACTOR_ID",
    "RUNNER_OS",
    "RUNNER_ARCH",
    "RUNNER_NAME",
    "RUNNER_ENVIRONMENT",
    "GITHUB_WORKFLOW_REF",
    "GH_AW_INFO_VERSION",
    "GH_AW_INFO_CLI_VERSION",
    "GH_AW_INFO_STAGED",
    "OTEL_RESOURCE_ATTRIBUTES",
  ];
  let mkdirSpy, appendSpy;

  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
    for (const k of envKeys) {
      savedEnv[k] = process.env[k];
      delete process.env[k];
    }
    mkdirSpy = vi.spyOn(fs, "mkdirSync").mockImplementation(() => {});
    appendSpy = vi.spyOn(fs, "appendFileSync").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    for (const k of envKeys) {
      if (savedEnv[k] !== undefined) {
        process.env[k] = savedEnv[k];
      } else {
        delete process.env[k];
      }
    }
    mkdirSpy.mockRestore();
    appendSpy.mockRestore();
  });

  /**
   * Extract the scalar value from an OTLP attribute's `value` union, covering all
   * known OTLP value types (stringValue, intValue, boolValue).
   *
   * @param {{ key: string, value: { stringValue?: string, intValue?: number, boolValue?: boolean } }} attr
   * @returns {string | number | boolean | undefined}
   */
  function attrValue(attr) {
    if (attr.value.stringValue !== undefined) return attr.value.stringValue;
    if (attr.value.intValue !== undefined) return attr.value.intValue;
    if (attr.value.boolValue !== undefined) return attr.value.boolValue;
    return undefined;
  }

  it("returns a trace ID and span ID even when GH_AW_OTLP_ENDPOINTS is not set", async () => {
    const { traceId, spanId } = await sendJobSetupSpan();
    expect(traceId).toMatch(/^[0-9a-f]{32}$/);
    expect(spanId).toMatch(/^[0-9a-f]{16}$/);
    expect(fetch).not.toHaveBeenCalled();
  });

  it("writes JSONL mirror even when GH_AW_OTLP_ENDPOINTS is not set", async () => {
    await sendJobSetupSpan();
    expect(appendSpy).toHaveBeenCalledOnce();
    const [filePath, content] = appendSpy.mock.calls[0];
    expect(filePath).toBe(OTEL_JSONL_PATH);
    const payload = JSON.parse(content.trim());
    expect(payload).toHaveProperty("resourceSpans");
  });

  it("returns the same trace ID when called with INPUT_TRACE_ID and no endpoint", async () => {
    process.env.INPUT_TRACE_ID = "a".repeat(32);
    const { traceId } = await sendJobSetupSpan();
    expect(traceId).toBe("a".repeat(32));
    expect(fetch).not.toHaveBeenCalled();
  });

  it("generates a new trace ID when INPUT_TRACE_ID is invalid", async () => {
    process.env.INPUT_TRACE_ID = "not-a-valid-trace-id";
    const { traceId } = await sendJobSetupSpan();
    expect(traceId).toMatch(/^[0-9a-f]{32}$/);
    expect(traceId).not.toBe("not-a-valid-trace-id");
  });

  it("normalizes uppercase INPUT_TRACE_ID to lowercase and accepts it", async () => {
    // Trace IDs pasted from external tools may be uppercase; we normalise them.
    process.env.INPUT_TRACE_ID = "A".repeat(32);
    const { traceId } = await sendJobSetupSpan();
    expect(traceId).toBe("a".repeat(32));
  });

  it("rejects an invalid options.traceId and generates a new trace ID", async () => {
    const { traceId } = await sendJobSetupSpan({ traceId: "too-short" });
    expect(traceId).toMatch(/^[0-9a-f]{32}$/);
    expect(traceId).not.toBe("too-short");
  });

  it("sends a span when endpoint is configured and returns the trace ID and span ID", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";
    process.env.GH_AW_INFO_WORKFLOW_NAME = "my-workflow";
    process.env.GH_AW_INFO_ENGINE_ID = "copilot";
    process.env.GITHUB_RUN_ID = "123456789";
    process.env.GITHUB_RUN_ATTEMPT = "2";
    process.env.GITHUB_ACTOR = "octocat";
    process.env.GITHUB_REPOSITORY = "owner/repo";

    const { traceId, spanId } = await sendJobSetupSpan();

    expect(traceId).toMatch(/^[0-9a-f]{32}$/);
    expect(spanId).toMatch(/^[0-9a-f]{16}$/);
    expect(mockFetch).toHaveBeenCalledOnce();
    const [url, init] = mockFetch.mock.calls[0];
    expect(url).toBe("https://traces.example.com/v1/traces");
    expect(init.method).toBe("POST");

    const body = JSON.parse(init.body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.name).toBe("gh-aw.agent.setup");
    expect(span.traceId).toBe(traceId);
    expect(span.spanId).toBe(spanId);

    const attrs = Object.fromEntries(span.attributes.map(a => [a.key, attrValue(a)]));
    expect(attrs["gh-aw.job.name"]).toBe("agent");
    expect(attrs["gh-aw.workflow.name"]).toBe("my-workflow");
    expect(attrs["gen_ai.system"]).toBe("github_models");
    expect(attrs["gh-aw.engine.id"]).toBe("copilot");
    expect(attrs["gh-aw.run.id"]).toBe("123456789");
    expect(attrs["gh-aw.run.attempt"]).toBe("2");
    expect(attrs["gh-aw.run.actor"]).toBe("octocat");
    expect(attrs["gh-aw.repository"]).toBe("owner/repo");
  });

  it("includes frontmatter source/emoji/body-modified metadata from aw_info.json on setup spans", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/aw_info.json") {
        return JSON.stringify({
          frontmatter_source: "github/gh-aw/.github/workflows/example.md@main",
          frontmatter_emoji: "🧪",
          body_modified: true,
        });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobSetupSpan();
    readFileSpy.mockRestore();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(span.attributes.map(a => [a.key, attrValue(a)]));
    expect(attrs["gh-aw.frontmatter.source"]).toBe("github/gh-aw/.github/workflows/example.md@main");
    expect(attrs["gh-aw.frontmatter.emoji"]).toBe("🧪");
    expect(attrs["gh-aw.frontmatter.body_modified"]).toBe(true);
  });

  it("falls back to setup env frontmatter source/emoji/body-modified metadata when aw_info.json is unavailable", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GH_AW_INFO_FRONTMATTER_SOURCE = "github/gh-aw/.github/workflows/env.md@main";
    process.env.GH_AW_INFO_FRONTMATTER_EMOJI = "🧭";
    process.env.GH_AW_INFO_BODY_MODIFIED = "false";

    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/aw_info.json") {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobSetupSpan();
    readFileSpy.mockRestore();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(span.attributes.map(a => [a.key, attrValue(a)]));
    expect(attrs["gh-aw.frontmatter.source"]).toBe("github/gh-aw/.github/workflows/env.md@main");
    expect(attrs["gh-aw.frontmatter.emoji"]).toBe("🧭");
    expect(attrs["gh-aw.frontmatter.body_modified"]).toBe(false);
  });

  it("uses setup workflow identity and inbound aw_context when aw_info.json is not available yet", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";
    process.env.GITHUB_RUN_ID = "25280567207";
    process.env.GITHUB_RUN_ATTEMPT = "1";
    process.env.GITHUB_WORKFLOW = "Smoke Call Workflow";
    process.env.GITHUB_WORKFLOW_REF = "owner/repo/.github/workflows/smoke-call-workflow.lock.yml@refs/heads/main";
    process.env.GH_AW_SETUP_WORKFLOW_NAME = "Smoke Workflow Call";
    process.env.GH_AW_CURRENT_WORKFLOW_REF = "owner/repo/.github/workflows/smoke-workflow-call.lock.yml@refs/heads/main";
    process.env.GH_AW_SETUP_AW_CONTEXT = JSON.stringify({
      episode_id: "25280567207-1:owner/repo/.github/workflows/smoke-call-workflow.lock.yml@refs/heads/main",
      hop_id: "25280567207-1:owner/repo/.github/workflows/smoke-call-workflow.lock.yml@refs/heads/main",
      parent_hop_id: "parent-hop-root",
      origin_event: "workflow_dispatch",
      root_repo: "owner/repo",
      root_workflow_id: "owner/repo/.github/workflows/smoke-call-workflow.lock.yml@refs/heads/main",
    });

    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/aw_info.json") {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobSetupSpan();
    readFileSpy.mockRestore();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(span.attributes.map(a => [a.key, attrValue(a)]));
    expect(attrs["gh-aw.workflow.name"]).toBe("Smoke Workflow Call");
    expect(attrs["gh-aw.episode.id"]).toBe("25280567207-1:owner/repo/.github/workflows/smoke-call-workflow.lock.yml@refs/heads/main");
    expect(attrs["gh-aw.hop.id"]).toBe("25280567207-1:owner/repo/.github/workflows/smoke-workflow-call.lock.yml@refs/heads/main");
    expect(attrs["gh-aw.hop.parent_id"]).toBe("parent-hop-root");

    const resourceAttrs = Object.fromEntries(body.resourceSpans[0].resource.attributes.map(a => [a.key, attrValue(a)]));
    expect(resourceAttrs["github.workflow_ref"]).toBe("owner/repo/.github/workflows/smoke-workflow-call.lock.yml@refs/heads/main");
  });

  it("defaults gh-aw.run.attempt to '1' when GITHUB_RUN_ATTEMPT is not set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.stringValue]));
    expect(attrs["gh-aw.run.attempt"]).toBe("1");
  });

  it("uses trace ID from options.traceId for cross-job correlation", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    const correlationTraceId = "b".repeat(32);

    const { traceId } = await sendJobSetupSpan({ traceId: correlationTraceId });

    expect(traceId).toBe(correlationTraceId);
    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    expect(body.resourceSpans[0].scopeSpans[0].spans[0].traceId).toBe(correlationTraceId);
  });

  it("uses trace ID from INPUT_TRACE_ID env var when options.traceId is absent", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_TRACE_ID = "c".repeat(32);

    const { traceId } = await sendJobSetupSpan();

    expect(traceId).toBe("c".repeat(32));
    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    expect(body.resourceSpans[0].scopeSpans[0].spans[0].traceId).toBe("c".repeat(32));
  });

  it("options.traceId takes priority over INPUT_TRACE_ID", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_TRACE_ID = "d".repeat(32);

    const { traceId } = await sendJobSetupSpan({ traceId: "e".repeat(32) });

    expect(traceId).toBe("e".repeat(32));
    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    expect(body.resourceSpans[0].scopeSpans[0].spans[0].traceId).toBe("e".repeat(32));
  });

  it("uses the provided startMs for the span start time", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    const startMs = 1_700_000_000_000;
    await sendJobSetupSpan({ startMs });

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.startTimeUnixNano).toBe(toNanoString(startMs));
  });

  it("uses OTEL_SERVICE_NAME for the resource service.name attribute", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.OTEL_SERVICE_NAME = "my-service";

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "service.name", value: { stringValue: "my-service" } });
  });

  it("includes github.repository and github.run_id as resource attributes", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_REPOSITORY = "owner/repo";
    process.env.GITHUB_RUN_ID = "987654321";

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "github.repository", value: { stringValue: "owner/repo" } });
    expect(resourceAttrs).toContainEqual({ key: "github.run_id", value: { stringValue: "987654321" } });
  });

  it("merges OTEL_RESOURCE_ATTRIBUTES into resource attributes", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_REPOSITORY = "owner/repo";
    process.env.GITHUB_RUN_ID = "987654321";
    process.env.OTEL_RESOURCE_ATTRIBUTES = "gh-aw.workflow.name=Daily%20Token%2C%20Report,gh-aw.run.id=987654321,gh-aw.engine.id=claude,service.version=1.2.3";

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "gh-aw.workflow.name", value: { stringValue: "Daily Token, Report" } });
    expect(resourceAttrs).toContainEqual({ key: "gh-aw.run.id", value: { stringValue: "987654321" } });
    expect(resourceAttrs).toContainEqual({ key: "gh-aw.engine.id", value: { stringValue: "claude" } });
    expect(resourceAttrs).toContainEqual({ key: "service.version", value: { stringValue: "1.2.3" } });
    expect(resourceAttrs).toContainEqual({ key: "github.repository", value: { stringValue: "owner/repo" } });
    expect(resourceAttrs).toContainEqual({ key: "github.run_id", value: { stringValue: "987654321" } });
  });

  it("includes github.run_attempt as resource attribute from GITHUB_RUN_ATTEMPT", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_RUN_ATTEMPT = "3";

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "github.run_attempt", value: { stringValue: "3" } });
  });

  it("defaults github.run_attempt resource attribute to '1' when GITHUB_RUN_ATTEMPT is not set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    delete process.env.GITHUB_RUN_ATTEMPT;

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "github.run_attempt", value: { stringValue: "1" } });
  });

  it("includes runner.* and github.actor_id as resource attributes when available", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_ACTOR_ID = "4175913";
    process.env.RUNNER_OS = "Linux";
    process.env.RUNNER_ARCH = "X64";
    process.env.RUNNER_NAME = "GitHub Actions 1187452382";
    process.env.RUNNER_ENVIRONMENT = "github-hosted";

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "github.actor_id", value: { stringValue: "4175913" } });
    expect(resourceAttrs).toContainEqual({ key: "runner.os", value: { stringValue: "Linux" } });
    expect(resourceAttrs).toContainEqual({ key: "runner.arch", value: { stringValue: "X64" } });
    expect(resourceAttrs).toContainEqual({ key: "runner.name", value: { stringValue: "GitHub Actions 1187452382" } });
    expect(resourceAttrs).toContainEqual({ key: "runner.environment", value: { stringValue: "github-hosted" } });
  });

  it("omits runner.* and github.actor_id resource attributes when unavailable", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceKeys = body.resourceSpans[0].resource.attributes.map(a => a.key);
    expect(resourceKeys).not.toContain("github.actor_id");
    expect(resourceKeys).not.toContain("runner.os");
    expect(resourceKeys).not.toContain("runner.arch");
    expect(resourceKeys).not.toContain("runner.name");
    expect(resourceKeys).not.toContain("runner.environment");
  });

  it("includes github.event_name as resource attribute when GITHUB_EVENT_NAME is set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_EVENT_NAME = "workflow_dispatch";

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "github.event_name", value: { stringValue: "workflow_dispatch" } });
  });

  it("omits github.event_name resource attribute when GITHUB_EVENT_NAME is not set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    const resourceKeys = resourceAttrs.map(a => a.key);
    expect(resourceKeys).not.toContain("github.event_name");
  });

  it("includes gh-aw.event_name as span attribute when GITHUB_EVENT_NAME is set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_EVENT_NAME = "workflow_dispatch";

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.attributes).toContainEqual({ key: "gh-aw.event_name", value: { stringValue: "workflow_dispatch" } });
  });

  it("omits gh-aw.event_name span attribute when GITHUB_EVENT_NAME is not set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const keys = span.attributes.map(a => a.key);
    expect(keys).not.toContain("gh-aw.event_name");
  });

  it("includes github.ref as resource attribute when GITHUB_REF is set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_REF = "refs/heads/main";

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "github.ref", value: { stringValue: "refs/heads/main" } });
  });

  it("includes github.ref_name and github.head_ref as resource attributes when set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_REF_NAME = "main";
    process.env.GITHUB_HEAD_REF = "feature-branch";

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "github.ref_name", value: { stringValue: "main" } });
    expect(resourceAttrs).toContainEqual({ key: "github.head_ref", value: { stringValue: "feature-branch" } });
  });

  it("omits github.ref resource attribute when GITHUB_REF is not set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    const resourceKeys = resourceAttrs.map(a => a.key);
    expect(resourceKeys).not.toContain("github.ref");
  });

  it("omits github.ref_name and github.head_ref resource attributes when not set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    const resourceKeys = resourceAttrs.map(a => a.key);
    expect(resourceKeys).not.toContain("github.ref_name");
    expect(resourceKeys).not.toContain("github.head_ref");
  });

  it("includes github.sha as resource attribute when GITHUB_SHA is set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_SHA = "abc1234567890def";

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "github.sha", value: { stringValue: "abc1234567890def" } });
  });

  it("omits github.sha resource attribute when GITHUB_SHA is not set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    const resourceKeys = resourceAttrs.map(a => a.key);
    expect(resourceKeys).not.toContain("github.sha");
  });

  it("includes github.workflow_ref as resource attribute when GITHUB_WORKFLOW_REF is set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_WORKFLOW_REF = "owner/repo/.github/workflows/my-workflow.yml@refs/heads/main";

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "github.workflow_ref", value: { stringValue: "owner/repo/.github/workflows/my-workflow.yml@refs/heads/main" } });
  });

  it("omits github.workflow_ref resource attribute when GITHUB_WORKFLOW_REF is not set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    const resourceKeys = resourceAttrs.map(a => a.key);
    expect(resourceKeys).not.toContain("github.workflow_ref");
  });

  it("includes github.job as resource attribute when GITHUB_JOB is set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_JOB = "agent";

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "github.job", value: { stringValue: "agent" } });
  });

  it("includes github.actions.run_url as resource and span attributes when repository and run_id are set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_REPOSITORY = "owner/repo";
    process.env.GITHUB_RUN_ID = "987654321";
    delete process.env.GITHUB_SERVER_URL;

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    const spanAttrs = body.resourceSpans[0].scopeSpans[0].spans[0].attributes;
    expect(resourceAttrs).toContainEqual({
      key: "github.actions.run_url",
      value: { stringValue: "https://github.com/owner/repo/actions/runs/987654321" },
    });
    expect(spanAttrs).toContainEqual({
      key: "github.actions.run_url",
      value: { stringValue: "https://github.com/owner/repo/actions/runs/987654321" },
    });
  });

  it("uses GITHUB_SERVER_URL for github.actions.run_url in sendJobSetupSpan", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_REPOSITORY = "owner/repo";
    process.env.GITHUB_RUN_ID = "987654321";
    process.env.GITHUB_SERVER_URL = "https://github.example.com";

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    const spanAttrs = body.resourceSpans[0].scopeSpans[0].spans[0].attributes;
    expect(resourceAttrs).toContainEqual({
      key: "github.actions.run_url",
      value: { stringValue: "https://github.example.com/owner/repo/actions/runs/987654321" },
    });
    expect(spanAttrs).toContainEqual({
      key: "github.actions.run_url",
      value: { stringValue: "https://github.example.com/owner/repo/actions/runs/987654321" },
    });
  });

  it("omits github.actions.run_url when repository or run_id is missing in sendJobSetupSpan", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    delete process.env.GITHUB_REPOSITORY;
    delete process.env.GITHUB_RUN_ID;

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    const resourceKeys = resourceAttrs.map(a => a.key);
    const spanAttrs = body.resourceSpans[0].scopeSpans[0].spans[0].attributes;
    const spanKeys = spanAttrs.map(a => a.key);
    expect(resourceKeys).not.toContain("github.actions.run_url");
    expect(spanKeys).not.toContain("github.actions.run_url");
  });

  it("includes service.version resource attribute when GH_AW_INFO_VERSION is set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GH_AW_INFO_VERSION = "v1.2.3";

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "service.version", value: { stringValue: "v1.2.3" } });
    const spanAttrs = body.resourceSpans[0].scopeSpans[0].spans[0].attributes;
    expect(spanAttrs).toContainEqual({ key: "gh-aw.cli.version", value: { stringValue: "v1.2.3" } });
  });

  it("falls back to GH_AW_INFO_CLI_VERSION for service.version when GH_AW_INFO_VERSION is absent (custom engines)", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    // GH_AW_INFO_VERSION deliberately absent (custom engine with no default version)
    process.env.GH_AW_INFO_CLI_VERSION = "v2.5.0";

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "service.version", value: { stringValue: "v2.5.0" } });
    // scope.version also uses the CLI fallback
    expect(body.resourceSpans[0].scopeSpans[0].scope.version).toBe("v2.5.0");
  });

  it("falls back to GITHUB_SHA for service.version when no gh-aw version is available", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    // No GH_AW_INFO_VERSION or GH_AW_INFO_CLI_VERSION — only SHA available
    process.env.GITHUB_SHA = "aabbccdd1122334455667788aabbccdd11223344";

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "service.version", value: { stringValue: "aabbccdd1122334455667788aabbccdd11223344" } });
    expect(body.resourceSpans[0].scopeSpans[0].scope.version).toBe("aabbccdd1122334455667788aabbccdd11223344");
  });

  it("includes gh-aw.awf.version and gh-aw.awmg.version resource attributes from aw_info.json", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/aw_info.json") {
        return JSON.stringify({ awf_version: "v1.2.3-awf", awmg_version: "v4.5.6-awmg" });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobSetupSpan();
    readFileSpy.mockRestore();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "gh-aw.awf.version", value: { stringValue: "v1.2.3-awf" } });
    expect(resourceAttrs).toContainEqual({ key: "gh-aw.awmg.version", value: { stringValue: "v4.5.6-awmg" } });
  });

  it("omits gh-aw.engine.id attribute when engine is not set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    await sendJobSetupSpan();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const keys = span.attributes.map(a => a.key);
    expect(keys).not.toContain("gen_ai.system");
    expect(keys).not.toContain("gh-aw.engine.id");
  });

  describe("engine ID resolution in setup span", () => {
    let readFileSpy;

    beforeEach(() => {
      readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
    });

    afterEach(() => {
      readFileSpy.mockRestore();
    });

    it("reads gh-aw.engine.id from aw_info.json when env var is absent", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/aw_info.json") {
          return JSON.stringify({ engine_id: "claude" });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobSetupSpan();

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.stringValue ?? a.value.intValue ?? a.value.boolValue]));
      expect(attrs["gen_ai.system"]).toBe("anthropic");
      expect(attrs["gh-aw.engine.id"]).toBe("claude");
    });

    it("reads gh-aw.engine.id from aw_context when aw_info.json has no engine_id", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_SETUP_AW_CONTEXT = JSON.stringify({ engine_id: "copilot" });

      await sendJobSetupSpan();

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.stringValue ?? a.value.intValue ?? a.value.boolValue]));
      expect(attrs["gh-aw.engine.id"]).toBe("copilot");
    });

    it("prefers aw_info.json engine_id over aw_context engine_id", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_SETUP_AW_CONTEXT = JSON.stringify({ engine_id: "copilot" });

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/aw_info.json") {
          return JSON.stringify({ engine_id: "claude" });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobSetupSpan();

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.stringValue ?? a.value.intValue ?? a.value.boolValue]));
      expect(attrs["gh-aw.engine.id"]).toBe("claude");
    });

    it("falls back to GH_AW_INFO_ENGINE_ID env var when aw_info.json and aw_context have no engine_id", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_INFO_ENGINE_ID = "codex";

      await sendJobSetupSpan();

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.stringValue ?? a.value.intValue ?? a.value.boolValue]));
      expect(attrs["gen_ai.system"]).toBe("openai");
      expect(attrs["gh-aw.engine.id"]).toBe("codex");
    });
  });

  describe("cross-job parent span propagation via aw_context", () => {
    let readFileSpy;

    beforeEach(() => {
      readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
    });

    afterEach(() => {
      readFileSpy.mockRestore();
    });

    it("uses otel_parent_span_id from aw_context as parentSpanId for cross-job trace hierarchy", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      const parentSpanId = "abcdef1234567890";

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/aw_info.json") {
          return JSON.stringify({ context: { otel_trace_id: "a".repeat(32), otel_parent_span_id: parentSpanId } });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobSetupSpan();

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.parentSpanId).toBe(parentSpanId);
    });

    it("omits parentSpanId when aw_context.otel_parent_span_id is absent", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/aw_info.json") {
          return JSON.stringify({ context: { otel_trace_id: "a".repeat(32) } });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobSetupSpan();

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.parentSpanId).toBeUndefined();
    });

    it("ignores invalid otel_parent_span_id from aw_context and omits parentSpanId", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/aw_info.json") {
          return JSON.stringify({ context: { otel_trace_id: "a".repeat(32), otel_parent_span_id: "not-a-valid-span-id" } });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobSetupSpan();

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.parentSpanId).toBeUndefined();
    });
  });

  describe("staged / deployment.environment", () => {
    let readFileSpy;

    beforeEach(() => {
      readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
    });

    afterEach(() => {
      readFileSpy.mockRestore();
    });

    it("sets deployment.environment=production when aw_info.json is absent", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      await sendJobSetupSpan();

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const resourceAttrs = body.resourceSpans[0].resource.attributes;
      expect(resourceAttrs).toContainEqual({ key: "deployment.environment", value: { stringValue: "production" } });
    });

    it("sets deployment.environment=staging when aw_info.json is absent and GH_AW_INFO_STAGED=true", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_INFO_STAGED = "true";

      await sendJobSetupSpan();

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.attributes).toContainEqual({ key: "gh-aw.staged", value: { boolValue: true } });

      const resourceAttrs = body.resourceSpans[0].resource.attributes;
      expect(resourceAttrs).toContainEqual({ key: "deployment.environment", value: { stringValue: "staging" } });
    });

    it("sets deployment.environment=staging when awInfo.staged=true", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/aw_info.json") {
          return JSON.stringify({ staged: true });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobSetupSpan();

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const resourceAttrs = body.resourceSpans[0].resource.attributes;
      expect(resourceAttrs).toContainEqual({ key: "deployment.environment", value: { stringValue: "staging" } });
    });

    it("sets deployment.environment=production when awInfo.staged=false", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/aw_info.json") {
          return JSON.stringify({ staged: false });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobSetupSpan();

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.attributes).toContainEqual({ key: "gh-aw.staged", value: { boolValue: false } });

      const resourceAttrs = body.resourceSpans[0].resource.attributes;
      expect(resourceAttrs).toContainEqual({ key: "deployment.environment", value: { stringValue: "production" } });
    });
  });

  describe("trigger item context from aw_info.json", () => {
    let readFileSpy;

    beforeEach(() => {
      readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
    });

    afterEach(() => {
      readFileSpy.mockRestore();
    });

    it("emits gh-aw.trigger.item_type and gh-aw.trigger.item_number from aw_info.context", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/aw_info.json") {
          return JSON.stringify({ context: { item_type: "issue", item_number: "42", trigger_label: "" } });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobSetupSpan();

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.attributes).toContainEqual({ key: "gh-aw.trigger.item_type", value: { stringValue: "issue" } });
      expect(span.attributes).toContainEqual({ key: "gh-aw.trigger.item_number", value: { stringValue: "42" } });
      const keys = span.attributes.map(a => a.key);
      expect(keys).not.toContain("gh-aw.trigger.label");
      expect(keys).not.toContain("gh-aw.trigger.comment_id");
    });

    it("emits gh-aw.trigger.label when trigger_label is non-empty", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/aw_info.json") {
          return JSON.stringify({ context: { item_type: "pull_request", item_number: "99", trigger_label: "copilot" } });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobSetupSpan();

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.attributes).toContainEqual({ key: "gh-aw.trigger.item_type", value: { stringValue: "pull_request" } });
      expect(span.attributes).toContainEqual({ key: "gh-aw.trigger.item_number", value: { stringValue: "99" } });
      expect(span.attributes).toContainEqual({ key: "gh-aw.trigger.label", value: { stringValue: "copilot" } });
    });

    it("emits gh-aw.trigger.comment_id when comment_id is non-empty", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/aw_info.json") {
          return JSON.stringify({ context: { item_type: "pull_request", item_number: "99", trigger_label: "copilot", comment_id: "123456789" } });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobSetupSpan();

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.attributes).toContainEqual({ key: "gh-aw.trigger.comment_id", value: { stringValue: "123456789" } });
    });

    it("omits trigger attributes when aw_info.json is absent", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GITHUB_RUN_ID = "555";
      process.env.GITHUB_RUN_ATTEMPT = "2";

      await sendJobSetupSpan();

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const keys = span.attributes.map(a => a.key);
      expect(keys).not.toContain("gh-aw.trigger.item_type");
      expect(keys).not.toContain("gh-aw.trigger.item_number");
      expect(keys).not.toContain("gh-aw.trigger.label");
      expect(keys).not.toContain("gh-aw.trigger.comment_id");
      expect(span.attributes).toContainEqual({ key: "gh-aw.episode.id", value: { stringValue: "555-2" } });
      expect(span.attributes).toContainEqual({ key: "gh-aw.episode.kind", value: { stringValue: "run" } });
      expect(span.attributes).toContainEqual({ key: "gh-aw.workflow_call.id", value: { stringValue: "555-2" } });
    });

    it("uses aw_info context lineage fields as the live episode id for child workflows", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GITHUB_RUN_ID = "777";
      process.env.GITHUB_RUN_ATTEMPT = "3";

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/aw_info.json") {
          return JSON.stringify({
            context: {
              episode_id: "episode-99",
              hop_id: "123-1",
              parent_hop_id: "122-1",
              origin_event: "workflow_run",
              root_repo: "owner/repo",
              root_workflow_id: "owner/repo/.github/workflows/root.yml@refs/heads/main",
              workflow_call_id: "123-1",
              item_type: "issue",
              item_number: "42",
            },
          });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobSetupSpan();

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.attributes).toContainEqual({ key: "gh-aw.episode.id", value: { stringValue: "episode-99" } });
      expect(span.attributes).toContainEqual({ key: "gh-aw.episode.kind", value: { stringValue: "workflow_call" } });
      expect(span.attributes).toContainEqual({ key: "gh-aw.hop.id", value: { stringValue: "777-3" } });
      expect(span.attributes).toContainEqual({ key: "gh-aw.hop.parent_id", value: { stringValue: "122-1" } });
      expect(span.attributes).toContainEqual({ key: "gh-aw.origin.event", value: { stringValue: "workflow_run" } });
      expect(span.attributes).toContainEqual({ key: "gh-aw.root.repo", value: { stringValue: "owner/repo" } });
      expect(span.attributes).toContainEqual({ key: "gh-aw.workflow_call.id", value: { stringValue: "777-3" } });
      expect(span.attributes).toContainEqual({ key: "gh-aw.workflow_call.parent_id", value: { stringValue: "122-1" } });
    });
  });

  describe("experiment attributes", () => {
    let readFileSpy;

    beforeEach(() => {
      readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
    });

    afterEach(() => {
      readFileSpy.mockRestore();
    });

    it("includes gh-aw.experiment.<name> and gh-aw.experiments attributes when assignments file exists", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === EXPERIMENT_ASSIGNMENTS_PATH) {
          return JSON.stringify({ caveman: "yes", style: "detailed" });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobSetupSpan();

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.stringValue]));
      expect(attrs["gh-aw.experiment.caveman"]).toBe("yes");
      expect(attrs["gh-aw.experiment.style"]).toBe("detailed");
      expect(attrs["gh-aw.experiments"]).toBe(JSON.stringify({ caveman: "yes", style: "detailed" }));
    });

    it("omits experiment attributes when assignments file is absent", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      await sendJobSetupSpan();

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const keys = span.attributes.map(a => a.key);
      expect(keys.some(k => k.startsWith("gh-aw.experiment."))).toBe(false);
      expect(keys).not.toContain("gh-aw.experiments");
    });
  });
});

// ---------------------------------------------------------------------------
// readExperimentAssignments / buildExperimentAttributes
// ---------------------------------------------------------------------------

describe("readExperimentAssignments", () => {
  let readFileSpy;
  const savedStateDir = process.env.GH_AW_EXPERIMENT_STATE_DIR;

  beforeEach(() => {
    delete process.env.GH_AW_EXPERIMENT_STATE_DIR;
    readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });
  });

  afterEach(() => {
    readFileSpy.mockRestore();
    if (savedStateDir !== undefined) {
      process.env.GH_AW_EXPERIMENT_STATE_DIR = savedStateDir;
    } else {
      delete process.env.GH_AW_EXPERIMENT_STATE_DIR;
    }
  });

  it("returns null when the assignments file does not exist", () => {
    expect(readExperimentAssignments()).toBeNull();
  });

  it("returns null when the assignments file contains invalid JSON", () => {
    readFileSpy.mockReturnValue("not-valid-json");
    expect(readExperimentAssignments()).toBeNull();
  });

  it("returns null when the assignments file contains a non-object value", () => {
    readFileSpy.mockReturnValue(JSON.stringify(["A", "B"]));
    expect(readExperimentAssignments()).toBeNull();
  });

  it("returns the parsed assignments object when the file is valid", () => {
    readFileSpy.mockImplementation(filePath => {
      if (filePath === EXPERIMENT_ASSIGNMENTS_PATH) {
        return JSON.stringify({ caveman: "yes", style: "detailed" });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });
    expect(readExperimentAssignments()).toEqual({ caveman: "yes", style: "detailed" });
  });

  it("reads from GH_AW_EXPERIMENT_STATE_DIR/assignments.json when env var is set", () => {
    process.env.GH_AW_EXPERIMENT_STATE_DIR = "/custom/experiments";
    readFileSpy.mockImplementation(filePath => {
      if (filePath === "/custom/experiments/assignments.json") {
        return JSON.stringify({ feature: "on" });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });
    expect(readExperimentAssignments()).toEqual({ feature: "on" });
  });

  it("falls back to EXPERIMENT_ASSIGNMENTS_PATH when GH_AW_EXPERIMENT_STATE_DIR is not set", () => {
    readFileSpy.mockImplementation(filePath => {
      if (filePath === EXPERIMENT_ASSIGNMENTS_PATH) {
        return JSON.stringify({ mode: "fast" });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });
    expect(readExperimentAssignments()).toEqual({ mode: "fast" });
  });
});

describe("buildExperimentAttributes", () => {
  it("returns an empty array for null input", () => {
    expect(buildExperimentAttributes(null)).toEqual([]);
  });

  it("returns an empty array for an empty assignments object", () => {
    expect(buildExperimentAttributes({})).toEqual([]);
  });

  it("builds one attribute per experiment plus the aggregated gh-aw.experiments attribute", () => {
    const attrs = buildExperimentAttributes({ caveman: "yes", style: "detailed" });
    const attrMap = Object.fromEntries(attrs.map(a => [a.key, a.value.stringValue]));
    expect(attrMap["gh-aw.experiment.caveman"]).toBe("yes");
    expect(attrMap["gh-aw.experiment.style"]).toBe("detailed");
    // experiments JSON is sorted by key
    expect(JSON.parse(attrMap["gh-aw.experiments"])).toEqual({ caveman: "yes", style: "detailed" });
  });

  it("skips assignments with non-string or empty-string variants and still adds gh-aw.experiments for valid ones", () => {
    const attrs = buildExperimentAttributes({ good: "A", bad: "" });
    const keys = attrs.map(a => a.key);
    expect(keys).toContain("gh-aw.experiment.good");
    expect(keys).not.toContain("gh-aw.experiment.bad");
    // gh-aw.experiments is present and only contains the valid variant
    const experimentsAttr = attrs.find(a => a.key === "gh-aw.experiments");
    expect(experimentsAttr).toBeDefined();
    expect(JSON.parse(experimentsAttr.value.stringValue)).toEqual({ good: "A" });
  });

  it("returns empty array and omits gh-aw.experiments when all variants are empty strings", () => {
    const attrs = buildExperimentAttributes({ exp1: "", exp2: "" });
    expect(attrs).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// sendJobConclusionSpan (continued — experiment attributes)

describe("sendJobConclusionSpan", () => {
  /** @type {Record<string, string | undefined>} */
  const savedEnv = {};
  const envKeys = [
    "GH_AW_OTLP_ENDPOINTS",
    "OTEL_SERVICE_NAME",
    "GH_AW_EFFECTIVE_TOKENS",
    "GH_AW_AIC",
    "GH_AW_MAX_AI_CREDITS",
    "GH_AW_AI_CREDITS_RATE_LIMIT_ERROR",
    "GH_AW_INFO_VERSION",
    "GH_AW_INFO_CLI_VERSION",
    "GITHUB_AW_OTEL_TRACE_ID",
    "GITHUB_AW_OTEL_PARENT_SPAN_ID",
    "GITHUB_RUN_ID",
    "GITHUB_RUN_ATTEMPT",
    "GITHUB_ACTOR",
    "GITHUB_REPOSITORY",
    "GITHUB_EVENT_NAME",
    "GITHUB_REF",
    "GITHUB_REF_NAME",
    "GITHUB_HEAD_REF",
    "GITHUB_SHA",
    "GITHUB_JOB",
    "GITHUB_ACTOR_ID",
    "RUNNER_OS",
    "RUNNER_ARCH",
    "RUNNER_NAME",
    "RUNNER_ENVIRONMENT",
    "GITHUB_WORKFLOW_REF",
    "INPUT_JOB_NAME",
    "GH_AW_AGENT_CONCLUSION",
    "GH_AW_DETECTION_CONCLUSION",
    "GH_AW_DETECTION_REASON",
    "GH_AW_TRACKER_ID",
    "GH_AW_INFO_WORKFLOW_NAME",
    "GITHUB_WORKFLOW",
    "OTEL_RESOURCE_ATTRIBUTES",
  ];
  let mkdirSpy, appendSpy;

  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
    for (const k of envKeys) {
      savedEnv[k] = process.env[k];
      delete process.env[k];
    }
    mkdirSpy = vi.spyOn(fs, "mkdirSync").mockImplementation(() => {});
    appendSpy = vi.spyOn(fs, "appendFileSync").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    for (const k of envKeys) {
      if (savedEnv[k] !== undefined) {
        process.env[k] = savedEnv[k];
      } else {
        delete process.env[k];
      }
    }
    mkdirSpy.mockRestore();
    appendSpy.mockRestore();
  });

  it("skips OTLP export but writes JSONL mirror when GH_AW_OTLP_ENDPOINTS is not set", async () => {
    await sendJobConclusionSpan("gh-aw.job.conclusion");
    expect(fetch).not.toHaveBeenCalled();
    expect(appendSpy).toHaveBeenCalledOnce();
    const [filePath, content] = appendSpy.mock.calls[0];
    expect(filePath).toBe(OTEL_JSONL_PATH);
    const payload = JSON.parse(content.trim());
    expect(payload).toHaveProperty("resourceSpans");
  });

  it("sends a span with the given span name", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_RUN_ID = "111";
    process.env.GITHUB_ACTOR = "octocat";
    process.env.GITHUB_REPOSITORY = "owner/repo";

    await sendJobConclusionSpan("gh-aw.job.safe-outputs");

    expect(mockFetch).toHaveBeenCalledOnce();
    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.name).toBe("gh-aw.job.safe-outputs");
    expect(span.traceId).toMatch(/^[0-9a-f]{32}$/);
    expect(span.spanId).toMatch(/^[0-9a-f]{16}$/);
  });

  it("emits graders only on the agent job conclusion span", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/agent/graders/grader_results.json") {
        return JSON.stringify({ results: [{ id: "quality", status: "pass", value: 0.9 }] });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion");
    process.env.INPUT_JOB_NAME = "conclusion";
    await sendJobConclusionSpan("gh-aw.conclusion.conclusion");
    readFileSpy.mockRestore();

    const spans = mockFetch.mock.calls.map(([, request]) => JSON.parse(request.body).resourceSpans[0].scopeSpans[0].spans[0]);
    expect(spans[0].attributes).toContainEqual(buildAttr("gh-aw.graders.count", 1));
    expect(spans[0].events).toContainEqual(expect.objectContaining({ name: "grader.result" }));
    expect(spans[1].attributes.map(attribute => attribute.key)).not.toContain("gh-aw.graders.count");
    expect((spans[1].events ?? []).map(event => event.name)).not.toContain("grader.result");
  });

  it("emits eval results only on the evals job conclusion span", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "evals";
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/evals.jsonl") {
        return `${JSON.stringify({ id: "quality", answer: "YES", model: "gpt-5" })}\n`;
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.evals.conclusion");
    process.env.INPUT_JOB_NAME = "conclusion";
    await sendJobConclusionSpan("gh-aw.conclusion.conclusion");
    readFileSpy.mockRestore();

    const spans = mockFetch.mock.calls.map(([, request]) => JSON.parse(request.body).resourceSpans[0].scopeSpans[0].spans[0]);
    expect(spans[0].attributes).toContainEqual(buildAttr("gh-aw.evals.count", 1));
    expect(spans[0].events).toContainEqual(expect.objectContaining({ name: "eval.result" }));
    expect(spans[1].attributes.map(attribute => attribute.key)).not.toContain("gh-aw.evals.count");
    expect((spans[1].events ?? []).map(event => event.name)).not.toContain("eval.result");
  });

  it("emits live episode attributes on conclusion spans from aw_info workflow_call context", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_RUN_ID = "888";
    process.env.GITHUB_RUN_ATTEMPT = "4";
    process.env.GITHUB_REPOSITORY = "owner/repo";

    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/aw_info.json") {
        return JSON.stringify({ context: { episode_id: "episode-123", hop_id: "123-1", parent_hop_id: "122-1", workflow_call_id: "123-1", otel_trace_id: "a".repeat(32) } });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    readFileSpy.mockRestore();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.attributes).toContainEqual({ key: "gh-aw.episode.id", value: { stringValue: "episode-123" } });
    expect(span.attributes).toContainEqual({ key: "gh-aw.episode.kind", value: { stringValue: "workflow_call" } });
    expect(span.attributes).toContainEqual({ key: "gh-aw.hop.id", value: { stringValue: "888-4" } });
    expect(span.attributes).toContainEqual({ key: "gh-aw.hop.parent_id", value: { stringValue: "122-1" } });
    expect(span.attributes).toContainEqual({ key: "gh-aw.workflow_call.id", value: { stringValue: "888-4" } });
    expect(span.attributes).toContainEqual({ key: "gh-aw.workflow_call.parent_id", value: { stringValue: "122-1" } });
    expect(span.traceId).toBe("a".repeat(32));
  });

  it("emits a dedicated gh-aw.<job>.agent span when startMs and agent_output mtime are available", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";
    process.env.GITHUB_AW_OTEL_TRACE_ID = "f".repeat(32);
    process.env.GITHUB_AW_OTEL_PARENT_SPAN_ID = "abcdef1234567890";
    process.env.GH_AW_MAX_AI_CREDITS = "1000";

    const startMs = 1_700_000_000_000;
    const endMs = 1_700_000_005_000;
    const statSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: endMs });
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/agent_output.json") {
        return JSON.stringify({ items: [{ type: "issue" }, { type: "pull_request" }] });
      }
      if (filePath === "/tmp/gh-aw/agent/graders/grader_results.json") {
        return JSON.stringify({ results: [{ id: "quality", status: "pass" }] });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    readFileSpy.mockRestore();
    expect(mockFetch).toHaveBeenCalledTimes(2);

    const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
    const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
    expect(agentSpan.name).toBe("gh-aw.agent.agent");
    expect(agentSpan.startTimeUnixNano).toBe(toNanoString(startMs));
    expect(agentSpan.endTimeUnixNano).toBe(toNanoString(endMs));

    const conclusionBody = JSON.parse(mockFetch.mock.calls[1][1].body);
    const conclusionSpan = conclusionBody.resourceSpans[0].scopeSpans[0].spans[0];
    expect(conclusionSpan.name).toBe("gh-aw.agent.conclusion");
    expect(agentSpan.traceId).toBe(conclusionSpan.traceId);
    expect(agentSpan.parentSpanId).toBe(conclusionSpan.spanId);
    expect(agentSpan.parentSpanId).not.toBe("abcdef1234567890");
    expect(conclusionSpan.parentSpanId).toBe("abcdef1234567890");
    expect(agentSpan.attributes).toContainEqual({ key: "gh-aw.output.item_count", value: { intValue: 2 } });
    expect(conclusionSpan.attributes).toContainEqual({ key: "gh-aw.output.item_count", value: { intValue: 2 } });
    expect(agentSpan.attributes.map(attribute => attribute.key)).not.toContain("gh-aw.graders.count");
    expect(conclusionSpan.attributes).toContainEqual(buildAttr("gh-aw.graders.count", 1));
    expect(conclusionSpan.events).toContainEqual(expect.objectContaining({ name: "grader.result" }));
    const agentKeys = agentSpan.attributes.map(a => a.key);
    const conclusionKeys = conclusionSpan.attributes.map(a => a.key);
    expect(agentKeys).not.toContain("gh-aw.max_ai_credits");
    expect(agentKeys).not.toContain("gh-aw.max_ai_credits_exceeded");
    expect(agentKeys).not.toContain("gh-aw.ai_credits_rate_limit_error");
    expect(conclusionKeys).toContain("gh-aw.max_ai_credits");
    expect(conclusionKeys).toContain("gh-aw.max_ai_credits_exceeded");
    expect(conclusionKeys).toContain("gh-aw.ai_credits_rate_limit_error");
  });

  it("uses agent_cli_start_ms.txt as agent span start time when file is present", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";
    process.env.GITHUB_AW_OTEL_TRACE_ID = "f".repeat(32);
    process.env.GITHUB_AW_OTEL_PARENT_SPAN_ID = "abcdef1234567890";

    // The CLI start time is later than the job start time passed as options.startMs,
    // simulating a realistic scenario where the audit and proxy startup took time.
    const jobStartMs = 1_700_000_000_000; // end of setup step
    const cliStartMs = 1_700_000_030_000; // 30 s later: start of Execute CLI step
    const endMs = 1_700_000_090_000;

    const statSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: endMs });
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/agent_cli_start_ms.txt") {
        return String(cliStartMs);
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs: jobStartMs });

    statSpy.mockRestore();
    readFileSpy.mockRestore();
    expect(mockFetch).toHaveBeenCalledTimes(2);

    const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
    const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
    expect(agentSpan.name).toBe("gh-aw.agent.agent");
    // Agent span must start from the CLI-step timestamp, not the job start time.
    expect(agentSpan.startTimeUnixNano).toBe(toNanoString(cliStartMs));
    expect(agentSpan.endTimeUnixNano).toBe(toNanoString(endMs));

    const conclusionBody = JSON.parse(mockFetch.mock.calls[1][1].body);
    const conclusionSpan = conclusionBody.resourceSpans[0].scopeSpans[0].spans[0];
    // Conclusion span still starts from the job start time (options.startMs).
    expect(conclusionSpan.startTimeUnixNano).toBe(toNanoString(jobStartMs));
  });

  it("falls back to options.startMs as agent span start when agent_cli_start_ms.txt is absent", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";
    process.env.GITHUB_AW_OTEL_TRACE_ID = "f".repeat(32);

    const startMs = 1_700_000_000_000;
    const endMs = 1_700_000_005_000;

    const statSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: endMs });
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
      // Simulate all files absent (no agent_cli_start_ms.txt)
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    readFileSpy.mockRestore();
    expect(mockFetch).toHaveBeenCalledTimes(2);

    const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
    const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
    expect(agentSpan.name).toBe("gh-aw.agent.agent");
    // Must fall back to options.startMs when the file is absent.
    expect(agentSpan.startTimeUnixNano).toBe(toNanoString(startMs));
  });

  it("falls back to options.startMs when agent_cli_start_ms.txt contains an invalid value", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";
    process.env.GITHUB_AW_OTEL_TRACE_ID = "f".repeat(32);

    const startMs = 1_700_000_000_000;
    const endMs = 1_700_000_005_000;

    const statSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: endMs });
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/agent_cli_start_ms.txt") {
        return "not-a-number"; // invalid content
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    readFileSpy.mockRestore();
    expect(mockFetch).toHaveBeenCalledTimes(2);

    const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
    const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
    expect(agentSpan.name).toBe("gh-aw.agent.agent");
    // Must fall back to options.startMs for invalid file content.
    expect(agentSpan.startTimeUnixNano).toBe(toNanoString(startMs));
  });

  it("does not emit a dedicated agent span when agent_output mtime is unavailable", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    const statSpy = vi.spyOn(fs, "statSync").mockImplementation(() => {
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.job.conclusion", { startMs: 1_700_000_000_000 });

    statSpy.mockRestore();
    expect(mockFetch).toHaveBeenCalledOnce();
    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.name).toBe("gh-aw.job.conclusion");
  });

  it("emits a dedicated agent span on timed_out when agent_output mtime is unavailable", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";
    process.env.GH_AW_AGENT_CONCLUSION = "timed_out";

    const startMs = 1_700_000_000_000;
    const statSpy = vi.spyOn(fs, "statSync").mockImplementation(() => {
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    expect(mockFetch).toHaveBeenCalledTimes(2);

    const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
    const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
    expect(agentSpan.name).toBe("gh-aw.agent.agent");
    expect(agentSpan.startTimeUnixNano).toBe(toNanoString(startMs));
    expect(BigInt(agentSpan.endTimeUnixNano)).toBeGreaterThan(BigInt(toNanoString(startMs)));

    const conclusionBody = JSON.parse(mockFetch.mock.calls[1][1].body);
    const conclusionSpan = conclusionBody.resourceSpans[0].scopeSpans[0].spans[0];
    expect(conclusionSpan.name).toBe("gh-aw.agent.conclusion");
    expect(agentSpan.parentSpanId).toBe(conclusionSpan.spanId);
    expect(conclusionSpan.status.code).toBe(2);
    expect(conclusionSpan.status.message).toContain("agent timed_out");
  });

  it("emits a dedicated agent span on cancelled when agent_output mtime is unavailable", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";
    process.env.GH_AW_AGENT_CONCLUSION = "cancelled";

    const startMs = 1_700_000_000_000;
    const statSpy = vi.spyOn(fs, "statSync").mockImplementation(() => {
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    expect(mockFetch).toHaveBeenCalledTimes(2);

    const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
    const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
    expect(agentSpan.name).toBe("gh-aw.agent.agent");
    expect(agentSpan.startTimeUnixNano).toBe(toNanoString(startMs));
    expect(BigInt(agentSpan.endTimeUnixNano)).toBeGreaterThan(BigInt(toNanoString(startMs)));

    const conclusionBody = JSON.parse(mockFetch.mock.calls[1][1].body);
    const conclusionSpan = conclusionBody.resourceSpans[0].scopeSpans[0].spans[0];
    expect(conclusionSpan.name).toBe("gh-aw.agent.conclusion");
    expect(agentSpan.parentSpanId).toBe(conclusionSpan.spanId);
    expect(conclusionSpan.status.code).toBe(2);
    expect(conclusionSpan.status.message).toContain("agent cancelled");
  });

  it("does not emit a dedicated agent span for non-agent jobs", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "safe-outputs";

    const statSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: 1_700_000_005_000 });

    await sendJobConclusionSpan("gh-aw.safe-outputs.conclusion", { startMs: 1_700_000_000_000 });

    statSpy.mockRestore();
    expect(mockFetch).toHaveBeenCalledOnce();
    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    expect(body.resourceSpans[0].scopeSpans[0].spans).toHaveLength(1);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.name).toBe("gh-aw.safe-outputs.conclusion");
  });

  it("emits the agent span with SPAN_KIND_CLIENT (3)", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";

    const startMs = 1_700_000_000_000;
    const endMs = 1_700_000_005_000;
    const statSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: endMs });
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    readFileSpy.mockRestore();

    const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
    const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
    expect(agentSpan.name).toBe("gh-aw.agent.agent");
    expect(agentSpan.kind).toBe(3); // SPAN_KIND_CLIENT
  });

  it("includes gen_ai.request.model, gen_ai.system, gh-aw.engine.id, gen_ai.operation.name and gen_ai.workflow.name on the agent span from aw_info.json", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";

    const startMs = 1_700_000_000_000;
    const endMs = 1_700_000_005_000;
    const statSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: endMs });
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/aw_info.json") {
        return JSON.stringify({ model: "claude-3-5-sonnet-20241022", engine_id: "claude", workflow_name: "otel-advisor" });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    readFileSpy.mockRestore();

    const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
    const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
    expect(agentSpan.name).toBe("gh-aw.agent.agent");
    const attrs = Object.fromEntries(agentSpan.attributes.map(a => [a.key, a.value.stringValue ?? a.value.intValue]));
    expect(attrs["gen_ai.operation.name"]).toBe("chat");
    expect(attrs["gen_ai.request.model"]).toBe("claude-3-5-sonnet-20241022");
    expect(attrs["gen_ai.system"]).toBe("anthropic");
    expect(attrs["gh-aw.engine.id"]).toBe("claude");
    expect(attrs["gh-aw.engine"]).toBeUndefined();
    expect(attrs["gen_ai.workflow.name"]).toBe("otel-advisor");

    const conclusionBody = JSON.parse(mockFetch.mock.calls[1][1].body);
    const conclusionSpan = conclusionBody.resourceSpans[0].scopeSpans[0].spans[0];
    const conclusionAttrs = Object.fromEntries(conclusionSpan.attributes.map(a => [a.key, a.value.stringValue ?? a.value.intValue]));
    expect(conclusionAttrs["gen_ai.operation.name"]).toBe("chat");
    expect(conclusionAttrs["gen_ai.workflow.name"]).toBe("otel-advisor");
  });

  it("does not duplicate gen_ai.request.model on the agent span", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";

    const startMs = 1_700_000_000_000;
    const endMs = 1_700_000_005_000;
    const statSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: endMs });
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/aw_info.json") {
        return JSON.stringify({ model: "gpt-4o", engine_id: "codex" });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    readFileSpy.mockRestore();

    const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
    const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
    const modelKeys = agentSpan.attributes.filter(a => a.key === "gen_ai.request.model");
    // gen_ai.request.model must appear exactly once — no duplicate from a second push
    expect(modelKeys).toHaveLength(1);
    expect(modelKeys[0].value.stringValue).toBe("gpt-4o");
  });

  it("does not duplicate gen_ai.system on the agent span", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";

    const startMs = 1_700_000_000_000;
    const endMs = 1_700_000_005_000;
    const statSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: endMs });
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/aw_info.json") {
        return JSON.stringify({ engine_id: "claude" });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    readFileSpy.mockRestore();

    const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
    const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
    const systemKeys = agentSpan.attributes.filter(a => a.key === "gen_ai.system");
    expect(systemKeys).toHaveLength(1);
    expect(systemKeys[0].value.stringValue).toBe("anthropic");
  });

  it("omits gen_ai.request.model, gen_ai.system, gh-aw.engine.id and gen_ai.workflow.name from the agent span when model, engine_id and workflow_name are absent", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";

    const startMs = 1_700_000_000_000;
    const endMs = 1_700_000_005_000;
    const statSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: endMs });
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    readFileSpy.mockRestore();

    const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
    const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(agentSpan.attributes.map(a => [a.key, a.value.stringValue ?? a.value.intValue]));
    // gen_ai.operation.name is always present
    expect(attrs["gen_ai.operation.name"]).toBe("chat");
    const keys = agentSpan.attributes.map(a => a.key);
    expect(keys).not.toContain("gen_ai.request.model");
    expect(keys).not.toContain("gen_ai.system");
    expect(keys).not.toContain("gh-aw.engine.id");
    expect(keys).not.toContain("gen_ai.workflow.name");
  });

  it("uses the raw engine ID as gen_ai.system fallback for unknown engines on the agent span", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";

    const startMs = 1_700_000_000_000;
    const endMs = 1_700_000_005_000;
    const statSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: endMs });
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/aw_info.json") {
        return JSON.stringify({ engine_id: "custom-engine" });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    readFileSpy.mockRestore();

    const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
    const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(agentSpan.attributes.map(a => [a.key, a.value.stringValue ?? a.value.intValue]));
    // Unknown engine ID falls back to the raw value for gen_ai.system
    expect(attrs["gen_ai.system"]).toBe("custom-engine");
    expect(attrs["gh-aw.engine.id"]).toBe("custom-engine");
  });

  it("includes gen_ai.response.finish_reasons on the agent span when stop_reason is present in agent-stdio.log", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";

    const startMs = 1_700_000_000_000;
    const endMs = 1_700_000_005_000;
    const statSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: endMs });
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/agent-stdio.log") {
        return '{"type":"result","subtype":"success","num_turns":3,"total_cost_usd":0.5,"stop_reason":"end_turn"}\n';
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    readFileSpy.mockRestore();

    const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
    const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
    expect(agentSpan.name).toBe("gh-aw.agent.agent");
    const finishAttr = agentSpan.attributes.find(a => a.key === "gen_ai.response.finish_reasons");
    expect(finishAttr).toBeDefined();
    expect(finishAttr.value.arrayValue.values).toEqual([{ stringValue: "end_turn" }]);

    const conclusionBody = JSON.parse(mockFetch.mock.calls[1][1].body);
    const conclusionSpan = conclusionBody.resourceSpans[0].scopeSpans[0].spans[0];
    const conclusionFinishAttr = conclusionSpan.attributes.find(a => a.key === "gen_ai.response.finish_reasons");
    expect(conclusionFinishAttr).toBeDefined();
    expect(conclusionFinishAttr.value.arrayValue.values).toEqual([{ stringValue: "end_turn" }]);
  });

  it("includes gen_ai.response.model on agent spans from resolved init model in agent-stdio.log", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";

    const startMs = 1_700_000_000_000;
    const endMs = 1_700_000_005_000;
    const statSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: endMs });
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/aw_info.json") {
        return JSON.stringify({ model: "auto", engine_id: "claude", workflow_name: "otel-advisor" });
      }
      if (filePath === "/tmp/gh-aw/agent-stdio.log") {
        return '[DEBUG] Starting Claude Code CLI\n[{"type":"system","subtype":"init","session_id":"sess-1","model":"claude-sonnet-4-20250514"},{"type":"result","num_turns":3,"total_cost_usd":0.1}]\n';
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    readFileSpy.mockRestore();

    const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
    const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
    const agentAttrs = Object.fromEntries(agentSpan.attributes.map(a => [a.key, a.value.stringValue ?? a.value.intValue]));
    expect(agentAttrs["gen_ai.request.model"]).toBe("auto");
    expect(agentAttrs["gen_ai.response.model"]).toBe("claude-sonnet-4-20250514");

    const conclusionBody = JSON.parse(mockFetch.mock.calls[1][1].body);
    const conclusionSpan = conclusionBody.resourceSpans[0].scopeSpans[0].spans[0];
    const conclusionAttrs = Object.fromEntries(conclusionSpan.attributes.map(a => [a.key, a.value.stringValue ?? a.value.intValue]));
    expect(conclusionAttrs["gen_ai.response.model"]).toBe("claude-sonnet-4-20250514");
  });

  it("emits gen_ai.response.finish_reasons=[unknown] on the agent span when stop_reason is absent from agent-stdio.log", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";

    const startMs = 1_700_000_000_000;
    const endMs = 1_700_000_005_000;
    const statSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: endMs });
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/agent-stdio.log") {
        return '{"type":"result","num_turns":2,"total_cost_usd":0.25}\n';
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    readFileSpy.mockRestore();

    const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
    const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
    expect(agentSpan.name).toBe("gh-aw.agent.agent");
    const agentFinishAttr = agentSpan.attributes.find(a => a.key === "gen_ai.response.finish_reasons");
    expect(agentFinishAttr).toBeDefined();
    expect(agentFinishAttr.value.arrayValue.values).toEqual([{ stringValue: "unknown" }]);

    const conclusionBody = JSON.parse(mockFetch.mock.calls[1][1].body);
    const conclusionSpan = conclusionBody.resourceSpans[0].scopeSpans[0].spans[0];
    const conclusionFinishAttr = conclusionSpan.attributes.find(a => a.key === "gen_ai.response.finish_reasons");
    expect(conclusionFinishAttr).toBeDefined();
    expect(conclusionFinishAttr.value.arrayValue.values).toEqual([{ stringValue: "unknown" }]);
  });

  it("includes gen_ai.response.finish_reasons with max_tokens on the agent span when truncated", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";

    const startMs = 1_700_000_000_000;
    const endMs = 1_700_000_005_000;
    const statSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: endMs });
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/agent-stdio.log") {
        return '{"type":"result","subtype":"error","num_turns":10,"total_cost_usd":2.0,"stop_reason":"max_tokens"}\n';
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    readFileSpy.mockRestore();

    const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
    const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
    expect(agentSpan.name).toBe("gh-aw.agent.agent");
    const finishAttr = agentSpan.attributes.find(a => a.key === "gen_ai.response.finish_reasons");
    expect(finishAttr).toBeDefined();
    expect(finishAttr.value.arrayValue.values).toEqual([{ stringValue: "max_tokens" }]);
  });

  it("emits gen_ai.response.finish_reasons=[timeout] when agent is timed_out and stop_reason is absent", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";
    process.env.GH_AW_AGENT_CONCLUSION = "timed_out";

    const startMs = 1_700_000_000_000;
    const statSpy = vi.spyOn(fs, "statSync").mockImplementation(() => {
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    readFileSpy.mockRestore();

    // Both agent span and conclusion span should carry finish_reasons=["timeout"]
    const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
    const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
    expect(agentSpan.name).toBe("gh-aw.agent.agent");
    const agentFinishAttr = agentSpan.attributes.find(a => a.key === "gen_ai.response.finish_reasons");
    expect(agentFinishAttr).toBeDefined();
    expect(agentFinishAttr.value.arrayValue.values).toEqual([{ stringValue: "timeout" }]);

    const conclusionBody = JSON.parse(mockFetch.mock.calls[1][1].body);
    const conclusionSpan = conclusionBody.resourceSpans[0].scopeSpans[0].spans[0];
    const conclusionFinishAttr = conclusionSpan.attributes.find(a => a.key === "gen_ai.response.finish_reasons");
    expect(conclusionFinishAttr).toBeDefined();
    expect(conclusionFinishAttr.value.arrayValue.values).toEqual([{ stringValue: "timeout" }]);
  });

  it("prefers engine stop_reason over synthetic timeout when stop_reason is present in agent-stdio.log", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";
    process.env.GH_AW_AGENT_CONCLUSION = "timed_out";

    const startMs = 1_700_000_000_000;
    const endMs = 1_700_000_005_000;
    const statSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: endMs });
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/agent-stdio.log") {
        return '{"type":"result","num_turns":5,"stop_reason":"max_tokens"}\n';
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    readFileSpy.mockRestore();

    const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
    const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
    const agentFinishAttr = agentSpan.attributes.find(a => a.key === "gen_ai.response.finish_reasons");
    expect(agentFinishAttr).toBeDefined();
    // Engine-reported reason wins over the synthetic "timeout" fallback
    expect(agentFinishAttr.value.arrayValue.values).toEqual([{ stringValue: "max_tokens" }]);
  });

  it("includes gen_ai.request.model on the conclusion span when model is set in aw_info.json", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/aw_info.json") {
        return JSON.stringify({ model: "claude-3-5-sonnet-20241022", engine_id: "claude" });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.job.conclusion");
    readFileSpy.mockRestore();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.name).toBe("gh-aw.job.conclusion");
    const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.stringValue ?? a.value.intValue]));
    expect(attrs["gen_ai.system"]).toBe("anthropic");
    expect(attrs["gen_ai.request.model"]).toBe("claude-3-5-sonnet-20241022");
  });

  it("omits gen_ai.request.model from the conclusion span when model is absent in aw_info.json", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/aw_info.json") {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.job.conclusion");
    readFileSpy.mockRestore();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.name).toBe("gh-aw.job.conclusion");
    const keys = span.attributes.map(a => a.key);
    expect(keys).not.toContain("gen_ai.system");
    expect(keys).not.toContain("gen_ai.request.model");
  });

  it("emits gh-aw.detection.conclusion and gh-aw.detection.reason when both are set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GH_AW_DETECTION_CONCLUSION = "warning";
    process.env.GH_AW_DETECTION_REASON = "threat_detected";

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.stringValue ?? a.value.intValue]));
    expect(attrs["gh-aw.detection.conclusion"]).toBe("warning");
    expect(attrs["gh-aw.detection.reason"]).toBe("threat_detected");
  });

  it("emits gh-aw.detection.conclusion without gh-aw.detection.reason when reason is absent", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GH_AW_DETECTION_CONCLUSION = "failure";

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const keys = span.attributes.map(a => a.key);
    expect(keys).toContain("gh-aw.detection.conclusion");
    expect(keys).not.toContain("gh-aw.detection.reason");
  });

  it("omits gh-aw.detection.conclusion and gh-aw.detection.reason when neither env var is set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const keys = span.attributes.map(a => a.key);
    expect(keys).not.toContain("gh-aw.detection.conclusion");
    expect(keys).not.toContain("gh-aw.detection.reason");
  });

  it("emits gh-aw.failure.categories as an array attribute when failure_categories.json exists", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === FAILURE_CATEGORIES_PATH) {
        return JSON.stringify(["agent_failure", "inference_access_error"]);
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    try {
      await sendJobConclusionSpan("gh-aw.job.conclusion");
    } finally {
      readFileSpy.mockRestore();
    }

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const attr = span.attributes.find(a => a.key === "gh-aw.failure.categories");
    expect(attr).toBeDefined();
    expect(attr.value.arrayValue.values.map(v => v.stringValue)).toEqual(["agent_failure", "inference_access_error"]);
  });

  it("omits gh-aw.failure.categories when failure_categories.json does not exist", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    fs.rmSync(FAILURE_CATEGORIES_PATH, { force: true });

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const keys = span.attributes.map(a => a.key);
    expect(keys).not.toContain("gh-aw.failure.categories");
  });

  it("omits gh-aw.failure.categories when failure_categories.json is an empty array", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === FAILURE_CATEGORIES_PATH) {
        return JSON.stringify([]);
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    try {
      await sendJobConclusionSpan("gh-aw.job.conclusion");
    } finally {
      readFileSpy.mockRestore();
    }

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const keys = span.attributes.map(a => a.key);
    expect(keys).not.toContain("gh-aw.failure.categories");
  });

  it("includes gh-aw.run.attempt attribute from GITHUB_RUN_ATTEMPT env var", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_RUN_ATTEMPT = "3";

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.stringValue]));
    expect(attrs["gh-aw.run.attempt"]).toBe("3");
  });

  it("defaults gh-aw.run.attempt to '1' when neither awInfo nor GITHUB_RUN_ATTEMPT is set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.stringValue]));
    expect(attrs["gh-aw.run.attempt"]).toBe("1");
  });

  it("reads gh-aw.workflow.name from aw_info.json when present", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GH_AW_INFO_WORKFLOW_NAME = "env-workflow";
    process.env.GITHUB_WORKFLOW = "github-workflow";

    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/aw_info.json") {
        return JSON.stringify({ workflow_name: "aw-info-workflow" });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.job.conclusion");
    readFileSpy.mockRestore();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.stringValue]));
    expect(attrs["gh-aw.workflow.name"]).toBe("aw-info-workflow");
  });

  it("falls back to GH_AW_INFO_WORKFLOW_NAME when aw_info.json is absent", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GH_AW_INFO_WORKFLOW_NAME = "env-workflow";
    process.env.GITHUB_WORKFLOW = "github-workflow";

    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/aw_info.json") {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.job.conclusion");
    readFileSpy.mockRestore();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.stringValue]));
    expect(attrs["gh-aw.workflow.name"]).toBe("env-workflow");
  });

  it("falls back to GITHUB_WORKFLOW when aw_info.json and GH_AW_INFO_WORKFLOW_NAME are absent", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_WORKFLOW = "github-workflow";

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.stringValue]));
    expect(attrs["gh-aw.workflow.name"]).toBe("github-workflow");
  });

  it("sets gh-aw.workflow.name to empty string when all sources are absent", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.stringValue]));
    expect(attrs["gh-aw.workflow.name"]).toBe("");
  });

  it("includes gh-aw.aic when GH_AW_AIC is set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GH_AW_AIC = "0.125";
    process.env.INPUT_JOB_NAME = "agent";

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const aicAttr = span.attributes.find(a => a.key === "gh-aw.aic");
    expect(aicAttr).toBeDefined();
    expect(aicAttr.value.doubleValue).toBe(0.125);
  });

  it("emits usage activity attributes on the built-in conclusion span", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "conclusion";

    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/usage/activity/summary.json") {
        return JSON.stringify({
          integrity: { total_filtered: 3 },
          firewall: { total_requests: 12, allowed_requests: 10, blocked_requests: 2 },
          gateway: {
            total_calls: 5,
            failed_calls: 1,
            total_input_size: 1000,
            max_input_size: 400,
            total_output_size: 5000,
            max_output_size: 3000,
          },
          working_set: {
            measurement_state: "measured",
            rebuild_factor: 3.9017857142857144,
            cumulative_input_tokens: 874000,
            peak_input_tokens: 224000,
            rebuild_excess_tokens: 650000,
            invocations: 5,
          },
        });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.conclusion.conclusion");
    readFileSpy.mockRestore();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(span.attributes.map(attribute => [attribute.key, attribute.value]));
    expect(attrs["gh-aw.integrity.filtered_events"].intValue).toBe(3);
    expect(attrs["gh-aw.firewall.requests"].intValue).toBe(12);
    expect(attrs["gh-aw.firewall.allowed_requests"].intValue).toBe(10);
    expect(attrs["gh-aw.firewall.blocked_requests"].intValue).toBe(2);
    expect(attrs["gh-aw.mcp.tool_calls"].intValue).toBe(5);
    expect(attrs["gh-aw.mcp.failed_tool_calls"].intValue).toBe(1);
    expect(attrs["gh-aw.mcp.input_bytes"].intValue).toBe(1000);
    expect(attrs["gh-aw.mcp.max_input_bytes"].intValue).toBe(400);
    expect(attrs["gh-aw.mcp.output_bytes"].intValue).toBe(5000);
    expect(attrs["gh-aw.mcp.max_output_bytes"].intValue).toBe(3000);
    expect(attrs["gh-aw.working_set.measurement_state"].stringValue).toBe("measured");
    expect(attrs["gh-aw.working_set.rebuild_factor"].doubleValue).toBeCloseTo(3.9017857142857144);
    expect(attrs["gh-aw.working_set.cumulative_input_tokens"].intValue).toBe(874000);
    expect(attrs["gh-aw.working_set.peak_input_tokens"].intValue).toBe(224000);
    expect(attrs["gh-aw.working_set.rebuild_excess_tokens"].intValue).toBe(650000);
    expect(attrs["gh-aw.working_set.invocations"].intValue).toBe(5);
  });

  it("emits working-set token counters that exceed Number.MAX_SAFE_INTEGER on high-volume runs", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "conclusion";

    const hugeCumulativeInputTokens = Number.MAX_SAFE_INTEGER + 10_000;
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/usage/activity/summary.json") {
        return JSON.stringify({
          working_set: {
            measurement_state: "measured",
            rebuild_factor: 4.2,
            cumulative_input_tokens: hugeCumulativeInputTokens,
            peak_input_tokens: 224000,
            rebuild_excess_tokens: hugeCumulativeInputTokens - 224000,
            invocations: 50000,
          },
        });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.conclusion.conclusion");
    readFileSpy.mockRestore();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(span.attributes.map(attribute => [attribute.key, attribute.value]));
    expect(attrs["gh-aw.working_set.cumulative_input_tokens"]).toBeDefined();
    expect(attrs["gh-aw.working_set.cumulative_input_tokens"].intValue).toBe(hugeCumulativeInputTokens);
    expect(attrs["gh-aw.working_set.rebuild_excess_tokens"]).toBeDefined();
    expect(attrs["gh-aw.working_set.invocations"].intValue).toBe(50000);
  });

  it("emits only working-set measurement state when unavailable", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "conclusion";

    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/usage/activity/summary.json") {
        return JSON.stringify({
          working_set: {
            measurement_state: "unavailable",
            cumulative_input_tokens: 0,
            peak_input_tokens: 0,
            rebuild_excess_tokens: 0,
            invocations: 0,
          },
        });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.conclusion.conclusion");
    readFileSpy.mockRestore();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const keys = span.attributes.map(attribute => attribute.key);
    expect(keys).toContain("gh-aw.working_set.measurement_state");
    expect(keys).not.toContain("gh-aw.working_set.rebuild_factor");
    expect(keys).not.toContain("gh-aw.working_set.cumulative_input_tokens");
  });

  it("includes gh-aw.aic when INPUT_JOB_NAME is missing but span name is gh-aw.agent.conclusion", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GH_AW_AIC = "0.125";
    delete process.env.INPUT_JOB_NAME;

    await sendJobConclusionSpan("gh-aw.agent.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const aicAttr = span.attributes.find(a => a.key === "gh-aw.aic");
    expect(aicAttr).toBeDefined();
    expect(aicAttr.value.doubleValue).toBe(0.125);
  });

  it("emits gh-aw.max_ai_credits as a numeric conclusion-span attribute when available", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GH_AW_MAX_AI_CREDITS = "1000.5";

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const maxAICreditsAttr = span.attributes.find(a => a.key === "gh-aw.max_ai_credits");
    expect(maxAICreditsAttr).toBeDefined();
    expect(maxAICreditsAttr.value.doubleValue).toBe(1000.5);
  });

  it("emits AI credits boolean cap/rate-limit attributes on conclusion spans when detected", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    const stdioContent = Buffer.from("CAPIError: 429 Maximum AI credits exceeded (1002.381900 / 1000).", "utf8");
    const stdioLogPath = "/tmp/gh-aw/agent-stdio.log";
    const MOCK_FD = 42;
    const existsSpy = vi.spyOn(fs, "existsSync").mockImplementation(p => p === stdioLogPath);
    const statSpy = vi.spyOn(fs, "statSync").mockImplementation(p => {
      if (p === stdioLogPath) return /** @type {fs.Stats} */ { size: stdioContent.length };
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });
    const openSpy = vi.spyOn(fs, "openSync").mockReturnValue(/** @type {number} */ MOCK_FD);
    const readSpy = vi.spyOn(fs, "readSync").mockImplementation((_fd, buf) => {
      stdioContent.copy(/** @type {Buffer} */ buf);
      return stdioContent.length;
    });
    const closeSpy = vi.spyOn(fs, "closeSync").mockImplementation(() => {});

    try {
      await sendJobConclusionSpan("gh-aw.job.conclusion");
    } finally {
      existsSpy.mockRestore();
      statSpy.mockRestore();
      openSpy.mockRestore();
      readSpy.mockRestore();
      closeSpy.mockRestore();
    }

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.boolValue ?? a.value.doubleValue ?? a.value.intValue ?? a.value.stringValue]));
    expect(attrs["gh-aw.max_ai_credits_exceeded"]).toBe(true);
    expect(attrs["gh-aw.ai_credits_rate_limit_error"]).toBe(true);
  });

  it("does not emit gh-aw.max_ai_credits when max AI credits is missing or invalid", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GH_AW_MAX_AI_CREDITS = "not-a-number";

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const keys = span.attributes.map(a => a.key);
    expect(keys).not.toContain("gh-aw.max_ai_credits");
  });

  it("emits dashboard metrics and aliases on the conclusion span", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GH_AW_AGENT_CONCLUSION = "timed_out";
    process.env.GH_AW_DETECTION_CONCLUSION = "warning";
    process.env.GH_AW_TRACKER_ID = "copilot-token-optimizer";
    process.env.INPUT_JOB_NAME = "agent";

    const startMs = 1_700_000_000_000;
    const endMs = 1_700_000_120_000;
    const statSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: endMs });
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/aw_info.json") {
        return JSON.stringify({ context: { item_number: "42" } });
      }
      if (filePath === "/tmp/gh-aw/agent_output.json") {
        return JSON.stringify({ errors: [{ message: "first" }, { message: "second" }] });
      }
      if (filePath === "/tmp/gh-aw/agent-stdio.log") {
        return '[WARN] first warning\nnpm warn second warning\n{"type":"result","num_turns":7,"total_cost_usd":1.75}\n';
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    readFileSpy.mockRestore();

    const conclusionBody = JSON.parse(mockFetch.mock.calls[1][1].body);
    const conclusionSpan = conclusionBody.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(conclusionSpan.attributes.map(a => [a.key, a.value.intValue ?? a.value.doubleValue ?? a.value.stringValue ?? a.value.boolValue]));

    expect(attrs["gh-aw.action_minutes"]).toBeGreaterThan(0);
    expect(attrs["gh-aw.turns"]).toBe(7);
    expect(attrs["gh-aw.error_count"]).toBe(2);
    expect(attrs["gh-aw.warning_count"]).toBe(3);
    expect(attrs["gh-aw.permission_denied_count"]).toBe(0);
    expect(attrs["gh-aw.steering_event_count"]).toBe(0);
    expect(attrs["gh-aw.run.status"]).toBe("timeout");
    expect(attrs["gh-aw.tracker.id"]).toBe("copilot-token-optimizer");
  });

  it("emits gh-aw.permission_denied_count from structured result data in agent-stdio.log", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";

    const startMs = 1_700_000_000_000;
    const endMs = 1_700_000_005_000;
    const statSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: endMs });
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/agent-stdio.log") {
        return '{"type":"result","num_turns":4,"permission_denials":["cmd1","cmd2","cmd3"]}\n';
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    readFileSpy.mockRestore();

    const conclusionBody = JSON.parse(mockFetch.mock.calls[1][1].body);
    const conclusionSpan = conclusionBody.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(conclusionSpan.attributes.map(a => [a.key, a.value.intValue ?? a.value.doubleValue ?? a.value.stringValue ?? a.value.boolValue]));
    expect(attrs["gh-aw.permission_denied_count"]).toBe(3);
  });

  it("emits gh-aw.permission_denied_count from structured numeric result data in agent-stdio.log", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";

    const startMs = 1_700_000_000_000;
    const endMs = 1_700_000_005_000;
    const statSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: endMs });
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/agent-stdio.log") {
        return '{"type":"result","num_turns":4,"permission_denied_count":5}\n';
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    readFileSpy.mockRestore();

    const conclusionBody = JSON.parse(mockFetch.mock.calls[1][1].body);
    const conclusionSpan = conclusionBody.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(conclusionSpan.attributes.map(a => [a.key, a.value.intValue ?? a.value.doubleValue ?? a.value.stringValue ?? a.value.boolValue]));
    expect(attrs["gh-aw.permission_denied_count"]).toBe(5);
  });

  it("falls back to text-pattern counting for gh-aw.permission_denied_count when structured fields are absent", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";

    const startMs = 1_700_000_000_000;
    const endMs = 1_700_000_005_000;
    const statSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: endMs });
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/agent-stdio.log") {
        return ["[ERROR] Permission denied while running tool", "EACCES: permission denied", "EPERM operation not permitted", "permission denied count: 7", "permissions denied counts: 4", '{"type":"result","num_turns":2}'].join("\n");
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    readFileSpy.mockRestore();

    const conclusionBody = JSON.parse(mockFetch.mock.calls[1][1].body);
    const conclusionSpan = conclusionBody.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(conclusionSpan.attributes.map(a => [a.key, a.value.intValue ?? a.value.doubleValue ?? a.value.stringValue ?? a.value.boolValue]));
    expect(attrs["gh-aw.permission_denied_count"]).toBe(3);
  });

  it("emits gh-aw.steering_event_count from api-proxy event logs JSONL", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";

    const startMs = 1_700_000_000_000;
    const endMs = 1_700_000_005_000;
    const eventLogPath = "/tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/event-logs.jsonl";
    const statSpy = vi.spyOn(fs, "statSync").mockImplementation(filePath => {
      if (filePath === eventLogPath) {
        return /** @type {Partial<fs.Stats>} */ { size: 1024 };
      }
      if (filePath === "/tmp/gh-aw/agent_output.json") {
        return /** @type {Partial<fs.Stats>} */ { mtimeMs: endMs };
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === eventLogPath) {
        return [
          '{"event":"token_steering","request_id":"r1"}',
          '{"type":"token_steering","request_id":"r2"}',
          '{"event":"steering","request_id":"r3"}',
          '{"event":"TOKEN_STEERING","request_id":"r4"}',
          '{"event":"request","request_id":"r5"}',
          '{"payload":{"event":"token_steering"},"request_id":"r6"}',
          '{"payload":{"type":"model_steering"},"request_id":"r7"}',
          "not-json",
        ].join("\n");
      }
      if (filePath === "/tmp/gh-aw/agent-stdio.log") {
        return '{"type":"result","num_turns":2}\n';
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    readFileSpy.mockRestore();

    const conclusionBody = JSON.parse(mockFetch.mock.calls[1][1].body);
    const conclusionSpan = conclusionBody.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(conclusionSpan.attributes.map(a => [a.key, a.value.intValue ?? a.value.doubleValue ?? a.value.stringValue ?? a.value.boolValue]));
    expect(attrs["gh-aw.steering_event_count"]).toBe(6);
  });

  it("falls back to second api-proxy log path when first is absent", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";

    const startMs = 1_700_000_000_000;
    const endMs = 1_700_000_005_000;
    const fallbackLogPath = "/tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/events.jsonl";
    const statSpy = vi.spyOn(fs, "statSync").mockImplementation(filePath => {
      if (filePath === fallbackLogPath) {
        return /** @type {Partial<fs.Stats>} */ { size: 512 };
      }
      if (filePath === "/tmp/gh-aw/agent_output.json") {
        return /** @type {Partial<fs.Stats>} */ { mtimeMs: endMs };
      }
      // First api-proxy log path and any other paths are treated as absent.
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === fallbackLogPath) {
        return '{"event":"token_steering"}\n';
      }
      if (filePath === "/tmp/gh-aw/agent-stdio.log") {
        return '{"type":"result","num_turns":1}\n';
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });

    statSpy.mockRestore();
    readFileSpy.mockRestore();

    const conclusionBody = JSON.parse(mockFetch.mock.calls[1][1].body);
    const conclusionSpan = conclusionBody.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(conclusionSpan.attributes.map(a => [a.key, a.value.intValue ?? a.value.doubleValue ?? a.value.stringValue ?? a.value.boolValue]));
    expect(attrs["gh-aw.steering_event_count"]).toBe(1);
  });

  it("emits gh-aw.otlp.export_errors on the conclusion job span", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/otlp-export-errors.count") {
        return "3";
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.conclusion.conclusion");
    readFileSpy.mockRestore();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.intValue ?? a.value.stringValue]));
    expect(attrs["gh-aw.otlp.export_errors"]).toBe(3);
  });

  it("emits gh-aw.otlp.export_error_details on the conclusion job span", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/otlp-export-errors.count") {
        return "2";
      }
      if (filePath === "/tmp/gh-aw/otlp-export-errors.jsonl") {
        return [JSON.stringify({ host: "sentry.example.com:4318", status: 401, reason: "Unauthorized" }), "{not-json", JSON.stringify({ host: "grafana.example.com:4318", status: 503, reason: "Service Unavailable" })].join("\n") + "\n";
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.conclusion.conclusion");
    readFileSpy.mockRestore();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.intValue ?? a.value.stringValue]));
    expect(attrs["gh-aw.otlp.export_error_details"]).toBe("sentry.example.com:4318 status=401 reason=Unauthorized | grafana.example.com:4318 status=503 reason=Service Unavailable");
  });

  it("summarizes OTLP export error details before they exceed the attribute limit", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    const longReason = "x".repeat(700);

    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/otlp-export-errors.count") {
        return "3";
      }
      if (filePath === "/tmp/gh-aw/otlp-export-errors.jsonl") {
        return (
          [
            JSON.stringify({ host: "first.example.com:4318", status: 500, reason: longReason }),
            JSON.stringify({ host: "second.example.com:4318", status: 503, reason: longReason }),
            JSON.stringify({ host: "third.example.com:4318", status: 504, reason: longReason }),
          ].join("\n") + "\n"
        );
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.conclusion.conclusion");
    readFileSpy.mockRestore();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.intValue ?? a.value.stringValue]));
    expect(attrs["gh-aw.otlp.export_error_details"].length).toBeLessThanOrEqual(1024);
    expect(attrs["gh-aw.otlp.export_error_details"]).toContain("… (+");
    expect(attrs["gh-aw.otlp.export_error_details"]).toContain("first.example.com:4318 status=500 reason=");
  });

  it("emits gh-aw.otlp.export_errors on non-conclusion job spans", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/otlp-export-errors.count") {
        return "2";
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion");
    readFileSpy.mockRestore();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.intValue ?? a.value.stringValue]));
    expect(attrs["gh-aw.otlp.export_errors"]).toBe(2);
  });

  it("never emits effective_tokens and emits gh-aw.aic when GH_AW_EFFECTIVE_TOKENS is set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GH_AW_EFFECTIVE_TOKENS = "9800";
    process.env.GH_AW_AIC = "0.125";
    process.env.INPUT_JOB_NAME = "agent";

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const keys = span.attributes.map(a => a.key);
    const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.intValue ?? a.value.doubleValue ?? a.value.stringValue]));
    expect(keys).not.toContain("gh-aw.effective_tokens");
    expect(attrs["gh-aw.aic"]).toBe(0.125);
  });

  it("uses GH_AW_INFO_VERSION as scope version when aw_info.json is absent", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GH_AW_INFO_VERSION = "v2.0.0";

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    expect(body.resourceSpans[0].scopeSpans[0].scope.version).toBe("v2.0.0");
  });

  it("falls back to GH_AW_INFO_CLI_VERSION for scope version when agent and workflow versions are absent", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    // GH_AW_INFO_VERSION absent — simulates a custom engine with no default version
    process.env.GH_AW_INFO_CLI_VERSION = "v3.1.0";

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    expect(body.resourceSpans[0].scopeSpans[0].scope.version).toBe("v3.1.0");
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "service.version", value: { stringValue: "v3.1.0" } });
    const spanAttrs = body.resourceSpans[0].scopeSpans[0].spans[0].attributes;
    expect(spanAttrs).toContainEqual({ key: "gh-aw.cli.version", value: { stringValue: "v3.1.0" } });
  });

  it("prefers cli_version from aw_info.json over agent_version for scope version", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GH_AW_INFO_CLI_VERSION = "v3.1.0";

    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/aw_info.json") {
        return JSON.stringify({ cli_version: "v3.2.0", agent_version: "2.1.150" });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    readFileSpy.mockRestore();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    expect(body.resourceSpans[0].scopeSpans[0].scope.version).toBe("v3.2.0");
  });

  it("uses GITHUB_AW_OTEL_TRACE_ID from env as trace ID (1 trace per run)", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_AW_OTEL_TRACE_ID = "f".repeat(32);

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.traceId).toBe("f".repeat(32));
  });

  it("uses GITHUB_AW_OTEL_PARENT_SPAN_ID as parentSpanId (1 parent span per job)", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    const parentSpanId = "abcdef1234567890";
    process.env.GITHUB_AW_OTEL_PARENT_SPAN_ID = parentSpanId;

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.parentSpanId).toBe(parentSpanId);
  });

  it("omits parentSpanId when GITHUB_AW_OTEL_PARENT_SPAN_ID is absent", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.parentSpanId).toBeUndefined();
  });

  it("normalizes uppercase GITHUB_AW_OTEL_TRACE_ID to lowercase", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_AW_OTEL_TRACE_ID = "F".repeat(32); // uppercase — should be normalised

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.traceId).toBe("f".repeat(32));
  });

  it("includes github.repository and github.run_id as resource attributes", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_REPOSITORY = "owner/repo";
    process.env.GITHUB_RUN_ID = "987654321";

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "github.repository", value: { stringValue: "owner/repo" } });
    expect(resourceAttrs).toContainEqual({ key: "github.run_id", value: { stringValue: "987654321" } });
  });

  it("includes github.run_attempt as resource attribute from GITHUB_RUN_ATTEMPT", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_RUN_ATTEMPT = "2";

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "github.run_attempt", value: { stringValue: "2" } });
  });

  it("defaults github.run_attempt resource attribute to '1' when GITHUB_RUN_ATTEMPT is not set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    delete process.env.GITHUB_RUN_ATTEMPT;

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "github.run_attempt", value: { stringValue: "1" } });
  });

  it("includes runner.* and github.actor_id as resource attributes when available", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_ACTOR_ID = "4175913";
    process.env.RUNNER_OS = "Linux";
    process.env.RUNNER_ARCH = "X64";
    process.env.RUNNER_NAME = "GitHub Actions 1187452382";
    process.env.RUNNER_ENVIRONMENT = "github-hosted";

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "github.actor_id", value: { stringValue: "4175913" } });
    expect(resourceAttrs).toContainEqual({ key: "runner.os", value: { stringValue: "Linux" } });
    expect(resourceAttrs).toContainEqual({ key: "runner.arch", value: { stringValue: "X64" } });
    expect(resourceAttrs).toContainEqual({ key: "runner.name", value: { stringValue: "GitHub Actions 1187452382" } });
    expect(resourceAttrs).toContainEqual({ key: "runner.environment", value: { stringValue: "github-hosted" } });
  });

  it("includes github.event_name as resource attribute when GITHUB_EVENT_NAME is set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_EVENT_NAME = "pull_request";

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "github.event_name", value: { stringValue: "pull_request" } });
  });

  it("omits github.event_name resource attribute when GITHUB_EVENT_NAME is not set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    const resourceKeys = resourceAttrs.map(a => a.key);
    expect(resourceKeys).not.toContain("github.event_name");
  });

  it("includes gh-aw.event_name as span attribute when GITHUB_EVENT_NAME is set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_EVENT_NAME = "pull_request";

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.attributes).toContainEqual({ key: "gh-aw.event_name", value: { stringValue: "pull_request" } });
  });

  it("omits gh-aw.event_name span attribute when GITHUB_EVENT_NAME is not set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const keys = span.attributes.map(a => a.key);
    expect(keys).not.toContain("gh-aw.event_name");
  });

  it("includes github.ref as resource attribute when GITHUB_REF is set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_REF = "refs/heads/main";

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "github.ref", value: { stringValue: "refs/heads/main" } });
  });

  it("includes github.ref_name and github.head_ref as resource attributes when set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_REF_NAME = "123/merge";
    process.env.GITHUB_HEAD_REF = "feature-branch";

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "github.ref_name", value: { stringValue: "123/merge" } });
    expect(resourceAttrs).toContainEqual({ key: "github.head_ref", value: { stringValue: "feature-branch" } });
  });

  it("omits github.ref resource attribute when GITHUB_REF is not set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    const resourceKeys = resourceAttrs.map(a => a.key);
    expect(resourceKeys).not.toContain("github.ref");
  });

  it("omits github.ref_name and github.head_ref resource attributes when not set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    const resourceKeys = resourceAttrs.map(a => a.key);
    expect(resourceKeys).not.toContain("github.ref_name");
    expect(resourceKeys).not.toContain("github.head_ref");
  });

  it("includes github.sha as resource attribute when GITHUB_SHA is set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_SHA = "abc1234567890def";

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "github.sha", value: { stringValue: "abc1234567890def" } });
  });

  it("omits github.sha resource attribute when GITHUB_SHA is not set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    const resourceKeys = resourceAttrs.map(a => a.key);
    expect(resourceKeys).not.toContain("github.sha");
  });

  it("includes github.actions.run_url as resource and span attributes when repository and run_id are set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_REPOSITORY = "owner/repo";
    process.env.GITHUB_RUN_ID = "987654321";
    delete process.env.GITHUB_SERVER_URL;

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    const spanAttrs = body.resourceSpans[0].scopeSpans[0].spans[0].attributes;
    expect(resourceAttrs).toContainEqual({
      key: "github.actions.run_url",
      value: { stringValue: "https://github.com/owner/repo/actions/runs/987654321" },
    });
    expect(spanAttrs).toContainEqual({
      key: "github.actions.run_url",
      value: { stringValue: "https://github.com/owner/repo/actions/runs/987654321" },
    });
  });

  it("uses GITHUB_SERVER_URL for github.actions.run_url in sendJobConclusionSpan", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_REPOSITORY = "owner/repo";
    process.env.GITHUB_RUN_ID = "987654321";
    process.env.GITHUB_SERVER_URL = "https://github.example.com";

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    const spanAttrs = body.resourceSpans[0].scopeSpans[0].spans[0].attributes;
    expect(resourceAttrs).toContainEqual({
      key: "github.actions.run_url",
      value: { stringValue: "https://github.example.com/owner/repo/actions/runs/987654321" },
    });
    expect(spanAttrs).toContainEqual({
      key: "github.actions.run_url",
      value: { stringValue: "https://github.example.com/owner/repo/actions/runs/987654321" },
    });
  });

  it("omits github.actions.run_url when repository or run_id is missing in sendJobConclusionSpan", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    delete process.env.GITHUB_REPOSITORY;
    delete process.env.GITHUB_RUN_ID;

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    const resourceKeys = resourceAttrs.map(a => a.key);
    const spanAttrs = body.resourceSpans[0].scopeSpans[0].spans[0].attributes;
    const spanKeys = spanAttrs.map(a => a.key);
    expect(resourceKeys).not.toContain("github.actions.run_url");
    expect(spanKeys).not.toContain("github.actions.run_url");
  });

  it("includes service.version resource attribute when version is known", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GH_AW_INFO_VERSION = "v3.0.0";

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "service.version", value: { stringValue: "v3.0.0" } });
  });

  it("falls back to GITHUB_SHA for service.version on conclusion span when no gh-aw version is available", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    // No GH_AW_INFO_VERSION, GH_AW_INFO_CLI_VERSION, or aw_info.json version fields
    process.env.GITHUB_SHA = "deadbeef1234567890abcdef1234567890abcdef";

    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.job.conclusion");
    readFileSpy.mockRestore();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "service.version", value: { stringValue: "deadbeef1234567890abcdef1234567890abcdef" } });
    expect(body.resourceSpans[0].scopeSpans[0].scope.version).toBe("deadbeef1234567890abcdef1234567890abcdef");
  });

  describe("agent_output.json error enrichment", () => {
    let readFileSpy;

    beforeEach(() => {
      readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
    });

    afterEach(() => {
      readFileSpy.mockRestore();
    });

    it("adds gh-aw.error.count and gh-aw.error.messages attributes when agent_output.json has errors on failure", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "failure";

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return JSON.stringify({ errors: [{ message: "Rate limit exceeded" }, { message: "Tool call failed" }] });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = span.attributes;
      const errorCount = attrs.find(a => a.key === "gh-aw.error.count");
      const errorMessages = attrs.find(a => a.key === "gh-aw.error.messages");
      expect(errorCount).toBeDefined();
      expect(errorCount.value.intValue).toBe(2);
      expect(errorMessages).toBeDefined();
      expect(errorMessages.value.stringValue).toBe("Rate limit exceeded | Tool call failed");
    });

    it("adds gh-aw.error attributes when agent_output.json has errors on success", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "success";

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return JSON.stringify({ errors: [{ message: "partial failure one" }, { message: "partial failure two" }] });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = span.attributes;
      expect(span.status.code).toBe(1);
      expect(attrs).toContainEqual({ key: "gh-aw.error.count", value: { intValue: 2 } });
      expect(attrs).toContainEqual({ key: "gh-aw.error.messages", value: { stringValue: "partial failure one | partial failure two" } });
      expect(span.events).toHaveLength(2);
      expect(span.events[0].name).toBe("exception");
      expect(span.events[0].attributes).toContainEqual({ key: "exception.type", value: { stringValue: "gh-aw.AgentError" } });
      expect(span.events[0].attributes).toContainEqual({ key: "exception.message", value: { stringValue: "partial failure one" } });
    });

    it("enriches statusMessage with the first error message on failure", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "failure";

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return JSON.stringify({ errors: [{ message: "Rate limit exceeded on claude-3-5-sonnet" }] });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.status.message).toBe("agent failure: Rate limit exceeded on claude-3-5-sonnet");
    });

    it("enriches statusMessage with the first error message on timed_out", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "timed_out";

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return JSON.stringify({ errors: [{ message: "Execution exceeded 30 minute limit" }] });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.status.message).toBe("agent timed_out: Execution exceeded 30 minute limit");
    });

    it("sets gh-aw.run.status=timeout (not failure) when conclusion is timed_out", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "timed_out";

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.stringValue ?? a.value.intValue]));
      // Distinct "timeout" value allows queries that separate timeouts from generic failures.
      expect(attrs["gh-aw.run.status"]).toBe("timeout");
      // OTLP status.code must still be ERROR so the span is treated as a failure by backends.
      expect(span.status.code).toBe(2);
      expect(span.status.message).toBe("agent timed_out");
    });

    it("marks cancelled conclusion spans as errors", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "cancelled";

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.status.code).toBe(2);
      expect(span.status.message).toBe("agent cancelled");
    });

    it("caps error messages at 5 entries", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "failure";

      const manyErrors = [1, 2, 3, 4, 5, 6, 7].map(i => ({ message: `Error ${i}` }));
      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return JSON.stringify({ errors: manyErrors });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const errorMessages = span.attributes.find(a => a.key === "gh-aw.error.messages");
      expect(errorMessages).toBeDefined();
      expect(errorMessages.value.stringValue).toBe("Error 1 | Error 2 | Error 3 | Error 4 | Error 5");
      // gh-aw.error.count reflects the full error count (7), not the capped count
      const errorCount = span.attributes.find(a => a.key === "gh-aw.error.count");
      expect(errorCount.value.intValue).toBe(7);
    });

    it("truncates statusMessage to 256 characters", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "failure";

      const longMessage = "x".repeat(300);
      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return JSON.stringify({ errors: [{ message: longMessage }] });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.status.message.length).toBe(256);
      expect(span.status.message.startsWith("agent failure: ")).toBe(true);
    });

    it("does not add error attributes when agent_output.json has no errors array", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "failure";

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return JSON.stringify({ items: [] });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const keys = span.attributes.map(a => a.key);
      expect(keys).not.toContain("gh-aw.error.count");
      expect(keys).not.toContain("gh-aw.error.messages");
      expect(span.status.message).toBe("agent failure");
    });

    it("reads agent_output.json and adds output metrics when agent conclusion is success", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "success";
      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return JSON.stringify({
            items: [{ type: "pull_request" }, { type: "issue" }, { type: "pull_request" }, {}],
          });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const agentOutputCalls = readFileSpy.mock.calls.filter(([p]) => p === "/tmp/gh-aw/agent_output.json");
      expect(agentOutputCalls).toHaveLength(1);
      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = span.attributes;
      expect(attrs).toContainEqual({ key: "gh-aw.output.item_count", value: { intValue: 4 } });
      expect(attrs).toContainEqual({ key: "gh-aw.output.item_types", value: { stringValue: "issue,pull_request" } });
    });

    it("does not add error attributes when agent_output.json is absent on failure", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "failure";

      // readFileSpy already throws ENOENT for all paths (set in beforeEach)

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const keys = span.attributes.map(a => a.key);
      expect(keys).not.toContain("gh-aw.error.count");
      expect(keys).not.toContain("gh-aw.error.messages");
      expect(span.status.message).toBe("agent failure");
    });

    it("emits one exception span event per error on agent failure", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "failure";

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return JSON.stringify({ errors: [{ message: "Rate limit exceeded" }, { message: "Tool call failed" }] });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.events).toHaveLength(2);
      expect(span.events[0].name).toBe("exception");
      expect(span.events[0].attributes).toContainEqual({ key: "exception.type", value: { stringValue: "gh-aw.AgentError" } });
      expect(span.events[0].attributes).toContainEqual({ key: "exception.message", value: { stringValue: "Rate limit exceeded" } });
      expect(span.events[1].name).toBe("exception");
      expect(span.events[1].attributes).toContainEqual({ key: "exception.type", value: { stringValue: "gh-aw.AgentError" } });
      expect(span.events[1].attributes).toContainEqual({ key: "exception.message", value: { stringValue: "Tool call failed" } });
    });

    it("truncates exception.message to 1024 characters", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "failure";

      const longMessage = "x".repeat(2000);
      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return JSON.stringify({ errors: [{ message: longMessage }] });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.events).toHaveLength(1);
      const msg = span.events[0].attributes.find(a => a.key === "exception.message");
      expect(msg.value.stringValue.length).toBe(1024);
    });

    it("does not emit exception events when agent conclusion is success", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "success";

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.events).toBeUndefined();
    });

    it("emits a synthetic failure exception event when agent_output.json is absent", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "failure";

      // readFileSpy already throws ENOENT for all paths (set in beforeEach)

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.events).toHaveLength(1);
      expect(span.events[0].name).toBe("exception");
      expect(span.events[0].attributes).toContainEqual({ key: "exception.type", value: { stringValue: "gh-aw.AgentFailed" } });
      expect(span.events[0].attributes).toContainEqual({ key: "exception.message", value: { stringValue: "agent failure" } });
    });

    it("emits a synthetic failure exception event when agent_output.json is unreadable", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "failure";

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return "{";
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.events).toHaveLength(1);
      expect(span.events[0].name).toBe("exception");
      expect(span.events[0].attributes).toContainEqual({ key: "exception.type", value: { stringValue: "gh-aw.AgentFailed" } });
      expect(span.events[0].attributes).toContainEqual({ key: "exception.message", value: { stringValue: "agent failure" } });
    });

    it("emits a synthetic timeout exception event when agent_output.json is absent", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "timed_out";

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.events).toHaveLength(1);
      expect(span.events[0].name).toBe("exception");
      expect(span.events[0].attributes).toContainEqual({ key: "exception.type", value: { stringValue: "gh-aw.AgentTimedOut" } });
      expect(span.events[0].attributes).toContainEqual({ key: "exception.message", value: { stringValue: "agent timed_out" } });
    });

    it("emits a synthetic cancelled exception event when agent_output.json is absent", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "cancelled";

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.events).toHaveLength(1);
      expect(span.events[0].name).toBe("exception");
      expect(span.events[0].attributes).toContainEqual({ key: "exception.type", value: { stringValue: "gh-aw.AgentCancelled" } });
      expect(span.events[0].attributes).toContainEqual({ key: "exception.message", value: { stringValue: "agent cancelled" } });
    });

    it("emits exception events for all errors (not capped at 5 like error messages attribute)", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "failure";

      const manyErrors = [1, 2, 3, 4, 5, 6, 7].map(i => ({ message: `Error ${i}` }));
      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return JSON.stringify({ errors: manyErrors });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.events).toHaveLength(7);
      for (let i = 0; i < 7; i++) {
        expect(span.events[i].name).toBe("exception");
        expect(span.events[i].attributes).toContainEqual({ key: "exception.type", value: { stringValue: "gh-aw.AgentError" } });
        expect(span.events[i].attributes).toContainEqual({ key: "exception.message", value: { stringValue: `Error ${i + 1}` } });
      }
    });

    it("sets valid timeUnixNano on each exception event", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "failure";

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return JSON.stringify({ errors: [{ message: "test error" }] });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.events).toHaveLength(1);
      expect(span.events[0].timeUnixNano).toMatch(/^\d+$/);
    });

    it("extracts exception.type from colon-prefixed error messages", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "failure";

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return JSON.stringify({ errors: [{ message: "push_to_pull_request_branch:Cannot push to remote" }] });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.events).toHaveLength(1);
      const typeAttr = span.events[0].attributes.find(a => a.key === "exception.type");
      expect(typeAttr.value.stringValue).toBe("gh-aw.push_to_pull_request_branch");
      const msgAttr = span.events[0].attributes.find(a => a.key === "exception.message");
      expect(msgAttr.value.stringValue).toBe("Cannot push to remote");
    });

    it("normalizes uppercase exception.type prefix to lowercase", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "failure";

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return JSON.stringify({ errors: [{ message: "Push_To_PR:Cannot push to remote" }] });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const typeAttr = span.events[0].attributes.find(a => a.key === "exception.type");
      expect(typeAttr.value.stringValue).toBe("gh-aw.push_to_pr");
      const msgAttr = span.events[0].attributes.find(a => a.key === "exception.message");
      expect(msgAttr.value.stringValue).toBe("Cannot push to remote");
    });

    it("falls back to gh-aw.AgentError when message has no colon prefix", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "failure";

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return JSON.stringify({ errors: [{ message: "Something went wrong" }] });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.events).toHaveLength(1);
      const typeAttr = span.events[0].attributes.find(a => a.key === "exception.type");
      expect(typeAttr.value.stringValue).toBe("gh-aw.AgentError");
      const msgAttr = span.events[0].attributes.find(a => a.key === "exception.message");
      expect(msgAttr.value.stringValue).toBe("Something went wrong");
    });

    it("falls back to gh-aw.AgentError when colon prefix contains invalid characters", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "failure";

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return JSON.stringify({ errors: [{ message: "Error with spaces:details here" }] });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const typeAttr = span.events[0].attributes.find(a => a.key === "exception.type");
      expect(typeAttr.value.stringValue).toBe("gh-aw.AgentError");
      // Full original message kept when type extraction fails
      const msgAttr = span.events[0].attributes.find(a => a.key === "exception.message");
      expect(msgAttr.value.stringValue).toBe("Error with spaces:details here");
    });

    it("falls back to gh-aw.AgentError when colon prefix exceeds 64 characters", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "failure";

      const longPrefix = "a".repeat(65);
      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return JSON.stringify({ errors: [{ message: `${longPrefix}:some error` }] });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const typeAttr = span.events[0].attributes.find(a => a.key === "exception.type");
      expect(typeAttr.value.stringValue).toBe("gh-aw.AgentError");
    });
  });

  describe("run.status fallback from observable error signals", () => {
    let readFileSpy;

    beforeEach(() => {
      readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
    });

    afterEach(() => {
      readFileSpy.mockRestore();
    });

    it("sets gh-aw.run.status=failure and STATUS_CODE_ERROR when conclusion is absent but outputErrors exist", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      // GH_AW_AGENT_CONCLUSION intentionally not set (simulates agent job post-step)

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return JSON.stringify({ errors: [{ message: "push_to_pull_request_branch:push failed" }] });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.intValue ?? a.value.stringValue ?? a.value.boolValue]));
      expect(attrs["gh-aw.run.status"]).toBe("failure");
      expect(span.status.code).toBe(2);
      expect(span.status.message).toBe("errors detected: push_to_pull_request_branch:push failed");
    });

    it("includes the first error message in statusMessage when conclusion is absent", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return JSON.stringify({ errors: [{ message: "Rate limit exceeded" }, { message: "second error" }] });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.status.message).toBe("errors detected: Rate limit exceeded");
    });

    it("does not override explicit GH_AW_AGENT_CONCLUSION=success even when outputErrors exist", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      process.env.GH_AW_AGENT_CONCLUSION = "success";

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return JSON.stringify({ errors: [{ message: "non-fatal warning error" }] });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.intValue ?? a.value.stringValue ?? a.value.boolValue]));
      expect(attrs["gh-aw.run.status"]).toBe("success");
      expect(span.status.code).toBe(1);
    });

    it("keeps gh-aw.run.status=success and STATUS_CODE_OK when conclusion is absent and there are no outputErrors", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
      // No GH_AW_AGENT_CONCLUSION and no errors

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return JSON.stringify({ items: [{ type: "pull_request" }] });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.intValue ?? a.value.stringValue ?? a.value.boolValue]));
      expect(attrs["gh-aw.run.status"]).toBe("success");
      expect(span.status.code).toBe(1);
    });

    it("sets gh-aw.run.status=failure and STATUS_CODE_ERROR when conclusion is absent, error_count=0, and execution exit code is non-zero", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_output.json") {
          return JSON.stringify({ items: [{ type: "pull_request" }] });
        }
        if (filePath === "/tmp/gh-aw/agent_execution_exit_code.txt") {
          return "22";
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.intValue ?? a.value.stringValue ?? a.value.boolValue]));
      expect(attrs["gh-aw.run.status"]).toBe("failure");
      expect(attrs["gh-aw.error_count"]).toBe(0);
      expect(attrs["gh-aw.agent.execution.exit_code"]).toBe(22);
      expect(span.status.code).toBe(2);
      expect(span.status.message).toBe("copilot cli exited with code 22");
    });
  });

  describe("rate-limit enrichment in conclusion span", () => {
    let readFileSpy;

    beforeEach(() => {
      readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
    });

    afterEach(() => {
      readFileSpy.mockRestore();
    });

    it("includes rate-limit attributes when github_rate_limits.jsonl has entries", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      const entry = { timestamp: "2026-04-05T09:00:00.000Z", source: "response_headers", operation: "issues.get", resource: "core", limit: 5000, remaining: 4823, used: 177, reset: "2026-04-05T09:30:00.000Z" };
      readFileSpy.mockImplementation(filePath => {
        if (filePath === GITHUB_RATE_LIMITS_JSONL_PATH) {
          return JSON.stringify(entry) + "\n";
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.intValue ?? a.value.stringValue]));
      expect(attrs["gh-aw.github.rate_limit.remaining"]).toBe(4823);
      expect(attrs["gh-aw.github.rate_limit.limit"]).toBe(5000);
      expect(attrs["gh-aw.github.rate_limit.used"]).toBe(177);
      expect(attrs["gh-aw.github.rate_limit.resource"]).toBe("core");
      expect(attrs["gh-aw.github.rate_limit.reset"]).toBe("2026-04-05T09:30:00.000Z");
    });

    it("uses the last entry when the file contains multiple lines", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      const first = { resource: "core", limit: 5000, remaining: 4900, used: 100 };
      const last = { resource: "core", limit: 5000, remaining: 4500, used: 500 };
      readFileSpy.mockImplementation(filePath => {
        if (filePath === GITHUB_RATE_LIMITS_JSONL_PATH) {
          return JSON.stringify(first) + "\n" + JSON.stringify(last) + "\n";
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.intValue ?? a.value.stringValue]));
      expect(attrs["gh-aw.github.rate_limit.remaining"]).toBe(4500);
      expect(attrs["gh-aw.github.rate_limit.used"]).toBe(500);
    });

    it("omits rate-limit attributes when github_rate_limits.jsonl is absent", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      // readFileSpy already throws ENOENT for all paths

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const keys = span.attributes.map(a => a.key);
      expect(keys).not.toContain("gh-aw.github.rate_limit.remaining");
      expect(keys).not.toContain("gh-aw.github.rate_limit.limit");
      expect(keys).not.toContain("gh-aw.github.rate_limit.used");
      expect(keys).not.toContain("gh-aw.github.rate_limit.resource");
      expect(keys).not.toContain("gh-aw.github.rate_limit.reset");
    });

    it("omits reset attribute when entry has no reset field", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      const entry = { resource: "core", limit: 5000, remaining: 4823, used: 177 };
      readFileSpy.mockImplementation(filePath => {
        if (filePath === GITHUB_RATE_LIMITS_JSONL_PATH) {
          return JSON.stringify(entry) + "\n";
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.intValue ?? a.value.stringValue]));
      expect(attrs["gh-aw.github.rate_limit.remaining"]).toBe(4823);
      expect(Object.keys(attrs)).not.toContain("gh-aw.github.rate_limit.reset");
    });

    it("omits rate-limit attributes when the file contains only invalid JSON", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === GITHUB_RATE_LIMITS_JSONL_PATH) {
          return "not valid json\n";
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const keys = span.attributes.map(a => a.key);
      expect(keys).not.toContain("gh-aw.github.rate_limit.remaining");
    });
  });

  describe("token breakdown enrichment in agent span", () => {
    let readFileSpy;
    let statSpy;

    beforeEach(() => {
      process.env.INPUT_JOB_NAME = "agent";
      const agentEndMs = 1_700_000_005_000;
      statSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: agentEndMs });
      readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
    });

    afterEach(() => {
      readFileSpy.mockRestore();
      statSpy.mockRestore();
    });

    it("includes all four gen_ai token breakdown attributes on the agent span when agent_usage.json is present", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      const usage = { input_tokens: 48200, output_tokens: 1350, cache_read_tokens: 41000, cache_write_tokens: 3100, effective_tokens: 9800, ai_credits: 0.125 };
      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_usage.json") {
          return JSON.stringify(usage);
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs: 1_700_000_000_000 });

      const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
      const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = Object.fromEntries(agentSpan.attributes.map(a => [a.key, a.value.intValue ?? a.value.doubleValue ?? a.value.stringValue]));
      expect(attrs["gen_ai.usage.input_tokens"]).toBe(48200);
      expect(attrs["gen_ai.usage.output_tokens"]).toBe(1350);
      expect(attrs["gen_ai.usage.cache_read.input_tokens"]).toBe(41000);
      expect(attrs["gen_ai.usage.cache_creation.input_tokens"]).toBe(3100);
      // total_tokens = input + output (cache tokens excluded per OTel GenAI spec)
      expect(attrs["gen_ai.usage.total_tokens"]).toBe(48200 + 1350);
      expect(attrs["gh-aw.aic"]).toBe(0.125);
    });

    it("normalizes string token counters from agent_usage.json on the agent span", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      const usage = { input_tokens: "48200", output_tokens: "1350", cache_read_tokens: "41000", cache_write_tokens: "3100" };
      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_usage.json") {
          return JSON.stringify(usage);
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs: 1_700_000_000_000 });

      const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
      const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = Object.fromEntries(agentSpan.attributes.map(a => [a.key, a.value.intValue ?? a.value.stringValue]));
      expect(attrs["gen_ai.usage.input_tokens"]).toBe(48200);
      expect(attrs["gen_ai.usage.output_tokens"]).toBe(1350);
      expect(attrs["gen_ai.usage.cache_read.input_tokens"]).toBe(41000);
      expect(attrs["gen_ai.usage.cache_creation.input_tokens"]).toBe(3100);
      expect(attrs["gen_ai.usage.total_tokens"]).toBe(48200 + 1350);
    });

    it("omits all gen_ai token breakdown attributes when agent_usage.json is absent", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      // readFileSpy already throws ENOENT for all paths

      await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs: 1_700_000_000_000 });

      const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
      const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
      const keys = agentSpan.attributes.map(a => a.key);
      expect(keys).not.toContain("gen_ai.usage.input_tokens");
      expect(keys).not.toContain("gen_ai.usage.output_tokens");
      expect(keys).not.toContain("gen_ai.usage.cache_read.input_tokens");
      expect(keys).not.toContain("gen_ai.usage.cache_creation.input_tokens");
      expect(keys).not.toContain("gen_ai.usage.total_tokens");
    });

    it("omits a gen_ai token attribute when its value is zero", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      const usage = { input_tokens: 1000, output_tokens: 0, cache_read_tokens: 500, cache_write_tokens: 0 };
      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_usage.json") {
          return JSON.stringify(usage);
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs: 1_700_000_000_000 });

      const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
      const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = Object.fromEntries(agentSpan.attributes.map(a => [a.key, a.value.intValue ?? a.value.stringValue]));
      expect(attrs["gen_ai.usage.input_tokens"]).toBe(1000);
      expect(attrs["gen_ai.usage.cache_read.input_tokens"]).toBe(500);
      // total_tokens = input(1000) + output(0) = 1000 (non-zero so it IS emitted)
      expect(attrs["gen_ai.usage.total_tokens"]).toBe(1000);
      const keys = agentSpan.attributes.map(a => a.key);
      expect(keys).not.toContain("gen_ai.usage.output_tokens");
      expect(keys).not.toContain("gen_ai.usage.cache_creation.input_tokens");
    });

    it("omits gen_ai token breakdown attributes when agent_usage.json contains invalid JSON", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_usage.json") {
          return "not valid json";
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs: 1_700_000_000_000 });

      const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
      const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
      const keys = agentSpan.attributes.map(a => a.key);
      expect(keys).not.toContain("gen_ai.usage.input_tokens");
      expect(keys).not.toContain("gen_ai.usage.output_tokens");
      expect(keys).not.toContain("gen_ai.usage.cache_read.input_tokens");
      expect(keys).not.toContain("gen_ai.usage.cache_creation.input_tokens");
    });

    // CI guardrail: gh-aw.aic must be emitted as a numeric attribute alongside
    // gh-aw.workflow.name so per-workflow AIC rollups work in Sentry EAP even
    // when the total is zero (zero != no-data for observability purposes).
    it("emits gh-aw.aic as a numeric zero and co-locates it with gh-aw.workflow.name on the agent span when ai_credits is 0", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      // Simulate parse_token_usage.cjs writing ai_credits: 0 (firewall proxy had no AIC data).
      const usage = { input_tokens: 5000, output_tokens: 200, cache_read_tokens: 100, cache_write_tokens: 50, ai_credits: 0 };
      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_usage.json") {
          return JSON.stringify(usage);
        }
        if (filePath === "/tmp/gh-aw/aw_info.json") {
          return JSON.stringify({ workflow_name: "my-workflow" });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs: 1_700_000_000_000 });

      const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
      const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = Object.fromEntries(agentSpan.attributes.map(a => [a.key, a.value.intValue ?? a.value.doubleValue ?? a.value.stringValue ?? a.value.boolValue]));
      // gh-aw.aic must be present as a numeric 0 — not absent — so EAP indexes it as number.
      expect(attrs["gh-aw.aic"]).toBe(0);
      expect(typeof attrs["gh-aw.aic"]).toBe("number");
      // gh-aw.workflow.name must appear on the same span for per-workflow rollups.
      expect(attrs["gh-aw.workflow.name"]).toBe("my-workflow");
    });

    it("prefers engine-reported ai_credits from agent-stdio.log over a zero from agent_usage.json", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      // Firewall proxy reports ai_credits: 0, but the engine result event carries 0.42.
      const fileUsage = { input_tokens: 5000, output_tokens: 200, ai_credits: 0 };
      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_usage.json") {
          return JSON.stringify(fileUsage);
        }
        if (filePath === "/tmp/gh-aw/agent-stdio.log") {
          return '{"type":"result","num_turns":3,"usage":{"input_tokens":5000,"output_tokens":200,"ai_credits":0.42}}\n';
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs: 1_700_000_000_000 });

      const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
      const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = Object.fromEntries(agentSpan.attributes.map(a => [a.key, a.value.intValue ?? a.value.doubleValue ?? a.value.stringValue ?? a.value.boolValue]));
      // Engine-reported 0.42 must win over the file's 0.
      expect(attrs["gh-aw.aic"]).toBe(0.42);
    });

    it("emits gh-aw.aic as numeric 0 when both agent_usage.json and agent-stdio.log are absent", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      // readFileSpy already throws ENOENT for all paths

      await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs: 1_700_000_000_000 });

      const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
      const agentSpan = agentBody.resourceSpans[0].scopeSpans[0].spans[0];
      const aicAttr = agentSpan.attributes.find(a => a.key === "gh-aw.aic");
      // gh-aw.aic must be present as numeric 0 even when no data is available so
      // Sentry EAP indexes the field as numeric (not string) and Tempo can query
      // { span."gh-aw.aic" > 0 } without a manual schema override.
      expect(aicAttr).toBeDefined();
      // gh-aw.aic is declared as type `double` in the OTel spec; use doubleValue
      // (not intValue) so Sentry EAP consistently indexes it as float even when
      // the value is 0, enabling sum()/avg()/p95() rollups without schema overrides.
      expect(aicAttr.value.doubleValue).toBe(0);
      expect(typeof aicAttr.value.doubleValue).toBe("number");
    });
  });

  describe("token breakdown enrichment in conclusion span", () => {
    let readFileSpy;
    let statSpy;

    beforeEach(() => {
      process.env.INPUT_JOB_NAME = "agent";
      const agentEndMs = 1_700_000_005_000;
      statSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: agentEndMs });
      readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
    });

    afterEach(() => {
      readFileSpy.mockRestore();
      statSpy.mockRestore();
      delete process.env.GH_AW_AGENT_CONCLUSION;
    });

    it("omits all gen_ai token breakdown attributes from the conclusion span when agent sub-span is emitted", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      const usage = { input_tokens: 48200, output_tokens: 1350, cache_read_tokens: 41000, cache_write_tokens: 3100, effective_tokens: 9800 };
      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_usage.json") {
          return JSON.stringify(usage);
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs: 1_700_000_000_000 });

      // mockFetch.mock.calls[0] is the agent span, [1] is the conclusion span
      const conclusionBody = JSON.parse(mockFetch.mock.calls[1][1].body);
      const conclusionSpan = conclusionBody.resourceSpans[0].scopeSpans[0].spans[0];
      const keys = conclusionSpan.attributes.map(a => a.key);
      expect(keys).not.toContain("gen_ai.usage.input_tokens");
      expect(keys).not.toContain("gen_ai.usage.output_tokens");
      expect(keys).not.toContain("gen_ai.usage.cache_read.input_tokens");
      expect(keys).not.toContain("gen_ai.usage.cache_creation.input_tokens");
    });

    it("includes gen_ai token breakdown on agent conclusion span when no agent sub-span is emitted", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      // Keep INPUT_JOB_NAME=agent but prevent dedicated agent sub-span by
      // making agent_output.json absent and not setting startMs so
      // hasDedicatedAgentSpan is false.
      process.env.INPUT_JOB_NAME = "agent";
      statSpy.mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      const usage = { input_tokens: 5000, output_tokens: 200, cache_read_tokens: 100, cache_write_tokens: 50, effective_tokens: 500, ai_credits: 0.125 };
      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_usage.json") {
          return JSON.stringify(usage);
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.agent.conclusion");

      // Only one fetch call: the conclusion span (no agent sub-span)
      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.intValue ?? a.value.stringValue]));
      expect(attrs["gen_ai.usage.input_tokens"]).toBe(5000);
      expect(attrs["gen_ai.usage.output_tokens"]).toBe(200);
      expect(attrs["gen_ai.usage.cache_read.input_tokens"]).toBe(100);
      expect(attrs["gen_ai.usage.cache_creation.input_tokens"]).toBe(50);
      // total_tokens also falls through to the conclusion span
      expect(attrs["gen_ai.usage.total_tokens"]).toBe(5000 + 200);
    });

    it("omits gen_ai token breakdown from non-agent job conclusion span even when agent_usage.json is present", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      // Simulate a downstream job (e.g. conclusion, detection, safe_outputs) that
      // has agent_usage.json on disk via artifact download but should NOT emit tokens.
      // Compiled workflows also propagate GH_AW_EFFECTIVE_TOKENS and GH_AW_AIC from
      // needs.agent.outputs.* — set them here to guard against unconditional env reads.
      process.env.INPUT_JOB_NAME = "conclusion";
      process.env.GH_AW_EFFECTIVE_TOKENS = "500";
      process.env.GH_AW_AIC = "0.05";
      statSpy.mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      const usage = { input_tokens: 5000, output_tokens: 200, cache_read_tokens: 100, cache_write_tokens: 50, effective_tokens: 500 };
      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_usage.json") {
          return JSON.stringify(usage);
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.conclusion.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const keys = span.attributes.map(a => a.key);
      expect(keys).not.toContain("gen_ai.usage.input_tokens");
      expect(keys).not.toContain("gen_ai.usage.output_tokens");
      expect(keys).not.toContain("gen_ai.usage.cache_read.input_tokens");
      expect(keys).not.toContain("gen_ai.usage.cache_creation.input_tokens");
      expect(keys).not.toContain("gen_ai.usage.total_tokens");
      expect(keys).not.toContain("gh-aw.effective_tokens");
      expect(keys).not.toContain("gh-aw.aic");
    });

    it("includes detection-job token breakdown and cost attributes when detection writes agent_usage.json", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.INPUT_JOB_NAME = "detection";
      process.env.GH_AW_DETECTION_CONCLUSION = "success";
      statSpy.mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      const usage = { input_tokens: 5000, output_tokens: 200, cache_read_tokens: 100, ai_credits: 0.125 };
      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_usage.json") {
          return JSON.stringify(usage);
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.detection.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const attrs = Object.fromEntries(body.resourceSpans[0].scopeSpans[0].spans[0].attributes.map(a => [a.key, a.value.intValue ?? a.value.doubleValue ?? a.value.stringValue]));
      expect(attrs["gen_ai.usage.input_tokens"]).toBe(5000);
      expect(attrs["gen_ai.usage.output_tokens"]).toBe(200);
      expect(attrs["gen_ai.usage.cache_read.input_tokens"]).toBe(100);
      expect(attrs["gen_ai.usage.total_tokens"]).toBe(5200);
      expect(attrs["gh-aw.aic"]).toBe(0.125);
      expect(attrs["gh-aw.detection.conclusion"]).toBe("success");
    });

    it("includes engine-job token breakdown and cost attributes when the job name matches the engine id", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.INPUT_JOB_NAME = "copilot";
      process.env.GH_AW_INFO_ENGINE_ID = "copilot";
      statSpy.mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      const usage = { input_tokens: 5000, output_tokens: 200, cache_read_tokens: 100, ai_credits: 0.125 };
      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_usage.json") {
          return JSON.stringify(usage);
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.copilot.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const attrs = Object.fromEntries(body.resourceSpans[0].scopeSpans[0].spans[0].attributes.map(a => [a.key, a.value.intValue ?? a.value.doubleValue ?? a.value.stringValue]));
      expect(attrs["gen_ai.usage.input_tokens"]).toBe(5000);
      expect(attrs["gen_ai.usage.output_tokens"]).toBe(200);
      expect(attrs["gen_ai.usage.cache_read.input_tokens"]).toBe(100);
      expect(attrs["gen_ai.usage.total_tokens"]).toBe(5200);
      expect(attrs["gh-aw.aic"]).toBe(0.125);
    });

    it("includes detection-job warning result attribute when detection finds threats", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.INPUT_JOB_NAME = "detection";
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";
      process.env.GH_AW_DETECTION_REASON = "threat_detected";
      statSpy.mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.detection.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const attrs = Object.fromEntries(body.resourceSpans[0].scopeSpans[0].spans[0].attributes.map(a => [a.key, a.value.intValue ?? a.value.doubleValue ?? a.value.stringValue]));
      expect(attrs["gh-aw.detection.conclusion"]).toBe("warning");
      expect(attrs["gh-aw.detection.reason"]).toBe("threat_detected");
    });

    it("includes skipped detection result attribute on detection span when detection was not needed", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.INPUT_JOB_NAME = "detection";
      process.env.GH_AW_DETECTION_CONCLUSION = "skipped";
      statSpy.mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.detection.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const attrs = Object.fromEntries(body.resourceSpans[0].scopeSpans[0].spans[0].attributes.map(a => [a.key, a.value.intValue ?? a.value.doubleValue ?? a.value.stringValue]));
      expect(attrs["gh-aw.detection.conclusion"]).toBe("skipped");
    });

    it("omits gen_ai token breakdown when INPUT_JOB_NAME is empty", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      // Empty/missing INPUT_JOB_NAME should not emit tokens — regression guard
      // for the pre-fix behavior where any job with agent_usage.json would emit.
      delete process.env.INPUT_JOB_NAME;
      statSpy.mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      const usage = { input_tokens: 5000, output_tokens: 200 };
      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_usage.json") {
          return JSON.stringify(usage);
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const keys = body.resourceSpans[0].scopeSpans[0].spans[0].attributes.map(a => a.key);
      expect(keys).not.toContain("gen_ai.usage.input_tokens");
      expect(keys).not.toContain("gen_ai.usage.output_tokens");
      expect(keys).not.toContain("gen_ai.usage.total_tokens");
    });

    it("attaches tokens to dedicated agent span on failure when agent_cli_start_ms.txt exists", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      // Agent failed but agent_cli_start_ms.txt exists → agentEndMs uses nowMs()
      // fallback → hasDedicatedAgentSpan is true → tokens go to sub-span, not conclusion.
      process.env.INPUT_JOB_NAME = "agent";
      process.env.GH_AW_AGENT_CONCLUSION = "failure";
      statSpy.mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      const usage = { input_tokens: 3000, output_tokens: 150 };
      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_cli_start_ms.txt") {
          return "1700000001000";
        }
        if (filePath === "/tmp/gh-aw/agent_usage.json") {
          return JSON.stringify(usage);
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.agent.conclusion");

      // Two calls: [0] = dedicated agent span, [1] = conclusion span
      expect(mockFetch.mock.calls.length).toBe(2);

      // Dedicated agent span has tokens
      const agentBody = JSON.parse(mockFetch.mock.calls[0][1].body);
      const agentAttrs = Object.fromEntries(agentBody.resourceSpans[0].scopeSpans[0].spans[0].attributes.map(a => [a.key, a.value.intValue ?? a.value.stringValue]));
      expect(agentAttrs["gen_ai.usage.input_tokens"]).toBe(3000);
      expect(agentAttrs["gen_ai.usage.output_tokens"]).toBe(150);
      expect(agentAttrs["gen_ai.usage.total_tokens"]).toBe(3150);

      // Conclusion span does NOT have tokens
      const conclusionBody = JSON.parse(mockFetch.mock.calls[1][1].body);
      const conclusionKeys = conclusionBody.resourceSpans[0].scopeSpans[0].spans[0].attributes.map(a => a.key);
      expect(conclusionKeys).not.toContain("gen_ai.usage.input_tokens");
      expect(conclusionKeys).not.toContain("gen_ai.usage.total_tokens");
    });

    it("falls back tokens to agent conclusion span on failure when agent_cli_start_ms.txt is absent", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      // Agent failed and agent_cli_start_ms.txt is absent → agentStartMs is undefined
      // → hasDedicatedAgentSpan is false → tokens fall through to agent conclusion.
      process.env.INPUT_JOB_NAME = "agent";
      process.env.GH_AW_AGENT_CONCLUSION = "failure";
      statSpy.mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      const usage = { input_tokens: 3000, output_tokens: 150 };
      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_usage.json") {
          return JSON.stringify(usage);
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.agent.conclusion");

      // Only one call: conclusion span (no dedicated span without agentStartMs)
      expect(mockFetch.mock.calls.length).toBe(1);
      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const attrs = Object.fromEntries(body.resourceSpans[0].scopeSpans[0].spans[0].attributes.map(a => [a.key, a.value.intValue ?? a.value.stringValue]));
      expect(attrs["gen_ai.usage.input_tokens"]).toBe(3000);
      expect(attrs["gen_ai.usage.output_tokens"]).toBe(150);
      expect(attrs["gen_ai.usage.total_tokens"]).toBe(3150);
    });

    it("falls back to agent-stdio.log usage when agent_usage.json is absent", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.INPUT_JOB_NAME = "agent";
      process.env.GH_AW_AGENT_CONCLUSION = "failure";
      statSpy.mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent-stdio.log") {
          return '{"type":"result","num_turns":3,"usage":{"input_tokens":48200,"output_tokens":1350,"cache_read_input_tokens":41000,"cache_creation_input_tokens":3100}}\n';
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.agent.conclusion");

      expect(mockFetch.mock.calls.length).toBe(1);
      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const attrs = Object.fromEntries(body.resourceSpans[0].scopeSpans[0].spans[0].attributes.map(a => [a.key, a.value.intValue ?? a.value.stringValue]));
      expect(attrs["gen_ai.usage.input_tokens"]).toBe(48200);
      expect(attrs["gen_ai.usage.output_tokens"]).toBe(1350);
      expect(attrs["gen_ai.usage.cache_read.input_tokens"]).toBe(41000);
      expect(attrs["gen_ai.usage.cache_creation.input_tokens"]).toBe(3100);
      expect(attrs["gen_ai.usage.total_tokens"]).toBe(48200 + 1350);
    });

    it("prefers agent_usage.json over agent-stdio.log usage when both are present", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.INPUT_JOB_NAME = "agent";
      process.env.GH_AW_AGENT_CONCLUSION = "failure";
      statSpy.mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_usage.json") {
          return JSON.stringify({ input_tokens: 3000, output_tokens: 150, cache_read_tokens: 200, cache_write_tokens: 75 });
        }
        if (filePath === "/tmp/gh-aw/agent-stdio.log") {
          return '{"type":"result","usage":{"input_tokens":9999,"output_tokens":888,"cache_read_input_tokens":777,"cache_creation_input_tokens":66}}\n';
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.agent.conclusion");

      expect(mockFetch.mock.calls.length).toBe(1);
      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const attrs = Object.fromEntries(body.resourceSpans[0].scopeSpans[0].spans[0].attributes.map(a => [a.key, a.value.intValue ?? a.value.stringValue]));
      expect(attrs["gen_ai.usage.input_tokens"]).toBe(3000);
      expect(attrs["gen_ai.usage.output_tokens"]).toBe(150);
      expect(attrs["gen_ai.usage.cache_read.input_tokens"]).toBe(200);
      expect(attrs["gen_ai.usage.cache_creation.input_tokens"]).toBe(75);
      expect(attrs["gen_ai.usage.total_tokens"]).toBe(3150);
    });

    it("falls back to agent-stdio.log usage when agent_usage.json has no recognized token counters", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.INPUT_JOB_NAME = "agent";
      process.env.GH_AW_AGENT_CONCLUSION = "failure";
      statSpy.mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_usage.json") {
          return JSON.stringify({ model: "claude-3" });
        }
        if (filePath === "/tmp/gh-aw/agent-stdio.log") {
          return '{"type":"result","usage":{"input_tokens":48200,"output_tokens":1350,"cache_read_input_tokens":41000,"cache_creation_input_tokens":3100}}\n';
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.agent.conclusion");

      expect(mockFetch.mock.calls.length).toBe(1);
      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const attrs = Object.fromEntries(body.resourceSpans[0].scopeSpans[0].spans[0].attributes.map(a => [a.key, a.value.intValue ?? a.value.stringValue]));
      expect(attrs["gen_ai.usage.input_tokens"]).toBe(48200);
      expect(attrs["gen_ai.usage.output_tokens"]).toBe(1350);
      expect(attrs["gen_ai.usage.cache_read.input_tokens"]).toBe(41000);
      expect(attrs["gen_ai.usage.cache_creation.input_tokens"]).toBe(3100);
      expect(attrs["gen_ai.usage.total_tokens"]).toBe(48200 + 1350);
    });

    it("omits gen_ai tokens from downstream job even when all agent files are readable from artifact", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      // Realistic production scenario: the 'conclusion' job downloads the full
      // agent artifact to /tmp/gh-aw/ so agent_cli_start_ms.txt, agent_output.json,
      // AND agent_usage.json are all readable.  Despite having enough data to form
      // a dedicated agent span, jobName !== "agent" prevents both sub-span emission
      // and token fallback.  This is the exact scenario that caused 4x inflation.
      process.env.INPUT_JOB_NAME = "conclusion";
      // statSpy already returns valid mtimeMs from beforeEach (agent_output.json exists)
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      const usage = { input_tokens: 48200, output_tokens: 1350, cache_read_tokens: 41000, cache_write_tokens: 3100 };
      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_cli_start_ms.txt") {
          return "1700000001000";
        }
        if (filePath === "/tmp/gh-aw/agent_usage.json") {
          return JSON.stringify(usage);
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.conclusion.conclusion");

      // Only one fetch call — no dedicated agent sub-span despite files being present
      expect(mockFetch.mock.calls.length).toBe(1);
      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const keys = body.resourceSpans[0].scopeSpans[0].spans[0].attributes.map(a => a.key);
      expect(keys).not.toContain("gen_ai.usage.input_tokens");
      expect(keys).not.toContain("gen_ai.usage.output_tokens");
      expect(keys).not.toContain("gen_ai.usage.cache_read.input_tokens");
      expect(keys).not.toContain("gen_ai.usage.cache_creation.input_tokens");
      expect(keys).not.toContain("gen_ai.usage.total_tokens");
    });

    it("omits all gen_ai token breakdown attributes from conclusion span when agent_usage.json is absent", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      // readFileSpy already throws ENOENT for all paths

      await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs: 1_700_000_000_000 });

      // mockFetch.mock.calls[0] is the agent span, [1] is the conclusion span
      const conclusionBody = JSON.parse(mockFetch.mock.calls[1][1].body);
      const conclusionSpan = conclusionBody.resourceSpans[0].scopeSpans[0].spans[0];
      const keys = conclusionSpan.attributes.map(a => a.key);
      expect(keys).not.toContain("gen_ai.usage.input_tokens");
      expect(keys).not.toContain("gen_ai.usage.output_tokens");
      expect(keys).not.toContain("gen_ai.usage.cache_read.input_tokens");
      expect(keys).not.toContain("gen_ai.usage.cache_creation.input_tokens");
      expect(keys).not.toContain("gen_ai.usage.total_tokens");
    });

    it("omits non-zero gen_ai token breakdown attributes from conclusion span when agent sub-span is emitted", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      const usage = { input_tokens: 1000, output_tokens: 0, cache_read_tokens: 500, cache_write_tokens: 0 };
      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/agent_usage.json") {
          return JSON.stringify(usage);
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs: 1_700_000_000_000 });

      // mockFetch.mock.calls[0] is the agent span, [1] is the conclusion span
      const conclusionBody = JSON.parse(mockFetch.mock.calls[1][1].body);
      const conclusionSpan = conclusionBody.resourceSpans[0].scopeSpans[0].spans[0];
      const keys = conclusionSpan.attributes.map(a => a.key);
      expect(keys).not.toContain("gen_ai.usage.input_tokens");
      expect(keys).not.toContain("gen_ai.usage.cache_read.input_tokens");
      expect(keys).not.toContain("gen_ai.usage.output_tokens");
      expect(keys).not.toContain("gen_ai.usage.cache_creation.input_tokens");
      expect(keys).not.toContain("gen_ai.usage.total_tokens");
    });
  });

  it("includes github.workflow_ref as resource attribute when GITHUB_WORKFLOW_REF is set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_WORKFLOW_REF = "owner/repo/.github/workflows/my-workflow.yml@refs/heads/main";

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "github.workflow_ref", value: { stringValue: "owner/repo/.github/workflows/my-workflow.yml@refs/heads/main" } });
  });

  it("omits github.workflow_ref resource attribute when GITHUB_WORKFLOW_REF is not set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    const resourceKeys = resourceAttrs.map(a => a.key);
    expect(resourceKeys).not.toContain("github.workflow_ref");
  });

  it("includes github.job as resource attribute when GITHUB_JOB is set", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GITHUB_JOB = "conclusion";

    await sendJobConclusionSpan("gh-aw.job.conclusion");

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const resourceAttrs = body.resourceSpans[0].resource.attributes;
    expect(resourceAttrs).toContainEqual({ key: "github.job", value: { stringValue: "conclusion" } });
  });

  describe("staged / deployment.environment", () => {
    let readFileSpy;

    beforeEach(() => {
      readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
    });

    afterEach(() => {
      readFileSpy.mockRestore();
    });

    it("sets gh-aw.staged=false and deployment.environment=production when staged is not set", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const stagedAttr = span.attributes.find(a => a.key === "gh-aw.staged");
      expect(stagedAttr).toBeDefined();
      expect(stagedAttr.value.boolValue).toBe(false);

      const resourceAttrs = body.resourceSpans[0].resource.attributes;
      expect(resourceAttrs).toContainEqual({ key: "deployment.environment", value: { stringValue: "production" } });
    });

    it("sets gh-aw.staged=true and deployment.environment=staging when awInfo.staged=true", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/aw_info.json") {
          return JSON.stringify({ staged: true });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const stagedAttr = span.attributes.find(a => a.key === "gh-aw.staged");
      expect(stagedAttr).toBeDefined();
      expect(stagedAttr.value.boolValue).toBe(true);

      const resourceAttrs = body.resourceSpans[0].resource.attributes;
      expect(resourceAttrs).toContainEqual({ key: "deployment.environment", value: { stringValue: "staging" } });
    });

    it("sets gh-aw.staged=false and deployment.environment=production when awInfo.staged=false", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/aw_info.json") {
          return JSON.stringify({ staged: false });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const stagedAttr = span.attributes.find(a => a.key === "gh-aw.staged");
      expect(stagedAttr).toBeDefined();
      expect(stagedAttr.value.boolValue).toBe(false);

      const resourceAttrs = body.resourceSpans[0].resource.attributes;
      expect(resourceAttrs).toContainEqual({ key: "deployment.environment", value: { stringValue: "production" } });
    });

    it("includes gh-aw.awf.version and gh-aw.awmg.version on conclusion span resources from aw_info.json", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/aw_info.json") {
          return JSON.stringify({ awf_version: "v7.8.9-awf", awmg_version: "v10.11.12-awmg" });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const resourceAttrs = body.resourceSpans[0].resource.attributes;
      expect(resourceAttrs).toContainEqual({ key: "gh-aw.awf.version", value: { stringValue: "v7.8.9-awf" } });
      expect(resourceAttrs).toContainEqual({ key: "gh-aw.awmg.version", value: { stringValue: "v10.11.12-awmg" } });
    });
  });

  describe("trigger item context from aw_info.json", () => {
    let readFileSpy;

    beforeEach(() => {
      readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
    });

    afterEach(() => {
      readFileSpy.mockRestore();
    });

    it("emits gh-aw.trigger.item_type and gh-aw.trigger.item_number from aw_info.context", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/aw_info.json") {
          return JSON.stringify({ context: { item_type: "issue", item_number: "7", trigger_label: "" } });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.attributes).toContainEqual({ key: "gh-aw.trigger.item_type", value: { stringValue: "issue" } });
      expect(span.attributes).toContainEqual({ key: "gh-aw.trigger.item_number", value: { stringValue: "7" } });
      const keys = span.attributes.map(a => a.key);
      expect(keys).not.toContain("gh-aw.trigger.label");
      expect(keys).not.toContain("gh-aw.trigger.comment_id");
    });

    it("emits gh-aw.trigger.label when trigger_label is non-empty", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/aw_info.json") {
          return JSON.stringify({ context: { item_type: "pull_request", item_number: "456", trigger_label: "bug" } });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.attributes).toContainEqual({ key: "gh-aw.trigger.item_type", value: { stringValue: "pull_request" } });
      expect(span.attributes).toContainEqual({ key: "gh-aw.trigger.item_number", value: { stringValue: "456" } });
      expect(span.attributes).toContainEqual({ key: "gh-aw.trigger.label", value: { stringValue: "bug" } });
    });

    it("emits gh-aw.trigger.comment_id when comment_id is non-empty", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/aw_info.json") {
          return JSON.stringify({ context: { item_type: "pull_request", item_number: "456", trigger_label: "bug", comment_id: "987654321" } });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.attributes).toContainEqual({ key: "gh-aw.trigger.comment_id", value: { stringValue: "987654321" } });
    });

    it("emits frontmatter source/emoji/body-modified attributes when present", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === "/tmp/gh-aw/aw_info.json") {
          return JSON.stringify({
            frontmatter_source: "github/gh-aw/.github/workflows/example.md@main",
            frontmatter_emoji: "🧪",
            body_modified: false,
          });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      expect(span.attributes).toContainEqual({ key: "gh-aw.frontmatter.source", value: { stringValue: "github/gh-aw/.github/workflows/example.md@main" } });
      expect(span.attributes).toContainEqual({ key: "gh-aw.frontmatter.emoji", value: { stringValue: "🧪" } });
      expect(span.attributes).toContainEqual({ key: "gh-aw.frontmatter.body_modified", value: { boolValue: false } });
    });

    it("omits trigger attributes when aw_info.json is absent", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const keys = span.attributes.map(a => a.key);
      expect(keys).not.toContain("gh-aw.trigger.item_type");
      expect(keys).not.toContain("gh-aw.trigger.item_number");
      expect(keys).not.toContain("gh-aw.trigger.label");
      expect(keys).not.toContain("gh-aw.trigger.comment_id");
    });
  });

  describe("experiment attributes", () => {
    let readFileSpy;

    beforeEach(() => {
      readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
    });

    afterEach(() => {
      readFileSpy.mockRestore();
    });

    it("includes gh-aw.experiment.<name> and gh-aw.experiments attributes in conclusion span", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      readFileSpy.mockImplementation(filePath => {
        if (filePath === EXPERIMENT_ASSIGNMENTS_PATH) {
          return JSON.stringify({ feature: "on", model: "fast" });
        }
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const attrs = Object.fromEntries(span.attributes.map(a => [a.key, a.value.stringValue]));
      expect(attrs["gh-aw.experiment.feature"]).toBe("on");
      expect(attrs["gh-aw.experiment.model"]).toBe("fast");
      expect(attrs["gh-aw.experiments"]).toBe(JSON.stringify({ feature: "on", model: "fast" }));
    });

    it("omits experiment attributes in conclusion span when assignments file is absent", async () => {
      const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      vi.stubGlobal("fetch", mockFetch);

      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

      await sendJobConclusionSpan("gh-aw.job.conclusion");

      const body = JSON.parse(mockFetch.mock.calls[0][1].body);
      const span = body.resourceSpans[0].scopeSpans[0].spans[0];
      const keys = span.attributes.map(a => a.key);
      expect(keys.some(k => k.startsWith("gh-aw.experiment."))).toBe(false);
      expect(keys).not.toContain("gh-aw.experiments");
    });
  });
});

// ---------------------------------------------------------------------------
// buildGraderTelemetry
// ---------------------------------------------------------------------------

describe("buildGraderTelemetry", () => {
  it("builds summary attributes and one event per grader result", () => {
    const telemetry = buildGraderTelemetry(
      {
        results: [
          {
            id: "quality",
            name: "Quality",
            value: 0.75,
            unit: "ratio",
            passed: true,
            status: "pass",
            source: "builtin",
            severity: "info",
            baselineValue: 0.5,
            deltaFromBaseline: 0.25,
          },
          { id: "reliability", name: "Reliability", value: null, passed: false, status: "fail", source: "inline" },
          { id: "broken", status: "error", source: "inline" },
          { id: "missing", status: "unavailable", source: "builtin" },
        ],
      },
      1700000000000
    );

    expect(telemetry.attributes).toEqual([
      buildAttr("gh-aw.graders.count", 4),
      buildAttr("gh-aw.graders.passed", 1),
      buildAttr("gh-aw.graders.failed", 1),
      buildAttr("gh-aw.graders.errors", 1),
      buildAttr("gh-aw.graders.unavailable", 1),
      buildAttr("gh-aw.graders.other", 0),
    ]);
    expect(telemetry.events).toHaveLength(4);
    expect(telemetry.events[0]).toEqual({
      timeUnixNano: toNanoString(1700000000000),
      name: "grader.result",
      attributes: [
        buildAttr("gh-aw.grader.id", "quality"),
        buildAttr("gh-aw.grader.name", "Quality"),
        buildAttr("gh-aw.grader.status", "pass"),
        buildAttr("gh-aw.grader.source", "builtin"),
        buildAttr("gh-aw.grader.unit", "ratio"),
        buildDoubleAttr("gh-aw.grader.value", 0.75),
        buildAttr("gh-aw.grader.passed", true),
        buildAttr("gh-aw.grader.severity", "info"),
        buildDoubleAttr("gh-aw.grader.baseline_value", 0.5),
        buildDoubleAttr("gh-aw.grader.delta_from_baseline", 0.25),
      ],
    });
  });

  it("omits free-form and non-finite grader values", () => {
    const telemetry = buildGraderTelemetry(
      {
        results: [
          {
            id: "custom",
            status: "error",
            value: Number.NaN,
            message: "sensitive message",
            details: "sensitive details",
            error: "sensitive error",
          },
        ],
      },
      1
    );

    const eventKeys = telemetry.events[0].attributes.map(attribute => attribute.key);
    expect(eventKeys).toEqual(["gh-aw.grader.id", "gh-aw.grader.status"]);
    expect(JSON.stringify(telemetry)).not.toContain("sensitive");
  });

  it.each([null, {}, { results: [] }, { results: [null, {}, { id: "" }] }])("returns empty telemetry for output without valid results", output => {
    expect(buildGraderTelemetry(output, 1)).toEqual({ attributes: [], events: [] });
  });

  it("counts unrecognized statuses as other", () => {
    const telemetry = buildGraderTelemetry({ results: [{ id: "skipped", status: "skipped" }] }, 1);
    expect(telemetry.attributes).toContainEqual(buildAttr("gh-aw.graders.other", 1));
  });
});

// ---------------------------------------------------------------------------
// buildEvalTelemetry
// ---------------------------------------------------------------------------

describe("buildEvalTelemetry", () => {
  it("builds summary attributes and one event per eval result", () => {
    const telemetry = buildEvalTelemetry(
      [
        { id: "quality", question: "Sensitive free-form content", answer: " yes ", model: "gpt-5" },
        { id: "tests", answer: "NO" },
        { id: "unknown", answer: "MAYBE" },
      ],
      1700000000000
    );

    expect(telemetry.attributes).toEqual([buildAttr("gh-aw.evals.count", 3), buildAttr("gh-aw.evals.yes", 1), buildAttr("gh-aw.evals.no", 1), buildAttr("gh-aw.evals.unknown", 1)]);
    expect(telemetry.events[0]).toEqual({
      timeUnixNano: toNanoString(1700000000000),
      name: "eval.result",
      attributes: [buildAttr("gh-aw.eval.id", "quality"), buildAttr("gh-aw.eval.answer", "YES"), buildAttr("gh-aw.eval.model", "gpt-5")],
    });
    expect(JSON.stringify(telemetry)).not.toContain("Sensitive free-form content");
  });

  it.each([null, undefined, [], [null, {}, { id: "" }]])("returns empty telemetry without valid results", results => {
    expect(buildEvalTelemetry(results, 1)).toEqual({ attributes: [], events: [] });
  });
});

// ---------------------------------------------------------------------------
// parseOTLPEndpoints
// ---------------------------------------------------------------------------

describe("parseOTLPEndpoints", () => {
  afterEach(() => {
    delete process.env.GH_AW_OTLP_ENDPOINTS;
    delete process.env.OTEL_EXPORTER_OTLP_ENDPOINT;
    delete process.env.OTEL_EXPORTER_OTLP_HEADERS;
  });

  it("returns empty array when no env vars are set", () => {
    const result = parseOTLPEndpoints();
    expect(result).toEqual([]);
  });

  it("returns empty array when only legacy OTEL_EXPORTER_OTLP_ENDPOINT is set (no longer a fallback)", () => {
    process.env.OTEL_EXPORTER_OTLP_ENDPOINT = "https://traces.example.com:4317";
    const result = parseOTLPEndpoints();
    expect(result).toEqual([]);
  });

  it("parses GH_AW_OTLP_ENDPOINTS JSON array with single entry", () => {
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com:4317", headers: "Authorization=Bearer tok" }]);
    const result = parseOTLPEndpoints();
    expect(result).toEqual([{ url: "https://traces.example.com:4317", headers: "Authorization=Bearer tok" }]);
  });

  it("parses GH_AW_OTLP_ENDPOINTS JSON array with multiple entries", () => {
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://primary.example.com:4317", headers: "Authorization=Bearer tok1" }, { url: "https://secondary.example.com:4317" }]);
    const result = parseOTLPEndpoints();
    expect(result).toEqual([{ url: "https://primary.example.com:4317", headers: "Authorization=Bearer tok1" }, { url: "https://secondary.example.com:4317" }]);
  });

  it("filters out entries with empty url from GH_AW_OTLP_ENDPOINTS", () => {
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "" }, { url: "https://valid.example.com:4317" }]);
    const result = parseOTLPEndpoints();
    expect(result).toEqual([{ url: "https://valid.example.com:4317" }]);
  });

  it("returns empty array when GH_AW_OTLP_ENDPOINTS is invalid JSON", () => {
    process.env.GH_AW_OTLP_ENDPOINTS = "not-valid-json";
    const result = parseOTLPEndpoints();
    expect(result).toEqual([]);
  });

  it("returns empty array when GH_AW_OTLP_ENDPOINTS is an empty array", () => {
    process.env.GH_AW_OTLP_ENDPOINTS = "[]";
    const result = parseOTLPEndpoints();
    expect(result).toEqual([]);
  });

  it("parses single-endpoint array emitted by go compiler for a single endpoint config", () => {
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com:4317" }]);
    const result = parseOTLPEndpoints();
    expect(result).toEqual([{ url: "https://traces.example.com:4317" }]);
  });

  it.each(["Authorization=", "x-sentry-auth="])("drops an endpoint when its %s header is empty", headers => {
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com:4317", headers }]);
    expect(parseOTLPEndpoints()).toEqual([]);
  });

  it.each(["Authorization=ApiKey", "Authorization=Bearer", "Authorization=Digest%20%20", "x-sentry-auth=ApiKey", "x-sentry-auth=DSN"])("drops an endpoint when its %s header has an auth scheme without credentials", headers => {
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com:4317", headers }]);
    expect(parseOTLPEndpoints()).toEqual([]);
  });

  it("keeps an endpoint when its authorization header has credentials", () => {
    const endpoint = { url: "https://traces.example.com:4317", headers: "Authorization=ApiKey secret-token" };
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([endpoint]);
    expect(parseOTLPEndpoints()).toEqual([endpoint]);
  });

  it("keeps an endpoint when an unrelated header is empty", () => {
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com:4317", headers: "X-Tenant=" }]);
    expect(parseOTLPEndpoints()).toEqual([{ url: "https://traces.example.com:4317", headers: "X-Tenant=" }]);
  });

  it("keeps later endpoints when an earlier header pair has an invalid percent escape", () => {
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([
      { url: "https://malformed.example.com:4317", headers: "X-Tenant=%" },
      { url: "https://traces.example.com:4317", headers: "Authorization=******" },
    ]);
    expect(parseOTLPEndpoints()).toEqual([
      { url: "https://malformed.example.com:4317", headers: "X-Tenant=%" },
      { url: "https://traces.example.com:4317", headers: "Authorization=******" },
    ]);
  });

  describe("GH_AW_OTLP_IF_MISSING=ignore (enterprise-default fallback)", () => {
    afterEach(() => {
      delete process.env.GH_AW_OTLP_IF_MISSING;
      delete process.env.GH_AW_OTLP_ENDPOINTS;
    });

    it("drops an endpoint with a URL but no headers to avoid unauthenticated export", () => {
      process.env.GH_AW_OTLP_IF_MISSING = "ignore";
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com:4317" }]);
      const result = parseOTLPEndpoints();
      expect(result).toEqual([]);
    });

    it("keeps an endpoint when both URL and headers are set", () => {
      process.env.GH_AW_OTLP_IF_MISSING = "ignore";
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com:4317", headers: "Authorization=******" }]);
      const result = parseOTLPEndpoints();
      expect(result).toEqual([{ url: "https://traces.example.com:4317", headers: "Authorization=******" }]);
    });

    it("does not drop endpoints without headers when if-missing is not ignore", () => {
      process.env.GH_AW_OTLP_IF_MISSING = "warn";
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com:4317" }]);
      const result = parseOTLPEndpoints();
      expect(result).toEqual([{ url: "https://traces.example.com:4317" }]);
    });
  });
});

// ---------------------------------------------------------------------------
// sendOTLPToAllEndpoints
// ---------------------------------------------------------------------------

describe("sendOTLPToAllEndpoints", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    delete process.env.OTEL_EXPORTER_OTLP_HEADERS;
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("sends to a single endpoint", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    const payload = buildOTLPPayload({
      traceId: "a".repeat(32),
      spanId: "b".repeat(16),
      spanName: "test.span",
      startMs: 1000,
      endMs: 2000,
      serviceName: "gh-aw",
      attributes: [],
    });

    await sendOTLPToAllEndpoints([{ url: "https://primary.example.com:4317" }], payload, { skipJSONL: true });

    expect(mockFetch).toHaveBeenCalledTimes(1);
    expect(mockFetch.mock.calls[0][0]).toBe("https://primary.example.com:4317/v1/traces");
  });

  it("sends to multiple endpoints concurrently", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    const payload = buildOTLPPayload({
      traceId: "a".repeat(32),
      spanId: "b".repeat(16),
      spanName: "test.span",
      startMs: 1000,
      endMs: 2000,
      serviceName: "gh-aw",
      attributes: [],
    });

    await sendOTLPToAllEndpoints([{ url: "https://primary.example.com:4317" }, { url: "https://secondary.example.com:4317" }], payload, { skipJSONL: true });

    expect(mockFetch).toHaveBeenCalledTimes(2);
    const urls = mockFetch.mock.calls.map(c => c[0]).sort();
    expect(urls).toEqual(["https://primary.example.com:4317/v1/traces", "https://secondary.example.com:4317/v1/traces"].sort());
  });

  it("uses per-endpoint headers (not global OTEL_EXPORTER_OTLP_HEADERS)", async () => {
    process.env.OTEL_EXPORTER_OTLP_HEADERS = "X-Global=global";
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    const payload = buildOTLPPayload({
      traceId: "a".repeat(32),
      spanId: "b".repeat(16),
      spanName: "test.span",
      startMs: 1000,
      endMs: 2000,
      serviceName: "gh-aw",
      attributes: [],
    });

    await sendOTLPToAllEndpoints(
      [
        { url: "https://primary.example.com:4317", headers: "Authorization=Bearer tok1" },
        { url: "https://secondary.example.com:4317", headers: "Authorization=Bearer tok2" },
      ],
      payload,
      { skipJSONL: true }
    );

    expect(mockFetch).toHaveBeenCalledTimes(2);
    const call1 = mockFetch.mock.calls.find(c => c[0].includes("primary"));
    const call2 = mockFetch.mock.calls.find(c => c[0].includes("secondary"));
    expect(call1[1].headers["Authorization"]).toBe("Bearer tok1");
    expect(call2[1].headers["Authorization"]).toBe("Bearer tok2");
    // Global header should NOT be included since per-endpoint headers override it.
    expect(call1[1].headers["X-Global"]).toBeUndefined();
  });

  it("continues to other endpoints when one fails", async () => {
    let callCount = 0;
    const mockFetch = vi.fn().mockImplementation(url => {
      callCount++;
      if (url.includes("primary")) {
        return Promise.reject(new Error("connection refused"));
      }
      return Promise.resolve({ ok: true, status: 200, statusText: "OK" });
    });
    vi.stubGlobal("fetch", mockFetch);

    const payload = buildOTLPPayload({
      traceId: "a".repeat(32),
      spanId: "b".repeat(16),
      spanName: "test.span",
      startMs: 1000,
      endMs: 2000,
      serviceName: "gh-aw",
      attributes: [],
    });

    // Should not throw even if one endpoint fails.
    await expect(sendOTLPToAllEndpoints([{ url: "https://primary.example.com:4317" }, { url: "https://secondary.example.com:4317" }], payload, { skipJSONL: true, maxRetries: 0 })).resolves.toBeUndefined();

    // Both endpoints were attempted.
    expect(mockFetch).toHaveBeenCalledTimes(2);
  });

  it("is a no-op when endpoints array is empty", async () => {
    const mockFetch = vi.fn();
    vi.stubGlobal("fetch", mockFetch);

    const payload = buildOTLPPayload({
      traceId: "a".repeat(32),
      spanId: "b".repeat(16),
      spanName: "test.span",
      startMs: 1000,
      endMs: 2000,
      serviceName: "gh-aw",
      attributes: [],
    });

    await sendOTLPToAllEndpoints([], payload, { skipJSONL: true });
    expect(mockFetch).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// resolveEngineId
// ---------------------------------------------------------------------------

describe("resolveEngineId", () => {
  const savedEnv = {};

  beforeEach(() => {
    savedEnv.GH_AW_INFO_ENGINE_ID = process.env.GH_AW_INFO_ENGINE_ID;
    delete process.env.GH_AW_INFO_ENGINE_ID;
  });

  afterEach(() => {
    if (savedEnv.GH_AW_INFO_ENGINE_ID !== undefined) {
      process.env.GH_AW_INFO_ENGINE_ID = savedEnv.GH_AW_INFO_ENGINE_ID;
    } else {
      delete process.env.GH_AW_INFO_ENGINE_ID;
    }
  });

  it("returns engine_id from awInfo when set", () => {
    expect(resolveEngineId({ engine_id: "claude" })).toBe("claude");
  });

  it("falls back to context.engine_id when awInfo.engine_id is absent", () => {
    expect(resolveEngineId({ context: { engine_id: "copilot" } })).toBe("copilot");
  });

  it("falls back to env var when both awInfo fields are absent", () => {
    process.env.GH_AW_INFO_ENGINE_ID = "codex";
    expect(resolveEngineId({})).toBe("codex");
  });

  it("prefers awInfo.engine_id over context.engine_id", () => {
    expect(resolveEngineId({ engine_id: "claude", context: { engine_id: "copilot" } })).toBe("claude");
  });

  it("prefers context.engine_id over env var", () => {
    process.env.GH_AW_INFO_ENGINE_ID = "codex";
    expect(resolveEngineId({ context: { engine_id: "copilot" } })).toBe("copilot");
  });

  it("returns empty string when no source provides engine_id", () => {
    expect(resolveEngineId({})).toBe("");
  });

  it("ignores whitespace-only awInfo.engine_id and falls back to context", () => {
    expect(resolveEngineId({ engine_id: "  ", context: { engine_id: "gemini" } })).toBe("gemini");
  });
});

// ---------------------------------------------------------------------------
// parseOTLPCustomAttributes
// ---------------------------------------------------------------------------

describe("parseOTLPCustomAttributes", () => {
  const savedEnv = {};

  beforeEach(() => {
    savedEnv.GH_AW_OTLP_ATTRIBUTES = process.env.GH_AW_OTLP_ATTRIBUTES;
    delete process.env.GH_AW_OTLP_ATTRIBUTES;
  });

  afterEach(() => {
    if (savedEnv.GH_AW_OTLP_ATTRIBUTES !== undefined) {
      process.env.GH_AW_OTLP_ATTRIBUTES = savedEnv.GH_AW_OTLP_ATTRIBUTES;
    } else {
      delete process.env.GH_AW_OTLP_ATTRIBUTES;
    }
  });

  it("returns null when the env var is not set", () => {
    expect(parseOTLPCustomAttributes()).toBeNull();
  });

  it("returns null when the env var is an empty string", () => {
    process.env.GH_AW_OTLP_ATTRIBUTES = "";
    expect(parseOTLPCustomAttributes()).toBeNull();
  });

  it("returns null when the env var is not valid JSON", () => {
    process.env.GH_AW_OTLP_ATTRIBUTES = "not-json";
    expect(parseOTLPCustomAttributes()).toBeNull();
  });

  it("returns null when the env var is a JSON array", () => {
    process.env.GH_AW_OTLP_ATTRIBUTES = '["a","b"]';
    expect(parseOTLPCustomAttributes()).toBeNull();
  });

  it("returns null when the env var is JSON null", () => {
    process.env.GH_AW_OTLP_ATTRIBUTES = "null";
    expect(parseOTLPCustomAttributes()).toBeNull();
  });

  it("returns the parsed object when the env var is a valid JSON object", () => {
    process.env.GH_AW_OTLP_ATTRIBUTES = JSON.stringify({
      "langfuse.session.id": "my-session",
      "langfuse.user.id": "my-user",
    });
    const result = parseOTLPCustomAttributes();
    expect(result).toEqual({
      "langfuse.session.id": "my-session",
      "langfuse.user.id": "my-user",
    });
  });
});

// ---------------------------------------------------------------------------
// buildCustomOTLPAttributes
// ---------------------------------------------------------------------------

describe("buildCustomOTLPAttributes", () => {
  const savedEnv = {};

  beforeEach(() => {
    savedEnv.GH_AW_OTLP_ATTRIBUTES = process.env.GH_AW_OTLP_ATTRIBUTES;
    delete process.env.GH_AW_OTLP_ATTRIBUTES;
  });

  afterEach(() => {
    if (savedEnv.GH_AW_OTLP_ATTRIBUTES !== undefined) {
      process.env.GH_AW_OTLP_ATTRIBUTES = savedEnv.GH_AW_OTLP_ATTRIBUTES;
    } else {
      delete process.env.GH_AW_OTLP_ATTRIBUTES;
    }
  });

  it("returns an empty array when GH_AW_OTLP_ATTRIBUTES is not set", () => {
    expect(buildCustomOTLPAttributes()).toEqual([]);
  });

  it("returns static attribute values as-is", () => {
    process.env.GH_AW_OTLP_ATTRIBUTES = JSON.stringify({
      "langfuse.session.id": "my-session",
      "langfuse.user.id": "my-user",
    });
    const result = buildCustomOTLPAttributes();
    expect(result).toContainEqual({ key: "langfuse.session.id", value: { stringValue: "my-session" } });
    expect(result).toContainEqual({ key: "langfuse.user.id", value: { stringValue: "my-user" } });
  });

  it("omits custom attributes whose value is an empty string", () => {
    process.env.GH_AW_OTLP_ATTRIBUTES = JSON.stringify({
      "my.attr": "",
    });
    const result = buildCustomOTLPAttributes();
    expect(result).toHaveLength(0);
  });

  it("preserves static attribute values", () => {
    process.env.GH_AW_OTLP_ATTRIBUTES = JSON.stringify({
      "deployment.environment": "production",
    });
    const result = buildCustomOTLPAttributes();
    expect(result).toContainEqual({ key: "deployment.environment", value: { stringValue: "production" } });
  });
});

// ---------------------------------------------------------------------------
// sendJobSetupSpan – custom attributes integration
// ---------------------------------------------------------------------------

describe("sendJobSetupSpan custom attributes", () => {
  const savedEnv = {};
  const envKeys = ["GH_AW_OTLP_ENDPOINTS", "GH_AW_OTLP_ATTRIBUTES", "GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT", "GITHUB_ACTOR", "GITHUB_REPOSITORY", "GH_AW_SETUP_AW_CONTEXT"];
  let mkdirSpy, appendSpy;

  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
    for (const k of envKeys) {
      savedEnv[k] = process.env[k];
      delete process.env[k];
    }
    mkdirSpy = vi.spyOn(fs, "mkdirSync").mockImplementation(() => {});
    appendSpy = vi.spyOn(fs, "appendFileSync").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    for (const k of envKeys) {
      if (savedEnv[k] !== undefined) {
        process.env[k] = savedEnv[k];
      } else {
        delete process.env[k];
      }
    }
    mkdirSpy.mockRestore();
    appendSpy.mockRestore();
  });

  it("emits langfuse.session.id and langfuse.user.id when configured via GH_AW_OTLP_ATTRIBUTES", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://langfuse.example.com" }]);
    process.env.GITHUB_ACTOR = "octocat";
    process.env.GITHUB_RUN_ID = "99001122";
    process.env.GITHUB_RUN_ATTEMPT = "1";
    process.env.GH_AW_SETUP_AW_CONTEXT = JSON.stringify({
      episode_id: "99001122-1:owner/repo/.github/workflows/test.lock.yml@refs/heads/main",
    });
    process.env.GH_AW_OTLP_ATTRIBUTES = JSON.stringify({
      "langfuse.session.id": "my-session-id",
      "session.id": "my-session-id",
      "langfuse.user.id": "my-user-id",
      "user.id": "my-user-id",
    });

    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobSetupSpan();
    readFileSpy.mockRestore();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const attrMap = Object.fromEntries(span.attributes.map(a => [a.key, a.value.stringValue]));

    expect(attrMap["langfuse.session.id"]).toBe("my-session-id");
    expect(attrMap["session.id"]).toBe("my-session-id");
    expect(attrMap["langfuse.user.id"]).toBe("my-user-id");
    expect(attrMap["user.id"]).toBe("my-user-id");
  });
});

// ---------------------------------------------------------------------------
// sendJobConclusionSpan – custom attributes integration
// ---------------------------------------------------------------------------

describe("sendJobConclusionSpan custom attributes", () => {
  const savedEnv = {};
  const envKeys = ["GH_AW_OTLP_ENDPOINTS", "GH_AW_OTLP_ATTRIBUTES", "GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT", "GITHUB_ACTOR", "GITHUB_REPOSITORY", "GITHUB_AW_OTEL_TRACE_ID", "GITHUB_AW_OTEL_PARENT_SPAN_ID", "GH_AW_AGENT_CONCLUSION"];
  let mkdirSpy, appendSpy;

  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
    for (const k of envKeys) {
      savedEnv[k] = process.env[k];
      delete process.env[k];
    }
    mkdirSpy = vi.spyOn(fs, "mkdirSync").mockImplementation(() => {});
    appendSpy = vi.spyOn(fs, "appendFileSync").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    for (const k of envKeys) {
      if (savedEnv[k] !== undefined) {
        process.env[k] = savedEnv[k];
      } else {
        delete process.env[k];
      }
    }
    mkdirSpy.mockRestore();
    appendSpy.mockRestore();
  });

  it("emits langfuse.session.id and langfuse.user.id on conclusion spans when configured", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://langfuse.example.com" }]);
    process.env.GITHUB_ACTOR = "monalisa";
    process.env.GITHUB_RUN_ID = "88002233";
    process.env.GITHUB_RUN_ATTEMPT = "1";
    process.env.GITHUB_AW_OTEL_TRACE_ID = "a".repeat(32);
    process.env.GH_AW_OTLP_ATTRIBUTES = JSON.stringify({
      "langfuse.session.id": "my-session-id",
      "langfuse.user.id": "my-user-id",
    });

    const readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (String(filePath).includes("aw_info.json")) {
        return JSON.stringify({
          context: {
            episode_id: "88002233-1:owner/repo/.github/workflows/test.lock.yml@refs/heads/main",
          },
        });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion");
    readFileSpy.mockRestore();

    const body = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = body.resourceSpans[0].scopeSpans[0].spans[0];
    const attrMap = Object.fromEntries(span.attributes.map(a => [a.key, a.value.stringValue]));

    expect(attrMap["langfuse.session.id"]).toBe("my-session-id");
    expect(attrMap["langfuse.user.id"]).toBe("my-user-id");
  });
});

// ---------------------------------------------------------------------------
// sendJobConclusionSpan — no OTLP metric emission
// ---------------------------------------------------------------------------

describe("sendJobConclusionSpan does not emit OTLP metrics", () => {
  let readFileSpy;
  let statSpy;

  beforeEach(() => {
    vi.resetModules();
    process.env.INPUT_JOB_NAME = "agent";
    process.env.GH_AW_AGENT_CONCLUSION = "success";
    readFileSpy = vi.spyOn(fs, "readFileSync");
    statSpy = vi.spyOn(fs, "statSync").mockImplementation(() => {
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });
    readFileSpy.mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/agent_usage.json") {
        return JSON.stringify({ input_tokens: 1000, output_tokens: 200, ai_credits: 0.5 });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    readFileSpy.mockRestore();
    statSpy.mockRestore();
    delete process.env.INPUT_JOB_NAME;
    delete process.env.GH_AW_AGENT_CONCLUSION;
    delete process.env.GH_AW_AIC;
    delete process.env.GH_AW_OTLP_ENDPOINTS;
  });

  it("does not send a /v1/metrics POST when aiCredits > 0", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs: 1_700_000_000_000 });

    const metricsCalls = mockFetch.mock.calls.filter(([url]) => url.includes("/v1/metrics"));
    expect(metricsCalls.length).toBe(0);

    const traceCalls = mockFetch.mock.calls.filter(([url]) => url.includes("/v1/traces"));
    expect(traceCalls.length).toBeGreaterThan(0);
  });

  it("does not send a /v1/metrics POST when aiCredits is 0", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    readFileSpy.mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/agent_usage.json") {
        return JSON.stringify({ input_tokens: 0, output_tokens: 0, ai_credits: 0 });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs: 1_700_000_000_000 });

    const metricsCalls = mockFetch.mock.calls.filter(([url]) => url.includes("/v1/metrics"));
    expect(metricsCalls.length).toBe(0);

    const traceCalls = mockFetch.mock.calls.filter(([url]) => url.includes("/v1/traces"));
    expect(traceCalls.length).toBeGreaterThan(0);
  });

  it("does not send a /v1/metrics POST for non-agent jobs", async () => {
    process.env.INPUT_JOB_NAME = "conclusion";
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.GH_AW_AIC = "0.5";

    await sendJobConclusionSpan("gh-aw.conclusion", { startMs: 1_700_000_000_000 });

    const metricsCalls = mockFetch.mock.calls.filter(([url]) => url.includes("/v1/metrics"));
    expect(metricsCalls.length).toBe(0);

    const traceCalls = mockFetch.mock.calls.filter(([url]) => url.includes("/v1/traces"));
    expect(traceCalls.length).toBeGreaterThan(0);
  });
});
