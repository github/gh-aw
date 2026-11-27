// @ts-check
"use strict";

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { CATEGORIES } = require("./threat_report_result.cjs");
const { supervise } = require("./threat_supervision.cjs");

function quote(value) {
  return "'" + value.replaceAll("'", "'\\''") + "'";
}

function provisionTool(dir, sink, eligibility, env) {
  const reasons = path.join(path.dirname(sink), "threat-detection-reasons.json");
  fs.rmSync(reasons, { force: true });
  const script = [
    "#!/bin/sh",
    `export THREAT_DETECTION_BOUND_RESULT_FILE=${quote(sink)}`,
    `export THREAT_DETECTION_RESULT_FILE=${quote(sink)}`,
    `exec ${quote(process.execPath)} ${quote(path.join(__dirname, "threat_report.cjs"))} "$@"`,
    "",
  ].join("\n");
  fs.writeFileSync(path.join(dir, "threat_detection_result"), script, { mode: 0o700, flag: "wx" });
  env.PATH = dir + path.delimiter + (env.PATH || "");
  env.THREAT_DETECTION_RESULT_FILE = sink;
  env.THREAT_DETECTION_REASONS_FILE = reasons;
  for (const category of CATEGORIES) {
    const key = `THREAT_DETECTION_ELIGIBLE_${category.toUpperCase()}`;
    if (typeof eligibility?.[category] === "boolean") env[key] = String(eligibility[category]);
    else delete env[key];
  }
}

function launchArgs(launch, env, promptFile, artifactsDir, sink, maxTurns) {
  const args = [...(launch.args || [])];
  const { convention, model, provider } = launch;
  const direct = launch.mode === "direct";
  switch (convention) {
    case "copilot": {
      args.push("--log-level", "all", "--disable-builtin-mcps", "--no-ask-user");
      if (artifactsDir) args.push("--add-dir", artifactsDir, "--allow-tool", `read(${artifactsDir}/**)`);
      if (sink) {
        args.push("--add-dir", path.dirname(sink), "--allow-tool", `write(${env.THREAT_DETECTION_REASONS_FILE})`, "--allow-tool", "shell(threat_detection_result:*)");
        // The SDK harness independently adds GITHUB_WORKSPACE to its sidecar.
        env.GITHUB_WORKSPACE = path.dirname(sink);
        env.GH_AW_ENGINE_CWD = path.dirname(sink);
      }
      if (!direct) env.GH_AW_PROMPT = promptFile;
      if (model) env.COPILOT_MODEL = model;
      if (env.RUNNER_TEMP && !env.GH_AW_AWF_CONFIG_PATH && !fs.existsSync("/tmp/gh-aw/awf-config.json")) {
        const mounted = path.join(env.RUNNER_TEMP, "gh-aw", "awf-config.json");
        if (fs.existsSync(mounted)) env.GH_AW_AWF_CONFIG_PATH = mounted;
      }
      break;
    }
    case "claude":
      args.push("--print", "--verbose", "--output-format", "stream-json");
      if (sink) {
        // Claude's // prefix denotes a filesystem-absolute permission rule.
        const allow = ["Bash(threat_detection_result:*)", `Write(/${env.THREAT_DETECTION_REASONS_FILE})`];
        if (artifactsDir) {
          allow.push(`Read(/${artifactsDir}/**)`);
          args.push("--add-dir", artifactsDir);
        }
        args.push("--add-dir", path.dirname(sink), "--permission-mode", "dontAsk", "--tools", "Bash,Read,Write", "--setting-sources", "", "--strict-mcp-config", "--settings", JSON.stringify({ permissions: { allow } }));
      }
      if (model) args.push("--model", model);
      if (maxTurns > 0) args.push("--max-turns", String(maxTurns));
      if (direct) args.push("-");
      break;
    case "codex":
      args.push("exec");
      if (model) args.push("-c", `model=${model}`);
      if (provider) args.push("-c", `model_provider=${provider}`);
      args.push("-c", "web_search=disabled", "-c", "fetch=disabled", "--skip-git-repo-check");
      if (sink) {
        // Codex always includes its cwd in workspace-write. Make that
        // workspace the private result directory, never the repository.
        args.push(
          "--cd",
          path.dirname(sink),
          "--sandbox",
          "workspace-write",
          "-c",
          "sandbox_workspace_write.writable_roots=[]",
          "-c",
          "sandbox_workspace_write.exclude_slash_tmp=true",
          "-c",
          "sandbox_workspace_write.exclude_tmpdir_env_var=true"
        );
      } else args.push("--sandbox", "read-only");
      if (direct) args.push("--", "-");
      break;
    default:
      throw new Error(`Unknown threat detection launch convention: ${convention}`);
  }
  if (!direct) args.push("--prompt-file", promptFile);
  return args;
}

/**
 * Run one detector attempt. launch.command + launch.args are the resolved
 * executable and argv prefix (e.g. node + [harnessPath, engineBinary]).
 * Only launch.convention selects flags. Optional launch.mode="direct" feeds
 * a bare CLI via stdin; launch.model/provider/env/cwd are resolved inputs.
 * The caller owns the sink and must validate it even on timeout/nonzero exit.
 * Subprocess output is untrusted; never forward it as workflow commands.
 */
async function runHarness({ launch, prompt, artifactsDir, sink, eligibility, maxTurns = 0, timeoutMs = 0, signal }) {
  if (signal?.aborted) return { timedOut: false, cancelled: true, exitCode: null, error: new Error("engine cancelled") };
  let dir;
  try {
    if (!launch || typeof launch.command !== "string" || !launch.command) throw new Error("Expected a resolved launch.command");
    if (launch.args !== undefined && (!Array.isArray(launch.args) || launch.args.some(arg => typeof arg !== "string"))) throw new Error("Expected launch.args to be an argv array");
    if (launch.mode !== undefined && !["harness", "direct"].includes(launch.mode)) throw new Error("Expected launch.mode to be harness or direct");
    dir = fs.mkdtempSync(path.join(os.tmpdir(), "threat-harness-"));
    const env = { ...process.env, ...launch.env };
    if (artifactsDir) {
      artifactsDir = path.resolve(artifactsDir);
      env.GH_AW_DETECTION_INPUT_DIR = artifactsDir;
    } else delete env.GH_AW_DETECTION_INPUT_DIR;
    env.GH_AW_PHASE = "detection";
    if (sink) sink = path.resolve(sink);
    // Permission rules use glob syntax. Never interpret metacharacters in a
    // caller-supplied path as part of the detector's permission policy.
    if (launch.convention !== "codex" && [artifactsDir, sink && path.dirname(sink)].some(root => root && /[*?[\]{}()\\,\r\n]/u.test(root))) throw new Error("Unsupported metacharacter in detector permission path");
    delete env.GH_AW_MAX_TURNS;
    if (maxTurns > 0) env.GH_AW_MAX_TURNS = String(maxTurns);
    if (sink) {
      provisionTool(dir, sink, eligibility, env);
      prompt = `Host-provided reasons file (literal path, JSON-encoded): ${JSON.stringify(env.THREAT_DETECTION_REASONS_FILE)}\n\n${prompt}`;
      if (["claude", "codex"].includes(launch.convention)) {
        launch = { ...launch, cwd: path.dirname(sink) };
        env.GITHUB_WORKSPACE = path.dirname(sink);
        env.GH_AW_ENGINE_CWD = path.dirname(sink);
      }
    }
    const promptFile = path.join(dir, "prompt.txt");
    if (launch.mode !== "direct") fs.writeFileSync(promptFile, prompt, { mode: 0o600, flag: "wx" });
    const args = launchArgs(launch, env, promptFile, artifactsDir, sink, maxTurns);
    return await supervise({ launch, args, env, prompt, sink, timeoutMs, signal });
  } catch (error) {
    return { timedOut: false, cancelled: false, exitCode: null, error };
  } finally {
    if (dir) fs.rmSync(dir, { force: true, recursive: true });
  }
}

module.exports = { runHarness };
