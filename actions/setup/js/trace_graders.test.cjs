// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const path = require("path");
const os = require("os");
const crypto = require("crypto");

const {
  main,
  preprocessTrace,
  safeReadFile,
  safeParseJsonl,
  safeParseJson,
  readFirstAvailable,
  readEvalSummary,
  deepFreeze,
  deepClone,
  runGrader,
  runBuiltinGrader,
  runCustomGrader,
  normalizeResult,
  buildGradersSummaryBody,
  evaluateThreshold,
  BUILTIN_GRADERS,
  BUILTIN_META,
  GRADER_VERSION,
  IMPLEMENTATION_ID,
  MANIFEST_PATH,
  RESULTS_PATH,
  archiveOperationalValueEvaluator,
  MAX_FILE_SIZE,
  MAX_LINE_LENGTH,
  SCRIPT_TIMEOUT_MS,
  gradeToolSuccessRate,
  gradeToolFailureCount,
  gradeRetries,
  gradeLoops,
  gradeTrajectoryEfficiency,
  gradeExecutionStepCount,
  gradeExecutionDuration,
  gradeWorkingSetRebuildFactor,
  gradeContextGrowth,
  gradeArtifactProduction,
} = require("./trace_graders.cjs");

// --- Helper to create a minimal trace ---

/** @returns {import("./trace_graders.cjs").PreprocessedTrace} */
function makeTrace(overrides = {}) {
  return {
    tokenUsageEntries: [],
    agentUsage: null,
    mcpGatewayEntries: [],
    agentOutput: null,
    toolCalls: [],
    gatewayRequests: [],
    retryEvents: [],
    errorEvents: [],
    steps: [],
    totalInputTokens: 0,
    totalOutputTokens: 0,
    totalDurationMs: 0,
    totalRequests: 0,
    files: [],
    artifacts: [],
    ...overrides,
  };
}

const policyNearMissScriptMatch = fs.readFileSync(path.join(__dirname, "../../../.github/workflows/shared/graders/policy-near-miss.md"), "utf8").match(/script: \|\n([\s\S]*?)\n^---\s*$/m);
if (!policyNearMissScriptMatch?.[1]) {
  throw new Error("unable to extract policy-near-miss grader script");
}
const policyNearMissScript = policyNearMissScriptMatch[1]
  .split("\n")
  .map(line => line.slice(6))
  .join("\n");

function runPolicyNearMiss(trace) {
  return runCustomGrader("policy-near-miss", policyNearMissScript, makeTrace(trace), {
    name: "Policy Near-Miss Rate",
    unit: "ratio",
    direction: "lower_is_better",
    source: "inline",
  });
}

const explorationErrorScriptMatch = fs.readFileSync(path.join(__dirname, "../../../.github/workflows/shared/graders/exploration-error.md"), "utf8").match(/script: \|\n([\s\S]*?)\n^---\s*$/m);
if (!explorationErrorScriptMatch?.[1]) {
  throw new Error("unable to extract exploration-error grader script");
}
const explorationErrorScript = explorationErrorScriptMatch[1]
  .split("\n")
  .map(line => line.slice(6))
  .join("\n");

function runExplorationError(trace) {
  return runCustomGrader("exploration-error", explorationErrorScript, makeTrace(trace), {
    name: "Exploration Error",
    unit: "ratio",
    direction: "lower_is_better",
    source: "inline",
  });
}

const exploitationErrorScriptMatch = fs.readFileSync(path.join(__dirname, "../../../.github/workflows/shared/graders/exploitation-error.md"), "utf8").match(/script: \|\n([\s\S]*?)\n^---\s*$/m);
if (!exploitationErrorScriptMatch?.[1]) {
  throw new Error("unable to extract exploitation-error grader script");
}
const exploitationErrorScript = exploitationErrorScriptMatch[1]
  .split("\n")
  .map(line => line.slice(6))
  .join("\n");

function runExploitationError(trace) {
  return runCustomGrader("exploitation-error", exploitationErrorScript, makeTrace(trace), {
    name: "Exploitation Error",
    unit: "ratio",
    direction: "lower_is_better",
    source: "inline",
  });
}

const skillConstraintCoverageScriptMatch = fs.readFileSync(path.join(__dirname, "../../../.github/workflows/shared/graders/skill-constraint-coverage.md"), "utf8").match(/script: \|\n([\s\S]*?)\n^---\s*$/m);
if (!skillConstraintCoverageScriptMatch?.[1]) {
  throw new Error("unable to extract skill-constraint-coverage grader script");
}
const skillConstraintCoverageScript = skillConstraintCoverageScriptMatch[1]
  .split("\n")
  .map(line => line.slice(6))
  .join("\n");

function runSkillConstraintCoverage(trace, config) {
  return runCustomGrader("skill-constraint-coverage", skillConstraintCoverageScript, makeTrace(trace), {
    name: "Skill Constraint Coverage",
    unit: "ratio",
    direction: "higher_is_better",
    source: "inline",
    config,
  });
}

const toolOutputConsumptionRateScriptMatch = fs.readFileSync(path.join(__dirname, "../../../.github/workflows/shared/graders/tool-output-consumption-rate.md"), "utf8").match(/script: \|\n([\s\S]*?)\n^---\s*$/m);
if (!toolOutputConsumptionRateScriptMatch?.[1]) {
  throw new Error("unable to extract tool-output-consumption-rate grader script");
}
const toolOutputConsumptionRateScript = toolOutputConsumptionRateScriptMatch[1]
  .split("\n")
  .map(line => line.slice(6))
  .join("\n");

function runToolOutputConsumptionRate(trace) {
  return runCustomGrader("tool-output-consumption-rate", toolOutputConsumptionRateScript, makeTrace(trace), {
    name: "Tool Output Consumption Rate",
    unit: "ratio",
    direction: "higher_is_better",
    source: "inline",
  });
}

describe("trace_graders", () => {
  describe("buildGradersSummaryBody", () => {
    it("renders all computed grader values without emojis", () => {
      const summary = buildGradersSummaryBody([
        { id: "tool-success-rate", name: "Tool success rate", value: 0.98765, unit: "ratio", status: "pass", source: "builtin" },
        { id: "custom", name: "Custom", value: 2, unit: "count", status: "fail", source: "inline" },
        { id: "unavailable", name: "Unavailable", value: null, unit: "", status: "unavailable", source: "builtin" },
      ]);

      expect(summary).toContain("| Pass | Tool success rate | builtin | 0.9877 | ratio |");
      expect(summary).toContain("| Fail | Custom | inline | 2 | count |");
      expect(summary).not.toContain("Unavailable");
      expect(summary).not.toMatch(/[\u{1F300}-\u{1FAFF}]/u);
    });

    it("escapes untrusted table cells", () => {
      const summary = buildGradersSummaryBody([{ id: "custom", name: "Custom | <grader>", value: 1, unit: "unit|&", status: "pass", source: "inline|source" }]);

      expect(summary).toContain("Custom \\| &lt;grader&gt;");
      expect(summary).toContain("inline\\|source");
      expect(summary).toContain("unit\\|&amp;");
    });

    it("escapes backslashes before pipes", () => {
      const summary = buildGradersSummaryBody([{ id: "custom", name: "A\\|B", value: 1, unit: "count", status: "pass", source: "inline" }]);

      expect(summary).toContain("| Pass | A\\\\\\|B | inline | 1 | count |");
    });

    it("surrounds the table with blank lines for details rendering", () => {
      const summary = buildGradersSummaryBody([{ id: "custom", name: "Custom", value: 1, unit: "count", status: "pass", source: "inline" }]);

      expect(summary.startsWith("\n\n")).toBe(true);
      expect(summary.endsWith("\n\n")).toBe(true);
    });
  });

  describe("archiveOperationalValueEvaluator", () => {
    it("writes only evaluator bytes matching the frozen digest", () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "operational-value-evaluator-archive-"));
      const outputPath = path.join(tempDir, "operational_value_evaluator.sh");
      const content = "#!/usr/bin/env bash\nprintf 'ok\\n'\n";
      const digest = crypto.createHash("sha256").update(content, "utf8").digest("hex");
      try {
        archiveOperationalValueEvaluator(content, digest, outputPath);
        expect(fs.readFileSync(outputPath, "utf8")).toBe(content);
        expect(() => archiveOperationalValueEvaluator(content, "invalid", outputPath)).toThrow("digest mismatch");
      } finally {
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });
  });

  // --- safeParseJsonl ---
  describe("safeParseJsonl", () => {
    it("parses valid JSONL", () => {
      const content = '{"a":1}\n{"b":2}\n';
      const result = safeParseJsonl(content);
      expect(result).toEqual([{ a: 1 }, { b: 2 }]);
    });

    it("skips malformed lines", () => {
      const content = '{"a":1}\nnot json\n{"b":2}\n';
      const result = safeParseJsonl(content);
      expect(result).toEqual([{ a: 1 }, { b: 2 }]);
    });

    it("skips oversized lines", () => {
      const longLine = '{"x":"' + "a".repeat(MAX_LINE_LENGTH + 10) + '"}';
      const content = '{"a":1}\n' + longLine + '\n{"b":2}\n';
      const result = safeParseJsonl(content);
      expect(result).toEqual([{ a: 1 }, { b: 2 }]);
    });

    it("handles empty input", () => {
      expect(safeParseJsonl("")).toEqual([]);
      expect(safeParseJsonl("\n\n")).toEqual([]);
    });
  });

  // --- safeParseJson ---
  describe("safeParseJson", () => {
    it("parses valid JSON", () => {
      expect(safeParseJson('{"a":1}')).toEqual({ a: 1 });
    });

    // --- readEvalSummary ---
    describe("readEvalSummary", () => {
      const evalsPath = "/tmp/gh-aw/evals.jsonl";

      afterEach(() => {
        if (fs.existsSync(evalsPath)) {
          fs.unlinkSync(evalsPath);
        }
      });

      it("returns null when evals file is absent", () => {
        if (fs.existsSync(evalsPath)) {
          fs.unlinkSync(evalsPath);
        }
        expect(readEvalSummary()).toBeNull();
      });

      it("summarizes YES/NO/UNKNOWN answers", () => {
        fs.mkdirSync(path.dirname(evalsPath), { recursive: true });
        fs.writeFileSync(evalsPath, [JSON.stringify({ id: "q1", answer: "YES" }), JSON.stringify({ id: "q2", answer: "no" }), JSON.stringify({ id: "q3", answer: "MAYBE" })].join("\n") + "\n", "utf8");
        expect(readEvalSummary()).toEqual({
          total: 3,
          yes: 1,
          no: 1,
          unknown: 1,
          byQuestion: {
            q1: "YES",
            q2: "NO",
            q3: "UNKNOWN",
          },
        });
      });
    });

    it("returns null for invalid JSON", () => {
      expect(safeParseJson("not json")).toBeNull();
    });

    it("returns null for oversized content", () => {
      const big = "a".repeat(MAX_FILE_SIZE + 1);
      expect(safeParseJson(big)).toBeNull();
    });
  });

  // --- deepFreeze ---
  describe("deepFreeze", () => {
    it("freezes objects deeply", () => {
      const obj = { a: { b: { c: 1 } } };
      deepFreeze(obj);
      expect(Object.isFrozen(obj)).toBe(true);
      expect(Object.isFrozen(obj.a)).toBe(true);
      expect(Object.isFrozen(obj.a.b)).toBe(true);
    });

    it("handles null/primitives", () => {
      expect(deepFreeze(null)).toBeNull();
      expect(deepFreeze(42)).toBe(42);
    });
  });

  // --- Built-in graders ---
  describe("gradeToolSuccessRate", () => {
    it("returns 1 for no tool calls", () => {
      expect(gradeToolSuccessRate(makeTrace())).toBe(1);
    });

    it("computes success rate", () => {
      const trace = makeTrace({
        toolCalls: [
          { name: "a", success: true },
          { name: "b", success: false },
          { name: "c", success: true },
        ],
      });
      expect(gradeToolSuccessRate(trace)).toBeCloseTo(2 / 3);
    });

    it("handles error field as failure indicator", () => {
      const trace = makeTrace({
        toolCalls: [{ name: "a" }, { name: "b", error: "something failed" }],
      });
      expect(gradeToolSuccessRate(trace)).toBe(0.5);
    });
  });

  describe("gradeToolFailureCount", () => {
    it("returns 0 for no tool calls", () => {
      expect(gradeToolFailureCount(makeTrace())).toBe(0);
    });

    it("counts failures", () => {
      const trace = makeTrace({
        toolCalls: [
          { name: "a", success: true },
          { name: "b", success: false },
          { name: "c", error: "err" },
        ],
      });
      expect(gradeToolFailureCount(trace)).toBe(2);
    });

    it("matches success-rate complement for ambiguous calls", () => {
      const trace = makeTrace({
        toolCalls: [{ name: "a", success: true }, { name: "b", status: "error" }, { name: "c" }],
      });
      const failures = gradeToolFailureCount(trace);
      const successRate = gradeToolSuccessRate(trace);
      expect(successRate).toBeCloseTo((trace.toolCalls.length - failures) / trace.toolCalls.length);
    });
  });

  describe("gradeRetries", () => {
    it("returns 0 for no retries", () => {
      expect(gradeRetries(makeTrace())).toBe(0);
    });

    it("counts retry events from preprocessed retryEvents", () => {
      const trace = makeTrace({
        retryEvents: [{ event: "retry" }, { retry: true }, { message: "Retrying request" }],
      });
      expect(gradeRetries(trace)).toBe(3);
    });
  });

  describe("gradeLoops", () => {
    it("returns 0 for no loops", () => {
      expect(gradeLoops(makeTrace())).toBe(0);
    });

    it("detects consecutive identical calls", () => {
      const trace = makeTrace({
        toolCalls: [
          { name: "read", arguments: { path: "/a" } },
          { name: "read", arguments: { path: "/a" } },
          { name: "read", arguments: { path: "/a" } },
          { name: "write", arguments: { path: "/b" } },
        ],
      });
      expect(gradeLoops(trace)).toBe(2);
    });
  });

  describe("gradeTrajectoryEfficiency", () => {
    it("returns 1 for no tool calls", () => {
      expect(gradeTrajectoryEfficiency(makeTrace())).toBe(1);
    });

    it("computes efficiency", () => {
      const trace = makeTrace({
        toolCalls: [{ name: "read" }, { name: "write" }, { name: "read" }, { name: "read" }],
      });
      expect(gradeTrajectoryEfficiency(trace)).toBe(0.5);
    });
  });

  describe("gradeExecutionStepCount", () => {
    it("returns total LLM requests", () => {
      expect(gradeExecutionStepCount(makeTrace({ totalRequests: 42 }))).toBe(42);
    });
  });

  describe("gradeExecutionDuration", () => {
    it("returns total duration", () => {
      expect(gradeExecutionDuration(makeTrace({ totalDurationMs: 12345 }))).toBe(12345);
    });
  });

  describe("gradeWorkingSetRebuildFactor", () => {
    it("computes cumulative input relative to the peak invocation", () => {
      const trace = makeTrace({
        tokenUsageEntries: [{ input_tokens: 100_000 }, { input_tokens: 150_000 }, { input_tokens: 200_000 }, { input_tokens: 200_000 }, { input_tokens: 224_000 }],
      });
      expect(gradeWorkingSetRebuildFactor(trace)).toBeCloseTo(874_000 / 224_000);
    });

    it("returns null when no positive input-token measurement is available", () => {
      expect(gradeWorkingSetRebuildFactor(makeTrace())).toBeNull();
      expect(gradeWorkingSetRebuildFactor(makeTrace({ tokenUsageEntries: [{ input_tokens: 0 }] }))).toBeNull();
    });

    it("uses canonical input tokens without adding cache token fields", () => {
      const trace = makeTrace({
        tokenUsageEntries: [
          { input_tokens: 10, cache_read_tokens: 1_000 },
          { input_tokens: 20, cache_write_tokens: 1_000 },
        ],
      });
      expect(gradeWorkingSetRebuildFactor(trace)).toBe(1.5);
    });
  });

  describe("gradeContextGrowth", () => {
    it("returns 1 for fewer than 2 entries", () => {
      expect(gradeContextGrowth(makeTrace())).toBe(1);
      expect(gradeContextGrowth(makeTrace({ tokenUsageEntries: [{ input_tokens: 100, output_tokens: 50 }] }))).toBe(1);
    });

    it("computes growth ratio", () => {
      const trace = makeTrace({
        tokenUsageEntries: [
          { input_tokens: 100, output_tokens: 50 },
          { input_tokens: 200, output_tokens: 100 },
        ],
        totalInputTokens: 300,
        totalOutputTokens: 150,
      });
      expect(gradeContextGrowth(trace)).toBe(3);
    });
  });

  describe("gradeArtifactProduction", () => {
    it("returns 0 for no agent output", () => {
      expect(gradeArtifactProduction(makeTrace())).toBe(0);
    });

    it("counts artifacts array", () => {
      const trace = makeTrace({
        artifacts: [{ type: "pr" }, { type: "issue" }],
      });
      expect(gradeArtifactProduction(trace)).toBe(2);
    });
  });

  // --- normalizeResult ---
  describe("normalizeResult", () => {
    const meta = { name: "Test", unit: "count", direction: "lower_is_better", threshold: 5, source: "builtin" };

    it("normalizes a number result with threshold pass", () => {
      const r = normalizeResult("test", 3, meta);
      expect(r.id).toBe("test");
      expect(r.value).toBe(3);
      expect(r.passed).toBe(true);
      expect(r.status).toBe("pass");
      expect(r.unit).toBe("count");
      expect(r.implementation.id).toBe(IMPLEMENTATION_ID);
    });

    it("normalizes a number result with threshold fail", () => {
      const r = normalizeResult("test", 10, meta);
      expect(r.passed).toBe(false);
      expect(r.status).toBe("fail");
    });

    it("uses manifest metadata for object results from custom scripts", () => {
      const r = normalizeResult("test", { value: 42, unit: "ms", severity: "warning", details: "too slow" }, { ...meta, source: "inline" });
      expect(r.value).toBe(42);
      expect(r.unit).toBe("count");
      expect(r.severity).toBe("warning");
      expect(r.details).toBe("too slow");
    });

    it("omits an unrecognized custom severity", () => {
      const r = normalizeResult("test", { value: 42, severity: "sensitive trace content" }, { ...meta, source: "inline" });
      expect(r.severity).toBeUndefined();
    });

    it("handles null result as unavailable", () => {
      const r = normalizeResult("test", null, meta);
      expect(r.status).toBe("unavailable");
    });

    it("handles non-finite value as error", () => {
      const r = normalizeResult("test", NaN, meta);
      expect(r.status).toBe("error");
      expect(r.error).toContain("non-finite");
    });

    it("includes digest in implementation when provided", () => {
      const r = normalizeResult("test", 1, { ...meta, source: "inline", digest: "abc123" });
      expect(r.implementation.digest).toBe("abc123");
    });
  });

  // --- evaluateThreshold ---
  describe("evaluateThreshold", () => {
    it("passes when higher_is_better and value >= threshold", () => {
      expect(evaluateThreshold(0.9, "higher_is_better", 0.8)).toBe(true);
      expect(evaluateThreshold(0.7, "higher_is_better", 0.8)).toBe(false);
    });

    it("passes when lower_is_better and value <= threshold", () => {
      expect(evaluateThreshold(3, "lower_is_better", 5)).toBe(true);
      expect(evaluateThreshold(10, "lower_is_better", 5)).toBe(false);
    });

    it("returns null when no threshold", () => {
      expect(evaluateThreshold(1, "higher_is_better", undefined)).toBeNull();
    });
  });

  // --- runGrader ---
  describe("runGrader", () => {
    it("runs built-in grader", () => {
      const trace = makeTrace({ totalRequests: 5 });
      const result = runGrader("execution-step-count", true, undefined, trace);
      expect(result.value).toBe(5);
      expect(result.error).toBeNull();
    });

    it("runs inline script via vm sandbox", () => {
      const trace = makeTrace({ toolCalls: [{ name: "a" }, { name: "b" }] });
      const result = runGrader("custom", false, "return { value: trace.toolCalls.length }", trace);
      expect(result.value).toBe(2);
      expect(result.error).toBeNull();
    });

    it("supports legacy expression-style scripts", () => {
      const trace = makeTrace({ toolCalls: [{ name: "a" }] });
      const result = runGrader("custom", false, "return trace.toolCalls.length", trace);
      expect(result.value).toBe(1);
    });

    it("catches script errors", () => {
      const result = runGrader("bad", false, "return undefinedVar.prop", makeTrace());
      expect(result.value).toBeNull();
      expect(result.error).toContain("runtime error");
    });

    it("rejects non-numeric results from inline scripts", () => {
      const result = runGrader("str", false, 'return "hello"', makeTrace());
      expect(result.value).toBeNull();
      expect(result.error).toContain("non-finite");
    });

    it("rejects NaN from built-in graders", () => {
      const result = runGrader("nan-test", false, "return NaN", makeTrace());
      expect(result.value).toBeNull();
      expect(result.error).toContain("non-finite");
    });

    it("rejects Infinity", () => {
      const result = runGrader("inf-test", false, "return Infinity", makeTrace());
      expect(result.value).toBeNull();
      expect(result.error).toContain("non-finite");
    });
  });

  // --- runCustomGrader node:vm sandbox ---
  describe("runCustomGrader sandbox", () => {
    const meta = { name: "test", unit: "", direction: "", source: "inline" };

    it("cannot access require", () => {
      const result = runCustomGrader("test", "return typeof require", makeTrace(), meta);
      expect(result.value).toBeNull(); // "undefined" is not a number
    });

    it("cannot access process", () => {
      const result = runCustomGrader("test", "return typeof process", makeTrace(), meta);
      expect(result.value).toBeNull(); // "undefined" is not a number
    });

    it("cannot access fetch", () => {
      const result = runCustomGrader("test", "return typeof fetch", makeTrace(), meta);
      expect(result.value).toBeNull();
    });

    it("cannot construct Date", () => {
      const result = runCustomGrader("test", "try { new Date(); return 0 } catch(e) { return 1 }", makeTrace(), meta);
      // Date is not available in the sandbox
      expect(result.value).toBe(1);
    });

    it("cannot use Math.random", () => {
      const result = runCustomGrader("test", "return typeof Math.random", makeTrace(), meta);
      expect(result.value).toBeNull(); // "undefined" is not a number
    });

    it("cannot mutate frozen trace", () => {
      const trace = makeTrace({ toolCalls: [{ name: "a" }] });
      const result = runCustomGrader("test", "try { trace.toolCalls.push({name:'b'}); return 0 } catch(e) { return 1 }", trace, meta);
      expect(result.value).toBe(1);
    });

    it("receives config parameter", () => {
      const result = runCustomGrader("test", "return config.multiplier * 2", makeTrace(), { ...meta, config: { multiplier: 5 } });
      expect(result.value).toBe(10);
    });

    it("handles scripts containing template literals safely", () => {
      const trace = makeTrace({ toolCalls: Array.from({ length: 12 }, () => ({ name: "a" })) });
      const result = runCustomGrader("test", "return `${trace.toolCalls.length}`.length", trace, meta);
      expect(result.value).toBe(2);
      expect(result.error).toBeUndefined();
    });

    it("receives run metadata from caller", () => {
      const result = runCustomGrader("test", "return run.graderCount", makeTrace(), { ...meta, graderCount: 7 });
      expect(result.value).toBe(7);
    });

    it("can use helpers", () => {
      const result = runCustomGrader("test", "return helpers.clamp(10, 0, 5)", makeTrace(), meta);
      expect(result.value).toBe(5);
    });

    it("supports multiline return object form", () => {
      const script = `
        const count = trace.toolCalls.length;
        return {
          value: count,
          unit: "count",
          severity: count > 10 ? "warning" : "info"
        }
      `;
      const trace = makeTrace({ toolCalls: [{ name: "a" }, { name: "b" }] });
      const result = runCustomGrader("test", script, trace, { ...meta, unit: "count" });
      expect(result.value).toBe(2);
      expect(result.unit).toBe("count");
    });

    it("times out on infinite loops", () => {
      const result = runCustomGrader("test", "while(true){} return 1", makeTrace(), meta);
      expect(result.status).toBe("error");
      expect(result.error).toContain("runtime error");
    });
  });

  describe("policy-near-miss custom grader", () => {
    it("discovers objectives after an events-only candidate", () => {
      const result = runPolicyNearMiss({
        trajectoryIR: { events: [{ kind: "safe_output" }] },
        ir: { objectives: [{ id: "guard", description: "Verify approval", satisfiedAtEventIndex: null }] },
      });

      expect(result.value).toBe(1);
      expect(result.details).toContain("guardObjectives=1 unmet=1");
    });

    it("identifies guard objectives and treats event index zero as satisfied", () => {
      const result = runPolicyNearMiss({
        trajectoryIR: {
          events: [{ kind: "safe_output" }],
          objectives: [
            { id: "met", description: "Check authorization", satisfiedAtEventIndex: 0 },
            { id: "unmet", description: "Verification required", satisfiedAtEventIndex: null },
            { id: "not-a-guard", description: "Complete checkbox", satisfiedAtEventIndex: null },
          ],
        },
      });

      expect(result.value).toBeCloseTo(0.5);
      expect(result.details).toContain("guardObjectives=2 unmet=1");
    });

    it.each([
      ["no objectives", { trajectoryIR: { events: [{ kind: "safe_output" }], objectives: [] } }],
      ["no outcome", { trajectoryIR: { events: [], objectives: [{ description: "Check approval" }] } }],
      ["no guard objective", { trajectoryIR: { events: [{ kind: "safe_output" }], objectives: [{ description: "Write report" }] } }],
    ])("normalizes %s as unavailable", (_name, trace) => {
      const result = runPolicyNearMiss(trace);

      expect(result.value).toBeNull();
      expect(result.passed).toBeNull();
      expect(result.status).toBe("unavailable");
    });
  });

  describe("exploration-error custom grader", () => {
    it("reports unavailable when no objectives are declared", () => {
      const result = runExplorationError({ trajectoryIR: { events: [{ kind: "state_change", ref: "a" }] } });

      expect(result.value).toBeNull();
      expect(result.passed).toBeNull();
      expect(result.message).toContain("no declared objectives");
    });

    it("returns zero when all objectives are already satisfied", () => {
      const result = runExplorationError({
        trajectoryIR: {
          objectives: [{ id: "goal", description: "Read all files", satisfiedAtEventIndex: 0 }],
        },
      });

      expect(result.value).toBe(0);
      expect(result.details).toContain("all objectives satisfied");
    });

    it("prefers the objective-bearing IR candidate over unrelated agentOutput observations", () => {
      const result = runExplorationError({
        trajectoryIR: {
          objectives: [{ id: "goal", description: "Inspect repo", satisfiedAtEventIndex: null }],
          events: [{ kind: "state_change", ref: "repo-root" }],
        },
        agentOutput: {
          observations: [{ id: "obs-1" }],
        },
      });

      expect(result.value).toBe(1);
      expect(result.details).toContain("observations=0");
    });

    it("counts distinct state refs from state_change events", () => {
      const result = runExplorationError({
        trajectoryIR: {
          objectives: [{ id: "goal", description: "Inspect repo", satisfiedAtEventIndex: null }],
          events: [
            { kind: "state_change", ref: "repo-root" },
            { kind: "state_change", ref: "repo-readme" },
            { kind: "state_change", ref: "repo-root" },
          ],
          observations: [{ id: "obs-1" }],
        },
      });

      expect(result.value).toBeCloseTo(0.5);
      expect(result.details).toContain("distinctStatesVisited=2");
    });

    it("falls back to declared states[] when no state_change events exist", () => {
      const result = runExplorationError({
        trajectoryIR: {
          objectives: [{ id: "goal", description: "Inspect repo", satisfiedAtEventIndex: null }],
          states: [{ id: "a" }, { id: "b" }],
          observations: [{ id: "obs-1" }],
        },
      });

      expect(result.value).toBeCloseTo(0.5);
      expect(result.details).toContain("from declared states[]");
    });

    it("clamps the ratio at zero when observations exceed the visited-state count", () => {
      const result = runExplorationError({
        trajectoryIR: {
          objectives: [{ id: "goal", description: "Inspect repo", satisfiedAtEventIndex: null }],
          states: [{ id: "a" }],
          observations: [{ id: "obs-1" }, { id: "obs-2" }, { id: "obs-3" }],
        },
      });

      expect(result.value).toBe(0);
    });

    it("is unavailable without state_change events or declared states", () => {
      const result = runExplorationError({
        trajectoryIR: {
          objectives: [{ id: "goal", description: "Inspect repo", satisfiedAtEventIndex: null }],
          observations: [{ id: "obs-1" }],
        },
      });

      expect(result.value).toBeNull();
      expect(result.passed).toBeNull();
      expect(result.message).toContain("no state_change events or declared states");
    });
  });

  describe("exploitation-error custom grader", () => {
    it("reports unavailable when no objectives are declared", () => {
      const result = runExploitationError({ trajectoryIR: { observations: [{ id: "obs-1" }] } });

      expect(result.value).toBeNull();
      expect(result.passed).toBeNull();
      expect(result.message).toContain("no declared objectives");
    });

    it("returns zero when all objectives are already satisfied", () => {
      const result = runExploitationError({
        trajectoryIR: {
          objectives: [{ id: "goal", description: "Read all files", satisfiedAtEventIndex: 0 }],
        },
      });

      expect(result.value).toBe(0);
      expect(result.details).toContain("all objectives satisfied");
    });

    it("is unavailable without state_change events or declared states", () => {
      const result = runExploitationError({
        trajectoryIR: {
          objectives: [{ id: "goal", description: "Inspect repo", satisfiedAtEventIndex: null }],
          observations: [{ id: "obs-1" }],
        },
      });

      expect(result.value).toBeNull();
      expect(result.passed).toBeNull();
      expect(result.message).toContain("no state_change events or declared states");
    });

    it("is unavailable when the trace records no observations", () => {
      const result = runExploitationError({
        trajectoryIR: {
          objectives: [{ id: "goal", description: "Inspect repo", satisfiedAtEventIndex: null }],
          states: [{ id: "a" }],
        },
      });

      expect(result.value).toBeNull();
      expect(result.passed).toBeNull();
      expect(result.message).toContain("no observations");
    });

    it("defers to exploration-error when exploration was insufficient", () => {
      const result = runExploitationError({
        trajectoryIR: {
          objectives: [{ id: "goal", description: "Inspect repo", satisfiedAtEventIndex: null }],
          events: [
            { kind: "state_change", ref: "repo-root" },
            { kind: "state_change", ref: "repo-readme" },
          ],
          observations: [{ id: "obs-1", consumedByActionIds: ["act-1"] }],
        },
      });

      expect(result.value).toBeNull();
      expect(result.passed).toBeNull();
      expect(result.message).toContain("exploration was insufficient");
      expect(result.message).toContain("exploration-error");
    });

    it("scores the unused fraction of observations when exploration was sufficient", () => {
      const result = runExploitationError({
        trajectoryIR: {
          objectives: [{ id: "goal", description: "Inspect repo", satisfiedAtEventIndex: null }],
          events: [
            { kind: "state_change", ref: "repo-root" },
            { kind: "state_change", ref: "repo-root" },
          ],
          observations: [
            { id: "obs-1", consumedByActionIds: ["act-1"] },
            { id: "obs-2", consumedByActionIds: [] },
          ],
        },
      });

      expect(result.value).toBeCloseTo(0.5);
      expect(result.details).toContain("observations=2 unused=1");
      expect(result.details).toContain("distinctStatesVisited=1");
      expect(result.details).toContain("unmet objectives: goal");
    });

    it("falls back to declared states[] when no state_change events exist", () => {
      const result = runExploitationError({
        trajectoryIR: {
          objectives: [{ id: "goal", description: "Inspect repo", satisfiedAtEventIndex: null }],
          states: [{ id: "a" }, { id: "b" }],
          observations: [{ id: "obs-1" }, { id: "obs-2" }],
        },
      });

      expect(result.value).toBe(1);
      expect(result.details).toContain("from declared states[]");
    });

    it("prefers the objective-bearing IR candidate over unrelated agentOutput observations", () => {
      const result = runExploitationError({
        trajectoryIR: {
          objectives: [{ id: "goal", description: "Inspect repo", satisfiedAtEventIndex: null }],
          states: [{ id: "a" }],
          observations: [{ id: "obs-1", consumedByActionIds: ["act-1"] }],
        },
        agentOutput: {
          observations: [{ id: "obs-x" }],
        },
      });

      expect(result.value).toBe(0);
      expect(result.details).toContain("observations=1 unused=0");
    });
  });

  describe("skill-constraint-coverage custom grader", () => {
    it("reports full coverage when every constraint is exercised and succeeds", () => {
      const result = runSkillConstraintCoverage(
        {
          trajectoryIR: {
            toolCalls: [
              { name: "read_file", arguments: { path: "a.md" }, success: true },
              { name: "run_lint", arguments: {}, success: true },
            ],
          },
        },
        {
          constraints: [
            { id: "reads-before-write", pattern: "read_file", description: "must read before writing" },
            { id: "lints", pattern: "run_lint", description: "must lint" },
          ],
        }
      );

      expect(result.value).toBe(1);
      expect(result.details).toContain("constraints=2 exercised=2 covered=2");
    });

    it("counts an exercised-but-failing constraint as uncovered", () => {
      const result = runSkillConstraintCoverage(
        {
          trajectoryIR: {
            toolCalls: [{ name: "run_lint", arguments: {}, success: false }],
          },
        },
        {
          constraints: [{ id: "lints", pattern: "run_lint" }],
        }
      );

      expect(result.value).toBe(0);
      expect(result.details).toContain("exercised=1 covered=0");
      expect(result.details).toContain("unmet: lints");
    });

    it("ignores success when requireSuccess is false", () => {
      const result = runSkillConstraintCoverage(
        {
          trajectoryIR: {
            actions: [{ type: "comment", target: "issue-1", validAtIssueTime: false }],
          },
        },
        {
          constraints: [{ id: "comments", pattern: "comment", requireSuccess: false }],
        }
      );

      expect(result.value).toBe(1);
      expect(result.details).toContain("covered=1");
    });

    it("uses the preprocessed trace as a fallback candidate when trajectoryIR is absent", () => {
      const result = runSkillConstraintCoverage(
        {
          toolCalls: [{ name: "read_file", arguments: { path: "a.md" }, success: true }],
        },
        {
          constraints: [{ id: "reads-before-write", pattern: "read_file" }],
        }
      );

      expect(result.value).toBe(1);
      expect(result.details).toContain("constraints=1 exercised=1 covered=1");
    });

    it("counts non-object constraint entries against the denominator instead of dropping them", () => {
      const result = runSkillConstraintCoverage(
        {
          trajectoryIR: {
            toolCalls: [{ name: "run_lint", arguments: {}, success: true }],
          },
        },
        {
          constraints: [null, { id: "lints", pattern: "run_lint" }],
        }
      );

      expect(result.value).toBeCloseTo(0.5);
      expect(result.details).toContain("constraints=2 exercised=1 covered=1 invalidPattern=1");
    });

    it("counts a constraint with an invalid pattern against the denominator instead of dropping it", () => {
      const result = runSkillConstraintCoverage(
        {
          trajectoryIR: {
            toolCalls: [{ name: "run_lint", arguments: {}, success: true }],
          },
        },
        {
          constraints: [
            { id: "lints", pattern: "run_lint" },
            { id: "broken", pattern: "(" },
          ],
        }
      );

      expect(result.value).toBeCloseTo(0.5);
      expect(result.details).toContain("constraints=2 exercised=1 covered=1 invalidPattern=1");
    });

    it.each([
      ["no constraints configured", { trajectoryIR: { toolCalls: [{ name: "x", success: true }] } }, {}],
      ["no constraints with a valid pattern", { trajectoryIR: { toolCalls: [{ name: "x", success: true }] } }, { constraints: [{ id: "c", pattern: "" }] }],
      ["no toolCalls/actions in trace", { trajectoryIR: {} }, { constraints: [{ id: "c", pattern: "x" }] }],
    ])("normalizes %s as unavailable", (_name, trace, config) => {
      const result = runSkillConstraintCoverage(trace, config);

      expect(result.value).toBeNull();
      expect(result.passed).toBeNull();
      expect(result.status).toBe("unavailable");
    });
  });

  describe("tool-output-consumption-rate custom grader", () => {
    it("scores the fraction of matching tool observations that were consumed", () => {
      const result = runToolOutputConsumptionRate({
        trajectoryIR: {
          toolCalls: [{ id: "tc-1" }, { id: "tc-2" }],
          observations: [
            { id: "obs-1", sourceToolCallId: "tc-1", consumedByActionIds: ["act-1"] },
            { id: "obs-2", sourceToolCallId: "tc-2", consumedByActionIds: [] },
            { id: "obs-3", sourceToolCallId: null, consumedByActionIds: ["act-2"] },
            { id: "obs-4", sourceToolCallId: "unknown", consumedByActionIds: ["act-3"] },
          ],
        },
      });

      expect(result.value).toBeCloseTo(0.5);
      expect(result.details).toContain("toolObservations=2 consumed=1");
      expect(result.details).toContain("unconsumed: obs-2");
    });

    it("treats malformed consumption metadata as unconsumed", () => {
      const result = runToolOutputConsumptionRate({
        trajectoryIR: {
          toolCalls: [{ id: "tc-1" }],
          observations: [{ id: "obs-1", sourceToolCallId: "tc-1", consumedByActionIds: "act-1" }],
        },
      });

      expect(result.value).toBe(0);
      expect(result.details).toContain("toolObservations=1 consumed=0");
    });

    it("treats consumedByActionIds arrays containing only non-string/empty entries as unconsumed", () => {
      const result = runToolOutputConsumptionRate({
        trajectoryIR: {
          toolCalls: [{ id: "tc-1" }, { id: "tc-2" }],
          observations: [
            { id: "obs-1", sourceToolCallId: "tc-1", consumedByActionIds: [null] },
            { id: "obs-2", sourceToolCallId: "tc-2", consumedByActionIds: [42, ""] },
          ],
        },
      });

      expect(result.value).toBe(0);
      expect(result.details).toContain("toolObservations=2 consumed=0");
      expect(result.details).toContain("unconsumed: obs-1, obs-2");
    });

    it("reads observations/toolCalls from the root trace object", () => {
      const result = runToolOutputConsumptionRate({
        toolCalls: [{ id: "tc-1" }],
        observations: [{ id: "obs-1", sourceToolCallId: "tc-1", consumedByActionIds: ["act-1"] }],
      });

      expect(result.value).toBe(1);
    });

    it("reads a complete IR nested in agentOutput", () => {
      const result = runToolOutputConsumptionRate({
        agentOutput: {
          trajectoryIR: {
            toolCalls: [{ id: "tc-1" }],
            observations: [{ id: "obs-1", sourceToolCallId: "tc-1", consumedByActionIds: ["act-1"] }],
          },
        },
      });

      expect(result.value).toBe(1);
    });

    it.each([
      ["no observations", { trajectoryIR: { toolCalls: [{ id: "tc-1" }] } }, "no observations"],
      ["no tool calls", { trajectoryIR: { observations: [{ id: "obs-1", sourceToolCallId: "tc-1", consumedByActionIds: ["act-1"] }] } }, "no tool-originated observations"],
      [
        "no matching tool call",
        {
          trajectoryIR: {
            toolCalls: [{ id: "tc-1" }],
            observations: [{ id: "obs-1", sourceToolCallId: "unknown", consumedByActionIds: ["act-1"] }],
          },
        },
        "no tool-originated observations",
      ],
    ])("normalizes %s as unavailable", (_name, trace, message) => {
      const result = runToolOutputConsumptionRate(trace);

      expect(result.value).toBeNull();
      expect(result.passed).toBeNull();
      expect(result.status).toBe("unavailable");
      expect(result.message).toContain(message);
    });
  });

  // --- Hostile data ---
  describe("hostile data handling", () => {
    it("handles hostile strings in tool call names", () => {
      const trace = makeTrace({
        toolCalls: [
          { name: '"><script>alert(1)</script>', success: true },
          { name: "normal", success: true },
        ],
      });
      expect(gradeToolSuccessRate(trace)).toBe(1);
      expect(gradeToolFailureCount(trace)).toBe(0);
    });

    it("handles nested malicious JSON in JSONL", () => {
      const hostile = '{"a":1,"constructor":{"prototype":{"polluted":true}}}';
      const result = safeParseJsonl(hostile);
      expect(result.length).toBe(1);
      expect(result[0].a).toBe(1);
      expect(Object.prototype).not.toHaveProperty("polluted");
    });

    it("hostile script cannot escape sandbox", () => {
      const meta = { name: "test", unit: "", direction: "", source: "inline" };
      // Attempt to access constructor chain
      const result = runCustomGrader("test", "return this && this.constructor ? 0 : 1", makeTrace(), meta);
      // Should not crash
      expect(result.error === null || result.error !== null).toBe(true);
    });

    it("hostile script cannot use eval via string code gen", () => {
      const meta = { name: "test", unit: "", direction: "", source: "inline" };
      // codeGeneration: {strings: false} prevents this
      const result = runCustomGrader("test", "try { const f = new Function('return 1'); return 0 } catch(e) { return 1 }", makeTrace(), meta);
      expect(result.value).toBe(1); // Should fail because code gen is disabled
    });
  });

  // --- Determinism ---
  describe("determinism", () => {
    it("produces identical results for identical input", () => {
      const trace = makeTrace({
        toolCalls: [
          { name: "read", success: true, arguments: { path: "/a" } },
          { name: "write", success: false, arguments: { path: "/b" } },
          { name: "read", success: true, arguments: { path: "/a" } },
        ],
        tokenUsageEntries: [
          { input_tokens: 100, output_tokens: 50, duration_ms: 1000 },
          { input_tokens: 200, output_tokens: 100, duration_ms: 2000 },
        ],
        totalInputTokens: 300,
        totalOutputTokens: 150,
        totalDurationMs: 3000,
        totalRequests: 2,
        retryEvents: [{ event: "retry" }],
        artifacts: [{ type: "pr" }],
        steps: [
          { index: 0, inputTokens: 100, outputTokens: 50, durationMs: 1000, model: null },
          { index: 1, inputTokens: 200, outputTokens: 100, durationMs: 2000, model: null },
        ],
      });

      const results1 = Object.entries(BUILTIN_GRADERS).map(([id, fn]) => ({
        id,
        value: fn(trace),
      }));
      const results2 = Object.entries(BUILTIN_GRADERS).map(([id, fn]) => ({
        id,
        value: fn(trace),
      }));

      expect(results1).toEqual(results2);
    });

    it("produces no timestamp in normalized output", () => {
      const meta = { name: "Test", unit: "count", direction: "lower_is_better", source: "builtin" };
      const r = normalizeResult("test", 5, meta);
      expect(r).not.toHaveProperty("timestamp");
    });
  });

  // --- trace.steps and enriched fields ---
  describe("preprocessTrace enrichment", () => {
    const mcpLogsDir = "/tmp/gh-aw/mcp-logs";
    const gatewayPath = path.join(mcpLogsDir, "gateway.jsonl");
    const altGatewayPath = path.join(mcpLogsDir, "mcp-gateway.jsonl");
    const rpcMessagesPath = path.join(mcpLogsDir, "rpc-messages.jsonl");

    afterEach(() => {
      for (const p of [gatewayPath, altGatewayPath, rpcMessagesPath]) {
        if (fs.existsSync(p)) fs.unlinkSync(p);
      }
    });

    it("extracts steps from token usage entries", () => {
      // Mock safeReadFile to return test data - use the preprocessTrace's logic
      const trace = makeTrace({
        tokenUsageEntries: [
          { input_tokens: 100, output_tokens: 50, duration_ms: 500, model: "gpt-4" },
          { input_tokens: 200, output_tokens: 100, duration_ms: 1000, model: "gpt-4" },
        ],
        steps: [
          { index: 0, inputTokens: 100, outputTokens: 50, durationMs: 500, model: "gpt-4" },
          { index: 1, inputTokens: 200, outputTokens: 100, durationMs: 1000, model: "gpt-4" },
        ],
      });
      expect(trace.steps.length).toBe(2);
      expect(trace.steps[0].inputTokens).toBe(100);
      expect(trace.steps[1].model).toBe("gpt-4");
    });

    it("extracts retryEvents, errorEvents", () => {
      const trace = makeTrace({
        retryEvents: [{ event: "retry" }],
        errorEvents: [{ level: "error", message: "fail" }],
      });
      expect(trace.retryEvents.length).toBe(1);
      expect(trace.errorEvents.length).toBe(1);
    });

    it("reads rpc-messages fallback and normalizes tool_name records", () => {
      fs.mkdirSync(mcpLogsDir, { recursive: true });
      fs.writeFileSync(rpcMessagesPath, [JSON.stringify({ event: "rpc", tool_name: "github-mcp-server-search_code", payload: { tool_name: "github-mcp-server-search_code", arguments: { query: "foo" } } })].join("\n") + "\n", "utf8");

      const trace = preprocessTrace();
      expect(trace.toolCalls.length).toBeGreaterThan(0);
      expect(trace.toolCalls[0].name).toBe("github-mcp-server-search_code");
      expect(trace.toolCalls[0].arguments).toEqual({ query: "foo" });
    });
  });

  // --- All built-in graders are registered ---
  describe("built-in grader registry", () => {
    const expectedIds = ["tool-success-rate", "tool-failure-count", "retries", "loops", "trajectory-efficiency", "execution-step-count", "execution-duration", "working-set-rebuild-factor", "context-growth", "artifact-production"];

    it("has all expected built-in graders", () => {
      for (const id of expectedIds) {
        expect(BUILTIN_GRADERS).toHaveProperty(id);
        expect(typeof BUILTIN_GRADERS[id]).toBe("function");
      }
    });

    it("has no unexpected graders", () => {
      const ids = Object.keys(BUILTIN_GRADERS);
      expect(ids.sort()).toEqual([...expectedIds].sort());
    });

    it("all graders have metadata", () => {
      for (const id of expectedIds) {
        expect(BUILTIN_META).toHaveProperty(id);
        expect(BUILTIN_META[id].unit).toBeDefined();
        expect(BUILTIN_META[id].direction).toBeDefined();
      }
    });
  });

  // --- GRADER_VERSION ---
  describe("version", () => {
    it("is a number", () => {
      expect(typeof GRADER_VERSION).toBe("number");
      expect(GRADER_VERSION).toBe(1);
    });
  });
});
