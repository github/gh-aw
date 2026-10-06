"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const crypto = require("node:crypto");
const { test } = require("node:test");
const { classify, checkpointBundle, runVerification } = require("./work-queue-formal-check.cjs");

const SUCCESS = "Model checking completed. No error has been found.\n42 states generated, 21 distinct states found, 0 states left on queue.\nThe depth of the complete state graph search is 7.\n";

function fixture(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "work-queue-formal-test-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const jar = path.join(dir, "test.jar");
  fs.writeFileSync(jar, "fixture, not a real TLC jar");
  const javaBin = path.join(dir, "java");
  fs.writeFileSync(
    javaBin,
    `#!${process.execPath}
const fs = require("node:fs");
const path = require("node:path");
if (process.argv.includes("-version")) {
  console.error("test java version");
  process.exit(0);
}
const mode = process.env.FORMAL_TEST_MODE;
if (mode === "passed") {
  process.stdout.write(${JSON.stringify(SUCCESS)});
} else if (mode === "violation") {
  console.error("Error: Invariant Safety is violated.");
  process.exit(12);
} else if (mode === "tool_error") {
  console.error("Error: Parsing or semantic analysis failed.");
  process.exit(150);
} else {
  console.log("Progress(3): 100 states generated, 50 distinct states found, 20 states left on queue.");
  process.on("SIGINT", () => { process.stdout.write(${JSON.stringify(SUCCESS)}); process.exit(0); });
  setInterval(() => {}, 1000);
}
`
  );
  fs.chmodSync(javaBin, 0o755);
  return {
    config: "FairDAGGitHub",
    outputDir: path.join(dir, "output"),
    javaBin,
    jar,
    expectedJarSha256: crypto.createHash("sha256").update(fs.readFileSync(jar)).digest("hex"),
    timeoutSeconds: 10,
    env: { FORMAL_TEST_MODE: "passed" },
  };
}

test("only natural exit plus exhaustion is a pass", () => {
  assert.equal(classify(0, null, false, SUCCESS), "passed");
  assert.equal(classify(0, null, true, SUCCESS), "timed_out");
  assert.equal(classify(1, null, false, SUCCESS), "tool_error");
  assert.equal(classify(0, "SIGINT", false, SUCCESS), "tool_error");
  assert.equal(classify(0, null, false, "Progress(3): 20 states left on queue."), "tool_error");
  assert.equal(classify(0, null, false, SUCCESS.replace("0 states left", "100 states left")), "tool_error");
  assert.equal(classify(12, null, false, "Error: Invariant Safety is violated."), "violation");
  assert.equal(classify(150, null, false, "Error: Parsing failed."), "tool_error");
});

test("successful run persists machine-readable evidence and exact sources", async t => {
  const options = fixture(t);
  const result = await runVerification(options);
  assert.equal(result.status, "passed");
  assert.equal(result.exhausted, true);
  assert.deepEqual(result.final_counts, { states_generated: "42", distinct_states: "21", states_remaining: "0" });
  assert.equal(result.depth, "7");
  assert.equal(result.module, "FairWorkQueue");
  const bundle = path.join(options.outputDir, "bundle");
  assert.deepEqual(JSON.parse(fs.readFileSync(path.join(bundle, "result.json"), "utf8")), result);
  assert.ok(fs.existsSync(path.join(bundle, "FairWorkQueue.tla")));
  assert.ok(fs.existsSync(path.join(bundle, "FairDAGGitHub.cfg")));
  assert.match(fs.readFileSync(path.join(bundle, "summary.md"), "utf8"), /passed/);
  assert.equal(result.checkpoints.reason, "no_checkpoint");
});

test("invariant and tooling failures are distinct", async t => {
  for (const mode of ["violation", "tool_error"]) {
    const options = fixture(t);
    options.env.FORMAL_TEST_MODE = mode;
    assert.equal((await runVerification(options)).status, mode);
  }
});

test("timeout cannot become a pass after the process prints success on interruption", async t => {
  const options = fixture(t);
  options.env.FORMAL_TEST_MODE = "waiting";
  options.timeoutSeconds = 0.3;
  options.graceSeconds = 0.2;
  const result = await runVerification(options);
  assert.equal(result.status, "timed_out");
  assert.equal(result.exhausted, false);
  assert.ok(fs.existsSync(path.join(options.outputDir, "bundle", "result.json")));
  assert.deepEqual(result.latest_progress, { depth: "3", states_generated: "100", distinct_states: "50", states_remaining: "20" });
});

test("missing Java produces an explicit tool error", async t => {
  const options = fixture(t);
  options.javaBin = path.join(options.outputDir, "missing-java");
  const result = await runVerification(options);
  assert.equal(result.status, "tool_error");
  assert.match(result.error, /ENOENT/);
});

test("invalid model, deadline and jar checksum fail before executing TLC", async t => {
  const options = fixture(t);
  await assert.rejects(runVerification({ ...options, config: "Other" }), /config must/);
  await assert.rejects(runVerification({ ...options, outputDir: "" }), /absolute non-root/);
  await assert.rejects(runVerification({ ...options, outputDir: "/" }), /absolute non-root/);
  await assert.rejects(runVerification({ ...options, jar: "relative.jar" }), /absolute file/);
  await assert.rejects(runVerification({ ...options, timeoutSeconds: 18_001 }), /at most 16800/);
  await assert.rejects(runVerification({ ...options, expectedJarSha256: "bad" }), /checksum/);
});

test("oversized state is explicitly omitted rather than uploaded as resumable", t => {
  const options = fixture(t);
  const state = path.join(options.outputDir, "state");
  const bundle = path.join(options.outputDir, "bundle");
  fs.mkdirSync(state, { recursive: true });
  fs.mkdirSync(bundle, { recursive: true });
  fs.writeFileSync(path.join(state, "vars.chkpt"), "fixture");
  const result = checkpointBundle(state, bundle, 0);
  assert.equal(result.archived, false);
  assert.equal(result.resumable, false);
  assert.equal(result.reason, "state_size_limit");
});

test("small checkpoint directory is archived without including unrelated files", t => {
  const options = fixture(t);
  const state = path.join(options.outputDir, "state");
  const bundle = path.join(options.outputDir, "bundle");
  fs.mkdirSync(state, { recursive: true });
  fs.mkdirSync(bundle, { recursive: true });
  fs.writeFileSync(path.join(state, "vars.chkpt"), "fixture");
  fs.writeFileSync(path.join(state, "queue.chkpt"), "fixture");
  const result = checkpointBundle(state, bundle, 1024, true);
  assert.equal(result.archived, true);
  assert.equal(result.resumable, true);
  assert.ok(fs.existsSync(path.join(bundle, "checkpoint.tar.gz")));
});

test("unconfirmed checkpoint files are not labeled resumable", t => {
  const options = fixture(t);
  const state = path.join(options.outputDir, "state");
  const bundle = path.join(options.outputDir, "bundle");
  fs.mkdirSync(state, { recursive: true });
  fs.mkdirSync(bundle, { recursive: true });
  fs.writeFileSync(path.join(state, "vars.chkpt"), "fixture");
  fs.writeFileSync(path.join(state, "queue.chkpt"), "fixture");
  assert.equal(checkpointBundle(state, bundle, 1024).reason, "incomplete_checkpoint");
});
