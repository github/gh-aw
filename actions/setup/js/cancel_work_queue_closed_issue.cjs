// @ts-check
"use strict";

const { canonical, digest, parseStrictJSON, queueError } = require("./work_queue_codec.cjs");
const { issueIdentity } = require("./work_queue_issue_contract.cjs");
const { preflightIssues, statusLabelName, STATUSES } = require("./work_queue_issue_api.cjs");
const { issuesConfiguration } = require("./work_queue_issues.cjs");
const { parseSettings } = require("./work_queue_settings.cjs");
const { nativeId, authenticatePublisher } = require("./work_queue_native.cjs");
const { actorFromContext } = require("./work_queue_policy.cjs");
const { newRequest, generateRequestOperations } = require("./work_queue_replay.cjs");
const { workExpired } = require("./work_queue_expiration.cjs");
const { publishWorkQueueRequest } = require("./work_queue_store.cjs");
const { readCheckedQueue } = require("./work_queue_checked_transport.cjs");
const { expectedGitHubHost } = require("./work_queue_host.cjs");
const { renderSizedReport } = require("./report_size.cjs");

const REASON = "related_issue_closed";

function closureIdentity(resource, workId) {
  return digest({ resource, work_id: workId });
}

async function readIssuesConfig(githubClient, context) {
  let response;
  try {
    response = await githubClient.rest.repos.getContent({
      ...context.repo,
      path: ".github/workflows/aw.json",
      ref: context.sha,
    });
  } catch (error) {
    if (error.status === 404) return null;
    throw error;
  }
  if (response?.data?.type !== "file" || response.data.encoding !== "base64" || typeof response.data.content !== "string" || response.data.size > 1024 * 1024) throw queueError("request_invalid", "work queue configuration is unavailable");
  const config = parseStrictJSON(Buffer.from(response.data.content.replace(/\s/g, ""), "base64").toString("utf8"));
  return issuesConfiguration(parseSettings(config?.work_queue).issues);
}

function matchingWork(state, resource) {
  const matches = [...state.works.values()].filter(work => {
    return [work.backing_issue, work.subject].some(binding => binding?.kind === "issue" && canonical(binding) === canonical(resource) && issueIdentity(binding) === issueIdentity(resource));
  });
  if (matches.length > 1) throw queueError("issue_binding_conflict", "Issue belongs to multiple Work items; cancellation requires an unambiguous exact target");
  return matches[0];
}

async function closedIssue(githubClient, context, resource, config) {
  const target = { work_id: "closed_issue", resource };
  const issue = (await preflightIssues(githubClient, [target])).get(target.work_id);
  if (!issue || issue.state !== "CLOSED") return false;
  const names = new Set(issue.labels.nodes.map(label => label.name));
  return names.has(config.label) && STATUSES.some(status => names.has(statusLabelName(config, status)));
}

async function hasConfirmation(githubClient, context, number, marker) {
  for (let page = 1; page <= 20; page++) {
    const response = await githubClient.rest.issues.listComments({ ...context.repo, issue_number: number, per_page: 100, page });
    if (!Array.isArray(response.data)) throw queueError("projection_comment_pending", "Issue comments are inaccessible");
    if (response.data.some(comment => typeof comment.body === "string" && comment.body.includes(marker))) return true;
    if (response.data.length < 100) return false;
  }
  throw queueError("projection_comment_pending", "Issue comment pagination exceeded; confirmation cannot safely be posted");
}

async function main(options = {}) {
  const context = options.context || global.context;
  const githubClient = options.githubClient || global.github;
  const coreApi = options.core || global.core;
  if (context.eventName !== "issues" || context.payload?.action !== "closed") return { status: "ignored" };
  const eventIssue = context.payload.issue;
  if (!eventIssue || eventIssue.pull_request || nativeId(eventIssue.number) !== nativeId(context.issue?.number)) throw queueError("issue_binding_invalid", "Issue closure event identity is invalid");
  const repoId = nativeId(context.payload.repository?.id);
  const repository = context.payload.repository.full_name;
  if (typeof repository !== "string" || repository.toLowerCase() !== `${context.repo.owner}/${context.repo.repo}`.toLowerCase()) throw queueError("issue_binding_invalid", "Issue closure event repository differs from queue repository");
  const number = Number(nativeId(eventIssue.number));
  const resource = {
    kind: "issue",
    host: expectedGitHubHost(),
    repository,
    repository_id: repoId,
    resource_id: nativeId(eventIssue.id),
    number: String(number),
  };
  const read = options.readWorkQueueLog || readCheckedQueue;
  const queueOptions = { githubClient, ...context.repo };
  const initial = await read(queueOptions);
  if (!initial.sha) return { status: "queue_missing" };
  const work = matchingWork(initial.state, resource);
  if (!work) return { status: "unbound" };
  const config = options.issues === undefined ? await readIssuesConfig(githubClient, context) : issuesConfiguration(options.issues);
  if (!config) return { status: "disabled" };
  const trusted = await (options.authenticatePublisher || authenticatePublisher)({ githubClient, context, role: "administrator" });
  if (trusted.repository.toLowerCase() !== repository.toLowerCase() || trusted.event !== "issues" || trusted.workflow !== ".github/workflows/agentic-workqueue.yml")
    throw queueError("actor_unauthorized", "Issue closure requires the authenticated Agentic Workqueue workflow");
  if (nativeId(trusted.native_run?.repository?.id) !== repoId) throw queueError("issue_binding_invalid", "Issue closure event repository differs from authenticated repository");
  const fresh = options.closedIssue || closedIssue;
  if (!(await fresh(githubClient, context, resource, config))) return { status: "stale" };
  const actor = actorFromContext(trusted);
  const operation = { kind: "WorkCancellation", work_id: work.work_id, reason: REASON };
  const request = newRequest(`issue-closed:${closureIdentity(resource, work.work_id)}`, "cancel_work", actor, { operations: [operation] });
  const publish = options.publishWorkQueueRequest || publishWorkQueueRequest;
  let result;
  if (work.state !== "cancelled" && work.state !== "completed")
    result = await publish({
      ...queueOptions,
      context: trusted,
      actor,
      request,
      core: coreApi,
      generateOperations: async (state, stable, currentActor, at, id, observations) => {
        const current = matchingWork(state, resource);
        if (!current || current.work_id !== work.work_id) throw queueError("issue_binding_invalid", "related Issue binding changed");
        if (current.state === "cancelled" || current.state === "completed") return { operations: [], reason: "already_terminal" };
        if (!(await fresh(githubClient, context, resource, config))) return { operations: [], reason: "issue_reopened" };
        if (workExpired(current, Math.max(at, state.time_watermark ?? 0))) return { operations: [], reason: "work_expired" };
        return generateRequestOperations(state, stable, currentActor, at, id, observations);
      },
    });
  const confirmed = await read(queueOptions);
  const receipt = confirmed.state.requests.get(request.id);
  const finalWork = matchingWork(confirmed.state, resource);
  const ownReceipt = receipt && receipt.request.fingerprint === request.fingerprint && receipt.operations.some(op => canonical(op) === canonical(operation));
  // A checked terminal commit, whether ours or an earlier request's, is the
  // only authority for the outcome reported in the Issue.
  // Derived expiration has no cancellation receipt; a retained commit or
  // checked checkpoint receipt, not an as-of watermark, must reach its deadline.
  const committedExpiration =
    finalWork?.state === "cancelled" &&
    finalWork.cancellation_reason === "work_expired" &&
    finalWork.expires_at !== undefined &&
    (confirmed.state.transactions.some(commit => commit.at >= finalWork.expires_at) || confirmed.state.historicalTransactions?.some(commit => commit.at >= finalWork.expires_at));
  const terminal = finalWork?.state === "completed" ? finalWork.completion_id : finalWork?.state === "cancelled" ? finalWork.cancellation_commit_id || committedExpiration : undefined;
  if (!terminal || (result?.publishedNow === true && !ownReceipt)) return { status: "pending" };
  const state = finalWork.state;
  const identity = closureIdentity(resource, work.work_id);
  const marker = `<!-- gh-aw-issue-closure:${identity} -->`;
  if (!(await (options.hasConfirmation || hasConfirmation)(githubClient, context, number, marker))) {
    const outcome =
      state === "cancelled"
        ? ownReceipt
          ? "Cancellation was recorded in the work queue ledger. New state: "
          : "The work was already cancelled in the work queue ledger; this Issue closure did not change it. New state: "
        : "The work was already completed in the work queue ledger; this Issue closure did not cancel it. New state: ";
    const body = renderSizedReport({
      type: "report",
      title: "Work queue closure",
      children: [
        {
          type: "paragraph",
          children: [
            { type: "text", value: outcome, agentTainted: false },
            { type: "strong", children: [{ type: "text", value: state, agentTainted: false }] },
            { type: "text", value: state === "cancelled" && ownReceipt ? ". The work is now canceled." : ".", agentTainted: false },
          ],
        },
        {
          type: "paragraph",
          children: [
            { type: "link", url: `https://github.com/${repository}/blob/work-queue/work-queue.jsonl`, children: [{ type: "text", value: "View the work queue ledger", agentTainted: false }] },
            { type: "text", value: ".", agentTainted: false },
          ],
        },
        { type: "xml_marker", name: "gh-aw-issue-closure", value: identity, compact: true, agentTainted: false },
      ],
    }).body;
    try {
      await githubClient.rest.issues.createComment({ ...context.repo, issue_number: number, body, request: { retries: 0, timeout: 15000 } });
    } catch (error) {
      // A lost response is not permission to repeat a potentially accepted comment.
      if (!(await (options.hasConfirmation || hasConfirmation)(githubClient, context, number, marker))) throw error;
    }
  }
  coreApi?.info(`Work queue Issue closure observed ${state} Work`);
  return { status: state, publishedNow: result?.publishedNow === true };
}

if (require.main === module) main().catch(error => global.core.setFailed(error instanceof Error ? error.message : "Issue closure failed"));

module.exports = { main, matchingWork, closedIssue, readIssuesConfig, hasConfirmation };
