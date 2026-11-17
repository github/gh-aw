import { describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { main, runCustomGraders, validateOperationalValueMetrics } from "./custom_graders.cjs";
import { computeGraders } from "./trace_graders.cjs";

const provenance = { component: "agent", phase: "agent", path: "session.jsonl", index: 0 };
const session = [{ type: "tool.execution_complete", data: { success: true }, provenance }];

const javaScriptSource = `
const { eventsOfType } = helpers.createSessionLookups(session);
return [
  {id: "observations", value: eventsOfType("tool.execution_complete").length + config.offset},
  {id: "missing", value: null},
];
`;
const javaScriptDefinition = (script = javaScriptSource, extra = {}) => ({
  id: "operational-value",
  source: "operational-value",
  script,
  config: { offset: 2 },
  ...extra,
});

describe("restricted operational-value JavaScript", () => {
  it("preserves ordered metrics, primary value, diagnostics, source, and implementation digest", () => {
    const { output, manifest } = runCustomGraders(session, [javaScriptDefinition(javaScriptSource, { direction: "higher_is_better", threshold: 4 })]);
    expect(output.results[0]).toMatchObject({
      id: "operational-value",
      source: "operational-value",
      value: 3,
      status: "fail",
      diagnostics: { missing: null },
      metrics: [
        { id: "observations", value: 3 },
        { id: "missing", value: null },
      ],
      implementation: { id: "gh-aw/operational-value", version: 2, digest: expect.stringMatching(/^[a-f0-9]{64}$/) },
    });
    expect(manifest.graders[0]).not.toHaveProperty("script");
    expect(manifest.graders[0]).not.toHaveProperty("shellScript");
    expect(manifest.graders[0]).not.toHaveProperty("run");
  });

  it("uses the restricted custom-grader capabilities and immutable lookups", () => {
    const script = `
      const lookups = helpers.createSessionLookups(session);
      if (!Object.isFrozen(session) || !Object.isFrozen(config) || !Object.isFrozen(lookups.stats)) throw Error("mutable inputs");
      return [{id: "restricted", value:
        typeof process === "undefined" && typeof require === "undefined" &&
        typeof fetch === "undefined" && typeof Date === "undefined" &&
        typeof Math.random === "undefined" ? 1 : 0}];
    `;
    expect(runCustomGraders(session, [javaScriptDefinition(script)]).output.results[0]).toMatchObject({
      source: "operational-value",
      value: 1,
      metrics: [{ id: "restricted", value: 1 }],
      status: "pass",
    });
  });

  it.each([
    ["scalar", "return 1"],
    ["empty", "return []"],
    ["duplicate IDs", `return [{id: "same", value: 1}, {id: "same", value: 0}]`],
    ["empty ID", `return [{id: " ", value: 1}]`],
    ["string value", `return [{id: "score", value: "1"}]`],
    ["non-finite value", `return [{id: "score", value: NaN}]`],
    ["extra field", `return [{id: "score", value: 1, message: "no"}]`],
    ["tampered validators", `Number.isFinite = () => true; Object.keys = () => ["id", "value"]; String.prototype.trim = () => "x"; return [{id: "", value: NaN, extra: true}]`],
    ["promise", `return Promise.resolve([{id: "score", value: 1}])`],
  ])("rejects invalid %s output", (_name, script) => {
    expect(runCustomGraders(session, [javaScriptDefinition(script)]).output.results[0]).toMatchObject({
      source: "operational-value",
      value: null,
      status: "error",
      error: expect.any(String),
    });
  });

  it("validates the same metric contract at the orchestration boundary", () => {
    expect(
      validateOperationalValueMetrics([
        { id: "first", value: -2 },
        { id: "second", value: null },
      ])
    ).toEqual([
      { id: "first", value: -2 },
      { id: "second", value: null },
    ]);
    expect(() => validateOperationalValueMetrics([])).toThrow(/nonempty metric array/);
  });

  it("rejects removed Bash implementations with migration guidance", () => {
    expect(() =>
      runCustomGraders(session, [
        javaScriptDefinition(javaScriptSource, {
          run: ".github/graders/score.sh",
          shellScript: "#!/bin/bash\nprintf '[]'\n",
        }),
      ])
    ).toThrow(/run has been removed.*script/);
  });

  it("publishes source-free combined artifacts for JavaScript operational value", async () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-operational-value-js-artifact-"));
    const previousCore = global.core;
    global.core = { warning: vi.fn(), setFailed: vi.fn() };
    try {
      const builtins = computeGraders(session);
      const input = [...session, { type: "grader.manifest", data: builtins.manifest, provenance }, { type: "grader.result", data: builtins.output, provenance }];
      const sessionPath = path.join(root, "aw_session.jsonl");
      fs.writeFileSync(sessionPath, input.map(event => JSON.stringify(event)).join("\n") + "\n");
      vi.stubEnv("GH_AW_CUSTOM_GRADER_MANIFEST_CHUNKS", "1");
      vi.stubEnv("GH_AW_CUSTOM_GRADER_MANIFEST_0", JSON.stringify({ graders: [javaScriptDefinition()] }));
      await main({ sessionPath, outputDirectory: path.join(root, "results") });
      const output = JSON.parse(fs.readFileSync(path.join(root, "results/grader_results.json"), "utf8"));
      expect(output.results.at(-1)).toMatchObject({
        id: "operational-value",
        source: "operational-value",
        value: 3,
        metrics: [
          { id: "observations", value: 3 },
          { id: "missing", value: null },
        ],
      });
      const manifest = JSON.parse(fs.readFileSync(path.join(root, "results/grader_manifest.json"), "utf8"));
      const operational = manifest.graders.find(grader => grader.id === "operational-value");
      expect(operational).not.toHaveProperty("script");
      expect(operational).not.toHaveProperty("shellScript");
      expect(global.core.setFailed).not.toHaveBeenCalled();
    } finally {
      vi.unstubAllEnvs();
      global.core = previousCore;
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
});
