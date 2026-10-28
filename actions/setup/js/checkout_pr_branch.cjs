// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Checkout PR branch when PR context is available
 *
 * This script handles checkout for different GitHub event types:
 *
 * 1. pull_request: Runs in merge commit context (PR head + base merged)
 *    - Fetches the immutable event head SHA, even after branch deletion
 *
 * 2. pull_request_target: Runs in BASE repository context (not PR head)
 *    - CRITICAL: For fork PRs, the head branch doesn't exist in base repo
 *    - Fetches the immutable event head SHA from origin (works for forks too)
 *    - Has write permissions (be cautious with untrusted code)
 *
 * 3. Other PR events (issue_comment, pull_request_review, etc.):
 *    - Also run in base repository context
 *    - Review events bind to the review/comment commit; issue comments select
 *      the current head, not a historically approved revision
 *
 * 4. workflow_dispatch with aw_context:
 *    - When aw_context input contains item_type=="pull_request" and item_number,
 *      the PR number is extracted and a specific head SHA is fetched
 *    - Mirrors the guard in the compiled workflow's if: condition
 *
 * NOTE: This handler operates within the PR context from the workflow event
 * and does not support cross-repository operations or target-repo parameters.
 * No allowlist validation (checkAllowedRepo/validateTargetRepo) is needed as
 * it only works with the PR from the triggering event.
 */

const { getErrorMessage } = require("./error_helpers.cjs");
const { redactAndBoundDiagnostics } = require("./diagnostic_sanitization.cjs");
const { renderTemplateFromFile, getPromptPath } = require("./messages_core.cjs");
const { detectForkPR } = require("./pr_helpers.cjs");
const { parseAllowedBots, isAllowedBot, canonicalizeBotIdentifier } = require("./check_permissions_utils.cjs");
const { ERR_API, ERR_PERMISSION } = require("./error_codes.cjs");
const TRUSTED_CHECKOUT_PERMISSIONS = ["write", "maintain", "admin"];
const TRUSTED_BOT_PR_ACTIONS = ["opened", "synchronize", "ready_for_review", "reopened"];
// Centralized command/label routing uses the repository GITHUB_TOKEN, whose
// GitHub-authenticated actor is fixed to this platform identity.
const CENTRALIZED_ROUTER_ACTOR = "github-actions[bot]";
const PR_HEAD_BASE_REF = "refs/remotes/origin/pr-head";

/** @param {string} text */
function redactCheckoutDiagnostics(text) {
  const redactCredentials = value => value.replace(/(https?:\/\/)[^/\s@]+@/gi, "$1[REDACTED]@").replace(/(authorization:\s*(?:bearer|basic)\s+)\S+/gi, "$1[REDACTED]");
  return redactCredentials(redactAndBoundDiagnostics(redactCredentials(text), { maskedValues: [process.env.GITHUB_TOKEN, process.env.GH_TOKEN].flatMap(value => (value ? [value] : [])) }));
}

class PRHeadFetchError extends Error {
  /** @param {number} exitCode @param {string} stderr */
  constructor(exitCode, stderr) {
    const missingRef = /could(?:n't| not) find remote ref|remote ref does not exist|not our ref [0-9a-f]{40}/i.test(stderr);
    const shaFetchRejected = missingRef || /unadvertised object/i.test(stderr);
    const guidance = shaFetchRejected
      ? " Immutable PR checkout requires origin to allow fetching the selected commit SHA, including reachable unadvertised commits. On GitHub Enterprise Server, ask your administrator to verify commit availability and the upload-pack SHA fetch policy, then trigger a new PR event; do not fall back to a mutable ref."
      : "";
    super(`git fetch failed with exit code ${exitCode}${stderr.trim() ? `: ${redactCheckoutDiagnostics(stderr.trim())}` : ""}${guidance}`);
    this.missingRef = missingRef;
  }
}

/** @param {string[]} args */
async function fetchPRHead(args) {
  // @actions/exec rejects with only the exit code, not git's stderr.
  const { exitCode, stderr } = await exec.getExecOutput("git", ["fetch", "origin", ...args], { silent: true, ignoreReturnCode: true });
  if (exitCode !== 0) {
    throw new PRHeadFetchError(exitCode, stderr);
  }
}

/** @param {unknown} value @returns {value is string} */
function isCommitSha(value) {
  return typeof value === "string" && /^[0-9a-f]{40}$/i.test(value);
}

function isOrdinaryPREvent() {
  return context.eventName === "pull_request" && !["labeled", "unlabeled"].includes(context.payload.action || "");
}

/**
 * Issue comments have no historical PR head SHA. They authorize latest-head
 * execution, not approval of a revision. PR update timestamps also advance
 * for unrelated activity such as the router's own status comment.
 */
function validateCommentContext(commentTime, pullRequest) {
  const commandTime = typeof commentTime === "string" ? Date.parse(commentTime) : NaN;
  const updateTime = typeof pullRequest.updated_at === "string" ? Date.parse(pullRequest.updated_at) : NaN;
  if (!Number.isFinite(commandTime) || !Number.isFinite(updateTime)) {
    throw new Error(`${ERR_PERMISSION}: Refusing PR checkout: comment freshness metadata is missing or invalid; rerun from a PR event or disable PR checkout`);
  }
  if (updateTime > commandTime) {
    core.warning("PR was updated after the triggering comment; selecting the current head, not an approved revision.");
  }
  core.warning("Issue comments select the current PR head; they do not prove approval of that revision.");
}

async function resolvePRHeadSha(pullRequest, awContext) {
  let expectedSha;
  if (context.eventName === "pull_request_review") {
    expectedSha = context.payload.review?.commit_id;
    if (!isCommitSha(expectedSha)) {
      throw new Error(`${ERR_PERMISSION}: Refusing PR checkout: missing or invalid review commit SHA; trigger a new review event`);
    }
  } else if (context.eventName === "pull_request_review_comment") {
    expectedSha = context.payload.comment?.commit_id;
    if (!isCommitSha(expectedSha)) {
      throw new Error(`${ERR_PERMISSION}: Refusing PR checkout: missing or invalid review comment commit SHA; trigger a new review comment event`);
    }
  } else if (context.eventName === "issue_comment") {
    validateCommentContext(context.payload.comment?.updated_at ?? context.payload.comment?.created_at, pullRequest);
  } else if (context.eventName === "workflow_dispatch") {
    expectedSha = awContext?.pr_head_sha === "" ? undefined : awContext?.pr_head_sha;
    if (awContext?.command_name || awContext?.trigger_label) {
      if (awContext.event_type === "issue_comment") {
        validateCommentContext(awContext.comment_updated_at, pullRequest);
      } else if (!isCommitSha(expectedSha)) {
        throw new Error(`${ERR_PERMISSION}: Refusing PR checkout: command/label dispatch has no immutable PR revision context; trigger a new PR/review event through the current router`);
      }
    } else if (expectedSha === undefined) {
      core.warning("Manual PR dispatch selects the current head, not an approved revision.");
    }
  } else {
    expectedSha = context.payload.pull_request?.head?.sha;
    if (!isCommitSha(expectedSha)) {
      throw new Error(`${ERR_PERMISSION}: Refusing PR checkout: missing or invalid event PR head SHA; trigger a new PR event`);
    }
  }
  if (expectedSha !== undefined && !isCommitSha(expectedSha)) {
    throw new Error(`${ERR_PERMISSION}: Refusing PR checkout: invalid event PR head SHA; expected a full 40-character hexadecimal commit SHA`);
  }
  expectedSha = expectedSha?.toLowerCase();
  const sha = expectedSha || pullRequest.head?.sha;
  if (!isCommitSha(sha)) {
    throw new Error(`${ERR_PERMISSION}: Refusing PR checkout: missing or invalid PR head SHA; expected a full 40-character hexadecimal commit SHA`);
  }
  if (expectedSha && !isOrdinaryPREvent() && (!isCommitSha(pullRequest.head?.sha) || pullRequest.head.sha.toLowerCase() !== expectedSha)) {
    throw new Error(`${ERR_PERMISSION}: Refusing PR checkout: PR head changed since the triggering revision; trigger a new event for the current revision`);
  }
  return sha.toLowerCase();
}

/** @param {string} expectedSha */
async function assertCheckedOutRevision(expectedSha) {
  const actualSha = await resolveCheckedOutHeadSha();
  if (actualSha !== expectedSha) {
    throw new Error(`${ERR_PERMISSION}: Refusing PR checkout: checked-out HEAD does not match expected commit ${expectedSha}; stop and inspect the checkout`);
  }
  return actualSha;
}

/**
 * Resolve the commit SHA actually checked out at HEAD.
 *
 * Resolve independently of the payload/API so an unexpected fetch or checkout
 * result cannot become a successful revision binding or patch baseline.
 *
 * @returns {Promise<string | null>}
 */
async function resolveCheckedOutHeadSha() {
  try {
    const result = await exec.getExecOutput("git", ["rev-parse", "HEAD^{commit}"], {
      silent: true,
      ignoreReturnCode: true,
    });
    if (result.exitCode !== 0) {
      return null;
    }
    const sha = result.stdout.trim();
    return sha || null;
  } catch (e) {
    core.warning(`Could not resolve checked-out HEAD commit: ${getErrorMessage(e)}`);
    return null;
  }
}

async function exportPRHeadBaseline({ branchName, baseRepo, headRepo, prNumber, expectedSha }) {
  const baseSha = await assertCheckedOutRevision(expectedSha);
  if (!branchName) {
    return;
  }
  core.exportVariable("GH_AW_PR_HEAD_BASE_BRANCH", branchName);
  core.exportVariable("GH_AW_PR_HEAD_BASE_SHA", baseSha);
  if (baseRepo) {
    core.exportVariable("GH_AW_PR_HEAD_BASE_REPO", baseRepo);
  }
  if (headRepo) {
    core.exportVariable("GH_AW_PR_HEAD_REPO", headRepo);
  }
  if (prNumber != null) {
    core.exportVariable("GH_AW_PR_HEAD_BASE_PR_NUMBER", String(prNumber));
  }
  core.exportVariable("GH_AW_PR_HEAD_BASE_REF", PR_HEAD_BASE_REF);
  core.info(`Recorded PR head baseline for incremental patches: ${branchName}@${baseSha}`);
}

/**
 * Determine whether the current repository is a shallow clone.
 *
 * A `--depth` fetch against an already-complete clone writes `.git/shallow` and
 * grafts history, silently undoing an explicit `checkout: fetch-depth: 0`. That
 * breaks later `git merge-base` calls (e.g. patch generation for
 * create_pull_request). We therefore only pass `--depth` when the repository is
 * already shallow; a complete clone already has the objects we need.
 *
 * @returns {Promise<boolean>} true when the repository is shallow
 */
async function isShallowRepository() {
  try {
    const result = await exec.getExecOutput("git", ["rev-parse", "--is-shallow-repository"], {
      silent: true,
      ignoreReturnCode: true,
    });
    if (result.exitCode !== 0) {
      return false;
    }
    return result.stdout.trim() === "true";
  } catch (e) {
    core.warning(`Could not determine repository shallowness, assuming complete clone: ${getErrorMessage(e)}`);
    return false;
  }
}

/**
 * Build the optional `--depth=N` argument for a fetch, omitting it when the
 * repository is a complete (non-shallow) clone.
 *
 * @param {number} fetchDepth
 * @returns {Promise<string[]>}
 */
async function depthArgs(fetchDepth) {
  if (await isShallowRepository()) {
    return [`--depth=${fetchDepth}`];
  }
  core.info("Repository is not shallow (e.g. fetch-depth: 0), fetching without --depth to preserve full history");
  return [];
}

/**
 * Log detailed PR context information for debugging
 */
function logPRContext(eventName, pullRequest) {
  core.startGroup("📋 PR Context Details");

  core.info(`Event type: ${eventName}`);
  core.info(`PR number: ${pullRequest.number}`);
  core.info(`PR state: ${pullRequest.state || "unknown"}`);

  // Log head information
  if (pullRequest.head) {
    core.info(`Head ref: ${pullRequest.head.ref || "unknown"}`);
    core.info(`Head SHA: ${pullRequest.head.sha || "unknown"}`);

    if (pullRequest.head.repo) {
      core.info(`Head repo: ${pullRequest.head.repo.full_name || "unknown"}`);
      core.info(`Head repo owner: ${pullRequest.head.repo.owner?.login || "unknown"}`);
    } else {
      core.warning("⚠️ Head repo information not available (repo may be deleted)");
    }
  }

  // Log base information
  if (pullRequest.base) {
    core.info(`Base ref: ${pullRequest.base.ref || "unknown"}`);
    core.info(`Base SHA: ${pullRequest.base.sha || "unknown"}`);

    if (pullRequest.base.repo) {
      core.info(`Base repo: ${pullRequest.base.repo.full_name || "unknown"}`);
      core.info(`Base repo owner: ${pullRequest.base.repo.owner?.login || "unknown"}`);
    }
  }

  // Determine if this is a fork PR using the helper function.
  // Only call detectForkPR when head/base data is present (pull_request and
  // pull_request_target payloads). For minimal PR objects (e.g. issue_comment)
  // fork status is unknown until we fetch full PR details from the API.
  /** @type {any} */
  let isFork = null;
  if (pullRequest.head?.repo && pullRequest.base?.repo) {
    const { isFork: detected, reason: forkReason } = detectForkPR(pullRequest);
    isFork = detected;
    core.info(`Is fork PR: ${isFork} (${forkReason})`);
  } else {
    core.info("Is fork PR: unknown (head/base repo details not available in event payload)");
  }

  // Log current repository context
  core.info(`Current repository: ${context.repo.owner}/${context.repo.repo}`);
  core.info(`GitHub SHA: ${context.sha}`);

  core.endGroup();

  return { isFork };
}

/**
 * Fetch PR details from the GitHub API.
 * Returns head ref and commit count needed for checkout.
 */
async function fetchPRDetails(prNumber) {
  const { data } = await github.rest.pulls.get({
    owner: context.repo.owner,
    repo: context.repo.repo,
    pull_number: prNumber,
  });
  return { commitCount: data.commits, headRef: data.head.ref, pullRequest: data };
}

/**
 * Log the checkout strategy being used
 */
function logCheckoutStrategy(eventName, strategy, reason) {
  core.startGroup("🔄 Checkout Strategy");
  core.info(`Event type: ${eventName}`);
  core.info(`Strategy: ${strategy}`);
  core.info(`Reason: ${reason}`);
  core.endGroup();
}

/**
 * Parse a canonical positive decimal pull request number without coercing
 * booleans, exponent notation, hexadecimal, whitespace, or fractional values.
 * @param {unknown} value
 * @returns {number | null}
 */
function parsePullRequestNumber(value) {
  if (typeof value === "number") {
    return Number.isSafeInteger(value) && value > 0 ? value : null;
  }
  if (typeof value !== "string" || !/^[1-9]\d*$/.test(value)) {
    return null;
  }
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) ? parsed : null;
}

/**
 * Ensure checkout step only runs in trusted runtime contexts.
 * - workflow_dispatch PR replay must not run in a forked repository
 * - triggering actor must have write-or-higher repository permission, or be
 *   the bot sender of a same-repository PR event
 * @param {Record<string, any> | undefined} awContext Parsed workflow_dispatch context, consulted only for github-actions bot dispatches
 */
async function assertTrustedCheckoutRuntime(awContext, pullRequest) {
  if (context.eventName === "workflow_dispatch") {
    const repository = context.payload.repository;
    // Fork status can only be verified when the payload carries repository
    // data. Fail closed rather than silently trusting an unverifiable
    // runtime: RS-05a requires proving "not a fork", not merely failing to
    // prove "is a fork".
    if (!repository) {
      throw new Error(`${ERR_PERMISSION}: ` + "Refusing PR checkout: unable to determine repository fork status for workflow_dispatch");
    }
    if (repository.fork === true) {
      throw new Error(`${ERR_PERMISSION}: ` + "Refusing PR checkout in forked repository runtime context");
    }
    if (repository.fork !== false) {
      throw new Error(`${ERR_PERMISSION}: ` + "Refusing PR checkout: unable to verify repository is not a fork for workflow_dispatch");
    }
  }

  // context.actor is preferred when available; sender.login and GITHUB_ACTOR
  // are retained as event/runtime-compatible fallbacks.
  let actor = context.actor || context.payload.sender?.login || process.env.GITHUB_ACTOR;
  const senderType = context.payload.sender?.type;
  // GitHub attributes direct dispatches to their initiating user/app, while
  // repository workflows using GITHUB_TOKEN run as github-actions[bot].
  // Only trusted workflows may hold actions:write. command_name and
  // trigger_label are shape checks, not additional proof of provenance.
  if (context.eventName === "workflow_dispatch" && actor === CENTRALIZED_ROUTER_ACTOR) {
    if (senderType !== "Bot") {
      throw new Error(`${ERR_PERMISSION}: ` + "Refusing PR checkout: unable to verify centralized workflow_dispatch identity");
    }
    const commandName = typeof awContext?.command_name === "string" ? awContext.command_name.trim() : "";
    const triggerLabel = typeof awContext?.trigger_label === "string" ? awContext.trigger_label.trim() : "";
    if (!commandName && !triggerLabel) {
      throw new Error(`${ERR_PERMISSION}: ` + "Refusing PR checkout: unable to identify centralized workflow_dispatch");
    }
    const propagatedActor = typeof awContext?.actor === "string" ? awContext.actor.trim() : "";
    if (!propagatedActor) {
      throw new Error(`${ERR_PERMISSION}: ` + "Refusing PR checkout: unable to determine originating actor for centralized workflow_dispatch");
    }
    if (propagatedActor === CENTRALIZED_ROUTER_ACTOR) {
      throw new Error(`${ERR_PERMISSION}: ` + "Refusing PR checkout: centralized workflow_dispatch must identify an originating actor");
    }
    actor = propagatedActor;
    core.info(`Validating centralized workflow_dispatch against originating actor '${actor}'`);
  }
  if (!actor) {
    throw new Error(`${ERR_PERMISSION}: ` + "Refusing PR checkout: unable to determine triggering actor");
  }

  const changes = context.payload.changes;
  const isAllowedBotBodyEdit =
    context.payload.action === "edited" &&
    isAllowedBot(actor, parseAllowedBots()) &&
    changes !== null &&
    typeof changes === "object" &&
    Object.prototype.hasOwnProperty.call(changes, "body") &&
    changes.body !== null &&
    typeof changes.body === "object" &&
    !Array.isArray(changes.body) &&
    Object.keys(changes).every(key => key === "body");

  // Apps are not collaborators: their permission endpoint returns "none" even
  // when they can write to the repository. A bot that triggered a PR event for
  // a branch in this repository can have its checkout trusted instead. Other
  // events require the collaborator check unless explicitly allow-listed below.
  if (
    (context.eventName === "pull_request" || context.eventName === "pull_request_target") &&
    typeof context.payload.action === "string" &&
    (TRUSTED_BOT_PR_ACTIONS.includes(context.payload.action) || isAllowedBotBodyEdit) &&
    senderType === "Bot" &&
    context.payload.sender?.login === actor &&
    context.payload.repository !== undefined &&
    context.payload.repository !== null &&
    Number.isSafeInteger(context.payload.repository.id) &&
    context.payload.repository.id > 0 &&
    pullRequest?.head?.repo?.id === context.payload.repository.id &&
    pullRequest?.base?.repo?.id === context.payload.repository.id
  ) {
    core.info(`Runtime safety check passed for same-repository bot PR actor '${actor}'`);
    return undefined;
  }

  const runtimeRepositoryId = context.payload.repository?.id;
  const senderLogin = context.payload.sender?.login;
  const isReview = context.eventName === "pull_request_review";
  if (
    (context.eventName === "issue_comment" || context.eventName === "pull_request_review_comment" || isReview) &&
    senderType === "Bot" &&
    typeof senderLogin === "string" &&
    (isReview ? context.payload.review?.user?.login : context.payload.comment?.user?.login) === senderLogin &&
    canonicalizeBotIdentifier(senderLogin) === canonicalizeBotIdentifier(actor) &&
    isAllowedBot(actor, parseAllowedBots()) &&
    Number.isSafeInteger(runtimeRepositoryId) &&
    runtimeRepositoryId > 0
  ) {
    const details = await fetchPRDetails(pullRequest.number);
    if (details.pullRequest.head?.repo?.id === runtimeRepositoryId && details.pullRequest.base?.repo?.id === runtimeRepositoryId) {
      core.info(`Runtime safety check passed for same-repository allowed bot ${isReview ? "review" : "comment"} actor '${actor}'`);
      return details;
    }
  }

  // All other actors must satisfy the collaborator permission floor.
  try {
    const { data: permissionData } = await github.rest.repos.getCollaboratorPermissionLevel({
      owner: context.repo.owner,
      repo: context.repo.repo,
      username: actor,
    });

    const permission = permissionData?.permission || "none";
    const hasWriteOrHigher = TRUSTED_CHECKOUT_PERMISSIONS.includes(permission);
    if (!hasWriteOrHigher) {
      throw new Error(`${ERR_PERMISSION}: Refusing PR checkout: actor '${actor}' has '${permission}' permission (requires write or higher)`);
    }

    core.info(`Runtime safety check passed for actor '${actor}' with '${permission}' permission`);
  } catch (err) {
    // A 404 here is ambiguous: it can indicate either a non-user app/bot actor
    // or a real user that is not a collaborator. Disambiguate via users API so
    // user denials stay clear, but fail closed for app/bot actors whose
    // repository permission cannot be verified.
    const errAny = /** @type {any} */ err;
    if (errAny.status === 404) {
      try {
        await github.rest.users.getByUsername({ username: actor });
        throw new Error(`${ERR_PERMISSION}: Refusing PR checkout: actor '${actor}' is not a collaborator (requires write or higher)`);
      } catch (userErr) {
        const userErrAny = /** @type {any} */ userErr;
        if (userErrAny.status === 404) {
          throw new Error(`${ERR_PERMISSION}: Refusing PR checkout: bot/app actor '${actor}' repository permission could not be verified (sender type: ${senderType || "unknown"})`);
        }
        throw userErr;
      }
    }
    throw err;
  }
  return undefined;
}

async function main() {
  const eventName = context.eventName;
  // For pull_request events, the PR context is in context.payload.pull_request.
  // For issue_comment events on PRs, context.payload.pull_request is not set;
  // instead context.payload.issue.pull_request indicates the issue is a PR.
  let pullRequest = context.payload.pull_request;
  /** @type {Record<string, any> | undefined} */
  let workflowDispatchAwContext;

  // Handle issue_comment (and similar) events triggered on a PR
  if (!pullRequest && context.payload.issue?.pull_request) {
    pullRequest = {
      number: context.payload.issue.number,
      state: context.payload.issue.state || "open",
    };
    core.info(`Detected ${eventName} event on PR #${pullRequest.number}, will fetch PR ref`);
  }

  // Handle workflow_dispatch events with aw_context pointing to a PR
  if (!pullRequest && eventName === "workflow_dispatch") {
    const awContextStr = context.payload.inputs?.aw_context;
    if (awContextStr) {
      try {
        const awContext = JSON.parse(awContextStr);
        if (!awContext || typeof awContext !== "object" || Array.isArray(awContext)) {
          throw new Error(`${ERR_PERMISSION}: aw_context must be an object, for example {"item_type":"pull_request","item_number":123}`);
        }
        workflowDispatchAwContext = awContext;
        const prNumber = parsePullRequestNumber(awContext.item_number);
        if (awContext.item_type === "pull_request") {
          if (prNumber === null) {
            throw new Error(`${ERR_PERMISSION}: aw_context must identify a canonical positive PR number, for example {"item_type":"pull_request","item_number":123}`);
          }
          if (awContext.repo !== undefined && awContext.repo !== "") {
            const currentRepo = `${context.repo.owner}/${context.repo.repo}`;
            if (awContext.repo !== currentRepo) {
              throw new Error(`${ERR_PERMISSION}: Cross-repository workflow_dispatch is not supported: aw_context.repo does not match the current repository; dispatch in the PR repository instead`);
            } else {
              pullRequest = {
                number: prNumber,
                state: "open",
              };
              core.info(`Detected workflow_dispatch event for PR #${pullRequest.number} via aw_context, will fetch PR ref`);
            }
          } else {
            pullRequest = {
              number: prNumber,
              state: "open",
            };
            core.info(`Detected workflow_dispatch event for PR #${pullRequest.number} via aw_context, will fetch PR ref`);
          }
        }
      } catch (e) {
        core.setOutput("checkout_pr_success", "false");
        core.setFailed(`${ERR_PERMISSION}: Refusing PR checkout: invalid aw_context: ${redactCheckoutDiagnostics(getErrorMessage(e))}`);
        return;
      }
    }
  }

  if (!pullRequest) {
    core.info("No pull request context available, skipping checkout");
    core.setOutput("checkout_pr_success", "true");
    return;
  }

  core.info(`Event: ${eventName}`);
  core.info(`Pull Request #${pullRequest.number}`);

  // Check if PR is closed
  const isClosed = pullRequest.state === "closed";
  if (isClosed) {
    core.info("⚠️ Pull request is closed");
  }

  try {
    const trustedPRDetails = await assertTrustedCheckoutRuntime(workflowDispatchAwContext, pullRequest);

    // Log detailed context for debugging
    const { isFork } = logPRContext(eventName, pullRequest);

    if (isOrdinaryPREvent() && isFork === false && !isClosed) {
      // Fetch the event commit, not a branch that may have moved or disappeared.
      const branchName = pullRequest.head.ref;
      // commits is in the payload for pull_request events; +1 to include the merge base
      const commitCount = pullRequest.commits || 1;
      const fetchDepth = commitCount + 1;
      const expectedSha = await resolvePRHeadSha(pullRequest, workflowDispatchAwContext);

      logCheckoutStrategy(eventName, "git fetch SHA + checkout", "same-repository pull_request event; immutable event SHA survives branch deletion");

      core.info(`Fetching PR #${pullRequest.number} commit ${expectedSha} (depth: ${fetchDepth} for ${commitCount} PR commit(s))`);
      const fetchArgs = await depthArgs(fetchDepth);
      core.info(fetchArgs.length > 0 ? `Fetching with ${fetchArgs.join(" ")}` : "Fetching without --depth (full history preserved)");
      // Origin must permit fetching reachable unadvertised commit SHAs.
      await fetchPRHead([`+${expectedSha}:${PR_HEAD_BASE_REF}`, ...fetchArgs]);

      core.info(`Checking out branch: ${branchName}`);
      await exec.exec("git", ["checkout", "-B", branchName, PR_HEAD_BASE_REF]);

      await exportPRHeadBaseline({
        branchName,
        baseRepo: pullRequest.base?.repo?.full_name || `${context.repo.owner}/${context.repo.repo}`,
        headRepo: pullRequest.head?.repo?.full_name,
        prNumber: pullRequest.number,
        expectedSha,
      });
      core.info(`✅ Successfully checked out branch: ${branchName}`);
    } else {
      // For pull_request_target, fork pull_request events, and other PR events,
      // we run in base repository context.
      // Fetch the resolved SHA from origin for same-repository and fork PRs.
      const prNumber = pullRequest.number;

      // Get PR details from API to determine head ref name and commit count.
      // This also gives us the full PR object for accurate fork detection
      // when the event payload only had a minimal PR (e.g. issue_comment).
      const { commitCount: currentCommitCount, headRef, pullRequest: fullPR } = trustedPRDetails || (await fetchPRDetails(prNumber));
      const expectedSha = await resolvePRHeadSha(fullPR, workflowDispatchAwContext);
      // Ordinary PR events select the historical event SHA, not the live head.
      const commitCount = isOrdinaryPREvent() ? pullRequest.commits : currentCommitCount;

      // Re-evaluate fork status with full PR data when it was unknown
      const fullPRForkDetection = detectForkPR(fullPR);
      const actualIsFork = isFork ?? fullPRForkDetection.isFork;
      if (isFork === null) {
        core.info(`Is fork PR (from API): ${actualIsFork} (${fullPRForkDetection.reason})`);
      }

      const strategyReason =
        eventName === "pull_request_target"
          ? "pull_request_target runs in base repo context; fetching an immutable PR head SHA"
          : eventName === "pull_request" && actualIsFork
            ? "pull_request event from fork repository; fetching an immutable PR head SHA"
            : `${eventName} event runs in base repo context; fetching an immutable PR head SHA`;

      logCheckoutStrategy(eventName, "git fetch SHA + checkout", strategyReason);

      if (actualIsFork) {
        core.warning("⚠️ Fork PR detected - fetching an immutable PR head SHA from origin");
      }
      const fetchDepth = (commitCount || 1) + 1; // +1 to include the merge base

      core.info(`Fetching PR #${prNumber} commit ${expectedSha} (depth: ${fetchDepth} for ${commitCount} PR commit(s))`);
      const prFetchArgs = await depthArgs(fetchDepth);
      core.info(prFetchArgs.length > 0 ? `Fetching with ${prFetchArgs.join(" ")}` : "Fetching without --depth (full history preserved)");
      // Origin must permit fetching reachable unadvertised commit SHAs.
      await fetchPRHead([`+${expectedSha}:${PR_HEAD_BASE_REF}`, ...prFetchArgs]);
      if (!isOrdinaryPREvent()) {
        const currentDetails = await fetchPRDetails(prNumber);
        const currentSha = await resolvePRHeadSha(currentDetails.pullRequest, workflowDispatchAwContext);
        if (currentSha !== expectedSha) {
          throw new Error(`${ERR_PERMISSION}: Refusing PR checkout: PR head changed while fetching the triggering revision; trigger a new event for the current revision`);
        }
      }

      const branchName = headRef || `pr-${prNumber}`;
      core.info(`Checking out branch: ${branchName}`);
      await exec.exec("git", ["checkout", "-B", branchName, PR_HEAD_BASE_REF]);

      await exportPRHeadBaseline({
        branchName,
        baseRepo: fullPR.base?.repo?.full_name || `${context.repo.owner}/${context.repo.repo}`,
        headRepo: fullPR.head?.repo?.full_name,
        prNumber,
        expectedSha,
      });
      core.info(`✅ Successfully checked out PR #${prNumber}`);
      core.info(`Current branch: ${branchName}`);
    }

    // Set output to indicate successful checkout
    core.setOutput("checkout_pr_success", "true");
  } catch (error) {
    const errorMsg = redactCheckoutDiagnostics(getErrorMessage(error));

    // Re-check current PR state via API to handle race conditions where
    // the PR was merged/closed after the webhook payload was captured but
    // before the agent job ran (e.g. PR merged within seconds of triggering).
    let isNowClosed = isClosed;
    try {
      const { data: currentPR } = await github.rest.pulls.get({
        owner: context.repo.owner,
        repo: context.repo.repo,
        pull_number: pullRequest.number,
      });
      isNowClosed = currentPR.state === "closed";
      if (isNowClosed) {
        core.info(`PR #${pullRequest.number} is now closed (was '${pullRequest.state}' in webhook payload)`);
      }
    } catch (apiError) {
      const apiErrorMsg = getErrorMessage(apiError);
      const statusCode = /** @type {any} */ apiError?.status;
      const statusSuffix = statusCode ? ` (HTTP ${statusCode})` : "";
      core.warning(`Could not fetch current PR state${statusSuffix}: ${apiErrorMsg}`);
    }

    // Closure alone must not hide permission, authentication, or network errors.
    if (!isClosed && isNowClosed && error instanceof PRHeadFetchError && error.missingRef) {
      core.setOutput("checkout_pr_invalidated", "true");
      core.warning(`PR #${pullRequest.number} closed before its head could be fetched. Stopping rather than running on the previous checkout.`);
    }

    // For open PRs, treat checkout failure as an error
    // Log detailed error context
    core.startGroup("❌ Checkout Error Details");
    core.error(`Event type: ${eventName}`);
    core.error(`PR number: ${pullRequest.number}`);
    core.error(`Error message: ${errorMsg}`);

    if (pullRequest.head?.ref) {
      core.error(`Attempted to check out: ${pullRequest.head.ref}`);
    }

    // Log current git state for debugging
    try {
      /** @type {[string, string[]][]} */
      const diagnostics = [
        ["Current git status:", ["status"]],
        ["Available remotes:", ["remote", "-v"]],
        ["Current branch:", ["branch", "--show-current"]],
      ];
      for (const [label, args] of diagnostics) {
        core.info(label);
        const { stdout, stderr } = await exec.getExecOutput("git", args, { silent: true });
        if (stdout) core.info(redactCheckoutDiagnostics(stdout));
        if (stderr) core.warning(redactCheckoutDiagnostics(stderr));
      }
    } catch (gitError) {
      core.warning(`Could not retrieve git state: ${redactCheckoutDiagnostics(getErrorMessage(gitError))}`);
    }

    core.endGroup();

    // Set output to indicate checkout failure
    core.setOutput("checkout_pr_success", "false");

    // Load and render step summary template
    const templatePath = getPromptPath("pr_checkout_failure.md");
    const summaryContent = renderTemplateFromFile(templatePath, {
      error_message: errorMsg,
    });

    await core.summary.addRaw(summaryContent).write();
    core.setFailed(`${ERR_API}: Failed to checkout PR branch: ${errorMsg}`);
  }
}

module.exports = { main };
