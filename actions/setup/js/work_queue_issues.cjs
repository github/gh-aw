// @ts-check
"use strict";

const { loadQueue, publishRequest } = require("./work_queue_binding.cjs");
const { readWorkQueuePolicyEnv } = require("./work_queue_policy.cjs");
const { fifoCompare } = require("./work_queue_scheduler.cjs");
const { addComment, closeIssue, createIssue, getIssue, issueResource, listLabelledOpenIssues, recentComments, repositoryId, writerId } = require("./work_queue_issue_api.cjs");
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

    // Markers are editable text anyone can copy: only Issues and comments authored by the
    // token's own principal are reused. It is resolved once, and only for a marked candidate;
    // if it cannot be verified nothing is reused (a lost write may repeat, never be suppressed).
    /** @type {Promise<string | null> | undefined} */
    let self;
    /** @param {unknown} author */
    const byProjector = async author => {
      self ??= writerId(githubClient).catch(error => {
        warn(`Work queue projector identity is unverified; marked Issues and comments are not reused: ${message(error)}`);
        return null;
      });
      const id = await self;
      return id !== null && author != null && String(author) === id;
    };

    /** @type {string | undefined} */
    let repoId;
    const localRepoId = async () => (repoId ??= await repositoryId(githubClient, repo));

    /**
     * Comment once per marker (checked on the projector's most recent comments) on the exact
     * Issue `resourceId`; returns the comment and the Issue.
     * @param {{number: number, resourceId: string}} target @param {string} marker @param {string} body
     */
    const commentOnce = async ({ number, resourceId }, marker, body) => {
      const issue = await getIssue(githubClient, repo, number);
      if (String(issue.id) !== resourceId) throw new Error(`Issue #${number} is not Issue ${resourceId}`);
      for (const comment of await recentComments(githubClient, repo, number, issue.comments)) {
        if (comment.body.includes(marker) && (await byProjector(comment.author))) return { comment, issue };
      }
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
    // Lost-link recovery and the orphan sweep consider only Issues the projector created.
    /** @type {Map<string, any>} */
    const marked = new Map();
    for (const issue of labelled) {
      const workId = markedWorkId(issue.body);
      if (workId && !marked.has(workId) && (await byProjector(issue.user?.id))) marked.set(workId, issue);
    }

    /** @param {any} work @returns {(() => Promise<void>) | undefined} */
    const projection = work => {
      if (!work.backing_issue) {
        if (work.state !== "available" && work.state !== "claimed") return undefined;
        return async () => {
          const id = await localRepoId();
          let issue = marked.get(work.work_id);
          if (!issue) {
            issue = await createIssue(githubClient, repo, { title: issueTitle(work, config), body: issueBody(work, repository), label: config.label });
            result.created++;
          }
          const resource = issueResource(issue, repository, id);
          await link({ kind: "IssueLink", work_id: work.work_id, resource }, latest => !latest.works.get(work.work_id)?.backing_issue);
        };
      }
      const target = { number: Number(work.backing_issue.number), resourceId: work.backing_issue.resource_id };
      if (work.state === "claimed") {
        const claim = current.claims.get(work.claim_id);
        const dispatch = claim && current.dispatches.get(claim.dispatch_id);
        if (claim?.state !== "open" || claim.issue_comment !== undefined || dispatch?.state !== "bound") return undefined;
        return async () => {
          const { comment } = await commentOnce(target, commentMarker(work.work_id, `claim-${claim.claim_id}`), claimComment(work, claim, dispatch, repository));
          await link({ kind: "IssueComment", work_id: work.work_id, comment_id: String(comment.id), claim_id: claim.claim_id }, latest => latest.claims.get(claim.claim_id)?.issue_comment === undefined);
        };
      }
      if ((work.state === "completed" || work.state === "cancelled") && work.issue_comment === undefined) {
        return async () => {
          const { comment, issue } = await commentOnce(target, commentMarker(work.work_id, work.state), terminalComment(work));
          if (issue.state === "open") {
            await closeIssue(githubClient, repo, target.number);
            result.closed++;
          }
          await link({ kind: "IssueComment", work_id: work.work_id, comment_id: String(comment.id) }, latest => latest.works.get(work.work_id)?.issue_comment === undefined);
        };
      }
      return undefined;
    };

    const pools = new Set(Object.keys(policy.pools));
    const works = [...current.works.values()].filter(work => pools.has(work.pool)).sort(fifoCompare);
    /** @type {Array<{work: any, run: () => Promise<void>}>} */
    const tasks = [];
    for (const work of works) {
      const run = projection(work);
      if (!run) continue;
      // The REST calls address this repository by Issue number, so a backing Issue of another
      // repository is skipped (without spending the per-run budget), never resolved locally.
      if (work.backing_issue && work.backing_issue.repository_id !== (await localRepoId())) {
        coreApi?.info(`Work ${work.work_id}: backing Issue ${work.backing_issue.repository}#${work.backing_issue.number} is in another repository and is not projected`);
        continue;
      }
      tasks.push({ work, run });
    }
    for (const task of tasks.slice(0, MAX_PROJECTED_WORK)) {
      try {
        await task.run();
      } catch (error) {
        warn(`Work queue Issue projection failed for Work ${task.work.work_id}: ${message(error)}`);
      }
    }

    // Orphan sweep: the projector's labelled open Issues whose marked Work is absent from the ledger.
    for (const [workId, issue] of marked) {
      if (current.works.has(workId)) continue;
      try {
        await commentOnce({ number: issue.number, resourceId: String(issue.id) }, commentMarker(workId, "orphaned"), orphanComment(workId));
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
