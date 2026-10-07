#!/usr/bin/env node
import assert from "node:assert/strict";
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { TLC_SHA256, classify } from "../../.github/scripts/work-queue-formal-check.cjs";

const root = path.dirname(fileURLToPath(import.meta.url));
const args = process.argv.slice(2);
assert.equal(args.length, 2, "usage: compare-evaluation.mjs BASELINE_DIRECTORY RESULTS_DIRECTORY");
const baseline = path.resolve(args[0]);
const output = path.resolve(args[1]);
const java = process.env.JAVA_BIN || "java";
const jar = process.env.TLA2TOOLS_JAR;
assert(jar && path.isAbsolute(jar), "TLA2TOOLS_JAR must be an absolute pinned jar path");
const hash = value => crypto.createHash("sha256").update(value).digest("hex");
assert.equal(hash(fs.readFileSync(jar)), TLC_SHA256, "unexpected TLC jar checksum");
fs.mkdirSync(output, { recursive: true });
assert(!fs.existsSync(path.join(output, "comparison.json")), "do not overwrite previous comparison evidence");
const cases = [
  ["WorkQueue", "Recovery"],
  ["FairWorkQueue", "FairBatch"],
  ["FairWorkQueue", "FairPriority"],
  ["FairWorkQueue", "FairStrict"],
  ["FairWorkQueue", "FairGitHubDependencies"],
  ["WorkQueue", "Recovery", "missing-transition"],
  ["FairWorkQueue", "FairBatch", "changed-normalization"],
];
const variables = {
  WorkQueue: "log, head, dispatch, workers, compact, recovery, deadRuns, terminalHistory, authorizations, effects",
  FairWorkQueue: "log, head, pending, worker, intents, handled, authorized, effects, completed",
};
const constants = {
  WorkQueue: "WorkCount, ClaimCount, WorkerCount, DispatcherCount, RetryLimit, MaxHead, MaxLog",
  FairWorkQueue: "WorkCount, DispatcherCount, MaxBatch, ClaimLimit, RunLimit, KeyLimit, RetryLimit, MaxLog, Mode, Dependencies, ExternalDependencies",
};
const comparisons = [];
for (const [moduleName, config, mutation] of cases) {
  const name = mutation ? `${config}-${mutation}` : config;
  const directory = path.join(output, name);
  fs.mkdirSync(directory);
  const original = fs.readFileSync(path.join(baseline, `${moduleName}.tla`), "utf8");
  const current = fs.readFileSync(path.join(root, `${moduleName}.tla`), "utf8");
  let candidate = current;
  if (mutation === "missing-transition") {
    candidate = candidate.replace(/^Next ==[\s\S]*?(?=^Spec ==)/m, "Next == FALSE\n");
    assert.notEqual(candidate, current, "missing-transition mutation did not apply");
  } else if (mutation === "changed-normalization") {
    candidate = candidate.replace("Max(@, s.cv + 1)", "Max(@, s.cv + 2)");
    assert.notEqual(candidate, current, "normalization mutation did not apply");
  }
  for (const [label, source] of [
    ["Original", original],
    ["Revised", candidate],
  ]) {
    const renamed = source.replace(`MODULE ${moduleName} `, `MODULE ${moduleName}${label} `);
    assert.notEqual(renamed, source, "expected model module header");
    fs.writeFileSync(path.join(directory, `${moduleName}${label}.tla`), renamed);
  }
  const fair = moduleName === "FairWorkQueue";
  const wrapper = `---------------- MODULE Comparison ----------------
EXTENDS Naturals, FiniteSets, Sequences, TLC
CONSTANTS ${constants[moduleName]}
VARIABLES ${variables[moduleName]}
Original == INSTANCE ${moduleName}Original
Revised == INSTANCE ${moduleName}Revised
${
  fair
    ? `IndependentDependencies == Original!IndependentDependencies
NoExternalDependencies == Original!NoExternalDependencies
GitHubDependencies == Original!GitHubDependencies`
    : ""
}
vars == <<${variables[moduleName]}>>
Init == Original!Init \\/ Revised!Init
Next == Original!Next \\/ Revised!Next
Spec == Init /\\ [][Next]_vars
Safety == Original!Safety /\\ Revised!Safety
Bound == Original!Bound
${fair ? "OneObservationPerResource == Original!OneObservationPerResource" : ""}
InitialEquivalence == Original!Init = Revised!Init
NextEquivalence == [] [Original!Next = Revised!Next]_vars
${
  fair
    ? "ReplayEquivalence == Original!State = Revised!State"
    : `WorkerOneShot == Original!WorkerOneShot /\\ Revised!WorkerOneShot
QueueSelection == Original!QueueSelection /\\ Revised!QueueSelection
WorkResubmissionNoOp == Original!WorkResubmissionNoOp /\\ Revised!WorkResubmissionNoOp`
}
=============================================================================
`;
  fs.writeFileSync(path.join(directory, "Comparison.tla"), wrapper);
  const configuration = fs.readFileSync(path.join(baseline, `${config}.cfg`), "utf8");
  assert.equal(fs.readFileSync(path.join(root, `${config}.cfg`), "utf8"), configuration, "comparison configuration differs from its baseline");
  fs.writeFileSync(path.join(directory, "Comparison.cfg"), `${configuration}\nPROPERTY InitialEquivalence\nPROPERTY NextEquivalence\n${fair ? "INVARIANT ReplayEquivalence\n" : ""}`);
  const command = ["-XX:+UseParallelGC", "-Xmx1g", "-cp", jar, "tlc2.TLC", "-workers", "2", "-seed", "1", "-fp", "0", "-config", "Comparison.cfg", "-metadir", "state", "Comparison.tla"];
  const started = Date.now();
  const logPath = path.join(directory, "tlc.log");
  const fd = fs.openSync(logPath, "w");
  const checked = spawnSync(java, command, { cwd: directory, timeout: 900_000, killSignal: "SIGINT", stdio: ["ignore", fd, fd] });
  fs.closeSync(fd);
  const log = fs.readFileSync(logPath, "utf8");
  const diagnostic = mutation === "missing-transition" ? "Action property NextEquivalence is violated" : mutation === "changed-normalization" ? "Invariant ReplayEquivalence is violated" : null;
  const expectedExit = mutation === "missing-transition" ? 13 : mutation ? 12 : 0;
  const status = classify(checked.status, checked.signal, checked.error?.code === "ETIMEDOUT", log);
  const counts = [...log.matchAll(/(\d+) states generated, (\d+) distinct states found, (\d+) states left on queue\./g)].at(-1);
  const result = {
    config,
    module: moduleName,
    mutation: mutation || null,
    baseline_sha256: hash(original),
    candidate_sha256: hash(candidate),
    config_sha256: hash(configuration),
    wrapper_sha256: hash(wrapper),
    tlc_sha256: TLC_SHA256,
    command: [java, ...command],
    status,
    exit_code: checked.status,
    signal: checked.signal,
    error: checked.error?.message || null,
    expected_exit: expectedExit,
    expected_diagnostic: diagnostic,
    elapsed_seconds: (Date.now() - started) / 1000,
    distinct_states: counts?.[2] || null,
    passed: !checked.error && checked.status === expectedExit && (diagnostic ? log.includes(diagnostic) : status === "passed"),
  };
  comparisons.push(result);
  const complete = comparisons.length === cases.length;
  fs.writeFileSync(path.join(output, "comparison.json"), `${JSON.stringify({ complete, passed: complete && comparisons.every(c => c.passed), comparisons }, null, 2)}\n`);
  console.log(`${name}: ${result.passed ? "expected result" : "FAILED"} (${result.elapsed_seconds}s)`);
  assert(result.passed, `comparison failed; inspect ${logPath}`);
}
