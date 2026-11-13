// @ts-check
/// <reference types="@actions/github-script" />
require("./shim.cjs");

const { sanitizeContent } = require("./sanitize_content.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { SAFE_OUTPUT_E001 } = require("./error_codes.cjs");
const { resolveTarget, isStagedMode } = require("./safe_output_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { createAuthenticatedGitHubClient, createAuthenticatedLoginResolver } = require("./handler_auth.cjs");
const { renderTemplateFromFile } = require("./messages_core.cjs");
const { assembleReportBodyParts } = require("./markdown_body_helpers.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");
const { getTrackerID } = require("./get_tracker_id.cjs");
const { generateHistoryUrl } = require("./generate_history_link.cjs");
const { enforceCommentLimits, MAX_LINKS } = require("./comment_limit_helpers.cjs");
const { COMMENT_MEMORY_TAG, COMMENT_MEMORY_MAX_SCAN_PAGES, buildCodeFenceOpener, stripCommentMemoryPayloads } = require("./comment_memory_helpers.cjs");
const { getBodyFooterBlocks } = require("./messages_footer.cjs");
const { appendReportExecutionProvenance } = require("./execution_provenance.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");
const { renderSizedReport } = require("./report_size.cjs");
const { applyToNonCodeRegions } = require("./sanitize_content_core.cjs");
// Require provenance marker to avoid accidentally updating user-authored comments
// that happen to contain a matching comment-memory tag.
const MANAGED_COMMENT_PROVENANCE_MARKER = "<!-- gh-aw-agentic-workflow:";

function getFrameworkMetadataBody(body) {
  // Neither fenced memory nor legacy XML payloads are framework metadata.
  return applyToNonCodeRegions(
    stripCommentMemoryPayloads(body),
    value => value,
    () => "\n"
  );
}

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

/** @returns {import("./report_ast.cjs").ReportBlock} */
function buildCollapsedMemorySection(rawBody, memoryID, memoryTag, memoryBlock) {
  return {
    type: "details",
    summary: `Peek at saved memory (${memoryID})`,
    agentTainted: false,
    children: [memoryBlock || { type: "code_block", language: `${memoryTag}:${memoryID}`, fenceLength: 6, value: sanitizeContent(rawBody), agentTainted: true, sizeProtected: true }],
  };
}

/**
 * @param {string} previousBody
 * @param {string} runUrl
 * @param {string} workflowName
 * @returns {import("./report_ast.cjs").ReportBlock}
 */
function buildTransactionHistory(previousBody, runUrl, workflowName) {
  const previous = parseReportMarkdown(getFrameworkMetadataBody(previousBody), { allowMetadata: true, agentTainted: true });
  const history = previous.find(node => node.type === "details" && node.summary === "History");
  const trustedRun = /^(https:\/\/[\w.-]+(?::\d+)?\/[\w.-]+\/[\w.-]+\/actions\/runs\/)[1-9]\d*$/.exec(runUrl);
  /** @type {Map<string, string>} */
  const runs = new Map();
  const addRun = url => {
    const match = /^(https:\/\/[\w.-]+(?::\d+)?\/[\w.-]+\/[\w.-]+\/actions\/runs\/)([1-9]\d*)$/.exec(url);
    if (match && trustedRun && match[1] === trustedRun[1]) {
      runs.set(url, `Workflow (run ${match[2]})`);
    }
  };
  if (history?.type === "details") {
    for (const list of history.children) {
      if (list.type !== "list") continue;
      for (const item of list.items) {
        for (const block of item) {
          if (block.type !== "paragraph") continue;
          for (const link of block.children) {
            if (link.type === "link") {
              addRun(link.url);
            }
          }
        }
      }
    }
  } else {
    // Older comments only retain their most recent writer in the attribution.
    for (const node of previous) {
      if (node.type !== "xml_marker" || node.name !== "gh-aw-agentic-workflow") continue;
      const url = node.fields?.find(field => field.key === "run")?.value;
      if (url) addRun(url);
    }
  }
  runs.delete(runUrl);
  runs.set(runUrl, `${workflowName} (run ${runUrl.split("/").pop()})`);
  return {
    type: "details",
    summary: "History",
    agentTainted: false,
    sizeProtected: true,
    children: [
      {
        type: "list",
        ordered: false,
        agentTainted: false,
        items: [...runs].map(([url, label]) => [
          {
            type: "paragraph",
            agentTainted: false,
            children: [{ type: "link", url, agentTainted: false, children: [{ type: "text", value: label, agentTainted: false }] }],
          },
        ]),
      },
    ],
  };
}

/** @returns {import("./report_ast.cjs").Report} */
function buildManagedMemoryReport(rawBody, memoryID, options) {
  const { includeFooter, bodyFooter, runUrl, workflowName, workflowSource, workflowSourceURL, historyUrl, triggeringIssueNumber, triggeringPRNumber, memoryTag = COMMENT_MEMORY_TAG, memoryBlock, previousBody = "" } = options;
  if (!/^[a-zA-Z0-9_-]+$/.test(memoryID)) {
    throw new Error(`${SAFE_OUTPUT_E001}: memory_id must contain only alphanumeric characters, hyphens, and underscores`);
  }
  core.info(`comment_memory: building managed body for memory_id='${memoryID}'`);

  const markdownParts = assembleReportBodyParts({
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

  /** @type {import("./report_ast.cjs").ReportBlock[]} */
  const children = [
    ...markdownParts.detectionCaution,
    { type: "heading", level: 3, agentTainted: false, children: [{ type: "text", value: "Comment Memory", agentTainted: false }] },
    buildCollapsedMemorySection(rawBody, memoryID, memoryTag, memoryBlock),
    buildTransactionHistory(previousBody, runUrl, workflowName),
  ];

  const tracker = getTrackerID("markdown");
  if (tracker) {
    children.push(...parseReportMarkdown(tracker, { allowMetadata: true, agentTainted: false }));
  }

  if (includeFooter) {
    core.info(`comment_memory: footer enabled for memory_id='${memoryID}'`);
    const resolvedDisclosureNote = renderManagedCommentDisclosureNote();
    children.push(...parseReportMarkdown(resolvedDisclosureNote, { minimumHeadingLevel: 2, agentTainted: false }), ...markdownParts.footer);
  } else {
    core.info(`comment_memory: footer disabled for memory_id='${memoryID}', adding provenance marker only`);
    children.push(...markdownParts.noFooterMarker);
  }
  children.push(...getBodyFooterBlocks(bodyFooter, { workflowName, runUrl }));

  return { type: "report", title: "Comment Memory", children };
}

function buildManagedMemoryBody(rawBody, memoryID, options) {
  return renderSizedReport(buildManagedMemoryReport(rawBody, memoryID, options)).body;
}

async function findManagedComment(github, owner, repo, itemNumber, memoryID) {
  const newFormatMarker = buildCodeFenceOpener(memoryID);
  const legacyMarker = `<${COMMENT_MEMORY_TAG} id="${memoryID}">`;
  const getAuthenticatedLogin = createAuthenticatedLoginResolver(github, core);
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
    const candidates = data.filter(comment => {
      const body = comment.body;
      if (typeof body !== "string") {
        return false;
      }
      if (!body.includes(newFormatMarker) && !body.includes(legacyMarker)) {
        return false;
      }
      return getFrameworkMetadataBody(body).includes(MANAGED_COMMENT_PROVENANCE_MARKER);
    });
    const authenticatedLogin = candidates.length > 0 ? await getAuthenticatedLogin() : null;
    const match = candidates.find(comment => {
      if (!comment.user?.login) return false;
      return authenticatedLogin ? comment.user.login === authenticatedLogin : comment.user.type === "Bot";
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

    try {
      const existing = staged ? null : await findManagedComment(githubClient, repoResolution.repoParts.owner, repoResolution.repoParts.repo, targetResult.number, memoryID);
      const report = buildManagedMemoryReport(message.body || "", memoryID, {
        includeFooter,
        runUrl,
        workflowName,
        workflowSource,
        workflowSourceURL,
        historyUrl,
        triggeringIssueNumber,
        triggeringPRNumber,
        bodyFooter: config.body_footer,
        previousBody: existing?.body || "",
      });
      const managedBody = renderSizedReport(appendReportExecutionProvenance(report, workflowName, runUrl)).body;
      const transactionHistory = report.children.find(node => node.type === "details" && node.summary === "History");
      const historyList = transactionHistory?.type === "details" ? transactionHistory.children[0] : undefined;
      const historyLinkCount = historyList?.type === "list" ? historyList.items.length : 0;
      // Accumulated framework run links do not consume the agent's link budget.
      enforceCommentLimits(managedBody, { maxLinks: MAX_LINKS + historyLinkCount });
      core.info(`comment_memory: body validation passed for memory_id='${memoryID}'`);

      if (staged) {
        core.info(`🎭 Staged Mode: would upsert comment-memory '${memoryID}' on #${targetResult.number} in ${repoResolution.repo}`);
        return { success: true, staged: true };
      }

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
      const reason = getErrorMessage(error);
      const errorMessage = reason.includes("exceeds maximum length") ? `${reason} No comment was changed. Reduce the saved memory size or use a new memory_id; existing History entries cannot be dropped.` : reason;
      core.warning(`comment_memory: upsert failed: ${errorMessage}`);
      return { success: false, error: errorMessage };
    }
  };
}

module.exports = { main, sanitizeMemoryID, findManagedComment, buildManagedMemoryBody };
