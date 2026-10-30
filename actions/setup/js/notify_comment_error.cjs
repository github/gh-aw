// @ts-check
/// <reference types="@actions/github-script" />

// This script updates an existing comment created by the activation job
// to notify about the workflow completion status (success or failure).
// It also adds the categorized, privacy-safe noop body to the activation comment.

const { loadAgentOutput } = require("./load_agent_output.cjs");
const { getRunSuccessMessage, getRunFailureMessage, getDetectionFailureMessage, getDetectionWarningMessage } = require("./messages_run_status.cjs");
const { getMessages } = require("./messages_core.cjs");
const { getErrorMessage, isLockedError } = require("./error_helpers.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");
const { parseBoolTemplatable } = require("./templatable.cjs");
const { resolveTopLevelDiscussionCommentId } = require("./github_api_helpers.cjs");
const { assembleReportBodyParts } = require("./markdown_body_helpers.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");
const { renderSizedReport } = require("./report_size.cjs");
const { normalizeReportAllowedURL } = require("./report_ast.cjs");
const { buildNoopConclusionSummary } = require("./conclusion_summary.cjs");

/**
 * Collect generated asset URLs from safe output jobs
 * @returns {Array<string>} Array of generated asset URLs
 */
function collectGeneratedAssets() {
  const assets = [];

  // Get the safe output jobs mapping from environment
  const safeOutputJobsEnv = process.env.GH_AW_SAFE_OUTPUT_JOBS;
  if (!safeOutputJobsEnv) {
    return assets;
  }

  let jobOutputMapping;
  try {
    jobOutputMapping = JSON.parse(safeOutputJobsEnv);
  } catch (error) {
    core.warning(`Failed to parse GH_AW_SAFE_OUTPUT_JOBS: ${getErrorMessage(error)}`);
    return assets;
  }

  // Iterate through each job and collect its URL output
  for (const [jobName, urlKey] of Object.entries(jobOutputMapping)) {
    // Access the job output using the GitHub Actions context
    // The value will be set as an environment variable in the format GH_AW_OUTPUT_<JOB>_<KEY>
    const envVarName = `GH_AW_OUTPUT_${jobName.toUpperCase()}_${urlKey.toUpperCase()}`;
    const url = process.env[envVarName];

    if (url && url.trim() !== "") {
      assets.push(url);
      core.info(`Collected asset URL: ${url}`);
    }
  }

  return assets;
}

/**
 * Attempt to add a "needs-review" label to the triggering issue/PR.
 * This is a best-effort operation — failures are logged but do not fail the workflow.
 * @param {string|undefined} commentRepo - Repository in "owner/repo" format
 */
async function tryAddNeedsReviewLabel(commentRepo) {
  try {
    const repoOwner = commentRepo ? commentRepo.split("/")[0] : context.repo.owner;
    const repoName = commentRepo ? commentRepo.split("/")[1] : context.repo.repo;
    const issueNumber = context.payload?.issue?.number || context.payload?.pull_request?.number;

    if (!issueNumber) {
      core.info("No issue/PR number found — skipping needs-review label");
      return;
    }

    core.info(`Adding "needs-review" label to ${repoOwner}/${repoName}#${issueNumber}`);
    await github.request("POST /repos/{owner}/{repo}/issues/{issue_number}/labels", {
      owner: repoOwner,
      repo: repoName,
      issue_number: issueNumber,
      labels: ["needs-review"],
      headers: {
        Accept: "application/vnd.github+json",
      },
    });
    core.info('Successfully added "needs-review" label');
  } catch (/** @type {any} */ error) {
    // Best-effort: label might not exist in the repo, or permissions may be insufficient
    core.warning(`Failed to add "needs-review" label: ${getErrorMessage(error)}`);
  }
}

/**
 * Map agent conclusion and assignment error count to a run-failure status string.
 * @param {string} agentConclusion - The agent job conclusion value
 * @param {number} assignToAgentErrorCount - Number of assign-to-agent errors
 * @param {string|undefined} safeOutputsResult - The safe_outputs job result value
 * @returns {string} Human-readable status text for use in failure messages
 */
function getRunFailureStatusText(agentConclusion, assignToAgentErrorCount, safeOutputsResult) {
  if (safeOutputsResult === "failure") {
    return "failed to deliver outputs";
  } else if (agentConclusion === "success" && assignToAgentErrorCount > 0) {
    return "failed to assign the coding agent";
  } else if (agentConclusion === "cancelled") {
    return "was cancelled";
  } else if (agentConclusion === "skipped") {
    return "was skipped";
  } else if (agentConclusion === "timed_out") {
    return "timed out";
  } else {
    return "failed";
  }
}

async function main() {
  const commentId = process.env.GH_AW_COMMENT_ID;
  const commentRepo = process.env.GH_AW_COMMENT_REPO;
  const runUrl = process.env.GH_AW_RUN_URL;
  const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";
  const agentConclusion = process.env.GH_AW_AGENT_CONCLUSION || "failure";
  const detectionConclusion = process.env.GH_AW_DETECTION_CONCLUSION;
  const detectionReason = process.env.GH_AW_DETECTION_REASON || "";
  const assignToAgentErrorCount = Number(process.env.GH_AW_ASSIGNMENT_ERROR_COUNT || "0");
  if (!Number.isFinite(assignToAgentErrorCount) || !Number.isSafeInteger(assignToAgentErrorCount) || assignToAgentErrorCount < 0) {
    throw new Error(`${ERR_VALIDATION}: GH_AW_ASSIGNMENT_ERROR_COUNT must be a non-negative integer`);
  }
  const safeOutputsResult = process.env.GH_AW_SAFE_OUTPUTS_RESULT;
  const noopCommentBody = process.env.GH_AW_NOOP_COMMENT_BODY || "";
  const activationCommentLinks = (process.env.GH_AW_ACTIVATION_COMMENT_LINKS || "").trim();

  const messagesConfig = getMessages();
  const appendOnlyComments = messagesConfig?.appendOnlyComments === true;

  // If activation comments are disabled entirely, skip all comment updates
  if (!parseBoolTemplatable(messagesConfig?.activationComments, true)) {
    core.info("activation-comments is disabled: skipping completion comment update");
    return;
  }

  core.info(`Comment ID: ${commentId}`);
  core.info(`Comment Repo: ${commentRepo}`);
  core.info(`Run URL: ${runUrl}`);
  core.info(`Workflow Name: ${workflowName}`);
  core.info(`Agent Conclusion: ${agentConclusion}`);
  if (detectionConclusion) {
    core.info(`Detection Conclusion: ${detectionConclusion}`);
  }
  if (assignToAgentErrorCount > 0) {
    core.info(`Assignment Error Count: ${assignToAgentErrorCount}`);
  }
  if (safeOutputsResult) {
    core.info(`Safe Outputs Result: ${safeOutputsResult}`);
  }

  // Load agent output to check for noop messages
  let noopMessages = [];
  const agentOutputResult = loadAgentOutput();
  if (agentOutputResult.success) {
    const noopItems = agentOutputResult.items.filter(item => item.type === "noop");
    if (noopItems.length > 0) {
      core.info(`Found ${noopItems.length} noop message(s)`);
      noopMessages = noopItems.map(item => item.message);
    }
  }

  // If append-only is enabled, we do NOT require an activation comment ID.
  // If it's disabled, and there's no comment to update but we have noop messages, write to step summary.
  if (!appendOnlyComments && !commentId && noopMessages.length > 0) {
    core.info("No comment ID found, writing noop messages to step summary");
    const summaryContent = buildNoopConclusionSummary(noopMessages, { runUrl });
    await core.summary.addRaw(summaryContent).write();
    core.info(`Successfully wrote ${noopMessages.length} noop message(s) to step summary`);
    return;
  }

  if (!appendOnlyComments && !commentId) {
    core.info("No comment ID found and no noop messages to process, skipping comment update");
    return;
  }

  // At this point, we have a comment to update
  if (!runUrl) {
    core.setFailed(`${ERR_VALIDATION}: Run URL is required`);
    return;
  }

  // Parse comment repo (format: "owner/repo")
  const repoOwner = commentRepo ? commentRepo.split("/")[0] : context.repo.owner;
  const repoName = commentRepo ? commentRepo.split("/")[1] : context.repo.repo;

  core.info(`Updating comment in ${repoOwner}/${repoName}`);

  // Determine the message based on agent conclusion using custom messages if configured
  let message;
  let detectionWarningMessage = "";

  // Check if detection job failed (if detection job exists)
  if (detectionConclusion && detectionConclusion === "failure") {
    // Detection job failed in error mode - report this prominently
    message = getDetectionFailureMessage({
      workflowName: "{workflow_name}",
      runUrl: "{run_url}",
    });
  } else if (detectionConclusion && detectionConclusion === "warning") {
    // Detection job produced a warning (continue-on-error mode)
    // Show success message but append caution section with progressive disclosure
    if (agentConclusion === "success" && assignToAgentErrorCount === 0 && safeOutputsResult !== "failure") {
      message = getRunSuccessMessage({
        workflowName: "{workflow_name}",
        runUrl: "{run_url}",
      });
    } else {
      message = getRunFailureMessage({
        workflowName: "{workflow_name}",
        runUrl: "{run_url}",
        status: getRunFailureStatusText(agentConclusion, assignToAgentErrorCount, safeOutputsResult),
      });
    }
    // Build the caution section for detection warning
    detectionWarningMessage = getDetectionWarningMessage({
      workflowName: "{workflow_name}",
      runUrl: "{run_url}",
      reason: detectionReason,
    });
  } else if (agentConclusion === "success" && assignToAgentErrorCount === 0 && safeOutputsResult !== "failure") {
    message = getRunSuccessMessage({
      workflowName: "{workflow_name}",
      runUrl: "{run_url}",
    });
  } else {
    message = getRunFailureMessage({
      workflowName: "{workflow_name}",
      runUrl: "{run_url}",
      status: getRunFailureStatusText(agentConclusion, assignToAgentErrorCount, safeOutputsResult),
    });
  }

  const templateContext = Object.fromEntries(Array.from((message + detectionWarningMessage).matchAll(/\{(\w+)\}/g), match => [match[1], match[0]]));
  Object.assign(templateContext, { workflow_name: sanitizeContent(workflowName), run_url: runUrl });
  const children = parseReportMarkdown(sanitizeContent(message), { context: templateContext, agentTainted: false, minimumHeadingLevel: 2 });

  // Import template and existing Markdown separately so an unclosed fence cannot
  // consume the following status sections or generated metadata.
  if (detectionWarningMessage) {
    children.push(...parseReportMarkdown(sanitizeContent(detectionWarningMessage), { context: templateContext, agentTainted: false, minimumHeadingLevel: 2 }));
  }

  // Preserve links (e.g. "Pull request created: #X") that safe outputs appended to the
  // activation comment; otherwise this final status update would overwrite them.
  if (activationCommentLinks) {
    children.push(...parseReportMarkdown(sanitizeContent(activationCommentLinks), { agentTainted: false, minimumHeadingLevel: 2 }));
  }

  // Never fall back to raw agent prose in a public comment if rendering failed.
  if (noopCommentBody) {
    children.push(...parseReportMarkdown(sanitizeContent(noopCommentBody), { agentTainted: true, minimumHeadingLevel: 2 }));
  } else if (noopMessages.length > 0) {
    core.warning("No categorized no-op comment body is available; omitting agent messages from the public comment");
  }

  // Collect generated asset URLs from safe output jobs
  const generatedAssets = collectGeneratedAssets();
  if (generatedAssets.length > 0) {
    generatedAssets.forEach(url => {
      /** @type {import("./report_ast.cjs").ReportInline} */
      let asset = { type: "text", value: sanitizeContent(url), agentTainted: false };
      try {
        asset = { type: "link", url: normalizeReportAllowedURL(url), children: [asset] };
      } catch {
        // Keep a sanitized visible value when an asset is not an allowed URL.
      }
      children.push({ type: "paragraph", children: [asset] });
    });
  }

  // Build the generated footer (attribution + XML marker). Appended after sanitization
  // so that the XML traceability marker is not stripped by sanitizeContent.
  const workflowSource = process.env.GH_AW_WORKFLOW_SOURCE ?? "";
  const workflowSourceURL = process.env.GH_AW_WORKFLOW_SOURCE_URL ?? "";
  const triggeringIssueNumber = context.payload?.issue?.number;
  const triggeringPRNumber = context.payload?.pull_request?.number;
  const triggeringDiscussionNumber = context.payload?.discussion?.number;
  const markdownParts = assembleReportBodyParts({
    includeFooter: true,
    workflowName,
    runUrl,
    workflowSource,
    workflowSourceURL,
    triggeringIssueNumber,
    triggeringPRNumber,
    triggeringDiscussionNumber,
  });
  const sanitizedMessage = renderSizedReport({ type: "report", title: "Workflow status", children: [...children, ...markdownParts.footer] }).body;

  // Add "needs-review" label when detection produced a warning
  if (detectionConclusion === "warning") {
    await tryAddNeedsReviewLabel(commentRepo);
  }

  // Append-only mode: create a new comment instead of updating the activation comment.
  if (appendOnlyComments) {
    try {
      const eventName = context.eventName;

      // Discussions: create a new discussion comment (threaded reply for discussion_comment)
      if (eventName === "discussion" || eventName === "discussion_comment") {
        const discussionNumber = context.payload?.discussion?.number;
        if (!discussionNumber) {
          core.warning("Unable to determine discussion number for append-only comment; skipping");
          return;
        }

        const { repository } = await github.graphql(
          `
          query($owner: String!, $repo: String!, $num: Int!) {
            repository(owner: $owner, name: $repo) {
              discussion(number: $num) {
                id
              }
            }
          }`,
          { owner: repoOwner, repo: repoName, num: discussionNumber }
        );

        const discussionId = repository?.discussion?.id;
        if (!discussionId) {
          core.warning("Unable to resolve discussion id for append-only comment; skipping");
          return;
        }

        // GitHub Discussions only supports two nesting levels, so if the triggering comment is
        // itself a reply, we resolve the top-level parent's node ID.
        const replyToId = eventName === "discussion_comment" ? await resolveTopLevelDiscussionCommentId(github, context.payload?.comment?.node_id) : null;
        const mutation = replyToId
          ? `mutation($dId: ID!, $body: String!, $replyToId: ID!) {
              addDiscussionComment(input: { discussionId: $dId, body: $body, replyToId: $replyToId }) {
                comment { id url }
              }
            }`
          : `mutation($dId: ID!, $body: String!) {
              addDiscussionComment(input: { discussionId: $dId, body: $body }) {
                comment { id url }
              }
            }`;

        const variables = replyToId ? { dId: discussionId, body: sanitizedMessage, replyToId } : { dId: discussionId, body: sanitizedMessage };
        const result = await github.graphql(mutation, variables);
        const created = result?.addDiscussionComment?.comment;
        core.info("Successfully created append-only discussion comment");
        if (created?.id) core.info(`Comment ID: ${created.id}`);
        if (created?.url) core.info(`Comment URL: ${created.url}`);
        return;
      }

      // Issues/PRs: determine issue number from event payload and create a new issue comment
      const issueNumber = context.payload?.issue?.number || context.payload?.pull_request?.number;
      if (!issueNumber) {
        core.warning("Unable to determine issue/PR number for append-only comment; skipping");
        return;
      }

      const response = await github.request("POST /repos/{owner}/{repo}/issues/{issue_number}/comments", {
        owner: repoOwner,
        repo: repoName,
        issue_number: issueNumber,
        body: sanitizedMessage,
        headers: {
          Accept: "application/vnd.github+json",
        },
      });

      core.info("Successfully created append-only comment");
      if (response?.data?.id) core.info(`Comment ID: ${response.data.id}`);
      if (response?.data?.html_url) core.info(`Comment URL: ${response.data.html_url}`);
      return;
    } catch (error) {
      // Check if the error is due to a locked issue/PR/discussion
      if (isLockedError(error)) {
        // Silently ignore locked resource errors - just log for debugging
        core.info(`Cannot create append-only comment: resource is locked (this is expected and not an error)`);
        return;
      }

      // Don't fail the workflow if we can't create the comment (for other errors)
      core.warning(`Failed to create append-only comment: ${getErrorMessage(error)}`);
      return;
    }
  }

  // At this point, we must have a comment ID (verified by earlier checks)
  if (!commentId) {
    core.setFailed(`${ERR_VALIDATION}: Comment ID is required for updating existing comment`);
    return;
  }

  // Check if this is a discussion comment (GraphQL node ID format)
  const isDiscussionComment = commentId.startsWith("DC_");

  try {
    if (isDiscussionComment) {
      // Update discussion comment using GraphQL
      const result = await github.graphql(
        `
        mutation($commentId: ID!, $body: String!) {
          updateDiscussionComment(input: { commentId: $commentId, body: $body }) {
            comment {
              id
              url
            }
          }
        }`,
        { commentId: commentId, body: sanitizedMessage }
      );

      const comment = result.updateDiscussionComment.comment;
      core.info(`Successfully updated discussion comment`);
      core.info(`Comment ID: ${comment.id}`);
      core.info(`Comment URL: ${comment.url}`);
    } else {
      // Update issue/PR comment using REST API
      const response = await github.request("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", {
        owner: repoOwner,
        repo: repoName,
        comment_id: parseInt(commentId, 10),
        body: sanitizedMessage,
        headers: {
          Accept: "application/vnd.github+json",
        },
      });

      core.info(`Successfully updated comment`);
      core.info(`Comment ID: ${response.data.id}`);
      core.info(`Comment URL: ${response.data.html_url}`);
    }
  } catch (error) {
    // Check if the error is due to a locked issue/PR/discussion
    if (isLockedError(error)) {
      // Silently ignore locked resource errors - just log for debugging
      core.info(`Cannot update comment: resource is locked (this is expected and not an error)`);
      return;
    }

    // Don't fail the workflow if we can't update the comment (for other errors)
    core.warning(`Failed to update comment: ${getErrorMessage(error)}`);
  }
}

module.exports = { main };
