// @ts-check
"use strict";

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawn } = require("node:child_process");
const { CATEGORIES, readSink } = require("./threat_report_result.cjs");
// Poll for a structurally complete report, even an ineligible one. Eligibility
// is enforced by the host after exit, not by letting the attempt time out.
const ALL_ELIGIBLE = Object.fromEntries(CATEGORIES.map(key => [key, true]));

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

function launchArgs(launch, env, promptFile, sink, maxTurns) {
  const args = [...(launch.args || [])];
  const { convention, model, provider } = launch;
  const direct = launch.mode === "direct";
  switch (convention) {
    case "copilot": {
      args.push("--add-dir", path.dirname(promptFile), "--log-level", "all", "--disable-builtin-mcps", "--no-ask-user", "--allow-tool", "write", "--allow-tool", "shell(threat_detection_result:*)");
      if (sink) args.push("--add-dir", path.dirname(sink));
      if (env.GITHUB_WORKSPACE) args.push("--add-dir", env.GITHUB_WORKSPACE);
      if (model) env.COPILOT_MODEL = model;
      if (env.RUNNER_TEMP && !env.GH_AW_AWF_CONFIG_PATH && !fs.existsSync("/tmp/gh-aw/awf-config.json")) {
        const mounted = path.join(env.RUNNER_TEMP, "gh-aw", "awf-config.json");
        if (fs.existsSync(mounted)) env.GH_AW_AWF_CONFIG_PATH = mounted;
      }
      break;
    }
    case "claude":
      args.push("--print", "--verbose", "--output-format", "stream-json");
      if (sink) args.push("--permission-mode", "acceptEdits", "--allowed-tools", "Bash,Write,Edit,Read,Read(/tmp/*),Read(/tmp/gh-aw/*)");
      if (model) args.push("--model", model);
      if (maxTurns > 0) args.push("--max-turns", String(maxTurns));
      if (direct) args.push("-");
      break;
    case "codex":
      args.push("exec");
      if (model) args.push("-c", `model=${model}`);
      if (provider) args.push("-c", `model_provider=${provider}`);
      args.push("-c", "web_search=disabled", "-c", "fetch=disabled", "--sandbox", "workspace-write", "--skip-git-repo-check");
      if (sink) args.push("--add-dir", path.dirname(sink));
      if (direct) args.push("--", "-");
      break;
    default:
      throw new Error(`Unknown threat detection launch convention: ${convention}`);
  }
  if (!direct) args.push("--prompt-file", promptFile);
  return args;
}

function kill(pid, signal) {
  try {
    process.kill(pid, signal);
  } catch (error) {
    if (error.code !== "ESRCH") throw error;
  }
}

// The built-in JS harnesses run their CLIs in *separate* process groups.
// On Linux freeze the owned tree before terminating it so a nested group
// isn't lost to reparenting when its harness exits.
function stopTree(pid) {
  if (!pid) return;
  if (process.platform === "linux") {
    const pending = [pid];
    const visited = new Set();
    while (pending.length) {
      const current = pending.pop();
      if (visited.has(current)) continue;
      visited.add(current);
      kill(current, "SIGSTOP");
      const tasks = `/proc/${current}/task`;
      try {
        for (const tid of fs.readdirSync(tasks)) {
          for (const child of fs.readFileSync(`${tasks}/${tid}/children`, "utf8").match(/\d+/g) || []) pending.push(Number(child));
        }
      } catch (error) {
        // /proc can hide descendants. Still signal every discovered pid and
        // the original group, rather than leaving a SIGSTOP'ed harness behind.
        if (!["ENOENT", "ESRCH", "EACCES", "EPERM"].includes(error.code)) throw error;
      }
    }
    for (const descendant of [...visited].reverse()) kill(descendant, "SIGKILL");
  }
  kill(-pid, "SIGKILL");
}

function resultReady(sink) {
  try {
    return Boolean(sink && readSink(sink, ALL_ELIGIBLE));
  } catch {
    return false;
  }
}

function supervise({ launch, args, env, prompt, sink, timeoutMs, signal }) {
  return new Promise(resolve => {
    const child = spawn(launch.command, args, { env, cwd: launch.cwd, detached: true, stdio: ["pipe", "ignore", "ignore"] });
    let timedOut = false;
    let cancelled = false;
    let stopped = false;
    /** @type {Error|null} */
    let error = null;
    function stop(reason) {
      if (stopped) return;
      stopped = true;
      timedOut = reason === "timeout";
      cancelled = reason === "cancelled";
      if (timedOut || cancelled) error = new Error(timedOut ? "engine timed out" : "engine cancelled");
      stopTree(child.pid);
    }
    const abort = () => stop("cancelled");
    const timer = timeoutMs > 0 ? setTimeout(() => stop("timeout"), timeoutMs) : undefined;
    const poll = sink
      ? setInterval(() => {
          if (resultReady(sink)) stop("reported");
        }, 250)
      : undefined;
    signal?.addEventListener("abort", abort, { once: true });
    if (signal?.aborted) abort();
    child.stdin.on(
      "error",
      /** @param {NodeJS.ErrnoException} err */ err => {
        if (err.code !== "EPIPE" && err.code !== "ECONNRESET") error ??= err;
      }
    );
    child.stdin.end(launch.mode === "direct" ? prompt : undefined);
    child.once("error", err => {
      error ??= err;
    });
    child.once("close", (exitCode, exitSignal) => {
      clearTimeout(timer);
      clearInterval(poll);
      signal?.removeEventListener("abort", abort);
      if (!stopped && child.pid) kill(-child.pid, "SIGKILL"); // residual tools in the owned group
      if (!stopped && exitCode !== 0 && !resultReady(sink)) error ??= new Error(`engine exited with ${exitSignal || `code ${exitCode}`}`);
      resolve({ timedOut, cancelled, exitCode, error });
    });
  });
}

/**
 * Run one detector attempt. launch.command + launch.args are the resolved
 * executable and argv prefix (e.g. node + [harnessPath, engineBinary]).
 * Only launch.convention selects flags. Optional launch.mode="direct" feeds
 * a bare CLI via stdin; launch.model/provider/env/cwd are resolved inputs.
 * The caller owns the sink and must validate it even on timeout/nonzero exit.
 * Subprocess output is untrusted; never forward it as workflow commands.
 */
async function runHarness({ launch, prompt, sink, eligibility, maxTurns = 0, timeoutMs = 0, signal }) {
  if (signal?.aborted) return { timedOut: false, cancelled: true, exitCode: null, error: new Error("engine cancelled") };
  let dir;
  try {
    if (!launch || typeof launch.command !== "string" || !launch.command) throw new Error("Expected a resolved launch.command");
    if (launch.args !== undefined && (!Array.isArray(launch.args) || launch.args.some(arg => typeof arg !== "string"))) throw new Error("Expected launch.args to be an argv array");
    if (launch.mode !== undefined && !["harness", "direct"].includes(launch.mode)) throw new Error("Expected launch.mode to be harness or direct");
    dir = fs.mkdtempSync(path.join(os.tmpdir(), "threat-harness-"));
    const env = { ...process.env, ...launch.env };
    delete env.GH_AW_MAX_TURNS;
    if (maxTurns > 0) env.GH_AW_MAX_TURNS = String(maxTurns);
    if (sink) {
      sink = path.resolve(sink);
      provisionTool(dir, sink, eligibility, env);
    }
    const promptFile = path.join(dir, "prompt.txt");
    if (launch.mode !== "direct") fs.writeFileSync(promptFile, prompt, { mode: 0o600, flag: "wx" });
    const args = launchArgs(launch, env, promptFile, sink, maxTurns);
    return await supervise({ launch, args, env, prompt, sink, timeoutMs, signal });
  } catch (error) {
    return { timedOut: false, cancelled: false, exitCode: null, error };
  } finally {
    if (dir) fs.rmSync(dir, { force: true, recursive: true });
  }
}

module.exports = { runHarness };
