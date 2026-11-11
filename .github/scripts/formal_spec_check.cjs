"use strict";

const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");
const { spawnSync } = require("node:child_process");

const RESULTS_ROOT = "/tmp/gh-aw/agent/formal-spec-verifier";
const JAVA = "/tmp/gh-aw/agent/tla-tools/jre/bin/java";
const JAR = "/tmp/gh-aw/agent/tla-tools/tla2tools.jar";
const KOKA = "/tmp/gh-aw/agent/koka/bin/koka";
const BUDGET_MS = 8 * 60 * 1000;
const OUTPUT_LIMIT = 8192;
const TLA_MODULES = new Set(["Naturals", "Integers", "Reals", "Sequences", "FiniteSets", "Bags", "TLC", "TLCExt", "Randomization"]);

function validateDependencies(action, source) {
  if (action === "koka") {
    if (/\b(?:import|extern|include)\b/.test(source)) throw new Error("Unsupported Koka dependency: use a single file without imports, extern declarations, or includes");
    return;
  }
  const tokens = [];
  let depth = 0;
  for (let offset = 0; offset < source.length;) {
    const rest = source.slice(offset);
    if (rest.startsWith("(*")) {
      depth++;
      offset += 2;
    } else if (depth && rest.startsWith("*)")) {
      depth--;
      offset += 2;
    } else if (depth || /^\s/.test(rest)) {
      offset++;
    } else if (rest.startsWith("\\*")) {
      const end = rest.search(/[\r\n]/);
      offset += end < 0 ? rest.length : end;
    } else {
      const token = rest.match(/^"(?:\\.|[^"\\])*"|^[A-Za-z_][A-Za-z0-9_]*|^\S/)[0];
      tokens.push(token);
      offset += token.length;
    }
  }
  if (depth) throw new Error("Unterminated TLA+ block comment");
  if (tokens.includes("INSTANCE")) throw new Error("Unsupported TLA+ dependency: INSTANCE is not supported");
  for (let index = 0; index < tokens.length; index++) {
    if (tokens[index] !== "EXTENDS") continue;
    do {
      if (!TLA_MODULES.has(tokens[++index])) throw new Error("Unsupported TLA+ dependency: EXTENDS accepts only the documented pinned standard modules");
    } while (tokens[index + 1] === "," && ++index);
  }
}

function processIdentity(pid) {
  try {
    const stat = fs.readFileSync(`/proc/${pid}/stat`, "utf8");
    const fields = stat.slice(stat.lastIndexOf(")") + 2).split(" ");
    if (["Z", "X"].includes(fields[0])) return null;
    return `${fs.readFileSync("/proc/sys/kernel/random/boot_id", "utf8").trim()}:${fields[19]}`;
  } catch (error) {
    if (error.code === "ENOENT") return null;
    throw error;
  }
}

function writeBudget(file, elapsed) {
  const pending = `${file}.${crypto.randomUUID()}`;
  fs.writeFileSync(pending, JSON.stringify({ elapsed_ms: elapsed }) + "\n", { flag: "wx" });
  fs.renameSync(pending, file);
}

function acquireBudgetLock(root, identity = processIdentity) {
  const owner = identity(process.pid);
  if (!owner) throw new Error("Cannot identify the formal-check budget owner");
  // Completed/dead generations are never unlinked: competing recoverers cannot
  // accidentally delete a newly acquired live lock (including after PID reuse).
  for (let generation = 0; ; generation++) {
    const file = path.join(root, `budget.lock${generation ? `.${generation}` : ""}`);
    const candidate = path.join(root, `budget-owner-${crypto.randomUUID()}`);
    fs.writeFileSync(candidate, JSON.stringify({ pid: process.pid, identity: owner }), { flag: "wx" });
    try {
      fs.linkSync(candidate, file);
      return () => fs.writeFileSync(`${file}.done`, "", { flag: "wx" });
    } catch (error) {
      if (error.code !== "EEXIST") throw error;
    } finally {
      fs.unlinkSync(candidate);
    }
    confinedPath(root, path.basename(file));
    const previous = JSON.parse(fs.readFileSync(file, "utf8"));
    if (!Number.isSafeInteger(previous.pid) || previous.pid <= 0 || typeof previous.identity !== "string" || !previous.identity) {
      throw new Error("Invalid formal-check budget lock owner");
    }
    if (!fs.existsSync(`${file}.done`) && identity(previous.pid) === previous.identity) {
      throw new Error(`Formal check is still running; wait for its owner to finish before retrying: ${file}`);
    }
  }
}

function specSlug(relative) {
  return relative.slice(0, -".md".length).replaceAll("/", "-");
}

function rejectSlugCollision(root, selected, slug, prefix = "") {
  for (const entry of fs.readdirSync(root, { withFileTypes: true })) {
    const relative = prefix + entry.name;
    if (entry.isDirectory()) {
      rejectSlugCollision(path.join(root, entry.name), selected, slug, `${relative}/`);
    } else if (entry.isFile() && relative.endsWith(".md") && relative !== selected && specSlug(relative) === slug) {
      throw new Error(`Formal-check spec slug collision: specs/${selected} and specs/${relative} both map to ${slug}`);
    }
  }
}

function outputPreview(value, secrets) {
  let text = value == null ? "" : value.toString();
  for (const secret of secrets) text = text.replaceAll(secret, "[REDACTED]");
  const buffer = Buffer.from(text);
  text = buffer.subarray(0, OUTPUT_LIMIT).toString("utf8");
  if (Buffer.byteLength(text) > OUTPUT_LIMIT) text = text.slice(0, -1);
  return { text, truncated: buffer.length > OUTPUT_LIMIT };
}

function confinedPath(root, relative, directory = false) {
  const parts = relative.split("/");
  if (parts.some(part => !/^[A-Za-z0-9_-][A-Za-z0-9_.-]*$/.test(part) || part === "." || part === "..")) {
    throw new Error(`Invalid relative formal-check path: ${relative}`);
  }
  let current = root;
  for (const part of parts) {
    current = path.join(current, part);
    if (fs.lstatSync(current).isSymbolicLink()) throw new Error(`Symlinks are not permitted: ${current}`);
  }
  const stat = fs.statSync(current);
  if (directory ? !stat.isDirectory() : !stat.isFile()) throw new Error(`Unexpected formal-check file type: ${current}`);
  return current;
}

function createDirectory(directory) {
  const parent = path.dirname(directory);
  if (parent !== directory) {
    createDirectory(parent);
  }
  if (!fs.existsSync(directory)) fs.mkdirSync(directory);
  if (fs.lstatSync(directory).isSymbolicLink() || !fs.statSync(directory).isDirectory()) {
    throw new Error(`Formal-check results must be a real directory: ${directory}`);
  }
}

/**
 * The CLI fixes tool and result locations; tests substitute disposable paths
 * and a process double without executing the installed formal runtimes.
 */
function runFormalCheck(args, options = {}) {
  const [action, spec, model, config] = args;
  if (!["sany", "tlc", "koka"].includes(action) || args.length !== (action === "tlc" ? 4 : 3)) {
    throw new Error("Usage: formal_spec_check.cjs sany|koka <specs/path.md> <Model.tla|main.kk>, or tlc <specs/path.md> <Model.tla> <Model.cfg>");
  }
  if (!spec.startsWith("specs/") || !spec.endsWith(".md")) throw new Error("Select one Markdown specification under specs/");
  const workspace = fs.realpathSync(options.workspace || path.resolve(__dirname, "../.."));
  const specPath = confinedPath(workspace, spec);
  const relativeSpec = spec.slice("specs/".length);
  const slug = specSlug(relativeSpec);
  rejectSlugCollision(path.join(workspace, "specs"), relativeSpec, slug);
  const modelDirectory = confinedPath(workspace, `specs/formal/${slug}`, true);
  const extension = action === "koka" ? "kk" : "tla";
  if (!new RegExp(`^[A-Za-z][A-Za-z0-9_-]*\\.${extension}$`).test(model)) throw new Error(`Expected a ${extension} model basename`);
  const modelPath = confinedPath(modelDirectory, model);
  const modelBytes = fs.readFileSync(modelPath);
  validateDependencies(action, modelBytes.toString("utf8"));
  let configPath;
  if (action === "tlc") {
    if (!/^[A-Za-z][A-Za-z0-9_-]*\.cfg$/.test(config)) throw new Error("Expected a cfg basename");
    configPath = confinedPath(modelDirectory, config);
  }
  const resultsRoot = options.resultsRoot || path.join(fs.realpathSync("/tmp"), path.relative("/tmp", RESULTS_ROOT));
  createDirectory(resultsRoot);
  const release = acquireBudgetLock(resultsRoot, options.processIdentity);
  const budgetPath = path.join(resultsRoot, "budget.json");
  let spent;
  let reserved = false;
  let refundable = true;
  try {
    return runChecks();
  } finally {
    if (reserved && refundable) writeBudget(budgetPath, spent);
    release();
  }
  function runChecks() {
    spent = 0;
    if (fs.existsSync(budgetPath)) {
      confinedPath(resultsRoot, "budget.json");
      spent = JSON.parse(fs.readFileSync(budgetPath, "utf8")).elapsed_ms;
      if (!Number.isSafeInteger(spent) || spent < 0) throw new Error("Invalid formal-check elapsed-time budget");
    }
    if (spent >= BUDGET_MS) throw new Error("The shared eight-minute formal-check budget is exhausted");
    const slugDirectory = path.join(resultsRoot, slug);
    createDirectory(slugDirectory);
    const results = fs.mkdtempSync(path.join(slugDirectory, `${action}-`));
    const inputs = path.join(results, "inputs");
    const home = path.join(results, "home");
    const tmp = path.join(results, "tmp");
    createDirectory(home);
    createDirectory(tmp);
    createDirectory(inputs);
    const env = {
      PATH: "/usr/local/bin:/usr/bin:/bin",
      HOME: home,
      TMPDIR: tmp,
      LANG: "C.UTF-8",
      LC_ALL: "C.UTF-8",
      TZ: "UTC",
    };
    const secrets = Object.entries(process.env)
      .filter(([name, value]) => /TOKEN|SECRET|PASSWORD|CREDENTIAL|AUTH|API[_-]?KEY|PRIVATE[_-]?KEY|ACCESS[_-]?KEY/i.test(name) && value)
      .map(([, value]) => value)
      .sort((a, b) => b.length - a.length);
    const records = [];
    const execute = options.spawnSync || spawnSync;
    const java = options.java || JAVA;
    const jar = options.jar || JAR;
    const koka = options.koka || KOKA;
    const sourceFiles = [[specPath, fs.readFileSync(specPath)], [modelPath, modelBytes], ...(configPath ? [[configPath, fs.readFileSync(configPath)]] : [])];
    const sources = sourceFiles.map(([file, bytes]) => ({
      path: path.relative(workspace, file),
      sha256: crypto.createHash("sha256").update(bytes).digest("hex"),
    }));
    for (const [file, bytes] of sourceFiles.slice(1)) fs.writeFileSync(path.join(inputs, path.basename(file)), bytes, { flag: "wx" });
    // Reserve the entire remaining allowance before any child can start.
    // A killed owner (or uncertain child termination) forfeits this reservation;
    // only a normally reaped set of children can refund unused elapsed time.
    writeBudget(budgetPath, BUDGET_MS);
    reserved = true;
    function run(name, executable, argv, maxMs) {
      const remaining = BUDGET_MS - spent;
      if (remaining < 1000) throw new Error("The shared eight-minute formal-check budget is exhausted");
      const seconds = Math.min(Math.floor(remaining / 1000), maxMs / 1000);
      const start = Date.now();
      let result;
      try {
        result = execute("timeout", ["--kill-after=30s", `${seconds}s`, executable, ...argv], {
          cwd: inputs,
          env,
          stdio: ["ignore", "pipe", "pipe"],
          maxBuffer: 65536,
          timeout: (seconds + 35) * 1000,
        });
      } catch (error) {
        result = { status: null, signal: null, error };
      } finally {
        spent += Date.now() - start;
      }
      if (result.error || result.status === null) refundable = false;
      const diagnostics = `${result.stdout || ""}\n${result.stderr || ""}`;
      const validationStatus = name === "sany" && result.status === 0 && /(?:\*{3}\s*(?:Errors|Fatal errors|Abort messages):\s*[1-9]\d*|\b(?:Semantic|Level) errors:|\*{3}\s*Parse Error\s*\*{3})/i.test(diagnostics) ? 1 : result.status;
      const stdout = outputPreview(result.stdout, secrets);
      const stderr = outputPreview(result.stderr, secrets);
      const error = result.error ? outputPreview(result.error.message, secrets).text : undefined;
      records.push({
        name,
        command: [executable, ...argv],
        timeout_seconds: seconds,
        status: result.status,
        validation_status: validationStatus,
        signal: result.signal,
        error,
        stdout: stdout.text,
        stdout_truncated: stdout.truncated,
        stderr: stderr.text,
        stderr_truncated: stderr.truncated,
      });
      fs.writeFileSync(path.join(results, "evidence.json"), JSON.stringify({ action, sources, records }, null, 2) + "\n");
      if (result.error) throw new Error(error);
      if (result.status === null) throw new Error(`${name} terminated by ${result.signal || "unknown signal"}`);
      return validationStatus;
    }
    let status = run("version", action === "koka" ? koka : java, [action === "koka" ? "--version" : "-version"], 30000);
    if (status !== 0) return { status, results };
    if (action === "sany") {
      status = run("sany", java, ["-Xmx2g", "-cp", jar, "tla2sany.SANY", model], 120000);
    } else if (action === "tlc") {
      status = run("tlc", java, ["-Xmx2g", "-cp", jar, "tlc2.TLC", "-noGenerateSpecTE", "-workers", "2", "-config", config, "-metadir", path.join(results, "states"), model], 120000);
    } else {
      const executable = path.join(results, "model");
      status = run("compile", koka, [`--builddir=${path.join(results, "build")}`, "-o", executable, model], 120000);
      if (status === 0) {
        confinedPath(results, "model");
        status = run("model", executable, [], 30000);
      }
    }
    return { status, results };
  }
}

module.exports = { runFormalCheck };

if (require.main === module) {
  try {
    const settings = JSON.parse(fs.readFileSync(path.join(__dirname, "workspace.json"), "utf8"));
    const result = runFormalCheck(process.argv.slice(2), { workspace: settings.workspace });
    console.log(`Formal-check exit ${result.status}; evidence: ${path.join(result.results, "evidence.json")}`);
    process.exitCode = result.status;
  } catch (error) {
    console.error(`Formal-check error: ${error.message}`);
    process.exitCode = 1;
  }
}
