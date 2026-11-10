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
  let configPath;
  if (action === "tlc") {
    if (!/^[A-Za-z][A-Za-z0-9_-]*\.cfg$/.test(config)) throw new Error("Expected a cfg basename");
    configPath = confinedPath(modelDirectory, config);
  }
  const resultsRoot = options.resultsRoot || path.join(fs.realpathSync("/tmp"), path.relative("/tmp", RESULTS_ROOT));
  createDirectory(resultsRoot);
  const lockPath = path.join(resultsRoot, "budget.lock");
  let lock;
  try {
    lock = fs.openSync(lockPath, "wx");
  } catch (error) {
    if (error.code === "EEXIST") {
      throw new Error(`Formal-check budget lock already exists: ${lockPath}. Another check may still be running; remove this lock only after confirming no formal check is active.`);
    }
    throw error;
  }
  try {
    return runChecks();
  } finally {
    fs.closeSync(lock);
    fs.unlinkSync(lockPath);
  }
  function runChecks() {
    const budgetPath = path.join(resultsRoot, "budget.json");
    let spent = 0;
    if (fs.existsSync(budgetPath)) {
      confinedPath(resultsRoot, "budget.json");
      spent = JSON.parse(fs.readFileSync(budgetPath, "utf8")).elapsed_ms;
      if (!Number.isSafeInteger(spent) || spent < 0) throw new Error("Invalid formal-check elapsed-time budget");
    }
    if (spent >= BUDGET_MS) throw new Error("The shared eight-minute formal-check budget is exhausted");
    const slugDirectory = path.join(resultsRoot, slug);
    createDirectory(slugDirectory);
    const results = fs.mkdtempSync(path.join(slugDirectory, `${action}-`));
    const home = path.join(results, "home");
    const tmp = path.join(results, "tmp");
    createDirectory(home);
    createDirectory(tmp);
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
    const sources = [specPath, modelPath, ...(configPath ? [configPath] : [])].map(file => ({
      path: path.relative(workspace, file),
      sha256: crypto.createHash("sha256").update(fs.readFileSync(file)).digest("hex"),
    }));
    function run(name, executable, argv, maxMs) {
      const remaining = BUDGET_MS - spent;
      if (remaining < 1000) throw new Error("The shared eight-minute formal-check budget is exhausted");
      const seconds = Math.min(Math.floor(remaining / 1000), maxMs / 1000);
      const start = Date.now();
      let result;
      try {
        result = execute("timeout", ["--kill-after=30s", `${seconds}s`, executable, ...argv], {
          cwd: modelDirectory,
          env,
          stdio: ["ignore", "pipe", "pipe"],
          maxBuffer: 65536,
          timeout: (seconds + 35) * 1000,
        });
      } catch (error) {
        result = { status: null, signal: null, error };
      } finally {
        spent += Date.now() - start;
        fs.writeFileSync(budgetPath, JSON.stringify({ elapsed_ms: spent }) + "\n");
      }
      const stdout = outputPreview(result.stdout, secrets);
      const stderr = outputPreview(result.stderr, secrets);
      const error = result.error ? outputPreview(result.error.message, secrets).text : undefined;
      records.push({
        name,
        command: [executable, ...argv],
        timeout_seconds: seconds,
        status: result.status,
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
      return result.status;
    }
    let status = run("version", action === "koka" ? koka : java, [action === "koka" ? "--version" : "-version"], 30000);
    if (status !== 0) return { status, results };
    if (action === "sany") {
      status = run("sany", java, ["-Xmx2g", "-cp", jar, "tla2sany.SANY", model], 120000);
    } else if (action === "tlc") {
      status = run("tlc", java, ["-Xmx2g", "-cp", jar, "tlc2.TLC", "-workers", "2", "-config", config, "-metadir", path.join(results, "states"), model], 120000);
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
    const result = runFormalCheck(process.argv.slice(2));
    console.log(`Formal-check exit ${result.status}; evidence: ${path.join(result.results, "evidence.json")}`);
    process.exitCode = result.status;
  } catch (error) {
    console.error(`Formal-check error: ${error.message}`);
    process.exitCode = 1;
  }
}
