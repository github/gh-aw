"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");
const { spawnSync } = require("node:child_process");
const { test } = require("node:test");
const { TLC_SHA256, classify, checkpointBundle, runVerification } = require("./work-queue-formal-check.cjs");

const SUCCESS = "Model checking completed. No error has been found.\n42 states generated, 21 distinct states found, 0 states left on queue.\nThe depth of the complete state graph search is 7.\n";
const INVARIANT_FALSE = "Error: The invariant of DAGValidity is equal to FALSE\n";
const CONFIGS = [
  { config: "FairDAGGitHub", moduleName: "FairWorkQueue" },
  { config: "QueueOrdering", moduleName: "WorkQueue" },
];

function fixture(t, config = CONFIGS[0].config) {
  const dir = path.join(__dirname, `.work-queue-formal-test-${crypto.randomUUID()}`);
  fs.mkdirSync(dir);
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
} else if (mode === "invariant_false") {
  process.stderr.write(${JSON.stringify(INVARIANT_FALSE)});
  process.exit(151);
} else if (mode === "setup_error") {
  console.error("Error: The invariant DAGValidity is not a boolean expression.");
  process.exit(151);
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
    config,
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
  assert.equal(classify(151, null, false, INVARIANT_FALSE), "violation");
  assert.equal(classify(151, null, true, INVARIANT_FALSE), "timed_out");
  assert.equal(classify(151, null, false, "Error: The invariant DAGValidity is not a boolean expression.\n"), "tool_error");
  assert.equal(classify(151, null, false, "Error: Parsing or semantic analysis failed.\n"), "tool_error");
  assert.equal(classify(151, null, false, "The invariant of DAGValidity could not be evaluated.\n"), "tool_error");
  assert.equal(classify(150, null, false, INVARIANT_FALSE), "tool_error");
  assert.equal(classify(150, null, false, "Error: Parsing failed."), "tool_error");
});

for (const { config, moduleName } of CONFIGS) {
  test(`${config}: successful run persists machine-readable evidence and exact sources`, async t => {
    const options = fixture(t, config);
    const result = await runVerification(options);
    assert.equal(result.status, "passed");
    assert.equal(result.exhausted, true);
    assert.deepEqual(result.final_counts, { states_generated: "42", distinct_states: "21", states_remaining: "0" });
    assert.equal(result.depth, "7");
    assert.equal(result.config, config);
    assert.equal(result.module, moduleName);
    assert.equal(result.tlc_sha256, options.expectedJarSha256);
    assert.equal(result.tlc_actual_sha256, options.expectedJarSha256);
    const bundle = path.join(options.outputDir, "bundle");
    assert.deepEqual(JSON.parse(fs.readFileSync(path.join(bundle, "result.json"), "utf8")), result);
    const sources = [`${moduleName}.tla`, `${config}.cfg`];
    assert.deepEqual(Object.keys(result.source_sha256), sources);
    for (const source of sources) {
      const original = fs.readFileSync(path.resolve(__dirname, "../../specs/work-queue", source));
      assert.deepEqual(fs.readFileSync(path.join(bundle, source)), original);
      assert.equal(result.source_sha256[source], crypto.createHash("sha256").update(original).digest("hex"));
    }
    assert.equal(result.command[0], options.javaBin);
    assert.equal(result.command[result.command.indexOf("-cp") + 1], options.jar);
    assert.equal(result.command[result.command.indexOf("-config") + 1], path.resolve(__dirname, "../../specs/work-queue", `${config}.cfg`));
    assert.equal(result.command[result.command.indexOf("-metadir") + 1], path.join(options.outputDir, "state"));
    assert.equal(result.command.at(-1), path.resolve(__dirname, "../../specs/work-queue", `${moduleName}.tla`));
    assert.match(fs.readFileSync(path.join(bundle, "summary.md"), "utf8"), /passed/);
    assert.equal(result.checkpoints.reason, "no_checkpoint");
  });

  test(`${config}: invariant and tooling failures are distinct`, async t => {
    for (const [mode, status] of [
      ["violation", "violation"],
      ["invariant_false", "violation"],
      ["setup_error", "tool_error"],
      ["tool_error", "tool_error"],
    ]) {
      const options = fixture(t, config);
      options.env.FORMAL_TEST_MODE = mode;
      const result = await runVerification(options);
      assert.equal(result.status, status);
      assert.equal(result.exhausted, false);
      assert.deepEqual(JSON.parse(fs.readFileSync(path.join(options.outputDir, "bundle", "result.json"), "utf8")), result);
    }
  });

  test(`${config}: timeout cannot become a pass after the process prints success on interruption`, async t => {
    const options = fixture(t, config);
    options.env.FORMAL_TEST_MODE = "waiting";
    options.timeoutSeconds = 0.3;
    options.graceSeconds = 0.2;
    const result = await runVerification(options);
    assert.equal(result.status, "timed_out");
    assert.equal(result.exhausted, false);
    assert.ok(fs.existsSync(path.join(options.outputDir, "bundle", "result.json")));
    assert.deepEqual(result.latest_progress, { depth: "3", states_generated: "100", distinct_states: "50", states_remaining: "20" });
  });

  test(`${config}: missing Java produces an explicit tool error`, async t => {
    const options = fixture(t, config);
    options.javaBin = path.join(options.outputDir, "missing-java");
    const result = await runVerification(options);
    assert.equal(result.status, "tool_error");
    assert.match(result.error, /ENOENT/);
  });
}

test("invalid model, deadline and jar checksum fail before executing TLC", async t => {
  const options = fixture(t);
  await assert.rejects(runVerification({ ...options, config: "Other" }), /config must/);
  await assert.rejects(runVerification({ ...options, outputDir: "" }), /absolute non-root/);
  await assert.rejects(runVerification({ ...options, outputDir: "/" }), /absolute non-root/);
  await assert.rejects(runVerification({ ...options, jar: "relative.jar" }), /absolute file/);
  await assert.rejects(runVerification({ ...options, timeoutSeconds: 18_001 }), /at most 16800/);
  await assert.rejects(runVerification({ ...options, expectedJarSha256: "bad" }), /checksum/);
  const result = JSON.parse(fs.readFileSync(path.join(options.outputDir, "bundle", "result.json"), "utf8"));
  assert.equal(result.status, "tool_error");
  assert.equal(result.exhausted, false);
  assert.equal(result.tlc_sha256, "bad");
  assert.equal(result.tlc_actual_sha256, options.expectedJarSha256);
  assert.match(result.error, /checksum/);
  assert.ok(result.finished_at);
  assert.equal(fs.existsSync(path.join(options.outputDir, "bundle", "java-version.txt")), false);
  assert.equal(fs.existsSync(path.join(options.outputDir, "bundle", "tlc.log")), false);
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

test("CLI checksum rejection retains actual and pinned digests in its tool-error artifact", t => {
  const options = fixture(t);
  const collected = spawnSync(process.execPath, [path.join(__dirname, "work-queue-formal-check.cjs")], {
    encoding: "utf8",
    env: { ...process.env, FORMAL_CONFIG: options.config, RESULTS_DIR: options.outputDir, TLA2TOOLS_JAR: options.jar, JAVA_BIN: options.javaBin },
  });
  assert.equal(collected.status, 1);
  assert.match(collected.stderr, /checksum/);
  const result = JSON.parse(fs.readFileSync(path.join(options.outputDir, "bundle", "result.json"), "utf8"));
  assert.equal(result.status, "tool_error");
  assert.equal(result.exhausted, false);
  assert.equal(result.tlc_sha256, TLC_SHA256);
  assert.equal(result.tlc_actual_sha256, options.expectedJarSha256);
  assert.match(result.error, /checksum/);
  assert.ok(result.finished_at);
  assert.equal(fs.existsSync(path.join(options.outputDir, "bundle", "tlc.log")), false);
});

test("partial checkpoint directory is archived only as an unvalidated candidate", t => {
  const options = fixture(t);
  const state = path.join(options.outputDir, "state");
  const bundle = path.join(options.outputDir, "bundle");
  fs.mkdirSync(state, { recursive: true });
  fs.mkdirSync(bundle, { recursive: true });
  fs.writeFileSync(path.join(state, "vars.chkpt"), "fixture");
  fs.writeFileSync(path.join(state, "queue.chkpt"), "fixture");
  fs.writeFileSync(path.join(options.outputDir, "unrelated.txt"), "not state");
  const result = checkpointBundle(state, bundle, 1024, true);
  assert.equal(result.archived, true);
  assert.equal(result.resumable, false);
  assert.equal(result.recovery_validation, "not_attempted");
  assert.equal(result.reason, "unvalidated_checkpoint_candidate");
  const contents = spawnSync("tar", ["-tzf", path.join(bundle, "checkpoint.tar.gz")], { encoding: "utf8" });
  assert.equal(contents.status, 0);
  assert.match(contents.stdout, /vars\.chkpt/);
  assert.match(contents.stdout, /queue\.chkpt/);
  assert.doesNotMatch(contents.stdout, /unrelated\.txt/);
  assert.doesNotMatch(contents.stdout, /\.st\.chkpt/);
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

for (const archiveFails of [false, true]) {
  test(`emergency reserve survives ${archiveFails ? "failed" : "successful"} archive attempt and is released before final metadata`, async t => {
    const options = fixture(t);
    const state = path.join(options.outputDir, "state");
    const bundle = path.join(options.outputDir, "bundle");
    const reserve = path.join(options.outputDir, "result-space.reserve");
    const observation = path.join(options.outputDir, "archive-observation.json");
    fs.mkdirSync(state, { recursive: true });
    fs.writeFileSync(path.join(state, "vars.chkpt"), "fixture");
    fs.writeFileSync(path.join(state, "queue.chkpt"), "fixture");
    fs.appendFileSync(options.javaBin, '\nconsole.log("Checkpointing completed");\n');
    const tar = path.join(path.dirname(options.javaBin), "tar");
    fs.writeFileSync(
      tar,
      `#!${process.execPath}
const fs = require("node:fs");
fs.writeFileSync(${JSON.stringify(observation)}, JSON.stringify({
  reserve_bytes: fs.statSync(${JSON.stringify(reserve)}).size,
  inventory_exists: fs.existsSync(${JSON.stringify(path.join(bundle, "checkpoint-inventory.json"))})
}));
if (${archiveFails}) {
  fs.writeFileSync(process.argv[process.argv.indexOf("-czf") + 1], "partial archive");
  console.error("tar: No space left on device");
  process.exit(1);
}
const packed = require("node:child_process").spawnSync("/usr/bin/tar", process.argv.slice(2), { stdio: "inherit" });
process.exit(packed.status ?? 1);
`
    );
    fs.chmodSync(tar, 0o755);
    const originalPath = process.env.PATH;
    process.env.PATH = `${path.dirname(tar)}${path.delimiter}${originalPath}`;
    t.after(() => {
      process.env.PATH = originalPath;
    });
    const writeFileSync = fs.writeFileSync;
    const finalWrites = [];
    t.mock.method(fs, "writeFileSync", (file, ...args) => {
      if (file === path.join(bundle, "checkpoint-inventory.json") || (file === path.join(bundle, "result.json") && JSON.parse(args[0]).status !== "running")) {
        assert.equal(fs.existsSync(reserve), false);
        finalWrites.push(file);
      }
      return writeFileSync(file, ...args);
    });

    const result = await runVerification(options);
    assert.deepEqual(JSON.parse(fs.readFileSync(observation, "utf8")), { reserve_bytes: 1024 * 1024, inventory_exists: false });
    assert.equal(fs.existsSync(reserve), false);
    assert.equal(result.status, "passed");
    assert.equal(result.checkpoints.archived, !archiveFails);
    assert.equal(result.checkpoints.resumable, false);
    assert.equal(result.checkpoints.reason, archiveFails ? "archive_error" : "unvalidated_checkpoint_candidate");
    if (archiveFails) {
      assert.match(result.checkpoints.error, /No space left on device/);
      assert.equal(fs.existsSync(path.join(bundle, "checkpoint.tar.gz")), false);
    }
    assert.deepEqual(finalWrites, [path.join(bundle, "checkpoint-inventory.json"), path.join(bundle, "result.json")]);
    assert.deepEqual(JSON.parse(fs.readFileSync(path.join(bundle, "result.json"), "utf8")), result);
    assert.deepEqual(JSON.parse(fs.readFileSync(path.join(bundle, "checkpoint-inventory.json"), "utf8")), result.checkpoints);
    assert.match(fs.readFileSync(path.join(bundle, "README.md"), "utf8"), /unvalidated candidate/);
    assert.match(fs.readFileSync(path.join(bundle, "summary.md"), "utf8"), archiveFails ? /archive_error/ : /unvalidated candidate/);
  });
}

test("state inventory I/O failure produces explicit evidence without losing the verdict", async t => {
  const options = fixture(t);
  const state = path.join(options.outputDir, "state");
  const readdirSync = fs.readdirSync;
  t.mock.method(fs, "readdirSync", (dir, ...args) => {
    if (dir === state) throw Object.assign(new Error("state inventory I/O failure"), { code: "EIO" });
    return readdirSync(dir, ...args);
  });
  const result = await runVerification(options);
  assert.equal(result.status, "passed");
  assert.equal(result.checkpoints.reason, "inventory_error");
  assert.match(result.checkpoints.error, /I\/O failure/);
  assert.equal(result.checkpoints.archived, false);
  assert.equal(result.checkpoints.resumable, false);
  assert.equal(fs.existsSync(path.join(options.outputDir, "result-space.reserve")), false);
  assert.deepEqual(JSON.parse(fs.readFileSync(path.join(options.outputDir, "bundle", "result.json"), "utf8")), result);
});

test("unexpected inventory programming errors are not swallowed", t => {
  const options = fixture(t);
  const state = path.join(options.outputDir, "state");
  fs.mkdirSync(state, { recursive: true });
  t.mock.method(fs, "readdirSync", () => {
    throw new Error("unexpected defect");
  });
  assert.throws(() => checkpointBundle(state, path.join(options.outputDir, "bundle"), 1024), /unexpected defect/);
});
