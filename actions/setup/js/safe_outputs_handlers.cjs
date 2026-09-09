// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const os = require("os");
const path = require("path");
const crypto = require("crypto");

const { normalizeBranchName } = require("./normalize_branch_name.cjs");
const { estimateTokens } = require("./estimate_tokens.cjs");
const { writeLargeContentToFile } = require("./write_large_content_to_file.cjs");
const { getCurrentBranch } = require("./get_current_branch.cjs");
const { getBaseBranch } = require("./get_base_branch.cjs");
const { lookupCheckout } = require("./checkout_manifest.cjs");
const { generateGitPatch } = require("./generate_git_patch.cjs");
const { generateGitBundle } = require("./generate_git_bundle.cjs");
const { hasMergeCommitsInRange, execGitSync, ensureSafeDirectoryTrust } = require("./git_helpers.cjs");
const { enforceCommentLimits } = require("./comment_limit_helpers.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_CONFIG, ERR_PARSE, ERR_SYSTEM, ERR_VALIDATION } = require("./error_codes.cjs");
const { findRepoCheckout } = require("./find_repo_checkout.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { generateTemporaryId, getOrGenerateTemporaryId } = require("./temporary_id.cjs");
const { parseAllowedExtensionsEnv } = require("./allowed_extensions_helpers.cjs");
const { getStagedPatchDiffSizeBytes } = require("./git_patch_utils.cjs");
const { sanitizeTitle, applyTitlePrefix } = require("./sanitize_title.cjs");
const { parseDeduplicateByTitle, normalizeTitleForDedup, findDuplicateByTitle } = require("./issue_title_dedup.cjs");
const { validateCreatePullRequestIntent, validatePushToPullRequestBranchIntent, validateCreateIssueIntent, validateAddCommentIntent } = require("./intent_probe.cjs");
const { globPatternToRegex } = require("./glob_pattern_helpers.cjs");
const { resolveInvocationContext } = require("./invocation_context_helpers.cjs");
const { lstatGuard } = require("./symlink_guard.cjs");
const { validateValueAgainstSchema } = require("./mcp_scripts_validation.cjs");
const { resolveDataSchema } = require("./data_schema_normalizer.cjs");
const { clearValidationMarker, formatJSONFiles, runCustomMemoryValidation, writeValidationMarker } = require("./memory_custom_validation.cjs");
const { filterIneligibleMemoryFiles } = require("./memory_file_eligibility.cjs");

/** PR event names used for target:triggering context validation across all safe-output handlers. */
const PR_EVENT_NAMES = new Set(["pull_request", "pull_request_target", "pull_request_review", "pull_request_review_comment"]);

/**
 * Resolve effective event name and payload from an invocation context,
 * falling back to the raw GitHub Actions context.
 * @param {ReturnType<typeof resolveInvocationContext> | null | undefined} invocationContext
 * @param {any} rawContext
 */
function resolveEffectiveContext(invocationContext, rawContext) {
  return {
    effectiveEventName: invocationContext?.eventName || rawContext.eventName,
    effectivePayload: invocationContext?.eventPayload || rawContext.payload,
  };
}

function resolvePRHeadBaselineForPush(branchName, repoSlug, prNumber, server) {
  const baselineBranch = (process.env.GH_AW_PR_HEAD_BASE_BRANCH || "").trim();
  const baselineRepo = (process.env.GH_AW_PR_HEAD_BASE_REPO || "").trim();
  const baselineRef = (process.env.GH_AW_PR_HEAD_BASE_REF || "").trim();
  const baselineSha = (process.env.GH_AW_PR_HEAD_BASE_SHA || "").trim();
  const baselinePRNumber = (process.env.GH_AW_PR_HEAD_BASE_PR_NUMBER || "").trim();
  const headRepo = (process.env.GH_AW_PR_HEAD_REPO || "").trim();

  if (!baselineBranch || baselineBranch !== branchName || (!baselineRef && !baselineSha)) {
    return null;
  }

  if (baselineRepo && baselineRepo.toLowerCase() !== repoSlug.toLowerCase()) {
    server.debug(`Ignoring PR-head baseline for ${baselineBranch}: recorded repo ${baselineRepo} does not match target repo ${repoSlug}`);
    return null;
  }

  // Branch name + repo alone don't uniquely identify a fork PR: two forks can open
  // identically-named branches against the same base repo. In wildcard/batch workflows
  // where the target PR differs from the triggering PR, require the recorded PR number
  // to match the effective target PR before reusing the baseline.
  const effectivePRNumber = prNumber != null ? String(prNumber).trim() : "";
  if (!baselinePRNumber || !effectivePRNumber || effectivePRNumber !== baselinePRNumber) {
    server.debug(`Ignoring PR-head baseline for ${baselineBranch}: recorded PR #${baselinePRNumber || "unknown"} does not match target PR #${effectivePRNumber || "unknown"}`);
    return null;
  }

  server.debug(`Using recorded PR-head baseline for incremental push patch: ${baselineRef || baselineSha}`);
  return {
    ref: baselineRef,
    sha: baselineSha,
    headRepo,
  };
}

/**
 * Read and parse a JSON file.
 * @param {string} filePath
 * @returns {any}
 */
function readJSONFile(filePath) {
  let parsed;
  try {
    parsed = JSON.parse(fs.readFileSync(filePath, "utf8"));
  } catch (err) {
    throw new Error(`${ERR_PARSE}: ` + "Failed to parse JSON file " + filePath + ": " + getErrorMessage(err), { cause: err });
  }
  return parsed;
}

const safeOutputsTools = readJSONFile(path.join(__dirname, "safe_outputs_tools.json"));

const safeOutputsToolMap = new Map(safeOutputsTools.map(tool => [tool.name.replace(/-/g, "_"), tool]));

/**
 * @param {string} error
 * @returns {{content: Array<{type: "text", text: string}>, isError: true}}
 */
function buildIntentErrorResponse(error) {
  return {
    content: [
      {
        type: "text",
        text: JSON.stringify({
          result: "error",
          error,
        }),
      },
    ],
    isError: true,
  };
}

/**
 * Build an actionable missing temporary_id error for configured tools.
 * @param {string} toolName
 * @param {string} configKey
 * @returns {string}
 */
function buildMissingTemporaryIdError(toolName, configKey) {
  const temporaryIdExamples = {
    create_pull_request: "aw_pr1",
    create_issue: "aw_issue1",
  };
  const example = temporaryIdExamples[toolName] || "aw_item1";
  return `${toolName} requires 'temporary_id' when safe-outputs.${configKey}.require-temporary-id is enabled. Set temporary_id (for example "${example}") and retry.`;
}

/**
 * @param {Record<string, any>} safeOutputsConfig
 * @param {string} toolName
 * @returns {Record<string, any>}
 */
function getSafeOutputsToolConfig(safeOutputsConfig, toolName) {
  return safeOutputsConfig?.[toolName] || safeOutputsConfig?.[toolName.replace(/_/g, "-")] || {};
}

/**
 * @param {Record<string, any>} entry
 * @param {string[]} fieldNames
 * @returns {boolean}
 */
function hasExplicitTargetParameter(entry, fieldNames) {
  return fieldNames.some(field => entry[field] !== undefined && entry[field] !== null && String(entry[field]).trim() !== "");
}

/**
 * @param {string} toolName
 * @returns {{primary?: string, anyOf?: string[], allOf?: string[]} | null}
 */
function getWildcardTargetRequirement(toolName) {
  return safeOutputsToolMap.get(toolName)?.["x-safe-outputs-target-requirements"]?.["*"] || null;
}

/**
 * Returns true if `args` contains at least one meaningful field for update_pull_request:
 * a string `title`, a string `body`, or `update_branch === true`.
 * Mirrors the downstream requiresOneOf:title,body,update_branch validation in
 * safe_output_type_validator.cjs (which also excludes field === false from the count).
 * @param {Record<string, any> | null | undefined} args
 * @returns {boolean}
 */
function hasUpdatePullRequestFields(args) {
  const safeArgs = args || {};
  return typeof safeArgs.title === "string" || typeof safeArgs.body === "string" || safeArgs.update_branch === true;
}

/**
 * Split a combined title/body message when an agent incorrectly sends both values
 * in `title` separated by newlines.
 *
 * Supported examples:
 * - "My title\n\nMy body"
 * - "Title: My title\nBody: My body"
 *
 * @param {Record<string, any> | null | undefined} args
 * @returns {Record<string, any>}
 */
function normalizeCombinedTitleBodyArgs(args) {
  const safeArgs = { ...(args || {}) };
  if (typeof safeArgs.title !== "string") return safeArgs;
  // An explicitly supplied `body` (including an empty string) is a caller-provided
  // value and must never be overwritten by this recovery normalization.
  if (typeof safeArgs.body === "string") return safeArgs;

  const rawTitle = safeArgs.title.replace(/\r\n/g, "\n").trim();
  if (!rawTitle.includes("\n")) return safeArgs;

  const lines = rawTitle.split("\n");
  const firstLineIndex = lines.findIndex(line => line.trim().length > 0);
  if (firstLineIndex < 0) return safeArgs;

  const firstLine = lines[firstLineIndex];
  const remainingLines = lines.slice(firstLineIndex + 1);
  while (remainingLines.length > 0 && remainingLines[0].trim().length === 0) {
    remainingLines.shift();
  }
  if (remainingLines.length === 0) return safeArgs;

  // Only treat this as the labeled form (and strip the "Title:"/"Body:" prefixes)
  // when the first line actually starts with a "Title:" label. Otherwise a plain
  // split may have a body that legitimately starts with the literal text "Body:".
  const titleLabelMatch = firstLine.match(/^title\s*:\s*/i);
  const normalizedTitle = titleLabelMatch ? firstLine.slice(titleLabelMatch[0].length).trim() : firstLine.trim();
  if (!normalizedTitle) return safeArgs;

  const remainder = remainingLines.join("\n");
  const normalizedBody = titleLabelMatch ? remainder.replace(/^body\s*:\s*/i, "").trim() : remainder.trim();
  if (!normalizedBody) return safeArgs;

  safeArgs.title = normalizedTitle;
  safeArgs.body = normalizedBody;
  return safeArgs;
}

/**
 * Parse branch pattern configuration from array or comma-separated string.
 * @param {string[]|string|undefined} value
 * @returns {string[]}
 */
function parseAllowedBranchPatterns(value) {
  if (Array.isArray(value)) {
    return value.map(item => String(item).trim()).filter(Boolean);
  }
  if (typeof value === "string") {
    return value
      .split(",")
      .map(item => item.trim())
      .filter(Boolean);
  }
  return [];
}

/**
 * Parse trusted comment IDs supplied by workflow configuration.
 * @param {unknown} value
 * @returns {Set<string>}
 */
function parseAllowedCommentIds(value) {
  const values = [];
  const visit = item => {
    if (Array.isArray(item)) {
      for (const child of item) visit(child);
      return;
    }
    if (typeof item === "number" && Number.isInteger(item) && item > 0) {
      values.push(String(item));
      return;
    }
    if (typeof item !== "string") {
      return;
    }
    const trimmed = item.trim();
    if (!trimmed) {
      return;
    }
    if (trimmed.startsWith("[") || trimmed.startsWith('"')) {
      try {
        visit(JSON.parse(trimmed));
        return;
      } catch {
        // Fall through to delimiter parsing.
      }
    }
    for (const part of trimmed.split(/[,\s]+/)) {
      if (/^[1-9]\d*$/.test(part)) {
        values.push(part);
      }
    }
  };
  visit(value);
  return new Set(values);
}

/**
 * Validate an agent-supplied add_comment.comment_id against the trusted workflow allowlist.
 * Does not mutate the supplied entry; the caller is responsible for applying the normalized value.
 * @param {Record<string, any>} entry
 * @param {Record<string, any>} addCommentConfig
 * @returns {{error: {content: Array<{type: "text", text: string}>, isError: true}} | {error: null, commentId: number | undefined}}
 */
function validateAllowedAddCommentId(entry, addCommentConfig) {
  if (entry.comment_id === undefined || entry.comment_id === null || String(entry.comment_id).trim() === "") {
    return { error: null, commentId: undefined };
  }
  if (addCommentConfig.target !== "*") {
    return { error: buildIntentErrorResponse("add_comment comment_id is only allowed when safe-outputs.add-comment.target is '*' and the ID is listed in safe-outputs.add-comment.allows-comment-ids.") };
  }
  const commentId = Number(entry.comment_id);
  if (!Number.isInteger(commentId) || commentId <= 0) {
    return { error: buildIntentErrorResponse("add_comment comment_id must be a positive integer.") };
  }
  const allowedCommentIds = parseAllowedCommentIds(addCommentConfig.allows_comment_ids ?? addCommentConfig["allows-comment-ids"]);
  if (allowedCommentIds.size === 0) {
    return { error: buildIntentErrorResponse("add_comment comment_id requires safe-outputs.add-comment.allows-comment-ids to list trusted comment IDs.") };
  }
  if (!allowedCommentIds.has(String(commentId))) {
    return { error: buildIntentErrorResponse("add_comment comment_id is not listed in safe-outputs.add-comment.allows-comment-ids.") };
  }
  return { error: null, commentId };
}

/**
 * @param {string} branch
 * @param {string[]} allowedPatterns
 * @returns {boolean}
 */
function isAllowedBranch(branch, allowedPatterns) {
  for (const pattern of allowedPatterns) {
    if (branch === pattern) {
      return true;
    }
    if (pattern === "*") {
      // Add this fast-path
      return true;
    }
    if (pattern.includes("*") && globPatternToRegex(pattern, { pathMode: true, caseSensitive: true }).test(branch)) {
      return true;
    }
  }
  return false;
}

/**
 * Resolve and validate a workspace-relative patch path.
 * @param {string|undefined} workspacePath
 * @returns {{success: true, absolutePath: string} | {success: false, error: string}}
 */
function resolvePatchWorkspacePath(workspacePath) {
  const candidatePath = typeof workspacePath === "string" ? workspacePath.trim() : "";
  if (!candidatePath) {
    return { success: false, error: "patch_workspace_path is empty" };
  }
  const workspaceRoot = path.resolve(process.env.GITHUB_WORKSPACE || process.cwd());
  const resolved = path.resolve(workspaceRoot, candidatePath);
  const relative = path.relative(workspaceRoot, resolved);
  if (relative.startsWith("..") || path.isAbsolute(relative)) {
    return { success: false, error: `Invalid patch_workspace_path '${candidatePath}': path must stay under GITHUB_WORKSPACE` };
  }
  if (!fs.existsSync(resolved)) {
    return { success: false, error: `Invalid patch_workspace_path '${candidatePath}': directory does not exist` };
  }
  let resolvedStats;
  try {
    resolvedStats = fs.statSync(resolved);
  } catch (err) {
    return { success: false, error: `Failed to inspect patch_workspace_path '${candidatePath}': ${getErrorMessage(err)}` };
  }
  if (!resolvedStats.isDirectory()) {
    return { success: false, error: `Invalid patch_workspace_path '${candidatePath}': path is not a directory` };
  }
  return { success: true, absolutePath: resolved };
}

/**
 * Create handlers for safe output tools
 * @param {Object} server - The MCP server instance for logging
 * @param {Function} appendSafeOutput - Function to append entries to the output file
 * @param {Object} [config] - Optional configuration object with safe output settings
 * @returns {Object} An object containing all handler functions
 */
function createHandlers(server, appendSafeOutput, config = {}) {
  // Ensure the workspace is trusted for the lifetime of this server process.
  // This covers all current and future handlers automatically; per-handler calls
  // additionally cover per-request checkout paths that differ from GITHUB_WORKSPACE.
  ensureSafeDirectoryTrust(process.env.GITHUB_WORKSPACE || process.cwd(), server);

  const TOKEN_THRESHOLD = 16000;

  /**
   * Session-scoped per-type operation counters.
   * Incremented on every successful appendSafeOutput call (MCE4 dual enforcement).
   * @type {Map<string, number>}
   */
  const operationCounts = new Map();
  const uploadedAssetPaths = new Set();

  /**
   * Return the explicitly user-configured max for a safe-output type, or null if not set / unlimited.
   * Uses getSafeOutputsToolConfig for consistent key-normalisation (hyphens → underscores).
   * Does NOT fall back to validation-config defaults: MCP-time enforcement is only
   * applied when the user has explicitly set a limit; downstream enforcement covers defaults.
   * Per Safe Outputs Specification MCE5: the same config source as the processor.
   * @param {string} type - normalised safe-output type name (e.g. "add_comment")
   * @returns {number | null}
   */
  function getExplicitMax(type) {
    const toolConfig = getSafeOutputsToolConfig(config, type);
    if (!toolConfig || typeof toolConfig !== "object") return null;
    if (!("max" in toolConfig)) return null;
    const maxVal = toolConfig.max;
    if (maxVal === -1) return null; // -1 means unlimited
    if (typeof maxVal === "number" && Number.isInteger(maxVal) && maxVal > 0) {
      return maxVal;
    }
    return null;
  }

  /**
   * Enforce the per-type operation count limit at invocation time.
   * Throws a JSON-RPC -32602 error when the configured max has already been reached.
   * Per Safe Outputs Specification MCE4: Dual Enforcement — constraints MUST be
   * enforced at both invocation time (MCP server) and processing time (safe output
   * processor) to provide defence-in-depth.
   * @param {string} type - normalised safe-output type name
   */
  function enforcePerTypeMax(type) {
    const maxAllowed = getExplicitMax(type);
    if (maxAllowed === null) return; // no explicit limit configured
    const current = operationCounts.get(type) || 0;
    if (current >= maxAllowed) {
      throw {
        code: -32602,
        message: `E002: ${type} limit reached — ${current} of ${maxAllowed} already used this run`,
        data: {
          constraint: "max",
          type,
          limit: maxAllowed,
          guidance:
            `You have used all ${maxAllowed} ${type} operations for this run. ` +
            `Further ${type} calls will be ignored. Prioritize the most important items ` +
            `(e.g. consolidate multiple updates into one), or call noop. ` +
            `Note: other safe-output types have independent budgets, so applying one type ` +
            `without its companion type can leave inconsistent state.`,
        },
      };
    }
  }

  /**
   * Append a safe-output entry after enforcing the per-type max count.
   * Increments the session counter only after a successful write, mirroring the
   * approach used by inlineReviewCommentCount so that write errors do not advance
   * the counter.
   * Per Safe Outputs Specification MCE4: invocation-time half of dual enforcement.
   * @param {Record<string, any>} entry
   */
  const appendSafeOutputCounted = entry => {
    const type = entry?.type;
    if (type) enforcePerTypeMax(type);
    appendSafeOutput(entry);
    if (type) operationCounts.set(type, (operationCounts.get(type) || 0) + 1);
  };

  /**
   * Validate schema-declared explicit target parameters for wildcard-target tools.
   * @param {Record<string, any>} entry
   * @returns {{content: Array<{type: "text", text: string}>, isError: true} | null}
   */
  const validateWildcardTargetRequirement = entry => {
    const toolName = entry?.type;
    const requirement = getWildcardTargetRequirement(toolName);
    if (!requirement) {
      return null;
    }

    const toolConfig = getSafeOutputsToolConfig(config, toolName);
    if (toolConfig.target !== "*") {
      return null;
    }

    const configKey = toolName.replace(/_/g, "-");

    const anyOf = Array.isArray(requirement.anyOf) ? requirement.anyOf : [];
    if (anyOf.length > 0 && !hasExplicitTargetParameter(entry, anyOf)) {
      const primary = requirement.primary || anyOf[0];
      const guidance = anyOf.length === 1 ? primary : `one of: ${anyOf.join(", ")}`;
      return buildIntentErrorResponse(`${toolName} requires ${primary} when safe-outputs.${configKey}.target is '*'. Provide ${guidance} and retry.`);
    }

    const allOf = Array.isArray(requirement.allOf) ? requirement.allOf : [];
    for (const field of allOf) {
      if (!hasExplicitTargetParameter(entry, [field])) {
        return buildIntentErrorResponse(`${toolName} requires ${field} when safe-outputs.${configKey}.target is '*'. Provide ${field} and retry.`);
      }
    }

    return null;
  };

  /**
   * Detect and offload large string fields to files.
   * @param {Record<string, any>} entry
   * @returns {Object | null} MCP response if large content was handled, else null
   */
  const maybeHandleLargeContent = entry => {
    /** @type {any} */
    let largeContent = null;
    /** @type {any} */
    let largeFieldName = null;

    for (const [key, value] of Object.entries(entry)) {
      if (typeof value === "string") {
        const tokens = estimateTokens(value);
        if (tokens > TOKEN_THRESHOLD) {
          largeContent = value;
          largeFieldName = key;
          server.debug(`Field '${key}' has ${tokens} tokens (exceeds ${TOKEN_THRESHOLD})`);
          break;
        }
      }
    }

    if (!largeContent || !largeFieldName) {
      return null;
    }

    const fileInfo = writeLargeContentToFile(largeContent);
    entry[largeFieldName] = `[Content too large, saved to file: ${fileInfo.filename}]`;
    appendSafeOutputCounted(entry);

    return {
      content: [
        {
          type: "text",
          text: JSON.stringify(fileInfo),
        },
      ],
    };
  };

  /**
   * Default handler for safe output tools
   * Spec cross-reference: Safe Output Outcome Evaluation §2/§4/§5/§6/§7/§8/§9/§10/§11/§12/§13/§14/§15/§16/§18/§19/§20/§21/§22/§23/§24/§25/§26/§27/§28/§29.
   * @param {string} type - The tool type
   * @returns {Function} Handler function
   */
  const defaultHandler = type => args => {
    const entry = { ...(args || {}), type };
    if (entry.data !== undefined) {
      const toolConfig = getSafeOutputsToolConfig(config, type);
      const dataEnabled = toolConfig?.data_enabled === true || (toolConfig?.data_schema && typeof toolConfig.data_schema === "object");
      if (!dataEnabled) {
        return buildIntentErrorResponse(`${type} data is not enabled (set safe-outputs.data in workflow frontmatter)`);
      }
      /** @type {Record<string, any>|null} */
      let dataSchema = null;
      try {
        if (toolConfig?.data_schema !== undefined) {
          dataSchema = resolveDataSchema(toolConfig.data_schema, `safe-outputs.${type}.data`);
        }
      } catch (error) {
        return buildIntentErrorResponse(`${type} data schema is invalid: ${getErrorMessage(error)}`);
      }
      if (dataSchema) {
        const dataSchemaError = validateValueAgainstSchema(entry.data, dataSchema);
        if (dataSchemaError) {
          const errorPath = dataSchemaError.path ? `.${dataSchemaError.path}` : "";
          return buildIntentErrorResponse(`${type} data${errorPath} ${dataSchemaError.message}`);
        }
      }
    }
    const wildcardTargetValidationError = validateWildcardTargetRequirement(entry);
    if (wildcardTargetValidationError) {
      return wildcardTargetValidationError;
    }
    const largeContentResponse = maybeHandleLargeContent(entry);
    if (largeContentResponse) return largeContentResponse;

    // Normal case - no large content
    appendSafeOutputCounted(entry);
    return {
      content: [
        {
          type: "text",
          text: JSON.stringify({ result: "success" }),
        },
      ],
    };
  };

  const createIssueConfig = config.create_issue || {};
  let deduplicateByTitle = { enabled: false, maxDistance: 0 };
  try {
    deduplicateByTitle = parseDeduplicateByTitle(createIssueConfig.deduplicate_by_title);
  } catch (error) {
    throw new Error(`${ERR_VALIDATION}: ${getErrorMessage(error)}`, { cause: error });
  }
  const createIssueTitlePrefix = createIssueConfig.title_prefix ?? "";
  /** @type {Map<string, Array<{title: string, normalizedTitle: string}>>} */
  const seenIssueTitlesByRepo = new Map();

  /**
   * Handler for upload_asset tool
   * Spec cross-reference: not part of the numbered outcome types in Safe Output Outcome Evaluation v1.0.0.
   */
  const uploadAssetHandler = args => {
    const branchName = process.env.GH_AW_ASSETS_BRANCH;
    if (!branchName) throw new Error(`${ERR_CONFIG}: GH_AW_ASSETS_BRANCH not set`);

    // Normalize the branch name to ensure it's a valid git branch name
    const normalizedBranchName = normalizeBranchName(branchName);

    const { path: filePath } = args;

    // Validate file path is within allowed directories
    const absolutePath = path.resolve(filePath);
    const workspaceDir = process.env.GITHUB_WORKSPACE || process.cwd();
    const tmpDir = "/tmp";

    const isInWorkspace = absolutePath.startsWith(path.resolve(workspaceDir));
    const isInTmp = absolutePath.startsWith(tmpDir);

    if (!isInWorkspace && !isInTmp) {
      throw new Error(`${ERR_CONFIG}: File path must be within workspace directory (${workspaceDir}) or /tmp directory. ` + `Provided path: ${filePath} (resolved to: ${absolutePath})`);
    }
    if (uploadedAssetPaths.has(absolutePath)) {
      throw new Error(`${ERR_VALIDATION}: Duplicate upload_asset source path is not allowed: ${filePath}`);
    }

    // Validate file exists
    if (!fs.existsSync(filePath)) {
      throw new Error(`${ERR_SYSTEM}: File not found: ${filePath}`);
    }

    // Get file stats
    let stats;
    try {
      stats = fs.statSync(filePath);
    } catch (err) {
      throw new Error(`${ERR_SYSTEM}: Failed to inspect file ${filePath}: ${getErrorMessage(err)}`, { cause: err });
    }
    const sizeBytes = stats.size;
    const sizeKB = Math.ceil(sizeBytes / 1024);

    // Check file size - read from environment variable if available
    const maxSizeKB = process.env.GH_AW_ASSETS_MAX_SIZE_KB ? parseInt(process.env.GH_AW_ASSETS_MAX_SIZE_KB, 10) : 10240; // Default 10MB
    if (sizeKB > maxSizeKB) {
      throw new Error(`${ERR_VALIDATION}: File size ${sizeKB} KB exceeds maximum allowed size ${maxSizeKB} KB`);
    }

    // Check file extension - read from environment variable if available
    const ext = path.extname(filePath).toLowerCase();
    const parsedAllowedExts = parseAllowedExtensionsEnv(process.env.GH_AW_ASSETS_ALLOWED_EXTS);
    if (parsedAllowedExts?.hasUnresolvedExpression) {
      throw new Error(`${ERR_CONFIG}: GH_AW_ASSETS_ALLOWED_EXTS contains unresolved GitHub Actions expression. Ensure expressions resolve before safe outputs validation.`);
    }
    const allowedExts = parsedAllowedExts
      ? parsedAllowedExts.normalizedValues
      : [
          // Default set as specified in problem statement
          ".png",
          ".jpg",
          ".jpeg",
        ];
    if (!allowedExts.includes(ext)) {
      throw new Error(`${ERR_VALIDATION}: File extension '${ext}' is not allowed. Allowed extensions: ${allowedExts.join(", ")}`);
    }

    // Create assets directory
    // Use RUNNER_TEMP so the staged files land on the host filesystem (shared with
    // the artifact-upload step), matching the same pattern used by upload_artifact.
    const assetsDir = path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "safeoutputs", "assets");
    if (!fs.existsSync(assetsDir)) {
      try {
        fs.mkdirSync(assetsDir, { recursive: true });
      } catch (err) {
        throw new Error(`${ERR_SYSTEM}: Failed to create directory ${assetsDir}: ${getErrorMessage(err)}`, { cause: err });
      }
    }

    // Read file and compute hash
    let fileContent;
    try {
      fileContent = fs.readFileSync(filePath);
    } catch (err) {
      throw new Error(`${ERR_SYSTEM}: Failed to read file ${filePath}: ${getErrorMessage(err)}`, { cause: err });
    }
    const sha = crypto.createHash("sha256").update(fileContent).digest("hex");

    // Extract filename and extension
    const fileName = path.basename(filePath);
    const fileExt = path.extname(fileName).toLowerCase();

    // Key the staged file by its declared source path so same-basename assets
    // cannot overwrite each other before the privileged publishing job.
    const stagedFileName = `${crypto.createHash("sha256").update(filePath).digest("hex")}${fileExt}`;
    const targetPath = path.join(assetsDir, stagedFileName);
    try {
      fs.copyFileSync(filePath, targetPath);
    } catch (err) {
      throw new Error(`${ERR_SYSTEM}: Failed to copy file ${filePath} to ${targetPath}: ${getErrorMessage(err)}`, { cause: err });
    }

    // Generate target filename as sha + extension (lowercased)
    const targetFileName = (sha + fileExt).toLowerCase();

    const githubServer = process.env.GITHUB_SERVER_URL || "https://github.com";
    const repo = process.env.GITHUB_REPOSITORY || "owner/repo";
    let url;
    try {
      const serverHostname = new URL(githubServer).hostname;
      if (serverHostname === "github.com") {
        url = `https://github.com/${repo}/blob/${normalizedBranchName}/${targetFileName}?raw=true`;
      } else {
        // GitHub Enterprise Server - raw content is served from the same host with /raw/ path
        url = `${githubServer}/${repo}/raw/${normalizedBranchName}/${targetFileName}`;
      }
    } catch {
      url = `${githubServer}/${repo}/raw/${normalizedBranchName}/${targetFileName}`;
    }

    // Create entry for safe outputs
    const entry = {
      type: "upload_asset",
      path: filePath,
      fileName: fileName,
      sha: sha,
      size: sizeBytes,
      url: url,
      targetFileName: targetFileName,
    };

    appendSafeOutputCounted(entry);
    uploadedAssetPaths.add(absolutePath);

    return {
      content: [
        {
          type: "text",
          text: JSON.stringify({ result: url }),
        },
      ],
    };
  };

  /**
   * Handler for create_pull_request tool
   * Spec cross-reference: Safe Output Outcome Evaluation §1 (`create_pull_request`).
   * Resolves the current branch if branch is not provided or is the base branch
   * Validates exploratory probe payloads against the resolved effective branch
   * Generates git patch for the changes (unless allow-empty is true)
   * Supports multi-repo scenarios via the optional 'repo' parameter
   */
  const createPullRequestHandler = async args => {
    /** @type {any} */
    const normalizedArgs = normalizeCombinedTitleBodyArgs(args);
    /** @type {any} */
    const entry = { ...normalizedArgs, type: "create_pull_request" };
    if (config.create_pull_request?.require_temporary_id === true && !entry.temporary_id) {
      return buildIntentErrorResponse(buildMissingTemporaryIdError("create_pull_request", "create-pull-request"));
    }

    // Resolve target repo configuration and validate the target repo early
    // This is needed before getBaseBranch to ensure we resolve the base branch
    // for the correct repository (especially in cross-repo scenarios)
    const prConfig = config.create_pull_request || {};
    const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(prConfig);

    // Resolve and validate the target repository from the entry
    const repoResult = resolveAndValidateRepo(entry, defaultTargetRepo, allowedRepos, "pull request");
    if (!repoResult.success) {
      let error = repoResult.error;
      const owningRepo = process.env.GITHUB_REPOSITORY;
      if (entry.repo === owningRepo && defaultTargetRepo && defaultTargetRepo !== owningRepo) {
        error += ` Hint: This workflow runs in '${owningRepo}' but is configured to target '${defaultTargetRepo}'. Omit the 'repo' parameter to use the configured target, or pass repo: '${defaultTargetRepo}'.`;
      }
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify({
              result: "error",
              error,
            }),
          },
        ],
        isError: true,
      };
    }
    const { repoParts } = repoResult;
    const configuredHeadRepo = typeof prConfig["head-repo"] === "string" ? prConfig["head-repo"].trim() : "";
    entry.head_repo = configuredHeadRepo || repoResult.repo;

    // Determine the working directory for git operations
    // If repo is specified or configured, find where it's checked out
    /** @type {any} */
    let repoCwd = null;
    /** @type {any} */
    let repoSlug = null;
    const patchWorkspacePath = typeof prConfig.patch_workspace_path === "string" ? prConfig.patch_workspace_path.trim() : "";
    const currentCheckoutRepo = typeof prConfig.current_checkout_repo === "string" ? prConfig.current_checkout_repo.trim() : "";
    const patchWorkspaceMatchesTargetRepo = patchWorkspacePath && (!currentCheckoutRepo || currentCheckoutRepo === repoResult.repo);

    if (patchWorkspaceMatchesTargetRepo) {
      const patchWorkspaceResult = resolvePatchWorkspacePath(patchWorkspacePath);
      if (!patchWorkspaceResult.success) {
        return {
          content: [
            {
              type: "text",
              text: JSON.stringify({
                result: "error",
                error: patchWorkspaceResult.error,
              }),
            },
          ],
          isError: true,
        };
      }
      repoCwd = patchWorkspaceResult.absolutePath;
      repoSlug = repoResult.repo;
      server.debug(`Using configured patch_workspace_path for create_pull_request: ${patchWorkspacePath} -> ${repoCwd}`);
    }

    if (((entry.repo && entry.repo.trim()) || prConfig["target-repo"]) && !repoCwd) {
      // Use the validated/qualified repo slug from repoResult to avoid divergence
      // between the raw user input and the normalized/qualified repo name
      repoSlug = repoResult.repo;
      server.debug(`Multi-repo mode: looking for checkout of ${repoSlug}`);

      const checkoutResult = findRepoCheckout(repoSlug);
      if (!checkoutResult.success) {
        server.debug(`Failed to find repo checkout: ${checkoutResult.error}`);
        return {
          content: [
            {
              type: "text",
              text: JSON.stringify({
                result: "error",
                error: checkoutResult.error,
                details:
                  `Repository '${repoSlug}' was not found as a git checkout in the workspace. ` +
                  `For multi-repo workflows, use actions/checkout with a 'path' parameter to checkout ` +
                  `each repo to a subdirectory (e.g., 'repos/repo-a/').`,
              }),
            },
          ],
          isError: true,
        };
      }

      repoCwd = checkoutResult.path;
      server.debug(`Found repo checkout at: ${repoCwd}`);
    }

    // Get base branch for the resolved target repository.
    // Priority:
    //   1. Explicit `base-branch` from the workflow config (no I/O, no fetch).
    //   2. Checkout manifest written by the workflow's setup phase (no network).
    //   3. Local origin/HEAD metadata + payload/API fallbacks via getBaseBranch.
    let baseBranch;
    const configuredBaseBranch = typeof prConfig.base_branch === "string" ? prConfig.base_branch.trim() : "";
    if (configuredBaseBranch) {
      baseBranch = configuredBaseBranch;
    } else {
      const manifestEntry = lookupCheckout(repoResult.repo);
      if (manifestEntry && manifestEntry.default_branch) {
        baseBranch = manifestEntry.default_branch;
        server.debug(`Using checkout-manifest default_branch for ${repoResult.repo}: ${baseBranch}`);
      } else {
        baseBranch = await getBaseBranch(repoParts, {
          preferLocalDefaultBranchMetadata: Boolean(repoCwd),
          cwd: repoCwd || undefined,
        });
      }
    }

    // Store the resolved base branch in the entry so the apply-time checkout step
    // can use it directly instead of inferring from event context.
    // This makes the safe output "self-describing" and fixes checkout for events
    // like issue_comment on PRs targeting non-default branches.
    entry.base_branch = baseBranch;

    // If branch is not provided, is empty, or equals the base branch, use the current branch from git
    // This handles cases where the agent incorrectly passes the base branch instead of the working branch
    if (!entry.branch || entry.branch.trim() === "" || entry.branch === baseBranch) {
      const detectedBranch = getCurrentBranch(repoCwd);

      if (entry.branch === baseBranch) {
        server.debug(`Branch equals base branch (${baseBranch}), detecting actual working branch: ${detectedBranch}`);
      } else {
        server.debug(`Using current branch for create_pull_request: ${detectedBranch}`);
      }

      entry.branch = detectedBranch;
    }

    // Reject if branch still equals base_branch after detection.
    // This means the base branch was incorrectly resolved (e.g., resolved to the
    // feature branch itself due to a confused event context). Writing a safe output
    // in this state would cause a cryptic git exit-1 in the safe_outputs job when
    // it tries to fetch a non-existent remote ref.
    if (entry.branch === entry.base_branch) {
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify({
              result: "error",
              error: `Branch '${entry.branch}' equals base_branch '${entry.base_branch}'. Cannot create a pull request from a branch into itself. Ensure 'branch' is your feature branch and that the base branch resolves to the target (e.g., 'main' or 'master').`,
            }),
          },
        ],
        isError: true,
      };
    }

    const allowedBranches = parseAllowedBranchPatterns(prConfig.allowed_branches);
    if (allowedBranches.length > 0 && !isAllowedBranch(entry.branch, allowedBranches)) {
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify({
              result: "error",
              error: `Branch '${entry.branch}' does not match allowed-branches. Allowed patterns: ${allowedBranches.join(", ")}`,
            }),
          },
        ],
        isError: true,
      };
    }

    const intentValidationError = validateCreatePullRequestIntent(entry);
    if (intentValidationError) {
      return buildIntentErrorResponse(intentValidationError);
    }

    // Check if allow-empty is enabled in configuration
    const allowEmpty = config.create_pull_request?.allow_empty === true;

    if (allowEmpty) {
      server.debug(`allow-empty is enabled for create_pull_request - skipping patch generation`);
      // Append the safe output entry without generating a patch
      appendSafeOutputCounted(entry);
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify({
              result: "success",
              message: "Pull request prepared (allow-empty mode - no patch generated)",
              branch: entry.branch,
            }),
          },
        ],
      };
    }

    // Determine transport format: "bundle" (default) uses git bundle (preserves merge topology),
    // "am" uses git format-patch / git am (good for linear histories).
    // Use ?? (nullish coalescing) so an empty-string resolved value is preserved and
    // rejected below rather than silently falling back to "bundle".
    const patchFormat = prConfig["patch_format"] ?? config["patch_format"] ?? "bundle";
    const validPatchFormats = ["am", "bundle"];
    if (!validPatchFormats.includes(patchFormat)) {
      const errorMsg = `Invalid patch_format in configuration. Must be one of: ${validPatchFormats.join(", ")}`;
      server.debug(`create_pull_request: ${errorMsg}`);
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify({
              result: "error",
              error: errorMsg,
            }),
          },
        ],
        isError: true,
      };
    }
    const useBundle = patchFormat === "bundle";

    // Build common options for both patch and bundle generation
    const transportOptions = {};
    if (repoCwd) {
      transportOptions.cwd = repoCwd;
    }
    if (repoSlug) {
      transportOptions.repoSlug = repoSlug;
    }
    // Pass per-handler token so cross-repo PATs are used for git fetch when configured.
    // Falls back to GITHUB_TOKEN if not set.
    if (prConfig["github-token"]) {
      transportOptions.token = prConfig["github-token"];
    }

    // SECURITY: Pin the branch ref to a SHA before generating any transport artifacts.
    // This prevents TOCTOU races where the agent flips the ref between patch and bundle
    // generation, causing the two to represent different commit sets.
    const gitCwd = repoCwd || process.env.GITHUB_WORKSPACE || process.cwd();
    ensureSafeDirectoryTrust(gitCwd, server);
    let pinnedSha;
    try {
      pinnedSha = execGitSync(["rev-parse", "--verify", `refs/heads/${entry.branch}^{commit}`], { cwd: gitCwd })
        .toString()
        .trim();
      server.debug(`Pinned branch '${entry.branch}' to SHA ${pinnedSha}`);
    } catch (pinError) {
      server.debug(`Failed to pin branch '${entry.branch}': ${getErrorMessage(pinError)}`);
      if (useBundle) {
        server.debug(`create_pull_request: proceeding without branch pinning for '${entry.branch}'; bundle generation will fall back to HEAD-based strategies when available`);
      }
      pinnedSha = null;
    }

    // Always generate a patch for policy enforcement (allowed-files/protected-files/excluded-files),
    // even when bundle transport is selected for apply-time commit transport.
    server.debug(`Generating patch for create_pull_request with branch: ${entry.branch}${repoCwd ? ` in ${repoCwd} baseBranch: ${baseBranch}` : ""}`);
    /** @type {Record<string, any>} */
    const patchOptions = { ...transportOptions };
    if (patchWorkspaceMatchesTargetRepo) {
      patchOptions.workspacePath = patchWorkspacePath;
    }
    // Pass excluded_files so git excludes them via :(exclude) pathspecs at generation time.
    if (Array.isArray(prConfig.excluded_files) && prConfig.excluded_files.length > 0) {
      patchOptions.excludedFiles = prConfig.excluded_files;
    }
    // Pass pinnedSha so patch generation uses the pinned commit, not a potentially-flipped ref
    if (pinnedSha) {
      patchOptions.pinnedSha = pinnedSha;
    }
    const patchResult = await generateGitPatch(entry.branch, baseBranch, patchOptions);

    if (!patchResult.success) {
      // Patch generation failed or patch is empty
      const errorMsg = patchResult.error || "Failed to generate patch";
      server.debug(`Patch generation failed: ${errorMsg}`);

      // Return error as content so the agent can see it, rather than throwing
      // which causes the tool call to fail silently in some MCP clients
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify({
              result: "error",
              error: errorMsg,
              details: "No commits were found to create a pull request. Make sure you have committed your changes using git add and git commit before calling create_pull_request.",
            }),
          },
        ],
        isError: true,
      };
    }

    // prettier-ignore
    server.debug(`Patch generated successfully: ${patchResult.patchPath} (${patchResult.patchSize} bytes, ${patchResult.patchLines} lines)`);

    // Patch/bundle paths are not transmitted via the safe-output entry: the
    // privileged safe_outputs job re-derives them from the (validated) branch name
    // using resolve_transport_paths.

    // Store the base commit SHA so the create_pull_request handler can use it
    // directly in the fallback path (the From <sha> header in format-patch output
    // contains the agent's commit SHA which won't exist in the target checkout)
    if (patchResult.baseCommit) {
      entry.base_commit = patchResult.baseCommit;
    }

    if (useBundle) {
      // Bundle transport: preserves merge commits and per-commit metadata
      server.debug(`Generating bundle for create_pull_request with branch: ${entry.branch}${repoCwd ? ` in ${repoCwd} baseBranch: ${baseBranch}` : ""}`);
      if (Array.isArray(prConfig.excluded_files) && prConfig.excluded_files.length > 0) {
        transportOptions.excludedFiles = prConfig.excluded_files;
      }
      const bundleResult = await generateGitBundle(entry.branch, baseBranch, transportOptions);

      if (!bundleResult.success) {
        const errorMsg = bundleResult.error || "Failed to generate bundle";
        server.debug(`Bundle generation failed: ${errorMsg}`);
        return {
          content: [
            {
              type: "text",
              text: JSON.stringify({
                result: "error",
                error: errorMsg,
                details: "No commits were found to create a pull request. Make sure you have committed your changes using git add and git commit before calling create_pull_request.",
              }),
            },
          ],
          isError: true,
        };
      }

      server.debug(`Bundle generated successfully: ${bundleResult.bundlePath} (${bundleResult.bundleSize} bytes)`);

      // SECURITY: Verify the branch ref hasn't been flipped between patch and bundle
      // generation (TOCTOU check). If the SHA changed, the bundle may contain different
      // commits than the patch used for file-protection policy enforcement.
      if (pinnedSha) {
        try {
          const currentSha = execGitSync(["rev-parse", "--verify", `refs/heads/${entry.branch}^{commit}`], { cwd: gitCwd })
            .toString()
            .trim();
          if (currentSha !== pinnedSha) {
            server.debug(`SECURITY: Branch '${entry.branch}' SHA changed during transport generation (was ${pinnedSha}, now ${currentSha}). Aborting.`);
            return {
              content: [
                {
                  type: "text",
                  text: JSON.stringify({
                    result: "error",
                    error: "Branch ref changed during transport artifact generation. This may indicate a concurrent modification. Please retry.",
                    details: `Branch '${entry.branch}' pointed to ${pinnedSha} at start but ${currentSha} after bundle generation.`,
                  }),
                },
              ],
              isError: true,
            };
          }
        } catch (verifyError) {
          server.debug(`SECURITY: Failed to verify branch SHA after bundle generation: ${getErrorMessage(verifyError)}`);
          return {
            content: [
              {
                type: "text",
                text: JSON.stringify({
                  result: "error",
                  error: `Failed to verify branch integrity after bundle generation: ${getErrorMessage(verifyError)}`,
                }),
              },
            ],
            isError: true,
          };
        }
      }

      // Bundle path is not transmitted via the safe-output entry: the privileged
      // safe_outputs job re-derives it from the (validated) branch name using
      // resolve_transport_paths.

      // Prefer the base_commit captured from format-patch generation (used by
      // patch-based fallback/apply paths). Only fall back to bundle base commit
      // when patch generation did not record one.
      if (!entry.base_commit && bundleResult.baseCommit) {
        entry.base_commit = bundleResult.baseCommit;
      }

      appendSafeOutputCounted(entry);
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify({
              result: "success",
              patch: {
                path: patchResult.patchPath,
                size: patchResult.patchSize,
                lines: patchResult.patchLines,
              },
              bundle: {
                path: bundleResult.bundlePath,
                size: bundleResult.bundleSize,
              },
            }),
          },
        ],
      };
    }

    appendSafeOutputCounted(entry);
    return {
      content: [
        {
          type: "text",
          text: JSON.stringify({
            result: "success",
            patch: {
              path: patchResult.patchPath,
              size: patchResult.patchSize,
              lines: patchResult.patchLines,
            },
          }),
        },
      ],
    };
  };

  /**
   * Handler for push_to_pull_request_branch tool
   * Spec cross-reference: Safe Output Outcome Evaluation §17 (`push_to_pull_request_branch`).
   * The agent SHOULD supply a `branch` argument identifying the local branch it
   * committed onto. This is required for batch workflows that loop over multiple PRs
   * and checkout different branches; without it, the source branch would be inferred
   * from the current git HEAD which may not match the PR being processed. When
   * `branch` is omitted, it is derived from the current checkout as a fallback.
   * The destination branch is independently derived by the apply-time push handler
   * from pulls.get(pull_number).head.ref.
   *
   * The recorded triggering-PR baseline includes its trusted head repository, so
   * contributor forks that cannot be written are rejected before persisting an
   * output. The apply-time handler still validates full PR data independently.
   */
  const pushToPullRequestBranchHandler = async args => {
    const entry = { ...(args || {}), type: "push_to_pull_request_branch" };
    const wildcardTargetValidationError = validateWildcardTargetRequirement(entry);
    if (wildcardTargetValidationError) {
      return wildcardTargetValidationError;
    }

    // Resolve target repo configuration and validate the target repo early
    // This is needed before getBaseBranch to ensure we resolve the base branch
    // for the correct repository (especially in cross-repo scenarios)
    const pushConfig = config.push_to_pull_request_branch || {};
    const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(pushConfig);

    // Resolve and validate the target repository from the entry
    const repoResult = resolveAndValidateRepo(entry, defaultTargetRepo, allowedRepos, "push to PR branch");
    if (!repoResult.success) {
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify({
              result: "error",
              error: repoResult.error,
            }),
          },
        ],
        isError: true,
      };
    }
    const { repoParts } = repoResult;
    const configuredHeadRepo = typeof pushConfig["head-repo"] === "string" ? pushConfig["head-repo"].trim() : "";
    entry.head_repo = configuredHeadRepo || repoResult.repo;

    // Determine the working directory for git operations.
    // Look up the checkout path when the target repo is explicitly provided by the agent
    // or explicitly configured via target-repo in the workflow config — this ensures patch
    // generation runs from the correct directory when the target repo is checked out in a subdirectory.
    /** @type {any} */
    let repoCwd = null;
    const itemRepo = repoResult.repo;
    const pushPatchWorkspacePath = typeof pushConfig.patch_workspace_path === "string" ? pushConfig.patch_workspace_path.trim() : "";
    const pushCurrentCheckoutRepo = typeof pushConfig.current_checkout_repo === "string" ? pushConfig.current_checkout_repo.trim() : "";
    const pushPatchWorkspaceMatchesTargetRepo = pushPatchWorkspacePath && (!pushCurrentCheckoutRepo || pushCurrentCheckoutRepo === itemRepo);

    if (pushPatchWorkspaceMatchesTargetRepo) {
      const patchWorkspaceResult = resolvePatchWorkspacePath(pushPatchWorkspacePath);
      if (!patchWorkspaceResult.success) {
        return {
          content: [
            {
              type: "text",
              text: JSON.stringify({
                result: "error",
                error: patchWorkspaceResult.error,
              }),
            },
          ],
          isError: true,
        };
      }
      repoCwd = patchWorkspaceResult.absolutePath;
      entry.repo_cwd = repoCwd;
      server.debug(`Using configured patch_workspace_path for push_to_pull_request_branch: ${pushPatchWorkspacePath} -> ${repoCwd}`);
    }

    const envTargetSlug = (process.env.GH_AW_TARGET_REPO_SLUG || "").trim();
    const currentRepo = (process.env.GITHUB_REPOSITORY || "").toLowerCase();
    const envSlugIsSideRepo = envTargetSlug && envTargetSlug.toLowerCase() !== currentRepo;
    if (envTargetSlug && !envSlugIsSideRepo) {
      server.debug(`GH_AW_TARGET_REPO_SLUG (${envTargetSlug}) matches current repo; not using as side-repo checkout hint for push_to_pull_request_branch`);
    }
    const hasExplicitTargetRepoHint = (entry.repo && entry.repo.trim()) || pushConfig["target-repo"] || envSlugIsSideRepo;
    if (hasExplicitTargetRepoHint && !repoCwd) {
      server.debug(`Looking for checkout of target repo: ${itemRepo}`);
      const checkoutResult = findRepoCheckout(itemRepo);
      if (!checkoutResult.success) {
        return {
          content: [
            {
              type: "text",
              text: JSON.stringify({
                result: "error",
                error: `Repository '${itemRepo}' not found in workspace. Check out the target repo with actions/checkout and set its 'path' input so the checkout can be located. If checking out multiple repositories, ensure each actions/checkout step uses the appropriate 'path' input.`,
              }),
            },
          ],
          isError: true,
        };
      }
      repoCwd = checkoutResult.path;
      entry.repo_cwd = repoCwd;
      server.debug(`Selected checkout folder for ${itemRepo}: ${repoCwd}`);
    }

    // Get base branch for the resolved target repository.
    // Priority:
    //   1. Explicit `base-branch` from the workflow config (no I/O, no fetch).
    //   2. Checkout manifest written by the workflow's setup phase (no network).
    //   3. Local origin/HEAD metadata in the side-repo checkout (when available).
    //   4. Payload / GitHub API fallbacks via getBaseBranch.
    let baseBranch;
    const configuredBaseBranch = typeof pushConfig.base_branch === "string" ? pushConfig.base_branch.trim() : "";
    if (configuredBaseBranch) {
      baseBranch = configuredBaseBranch;
      server.debug(`Using configured base_branch for push_to_pull_request_branch: ${baseBranch}`);
    } else {
      const manifestEntry = lookupCheckout(itemRepo);
      if (manifestEntry && manifestEntry.default_branch) {
        baseBranch = manifestEntry.default_branch;
        server.debug(`Using checkout-manifest default_branch for ${itemRepo}: ${baseBranch}`);
      } else {
        baseBranch = await getBaseBranch(repoParts, {
          preferLocalDefaultBranchMetadata: Boolean(repoCwd),
          cwd: repoCwd || undefined,
        });
      }
    }

    // Store the resolved base branch in the entry so the apply-time checkout step
    // can use it directly instead of inferring from event context.
    // This makes the safe output "self-describing" and fixes checkout for events
    // like issue_comment on PRs targeting non-default branches.
    entry.base_branch = baseBranch;

    // Use the agent-supplied branch if provided; fall back to the current checkout.
    // The agent-supplied value is authoritative: in multi-PR batch workflows the
    // working tree may be checked out to a different PR's branch by the time the
    // MCP handler runs, so relying solely on HEAD can produce a wrong source ref
    // and cause the apply step to fail with "couldn't find remote ref".
    // The apply-time push job re-derives the destination from pulls.get(pull_number)
    // independently, so this branch name is used only as the source ref for the
    // incremental diff against origin/<branch>.
    if (entry.branch && typeof entry.branch === "string" && entry.branch.trim()) {
      entry.branch = entry.branch.trim();
      server.debug(`Using agent-supplied branch for push_to_pull_request_branch: ${entry.branch}`);
    } else {
      // Fallback: derive from the current checkout (backward compat for single-PR workflows
      // where the working tree is reliably on the PR head ref at call time).
      try {
        const detectedBranch = getCurrentBranch(repoCwd);
        server.debug(`Using current branch for push_to_pull_request_branch: ${detectedBranch}`);
        entry.branch = detectedBranch;
      } catch (branchErr) {
        return {
          content: [
            {
              type: "text",
              text: JSON.stringify({
                result: "error",
                error: `Failed to determine source branch for push_to_pull_request_branch: ${getErrorMessage(branchErr)}. Either supply a 'branch' argument explicitly or ensure the working tree is checked out to the pull request's head ref before calling this tool.`,
              }),
            },
          ],
          isError: true,
        };
      }
    }

    // Reject if the detected branch equals base_branch. This means the workspace
    // is checked out on the PR's base (e.g. main) rather than the PR's head ref,
    // so there is nothing to push. Writing a safe output in this state would
    // cause a cryptic git exit-1 in the safe_outputs job when it tries to fetch
    // a non-existent remote ref.
    if (entry.branch === entry.base_branch) {
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify({
              result: "error",
              error: `Detected branch '${entry.branch}' equals base_branch '${entry.base_branch}'. The workspace is checked out on the base branch, not the pull request's head branch — there is nothing to push. Check out the PR's head ref and commit your changes there before calling push_to_pull_request_branch.`,
            }),
          },
        ],
        isError: true,
      };
    }

    const intentValidationError = validatePushToPullRequestBranchIntent(entry);
    if (intentValidationError) {
      return buildIntentErrorResponse(intentValidationError);
    }

    // Resolve the effective target PR number so a recorded PR-head baseline can be
    // validated against it: branch name + repo alone don't uniquely identify a fork PR
    // (two forks can open identically-named branches against the same base repo).
    let effectivePushPRNumber = entry.pull_request_number != null ? parseInt(String(entry.pull_request_number), 10) : undefined;
    if (effectivePushPRNumber == null || Number.isNaN(effectivePushPRNumber)) {
      const pushInvocationContext = resolveInvocationContext(context);
      const { effectivePayload: pushEffectivePayload } = resolveEffectiveContext(pushInvocationContext, context);
      effectivePushPRNumber = pushEffectivePayload?.pull_request?.number || pushEffectivePayload?.issue?.number || undefined;
    }

    const prHeadBaseline = resolvePRHeadBaselineForPush(entry.branch, itemRepo, effectivePushPRNumber, server);
    if (prHeadBaseline?.headRepo && prHeadBaseline.headRepo.toLowerCase() !== itemRepo.toLowerCase() && prHeadBaseline.headRepo.toLowerCase() !== configuredHeadRepo.toLowerCase()) {
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify({
              result: "error",
              error: `Cannot push to pull request #${effectivePushPRNumber}: its branch is in contributor fork '${prHeadBaseline.headRepo}', but safe-outputs.push-to-pull-request-branch.head-repo does not authorize that repository. A github-token or PAT alone does not authorize an unconfigured fork, even if the credential has write access. Configure head-repo as '${prHeadBaseline.headRepo}' with matching credentials to allow the push. Do not retry this push with the current configuration. If add_comment is available, comment on the pull request with the proposed code or patch; otherwise call report_incomplete with the proposed change and this error so the workflow can report it to the maintainer.`,
            }),
          },
        ],
        isError: true,
      };
    }

    // Build common options for both patch and bundle generation
    const pushTransportOptions = { mode: "incremental" };
    if (prHeadBaseline) {
      if (prHeadBaseline.ref) {
        pushTransportOptions.incrementalBaseRef = prHeadBaseline.ref;
      }
      if (prHeadBaseline.sha) {
        pushTransportOptions.incrementalBaseSha = prHeadBaseline.sha;
      }
    }
    if (repoCwd) {
      pushTransportOptions.cwd = repoCwd;
      pushTransportOptions.repoSlug = repoResult.repo;
    }
    // Pass per-handler token so cross-repo PATs are used for git fetch when configured.
    // Falls back to GITHUB_TOKEN if not set.
    if (pushConfig["github-token"]) {
      pushTransportOptions.token = pushConfig["github-token"];
    }

    // Determine transport format: "bundle" (default) uses git bundle (preserves merge topology),
    // "am" uses git format-patch / git am (good for linear histories).
    // Use ?? (nullish coalescing) so an empty-string resolved value is preserved and
    // rejected below rather than silently falling back to "bundle".
    // Track whether the user explicitly set patch_format so we can auto-fall-back
    // to bundle transport when merge commits are detected (since `git am` cannot
    // apply merge commits). When the user explicitly chose a format, respect it.
    const patchFormatExplicit = pushConfig["patch_format"] !== undefined || config["patch_format"] !== undefined;
    const pushPatchFormat = pushConfig["patch_format"] ?? config["patch_format"] ?? "bundle";
    const validPushPatchFormats = ["am", "bundle"];
    if (!validPushPatchFormats.includes(pushPatchFormat)) {
      const errorMsg = `Invalid patch_format in configuration. Must be one of: ${validPushPatchFormats.join(", ")}`;
      server.debug(`push_to_pull_request_branch: ${errorMsg}`);
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify({
              result: "error",
              error: errorMsg,
            }),
          },
        ],
        isError: true,
      };
    }
    let useBundle = pushPatchFormat === "bundle";

    // Auto-fallback: when patch_format is not explicitly configured and the
    // incremental range (origin/<branch>..<branch>) contains merge commits,
    // automatically switch to bundle transport. `git am` cannot
    // apply merge commits, so without this fallback long-running branches that
    // periodically merge their base branch locally would fail with add/add
    // conflicts on every push attempt. The detection is best-effort and uses
    // only local refs (no extra fetch); a detection miss simply preserves the
    // existing behavior.
    if (!useBundle && !patchFormatExplicit && entry.branch) {
      const rangeBaseRef = prHeadBaseline?.sha || prHeadBaseline?.ref || `refs/remotes/origin/${entry.branch}`;
      const hasMerges = hasMergeCommitsInRange(rangeBaseRef, entry.branch, { cwd: repoCwd || undefined });
      if (hasMerges) {
        server.debug(`push_to_pull_request_branch: detected merge commit(s) in incremental range ${rangeBaseRef}..${entry.branch}; auto-switching to bundle transport (set patch-format: am to override).`);
        useBundle = true;
      }
    }

    // SECURITY: Pin the branch ref to a SHA before generating any transport artifacts.
    // This prevents TOCTOU races where the agent flips the ref between patch and bundle
    // generation, causing the two to represent different commit sets.
    const pushGitCwd = repoCwd || process.env.GITHUB_WORKSPACE || process.cwd();
    ensureSafeDirectoryTrust(pushGitCwd, server);
    let pushPinnedSha;
    try {
      pushPinnedSha = execGitSync(["rev-parse", "--verify", `refs/heads/${entry.branch}^{commit}`], { cwd: pushGitCwd })
        .toString()
        .trim();
      server.debug(`Pinned branch '${entry.branch}' to SHA ${pushPinnedSha}`);
    } catch (pinError) {
      server.debug(`Failed to pin branch '${entry.branch}': ${getErrorMessage(pinError)}`);
      if (useBundle) {
        return {
          content: [
            {
              type: "text",
              text: JSON.stringify({
                result: "error",
                error: `Failed to pin branch '${entry.branch}' before bundle generation: ${getErrorMessage(pinError)}`,
                details: `Bundle transport requires branch pinning to prevent patch/bundle desynchronization. Retry after ensuring the branch exists locally (for example: git branch --list '${entry.branch}').`,
              }),
            },
          ],
          isError: true,
        };
      }
      pushPinnedSha = null;
    }

    // Full-branch allowed_files check: validate that ALL commits on the PR branch
    // (relative to origin/baseBranch) only touch files permitted by allowed_files.
    // The incremental patch check at apply-time only inspects the net diff between
    // origin/<branch> and the local branch tip; this catches disallowed files that
    // appear in earlier commits on the branch (e.g. a Copilot branch that also
    // modified .github/workflows/**) and returns an actionable error to the agent
    // before any transport artifacts are generated.
    if (Array.isArray(pushConfig.allowed_files) && pushConfig.allowed_files.length > 0) {
      try {
        // Use the pinned SHA as the range head to avoid any TOCTOU window between
        // the time the SHA was recorded and the time of the git log query.  If no
        // pinned SHA is available (e.g. non-bundle path), skip the check so we do
        // not race against a mutable ref; the apply-time check still enforces policy.
        if (!pushPinnedSha) {
          server.debug("Full-branch allowed-files check skipped: branch SHA not pinned (non-bundle path)");
        } else {
          const branchHistoryFiles = execGitSync(["log", "--name-only", "--pretty=format:", `origin/${baseBranch}..${pushPinnedSha}`, "--"], { cwd: pushGitCwd })
            .toString()
            .split("\n")
            .map(s => s.trim())
            .filter(Boolean);

          if (branchHistoryFiles.length > 0) {
            const allowedPatterns = pushConfig.allowed_files.map(p => globPatternToRegex(p));
            // Files matching excluded_files are intentionally exempt: they will be stripped
            // from the patch at generation time via :(exclude) pathspecs, so they won't be
            // present in the final changeset applied to the branch.
            const excludedPatterns = Array.isArray(pushConfig.excluded_files) ? pushConfig.excluded_files.map(p => globPatternToRegex(p)) : [];
            const uniqueFiles = [...new Set(branchHistoryFiles)];
            const disallowedFiles = uniqueFiles.filter(f => !allowedPatterns.some(re => re.test(f)) && !excludedPatterns.some(re => re.test(f)));

            if (disallowedFiles.length > 0) {
              const sample = disallowedFiles.slice(0, 5);
              const remaining = disallowedFiles.length - sample.length;
              const filesStr = remaining > 0 ? `${sample.join(", ")} (+${remaining} more)` : sample.join(", ");
              server.debug(`Full-branch allowed-files check failed: ${filesStr}`);
              return {
                content: [
                  {
                    type: "text",
                    text: JSON.stringify({
                      result: "error",
                      error: `Cannot push to pull request branch: the branch '${entry.branch}' history contains commits that modify files outside the allowed-files configuration: ${filesStr}. Remove the disallowed file changes from your commits and retry, or update the allowed-files configuration to include these files.`,
                      disallowed_files: disallowedFiles,
                    }),
                  },
                ],
                isError: true,
              };
            }
          }
        }
      } catch (fullBranchCheckError) {
        // Non-fatal: if origin/baseBranch is not available locally or git fails,
        // skip the full-branch check and continue.  The apply-time policy check in
        // push_to_pull_request_branch.cjs will still enforce allowed_files against
        // the incremental patch content.
        server.debug(`Full-branch allowed-files check skipped (non-fatal): ${getErrorMessage(fullBranchCheckError)}`);
      }
    }

    // Always generate an incremental patch for policy enforcement (allowed-files/protected-files/excluded-files),
    // even when bundle transport is selected for apply-time commit transport.
    server.debug(`Generating incremental patch for push_to_pull_request_branch with branch: ${entry.branch}, baseBranch: ${baseBranch}`);
    /** @type {Record<string, any>} */
    const pushPatchOptions = { ...pushTransportOptions };
    if (pushPatchWorkspaceMatchesTargetRepo) {
      pushPatchOptions.workspacePath = pushPatchWorkspacePath;
    }
    // Pass excluded_files so git excludes them via :(exclude) pathspecs at generation time.
    if (Array.isArray(pushConfig.excluded_files) && pushConfig.excluded_files.length > 0) {
      pushPatchOptions.excludedFiles = pushConfig.excluded_files;
    }
    // Pass pinnedSha so patch generation uses the pinned commit, not a potentially-flipped ref
    if (pushPinnedSha) {
      pushPatchOptions.pinnedSha = pushPinnedSha;
    }
    const patchResult = await generateGitPatch(entry.branch, baseBranch, pushPatchOptions);

    if (!patchResult.success) {
      // Patch generation failed or patch is empty
      const errorMsg = patchResult.error || "Failed to generate patch";
      server.debug(`Patch generation failed: ${errorMsg}`);

      // Return error as content so the agent can see it, rather than throwing
      // which causes the tool call to fail silently in some MCP clients
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify({
              result: "error",
              error: errorMsg,
              details: "No commits were found to push to the pull request branch. Make sure you have committed your changes using git add and git commit before calling push_to_pull_request_branch.",
            }),
          },
        ],
        isError: true,
      };
    }

    // prettier-ignore
    server.debug(`Patch generated successfully: ${patchResult.patchPath} (${patchResult.patchSize} bytes, ${patchResult.patchLines} lines, diffSize=${patchResult.diffSize ?? "(n/a)"} bytes)`);

    // Patch/bundle paths are not transmitted via the safe-output entry: the
    // privileged safe_outputs job re-derives them from the (validated) branch name
    // using resolve_transport_paths.

    // Store the base commit SHA so the push handler can use it directly
    if (patchResult.baseCommit) {
      entry.base_commit = patchResult.baseCommit;
    }

    // Store the incremental net diff size so push_to_pull_request_branch can
    // validate `max_patch_size` against the actual incremental change relative
    // to the existing PR branch head, not the (potentially much larger) size of
    // the format-patch transport file. This is critical for the long-running
    // branch pattern where the format-patch can include many
    // commits but each iteration only changes a few KB.
    if (typeof patchResult.diffSize === "number" && patchResult.diffSize >= 0) {
      entry.diff_size = patchResult.diffSize;
    }

    if (useBundle) {
      // Bundle transport: preserves merge commits and per-commit metadata
      server.debug(`Generating incremental bundle for push_to_pull_request_branch with branch: ${entry.branch}, baseBranch: ${baseBranch}`);
      if (Array.isArray(pushConfig.excluded_files) && pushConfig.excluded_files.length > 0) {
        pushTransportOptions.excludedFiles = pushConfig.excluded_files;
      }
      const bundleResult = await generateGitBundle(entry.branch, baseBranch, pushTransportOptions);

      if (!bundleResult.success) {
        const errorMsg = bundleResult.error || "Failed to generate bundle";
        server.debug(`Bundle generation failed: ${errorMsg}`);
        return {
          content: [
            {
              type: "text",
              text: JSON.stringify({
                result: "error",
                error: errorMsg,
                details: "No commits were found to push to the pull request branch. Make sure you have committed your changes using git add and git commit before calling push_to_pull_request_branch.",
              }),
            },
          ],
          isError: true,
        };
      }

      server.debug(`Bundle generated successfully: ${bundleResult.bundlePath} (${bundleResult.bundleSize} bytes)`);

      // SECURITY: Verify the branch ref hasn't been flipped between patch and bundle
      // generation (TOCTOU check).
      if (pushPinnedSha) {
        try {
          const currentSha = execGitSync(["rev-parse", "--verify", `refs/heads/${entry.branch}^{commit}`], { cwd: pushGitCwd })
            .toString()
            .trim();
          if (currentSha !== pushPinnedSha) {
            server.debug(`SECURITY: Branch '${entry.branch}' SHA changed during transport generation (was ${pushPinnedSha}, now ${currentSha}). Aborting.`);
            return {
              content: [
                {
                  type: "text",
                  text: JSON.stringify({
                    result: "error",
                    error: "Branch ref changed during transport artifact generation. This may indicate a concurrent modification. Please retry.",
                    details: `Branch '${entry.branch}' pointed to ${pushPinnedSha} at start but ${currentSha} after bundle generation.`,
                  }),
                },
              ],
              isError: true,
            };
          }
        } catch (verifyError) {
          server.debug(`SECURITY: Failed to verify branch SHA after bundle generation: ${getErrorMessage(verifyError)}`);
          return {
            content: [
              {
                type: "text",
                text: JSON.stringify({
                  result: "error",
                  error: `Failed to verify branch integrity after bundle generation: ${getErrorMessage(verifyError)}`,
                }),
              },
            ],
            isError: true,
          };
        }
      }

      // Bundle path is not transmitted via the safe-output entry: the privileged
      // safe_outputs job re-derives it from the (validated) branch name using
      // resolve_transport_paths.

      // Prefer the base_commit captured from format-patch generation (used by
      // patch-based fallback/apply paths). Only fall back to bundle base commit
      // when patch generation did not record one.
      if (!entry.base_commit && bundleResult.baseCommit) {
        entry.base_commit = bundleResult.baseCommit;
      }

      appendSafeOutputCounted(entry);
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify({
              result: "success",
              patch: {
                path: patchResult.patchPath,
                size: patchResult.patchSize,
                lines: patchResult.patchLines,
              },
              bundle: {
                path: bundleResult.bundlePath,
                size: bundleResult.bundleSize,
              },
            }),
          },
        ],
      };
    }

    appendSafeOutputCounted(entry);
    return {
      content: [
        {
          type: "text",
          text: JSON.stringify({
            result: "success",
            patch: {
              path: patchResult.patchPath,
              size: patchResult.patchSize,
              lines: patchResult.patchLines,
            },
          }),
        },
      ],
    };
  };

  /**
   * Handler for push_repo_memory tool
   * Spec cross-reference: not part of the numbered outcome types in Safe Output Outcome Evaluation v1.0.0.
   * Validates that memory files in the configured memory directory are within size limits.
   * Returns an error if any file or the total size exceeds the configured limits,
   * with guidance to reduce memory size before the workflow completes.
   */
  const pushRepoMemoryHandler = args => {
    const memoryId = (args && args.memory_id) || "default";
    const repoMemoryConfig = config.push_repo_memory;

    if (!repoMemoryConfig || !repoMemoryConfig.memories || repoMemoryConfig.memories.length === 0) {
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify({ result: "success", message: "No repo-memory configured." }),
          },
        ],
      };
    }

    // Find the memory config for the requested memory_id
    const memoryConf = repoMemoryConfig.memories.find(m => m.id === memoryId);
    if (!memoryConf) {
      const availableIds = repoMemoryConfig.memories.map(m => m.id).join(", ");
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify({
              result: "error",
              error: `Memory ID '${memoryId}' not found. Available memory IDs: ${availableIds}`,
            }),
          },
        ],
        isError: true,
      };
    }

    const memoryDir = memoryConf.dir;
    const maxFileSize = memoryConf.max_file_size || 10240;
    const maxPatchSize = memoryConf.max_patch_size || 10240;
    const maxFileCount = memoryConf.max_file_count || 100;
    const allowedExtensions = Array.isArray(memoryConf.allowed_extensions) ? memoryConf.allowed_extensions : [];
    const fileGlobFilter = typeof memoryConf.file_glob === "string" ? memoryConf.file_glob : "";
    const validationConfig = memoryConf.validation || null;
    const validationScript = validationConfig && typeof validationConfig.script === "string" ? validationConfig.script : "";
    const validationTimeoutSeconds = validationConfig && Number.isFinite(validationConfig.timeout) ? validationConfig.timeout : undefined;
    // The effective limit is max_patch_size × 1.2, matching the push gate in push_repo_memory.cjs.
    // This catches cases where total memory content is close to or exceeds the push diff limit.
    const effectiveMaxPatchSize = Math.floor(maxPatchSize * 1.2);

    if (!fs.existsSync(memoryDir)) {
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify({ result: "success", message: `Memory directory '${memoryDir}' does not exist yet. No files to validate.` }),
          },
        ],
      };
    }

    // Allowed-extensions and file-glob are persistence filters: ineligible files must be
    // removed here too, before formatting/scanning/staging/custom-validation, so this
    // preflight sees the same effective file set as the later filter/upload/push steps
    // and never hard-fails on a file that would have been silently dropped downstream.
    if (allowedExtensions.length > 0 || fileGlobFilter) {
      const { removed } = filterIneligibleMemoryFiles(memoryDir, allowedExtensions, fileGlobFilter, core);
      if (removed.length > 0) {
        core.info(`push_repo_memory: ignored ${removed.length} ineligible file(s) before validation`);
      }
    }

    clearValidationMarker("repo", memoryId);

    if (memoryConf.format_json === true) {
      try {
        const formattedFiles = formatJSONFiles(memoryDir, maxFileSize);
        if (formattedFiles.length > 0) {
          core.info(`Formatted ${formattedFiles.length} repo-memory JSON file(s) before validation: ${formattedFiles.join(", ")}`);
        }
      } catch (/** @type {any} */ error) {
        return {
          content: [
            {
              type: "text",
              text: JSON.stringify({
                result: "error",
                error: `Failed to format repo-memory JSON before validation: ${getErrorMessage(error)}`,
              }),
            },
          ],
          isError: true,
        };
      }
    }

    // Recursively scan all files in the memory directory
    /** @type {Array<{relativePath: string, size: number}>} */
    const files = [];

    /**
     * @param {string} dirPath
     * @param {string} relativePath
     */
    function scanDir(dirPath, relativePath) {
      let entries;
      try {
        entries = fs.readdirSync(dirPath, { withFileTypes: true });
      } catch (err) {
        throw new Error(`${ERR_SYSTEM}: Failed to read directory ${dirPath}: ${getErrorMessage(err)}`, { cause: err });
      }
      for (const entry of entries) {
        // Skip .git directory to avoid counting git metadata as memory content.
        // The memory directory is a git clone, so .git may contain pack files that
        // grow with each commit and must not be counted toward the memory size limit.
        if (entry.isDirectory() && entry.name === ".git") {
          continue;
        }
        const fullPath = path.join(dirPath, entry.name);
        const relPath = relativePath ? path.join(relativePath, entry.name) : entry.name;
        if (entry.isDirectory()) {
          scanDir(fullPath, relPath);
        } else if (entry.isFile()) {
          let stats;
          try {
            stats = fs.statSync(fullPath);
          } catch (err) {
            throw new Error(`${ERR_SYSTEM}: Failed to inspect file ${fullPath}: ${getErrorMessage(err)}`, { cause: err });
          }
          files.push({ relativePath: relPath.replace(/\\/g, "/"), size: stats.size });
        }
      }
    }

    try {
      scanDir(memoryDir, "");
    } catch (/** @type {any} */ error) {
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify({
              result: "error",
              error: `Failed to scan memory directory: ${getErrorMessage(error)}`,
            }),
          },
        ],
        isError: true,
      };
    }

    // Check individual file sizes
    const oversizedFiles = files.filter(f => f.size > maxFileSize);
    if (oversizedFiles.length > 0) {
      const details = oversizedFiles.map(f => `  - ${f.relativePath} (${f.size} bytes > ${maxFileSize} bytes limit)`).join("\n");
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify({
              result: "error",
              error:
                `${oversizedFiles.length} file(s) exceed the maximum file size of ${maxFileSize} bytes (${Math.ceil(maxFileSize / 1024)} KB):\n${details}\n\n` +
                `Please reduce the size of these files before the workflow completes. Consider summarizing or truncating the content.`,
            }),
          },
        ],
        isError: true,
      };
    }

    // Check file count
    if (files.length > maxFileCount) {
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify({
              result: "error",
              error: `Too many files in memory: ${files.length} files exceeds the limit of ${maxFileCount} files.\n\n` + `Please reduce the number of files in '${memoryDir}' before the workflow completes.`,
            }),
          },
        ],
        isError: true,
      };
    }

    const totalSize = files.reduce((sum, f) => sum + f.size, 0);
    const totalSizeKb = Math.ceil(totalSize / 1024);
    const effectiveMaxKb = Math.floor(effectiveMaxPatchSize / 1024);
    const maxPatchSizeKb = Math.floor(maxPatchSize / 1024);

    let patchSizeBytes;
    try {
      ensureSafeDirectoryTrust(memoryDir, server);
      execGitSync(["add", "--sparse", "."], { cwd: memoryDir, stdio: "pipe" });
      patchSizeBytes = getStagedPatchDiffSizeBytes({ execGitSyncFn: execGitSync, cwd: memoryDir });
    } catch (/** @type {any} */ error) {
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify({
              result: "error",
              error: `Failed to compute staged patch diff size for '${memoryDir}': ${getErrorMessage(error)}`,
            }),
          },
        ],
        isError: true,
      };
    }
    const patchSizeKb = Math.ceil(patchSizeBytes / 1024);

    core.debug(`push_repo_memory validation: ${files.length} files, total ${totalSize} bytes, patch diff ${patchSizeBytes} bytes, effective limit ${effectiveMaxPatchSize} bytes`);

    if (patchSizeBytes > effectiveMaxPatchSize) {
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify({
              result: "error",
              error:
                `Patch diff size (${patchSizeKb} KB, ${patchSizeBytes} bytes) exceeds the allowed limit of ${effectiveMaxKb} KB ` +
                `(${effectiveMaxPatchSize} bytes, configured max-patch-size: ${maxPatchSizeKb} KB / ${maxPatchSize} bytes with 20% overhead).\n\n` +
                `Please reduce the size of staged changes in '${memoryDir}' before the workflow completes. ` +
                `Then call push_repo_memory again to verify the patch size is within limits.`,
            }),
          },
        ],
        isError: true,
      };
    }

    /** @type {ReturnType<typeof runCustomMemoryValidation> | null} */
    let customValidation = null;
    if (validationConfig) {
      customValidation = runCustomMemoryValidation({
        script: validationScript,
        memoryDir,
        memoryId,
        kind: "repo",
        timeoutSeconds: validationTimeoutSeconds,
      });
      if (!customValidation.ok) {
        const reason = customValidation.timedOut ? `timed out after ${validationTimeoutSeconds || 30} second(s)` : `exited with code ${customValidation.exitCode}`;
        return {
          content: [
            {
              type: "text",
              text: JSON.stringify({
                result: "error",
                error: `Custom repo-memory validation failed for '${memoryId}': ${reason}.`,
                storage_validation: {
                  result: "success",
                  message: `Storage validation passed: ${files.length} file(s), ${totalSizeKb} KB total content, ${patchSizeKb} KB patch diff (${patchSizeBytes} bytes).`,
                },
                custom_validation: {
                  result: "error",
                  stdout: customValidation.stdout,
                  stderr: customValidation.stderr,
                },
              }),
            },
          ],
          isError: true,
        };
      }
    }

    const markerPath = writeValidationMarker("repo", memoryId);
    return {
      content: [
        {
          type: "text",
          text: JSON.stringify({
            result: "success",
            message:
              `Storage validation passed: ${files.length} file(s), ${totalSizeKb} KB total content, ` +
              `${patchSizeKb} KB patch diff (${patchSizeBytes} bytes) (limit: ${effectiveMaxKb} KB / ${effectiveMaxPatchSize} bytes).` +
              (customValidation ? " Custom domain validation passed." : ""),
            storage_validation: {
              result: "success",
              files: files.length,
              total_size_kb: totalSizeKb,
              patch_size_bytes: patchSizeBytes,
              effective_patch_limit_bytes: effectiveMaxPatchSize,
            },
            custom_validation: customValidation
              ? {
                  result: "success",
                  stdout: customValidation.stdout,
                  stderr: customValidation.stderr,
                }
              : undefined,
            validation_marker: markerPath,
          }),
        },
      ],
    };
  };

  /**
   * Handler for create_issue tool
   * Applies title-based within-run deduplication for immediate feedback.
   */
  const createIssueHandler = args => {
    const entry = { ...(args || {}), type: "create_issue" };
    if (createIssueConfig.require_temporary_id === true && !entry.temporary_id) {
      return buildIntentErrorResponse(buildMissingTemporaryIdError("create_issue", "create-issue"));
    }
    const intentValidationError = validateCreateIssueIntent(entry);
    if (intentValidationError) {
      return buildIntentErrorResponse(intentValidationError);
    }

    const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(createIssueConfig);
    const repoResult = resolveAndValidateRepo(entry, defaultTargetRepo, allowedRepos, "issue");
    if (!repoResult.success) {
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify({
              result: "error",
              error: repoResult.error,
            }),
          },
        ],
        isError: true,
      };
    }
    const resolvedRepo = repoResult.repo;

    let resolvedTitle = entry.title?.trim() || "";
    if (!resolvedTitle) {
      // Use the first non-empty line of the body as the title fallback rather than
      // the entire body, so the title stays concise and the body remains intact.
      const firstBodyLine = (entry.body || "")
        .split("\n")
        .map(l => l.replace(/^#+\s*/, "").trim())
        .find(l => l.length > 0);
      resolvedTitle = firstBodyLine || "Agent Output";
    }
    resolvedTitle = applyTitlePrefix(sanitizeTitle(resolvedTitle, createIssueTitlePrefix), createIssueTitlePrefix);

    if (deduplicateByTitle.enabled) {
      const normalizedTitle = normalizeTitleForDedup(resolvedTitle);
      const seenTitles = seenIssueTitlesByRepo.get(resolvedRepo) || [];
      const duplicate = findDuplicateByTitle(normalizedTitle, seenTitles, deduplicateByTitle.maxDistance);
      if (duplicate) {
        const droppedEntry = {
          ...entry,
          _dropped_duplicate_by_title: true,
          _dedup_source: "mcp-within-run",
          _duplicate_title: duplicate.title,
          _duplicate_distance: duplicate.distance,
        };
        const largeContentResponse = maybeHandleLargeContent(droppedEntry);
        if (!largeContentResponse) {
          appendSafeOutputCounted(droppedEntry);
        }
        return {
          content: [
            {
              type: "text",
              text: JSON.stringify({
                result: "duplicate_dropped",
                reason: `Duplicate create_issue title matched "${duplicate.title}" (distance=${duplicate.distance})`,
              }),
            },
          ],
        };
      }
      seenTitles.push({ title: resolvedTitle, normalizedTitle });
      seenIssueTitlesByRepo.set(resolvedRepo, seenTitles);
    }

    const largeContentResponse = maybeHandleLargeContent(entry);
    if (largeContentResponse) return largeContentResponse;

    appendSafeOutputCounted(entry);
    return {
      content: [
        {
          type: "text",
          text: JSON.stringify({ result: "success" }),
        },
      ],
    };
  };

  const createWorkItemHandler = args => {
    const temporaryId = `#${generateTemporaryId()}`;
    const entry = { ...(args || {}), type: "ado_create_work_item", temporary_id: temporaryId };
    appendSafeOutputCounted(entry);
    const output = { result: "success", temporary_id: temporaryId };
    return {
      content: [{ type: "text", text: JSON.stringify(output) }],
      structuredContent: output,
    };
  };

  const createAzureDevOpsWorkItemHandler = type => args => {
    const entry = { ...(args || {}), type };
    appendSafeOutputCounted(entry);
    return {
      content: [{ type: "text", text: JSON.stringify({ result: "success" }) }],
    };
  };

  const uploadWorkItemAttachmentHandler = args => {
    const entry = { ...(args || {}), type: "ado_upload_workitem_attachment" };
    const rawPath = typeof entry.file_path === "string" ? entry.file_path.trim() : "";
    if (!rawPath || path.isAbsolute(rawPath) || rawPath.includes(":")) {
      return buildIntentErrorResponse("ado_upload_workitem_attachment file_path must be a workspace-relative path without ':'");
    }

    const segments = rawPath.split(/[\\/]+/);
    if (segments.some(segment => !segment || segment === "." || segment === "..")) {
      return buildIntentErrorResponse("ado_upload_workitem_attachment file_path must not contain empty, '.' or '..' path segments");
    }

    const workspace = path.resolve(process.env.GITHUB_WORKSPACE || process.cwd());
    const sourcePath = path.resolve(workspace, ...segments);
    if (sourcePath !== workspace && !sourcePath.startsWith(workspace + path.sep)) {
      return buildIntentErrorResponse("ado_upload_workitem_attachment file_path resolves outside the workspace");
    }

    let current = workspace;
    let sourceStat;
    try {
      for (const segment of segments) {
        current = path.join(current, segment);
        sourceStat = lstatGuard(current);
        if (!sourceStat) {
          return buildIntentErrorResponse("ado_upload_workitem_attachment does not accept symbolic links");
        }
      }
    } catch (error) {
      return buildIntentErrorResponse(`ado_upload_workitem_attachment could not read file_path: ${getErrorMessage(error)}`);
    }
    if (!sourceStat?.isFile()) {
      return buildIntentErrorResponse("ado_upload_workitem_attachment file_path must identify one regular file");
    }

    const attachmentConfig = getSafeOutputsToolConfig(config, "ado_upload_workitem_attachment");
    const maxFileSize = Number(attachmentConfig.max_file_size || 5 * 1024 * 1024);
    if (!Number.isSafeInteger(maxFileSize) || maxFileSize < 1 || sourceStat.size > maxFileSize) {
      return buildIntentErrorResponse(`ado_upload_workitem_attachment file exceeds the configured max-file-size of ${maxFileSize} bytes`);
    }
    const allowedExtensions = Array.isArray(attachmentConfig.allowed_extensions) ? attachmentConfig.allowed_extensions : [];
    if (allowedExtensions.length > 0 && !allowedExtensions.some(extension => rawPath.toLowerCase().endsWith(String(extension).toLowerCase()))) {
      return buildIntentErrorResponse("ado_upload_workitem_attachment file extension is not allowed by the workflow configuration");
    }

    try {
      const stagingRoot = path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "safeoutputs", "upload-artifacts");
      const stagingDirectory = path.join(stagingRoot, "azure-devops-work-items");
      fs.mkdirSync(stagingDirectory, { recursive: true, mode: 0o700 });
      const stagedName = `${crypto.randomUUID()}-${segments[segments.length - 1]}`;
      const stagedPath = path.join(stagingDirectory, stagedName);
      fs.copyFileSync(sourcePath, stagedPath, fs.constants.COPYFILE_EXCL);
      fs.chmodSync(stagedPath, 0o600);
      entry.staged_file = path.posix.join("azure-devops-work-items", stagedName);
    } catch (error) {
      throw new Error(`${ERR_SYSTEM}: Failed to stage Azure DevOps work-item attachment: ${getErrorMessage(error)}`, { cause: error });
    }

    entry.file_path = rawPath;
    appendSafeOutputCounted(entry);
    return {
      content: [{ type: "text", text: JSON.stringify({ result: "success", file_path: rawPath }) }],
    };
  };

  /**
   * Handler for create_project tool
   * Spec cross-reference: not part of the numbered outcome types in Safe Output Outcome Evaluation v1.0.0.
   * Auto-generates a temporary ID if not provided and returns it to the agent
   */
  const createProjectHandler = args => {
    const entry = { ...(args || {}), type: "create_project" };

    // Use helper to validate or generate temporary_id
    const tempIdResult = getOrGenerateTemporaryId(entry, "create_project");
    if (tempIdResult.error) {
      throw {
        code: -32602,
        message: tempIdResult.error,
      };
    }
    entry.temporary_id = tempIdResult.temporaryId;
    server.debug(`temporary_id for create_project: ${entry.temporary_id}`);

    // Append to safe outputs
    appendSafeOutputCounted(entry);

    // Return the temporary_id to the agent so it can reference this project
    return {
      content: [
        {
          type: "text",
          text: JSON.stringify({
            result: "success",
            temporary_id: entry.temporary_id,
            project: `#${entry.temporary_id}`,
          }),
        },
      ],
    };
  };

  /**
   * Handler for add_comment tool
   * Spec cross-reference: Safe Output Outcome Evaluation §3 (`add_comment`).
   * Per Safe Outputs Specification MCE1: Enforces constraints during tool invocation
   * to provide immediate feedback to the LLM before recording to NDJSON
   * Also auto-generates a temporary_id if not provided and returns it to the agent
   */
  const addCommentHandler = args => {
    // Validate comment constraints before appending to safe outputs
    // This provides early feedback per Requirement MCE1 (Early Validation)
    try {
      const body = (args && args.body) || "";
      enforceCommentLimits(body);
    } catch (error) {
      // Return validation error with specific constraint violation details
      // Per Requirement MCE3 (Actionable Error Responses)
      // Use JSON-RPC error code -32602 (Invalid params) per MCP specification
      throw {
        code: -32602,
        message: getErrorMessage(error),
      };
    }

    // Refuse discussion-specific requests when discussions are not enabled in config.
    // reply_to_id is a discussion-only field; its presence unambiguously means the
    // agent is targeting a GitHub Discussion.  Guard here (MCP phase) so the agent
    // gets immediate, actionable feedback rather than a late failure at execution time.
    const addCommentConfig = getSafeOutputsToolConfig(config, "add_comment");
    const discussionsEnabled = addCommentConfig.discussions === true;
    const hasReplyToId = args?.reply_to_id != null && String(args.reply_to_id).trim() !== "";
    if (hasReplyToId && !discussionsEnabled) {
      return buildIntentErrorResponse(
        "add_comment with reply_to_id targets a GitHub Discussion, but discussion comments are not enabled for this workflow. " +
          "Set 'discussions: true' in the workflow's safe-outputs.add-comment configuration to enable discussion comments and request discussions:write permission."
      );
    }

    // Reject target:triggering early when no explicit item number and no issue/PR/discussion context.
    // Per Safe Outputs Specification MCE1: provides actionable feedback before writing to NDJSON.
    // Mirrors update_issue validation; explicit item_number bypasses this check because the
    // downstream handler resolves explicit numbers before falling back to triggering context.
    const effectiveAddCommentTarget = addCommentConfig.target || "triggering";
    const hasExplicitItemNumber = args?.item_number != null || args?.issue_number != null || args?.["pr-number"] != null;
    if (effectiveAddCommentTarget === "triggering" && !hasExplicitItemNumber) {
      /** @type {any} */
      let invocationContext = null;
      try {
        invocationContext = resolveInvocationContext(context);
      } catch (err) {
        // A validation error (e.g. disallowed target_repo / SEC-005) is a real failure — surface it.
        const errMsg = getErrorMessage(err);
        if (errMsg.startsWith(ERR_VALIDATION)) {
          return buildIntentErrorResponse(errMsg);
        }
        // Unexpected structural error: skip validation and let downstream handle gracefully.
      }
      if (invocationContext != null) {
        const { effectiveEventName, effectivePayload } = resolveEffectiveContext(invocationContext, context);
        const isIssueCommentOnPR = effectiveEventName === "issue_comment" && Boolean(effectivePayload?.issue?.pull_request);
        const isIssueContext = effectiveEventName === "issues" || (effectiveEventName === "issue_comment" && !isIssueCommentOnPR);
        const isPRContext = PR_EVENT_NAMES.has(effectiveEventName) || isIssueCommentOnPR;
        const isDiscussionContext = effectiveEventName === "discussion" || effectiveEventName === "discussion_comment";
        if (!isIssueContext && !isPRContext && !isDiscussionContext) {
          return buildIntentErrorResponse(
            `add_comment requires an issue, pull request, or discussion context but the workflow is running on a "${effectiveEventName}" event. ` +
              `The add-comment handler uses target: triggering which only applies when an issue, pull request, or discussion triggered the workflow. ` +
              `To report results from this workflow, use create_discussion or create_issue instead. ` +
              `If you need to comment on a specific item, provide an explicit item_number.`
          );
        }
      }
    }

    // Build the entry with a temporary_id
    const entry = { ...(args || {}), type: "add_comment" };
    const commentIdValidationResult = validateAllowedAddCommentId(entry, addCommentConfig);
    if (commentIdValidationResult.error) {
      return commentIdValidationResult.error;
    }
    if (commentIdValidationResult.commentId === undefined) {
      // entry was spread from args, so a blank/whitespace comment_id (rather than an
      // absent one) could still be sitting on entry; strip it so downstream code never
      // sees an unvalidated raw value.
      delete entry.comment_id;
    } else {
      entry.comment_id = commentIdValidationResult.commentId;
    }
    const wildcardTargetValidationError = validateWildcardTargetRequirement(entry);
    if (wildcardTargetValidationError) {
      return wildcardTargetValidationError;
    }
    const intentValidationError = validateAddCommentIntent(entry);
    if (intentValidationError) {
      return buildIntentErrorResponse(intentValidationError);
    }

    // Use helper to validate or generate temporary_id
    const tempIdResult = getOrGenerateTemporaryId(entry, "add_comment");
    if (tempIdResult.error) {
      throw {
        code: -32602,
        message: tempIdResult.error,
      };
    }
    entry.temporary_id = tempIdResult.temporaryId;
    server.debug(`temporary_id for add_comment: ${entry.temporary_id}`);

    // Append to safe outputs
    appendSafeOutputCounted(entry);

    // Return the temporary_id to the agent so it can reference this comment
    return {
      content: [
        {
          type: "text",
          text: JSON.stringify({
            result: "success",
            temporary_id: entry.temporary_id,
            comment: `#${entry.temporary_id}`,
          }),
        },
      ],
    };
  };

  /**
   * Session-scoped counter for buffered inline review comments.
   * Incremented by createPullRequestReviewCommentHandler, read by submitPullRequestReviewHandler
   * to guard against empty review submissions at the MCP server phase.
   */
  let inlineReviewCommentCount = 0;

  /**
   * Handler for create_pull_request_review_comment tool (MCP server phase).
   * Increments the session-scoped inline comment counter so that the subsequent
   * submitPullRequestReviewHandler can detect an otherwise-empty review.
   * Per Safe Outputs Specification MCE1: enforces constraints during tool invocation
   * to provide immediate feedback to the LLM before recording to NDJSON.
   */
  const createPullRequestReviewCommentHandler = args => {
    const result = defaultHandler("create_pull_request_review_comment")(args);
    // Increment only after the default handler returns successfully; if it throws
    // (e.g. due to large-content rejection or an append write error) the counter
    // must not advance so the empty-review guard remains accurate.
    if (!result?.isError) {
      inlineReviewCommentCount++;
    }
    return result;
  };

  /**
   * Handler for submit_pull_request_review tool (MCP server phase).
   * Validates the review before writing it to the NDJSON output so that the agent
   * receives an immediate MCP error rather than a silent 422 at finalization time.
   *
   * Checks performed:
   *  1. REQUEST_CHANGES requires a non-empty body (GitHub API requirement).
   *  2. If the review body is empty AND no inline comments were buffered during this
   *     session, the review would be contentless and GitHub would return 422 — reject
   *     early (mirrors Sub-pattern A guard in pr_review_buffer.cjs).
   *
   * Per Safe Outputs Specification MCE1: enforces constraints during tool invocation
   * to provide immediate feedback to the LLM before recording to NDJSON.
   */
  const submitPullRequestReviewHandler = args => {
    const body = (args && typeof args.body === "string" ? args.body : "").trim();
    const event = args && args.event ? String(args.event).toUpperCase() : "COMMENT";

    const VALID_REVIEW_EVENTS = ["APPROVE", "REQUEST_CHANGES", "COMMENT"];
    if (!VALID_REVIEW_EVENTS.includes(event)) {
      throw {
        code: -32602,
        message: `${ERR_VALIDATION}: submit_pull_request_review: invalid event '${args.event}'. Must be one of: ${VALID_REVIEW_EVENTS.join(", ")}`,
      };
    }

    if (event === "REQUEST_CHANGES" && !body) {
      throw {
        code: -32602,
        message: `${ERR_VALIDATION}: submit_pull_request_review: 'body' is required when event is REQUEST_CHANGES`,
      };
    }

    if (!body && inlineReviewCommentCount === 0) {
      throw {
        code: -32602,
        message:
          `${ERR_VALIDATION}: submit_pull_request_review: review body is empty and no ` +
          `create_pull_request_review_comment calls were made — GitHub would return 422 for a contentless review. ` +
          `Provide a non-empty 'body' or call create_pull_request_review_comment before submitting.`,
      };
    }

    // Reset the counter after a successful review submission so that subsequent
    // reviews in the same MCP session start with a clean slate.
    inlineReviewCommentCount = 0;

    return defaultHandler("submit_pull_request_review")(args);
  };

  /**
   * Handler for dismiss_pull_request_review tool (MCP server phase).
   * Enforces justification minimum length and actor-author consistency before recording.
   */
  const dismissPullRequestReviewHandler = args => {
    const justification = (args && typeof args.justification === "string" ? args.justification : "").trim();
    if (justification.length < 20) {
      throw {
        code: -32602,
        message: `${ERR_VALIDATION}: dismiss_pull_request_review: 'justification' must be at least 20 characters`,
      };
    }

    const actor = (process.env.GITHUB_ACTOR || context?.actor || "github-actions[bot]").trim() || "github-actions[bot]";
    const author = args && typeof args.author === "string" ? args.author.trim() : "";
    if (author && author !== actor) {
      throw {
        code: -32602,
        message: `${ERR_VALIDATION}: dismiss_pull_request_review: 'author' must match current workflow actor (${actor})`,
      };
    }

    return defaultHandler("dismiss_pull_request_review")(args);
  };

  /**
   * Resolve an allowed-root path to its canonical form, falling back to path.resolve when the
   * directory does not yet exist (e.g. GITHUB_WORKSPACE before checkout).
   * @param {string} root
   * @returns {string}
   */
  function canonicalizeAllowedRoot(root) {
    try {
      return fs.realpathSync(root);
    } catch {
      return path.resolve(root);
    }
  }

  /**
   * Validate that a canonical absolute path does not refer to sensitive system or credential
   * locations. Returns an error message string, or null if the path is safe.
   *
   * Rejected patterns:
   *  - Any path with a ".git" directory component (prevents .git/config leakage).
   *  - System directories: /etc, /proc, /sys, /dev, /run, /boot, /lib*, /usr/lib*.
   *  - HOME credential/config subtrees: .ssh, .aws, .netrc, .npmrc, .gitconfig, .gnupg,
   *    .config, .docker, .kube, .azure, .gcp.
   *
   * @param {string} canonicalPath - Resolved absolute path (output of fs.realpathSync or path.resolve)
   * @returns {string|null}
   */
  function validateUploadSourcePath(canonicalPath) {
    const parts = canonicalPath.split(path.sep);
    if (parts.some(p => p === ".git")) {
      return `path contains sensitive repository metadata (.git): ${canonicalPath}`;
    }

    const systemDenied = ["/etc", "/proc", "/sys", "/dev", "/run", "/boot", "/lib", "/lib64", "/usr/lib", "/usr/local/lib"];
    for (const denied of systemDenied) {
      const normalDenied = path.resolve(denied);
      if (canonicalPath === normalDenied || canonicalPath.startsWith(normalDenied + path.sep)) {
        return `path refers to a system directory: ${canonicalPath}`;
      }
    }

    const homeDir = os.homedir();
    if (homeDir) {
      const sensitiveNames = [".ssh", ".aws", ".gnupg", ".docker", ".kube", ".azure", ".gcp", ".config", ".netrc", ".npmrc", ".gitconfig", ".gitcredentials", ".git-credentials"];
      for (const name of sensitiveNames) {
        const sensitive = path.join(path.resolve(homeDir), name);
        if (canonicalPath === sensitive || canonicalPath.startsWith(sensitive + path.sep)) {
          return `path refers to a sensitive HOME location: ${canonicalPath}`;
        }
      }
    }

    return null;
  }

  /**
   * Recursively copy all regular files from srcDir into destDir, preserving the relative
   * path structure under srcDir. Non-regular entries (sockets, devices, pipes, symlinks)
   * are skipped silently.
   * @param {string} srcDir - Absolute source directory path
   * @param {string} destDir - Absolute destination directory path
   */
  function copyDirectoryRecursive(srcDir, destDir) {
    if (!fs.existsSync(destDir)) {
      try {
        fs.mkdirSync(destDir, { recursive: true });
      } catch (err) {
        throw new Error(`${ERR_SYSTEM}: Failed to create directory ${destDir}: ${getErrorMessage(err)}`, { cause: err });
      }
    }
    let entries;
    try {
      entries = fs.readdirSync(srcDir, { withFileTypes: true });
    } catch (err) {
      throw new Error(`${ERR_SYSTEM}: Failed to read directory ${srcDir}: ${getErrorMessage(err)}`, { cause: err });
    }
    for (const ent of entries) {
      const srcPath = path.join(srcDir, ent.name);
      const destPath = path.join(destDir, ent.name);
      if (ent.isDirectory()) {
        // Reject sensitive directory names at every level (e.g. foo/.git/config).
        let canonicalSrcPath;
        try {
          canonicalSrcPath = fs.realpathSync(srcPath);
        } catch (err) {
          throw new Error(`${ERR_SYSTEM}: Failed to resolve canonical path for ${srcPath}: ${getErrorMessage(err)}`, { cause: err });
        }
        const sensitiveErr = validateUploadSourcePath(canonicalSrcPath);
        if (sensitiveErr) {
          throw {
            code: -32602,
            message: `${ERR_VALIDATION}: upload_artifact: ${sensitiveErr}`,
          };
        }
        copyDirectoryRecursive(srcPath, destPath);
      } else if (ent.isFile() && !ent.isSymbolicLink() && !fs.existsSync(destPath)) {
        // Revalidate each file's canonical path before copying.
        let canonicalSrcPath;
        try {
          canonicalSrcPath = fs.realpathSync(srcPath);
        } catch (err) {
          throw new Error(`${ERR_SYSTEM}: Failed to resolve canonical path for ${srcPath}: ${getErrorMessage(err)}`, { cause: err });
        }
        const sensitiveErr = validateUploadSourcePath(canonicalSrcPath);
        if (sensitiveErr) {
          throw {
            code: -32602,
            message: `${ERR_VALIDATION}: upload_artifact: ${sensitiveErr}`,
          };
        }
        try {
          fs.copyFileSync(srcPath, destPath);
          fs.chmodSync(destPath, 0o600);
        } catch (err) {
          throw new Error(`${ERR_SYSTEM}: Failed to copy file ${srcPath} to ${destPath}: ${getErrorMessage(err)}`, { cause: err });
        }
      }
      // Skip symlinks, sockets, pipes, block/char devices — non-regular file types.
    }
  }

  /**
   * Handler for upload_artifact tool.
   * Spec cross-reference: not part of the numbered outcome types in Safe Output Outcome Evaluation v1.0.0.
   *
   * When the agent calls upload_artifact with an absolute path (e.g.,
   * /tmp/gh-aw/python/charts/loc_by_language.png), the file lives only inside the
   * sandboxed container.  After the container exits the file is gone, so the safe_outputs
   * job running on a different runner cannot find it.
   *
   * This handler copies the file (or directory) to the staging directory
   * ($RUNNER_TEMP/gh-aw/safeoutputs/upload-artifacts/), which is bind-mounted rw into
   * the container.  The agent job then uploads that staging directory as the
   * safe-outputs-upload-artifacts artifact, and the safe_outputs job downloads it before
   * processing.
   *
   * For path-based requests with an absolute path the handler also rewrites entry.path to
   * the staging-relative basename so that upload_artifact.cjs on the safe_outputs runner
   * resolves the file from staging rather than trying the (non-existent) absolute path.
   *
   * Relative paths and filter-based requests are passed through unchanged because the
   * agent is expected to have placed those files in staging directly.
   */
  const uploadArtifactHandler = args => {
    const entry = { ...(args || {}), type: "upload_artifact" };

    if (typeof entry.path === "string" && path.isAbsolute(entry.path)) {
      const filePath = entry.path;

      if (!fs.existsSync(filePath)) {
        throw {
          code: -32602,
          message: `${ERR_VALIDATION}: upload_artifact: file not found: ${filePath}`,
        };
      }

      const stat = lstatGuard(filePath);
      if (stat === null) {
        throw {
          code: -32602,
          message: `${ERR_VALIDATION}: upload_artifact: symlinks are not allowed: ${filePath}`,
        };
      }

      // Canonicalize to detect traversal escapes and symlink chains.
      let canonicalFilePath;
      try {
        canonicalFilePath = fs.realpathSync(filePath);
      } catch (err) {
        throw {
          code: -32602,
          message: `${ERR_VALIDATION}: upload_artifact: failed to resolve canonical path for ${filePath}: ${getErrorMessage(err)}`,
        };
      }

      // Reject sensitive paths (system dirs, .git, HOME credentials).
      const sensitiveError = validateUploadSourcePath(canonicalFilePath);
      if (sensitiveError) {
        throw {
          code: -32602,
          message: `${ERR_VALIDATION}: upload_artifact: ${sensitiveError}`,
        };
      }

      // Enforce allowed canonical source roots: staging dir and GITHUB_WORKSPACE.
      // RUNNER_TEMP is intentionally excluded — only the specific staging subdirectory is allowed.
      const stagingDir = path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "safeoutputs", "upload-artifacts");
      const allowedRoots = [canonicalizeAllowedRoot(stagingDir)];
      if (process.env.GITHUB_WORKSPACE) {
        allowedRoots.push(canonicalizeAllowedRoot(process.env.GITHUB_WORKSPACE));
      }
      const withinAllowedRoot = allowedRoots.some(root => canonicalFilePath === root || canonicalFilePath.startsWith(root + path.sep));
      if (!withinAllowedRoot) {
        throw {
          code: -32602,
          message: `${ERR_VALIDATION}: upload_artifact: path is outside allowed source roots (GITHUB_WORKSPACE, staging directory): ${canonicalFilePath}`,
        };
      }

      if (!fs.existsSync(stagingDir)) {
        try {
          fs.mkdirSync(stagingDir, { recursive: true });
        } catch (err) {
          throw new Error(`${ERR_SYSTEM}: Failed to create directory ${stagingDir}: ${getErrorMessage(err)}`, { cause: err });
        }
      }

      const destName = path.basename(filePath);

      if (stat.isDirectory()) {
        copyDirectoryRecursive(filePath, path.join(stagingDir, destName));
      } else {
        const destPath = path.join(stagingDir, destName);
        if (!fs.existsSync(destPath)) {
          try {
            fs.copyFileSync(filePath, destPath);
            fs.chmodSync(destPath, 0o600);
          } catch (err) {
            throw new Error(`${ERR_SYSTEM}: Failed to copy file ${filePath} to ${destPath}: ${getErrorMessage(err)}`, { cause: err });
          }
        }
      }

      // Rewrite to staging-relative path so upload_artifact.cjs resolves it from staging.
      entry.path = destName;
      server.debug(`upload_artifact: staged ${filePath} as ${destName}`);
    }

    appendSafeOutputCounted(entry);

    const temporaryId = entry.temporary_id || null;
    return {
      content: [
        {
          type: "text",
          text: JSON.stringify({
            result: "success",
            ...(temporaryId ? { temporary_id: temporaryId } : {}),
          }),
        },
      ],
    };
  };

  /**
   * Handler for upload_code_coverage tool.
   * Spec cross-reference: not part of the numbered outcome types in Safe Output Outcome Evaluation v1.0.0.
   *
   * Mirrors uploadArtifactHandler: when the agent calls upload_code_coverage with an
   * absolute path (or a workspace-relative path) for the coverage report file, this handler
   * copies it into the upload-code-coverage staging directory
   * ($RUNNER_TEMP/gh-aw/safeoutputs/upload-code-coverage/), which is uploaded as the
   * safe-outputs-upload-code-coverage artifact by the agent job and downloaded by the
   * dedicated upload_code_coverage job that invokes actions/upload-code-coverage.
   *
   * For requests with an absolute path, the handler also rewrites entry.file to the
   * staging-relative basename so upload_code_coverage.cjs on the safe_outputs runner
   * resolves the file from staging rather than trying the (non-existent) absolute path.
   * A bare filename already in staging is passed through unchanged.
   */
  const uploadCodeCoverageHandler = args => {
    const entry = { ...(args || {}), type: "upload_code_coverage" };
    const stagingDir = path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "safeoutputs", "upload-code-coverage");
    const coverageRoot = process.env.GITHUB_WORKSPACE ? path.join(process.env.GITHUB_WORKSPACE, "coverage") : "";

    if (typeof entry.file === "string") {
      let filePath = entry.file;
      if (!path.isAbsolute(filePath)) {
        const stagedCandidate = path.resolve(stagingDir, filePath);
        if (fs.existsSync(stagedCandidate)) {
          filePath = stagedCandidate;
        } else if (process.env.GITHUB_WORKSPACE) {
          const workspaceResolvedPath = path.resolve(process.env.GITHUB_WORKSPACE, filePath);
          const resolvedCoverageRoot = path.resolve(process.env.GITHUB_WORKSPACE, "coverage");
          const withinCoverageRoot = workspaceResolvedPath === resolvedCoverageRoot || workspaceResolvedPath.startsWith(resolvedCoverageRoot + path.sep);
          if (!withinCoverageRoot) {
            throw {
              code: -32602,
              message: `${ERR_VALIDATION}: upload_code_coverage: path is outside allowed source roots (GITHUB_WORKSPACE/coverage, staging directory)`,
            };
          }
          filePath = workspaceResolvedPath;
        } else {
          throw {
            code: -32602,
            message: `${ERR_VALIDATION}: upload_code_coverage: GITHUB_WORKSPACE is required when file is a relative path outside staging`,
          };
        }
      }

      if (!fs.existsSync(filePath)) {
        throw {
          code: -32602,
          message: `${ERR_VALIDATION}: upload_code_coverage: file not found: ${filePath}`,
        };
      }

      const stat = lstatGuard(filePath);
      if (stat === null) {
        throw {
          code: -32602,
          message: `${ERR_VALIDATION}: upload_code_coverage: symlinks are not allowed: ${filePath}`,
        };
      }
      if (!stat.isFile()) {
        throw {
          code: -32602,
          message: `${ERR_VALIDATION}: upload_code_coverage: path must be a regular file: ${filePath}`,
        };
      }

      // Canonicalize to detect traversal escapes and symlink chains.
      let canonicalFilePath;
      try {
        canonicalFilePath = fs.realpathSync(filePath);
      } catch (err) {
        throw {
          code: -32602,
          message: `${ERR_VALIDATION}: upload_code_coverage: failed to resolve canonical path for ${filePath}: ${getErrorMessage(err)}`,
        };
      }

      // Reject sensitive paths (system dirs, .git, HOME credentials).
      const sensitiveError = validateUploadSourcePath(canonicalFilePath);
      if (sensitiveError) {
        throw {
          code: -32602,
          message: `${ERR_VALIDATION}: upload_code_coverage: ${sensitiveError}`,
        };
      }

      // Enforce allowed canonical source roots: staging dir and GITHUB_WORKSPACE/coverage.
      const allowedRoots = [canonicalizeAllowedRoot(stagingDir)];
      if (coverageRoot) {
        allowedRoots.push(canonicalizeAllowedRoot(coverageRoot));
      }
      const withinAllowedRoot = allowedRoots.some(root => canonicalFilePath === root || canonicalFilePath.startsWith(root + path.sep));
      if (!withinAllowedRoot) {
        throw {
          code: -32602,
          message: `${ERR_VALIDATION}: upload_code_coverage: path is outside allowed source roots (GITHUB_WORKSPACE/coverage, staging directory): ${canonicalFilePath}`,
        };
      }

      if (!fs.existsSync(stagingDir)) {
        try {
          fs.mkdirSync(stagingDir, { recursive: true });
        } catch (err) {
          throw new Error(`${ERR_SYSTEM}: Failed to create directory ${stagingDir}: ${getErrorMessage(err)}`, { cause: err });
        }
      }

      const canonicalStagingDir = canonicalizeAllowedRoot(stagingDir);
      const alreadyStaged = canonicalFilePath === canonicalStagingDir || canonicalFilePath.startsWith(canonicalStagingDir + path.sep);
      const destName = path.basename(filePath);
      const destPath = path.join(stagingDir, destName);
      if (!alreadyStaged && !fs.existsSync(destPath)) {
        try {
          fs.copyFileSync(filePath, destPath);
          fs.chmodSync(destPath, 0o600);
        } catch (err) {
          throw new Error(`${ERR_SYSTEM}: Failed to copy file ${filePath} to ${destPath}: ${getErrorMessage(err)}`, { cause: err });
        }
      }

      // Rewrite to staging-relative path so upload_code_coverage.cjs resolves it from staging.
      entry.file = destName;
      server.debug(`upload_code_coverage: staged ${filePath} as ${destName}`);
    }

    appendSafeOutputCounted(entry);

    return {
      content: [
        {
          type: "text",
          text: JSON.stringify({ result: "success" }),
        },
      ],
    };
  };

  /**
   * Handler for update_issue tool
   * Spec cross-reference: Safe Output Outcome Evaluation §update_issue.
   * Per Safe Outputs Specification MCE1: Enforces context constraints during tool invocation
   * to provide immediate feedback to the LLM before recording to NDJSON.
   * Rejects `target: triggering` (the default) when the workflow has no issue context
   * (e.g. on schedule or push events), so the agent receives an actionable error
   * instead of a downstream Process Safe Outputs failure.
   */
  const updateIssueHandler = args => {
    const updateIssueConfig = getSafeOutputsToolConfig(config, "update_issue");
    const effectiveTarget = updateIssueConfig.target || "triggering";

    if (effectiveTarget === "triggering") {
      /** @type {any} */
      let invocationContext = null;
      try {
        invocationContext = resolveInvocationContext(context);
      } catch (err) {
        // A validation error (e.g. disallowed target_repo / SEC-005) is a real failure — surface it.
        const errMsg = getErrorMessage(err);
        if (errMsg.startsWith(ERR_VALIDATION)) {
          return buildIntentErrorResponse(errMsg);
        }
        // Unexpected structural error: skip validation and let downstream handle gracefully.
      }
      if (invocationContext != null) {
        const { effectiveEventName, effectivePayload } = resolveEffectiveContext(invocationContext, context);
        const isIssueCommentOnPR = effectiveEventName === "issue_comment" && Boolean(effectivePayload?.issue?.pull_request);
        const isIssueContext = effectiveEventName === "issues" || (effectiveEventName === "issue_comment" && !isIssueCommentOnPR);

        if (!isIssueContext) {
          return buildIntentErrorResponse(
            `update_issue requires an issue context but the workflow is running on a "${effectiveEventName}" event. ` +
              `The update-issue handler uses target: triggering which only applies when an issue triggered the workflow. ` +
              `To report results from this workflow, use create_discussion or create_issue instead. ` +
              `If you need to update a specific issue, the workflow must configure update-issue: target: '*' and you must supply issue_number.`
          );
        }
      }
    }

    return defaultHandler("update_issue")(args || {});
  };

  const jiraCreateIssueHandler = defaultHandler("jira_create_issue");
  const jiraAddCommentHandler = defaultHandler("jira_add_comment");
  const jiraAddLabelHandler = defaultHandler("jira_add_label");
  const jiraUpdateIssueHandler = args => {
    const summary = typeof args?.summary === "string" ? args.summary.trim() : "";
    const description = typeof args?.description === "string" ? args.description.trim() : "";
    if (!summary && !description) {
      return buildIntentErrorResponse("jira_update_issue requires at least one non-empty field: summary or description");
    }
    return defaultHandler("jira_update_issue")(args || {});
  };

  /**
   * Handler for update_pull_request tool
   * Spec cross-reference: Safe Output Outcome Evaluation §update_pull_request.
   * Per Safe Outputs Specification MCE1: Enforces constraints during tool invocation
   * to provide immediate feedback to the LLM before recording to NDJSON.
   * Uses hasUpdatePullRequestFields to validate that at least one of 'title', 'body',
   * or 'update_branch' is provided before recording to NDJSON.
   * Rejects `target: triggering` (the default) when the workflow has no pull request context
   * (e.g. on schedule or push events), so the agent receives an actionable error
   * instead of a downstream Process Safe Outputs failure.
   */
  const updatePullRequestHandler = args => {
    /** @type {any} */
    const normalizedArgs = normalizeCombinedTitleBodyArgs(args);
    if (!hasUpdatePullRequestFields(normalizedArgs)) {
      throw {
        code: -32602,
        message: `${ERR_VALIDATION}: update_pull_request requires at least one of: 'title', 'body', 'update_branch' fields`,
      };
    }

    const updatePRConfig = getSafeOutputsToolConfig(config, "update_pull_request");
    const effectivePRTarget = updatePRConfig.target || "triggering";
    if (effectivePRTarget === "triggering") {
      /** @type {any} */
      let invocationContext = null;
      try {
        invocationContext = resolveInvocationContext(context);
      } catch (err) {
        // A validation error (e.g. disallowed target_repo / SEC-005) is a real failure — surface it.
        const errMsg = getErrorMessage(err);
        if (errMsg.startsWith(ERR_VALIDATION)) {
          return buildIntentErrorResponse(errMsg);
        }
        // Unexpected structural error: skip validation and let downstream handle gracefully.
      }
      if (invocationContext != null) {
        const { effectiveEventName, effectivePayload } = resolveEffectiveContext(invocationContext, context);
        const isIssueCommentOnPR = effectiveEventName === "issue_comment" && Boolean(effectivePayload?.issue?.pull_request);
        const isPRContext = PR_EVENT_NAMES.has(effectiveEventName) || isIssueCommentOnPR;

        if (!isPRContext) {
          return buildIntentErrorResponse(
            `update_pull_request requires a pull request context but the workflow is running on a "${effectiveEventName}" event. ` +
              `The update-pull-request handler uses target: triggering which only applies when a pull request triggered the workflow. ` +
              `To report results from this workflow, use create_discussion or create_issue instead. ` +
              `If you need to update a specific pull request, the workflow must configure update-pull-request: target: '*' and you must supply pull_request_number.`
          );
        }
      }
    }

    return defaultHandler("update_pull_request")(normalizedArgs);
  };

  // ============================================================
  // Egress context validators for tools that target existing items
  // ============================================================
  //
  // Per Safe Outputs Specification MCE1: these handlers validate that the
  // required triggering context (PR, issue, or discussion) is available
  // BEFORE writing to NDJSON. When a tool targets a triggering entity
  // (no explicit item number supplied) on a scheduled or workflow_dispatch
  // run that has no such context, the agent receives an actionable error
  // immediately rather than a downstream processing hard-failure.
  //
  // Pattern: if an explicit target number is provided, bypass the context
  // check and let the downstream handler resolve it normally. Only when the
  // number is absent do we gate on triggering-context availability.

  /**
   * Build a handler that validates triggering context before recording to NDJSON.
   * Used for tools that fall back to triggering entity context when no explicit
   * target number is supplied (e.g. close_pull_request, close_issue, add_labels).
   *
   * Context validation only runs when the tool's configured `target` is `"triggering"`
   * (the default). When `target` is a fixed number (e.g. `"42"`) or `"*"` (wildcard),
   * the check is skipped: the fixed number is resolved downstream, and wildcard
   * enforcement is handled by `validateWildcardTargetRequirement`.
   *
   * @param {Object} opts
   * @param {string} opts.toolName - Normalised tool name (e.g. "close_pull_request")
   * @param {string[]} opts.explicitNumberFields - Args fields that constitute an explicit target
   *   (if any is present the context check is skipped)
   * @param {"pr"|"issue"|"issue_or_pr"|"discussion"} opts.contextType - Required triggering context
   * @param {(eventName: string) => string} opts.buildErrorMessage - Returns the error string to
   *   surface when the required context is missing
   * @returns {(args: any) => any} Handler function
   */
  const createTriggeringContextHandler = ({ toolName, explicitNumberFields, contextType, buildErrorMessage }) => {
    return args => {
      const toolConfig = getSafeOutputsToolConfig(config, toolName);
      const effectiveTarget = toolConfig.target || "triggering";

      // Only validate triggering context when the tool is configured to target the
      // triggering entity. With a fixed number target the downstream handler resolves
      // it directly; with wildcard targeting the per-call number requirement is
      // enforced by validateWildcardTargetRequirement in defaultHandler.
      if (effectiveTarget === "triggering") {
        // If the caller supplied an explicit target number, skip context validation.
        // The downstream execution handler will resolve the number normally.
        const hasExplicitNumber = explicitNumberFields.some(field => args?.[field] != null);
        if (!hasExplicitNumber) {
          /** @type {any} */
          let invocationContext = null;
          try {
            invocationContext = resolveInvocationContext(context);
          } catch (err) {
            // A validation error (e.g. disallowed target_repo / SEC-005) is a real failure — surface it.
            const errMsg = getErrorMessage(err);
            if (errMsg.startsWith(ERR_VALIDATION)) {
              return buildIntentErrorResponse(errMsg);
            }
            // Unexpected structural error: skip validation and let downstream handle gracefully.
          }
          if (invocationContext != null) {
            const { effectiveEventName, effectivePayload } = resolveEffectiveContext(invocationContext, context);
            const isIssueCommentOnPR = effectiveEventName === "issue_comment" && Boolean(effectivePayload?.issue?.pull_request);

            let hasContext;
            if (contextType === "pr") {
              hasContext = PR_EVENT_NAMES.has(effectiveEventName) || isIssueCommentOnPR;
            } else if (contextType === "issue") {
              hasContext = effectiveEventName === "issues" || (effectiveEventName === "issue_comment" && !isIssueCommentOnPR);
            } else if (contextType === "issue_or_pr") {
              const isPR = PR_EVENT_NAMES.has(effectiveEventName) || isIssueCommentOnPR;
              const isIssue = effectiveEventName === "issues" || (effectiveEventName === "issue_comment" && !isIssueCommentOnPR);
              hasContext = isPR || isIssue;
            } else if (contextType === "discussion") {
              hasContext = effectiveEventName === "discussion" || effectiveEventName === "discussion_comment";
            } else {
              hasContext = false;
            }

            if (!hasContext) {
              return buildIntentErrorResponse(buildErrorMessage(effectiveEventName));
            }
          }
        }
      }

      return defaultHandler(toolName)(args || {});
    };
  };

  /**
   * Handler for close_pull_request tool.
   * Per Safe Outputs Specification MCE1: validates PR context on egress when no
   * explicit pull_request_number is supplied.
   */
  const closePullRequestHandler = createTriggeringContextHandler({
    toolName: "close_pull_request",
    explicitNumberFields: ["pull_request_number"],
    contextType: "pr",
    buildErrorMessage: eventName =>
      `close_pull_request requires a pull request context but the workflow is running on a "${eventName}" event. ` +
      `The close-pull-request handler auto-targets the pull request that triggered this workflow. ` +
      `To close a specific pull request, supply pull_request_number explicitly.`,
  });

  /**
   * Handler for merge_pull_request tool.
   * Per Safe Outputs Specification MCE1: validates PR context on egress when no
   * explicit pull_request_number is supplied.
   */
  const mergePullRequestHandler = createTriggeringContextHandler({
    toolName: "merge_pull_request",
    explicitNumberFields: ["pull_request_number"],
    contextType: "pr",
    buildErrorMessage: eventName =>
      `merge_pull_request requires a pull request context but the workflow is running on a "${eventName}" event. ` +
      `The merge-pull-request handler auto-targets the pull request that triggered this workflow. ` +
      `To merge a specific pull request, supply pull_request_number explicitly.`,
  });

  /**
   * Handler for mark_pull_request_as_ready_for_review tool.
   * Per Safe Outputs Specification MCE1: validates PR context on egress when no
   * explicit pull_request_number is supplied.
   */
  const markPullRequestAsReadyForReviewHandler = createTriggeringContextHandler({
    toolName: "mark_pull_request_as_ready_for_review",
    explicitNumberFields: ["pull_request_number"],
    contextType: "pr",
    buildErrorMessage: eventName =>
      `mark_pull_request_as_ready_for_review requires a pull request context but the workflow is running on a "${eventName}" event. ` +
      `This handler auto-targets the pull request that triggered this workflow. ` +
      `To target a specific pull request, supply pull_request_number explicitly.`,
  });

  /**
   * Handler for add_reviewer tool.
   * Per Safe Outputs Specification MCE1: validates PR context on egress when no
   * explicit pull_request_number is supplied.
   */
  const addReviewerHandler = createTriggeringContextHandler({
    toolName: "add_reviewer",
    explicitNumberFields: ["pull_request_number"],
    contextType: "pr",
    buildErrorMessage: eventName =>
      `add_reviewer requires a pull request context but the workflow is running on a "${eventName}" event. ` +
      `The add-reviewer handler auto-targets the pull request that triggered this workflow. ` +
      `To add a reviewer to a specific pull request, supply pull_request_number explicitly.`,
  });

  /**
   * Handler for reply_to_pull_request_review_comment tool.
   * Per Safe Outputs Specification MCE1: validates PR context on egress when no
   * explicit pull_request_number is supplied.
   */
  const replyToPullRequestReviewCommentHandler = createTriggeringContextHandler({
    toolName: "reply_to_pull_request_review_comment",
    explicitNumberFields: ["pull_request_number"],
    contextType: "pr",
    buildErrorMessage: eventName =>
      `reply_to_pull_request_review_comment requires a pull request context but the workflow is running on a "${eventName}" event. ` +
      `This handler auto-targets the pull request that triggered this workflow. ` +
      `To reply to a review comment on a specific pull request, supply pull_request_number explicitly.`,
  });

  /**
   * Handler for close_issue tool.
   * Per Safe Outputs Specification MCE1: validates issue context on egress when no
   * explicit issue_number is supplied.
   */
  const closeIssueHandler = createTriggeringContextHandler({
    toolName: "close_issue",
    explicitNumberFields: ["issue_number"],
    contextType: "issue",
    buildErrorMessage: eventName =>
      `close_issue requires an issue context but the workflow is running on a "${eventName}" event. ` +
      `The close-issue handler auto-targets the issue that triggered this workflow. ` +
      `To close a specific issue, supply issue_number explicitly.`,
  });

  /**
   * Handler for add_labels tool.
   * Per Safe Outputs Specification MCE1: validates issue or PR context on egress when no
   * explicit item_number is supplied.
   */
  const addLabelsHandler = createTriggeringContextHandler({
    toolName: "add_labels",
    explicitNumberFields: ["item_number", "issue_number", "pr_number", "pull_number"],
    contextType: "issue_or_pr",
    buildErrorMessage: eventName =>
      `add_labels requires an issue or pull request context but the workflow is running on a "${eventName}" event. ` +
      `The add-labels handler auto-targets the issue or pull request that triggered this workflow. ` +
      `To label a specific item, supply item_number explicitly.`,
  });

  /**
   * Handler for remove_labels tool.
   * Per Safe Outputs Specification MCE1: validates issue or PR context on egress when no
   * explicit item_number is supplied.
   */
  const removeLabelsHandler = createTriggeringContextHandler({
    toolName: "remove_labels",
    explicitNumberFields: ["item_number", "issue_number", "pr_number", "pull_number"],
    contextType: "issue_or_pr",
    buildErrorMessage: eventName =>
      `remove_labels requires an issue or pull request context but the workflow is running on a "${eventName}" event. ` +
      `The remove-labels handler auto-targets the issue or pull request that triggered this workflow. ` +
      `To remove labels from a specific item, supply item_number explicitly.`,
  });

  /**
   * Handler for update_discussion tool.
   * Per Safe Outputs Specification MCE1: validates discussion context on egress when no
   * explicit discussion_number is supplied.
   */
  const updateDiscussionHandler = createTriggeringContextHandler({
    toolName: "update_discussion",
    explicitNumberFields: ["discussion_number"],
    contextType: "discussion",
    buildErrorMessage: eventName =>
      `update_discussion requires a discussion context but the workflow is running on a "${eventName}" event. ` +
      `The update-discussion handler auto-targets the discussion that triggered this workflow. ` +
      `To update a specific discussion, supply discussion_number explicitly.`,
  });

  /**
   * Handler for close_discussion tool.
   * Per Safe Outputs Specification MCE1: validates discussion context on egress when no
   * explicit discussion_number is supplied.
   */
  const closeDiscussionHandler = createTriggeringContextHandler({
    toolName: "close_discussion",
    explicitNumberFields: ["discussion_number"],
    contextType: "discussion",
    buildErrorMessage: eventName =>
      `close_discussion requires a discussion context but the workflow is running on a "${eventName}" event. ` +
      `The close-discussion handler auto-targets the discussion that triggered this workflow. ` +
      `To close a specific discussion, supply discussion_number explicitly.`,
  });

  return {
    defaultHandler,
    uploadAssetHandler,
    uploadArtifactHandler,
    uploadCodeCoverageHandler,
    createPullRequestHandler,
    pushToPullRequestBranchHandler,
    pushRepoMemoryHandler,
    createIssueHandler,
    createWorkItemHandler,
    updateWorkItemHandler: createAzureDevOpsWorkItemHandler("ado_update_work_item"),
    commentOnWorkItemHandler: createAzureDevOpsWorkItemHandler("ado_comment_on_work_item"),
    assignWorkItemHandler: createAzureDevOpsWorkItemHandler("ado_assign_work_item"),
    linkWorkItemsHandler: createAzureDevOpsWorkItemHandler("ado_link_work_items"),
    uploadWorkItemAttachmentHandler,
    jiraCreateIssueHandler,
    jiraUpdateIssueHandler,
    jiraAddCommentHandler,
    jiraAddLabelHandler,
    createProjectHandler,
    addCommentHandler,
    createPullRequestReviewCommentHandler,
    submitPullRequestReviewHandler,
    dismissPullRequestReviewHandler,
    updateIssueHandler,
    updatePullRequestHandler,
    closePullRequestHandler,
    mergePullRequestHandler,
    markPullRequestAsReadyForReviewHandler,
    addReviewerHandler,
    replyToPullRequestReviewCommentHandler,
    closeIssueHandler,
    addLabelsHandler,
    removeLabelsHandler,
    updateDiscussionHandler,
    closeDiscussionHandler,
  };
}

module.exports = {
  buildIntentErrorResponse,
  createHandlers,
  hasUpdatePullRequestFields,
};
