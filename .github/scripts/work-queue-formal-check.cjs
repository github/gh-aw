// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");
const { spawn, spawnSync } = require("node:child_process");

const MODELS = Object.freeze({ FairDAGGitHub: "FairWorkQueue", QueueOrdering: "WorkQueue" });
const TLC_SHA256 = "936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88";
const DEFAULT_TIMEOUT_SECONDS = 280 * 60;
const CHECKPOINT_MAX_BYTES = 256 * 1024 * 1024;

function writeJSON(file, value) {
  fs.writeFileSync(file, `${JSON.stringify(value, null, 2)}\n`);
}

function classify(exitCode, signal, timedOut, log) {
  if (timedOut) return "timed_out";
  if (exitCode === 0 && !signal && log.includes("Model checking completed. No error has been found.") && /(?:^|\n)\d+ states generated, \d+ distinct states found, 0 states left on queue\.(?:\r?\n|$)/.test(log)) return "passed";
  if ([12, 13].includes(exitCode) && /(?:Invariant|Action property|Temporal property) .+ is violated/.test(log)) return "violation";
  return "tool_error";
}

function lastMatch(log, pattern) {
  const matches = [...log.matchAll(pattern)];
  return matches.length ? matches[matches.length - 1].slice(1) : null;
}

function inventory(directory) {
  let bytes = 0;
  let files = 0;
  const checkpoints = [];
  function visit(current) {
    for (const entry of fs.readdirSync(current, { withFileTypes: true })) {
      const file = path.join(current, entry.name);
      if (entry.isDirectory()) visit(file);
      else if (entry.isFile()) {
        const size = fs.statSync(file).size;
        bytes += size;
        files++;
        if (entry.name.endsWith(".chkpt")) checkpoints.push({ path: path.relative(directory, file), bytes: size });
      }
    }
  }
  if (fs.existsSync(directory)) visit(directory);
  return { bytes, files, checkpoints };
}

function checkpointBundle(stateDir, bundleDir, maxBytes, checkpointConfirmed = false) {
  const state = inventory(stateDir);
  const result = { ...state, archived: false, resumable: false, reason: "no_checkpoint" };
  if (!state.checkpoints.length) return result;
  if (state.bytes > maxBytes) return { ...result, reason: "state_size_limit" };
  if (!checkpointConfirmed || !state.checkpoints.some(file => file.path.endsWith("vars.chkpt")) || !state.checkpoints.some(file => file.path.endsWith("queue.chkpt"))) {
    return { ...result, reason: "incomplete_checkpoint" };
  }
  const archive = path.join(bundleDir, "checkpoint.tar.gz");
  const packed = spawnSync("tar", ["-czf", archive, "-C", stateDir, "."], { timeout: 90_000, encoding: "utf8" });
  if (packed.error || packed.status !== 0) {
    if (fs.existsSync(archive)) fs.unlinkSync(archive);
    return { ...result, reason: "archive_error", error: packed.error?.message || packed.stderr };
  }
  return { ...result, archived: true, resumable: true, reason: null };
}

/**
 * @param {{config: string, outputDir: string, jar: string, javaBin?: string, timeoutSeconds?: number, graceSeconds?: number, checkpointMaxBytes?: number, expectedJarSha256?: string, env?: NodeJS.ProcessEnv}} options
 */
async function runVerification(options) {
  const { config, jar, outputDir } = options;
  if (!Object.hasOwn(MODELS, config)) throw new Error("config must be FairDAGGitHub or QueueOrdering");
  if (!path.isAbsolute(outputDir) || path.resolve(outputDir) === path.parse(outputDir).root) {
    throw new Error("outputDir must be an absolute non-root directory");
  }
  if (!path.isAbsolute(jar)) throw new Error("jar must be an absolute file path");
  const timeoutSeconds = options.timeoutSeconds ?? DEFAULT_TIMEOUT_SECONDS;
  if (!Number.isFinite(timeoutSeconds) || timeoutSeconds <= 0 || timeoutSeconds > DEFAULT_TIMEOUT_SECONDS) {
    throw new Error("timeoutSeconds must be positive and at most 16800 (4h40m)");
  }
  const javaBin = options.javaBin || process.env.JAVA_BIN || "java";
  const expectedJarSha256 = options.expectedJarSha256 || TLC_SHA256;
  const root = path.resolve(__dirname, "../..");
  const specDir = path.join(root, "specs/work-queue");
  const bundleDir = path.join(outputDir, "bundle");
  const stateDir = path.join(outputDir, "state");
  fs.mkdirSync(bundleDir, { recursive: true });
  fs.mkdirSync(stateDir, { recursive: true });
  const moduleName = MODELS[config];
  const sources = [`${moduleName}.tla`, `${config}.cfg`];
  const sourceHashes = {};
  for (const source of sources) {
    const content = fs.readFileSync(path.join(specDir, source));
    fs.writeFileSync(path.join(bundleDir, source), content);
    sourceHashes[source] = crypto.createHash("sha256").update(content).digest("hex");
  }
  const args = [
    "-XX:+UseParallelGC",
    "-Xmx1g",
    "-cp",
    jar,
    "tlc2.TLC",
    "-workers",
    "2",
    "-seed",
    "1",
    "-fp",
    "0",
    "-checkpoint",
    "5",
    "-config",
    path.join(specDir, `${config}.cfg`),
    "-metadir",
    stateDir,
    path.join(specDir, `${moduleName}.tla`),
  ];
  const started = new Date();
  const base = {
    schema_version: 1,
    config,
    module: moduleName,
    status: "running",
    exhausted: false,
    repository: process.env.GITHUB_REPOSITORY || null,
    sha: process.env.GITHUB_SHA || null,
    server_url: process.env.GITHUB_SERVER_URL || "https://github.com",
    run_id: process.env.GITHUB_RUN_ID || null,
    run_attempt: process.env.GITHUB_RUN_ATTEMPT || null,
    started_at: started.toISOString(),
    timeout_seconds: timeoutSeconds,
    tlc_sha256: expectedJarSha256,
    source_sha256: sourceHashes,
    command: [javaBin, ...args],
  };
  const resultPath = path.join(bundleDir, "result.json");
  writeJSON(resultPath, base);
  const jarHash = crypto.createHash("sha256").update(fs.readFileSync(jar)).digest("hex");
  if (jarHash !== expectedJarSha256) throw new Error("TLC jar checksum does not match the pinned release");
  const version = spawnSync(javaBin, ["-version"], { encoding: "utf8", timeout: 10_000, env: { ...process.env, ...options.env } });
  fs.writeFileSync(path.join(bundleDir, "java-version.txt"), `${version.stdout || ""}${version.stderr || ""}`);
  const logPath = path.join(bundleDir, "tlc.log");
  const reservePath = path.join(outputDir, "result-space.reserve");
  fs.writeFileSync(reservePath, Buffer.alloc(1024 * 1024));
  const fd = fs.openSync(logPath, "w");
  let timedOut = false;
  let spawnError = null;
  /** @type {NodeJS.Timeout | undefined} */
  let killTimer;
  const child = spawn(javaBin, args, { cwd: root, env: { ...process.env, ...options.env }, detached: true, stdio: ["ignore", fd, fd] });
  const signalChild = signal => {
    if (child.pid && child.exitCode === null) {
      try {
        process.kill(-child.pid, signal);
      } catch (error) {
        if (!(error && typeof error === "object" && "code" in error && error.code === "ESRCH")) throw error;
      }
    }
  };
  const timer = setTimeout(() => {
    timedOut = true;
    signalChild("SIGINT");
    killTimer = setTimeout(() => signalChild("SIGKILL"), (options.graceSeconds ?? 30) * 1000);
  }, timeoutSeconds * 1000);
  const { exitCode, signal } = await new Promise(resolve => {
    child.on("error", error => {
      spawnError = error.message;
    });
    child.on("close", (exitCode, signal) => resolve({ exitCode, signal }));
  });
  clearTimeout(timer);
  clearTimeout(killTimer);
  fs.closeSync(fd);
  fs.unlinkSync(reservePath);
  const log = fs.readFileSync(logPath, "utf8");
  const finished = lastMatch(log, /(\d+) states generated, (\d+) distinct states found, (\d+) states left on queue\./g);
  const progress = lastMatch(log, /Progress\((\d+)\).*?: ([\d,]+) states generated.*?, ([\d,]+) distinct states found.*?, ([\d,]+) states left on queue\./g);
  const checkpoints = checkpointBundle(stateDir, bundleDir, options.checkpointMaxBytes ?? CHECKPOINT_MAX_BYTES, log.includes("Checkpointing completed"));
  writeJSON(path.join(bundleDir, "checkpoint-inventory.json"), checkpoints);
  const status = classify(exitCode, signal, timedOut, log);
  const result = {
    ...base,
    status,
    exhausted: status === "passed",
    finished_at: new Date().toISOString(),
    elapsed_seconds: Math.round((Date.now() - started.getTime()) / 1000),
    exit_code: exitCode,
    signal,
    error: spawnError,
    final_counts: finished ? { states_generated: finished[0], distinct_states: finished[1], states_remaining: finished[2] } : null,
    latest_progress: progress
      ? {
          depth: progress[0],
          states_generated: progress[1].replaceAll(",", ""),
          distinct_states: progress[2].replaceAll(",", ""),
          states_remaining: progress[3].replaceAll(",", ""),
        }
      : null,
    depth: lastMatch(log, /depth of the complete state graph search is (\d+)/g)?.[0] || null,
    checkpoints,
  };
  writeJSON(resultPath, result);
  const summary =
    `### ${config}\n\nStatus: **${status}**. Exhausted: **${result.exhausted}**.\n\n` +
    `Elapsed: ${result.elapsed_seconds}s. Exit: ${exitCode}. Signal: ${signal || "none"}.\n\n` +
    `Checkpoint archive: ${checkpoints.archived ? "available" : checkpoints.reason}.\n`;
  fs.writeFileSync(path.join(bundleDir, "summary.md"), summary);
  fs.writeFileSync(
    path.join(bundleDir, "README.md"),
    "# Formal verification evidence\n\n" +
      "Read result.json and summary.md first. tlc.log contains progress and counterexamples. " +
      "The model and configuration are exact source snapshots; verify their recorded SHA-256 hashes.\n\n" +
      "A timed_out, tool_error, setup_incomplete, or missing result is not a proof. " +
      "Decimal state counts are strings to avoid numeric precision loss.\n\n" +
      "checkpoint-inventory.json states whether the complete state directory was archived. " +
      "Only checkpoint.tar.gz marked resumable contains a bounded full state bundle; omitted state is not recoverable from this artifact.\n\n" +
      "For recovery, extract the archive, locate the directory containing vars.chkpt, and use the pinned TLC jar, " +
      "the snapshot model/configuration, -workers 2, -fp 0, and -recover /path/to/that/directory. " +
      "Rewrite machine-specific source paths from command when replaying elsewhere.\n"
  );
  if (process.env.GITHUB_STEP_SUMMARY) fs.appendFileSync(process.env.GITHUB_STEP_SUMMARY, summary);
  return result;
}

if (require.main === module) {
  runVerification({ config: process.env.FORMAL_CONFIG || "", outputDir: process.env.RESULTS_DIR || "", jar: process.env.TLA2TOOLS_JAR || "" })
    .then(result => {
      console.log(JSON.stringify({ config: result.config, status: result.status, exhausted: result.exhausted }));
      process.exitCode = result.status === "passed" ? 0 : 1;
    })
    .catch(error => {
      console.error(`Formal verification collection failed: ${error.message}`);
      const outputDir = process.env.RESULTS_DIR || "";
      const resultPath = path.join(outputDir, "bundle", "result.json");
      if (path.isAbsolute(outputDir) && path.resolve(outputDir) !== path.parse(outputDir).root && fs.existsSync(resultPath)) {
        const result = JSON.parse(fs.readFileSync(resultPath, "utf8"));
        writeJSON(resultPath, { ...result, status: "tool_error", exhausted: false, error: error.message, finished_at: new Date().toISOString() });
      }
      process.exitCode = 1;
    });
}

module.exports = { TLC_SHA256, classify, inventory, checkpointBundle, runVerification };
