"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { test } = require("node:test");
const { spawnSync } = require("node:child_process");
const { VERSION, parseEvents, summarizeEvents, runCLI, runProbe } = require("./agy-authentication.cjs");

const eventResult = result => JSON.stringify({ event: "result", result }) + "\n";
const completed = stdout => ({ code: 0, signal: null, stdout, stderr: "", timedOut: false, outputLimit: false, interrupted: false });
const failed = message => ({ ...completed(eventResult({ status: "ERROR", error: message })), code: 1 });

test("parses native streams and startup failure envelopes, rejects malformed events", () => {
  assert.deepEqual(parseEvents(eventResult({ status: "SUCCESS" })), [{ event: "result", result: { status: "SUCCESS" } }]);
  assert.deepEqual(parseEvents('{"status":"ERROR","error":"invalid model"}'), [{ event: "result", result: { status: "ERROR", error: "invalid model" } }]);
  for (const value of ["not-json", "null", "[]", '{"event":"unexpected"}']) {
    assert.throws(() => parseEvents(value));
  }
});

test("evidence preserves only allowlisted metadata, not credentials, prompts, diagnostics, or tool payloads", () => {
  const secret = "provider-secret-value";
  const events = [
    { event: "init", init: { model: secret, cwd: secret, tools: [secret] }, conversation_id: secret },
    { event: "step_update", step_update: { step_index: 2, step_type: "tool", state: "DONE", text_delta: secret, tool_info: { parameters: { key: secret }, output: secret, error: { message: secret } } } },
    { event: "result", result: { status: "ERROR", response: secret, error: secret, num_turns: 1, usage: { input_tokens: 10, output_tokens: 2, total_tokens: 12, thinking_tokens: -1, cache_read_tokens: NaN, unknown: secret } } },
  ];
  const summaries = summarizeEvents(events);
  assert.ok(!JSON.stringify(summaries).includes(secret));
  assert.deepEqual(summaries[2].usage, { input_tokens: 10, output_tokens: 2, total_tokens: 12 });
  assert.equal(summaries[1].hasToolError, true);
  assert.ok(!Object.hasOwn(summarizeEvents([{ event: "result", result: {} }])[0], "usage"));
});

function fakeCLI(overrides = {}) {
  return async options => {
    if (options.args[0] === "--version") return completed(VERSION + "\n");
    assert.ok(!Object.hasOwn(options.env, "GH_TOKEN"));
    assert.ok(!Object.hasOwn(options.env, "GOOGLE_APPLICATION_CREDENTIALS"));
    assert.ok(!options.args.some(argument => argument.includes("AUTH_")));
    const settings = path.join(options.env.HOME, ".gemini", "antigravity-cli", "settings.json");
    assert.deepEqual(JSON.parse(fs.readFileSync(settings)), { modelProvider: "gemini" });
    assert.equal(fs.statSync(settings).mode & 0o777, 0o600);
    if (!options.env.GEMINI_API_KEY) return { ...failed("GEMINI_API_KEY is required"), stderr: "GEMINI_API_KEY is required" };
    if (options.env.GOOGLE_GEMINI_BASE_URL) {
      await fetch(options.env.GOOGLE_GEMINI_BASE_URL + "/v1beta/models/gemini-test:generateContent", { method: "POST", headers: { "x-goog-api-key": options.env.GEMINI_API_KEY }, body: "{}" });
      return failed("UNAUTHENTICATED");
    }
    if (options.env.GEMINI_API_KEY.startsWith("gh-aw-deliberately-invalid")) return overrides.invalid || failed("API key is invalid");
    const model = options.args[options.args.indexOf("--model") + 1];
    if (model.startsWith("gh-aw-nonexistent")) return failed("invalid model selection: model is not recognized");
    const prompt = JSON.parse(options.input).message.content;
    const marker = prompt.match(/AUTH_[a-f0-9]+/)[0];
    return overrides.inference || completed(JSON.stringify({ event: "init", init: { model } }) + "\n" + eventResult({ status: "SUCCESS", response: marker, num_turns: 1, usage: { total_tokens: 42 } }));
  };
}

const options = () => ({ binary: "/verified/agy", model: "gemini-test", apiKey: "test-provider-key", platform: "linux", arch: "x64" });

test("mocked gate exercises isolated positive, negative, and actual local endpoint paths", async () => {
  const report = await runProbe({ ...options(), run: fakeCLI() });
  assert.equal(report.status, "passed");
  assert.equal(report.awfConformance, "not-run");
  assert.equal(report.experimental, true);
  assert.equal(report.checks.length, 5);
  assert.equal(report.checks.at(-1).geminiProtocolConfirmed, true);
  assert.ok(!JSON.stringify(report).includes("test-provider-key"));
});

test("mocked startup success cannot pass as inference and diagnostics cannot leak", async () => {
  const report = await runProbe({ ...options(), run: fakeCLI({ inference: completed('{"event":"init","init":{"model":"gemini-test"}}\n') }) });
  assert.equal(report.status, "failed");
  assert.equal(report.checks.find(item => item.name === "actual-api-key-inference").status, "failed");
});

test("an invalid key exiting zero or timing out fails the gate", async () => {
  for (const invalid of [completed(eventResult({ status: "ERROR", error: "API key invalid" })), { ...failed("API key invalid"), timedOut: true }]) {
    const report = await runProbe({ ...options(), run: fakeCLI({ invalid }) });
    assert.equal(report.status, "failed");
    assert.equal(report.checks.find(item => item.name === "invalid-api-key").status, "failed");
  }
});

test("missing credentials, incorrect platform, unsafe model, and incorrect native version fail without skipping", async () => {
  for (const override of [{ apiKey: "" }, { platform: "darwin" }, { arch: "arm64" }, { model: "model;echo secret" }, { binary: "agy" }]) {
    await assert.rejects(runProbe({ ...options(), ...override, run: fakeCLI() }));
  }
  await assert.rejects(runProbe({ ...options(), run: async () => completed("0.0.0\n") }));
});

test("child runner delivers literal stdin and propagates native nonzero exit codes", async () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "agy-child-test-"));
  try {
    const input = "literal `command` $(echo do-not-run)\n";
    const result = await runCLI({ binary: process.execPath, args: ["-e", "process.stdin.pipe(process.stdout); process.exitCode = 7;"], input, env: {}, cwd: directory });
    assert.equal(result.stdout, input);
    assert.equal(result.code, 7);
    assert.equal(result.timedOut, false);
  } finally {
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test("child runner bounds hung processes and oversized output", async () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "agy-child-test-"));
  try {
    const hung = await runCLI({ binary: process.execPath, args: ["-e", "setInterval(() => {}, 1000);"], env: {}, cwd: directory, timeoutMs: 100 });
    assert.equal(hung.timedOut, true);
    assert.notEqual(hung.code, 0);
    const oversized = await runCLI({ binary: process.execPath, args: ["-e", "process.stdout.write('x'.repeat(3 * 1024 * 1024));"], env: {}, cwd: directory });
    assert.equal(oversized.outputLimit, true);
  } finally {
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

for (const detached of [false, true]) {
  for (const earlyExit of [false, true]) {
    test(`child runner bounds inherited output pipes (${detached ? "escaped group" : "native process group"}, ${earlyExit ? "parent exited" : "parent hung"})`, async () => {
      const directory = fs.mkdtempSync(path.join(os.tmpdir(), "agy-descendant-test-"));
      const receipt = path.join(directory, "descendant.json");
      let pid;
      try {
        const body = `
        const { spawn } = require("node:child_process");
        const fs = require("node:fs");
        const child = spawn(process.execPath, ["-e", "process.on('SIGTERM', () => {}); setInterval(() => {}, 1000);"], {
          detached: ${detached}, stdio: ["ignore", "inherit", "inherit"]
        });
        fs.writeFileSync(${JSON.stringify(receipt)}, JSON.stringify({ pid: child.pid }));
        process.on("SIGTERM", () => {});
        ${earlyExit ? "process.exit(0);" : ""}
        setInterval(() => {}, 1000);
      `;
        const start = Date.now();
        const result = await runCLI({ binary: process.execPath, args: ["-e", body], env: {}, cwd: directory, timeoutMs: 250 });
        pid = JSON.parse(fs.readFileSync(receipt, "utf8")).pid;
        assert.equal(result.timedOut, !earlyExit || detached);
        assert.ok(Date.now() - start < 2500, "A pipe holder must not extend the wall-time bound");
        if (earlyExit) assert.equal(result.code, 0);
        else assert.notEqual(result.code, 0);
        if (!detached) {
          const state = spawnSync("ps", ["-p", String(pid), "-o", "stat="], { encoding: "utf8" }).stdout.trim();
          assert.ok(!state || state.startsWith("Z"), "The native process group must have no live descendant");
        }
      } finally {
        if (pid) {
          try {
            process.kill(pid, "SIGKILL");
          } catch (error) {
            if (error.code !== "ESRCH") throw error;
          }
        }
        fs.rmSync(directory, { recursive: true, force: true });
      }
    });
  }
}
