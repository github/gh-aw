import { afterEach, beforeEach, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const { runHarness } = require("./threat_harness.cjs");
let root, workspace, artifactsDir, outputDir, sink, engine;

beforeEach(() => {
  root = fs.mkdtempSync(path.join(os.tmpdir(), "detector permissions-"));
  [workspace, artifactsDir, outputDir] = ["workspace", "staged files", "private"].map(name => path.join(root, name));
  for (const dir of [workspace, artifactsDir, outputDir]) fs.mkdirSync(dir);
  sink = path.join(outputDir, "sink.json");
  engine = path.join(root, "probe.cjs");
  fs.writeFileSync(
    engine,
    `
    const fs = require("node:fs"), path = require("node:path"), env = process.env;
    const args = process.argv.slice(2), sdkPrompt = env.GH_AW_PROMPT;
    const file = args.includes("--prompt-file") ? args[args.indexOf("--prompt-file") + 1] : null;
    fs.writeFileSync(env.CAPTURE, JSON.stringify({
      args, file, sdkPrompt, cwd:process.cwd(), workspace:env.GITHUB_WORKSPACE,
      prompt:fs.readFileSync(file || 0, "utf8"), shimDir:env.PATH.split(path.delimiter)[0],
      input:env.GH_AW_DETECTION_INPUT_DIR
    }));
  `
  );
});
afterEach(() => fs.rmSync(root, { recursive: true, force: true }));

async function observe(convention, mode) {
  const capture = path.join(root, "capture.json");
  const result = await runHarness({
    launch: {
      command: process.execPath,
      args: [engine],
      convention,
      mode,
      cwd: workspace,
      env: { CAPTURE: capture, GITHUB_WORKSPACE: workspace, GH_AW_PROMPT: path.join(workspace, "stale-prompt"), GH_AW_DETECTION_INPUT_DIR: workspace },
    },
    prompt: "detector instructions",
    sink,
    artifactsDir: path.relative(process.cwd(), artifactsDir),
    timeoutMs: 5000,
  });
  expect(result.error).toBeNull();
  return JSON.parse(fs.readFileSync(capture, "utf8"));
}

const values = (args, flag) => args.flatMap((arg, i) => (arg === flag ? [args[i + 1]] : []));
it.each(["direct", "harness"])("scopes Copilot paths and supplies literal reasons in %s mode", async mode => {
  const seen = await observe("copilot", mode);
  const reasons = path.join(outputDir, "threat-detection-reasons.json");
  expect(seen.input).toBe(artifactsDir);
  expect(seen.workspace).toBe(outputDir);
  expect(values(seen.args, "--add-dir")).toEqual([artifactsDir, outputDir]);
  expect(values(seen.args, "--allow-tool")).toEqual([`read(${artifactsDir}/**)`, `write(${reasons})`, "shell(threat_detection_result:*)"]);
  for (const dir of [artifactsDir, outputDir, workspace]) expect(seen.shimDir.startsWith(dir + path.sep) || seen.shimDir === dir).toBe(false);
  if (mode === "harness") expect(seen.sdkPrompt).toBe(seen.file);
  expect(seen.prompt).toContain(JSON.stringify(reasons));
});

it("confines Claude's default read root and only approves detector operations", async () => {
  const seen = await observe("claude", "harness");
  expect(seen.cwd).toBe(outputDir);
  expect(values(seen.args, "--add-dir")).toEqual([artifactsDir, outputDir]);
  expect(values(seen.args, "--permission-mode")).toEqual(["dontAsk"]);
  expect(values(seen.args, "--tools")).toEqual(["Bash,Read,Write"]);
  const settings = JSON.parse(values(seen.args, "--settings")[0]);
  expect(settings.permissions.allow).toEqual(["Bash(threat_detection_result:*)", `Write(/${outputDir}/threat-detection-reasons.json)`, `Read(/${artifactsDir}/**)`]);
});
