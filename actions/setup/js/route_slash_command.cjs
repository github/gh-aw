// @ts-check
/// <reference types="@actions/github-script" />
// @safe-outputs-exempt SEC-004 — event body text is read only for slash-command parsing; outbound /help comments are built from internal metadata.
const { renderSizedReport } = require("./report_size.cjs");

const { REACTION_MAP } = require("./add_reaction.cjs");
const { createOrReuseStatusComment } = require("./add_workflow_run_comment.cjs");
const nodePath = require("node:path");
const { matchesCommandName, parseSlashCommand } = require("./slash_command_matcher.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
// Keep this aligned with the current default stable GitHub REST API version used by workflows.
// Update when GitHub advances the recommended version to avoid sunset/deprecation warnings.
const GITHUB_API_VERSION = "2022-11-28";

/**
 * Appends centralized command routing details to the current step summary.
 * @param {string[]} existingCommands
 * @param {string} selectedCommand
 * @returns {Promise<void>}
 */
async function appendRoutingSummary(existingCommands, selectedCommand) {
  const summary = core.summary;
  if (!summary || typeof summary.addHeading !== "function" || typeof summary.addRaw !== "function" || typeof summary.write !== "function") {
    return;
  }

  const normalizedCommands = existingCommands
    .filter(command => typeof command === "string" && command.trim())
    .map(command => `/${command.trim()}`)
    .sort();

  const selectedCommandText = selectedCommand ? `\`/${selectedCommand}\`` : "`<none>`";
  const existingCommandsList = normalizedCommands.map(command => `- \`${command}\``).join("\n");

  try {
    summary.addHeading("Agentic Commands Router", 3).addRaw(`- Selected command: ${selectedCommandText}`, true).addEOL().addRaw(`- Configured commands: ${normalizedCommands.length}`, true).addEOL();
    if (existingCommandsList) {
      summary.addEOL().addRaw(`<details><summary>Configured commands</summary>\n\n${existingCommandsList}\n\n</details>`, true).addEOL();
    }
    await summary.write({ overwrite: false });
  } catch (error) {
    core.warning(`Failed to write centralized routing details to step summary: ${getErrorMessage(error)}`);
  }
}

function eventIdentifier() {
  if (context.eventName !== "issue_comment") {
    return context.eventName;
  }
  return context.payload?.issue?.pull_request ? "pull_request_comment" : "issue_comment";
}

function resolveBodyText() {
  const bodyByEvent = {
    issues: context.payload?.issue?.body ?? "",
    pull_request: context.payload?.pull_request?.body ?? "",
    issue_comment: context.payload?.comment?.body ?? "",
    pull_request_review_comment: context.payload?.comment?.body ?? "",
    pull_request_review: context.payload?.review?.body ?? "",
    discussion: context.payload?.discussion?.body ?? "",
    discussion_comment: context.payload?.comment?.body ?? "",
  };
  return bodyByEvent[context.eventName] ?? "";
}

function isPRClosedAtStart() {
  const pullRequestState = context.payload?.pull_request?.state;
  if (pullRequestState === "closed") {
    return true;
  }
  const issueState = context.payload?.issue?.state;
  if (context.payload?.issue?.pull_request && issueState === "closed") {
    return true;
  }
  return false;
}

function normalizeDispatchRef(ref) {
  if (!ref) {
    return "";
  }
  return ref.startsWith("refs/") ? ref : `refs/heads/${ref}`;
}

async function resolveIssueBackedPRHeadRef() {
  const isIssueBackedPullRequest = context.payload?.issue?.pull_request;
  const pullNumber = context.payload?.issue?.number;
  if (!isIssueBackedPullRequest || !pullNumber) {
    return "";
  }

  try {
    const response = await github.rest.pulls.get({
      owner: context.repo.owner,
      repo: context.repo.repo,
      pull_number: pullNumber,
      headers: {
        "X-GitHub-Api-Version": GITHUB_API_VERSION,
      },
    });
    const headRef = response?.data?.head?.ref;
    if (!headRef) {
      return "";
    }
    return normalizeDispatchRef(headRef);
  } catch (error) {
    core.warning(`Failed to resolve PR head ref for #${pullNumber}: ${getErrorMessage(error)}`);
    return "";
  }
}

async function resolveDispatchRef() {
  if (process.env.GITHUB_HEAD_REF) {
    return normalizeDispatchRef(process.env.GITHUB_HEAD_REF);
  }

  const payloadHeadRef = context.payload?.pull_request?.head?.ref;
  if (payloadHeadRef) {
    return normalizeDispatchRef(payloadHeadRef);
  }

  const issuePullRequestHeadRef = await resolveIssueBackedPRHeadRef();
  if (issuePullRequestHeadRef) {
    return issuePullRequestHeadRef;
  }

  const fallbackRef = process.env.GITHUB_REF || context.ref;
  if (fallbackRef) {
    return normalizeDispatchRef(fallbackRef);
  }

  const defaultBranch = context.payload?.repository?.default_branch || "main";
  return normalizeDispatchRef(defaultBranch);
}

/** @typedef {"+1" | "-1" | "confused" | "eyes" | "heart" | "hooray" | "laugh" | "rocket"} ReactionName */

/**
 * @param {string} value
 * @returns {value is ReactionName}
 */
function isReactionName(value) {
  return Object.hasOwn(REACTION_MAP, value);
}

/**
 * @param {unknown} reaction
 * @returns {ReactionName | ""}
 */
function normalizeReaction(reaction) {
  if (typeof reaction !== "string") {
    return "";
  }
  const trimmed = reaction.trim();
  if (!trimmed || trimmed === "none") {
    return "";
  }
  return isReactionName(trimmed) ? trimmed : "";
}

function maintainsStatusComment(route) {
  return route?.status_comment === true;
}

function routeEmoji(route) {
  return typeof route?.emoji === "string" ? route.emoji : "";
}

/**
 * Returns the first valid non-"none" ai_reaction configured on matching routes.
 * @param {Array<{ai_reaction?: unknown}>} routes
 * @returns {ReactionName | ""}
 */
function resolveImmediateReaction(routes) {
  for (const route of routes) {
    const reaction = normalizeReaction(route?.ai_reaction);
    if (reaction) {
      return reaction;
    }
  }
  return "";
}

async function addImmediateReaction(reaction) {
  const normalized = normalizeReaction(reaction);
  if (!normalized) {
    return;
  }

  const { owner, repo } = context.repo;
  try {
    switch (context.eventName) {
      case "issues": {
        const issueNumber = context.payload?.issue?.number;
        if (!issueNumber) {
          core.warning("Skipping immediate reaction: issue number was not found in payload.");
          return;
        }
        await github.request("POST /repos/{owner}/{repo}/issues/{issue_number}/reactions", {
          owner,
          repo,
          issue_number: issueNumber,
          content: normalized,
          headers: { Accept: "application/vnd.github+json" },
        });
        return;
      }
      case "issue_comment": {
        const commentId = context.payload?.comment?.id;
        if (!commentId) {
          core.warning("Skipping immediate reaction: comment id was not found in payload.");
          return;
        }
        await github.request("POST /repos/{owner}/{repo}/issues/comments/{comment_id}/reactions", {
          owner,
          repo,
          comment_id: commentId,
          content: normalized,
          headers: { Accept: "application/vnd.github+json" },
        });
        return;
      }
      case "pull_request": {
        const prNumber = context.payload?.pull_request?.number;
        if (!prNumber) {
          core.warning("Skipping immediate reaction: pull request number was not found in payload.");
          return;
        }
        await github.request("POST /repos/{owner}/{repo}/issues/{issue_number}/reactions", {
          owner,
          repo,
          issue_number: prNumber,
          content: normalized,
          headers: { Accept: "application/vnd.github+json" },
        });
        return;
      }
      case "pull_request_review_comment": {
        const reviewCommentId = context.payload?.comment?.id;
        if (!reviewCommentId) {
          core.warning("Skipping immediate reaction: review comment id was not found in payload.");
          return;
        }
        await github.request("POST /repos/{owner}/{repo}/pulls/comments/{comment_id}/reactions", {
          owner,
          repo,
          comment_id: reviewCommentId,
          content: normalized,
          headers: { Accept: "application/vnd.github+json" },
        });
        return;
      }
      case "discussion_comment": {
        const commentNodeId = context.payload?.comment?.node_id;
        if (!commentNodeId) {
          core.warning("Skipping immediate reaction: discussion comment node id was not found in payload.");
          return;
        }
        await github.graphql(
          `
            mutation($subjectId: ID!, $content: ReactionContent!) {
              addReaction(input: { subjectId: $subjectId, content: $content }) {
                reaction { id }
              }
            }`,
          { subjectId: commentNodeId, content: REACTION_MAP[normalized] }
        );
        return;
      }
      case "discussion": {
        const discussionNumber = context.payload?.discussion?.number;
        if (!discussionNumber) {
          core.warning("Skipping immediate reaction: discussion number was not found in payload.");
          return;
        }
        const { repository } = await github.graphql(
          `
            query($owner: String!, $repo: String!, $num: Int!) {
              repository(owner: $owner, name: $repo) {
                discussion(number: $num) { id }
              }
            }`,
          { owner, repo, num: discussionNumber }
        );
        const discussionNodeId = repository?.discussion?.id;
        if (!discussionNodeId) {
          core.warning("Skipping immediate reaction: discussion node id was not found.");
          return;
        }
        await github.graphql(
          `
            mutation($subjectId: ID!, $content: ReactionContent!) {
              addReaction(input: { subjectId: $subjectId, content: $content }) {
                reaction { id }
              }
            }`,
          { subjectId: discussionNodeId, content: REACTION_MAP[normalized] }
        );
        return;
      }
      default:
        core.warning(`Skipping immediate reaction: unsupported event type '${context.eventName}'.`);
        return;
    }
  } catch (error) {
    core.warning(`Immediate reaction '${normalized}' failed: ${getErrorMessage(error)}`);
  }
}

async function addImmediateStatusComment(workflowEmoji) {
  try {
    const comment = await createOrReuseStatusComment({
      ...context,
      nonFatalStatusCommentErrors: true,
      workflowEmoji,
    });
    if (!comment?.id) {
      return null;
    }
    return {
      status_comment_id: String(comment.id),
      ...(comment.url ? { status_comment_url: comment.url } : {}),
      ...(comment.repo?.owner && comment.repo?.repo ? { status_comment_repo: `${comment.repo.owner}/${comment.repo.repo}` } : {}),
    };
  } catch (error) {
    core.warning(`Immediate status comment failed: ${getErrorMessage(error)}`);
    return null;
  }
}

/**
 * @param {Array<unknown>} values
 * @returns {string}
 */
function firstNonEmptyString(values) {
  for (const value of values) {
    if (typeof value === "string" && value.trim()) {
      return value;
    }
  }
  return "";
}

/**
 * Dispatches a workflow with the API version header required by GitHub REST.
 * @param {string} workflowId
 * @param {string} ref
 * @param {Record<string, string>} inputs
 * @returns {Promise<{ dispatched: boolean, run_id?: number, run_url?: string }>}
 */
async function dispatchWorkflow(workflowId, ref, inputs) {
  try {
    const dispatchArgs = {
      owner: context.repo.owner,
      repo: context.repo.repo,
      workflow_id: workflowId,
      ref,
      inputs,
      headers: {
        "X-GitHub-Api-Version": GITHUB_API_VERSION,
      },
    };

    /** @type {{ data?: any }} */
    let response;
    try {
      response = await github.request("POST /repos/{owner}/{repo}/actions/workflows/{workflow_id}/dispatches", {
        ...dispatchArgs,
        return_run_details: true,
      });
    } catch (dispatchError) {
      /** @type {any} */
      const err = dispatchError;
      const status = err && typeof err === "object" ? err.status : undefined;
      const message = typeof err?.response?.data?.message === "string" ? err.response.data.message : String(dispatchError);
      const isValidationStatus = status === 400 || status === 422;
      const mentionsReturnRunDetails = typeof message === "string" && message.toLowerCase().includes("return_run_details");
      if (!(isValidationStatus && mentionsReturnRunDetails)) {
        throw err;
      }
      core.info("Workflow dispatch endpoint rejected 'return_run_details' (common on older GHES versions); retrying without it.");
      response = await github.request("POST /repos/{owner}/{repo}/actions/workflows/{workflow_id}/dispatches", dispatchArgs);
    }

    const responseData = response?.data || {};
    // GitHub may return run metadata in either shape:
    // - flat: { workflow_run_id, workflow_run_url }  (workflow_run_url is a REST API URL)
    // - nested: { workflow_run: { id, html_url, url } }  (url is a REST API URL; html_url is the Actions page URL)
    const parsedRunId = Number(responseData?.workflow_run_id ?? responseData?.workflow_run?.id);
    const runId = Number.isFinite(parsedRunId) && parsedRunId > 0 ? parsedRunId : undefined;
    const serverUrl = context.serverUrl || process.env.GITHUB_SERVER_URL || "https://github.com";
    // Prefer the constructed HTML URL when runId is available — it is always the Actions run page URL.
    // Avoid workflow_run_url and workflow_run.url which are REST API URLs, not Actions run page URLs.
    const runUrlFromRunId = runId ? `${serverUrl}/${context.repo.owner}/${context.repo.repo}/actions/runs/${runId}` : undefined;
    const runUrlFromResponse = firstNonEmptyString([responseData?.workflow_run?.html_url]);
    const runUrl = runUrlFromRunId || runUrlFromResponse;
    return {
      dispatched: true,
      ...(runId ? { run_id: runId } : {}),
      ...(runUrl ? { run_url: runUrl } : {}),
    };
  } catch (error) {
    if (isDisabledWorkflowDispatchError(error)) {
      core.info(`Skipping workflow '${workflowId}' because it is disabled.`);
      return { dispatched: false };
    }
    throw new Error(`Failed to dispatch workflow '${workflowId}' on ref '${ref}': ${getErrorMessage(error)}`, { cause: error });
  }
}

/**
 * Update the shared status comment with dispatched workflow run metadata.
 * @param {{ status_comment_id: string, status_comment_url?: string, status_comment_repo?: string }} statusCommentContext
 * @param {string} eventName
 * @param {string} workflowId
 * @param {string|undefined} runUrl
 * @param {string|undefined} workflowEmoji
 */
async function updateStatusCommentWithDispatch(statusCommentContext, eventName, workflowId, runUrl, workflowEmoji) {
  if (!statusCommentContext?.status_comment_id) {
    return;
  }
  if (!runUrl) {
    return;
  }
  try {
    await createOrReuseStatusComment({
      ...context,
      eventName: "workflow_dispatch",
      payload: {
        inputs: {
          event_name: eventName,
          event_payload: JSON.stringify(context.payload || {}),
          aw_context: JSON.stringify({
            ...statusCommentContext,
            dispatched_workflow_name: workflowId.replace(/\.lock\.yml$/, ""),
            dispatched_run_url: runUrl,
          }),
        },
      },
      nonFatalStatusCommentErrors: true,
      workflowEmoji,
    });
  } catch (error) {
    core.warning(`Failed to update immediate status comment with dispatched run details: ${getErrorMessage(error)}`);
  }
}

function isBuiltinHelpEnabled() {
  const raw = (process.env.GH_AW_HELP_COMMAND_ENABLED || "").trim().toLowerCase();
  if (!raw || raw === "true") {
    return true;
  }
  if (raw === "false") {
    return false;
  }
  core.warning(`Invalid value for GH_AW_HELP_COMMAND_ENABLED (expected 'true' or 'false', got '${raw}'). Using default: enabled.`);
  return true;
}

function parseHelpCommandsMetadata() {
  const raw = process.env.GH_AW_HELP_COMMANDS || "[]";
  try {
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) {
      return [];
    }
    return parsed
      .flatMap(item => {
        const command = typeof item?.command === "string" ? item.command.trim() : "";
        if (!command) {
          return [];
        }
        const description = typeof item?.description === "string" ? item.description.trim() : "";
        return [
          {
            command,
            description,
            centralized: Boolean(item?.centralized),
            decentralized: Boolean(item?.decentralized),
            label: Boolean(item?.label),
            source_file: typeof item?.source_file === "string" ? item.source_file.trim() : "",
          },
        ];
      })
      .sort((left, right) => left.command.localeCompare(right.command));
  } catch (error) {
    core.warning(`Failed to parse GH_AW_HELP_COMMANDS metadata: ${getErrorMessage(error)}`);
    return [];
  }
}

/** @returns {import("./report_ast.cjs").ReportBlock[]} */
function buildCommandBullet(entry, label = false) {
  /** @type {import("./report_ast.cjs").ReportInline} */
  const command = { type: "code", value: label ? entry.command : `/${entry.command}`, agentTainted: false };
  /** @type {import("./report_ast.cjs").ReportInline[]} */
  const children = [];
  if (!label && entry.source_file) {
    const owner = context.repo.owner;
    const repo = context.repo.repo;
    const githubServer = process.env.GITHUB_SERVER_URL || "https://github.com";
    const sourceUrl = `${githubServer}/${owner}/${repo}/blob/HEAD/.github/workflows/${entry.source_file}.md`;
    children.push({ type: "link", url: sourceUrl, children: [command] });
  } else {
    children.push(command);
  }
  if (entry.description) children.push({ type: "text", value: ` — ${entry.description}`, agentTainted: false });
  return [{ type: "paragraph", children }];
}

function buildHelpCommentBody(helpCommands) {
  // Commands that are centralized should appear only in the centralized section even if
  // they are also registered as decentralized (e.g. two workflows for the same command).
  const centralized = helpCommands.filter(entry => entry.centralized);
  const centralizedNames = new Set(centralized.map(entry => entry.command));
  const decentralized = helpCommands.filter(entry => entry.decentralized && !centralizedNames.has(entry.command));
  const labels = helpCommands.filter(entry => entry.label);

  /** @type {import("./report_ast.cjs").ReportBlock[]} */
  const children = [{ type: "heading", level: 3, children: [{ type: "text", value: "Agentic Workflow Commands", agentTainted: false }] }];
  for (const [title, entries, label] of [
    ["Centralized slash commands", centralized, false],
    ["Non-centralized slash commands", decentralized, false],
    ["Label commands", labels, true],
  ]) {
    children.push(
      { type: "paragraph", children: [{ type: "strong", children: [{ type: "text", value: title, agentTainted: false }] }] },
      {
        type: "list",
        ordered: false,
        items: entries.length ? entries.map(entry => buildCommandBullet(entry, label)) : [[{ type: "paragraph", children: [{ type: "emphasis", children: [{ type: "text", value: "None", agentTainted: false }] }] }]],
      }
    );
  }

  const docsUrl = (process.env.GH_AW_SLASH_COMMAND_DOCS_URL || "").trim();
  if (docsUrl) {
    children.push({
      type: "paragraph",
      children: [
        { type: "text", value: "Learn more: ", agentTainted: false },
        { type: "link", url: docsUrl, children: [{ type: "text", value: "Slash command documentation", agentTainted: false }] },
      ],
    });
  }
  return renderSizedReport({ type: "report", title: "Agentic Workflow Commands", children }).body.trimEnd();
}

async function postBuiltinHelpComment(commentBody) {
  const owner = context.repo.owner;
  const repo = context.repo.repo;

  try {
    const issueNumber = context.payload?.issue?.number ?? context.payload?.pull_request?.number;
    if (issueNumber) {
      await github.rest.issues.createComment({
        owner,
        repo,
        issue_number: issueNumber,
        body: commentBody,
        headers: {
          "X-GitHub-Api-Version": GITHUB_API_VERSION,
        },
      });
      return true;
    }

    if (context.eventName === "discussion" || context.eventName === "discussion_comment") {
      const discussionID = context.payload?.discussion?.node_id;
      if (!discussionID) {
        core.warning("Unable to post builtin /help response: discussion node_id missing.");
        return false;
      }
      await github.graphql(
        `
          mutation($discussionId: ID!, $body: String!) {
            addDiscussionComment(input: { discussionId: $discussionId, body: $body }) {
              comment { id }
            }
          }`,
        { discussionId: discussionID, body: commentBody }
      );
      return true;
    }

    core.warning(`Unable to post builtin /help response for event '${context.eventName}'.`);
    return false;
  } catch (error) {
    core.warning(`Failed to post builtin /help comment: ${getErrorMessage(error)}`);
    return false;
  }
}

function toWorkflowDispatchID(route) {
  if (!route?.workflow || typeof route.workflow !== "string" || !route.workflow.trim()) {
    return "";
  }
  // Routing config may provide either bare workflow name ("archie") or full lock filename ("archie.lock.yml").
  const baseName = nodePath.posix.basename(route.workflow.trim());
  if (!baseName) {
    return "";
  }
  return baseName.endsWith(".lock.yml") ? baseName : `${baseName}.lock.yml`;
}

function isDisabledWorkflowDispatchError(error) {
  const status = error?.status ?? error?.response?.status;
  const message = [error?.message, error?.response?.data?.message]
    .filter(value => typeof value === "string" && value.trim())
    .join(" ")
    .toLowerCase();

  if (status !== 422 || !message) {
    return false;
  }

  return message.includes("workflow is disabled") || message.includes("workflow was disabled") || message.includes("disabled workflow");
}

/**
 * @param {Record<string, Array<{workflow?: unknown, events?: unknown, ai_reaction?: unknown, emoji?: unknown, status_comment?: unknown}>>} slashRouteMap
 * @param {string} actualCommand
 * @returns {Array<{workflow?: unknown, events?: unknown, ai_reaction?: unknown, emoji?: unknown, status_comment?: unknown}>}
 */
function resolveMatchingSlashRoutes(slashRouteMap, actualCommand) {
  /** @type {Array<{workflow?: unknown, events?: unknown, ai_reaction?: unknown, emoji?: unknown, status_comment?: unknown}>} */
  const specificRoutes = [];
  /** @type {Array<{workflow?: unknown, events?: unknown, ai_reaction?: unknown, emoji?: unknown, status_comment?: unknown}>} */
  const catchAllRoutes = [];
  const seen = new Set();

  for (const [configuredCommand, configuredRoutes] of Object.entries(slashRouteMap)) {
    if (!matchesCommandName(configuredCommand, actualCommand) || !Array.isArray(configuredRoutes)) {
      continue;
    }

    // Catch-all ("*") routes are only used as a fallback when no specific
    // routes match.  Keeping them separate ensures that a command like
    // /smoke-opencode dispatches only smoke-opencode and not skillet.
    const isCatchAll = configuredCommand === "*";

    for (const route of configuredRoutes) {
      // Keep the de-duplication key explicit so routes that differ only by
      // status-comment behavior remain distinct dispatch targets.
      const key = JSON.stringify([route?.workflow ?? "", route?.ai_reaction ?? "", route?.emoji ?? "", route?.status_comment === true, Array.isArray(route?.events) ? route.events : []]);
      if (seen.has(key)) {
        continue;
      }
      seen.add(key);
      if (isCatchAll) {
        catchAllRoutes.push(route);
      } else {
        specificRoutes.push(route);
      }
    }
  }

  return specificRoutes.length > 0 ? specificRoutes : catchAllRoutes;
}

async function main() {
  core.info("Starting centralized command routing.");
  core.info(`Incoming event name: '${context.eventName}'.`);

  let slashRouteMap;
  try {
    slashRouteMap = JSON.parse(process.env.GH_AW_SLASH_ROUTING || "{}");
  } catch (err) {
    throw new Error("Failed to parse GH_AW_SLASH_ROUTING: " + getErrorMessage(err), { cause: err });
  }
  let labelRouteMap;
  try {
    labelRouteMap = JSON.parse(process.env.GH_AW_LABEL_ROUTING || "{}");
  } catch (err) {
    throw new Error("Failed to parse GH_AW_LABEL_ROUTING: " + getErrorMessage(err), { cause: err });
  }
  core.info(`Configured centralized slash commands: ${Object.keys(slashRouteMap).length}.`);
  core.info(`Configured decentralized label commands: ${Object.keys(labelRouteMap).length}.`);
  const text = resolveBodyText();
  const selectedCommand = parseSlashCommand(text);
  await appendRoutingSummary(Object.keys(slashRouteMap), selectedCommand);

  const identifier = eventIdentifier();
  const { buildAwContext } = require("./aw_context.cjs");
  const ref = await resolveDispatchRef();
  if (isPRClosedAtStart()) {
    core.info("Pull request is closed at workflow start; skipping centralized routing.");
    return;
  }

  if (context.payload?.action === "labeled") {
    const labelName = context.payload?.label?.name ?? "";
    if (!labelName) {
      core.info("Labeled event missing label name; skipping dispatch.");
      return;
    }
    const configuredRoutes = labelRouteMap[labelName] ?? [];
    core.info(`Configured routes for label '${labelName}': ${configuredRoutes.length}.`);
    const routes = configuredRoutes.filter(route => Array.isArray(route.events) && route.events.includes(identifier));
    if (routes.length === 0) {
      core.info(`No decentralized label routes matched label '${labelName}' for event '${identifier}'.`);
      return;
    }
    core.info(`Matched routes for label '${labelName}' on '${identifier}': ${routes.map(route => route.workflow).join(", ")}.`);
    const immediateReaction = resolveImmediateReaction(routes);
    if (immediateReaction) {
      core.info(`Adding immediate '${immediateReaction}' reaction for label '${labelName}'.`);
      await addImmediateReaction(immediateReaction);
    }
    /** @type {any} */
    let statusCommentContext = null;
    if (routes.some(maintainsStatusComment)) {
      core.info(`Adding immediate status comment for label '${labelName}'.`);
      statusCommentContext = await addImmediateStatusComment(firstNonEmptyString(routes.filter(maintainsStatusComment).map(routeEmoji)));
    }
    for (const route of routes) {
      const workflowID = toWorkflowDispatchID(route);
      if (!workflowID) {
        core.warning("Skipping label route with missing workflow identifier.");
        continue;
      }
      const routeReaction = normalizeReaction(route?.ai_reaction);
      const awContext = {
        ...buildAwContext(),
        command_name: "",
        ...(routeReaction ? { desired_ai_reaction: routeReaction } : {}),
        ...(maintainsStatusComment(route) && statusCommentContext ? statusCommentContext : {}),
      };
      core.info(`Dispatching workflow '${workflowID}' for label '${labelName}'.`);
      const dispatched = await dispatchWorkflow(workflowID, ref, {
        aw_context: JSON.stringify(awContext),
      });
      if (dispatched.dispatched) {
        core.info(`Dispatched '${workflowID}' for label '${labelName}'`);
        if (maintainsStatusComment(route) && statusCommentContext) {
          await updateStatusCommentWithDispatch(statusCommentContext, identifier, workflowID, dispatched.run_url, routeEmoji(route));
        }
      }
    }
    core.info(`Completed decentralized label routing for '${labelName}'.`);
    return;
  }

  core.info(`Resolved payload text length: ${String(text).length}.`);
  core.info(`First token in payload: '${selectedCommand ? `/${selectedCommand}` : "<empty>"}'.`);
  if (!selectedCommand) {
    core.info("No slash command found at start of payload text; skipping dispatch.");
    return;
  }

  const commandName = selectedCommand;
  if (commandName === "help") {
    if (isBuiltinHelpEnabled()) {
      await addImmediateReaction("eyes");
      const posted = await postBuiltinHelpComment(buildHelpCommentBody(parseHelpCommandsMetadata()));
      if (posted) {
        core.info("Posted builtin /help command response.");
      }
      return;
    }
    // Builtin /help is disabled — fall through so custom /help workflows still dispatch.
    core.info("Builtin /help command is disabled by aw.json (help_command=false); routing normally.");
  }

  core.info(`Resolved command '/${commandName}' for event identifier '${identifier}'.`);
  const configuredRoutes = resolveMatchingSlashRoutes(slashRouteMap, commandName);
  core.info(`Configured routes for '/${commandName}': ${configuredRoutes.length}.`);
  const routes = configuredRoutes.filter(route => Array.isArray(route.events) && route.events.includes(identifier));
  if (routes.length === 0) {
    core.info(`No centralized routes matched command '/${commandName}' for event '${identifier}'.`);
    return;
  }
  core.info(`Matched routes for '/${commandName}' on '${identifier}': ${routes.map(route => route.workflow).join(", ")}.`);
  const immediateReaction = resolveImmediateReaction(routes);
  if (immediateReaction) {
    core.info(`Adding immediate '${immediateReaction}' reaction for '/${commandName}'.`);
    await addImmediateReaction(immediateReaction);
  }
  /** @type {any} */
  let statusCommentContext = null;
  if (routes.some(maintainsStatusComment)) {
    core.info(`Adding immediate status comment for '/${commandName}'.`);
    statusCommentContext = await addImmediateStatusComment(firstNonEmptyString(routes.filter(maintainsStatusComment).map(routeEmoji)));
  }

  core.info(`Dispatch ref resolved to '${ref}'.`);
  for (const route of routes) {
    const workflowID = toWorkflowDispatchID(route);
    if (!workflowID) {
      core.warning("Skipping slash route with missing workflow identifier.");
      continue;
    }
    const routeReaction = normalizeReaction(route?.ai_reaction);
    const awContext = {
      ...buildAwContext(),
      command_name: commandName,
      ...(routeReaction ? { desired_ai_reaction: routeReaction } : {}),
      ...(maintainsStatusComment(route) && statusCommentContext ? statusCommentContext : {}),
    };
    core.info(`Dispatching workflow '${workflowID}' for '/${commandName}'.`);
    const dispatched = await dispatchWorkflow(workflowID, ref, {
      aw_context: JSON.stringify(awContext),
    });
    if (dispatched.dispatched) {
      core.info(`Dispatched '${workflowID}' for '/${commandName}'`);
      if (maintainsStatusComment(route) && statusCommentContext) {
        await updateStatusCommentWithDispatch(statusCommentContext, identifier, workflowID, dispatched.run_url, routeEmoji(route));
      }
    }
  }
  core.info(`Completed centralized routing for '/${commandName}'.`);
}

module.exports = { main, parseSlashCommand, eventIdentifier, resolveBodyText, resolveDispatchRef, GITHUB_API_VERSION };
