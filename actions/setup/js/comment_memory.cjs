// @ts-check
/// <reference types="@actions/github-script" />
require("./shim.cjs");

const { sanitizeContent } = require("./sanitize_content.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { SAFE_OUTPUT_E001 } = require("./error_codes.cjs");
const { resolveTarget, isStagedMode } = require("./safe_output_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { renderTemplateFromFile } = require("./messages_core.cjs");
const { assembleMarkdownBodyParts } = require("./markdown_body_helpers.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");
const { getTrackerID } = require("./get_tracker_id.cjs");
const { generateHistoryUrl } = require("./generate_history_link.cjs");
const { enforceCommentLimits } = require("./comment_limit_helpers.cjs");
const { COMMENT_MEMORY_TAG, COMMENT_MEMORY_MAX_SCAN_PAGES, COMMENT_MEMORY_CODE_FENCE, buildCodeFenceOpener } = require("./comment_memory_helpers.cjs");
const { getBodyFooterMessage } = require("./messages_footer.cjs");
// Require provenance marker to avoid accidentally updating user-authored comments
// that happen to contain a matching comment-memory tag.
const MANAGED_COMMENT_PROVENANCE_MARKER = "<!-- gh-aw-agentic-workflow:";
const MANAGED_COMMENT_HEADER = "### Comment Memory";

function renderManagedCommentDisclosureNote() {
  const promptsDir = process.env.GH_AW_PROMPTS_DIR || `${process.env.RUNNER_TEMP}/gh-aw/prompts`;
  const templatePath = `${promptsDir}/comment_memory_disclosure_note.md`;
  return renderTemplateFromFile(templatePath, {});
}

function sanitizeMemoryID(memoryID) {
  const normalized = String(memoryID || "default").trim();
  if (!/^[a-zA-Z0-9_-]+$/.test(normalized)) {
    core.info(`comment_memory: rejected invalid memory_id '${normalized}'`);
    return null;
  }
  return normalized;
}

function buildManagedMemoryBody(rawBody, memoryID, options) {
  const { includeFooter, bodyFooter, runUrl, workflowName, workflowSource, workflowSourceURL, historyUrl, triggeringIssueNumber, triggeringPRNumber } = options;
  if (!/^[a-zA-Z0-9_-]+$/.test(memoryID)) {
    throw new Error(`${SAFE_OUTPUT_E001}: memory_id must contain only alphanumeric characters, hyphens, and underscores`);
  }
  core.info(`comment_memory: building managed body for memory_id='${memoryID}'`);
  // Use code-fence-as-container so the memory content is visible in GitHub's rendered Markdown.
  // The language specifier encodes the memory ID: ``````gh-aw-comment-memory:<id>
  const codeFenceOpener = buildCodeFenceOpener(memoryID);

  const markdownParts = assembleMarkdownBodyParts({
    includeFooter,
    workflowName,
    runUrl,
    workflowSource,
    workflowSourceURL,
    triggeringIssueNumber,
    triggeringPRNumber,
    historyUrl,
    markerWhenFooterDisabled: "xml",
  });

  // Inject CAUTION at top of body if threat detection warning was raised
  const detectionCaution = markdownParts.detectionCaution;
  const cautionPrefix = detectionCaution ? detectionCaution + "\n\n" : "";

  let body = `${cautionPrefix}${MANAGED_COMMENT_HEADER}\n\n${codeFenceOpener}\n${sanitizeContent(rawBody)}\n${COMMENT_MEMORY_CODE_FENCE}`;

  const tracker = getTrackerID("markdown");
  if (tracker) {
    body += `\n\n${tracker}`;
  }

  if (includeFooter) {
    core.info(`comment_memory: footer enabled for memory_id='${memoryID}'`);
    const resolvedDisclosureNote = renderManagedCommentDisclosureNote();
    body += "\n\n" + resolvedDisclosureNote;
    body += "\n\n" + markdownParts.footer;
  } else {
    core.info(`comment_memory: footer disabled for memory_id='${memoryID}', adding provenance marker only`);
    body += "\n\n" + markdownParts.noFooterMarker;
  }
  const renderedBodyFooter = getBodyFooterMessage(bodyFooter, { workflowName, runUrl });
  if (renderedBodyFooter) {
    body += "\n\n" + renderedBodyFooter.trimEnd();
  }

  core.info(`comment_memory: built body length=${body.length} for memory_id='${memoryID}'`);
  return body;
}

async function findManagedComment(github, owner, repo, itemNumber, memoryID) {
  const newFormatMarker = buildCodeFenceOpener(memoryID);
  const legacyMarker = `<${COMMENT_MEMORY_TAG} id="${memoryID}">`;
  core.info(`comment_memory: scanning comments for memory_id='${memoryID}' on #${itemNumber} in ${owner}/${repo}`);
  let page = 1;
  const perPage = 100;
  while (page <= COMMENT_MEMORY_MAX_SCAN_PAGES) {
    core.info(`comment_memory: scanning page ${page}/${COMMENT_MEMORY_MAX_SCAN_PAGES}`);
    const { data } = await github.rest.issues.listComments({
      owner,
      repo,
      issue_number: itemNumber,
      per_page: perPage,
      page,
    });
    if (!Array.isArray(data) || data.length === 0) {
      core.info(`comment_memory: no comments found on page ${page}`);
      return null;
    }
    const match = data.find(comment => {
      const body = comment.body;
      if (typeof body !== "string") {
        return false;
      }
      if (!body.includes(newFormatMarker) && !body.includes(legacyMarker)) {
        return false;
      }
      return body.includes(MANAGED_COMMENT_PROVENANCE_MARKER);
    });
    if (match) {
      core.info(`comment_memory: found existing managed comment id=${match.id} on page ${page}`);
      return match;
    }
    if (data.length < perPage) {
      core.info(`comment_memory: reached final page ${page} without match`);
      return null;
    }
    page += 1;
  }
  core.warning(`comment_memory: reached scan limit (${COMMENT_MEMORY_MAX_SCAN_PAGES} pages) without match for memory_id='${memoryID}'`);
  return null;
}

async function main(config = {}) {
  const parsedMaxCount = parseInt(String(config.max ?? "1"), 10);
  const maxCount = Number.isInteger(parsedMaxCount) && parsedMaxCount > 0 ? parsedMaxCount : 1;
  const defaultMemoryID = sanitizeMemoryID(config.memory_id || "default") || "default";
  const includeFooter = String(config.footer ?? "true") !== "false";
  const target = config.target || "triggering";
  const githubClient = await createAuthenticatedGitHubClient(config);
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
  const staged = isStagedMode(config);
  core.info(`comment_memory: initialized with max=${maxCount}, defaultMemoryID='${defaultMemoryID}', target='${target}', footer=${includeFooter}, staged=${staged}`);

  let processedCount = 0;

  return async message => {
    if (!message || message.type !== "comment_memory") {
      return null;
    }

    processedCount += 1;
    if (processedCount > maxCount) {
      core.info(`comment_memory: skipping item because max count reached (${maxCount})`);
      return { success: true, skipped: true, warning: `Skipped comment_memory item: max ${maxCount} reached` };
    }
    core.info(`comment_memory: processing item ${processedCount}/${maxCount}`);

    const targetResult = resolveTarget({
      targetConfig: target,
      item: message,
      context,
      itemType: "comment memory",
      // supportsPR=true means both issues and PRs in resolveTarget().
      supportsPR: true,
    });
    if (!targetResult.success) {
      core.warning(`comment_memory: target resolution failed: ${targetResult.error}`);
      if (!targetResult.shouldFail) {
        // No triggering context (e.g. schedule/workflow_dispatch run) — skip rather than fail
        return { success: false, skipped: true, error: targetResult.error };
      }
      return { success: false, error: targetResult.error };
    }
    core.info(`comment_memory: resolved target item_number=${targetResult.number}`);

    const repoResolution = resolveAndValidateRepo(message, defaultTargetRepo, allowedRepos, "comment memory");
    if (!repoResolution.success) {
      core.warning(`comment_memory: repo resolution failed: ${repoResolution.error}`);
      return { success: false, error: repoResolution.error };
    }
    core.info(`comment_memory: resolved target repo=${repoResolution.repo}`);

    const memoryID = sanitizeMemoryID(message.memory_id || defaultMemoryID);
    if (!memoryID) {
      return { success: false, error: "memory_id must contain only alphanumeric characters, hyphens, and underscores" };
    }
    core.info(`comment_memory: using memory_id='${memoryID}'`);

    const runUrl = buildWorkflowRunUrl(context, context.repo);
    const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";
    const workflowSource = process.env.GH_AW_WORKFLOW_SOURCE ?? "";
    const workflowSourceURL = process.env.GH_AW_WORKFLOW_SOURCE_URL ?? "";
    const triggeringIssueNumber = context.payload.issue?.number;
    const triggeringPRNumber = context.payload.pull_request?.number;
    const historyUrl =
      generateHistoryUrl({
        owner: repoResolution.repoParts.owner,
        repo: repoResolution.repoParts.repo,
        itemType: "comment",
        workflowCallId: process.env.GH_AW_CALLER_WORKFLOW_ID || "",
        workflowId: process.env.GH_AW_WORKFLOW_ID || "",
        serverUrl: context.serverUrl,
      }) || undefined;

    const managedBody = buildManagedMemoryBody(message.body || "", memoryID, {
      includeFooter,
      runUrl,
      workflowName,
      workflowSource,
      workflowSourceURL,
      historyUrl,
      triggeringIssueNumber,
      triggeringPRNumber,
      bodyFooter: config.body_footer,
    });
    try {
      enforceCommentLimits(managedBody);
    } catch (error) {
      core.warning(`comment_memory: body validation failed: ${getErrorMessage(error)}`);
      return { success: false, error: getErrorMessage(error) };
    }
    core.info(`comment_memory: body validation passed for memory_id='${memoryID}'`);

    if (staged) {
      core.info(`🎭 Staged Mode: would upsert comment-memory '${memoryID}' on #${targetResult.number} in ${repoResolution.repo}`);
      return { success: true, staged: true };
    }

    try {
      const existing = await findManagedComment(githubClient, repoResolution.repoParts.owner, repoResolution.repoParts.repo, targetResult.number, memoryID);
      if (existing) {
        core.info(`comment_memory: updating existing managed comment id=${existing.id}`);
        const { data } = await githubClient.rest.issues.updateComment({
          owner: repoResolution.repoParts.owner,
          repo: repoResolution.repoParts.repo,
          comment_id: existing.id,
          body: managedBody,
        });
        core.info(`comment_memory: updated comment url=${data.html_url}`);
        return {
          success: true,
          url: data.html_url,
          commentId: data.id,
          number: targetResult.number,
          repo: repoResolution.repo,
          managedBody,
        };
      }

      core.info(`comment_memory: creating new managed comment`);
      const { data } = await githubClient.rest.issues.createComment({
        owner: repoResolution.repoParts.owner,
        repo: repoResolution.repoParts.repo,
        issue_number: targetResult.number,
        body: managedBody,
      });
      core.info(`comment_memory: created comment id=${data.id} url=${data.html_url}`);
      return {
        success: true,
        url: data.html_url,
        commentId: data.id,
        number: targetResult.number,
        repo: repoResolution.repo,
        managedBody,
      };
    } catch (error) {
      core.warning(`comment_memory: upsert failed: ${getErrorMessage(error)}`);
      return { success: false, error: getErrorMessage(error) };
    }
  };
}

module.exports = { main, sanitizeMemoryID, findManagedComment, buildManagedMemoryBody };
