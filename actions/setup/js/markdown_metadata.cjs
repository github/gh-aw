// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { createHash } = require("node:crypto");
const { isDeepStrictEqual } = require("node:util");
const { parseLockFileHeaders, parseMarkdownJSON } = require("./lock_file_headers.cjs");

/** @typedef {{sha256: string, kind: string, value: any, error?: string}} MetadataRecord */
/** @typedef {{version: number, records: MetadataRecord[]}} MetadataCatalog */
/** @type {Map<string, MetadataRecord>} */
let records = new Map();
let initialized = false;
/** @type {Map<string, MetadataRecord>} */
let generatedRecords = new Map();

/** @param {string} source */
function metadataDigest(source) {
  return createHash("sha256").update(source.replaceAll("\r\n", "\n")).digest("hex");
}

/**
 * Find only document and inline-artifact headers, preserving original offsets.
 * Delimiters follow the same BOM/whitespace rules as the engine body reader.
 * @param {string} content
 * @param {boolean} [documentOnly]
 */
function markdownMetadataBlocks(content, documentOnly = false) {
  const lines = [...content.matchAll(/[^\n]*(?:\n|$)/g)].filter(line => line[0]);
  const blocks = [];
  const lineText = (/** @type {number} */ index) => lines[index][0].replace(/\r?\n$/, "");
  for (let index = 0; index < lines.length; index++) {
    let opening = index;
    if (index !== 0 || /^##[ \t]+(?:agent|skill):/.test(lineText(index))) {
      if (documentOnly || !/^##[ \t]+(?:agent|skill):/.test(lineText(index))) continue;
      opening++;
      while (opening < lines.length && !lineText(opening).trim()) opening++;
    }
    if (
      opening >= lines.length ||
      lineText(opening)
        .replace(/^\uFEFF/, "")
        .trim() !== "---"
    )
      continue;
    let closing = opening + 1;
    while (closing < lines.length && lineText(closing).trim() !== "---") closing++;
    if (closing === lines.length) throw new Error("unterminated frontmatter");
    const start = /** @type {number} */ lines[opening].index;
    const sourceStart = start + lines[opening][0].length;
    const sourceEnd = /** @type {number} */ lines[closing].index;
    const source = content.slice(sourceStart, sourceEnd).replace(/\r?\n$/, "");
    const end = sourceEnd + lineText(closing).length;
    blocks.push({ start, end, source, skill: /^##[ \t]+skill:/.test(lineText(index)) });
    index = closing;
    if (documentOnly) break;
  }
  return blocks;
}

class CompilerMetadataUnavailableError extends Error {
  constructor() {
    super("Markdown metadata is absent from the compiler manifest; recompile this workflow");
  }
}

/**
 * Install compiler-owned records. Source digests are exact provenance keys;
 * changed or newly-created metadata, including JSON, must be recompiled.
 * @param {unknown} metadata
 * @param {boolean} [merge]
 */
function installMarkdownMetadata(metadata, merge = false) {
  if (!metadata || typeof metadata !== "object" || !("version" in metadata) || metadata.version !== 2 || !("records" in metadata) || !Array.isArray(metadata.records)) throw new Error("Invalid compiler Markdown metadata manifest");
  const next = new Map();
  for (const record of metadata.records) {
    const key = `${record?.kind}:${record?.sha256}`;
    if (
      !record ||
      !/^[a-f0-9]{64}$/.test(record.sha256) ||
      !["artifact", "model-pins"].includes(record.kind) ||
      !Object.hasOwn(record, "value") ||
      Object.hasOwn(record, "value_json") ||
      Object.hasOwn(record, "references") ||
      (record.error !== undefined && typeof record.error !== "string") ||
      next.has(key)
    )
      throw new Error("Invalid or duplicate compiler Markdown metadata record");
    next.set(key, { ...record, value: parseMarkdownJSON(JSON.stringify(record.value)) });
  }
  const result = merge ? new Map(records) : new Map();
  for (const [key, record] of next) {
    const previous = result.get(key);
    if (previous && (previous.error !== record.error || !isDeepStrictEqual(previous.value, record.value))) throw new Error("Conflicting compiler Markdown metadata for the same author source");
    result.set(key, record);
  }
  records = result;
  generatedRecords = new Map();
  // Engine copies use canonical JSON, not the original author YAML. Only
  // compiler-approved artifact values can authorize that representation.
  for (const record of records.values()) {
    if (record.kind === "artifact" && !record.error) {
      const source = JSON.stringify(record.value);
      generatedRecords.set(`artifact:${metadataDigest(source)}`, { ...record, sha256: metadataDigest(source) });
    }
  }
  initialized = true;
}

function currentMarkdownMetadata() {
  return { version: 2, records: [...records.values()] };
}

/** @param {MetadataCatalog} catalog */
function metadataManifestFields(catalog) {
  /** @type {Record<string, any>} */
  const agent_metadata = {};
  /** @type {Record<string, any>} */
  const model_pins = {};
  /** @type {Record<string, any>} */
  const metadata_refs = {};
  /** @type {Record<string, string>} */
  const metadata_errors = {};
  for (const record of catalog.records) {
    const key = `${record.kind}:${record.sha256}`;
    (record.kind === "artifact" ? agent_metadata : model_pins)[record.sha256] = record.value;
    if ("references" in record) metadata_refs[key] = record.references;
    if (record.error !== undefined) metadata_errors[key] = record.error;
  }
  return {
    ...(Object.keys(agent_metadata).length ? { agent_metadata } : {}),
    model_pins,
    ...(Object.keys(metadata_refs).length ? { metadata_refs } : {}),
    ...(Object.keys(metadata_errors).length ? { metadata_errors } : {}),
  };
}

/** @param {Record<string, any>} manifest */
function manifestMetadataCatalog(manifest) {
  const groups = [
    ["artifact", manifest.agent_metadata],
    ["model-pins", manifest.model_pins],
  ];
  const references = manifest.metadata_refs ?? {};
  const errors = manifest.metadata_errors ?? {};
  if (![references, errors].every(value => value && typeof value === "object" && !Array.isArray(value))) throw new Error("Invalid compiler metadata manifest");
  const keys = new Set();
  /** @type {any[]} */
  const catalogRecords = [];
  for (const [kind, group] of groups) {
    if (group === undefined) continue;
    if (!group || typeof group !== "object" || Array.isArray(group)) throw new Error("Invalid compiler metadata manifest");
    for (const [sha256, value] of Object.entries(group)) {
      const key = `${kind}:${sha256}`;
      keys.add(key);
      catalogRecords.push({ kind, sha256, value, ...(Object.hasOwn(references, key) ? { references: references[key] } : {}), ...(Object.hasOwn(errors, key) ? { error: errors[key] } : {}) });
    }
  }
  if ([...Object.keys(references), ...Object.keys(errors)].some(key => !keys.has(key))) throw new Error("Compiler metadata reference or error has no source");
  return { version: 2, records: catalogRecords };
}

/** @param {string} pointer */
function metadataPointer(pointer) {
  if (typeof pointer !== "string" || (pointer !== "" && !pointer.startsWith("/")) || /~(?![01])/u.test(pointer)) throw new Error("Invalid Markdown metadata reference");
  return pointer === ""
    ? []
    : pointer
        .slice(1)
        .split("/")
        .map(part => part.replaceAll("~1", "/").replaceAll("~0", "~"));
}

/** @param {any} root @param {string[]} parts */
function referencedMetadataValue(root, parts) {
  let value = root;
  for (const part of parts) {
    if (!value || typeof value !== "object" || !Object.hasOwn(value, part) || (Array.isArray(value) && !/^(?:0|[1-9]\d*)$/.test(part))) throw new Error("Missing Markdown metadata reference");
    value = value[part];
  }
  return value;
}

/** @param {{metadata?: Record<string, unknown>, manifest?: Record<string, unknown>}} headers */
function resolveMarkdownMetadata(headers) {
  const manifest = headers.manifest;
  if (!manifest || (!Object.hasOwn(manifest, "agent_metadata") && !Object.hasOwn(manifest, "model_pins"))) return undefined;
  const catalog = manifestMetadataCatalog(manifest);
  if (!catalog || typeof catalog !== "object" || !("version" in catalog) || catalog.version !== 2 || !("records" in catalog) || !Array.isArray(catalog.records)) throw new Error("Invalid compiler Markdown metadata manifest");
  return {
    version: 2,
    records: catalog.records.map(record => {
      if (!record || typeof record !== "object" || Array.isArray(record) || !Object.hasOwn(record, "value")) throw new Error("Invalid compiler Markdown metadata record");
      const { references, ...resolved } = record;
      resolved.value = structuredClone(record.value);
      if (references === undefined) return resolved;
      if (!references || typeof references !== "object" || Array.isArray(references)) throw new Error("Invalid Markdown metadata references");
      const destinations = Object.keys(references);
      for (const destination of destinations) {
        const parts = metadataPointer(destination);
        const source = metadataPointer(references[destination]);
        if (source.length < 2 || !["metadata", "manifest"].includes(source[0]) || (source[0] === "manifest" && ["agent_metadata", "model_pins", "metadata_refs", "metadata_errors", "markdown_metadata"].includes(source[1])))
          throw new Error("Invalid Markdown metadata reference source");
        if (destinations.some(other => other !== destination && (destination === "" || other.startsWith(destination + "/")))) throw new Error("Overlapping Markdown metadata references");
        const value = structuredClone(referencedMetadataValue(headers, source));
        if (!parts.length) {
          if (resolved.value !== null) throw new Error("Markdown metadata reference duplicates an inline value");
          resolved.value = value;
          continue;
        }
        const key = parts.pop();
        const parent = referencedMetadataValue(resolved.value, parts);
        if (!parent || typeof parent !== "object" || key === undefined) throw new Error("Invalid Markdown metadata reference destination");
        if (Array.isArray(parent)) {
          if (!/^(?:0|[1-9]\d*)$/.test(key) || !Object.hasOwn(parent, key) || parent[Number(key)] !== null) throw new Error("Invalid Markdown metadata array reference");
        } else if (Object.hasOwn(parent, key)) throw new Error("Markdown metadata reference duplicates an inline value");
        Object.defineProperty(parent, key, { value, enumerable: true, writable: true, configurable: true });
      }
      return resolved;
    }),
  };
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
  return resolveMarkdownMetadata(parseLockFileHeaders(content.split(/\r?\n/, 3)));
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
  return resolveMarkdownMetadata(parseLockFileHeaders(content.split(/\r?\n/, 3)));
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
  for (const directory of [".github/agents", ".claude/agents", ".codex/agents", ".gemini/agents", ".pi/agents", ".agents/agents"]) {
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
    for (const block of markdownMetadataBlocks(content)) {
      if (block.skill) continue;
      const source = block.source;
      if (!source?.trim()) continue;
      try {
        readDocumentMetadata(source, { kind: "artifact" });
      } catch (error) {
        if (!(error instanceof CompilerMetadataUnavailableError)) throw error;
        missing.add(metadataDigest(source));
      }
    }
  }
  return missing;
}

/** @param {unknown} metadata @param {Set<string>} missing */
function mergeMatchingMarkdownMetadata(metadata, missing) {
  if (!metadata) return;
  if (typeof metadata !== "object" || !("version" in metadata) || metadata.version !== 2 || !("records" in metadata) || !Array.isArray(metadata.records)) throw new Error("Invalid compiler Markdown metadata manifest");
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
  for (const key of ["markdown_metadata", "agent_metadata", "model_pins", "metadata_refs", "metadata_errors"]) delete info[key];
  Object.assign(info, metadataManifestFields(currentMarkdownMetadata()));
  fs.writeFileSync(infoPath, JSON.stringify(info));
}

function restoreCachedMetadata() {
  if (initialized) return;
  const infoPath = path.join(process.env.GH_AW_TMP_DIR || require("./constants.cjs").TMP_GH_AW_PATH, "aw_info.json");
  if (fs.existsSync(infoPath)) {
    const info = JSON.parse(fs.readFileSync(infoPath, "utf8"));
    const metadata = resolveMarkdownMetadata({ manifest: info });
    if (metadata) installMarkdownMetadata(metadata);
  }
}

/** @param {string} source @param {{kind?: string}} [options] */
function readDocumentMetadata(source, { kind = "document" } = {}) {
  if (!source?.trim()) return { value: {} };
  restoreCachedMetadata();
  const digest = metadataDigest(source);
  const artifact = records.get(`artifact:${digest}`) || generatedRecords.get(`artifact:${digest}`);
  const record = kind === "artifact" ? artifact : records.get(`model-pins:${digest}`) || artifact;
  if (!record) throw new CompilerMetadataUnavailableError();
  if (record.error) throw new Error(record.error);
  const compiled = structuredClone(record.value);
  let runtime;
  try {
    runtime = parseMarkdownJSON(source.replaceAll("\x00", "\\u0000"));
  } catch {
    return { value: compiled };
  }
  // Model-pin records deliberately project workflow configuration; only
  // artifact metadata is also used directly by runtime engine readers.
  if (record.kind === "artifact") {
    const projected =
      runtime && typeof runtime === "object" && !Array.isArray(runtime) && compiled && typeof compiled === "object" && !Array.isArray(compiled)
        ? Object.fromEntries(
            Object.keys(compiled)
              .filter(key => Object.hasOwn(runtime, key))
              .map(key => [key, runtime[key]])
          )
        : runtime;
    if (!isDeepStrictEqual(projected, compiled)) throw new Error("Markdown JSON metadata does not match its compiler manifest");
  }
  return { value: compiled };
}

/**
 * Authorize only output of a trusted edit whose input was already validated.
 * Persist exact derived digests for downstream engine copies in fresh processes.
 * This is deliberately private, never a runtime-input registration API.
 * @param {any} value
 */
function renderMetadata(value) {
  const source = JSON.stringify(value);
  parseMarkdownJSON(source);
  const digest = metadataDigest(source);
  const record = { kind: "artifact", sha256: digest, value: parseMarkdownJSON(source) };
  generatedRecords.set(`artifact:${digest}`, record);
  records.set(`artifact:${digest}`, record);
  const infoPath = path.join(process.env.GH_AW_TMP_DIR || require("./constants.cjs").TMP_GH_AW_PATH, "aw_info.json");
  if (fs.existsSync(infoPath)) persistMarkdownMetadata();
  return source;
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
  const block = markdownMetadataBlocks(content, true)[0];
  if (!block) return content;
  const value = readDocumentMetadata(block.source, { kind: "artifact" }).value ?? {};
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("agent frontmatter must be a mapping");
  return `---\n${renderMetadata(transform(value))}\n---${content.slice(block.end)}`;
}

/**
 * Apply author text operations to decoded JSON scalar values independently of
 * the document structure. Engine frontmatter is JSON (a YAML-compatible subset).
 * @param {string} content @param {(text: string) => string} transform
 */
function transformMarkdownContent(content, transform) {
  const blocks = markdownMetadataBlocks(content);
  if (!blocks.length) return transform(content);
  const prefix = `\x00METADATA_${require("node:crypto").randomUUID()}_`;
  const metadata = [];
  let cursor = 0;
  let result = "";
  for (const block of blocks) {
    result += content.slice(cursor, block.start);
    if (block.skill) {
      cursor = block.end;
      continue;
    }
    const value = readDocumentMetadata(block.source, { kind: "artifact" }).value ?? {};
    if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("agent frontmatter must be a mapping");
    metadata.push(value);
    result += `${prefix}${metadata.length - 1}\x00`;
    cursor = block.end;
  }
  result = transform(result + content.slice(cursor));
  return result.replace(new RegExp(`${prefix}(\\d+)\x00`, "g"), (_, index) => `---\n${renderMetadata(transformValues(metadata[Number(index)], transform))}\n---`);
}

/** @param {string} operation @param {string} content @param {{value?: string, replacements?: Record<string, string>}} [options] */
function readMarkdownMetadata(operation, content, options = {}) {
  if (operation === "document") return readDocumentMetadata(content);
  const block = markdownMetadataBlocks(content, true)[0];
  const value = block ? (readDocumentMetadata(block.source, { kind: "artifact" }).value ?? {}) : {};
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("agent frontmatter must be a mapping");
  if (operation === "agent") return { value, content };
  if (operation === "ensure-name") {
    if (Object.hasOwn(value, "name")) return { value, content };
    const named = { name: options.value, ...value };
    return { value: named, content: block ? editMarkdownMetadata(content, () => named) : `---\n${renderMetadata(named)}\n---\n${content}` };
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
  markdownMetadataBlocks,
  CompilerMetadataUnavailableError,
  metadataManifestFields,
};
