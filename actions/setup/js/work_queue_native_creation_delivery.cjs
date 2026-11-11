// @ts-check
"use strict";
// @safe-outputs-exempt SEC-005 — creation receipts are private, Claim-bound records; delivery performs independently scoped native readback only.

const { canonical, digest } = require("./work_queue_codec.cjs");
const { currentClaimHandle, claimIdentity, receiptMatchesClaim, createClaimResourceVerification } = require("./work_queue_claim_scope.cjs");
const { canonicalResourceTarget, resolveRepositoryTarget, resolveParentResourceTarget, nativeDecimalIdentity } = require("./work_queue_effect_resource.cjs");
const log = require("./work_queue_logging.cjs").createWorkQueueLogger("native_delivery");
const { readQueueMetadata } = require("./work_queue_read_retry.cjs");
const { MAX_CLOSE_COUNT } = require("./constants.cjs");

const receipts = new WeakMap();
const REVISION = /^(?:[a-f0-9]{40}|[a-f0-9]{64})$/;

function nativeIdentityMatches(value, expected) {
  const identity = typeof value === "number" && Number.isSafeInteger(value) ? String(value) : value;
  return typeof identity === "string" && /^[1-9][0-9]{0,255}$/.test(identity) && nativeDecimalIdentity(value) === expected;
}

function captureIssueDelivery(result, expected) {
  if (currentClaimHandle()) receipts.set(result, { ...claimIdentity(currentClaimHandle()), type: "create_issue", ...structuredClone(expected) });
  return result;
}

function captureDiscussionDelivery(result, message, expected) {
  if (currentClaimHandle()) receipts.set(result, { ...claimIdentity(currentClaimHandle()), type: "create_discussion", message: canonical({ ...message, repo: expected.repository }), ...structuredClone(expected) });
  return result;
}

async function preparePullRequestDelivery(exec, baseBranch, transportBaseBranch = baseBranch) {
  if (!currentClaimHandle()) return null;
  const read = async revision => {
    const result = await exec.getExecOutput("git", ["rev-parse", "--verify", revision], { silent: true });
    const sha = result.stdout.trim();
    if (result.exitCode !== 0 || !REVISION.test(sha)) throw new Error("Claim pull request preparation requires exact local commit/tree evidence");
    return sha;
  };
  return {
    tree: await read("HEAD^{tree}"),
    base: await read(`refs/remotes/origin/${baseBranch}^{commit}`),
    transportBase: await read(`refs/remotes/origin/${transportBaseBranch}^{commit}`),
  };
}

async function readAncestry(github, repository, base, head) {
  const [owner, repo] = repository.split("/");
  const { data } = await github.rest.repos.compareCommits({ owner, repo, base, head });
  return data?.base_commit?.sha === base && data.merge_base_commit?.sha === base && data.status === "ahead" && Number.isSafeInteger(data.ahead_by) && data.ahead_by > 0 && data.behind_by === 0;
}

async function readCode(github, receipt, pinnedCommit) {
  const [owner, repo] = receipt.headRepository.split("/");
  const { data: ref } = await github.rest.git.getRef({ owner, repo, ref: `heads/${receipt.branch}` });
  const sha = ref?.object?.sha;
  if (ref?.ref !== `refs/heads/${receipt.branch}` || ref.object.type !== "commit" || !REVISION.test(sha) || (pinnedCommit !== undefined && sha !== pinnedCommit)) {
    log.debug("creation.code.ref_mismatch", { ref_matches: ref?.ref === `refs/heads/${receipt.branch}`, commit_type: ref?.object?.type === "commit", pinned_commit_matches: pinnedCommit === undefined || sha === pinnedCommit });
    return null;
  }
  const { data: commit } = await github.rest.git.getCommit({ owner, repo, commit_sha: sha });
  if (commit?.sha !== sha || commit.tree?.sha !== receipt.tree || !Array.isArray(commit.parents) || !commit.parents.length || commit.parents.some(parent => !REVISION.test(parent.sha))) {
    log.debug("creation.code.commit_mismatch", { commit_matches: commit?.sha === sha, tree_matches: commit?.tree?.sha === receipt.tree, parents_present: Array.isArray(commit?.parents) && commit.parents.length > 0 });
    return null;
  }
  const { data: tree } = await github.rest.git.getTree({ owner, repo, tree_sha: receipt.tree });
  if (tree?.sha !== receipt.tree || tree.truncated !== false || !Array.isArray(tree.tree)) {
    log.debug("creation.code.tree_invalid", { tree_matches: tree?.sha === receipt.tree, complete: tree?.truncated === false, entries_present: Array.isArray(tree?.tree) });
    return null;
  }
  // Cross-repository PR comparisons are read from the base repository using a qualified head.
  const head = receipt.headRepository === receipt.repository ? sha : `${owner}:${sha}`;
  if (!(await readAncestry(github, receipt.repository, receipt.base, head))) {
    log.debug("creation.code.ancestry_mismatch");
    return null;
  }
  return sha;
}

async function capturePullRequestDelivery(result, message, expected, github) {
  if (!currentClaimHandle() || !expected.prepared) return result;
  const receipt = { ...claimIdentity(currentClaimHandle()), type: "create_pull_request", message: canonical({ ...message, repo: expected.repository }), ...structuredClone(expected), ...expected.prepared };
  delete receipt.prepared;
  try {
    const commit = await readCode(github, receipt);
    const repository = await resolveRepositoryTarget(github, { repository: receipt.repository });
    const headRepository = await resolveRepositoryTarget(github, { repository: receipt.headRepository });
    if (commit && repository.repository_id && headRepository.repository_id) receipts.set(result, { ...receipt, commit, repositoryId: repository.repository_id, headRepositoryId: headRepository.repository_id });
    else log.debug("creation.capture.receipt_missing", { code_verified: !!commit, repository_verified: !!repository.repository_id, head_repository_verified: !!headRepository.repository_id });
  } catch (error) {
    log.failure("creation.capture.unverified", error);
    // An unavailable readback must leave the successful write unverified, not create a fallback resource.
  }
  return result;
}

async function verifyDiscussion(options, receipt) {
  const { github, claim } = options;
  // Each older discussion can produce both a comment receipt and a close receipt.
  if (receipt.cleanup !== undefined && (!Array.isArray(receipt.cleanup) || receipt.cleanup.length > 2 * MAX_CLOSE_COUNT)) return { verified: false };
  if (
    (receipt.cleanup || []).some(
      cleanup =>
        !cleanup ||
        typeof cleanup !== "object" ||
        Array.isArray(cleanup) ||
        !["comment", "close"].includes(cleanup.type) ||
        [cleanup.id, cleanup.discussionId].some(id => typeof id !== "string" || !id || Buffer.byteLength(id) > 256 || /[\x00-\x1f\x7f]/.test(id)) ||
        typeof cleanup.effectId !== "string" ||
        !/^[a-f0-9]{64}$/.test(cleanup.effectId)
    )
  ) {
    log.diagnostic("discussion.cleanup.readback.mismatch", { receipt_valid: false });
    return { verified: false };
  }
  const response = await readQueueMetadata(
    () =>
      github.graphql(
        `query ClaimDiscussionDelivery($id: ID!) {
      node(id: $id) { __typename id ... on Discussion { number repository { id databaseId nameWithOwner } } }
    }`,
        { id: receipt.id }
      ),
    log,
    "discussion.read"
  );
  const node = response?.node;
  if (
    typeof receipt.id !== "string" ||
    !receipt.id ||
    Buffer.byteLength(receipt.id) > 256 ||
    !Number.isSafeInteger(receipt.number) ||
    receipt.number < 1 ||
    node?.__typename !== "Discussion" ||
    node.id !== receipt.id ||
    node.number !== receipt.number ||
    node.repository?.id !== receipt.repositoryNodeId ||
    node.repository.nameWithOwner !== receipt.repository
  ) {
    log.debug("discussion.readback.mismatch", {
      discussion_present: node?.__typename === "Discussion",
      identity_matches: node?.id === receipt.id && node?.number === receipt.number,
      repository_matches: node?.repository?.id === receipt.repositoryNodeId && node?.repository?.nameWithOwner === receipt.repository,
    });
    return { verified: false };
  }
  const authority = await resolveRepositoryTarget(github, { repository: receipt.repository, repository_id: nativeDecimalIdentity(node.repository.databaseId), number: node.number });
  const authorities = [authority];
  const authorityKeys = new Set([canonical(authority)]);
  const effects = receipt.effectIds.map(id => ({ kind: "graphql", repository: receipt.repository, id }));
  for (const cleanup of receipt.cleanup || []) {
    const comment = cleanup.type === "comment";
    const { node: resource } = await readQueueMetadata(
      () =>
        github.graphql(
          `query ClaimDiscussionCleanup($id: ID!) {
          node(id: $id) {
            __typename id
            ... on Discussion { number repository { id databaseId nameWithOwner } }
            ... on DiscussionComment { discussion { id number repository { id databaseId nameWithOwner } } }
          }
        }`,
          { id: cleanup.id }
        ),
      log,
      "discussion.cleanup.read"
    );
    const discussion = comment ? resource?.discussion : resource;
    if (
      resource?.__typename !== (comment ? "DiscussionComment" : "Discussion") ||
      resource.id !== cleanup.id ||
      discussion?.id !== cleanup.discussionId ||
      discussion.id === receipt.id ||
      discussion.repository?.id !== receipt.repositoryNodeId ||
      discussion.repository.nameWithOwner !== receipt.repository ||
      nativeDecimalIdentity(discussion.repository.databaseId) !== authority.repository_id
    ) {
      // Cleanup failures must remain visible without DEBUG when creation itself succeeded.
      log.diagnostic("discussion.cleanup.readback.mismatch", { comment, resource_present: !!resource });
      return { verified: false };
    }
    const target = canonicalResourceTarget({ ...authority, number: discussion.number });
    const key = canonical(target);
    if (!authorityKeys.has(key)) {
      authorityKeys.add(key);
      authorities.push(target);
    }
    effects.push({ kind: "graphql", repository: receipt.repository, id: cleanup.effectId });
  }
  return createClaimResourceVerification({
    verified: true,
    claim_handle: claim.handle,
    resource: { kind: "discussion", repository: receipt.repository, id: node.id, number: node.number, url: receipt.url },
    authority_resource: authority,
    authority_resources: authorities,
    effect_resources: effects,
    evidence: { source: "independent_native_discussion_readback", id: node.id, digest: digest(node) },
  });
}

async function verifyPullRequest(options, receipt) {
  const { github, claim } = options;
  if (!Number.isSafeInteger(receipt.number) || receipt.number < 1 || nativeDecimalIdentity(receipt.id) !== receipt.id) return { verified: false };
  const message = options.message;
  if ((message.head_repo !== undefined && message.head_repo !== receipt.headRepository) || (message.base_branch !== undefined && message.base_branch !== receipt.transportBaseBranch)) return { verified: false };
  if (message.base_commit !== undefined) {
    if (typeof message.base_commit !== "string" || !REVISION.test(message.base_commit)) return { verified: false };
    if (message.base_commit !== receipt.transportBase && !(await readAncestry(github, receipt.repository, message.base_commit, receipt.transportBase))) return { verified: false };
  }
  const [owner, repo] = receipt.repository.split("/");
  const { data } = await github.rest.pulls.get({ owner, repo, pull_number: receipt.number });
  if (!data || !nativeIdentityMatches(data.id, receipt.id) || data.number !== receipt.number || data.base?.repo?.full_name !== receipt.repository || !nativeIdentityMatches(data.base.repo.id, receipt.repositoryId)) {
    log.debug("pull_request.readback.mismatch", {
      resource_present: !!data,
      identity_matches: !!data && nativeIdentityMatches(data.id, receipt.id) && data.number === receipt.number,
      repository_id_matches: nativeIdentityMatches(data?.base?.repo?.id, receipt.repositoryId),
    });
    return { verified: false };
  }
  const authority = await resolveParentResourceTarget(github, { repository: receipt.repository, kind: "pull_request", number: receipt.number }, data);
  const head = await resolveRepositoryTarget(github, { repository: receipt.headRepository, repository_id: receipt.headRepositoryId, ref: `refs/heads/${receipt.branch}` });
  const effects = [];
  for (const effect of options.effects || []) {
    if (!receiptMatchesClaim(effect, claim) || effect.outcome !== "succeeded") continue;
    if (effect.repository === receipt.headRepository && effect.kind === "git_ref") {
      if (
        typeof effect.expected?.ref !== "string" ||
        effect.expected.ref.replace(/^refs\//, "") !== `heads/${receipt.branch}` ||
        !REVISION.test(effect.id) ||
        effect.expected.sha !== effect.id ||
        (effect.id !== receipt.commit && !(await readAncestry(github, receipt.headRepository, effect.id, receipt.commit)))
      )
        return { verified: false };
      effects.push({ kind: "git_ref", repository: receipt.headRepository, id: effect.id });
    }
    if (effect.repository === receipt.headRepository && effect.kind === "graphql" && effect.commit) {
      if (!REVISION.test(effect.commit) || (effect.commit !== receipt.commit && !(await readAncestry(github, receipt.headRepository, effect.commit, receipt.commit)))) return { verified: false };
      effects.push({ kind: "graphql", repository: receipt.headRepository, id: effect.id });
    }
    if (effect.repository === receipt.repository && effect.kind === "issue" && effect.number === receipt.number) {
      if (!effect.expected || !Object.keys(effect.expected).length || Object.keys(effect.expected).some(field => !["labels", "assignees"].includes(field))) continue;
      const { data: issue } = await github.rest.issues.get({ owner, repo, issue_number: receipt.number });
      const parent = await resolveParentResourceTarget(github, { repository: receipt.repository, kind: "issue", number: receipt.number }, issue);
      if (parent.kind !== "pull_request" || parent.resource_id !== receipt.id || (effect.id && effect.id !== String(issue.id))) return { verified: false };
      effects.push({ kind: "issue", repository: receipt.repository, id: nativeDecimalIdentity(issue.id), number: receipt.number });
    }
  }
  return createClaimResourceVerification({
    verified: true,
    claim_handle: claim.handle,
    resource: { kind: "pull_request", repository: receipt.repository, id: receipt.id, number: receipt.number, url: receipt.url },
    authority_resource: authority,
    authority_resources: [authority, head],
    effect_resources: effects,
    evidence: { source: "independent_native_pull_request_readback", commit: receipt.commit, tree: receipt.tree, base: receipt.base, digest: digest(data) },
  });
}

/** @returns {Promise<null | {verified: false} | {verified: true, authorities: Record<string, string>[], effects: {kind: string, repository: string, id: string, number?: number}[]}>} */
async function verifyIssueGroupDelivery(options) {
  if (!currentClaimHandle()) return null;
  const receipt = options.result && receipts.get(options.result);
  if (!receipt || receipt.type !== "create_issue") return null;
  if (!receiptMatchesClaim(receipt, options.claim) || receipt.repository !== options.repository) return { verified: false };

  const validNumber = number => Number.isSafeInteger(number) && number >= 1;
  const effectsValid = Array.isArray(receipt.effectIds) && receipt.effectIds.length <= 16 && receipt.effectIds.every(id => typeof id === "string" && /^[a-f0-9]{64}$/.test(id));
  const commentsValid =
    Array.isArray(receipt.comments) &&
    receipt.comments.length <= 16 &&
    receipt.comments.every(entry => entry && typeof entry === "object" && !Array.isArray(entry) && typeof entry.id === "string" && /^[1-9][0-9]{0,255}$/.test(entry.id) && validNumber(entry.number));
  const parentValid = receipt.parent === null || (receipt.parent && typeof receipt.parent === "object" && !Array.isArray(receipt.parent) && validNumber(receipt.parent.number));
  const parentPresent = receipt.parent !== null || (effectsValid && commentsValid && receipt.effectIds.length === 0 && receipt.comments.length === 0);
  if (!effectsValid || !commentsValid || !parentValid || !parentPresent) {
    log.diagnostic("issue.group.receipt.invalid", { effects_valid: effectsValid, comments_valid: commentsValid, parent_valid: !!parentValid, parent_present: parentPresent });
    return { verified: false };
  }
  if (!receipt.parent) return { verified: true, authorities: [], effects: [] };

  const { github, repository } = options;
  const [owner, repo] = repository.split("/");
  try {
    const { data } = await github.rest.issues.get({ owner, repo, issue_number: receipt.parent.number });
    if (!data || data.number !== receipt.parent.number || data.pull_request || !nativeIdentityMatches(data.id, String(data.id))) {
      log.diagnostic("issue.group.parent.readback.mismatch", { resource_present: !!data });
      return { verified: false };
    }
    const authority = await resolveParentResourceTarget(github, { repository, kind: "issue", number: receipt.parent.number }, data);
    const effects = [];
    effects.push({ kind: "issue", repository, id: nativeDecimalIdentity(data.id), number: receipt.parent.number });
    for (const id of receipt.effectIds) effects.push({ kind: "graphql", repository, id });
    const api = (process.env.GITHUB_API_URL || "https://api.github.com").replace(/\/$/, "");
    for (const entry of receipt.comments) {
      const { data: comment } = await github.rest.issues.getComment({ owner, repo, comment_id: Number(entry.id) });
      if (String(comment?.id) !== entry.id || comment.issue_url !== `${api}/repos/${repository}/issues/${receipt.parent.number}`) return { verified: false };
      effects.push({ kind: "comment", repository, id: entry.id, number: receipt.parent.number });
    }
    return { verified: true, authorities: [authority], effects };
  } catch (error) {
    log.failure("issue.group.readback.unverified", error);
    return { verified: false };
  }
}

async function verifyNativeCreationDelivery(options) {
  const receipt = options.result && receipts.get(options.result);
  if (
    !receiptMatchesClaim(receipt, options.claim) ||
    receipt.type !== options.message.type ||
    (options.message.repo !== undefined && options.message.repo !== receipt.repository) ||
    receipt.message !== canonical({ ...options.message, repo: receipt.repository })
  ) {
    log.debug("creation.receipt.unverified", { receipt: !!receipt, claim_matches: receiptMatchesClaim(receipt, options.claim), type_matches: receipt?.type === options.message.type });
    return { verified: false };
  }
  const fields = new Set([
    "type",
    "output_id",
    "depends-on",
    "claim_handle",
    "claim_id",
    "work_id",
    "repo",
    "temporary_id",
    "title",
    "body",
    "labels",
    ...(receipt.type === "create_discussion" ? ["category"] : ["branch", "base", "draft", "head_repo", "base_branch", "base_commit"]),
  ]);
  if (Object.keys(options.message).some(field => !fields.has(field)) || options.result.staged || options.result.fallback_used) {
    log.debug("creation.receipt.unsupported", { staged: !!options.result.staged, fallback_used: !!options.result.fallback_used, undeclared_fields: Object.keys(options.message).some(field => !fields.has(field)) });
    return { verified: false };
  }
  try {
    const proof = receipt.type === "create_discussion" ? await verifyDiscussion(options, receipt) : await verifyPullRequest(options, receipt);
    log.debug("creation.readback.complete", { verified: proof.verified === true, discussion: receipt.type === "create_discussion" });
    return proof;
  } catch (error) {
    log.failure("creation.readback.unverified", error);
    return { verified: false };
  }
}

module.exports = { captureIssueDelivery, verifyIssueGroupDelivery, captureDiscussionDelivery, preparePullRequestDelivery, capturePullRequestDelivery, verifyNativeCreationDelivery };
