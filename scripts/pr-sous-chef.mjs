import { execFile as execFileCallback } from "node:child_process";
import { createHash } from "node:crypto";
import { appendFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { ensureSecureDirectory, writeFileSecure } from "../.github/scripts/secure_write.cjs";
import "../actions/setup/js/shim.cjs";
import { withRetry, isTransientError } from "../actions/setup/js/error_recovery.cjs";
import { renderSizedReport } from "../actions/setup/js/report_size.cjs";
import { parseReportMarkdown } from "../actions/setup/js/report_markdown.cjs";
import { normalizeReportURL } from "../actions/setup/js/report_ast.cjs";
import { validateValueAgainstSchema } from "../actions/setup/js/schema-validation.cjs";

const execFile = promisify(execFileCallback);

const marker = "<!-- gh-aw-pr-sous-chef-nudge -->";
const statePattern = /Sous-chef state: ([a-f0-9]{64})/;
const activeStatuses = new Set(["queued", "in_progress", "pending", "requested", "waiting"]);
const agentPattern = /copilot|coding.agent|claude|codex|gemini|opencode|reviewer|(^|[/ -])agent([/ -]|$)|^Q$/i;
const automationPattern = /copilot|github-actions|gh-aw-bot|\[bot\]$|^app\//i;
const dependencyPattern = /^(?:app\/)?(?:dependabot|renovate)(?:\[bot\])?$/i;
const failureStates = new Set(["FAILURE", "ERROR", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE"]);
const idleMs = 10 * 60 * 1000;
const staleMs = 14 * 24 * 60 * 60 * 1000;
const approvalWorkflows = new Set([".github/workflows/cjs.yml", ".github/workflows/cgo.yml", ".github/workflows/CWI.yml"]);
const commentDataCache = new WeakMap();
const unreadableReportErrors = new Set([
  "Report template nesting exceeds the maximum depth",
  "Report summary nesting exceeds the maximum depth",
  "Report inline nesting exceeds the maximum depth",
  "Report details nesting exceeds the maximum depth",
  "Report text contains excessively nested encodings",
  "Publication text contains excessively nested encodings",
]);

export const sousChefDataSchema = {
  type: "object",
  properties: {
    workflow: { type: "string", enum: ["pr-sous-chef"] },
    schema_version: { type: "string", enum: ["1"] },
    kind: { type: "string", enum: ["requested", "running", "ci-follow-up"] },
    head: { type: "string", minLength: 1 },
    work: { type: "array", items: { type: "string", pattern: "^[a-f0-9]{64}$" } },
    state: { type: "string", pattern: "^[a-f0-9]{64}$" },
    completed_task: { type: "integer", minimum: 0 },
  },
  required: ["workflow", "schema_version", "kind", "head", "work", "state", "completed_task"],
  additionalProperties: false,
};

export function readSousChefData(body) {
  if (!body?.includes('"pr-sous-chef"')) return null;
  const matches = [];
  function visit(nodes) {
    for (const node of nodes) {
      if (node.type === "details") visit(node.children);
      else if (node.type === "structured_data" && !validateValueAgainstSchema(node.data, sousChefDataSchema)) {
        const data = node.data;
        if (
          /^\S+$/.test(data.head) &&
          /^[a-f0-9]{64}$/.test(data.state) &&
          data.work.every(token => /^[a-f0-9]{64}$/.test(token)) &&
          Number.isSafeInteger(data.completed_task) &&
          (data.kind === "ci-follow-up" ? data.completed_task > 0 : data.completed_task === 0)
        )
          matches.push(data);
      }
    }
  }
  let nodes;
  try {
    nodes = parseReportMarkdown(body, { agentTainted: true });
  } catch (error) {
    if (!(error instanceof TypeError) || !unreadableReportErrors.has(error.message)) throw error;
    core.warning(`Ignoring unreadable PR Sous Chef comment data: ${error.message}`);
    return null;
  }
  visit(nodes);
  return matches.length === 1 ? matches[0] : null;
}

function commentData(comment) {
  if (!trustedSousChefAuthor(comment)) return null;
  const body = comment.body ?? "";
  const cached = commentDataCache.get(comment);
  if (cached?.body === body) return cached.data;
  const data = readSousChefData(body);
  commentDataCache.set(comment, { body, data });
  return data;
}

function recordedState(comment) {
  const data = commentData(comment);
  return data?.kind === "running" ? data.state : statePattern.exec(comment.body ?? "")?.[1];
}

function isAssignment(comment) {
  if (!isSousChefComment(comment)) return false;
  const data = commentData(comment);
  return data ? ["requested", "running"].includes(data.kind) : /^Sous-chef (?:requested-)?state: [a-f0-9]{64}$/m.test(comment.body ?? "");
}

function epoch(value) {
  if (!value) return 0;
  const result = Date.parse(value);
  if (!Number.isFinite(result)) throw new Error(`Invalid activity timestamp: ${value}`);
  return result;
}

function login(comment) {
  return comment.author?.login ?? comment.user?.login ?? "";
}

function normalizedLogin(comment) {
  return login(comment)
    .replace(/^app\//i, "")
    .replace(/\[bot\]$/i, "")
    .toLowerCase();
}

function authorReply(comment, pr) {
  const author = normalizedLogin(comment);
  return author && (author === normalizedLogin(pr) || ["copilot", "github-copilot", "copilot-swe-agent"].includes(author));
}

function commentTime(comment) {
  return Math.max(epoch(comment.updatedAt ?? comment.updated_at), epoch(comment.createdAt ?? comment.created_at));
}

function trusted(comment) {
  return ["gh-aw-bot", "github-actions"].includes(normalizedLogin(comment)) || ["OWNER", "MEMBER", "COLLABORATOR"].includes(comment.authorAssociation ?? comment.author_association);
}

function trustedReviewer(comment) {
  return trusted(comment) || ["copilot", "github-copilot", "copilot-pull-request-reviewer", "github-advanced-security"].includes(normalizedLogin(comment));
}

function trustedSousChefAuthor(comment) {
  return trusted(comment) || ["copilot", "github-copilot", "copilot-swe-agent"].includes(normalizedLogin(comment));
}

function isSousChefComment(comment) {
  const body = comment.body ?? "";
  return (
    trustedSousChefAuthor(comment) &&
    (Boolean(commentData(comment)) ||
      body.includes(marker) ||
      statePattern.test(body) ||
      /^Sous-chef requested-state: [a-f0-9]{64}$/m.test(body) ||
      /^Sous-chef ci-follow-up: [1-9]\d*$/m.test(body) ||
      /Generated by .*PR Sous Chef/.test(body) ||
      body.includes("gh-aw-workflow-call-id: github/gh-aw/pr-sous-chef"))
  );
}

function isNudge(comment) {
  if (!isSousChefComment(comment)) return false;
  const body = comment.body ?? "";
  if (/The code changes were not applied\.|a fallback review issue was created instead due to protected file changes:|Direct push to the original pull request branch was not possible/.test(body)) return false;
  const data = commentData(comment);
  if (data) return data.kind === "running";
  if (/^Sous-chef requested-state: [a-f0-9]{64}$/m.test(body) && !statePattern.test(body)) return false;
  return body.includes("@copilot") || statePattern.test(body);
}

function workTokens(pr) {
  const work = [
    ...pr.unresolved_reviews.map(review => ["review", review.id, review.feedback_hash]),
    ...pr.failed_checks.filter(check => check.conclusion !== "ACTION_REQUIRED").map(check => ["check", check.name, check.conclusion]),
    ...(pr.zero_diff_stalled ? [["zero_diff"]] : []),
  ];
  return [...new Set(work.map(item => createHash("sha256").update(JSON.stringify(item)).digest("hex")))].sort();
}

export function activeAgent(pr, runs) {
  return (
    runs.some(
      run =>
        activeStatuses.has(run.status) &&
        ((run.path === ".github/workflows/pr-finisher.lock.yml" && run.display_title === `PR Finisher #${pr.number}`) ||
          ((agentPattern.test(run.name ?? "") || (run.path ?? "").startsWith("dynamic/")) && (run.pull_requests?.some(item => item.number === pr.number) || run.head_branch === pr.headRefName)))
    ) || (pr.statusCheckRollup ?? []).some(check => check.status !== "COMPLETED" && agentPattern.test(check.name ?? ""))
  );
}

function metadataSkip(pr, now) {
  if (dependencyPattern.test(pr.author?.login ?? "")) return "dependency_bot";
  if (pr.isDraft || (pr.state && pr.state !== "OPEN")) return "closed_or_draft";
  if (pr.labels?.some(label => label.name === "broccoli")) return "opted_out";
  const updated = epoch(pr.updatedAt);
  if (now - updated > staleMs) return "stale";
  if (now - updated < idleMs) return "not_idle";
  return null;
}

function checkSkip(pr, now) {
  if (activeAgent(pr, [])) return "agent_active";
  const checks = pr.statusCheckRollup ?? [];
  if (
    checks.some(check => {
      if (check.__typename === "StatusContext") return check.state === "PENDING";
      if (!["QUEUED", "IN_PROGRESS", "REQUESTED", "PENDING"].includes(check.status)) return false;
      const started = epoch(check.startedAt || check.createdAt);
      return !started || now - started < 60 * 60 * 1000;
    })
  ) {
    return "checks_pending";
  }
  return null;
}

export function approvableRuns(pr, runs) {
  const candidates = runs
    .filter(
      run =>
        Number.isSafeInteger(run.id) &&
        run.id > 0 &&
        Number.isSafeInteger(run.workflow_id) &&
        run.workflow_id > 0 &&
        run.event === "pull_request" &&
        (["waiting", "action_required"].includes(run.status) || run.conclusion === "action_required") &&
        run.head_branch === pr.headRefName &&
        run.head_sha === pr.headRefOid &&
        approvalWorkflows.has((run.path ?? "").split("@")[0]) &&
        run.pull_requests?.length > 0 &&
        run.pull_requests.every(item => item.number === pr.number)
    )
    .map(run => [run.id, { run_id: run.id, workflow_id: run.workflow_id, path: run.path }]);
  return [...new Map(candidates).values()].sort((a, b) => a.run_id - b.run_id);
}

export function classify(pr, detail, comments, runs, now = Date.now()) {
  const skip = metadataSkip(pr, now) || checkSkip(pr, now);
  if (skip) return { skip_reason: skip };
  if (activeAgent(pr, runs)) return { skip_reason: "agent_active" };
  const checks = pr.statusCheckRollup ?? [];
  if (detail.reviewThreads.pageInfo.hasNextPage || detail.reviews.pageInfo.hasPreviousPage) {
    return { skip_reason: "incomplete_reviews" };
  }
  const unresolved = detail.reviewThreads.nodes.filter(thread => !thread.isResolved);
  if (unresolved.some(thread => thread.comments.pageInfo.hasNextPage)) {
    return { skip_reason: "incomplete_reviews" };
  }
  const feedback = unresolved
    .filter(thread => trustedReviewer(thread.comments.nodes[0] ?? {}))
    .flatMap(thread => {
      const replyAt = Math.max(0, ...thread.comments.nodes.filter(comment => authorReply(comment, pr) && comment.body?.trim()).map(commentTime));
      const reviewerComments = thread.comments.nodes.filter(comment => comment.body?.trim() && trustedReviewer(comment) && !authorReply(comment, pr) && commentTime(comment) > replyAt);
      if (!reviewerComments.length) return [];
      const original = reviewerComments[0];
      const last = [...reviewerComments].sort((a, b) => commentTime(b) - commentTime(a))[0];
      return {
        id: thread.id,
        reviewer: login(original),
        url: original.url,
        path: thread.path,
        line: thread.line ?? thread.originalLine,
        comments: reviewerComments.map(comment => ({ body: comment.body, url: comment.url })),
        feedback_at: last.updatedAt ?? last.createdAt,
        feedback_hash: createHash("sha256")
          .update(JSON.stringify(reviewerComments.map(comment => [login(comment), comment.body])))
          .digest("hex"),
      };
    });
  const humanComments = comments.filter(comment => trusted(comment) && !automationPattern.test(login(comment)) && !isSousChefComment(comment));
  const activity = Math.max(
    epoch(pr.createdAt),
    epoch(pr.headCommittedAt),
    ...feedback.map(item => epoch(item.feedback_at)),
    ...humanComments.map(commentTime),
    ...detail.reviews.nodes.filter(review => trustedReviewer(review)).map(review => epoch(review.submittedAt))
  );
  if (now - activity > staleMs) return { skip_reason: "stale" };
  const lastActivity = Math.max(activity, epoch(pr.updatedAt), ...comments.map(commentTime), ...detail.reviewThreads.nodes.flatMap(thread => thread.comments.nodes.map(commentTime)));
  if (now - lastActivity < idleMs) return { skip_reason: "not_idle" };
  const nudges = comments.filter(isNudge).sort((a, b) => epoch(b.created_at) - epoch(a.created_at));
  const latest = nudges[0];
  const lastComment = [...comments].sort((a, b) => epoch(b.createdAt ?? b.created_at) - epoch(a.createdAt ?? a.created_at) || (b.id ?? 0) - (a.id ?? 0))[0];
  const failedChecks = checks
    .filter(check => failureStates.has(check.conclusion ?? check.state) || check.status === "WAITING")
    .map(check => ({
      name: check.name ?? check.context,
      conclusion: check.status === "WAITING" ? "ACTION_REQUIRED" : (check.conclusion ?? check.state),
      url: check.detailsUrl ?? check.targetUrl,
    }))
    .sort((a, b) => a.name.localeCompare(b.name) || a.conclusion.localeCompare(b.conclusion));
  const zeroDiff = pr.changedFiles === 0 && now - epoch(pr.createdAt) >= 24 * 60 * 60 * 1000;
  const branchAction = ["CONFLICTING", "BEHIND"].includes(pr.mergeStateStatus) ? pr.mergeStateStatus : null;
  const dismissReviews = unresolved.length === 0 ? detail.reviews.nodes.filter(review => normalizedLogin(review) === "github-actions" && review.state === "CHANGES_REQUESTED").map(review => review.databaseId) : [];
  const approveRuns = approvableRuns(pr, runs);
  const state = createHash("sha256")
    .update(
      JSON.stringify({
        head: pr.headRefOid,
        branch: branchAction,
        checks: [...new Set(failedChecks.map(check => `${check.name}:${check.conclusion}`))],
        feedback: feedback.map(({ id, feedback_hash }) => ({ id, feedback_hash })).sort((a, b) => a.id.localeCompare(b.id)),
        zero_diff: zeroDiff,
      })
    )
    .digest("hex");
  // Recognize fingerprints emitted before replied-feedback filtering was introduced.
  const alreadyNudged = nudges.some(comment => {
    const previous = recordedState(comment);
    if (previous === state) return true;
    const previousState = createHash("sha256")
      .update(
        JSON.stringify({
          head: pr.headRefOid,
          branch: branchAction,
          checks: [...new Set(failedChecks.map(check => `${check.name}:${check.conclusion}`))],
          feedback: feedback.map(({ id, reviewer, url, feedback_at, feedback_hash }) => ({ id, reviewer, url, feedback_at, feedback_hash })).sort((a, b) => a.id.localeCompare(b.id)),
          human_activity: humanComments.filter(item => epoch(item.created_at) <= epoch(comment.created_at)).map(item => [item.id, item.updated_at ?? item.created_at]),
          zero_diff: zeroDiff,
        })
      )
      .digest("hex");
    return previous === previousState;
  });
  const work = workTokens({ unresolved_reviews: feedback, failed_checks: failedChecks, zero_diff_stalled: zeroDiff });
  const assignments = [...comments].filter(isAssignment).sort((a, b) => epoch(b.createdAt ?? b.created_at) - epoch(a.createdAt ?? a.created_at));
  const assignment = assignments[0];
  const assignmentState = comment => commentData(comment)?.state ?? /^Sous-chef (?:requested-)?state: ([a-f0-9]{64})$/m.exec(comment.body ?? "")?.[1];
  const request = assignment && assignments.find(comment => assignmentState(comment) === assignmentState(assignment) && (commentData(comment)?.kind === "requested" || /^Sous-chef requested-state:/m.test(comment.body ?? "")));
  const assignedAt = assignment ? epoch((request ?? assignment).createdAt ?? (request ?? assignment).created_at) : 0;
  const completedTask =
    assignment && failedChecks.some(check => check.conclusion !== "ACTION_REQUIRED")
      ? runs
          .filter(
            run =>
              run.status === "completed" &&
              run.conclusion === "success" &&
              Number.isSafeInteger(run.id) &&
              run.id > 0 &&
              run.head_branch === pr.headRefName &&
              (run.pull_requests?.length ? run.pull_requests.every(item => item.number === pr.number) : true) &&
              run.event === "dynamic" &&
              /^dynamic\/copilot-swe-agent(?:\/|$)/.test(run.path ?? "") &&
              epoch(run.created_at ?? run.run_started_at) >= assignedAt &&
              epoch(run.updated_at) > epoch(assignment.createdAt ?? assignment.created_at) &&
              now - epoch(run.updated_at) >= idleMs
          )
          .sort((a, b) => epoch(b.updated_at) - epoch(a.updated_at))[0]
      : null;
  const coveredWork = new Set(
    nudges
      .filter(comment => (commentData(comment)?.head ?? /^Sous-chef head: (\S+)$/m.exec(comment.body)?.[1]) === pr.headRefOid)
      .flatMap(comment => {
        const data = commentData(comment);
        if (data?.kind === "running") return data.work;
        const recorded = /^Sous-chef work: ([a-f0-9 ]+)$/m.exec(comment.body)?.[1];
        return recorded?.split(" ") ?? [];
      })
  );
  // Only the successfully completed assignment can cover work across its head update.
  if (completedTask && epoch(pr.headCommittedAt) <= epoch(completedTask.updated_at)) {
    const data = commentData(assignment);
    const recorded = /^Sous-chef (?:requested-)?work: ([a-f0-9 ]+)$/m.exec(assignment.body ?? "")?.[1];
    for (const token of data?.work ?? recorded?.split(" ") ?? []) coveredWork.add(token);
  }
  const unchangedWork = alreadyNudged || (work.length > 0 && work.every(token => coveredWork.has(token)));
  let nudgeSkip;
  if (!branchAction && unchangedWork) nudgeSkip = "unchanged";
  else if (!branchAction && latest && !recordedState(latest) && activity <= epoch(latest.created_at)) nudgeSkip = "unchanged_legacy";
  else if (!branchAction && !work.length) nudgeSkip = failedChecks.length ? "approval_required" : "nothing_actionable";
  if (!nudgeSkip && lastComment && isSousChefComment(lastComment)) nudgeSkip = "last_comment_sous_chef";
  const ciFollowUp =
    nudgeSkip &&
    !branchAction &&
    unchangedWork &&
    completedTask &&
    !comments.some(
      comment =>
        isSousChefComment(comment) && (commentData(comment)?.kind === "ci-follow-up" ? commentData(comment).completed_task === completedTask.id : (comment.body ?? "").split("\n").includes(`Sous-chef ci-follow-up: ${completedTask.id}`))
    )
      ? { run_id: completedTask.id, url: completedTask.html_url }
      : null;
  if (nudgeSkip && !dismissReviews.length && !approveRuns.length && !ciFollowUp) {
    return { skip_reason: nudgeSkip };
  }
  return {
    number: pr.number,
    title: pr.title,
    url: pr.url,
    headRefOid: pr.headRefOid,
    headRefName: pr.headRefName,
    baseRefName: pr.baseRefName,
    createdAt: pr.createdAt,
    updatedAt: pr.updatedAt,
    changedFiles: pr.changedFiles,
    mergeStateStatus: pr.mergeStateStatus,
    author: pr.author?.login ?? "unknown",
    state_fingerprint: state,
    last_activity_at: new Date(lastActivity).toISOString(),
    failed_checks: failedChecks,
    unresolved_reviews: feedback,
    dismiss_reviews: dismissReviews,
    approve_runs: approveRuns,
    ci_follow_up: ciFollowUp,
    nudge_needed: !nudgeSkip,
    zero_diff_stalled: zeroDiff,
    priority: branchAction === "CONFLICTING" ? 0 : ciFollowUp || (!nudgeSkip && failedChecks.some(check => check.conclusion !== "ACTION_REQUIRED")) ? 1 : !nudgeSkip && feedback.length ? 2 : !nudgeSkip && zeroDiff ? 3 : 4,
  };
}

export async function gh(args, run = execFile, retryConfig = {}) {
  const stdout = await withRetry(
    async () => {
      try {
        const result = await run("gh", args, {
          encoding: "utf8",
          timeout: 60_000,
          maxBuffer: 32 * 1024 * 1024,
        });
        return result.stdout;
      } catch (error) {
        const message = error.killed && error.signal === "SIGTERM" && error.code !== "ERR_CHILD_PROCESS_STDIO_MAXBUFFER" ? "GitHub CLI request timeout after 60000ms" : error.stderr?.trim() || error.message;
        const failure = new Error(message);
        // A child-process exit code is not an HTTP status.
        failure.status = Number(message.match(/\bHTTP (\d{3})\b/)?.[1]) || undefined;
        throw failure;
      }
    },
    {
      maxRetries: 2,
      ...retryConfig,
      shouldRetry: error => error.status !== 401 && error.status !== 403 && isTransientError(error),
    },
    `GitHub CLI ${args[0]} ${args[1]}`
  );
  return JSON.parse(stdout);
}

export async function fetchActiveRuns(repo, api = gh, branch) {
  const branchFilter = branch ? `&branch=${encodeURIComponent(branch)}` : "";
  const runs = [];
  for (const status of activeStatuses) {
    const pages = await api(["api", `repos/${repo}/actions/runs?status=${status}&per_page=100${branchFilter}`, "--paginate", "--slurp"]);
    runs.push(...pages.flatMap(page => page.workflow_runs));
    if (branch) {
      const finishers = await api(["api", `repos/${repo}/actions/workflows/pr-finisher.lock.yml/runs?status=${status}&per_page=100`, "--paginate", "--slurp"]);
      runs.push(...finishers.flatMap(page => page.workflow_runs));
    }
  }
  return runs;
}

async function inspect(repo, pr, runs, api = gh, now = Date.now()) {
  const [owner, name] = repo.split("/");
  // The proxy reconstructs the first connection when reviews are filtered.
  // Keep reviews separate so they cannot replace thread or commit nodes.
  const detail = {};
  for (const [field, selection] of [
    ["reviewThreads", "reviewThreads(first:100){pageInfo{hasNextPage} nodes{id isResolved path line originalLine comments(first:100){pageInfo{hasNextPage} nodes{author{login} authorAssociation body createdAt updatedAt url}}}}"],
    ["reviews", "reviews(last:100){pageInfo{hasPreviousPage} nodes{databaseId author{login} authorAssociation state submittedAt}}"],
  ]) {
    const response = await api([
      "api",
      "graphql",
      "-f",
      `query=query($owner:String!,$name:String!,$number:Int!){
        repository(owner:$owner,name:$name){pullRequest(number:$number){
          number author{login} authorAssociation
          ${selection}
        }}}`,
      "-f",
      `owner=${owner}`,
      "-f",
      `name=${name}`,
      "-F",
      `number=${pr.number}`,
    ]);
    if (response?.errors?.length) throw new Error(`PR #${pr.number}: ${JSON.stringify(response.errors)}`);
    const candidate = response?.data?.repository?.pullRequest;
    const connection = candidate?.[field];
    const pageFlag = field === "reviews" ? "hasPreviousPage" : "hasNextPage";
    if (
      candidate?.number !== pr.number ||
      !Array.isArray(connection?.nodes) ||
      typeof connection.pageInfo?.[pageFlag] !== "boolean" ||
      !connection.nodes.every(node =>
        field === "reviews"
          ? node && typeof node.state === "string" && Number.isSafeInteger(node.databaseId) && node.databaseId > 0
          : typeof node?.id === "string" &&
            typeof node.isResolved === "boolean" &&
            Array.isArray(node.comments?.nodes) &&
            typeof node.comments.pageInfo?.hasNextPage === "boolean" &&
            node.comments.nodes.every(comment => comment && typeof comment.body === "string" && Number.isFinite(Date.parse(comment.createdAt)))
      )
    ) {
      throw new Error(`PR #${pr.number}: could not refresh ${field}; missing or malformed GitHub response (possibly proxy-filtered)`);
    }
    detail[field] = connection;
  }
  // Issue comments do not support sort/direction: paginate, then sort locally.
  const comments = (await api(["api", `repos/${repo}/issues/${pr.number}/comments?per_page=100`, "--paginate", "--slurp"])).flat();
  const result = classify(pr, detail, comments, runs, now);
  if (
    (!result.skip_reason || ["unchanged", "unchanged_legacy", "last_comment_sous_chef"].includes(result.skip_reason)) &&
    !activeAgent(pr, runs) &&
    pr.statusCheckRollup?.some(check => failureStates.has(check.conclusion ?? check.state) && (check.conclusion ?? check.state) !== "ACTION_REQUIRED") &&
    comments.some(isAssignment)
  ) {
    const completed = await api(["api", `repos/${repo}/actions/runs?status=completed&event=dynamic&branch=${encodeURIComponent(pr.headRefName)}&per_page=100`]);
    if (!Array.isArray(completed?.workflow_runs)) throw new Error(`PR #${pr.number}: could not refresh completed coding tasks`);
    return classify(pr, detail, comments, [...runs, ...completed.workflow_runs], now);
  }
  return result;
}

const prFields = "number,updatedAt,author,isDraft,state,labels";

export async function fetchCandidate(repo, number, api = gh, now) {
  // The proxy can mistake REST PR labels for the response collection and return [].
  // Use an explicit repository-scoped query, without gh pr view's union fragments.
  const [owner, name] = repo.split("/");
  const response = await api([
    "api",
    "graphql",
    "-f",
    `query=query($owner:String!,$name:String!,$number:Int!){
      repository(owner:$owner,name:$name){pullRequest(number:$number){
        number title url state isDraft headRefOid headRefName baseRefName
        createdAt updatedAt changedFiles mergeStateStatus author{login} authorAssociation
        labels(first:100){pageInfo{hasNextPage} nodes{name}}
      }}}`,
    "-f",
    `owner=${owner}`,
    "-f",
    `name=${name}`,
    "-F",
    `number=${number}`,
  ]);
  if (response?.errors?.length) throw new Error(`PR #${number}: ${JSON.stringify(response.errors)}`);
  const candidate = response?.data?.repository?.pullRequest;
  if (
    candidate?.number !== Number(number) ||
    !candidate.updatedAt ||
    !candidate.headRefOid ||
    !candidate.headRefName ||
    !candidate.baseRefName ||
    !["OPEN", "CLOSED", "MERGED"].includes(candidate.state) ||
    typeof candidate.isDraft !== "boolean" ||
    !Array.isArray(candidate.labels?.nodes) ||
    !candidate.labels.nodes.every(label => typeof label?.name === "string") ||
    candidate.labels?.pageInfo?.hasNextPage !== false
  )
    throw new Error(`PR #${number}: could not refresh PR metadata`);
  const metadata = {
    ...candidate,
    labels: candidate.labels.nodes,
    mergeStateStatus: candidate.mergeStateStatus === "DIRTY" ? "CONFLICTING" : candidate.mergeStateStatus,
  };
  if (now !== undefined && metadataSkip(metadata, now)) return metadata;
  // Read the exact head SHA without relying on nested GraphQL union fragments
  // that the CLI proxy may omit from an otherwise successful metadata response.
  const head = await api(["api", `repos/${repo}/commits/${candidate.headRefOid}`]);
  if (head?.sha !== candidate.headRefOid || !Number.isFinite(Date.parse(head?.commit?.committer?.date))) {
    throw new Error(`PR #${number}: could not refresh head commit activity`);
  }
  metadata.headCommittedAt = head.commit.committer.date;
  const checkRuns = (await api(["api", `repos/${repo}/commits/${candidate.headRefOid}/check-runs?per_page=100&filter=latest`, "--paginate", "--slurp"])).flatMap(page => page.check_runs);
  const statuses = (await api(["api", `repos/${repo}/commits/${candidate.headRefOid}/status?per_page=100`, "--paginate", "--slurp"])).flatMap(page => page.statuses);
  return {
    ...metadata,
    statusCheckRollup: [
      ...checkRuns.map(check => ({
        name: check.name,
        status: check.status.toUpperCase(),
        conclusion: check.conclusion?.toUpperCase(),
        detailsUrl: check.html_url,
        startedAt: check.started_at,
        createdAt: check.created_at,
      })),
      ...statuses.map(check => ({
        __typename: "StatusContext",
        context: check.context,
        state: check.state.toUpperCase(),
        targetUrl: check.target_url,
      })),
    ],
  };
}

export async function buildQueue(candidates, inspectCandidate, now = Date.now()) {
  const skipped = [];
  const eligible = [];
  for (const pr of candidates) {
    let result;
    const skip = metadataSkip(pr, now);
    if (skip) result = { skip_reason: skip };
    else result = await inspectCandidate(pr);
    if (result.skip_reason) skipped.push({ pr_number: pr.number, ...result });
    else eligible.push(result);
  }
  eligible.sort((a, b) => a.priority - b.priority || epoch(b.updatedAt) - epoch(a.updatedAt) || a.number - b.number);
  return {
    fetched: candidates.length,
    generated_at: new Date(now).toISOString(),
    skipped,
    prs: eligible.slice(0, 4),
    deferred: Math.max(0, eligible.length - 4),
  };
}

/** @returns {import("../actions/setup/js/report_ast.cjs").ReportInline} */
function reportLink(url, label, fallback, agentTainted = true) {
  if (url) {
    try {
      return { type: "link", url: normalizeReportURL(url), children: [{ type: "text", value: label, agentTainted: false }], agentTainted };
    } catch (error) {
      if (!(error instanceof TypeError)) throw error;
    }
  }
  return { type: "text", value: fallback, agentTainted: false };
}

/** @returns {import("../actions/setup/js/report_ast.cjs").Report | null} */
export function buildNudgeReport(pr) {
  if (pr.nudge_needed === false) return null;
  const checks = pr.failed_checks.filter(check => check.conclusion !== "ACTION_REQUIRED");
  if (!pr.unresolved_reviews.length && !checks.length && !pr.zero_diff_stalled && !["CONFLICTING", "BEHIND"].includes(pr.mergeStateStatus)) return null;
  /** @param {string} value @returns {import("../actions/setup/js/report_ast.cjs").ReportInline} */
  const text = value => ({ type: "text", value, agentTainted: false });
  /** @param {import("../actions/setup/js/report_ast.cjs").ReportInline[]} children @returns {import("../actions/setup/js/report_ast.cjs").ReportBlock} */
  const paragraph = children => ({ type: "paragraph", children, agentTainted: false });
  /** @type {import("../actions/setup/js/report_ast.cjs").ReportBlock[][]} */
  const tasks = [
    [
      paragraph([
        text("Update this branch with the latest "),
        { type: "code", value: pr.baseRefName ?? "main", agentTainted: true },
        text(" using "),
        { type: "code", value: "make merge-main", agentTainted: false },
        text(", resolving any conflicts and preserving the intended changes."),
      ]),
    ],
  ];
  for (const review of pr.unresolved_reviews) {
    const location = review.path ? ` (${review.path}${review.line ? `:${review.line}` : ""})` : "";
    for (const comment of review.comments) {
      tasks.push([paragraph([{ type: "text", value: `Review${location}: address unresolved feedback - `, agentTainted: true }, reportLink(comment.url, "Review comment", "see the PR review comments")])]);
    }
  }
  for (const check of checks)
    tasks.push([paragraph([text("Fix failing check "), { type: "code", value: check.name, agentTainted: true }, text(` (${check.conclusion}): `), reportLink(check.url, "Check run", "see the PR checks"), text(".")])]);
  if (pr.zero_diff_stalled) tasks.push([paragraph([text("Implement the requested change, or close this zero-diff PR with a reason.")])]);
  return {
    type: "report",
    title: "PR Sous Chef",
    agentTainted: false,
    children: [
      paragraph([{ type: "mention", login: "copilot", agentTainted: false }]),
      paragraph([text("🍳 A little prep to help this PR along! Copilot assignment requested for the following outstanding work:")]),
      { type: "list", ordered: true, items: tasks, agentTainted: false },
      paragraph([text("Keeping the changes focused; human review and CI get the final taste test. No merging or CI triggering.")]),
    ],
  };
}

export function formatAssignmentRequest(pr) {
  const report = buildNudgeReport(pr);
  return report ? renderSizedReport(report).body : null;
}

export function formatSousChefData(pr, kind) {
  return {
    workflow: "pr-sous-chef",
    schema_version: "1",
    kind,
    head: pr.headRefOid,
    work: workTokens(pr),
    state: pr.state_fingerprint,
    completed_task: kind === "ci-follow-up" ? pr.ci_follow_up.run_id : 0,
  };
}

export function formatCIFollowUp(pr) {
  if (!pr.ci_follow_up) return null;
  /** @type {import("../actions/setup/js/report_ast.cjs").Report} */
  const report = {
    type: "report",
    title: "PR Sous Chef CI follow-up",
    agentTainted: false,
    children: [
      {
        type: "paragraph",
        agentTainted: false,
        children: [
          { type: "text", value: "🍳 The prep is done, but CI needs another taste test. Copilot's assigned task has finished; these checks are still failing:", agentTainted: false },
          ...(pr.ci_follow_up.url ? [{ type: "text", value: " ", agentTainted: false }, reportLink(pr.ci_follow_up.url, "Completed task", "see the completed Copilot task", false)] : []),
        ],
      },
      {
        type: "list",
        ordered: false,
        agentTainted: false,
        items: pr.failed_checks
          .filter(check => check.conclusion !== "ACTION_REQUIRED")
          .map(check => [
            {
              type: "paragraph",
              agentTainted: false,
              children: [
                { type: "code", value: check.name, agentTainted: true },
                { type: "text", value: ` (${check.conclusion})`, agentTainted: false },
                ...(check.url ? [{ type: "text", value: " — ", agentTainted: false }, reportLink(check.url, "Check run", "see the PR checks")] : []),
              ],
            },
          ]),
      },
      {
        type: "paragraph",
        agentTainted: false,
        children: [{ type: "text", value: "Flagging the remaining blockers for maintainer investigation, not starting another copy of the same task.", agentTainted: false }],
      },
    ],
  };
  return renderSizedReport(report).body;
}

export async function checkRateLimit(api = gh) {
  const response = await api(["api", "rate_limit"]);
  const resources = ["core", "graphql", "search"].map(resource => {
    const budget = response.resources?.[resource];
    if (!budget || !Number.isInteger(budget.limit) || !Number.isInteger(budget.remaining) || !Number.isInteger(budget.reset) || budget.limit < 0 || budget.remaining < 0 || budget.remaining > budget.limit)
      throw new Error(`Missing or invalid GitHub ${resource} rate limit`);
    const threshold = Math.max(1, Math.ceil(budget.limit * 0.1));
    return { resource, remaining: budget.remaining, limit: budget.limit, threshold, reset_at: new Date(budget.reset * 1000).toISOString() };
  });
  return { low: resources.some(budget => budget.remaining <= budget.threshold), resources };
}

export async function fetchQueue(repo, checkNumber, api = gh, now = Date.now()) {
  let rateLimit = await checkRateLimit(api);
  const limitedQueue = () => ({
    fetched: 0,
    generated_at: new Date(now).toISOString(),
    skipped: [{ skip_reason: "rate_limit" }],
    prs: [],
    deferred: 0,
    rate_limit: rateLimit,
  });
  if (rateLimit.low) return limitedQueue();
  const candidates = checkNumber
    ? [await fetchCandidate(repo, checkNumber, api, now)]
    : await api(["pr", "list", "--repo", repo, "--state", "open", "--search", "is:pr is:open -is:draft -author:app/dependabot -author:app/renovate -label:broccoli sort:updated-desc", "--limit", "200", "--json", prFields]);
  if (!Array.isArray(candidates)) throw new Error("PR queue must be an array");
  let runs;
  const output = await buildQueue(
    candidates,
    async pr => {
      if (!rateLimit.low) rateLimit = await checkRateLimit(api);
      if (rateLimit.low) return { skip_reason: "rate_limit" };
      const current = checkNumber ? pr : await fetchCandidate(repo, pr.number, api, now);
      const skip = metadataSkip(current, now) || checkSkip(current, now);
      if (skip) return { skip_reason: skip };
      runs ??= await fetchActiveRuns(repo, api, checkNumber ? current.headRefName : undefined);
      if (activeAgent(current, runs)) return { skip_reason: "agent_active" };
      const approvals = current.statusCheckRollup.some(check => (check.conclusion ?? check.state) === "ACTION_REQUIRED" || check.status === "WAITING")
        ? (await api(["api", `repos/${repo}/actions/runs?status=action_required&per_page=100&branch=${encodeURIComponent(current.headRefName)}`, "--paginate", "--slurp"])).flatMap(page => page.workflow_runs)
        : [];
      return inspect(repo, current, [...runs, ...approvals], api, now);
    },
    now
  );
  if (rateLimit.low) return limitedQueue();
  return { ...output, rate_limit: rateLimit };
}

async function main() {
  const repo = process.env.PR_SOUS_CHEF_REPOSITORY ?? process.env.GITHUB_REPOSITORY;
  if (!/^[\w.-]+\/[\w.-]+$/.test(repo ?? "")) throw new Error("PR_SOUS_CHEF_REPOSITORY must be owner/repo");
  const args = process.argv.slice(2);
  const simulate = args.includes("--simulate");
  const numbers = args.filter(arg => arg !== "--simulate");
  const checkNumber = numbers[0];
  if (numbers.length > 1 || (checkNumber && !/^[1-9]\d*$/.test(checkNumber))) throw new Error("Expected [PR number] [--simulate]");
  const output = await fetchQueue(repo, checkNumber);
  if (simulate) {
    output.nudges = output.prs.flatMap(pr => {
      const body = formatAssignmentRequest(pr);
      return body ? [{ pr_number: pr.number, body, data: formatSousChefData(pr, "requested") }] : [];
    });
    output.ci_follow_ups = output.prs.flatMap(pr => {
      const body = formatCIFollowUp(pr);
      return body ? [{ pr_number: pr.number, body, data: formatSousChefData(pr, "ci-follow-up") }] : [];
    });
  }
  if (checkNumber || simulate) {
    console.log(JSON.stringify(output));
    return;
  }
  const directory = process.env.PR_SOUS_CHEF_OUTPUT_DIR ?? "/tmp/gh-aw/agent";
  ensureSecureDirectory(directory);
  writeFileSecure(`${directory}/pr-sous-chef-candidates-compact.json`, JSON.stringify(output));
  if (process.env.GITHUB_OUTPUT) {
    appendFileSync(process.env.GITHUB_OUTPUT, `eligible_count=${output.prs.length}\neligible_pull_request_numbers=${JSON.stringify(output.prs.map(pr => String(pr.number)))}\nrate_limit_low=${output.rate_limit.low}\n`);
  }
  if (process.env.GITHUB_STEP_SUMMARY) {
    appendFileSync(process.env.GITHUB_STEP_SUMMARY, formatSummary(output));
  }
}

export function formatSummary(output) {
  const counts = new Map();
  for (const item of output.skipped) counts.set(item.skip_reason, (counts.get(item.skip_reason) ?? 0) + 1);
  const budget = output.rate_limit
    ? `\n\n| API budget | Remaining / limit | No-op at or below | Resets |\n|---|---|---|---|\n${output.rate_limit.resources.map(item => `| ${item.resource} | ${item.remaining} / ${item.limit} | ${item.threshold} | ${item.reset_at} |`).join("\n")}\n`
    : "";
  const decision = output.rate_limit?.low ? "Agent skipped: GitHub API budget is low." : output.prs.length ? "Agent needed: executable progress actions remain." : "Agent skipped: no executable progress actions remain.";
  return `### PR Sous Chef prefilter\n\nFetched: ${output.fetched}; eligible: ${output.prs.length}; deferred: ${output.deferred}\n\n${decision}\n\n| Skip reason | Count |\n|---|---|\n${[...counts].map(([reason, count]) => `| ${reason} | ${count} |`).join("\n")}\n${budget}`;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch(error => {
    console.error(error.message);
    process.exitCode = 1;
  });
}
