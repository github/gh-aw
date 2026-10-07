// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_API, ERR_SYSTEM } = require("./error_codes.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { generateFooterWithExpiration } = require("./ephemerals.cjs");
const { renderTemplateFromFile, getPromptPath } = require("./messages_core.cjs");
const { loadAgentOutput } = require("./load_agent_output.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { generateHistoryUrl } = require("./generate_history_link.cjs");
const { formatAIC } = require("./model_costs.cjs");
const { reduceModelNameToIdentifier } = require("./model_aliases.cjs");
const { buildNoopConclusionSummary } = require("./conclusion_summary.cjs");
/**
 * Search for or create the parent issue for all agentic workflow no-op runs
 * @returns {Promise<{number: number, node_id: string}>} Parent issue number and node ID
 */
async function ensureAgentRunsIssue() {
  const { owner, repo } = context.repo;
  const parentTitle = "[aw] No-Op Runs";
  const parentLabel = "agentic-workflows";

  core.info(`Searching for no-op runs issue: "${parentTitle}"`);

  // Search for existing no-op runs issue
  const searchQuery = `repo:${owner}/${repo} is:issue is:open label:${parentLabel} in:title "${parentTitle}"`;

  try {
    const { data } = await github.rest.search.issuesAndPullRequests({
      q: searchQuery,
      per_page: 1,
    });

    if (data.total_count > 0) {
      const existingIssue = data.items[0];
      core.info(`Found existing no-op runs issue #${existingIssue.number}: ${existingIssue.html_url}`);

      return {
        number: existingIssue.number,
        node_id: existingIssue.node_id,
      };
    }
  } catch (error) {
    throw new Error(`${ERR_API}: Failed to search for existing no-op runs issue: ${getErrorMessage(error)}`, { cause: error });
  }

  // Create no-op runs issue if it doesn't exist
  core.info(`No no-op runs issue found, creating one`);

  // Load template from file
  const templatePath = getPromptPath("noop_runs_issue.md");
  let parentBodyContent;
  try {
    parentBodyContent = fs.readFileSync(templatePath, "utf8");
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to read file ${templatePath}: ${getErrorMessage(err)}`, { cause: err });
  }

  const parentBody = generateFooterWithExpiration({
    footerText: parentBodyContent,
    expiresHours: 24 * 30, // 30 days
  });

  const { data: newIssue } = await github.rest.issues.create({
    owner,
    repo,
    title: parentTitle,
    body: parentBody,
    labels: [parentLabel],
  });

  core.info(`✓ Created no-op runs issue #${newIssue.number}: ${newIssue.html_url}`);
  return {
    number: newIssue.number,
    node_id: newIssue.node_id,
  };
}

/**
 * Parse a raw AIC environment variable value and return it as a positive number.
 * Returns undefined when the value is absent, non-numeric, or non-positive.
 * @param {string|undefined} raw
 * @returns {number|undefined}
 */
function parsePositiveAIC(raw) {
  const parsed = raw ? Number.parseFloat(raw) : NaN;
  return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined;
}

/**
 * @param {string} label
 * @param {number|undefined} value
 * @param {string|undefined} [modelAlias]
 * @returns {string}
 */
function buildAICEntry(label, value, modelAlias) {
  const formatted = typeof value === "number" ? formatAIC(value) : "";
  if (!formatted) {
    return "";
  }
  const prefix = [label, modelAlias].filter(Boolean).join(" ");
  return ` · ${prefix ? `${prefix}${modelAlias ? " · " : " "}` : ""}${formatted} AIC`;
}

function buildAICSuffix() {
  const agentAIC = parsePositiveAIC(process.env.GH_AW_AIC);
  const detectionAIC = parsePositiveAIC(process.env.GH_AW_THREAT_DETECTION_AIC);
  const evalsAIC = parsePositiveAIC(process.env.GH_AW_EVALS_AIC);
  const compressedModelName = reduceModelNameToIdentifier(require("./model_fallback.cjs").getFallbackModel() || process.env.GH_AW_PRIMARY_MODEL || process.env.GH_AW_ENGINE_MODEL);
  const agentSuffix = buildAICEntry("", agentAIC, compressedModelName);
  const detectionSuffix = buildAICEntry("⌖", detectionAIC);
  const evalsSuffix = buildAICEntry("◇", evalsAIC);
  return `${agentSuffix}${detectionSuffix}${evalsSuffix}`;
}

/**
 * Build the ambient context suffix string for use in comment footers.
 * Returns a string like " · ⊞ 1.2K" or "" when not available.
 * @returns {string}
 */
function buildAmbientContextSuffix() {
  const raw = process.env.GH_AW_AMBIENT_CONTEXT;
  const parsed = raw ? Number.parseInt(raw, 10) : NaN;
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return "";
  }
  // Format compact integer values: e.g. 1200 → "1.2K"
  const formatted = parsed >= 1000 ? `${(parsed / 1000).toFixed(1)}K` : String(parsed);
  return ` · ⊞ ${formatted}`;
}

/**
 * Build a markdown history link for use in comment footers.
 * Returns a string like " · [◷](url)" or "" when not available.
 * @returns {string}
 */
function buildHistoryLink() {
  const workflowId = process.env.GH_AW_WORKFLOW_ID || "";
  if (!workflowId) {
    return "";
  }
  const { owner, repo } = context.repo;
  const historyUrl = generateHistoryUrl({
    owner,
    repo,
    itemType: "comment",
    workflowId,
    serverUrl: context.serverUrl,
  });
  return historyUrl ? ` · [◷](${historyUrl})` : "";
}

/**
 * Render the no-op comment body shared by the no-op runs issue comment and the
 * comment posted on other run-scoped tracking items.
 * @param {string} workflowName
 * @param {string} message
 * @param {string} runUrl
 * @returns {string}
 */
function buildNoopCommentBody(workflowName, message, runUrl) {
  const commentTemplatePath = getPromptPath("noop_comment.md");
  const commentBody = renderTemplateFromFile(commentTemplatePath, {
    workflow_name: workflowName,
    message,
    run_url: runUrl,
    aic_suffix: buildAICSuffix(),
    ambient_context_suffix: buildAmbientContextSuffix(),
    history_link: buildHistoryLink(),
  });
  return sanitizeContent(commentBody, { maxLength: 65000 });
}

/**
 * Process no-op safe outputs and optionally post to the no-op runs issue.
 * This merged step replaces the separate "Process no-op messages" + "Handle No-Op Message"
 * steps, eliminating the cross-step output dependency on GH_AW_NOOP_MESSAGE.
 *
 * Behaviour:
 * 1. Load noop items directly from the agent output artifact.
 * 2. In staged mode: write a summary preview and exit without posting.
 * 3. Otherwise: write a summary, set the `noop_message` step output, then post to the
 *    "[aw] No-Op Runs" tracking issue when the agent produced only noop outputs.
 */
async function main() {
  try {
    // --- Load and filter noop items from agent output ---
    const result = loadAgentOutput();
    if (!result.success) {
      core.info("Could not load agent output, skipping");
      return;
    }

    const maxCount = Number(process.env.GH_AW_NOOP_MAX || "0");
    if (!Number.isFinite(maxCount) || !Number.isSafeInteger(maxCount) || maxCount < 0) {
      throw new Error(`${ERR_SYSTEM}: GH_AW_NOOP_MAX must be a non-negative integer`);
    }
    const limitedMaxCount = maxCount;
    const allNoopItems = (result.items || []).filter(/** @param {any} item */ item => item.type === "noop");
    const noopItems = limitedMaxCount > 0 ? allNoopItems.slice(0, limitedMaxCount) : allNoopItems;

    if (noopItems.length === 0) {
      core.info("No noop items found in agent output");
      return;
    }

    core.info(`Found ${noopItems.length} noop item(s)`);
    const noopMessage = noopItems[0].message;

    // --- Staged mode: preview only, do not post ---
    if (isStagedMode()) {
      const summaryContent = buildNoopConclusionSummary(
        noopItems.map(item => item.message),
        { runUrl: process.env.GH_AW_RUN_URL, staged: true }
      );
      await core.summary.addRaw(summaryContent).write();
      core.info("📝 No-op message preview written to step summary");
      return;
    }

    // --- Write step summary ---
    for (let index = 0; index < noopItems.length; index++) {
      core.info(`No-op message ${index + 1}: ${noopItems[index].message}`);
    }
    const summaryContent = buildNoopConclusionSummary(
      noopItems.map(item => item.message),
      { runUrl: process.env.GH_AW_RUN_URL }
    );
    await core.summary.addRaw(summaryContent).write();

    // Export for downstream steps/jobs
    core.setOutput("noop_message", noopMessage);
    core.info(`Successfully processed ${noopItems.length} noop message(s)`);

    // --- Post to no-op runs issue ---
    const workflowName = process.env.GH_AW_WORKFLOW_NAME || "unknown";
    const runUrl = process.env.GH_AW_RUN_URL || "";
    const agentConclusion = process.env.GH_AW_AGENT_CONCLUSION || "";
    const reportAsIssue = process.env.GH_AW_NOOP_REPORT_AS_ISSUE !== "false"; // Default to true

    // Render the comment body once so later steps (e.g. discarding an unused
    // another run-scoped tracking item) can post the exact same message.
    let commentBody = "";
    try {
      commentBody = buildNoopCommentBody(workflowName, noopMessage, runUrl);
      core.setOutput("noop_comment_body", commentBody);
    } catch (error) {
      core.warning(`Could not render no-op comment body: ${getErrorMessage(error)}`);
    }

    core.info(`Workflow name: ${workflowName}`);
    core.info(`Run URL: ${runUrl}`);
    core.info(`Agent conclusion: ${agentConclusion}`);
    core.info(`Report as issue: ${reportAsIssue}`);

    if (!reportAsIssue) {
      core.info("report-as-issue is disabled (set to false), skipping no-op message posting to issue");
      return;
    }

    // Only post to "agent runs" issue if:
    // 1. The agent succeeded (agentConclusion === "success"), OR
    // 2. The agent failed but produced only noop outputs, which indicates a transient AI model
    //    error after the meaningful work (noop) was already captured. Skipped/cancelled runs
    //    and other non-success/non-failure conclusions are always skipped.
    if (agentConclusion !== "success" && agentConclusion !== "failure") {
      core.info(`Agent did not succeed (conclusion: ${agentConclusion}), skipping no-op message posting`);
      return;
    }

    // Skip posting when there are non-noop outputs (agent did real work)
    const nonNoopItems = result.items.filter(/** @param {any} item */ ({ type }) => type !== "noop");
    if (nonNoopItems.length > 0) {
      core.info(`Found ${nonNoopItems.length} non-noop output(s), skipping no-op message posting`);
      return;
    }

    if (agentConclusion === "failure") {
      core.info("Agent failed but produced only noop outputs (transient AI model error after noop was captured) - posting noop message");
    } else {
      core.info("Agent succeeded with only noop outputs - posting to no-op runs issue");
    }

    const { owner, repo } = context.repo;

    // Ensure no-op runs issue exists
    let noopRunsIssue;
    try {
      noopRunsIssue = await ensureAgentRunsIssue();
    } catch (error) {
      core.warning(`Could not create no-op runs issue: ${getErrorMessage(error)}`);
      // Don't fail the workflow if we can't create the issue
      return;
    }

    // Reuse the comment body rendered above; it is empty only when rendering failed.
    if (!commentBody) {
      core.warning("No-op comment body is unavailable, skipping no-op message posting");
      return;
    }

    try {
      await github.rest.issues.createComment({
        owner,
        repo,
        issue_number: noopRunsIssue.number,
        body: commentBody,
      });

      core.info(`✓ Posted no-op message to no-op runs issue #${noopRunsIssue.number}`);
    } catch (error) {
      core.warning(`Failed to post comment to no-op runs issue: ${getErrorMessage(error)}`);
      // Don't fail the workflow
    }
  } catch (error) {
    core.warning(`Error in handle_noop_message: ${getErrorMessage(error)}`);
    // Don't fail the workflow
  }
}

module.exports = { main, ensureAgentRunsIssue };
