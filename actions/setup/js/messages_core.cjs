// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Core Message Utilities Module
 *
 * This module provides shared utilities for message template processing.
 * It includes configuration parsing and template rendering functions.
 *
 * Supported placeholders:
 * - {workflow_name} - Name of the workflow
 * - {run_url} - URL to the workflow run
 * - {agentic_workflow_url} - Direct URL to the agentic workflow page ({run_url}/agentic_workflow)
 * - {workflow_source} - Source specification (owner/repo/path@ref)
 * - {workflow_source_url} - GitHub URL for the workflow source
 * - {triggering_number} - Issue/PR/Discussion number that triggered this workflow
 * - {ai_credits} - Raw total AI Credits for the run, only present when > 0
 * - {ai_credits_formatted} - Compact formatted AI Credits, only present when > 0
 * - {ai_credits_suffix} - Pre-formatted suffix (e.g. " · 1.25 AIC"), or "" when not available
 * - {operation} - Operation name (for staged mode titles/descriptions)
 * - {event_type} - Event type description (for run-started messages)
 * - {status} - Workflow status text (for run-failure messages)
 * - {repository} - Repository name (for workflow recompile messages)
 *
 * Both camelCase and snake_case placeholder formats are supported.
 */

const { getErrorMessage } = require("./error_helpers.cjs");
const fs = require("fs");

/**
 * @typedef {Object} SafeOutputMessages
 * @property {string} [footer] - Custom footer message template
 * @property {string} [footerInstall] - Custom installation instructions template
 * @property {string} [footerWorkflowRecompile] - Custom footer template for workflow recompile issues
 * @property {string} [footerWorkflowRecompileComment] - Custom footer template for comments on workflow recompile issues
 * @property {string} [stagedTitle] - Custom staged mode title template
 * @property {string} [stagedDescription] - Custom staged mode description template
 * @property {string} [runStarted] - Custom workflow activation message template
 * @property {string} [runSuccess] - Custom workflow success message template
 * @property {string} [runFailure] - Custom workflow failure message template
 * @property {string} [detectionFailure] - Custom detection job failure message template
 * @property {string} [pullRequestCreated] - Custom template for pull request creation link. Placeholders: {item_number}, {item_url}
 * @property {string} [issueCreated] - Custom template for issue creation link. Placeholders: {item_number}, {item_url}
 * @property {string} [commitPushed] - Custom template for commit push link. Placeholders: {commit_sha}, {short_sha}, {commit_url}
 * @property {string} [detectionWarning] - Custom detection warning message template (continue-on-error mode). Placeholders: {workflow_name}, {run_url}, {reason_text}
 * @property {string} [agentFailureIssue] - Custom footer template for agent failure tracking issues
 * @property {string} [agentFailureComment] - Custom footer template for comments on agent failure tracking issues
 * @property {string} [closeOlderDiscussion] - Custom message for closing older discussions as outdated
 * @property {string} [bodyHeader] - Custom header text prepended to every message body (issues, comments, PRs, discussions). Placeholders: {workflow_name}, {run_url}
 * @property {string|boolean} [disclosureHeader] - AI authorship disclosure header. Set true/"true" for built-in default or provide a custom template
 * @property {boolean} [appendOnlyComments] - If true, create new comments instead of updating the activation comment
 * @property {string|boolean} [activationComments] - If false or "false", disable all activation/fallback comments entirely. Supports templatable boolean values (default: true)
 */

/**
 * Get the safe-output messages configuration from environment variable.
 * @returns {SafeOutputMessages|null} Parsed messages config or null if not set
 */
function getMessages() {
  const messagesEnv = process.env.GH_AW_SAFE_OUTPUT_MESSAGES;
  if (!messagesEnv) {
    return null;
  }

  try {
    // Parse JSON with camelCase keys from Go struct (using json struct tags)
    return JSON.parse(messagesEnv);
  } catch (error) {
    core.warning(`Failed to parse GH_AW_SAFE_OUTPUT_MESSAGES: ${getErrorMessage(error)}`);
    return null;
  }
}

/**
 * Replace placeholders in a template string with values from context.
 * Supports {key} syntax for placeholder replacement.
 * @param {string} template - Template string with {key} placeholders
 * @param {Record<string, string|number|boolean|undefined>} context - Key-value pairs for replacement
 * @returns {string} Template with placeholders replaced
 */
function renderTemplate(template, context) {
  return template.replace(/\{(\w+)\}/g, (match, key) => {
    const value = context[key];
    if (value === undefined || value === null) {
      return match;
    }
    return String(value);
  });
}

/**
 * Render a comma-separated files list into markdown inline code spans.
 * - Trims each entry and drops empty segments
 * - Accepts filenames already wrapped in backticks
 * - Redacts unsafe/invalid entries as `redacted`
 * @param {string[]|string|number|boolean} value
 * @returns {string}
 */
function renderFilesList(value) {
  const files = Array.isArray(value) ? value : String(value).split(",");
  const normalizedFiles = files.map(file => String(file).trim()).filter(Boolean);

  return normalizedFiles
    .map(file => {
      let normalized = file;
      if (normalized.startsWith("`") && normalized.endsWith("`")) {
        normalized = normalized.slice(1, -1).trim();
      }
      if (!normalized || normalized.includes("`")) {
        normalized = "redacted";
      }
      return `\`${normalized}\``;
    })
    .join(", ");
}

/**
 * Resolve the absolute path to a prompt template file.
 * Prefers GH_AW_PROMPTS_DIR when set, otherwise falls back to
 * ${RUNNER_TEMP}/gh-aw/prompts (the runtime location used in production).
 * Throws if neither GH_AW_PROMPTS_DIR nor RUNNER_TEMP is set.
 * @param {string} name - Template filename (e.g. "agent_timeout.md")
 * @returns {string} Absolute path to the prompt template file
 */
function getPromptPath(name) {
  const promptsDir = process.env.GH_AW_PROMPTS_DIR || (process.env.RUNNER_TEMP ? `${process.env.RUNNER_TEMP}/gh-aw/prompts` : null);
  if (!promptsDir) {
    throw new Error("Cannot resolve prompt path: neither GH_AW_PROMPTS_DIR nor RUNNER_TEMP is set");
  }
  return `${promptsDir}/${name}`;
}

/**
 * Read a template file and render it with the given context.
 * Combines file loading and template rendering into a single helper.
 * @param {string} templatePath - Absolute path to the template file
 * @param {Record<string, string|number|boolean|undefined>} context - Key-value pairs for replacement
 * @returns {string} Rendered template with placeholders replaced
 */
function renderTemplateFromFile(templatePath, context) {
  let template;
  try {
    template = fs.readFileSync(templatePath, "utf8");
  } catch (err) {
    throw new Error(`Failed to read file ${templatePath}: ${getErrorMessage(err)}`, { cause: err });
  }
  return renderTemplate(template, context);
}

/**
 * Convert context object keys to snake_case for template rendering.
 * Also keeps original camelCase keys for backwards compatibility.
 * @param {Record<string, any>} obj - Object with camelCase keys
 * @returns {Record<string, any>} Object with both snake_case and original keys
 */
function toSnakeCase(obj) {
  return Object.fromEntries(
    Object.entries(obj).flatMap(([key, value]) => {
      const snakeKey = key.replace(/([A-Z])/g, "_$1").toLowerCase();
      return snakeKey === key
        ? [[key, value]]
        : [
            [snakeKey, value],
            [key, value],
          ];
    })
  );
}

/**
 * RFC3986-compliant encoding for individual URI components.
 * Starts with encodeURIComponent and then additionally percent-encodes
 * characters that are still reserved in RFC3986 (`!`, `'`, `(`, `)`, `*`).
 * This prevents these characters from breaking Markdown link parsing.
 * @param {string} value
 * @returns {string}
 */
function encodeRFC3986URIComponent(value) {
  return encodeURIComponent(value).replace(/[!'()*]/g, c => "%" + c.charCodeAt(0).toString(16).toUpperCase().padStart(2, "0"));
}

/**
 * URL-encode each segment of a slash-separated path.
 * Preserves the slash separators while encoding special characters in each segment
 * using RFC3986-compliant encoding to avoid breaking Markdown link syntax.
 * @param {string} path - A slash-separated path (e.g. branch name or file path)
 * @returns {string} Path with each segment individually URL-encoded
 */
function encodePathSegments(path) {
  return path.split("/").map(encodeRFC3986URIComponent).join("/");
}

/**
 * Build a markdown list of protected files with clickable GitHub URLs.
 * Both the branch name and each file path segment are individually URL-encoded.
 * Basename-only entries (no slash) are rendered as code spans to avoid linking
 * to a potentially incorrect root-level path.
 * @param {string[]} files - Array of file paths or basenames
 * @param {string} githubServer - GitHub server URL (e.g. "https://github.com")
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string} branch - Branch name (will be URL-encoded internally)
 * @returns {string} Markdown list with one entry per line
 */
function buildProtectedFileList(files, githubServer, owner, repo, branch) {
  const encodedBranch = encodePathSegments(branch);
  return files
    .map(f => {
      // If the entry looks like a full path (contains a slash), render it as a blob link.
      // Otherwise, treat it as a basename-only entry (e.g. from manifest matching) and
      // render it as a code span to avoid linking to a potentially incorrect root path.
      if (f.includes("/")) {
        const encodedPath = encodePathSegments(f);
        return `- [${f}](${githubServer}/${owner}/${repo}/blob/${encodedBranch}/${encodedPath})`;
      }
      return `- \`${f}\``;
    })
    .join("\n");
}

/**
 * @param {string[]} files
 * @param {string} githubServer
 * @param {string} owner
 * @param {string} repo
 * @param {string} branch
 * @returns {import("./report_ast.cjs").ReportBlock[]}
 */
function buildProtectedFileBlocks(files, githubServer, owner, repo, branch) {
  return [
    {
      type: "list",
      ordered: false,
      agentTainted: false,
      items: files.map(file => [
        {
          type: "paragraph",
          agentTainted: false,
          children: file.includes("/")
            ? [{ type: "link", url: `${githubServer}/${owner}/${repo}/blob/${encodePathSegments(branch)}/${encodePathSegments(file)}`, children: [{ type: "text", value: file, agentTainted: true }], agentTainted: false }]
            : [{ type: "code", value: file, agentTainted: true }],
        },
      ]),
    },
  ];
}

module.exports = {
  getMessages,
  getPromptPath,
  renderTemplate,
  renderFilesList,
  renderTemplateFromFile,
  toSnakeCase,
  encodePathSegments,
  buildProtectedFileList,
  buildProtectedFileBlocks,
};
