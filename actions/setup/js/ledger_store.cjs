// @ts-check

const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");
const vm = require("node:vm");
const { validateValueAgainstSchema } = require("./mcp_scripts_validation.cjs");

const MAX_FILES = 1024;
const MAX_SEGMENT_BYTES = 10 * 1024 * 1024;
const MAX_RECORD_BYTES = 32 * 1024;
const MAX_SCHEMA_BYTES = 1024 * 1024;
const MAX_PARENTS = 64;
const MAX_QUERY_LIMIT = 500;
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const RECORD_ID = /^ldg-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const HASH = /^sha256:[0-9a-f]{64}$/;
const SEGMENT_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

function canonicalJSON(value) {
  const seen = new Set();
  function normalize(item, depth) {
    if (depth > 32) throw new TypeError("Ledger JSON exceeds maximum nesting depth");
    if (item === null || typeof item === "string" || typeof item === "boolean") return item;
    if (typeof item === "number" && Number.isFinite(item)) return item;
    if (typeof item !== "object" || seen.has(item)) throw new TypeError("Ledger value must be finite, acyclic JSON");
    seen.add(item);
    let result;
    if (Array.isArray(item)) {
      if (Object.keys(item).length !== item.length) throw new TypeError("Ledger arrays cannot be sparse");
      result = item.map(entry => normalize(entry, depth + 1));
    } else {
      if (Object.getPrototypeOf(item) !== Object.prototype && Object.getPrototypeOf(item) !== null) throw new TypeError("Ledger value must be a plain JSON object");
      result = {};
      for (const key of Object.keys(item).sort()) {
        Object.defineProperty(result, key, { value: normalize(item[key], depth + 1), enumerable: true, configurable: true });
      }
    }
    seen.delete(item);
    return result;
  }
  return JSON.stringify(normalize(value, 0));
}

function sha256(value) {
  return `sha256:${crypto.createHash("sha256").update(canonicalJSON(value)).digest("hex")}`;
}

function compileSchema(schemaPath) {
  if (typeof schemaPath !== "string" || !schemaPath.trim()) throw new TypeError("Invalid ledger schemaPath");
  let bytes;
  try {
    const stat = fs.lstatSync(schemaPath);
    if (!stat.isFile() || stat.isSymbolicLink()) throw new TypeError("Ledger schema must be a regular file, not a symlink");
    if (stat.size > MAX_SCHEMA_BYTES) throw new RangeError("Ledger schema exceeds maximum size");
    const fd = fs.openSync(schemaPath, fs.constants.O_RDONLY | (fs.constants.O_NOFOLLOW || 0));
    try {
      const buffer = Buffer.alloc(MAX_SCHEMA_BYTES + 1);
      const length = fs.readSync(fd, buffer, 0, buffer.length, 0);
      if (length > MAX_SCHEMA_BYTES) throw new RangeError("Ledger schema exceeds maximum size");
      bytes = buffer.subarray(0, length);
    } finally {
      fs.closeSync(fd);
    }
  } catch (error) {
    if (error instanceof RangeError || error instanceof TypeError) throw error;
    throw new Error("Failed to read ledger schema", { cause: error });
  }
  let schema;
  try {
    schema = JSON.parse(bytes.toString("utf8"));
  } catch (error) {
    throw new TypeError("Ledger schema must be JSON", { cause: error });
  }
  const allowed = new Set(["type", "enum", "required", "properties", "additionalProperties", "items", "oneOf", "anyOf"]);
  const types = new Set(["object", "array", "string", "number", "integer", "boolean", "null"]);
  function check(node, depth = 0) {
    if (!node || typeof node !== "object" || Array.isArray(node) || depth > 32) throw new TypeError("Ledger schema must contain bounded objects");
    if (("oneOf" in node || "anyOf" in node) && Object.keys(node).length !== 1) throw new TypeError("Ledger schema alternatives cannot combine with ignored constraints");
    for (const [key, value] of Object.entries(node)) {
      if (!allowed.has(key)) throw new TypeError("Unsupported ledger schema keyword");
      if (key === "type") {
        const entries = Array.isArray(value) ? value : [value];
        if (!entries.length || !entries.every(type => types.has(type))) throw new TypeError("Invalid ledger schema type");
      } else if (key === "enum") {
        if (!Array.isArray(value) || !value.length || !value.every(item => item === null || ["string", "number", "boolean"].includes(typeof item))) throw new TypeError("Ledger schema enum supports only primitive JSON values");
      } else if (key === "required") {
        if (!Array.isArray(value) || !value.every(item => typeof item === "string")) throw new TypeError("Invalid ledger schema required fields");
      } else if (key === "additionalProperties") {
        if (value !== false) throw new TypeError("Only additionalProperties: false is supported");
      } else if (key === "properties") {
        if (!value || typeof value !== "object" || Array.isArray(value)) throw new TypeError("Invalid ledger schema properties");
        Object.values(value).forEach(child => check(child, depth + 1));
      } else if (key === "items") check(value, depth + 1);
      else if (key === "oneOf" || key === "anyOf") {
        if (!Array.isArray(value) || value.length === 0) throw new TypeError("Invalid ledger schema alternatives");
        value.forEach(child => check(child, depth + 1));
      }
    }
  }
  check(schema);
  return value => validateValueAgainstSchema(value, schema) === null;
}

function recordShape(record) {
  return (
    record &&
    typeof record === "object" &&
    !Array.isArray(record) &&
    Object.keys(record).sort().join(",") === "id,parents,payload,sha,timestamp,type,version" &&
    record.version === 1 &&
    typeof record.id === "string" &&
    RECORD_ID.test(record.id) &&
    typeof record.type === "string" &&
    record.type.length > 0 &&
    record.type.length <= 128 &&
    typeof record.timestamp === "string" &&
    Number.isFinite(Date.parse(record.timestamp)) &&
    new Date(record.timestamp).toISOString() === record.timestamp &&
    Array.isArray(record.parents) &&
    record.parents.length <= MAX_PARENTS &&
    new Set(record.parents).size === record.parents.length &&
    record.parents.every(parent => typeof parent === "string" && HASH.test(parent)) &&
    record.parents.every((parent, index) => index === 0 || record.parents[index - 1] < parent) &&
    typeof record.sha === "string" &&
    HASH.test(record.sha)
  );
}

function emptyState(diagnostics = []) {
  return { records: [], heads: [], shard_count: 0, diagnostics, shards: [], locations: new Map() };
}

function checkShardDirectories(shardDir, create = false) {
  for (const dir of [path.dirname(shardDir), shardDir]) {
    let stat;
    try {
      stat = fs.lstatSync(dir);
    } catch (error) {
      if (!error || typeof error !== "object" || Reflect.get(error, "code") !== "ENOENT") throw error;
      if (!create) return false;
      try {
        fs.mkdirSync(dir);
      } catch (mkdirError) {
        if (!mkdirError || typeof mkdirError !== "object" || Reflect.get(mkdirError, "code") !== "EEXIST") throw mkdirError;
      }
      stat = fs.lstatSync(dir);
    }
    if (!stat.isDirectory() || stat.isSymbolicLink()) throw new TypeError("Ledger directory must be a real directory, not a symlink");
  }
  return true;
}

function matchesPayload(payload, filters) {
  return filters.every(([key, condition]) => {
    const segments = key.slice("payload.".length).split(".");
    const value = segments.reduce((item, segment) => (item !== null && typeof item === "object" && Object.hasOwn(item, segment) ? item[segment] : undefined), payload);
    const operator = Object.keys(condition)[0];
    const expected = condition[operator];
    if (operator === "exists") return (value !== undefined) === expected;
    if (value === undefined) return false;
    if (operator === "eq") return canonicalJSON(value) === canonicalJSON(expected);
    if (operator === "in") return expected.some(item => canonicalJSON(item) === canonicalJSON(value));
    if (operator === "prefix") return typeof value === "string" && value.startsWith(expected);
    if (typeof value !== typeof expected || (typeof value !== "number" && typeof value !== "string")) return false;
    if (operator === "gt") return value > expected;
    if (operator === "gte") return value >= expected;
    if (operator === "lt") return value < expected;
    return value <= expected;
  });
}

function sqlPayloadFilter(filters, type) {
  const clauses = [];
  const params = [];
  if (type !== undefined) {
    clauses.push("type = ?");
    params.push(type);
  }
  for (const [key, condition] of filters) {
    const selector = `$${key
      .slice("payload.".length)
      .split(".")
      .map(part => `."${part}"`)
      .join("")}`;
    const [operator] = Object.keys(condition);
    const expected = condition[operator];
    if (operator === "exists") {
      clauses.push(`json_type(payload, ?) IS ${expected ? "NOT " : ""}NULL`);
      params.push(selector);
    } else if (operator === "eq" && expected === null) {
      clauses.push("json_type(payload, ?) = 'null'");
      params.push(selector);
    } else if (operator === "eq" && typeof expected !== "object") {
      clauses.push("json_extract(payload, ?) = ?");
      params.push(selector, typeof expected === "boolean" ? Number(expected) : expected);
    } else if (operator === "in" && expected.length && expected.every(item => item !== null && typeof item !== "object")) {
      clauses.push(`json_extract(payload, ?) IN (${expected.map(() => "?").join(",")})`);
      params.push(selector, ...expected.map(item => (typeof item === "boolean" ? Number(item) : item)));
    } else if (operator === "prefix") {
      clauses.push("json_type(payload, ?) = 'text' AND substr(json_extract(payload, ?), 1, length(?)) = ?");
      params.push(selector, selector, expected, expected);
    } else if (["gt", "gte", "lt", "lte"].includes(operator) && typeof expected === "number") {
      const comparison = { gt: ">", gte: ">=", lt: "<", lte: "<=" }[operator];
      clauses.push(`json_type(payload, ?) IN ('integer', 'real') AND json_extract(payload, ?) ${comparison} ?`);
      params.push(selector, selector, expected);
    }
  }
  return { where: clauses.length ? ` WHERE ${clauses.join(" AND ")}` : "", params };
}

class Ledger {
  /** @param {{memoryDir?: string, schemaPath?: string, maxFiles?: number, maxPatchBytes?: number, clock?: () => Date, excludeSegments?: string[]}} [options] */
  constructor({ memoryDir, schemaPath, maxFiles = MAX_FILES, maxPatchBytes = MAX_SEGMENT_BYTES, clock = () => new Date(), excludeSegments = [] } = {}) {
    if (typeof memoryDir !== "string" || !memoryDir.trim()) throw new TypeError("memoryDir is required");
    if (!Number.isSafeInteger(maxFiles) || maxFiles < 1 || maxFiles > MAX_FILES) throw new RangeError("Invalid maxFiles");
    if (!Number.isSafeInteger(maxPatchBytes) || maxPatchBytes < 1 || maxPatchBytes > MAX_SEGMENT_BYTES) throw new RangeError("Invalid maxPatchBytes");
    this.shardDir = path.join(memoryDir, "ledger", "shards");
    this.coverageDir = path.join(memoryDir, "ledger", "coverage");
    this.maxFiles = maxFiles;
    this.maxPatchBytes = maxPatchBytes;
    this.writtenBytes = 0;
    this.clock = clock;
    this.writerId = crypto.randomUUID();
    this.writerPath = path.join(this.shardDir, `${this.writerId}.jsonl`);
    this.excludeSegments = new Set(excludeSegments);
    /** @type {{db: import("node:sqlite").DatabaseSync, fingerprint: string} | null} */
    this.projection = null;
    if (schemaPath !== undefined) this.validate = compileSchema(schemaPath);
  }

  segmentPath(id) {
    if (typeof id !== "string" || !SEGMENT_ID.test(id)) throw new TypeError("Invalid ledger segment ID");
    return path.join(this.shardDir, `${id}.jsonl`);
  }

  listSegments({ closed = true, excludeCurrent = true } = {}) {
    const state = this.reconstruct();
    const byFile = new Map();
    for (const record of state.records) {
      const location = state.locations.get(record.sha);
      if (!location) continue;
      if (!byFile.has(location.shard)) byFile.set(location.shard, []);
      byFile.get(location.shard).push(record);
    }
    return state.shards
      .filter(shard => UUID.test(shard.file.slice(0, -6)))
      .filter(shard => !excludeCurrent || (shard.file !== path.basename(this.writerPath) && !this.excludeSegments.has(shard.file.slice(0, -6))))
      .map(shard => ({
        id: shard.file.slice(0, -6),
        file: shard.file,
        closed: shard.file !== path.basename(this.writerPath),
        bytes: shard.size,
        records: (byFile.get(shard.file) || []).length,
      }))
      .filter(segment => !closed || segment.closed)
      .sort((left, right) => left.id.localeCompare(right.id));
  }

  readRecords(segmentId) {
    const segment = this.listSegments({ closed: false, excludeCurrent: false }).find(item => item.id === segmentId);
    if (!segment) throw new TypeError("Unknown ledger segment");
    const state = this.reconstruct();
    return state.records.filter(record => state.locations.get(record.sha)?.shard === segment.file).sort((left, right) => left.sha.localeCompare(right.sha));
  }

  createSegment(records, metadata = {}) {
    if (!Array.isArray(records) || records.length === 0) throw new TypeError("Ledger segment records are required");
    const unique = new Map();
    for (const input of records) {
      const record = JSON.parse(JSON.stringify(input));
      if (!recordShape(record) || sha256(Object.fromEntries(Object.entries(record).filter(([key]) => key !== "sha"))) !== record.sha || canonicalJSON(record) !== JSON.stringify(record)) {
        throw new TypeError("Ledger segment contains an invalid record");
      }
      unique.set(record.sha, record);
    }
    const ordered = [...unique.values()].sort((left, right) => left.sha.localeCompare(right.sha));
    const body = { records: ordered, metadata: canonicalJSON(JSON.parse(JSON.stringify(metadata))) };
    const digest = crypto.createHash("sha256").update(canonicalJSON(body)).digest("hex");
    const id = `${digest.slice(0, 8)}-${digest.slice(8, 12)}-4${digest.slice(13, 16)}-${((parseInt(digest.slice(16, 18), 16) & 0x3f) | 0x80).toString(16).padStart(2, "0")}${digest.slice(18, 20)}-${digest.slice(20, 32)}`;
    const content = Buffer.from(ordered.map(record => `${canonicalJSON(record)}\n`).join(""));
    if (content.length > MAX_SEGMENT_BYTES) throw new RangeError("Ledger segment exceeds maximum size");
    checkShardDirectories(this.shardDir, true);
    const destination = this.segmentPath(id);
    if (fs.existsSync(destination)) {
      if (!fs.readFileSync(destination).equals(content)) throw new Error("Ledger segment identity collision");
      return { id, records: ordered.length };
    }
    const staging = path.join(this.shardDir, `${id}.pending`);
    const fd = fs.openSync(staging, "wx", 0o600);
    try {
      fs.writeFileSync(fd, content);
      fs.fsyncSync(fd);
    } finally {
      fs.closeSync(fd);
    }
    fs.renameSync(staging, destination);
    const directory = fs.openSync(this.shardDir, "r");
    try {
      fs.fsyncSync(directory);
    } finally {
      fs.closeSync(directory);
    }
    return { id, records: ordered.length };
  }

  markCovered(sourceIds, replacementId) {
    if (!Array.isArray(sourceIds) || sourceIds.length === 0 || !sourceIds.every(id => SEGMENT_ID.test(id)) || !SEGMENT_ID.test(replacementId)) {
      throw new TypeError("Invalid ledger coverage declaration");
    }
    const declaration = { sources: [...new Set(sourceIds)].sort(), replacement: replacementId };
    checkShardDirectories(this.coverageDir, true);
    const name = sha256(declaration).slice("sha256:".length);
    const destination = path.join(this.coverageDir, `${name}.jsonl`);
    if (!fs.existsSync(destination)) {
      fs.writeFileSync(destination, `${canonicalJSON(declaration)}\n`, { flag: "wx", mode: 0o600 });
      const directory = fs.openSync(this.coverageDir, "r");
      try {
        fs.fsyncSync(directory);
      } finally {
        fs.closeSync(directory);
      }
    }
    return { id: name, sources: declaration.sources, replacement: replacementId };
  }

  retireCovered() {
    if (!fs.existsSync(this.coverageDir)) return 0;
    const available = new Set(this.listSegments({ closed: false, excludeCurrent: false }).map(segment => segment.id));
    let retired = 0;
    for (const file of fs
      .readdirSync(this.coverageDir)
      .filter(name => name.endsWith(".jsonl"))
      .sort()) {
      let declaration;
      try {
        const lines = fs.readFileSync(path.join(this.coverageDir, file), "utf8").trim().split("\n");
        declaration = JSON.parse(lines[0]);
      } catch {
        continue;
      }
      if (!declaration || !Array.isArray(declaration.sources) || !available.has(declaration.replacement) || !declaration.sources.every(source => available.has(source))) continue;
      for (const source of declaration.sources) {
        if (source === this.writerId) continue;
        const sourcePath = this.segmentPath(source);
        if (fs.existsSync(sourcePath)) {
          fs.unlinkSync(sourcePath);
          retired++;
        }
      }
    }
    return retired;
  }

  async runCompactor(script) {
    if (typeof script !== "string" || !script.trim()) return { changed: false, logs: [] };
    const logs = [];
    const api = {
      listSegments: async options => this.listSegments(options),
      readRecords: async id => this.readRecords(id),
      createSegment: async (records, metadata) => this.createSegment(records, metadata),
      markCovered: async (sources, replacement) => this.markCovered(sources, replacement),
      compact: async segments => {
        const selected = segments.filter(segment => segment.closed && segment.id !== this.writerId);
        const records = (await Promise.all(selected.map(segment => this.readRecords(segment.id)))).flat();
        if (!records.length) return null;
        const replacement = await this.createSegment(records);
        await this.markCovered(
          selected.map(segment => segment.id),
          replacement.id
        );
        return replacement;
      },
      log: (...values) => logs.push(values.map(value => (typeof value === "string" ? value : JSON.stringify(value))).join(" ")),
      runTimestamp: this.clock().toISOString(),
    };
    const context = vm.createContext(Object.freeze({ ledger: api }), { codeGeneration: { strings: false, wasm: false } });
    await vm.runInContext(`(async () => {${script}\n})()`, context, { timeout: 1000 });
    return { changed: this.retireCovered() > 0, logs };
  }

  reconstruct() {
    const diagnostics = [];
    if (!checkShardDirectories(this.shardDir)) return emptyState();
    let files;
    try {
      files = fs
        .readdirSync(this.shardDir)
        .filter(name => name.endsWith(".jsonl"))
        .sort();
    } catch (error) {
      if (error && typeof error === "object" && "code" in error && error.code === "ENOENT") return emptyState();
      throw error;
    }
    if (files.length > this.maxFiles) {
      diagnostics.push({ code: "invalid-envelope", detail: `Only the first ${this.maxFiles} ledger shards were inspected` });
      files.length = this.maxFiles;
    }
    const bySha = new Map();
    const ids = new Map();
    const shards = [];
    const locations = new Map();
    for (const file of files) {
      const shard = { file, size: 0 };
      shards.push(shard);
      if (!UUID.test(file.slice(0, -6))) {
        diagnostics.push({ code: "invalid-envelope", file });
        continue;
      }
      let content;
      let raw;
      try {
        const fullPath = path.join(this.shardDir, file);
        const stat = fs.lstatSync(fullPath);
        shard.size = stat.size;
        if (stat.isSymbolicLink()) {
          diagnostics.push({ code: "invalid-envelope", file });
          continue;
        }
        if (!stat.isFile() || stat.size > MAX_SEGMENT_BYTES) {
          diagnostics.push({ code: "invalid-envelope", file });
          continue;
        }
        const fd = fs.openSync(fullPath, fs.constants.O_RDONLY | (fs.constants.O_NOFOLLOW || 0));
        try {
          const buffer = Buffer.alloc(MAX_SEGMENT_BYTES + 1);
          const length = fs.readSync(fd, buffer, 0, buffer.length, 0);
          if (length > MAX_SEGMENT_BYTES) {
            diagnostics.push({ code: "invalid-envelope", file });
            continue;
          }
          raw = buffer.subarray(0, length);
          content = raw.toString("utf8");
        } finally {
          fs.closeSync(fd);
        }
      } catch {
        diagnostics.push({ code: "invalid-envelope", file });
        continue;
      }
      const lines = content.split("\n");
      const offsets = [];
      for (let pos = 0; pos < raw.length; pos++) {
        if (pos === 0 || raw[pos - 1] === 10) offsets.push(pos);
      }
      if (lines.at(-1) !== "") diagnostics.push({ code: "truncated-record", file, line: lines.length });
      for (let index = 0; index < lines.length - 1; index++) {
        const text = lines[index];
        const position = { file, line: index + 1 };
        let record;
        try {
          record = JSON.parse(text);
        } catch {
          diagnostics.push({ code: "invalid-json", ...position });
          continue;
        }
        try {
          if (record && typeof record === "object" && Object.hasOwn(record, "version") && record.version !== 1) {
            diagnostics.push({ code: "unsupported-version", ...position });
            continue;
          }
          if (!recordShape(record)) {
            diagnostics.push({ code: "invalid-envelope", ...position });
            continue;
          }
          const { sha, ...body } = record;
          if (sha256(body) !== sha) {
            diagnostics.push({ code: "invalid-sha", ...position });
            continue;
          }
          if (text !== canonicalJSON(record)) {
            diagnostics.push({ code: "invalid-envelope", ...position });
            continue;
          }
          if (this.validate && !this.validate(record.payload)) {
            diagnostics.push({ code: "invalid-schema", ...position });
            continue;
          }
          if (bySha.has(sha)) {
            diagnostics.push({ code: "duplicate-id", ...position });
            continue;
          }
          if (ids.has(record.id)) diagnostics.push({ code: ids.get(record.id) === sha ? "duplicate-id" : "duplicate-id-conflict", ...position, id: record.id });
          else ids.set(record.id, sha);
          bySha.set(sha, record);
          locations.set(sha, { shard: file, offset: offsets[index] });
        } catch {
          diagnostics.push({ code: "invalid-envelope", ...position });
        }
      }
    }
    const records = [];
    let pending = [...bySha.keys()].sort();
    const accepted = new Set();
    while (pending.length) {
      const ready = pending.filter(sha => bySha.get(sha).parents.every(parent => accepted.has(parent) || !bySha.has(parent)));
      if (!ready.length) break;
      for (const sha of ready) {
        const record = bySha.get(sha);
        if (record.parents.some(parent => !bySha.has(parent))) diagnostics.push({ code: "missing-parent", id: record.id });
        accepted.add(sha);
        records.push(record);
      }
      const readySet = new Set(ready);
      pending = pending.filter(sha => !readySet.has(sha));
    }
    for (const sha of pending) diagnostics.push({ code: "cycle", id: bySha.get(sha).id });
    const parents = new Set(records.flatMap(record => record.parents));
    const heads = records
      .filter(record => !parents.has(record.sha))
      .map(record => record.sha)
      .sort();
    return { records, heads, shard_count: files.length, diagnostics, shards, locations };
  }

  append(type, payload) {
    if (typeof type !== "string" || !type || type.length > 128 || /[\u0000-\u001f]/.test(type)) throw new TypeError("Invalid ledger record type");
    let copy;
    try {
      copy = JSON.parse(canonicalJSON(payload));
    } catch (error) {
      throw new TypeError("Ledger payload must be JSON", { cause: error });
    }
    const validator = this.validate;
    if (validator && !validator(copy)) throw new TypeError("Ledger payload does not match local JSON Schema");
    const state = this.reconstruct();
    if (state.heads.length > MAX_PARENTS) throw new RangeError("Too many ledger DAG heads");
    const body = { version: 1, id: `ldg-${crypto.randomUUID()}`, type, timestamp: this.clock().toISOString(), parents: state.heads, payload: copy };
    const record = { ...body, sha: sha256(body) };
    const line = Buffer.from(`${canonicalJSON(record)}\n`);
    if (line.length > MAX_RECORD_BYTES) throw new RangeError("Ledger record exceeds maximum message size");
    if (this.writtenBytes + line.length > this.maxPatchBytes) throw new RangeError("Ledger run exceeds maximum patch size");
    try {
      checkShardDirectories(this.shardDir, true);
    } catch (error) {
      throw new Error("Failed to create ledger shard directory", { cause: error });
    }
    let existing = 0;
    try {
      let writerStat;
      try {
        writerStat = fs.lstatSync(this.writerPath);
      } catch (error) {
        if (!error || typeof error !== "object" || Reflect.get(error, "code") !== "ENOENT") throw error;
      }
      if (writerStat) {
        if (!writerStat.isFile() || writerStat.isSymbolicLink()) throw new TypeError("Ledger writer shard must be a regular file, not a symlink");
        existing = writerStat.size;
      }
      if (existing && state.diagnostics.some(entry => entry.file === path.basename(this.writerPath))) existing = MAX_SEGMENT_BYTES;
      if (existing + line.length > MAX_SEGMENT_BYTES) {
        this.writerId = crypto.randomUUID();
        this.writerPath = path.join(this.shardDir, `${this.writerId}.jsonl`);
        existing = 0;
      }
      if (!existing && fs.readdirSync(this.shardDir).filter(name => name.endsWith(".jsonl")).length >= this.maxFiles) throw new RangeError("Ledger shard file-count limit reached");
    } catch (error) {
      if (error instanceof RangeError) throw error;
      throw new Error("Failed to inspect ledger shard files", { cause: error });
    }
    checkShardDirectories(this.shardDir);
    if (existing) {
      const fd = fs.openSync(this.writerPath, fs.constants.O_WRONLY | fs.constants.O_APPEND | (fs.constants.O_NOFOLLOW || 0));
      try {
        fs.writeFileSync(fd, line);
        fs.fsyncSync(fd);
      } catch (error) {
        try {
          fs.ftruncateSync(fd, existing);
          fs.fsyncSync(fd);
        } catch {
          // Best-effort cleanup: reconstruction ignores incomplete JSONL lines.
        }
        throw new Error("Failed to durably append ledger record", { cause: error });
      } finally {
        fs.closeSync(fd);
      }
    } else {
      const staging = path.join(this.shardDir, `${this.writerId}.pending`);
      const fd = fs.openSync(staging, "wx", 0o600);
      let failure;
      try {
        fs.writeFileSync(fd, line);
        fs.fsyncSync(fd);
      } catch (error) {
        failure = error;
      } finally {
        fs.closeSync(fd);
      }
      if (failure) {
        try {
          fs.unlinkSync(staging);
        } catch {
          // Best-effort cleanup: reconstruction ignores pending files.
        }
        throw new Error("Failed to durably write ledger shard", { cause: failure });
      }
      try {
        fs.renameSync(staging, this.writerPath);
        const directory = fs.openSync(this.shardDir, "r");
        try {
          fs.fsyncSync(directory);
        } finally {
          fs.closeSync(directory);
        }
      } catch (error) {
        try {
          fs.rmSync(staging, { force: true });
          fs.rmSync(this.writerPath, { force: true });
        } catch {
          // Best-effort cleanup: a failed sync cannot acknowledge the record.
        }
        throw new Error("Failed to publish ledger shard", { cause: error });
      }
    }
    this.writtenBytes += line.length;
    if (this.projection) {
      const previous = this.projection;
      this.projection = null;
      previous.db.close();
    }
    return record;
  }

  get(idOrSha) {
    if (typeof idOrSha !== "string" || (!RECORD_ID.test(idOrSha) && !HASH.test(idOrSha))) throw new TypeError("Invalid ledger record ID or SHA");
    const state = this.reconstruct();
    const db = this.project(state);
    if (!db) return state.records.find(record => record.id === idOrSha || record.sha === idOrSha) || null;
    const row = db.prepare("SELECT ordinal FROM records WHERE id = ? OR sha = ? ORDER BY ordinal LIMIT 1").get(idOrSha, idOrSha);
    return row ? state.records[Number(row.ordinal)] : null;
  }

  project(state) {
    let DatabaseSync;
    try {
      ({ DatabaseSync } = require("node:sqlite"));
    } catch (error) {
      if (!error || typeof error !== "object" || Reflect.get(error, "code") !== "ERR_UNKNOWN_BUILTIN_MODULE") throw error;
      return null;
    }
    const fingerprint = sha256({
      records: state.records.map(record => record.sha),
      diagnostics: state.diagnostics,
      shards: state.shards,
      locations: [...state.locations.entries()],
    });
    if (this.projection?.fingerprint === fingerprint) return this.projection.db;
    const db = new DatabaseSync(":memory:");
    try {
      db.exec(`
        CREATE TABLE shards (file TEXT PRIMARY KEY, size INTEGER NOT NULL);
        CREATE TABLE records (ordinal INTEGER PRIMARY KEY, id TEXT NOT NULL, sha TEXT NOT NULL UNIQUE, type TEXT NOT NULL, timestamp TEXT NOT NULL, payload TEXT NOT NULL, shard TEXT NOT NULL, offset INTEGER NOT NULL);
        CREATE TABLE parents (child_sha TEXT NOT NULL, parent_sha TEXT NOT NULL, PRIMARY KEY(child_sha, parent_sha));
        CREATE TABLE diagnostics (ordinal INTEGER PRIMARY KEY, code TEXT NOT NULL, file TEXT, line INTEGER, id TEXT, detail TEXT);
        CREATE INDEX records_by_id ON records(id);
        CREATE INDEX records_by_type_time ON records(type, timestamp);
        CREATE INDEX records_by_shard_offset ON records(shard, offset);
        CREATE INDEX parents_by_parent ON parents(parent_sha);
        CREATE INDEX diagnostics_by_code ON diagnostics(code);
      `);
      db.exec("BEGIN");
      const insertShard = db.prepare("INSERT INTO shards (file, size) VALUES (?, ?)");
      const insertRecord = db.prepare("INSERT INTO records (ordinal, id, sha, type, timestamp, payload, shard, offset) VALUES (?, ?, ?, ?, ?, ?, ?, ?)");
      const insertParent = db.prepare("INSERT INTO parents (child_sha, parent_sha) VALUES (?, ?)");
      const insertDiagnostic = db.prepare("INSERT INTO diagnostics (ordinal, code, file, line, id, detail) VALUES (?, ?, ?, ?, ?, ?)");
      for (const shard of state.shards) insertShard.run(shard.file, shard.size);
      for (const [ordinal, record] of state.records.entries()) {
        const location = state.locations.get(record.sha);
        insertRecord.run(ordinal, record.id, record.sha, record.type, record.timestamp, canonicalJSON(record.payload), location.shard, location.offset);
        for (const parent of record.parents) insertParent.run(record.sha, parent);
      }
      for (const [ordinal, diagnostic] of state.diagnostics.entries()) {
        insertDiagnostic.run(ordinal, diagnostic.code, diagnostic.file || null, diagnostic.line || null, diagnostic.id || null, diagnostic.detail || null);
      }
      db.exec("COMMIT");
    } catch (error) {
      db.close();
      throw error;
    }
    if (this.projection) this.projection.db.close();
    this.projection = { db, fingerprint };
    return db;
  }

  /** @param {{type?: string, where?: Record<string, any>, limit?: number}} [filter] */
  query({ type, where, limit = 100 } = {}) {
    if (type !== undefined && (typeof type !== "string" || !type || type.length > 128)) throw new TypeError("Invalid query type");
    if (!Number.isSafeInteger(limit) || limit < 1 || limit > MAX_QUERY_LIMIT) throw new RangeError("Invalid query limit");
    if (where !== undefined && (!where || typeof where !== "object" || Array.isArray(where) || Object.keys(where).length > 8)) throw new TypeError("Invalid query where filter");
    const filters = Object.entries(where || {});
    for (const [key, condition] of filters) {
      if (!/^payload\.[a-zA-Z_][\w-]*(?:\.[a-zA-Z_][\w-]*){0,7}$/.test(key)) throw new TypeError("Invalid query payload path");
      if (!condition || typeof condition !== "object" || Array.isArray(condition) || Object.keys(condition).length !== 1) throw new TypeError("Query predicate must have one operator");
      const [op] = Object.keys(condition);
      if (!["eq", "in", "exists", "prefix", "gt", "gte", "lt", "lte"].includes(op)) throw new TypeError("Unsupported query operator");
      const json = canonicalJSON(condition);
      if (Buffer.byteLength(json) > 1024) throw new RangeError("Query value exceeds maximum size");
      if (op === "in" && (!Array.isArray(condition.in) || condition.in.length > 32)) throw new TypeError("Invalid in query filter");
      if (op === "exists" && typeof condition.exists !== "boolean") throw new TypeError("Invalid exists query filter");
      if (op === "prefix" && typeof condition.prefix !== "string") throw new TypeError("Invalid prefix query filter");
      if (["gt", "gte", "lt", "lte"].includes(op) && !["string", "number"].includes(typeof condition[op])) throw new TypeError("Invalid ordered query filter");
    }
    const state = this.reconstruct();
    const db = this.project(state);
    if (!db) {
      const rows = state.records.filter(record => (type === undefined || record.type === type) && matchesPayload(record.payload, filters)).slice(0, limit);
      return { rows, heads: state.heads, diagnostics: state.diagnostics };
    }
    const sql = sqlPayloadFilter(filters, type);
    const ordinals = db.prepare(`SELECT ordinal FROM records${sql.where} ORDER BY ordinal`).all(...sql.params);
    const rows = ordinals
      .map(row => state.records[Number(row.ordinal)])
      .filter(record => matchesPayload(record.payload, filters))
      .slice(0, limit);
    return { rows, heads: state.heads, diagnostics: state.diagnostics };
  }

  status() {
    const state = this.reconstruct();
    this.project(state);
    const incompleteRecords = state.diagnostics.filter(item => item.code === "missing-parent" || item.code === "cycle").length;
    const invalidRecords = state.diagnostics.filter(item => !["missing-parent", "cycle"].includes(item.code)).length;
    const inspectableConflicts = state.diagnostics.filter(item => item.code === "duplicate-id-conflict").length;
    return {
      records: state.records.length + invalidRecords - inspectableConflicts,
      validRecords: state.records.length - incompleteRecords - inspectableConflicts,
      invalidRecords,
      incompleteRecords,
      heads: state.heads,
      shards: state.shard_count,
      diagnostics: state.diagnostics,
    };
  }

  close() {
    if (this.projection) {
      this.projection.db.close();
      this.projection = null;
    }
  }
}

module.exports = { Ledger, canonicalJSON, sha256 };
