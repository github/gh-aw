// @ts-check

const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");
const { isDeepStrictEqual } = require("node:util");
const root = path.resolve(__dirname, "../../../..");
const { collectUnifiedSession, parseEngineSession, mergeSessionSources } = require(path.join(root, "actions/setup/js/unified_session.cjs"));
const { serializeSessionArtifact, collectAddMaskedValues } = require(path.join(root, "actions/setup/js/session_artifact.cjs"));
const { validateSession } = require(path.join(root, "actions/setup/js/scripts/validate_session.cjs"));

const hash = content => crypto.createHash("sha256").update(content).digest("hex");
const eventsOf = content =>
  content
    .trim()
    .split("\n")
    .map(line => JSON.parse(line));

function safePath(directory, relative) {
  if (!relative || path.isAbsolute(relative) || relative.split(/[/\\]/).includes("..")) throw new Error("Invalid relative input path");
  let current = directory;
  for (const part of relative.split(/[/\\]/)) {
    current = path.join(current, part);
    if (fs.lstatSync(current).isSymbolicLink()) throw new Error("Symlink input rejected");
  }
  if (!fs.statSync(current).isFile()) throw new Error("Input must be a regular file");
  return current;
}

function subset(actual, expected) {
  if (expected && typeof expected === "object" && !Array.isArray(expected)) {
    return actual !== null && typeof actual === "object" && Object.entries(expected).every(([key, value]) => Object.hasOwn(actual, key) && subset(actual[key], value));
  }
  return isDeepStrictEqual(actual, expected);
}

function checkExpectations(sessions, expectations) {
  if (!Array.isArray(expectations) || !expectations.length) throw new Error("Expectations must be a nonempty array");
  return expectations.map((check, index) => {
    if (
      !check ||
      typeof check.source !== "string" ||
      !sessions.has(check.source) ||
      typeof check.type !== "string" ||
      !Number.isSafeInteger(check.count) ||
      check.count < 0 ||
      !check.data ||
      typeof check.data !== "object" ||
      Array.isArray(check.data)
    ) {
      throw new Error("Invalid expectation");
    }
    const actual = sessions.get(check.source).filter(event => event.type === check.type && subset(event.data, check.data)).length;
    return { index, source: check.source, passed: actual === check.count, expected: check.count, actual };
  });
}

function comparison(left, right) {
  // Compare agent payload multisets, not collector clocks or generated provenance.
  const fingerprints = events => {
    const counts = new Map();
    const canonical = value => {
      if (Array.isArray(value)) return value.map(canonical);
      if (value && typeof value === "object")
        return Object.fromEntries(
          Object.keys(value)
            .sort()
            .map(key => [key, canonical(value[key])])
        );
      return value;
    };
    for (const event of events.filter(event => event.provenance.component === "agent")) {
      const digest = hash(JSON.stringify(canonical({ type: event.type, data: event.data })));
      counts.set(digest, (counts.get(digest) ?? 0) + 1);
    }
    return counts;
  };
  const a = fingerprints(left);
  const b = fingerprints(right);
  let matched = 0;
  let leftOnly = 0;
  let rightOnly = 0;
  for (const key of new Set([...a.keys(), ...b.keys()])) {
    matched += Math.min(a.get(key) ?? 0, b.get(key) ?? 0);
    leftOnly += Math.max(0, (a.get(key) ?? 0) - (b.get(key) ?? 0));
    rightOnly += Math.max(0, (b.get(key) ?? 0) - (a.get(key) ?? 0));
  }
  return { matched, leftOnly, rightOnly };
}

function audit({ input, engine, native = [], output, expectations }) {
  if (!/^[a-z][a-z0-9-]*$/.test(engine)) throw new Error("Invalid engine ID");
  const inputRoot = fs.realpathSync(input);
  const outputRoot = path.resolve(output);
  // Resolve the existing ancestor to detect symlink-mediated overlap.
  let ancestor = outputRoot;
  while (!fs.existsSync(ancestor)) ancestor = path.dirname(ancestor);
  const resolvedOutput = path.join(fs.realpathSync(ancestor), path.relative(ancestor, outputRoot));
  const contains = (parent, child) => child === parent || child.startsWith(parent + path.sep);
  if (contains(inputRoot, resolvedOutput) || contains(resolvedOutput, inputRoot) || fs.existsSync(outputRoot)) throw new Error("Output must be a new directory outside evidence");
  const parent = fs.lstatSync(path.dirname(outputRoot));
  if (!parent.isDirectory() || parent.isSymbolicLink() || (parent.mode & 0o077) !== 0) throw new Error("Output parent must be an existing private directory");

  const { events, maskedValues } = collectUnifiedSession({ rootDir: inputRoot, engine, warn: () => {}, dailyAIC: {}, admission: {}, jobResults: "", safeJobNames: "", safeJobResults: "", memorySaveResults: "" });
  const observedEngine = events.find(event => event.type === "workflow.info")?.data.engineId;
  if (observedEngine !== undefined && observedEngine !== engine) throw new Error("Requested engine does not match recorded metadata");
  const content = serializeSessionArtifact(events, maskedValues, { secretValues: [] });
  validateSession(content);
  const reconstructed = eventsOf(content);
  const agentEvents = reconstructed.filter(event => event.provenance.component === "agent");
  if (!agentEvents.length || agentEvents.every(event => event.type === "agent.execution")) throw new Error("No recognizable agent session evidence");
  const sessions = new Map([["reconstructed", reconstructed]]);
  const files = new Map([["reconstructed.jsonl", content]]);
  const report = {
    engine,
    reconstructed: { records: reconstructed.length, sha256: hash(content), warnings: reconstructed.filter(event => event.type === "session.collection_warning").length },
    native: [],
    recorded: { status: "unavailable" },
    semantics: { status: "not_checked", checks: [] },
  };
  for (const [index, relative] of native.entries()) {
    const source = fs.readFileSync(safePath(inputRoot, relative), "utf8");
    if (!source.trim()) throw new Error("Empty native input");
    if (relative.split(/[/\\]/).at(-1) === "agent-session.jsonl" || relative.split(/[/\\]/).at(-1) === "aw_session.jsonl") throw new Error("Canonical traces are not native evidence");
    const parsed = parseEngineSession(source, engine);
    if (!parsed.length || parsed.every(event => event.type === "agent.execution")) throw new Error("Unrecognized native input");
    const merged = mergeSessionSources([{ component: "agent", phase: "agent", path: relative.split(path.sep).join("/"), events: parsed }], engine);
    const normalized = serializeSessionArtifact([reconstructed[0], ...merged], [...maskedValues, ...collectAddMaskedValues(source)], { secretValues: [] });
    validateSession(normalized);
    const normalizedEvents = eventsOf(normalized);
    const name = `native-${index}`;
    sessions.set(name, normalizedEvents);
    files.set(`${name}.jsonl`, normalized);
    report.native.push({ source: name, path: relative, inputSha256: hash(source), records: normalizedEvents.length, sha256: hash(normalized), comparison: comparison(normalizedEvents, reconstructed) });
  }
  for (const relative of ["usage/aw_session.jsonl", "aw_session.jsonl"]) {
    if (!fs.existsSync(path.join(inputRoot, relative))) continue;
    const saved = fs.readFileSync(safePath(inputRoot, relative), "utf8");
    validateSession(saved);
    const recorded = eventsOf(saved);
    sessions.set("recorded", recorded);
    report.recorded = { status: "valid", records: recorded.length, sha256: hash(saved), comparison: comparison(reconstructed, recorded) };
    break;
  }
  if (expectations !== undefined) {
    const checks = checkExpectations(sessions, expectations);
    report.semantics = { status: checks.every(check => check.passed) ? "passed" : "failed", checks };
  }
  fs.mkdirSync(outputRoot, { mode: 0o700 });
  for (const [filename, value] of files) fs.writeFileSync(path.join(outputRoot, filename), value, { flag: "wx", mode: 0o600 });
  fs.writeFileSync(path.join(outputRoot, "report.json"), JSON.stringify(report) + "\n", { flag: "wx", mode: 0o600 });
  return report;
}

function main(args) {
  const options = { native: [] };
  for (let index = 0; index < args.length; index += 2) {
    const key = args[index];
    const value = args[index + 1];
    if (!value || !["--root", "--engine", "--output", "--native", "--expectations"].includes(key)) throw new Error("Invalid audit arguments");
    if (key === "--native") options.native.push(value);
    else if (key === "--root") options.input = value;
    else if (key === "--expectations") options.expectations = JSON.parse(fs.readFileSync(value, "utf8"));
    else options[key.slice(2)] = value;
  }
  if (!options.input || !options.engine || !options.output) throw new Error("Require --root, --engine and --output");
  const report = audit(options);
  process.stdout.write(JSON.stringify(report) + "\n");
  if (report.semantics.status === "failed" || report.reconstructed.warnings) process.exitCode = 1;
}

if (require.main === module) {
  try {
    main(process.argv.slice(2));
  } catch {
    console.error("Session audit failed; inspect bounded structural diagnostics locally, never publish source payloads");
    process.exitCode = 1;
  }
}

module.exports = { audit, checkExpectations, comparison, safePath };
