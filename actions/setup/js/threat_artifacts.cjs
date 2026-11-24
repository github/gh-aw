// @ts-check
const fs = require("node:fs");
const path = require("node:path");

const labels = {
  prompt: "aw-prompts/prompt.txt",
  prompt_template: "aw-prompts/prompt-template.txt",
  prompt_import_tree: "aw-prompts/prompt-import-tree.json",
};

function uninspectable(what, reason) {
  return `${what} was NOT analyzed: ${reason}. Treat this channel as unexamined rather than empty, and do not draw any conclusion about its contents. This is a staging or configuration failure, not evidence of a threat: do NOT report a threat on the basis of this notice alone.`;
}

function warning(field, message) {
  const chars = Array.from(message);
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
function inspect(dir, relative, { probe = false, limit = Infinity } = {}) {
  const file = path.join(dir, relative);
  let fd;
  try {
    const parent = path.dirname(relative);
    if (parent !== "." && !fs.lstatSync(path.join(dir, parent)).isDirectory()) throw new Error("parent is not a real directory");
    fd = fs.openSync(file, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
    const stat = fs.fstatSync(fd);
    if (!stat.isFile()) throw new Error("not a regular file");
    if (stat.size > limit) throw new Error(`${relative} exceeds ${limit}-byte limit`);
    let content = "";
    if (probe) fs.readSync(fd, Buffer.alloc(1), 0, 1, 0);
    else if (Number.isFinite(limit)) {
      const buffer = Buffer.alloc(limit + 1);
      let size = 0;
      while (size < buffer.length) {
        const bytes = fs.readSync(fd, buffer, size, buffer.length - size, null);
        if (!bytes) break;
        size += bytes;
      }
      if (size > limit) throw new Error(`${relative} exceeds ${limit}-byte limit`);
      content = buffer.toString("utf8", 0, size);
    } else content = fs.readFileSync(fd, "utf8");
    return { path: file, status: probe || content.trim() ? "ok" : "absent", size: stat.size, content, error: "" };
  } catch (err) {
    if (err.code === "ENOENT") return { path: "", status: "absent", size: 0, content: "", error: "" };
    return { path: file, status: "unreadable", size: 0, content: "", error: err.message };
  } finally {
    if (fd !== undefined) fs.closeSync(fd);
  }
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
    for (const name of fs.readdirSync(base).sort()) {
      if (!name.endsWith(".md")) continue;
      const filename = path.join(base, name);
      const entry = fs.lstatSync(filename);
      if (entry.isFile()) {
        files.push(filename);
        infos.push(`${filename} (${entry.size} bytes)`);
      } else refused.push(`${name} (${describe(entry)})`);
    }
  } catch (err) {
    warn("comment_memory", `Unable to read comment-memory directory at ${base}: ${err.message}`);
    return { files, info: uninspectable("The comment-memory directory", `${base} could not be read (${err.message})`) };
  }
  if (refused.length) {
    const n = refused.length;
    warn(
      "comment_memory",
      `Skipped ${n} comment-memory ${n === 1 ? "entry" : "entries"} in ${base} that ${n === 1 ? "is" : "are"} not regular files: ${refused.join(", ")}. Stage comment memory as real files: symlinks and special files are refused because they can resolve outside the artifacts directory.`
    );
    infos.push(uninspectable(files.length ? `${n} further comment-memory file(s) in ${base}` : `Every comment-memory file in ${base}`, "they are symlinks or special files rather than regular files, so they were refused"));
  }
  return { files, info: infos.join("\n") || info };
}

function readPatches(dir, warn) {
  const infos = [];
  const unreadable = [];
  let readable = false;
  const files = fs
    .readdirSync(dir)
    .sort()
    .filter(name => name.startsWith("aw-") && (name.endsWith(".patch") || name.endsWith(".bundle")) && !fs.lstatSync(path.join(dir, name)).isDirectory());
  for (const name of files) {
    const file = inspect(dir, name, { probe: true });
    if (!file.path || file.error) unreadable.push(`${path.join(dir, name)} (${file.error || "not found"})`);
    else {
      readable ||= file.size > 0;
      infos.push(`${file.path} (${file.size} bytes, git-${name.endsWith(".patch") ? "patch" : "bundle"})`);
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
  return { files, readable, info: infos.join("\n") || "No patch or bundle file found" };
}

function loadArtifacts(dir) {
  if (!dir || !fs.statSync(dir).isDirectory()) throw new Error(`Expected artifacts directory: ${dir}`);
  const warnings = [];
  const warn = (field, message) => warnings.push(warning(field, message));
  const inputs = Object.fromEntries(Object.entries(labels).map(([field, relative]) => [field, inspect(dir, relative)]));
  const prompt = inputs.prompt;
  if (!prompt.path) warn("prompt", `Missing detection context prompt at ${path.join(dir, labels.prompt)}. Ensure the agent artifact includes aw-prompts/prompt.txt. Detection will continue with fallback workflow context.`);
  else if (prompt.error)
    warn("prompt", `Detection context prompt at ${prompt.path} could not be read (${prompt.error}). Its contents were not examined; detection will continue with fallback workflow context. This failure is not itself evidence of a threat.`);
  else if (!prompt.size) warn("prompt", `Detection context prompt at ${prompt.path} is empty. Detection will continue with fallback workflow context.`);

  const output = inspect(dir, "agent_output.json");
  let validOutput = false;
  if (!output.path || output.error) warn("agent_output", `Missing agent output file at ${path.join(dir, "agent_output.json")}. Detection will run without the agent's structured output.`);
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
    outputPath: output.error || !output.path ? "No agent output file found" : output.path,
    activation,
    patches,
    memory,
    warnings,
    allPrimaryMissing: !prompt.size && !validOutput && !patches.readable,
  };
}

module.exports = { loadArtifacts, labels, uninspectable, warning };
