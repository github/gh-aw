// @ts-check
/// <reference types="@actions/github-script" />

const { ERR_API, ERR_VALIDATION } = require("./error_codes.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { GITHUB_API_VERSION } = require("./constants.cjs");
const { withRetry, RATE_LIMIT_RETRY_CONFIG } = require("./error_recovery.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { convertXmlTags, removeXmlComments, sanitizeUrlProtocols, sanitizeUrlDomains, buildAllowedDomains } = require("./sanitize_content_core.cjs");
const { sanitizeTitle } = require("./sanitize_title.cjs");
const path = require("node:path");
const { isUtf8 } = require("node:buffer");
const { readGuardedFile, resolveIssueArtifactPath } = require("./symlink_guard.cjs");
const DEFAULT_MAX_ISSUE_ARTIFACT_SIZE = 1024 * 1024;

/**
 * @typedef {import("./types/safe-outputs").IssueArtifact} IssueArtifact
 * @typedef {import("./types/safe-outputs").IssueArtifactInput} IssueArtifactInput
 * @typedef {import("./types/safe-outputs").CreatedIssueArtifact} CreatedIssueArtifact
 */

/**
 * Validate all artifacts before modifying their issue.
 * @param {unknown} value
 * @param {number} [maxArtifacts=10] - Per-issue limit; non-positive values disable artifacts.
 * @param {number} [maxSizeBytes=1048576] - Maximum UTF-8 bytes per artifact body.
 * @param {string} [toolName="create_issue"]
 * @returns {IssueArtifact[]}
 */
function normalizeIssueArtifacts(value, maxArtifacts = 10, maxSizeBytes = DEFAULT_MAX_ISSUE_ARTIFACT_SIZE, toolName = "create_issue") {
  return normalizeIssueArtifactInputs(value, maxArtifacts, maxSizeBytes, toolName).map((artifact, index) => {
    if (artifact.path !== undefined) throw new Error(`${ERR_VALIDATION}: ${toolName} artifacts[${index}].path must be read by the safeoutputs MCP backend before processing`);
    return { type: artifact.type ?? "generic", name: artifact.name, body: artifact.body };
  });
}

/**
 * @param {unknown} value
 * @param {number} maxArtifacts
 * @param {number} maxSizeBytes
 * @param {string} toolName
 * @returns {IssueArtifactInput[]}
 */
function normalizeIssueArtifactInputs(value, maxArtifacts, maxSizeBytes, toolName) {
  if (!Number.isSafeInteger(maxArtifacts)) throw new Error(`${ERR_VALIDATION}: max-issue-artifacts must be an integer`);
  if (!Number.isSafeInteger(maxSizeBytes) || maxSizeBytes <= 0) throw new Error(`${ERR_VALIDATION}: max-issue-artifact-size must be a positive integer in bytes`);
  if (value === undefined) return [];
  if (!Array.isArray(value)) throw new Error(`${ERR_VALIDATION}: ${toolName} 'artifacts' must be an array`);
  if (value.length > Math.max(0, maxArtifacts)) {
    throw new Error(maxArtifacts <= 0 ? `${ERR_VALIDATION}: Issue artifacts are disabled by max-issue-artifacts; omit artifacts` : `${ERR_VALIDATION}: ${toolName} artifacts exceeds max-issue-artifacts limit of ${maxArtifacts} per issue`);
  }
  /** @type {unknown[]} */
  const artifacts = value;
  return artifacts.map((artifact, index) => {
    const field = `${toolName} artifacts[${index}]`;
    if (!artifact || typeof artifact !== "object" || Array.isArray(artifact)) {
      throw new Error(`${ERR_VALIDATION}: ${field} must be an object`);
    }
    if (Object.keys(artifact).some(key => !["type", "name", "body", "path"].includes(key))) {
      throw new Error(`${ERR_VALIDATION}: ${field} accepts only type, name, body, and path`);
    }
    const type = !("type" in artifact) || artifact.type === undefined ? "generic" : artifact.type;
    if (type !== "generic" && type !== "link" && type !== "plan" && type !== "embedded_html") {
      throw new Error(`${ERR_VALIDATION}: ${field}.type must be generic, link, plan, or embedded_html`);
    }
    if (!("name" in artifact) || typeof artifact.name !== "string" || !artifact.name.trim()) {
      throw new Error(`${ERR_VALIDATION}: ${field}.name must be a non-empty string`);
    }
    const hasBody = "body" in artifact;
    const hasPath = "path" in artifact;
    if (hasBody === hasPath) {
      throw new Error(`${ERR_VALIDATION}: ${field} requires exactly one of body or path`);
    }
    if ("path" in artifact) {
      if (typeof artifact.path !== "string" || !artifact.path.trim() || !path.isAbsolute(artifact.path)) {
        throw new Error(`${ERR_VALIDATION}: ${field}.path must be a non-empty absolute file path`);
      }
      return { type, name: artifact.name.trim(), path: artifact.path };
    }
    if (!("body" in artifact) || typeof artifact.body !== "string" || !artifact.body.trim()) {
      throw new Error(`${ERR_VALIDATION}: ${field}.body must be a non-empty string`);
    }
    validateIssueArtifactBodySize(artifact.body, maxSizeBytes, `${field}.body`);
    return { type, name: artifact.name.trim(), body: artifact.body };
  });
}

/**
 * Read path-based requests in the unprivileged MCP backend; persist content, not paths.
 * @param {unknown} value
 * @param {number} [maxArtifacts=10]
 * @param {number} [maxSizeBytes=1048576]
 * @param {string} [toolName="create_issue"]
 * @returns {IssueArtifact[]}
 */
function resolveIssueArtifactPaths(value, maxArtifacts = 10, maxSizeBytes = DEFAULT_MAX_ISSUE_ARTIFACT_SIZE, toolName = "create_issue") {
  const artifacts = normalizeIssueArtifactInputs(value, maxArtifacts, maxSizeBytes, toolName);
  return artifacts.map((artifact, index) => {
    if (artifact.path === undefined) return { type: artifact.type ?? "generic", name: artifact.name, body: artifact.body };
    let content;
    try {
      content = readGuardedFile(resolveIssueArtifactPath(artifact.path), maxSizeBytes);
    } catch (error) {
      throw new Error(`${ERR_VALIDATION}: ${toolName} artifacts[${index}].path could not be read: ${getErrorMessage(error)}`, { cause: error });
    }
    if (!isUtf8(content) || content.includes(0)) throw new Error(`${ERR_VALIDATION}: ${toolName} artifacts[${index}].path must contain UTF-8 text without NUL bytes`);
    const body = content.toString("utf8");
    if (!body.trim()) throw new Error(`${ERR_VALIDATION}: ${toolName} artifacts[${index}].path must contain non-empty text`);
    return { type: artifact.type ?? "generic", name: artifact.name, body };
  });
}

/**
 * @param {unknown} value
 * @returns {CreatedIssueArtifact}
 */
function normalizeCreatedIssueArtifact(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`${ERR_API}: Invalid issue artifact API response; expected an object`);
  }
  const number = "number" in value ? value.number : undefined;
  const url = "html_url" in value ? value.html_url : undefined;
  if (typeof number !== "number" || !Number.isSafeInteger(number) || number <= 0 || (url !== undefined && typeof url !== "string")) {
    throw new Error(`${ERR_API}: Invalid issue artifact API response; expected a positive integer number and an optional string html_url`);
  }
  return { number, url };
}

/**
 * @param {string} body
 * @param {number} maxSizeBytes
 * @param {string} field
 */
function validateIssueArtifactBodySize(body, maxSizeBytes, field) {
  const size = Buffer.byteLength(body, "utf8");
  if (size > maxSizeBytes) {
    throw new Error(`${ERR_VALIDATION}: ${field} is ${size} UTF-8 bytes, exceeding max-issue-artifact-size limit of ${maxSizeBytes} bytes`);
  }
}

/**
 * Preserve supported HTML tags, without treating rendered HTML as inert Markdown code.
 * @param {IssueArtifact[]} artifacts
 * @param {import("./sanitize_content.cjs").SanitizeOptions} options
 * @returns {IssueArtifact[]}
 */
function sanitizeIssueArtifacts(artifacts, options) {
  // Size limits reject content before and after sanitization instead of truncating it.
  const bodyOptions = { ...options, maxLength: Number.MAX_SAFE_INTEGER, maxLines: Number.MAX_SAFE_INTEGER };
  return artifacts.map(artifact => {
    const name = sanitizeTitle(artifact.name);
    let body = sanitizeContent(artifact.body, bodyOptions);
    if (artifact.type === "embedded_html") {
      // Backticks in HTML do not create inert regions in an HTML preview.
      body = convertXmlTags(removeXmlComments(body));
      body = sanitizeUrlDomains(sanitizeUrlProtocols(body), buildAllowedDomains());
    }
    if (!name.trim() || !body.trim()) throw new Error(`${ERR_VALIDATION}: Issue artifact name and body must remain non-empty after sanitization`);
    return { ...artifact, name, body };
  });
}

/**
 * @param {typeof github} githubClient
 * @param {string} owner
 * @param {string} repo
 * @param {number} issueNumber
 * @param {IssueArtifact[]} artifacts
 * @param {number} [maxSizeBytes=1048576]
 * @returns {Promise<CreatedIssueArtifact[]>}
 */
async function createIssueArtifacts(githubClient, owner, repo, issueNumber, artifacts, maxSizeBytes = DEFAULT_MAX_ISSUE_ARTIFACT_SIZE) {
  /** @type {CreatedIssueArtifact[]} */
  const created = [];
  for (const artifact of artifacts) {
    try {
      validateIssueArtifactBodySize(artifact.body, maxSizeBytes, `Artifact "${artifact.name}" body`);
      const { data } = await withRetry(
        () =>
          githubClient.request("POST /repos/{owner}/{repo}/issues/{issue_number}/artifacts", {
            owner,
            repo,
            issue_number: issueNumber,
            ...artifact,
            headers: { "X-GitHub-Api-Version": GITHUB_API_VERSION },
          }),
        RATE_LIMIT_RETRY_CONFIG,
        `create issue artifact on ${owner}/${repo}#${issueNumber}`
      );
      created.push(normalizeCreatedIssueArtifact(data));
    } catch (error) {
      throw new Error(`${ERR_API}: Issue ${owner}/${repo}#${issueNumber} was created, but artifact "${artifact.name}" could not be attached (${created.length}/${artifacts.length} attached): ${getErrorMessage(error)}`, { cause: error });
    }
  }
  return created;
}

/**
 * List matching artifacts and validate the complete batch before any issue mutations.
 * @param {typeof github} githubClient
 * @param {string} owner
 * @param {string} repo
 * @param {number} issueNumber
 * @param {IssueArtifact[]} artifacts
 * @param {number} [maxSizeBytes=1048576]
 * @returns {Promise<Map<string, {number: number, type: string}>>}
 */
async function prepareIssueArtifactUpsert(githubClient, owner, repo, issueNumber, artifacts, maxSizeBytes = DEFAULT_MAX_ISSUE_ARTIFACT_SIZE) {
  /** @type {Map<string, {number: number, type: string}>} */
  const existing = new Map();
  for (let page = 1; ; page++) {
    const { data } = await withRetry(
      () =>
        githubClient.request("GET /repos/{owner}/{repo}/issues/{issue_number}/artifacts", {
          owner,
          repo,
          issue_number: issueNumber,
          per_page: 100,
          page,
          headers: { "X-GitHub-Api-Version": GITHUB_API_VERSION },
        }),
      RATE_LIMIT_RETRY_CONFIG,
      `list issue artifacts on ${owner}/${repo}#${issueNumber}`
    );
    if (!Array.isArray(data)) throw new Error(`${ERR_API}: Invalid issue artifact list response; expected an array`);
    for (const entry of data) {
      const metadata = normalizeCreatedIssueArtifact(entry);
      if (typeof entry.name !== "string" || !["generic", "link", "plan", "embedded_html"].includes(entry.type)) {
        throw new Error(`${ERR_API}: Invalid issue artifact list response; expected a name and supported type`);
      }
      const match = existing.get(entry.name);
      if (!match || metadata.number < match.number) existing.set(entry.name, { number: metadata.number, type: entry.type });
    }
    if (data.length < 100) break;
  }

  const types = new Map([...existing].map(([name, entry]) => [name, entry.type]));
  for (const artifact of artifacts) {
    validateIssueArtifactBodySize(artifact.body, maxSizeBytes, `Artifact "${artifact.name}" body`);
    if (types.has(artifact.name) && types.get(artifact.name) !== artifact.type) {
      throw new Error(`${ERR_VALIDATION}: Artifact "${artifact.name}" already has type ${types.get(artifact.name)}; artifact types cannot be changed`);
    }
    types.set(artifact.name, artifact.type);
  }
  return existing;
}

/**
 * Upsert sanitized artifacts by their exact name. The list API has no Link header,
 * so read pages until a short page is returned.
 * @param {typeof github} githubClient
 * @param {string} owner
 * @param {string} repo
 * @param {number} issueNumber
 * @param {IssueArtifact[]} artifacts
 * @param {number} [maxSizeBytes=1048576]
 * @param {Map<string, {number: number, type: string}>} [preparedExisting]
 * @returns {Promise<CreatedIssueArtifact[]>}
 */
async function upsertIssueArtifacts(githubClient, owner, repo, issueNumber, artifacts, maxSizeBytes = DEFAULT_MAX_ISSUE_ARTIFACT_SIZE, preparedExisting) {
  if (artifacts.length === 0) return [];
  /** @type {CreatedIssueArtifact[]} */
  const attached = [];
  let artifactName = artifacts[0].name;
  try {
    const existing = preparedExisting ?? (await prepareIssueArtifactUpsert(githubClient, owner, repo, issueNumber, artifacts, maxSizeBytes));
    for (const artifact of artifacts) {
      artifactName = artifact.name;
      const match = existing.get(artifact.name);
      const parameters = {
        owner,
        repo,
        issue_number: issueNumber,
        ...(match ? { artifact_number: match.number, name: artifact.name, body: artifact.body } : artifact),
        headers: { "X-GitHub-Api-Version": GITHUB_API_VERSION },
      };
      const { data } = match
        ? await withRetry(() => githubClient.request("PATCH /repos/{owner}/{repo}/issues/{issue_number}/artifacts/{artifact_number}", parameters), RATE_LIMIT_RETRY_CONFIG, `update issue artifact on ${owner}/${repo}#${issueNumber}`)
        : await githubClient.request("POST /repos/{owner}/{repo}/issues/{issue_number}/artifacts", parameters);
      const metadata = normalizeCreatedIssueArtifact(data);
      attached.push(metadata);
      existing.set(artifact.name, { number: metadata.number, type: artifact.type });
    }
  } catch (error) {
    const failure = Object.assign(
      new Error(`${ERR_API}: Issue ${owner}/${repo}#${issueNumber} artifact "${artifactName}" could not be attached or updated (${attached.length}/${artifacts.length} attached): ${getErrorMessage(error)}`, { cause: error }),
      { attached }
    );
    throw failure;
  }
  return attached;
}

module.exports = { normalizeIssueArtifacts, resolveIssueArtifactPaths, sanitizeIssueArtifacts, createIssueArtifacts, prepareIssueArtifactUpsert, upsertIssueArtifacts };
