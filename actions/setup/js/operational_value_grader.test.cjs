import { describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { executeOperationalValueEvaluator, validateOperationalValueMetrics } from "./operational_value_grader.cjs";
import { main, runCustomGraders } from "./custom_graders.cjs";
import { computeGraders } from "./trace_graders.cjs";

const provenance = { component: "agent", phase: "agent", path: "session.jsonl", index: 0 };
const session = [{ type: "tool.execution_complete", data: { success: true }, provenance }];
const source = `#!/usr/bin/env bash
[[ "$1" == "$GH_AW_SESSION_FILE" ]]
jq -s --argjson config "$GH_AW_GRADER_CONFIG" '[{id: "observations", value: (length + $config.offset)}, {id: "missing", value: null}]' "$1"
`;
const definition = (shellScript = source, extra = {}) => ({
  id: "operational-value",
  source: "operational-value",
  run: ".github/graders/score.sh",
  shellScript,
  config: { offset: 2 },
  ...extra,
});

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

  it("rejects mixed JavaScript and Bash implementations", () => {
    expect(() =>
      runCustomGraders(session, [
        javaScriptDefinition(javaScriptSource, {
          run: ".github/graders/score.sh",
          shellScript: "#!/bin/bash\nprintf '[]'\n",
        }),
      ])
    ).toThrow(/unsupported/);
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

describe("separate-job operational-value Bash", () => {
  it("passes a session file and config without the old request wrapper", () => {
    expect(executeOperationalValueEvaluator(source, session, { offset: 2 })).toEqual([
      { id: "observations", value: 3 },
      { id: "missing", value: null },
    ]);
    expect(session).toHaveLength(1);
  });

  it("keeps legacy metric arrays, using the first as the primary result", () => {
    const { output, manifest } = runCustomGraders(session, [definition(source, { direction: "higher_is_better", threshold: 4 })]);
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
    expect(manifest.graders[0]).not.toHaveProperty("shellScript");
    expect(manifest.graders[0]).not.toHaveProperty("script");
  });

  it("does not inherit credentials, shell startup hooks, or the working checkout", () => {
    vi.stubEnv("GH_TOKEN", "test-credential");
    vi.stubEnv("BASH_ENV", "/does-not-exist");
    try {
      const script = `#!/bin/bash
[[ -z "\${GH_TOKEN+x}" && -z "\${GITHUB_TOKEN+x}" && -z "\${BASH_ENV+x}" && -z "\${GITHUB_WORKSPACE+x}" ]]
[[ "$PWD" == */gh-aw-operational-value-* ]]
printf '[{"id":"isolated-env","value":1}]'
`;
      expect(executeOperationalValueEvaluator(script, [], {})).toEqual([{ id: "isolated-env", value: 1 }]);
    } finally {
      vi.unstubAllEnvs();
    }
  });

  it.each([
    "printf not-json",
    "printf '[]'",
    'printf \'[{"id":"a","value":"unavailable"}]\'',
    'printf \'[{"id":"a","value":1},{"id":"a","value":2}]\'',
    'printf \'[{"id":"a","value":1,"extra":true}]\'',
    "exit 4",
    "cat /missing-grader-session",
  ])("surfaces invalid output or execution failures: %s", script => {
    expect(() => executeOperationalValueEvaluator(script, [], {})).toThrow(/operational-value/);
    expect(runCustomGraders([], [definition("#!/bin/bash\n" + script)]).output.results[0]).toMatchObject({ status: "error", value: null });
  });

  it("bounds execution and output and removes staging files on errors", () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-shell-test-"));
    try {
      expect(() => executeOperationalValueEvaluator("while :; do :; done", [], {}, { timeout: 50, temporaryRoot: root })).toThrow(/limit/);
      const start = Date.now();
      expect(() => executeOperationalValueEvaluator("sleep 30 & wait", [], {}, { timeout: 50, temporaryRoot: root })).toThrow(/limit/);
      expect(Date.now() - start).toBeLessThan(3000);
      expect(() => executeOperationalValueEvaluator("printf '%1100000s' x", [], {}, { temporaryRoot: root })).toThrow(/limit/);
      expect(fs.readdirSync(root)).toEqual([]);
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it("does not execute disabled shell definitions", () => {
    expect(runCustomGraders([], [definition("#!/bin/bash\nexit 9", { enabled: false })]).output.results).toEqual([]);
  });

  it.each(["#!/usr/bin/bash", "#!/usr/bin/env -S bash -eu", "#!/bin/bash -e", "#!/bin/env bash"])("accepts compiler-supported Bash shebangs: %s", shebang => {
    const script = shebang + '\nprintf \'[{"id":"compatible","value":1}]\'\n';
    expect(runCustomGraders([], [definition(script)]).output.results[0]).toMatchObject({ value: 1, status: "pass" });
  });

  it("publishes combined source-free artifacts and fails the job on shell errors", async () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-shell-artifact-test-"));
    const previousCore = global.core;
    global.core = { warning: vi.fn(), setFailed: vi.fn() };
    try {
      const builtins = computeGraders(session);
      const input = [...session, { type: "grader.manifest", data: builtins.manifest, provenance }, { type: "grader.result", data: builtins.output, provenance }];
      const original = input.map(event => JSON.stringify(event)).join("\n") + "\n";
      const sessionPath = path.join(root, "aw_session.jsonl");
      fs.writeFileSync(sessionPath, original);
      vi.stubEnv("GH_AW_CUSTOM_GRADER_MANIFEST_CHUNKS", "1");
      vi.stubEnv("GH_AW_CUSTOM_GRADER_MANIFEST_0", JSON.stringify({ graders: [definition("#!/bin/bash\nexit 3")] }));
      await main({ sessionPath, outputDirectory: path.join(root, "results") });
      const output = JSON.parse(fs.readFileSync(path.join(root, "results/grader_results.json"), "utf8"));
      expect(output.results.slice(0, builtins.output.results.length)).toEqual(builtins.output.results);
      expect(output.results.at(-1)).toMatchObject({ id: "operational-value", status: "error", source: "operational-value" });
      expect(global.core.setFailed).toHaveBeenCalled();
      expect(global.core.warning).toHaveBeenCalled();
      expect(fs.readFileSync(path.join(root, "results/grader_manifest.json"), "utf8")).not.toContain("shellScript");
      expect(fs.readFileSync(sessionPath, "utf8")).toBe(original);
    } finally {
      vi.unstubAllEnvs();
      global.core = previousCore;
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
});
