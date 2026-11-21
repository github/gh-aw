// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { withRetry, RATE_LIMIT_RETRY_CONFIG } = require("./error_recovery.cjs");
const { DEFAULT_MAX_UPDATES_PER_RUN } = require("./expired_entity_cleanup_helpers.cjs");
const { renderSizedReport } = require("./report_size.cjs");

const STEERING_ISSUE_TITLE_PREFIX = "[WIP] ";
const STEERING_ISSUE_MAX_AGE_MS = 24 * 60 * 60 * 1000;
const CLEANUP_COMMENT_MARKER = "<!-- gh-aw:stale-steering-issue-cleanup -->";
/** @param {string} reason */
function renderCleanupComment(reason) {
  return renderSizedReport({
    type: "report",
    title: "Stale steering issue closed",
    children: [
      { type: "paragraph", children: [{ type: "text", value: `This steering issue was left open after its linked workflow run ${reason}. Closing it during scheduled agentic maintenance.`, agentTainted: false }] },
      { type: "xml_marker", name: "gh-aw", value: "stale-steering-issue-cleanup", compact: true, agentTainted: false },
    ],
  }).body;
}

const CLEANUP_COMMENT = renderCleanupComment("completed successfully");
// Failed runs normally retitle the steering issue into the failure report; a [WIP] issue left
// behind by a completed unsuccessful run is an orphan with no remaining owner.
const UNSUCCESSFUL_RUN_CLEANUP_COMMENT = renderCleanupComment("completed without success");
// Other conclusions (timed_out, action_required, neutral, skipped, stale, ...) may still need follow-up.
const ORPHANING_RUN_CONCLUSIONS = new Set(["failure", "cancelled"]);

const OPEN_ISSUES_QUERY = `
  query($owner: String!, $repo: String!, $cursor: String) {
    repository(owner: $owner, name: $repo) {
      issues(first: 100, after: $cursor, states: [OPEN], orderBy: { field: CREATED_AT, direction: ASC }) {
        nodes {
          number
          title
          createdAt
        }
        pageInfo {
          hasNextPage
          endCursor
        }
      }
    }
  }
`;

function getWorkflowRunId(issueBody, owner, repo) {
  const match = issueBody.match(/\[the workflow run\]\(([^)]+)\)/);
  if (!match) {
    return null;
  }

  try {
    const runURL = new URL(match[1]);
    const serverURL = new URL(context.serverUrl);
    const pathSegments = runURL.pathname.split("/").filter(Boolean).map(decodeURIComponent);
    const [runOwner, runRepo, actionsPath, runsPath, runID, ...extraSegments] = pathSegments;
    if (runURL.origin !== serverURL.origin || runOwner !== owner || runRepo !== repo || actionsPath !== "actions" || runsPath !== "runs" || extraSegments.length > 0 || !/^\d+$/.test(runID || "")) {
      return null;
    }
    const numericRunID = Number(runID);
    return Number.isSafeInteger(numericRunID) && numericRunID > 0 ? numericRunID : null;
  } catch {
    return null;
  }
}

async function getStaleSteeringIssues(owner, repo, cutoff) {
  const candidates = [];
  let cursor = null;
  let reachedCutoff = false;

  do {
    const result = await github.graphql(OPEN_ISSUES_QUERY, { owner, repo, cursor });
    const connection = result?.repository?.issues;
    if (!connection) {
      throw new Error("GraphQL query returned no repository issues connection");
    }

    for (const issue of connection.nodes || []) {
      const createdAt = Date.parse(issue.createdAt);
      if (!Number.isFinite(createdAt)) {
        continue;
      }
      if (createdAt > cutoff) {
        reachedCutoff = true;
        break;
      }
      if (issue.title?.startsWith(STEERING_ISSUE_TITLE_PREFIX) && createdAt <= cutoff) {
        candidates.push(issue);
      }
    }

    const pageInfo = connection.pageInfo || {};
    if (reachedCutoff || !pageInfo.hasNextPage) {
      break;
    }
    if (!pageInfo.endCursor || pageInfo.endCursor === cursor) {
      throw new Error("GraphQL issue pagination did not return a new cursor");
    }
    cursor = pageInfo.endCursor;
  } while (true);

  return candidates;
}

async function main() {
  const { owner, repo } = context.repo;
  const cutoff = Date.now() - STEERING_ISSUE_MAX_AGE_MS;
  const candidates = await getStaleSteeringIssues(owner, repo, cutoff);
  const failures = [];
  let closedCount = 0;

  for (const [index, candidate] of candidates.entries()) {
    if (closedCount >= DEFAULT_MAX_UPDATES_PER_RUN) {
      core.info(`Reached the limit of ${DEFAULT_MAX_UPDATES_PER_RUN} closed stale steering issues for this run; ${candidates.length - index} remaining candidate(s) will be checked in the next scheduled run`);
      break;
    }
    try {
      const { data: issue } = await github.rest.issues.get({ owner, repo, issue_number: candidate.number });
      const runID = getWorkflowRunId(issue.body || "", owner, repo);
      if (!runID) {
        continue;
      }

      let run;
      try {
        ({ data: run } = await github.rest.actions.getWorkflowRun({ owner, repo, run_id: runID }));
      } catch (error) {
        const status = error && typeof error === "object" ? (error.response?.status ?? error.status) : undefined;
        if (status === 404) {
          core.info(`Skipping stale steering issue #${candidate.number}: workflow run ${runID} no longer exists`);
          continue;
        }
        throw error;
      }
      if (run.status !== "completed") {
        continue;
      }
      const succeeded = run.conclusion === "success";
      if (!succeeded && !ORPHANING_RUN_CONCLUSIONS.has(run.conclusion)) {
        core.info(`Skipping stale steering issue #${candidate.number}: workflow run ${runID} concluded with ${run.conclusion || "no conclusion"}`);
        continue;
      }

      // handle_agent_failure may have retitled this issue into a failure report since it was listed.
      const { data: current } = await github.rest.issues.get({ owner, repo, issue_number: candidate.number });
      if (current.state !== "open" || !current.title?.startsWith(STEERING_ISSUE_TITLE_PREFIX)) {
        core.info(`Skipping stale steering issue #${candidate.number}: no longer an open steering issue`);
        continue;
      }

      try {
        const comments = await github.paginate(github.rest.issues.listComments, {
          owner,
          repo,
          issue_number: candidate.number,
          per_page: 100,
        });
        if (!comments.some(comment => comment.body?.includes(CLEANUP_COMMENT_MARKER))) {
          await withRetry(
            () =>
              github.rest.issues.createComment({
                owner,
                repo,
                issue_number: candidate.number,
                body: succeeded ? CLEANUP_COMMENT : UNSUCCESSFUL_RUN_CLEANUP_COMMENT,
              }),
            RATE_LIMIT_RETRY_CONFIG,
            `comment on stale steering issue #${candidate.number}`
          );
        }
      } catch (error) {
        const message = `Failed to comment on stale steering issue #${candidate.number}: ${getErrorMessage(error)}`;
        core.warning(message);
        failures.push(message);
      }
      await withRetry(
        () =>
          github.rest.issues.update({
            owner,
            repo,
            issue_number: candidate.number,
            state: "closed",
            state_reason: succeeded ? "completed" : "not_planned",
          }),
        RATE_LIMIT_RETRY_CONFIG,
        `close stale steering issue #${candidate.number}`
      );
      closedCount++;
      core.info(`Closed stale steering issue #${candidate.number} for ${succeeded ? "successful" : run.conclusion} workflow run ${runID}`);
    } catch (error) {
      const message = `Failed to clean up stale steering issue #${candidate.number}: ${getErrorMessage(error)}`;
      core.warning(message);
      failures.push(message);
    }
  }
  if (failures.length > 0) {
    core.setFailed(`Stale steering issue cleanup encountered ${failures.length} error(s):\n${failures.join("\n")}`);
  }
}

module.exports = { main, getWorkflowRunId, getStaleSteeringIssues, CLEANUP_COMMENT, UNSUCCESSFUL_RUN_CLEANUP_COMMENT, CLEANUP_COMMENT_MARKER, STEERING_ISSUE_MAX_AGE_MS };
