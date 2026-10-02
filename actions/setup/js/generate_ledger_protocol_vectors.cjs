// @ts-check
"use strict";

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const crypto = require("node:crypto");
const { spawnSync } = require("node:child_process");
const prettier = require("prettier");

const modelDir = path.resolve(__dirname, "../../../specs/ledger");
const output = path.join(__dirname, "ledger_protocol_vectors.json");

function readFile(file) {
  try {
    return fs.readFileSync(file, "utf8");
  } catch (error) {
    throw new Error(`Cannot read ledger model file ${file}`, { cause: error });
  }
}

function writeFile(file, contents) {
  try {
    fs.writeFileSync(file, contents);
  } catch (error) {
    throw new Error(`Cannot write ledger model file ${file}`, { cause: error });
  }
}

function operations() {
  const source = readFile(path.join(modelDir, "LedgerProtocol.tla"));
  const declaration = source.match(/Ops == CASE([\s\S]*?)\n\n/);
  if (!declaration) throw new Error("Cannot find the ledger operations in LedgerProtocol.tla");
  const result = {};
  for (const [, kind, list] of declaration[1].matchAll(/(?:Kind = "([^"]+)"|OTHER)\s*->\s*\{([^}]+)\}/g)) {
    const name = kind || "notes";
    result[name] = [...list.matchAll(/"([^"]+)"/g)].map(match => match[1]);
  }
  if (Object.keys(result).length !== 7 || Object.values(result).some(list => !list.length)) throw new Error("Unexpected ledger type declarations in LedgerProtocol.tla");
  return result;
}

function sourceDigest() {
  const hash = crypto.createHash("sha256");
  for (const name of [
    "LedgerProtocol.tla",
    "LedgerProtocolWitness.tla",
    ...Object.keys(operations())
      .sort()
      .map(kind => `${kind}.cfg`),
  ]) {
    hash
      .update(name)
      .update("\0")
      .update(readFile(path.join(modelDir, name)))
      .update("\0");
  }
  return hash.digest("hex");
}

function state(value) {
  if (!Array.isArray(value) || value.length !== 2 || !value[1] || typeof value[1] !== "object") throw new Error("Unexpected TLC trace state");
  const { phase, request, artifact, history, projection, snapshot, shard } = value[1];
  return { phase, request, artifact, history, projection, snapshot, shard };
}

function witness(jar, kind, op) {
  let directory;
  try {
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-ledger-tlc-"));
  } catch (error) {
    throw new Error("Cannot create TLC witness directory", { cause: error });
  }
  try {
    const config = path.join(directory, "witness.cfg");
    const trace = path.join(directory, "trace.json");
    const metadata = path.join(directory, "metadata");
    writeFile(config, `${readFile(path.join(modelDir, `${kind}.cfg`))}\nCONSTANT TargetOp = "${op}"\nINVARIANT MissingAgentTarget\n`);
    const run = spawnSync("java", ["-cp", jar, "tlc2.TLC", "-workers", "1", "-noGenerateSpecTE", "-config", config, "-dumpTrace", "json", trace, "-metadir", metadata, "LedgerProtocolWitness.tla"], {
      cwd: modelDir,
      encoding: "utf8",
      timeout: 120000,
      maxBuffer: 4 * 1024 * 1024,
    });
    if (run.error || run.status !== 12 || !run.stdout.includes("Invariant MissingAgentTarget is violated.") || !fs.existsSync(trace)) {
      throw new Error(`TLC did not produce a ${kind}/${op} witness: ${run.error?.message || run.stderr || run.stdout.slice(-2000)}`);
    }
    let result;
    try {
      result = JSON.parse(readFile(trace));
    } catch (error) {
      throw new Error(`Cannot parse TLC witness for ${kind}/${op}`, { cause: error });
    }
    const transitions = result.counterexample?.action?.map(([before, action, after]) => {
      if (!["Queue", "ExternalAppend", "Validate", "Persist"].includes(action?.name)) throw new Error(`Unexpected TLC action: ${action?.name}`);
      return { action: action.name, before: state(before), after: state(after) };
    });
    if (!transitions?.length || transitions.at(-1).action !== "Persist" || transitions.at(-1).after.history.at(-1)?.op !== op) {
      throw new Error(`Incomplete TLC witness for ${kind}/${op}`);
    }
    return { kind, op, initial: transitions[0].before, steps: transitions.map(({ action, after }) => ({ action, state: after })) };
  } finally {
    try {
      fs.rmSync(directory, { recursive: true, force: true });
    } catch (error) {
      throw new Error(`Cannot clean up TLC witness for ${kind}/${op}`, { cause: error });
    }
  }
}

async function main() {
  const jar = process.env.TLA2TOOLS_JAR;
  let validJar = false;
  try {
    validJar = Boolean(jar && fs.statSync(jar).isFile());
  } catch {
    validJar = false;
  }
  if (!validJar) throw new Error("Set TLA2TOOLS_JAR to a local tla2tools.jar to regenerate ledger vectors");
  const vectors = Object.entries(operations()).flatMap(([kind, ops]) => ops.map(op => witness(jar, kind, op)));
  const formatted = await prettier.format(JSON.stringify({ sourceDigest: sourceDigest(), vectors }), { ...(await prettier.resolveConfig(output)), filepath: output });
  writeFile(output, formatted);
}

if (require.main === module) {
  main().catch(error => {
    console.error(error);
    process.exitCode = 1;
  });
}

module.exports = { operations, sourceDigest, state, witness };
