// @ts-check
"use strict";

const { loadQueue, publishRequest } = require("./work_queue_binding.cjs");
const { readWorkQueuePolicyEnv } = require("./work_queue_policy.cjs");
const { fifoCompare } = require("./work_queue_scheduler.cjs");
const { addComment, closeIssue, createIssue, getIssue, issueResource, listLabelledOpenIssues, recentComments, repositoryId } = require("./work_queue_issue_api.cjs");
const { claimComment, commentMarker, issueBody, issueTitle, markedWorkId, orphanComment, terminalComment } = require("./work_queue_issue_messages.cjs");

// Work items projected per invocation; the rest wait for the next projection.
const MAX_PROJECTED_WORK = 50;

/** @param {unknown} error */
function message(error) {
  return error instanceof Error ? error.message : String(error);
}

/**
 * Best-effort Issue projection of the queue state. Never throws: every failure
 * becomes a warning. Each Issue mutation is followed by its own `issue_link` commit.
 * @param {{githubClient: any, context: any, core?: any, policy: import("./work_queue_policy.cjs").PolicyEnv | null | undefined, state?: any, branch?: string}} options
 * @returns {Promise<{created: number, commented: number, closed: number, warnings: string[]}>}
 */
async function projectIssues({ githubClient, context, core: coreApi = undefined, policy, state = undefined, branch = undefined }) {
  /** @type {string[]} */
  const warnings = [];
  const result = { created: 0, commented: 0, closed: 0, warnings };
  /** @param {string} text */
  const warn = text => {
    result.warnings.push(text);
    coreApi?.warning(text);
  };
  if (!policy?.issues) return result;
  const config = policy.issues;
  try {
    const repo = { owner: context.repo.owner, repo: context.repo.repo };
    const repository = `${repo.owner}/${repo.repo}`;
    const queue = { githubClient, context, role: "projector", core: coreApi, ...(branch === undefined ? {} : { branch }) };
    const current = state ?? (await loadQueue({ ...queue, policy })).state;

    /** @param {any} operation @param {(state: any) => boolean} absent */
    const link = (operation, absent) => publishRequest({ ...queue, kind: "issue_link", parameters: { operations: [operation] }, operationsFor: latest => (absent(latest) ? [operation] : []) });

    /** Comment once per marker (checked on the most recent comments); returns the comment and the Issue. @param {number} number @param {string} marker @param {string} body */
    const commentOnce = async (number, marker, body) => {
      const issue = await getIssue(githubClient, repo, number);
      const existing = (await recentComments(githubClient, repo, number, issue.comments)).find(comment => comment.body.includes(marker));
      if (existing) return { comment: existing, issue };
      const comment = await addComment(githubClient, repo, number, body);
      result.commented++;
      return { comment, issue };
    };

    let labelled = /** @type {any[]} */ [];
    try {
      labelled = await listLabelledOpenIssues(githubClient, repo, config.label);
    } catch (error) {
      warn(`Work queue Issue listing failed: ${message(error)}`);
    }
    /** @type {Map<string, any>} */
    const marked = new Map();
    for (const issue of labelled) {
      const workId = markedWorkId(issue.body);
      if (workId && !marked.has(workId)) marked.set(workId, issue);
    }
    /** @type {string | undefined} */
    let repoId;

    const pools = new Set(Object.keys(policy.pools));
    const works = [...current.works.values()].filter(work => pools.has(work.pool)).sort(fifoCompare);
    /** @type {Array<{work: any, run: () => Promise<void>}>} */
    const tasks = [];
    for (const work of works) {
      if (!work.backing_issue) {
        if (work.state !== "available" && work.state !== "claimed") continue;
        tasks.push({
          work,
          run: async () => {
            repoId ??= await repositoryId(githubClient, repo);
            let issue = marked.get(work.work_id);
            if (!issue) {
              issue = await createIssue(githubClient, repo, { title: issueTitle(work, config), body: issueBody(work, repository), label: config.label });
              result.created++;
            }
            const resource = issueResource(issue, repository, repoId);
            await link({ kind: "IssueLink", work_id: work.work_id, resource }, latest => !latest.works.get(work.work_id)?.backing_issue);
          },
        });
        continue;
      }
      const number = Number(work.backing_issue.number);
      if (work.state === "claimed") {
        const claim = current.claims.get(work.claim_id);
        const dispatch = claim && current.dispatches.get(claim.dispatch_id);
        if (claim?.state !== "open" || claim.issue_comment !== undefined || dispatch?.state !== "bound") continue;
        tasks.push({
          work,
          run: async () => {
            const { comment } = await commentOnce(number, commentMarker(work.work_id, `claim-${claim.claim_id}`), claimComment(work, claim, dispatch, repository));
            await link({ kind: "IssueComment", work_id: work.work_id, comment_id: String(comment.id), claim_id: claim.claim_id }, latest => latest.claims.get(claim.claim_id)?.issue_comment === undefined);
          },
        });
      } else if ((work.state === "completed" || work.state === "cancelled") && work.issue_comment === undefined) {
        tasks.push({
          work,
          run: async () => {
            const { comment, issue } = await commentOnce(number, commentMarker(work.work_id, work.state), terminalComment(work));
            if (issue.state === "open") {
              await closeIssue(githubClient, repo, number);
              result.closed++;
            }
            await link({ kind: "IssueComment", work_id: work.work_id, comment_id: String(comment.id) }, latest => latest.works.get(work.work_id)?.issue_comment === undefined);
          },
        });
      }
    }
    for (const task of tasks.slice(0, MAX_PROJECTED_WORK)) {
      try {
        await task.run();
      } catch (error) {
        warn(`Work queue Issue projection failed for Work ${task.work.work_id}: ${message(error)}`);
      }
    }

    // Orphan sweep: labelled open Issues whose marked Work is absent from the ledger.
    for (const [workId, issue] of marked) {
      if (current.works.has(workId)) continue;
      try {
        await commentOnce(issue.number, commentMarker(workId, "orphaned"), orphanComment(workId));
        await closeIssue(githubClient, repo, issue.number);
        result.closed++;
      } catch (error) {
        warn(`Work queue orphaned Issue #${issue.number} could not be closed: ${message(error)}`);
      }
    }
  } catch (error) {
    warn(`Work queue Issue projection failed: ${message(error)}`);
  }
  return result;
}

/** Step entry point: project with the compiled policy env. */
async function main(options = {}) {
  const coreApi = options.core || global.core;
  let policy;
  try {
    policy = readWorkQueuePolicyEnv();
  } catch (error) {
    coreApi?.warning(`Work queue Issue projection skipped: ${message(error)}`);
    return { created: 0, commented: 0, closed: 0, warnings: [message(error)] };
  }
  return projectIssues({ githubClient: options.githubClient || global.github, context: options.context || global.context, core: coreApi, policy });
}

module.exports = { MAX_PROJECTED_WORK, main, projectIssues };
