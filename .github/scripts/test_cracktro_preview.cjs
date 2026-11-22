const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { EventEmitter } = require("node:events");
const { runInNewContext } = require("node:vm");
const { isPiBashAllowed } = require("../../actions/setup/js/pi_tool_policy.cjs");

function fixture() {
  const child = new EventEmitter();
  const process = new EventEmitter();
  process.argv = ["node", "cracktro_preview.cjs"];
  process.execPath = "/bin/node";
  process.env = { RUNNER_TEMP: "/tmp/cracktro-preview-fixture" };
  const calls = [];
  const errors = [];
  child.kill = signal => calls.push(["kill", signal]);
  child.unref = () => calls.push(["unref"]);
  child.pid = 12345;
  const module = { exports: {} };
  const require = name => {
    if (name === "node:fs") return { mkdirSync: (...args) => calls.push(["mkdir", ...args]) };
    if (name === "node:path") return path;
    if (name === "node:child_process") return { spawn: (...args) => (calls.push(["spawn", ...args]), child) };
    throw new Error(`Unexpected module: ${name}`);
  };
  require.main = module;
  const run = () =>
    runInNewContext(fs.readFileSync(path.join(__dirname, "cracktro_preview.cjs"), "utf8"), {
      require,
      module,
      process,
      __dirname,
      __filename: path.join(__dirname, "cracktro_preview.cjs"),
      setTimeout: (callback, delay) => ({ unref: () => calls.push(["deadline", callback, delay]) }),
      clearTimeout: () => {},
      console: { error: message => errors.push(message), log: message => calls.push(["log", message]) },
    });
  return { child, process, calls, errors, run };
}

test("launches only the fixed loopback preview and creates the exact screenshot directory", () => {
  const { run, calls } = fixture();
  run();
  assert.equal(calls[0][0], "mkdir");
  assert.equal(calls[0][1], "/tmp/cracktro-preview-fixture/gh-aw/safeoutputs/upload-artifacts/cracktro-review");
  assert.equal(calls[0][2].recursive, true);
  assert.equal(calls[1][0], "spawn");
  assert.equal(calls[1][1], "python3");
  assert.deepEqual(Array.from(calls[1][2]), ["-m", "http.server", "4329", "--bind", "127.0.0.1", "--directory", "docs/public"]);
  assert.equal(calls[1][3].cwd, path.resolve(__dirname, "../.."));
  assert.equal(calls[1][3].shell, false);
});

test("surfaces startup and exit failures and forwards cleanup signals to its own child", () => {
  const { run, child, process, errors, calls } = fixture();
  run();
  child.emit("error", new Error("python3 unavailable"));
  assert.equal(process.exitCode, 1);
  assert.match(errors[0], /python3 unavailable/);
  child.emit("exit", 2, null);
  assert.equal(process.exitCode, 2);
  process.emit("SIGTERM");
  assert.deepEqual(calls.at(-1), ["kill", "SIGTERM"]);
});

test("rejects arguments instead of allowing another bind address or document root", () => {
  const { run, process, calls } = fixture();
  process.argv.push("--bind", "0.0.0.0");
  assert.throws(run, /accepts only the optional background argument/);
  assert.equal(calls.length, 0);
});

test("fails before creating output or starting a server when the staging environment is missing", () => {
  const { run, process, calls } = fixture();
  delete process.env.RUNNER_TEMP;
  assert.throws(run, /RUNNER_TEMP is required/);
  assert.equal(calls.length, 0);
});

test("the background launcher returns a cleanup PID without blocking the shell", () => {
  const { run, process, child, calls } = fixture();
  process.argv.push("background");
  run();
  child.emit("spawn");
  assert.equal(calls[0][1], process.execPath);
  assert.equal(calls[0][3].detached, true);
  assert.equal(calls[0][3].stdio, "ignore");
  assert.deepEqual(calls.slice(1), [["log", "Cracktro preview launcher PID: 12345"], ["log", "Cracktro screenshot directory: /tmp/cracktro-preview-fixture/gh-aw/safeoutputs/upload-artifacts/cracktro-review"], ["unref"]]);
});

test("Pi allows the workflow preview lifecycle without shell background syntax", () => {
  const workflow = fs.readFileSync(path.join(__dirname, "../workflows/daily-cracktro-maintainer.md"), "utf8");
  const lock = fs.readFileSync(path.join(__dirname, "../workflows/daily-cracktro-maintainer.lock.yml"), "utf8");
  const launcher = "node .github/scripts/cracktro_preview.cjs background";
  assert.ok(workflow.includes(`\`${launcher}\``));
  assert.ok(workflow.includes('- "127.0.0.1"'));
  const policy = JSON.parse(lock.match(/GH_AW_PI_TOOL_POLICY: '(.+)'/)[1]);
  for (const command of [launcher, "curl http://127.0.0.1:4329/cracktro/ --fail", "playwright-cli open --browser=chromium http://127.0.0.1:4329/cracktro/", "kill 12345"]) {
    assert.equal(isPiBashAllowed(command, policy.bash), true, command);
  }
  assert.equal(isPiBashAllowed(`${launcher} &`, policy.bash), false);
  assert.ok(!workflow.includes('"python3 -m http.server *"'));
  assert.match(workflow, /Do not run Python directly, `mkdir`, or shell loops/);
  assert.match(workflow, /On the first tool denial/);
});
