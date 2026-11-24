// @ts-check
const fs = require("node:fs");
const path = require("node:path");
const bundleReader = require("./threat_bundle_diff.cjs");
const fileLimit = 512 * 1024;
const preparationLimit = 1024 * 1024;
const entryLimit = 256;

const labels = {
  prompt: "aw-prompts/prompt.txt",
  prompt_template: "aw-prompts/prompt-template.txt",
  prompt_import_tree: "aw-prompts/prompt-import-tree.json",
};

function uninspectable(what, reason) {
  return `${what} was NOT analyzed: ${reason}. Treat this channel as unexamined rather than empty, and do not draw any conclusion about its contents. This is a staging or configuration failure, not evidence of a threat: do NOT report a threat on the basis of this notice alone.`;
}

function warning(field, message) {
  const chars = Array.from(message.slice(0, 4002));
  return { field, code: "ERR_VALIDATION", message: chars.length > 2000 ? chars.slice(0, 1999).join("") + "…" : message };
}

function describe(stat) {
  if (stat.isSymbolicLink()) return "a symlink";
  if (stat.isDirectory()) return "a directory";
  if (stat.isFIFO()) return "a named pipe";
  if (stat.isSocket()) return "a socket";
  if (stat.isFile()) return "a regular file";
  return "a device file";
}

// Never follow agent-staged symlinks or block opening special files. Inspect
// the descriptor, not just the directory entry, before reading any bytes.
/** @param {string} dir @param {string} relative @param {{probe?:boolean, limit?:number, budget?:{remaining:number}}} options */
function inspect(dir, relative, { probe = false, limit = fileLimit, budget = undefined } = {}) {
  const file = path.join(dir, relative);
  let fd;
  try {
    const parent = path.dirname(relative);
    if (parent !== "." && !fs.lstatSync(path.join(dir, parent)).isDirectory()) throw new Error("parent is not a real directory");
    fd = fs.openSync(file, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
    const stat = fs.fstatSync(fd);
    if (!stat.isFile()) throw new Error("not a regular file");
    let content = "";
    if (probe) fs.readSync(fd, Buffer.alloc(1), 0, 1, 0);
    else {
      limit = Math.min(limit, budget?.remaining ?? limit);
      if (stat.size > limit) throw new Error(`exceeds ${limit}-byte preparation limit`);
      const buffer = Buffer.alloc(limit + 1);
      let size = 0;
      while (size < buffer.length) {
        const bytes = fs.readSync(fd, buffer, size, buffer.length - size, null);
        if (!bytes) break;
        size += bytes;
      }
      if (budget) budget.remaining -= Math.min(limit, size);
      if (size > limit) throw new Error(`exceeds ${limit}-byte preparation limit`);
      content = buffer.toString("utf8", 0, size);
    }
    return { path: file, status: probe || content.trim() ? "ok" : "absent", size: stat.size, content, error: "" };
  } catch (err) {
    if (err.code === "ENOENT") return { path: "", status: "absent", size: 0, content: "", error: "" };
    return { path: file, status: "unreadable", size: 0, content: "", error: err.message };
  } finally {
    if (fd !== undefined) fs.closeSync(fd);
  }
}

// Bound enumeration itself, not just the number of matching entries retained.
function listEntries(dir) {
  const handle = fs.opendirSync(dir);
  const names = [];
  try {
    for (let entry; (entry = handle.readSync());) {
      if (names.length === entryLimit) throw new Error(`directory exceeds ${entryLimit}-entry preparation limit`);
      names.push(entry.name);
    }
  } finally {
    handle.closeSync();
  }
  return names.sort();
}

function readActivation(dir) {
  const input = inspect(dir, "aw_info.json", { limit: 65536 });
  if (!input.path) return null;
  if (input.error) throw new Error(`Reading aw_info.json: ${input.error}`);
  // Preserve numeric IDs exactly as staged, including integers outside JS's
  // safe range. The runtime's JSON reviver exposes the original scalar token.
  let raw;
  try {
    raw = JSON.parse(input.content, (_key, value, context = { source: "" }) => (typeof value === "number" ? context.source : value));
  } catch {
    throw new Error("Unable to parse aw_info.json: invalid JSON");
  }
  function project(obj, fields, prefix = "") {
    if (obj !== null && (typeof obj !== "object" || Array.isArray(obj))) throw new Error(`Expected object in aw_info.json ${prefix}`);
    const result = {};
    for (const key of fields) {
      const value = obj?.[key];
      if (value == null || value === "") continue;
      if (typeof value === "object") throw new Error(`aw_info.json field "${prefix}${key}" must be a scalar value`);
      const text = String(value);
      if (Buffer.byteLength(text) > 512) throw new Error(`aw_info.json field "${prefix}${key}" exceeds 512-byte limit`);
      result[key] = text;
    }
    return result;
  }
  const activation = project(raw, ["event_name", "actor", "engine_id", "model", "workflow_name", "repository", "target_repo", "ref", "sha", "run_id", "run_number", "run_attempt"]);
  if (raw?.context != null) activation.context = project(raw.context, ["repo", "run_id", "workflow_id", "workflow_call_id", "actor", "event_type", "item_type", "item_number", "comment_id"], "context.");
  return activation;
}

function readCommentMemory(dir, warn) {
  const base = path.join(dir, "comment-memory");
  const files = [];
  let info = "No comment-memory files found";
  let stat;
  try {
    stat = fs.lstatSync(base);
  } catch (err) {
    if (err.code !== "ENOENT") {
      warn("comment_memory", `Unable to inspect comment-memory directory at ${base}: ${err.message}`);
      info = uninspectable("The comment-memory directory", `${base} could not be inspected (${err.message})`);
    }
    return { files, info };
  }
  if (!stat.isDirectory()) {
    warn(
      "comment_memory",
      `Expected ${base} to be a directory, found ${describe(stat)}. Comment-memory files were not read. Stage comment memory as a real directory: symlinks are refused because they can resolve outside the artifacts directory.`
    );
    return { files, info: uninspectable("The comment-memory directory", `${base} exists but is ${describe(stat)} rather than a directory, so it was refused`) };
  }
  const infos = [];
  const refused = [];
  try {
    for (const name of listEntries(base)) {
      if (!name.endsWith(".md")) continue;
      const filename = path.join(base, name);
      let entry;
      try {
        entry = fs.lstatSync(filename);
      } catch {
        refused.push(`${JSON.stringify(name)} (could not be inspected)`);
        continue;
      }
      if (!entry.isFile()) {
        refused.push(`${JSON.stringify(name)} (${describe(entry)})`);
        continue;
      }
      files.push(filename);
      const probe = inspect(dir, path.join("comment-memory", name), { probe: true });
      if (probe.status !== "ok") refused.push(`${JSON.stringify(name)} (could not be read)`);
      else infos.push(`${JSON.stringify(filename)} (${probe.size} bytes)`);
    }
  } catch (err) {
    warn("comment_memory", `Unable to read comment-memory directory at ${base}: ${err.message}`);
    return { files, info: uninspectable("The comment-memory directory", `${base} could not be read (${err.message})`) };
  }
  if (refused.length) {
    const n = refused.length;
    warn("comment_memory", `Skipped ${n} unreadable or non-regular comment-memory entries: ${refused.join(", ")}.`);
    infos.push(uninspectable("The following comment-memory entries", `they could not be safely read: ${refused.join(", ")}`));
  }
  return { files, info: infos.join("\n") || info };
}

function readPatches(dir, warn) {
  const infos = [];
  const unreadable = [];
  let readable = false;
  const bundleDiffs = [];
  let attempts = 0;
  let files = [];
  try {
    files = listEntries(dir).filter(name => name.startsWith("aw-") && (name.endsWith(".patch") || name.endsWith(".bundle")) && !fs.lstatSync(path.join(dir, name)).isDirectory());
  } catch (err) {
    warn("patch", `Unable to list patch/bundle entries: ${err.message}`);
    infos.push(uninspectable("Patch/bundle directory", "its entries could not be listed"));
  }
  for (const name of files) {
    const file = inspect(dir, name, { probe: true });
    if (!file.path || file.error) unreadable.push(`${JSON.stringify(path.join(dir, name))} (could not be read)`);
    else {
      if (name.endsWith(".bundle")) {
        if (++attempts > 4) {
          unreadable.push(`${JSON.stringify(name)} (four-bundle preparation limit)`);
          continue;
        }
        try {
          bundleDiffs.push({ path: file.path, diff: bundleReader.readBundleDiff(file.path) });
        } catch {
          unreadable.push(`${JSON.stringify(name)} (offline diff unavailable)`);
          continue;
        }
      }
      readable ||= file.size > 0;
      infos.push(`${JSON.stringify(file.path)} (${file.size} bytes, git-${name.endsWith(".patch") ? "patch" : "bundle"})`);
    }
  }
  if (unreadable.length) {
    const n = unreadable.length;
    warn("patch", `Unable to inspect ${n} patch/bundle file(s) in ${dir}: ${unreadable.join(", ")}. Detection will run without their contents.`);
    infos.push(
      infos.length ? uninspectable(`${n} further patch/bundle file(s)`, `they could not be read (${unreadable.join(", ")})`) : uninspectable("Every patch/bundle file staged for this run", `none could be read (${unreadable.join(", ")})`)
    );
  }
  if (process.env.HAS_PATCH === "true" && !readable) {
    warn("patch", `HAS_PATCH=true was set but no readable, non-empty aw-*.patch or aw-*.bundle file was found in ${dir}. Detection will run without patch context.`);
    if (!infos.length) infos.push(uninspectable("The patch produced by this run", `the agent job reported a patch (HAS_PATCH=true) but no patch or bundle file reached ${dir}`));
  }
  return { files, readable, bundleDiffs, info: infos.join("\n") || "No patch or bundle file found" };
}

function loadArtifacts(dir) {
  if (!dir || !fs.statSync(dir).isDirectory()) throw new Error(`Expected artifacts directory: ${dir}`);
  dir = path.resolve(dir);
  const warnings = [];
  const warn = (field, message) => warnings.push(warning(field, message));
  const budget = { remaining: preparationLimit };
  const inputs = Object.fromEntries(Object.entries(labels).map(([field, relative]) => [field, inspect(dir, relative, { budget })]));
  const prompt = inputs.prompt;
  if (!prompt.path) warn("prompt", `Missing detection context prompt at ${path.join(dir, labels.prompt)}. Ensure the agent artifact includes aw-prompts/prompt.txt. Detection will continue with fallback workflow context.`);
  else if (prompt.error)
    warn("prompt", `Detection context prompt at ${prompt.path} could not be read (${prompt.error}). Its contents were not examined; detection will continue with fallback workflow context. This failure is not itself evidence of a threat.`);
  else if (prompt.status === "absent") warn("prompt", `Detection context prompt at ${prompt.path} is empty. Detection will continue with fallback workflow context.`);

  const output = inspect(dir, "agent_output.json", { budget });
  let validOutput = false;
  if (output.error) warn("agent_output", `Agent output at ${output.path} could not be read: ${output.error}.`);
  else if (!output.path) warn("agent_output", `Missing agent output file at ${path.join(dir, "agent_output.json")}. Detection will run without the agent's structured output.`);
  else if (!output.size) warn("agent_output", `Agent output file at ${output.path} is empty. Detection will run without the agent's structured output.`);
  else {
    try {
      JSON.parse(output.content);
      validOutput = true;
    } catch {
      warn("agent_output", `Agent output file at ${output.path} is not valid JSON. Detection will run without the agent's structured output.`);
    }
  }
  const activation = readActivation(dir);
  const patches = readPatches(dir, warn);
  const memory = readCommentMemory(dir, warn);
  return {
    inputs,
    outputSize: output.size,
    outputUnreadable: output.status === "unreadable",
    outputPath: output.error ? uninspectable("Agent output", "it could not be read") : output.path || "No agent output file found",
    activation,
    patches,
    memory,
    warnings,
    allPrimaryMissing: prompt.status !== "ok" && !validOutput && !patches.readable,
  };
}

function readHostPrompt(file) {
  const resolved = fs.realpathSync(file);
  const input = inspect(path.dirname(resolved), path.basename(resolved));
  if (!input.path || input.error) throw new Error(`Unable to read detector prompt option ${file}: ${input.error || "not found"}`);
  return input.content;
}

module.exports = { loadArtifacts, labels, uninspectable, warning, readHostPrompt };
