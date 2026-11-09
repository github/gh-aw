// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { createHash } = require("node:crypto");
const { isDeepStrictEqual } = require("node:util");
const { parseLockFileHeaders } = require("./lock_file_headers.cjs");

/** @typedef {{sha256: string, kind: string, value_json: string, error?: string}} MetadataRecord */
/** @typedef {{version: number, records: MetadataRecord[]}} MetadataCatalog */
/** @type {Map<string, MetadataRecord>} */
let records = new Map();
let initialized = false;
const FRONTMATTER = /^---[ \t]*\r?\n(?:([\s\S]*?)\r?\n)?---[ \t]*\r?(?=\n|$)/;
const METADATA_BLOCKS = /(^|^##[ \t]+(?:agent|skill):[^\n]*\r?\n(?:[ \t]*\r?\n)*)---[ \t]*\r?\n(?:([\s\S]*?)\r?\n)?---[ \t]*\r?(?=\n|$)/gm;

class CompilerMetadataUnavailableError extends Error {
  constructor() {
    super("Markdown metadata is absent from the compiler manifest; recompile this workflow");
  }
}

/**
 * Install compiler-owned records. Source digests are exact provenance keys;
 * changed or newly-created YAML must be recompiled.
 * @param {unknown} metadata
 * @param {boolean} [merge]
 */
function installMarkdownMetadata(metadata, merge = false) {
  if (!metadata || typeof metadata !== "object" || !("version" in metadata) || metadata.version !== 1 || !("records" in metadata) || !Array.isArray(metadata.records)) throw new Error("Invalid compiler Markdown metadata manifest");
  const next = new Map();
  for (const record of metadata.records) {
    const key = `${record?.kind}:${record?.sha256}`;
    if (!record || !/^[a-f0-9]{64}$/.test(record.sha256) || !["artifact", "model-pins"].includes(record.kind) || typeof record.value_json !== "string" || (record.error !== undefined && typeof record.error !== "string") || next.has(key))
      throw new Error("Invalid or duplicate compiler Markdown metadata record");
    parseMarkdownJSON(record.value_json);
    next.set(key, record);
  }
  const result = merge ? new Map(records) : new Map();
  for (const [key, record] of next) {
    const previous = result.get(key);
    if (previous && (previous.error !== record.error || !isDeepStrictEqual(parseMarkdownJSON(previous.value_json), parseMarkdownJSON(record.value_json)))) throw new Error("Conflicting compiler Markdown metadata for the same author source");
    result.set(key, record);
  }
  records = result;
  initialized = true;
}

function currentMarkdownMetadata() {
  return { version: 1, records: [...records.values()] };
}

/**
 * JSON-only validation, including escaped-equivalent duplicate keys. Metadata
 * numbers are ordinary finite JSON numbers, not the queue codec's integers.
 * @param {string} source
 */
function parseMarkdownJSON(source) {
  if (Buffer.byteLength(source, "utf8") > 4 * 1024 * 1024) throw new Error("Markdown metadata exceeds the bounded input limit");
  const value = JSON.parse(source);
  /** @type {Array<{keys: Set<string>|null, key: boolean}>} */
  const stack = [];
  let nodes = 0;
  for (const token of source.matchAll(/"(?:\\.|[^"\\])*"|[{}\[\]:,]/g)) {
    if (++nodes > 10000) throw new Error("Markdown JSON metadata exceeds structural limits");
    const text = token[0];
    const frame = stack.at(-1);
    if (text === "{" || text === "[") {
      stack.push({ keys: text === "{" ? new Set() : null, key: text === "{" });
      if (stack.length > 100) throw new Error("Markdown JSON metadata exceeds structural limits");
    } else if (text === "}" || text === "]") stack.pop();
    else if (text === "," && frame?.keys) frame.key = true;
    else if (text.startsWith('"') && frame?.keys && frame.key) {
      const key = JSON.parse(text);
      if (frame.keys.has(key)) throw new Error("Duplicate Markdown JSON metadata key");
      frame.keys.add(key);
      frame.key = false;
    }
  }
  const checkNumbers = (/** @type {any} */ item) => {
    if (typeof item === "number" && !Number.isFinite(item)) throw new Error("Markdown JSON metadata numbers must be finite");
    if (item && typeof item === "object") for (const child of Object.values(item)) checkNumbers(child);
  };
  checkNumbers(value);
  return value;
}

/** @param {string} file @param {string} workspace */
function readLocalMarkdownMetadata(file, workspace) {
  const root = path.resolve(workspace);
  const absolute = path.resolve(file);
  const relative = path.relative(root, absolute);
  if (relative.startsWith("..") || path.isAbsolute(relative)) throw new Error("Invalid compiler Markdown metadata path");
  const info = fs.lstatSync(absolute);
  const realRelative = path.relative(fs.realpathSync(root), fs.realpathSync(absolute));
  if (!info.isFile() || info.size > 4 * 1024 * 1024 || realRelative.startsWith("..") || path.isAbsolute(realRelative)) throw new Error("Executing workflow lock manifest must be a bounded regular workspace file");
  const content = new TextDecoder("utf8", { fatal: true }).decode(fs.readFileSync(absolute));
  const { manifest } = parseLockFileHeaders(content.split(/\r?\n/, 3));
  return manifest?.markdown_metadata;
}

/** @param {any} githubClient @param {{owner: string, repo: string, path: string, ref: string}} identity */
async function readRemoteMarkdownMetadata(githubClient, identity) {
  if (!githubClient?.rest?.repos?.getContent) throw new Error("Compiler Markdown metadata requires the source repository's GitHub client");
  const { data } = await githubClient.rest.repos.getContent(identity);
  if (!data || data.type !== "file" || data.path !== identity.path || data.encoding !== "base64" || typeof data.content !== "string") throw new Error("Executing workflow lock manifest is not a regular source file");
  if (data.content.length > Math.ceil((4 * 1024 * 1024) / 3) * 4 + 65536) throw new Error("Executing workflow lock manifest exceeds the bounded input limit");
  const encoded = data.content.replace(/[\r\n]/g, "");
  const bytes = Buffer.from(encoded, "base64");
  if (bytes.length > 4 * 1024 * 1024 || bytes.toString("base64") !== encoded) throw new Error("Executing workflow lock manifest requires bounded canonical base64");
  const content = new TextDecoder("utf8", { fatal: true }).decode(bytes);
  const { manifest } = parseLockFileHeaders(content.split(/\r?\n/, 3));
  return manifest?.markdown_metadata;
}

/** @param {string} workflowId @param {string} workspace @param {{repository?: string, githubClient?: any, revision?: string}} [options] */
async function loadMarkdownMetadata(workflowId, workspace, { repository, githubClient, revision } = {}) {
  const match = /^([^/]+)\/([^/]+)\/(.+\.lock\.yml)@(.+)$/.exec(workflowId);
  if (!match) throw new Error("Compiler Markdown metadata requires the executing workflow identity");
  const workspaceRoot = path.resolve(workspace);
  const file = path.resolve(workspaceRoot, match[3]);
  const relative = path.relative(workspaceRoot, file);
  if (relative.startsWith("..") || path.isAbsolute(relative)) throw new Error("Invalid compiler Markdown metadata path");
  if (revision && (!/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(revision) || /^0+$/.test(revision))) throw new Error("Executing workflow metadata requires an immutable workflow revision");
  const remote = revision || (repository && repository !== `${match[1]}/${match[2]}`);
  const metadata = remote ? await readRemoteMarkdownMetadata(githubClient, { owner: match[1], repo: match[2], path: match[3], ref: revision || match[4] }) : readLocalMarkdownMetadata(file, workspaceRoot);
  if (!metadata) throw new Error("Executing lock manifest lacks compiler Markdown metadata; recompile this workflow");
  installMarkdownMetadata(metadata);
  ensureCheckoutMarkdownMetadata(checkoutArtifactDocuments(workspaceRoot), workspaceRoot);
  return currentMarkdownMetadata();
}

/** @param {string} workspace */
function checkoutArtifactDocuments(workspace) {
  const documents = [];
  const realWorkspace = fs.realpathSync(workspace);
  for (const directory of [
    ".github/agents",
    ".github/skills",
    ".claude/agents",
    ".claude/skills",
    ".codex/agents",
    ".codex/skills",
    ".gemini/agents",
    ".gemini/skills",
    ".pi/agents",
    ".pi/skills",
    ".agents/agents",
    ".agents/skills",
    "skills",
  ]) {
    const root = path.join(workspace, directory);
    if (!fs.existsSync(root)) continue;
    const relative = path.relative(realWorkspace, fs.realpathSync(root));
    if (relative.startsWith("..") || path.isAbsolute(relative)) continue;
    const walk = (/** @type {string} */ dir) => {
      if (fs.lstatSync(dir).isSymbolicLink()) return;
      for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
        const file = path.join(dir, entry.name);
        if (entry.isDirectory() && !["node_modules", ".git"].includes(entry.name)) walk(file);
        else if (entry.isFile() && entry.name.endsWith(".md")) documents.push(fs.readFileSync(file, "utf8"));
      }
    };
    walk(root);
  }
  return documents;
}

/** @param {string[]} documents */
function missingMarkdownMetadata(documents) {
  restoreCachedMetadata();
  const missing = new Set();
  for (const content of documents) {
    for (const match of content.matchAll(METADATA_BLOCKS)) {
      const source = match[2];
      if (!source?.trim()) continue;
      try {
        readDocumentMetadata(source, { kind: "artifact" });
      } catch (error) {
        if (!(error instanceof CompilerMetadataUnavailableError)) throw error;
        missing.add(createHash("sha256").update(source.replaceAll("\r\n", "\n")).digest("hex"));
      }
    }
  }
  return missing;
}

/** @param {unknown} metadata @param {Set<string>} missing */
function mergeMatchingMarkdownMetadata(metadata, missing) {
  if (!metadata) return;
  if (typeof metadata !== "object" || !("version" in metadata) || metadata.version !== 1 || !("records" in metadata) || !Array.isArray(metadata.records)) throw new Error("Invalid compiler Markdown metadata manifest");
  installMarkdownMetadata(
    {
      version: metadata.version,
      records: metadata.records.filter(record => {
        if (!record || typeof record.kind !== "string" || typeof record.sha256 !== "string") throw new Error("Invalid compiler Markdown metadata record");
        return record.kind === "artifact" && missing.has(record.sha256);
      }),
    },
    true
  );
}

/** @param {string[]} documents @param {string} workspace */
function ensureCheckoutMarkdownMetadata(documents, workspace) {
  let missing = missingMarkdownMetadata(documents);
  const directory = path.join(workspace, ".github/workflows");
  if (!missing.size || !fs.existsSync(directory)) return;
  for (const entry of fs.readdirSync(directory, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
    if (!entry.isFile() || !entry.name.endsWith(".lock.yml")) continue;
    mergeMatchingMarkdownMetadata(readLocalMarkdownMetadata(path.join(directory, entry.name), workspace), missing);
    missing = missingMarkdownMetadata(documents);
    if (!missing.size) break;
  }
}

/** @param {string[]} documents @param {any} githubClient @param {{repository: string, ref: string}} source */
async function ensureRemoteMarkdownMetadata(documents, githubClient, { repository, ref }) {
  let missing = missingMarkdownMetadata(documents);
  if (!missing.size) return;
  const [owner, repo, extra] = repository.split("/");
  if (!owner || !repo || extra || !githubClient?.rest?.repos?.getContent) throw new Error("Skill metadata requires the source repository's scoped GitHub client");
  if (!/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(ref)) {
    const { data } = await githubClient.rest.repos.getCommit({ owner, repo, ref: ref || "HEAD" });
    ref = data?.sha;
  }
  if (typeof ref !== "string" || !/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(ref) || /^0+$/.test(ref)) throw new Error("Skill metadata requires an immutable source revision");
  const { data } = await githubClient.rest.repos.getContent({ owner, repo, path: ".github/workflows", ref });
  if (!Array.isArray(data)) throw new Error("Skill source has no compiler workflow manifests");
  for (const entry of data.sort((a, b) => String(a.path).localeCompare(String(b.path)))) {
    if (entry?.type !== "file" || !/^\.github\/workflows\/[^/]+\.lock\.yml$/.test(entry.path)) continue;
    mergeMatchingMarkdownMetadata(await readRemoteMarkdownMetadata(githubClient, { owner, repo, path: entry.path, ref }), missing);
    missing = missingMarkdownMetadata(documents);
    if (!missing.size) return;
  }
  throw new CompilerMetadataUnavailableError();
}

function persistMarkdownMetadata() {
  const infoPath = path.join(process.env.GH_AW_TMP_DIR || require("./constants.cjs").TMP_GH_AW_PATH, "aw_info.json");
  if (!fs.existsSync(infoPath)) throw new Error("Compiler Markdown metadata cache is unavailable; activation must generate aw_info.json before installing skills");
  const info = JSON.parse(fs.readFileSync(infoPath, "utf8"));
  if (!info || typeof info !== "object" || Array.isArray(info)) throw new Error("Invalid compiler Markdown metadata cache");
  info.markdown_metadata = currentMarkdownMetadata();
  fs.writeFileSync(infoPath, JSON.stringify(info));
}

function restoreCachedMetadata() {
  if (initialized) return;
  const infoPath = path.join(process.env.GH_AW_TMP_DIR || require("./constants.cjs").TMP_GH_AW_PATH, "aw_info.json");
  if (fs.existsSync(infoPath)) {
    const info = JSON.parse(fs.readFileSync(infoPath, "utf8"));
    if (info.markdown_metadata) installMarkdownMetadata(info.markdown_metadata);
  }
}

/** @param {string} source @param {{kind?: string}} [options] */
function readDocumentMetadata(source, { kind = "document" } = {}) {
  if (!source?.trim()) return { value: {} };
  // JSON is the sole runtime representation. Literal prompt markers contain NUL
  // and are escaped for JSON decoding, never interpreted as syntax.
  try {
    return { value: parseMarkdownJSON(source.replaceAll("\x00", "\\u0000")) };
  } catch {
    restoreCachedMetadata();
    const digest = createHash("sha256").update(source.replaceAll("\r\n", "\n")).digest("hex");
    const record = kind === "artifact" ? records.get(`artifact:${digest}`) : records.get(`model-pins:${digest}`) || records.get(`artifact:${digest}`);
    if (!record) throw new CompilerMetadataUnavailableError();
    if (record.error) throw new Error(record.error);
    return { value: parseMarkdownJSON(record.value_json) };
  }
}

/** @param {any} value @param {(value: string) => string} transform */
function transformValues(value, transform) {
  if (typeof value === "string") return transform(value);
  if (Array.isArray(value)) return value.map(item => transformValues(item, transform));
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => {
        if (/\x00VALUE_[0-9a-f-]+_\d+\x00/.test(key)) throw new Error("Evaluated inline metadata must be a scalar value");
        return [key, transformValues(item, transform)];
      })
    );
  }
  return value;
}

/** @param {string} content @param {(value: any) => any} transform */
function editMarkdownMetadata(content, transform) {
  const match = FRONTMATTER.exec(content);
  if (!match) return content;
  const value = readDocumentMetadata(match[1], { kind: "artifact" }).value ?? {};
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("agent frontmatter must be a mapping");
  return `---\n${JSON.stringify(transform(value))}\n---${content.slice(match[0].length)}`;
}

/**
 * Apply author text operations to decoded JSON scalar values independently of
 * the document structure. Engine frontmatter is JSON (a YAML-compatible subset).
 * @param {string} content @param {(text: string) => string} transform
 */
function transformMarkdownContent(content, transform) {
  const blocks = [...content.matchAll(METADATA_BLOCKS)];
  if (!blocks.length) return transform(content);
  const prefix = `\x00METADATA_${require("node:crypto").randomUUID()}_`;
  const metadata = [];
  let cursor = 0;
  let result = "";
  for (const block of blocks) {
    const start = /** @type {number} */ block.index;
    result += content.slice(cursor, start) + block[1];
    const value = readDocumentMetadata(block[2], { kind: "artifact" }).value ?? {};
    if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("agent frontmatter must be a mapping");
    metadata.push(value);
    result += `${prefix}${metadata.length - 1}\x00`;
    cursor = start + block[0].length;
  }
  result = transform(result + content.slice(cursor));
  return result.replace(new RegExp(`${prefix}(\\d+)\x00`, "g"), (_, index) => `---\n${JSON.stringify(transformValues(metadata[Number(index)], transform))}\n---`);
}

/** @param {string} operation @param {string} content @param {{value?: string, replacements?: Record<string, string>}} [options] */
function readMarkdownMetadata(operation, content, options = {}) {
  if (operation === "document") return readDocumentMetadata(content);
  const match = FRONTMATTER.exec(content);
  const value = match ? (readDocumentMetadata(match[1], { kind: "artifact" }).value ?? {}) : {};
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("agent frontmatter must be a mapping");
  if (operation === "agent") return { value, content };
  if (operation === "ensure-name") {
    if (Object.hasOwn(value, "name")) return { value, content };
    const named = { name: options.value, ...value };
    return { value: named, content: match ? editMarkdownMetadata(content, () => named) : `---\n${JSON.stringify(named)}\n---\n${content}` };
  }
  if (operation === "restore") {
    const replacements = options.replacements || {};
    const restore = (/** @type {string} */ text) => text.replace(/\x00VALUE_[0-9a-f-]+_\d+\x00/g, marker => replacements[marker] ?? marker);
    return { value, content: editMarkdownMetadata(content, metadata => transformValues(metadata, restore)) };
  }
  throw new Error("Unsupported Markdown metadata operation");
}

module.exports = {
  readMarkdownMetadata,
  readDocumentMetadata,
  installMarkdownMetadata,
  loadMarkdownMetadata,
  readLocalMarkdownMetadata,
  currentMarkdownMetadata,
  ensureCheckoutMarkdownMetadata,
  ensureRemoteMarkdownMetadata,
  missingMarkdownMetadata,
  persistMarkdownMetadata,
  editMarkdownMetadata,
  transformMarkdownContent,
  transformValues,
  parseMarkdownJSON,
  CompilerMetadataUnavailableError,
};
