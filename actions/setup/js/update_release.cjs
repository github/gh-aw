// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Update Release Handler
 *
 * Content sanitization: message.body is sanitized by updateBody helper
 * (update_pr_description_helpers.cjs line 83) before writing to GitHub.
 */

const { getErrorMessage } = require("./error_helpers.cjs");
const { updateBody } = require("./update_pr_description_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { ERR_API, ERR_CONFIG, ERR_VALIDATION } = require("./error_codes.cjs");
const { parseBoolTemplatable } = require("./templatable.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");
const { withRetry } = require("./error_recovery.cjs");

/**
 * Search published and draft releases for one matching the given tag.
 * `GET /repos/{owner}/{repo}/releases/tags/{tag}` (used for the primary lookup)
 * never returns draft releases, so a tag that only has a draft release would be
 * misreported as missing entirely. This fallback paginates `listReleases`
 * (which does include drafts for callers with push access) to find it before
 * giving up. Published releases can match too, as a safety net against any
 * eventual-consistency gap between the two endpoints.
 * @param {typeof github} client
 * @param {string} owner
 * @param {string} repo
 * @param {string} tag
 * @returns {Promise<Object | undefined>}
 */
async function findReleaseByTagIncludingDrafts(client, owner, repo, tag) {
  /** @type {Object | undefined} */
  let found;
  await withRetry(
    () =>
      client.paginate(client.rest.repos.listReleases, { owner, repo, per_page: 100 }, (response, done) => {
        const match = response.data.find(r => r.tag_name === tag);
        if (match) {
          found = match;
          done();
          return [match];
        }
        return [];
      }),
    {},
    `list releases searching for release (including drafts) with tag '${tag}' in ${owner}/${repo}`
  );
  return found;
}

/**
 * Infer the release tag from event context or dispatch inputs.
 * @param {typeof context} ctx
 * @param {typeof github} client
 * @returns {Promise<string | undefined>}
 */
async function inferReleaseTag(ctx, client) {
  if (ctx.eventName === "release") {
    const tag = ctx.payload.release?.tag_name;
    if (tag) {
      core.info(`Inferred release tag from event context: ${tag}`);
      return tag;
    }
  }

  if (ctx.eventName === "workflow_dispatch" && ctx.payload.inputs) {
    const { release_url: releaseUrl, release_id: releaseId } = ctx.payload.inputs;

    if (releaseUrl) {
      const match = releaseUrl.match(/github\.com\/[^/]+\/[^/]+\/releases\/tag\/([^/?#]+)/);
      const tag = match?.[1] ? decodeURIComponent(match[1]) : undefined;
      if (tag) {
        core.info(`Inferred release tag from release_url input: ${tag}`);
        return tag;
      }
    }

    if (releaseId) {
      const releaseIdValue = `${releaseId}`.trim();
      if (!/^[1-9]\d*$/.test(releaseIdValue)) {
        throw new Error(`${ERR_VALIDATION}: Invalid release_id input '${releaseIdValue}'. Expected a positive integer.`);
      }

      core.info(`Fetching release with ID: ${releaseIdValue}`);
      const { data: release } = await client.rest.repos.getRelease({
        owner: ctx.repo.owner,
        repo: ctx.repo.repo,
        release_id: Number(releaseIdValue),
      });
      core.info(`Inferred release tag from release_id input: ${release.tag_name}`);
      return release.tag_name;
    }
  }

  return undefined;
}

/**
 * Create a handler for update-release messages
 * This is the factory function called by the handler manager
 *
 * @param {Object} config - Handler configuration
 * @param {number} [config.max] - Maximum number of releases to update
 * @param {boolean} [config.footer] - Controls whether AI-generated footer is added (default: true)
 * @param {string} [config.body_footer] - Deterministic body footer template
 * @returns {Promise<Function>} Handler function that processes a single message
 */
async function main(config = {}) {
  const isStaged = isStagedMode(config);
  const workflowName = process.env.GH_AW_WORKFLOW_NAME || "GitHub Agentic Workflow";
  const includeFooter = parseBoolTemplatable(config.footer, true);
  const githubClient = await createAuthenticatedGitHubClient(config);

  /**
   * Process a single update-release message
   * @param {Object} message - The update-release message
   * @param {Object} resolvedTemporaryIds - Map of resolved temporary IDs
   * @returns {Promise<Object>} Result with release info
   */
  return async function handleUpdateRelease(message, resolvedTemporaryIds = {}) {
    if (isStaged) {
      logStagedPreviewInfo(`Would update release with tag ${message.tag || "(inferred)"}`);
      return { skipped: true, reason: "staged_mode" };
    }

    core.info(`Processing update-release message`);

    try {
      const messageTag = typeof message.tag === "string" ? message.tag.trim() : message.tag;
      const releaseTag = messageTag || (await inferReleaseTag(context, githubClient));

      if (!releaseTag) {
        throw new Error(`${ERR_CONFIG}: Release tag is required but not provided and cannot be inferred from event context`);
      }

      core.info(`Fetching release with tag: ${releaseTag}`);
      let release;
      try {
        const response = await withRetry(
          () =>
            githubClient.rest.repos.getReleaseByTag({
              owner: context.repo.owner,
              repo: context.repo.repo,
              tag: releaseTag,
            }),
          {},
          `get release '${releaseTag}' in ${context.repo.owner}/${context.repo.repo}`
        );
        release = response.data;
      } catch (error) {
        const errorMessage = getErrorMessage(error);
        if (error?.status === 404 || errorMessage.includes("Not Found")) {
          const repository = `${context.repo.owner}/${context.repo.repo}`;
          /** @type {Object | undefined} */
          let fallbackRelease;
          let fallbackSearchFailed = false;
          try {
            fallbackRelease = await findReleaseByTagIncludingDrafts(githubClient, context.repo.owner, context.repo.repo, releaseTag);
          } catch (fallbackError) {
            fallbackSearchFailed = true;
            core.warning(`Could not search draft releases for tag '${releaseTag}' in ${repository}: ${getErrorMessage(fallbackError)}. Falling back to the standard missing-release diagnostic.`);
          }
          if (fallbackRelease) {
            core.info(`Found release for tag '${releaseTag}' (ID: ${fallbackRelease.id}, draft: ${!!fallbackRelease.draft}) via listReleases; the tag-lookup endpoint does not return draft releases.`);
            release = fallbackRelease;
          } else {
            const createReleaseUrl = `${context.serverUrl}/${repository}/releases/new?tag=${encodeURIComponent(releaseTag)}`;
            const permissionNote = fallbackSearchFailed
              ? " Draft releases could not be checked (the search itself failed, possibly due to insufficient permissions); if a draft release exists, verify the token has push access to this repository."
              : "";
            throw new Error(
              `${ERR_VALIDATION}: No GitHub Release exists for tag '${releaseTag}' in ${repository} (checked published and draft releases). A Git tag alone is not enough; create the release at ${createReleaseUrl}, then retry.${permissionNote}`
            );
          }
        } else {
          throw error;
        }
      }

      core.info(`Found release: ${release.name || release.tag_name} (ID: ${release.id})`);

      const runUrl = buildWorkflowRunUrl(context, context.repo);
      const workflowId = process.env.GH_AW_WORKFLOW_ID || "";

      const newBody = updateBody({
        currentBody: release.body ?? "",
        newContent: message.body,
        operation: message.operation || "append",
        workflowName,
        runUrl,
        workflowId,
        includeFooter,
        bodyFooter: config.body_footer,
      });

      const { data: updatedRelease } = await githubClient.rest.repos.updateRelease({
        owner: context.repo.owner,
        repo: context.repo.repo,
        release_id: release.id,
        body: newBody,
      });

      core.info(`Successfully updated release: ${updatedRelease.html_url}`);

      return {
        tag: releaseTag,
        url: updatedRelease.html_url,
        id: updatedRelease.id,
        releaseId: updatedRelease.id,
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      const tagInfo = message.tag || "inferred from context";

      if (errorMessage.startsWith(`${ERR_CONFIG}:`)) {
        throw new Error(`${ERR_CONFIG}: ${errorMessage.slice(ERR_CONFIG.length + 2)}`);
      }
      if (errorMessage.startsWith(`${ERR_VALIDATION}:`)) {
        throw new Error(`${ERR_VALIDATION}: ${errorMessage.slice(ERR_VALIDATION.length + 2)}`);
      }

      throw new Error(`${ERR_API}: Failed to update release with tag ${tagInfo}: ${errorMessage}`);
    }
  };
}

module.exports = { main };
