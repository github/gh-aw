// @ts-check

/**
 * Copilot SDK Permission Helpers
 *
 * Provides reusable permission-enforcement utilities for Copilot SDK driver
 * implementations.  Extracts allow-tool rules from sidecar server args, builds
 * an on-permission handler that enforces those rules, and logs every denial.
 *
 * Consumed by copilot_sdk_session.cjs (the built-in driver) and available to
 * any custom driver that wants to mirror the same permission policy.
 */

"use strict";

const path = require("path");
const { extractCommandNamesFromPipeline, splitOnPipelineOperators } = require("./bash_command_parser.cjs");

/** @const {number} Default maximum number of permission denials before the session is stopped. */
const MAX_TOOL_DENIALS_DEFAULT = 5;

/**
 * @typedef {{
 *   allowAllTools?: boolean,
 *   allowedTools?: string[],
 * }} CopilotSDKPermissionConfig
 */

/**
 * @typedef {{
 *   info?: (message: string) => void,
 *   warning?: (message: string) => void,
 * }} CopilotSDKCoreLogger
 */

/**
 * Parse a strict positive integer from a number or string.
 * Returns undefined when the input is not a whole positive integer.
 *
 * @param {unknown} value
 * @returns {number | undefined}
 */
function parseStrictPositiveInteger(value) {
  if (typeof value === "number" && Number.isSafeInteger(value) && value > 0) {
    return value;
  }
  if (typeof value === "string") {
    const trimmed = value.trim();
    if (/^\d+$/.test(trimmed)) {
      const parsed = Number.parseInt(trimmed, 10);
      if (Number.isSafeInteger(parsed) && parsed > 0) {
        return parsed;
      }
    }
  }
  return undefined;
}

/**
 * Parse max tool denials threshold from input.
 * Falls back to MAX_TOOL_DENIALS_DEFAULT when unset/invalid.
 *
 * @param {unknown} value
 * @returns {number}
 */
function parseMaxToolDenialsLimit(value) {
  return parseStrictPositiveInteger(value) ?? MAX_TOOL_DENIALS_DEFAULT;
}

/**
 * Read a positive integer from an environment variable with fallback.
 *
 * @param {string} key
 * @param {number} fallback
 * @param {NodeJS.ProcessEnv} [env]
 * @returns {number}
 */
function getEnvPositiveIntOrDefault(key, fallback, env = process.env) {
  return parseStrictPositiveInteger(env[key]) ?? fallback;
}

/**
 * Create a compact, human-readable permission-request summary for diagnostics.
 * Examples: shell(git status), mcp(github.get_file_contents), url(https://example.com).
 *
 * @param {import("@github/copilot-sdk").PermissionRequest} request
 * @returns {string}
 */
function summarizePermissionRequest(request) {
  switch (request.kind) {
    case "shell":
      return `shell(${String(request.fullCommandText || "").trim() || "unknown"})`;
    case "mcp":
      return `mcp(${request.serverName || "unknown"}.${request.toolName || "unknown"})`;
    case "url":
      return `url(${request.url || "unknown"})`;
    case "write":
      return `write(${request.fileName || "unknown"})`;
    case "read":
      return `read(${request.path || "unknown"})`;
    case "custom-tool":
      return `custom-tool(${request.toolName || "unknown"})`;
    default:
      return request.kind;
  }
}

/**
 * @param {CopilotSDKCoreLogger | undefined} coreLogger
 * @param {(msg: string) => void} logger
 * @param {import("@github/copilot-sdk").PermissionRequest} request
 */
function logPermissionDenied(coreLogger, logger, request) {
  const requestSummary = summarizePermissionRequest(request);
  logger(`permission denied by workflow tool permissions: ${requestSummary}`);
  if (coreLogger?.info) {
    coreLogger.info(`Copilot SDK permission denied: ${requestSummary}`);
  }
  if (coreLogger?.warning) {
    coreLogger.warning(`Copilot SDK permission denied by workflow tool permissions: ${requestSummary}`);
  }
}

/**
 * @param {string} value
 * @returns {string}
 */
function normalizePermissionPath(value) {
  return (
    String(value || "")
      .trim()
      .replace(/\\/g, "/")
      .replace(/\/+$/, "") || "/"
  );
}

/**
 * @param {string} shellRule
 * @returns {string[]}
 */
function extractReadablePathPatternsFromShellRule(shellRule) {
  const trimmed = String(shellRule || "").trim();
  if (!trimmed) return [];

  if (trimmed.startsWith("cat ")) {
    return [trimmed.slice("cat ".length).trim()];
  }

  const xargsCatMatch = trimmed.match(/^xargs\s+-a\s+(\S+)\s+cat(?:\s|$)/);
  if (xargsCatMatch) {
    return [xargsCatMatch[1]];
  }

  const lsMatch = trimmed.match(/^ls\s+(\S+)(?:\s|$)/);
  if (lsMatch) {
    return [lsMatch[1]];
  }

  return [];
}

/**
 * @param {string | undefined} requestedPath
 * @param {string[]} allowedPathPatterns
 * @param {string | undefined} [workspaceRoot] - Optional workspace root for relative pattern matching.
 *   When provided, absolute paths under this root are also matched against relative patterns by
 *   stripping the workspace prefix first.  This allows shell rules like `cat pkg/**\/*.go` to
 *   permit `view` tool requests that arrive as absolute paths (e.g.
 *   `/home/runner/work/gh-aw/gh-aw/pkg/workflow/file.go`).
 * @returns {boolean}
 */
function isReadPathAllowedByShellRules(requestedPath, allowedPathPatterns, workspaceRoot) {
  if (typeof requestedPath !== "string" || requestedPath.trim().length === 0) {
    return false;
  }

  const normalizedRequestedPath = normalizePermissionPath(requestedPath);

  // Pre-compute the workspace-relative path once when the requested path is
  // absolute and a workspace root is available.  Used below as a fallback when
  // the pattern is relative.
  let relativeRequestedPath;
  if (workspaceRoot && normalizedRequestedPath.startsWith("/")) {
    const normalizedWorkspace = normalizePermissionPath(workspaceRoot);
    if (normalizedRequestedPath.startsWith(normalizedWorkspace + "/")) {
      relativeRequestedPath = normalizedRequestedPath.slice(normalizedWorkspace.length + 1);
    }
  }

  return allowedPathPatterns.some(pattern => {
    const normalizedPattern = normalizePermissionPath(pattern);
    if (normalizedRequestedPath === normalizedPattern) {
      return true;
    }
    if (path.posix.matchesGlob(normalizedRequestedPath, normalizedPattern)) {
      return true;
    }
    // If the pattern is relative (does not start with "/") and we have a
    // workspace-relative path, try matching the relative portion against the
    // pattern.  This lets `cat pkg/**\/*.go` permit reads of workspace files
    // that the SDK delivers as absolute paths to the permission handler.
    if (relativeRequestedPath !== undefined && !normalizedPattern.startsWith("/")) {
      if (relativeRequestedPath === normalizedPattern) {
        return true;
      }
      return path.posix.matchesGlob(relativeRequestedPath, normalizedPattern);
    }
    return false;
  });
}

/**
 * Build an SDK on-permission handler from Copilot CLI allow-tool rules.
 * A handler is always returned so session creation consistently wires explicit
 * permission behavior derived from configuration input.
 *
 * @param {CopilotSDKPermissionConfig | undefined} permissionConfig
 * @param {import("@github/copilot-sdk").PermissionHandler} approveAll
 * @param {{coreLogger?: CopilotSDKCoreLogger, logger?: (msg: string) => void, onDenied?: (requestSummary: string) => void, workspaceRoot?: string}=} logOptions
 * @returns {import("@github/copilot-sdk").PermissionHandler}
 */
function buildCopilotSDKPermissionHandler(permissionConfig, approveAll, logOptions) {
  const logger = logOptions?.logger ?? (() => {});

  const allowAll = permissionConfig?.allowAllTools === true;
  const allowedTools = Array.isArray(permissionConfig?.allowedTools) ? permissionConfig.allowedTools : [];
  const normalizedAllowedTools = allowedTools
    .filter(tool => typeof tool === "string")
    .map(tool => tool.trim())
    .filter(tool => tool.length > 0);
  const allowedToolEntries = new Set(normalizedAllowedTools);
  const hasReadGrant = normalizedAllowedTools.some(tool => {
    const lower = tool.toLowerCase();
    return lower === "read" || lower.startsWith("read(") || lower === "read:*";
  });

  // Keep explicit allow-all behavior when requested by config input.
  if (allowAll || allowedToolEntries.size === 0) {
    return approveAll;
  }

  const shellRules = [...allowedToolEntries]
    .filter(tool => tool.startsWith("shell(") && tool.endsWith(")"))
    .map(tool => tool.slice("shell(".length, -1).trim())
    .filter(Boolean);
  const readablePathPatterns = shellRules.flatMap(extractReadablePathPatternsFromShellRule);

  /**
   * Returns true when `segmentText` is exactly `prefix`, or begins with `prefix`
   * followed by a word boundary (a space). Used to match multiword `:*` rule
   * prefixes (for example `"git checkout:*"`) against a full pipeline segment
   * such as `"git checkout -b automation/repro"`, since a subcommand like
   * `checkout` cannot be captured by a single executable-name identifier.
   *
   * @param {string} segmentText
   * @param {string} prefix
   * @returns {boolean}
   */
  function segmentStartsWithPrefix(segmentText, prefix) {
    if (!prefix) return false;
    if (/[`]|[$][(]|[<>][(]|&/.test(segmentText)) return false;
    const normalizedSegment = segmentText.trim().replace(/\s+/g, " ");
    const normalizedPrefix = prefix.trim().replace(/\s+/g, " ");
    return normalizedSegment === normalizedPrefix || normalizedSegment.startsWith(`${normalizedPrefix} `);
  }

  /**
   * @param {string} segmentText
   * @returns {boolean}
   */
  function isNonExecutableShellSegment(segmentText) {
    if (/[$][(]|[`]|[<>][(]/.test(segmentText)) return false;
    if (/^(?:[A-Za-z_][A-Za-z0-9_]*=(?:"(?:[^"\\]|\\.)*"|'[^']*'|\S*)\s*)+$/.test(segmentText)) return true;
    if (/^(?:for|select)\s+[A-Za-z_][A-Za-z0-9_]*(?:\s+in(?:\s+.*)?)?$/.test(segmentText)) return true;
    if (/^case\s+.+\s+in$/.test(segmentText)) return true;
    return /^(?:then|else|do|fi|done|esac|\{|\})$/.test(segmentText);
  }

  /**
   * Returns true if a single pipeline segment (a full command/subcommand
   * invocation, e.g. `"git checkout -b automation/repro"`) is allowed by any
   * shell rule.
   *
   * Matches multiword `:*` prefixes (rules whose prefix itself contains a
   * space, such as `"git checkout:*"` or `"git branch:*"`) against the segment
   * text, single-word/`:*` rules against the segment's executable name, and
   * exact full-command rules (rules containing a space with no `:*` suffix)
   * against the segment text as a whole. This is required regardless of what
   * the SDK reports as the command `identifier` for the segment: the SDK may
   * report only the executable name (e.g. `"git"`), the entire segment text,
   * or no identifier at all — none of which reliably exposes the subcommand
   * word needed to test a multiword prefix.
   *
   * @param {string} segmentText - A single shell segment (no pipeline operators)
   * @returns {boolean} True when any shell rule permits the segment
   */
  function isSegmentAllowedByShellRules(segmentText) {
    const trimmedSegment = String(segmentText || "").trim();
    if (!trimmedSegment) return false;
    const name = extractCommandNamesFromPipeline(trimmedSegment)[0];
    if (!name) {
      const keywordMatch = trimmedSegment.match(/^(if|while|until|time|coproc)\b\s*(.*)$/s);
      if (keywordMatch) {
        return keywordMatch[2].length > 0 && isSegmentAllowedByShellRules(keywordMatch[2]);
      }
      return isNonExecutableShellSegment(trimmedSegment);
    }
    return shellRules.some(rule => {
      if (rule.endsWith(":*")) {
        const prefix = rule.slice(0, -2).trim();
        if (!prefix) return false;
        if (prefix.includes(" ")) {
          return segmentStartsWithPrefix(trimmedSegment, prefix);
        }
        return name === prefix;
      }
      if (!rule.includes(" ")) {
        return name === rule;
      }
      return false;
    });
  }

  /**
   * @param {import("@github/copilot-sdk").PermissionRequest} request
   * @returns {boolean}
   */
  function isAllowed(request) {
    switch (request.kind) {
      case "shell": {
        if (allowedToolEntries.has("shell")) return true;
        const fullCommand = String(request.fullCommandText || "").trim();

        if (shellRules.some(rule => rule.includes(" ") && !rule.endsWith(":*") && fullCommand === rule)) {
          return true;
        }

        // Primary path: split the full command text into pipeline segments and
        // verify that every segment is individually allowed. This is preferred
        // over the SDK-provided `commands[].identifier` values because those
        // identifiers are unreliable for matching multiword `:*` prefixes
        // (for example `"git checkout:*"`): the SDK may report only the
        // executable name (`"git"`), the entire segment text, or no identifier
        // at all — none of which reliably exposes the subcommand word
        // (`"checkout"`) needed to test the prefix. Parsing fullCommandText
        // ourselves keeps command-chain matching (all stages must be
        // individually allowed) and multiword-prefix matching consistent.
        if (fullCommand) {
          const segments = splitOnPipelineOperators(fullCommand);
          if (segments.length > 0) {
            return segments.every(segment => isSegmentAllowedByShellRules(segment));
          }
          // Could not split into segments (e.g. complex subshell-only command).
          // Last resort: try an exact full-command match against rules with spaces.
          return shellRules.some(rule => rule.includes(" ") && !rule.endsWith(":*") && fullCommand === rule);
        }

        // Fallback path: no fullCommandText was provided at all. Fall back to
        // whatever identifiers the SDK did supply, matching each against the
        // single-word/`:*` rule forms (multiword prefixes cannot be tested
        // without the full command text).
        const commandIdentifiers = Array.isArray(request.commands) ? request.commands.map(cmd => cmd?.identifier).filter(Boolean) : [];
        const normalizedCommandIdentifiers = [...new Set(commandIdentifiers.map(identifier => String(identifier || "").trim()).filter(Boolean))];
        if (normalizedCommandIdentifiers.length > 0) {
          return normalizedCommandIdentifiers.every(identifier => {
            const segments = splitOnPipelineOperators(identifier);
            return (segments.length > 0 ? segments : [identifier]).every(segment => isSegmentAllowedByShellRules(segment));
          });
        }

        return false;
      }
      case "write":
        return allowedToolEntries.has("write");
      case "read":
        // Any read grant (read, read(...), read:*) is path-agnostic in Copilot SDK.
        // Always allow reads for paths at or under the workspace root (GITHUB_WORKSPACE).
        // Every workflow runs inside its own checkout and must be able to read its source tree
        // regardless of any narrower tool-permission scoping configured elsewhere.
        //
        // Use path.resolve + path.relative for containment rather than a string-prefix check so
        // that ".." traversal paths (e.g. workspace/../../../../etc/passwd) are rejected and
        // relative paths (e.g. "AGENTS.md") are correctly resolved inside the workspace.
        if (logOptions?.workspaceRoot && typeof request.path === "string" && request.path.length > 0) {
          const resolvedWorkspace = path.resolve(logOptions.workspaceRoot);
          const resolvedPath = path.isAbsolute(request.path) ? path.resolve(request.path) : path.resolve(resolvedWorkspace, request.path);
          const rel = path.relative(resolvedWorkspace, resolvedPath);
          if (rel === "" || (!rel.startsWith("..") && !path.isAbsolute(rel))) {
            return true;
          }
        }
        return hasReadGrant || allowedToolEntries.has("shell") || isReadPathAllowedByShellRules(request.path, readablePathPatterns, logOptions?.workspaceRoot);
      case "url":
        return allowedToolEntries.has("web_fetch");
      case "mcp":
        // Server-only entries (for example: "github") allow all tools from that server.
        // Server+tool entries (for example: "github(get_file_contents)") allow only that tool.
        return allowedToolEntries.has(request.serverName) || allowedToolEntries.has(`${request.serverName}(${request.toolName})`);
      case "custom-tool":
        return allowedToolEntries.has(request.toolName);
      default:
        return false;
    }
  }

  return request => {
    if (isAllowed(request)) {
      return { kind: "approve-once" };
    }
    const requestSummary = summarizePermissionRequest(request);
    logPermissionDenied(logOptions?.coreLogger, logger, request);
    if (logOptions?.onDenied) {
      logOptions.onDenied(requestSummary);
    }
    return { kind: "reject", feedback: "Tool invocation is not allowed by workflow tool permissions." };
  };
}

/**
 * Parse a CopilotSDKPermissionConfig from a JSON-encoded sidecar args array.
 *
 * Extracts --allow-tool values and the --allow-all-tools flag from the raw
 * GH_AW_COPILOT_SDK_SERVER_ARGS string that the Go engine writes. Returns
 * undefined when no permission-related flags are present so the session
 * on-permission handler can interpret config absence as unrestricted behavior.
 *
 * @param {string | undefined} serverArgsJson - Raw JSON value of GH_AW_COPILOT_SDK_SERVER_ARGS
 * @returns {CopilotSDKPermissionConfig | undefined}
 */
function parsePermissionConfigFromServerArgs(serverArgsJson) {
  if (!serverArgsJson) {
    return undefined;
  }
  /** @type {unknown} */
  let parsed;
  try {
    parsed = JSON.parse(serverArgsJson);
  } catch {
    return undefined;
  }
  if (!Array.isArray(parsed)) {
    return undefined;
  }
  const args = /** @type {unknown[]} */ parsed;

  // --allow-all-tools takes precedence: the sidecar was launched with blanket
  // tool approval, so the driver should mirror that policy.
  if (args.includes("--allow-all-tools")) {
    return { allowAllTools: true };
  }

  // Collect the value of every --allow-tool <entry> pair.
  /** @type {string[]} */
  const allowedTools = [];
  for (let i = 0; i < args.length - 1; i++) {
    if (args[i] === "--allow-tool" && typeof args[i + 1] === "string") {
      allowedTools.push(/** @type {string} */ args[i + 1]);
      i += 1; // consume the value so it is not re-examined as a flag
    }
  }

  return allowedTools.length > 0 ? { allowedTools } : undefined;
}

module.exports = {
  MAX_TOOL_DENIALS_DEFAULT,
  parseStrictPositiveInteger,
  parseMaxToolDenialsLimit,
  getEnvPositiveIntOrDefault,
  summarizePermissionRequest,
  logPermissionDenied,
  normalizePermissionPath,
  extractReadablePathPatternsFromShellRule,
  isReadPathAllowedByShellRules,
  buildCopilotSDKPermissionHandler,
  parsePermissionConfigFromServerArgs,
};
