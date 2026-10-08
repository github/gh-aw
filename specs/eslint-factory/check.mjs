import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import { analyze, root, sha256, checkoutIdentity } from "./compare.mjs";
import { cases, configuration } from "./cases.mjs";
import { parseTrace, validateTrace, readableTrace } from "./trace.mjs";

const directory = path.dirname(fileURLToPath(import.meta.url));
const options = Object.fromEntries(
  process.argv.slice(2).map(argument => {
    const equal = argument.indexOf("=");
    assert.ok(equal > 2, "Use --name=value options");
    return [argument.slice(2, equal), argument.slice(equal + 1)];
  })
);
const artifacts = path.resolve(options.artifacts || path.join(root, ".eslint-factory-formal-artifacts"));
const java = options.java || process.env.JAVA || "/opt/homebrew/opt/openjdk@21/bin/java";
const jar = options.jar || process.env.TLA2TOOLS_JAR;
const pinnedJar = "936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88";
const timeoutMs = Number(options["timeout-seconds"] || 60) * 1000;
assert.ok(jar, "Set TLA2TOOLS_JAR or --jar=<existing official tla2tools.jar>; this checker never downloads tools");
assert.equal(sha256(fs.readFileSync(jar)), pinnedJar, "Unexpected TLC jar; deliberately review/update the pin before use");
assert.ok(timeoutMs > 0 && timeoutMs <= 120_000, "Per-model timeout must be 1..120 seconds");
fs.mkdirSync(artifacts, { recursive: true });
const write = (filename, value) => fs.writeFileSync(path.join(artifacts, filename), value);
const model = fs.readFileSync(path.join(directory, "Factory.tla"), "utf8");
const specificationSources = fs
  .readdirSync(directory)
  .filter(name => /\.(mjs|tla|md)$/.test(name))
  .sort()
  .map(name => ({
    path: path.relative(root, path.join(directory, name)),
    sha256: sha256(fs.readFileSync(path.join(directory, name))),
  }));
const javaVersion = spawnSync(java, ["-version"], { encoding: "utf8", timeout: 10_000 });
assert.equal(javaVersion.status, 0, javaVersion.error?.message || javaVersion.stderr);
const report = {
  checkout: checkoutIdentity(),
  sources: specificationSources,
  tools: { java: javaVersion.stderr.trim(), javaPath: java, jarPath: path.resolve(jar), jarSha256: pinnedJar, node: process.version, workers: 2, maxHeap: "1GiB" },
  limits: { perCaseTimeoutMs: timeoutMs, totalTimeoutMs: 600_000, maximumWorks: 2, maximumOutputFamilyPerClaim: 3 },
  comparison: null,
  models: [],
};
const started = Date.now();
let failed = false;
try {
  const comparison = await analyze(path.join(artifacts, "comparison"));
  report.comparison = {
    checkout: comparison.checkout,
    mismatches: comparison.mismatches,
    runtimeProbes: comparison.facts.runtimeProbes,
    assumptions: comparison.assumptions,
    documentationFindings: comparison.documentationFindings,
    sources: comparison.sources,
    tools: comparison.tool,
  };
  if (comparison.mismatches.length) failed = true;
  for (const entry of cases.filter(entry => !options.case || entry.name === options.case)) {
    assert.ok(Date.now() - started < report.limits.totalTimeoutMs, "Total checker budget exhausted");
    const outputDirectory = path.join(artifacts, entry.name);
    fs.mkdirSync(outputDirectory, { recursive: true });
    fs.mkdirSync(path.join(outputDirectory, "jvm-files"), { recursive: true });
    const config = configuration(entry);
    fs.writeFileSync(path.join(outputDirectory, "Factory.tla"), model);
    fs.writeFileSync(path.join(outputDirectory, "Factory.cfg"), config);
    const args = [
      "-Xmx1g",
      "-XX:+UseParallelGC",
      `-Djava.io.tmpdir=${path.join(outputDirectory, "jvm-files")}`,
      "-cp",
      path.resolve(jar),
      "tlc2.TLC",
      "-workers",
      "2",
      "-seed",
      "1",
      "-metadir",
      path.join(outputDirectory, "states"),
      "-config",
      "Factory.cfg",
      "Factory",
    ];
    const checkStarted = Date.now();
    const result = spawnSync(java, args, { cwd: outputDirectory, encoding: "utf8", timeout: Math.min(timeoutMs, report.limits.totalTimeoutMs - (Date.now() - started)), maxBuffer: 32 * 1024 * 1024 });
    const log = (result.stdout || "") + (result.stderr || "");
    fs.writeFileSync(path.join(outputDirectory, "tlc.log"), log);
    const diagnostic = log.match(/Invariant (\w+) is violated\./)?.[1] || null;
    const counts = [...log.matchAll(/([\d,]+) states generated, ([\d,]+) distinct states found, ([\d,]+) states left on queue/g)].at(-1);
    const record = {
      name: entry.name,
      constants: entry.constants,
      expected: entry.expected,
      verdict: result.error?.code === "ETIMEDOUT" ? "timeout" : entry.expected === "exhaustive" && result.status === 0 && log.includes("Model checking completed. No error has been found.") ? "exhaustive" : diagnostic,
      diagnostic,
      status: result.status,
      signal: result.signal,
      durationMs: Date.now() - checkStarted,
      statesGenerated: counts ? Number(counts[1].replaceAll(",", "")) : null,
      distinctStates: counts ? Number(counts[2].replaceAll(",", "")) : null,
      statesLeft: counts ? Number(counts[3].replaceAll(",", "")) : null,
      modelSha256: sha256(model),
      configSha256: sha256(config),
      logSha256: sha256(log),
      command: [java, ...args],
      cwd: outputDirectory,
      traceValidated: false,
    };
    if (record.verdict === entry.expected && entry.expected !== "exhaustive") {
      assert.equal(result.status, 12, "Expected TLC safety-invariant violation exit status");
      const trace = parseTrace(log);
      validateTrace(trace, entry);
      const traceDocument = { source: { checkoutHead: report.checkout.head, originMain: report.checkout.originMain, modelSha256: record.modelSha256, configSha256: record.configSha256, logSha256: record.logSha256 }, case: entry, trace };
      fs.writeFileSync(path.join(outputDirectory, "trace.json"), JSON.stringify(traceDocument, null, 2) + "\n");
      fs.writeFileSync(path.join(outputDirectory, "trace.md"), readableTrace(trace, entry));
      record.traceValidated = true;
      record.traceStates = trace.length;
    }
    report.models.push(record);
    if (record.verdict !== entry.expected || (entry.expected === "exhaustive" && record.statesLeft !== 0)) failed = true;
    console.log(`${entry.name}: ${record.verdict} (${record.distinctStates} distinct states, ${record.durationMs}ms${record.traceValidated ? ", trace replay validated" : ""})`);
    write("validation.json", JSON.stringify(report, null, 2) + "\n");
  }
  assert.ok(report.models.length, "Unknown --case selector");
  for (const source of [...specificationSources, ...(report.comparison?.sources || [])]) {
    assert.equal(sha256(fs.readFileSync(path.join(root, source.path))), source.sha256, `Source changed during validation: ${source.path}; rerun against a stable tree`);
  }
  assert.equal(checkoutIdentity().head, report.checkout.head, "HEAD changed during validation; rerun against a stable checkout");
} catch (error) {
  report.error = error.stack;
  failed = true;
  console.error(error.stack);
}
report.durationMs = Date.now() - started;
report.passed = !failed;
write("validation.json", JSON.stringify(report, null, 2) + "\n");
write(
  "summary.md",
  [
    "# ESLint factory bounded validation",
    "",
    `Outcome: **${failed ? "FAILED" : "PASSED"}**. Comparison fact mismatches: ${report.comparison?.mismatches.length ?? "not completed"}.`,
    `Current HEAD: \`${report.checkout.head}\`; origin/main: \`${report.checkout.originMain || "unavailable"}\`. Measured sources are current working-tree bytes, including uncommitted changes.`,
    "",
    "| Case | Verdict | Generated | Distinct | Left | Trace states |",
    "|---|---|---|---|---|---|",
    ...report.models.map(model => `| ${model.name} | ${model.verdict} | ${model.statesGenerated} | ${model.distinctStates} | ${model.statesLeft} | ${model.traceStates || "—"} |`),
    "",
    "Exhaustive means the selected finite model's complete reachable graph. Witnesses deliberately violate NeverWitness; negative controls deliberately violate the named invariant. Neither is a production defect by itself.",
    "",
    ...(report.comparison?.assumptions || []).map(text => `- ${text}`),
    "",
  ].join("\n")
);
process.exitCode = failed ? 1 : 0;
