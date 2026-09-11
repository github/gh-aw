// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { renderTemplateFromFile } = require("./messages_core.cjs");
const { generateFooterWithExpiration } = require("./ephemerals.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { parseBoolTemplatable } = require("./templatable.cjs");
const { appendConfiguredBodyFooter } = require("./body_footer.cjs");

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/**
 * Build a shared handler factory for missing issue handlers.
 * Encapsulates the common search-or-create issue pipeline, differing only in
 * template paths, item field names, and item renderers.
 *
 * @param {Object} options
 * @param {string} options.handlerType - Handler type identifier used in log/warning messages
 * @param {string} options.defaultTitlePrefix - Default issue title prefix (e.g. "[missing data]")
 * @param {string} options.itemsField - Field name in the message containing the items array
 * @param {string} options.templatePath - Absolute path to the issue body template file
 * @param {string} options.templateListKey - Template variable name for the rendered items list
 * @param {(runUrl: string) => string[]} options.buildCommentHeader - Returns header lines for the comment body given runUrl
 * @param {(item: any, index: number) => string[]} options.renderCommentItem - Renders a single item for an existing-issue comment
 * @param {(item: any, index: number) => string[]} options.renderIssueItem - Renders a single item for a new-issue body
 * @param {string[]} [options.defaultLabels] - Labels always applied to created issues (merged with config.labels)
 * @param {(message: any) => Array<any> | null | undefined} [options.fallbackItems] - Optional fallback item builder used when the message items array is missing/empty
 * @returns {HandlerFactoryFunction}
 */
function buildMissingIssueHandler(options) {
  const { handlerType, defaultTitlePrefix, itemsField, templatePath, templateListKey, buildCommentHeader, renderCommentItem, renderIssueItem, defaultLabels = [], fallbackItems } = options;

  return async function main(config = {}) {
    // Extract configuration
    // create_issue: templatable boolean — default true.
    // Accepts: literal boolean (true/false), string 'true'/'false', or a GitHub Actions
    // expression (e.g. '${{ inputs.create-incomplete-issue }}'). Expressions are evaluated
    // by GitHub Actions before this handler runs, so config.create_issue holds the
    // resolved boolean or string value when the handler executes.
    const createIssue = parseBoolTemplatable(config.create_issue, true);
    const titlePrefix = config.title_prefix || defaultTitlePrefix;
    const userLabels = config.labels ? (Array.isArray(config.labels) ? config.labels : config.labels.split(",")).map(label => String(label).trim()).filter(label => label) : [];
    const envLabels = [...new Set([...defaultLabels, ...userLabels])];
    const maxCount = config.max || 1; // Default to 1 to create only one issue per workflow run

    core.info(`Title prefix: ${titlePrefix}`);
    if (envLabels.length > 0) {
      core.info(`Default labels: ${envLabels.join(", ")}`);
    }
    core.info(`Max count: ${maxCount}`);

    // Track how many items we've processed for max limit
    let processedCount = 0;

    // Track created/updated issues
    const processedIssues = [];

    /**
     * Create or update an issue for the missing items
     * @param {string} workflowName - Name of the workflow
     * @param {string} workflowSource - Source path of the workflow
     * @param {string} workflowSourceURL - URL to the workflow source
     * @param {string} runUrl - URL to the workflow run
     * @param {Array<Object>} items - Array of missing item objects
     * @returns {Promise<Object>} Result with success/error status
     */
    async function createOrUpdateIssue(workflowName, workflowSource, workflowSourceURL, runUrl, items) {
      const { owner, repo } = context.repo;

      // Create issue title
      const issueTitle = `${titlePrefix} ${workflowName}`;

      core.info(`Checking for existing issue with title: "${issueTitle}"`);

      // Search for existing open issue with this title
      const searchQuery = `repo:${owner}/${repo} is:issue is:open in:title "${issueTitle}"`;

      try {
        const searchResult = await github.rest.search.issuesAndPullRequests({
          q: searchQuery,
          per_page: 1,
        });

        if (searchResult.data.total_count > 0) {
          // Issue exists, add a comment
          const existingIssue = searchResult.data.items[0];
          core.info(`Found existing issue #${existingIssue.number}: ${existingIssue.html_url}`);

          // Build comment body
          const commentLines = buildCommentHeader(runUrl);
          items.forEach((item, index) => {
            commentLines.push(...renderCommentItem(item, index));
          });
          commentLines.push(`---`);
          commentLines.push(`> Workflow: [${workflowName}](${workflowSourceURL})`);
          commentLines.push(`> Run: ${runUrl}`);

          const commentBody = appendConfiguredBodyFooter(sanitizeContent(commentLines.join("\n")), config.body_footer);

          await github.rest.issues.createComment({
            owner,
            repo,
            issue_number: existingIssue.number,
            body: commentBody,
          });

          core.info(`✓ Added comment to existing issue #${existingIssue.number}`);

          return {
            success: true,
            issue_number: existingIssue.number,
            issue_url: existingIssue.html_url,
            action: "updated",
          };
        } else {
          // No existing issue, create a new one
          core.info("No existing issue found, creating a new one");

          // Build items list for template
          const issueListLines = [];
          items.forEach((item, index) => {
            issueListLines.push(...renderIssueItem(item, index));
          });

          // Create template context
          const templateContext = {
            workflow_name: workflowName,
            workflow_source_url: workflowSourceURL || "#",
            run_url: runUrl,
            workflow_source: workflowSource,
            [templateListKey]: issueListLines.join("\n"),
          };

          // Load and render the issue template
          const issueBodyContent = renderTemplateFromFile(templatePath, templateContext);

          // Add expiration marker (1 week from now) in a quoted section using helper
          const footer = generateFooterWithExpiration({
            footerText: `> Workflow: [${workflowName}](${workflowSourceURL})`,
            expiresHours: 24 * 7, // 7 days
          });
          const issueBody = appendConfiguredBodyFooter(sanitizeContent(`${issueBodyContent}\n\n${footer}`), config.body_footer);

          const newIssue = await github.rest.issues.create({
            owner,
            repo,
            title: issueTitle,
            body: issueBody,
            labels: envLabels,
          });

          core.info(`✓ Created new issue #${newIssue.data.number}: ${newIssue.data.html_url}`);

          return {
            success: true,
            issue_number: newIssue.data.number,
            issue_url: newIssue.data.html_url,
            action: "created",
          };
        }
      } catch (error) {
        core.warning(`Failed to create or update issue: ${getErrorMessage(error)}`);
        return {
          success: false,
          error: getErrorMessage(error),
        };
      }
    }

    /**
     * Message handler function that processes a single missing-issue message.
     * Accepts the same two-argument signature as all other handler types so the
     * handler manager can call it uniformly; resolvedTemporaryIds is unused here.
     * @param {Object} message - The message to process
     * @param {Object} _resolvedTemporaryIds - Temporary ID map (unused for missing-issue handlers)
     * @returns {Promise<Object>} Result with success/error status and issue details
     */
    return async function handleMissingIssue(message, _resolvedTemporaryIds) {
      // When create-issue is disabled (e.g. via a resolved GitHub Actions expression),
      // skip issue creation without recording a failure.
      if (!createIssue) {
        core.info(`${handlerType}: create-issue is disabled, skipping issue creation`);
        return { success: true, skipped: true, reason: "create-issue disabled" };
      }

      // Check if we've hit the max limit
      if (processedCount >= maxCount) {
        core.warning(`Skipping ${handlerType}: max count of ${maxCount} reached`);
        return {
          success: false,
          error: `Max count of ${maxCount} reached`,
        };
      }

      processedCount++;

      // Resolve fields from message, falling back to environment variables for fields
      // that the agent may not know (e.g. workflow_name is not part of the agent's context).
      const workflowName = message.workflow_name || process.env.GH_AW_WORKFLOW_NAME || "";
      const workflowSource = message.workflow_source || "";
      const workflowSourceURL = message.workflow_source_url || process.env.GH_AW_WORKFLOW_SOURCE_URL || "";
      const runUrl =
        message.run_url || (process.env.GITHUB_SERVER_URL && process.env.GITHUB_REPOSITORY && process.env.GITHUB_RUN_ID ? `${process.env.GITHUB_SERVER_URL}/${process.env.GITHUB_REPOSITORY}/actions/runs/${process.env.GITHUB_RUN_ID}` : "");
      let items = message[itemsField];

      // Validate required fields
      if (!workflowName) {
        core.warning(`Missing required field: workflow_name`);
        return {
          success: false,
          error: "Missing required field: workflow_name",
        };
      }

      if ((!items || !Array.isArray(items) || items.length === 0) && typeof fallbackItems === "function") {
        items = fallbackItems(message);
      }

      if (!items || !Array.isArray(items) || items.length === 0) {
        core.warning(`Missing or empty ${itemsField} array`);
        return {
          success: false,
          error: `Missing or empty ${itemsField} array`,
        };
      }

      // Create or update the issue
      const result = await createOrUpdateIssue(workflowName, workflowSource, workflowSourceURL, runUrl, items);

      if (result.success) {
        processedIssues.push(result);
      }

      return result;
    };
  };
}

module.exports = { buildMissingIssueHandler };
