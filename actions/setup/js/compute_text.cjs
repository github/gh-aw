// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Sanitizes content for safe output in GitHub Actions
 * @param {string} content - The content to sanitize
 * @returns {string} The sanitized content
 */
const { sanitizeIncomingText, writeRedactedDomainsLog } = require("./sanitize_incoming_text.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { parseAllowedBots, isAllowedBot } = require("./check_permissions_utils.cjs");
const { parseInboundAwContext } = require("./aw_context.cjs");

/**
 * Converts multiline content to a single line for safe workflow logging.
 * @param {string} content
 * @returns {string}
 */
function formatForWorkflowLog(content) {
  return String(content).replace(/\r?\n/g, "\\n");
}

async function main() {
  let text = "";
  let title = "";
  let body = "";

  const actor = context.actor;
  const { owner, repo } = context.repo;

  // Check if the actor has repository access (admin, maintain, write permissions)
  // Non-user actors (bots, GitHub Apps like "Copilot") may not have a user record,
  // causing the API to throw an error (e.g., "Copilot is not a user").
  // In that case, check the allowed bots list before returning empty outputs.
  let permission;
  try {
    const repoPermission = await github.rest.repos.getCollaboratorPermissionLevel({
      owner: owner,
      repo: repo,
      username: actor,
    });
    permission = repoPermission.data.permission;
    core.info(`Repository permission level: ${permission}`);
  } catch (permError) {
    core.warning(`Permission check failed for actor '${actor}': ${getErrorMessage(permError)}`);
    // Check if actor is in the allowed bots list (configured via on.bots in frontmatter)
    const allowedBots = parseAllowedBots();
    if (isAllowedBot(actor, allowedBots)) {
      core.info(`Actor '${actor}' is in the allowed bots list, treating as 'write' access`);
      permission = "write";
    } else {
      core.setOutput("text", "");
      core.setOutput("title", "");
      core.setOutput("body", "");
      return;
    }
  }

  if (permission !== "admin" && permission !== "maintain" && permission !== "write") {
    core.setOutput("text", "");
    core.setOutput("title", "");
    core.setOutput("body", "");
    return;
  }

  // Determine current body text based on event context
  switch (context.eventName) {
    case "issues":
      // For issues: title + body
      if (context.payload.issue) {
        title = context.payload.issue.title || "";
        body = context.payload.issue.body || "";
        text = `${title}\n\n${body}`;
      }
      break;

    case "pull_request":
      // For pull requests: title + body
      if (context.payload.pull_request) {
        title = context.payload.pull_request.title || "";
        body = context.payload.pull_request.body || "";
        text = `${title}\n\n${body}`;
      }
      break;

    case "pull_request_target":
      // For pull request target events: title + body
      if (context.payload.pull_request) {
        title = context.payload.pull_request.title || "";
        body = context.payload.pull_request.body || "";
        text = `${title}\n\n${body}`;
      }
      break;

    case "issue_comment":
      // For issue comments: comment body (no title)
      if (context.payload.comment) {
        body = context.payload.comment.body || "";
        text = body;
      }
      break;

    case "pull_request_review_comment":
      // For PR review comments: comment body (no title)
      if (context.payload.comment) {
        body = context.payload.comment.body || "";
        text = body;
      }
      break;

    case "pull_request_review":
      // For PR reviews: review body (no title)
      if (context.payload.review) {
        body = context.payload.review.body || "";
        text = body;
      }
      break;

    case "discussion":
      // For discussions: title + body
      if (context.payload.discussion) {
        title = context.payload.discussion.title || "";
        body = context.payload.discussion.body || "";
        text = `${title}\n\n${body}`;
      }
      break;

    case "discussion_comment":
      // For discussion comments: comment body (no title)
      if (context.payload.comment) {
        body = context.payload.comment.body || "";
        text = body;
      }
      break;

    case "release":
      // For releases: name + body
      if (context.payload.release) {
        title = context.payload.release.name || context.payload.release.tag_name || "";
        body = context.payload.release.body || "";
        text = `${title}\n\n${body}`;
      }
      break;

    case "workflow_dispatch":
      if (context.payload.inputs) {
        const releaseUrl = context.payload.inputs.release_url;
        const releaseId = context.payload.inputs.release_id;
        const awContext = parseInboundAwContext(context.payload.inputs.aw_context);

        // If release_url is provided, extract owner/repo/tag
        if (releaseUrl) {
          const urlMatch = releaseUrl.match(/github\.com\/([^\/]+)\/([^\/]+)\/releases\/tag\/([^\/]+)/);
          if (urlMatch) {
            const [, urlOwner, urlRepo, tag] = urlMatch;
            try {
              const { data: release } = await github.rest.repos.getReleaseByTag({
                owner: urlOwner,
                repo: urlRepo,
                tag: tag,
              });
              title = release.name || release.tag_name || "";
              body = release.body || "";
              text = `${title}\n\n${body}`;
            } catch (error) {
              core.warning(`Failed to fetch release from URL: ${getErrorMessage(error)}`);
            }
          }
        } else if (releaseId) {
          // If release_id is provided, fetch the release
          try {
            const { data: release } = await github.rest.repos.getRelease({
              owner: owner,
              repo: repo,
              release_id: parseInt(releaseId, 10),
            });
            title = release.name || release.tag_name || "";
            body = release.body || "";
            text = `${title}\n\n${body}`;
          } catch (error) {
            core.warning(`Failed to fetch release by ID: ${getErrorMessage(error)}`);
          }
        } else if (awContext && (!awContext.repo || awContext.repo === `${owner}/${repo}`)) {
          const positiveId = value => {
            const id = typeof value === "string" && /^[1-9]\d*$/.test(value) ? Number(value) : NaN;
            return Number.isSafeInteger(id) ? id : null;
          };
          const commentId = positiveId(awContext.comment_id);
          const itemNumber = positiveId(awContext.item_number);
          try {
            let item;
            if (commentId) {
              switch (awContext.event_type) {
                case "issue_comment":
                  item = (await github.rest.issues.getComment({ owner, repo, comment_id: commentId })).data;
                  break;
                case "pull_request_review_comment":
                  item = (await github.rest.pulls.getReviewComment({ owner, repo, comment_id: commentId })).data;
                  break;
                case "pull_request_review":
                  if (itemNumber && awContext.item_type === "pull_request") {
                    item = (await github.rest.pulls.getReview({ owner, repo, pull_number: itemNumber, review_id: commentId })).data;
                  }
                  break;
                case "discussion_comment":
                  if (typeof awContext.comment_node_id === "string" && awContext.comment_node_id && itemNumber && awContext.item_type === "discussion") {
                    const result = await github.graphql(
                      `query($id: ID!) {
                        node(id: $id) {
                          ... on DiscussionComment {
                            body
                            discussion { number repository { nameWithOwner } }
                          }
                        }
                      }`,
                      { id: awContext.comment_node_id }
                    );
                    const comment = result.node;
                    if (comment?.discussion?.number === itemNumber && comment.discussion.repository.nameWithOwner === `${owner}/${repo}`) {
                      item = comment;
                    }
                  }
                  break;
              }
              if (item) {
                body = item.body || "";
                text = body;
              }
            } else if (itemNumber) {
              switch (awContext.item_type) {
                case "issue":
                  if (awContext.event_type === "issues") item = (await github.rest.issues.get({ owner, repo, issue_number: itemNumber })).data;
                  break;
                case "pull_request":
                  if (awContext.event_type === "pull_request") item = (await github.rest.pulls.get({ owner, repo, pull_number: itemNumber })).data;
                  break;
                case "discussion":
                  if (awContext.event_type === "discussion") {
                    const result = await github.graphql(
                      `query($owner: String!, $repo: String!, $number: Int!) {
                        repository(owner: $owner, name: $repo) {
                          discussion(number: $number) { title body }
                        }
                      }`,
                      { owner, repo, number: itemNumber }
                    );
                    item = result.repository?.discussion;
                  }
                  break;
              }
              if (item) {
                title = item.title || "";
                body = item.body || "";
                text = `${title}\n\n${body}`;
              }
            }
          } catch (error) {
            core.warning(`Failed to fetch dispatched text: ${getErrorMessage(error)}`);
          }
        }
      }
      break;

    default:
      // Default: empty text
      text = "";
      break;
  }

  // Sanitize the text, title, and body before output
  // All mentions are escaped (wrapped in backticks) to prevent unintended notifications
  // Mention filtering will be applied by the agent output collector
  const sanitizedText = sanitizeIncomingText(text);
  const sanitizedTitle = sanitizeIncomingText(title);
  const sanitizedBody = sanitizeIncomingText(body);

  // Display sanitized outputs in logs
  core.info(`text: ${formatForWorkflowLog(sanitizedText)}`);
  core.info(`title: ${formatForWorkflowLog(sanitizedTitle)}`);
  core.info(`body: ${formatForWorkflowLog(sanitizedBody)}`);

  // Set the sanitized outputs
  core.setOutput("text", sanitizedText);
  core.setOutput("title", sanitizedTitle);
  core.setOutput("body", sanitizedBody);

  // Write redacted URL domains to log file if any were collected
  const logPath = writeRedactedDomainsLog();
  if (logPath) {
    core.info(`Redacted URL domains written to: ${logPath}`);
  }
}

module.exports = { main };
