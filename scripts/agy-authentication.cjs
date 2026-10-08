"use strict";

const { spawn } = require("node:child_process");
const { randomBytes } = require("node:crypto");
const fs = require("node:fs");
const http = require("node:http");
const os = require("node:os");
const path = require("node:path");

const VERSION = "1.3.1";
const TERMINAL_FAILURES = new Set(["ERROR", "CANCELED", "INTERRUPTED", "INVALID"]);
const STATUSES = new Set(["SUCCESS", ...TERMINAL_FAILURES, "WAITING", "RUNNING"]);
const USAGE_FIELDS = ["input_tokens", "output_tokens", "thinking_tokens", "cache_read_tokens", "total_tokens"];

class ProbeError extends Error {}

function check(condition, message) {
  if (!condition) throw new ProbeError(message);
}

function parseEvents(stdout) {
  return stdout
    .split(/\r?\n/)
    .filter(line => line.trim())
    .map(line => {
      let event;
      try {
        event = JSON.parse(line);
      } catch {
        throw new ProbeError("Native output contains malformed JSON");
      }
      check(event && typeof event === "object" && !Array.isArray(event), "Native output contains a non-object event");
      // Startup failures can precede stream initialization and use a bare envelope.
      if (!event.event && TERMINAL_FAILURES.has(event.status)) return { event: "result", result: event };
      check(["init", "step_update", "result"].includes(event.event), "Native output contains an unknown event");
      return event;
    });
}

function summarizeEvents(events) {
  return events.map(event => {
    const summary = { event: event.event };
    if (event.event === "init") {
      summary.modelReported = typeof event.init?.model === "string";
    } else if (event.event === "step_update") {
      const step = event.step_update;
      if (Number.isSafeInteger(step?.step_index) && step.step_index >= 0) summary.stepIndex = step.step_index;
      if (["user_input", "agent_response", "tool", "checkpoint"].includes(step?.step_type)) summary.stepType = step.step_type;
      if (["ACTIVE", "DONE"].includes(step?.state)) summary.state = step.state;
      summary.hasToolError = !!step?.tool_info?.error;
    } else {
      if (STATUSES.has(event.result?.status)) summary.status = event.result.status;
      if (Number.isSafeInteger(event.result?.num_turns) && event.result.num_turns >= 0) summary.turns = event.result.num_turns;
      const usage = {};
      for (const field of USAGE_FIELDS) {
        const value = event.result?.usage?.[field];
        if (Number.isSafeInteger(value) && value >= 0) usage[field] = value;
      }
      if (Object.keys(usage).length) summary.usage = usage;
    }
    return summary;
  });
}

function runCLI({ binary, args, input = "", env, cwd, timeoutMs = 50000 }) {
  return new Promise((resolve, reject) => {
    const child = spawn(binary, args, { env, cwd, stdio: ["pipe", "pipe", "pipe"] });
    let stdout = "",
      stderr = "",
      bytes = 0,
      timedOut = false,
      outputLimit = false,
      interrupted = false;
    let escalation;
    const terminate = signal => {
      child.kill(signal);
      escalation ??= setTimeout(() => child.kill("SIGKILL"), 1000);
    };
    const onInterrupt = () => {
      interrupted = true;
      terminate("SIGINT");
    };
    const onTerminate = () => {
      interrupted = true;
      terminate("SIGTERM");
    };
    process.once("SIGINT", onInterrupt);
    process.once("SIGTERM", onTerminate);
    const timer = setTimeout(() => {
      timedOut = true;
      terminate("SIGTERM");
    }, timeoutMs);
    const collect = (stream, chunk) => {
      bytes += Buffer.byteLength(chunk, "utf8");
      if (bytes > 2 * 1024 * 1024) {
        outputLimit = true;
        terminate("SIGTERM");
        return;
      }
      if (stream === "stdout") stdout += chunk.toString("utf8");
      else stderr += chunk.toString("utf8");
    };
    child.stdout.setEncoding("utf8").on("data", chunk => collect("stdout", chunk));
    child.stderr.setEncoding("utf8").on("data", chunk => collect("stderr", chunk));
    const cleanup = () => {
      clearTimeout(timer);
      clearTimeout(escalation);
      process.removeListener("SIGINT", onInterrupt);
      process.removeListener("SIGTERM", onTerminate);
    };
    child.once("error", () => {
      cleanup();
      reject(new ProbeError("Native executable could not be started"));
    });
    child.once("close", (code, signal) => {
      cleanup();
      resolve({ code, signal, stdout, stderr, timedOut, outputLimit, interrupted });
    });
    child.stdin.on("error", error => {
      if (error.code !== "EPIPE") terminate("SIGTERM");
    });
    child.stdin.end(input);
  });
}

function completedFailure(result, events) {
  check(!result.timedOut && !result.outputLimit && !result.interrupted && !result.signal, "Negative probe did not complete normally");
  check(Number.isInteger(result.code) && result.code !== 0, "Negative probe incorrectly exited successfully");
  check(
    events.some(event => TERMINAL_FAILURES.has(event.result?.status)),
    "Negative probe has no terminal failure event"
  );
}

async function runProbe({ binary, model, apiKey, run = runCLI, platform = process.platform, arch = process.arch }) {
  check(platform === "linux" && arch === "x64", "The live authentication gate requires a clean Linux x64 runner");
  check(typeof apiKey === "string" && apiKey.trim().length > 0, "GEMINI_API_KEY is required; authentication cannot be skipped");
  check(typeof model === "string" && /^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$/.test(model), "A native model slug is required");
  check(typeof binary === "string" && path.isAbsolute(binary), "An absolute verified native executable path is required");

  const root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-agy-auth-"));
  const checks = [];
  const prompt = `Reply with exactly AUTH_${randomBytes(24).toString("hex")}. Do not use tools.`;
  const expected = prompt.match(/AUTH_[a-f0-9]+/)[0];
  let interrupted = false;
  const invoke = async (name, key, selectedModel = model, baseURL, timeout = "40s", wallTimeout = 50000) => {
    const directory = fs.mkdtempSync(path.join(root, `${name}-`));
    const home = path.join(directory, "home");
    const cwd = path.join(directory, "workspace");
    const config = path.join(home, ".gemini", "antigravity-cli");
    fs.mkdirSync(config, { recursive: true, mode: 0o700 });
    fs.mkdirSync(cwd, { mode: 0o700 });
    fs.writeFileSync(path.join(config, "settings.json"), JSON.stringify({ modelProvider: "gemini" }), { mode: 0o600, flag: "wx" });
    const env = { PATH: process.env.PATH || "/usr/bin:/bin", HOME: home, XDG_CONFIG_HOME: path.join(home, ".config"), XDG_CACHE_HOME: path.join(home, ".cache"), CI: "true", NO_COLOR: "1", TERM: "dumb" };
    if (key !== undefined) env.GEMINI_API_KEY = key;
    if (baseURL) env.GOOGLE_GEMINI_BASE_URL = baseURL;
    const result = await run({
      binary,
      cwd,
      env,
      timeoutMs: wallTimeout,
      args: ["--input-format", "stream-json", "--output-format", "stream-json", "--print-timeout", timeout, "--model", selectedModel],
      input: JSON.stringify({ event: "user", message: { content: prompt } }) + "\n",
    });
    interrupted ||= result.interrupted;
    return result;
  };
  const probe = async (name, execute) => {
    if (interrupted) return;
    const receipt = { name, status: "failed" };
    checks.push(receipt);
    try {
      await execute(receipt);
      receipt.status = "passed";
    } catch (error) {
      // Never publish arbitrary CLI, provider, assertion, or parsing diagnostics.
      receipt.error = error instanceof ProbeError ? error.message : "Probe infrastructure failed";
    }
  };

  try {
    const version = await run({ binary, args: ["--version"], env: { PATH: process.env.PATH || "/usr/bin:/bin", HOME: root }, cwd: root, timeoutMs: 10000 });
    check(version.code === 0 && !version.timedOut && !version.outputLimit && !version.interrupted && !version.signal && version.stdout.trim() === VERSION, "Native executable does not match the pinned release");

    await probe("missing-api-key", async receipt => {
      const result = await invoke("missing", undefined);
      check(Number.isInteger(result.code) && result.code !== 0 && !result.timedOut && !result.signal && !result.outputLimit && !result.interrupted, "Missing API key did not fail promptly");
      check(/GEMINI_API_KEY/.test(result.stdout + result.stderr), "Missing API key diagnostic does not identify GEMINI_API_KEY");
      receipt.exitCode = result.code;
    });
    await probe("invalid-api-key", async receipt => {
      const result = await invoke("invalid", "gh-aw-deliberately-invalid-gemini-api-key");
      const events = parseEvents(result.stdout);
      receipt.events = summarizeEvents(events);
      completedFailure(result, events);
      check(/api.?key|unauthenticated|unauthorized|permission.denied|invalid.*key|credential/i.test(result.stdout + result.stderr), "Invalid key lacks an actionable authentication diagnostic");
      receipt.exitCode = result.code;
    });
    await probe("actual-api-key-inference", async receipt => {
      const result = await invoke("inference", apiKey);
      const events = parseEvents(result.stdout);
      receipt.events = summarizeEvents(events);
      check(result.code === 0 && !result.timedOut && !result.outputLimit && !result.interrupted && !result.signal, "Authenticated inference did not complete successfully");
      const init = events.filter(event => event.event === "init");
      const finals = events.filter(event => event.event === "result");
      check(init.length === 1 && finals.length === 1, "Authenticated inference requires one init and one result");
      check(init[0].init?.model === model, "Native initialization did not confirm the requested model");
      check(!events.some(event => event.step_update?.step_type === "tool"), "Authentication probe unexpectedly used a tool");
      const final = finals[0].result;
      check(final?.status === "SUCCESS" && final.num_turns === 1, "Authenticated inference has no successful single-turn result");
      check(typeof final.response === "string" && final.response.trim() === expected, "Authenticated inference did not return the fresh challenge");
      check(Number.isSafeInteger(final.usage?.total_tokens) && final.usage.total_tokens > 0, "Authenticated inference has no positive native token usage");
      receipt.requestedModelConfirmed = true;
    });
    await probe("unknown-model", async receipt => {
      const result = await invoke("unknown-model", apiKey, "gh-aw-nonexistent-authentication-model");
      const events = parseEvents(result.stdout);
      receipt.events = summarizeEvents(events);
      completedFailure(result, events);
      check(/model.*(not recognized|unknown|invalid|not found)|invalid model/i.test(result.stdout + result.stderr), "Unknown model lacks an actionable model diagnostic");
      receipt.exitCode = result.code;
    });
    await probe("controlled-gemini-endpoint", async receipt => {
      const key = "gh-aw-controlled-endpoint-key";
      let requests = 0,
        authenticated = false,
        geminiProtocol = false;
      const server = http.createServer((request, response) => {
        requests++;
        const url = new URL(request.url, "http://localhost");
        authenticated ||= request.headers["x-goog-api-key"] === key || url.searchParams.get("key") === key;
        geminiProtocol ||= request.method === "POST" && /\/models\/[^/]+:(streamGenerateContent|generateContent)$/.test(url.pathname);
        request.resume();
        response.writeHead(401, { "content-type": "application/json" });
        response.end(JSON.stringify({ error: { code: 401, status: "UNAUTHENTICATED", message: "Controlled endpoint rejected the probe credential" } }));
      });
      try {
        await new Promise((resolve, reject) => {
          server.once("error", reject);
          server.listen(0, "127.0.0.1", resolve);
        });
        const result = await invoke("endpoint", key, model, `http://127.0.0.1:${server.address().port}`);
        const events = parseEvents(result.stdout);
        receipt.events = summarizeEvents(events);
        completedFailure(result, events);
        check(requests > 0 && authenticated && geminiProtocol, "GOOGLE_GEMINI_BASE_URL did not produce the expected authenticated Gemini request");
        receipt.requests = requests;
        receipt.authHeaderOrQueryConfirmed = authenticated;
        receipt.geminiProtocolConfirmed = geminiProtocol;
      } finally {
        server.closeAllConnections();
        await new Promise(resolve => server.close(resolve));
      }
    });
    return {
      engine: "agy",
      experimental: true,
      version: VERSION,
      platform,
      architecture: arch,
      status: checks.length === 5 && checks.every(item => item.status === "passed") ? "passed" : "failed",
      checks,
      awfConformance: "not-run",
    };
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
}

async function main() {
  const binary = process.env.AGY_BINARY;
  const model = process.env.AGY_MODEL;
  const output = process.env.AGY_AUTH_EVIDENCE;
  check(typeof output === "string" && path.isAbsolute(output), "An absolute AGY_AUTH_EVIDENCE path is required");
  let report;
  try {
    report = await runProbe({ binary, model, apiKey: process.env.GEMINI_API_KEY });
  } catch (error) {
    report = { engine: "agy", experimental: true, version: VERSION, status: "failed", checks: [], error: error instanceof ProbeError ? error.message : "Probe infrastructure failed", awfConformance: "not-run" };
  }
  fs.mkdirSync(path.dirname(output), { recursive: true, mode: 0o700 });
  fs.writeFileSync(output, JSON.stringify(report, null, 2) + "\n", { mode: 0o600, flag: "wx" });
  const summary = [
    "## Experimental Agy native authentication gate",
    "",
    "| Probe | Result |",
    "|---|---|",
    ...report.checks.map(item => `| ${item.name} | ${item.status} |`),
    "",
    `Native authentication: **${report.status}**.`,
    "AWF/gateway conformance has not been run; this is not engine release approval.",
    "",
  ].join("\n");
  if (process.env.GITHUB_STEP_SUMMARY) fs.appendFileSync(process.env.GITHUB_STEP_SUMMARY, summary);
  console.log(summary);
  for (const item of report.checks.filter(item => item.status === "failed")) console.error(`${item.name}: ${item.error}`);
  if (report.error) console.error(report.error);
  if (report.status !== "passed") process.exitCode = 1;
}

if (require.main === module) {
  main().catch(() => {
    console.error("Could not write authentication gate evidence");
    process.exitCode = 1;
  });
}

module.exports = { VERSION, parseEvents, summarizeEvents, runCLI, runProbe };
