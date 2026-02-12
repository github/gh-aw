// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Sanitizes content for safe output in GitHub Actions
 * @param {string} content - The content to sanitize
 * @returns {string} The sanitized content
 */
const { sanitizeIncomingText, writeRedactedDomainsLog } = require("./sanitize_incoming_text.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

async function main() {
  let text = "";
  let title = "";
  let body = "";

  const actor = context.actor;
  const { owner, repo } = context.repo;

  // Check if the actor has repository access (admin, maintain permissions)
  const repoPermission = await github.rest.repos.getCollaboratorPermissionLevel({
    owner: owner,
    repo: repo,
    username: actor,
  });

  const permission = repoPermission.data.permission;
  core.info(`Repository permission level: ${permission}`);

  if (permission !== "admin" && permission !== "maintain") {
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
      // For workflow dispatch: check for release_url or release_id in inputs
      if (context.payload.inputs) {
        const releaseUrl = context.payload.inputs.release_url;
        const releaseId = context.payload.inputs.release_id;

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
  core.info(`text: ${sanitizedText}`);
  core.info(`title: ${sanitizedTitle}`);
  core.info(`body: ${sanitizedBody}`);

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
