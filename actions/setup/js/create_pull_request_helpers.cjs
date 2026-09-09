// @ts-check
/// <reference types="@actions/github-script" />

/** @type {typeof import("crypto")} */
const crypto = require("crypto");
const { globPatternToRegex } = require("./glob_pattern_helpers.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { isTransientError } = require("./error_recovery.cjs");
const { tryEnforceArrayLimit } = require("./limit_enforcement_helpers.cjs");
const { MAX_ASSIGNEES } = require("./constants.cjs");
const { encodePathSegments, renderTemplateFromFile, getPromptPath } = require("./messages_core.cjs");

/** @type {string} Label always added to fallback issues so the triage system can find them */
const MANAGED_FALLBACK_ISSUE_LABEL = "agentic-workflows";

/** @type {number} Number of retry attempts for label operations */
const LABEL_MAX_RETRIES = 5;
/** @type {number} Base delay in ms used to calculate label retry backoff (3 seconds) */
const LABEL_INITIAL_DELAY_MS = 3000;
/** @type {number} Maximum delay in ms between label retries (30 seconds) */
const LABEL_MAX_DELAY_MS = 30000;

/**
 * Summarize a list for log output to avoid excessively long lines.
 * @param {string[]} values
 * @param {number} limit
 * @returns {string}
 */
function summarizeListForLog(values, limit = 10) {
  if (!Array.isArray(values) || values.length === 0) {
    return "(none)";
  }
  const preview = values.slice(0, limit).join(", ");
  return values.length > limit ? `${preview} ... and ${values.length - limit} more` : preview;
}

/**
 * Creates a temporary refs/bundles ref for applying create_pull_request bundles.
 * Branch names are sanitized for ref compatibility, and a short crypto-random
 * suffix avoids collisions between branches that sanitize to the same value.
 *
 * @param {string} branchName - Target branch name
 * @returns {string} Temporary bundle ref name
 */
function createBundleTempRef(branchName) {
  const suffix = crypto.randomBytes(4).toString("hex");
  return `refs/bundles/create-pr-${branchName.replace(/[^a-zA-Z0-9-]/g, "-")}-${suffix}`;
}

/**
 * Quote a value as a single POSIX shell argument.
 * @param {string|number} value
 * @returns {string}
 */
function shellQuote(value) {
  const s = String(value);
  if (s.length === 0) return "''";
  return `'${s.replace(/'/g, `'\\''`)}'`;
}

/**
 * Determines if a label API error is transient and worth retrying.
 * Returns true for:
 *  - The GitHub race condition where a newly-created PR's node ID is not immediately
 *    resolvable via the REST/GraphQL bridge (unprocessable validation error).
 *  - Any standard transient error matched by {@link isTransientError} (network issues,
 *    rate limits, 5xx gateway errors, etc.).
 * @param {any} error - The error to check
 * @returns {boolean} True if the error is transient and should be retried
 */
function isLabelTransientError(error) {
  const msg = getErrorMessage(error);
  if (msg.includes("Could not resolve to a node with the global id")) {
    return true;
  }
  return isTransientError(error);
}

/**
 * Parse allowed base branch patterns from config value (array or comma-separated string)
 * @param {string[]|string|undefined} allowedBaseBranchesValue
 * @returns {Set<string>}
 */
function parseAllowedBaseBranches(allowedBaseBranchesValue) {
  const set = new Set();
  if (Array.isArray(allowedBaseBranchesValue)) {
    allowedBaseBranchesValue
      .map(branch => String(branch).trim())
      .filter(Boolean)
      .forEach(branch => set.add(branch));
  } else if (typeof allowedBaseBranchesValue === "string") {
    allowedBaseBranchesValue
      .split(",")
      .map(branch => branch.trim())
      .filter(Boolean)
      .forEach(branch => set.add(branch));
  }
  return set;
}

/**
 * Check if a base branch matches an allowed pattern.
 * Supports exact matches and "*" glob patterns (e.g. "release/*").
 * @param {string} baseBranch
 * @param {Set<string>} allowedBaseBranches
 * @returns {boolean}
 */
function isBaseBranchAllowed(baseBranch, allowedBaseBranches) {
  if (allowedBaseBranches.has(baseBranch)) {
    return true;
  }
  for (const pattern of allowedBaseBranches) {
    if (pattern === "*") {
      return true;
    }
    if (pattern.includes("*") && globPatternToRegex(pattern, { pathMode: true, caseSensitive: true }).test(baseBranch)) {
      return true;
    }
  }
  return false;
}

/**
 * Parse config values that may be arrays or comma-separated strings.
 * @param {string[]|string|undefined} value
 * @returns {string[]}
 */
function parseStringListConfig(value) {
  if (!value) {
    return [];
  }
  const raw = Array.isArray(value) ? value : String(value).split(",");
  return raw.map(item => String(item).trim()).filter(Boolean);
}

/**
 * Merges the required fallback label with any workflow-configured labels,
 * deduplicating and filtering empty values.
 * @param {string[]} [labels]
 * @returns {string[]}
 */
function mergeFallbackIssueLabels(labels = []) {
  const normalizedLabels = labels
    .filter(label => !!label)
    .map(label => String(label).trim())
    .filter(label => label);
  return [...new Set([MANAGED_FALLBACK_ISSUE_LABEL, ...normalizedLabels])];
}

/**
 * Sanitizes configured assignees for fallback issue creation.
 * Filters invalid values, removes the special "copilot" username (not a valid GitHub user
 * for issue assignment), and enforces the MAX_ASSIGNEES limit.
 * Returns null (no assignees field) if the sanitized list is empty.
 * @param {string[]} assignees - Raw assignees from config
 * @returns {string[] | null} Sanitized assignees or null if none remain
 */
function sanitizeFallbackAssignees(assignees) {
  if (!assignees || assignees.length === 0) {
    return null;
  }
  const sanitized = assignees
    .filter(a => typeof a === "string")
    .map(a => a.trim())
    .filter(a => a.length > 0 && a.toLowerCase() !== "copilot");

  if (sanitized.length === 0) {
    return null;
  }

  const limitResult = tryEnforceArrayLimit(sanitized, MAX_ASSIGNEES, "assignees");
  if (!limitResult.success) {
    core.warning(`Assignees limit exceeded for fallback issue: ${limitResult.error}. Using first ${MAX_ASSIGNEES}.`);
    return sanitized.slice(0, MAX_ASSIGNEES);
  }

  return sanitized;
}

/**
 * Neutralizes issue-closing keywords in body text to avoid unintended cross-issue closure
 * when PR content is reused in fallback issue bodies.
 *
 * Example: "Closes #123" -> "Closes \\#123"
 *
 * @param {string} content
 * @returns {string}
 */
function neutralizeClosingKeywordsForIssueBody(content) {
  if (!content) {
    return content;
  }
  const closingKeywordPattern = /\b(fix|fixes|fixed|close|closes|closed|resolve|resolves|resolved)\s+((?:[a-z0-9_.-]+\/[a-z0-9_.-]+)?#\d+)\b/gi;
  const escapeIssueRef = (_match, keyword, issueRef) => `${keyword} ${String(issueRef).replace("#", "\\#")}`;
  return String(content).replace(closingKeywordPattern, escapeIssueRef);
}

/**
 * Generate a patch preview with max 500 lines and 2000 chars for issue body
 * @param {string} patchContent - The full patch content
 * @returns {string} Formatted patch preview
 */
function generatePatchPreview(patchContent) {
  if (!patchContent || !patchContent.trim()) {
    return "";
  }

  const lines = patchContent.split("\n");
  const maxLines = 500;
  const maxChars = 2000;

  // Apply line limit first
  let preview = lines.length <= maxLines ? patchContent : lines.slice(0, maxLines).join("\n");
  const lineTruncated = lines.length > maxLines;

  // Apply character limit
  const charTruncated = preview.length > maxChars;
  if (charTruncated) {
    preview = preview.slice(0, maxChars);
  }

  const truncated = lineTruncated || charTruncated;
  const shownLines = preview.split("\n").length;
  const summary = truncated ? `Show patch preview (${shownLines} of ${lines.length} lines)` : `Show patch (${lines.length} lines)`;

  return `\n\n<details><summary>${summary}</summary>\n\n\`\`\`diff\n${preview}${truncated ? "\n... (truncated)" : ""}\n\`\`\`\n\n</details>`;
}

/**
 * Builds a compare URL used in protected-files fallback issue bodies.
 * Optionally appends a prefilled PR body that closes the fallback issue.
 * @param {string} githubServer
 * @param {{owner: string, repo: string}} repoParts
 * @param {string} baseBranch
 * @param {string} branchName
 * @param {string} title
 * @param {number} [fallbackIssueNumber]
 * @param {string} [headRef]
 * @returns {string}
 */
function buildManifestProtectionCreatePrUrl(githubServer, repoParts, baseBranch, branchName, title, fallbackIssueNumber, headRef) {
  const encodedBase = encodePathSegments(baseBranch);
  const encodedHead = encodePathSegments(headRef || branchName);
  let createPrUrl = `${githubServer}/${repoParts.owner}/${repoParts.repo}/compare/${encodedBase}...${encodedHead}?expand=1&title=${encodeURIComponent(title)}`;
  if (typeof fallbackIssueNumber === "number") {
    createPrUrl += `&body=${encodeURIComponent(`Closes #${fallbackIssueNumber}`)}`;
  }
  return createPrUrl;
}

/**
 * Build a formatted markdown error section for a push-failure note block.
 *
 * When the raw error message matches the `pushSignedCommits: refusing unsigned push`
 * pattern the section is expanded into a structured block that names the cause and
 * lists remediation steps. For all other errors it degrades gracefully to a single
 * `**Original error:**` line using the sanitised, whitespace-collapsed form.
 *
 * The returned string contains one or more blockquote lines (lines starting with `>`)
 * with no leading or trailing blank lines. It is designed to be embedded directly
 * inside a `> [!NOTE]` block in a GitHub issue body, for example:
 *
 * ```
 * > [!NOTE]
 * > Intro sentence.
 * >
 * ${buildPushErrorSection(rawMsg, sanitizedMsg)}
 * >
 * > **Workflow Run:** [details](url)
 * ```
 *
 * @param {string} rawErrorMessage - Unprocessed error message; used for pattern detection and cause extraction.
 * @param {string} sanitizedErrorMessage - Sanitised, whitespace-collapsed message; used as the fallback line.
 * @returns {string} One or more blockquote lines for the error section (no leading/trailing blank lines).
 */
function buildPushErrorSection(rawErrorMessage, sanitizedErrorMessage) {
  // Only render the structured block for PushSignedCommitsUnsupportedShape errors,
  // identified by the unique "cannot represent" boilerplate text. PushSignedCommitsPolicyViolation
  // errors also start with "refusing unsigned push" but lack this boilerplate, so they fall
  // through to the sanitized original-error fallback.
  if (!/pushSignedCommits: refusing unsigned push/.test(rawErrorMessage) || !/createCommitOnBranch GraphQL mutation cannot represent/.test(rawErrorMessage)) {
    return `> **Original error:** ${sanitizedErrorMessage}`;
  }

  // Extract the specific cause (e.g. "merge commit detected") anchored to the boilerplate.
  // Using '.*?' (lazy) instead of '[^']*' (restricted) handles apostrophes in branch names.
  const causeMatch = rawErrorMessage.match(/refusing unsigned push for branch '.*?': ([^.]+?)(?=\. GitHub's createCommitOnBranch)/);
  const cause = causeMatch ? causeMatch[1].trim() : "unsupported commit shape";

  const remediationLines = _remediationForCause(cause);

  return [
    `> **Error:** Signed commit push refused — ${cause}`,
    `>`,
    `> GitHub's \`createCommitOnBranch\` GraphQL API cannot represent:`,
    `> - Merge commits`,
    `> - Symlinks (mode \`120000\`)`,
    `> - Submodule entries (mode \`160000\`)`,
    `> - Executable files (mode \`100755\`)`,
    `>`,
    ...remediationLines,
  ].join("\n");
}

/**
 * Returns cause-specific remediation lines for a signed-commits push refusal.
 * @param {string} cause - The extracted cause string from the error message.
 * @returns {string[]} Two blockquote lines: the fix instruction and the unsigned-push alternative.
 */
function _remediationForCause(cause) {
  const c = cause.toLowerCase();
  let rewriteInstruction;
  if (c.includes("merge commit")) {
    rewriteInstruction = "Use `git rebase` instead of `git merge` to rewrite the commit history without merge commits";
  } else if (c.includes("submodule")) {
    rewriteInstruction = "Remove the submodule entry from the commit history";
  } else if (c.includes("symlink")) {
    rewriteInstruction = "Remove the symlink from the commit history";
  } else {
    rewriteInstruction = "Rewrite the commits to use only regular files (mode `100644`) with no merge commits or special entries";
  }
  return [`> **To fix:** ${rewriteInstruction},`, `> or set \`signed-commits: false\` in your workflow step if signed commits are not required.`];
}

/**
 * Build the shell instructions for manually recreating a branch (and later opening a PR)
 * from a workflow-run artifact, matching whichever transport (bundle or format-patch)
 * was actually used to encode the changes. Used when the automated push failed and a
 * human needs to recreate the branch locally before pushing and opening the PR themselves.
 *
 * The returned block intentionally stops right before the final `git push` / `gh pr create`
 * steps, which are appended separately by the caller since they are identical for both
 * transports.
 *
 * @param {object} params
 * @param {boolean} params.hasBundleFile - true when the artifact is a git bundle, false for a format-patch file
 * @param {string|number} params.runId - workflow run id, used to build the `gh run download` command
 * @param {string} params.artifactFileName - bundle or patch file name (relative to the artifact root)
 * @param {string} params.branchName - branch to create/checkout locally
 * @param {string} [params.baseBranch] - base branch to create the new branch from (patch transport only)
 * @param {string} [params.tempRef] - temporary ref used while transplanting the bundle (bundle transport only)
 * @returns {string} Shell instructions (no leading/trailing blank lines, no code fence)
 */
function buildManualBranchRecoveryCommands({ hasBundleFile, runId, artifactFileName, branchName, baseBranch, tempRef }) {
  if (hasBundleFile) {
    if (!tempRef) {
      throw new Error("tempRef is required for bundle manual branch recovery commands");
    }
    const bundlePath = `/tmp/agent-${runId}/${artifactFileName}`;
    const targetRef = `refs/heads/${branchName}`;
    return [
      `# Download the artifact from the workflow run`,
      `gh run download ${shellQuote(runId)} -n agent -D ${shellQuote(`/tmp/agent-${runId}`)}`,
      ``,
      `# Resolve the bundle source ref, fetch it into a temporary ref, then create the local branch`,
      `bundle_path=${shellQuote(bundlePath)}`,
      `temp_ref=${shellQuote(tempRef)}`,
      `target_ref=${shellQuote(targetRef)}`,
      `bundle_source_ref=$(git bundle list-heads "$bundle_path" | awk '$2 ~ /^refs\\/heads\\// { print $2 }')`,
      `if [ -z "$bundle_source_ref" ]; then`,
      `  bundle_source_ref=$(git bundle list-heads "$bundle_path" | awk '$2 == "HEAD" { print $2 }')`,
      `fi`,
      `if [ "$(printf '%s\\n' "$bundle_source_ref" | sed '/^$/d' | wc -l | tr -d ' ')" != "1" ]; then`,
      `  echo "Expected exactly one bundle source ref, found: $bundle_source_ref" >&2`,
      `  exit 1`,
      `fi`,
      `git fetch "$bundle_path" "\${bundle_source_ref}:\${temp_ref}"`,
      `git update-ref "$target_ref" "$temp_ref"`,
      `git checkout ${shellQuote(branchName)}`,
      `# Ensure the working tree matches the updated branch`,
      `git reset --hard`,
      `# Remove the temporary bundle ref`,
      `git update-ref -d "$temp_ref"`,
    ].join("\n");
  }
  if (!baseBranch) {
    throw new Error("baseBranch is required for patch manual branch recovery commands");
  }
  return [
    `# Download the artifact from the workflow run`,
    `gh run download ${shellQuote(runId)} -n agent -D ${shellQuote(`/tmp/agent-${runId}`)}`,
    ``,
    `# Create a new branch`,
    `git checkout -b ${shellQuote(branchName)} ${shellQuote(baseBranch)}`,
    ``,
    `# Apply the patch (--3way handles cross-repo patches where files may already exist)`,
    `git am --3way ${shellQuote(`/tmp/agent-${runId}/${artifactFileName}`)}`,
  ].join("\n");
}

/**
 * Build the shell instructions for manually applying a workflow-run artifact to an
 * *existing* remote branch (e.g. an already-open pull request branch), matching whichever
 * transport (bundle or format-patch) was actually used to encode the changes.
 *
 * Unlike {@link buildManualBranchRecoveryCommands}, the returned block is self-contained:
 * it includes the final `git push`, since there is no separate PR-creation step for this flow.
 *
 * @param {object} params
 * @param {boolean} params.hasBundleFile - true when the artifact is a git bundle, false for a format-patch file
 * @param {string|number} params.runId - workflow run id, used to build the `gh run download` command
 * @param {string} params.artifactFileName - bundle or patch file name (relative to the artifact root)
 * @param {string} params.branchName - existing remote branch to update
 * @param {string} [params.branchRemote] - remote name or URL containing the existing branch
 * @returns {string} Shell instructions (no leading/trailing blank lines, no code fence)
 */
function buildManualBranchApplyCommands({ hasBundleFile, runId, artifactFileName, branchName, branchRemote = "origin" }) {
  const bundlePath = `/tmp/agent-${runId}/${artifactFileName}`;
  const pushRef = `HEAD:refs/heads/${branchName}`;
  if (hasBundleFile) {
    return [
      `# Download the artifact from the workflow run`,
      `gh run download ${shellQuote(runId)} -n agent -D ${shellQuote(`/tmp/agent-${runId}`)}`,
      ``,
      `# Fetch the bundle into a temporary ref, then fast-forward the branch`,
      `bundle_path=${shellQuote(bundlePath)}`,
      `git fetch ${shellQuote(branchRemote)} ${shellQuote(branchName)}`,
      `git checkout -B ${shellQuote(branchName)} FETCH_HEAD`,
      `bundle_source_ref=$(git bundle list-heads "$bundle_path" | awk '$2 ~ /^refs\\/heads\\// { print $2 }')`,
      `if [ -z "$bundle_source_ref" ]; then`,
      `  bundle_source_ref=$(git bundle list-heads "$bundle_path" | awk '$2 == "HEAD" { print $2 }')`,
      `fi`,
      `if [ "$(printf '%s\\n' "$bundle_source_ref" | sed '/^$/d' | wc -l | tr -d ' ')" != "1" ]; then`,
      `  echo "Expected exactly one bundle source ref, found: $bundle_source_ref" >&2`,
      `  exit 1`,
      `fi`,
      `git fetch "$bundle_path" "\${bundle_source_ref}:refs/bundles/manual-apply"`,
      `git reset --hard refs/bundles/manual-apply`,
      `git update-ref -d refs/bundles/manual-apply`,
      `git push ${shellQuote(branchRemote)} ${shellQuote(pushRef)}`,
    ].join("\n");
  }
  return [
    `# Download the artifact from the workflow run`,
    `gh run download ${shellQuote(runId)} -n agent -D ${shellQuote(`/tmp/agent-${runId}`)}`,
    ``,
    `# Apply the patch to the pull request branch`,
    `git fetch ${shellQuote(branchRemote)} ${shellQuote(branchName)}`,
    `git checkout -B ${shellQuote(branchName)} FETCH_HEAD`,
    `git am --3way ${shellQuote(`/tmp/agent-${runId}/${artifactFileName}`)}`,
    `git push ${shellQuote(branchRemote)} ${shellQuote(pushRef)}`,
  ].join("\n");
}

/**
 * Renders protected-files fallback issue body with a prefilled compare URL.
 * @param {string} mainBodyContent
 * @param {string} footerContent
 * @param {string} fileList
 * @param {string} createPrUrl
 * @returns {string}
 */
function renderManifestProtectionFallbackBody(mainBodyContent, footerContent, fileList, createPrUrl) {
  const templatePath = getPromptPath("manifest_protection_create_pr_fallback.md");
  return renderTemplateFromFile(templatePath, {
    main_body: mainBodyContent,
    footer: footerContent,
    files: fileList,
    create_pr_url: createPrUrl,
  });
}

module.exports = {
  MANAGED_FALLBACK_ISSUE_LABEL,
  LABEL_MAX_RETRIES,
  LABEL_INITIAL_DELAY_MS,
  LABEL_MAX_DELAY_MS,
  summarizeListForLog,
  createBundleTempRef,
  shellQuote,
  isLabelTransientError,
  parseAllowedBaseBranches,
  isBaseBranchAllowed,
  parseStringListConfig,
  mergeFallbackIssueLabels,
  sanitizeFallbackAssignees,
  neutralizeClosingKeywordsForIssueBody,
  generatePatchPreview,
  buildManifestProtectionCreatePrUrl,
  renderManifestProtectionFallbackBody,
  buildPushErrorSection,
  buildManualBranchRecoveryCommands,
  buildManualBranchApplyCommands,
};
