// @ts-check
/// <reference types="@actions/github-script" />

const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { ERR_NOT_FOUND } = require("./error_codes.cjs");
const { createCloseEntityHandler, checkLabelFilter, buildCommentBody, PULL_REQUEST_CONFIG } = require("./close_entity_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/**
 * Get pull request details using REST API
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} prNumber - Pull request number
 * @returns {Promise<{number: number, title: string, labels: Array<{name: string}>, html_url: string, state: string}>} Pull request details
 */
async function getPullRequestDetails(github, owner, repo, prNumber) {
  const { data: pr } = await github.rest.pulls.get({
    owner,
    repo,
    pull_number: prNumber,
  });

  if (!pr) {
    throw new Error(`${ERR_NOT_FOUND}: Pull request #${prNumber} not found in ${owner}/${repo}`);
  }

  return pr;
}

/**
 * Add comment to a GitHub Pull Request using REST API
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} prNumber - Pull request number
 * @param {string} message - Comment body
 * @returns {Promise<{id: number, html_url: string}>} Comment details
 */
async function addPullRequestComment(github, owner, repo, prNumber, message) {
  const { data: comment } = await github.rest.issues.createComment({
    owner,
    repo,
    issue_number: prNumber,
    body: message,
  });

  return comment;
}

/**
 * Close a GitHub Pull Request using REST API
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} prNumber - Pull request number
 * @returns {Promise<{number: number, html_url: string, title: string}>} Pull request details
 */
async function closePullRequest(github, owner, repo, prNumber) {
  const { data: pr } = await github.rest.pulls.update({
    owner,
    repo,
    pull_number: prNumber,
    state: "closed",
  });

  return pr;
}

/**
 * Handler factory for close-pull-request safe outputs
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  const requiredLabels = config.required_labels || [];
  const requiredTitlePrefix = config.required_title_prefix || "";
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
  const githubClient = await createAuthenticatedGitHubClient(config);
  const configuredTargetRepo = config["target-repo"] || "";

  core.info(`Close pull request configuration: max=${config.max || 10}`);
  core.info(`Configured target repo: ${configuredTargetRepo || "(unset)"}`);
  core.info(`Default target repo: ${defaultTargetRepo}`);
  if (allowedRepos.size > 0) {
    core.info(`Allowed repos: ${Array.from(allowedRepos).join(", ")}`);
  }
  if (requiredLabels.length > 0) {
    core.info(`Required labels: ${requiredLabels.join(", ")}`);
  }
  if (requiredTitlePrefix) {
    core.info(`Required title prefix: ${requiredTitlePrefix}`);
  }

  return createCloseEntityHandler(
    config,
    PULL_REQUEST_CONFIG,
    {
      resolveTarget(item) {
        // Resolve and validate target repository
        const repoResult = resolveAndValidateRepo(item, defaultTargetRepo, allowedRepos, "pull request");
        if (!repoResult.success) {
          return { success: false, error: repoResult.error };
        }
        const { repo: entityRepo, repoParts } = repoResult;

        let prNumber;
        if (item.pull_request_number !== undefined) {
          prNumber = parseInt(String(item.pull_request_number), 10);
          if (Number.isNaN(prNumber)) {
            return { success: false, error: `Invalid pull request number: ${item.pull_request_number}` };
          }
        } else {
          const contextPR = context.payload?.pull_request?.number;
          if (!contextPR) {
            return { success: false, error: "No pull_request_number provided and not in pull request context" };
          }
          prNumber = contextPR;
        }
        return { success: true, entityNumber: prNumber, owner: repoParts.owner, repo: repoParts.repo, entityRepo };
      },

      getDetails: getPullRequestDetails,

      validateLabels(entity, entityNumber, requiredLabels) {
        if (!checkLabelFilter(entity.labels, requiredLabels)) {
          return {
            valid: false,
            warning: `Skipping PR #${entityNumber}: does not match label filter (required: ${requiredLabels.join(", ")})`,
            error: "PR does not match required labels",
          };
        }
        return { valid: true };
      },

      buildCommentBody(sanitizedBody) {
        const triggeringPRNumber = context.payload?.pull_request?.number;
        const triggeringIssueNumber = context.payload?.issue?.number;
        return buildCommentBody(sanitizedBody, triggeringIssueNumber, triggeringPRNumber, config.body_footer);
      },

      addComment: addPullRequestComment,

      closeEntity(github, owner, repo, prNumber) {
        core.info(`Closing PR #${prNumber} in ${owner}/${repo}`);
        return closePullRequest(github, owner, repo, prNumber);
      },

      continueOnCommentError: true,

      buildSuccessResult(closedEntity, commentResult, wasAlreadyClosed, commentPosted) {
        return {
          success: true,
          pull_request_number: closedEntity.number,
          pull_request_url: closedEntity.html_url,
          alreadyClosed: wasAlreadyClosed,
          commentPosted,
        };
      },
    },
    githubClient
  );
}

module.exports = { main };
