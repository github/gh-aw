const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { EventEmitter } = require("node:events");
const { runInNewContext } = require("node:vm");
const { domainURLPatterns } = require("../../actions/setup/js/copilot_url_permissions.cjs");

function fixture() {
  const child = new EventEmitter();
  const process = new EventEmitter();
  process.argv = ["node", "cracktro_preview.cjs"];
  process.env = { RUNNER_TEMP: "/tmp/cracktro-preview-fixture" };
  const calls = [];
  const errors = [];
  child.kill = signal => calls.push(["kill", signal]);
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
      console: { error: message => errors.push(message) },
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
  assert.throws(run, /accepts no arguments/);
  assert.equal(calls.length, 0);
});

test("fails before creating output or starting a server when the staging environment is missing", () => {
  const { run, process, calls } = fixture();
  delete process.env.RUNNER_TEMP;
  assert.throws(run, /RUNNER_TEMP is required/);
  assert.equal(calls.length, 0);
});

test("the workflow uses the allowed Node launcher and scopes native URL approval to loopback", () => {
  const workflow = fs.readFileSync(path.join(__dirname, "../workflows/daily-cracktro-maintainer.md"), "utf8");
  const lock = fs.readFileSync(path.join(__dirname, "../workflows/daily-cracktro-maintainer.lock.yml"), "utf8");
  assert.ok(workflow.includes("`node .github/scripts/cracktro_preview.cjs`"));
  assert.ok(workflow.includes('- "127.0.0.1"'));
  assert.deepEqual(domainURLPatterns(["127.0.0.1"]), ["http://127.0.0.1", "https://127.0.0.1"]);
  assert.ok(lock.includes("shell(node)"));
  assert.ok(!workflow.includes('"python3 -m http.server *"'));
  assert.match(workflow, /do not run Python directly, `mkdir`, or shell loops/);
  assert.match(workflow, /On the first tool denial/);
});
