// @ts-check
"use strict";

const { loadQueue, publishRequest } = require("./work_queue_binding.cjs");
const { queueError } = require("./work_queue_codec.cjs");

const REASON = "related_issue_closed";

/** Work whose backing Issue is the given native Issue id in this repository. @param {any} state @param {string} repository @param {string} issueId */
function backedWork(state, repository, issueId) {
  for (const work of state.works.values()) {
    const issue = work.backing_issue;
    if (issue && issue.resource_id === issueId && issue.repository.toLowerCase() === repository.toLowerCase()) return work;
  }
  return undefined;
}

/** Whether the live Issue still has the event's immutable id and is closed. @param {any} githubClient @param {any} context @param {number} number @param {string} issueId */
async function issueStillClosed(githubClient, context, number, issueId) {
  const { data } = await githubClient.rest.issues.get({ owner: context.repo.owner, repo: context.repo.repo, issue_number: number });
  return String(data?.id) === issueId && data?.state === "closed" && !data?.pull_request;
}

/**
 * Issue-closed trigger: cancel the available Work backed by the closed Issue.
 * @param {{githubClient?: any, context?: any, core?: any, branch?: string}} [options]
 */
async function main(options = {}) {
  const context = options.context || global.context;
  const githubClient = options.githubClient || global.github;
  const coreApi = options.core || global.core;
  if (context.eventName !== "issues" || context.payload?.action !== "closed") return { status: "ignored" };
  const issue = context.payload.issue;
  if (!issue || issue.pull_request || issue.id === undefined || !Number.isSafeInteger(issue.number)) throw queueError("issue_binding_invalid", "Issue closure event has no Issue");
  const repository = `${context.repo.owner}/${context.repo.repo}`;
  const issueId = String(issue.id);
  const queue = { githubClient, context, role: "producer", core: coreApi, ...(options.branch === undefined ? {} : { branch: options.branch }) };
  const { state } = await loadQueue(queue);
  const work = backedWork(state, repository, issueId);
  if (!work) return { status: "unbound" };
  if (work.state !== "available") {
    coreApi?.info(`Work ${work.work_id} is ${work.state}; Issue closure does not cancel it`);
    return { status: work.state };
  }
  const published = await publishRequest({
    ...queue,
    kind: "cancel_work",
    parameters: { work_ids: [work.work_id], reason: REASON },
    // Re-read the live Issue on every publication attempt: a delayed or rerun close event
    // must not cancel Work after the backing Issue was reopened (or replaced).
    operationsFor: async latest => {
      if (latest.works.get(work.work_id)?.state !== "available") return [];
      if (!(await issueStillClosed(githubClient, context, issue.number, issueId))) {
        coreApi?.info(`Issue #${issue.number} is no longer closed; Work ${work.work_id} stays available`);
        return [];
      }
      return [{ kind: "WorkCancellation", work_id: work.work_id, reason: REASON }];
    },
  });
  const status = published.state.works.get(work.work_id)?.state ?? "unbound";
  coreApi?.info(`Work queue Issue closure: Work ${work.work_id} is ${status}`);
  return { status, committed: published.committed === true };
}

if (require.main === module) main().catch(error => global.core.setFailed(error instanceof Error ? error.message : "Issue closure failed"));

module.exports = { backedWork, main };
