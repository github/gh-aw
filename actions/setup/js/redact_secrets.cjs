// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Redacts secrets from files in /tmp/gh-aw and ${RUNNER_TEMP}/gh-aw directories before uploading artifacts
 * This script processes all .txt, .json, .log, .md, .mdx, .yml, .jsonl files under /tmp/gh-aw and ${RUNNER_TEMP}/gh-aw
 * and redacts any strings matching the actual secret values provided via environment variables.
 * Successful redaction is silent; failures report safe diagnostics and input byte counts without payloads.
 */
const fs = require("fs");
const path = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");
const { iterateJsonlEvents } = require("./jsonl_helpers.cjs");
const { applyAddMaskRedaction, collectAddMaskedValues, isAddMaskCommandLine, redactArtifactMaskedValues } = require("./add_mask_redaction.cjs");
const { maskedValueMatches, overlappingMatchGroups } = require("./mask_value_matcher.cjs");
const { redactSafeOutputContent } = require("./safe_output_redaction.cjs");
const { redactPiSessionHTML } = require("./pi_session_redaction.cjs");
const { getOTLPSecretValues } = require("./otlp_header_helpers.cjs");
const { redactSensitiveConfig } = require("./safe_outputs_config_redact.cjs");

const AWF_RESOLVED_CONFIG_PATH_PATTERN = /^\/run\/awf-config-audit\/run-[a-zA-Z0-9]{6}\/awf-resolved-config\.json$/;
const AWF_RESOLVED_CONFIG_STAGING_PATH = "/tmp/gh-aw/awf-resolved-config.json";
const MAX_ADDITIONAL_ARTIFACT_BYTES = 16 * 1024 * 1024;
const DETECTION_MASKS_ENV = "GH_AW_DETECTION_MASKED_VALUES";

class UnsupportedArtifactInputError extends Error {}

/**
 * Bound reads before decoding recursively uploaded firewall byproducts.
 * @param {string} filePath
 * @returns {string}
 */
function readAdditionalArtifactText(filePath) {
  const descriptor = fs.openSync(filePath, "r");
  try {
    const size = fs.fstatSync(descriptor).size;
    if (size > MAX_ADDITIONAL_ARTIFACT_BYTES) throw new UnsupportedArtifactInputError(`${ERR_VALIDATION}: oversized`);
    const bytes = Buffer.alloc(size + 1);
    let length = 0;
    while (length < bytes.length) {
      const count = fs.readSync(descriptor, bytes, length, bytes.length - length, null);
      if (!count) break;
      length += count;
    }
    if (length > size) throw new UnsupportedArtifactInputError(`${ERR_VALIDATION}: size_changed`);
    let text;
    try {
      text = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(bytes.subarray(0, length));
    } catch {
      throw new UnsupportedArtifactInputError(`${ERR_VALIDATION}: non-text`);
    }
    if (/[\u0000-\u0008\u000b\u000e-\u001f]/.test(text)) throw new UnsupportedArtifactInputError(`${ERR_VALIDATION}: non-text`);
    return text;
  } finally {
    fs.closeSync(descriptor);
  }
}

/**
 * @param {unknown} error
 * @param {...string} codes
 * @returns {boolean}
 */
function isErrnoCode(error, ...codes) {
  return error !== null && typeof error === "object" && "code" in error && typeof error.code === "string" && codes.includes(error.code);
}

const REDACTION_FILE_ERROR_CODES = new Set(["EACCES", "EPERM", "ENOSPC", "ENOENT", "EBUSY", "EIO", "EROFS", "EMFILE", "ENFILE", "EDQUOT", "EISDIR", "ENOTDIR", "EXDEV"]);

/** @typedef {"read" | "redact" | "write" | "replace" | "unlink"} RedactionStage */

/**
 * Keep diagnostics independent of exception messages and artifact payloads.
 * @param {unknown} error
 * @returns {string}
 */
function redactionErrorCode(error) {
  if (error !== null && typeof error === "object") {
    const code = Object.getOwnPropertyDescriptor(error, "code")?.value;
    if (typeof code === "string" && REDACTION_FILE_ERROR_CODES.has(code)) return code;
  }
  return "unknown";
}

class RedactionOperationError extends Error {
  /**
   * @param {RedactionStage} stage
   * @param {unknown} error
   */
  constructor(stage, error) {
    const code = redactionErrorCode(error);
    super(`Secret redaction failed (stage=${stage}, code=${code})`);
    this.stage = stage;
    this.code = code;
  }
}

/**
 * Recursively finds all files matching the specified extensions
 * @param {string} dir - Directory to search
 * @param {string[] | null} extensions - File extensions to match, or null to include every regular file
 * @returns {string[]} Array of file paths
 */
function findFiles(dir, extensions) {
  const results = [];
  try {
    if (!fs.existsSync(dir)) {
      return results;
    }
    const pending = [dir];
    while (pending.length) {
      const currentDir = pending.pop();
      if (currentDir === undefined) break;
      try {
        let entries;
        try {
          entries = fs.readdirSync(currentDir, { withFileTypes: true });
        } catch (error) {
          if (isErrnoCode(error, "ENOENT")) {
            core.warning(`Skipping directory that disappeared during secret redaction: ${currentDir}`);
            continue;
          }
          throw error;
        }
        for (const entry of entries) {
          const fullPath = path.join(currentDir, entry.name);
          if (entry.isSymbolicLink()) {
            try {
              fs.unlinkSync(fullPath);
            } catch (error) {
              if (isErrnoCode(error, "ENOENT")) {
                core.warning(`Skipping symbolic link that disappeared during secret redaction: ${fullPath}`);
                continue;
              }
              throw error;
            }
            core.warning(`Removed symbolic link before artifact upload: ${fullPath}`);
          } else if (entry.isDirectory()) {
            pending.push(fullPath);
          } else if (entry.isFile() && (extensions === null || extensions.includes(path.extname(entry.name).toLowerCase()))) {
            results.push(fullPath);
          }
        }
      } catch (error) {
        throw new Error(`${ERR_VALIDATION}: Failed to scan directory ${currentDir}: ${getErrorMessage(error)}`, { cause: error });
      }
    }
  } catch (error) {
    throw new Error(`${ERR_VALIDATION}: Failed to scan directory ${dir}: ${getErrorMessage(error)}`, { cause: error });
  }
  return results;
}

/**
 * Built-in regex patterns for common credential types
 * Each pattern is designed to match legitimate credential formats
 */
const BUILT_IN_PATTERNS = [
  // GitHub tokens
  { name: "GitHub Personal Access Token (classic)", pattern: /ghp_[0-9a-zA-Z]{36}/g },
  // New stateless ghs_ token format allows dots, underscores, and dashes
  // and uses variable length (minimum 36 chars after prefix).
  // https://github.blog/changelog/2026-05-15-github-app-installation-tokens-per-request-override-header/
  { name: "GitHub Server-to-Server Token", pattern: /ghs_[0-9A-Za-z._-]{36,}/g },
  { name: "GitHub OAuth Access Token", pattern: /gho_[0-9a-zA-Z]{36}/g },
  { name: "GitHub User Access Token", pattern: /ghu_[0-9a-zA-Z]{36}/g },
  { name: "GitHub Fine-grained PAT", pattern: /github_pat_[0-9a-zA-Z_]{82}/g },
  { name: "GitHub Refresh Token", pattern: /ghr_[0-9a-zA-Z]{36}/g },

  // Azure tokens
  { name: "Azure Storage Account Key", pattern: /AccountKey=[a-zA-Z0-9+/]{86}==/g },
  { name: "Azure SAS Token", pattern: /\?sv=[0-9-]{1,20}&s[rts]=[\w\-]{1,20}&sig=[A-Za-z0-9%+/=]{1,200}/g },

  // Google/GCP tokens
  { name: "Google API Key", pattern: /AIzaSy[0-9A-Za-z_-]{33}/g },
  { name: "Google OAuth Access Token", pattern: /ya29\.[0-9A-Za-z_-]{1,800}/g },

  // AWS tokens
  { name: "AWS Access Key ID", pattern: /AKIA[0-9A-Z]{16}/g },

  // OpenAI tokens
  { name: "OpenAI API Key", pattern: /sk-[a-zA-Z0-9]{48}/g },
  { name: "OpenAI Project API Key", pattern: /sk-proj-[a-zA-Z0-9]{48,64}/g },

  // Anthropic tokens
  { name: "Anthropic API Key", pattern: /sk-ant-api03-[a-zA-Z0-9_-]{95}/g },

  // Linear tokens
  { name: "Linear API Key", pattern: /lin_api_[0-9A-Za-z]{40}/g },

  // Grafana Cloud tokens may be decoded from a stored Basic-auth header.
  { name: "Grafana Cloud API Token", pattern: /glc_[0-9A-Za-z+/=_-]{20,}/g },
];

/**
 * MCP gateway configuration files that may contain bearer tokens.
 * These are the canonical paths produced by the gateway setup scripts.
 * The list is defined as a module-level constant so tests can replace entries.
 */
// Shell setup scripts write under /tmp, while CJS converters use RUNNER_TEMP.
const MCP_GATEWAY_CONFIG_PATHS = [
  ...new Set(
    [
      path.join("/tmp", "gh-aw/mcp-config/gateway-output.json"),
      path.join("/tmp", "gh-aw/mcp-config/mcp-servers.json"),
      path.join("/tmp", "gh-aw/mcp-config/config.toml"),
      path.join("/tmp", "gh-aw/pi-agent-dir/mcp.json"),
      path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/gateway-output.json"),
      path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/mcp-servers.json"),
      path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/config.toml"),
      process.env.HOME ? path.join(process.env.HOME, ".copilot/mcp-config.json") : "",
      process.env.GITHUB_WORKSPACE ? path.join(process.env.GITHUB_WORKSPACE, ".gemini/settings.json") : "",
    ].filter(Boolean)
  ),
];

/**
 * Minimum credential length required before an Authorization value is treated
 * as a gateway token. Guards against redacting short placeholder values.
 */
const MIN_GATEWAY_TOKEN_LENGTH = 6;

/**
 * Extracts MCP gateway bearer tokens from known configuration files.
 * The gateway token is dynamically minted and has no recognisable prefix,
 * so it cannot be caught by the built-in regex patterns.  We read the
 * gateway config files directly and treat every Authorization header value
 * as a secret to be redacted.
 * @param {string[]} configPaths - Paths to MCP gateway config JSON files
 * @returns {string[]} Unique token values extracted from the files
 */
function extractMCPGatewayTokens(configPaths) {
  /** @type {Set<string>} */
  const tokens = new Set();

  /**
   * Records an Authorization header value plus, for a bearer header, the bare
   * credential so the token is redacted even when logged without the prefix.
   * @param {unknown} value - Raw Authorization header value
   */
  const addAuthorizationValue = value => {
    if (typeof value !== "string") return;
    const trimmed = value.trim();
    const bearerMatch = /^[Bb]earer\s+(.+)$/.exec(trimmed);
    // The minimum-length guard applies to the credential itself, never to the
    // bearer prefix, so short values cannot slip through by being prefixed.
    const credential = bearerMatch ? bearerMatch[1].trim() : trimmed;
    if (credential.length < MIN_GATEWAY_TOKEN_LENGTH) return;
    tokens.add(trimmed);
    tokens.add(credential);
  };

  for (const configPath of configPaths) {
    try {
      if (!fs.existsSync(configPath)) continue;
      const raw = fs.readFileSync(configPath, "utf8");
      let config;
      try {
        config = /** @type {Record<string, any>} */ JSON.parse(raw);
      } catch {
        // Codex writes TOML (`http_headers = { Authorization = "..." }`); the key
        // is matched case-insensitively to tolerate formatting differences.
        for (const match of raw.matchAll(/\bAuthorization\s*=\s*"([^"]+)"/gi)) {
          addAuthorizationValue(match[1]);
        }
        continue;
      }
      const servers = /** @type {Record<string, any>} */ config.mcpServers || {};
      for (const server of Object.values(servers)) {
        addAuthorizationValue(server?.headers?.Authorization);
      }
    } catch {
      // Unreadable or malformed files are ignored — absence of the gateway
      // config is normal when the MCP gateway is not used by a workflow.
    }
  }
  return [...tokens];
}

/**
 * Detects and redacts secrets matching built-in patterns
 * @param {string} content - File content to process
 * @returns {{content: string, redactionCount: number, detectedPatterns: string[]}} Redacted content, count, and detected pattern types
 */
function redactBuiltInPatterns(content) {
  let redactionCount = 0;
  let redacted = content;
  const detectedPatterns = [];

  for (const { name, pattern } of BUILT_IN_PATTERNS) {
    const matches = redacted.match(pattern);
    if (matches && matches.length > 0) {
      // Redact each match with fixed-length string
      const replacement = "***REDACTED***";
      for (const match of matches) {
        redacted = redacted.split(match).join(replacement);
      }
      redactionCount += matches.length;
      detectedPatterns.push(name);
    }
  }

  return { content: redacted, redactionCount, detectedPatterns };
}

/**
 * Redacts secrets from file content using exact string matching
 * @param {string} content - File content to process
 * @param {string[]} secretValues - Array of secret values to redact
 * @returns {{content: string, redactionCount: number}} Redacted content and count of redactions
 */
function redactSecrets(content, secretValues) {
  let redactionCount = 0;
  let redacted = content;
  // Sort secret values by length (longest first) to handle overlapping secrets
  const sortedSecrets = secretValues.slice().sort((a, b) => b.length - a.length);
  for (const secretValue of sortedSecrets) {
    // Skip empty or very short values (likely not actual secrets)
    if (!secretValue || secretValue.length < 6) {
      continue;
    }

    // Count occurrences before replacement
    // Use split and join for exact string matching (not regex)
    // This is safer than regex as it doesn't interpret special characters
    // Use fixed-length redaction string without prefix preservation
    const replacement = "***REDACTED***";
    const parts = redacted.split(secretValue);
    const occurrences = parts.length - 1;
    if (occurrences > 0) {
      redacted = parts.join(replacement);
      redactionCount += occurrences;
    }
  }
  return { content: redacted, redactionCount };
}

/**
 * @param {string} content
 * @returns {boolean}
 */
function isValidJSONArtifact(content) {
  try {
    JSON.parse(content);
    return true;
  } catch {
    let hasRecord = false;
    for (const event of iterateJsonlEvents(content)) {
      if (event.type === "json") hasRecord = true;
      else if (event.reason !== "blank") return false;
    }
    return hasRecord;
  }
}

/**
 * Match all credential sources before any replacement or JSON serialization can
 * fragment a longer secret or change its spelling.
 * @param {string} content
 * @param {string[]} secretValues
 * @param {string[]} maskedValues
 * @param {{decodedText?: boolean, patternMatches?: Array<{start: number, end: number, value: string, replacement: string}>}} [options] - Treat already-decoded protocol payloads as literal text
 * @returns {{content: string, redactionCount: number}}
 */
function redactArtifactContent(content, secretValues, maskedValues, options = {}) {
  const configured = new Set(secretValues.filter(value => value && value.length >= 6));
  /** @type {Map<string, string>} */
  const builtInValues = new Map();
  // Discovery only: include escaped built-in credentials without rewriting the
  // original numeric lexemes, malformed records, or unchanged JSON strings.
  const escapes = { '"': '"', "\\": "\\", "/": "/", b: "\b", f: "\f", n: "\n", r: "\r", t: "\t" };
  const decoded = options.decodedText ? content : content.replace(/\\(["\\/bfnrt]|u[0-9a-fA-F]{4})/g, (_, escape) => (escape[0] === "u" ? String.fromCharCode(parseInt(escape.slice(1), 16)) : escapes[escape]));
  for (const text of new Set([content, decoded])) {
    for (const { name, pattern } of BUILT_IN_PATTERNS) {
      for (const match of text.matchAll(pattern)) builtInValues.set(match[0], name);
    }
  }
  const values = [...new Set([...configured, ...builtInValues.keys(), ...maskedValues])].sort((a, b) => b.length - a.length);
  let redactionCount = 0;
  const replacement = value => {
    redactionCount++;
    const name = builtInValues.get(value);
    return name || configured.has(value) ? "***REDACTED***" : "***";
  };
  // Protocol callbacks already receive decoded strings, not serialized artifacts.
  const redacted = options.decodedText
    ? redactDecodedCredentialText(content, values, replacement)
    : redactArtifactMaskedValues(content, values, undefined, replacement, {
        textMode: !!options.patternMatches?.length || !isValidJSONArtifact(content),
        patternMatches: options.patternMatches,
      });
  return { content: redacted, redactionCount: redacted === content ? 0 : Math.max(1, redactionCount) };
}

/**
 * Keep command lines intact until removal, unless a credential union crosses
 * their boundary and must be replaced before removing any original text.
 * @param {string} content
 * @param {string[]} values
 * @param {(value: string) => string} replacement
 * @returns {string}
 */
function redactDecodedCredentialText(content, values, replacement) {
  const commands = [];
  let offset = 0;
  for (const line of content.split("\n")) {
    if (isAddMaskCommandLine(line)) commands.push({ start: offset, end: offset + line.length });
    offset += line.length + 1;
  }
  const parts = [];
  let cursor = 0;
  let commandIndex = 0;
  for (const match of overlappingMatchGroups(maskedValueMatches(content, values, undefined, true))) {
    while (commandIndex < commands.length && commands[commandIndex].end < match.start) commandIndex++;
    const command = commands[commandIndex];
    if (command && match.start >= command.start && match.end <= command.end) continue;
    parts.push(content.slice(cursor, match.start), replacement(match.value));
    cursor = match.end;
  }
  parts.push(content.slice(cursor));
  return applyAddMaskRedaction(parts.join(""), []);
}

/**
 * Validate and read the host-only AWF early configuration artifact. The exact
 * path contract and metadata checks prevent an environment variable from
 * turning this diagnostic step into an arbitrary host-file uploader.
 * @param {string} sourcePath
 * @returns {string}
 */
function readAWFResolvedConfig(sourcePath) {
  if (typeof sourcePath !== "string" || !AWF_RESOLVED_CONFIG_PATH_PATTERN.test(sourcePath)) {
    throw new Error(`${ERR_VALIDATION}: Invalid AWF resolved configuration publication path`);
  }
  const uid = process.getuid?.();
  const root = fs.lstatSync("/run/awf-config-audit");
  const directory = fs.lstatSync(path.dirname(sourcePath));
  const source = fs.lstatSync(sourcePath);
  if (
    !root.isDirectory() ||
    root.uid !== 0 ||
    (root.mode & 0o022) !== 0 ||
    !directory.isDirectory() ||
    directory.uid !== uid ||
    (directory.mode & 0o777) !== 0o700 ||
    !source.isFile() ||
    source.uid !== uid ||
    source.nlink !== 1 ||
    (source.mode & 0o777) !== 0o600
  ) {
    throw new Error(`${ERR_VALIDATION}: AWF resolved configuration artifact failed ownership or permission validation`);
  }
  const fd = fs.openSync(sourcePath, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW);
  try {
    const opened = fs.fstatSync(fd);
    if (!opened.isFile() || opened.nlink !== 1 || opened.uid !== uid || (opened.mode & 0o777) !== 0o600 || opened.ino !== source.ino || opened.dev !== source.dev) {
      throw new Error(`${ERR_VALIDATION}: AWF resolved configuration artifact changed during validation`);
    }
    return fs.readFileSync(fd, "utf8");
  } finally {
    fs.closeSync(fd);
  }
}

/**
 * Copy only sanitized AWF configuration into the normal artifact tree.
 * @param {string | undefined} sourcePath
 * @param {string[]} secretValues
 * @param {string[]} maskedValues
 * @returns {boolean}
 */
function stageAWFResolvedConfig(sourcePath, secretValues, maskedValues) {
  try {
    // Remove stale files, directories, or symlinks without following links. This
    // keeps rejected entries out of artifact uploads when older AWF omits this file.
    fs.rmSync(AWF_RESOLVED_CONFIG_STAGING_PATH, { recursive: true, force: true });
    if (!sourcePath) return false;
    const raw = readAWFResolvedConfig(sourcePath);
    let parsed;
    try {
      parsed = JSON.parse(raw);
    } catch {
      throw new Error(`${ERR_VALIDATION}: AWF resolved configuration is not valid JSON`);
    }
    const safeConfig = redactSensitiveConfig(parsed);
    const serialized = JSON.stringify(safeConfig, null, 2);
    const redactionValues = [...new Set([...maskedValues, ...secretValues])];
    const redacted = redactSecrets(redactBuiltInPatterns(redactArtifactMaskedValues(serialized, redactionValues)).content, redactionValues).content;
    const temporary = fs.mkdtempSync("/tmp/gh-aw/.awf-resolved-config-");
    try {
      const temporaryFile = path.join(temporary, "awf-resolved-config.json");
      fs.writeFileSync(temporaryFile, redacted, { encoding: "utf8", mode: 0o600, flag: "wx" });
      fs.renameSync(temporaryFile, AWF_RESOLVED_CONFIG_STAGING_PATH);
    } finally {
      fs.rmSync(temporary, { recursive: true, force: true });
    }
    return true;
  } catch (error) {
    // A failed validation or staging attempt must never leave stale output for upload.
    fs.rmSync(AWF_RESOLVED_CONFIG_STAGING_PATH, { recursive: true, force: true });
    throw error;
  }
}

/**
 * Redacts credential-shaped strings from content destined for the GitHub Actions
 * step summary.
 *
 * Step summaries reproduce agent-controlled data (tool inputs, tool outputs, agent
 * text, safe-output titles), and `::add-mask::` processing does not scrub
 * `$GITHUB_STEP_SUMMARY`, so the built-in credential patterns used for artifact
 * redaction are applied before the summary is written. Redaction failures are
 * non-fatal because the summary is best-effort output.
 *
 * @param {string} content - Markdown destined for the step summary
 * @returns {string} Content with credential-shaped strings replaced
 */
function redactStepSummaryContent(content) {
  if (typeof content !== "string" || content.length === 0) {
    return content;
  }
  try {
    return redactBuiltInPatterns(redactArtifactMaskedValues(content, getOTLPSecretValues())).content;
  } catch (error) {
    core.warning(`Failed to redact step summary content: ${getErrorMessage(error)}`);
    return content;
  }
}

/**
 * Writes redacted content or prepares unchanged files for configured custom
 * redactors. Called only when content changed or custom preparation is required.
 * @param {string} filePath
 * @param {string} content
 * @param {boolean} changed
 */
function writeProcessedFile(filePath, content, changed) {
  /** @type {RedactionStage} */
  let stage = "write";
  try {
    try {
      if (changed) {
        fs.writeFileSync(filePath, content, "utf8");
      } else {
        fs.accessSync(filePath, fs.constants.W_OK);
      }
      return;
    } catch (error) {
      if (!isErrnoCode(error, "EACCES", "EPERM")) {
        throw error;
      }
    }

    // Replacing a file needs a writable parent, not ownership of the old inode.
    // Keep the temporary copy private and on the same filesystem for atomic rename.
    let temporaryDir;
    try {
      stage = "replace";
      temporaryDir = fs.mkdtempSync(path.join(path.dirname(filePath), ".gh-aw-redact-"));
    } catch (error) {
      if (!isErrnoCode(error, "EACCES", "EPERM")) {
        throw error;
      }
      // Go module caches make both files and parents read-only. The file owner
      // can still prepare the inode without changing directory permissions.
      stage = "write";
      fs.chmodSync(filePath, 0o600);
      if (changed) {
        fs.writeFileSync(filePath, content, "utf8");
      } else {
        fs.accessSync(filePath, fs.constants.W_OK);
      }
      return;
    }
    /** @type {RedactionOperationError | undefined} */
    let operationDiagnostic;
    try {
      const replacement = path.join(temporaryDir, "redacted");
      stage = "write";
      fs.writeFileSync(replacement, content, { encoding: "utf8", mode: 0o600, flag: "wx" });
      stage = "replace";
      fs.renameSync(replacement, filePath);
      core.warning(`Replaced non-writable file for secret redaction: ${filePath}`);
    } catch (error) {
      operationDiagnostic = error instanceof RedactionOperationError ? error : new RedactionOperationError(stage, error);
      throw operationDiagnostic;
    } finally {
      try {
        fs.rmSync(temporaryDir, { recursive: true, force: true });
      } catch (error) {
        const cleanupDiagnostic = new RedactionOperationError("unlink", error);
        if (operationDiagnostic) {
          throw new AggregateError([operationDiagnostic, cleanupDiagnostic], "Secret redaction failed");
        }
        core.warning(`Failed to remove temporary redaction directory: ${cleanupDiagnostic.message}`);
      }
    }
  } catch (error) {
    if (error instanceof AggregateError && error.errors.every(item => item instanceof RedactionOperationError)) {
      throw error;
    }
    throw error instanceof RedactionOperationError ? error : new RedactionOperationError(stage, error);
  }
}

/**
 * Process a single file for secret redaction
 * @param {string} filePath - Path to the file
 * @param {string[]} secretValues - Array of secret values to redact
 * @param {string[]} [maskedValues] - Runtime masks collected before any file is sanitized
 * @param {boolean} [prepareForCustomMasking] - Prepare unchanged files for configured custom redactors
 * @param {boolean} [additionalArtifact] - Require bounded UTF-8 text for recursively uploaded files
 * @returns {number} Number of redactions made
 */
function processFile(filePath, secretValues, maskedValues = [], prepareForCustomMasking = false, additionalArtifact = false) {
  /** @type {RedactionStage} */
  let stage = "read";
  /** @type {number | undefined} */
  let inputBytes;
  try {
    const input = additionalArtifact ? readAdditionalArtifactText(filePath) : fs.readFileSync(filePath);
    const content = typeof input === "string" ? input : input.toString("utf8");
    inputBytes = typeof input === "string" ? Buffer.byteLength(input, "utf8") : input.length;
    stage = "redact";
    const structured = redactSafeOutputContent(
      filePath,
      content,
      [...maskedValues, ...secretValues.filter(value => value && value.length >= 6)].sort((a, b) => b.length - a.length),
      text => redactArtifactContent(text, secretValues, maskedValues, { decodedText: true }).content
    );
    if (structured !== null) {
      const redactionCount = structured === content ? 0 : 1;
      if (redactionCount || prepareForCustomMasking) {
        stage = "write";
        writeProcessedFile(filePath, structured, Boolean(redactionCount));
      }
      return redactionCount;
    }
    // Scan binary bytes losslessly in memory; remove sensitive binary state rather than rewrite it.
    if (Buffer.isBuffer(input) && !input.equals(Buffer.from(content, "utf8"))) {
      const byteResult = redactArtifactContent(input.toString("latin1"), secretValues, maskedValues);
      const textResult = redactArtifactContent(content, secretValues, maskedValues);
      const redactionCount = byteResult.redactionCount + textResult.redactionCount;
      if (redactionCount > 0) {
        stage = "unlink";
        fs.unlinkSync(filePath);
        return redactionCount;
      }
    }
    const encodedResult = path.extname(filePath).toLowerCase() === ".html" ? redactPiSessionHTML(content, text => redactArtifactContent(text, secretValues, maskedValues)) : { content, redactionCount: 0 };
    const genericResult = redactArtifactContent(encodedResult.content, secretValues, maskedValues);
    const redacted = genericResult.content;
    const totalRedactions = encodedResult.redactionCount + genericResult.redactionCount;

    if (totalRedactions > 0 || prepareForCustomMasking) {
      stage = "write";
      writeProcessedFile(filePath, redacted, totalRedactions > 0);
    }
    return totalRedactions;
  } catch (error) {
    if (isErrnoCode(error, "ENOENT") && !fs.existsSync(filePath)) {
      core.warning(`Skipping file that disappeared during secret redaction: ${filePath}`);
      return 0;
    }
    // Uploads can run with always(); do not leave an unsanitized source behind.
    const diagnostics = error instanceof AggregateError && error.errors.every(item => item instanceof RedactionOperationError) ? error.errors : [error instanceof RedactionOperationError ? error : new RedactionOperationError(stage, error)];
    try {
      fs.unlinkSync(filePath);
    } catch (cleanupError) {
      if (!isErrnoCode(cleanupError, "ENOENT")) {
        const cleanupDiagnostic = new RedactionOperationError("unlink", cleanupError);
        diagnostics.forEach(diagnostic => core.warning(diagnostic.message));
        core.warning(cleanupDiagnostic.message);
        throw new AggregateError([...diagnostics, cleanupDiagnostic], `${ERR_VALIDATION}: Failed to remove artifact source after secret redaction failed (input_bytes=${inputBytes ?? "unknown"})`);
      }
    }
    if (error instanceof UnsupportedArtifactInputError) {
      core.warning(`Removed unsupported artifact before upload (reason=${error.message})`);
      return 0;
    }
    diagnostics.forEach(diagnostic => core.warning(diagnostic.message));
    core.setFailed(`${ERR_VALIDATION}: Removed artifact source after secret redaction failed (input_bytes=${inputBytes ?? "unknown"})`);
    return 0;
  }
}

/**
 * Main function
 * @param {{prepareForCustomMasking?: boolean, additionalDirectories?: string[], persistDetectionMasks?: (values: string[]) => void}} [options]
 */
async function main({ prepareForCustomMasking = false, additionalDirectories = [], persistDetectionMasks } = {}) {
  // Get the list of secret names from environment variable
  const secretNames = process.env.GH_AW_SECRET_NAMES;

  try {
    // Collect custom secret values from environment variables
    const secretValues = [];
    if (secretNames) {
      // Parse the comma-separated list of secret names
      const secretNameList = secretNames.split(",").filter(name => name.trim());
      for (const secretName of secretNameList) {
        const envVarName = `SECRET_${secretName}`;
        const secretValue = process.env[envVarName];
        // Skip empty or undefined secrets
        if (!secretValue || secretValue.trim() === "") {
          continue;
        }
        secretValues.push(secretValue.trim());
      }
    }

    // Extract MCP gateway bearer tokens from known config files and add them to
    // the redaction list.  The gateway token has no fixed prefix so it cannot be
    // matched by the built-in regex patterns; we must read it from the config.
    const gatewayTokens = extractMCPGatewayTokens(MCP_GATEWAY_CONFIG_PATHS);
    if (gatewayTokens.length > 0) {
      secretValues.push(...gatewayTokens);
    }

    // Find all target files in /tmp/gh-aw and ${RUNNER_TEMP}/gh-aw directories
    const targetExtensions = [".txt", ".json", ".log", ".md", ".mdx", ".yml", ".jsonl", ".patch", ".html"];
    const tmpFiles = findFiles("/tmp/gh-aw", targetExtensions);
    const optFiles = findFiles(`${process.env.RUNNER_TEMP}/gh-aw`, targetExtensions);
    const additionalFiles = new Set(additionalDirectories.flatMap(dir => findFiles(dir, null)));
    const files = [...new Set([...tmpFiles, ...optFiles, ...additionalFiles])];
    // Collect before built-in/custom redaction or bootstrap removes mask commands.
    // Only sanitized sources cross the job boundary, never the raw mask values.
    const masks = new Set(getOTLPSecretValues());
    try {
      /** @type {Set<string>} */
      const detectionMasks = new Set();
      if (process.env[DETECTION_MASKS_ENV]) {
        const saved = JSON.parse(process.env[DETECTION_MASKS_ENV]);
        if (!Array.isArray(saved) || saved.some(value => typeof value !== "string")) throw new Error(`${ERR_VALIDATION}: Invalid detection mask state`);
        for (const value of saved) detectionMasks.add(value);
      }
      for (const file of files.filter(file => ["agent-stdio.log", "detection.log"].includes(path.basename(file)))) {
        const content = additionalFiles.has(file) ? readAdditionalArtifactText(file) : fs.readFileSync(file, "utf8");
        for (const value of collectAddMaskedValues(content)) {
          masks.add(value);
          if (path.basename(file) === "detection.log") detectionMasks.add(value);
        }
      }
      for (const value of detectionMasks) masks.add(value);
      if (detectionMasks.size) {
        // Runner command emission belongs to the detection step, not the shared
        // redaction dependency deployed to isolated MCP/safe-output processes.
        if (!persistDetectionMasks) throw new Error(`${ERR_VALIDATION}: Detection mask persistence is unavailable`);
        persistDetectionMasks([...detectionMasks]);
      }
    } catch (error) {
      for (const source of files) {
        try {
          fs.unlinkSync(source);
        } catch (cleanupError) {
          if (!isErrnoCode(cleanupError, "ENOENT")) {
            const cleanupDiagnostic = new RedactionOperationError("unlink", cleanupError);
            throw cleanupDiagnostic;
          }
        }
      }
      throw new Error(`${ERR_VALIDATION}: Removed artifact sources after runtime mask collection failed`, { cause: error });
    }
    const maskedValues = [...masks].sort((a, b) => b.length - a.length);
    try {
      stageAWFResolvedConfig(process.env.AWF_RESOLVED_CONFIG, secretValues, maskedValues);
    } catch (error) {
      core.setFailed(`${ERR_VALIDATION}: AWF resolved configuration was not collected safely`);
      core.warning(`AWF resolved configuration collection failed: ${getErrorMessage(error)}`);
      core.warning("Continuing with standard artifact redaction despite AWF resolved configuration collection failure");
    }
    // Process each file
    for (const file of files) {
      processFile(file, secretValues, maskedValues, prepareForCustomMasking, additionalFiles.has(file));
    }
  } catch (error) {
    core.setFailed(`${ERR_VALIDATION}: Secret redaction failed: ${getErrorMessage(error)}`);
  }
}

/**
 * Targeted redaction pass for files in a specific directory. Used by the
 * post-graders redaction step to scan grader output files that were written
 * after the initial full-workspace redaction pass.
 *
 * @param {string} dir - Absolute directory path to scan
 */
async function redactFilesInDir(dir) {
  try {
    const secretNames = (process.env.GH_AW_SECRET_NAMES || "").split(",").filter(n => n.trim());
    /** @type {string[]} */
    const secretValues = [];
    for (const secretName of secretNames) {
      const value = process.env[`SECRET_${secretName.trim()}`];
      if (typeof value === "string" && value.trim() !== "") {
        secretValues.push(value.trim());
      }
    }
    secretValues.push(...extractMCPGatewayTokens(MCP_GATEWAY_CONFIG_PATHS));

    const targetExtensions = [".json"];
    const files = findFiles(dir, targetExtensions);
    if (files.length === 0) return;

    const maskedValues = getOTLPSecretValues();
    for (const file of files) {
      processFile(file, secretValues, maskedValues);
    }
  } catch (error) {
    core.warning(`Grader output redaction failed: ${getErrorMessage(error)}`);
  }
}

module.exports = {
  main,
  redactFilesInDir,
  findFiles,
  processFile,
  redactSecrets,
  redactBuiltInPatterns,
  redactArtifactContent,
  redactStepSummaryContent,
  extractMCPGatewayTokens,
  readAWFResolvedConfig,
  stageAWFResolvedConfig,
  AWF_RESOLVED_CONFIG_STAGING_PATH,
  BUILT_IN_PATTERNS,
  MCP_GATEWAY_CONFIG_PATHS,
};
