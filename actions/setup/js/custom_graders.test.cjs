import { describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { main, runCustomGraders } from "./custom_graders.cjs";
import { createSessionLookups, serializeLookupsForWorker } from "./grader_session.cjs";
import { computeGraders } from "./trace_graders.cjs";

const provenance = { component: "agent", phase: "agent", path: "agent-session.jsonl", index: 0 };
const session = [
  { type: "tool.execution_start", data: { toolName: "view", toolCallId: "a", input: { path: "a" } }, provenance },
  { type: "tool.execution_complete", data: { toolName: "view", toolCallId: "a", success: true }, provenance },
];
const definition = (script, extra = {}) => ({ id: "custom-count", source: "inline", script, ...extra });
const grade = script => runCustomGraders(session, [definition(script)]).output.results[0];

describe("isolated custom graders", () => {
  it("uses a standardized code for invalid session input", () => {
    expect(() => createSessionLookups(null)).toThrow(/^ERR_GRADER_SESSION_INVALID:/);
  });

  it("serializes the shared data-only lookup contract for the worker", () => {
    const lookups = createSessionLookups(session);
    expect(serializeLookupsForWorker(lookups)).toEqual({
      stats: lookups.stats,
      toolCalls: lookups.toolCalls,
      trace: lookups.trace,
      toolCallsById: [...lookups.toolCallsById],
    });
  });

  it("reports invalid session input with a stable error code", () => {
    expect(() => createSessionLookups(null)).toThrow("ERR_GRADER_SESSION_INVALID: graders require an in-memory unified session event array");
  });

  it("loads the published session and persists combined data-only artifacts", async () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-custom-graders-"));
    const originalCore = global.core;
    global.core = { warning: vi.fn(), setFailed: vi.fn() };
    try {
      const builtin = computeGraders(session);
      const input = [
        ...session,
        { type: "grader.manifest", data: builtin.manifest, provenance: { component: "grader", phase: "conclusion" } },
        { type: "grader.result", data: builtin.output, provenance: { component: "grader", phase: "conclusion" } },
      ];
      const sessionPath = path.join(root, "aw_session.jsonl");
      fs.writeFileSync(sessionPath, input.map(event => JSON.stringify(event)).join("\n") + "\n");
      vi.stubEnv("GH_AW_CUSTOM_GRADER_MANIFEST_CHUNKS", "1");
      vi.stubEnv("GH_AW_CUSTOM_GRADER_MANIFEST_0", JSON.stringify({ version: 2, graders: [definition("return 4"), definition("throw Error('visible job error')", { id: "custom-error" })] }));
      const outputDirectory = path.join(root, "graders");
      await main({ sessionPath, outputDirectory });
      const result = JSON.parse(fs.readFileSync(path.join(outputDirectory, "grader_results.json"), "utf8"));
      expect(result.results.slice(0, builtin.output.results.length)).toEqual(builtin.output.results);
      expect(result.results.slice(-2)).toMatchObject([
        { id: "custom-count", value: 4 },
        { id: "custom-error", status: "error" },
      ]);
      expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("visible job error"));
      expect(global.core.setFailed).toHaveBeenCalled();
      const manifest = JSON.parse(fs.readFileSync(path.join(outputDirectory, "grader_manifest.json"), "utf8"));
      expect(manifest.graders.every(grader => !Object.hasOwn(grader, "script"))).toBe(true);
      expect(fs.readFileSync(sessionPath, "utf8")).toBe(input.map(event => JSON.stringify(event)).join("\n") + "\n");
    } finally {
      vi.unstubAllEnvs();
      global.core = originalCore;
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it("receives immutable in-memory session data and shared statistics and indexes", () => {
    const { manifest, output } = runCustomGraders(session, [
      definition(
        `
        const lookups = helpers.createSessionLookups(session);
        if (lookups.eventsOfType("tool.execution_start").length !== 1) throw Error("missing event index");
        if (lookups.toolCallsById.size !== 1) throw Error("missing tool index");
        if (!Object.isFrozen(session) || !Object.isFrozen(config) || !Object.isFrozen(lookups.stats)) throw Error("mutable data");
        return { value: lookups.stats.toolCallCount + config.offset, message: "measured" };
      `,
        { config: { offset: 2 }, direction: "lower_is_better", threshold: 2 }
      ),
    ]);
    expect(output.results[0]).toMatchObject({ id: "custom-count", source: "inline", status: "fail", value: 3, message: "measured" });
    expect(output.results[0].implementation).toMatchObject({ id: "gh-aw/custom-graders", version: 2, digest: expect.stringMatching(/^[a-f0-9]{64}$/) });
    expect(manifest.graders[0]).not.toHaveProperty("script");
    expect(session[0].data.input.path).toBe("a");
  });

  it("preserves conclusion's built-in outcomes and uses separate custom processes", () => {
    const builtin = computeGraders(session);
    const input = [
      ...session,
      { type: "grader.manifest", data: builtin.manifest, provenance: { component: "grader", phase: "conclusion" } },
      { type: "grader.result", data: builtin.output, provenance: { component: "grader", phase: "conclusion" } },
    ];
    const result = runCustomGraders(input, [definition("globalThis.poison = true; return 2"), definition("return typeof poison === 'undefined' ? 3 : 0", { id: "custom-second" })]);
    expect(result.output.results.slice(0, builtin.output.results.length)).toEqual(builtin.output.results);
    expect(result.output.results.slice(-2).map(value => value.value)).toEqual([2, 3]);
  });

  it.each([
    "return typeof process === 'undefined' && typeof require === 'undefined' && typeof fetch === 'undefined' ? 1 : 0",
    "return typeof Date === 'undefined' && typeof Math.random === 'undefined' && typeof setTimeout === 'undefined' ? 1 : 0",
    "return typeof console === 'undefined' && typeof WebAssembly === 'undefined' && typeof Intl === 'undefined' ? 1 : 0",
  ])("does not expose ambient capabilities: %s", script => {
    expect(grade(script)).toMatchObject({ value: 1, status: "pass" });
  });

  it.each([
    "return session.constructor.constructor('return process')().env",
    "return helpers.createSessionLookups.constructor('return process')().env",
    "return globalThis.constructor.constructor('return process')().env",
    "return import('node:fs').then(fs => fs.readFileSync('/etc/passwd'))",
    "return require('node:child_process').execSync('echo forbidden')",
    "session.push({}); return 1",
    "helpers.createSessionLookups(session).toolCallsById.set('poison', {}); return 1",
    "JSON.stringify = () => ({ toString() { while (true) {} } }); return 1",
    "JSON.stringify = () => { throw new Proxy({}, { get() { while (true) {} } }); }; return 1",
    "return Promise.resolve(1)",
    "return NaN",
    "return { value: Infinity }",
    "throw new Error('visible failure')",
    "invalid JavaScript",
  ])("reports invalid or capability-seeking scripts as errors: %s", script => {
    const result = grade(script);
    expect(result).toMatchObject({ value: null, status: "error", error: expect.any(String) });
  });

  it("bounds execution time", () => {
    expect(grade("while (true) {}")).toMatchObject({ status: "error", error: expect.stringMatching(/timed out|ETIMEDOUT/) });
  }, 12000);

  it("never executes disabled graders", () => {
    expect(runCustomGraders(session, [definition("throw Error('disabled')", { enabled: false })]).output.results).toEqual([]);
  });

  it.each([definition("return 1", { id: "loops" }), definition("return 1", { source: "builtin" }), definition("return 1", { run: "evaluator.sh" }), definition("return 1", { id: "operational-value" })])(
    "rejects built-in overrides and unsupported execution sources",
    grader => {
      expect(() => runCustomGraders(session, [grader])).toThrow(/unsupported/);
    }
  );
});
