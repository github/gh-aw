const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const cases = require("./testdata/threat-detector/v053.json");

// Run these same fixtures against the release executable when explicitly provided.
// Normal CI compares against the checked-in v0.5.3 observations without a download.
const producers = ["JavaScript"];
if (process.env.GH_AW_TEST_THREAT_DETECT_V053) producers.push("Go v0.5.3");
const roots = [];
afterEach(() => {
  for (const root of roots.splice(0)) fs.rmSync(root, { recursive: true, force: true });
});

function fixture() {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "detector-parity-"));
  roots.push(root);
  fs.mkdirSync(path.join(root, "artifacts/aw-prompts"), { recursive: true });
  for (const file of ["prompt.txt", "prompt-template.txt"]) fs.writeFileSync(path.join(root, "artifacts/aw-prompts", file), "Static prompt");
  fs.writeFileSync(path.join(root, "artifacts/agent_output.json"), '{"items":[]}');
  const engine = path.join(root, "fixture-reviewer");
  fs.copyFileSync(path.join(__dirname, "testdata/threat-detector/harness.sh"), engine);
  fs.chmodSync(engine, 0o700);
  fs.symlinkSync(engine, path.join(root, "copilot"));
  fs.writeFileSync(path.join(root, "launch.json"), JSON.stringify({ command: engine, convention: "copilot", mode: "direct" }));
  return root;
}

function run(root, command, args, env) {
  const child = spawnSync(command, args, { cwd: root, env, encoding: "utf8", timeout: 8000 });
  if (child.error) throw child.error;
  return child;
}

describe.each(producers)("%s sandbox producer to host conclusion", producer => {
  it.each(cases.scenarios)("$name", spec => {
    const root = fixture();
    const env = {
      ...process.env,
      PATH: root + path.delimiter + process.env.PATH,
      SCENARIO: spec.name,
      GH_AW_DETECTION_CONTINUE_ON_ERROR: "false",
      GH_AW_DETECTION_CONTINUE_ON_WARNING: spec.name === "strict-warning" ? "false" : "true",
    };
    const js = producer === "JavaScript";
    const common = ["--output", "detection_result.json", "--retries", String(spec.retries || 0), "--engine-timeout", spec.timeout || "5s", "artifacts"];
    const child = run(root, js ? process.execPath : process.env.GH_AW_TEST_THREAT_DETECT_V053, (js ? [path.join(__dirname, "detect_threats.cjs"), "--launch-config", "launch.json"] : ["--engine", "copilot"]).concat(common), env);
    expect(child.stderr.trimEnd().split("\n").at(-1)).toBe(`THREAT_DETECTION_STATUS: reason=${spec.reason} exit=${spec.exit}`);
    expect(child.status).toBe(spec.exit);
    const attempts = path.join(root, "attempts");
    expect(fs.existsSync(attempts) ? fs.readFileSync(attempts, "utf8").trim().split("\n").length : 0).toBe(spec.attempts);
    for (const [file, full] of [
      ["detection_result.json", false],
      ["detection_result_full.json", true],
    ]) {
      const resultFile = path.join(root, file);
      if (spec.exit === 2) expect(fs.existsSync(resultFile)).toBe(false);
      else
        expect(JSON.parse(fs.readFileSync(resultFile, "utf8"))).toEqual({
          prompt_injection: false,
          secret_leak: spec.exit === 1,
          malicious_patch: false,
          reasons: full && spec.exit === 1 ? ["private fixture reason"] : [],
          warnings: cases.warnings,
        });
    }
    fs.writeFileSync(path.join(root, "detection.log"), child.stderr);
    const githubOutput = path.join(root, "github-output");
    const conclude = run(root, process.execPath, [path.join(__dirname, "conclude_threat_detection.cjs"), "detection_result.json"], { ...env, RUN_DETECTION: "true", GITHUB_OUTPUT: githubOutput, GITHUB_ENV: path.join(root, "github-env") });
    expect(conclude.status).toBe(spec.exit === 0 ? 0 : 1);
    const outputs = fs.readFileSync(githubOutput, "utf8").trimEnd().split("\n");
    expect(outputs).toContain(`reason=${spec.hostReason}`);
    expect(outputs).toContain(`threat_kinds=${spec.exit === 1 ? "secret_leak" : ""}`);
  });
});

describe("eligibility after runner staging", () => {
  it.each([
    ["copilot", "empty output", [false, false, false]],
    ["claude", "comment memory", [true, true, false]],
    ["codex", "unstaged patch", [false, true, true]],
  ])("%s: %s", (convention, scenario, eligible) => {
    const root = fixture();
    const source = path.join(root, "artifacts");
    fs.writeFileSync(path.join(source, "agent_output.json"), "");
    fs.writeFileSync(path.join(source, "aw-prompts/prompt-import-tree.json"), "{}");
    if (scenario === "comment memory") {
      fs.mkdirSync(path.join(source, "comment-memory"));
      fs.writeFileSync(path.join(source, "comment-memory/prior.md"), "prior comment");
    }
    const env = { ...process.env, HAS_PATCH: String(scenario === "unstaged patch"), GH_AW_DETECTION_CONTINUE_ON_WARNING: "true", GH_AW_DETECTION_CONTINUE_ON_ERROR: "true" };
    const stage = run(root, "bash", [path.join(__dirname, "../sh/prepare_threat_detection_files.sh"), source], env);
    expect(stage.status, stage.stderr).toBe(0);
    const command = path.join(root, "fixture-reviewer");
    fs.writeFileSync(
      command,
      `#!/bin/sh
set -eu
cat >/dev/null
printf '%s\\n' "$THREAT_DETECTION_ELIGIBLE_PROMPT_INJECTION" "$THREAT_DETECTION_ELIGIBLE_SECRET_LEAK" "$THREAT_DETECTION_ELIGIBLE_MALICIOUS_PATCH" > "$FIXTURE_ELIGIBILITY"
exec threat_detection_result --prompt-injection=false --secret-leak=false --malicious-patch=false
`
    );
    fs.writeFileSync(path.join(root, "launch.json"), JSON.stringify({ command, convention, mode: "direct" }));
    const verdict = path.join(source, "threat-detection/detection_result.json");
    const detect = run(root, process.execPath, [path.join(__dirname, "detect_threats.cjs"), "--launch-config", "launch.json", "--output", verdict, path.join(source, "threat-detection-inputs")], {
      ...env,
      FIXTURE_ELIGIBILITY: path.join(root, "eligibility"),
    });
    expect(detect.status, detect.stderr).toBe(0);
    expect(fs.readFileSync(path.join(root, "eligibility"), "utf8").trim().split("\n")).toEqual(eligible.map(String));
    const warnings = JSON.parse(fs.readFileSync(verdict, "utf8")).warnings;
    if (scenario === "unstaged patch") expect(warnings).toContainEqual(expect.objectContaining({ field: "patch", message: expect.stringContaining("HAS_PATCH=true") }));
    expect(warnings.map(warning => warning.field)).toEqual(scenario === "unstaged patch" ? ["agent_output", "patch"] : ["agent_output"]);
    const output = path.join(root, "github-output");
    const host = run(root, process.execPath, [path.join(__dirname, "conclude_threat_detection.cjs"), verdict], { ...env, RUN_DETECTION: "true", GITHUB_OUTPUT: output });
    expect(host.status, host.stderr).toBe(0);
    expect(fs.readFileSync(output, "utf8").split("\n")).toEqual(expect.arrayContaining(["success=true", "conclusion=success"]));
  });
});

describe("result destinations", () => {
  it("does not leave a stale conventional sidecar when full output is disabled", () => {
    const root = fixture();
    const stale = path.join(root, "result_full.json");
    fs.writeFileSync(stale, '{"reasons":["from an earlier run"]}');
    const child = run(root, process.execPath, [path.join(__dirname, "detect_threats.cjs"), "--launch-config", "launch.json", "--output", "result.json", "--full-output=", "artifacts"], { ...process.env, SCENARIO: "clean" });
    expect(child.status).toBe(0);
    expect(fs.existsSync(stale)).toBe(false);
  });
  it.each(["strict-warning", "timeout"])("invalidates both old results before %s", scenario => {
    const root = fixture();
    const files = ["result.json", "result_full.json"].map(file => path.join(root, file));
    for (const file of files) fs.writeFileSync(file, '{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}');
    const child = run(root, process.execPath, [path.join(__dirname, "detect_threats.cjs"), "--launch-config", "launch.json", "--output", "result.json", "--engine-timeout", "150ms", "artifacts"], {
      ...process.env,
      SCENARIO: scenario,
      GH_AW_DETECTION_CONTINUE_ON_WARNING: scenario === "strict-warning" ? "false" : "true",
    });
    expect(child.status).toBe(2);
    for (const file of files) expect(fs.existsSync(file)).toBe(false);
  });
  it("rejects aliased outputs before invoking the engine", () => {
    const root = fixture();
    fs.symlinkSync(root, path.join(root, "alias"));
    const child = run(root, process.execPath, [path.join(__dirname, "detect_threats.cjs"), "--launch-config", "launch.json", "--output", "result", "--full-output", "alias/result", "artifacts"], { ...process.env, SCENARIO: "clean" });
    expect(child.stderr).toContain("reason=config_error exit=2");
    expect(fs.existsSync(path.join(root, "attempts"))).toBe(false);
  });

  it.each([
    ["result", "missing/full", "result_recorded", 0],
    ["missing/public", "full", "output_write_error", 2],
  ])("distinguishes private and public write failures (%s, %s)", (redacted, full, reason, code) => {
    const root = fixture();
    const child = run(root, process.execPath, [path.join(__dirname, "detect_threats.cjs"), "--launch-config", "launch.json", "--output", redacted, "--full-output", full, "artifacts"], { ...process.env, SCENARIO: "clean" });
    expect(child.stderr).toContain(`reason=${reason} exit=${code}`);
    expect(child.status).toBe(code);
  });
});
