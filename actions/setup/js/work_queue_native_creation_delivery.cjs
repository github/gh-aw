// @ts-check
"use strict";
// @safe-outputs-exempt SEC-005 — creation receipts are private, Claim-bound records; delivery performs independently scoped native readback only.

const { canonical, digest } = require("./work_queue_codec.cjs");
const { currentClaimHandle, claimIdentity, receiptMatchesClaim, claimDeliveryReceipt, durableReceipt, createClaimResourceVerification } = require("./work_queue_claim_scope.cjs");
const { canonicalResourceTarget, resolveRepositoryTarget, resolveParentResourceTarget, nativeDecimalIdentity } = require("./work_queue_effect_resource.cjs");
const log = require("./work_queue_logging.cjs").createWorkQueueLogger("native_delivery");
const { readQueueMetadata } = require("./work_queue_read_retry.cjs");
const { MAX_CLOSE_COUNT } = require("./constants.cjs");

const receipts = new WeakMap();
const REVISION = /^(?:[a-f0-9]{40}|[a-f0-9]{64})$/;
const names = values => [...new Set(values.map(value => String(typeof value === "string" ? value : value.name || value.login).toLowerCase()))].sort();

function nativeIdentityMatches(value, expected) {
  const identity = typeof value === "number" && Number.isSafeInteger(value) ? String(value) : value;
  return typeof identity === "string" && /^[1-9][0-9]{0,255}$/.test(identity) && nativeDecimalIdentity(value) === expected;
}

function diagnosticNamesMatch(actual, expected) {
  const valid = values => Array.isArray(values) && values.every(value => typeof value === "string" || (value !== null && typeof value === "object" && typeof (value.name || value.login) === "string"));
  return valid(actual) && valid(expected) && canonical(names(actual)) === canonical(names(expected));
}

function captureDiscussionDelivery(result, message, expected) {
  if (currentClaimHandle()) {
    const { title, body, cleanup, ...metadata } = structuredClone(expected);
    const receipt = { ...claimIdentity(currentClaimHandle()), type: "create_discussion", ...metadata, message_digest: digest({ ...message, repo: expected.repository }), title_digest: digest(title), body_digest: digest(body) };
    if (cleanup !== undefined)
      receipt.cleanup = Array.isArray(cleanup)
        ? cleanup.map(entry => {
            if (!entry || typeof entry !== "object" || Array.isArray(entry)) return entry;
            const { body: cleanupBody, ...fields } = entry;
            return { ...fields, ...(typeof cleanupBody === "string" ? { body_digest: digest(cleanupBody) } : {}) };
          })
        : cleanup;
    if (nativeMessageMatches(receipt, result, message, { handle: receipt.claim_handle, claim_id: receipt.claim_id, work_id: receipt.work_id })) receipts.set(result, receipt);
  }
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
    const fields = { ref_matches: ref?.ref === `refs/heads/${receipt.branch}`, commit_type: ref?.object?.type === "commit", pinned_commit_matches: pinnedCommit === undefined || sha === pinnedCommit };
    log.debug("creation.code.ref_mismatch", fields);
    return { sha: null, diagnostics: { event: "creation.code.ref_mismatch", ...fields } };
  }
  const { data: commit } = await github.rest.git.getCommit({ owner, repo, commit_sha: sha });
  if (commit?.sha !== sha || commit.tree?.sha !== receipt.tree || !Array.isArray(commit.parents) || !commit.parents.length || commit.parents.some(parent => !REVISION.test(parent.sha))) {
    const fields = { commit_matches: commit?.sha === sha, tree_matches: commit?.tree?.sha === receipt.tree, parents_present: Array.isArray(commit?.parents) && commit.parents.length > 0 };
    log.debug("creation.code.commit_mismatch", fields);
    return { sha: null, diagnostics: { event: "creation.code.commit_mismatch", ...fields } };
  }
  const { data: tree } = await github.rest.git.getTree({ owner, repo, tree_sha: receipt.tree });
  if (tree?.sha !== receipt.tree || tree.truncated !== false || !Array.isArray(tree.tree)) {
    const fields = { tree_matches: tree?.sha === receipt.tree, complete: tree?.truncated === false, entries_present: Array.isArray(tree?.tree) };
    log.debug("creation.code.tree_invalid", fields);
    return { sha: null, diagnostics: { event: "creation.code.tree_invalid", ...fields } };
  }
  // Cross-repository PR comparisons are read from the base repository using a qualified head.
  const head = receipt.headRepository === receipt.repository ? sha : `${owner}:${sha}`;
  if (!(await readAncestry(github, receipt.repository, receipt.base, head))) {
    log.debug("creation.code.ancestry_mismatch");
    return { sha: null, diagnostics: { event: "creation.code.ancestry_mismatch" } };
  }
  return { sha };
}

async function capturePullRequestDelivery(result, message, expected, github) {
  if (!currentClaimHandle() || !expected.prepared) return result;
  const { title, body, prepared, ...metadata } = structuredClone(expected);
  const transport = Object.fromEntries(["head_repo", "base_branch", "base_commit"].filter(key => message[key] !== undefined).map(key => [key, message[key]]));
  const receipt = {
    ...claimIdentity(currentClaimHandle()),
    type: "create_pull_request",
    ...metadata,
    ...prepared,
    message_digest: digest({ ...message, repo: expected.repository }),
    title_digest: digest(title),
    body_digest: digest(body),
    transport,
  };
  if (!nativeMessageMatches(receipt, result, message, { handle: receipt.claim_handle, claim_id: receipt.claim_id, work_id: receipt.work_id })) return result;
  try {
    const code = await readCode(github, receipt);
    const repository = await resolveRepositoryTarget(github, { repository: receipt.repository });
    const headRepository = await resolveRepositoryTarget(github, { repository: receipt.headRepository });
    if (code.sha && repository.repository_id && headRepository.repository_id) receipts.set(result, { ...receipt, commit: code.sha, repositoryId: repository.repository_id, headRepositoryId: headRepository.repository_id });
    else log.debug("creation.capture.receipt_missing", { code_verified: !!code.sha, repository_verified: !!repository.repository_id, head_repository_verified: !!headRepository.repository_id });
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
        !/^[a-f0-9]{64}$/.test(cleanup.effectId) ||
        (cleanup.type === "comment" && (typeof cleanup.body_digest !== "string" || !/^[a-f0-9]{64}$/.test(cleanup.body_digest)))
    )
  ) {
    const fields = { receipt_valid: false };
    log.diagnostic("discussion.cleanup.readback.mismatch", fields);
    return { verified: false, diagnostics: { event: "discussion.cleanup.readback.mismatch", ...fields } };
  }
  const response = await readQueueMetadata(
    () =>
      github.graphql(
        `query ClaimDiscussionDelivery($id: ID!) {
      node(id: $id) {
        __typename id
        ... on Discussion {
          number title body url
          repository { id databaseId nameWithOwner }
          category { id }
          labels(first: 100) { nodes { name } pageInfo { hasNextPage } }
        }
      }
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
    node.url !== receipt.url ||
    node.repository?.id !== receipt.repositoryNodeId ||
    node.repository.nameWithOwner !== receipt.repository ||
    node.category?.id !== receipt.category ||
    digest(node.title) !== receipt.title_digest ||
    digest(node.body) !== receipt.body_digest ||
    node.labels?.pageInfo?.hasNextPage !== false ||
    !Array.isArray(node.labels.nodes) ||
    canonical(names(node.labels.nodes)) !== canonical(names(receipt.labels))
  ) {
    const fields = {
      discussion_present: node?.__typename === "Discussion",
      identity_matches: node?.id === receipt.id && node?.number === receipt.number,
      repository_matches: node?.repository?.id === receipt.repositoryNodeId && node?.repository?.nameWithOwner === receipt.repository,
      category_matches: node?.category?.id === receipt.category,
      title_matches: typeof node?.title === "string" && digest(node.title) === receipt.title_digest,
      body_matches: typeof node?.body === "string" && digest(node.body) === receipt.body_digest,
      labels_complete: node?.labels?.pageInfo?.hasNextPage === false,
      labels_matches: diagnosticNamesMatch(node?.labels?.nodes, receipt.labels),
      url_matches: node?.url === receipt.url,
    };
    log.debug("discussion.readback.mismatch", fields);
    return { verified: false, diagnostics: { event: "discussion.readback.mismatch", ...fields } };
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
            ... on Discussion { number closed stateReason repository { id databaseId nameWithOwner } }
            ... on DiscussionComment { body discussion { id number repository { id databaseId nameWithOwner } } }
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
      nativeDecimalIdentity(discussion.repository.databaseId) !== authority.repository_id ||
      (comment ? digest(resource.body) !== cleanup.body_digest : discussion.closed !== true || discussion.stateReason !== "OUTDATED")
    ) {
      // Cleanup failures must remain visible without DEBUG when creation itself succeeded.
      const fields = { comment, resource_present: !!resource };
      log.diagnostic("discussion.cleanup.readback.mismatch", fields);
      return { verified: false, diagnostics: { event: "discussion.cleanup.readback.mismatch", ...fields } };
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
    resource: { kind: "discussion", repository: receipt.repository, id: node.id, number: node.number, url: node.url },
    authority_resource: authority,
    authority_resources: authorities,
    effect_resources: effects,
    evidence: { source: "independent_native_discussion_readback", id: node.id, category: node.category.id, digest: digest(node) },
  });
}

async function verifyPullRequest(options, receipt) {
  const { github, claim } = options;
  if (!Number.isSafeInteger(receipt.number) || receipt.number < 1 || nativeDecimalIdentity(receipt.id) !== receipt.id) return { verified: false, diagnostics: { event: "pull_request.receipt_identity_invalid" } };
  const message = receipt.transport;
  if ((message.head_repo !== undefined && message.head_repo !== receipt.headRepository) || (message.base_branch !== undefined && message.base_branch !== receipt.transportBaseBranch))
    return { verified: false, diagnostics: { event: "pull_request.transport_route_mismatch" } };
  if (message.base_commit !== undefined) {
    if (typeof message.base_commit !== "string" || !REVISION.test(message.base_commit)) return { verified: false, diagnostics: { event: "pull_request.base_commit_invalid" } };
    if (message.base_commit !== receipt.transportBase && !(await readAncestry(github, receipt.repository, message.base_commit, receipt.transportBase)))
      return { verified: false, diagnostics: { event: "pull_request.base_commit_not_ancestor" } };
  }
  const code = await readCode(github, receipt, receipt.commit);
  if (!code.sha) return { verified: false, diagnostics: code.diagnostics };
  const [owner, repo] = receipt.repository.split("/");
  const { data } = await github.rest.pulls.get({ owner, repo, pull_number: receipt.number });
  if (
    !data ||
    !nativeIdentityMatches(data.id, receipt.id) ||
    data.number !== receipt.number ||
    data.html_url !== receipt.url ||
    digest(data.title) !== receipt.title_digest ||
    digest(data.body) !== receipt.body_digest ||
    data.state !== "open" ||
    data.draft !== receipt.draft ||
    data.head?.sha !== receipt.commit ||
    data.head.ref !== receipt.branch ||
    data.head.repo?.full_name !== receipt.headRepository ||
    data.base?.ref !== receipt.baseBranch ||
    data.base.sha !== receipt.base ||
    data.base.repo?.full_name !== receipt.repository ||
    !nativeIdentityMatches(data.base.repo.id, receipt.repositoryId) ||
    !nativeIdentityMatches(data.head.repo.id, receipt.headRepositoryId) ||
    canonical(names(data.labels || [])) !== canonical(names(receipt.labels)) ||
    canonical(names(data.assignees || [])) !== canonical(names(receipt.assignees))
  ) {
    const fields = {
      resource_present: !!data,
      identity_matches: !!data && nativeIdentityMatches(data.id, receipt.id) && data.number === receipt.number,
      title_matches: typeof data?.title === "string" && digest(data.title) === receipt.title_digest,
      body_matches: typeof data?.body === "string" && digest(data.body) === receipt.body_digest,
      open: data?.state === "open",
      draft_matches: data?.draft === receipt.draft,
      head_matches: data?.head?.sha === receipt.commit && data?.head?.ref === receipt.branch && data?.head?.repo?.full_name === receipt.headRepository,
      base_matches: data?.base?.sha === receipt.base && data?.base?.ref === receipt.baseBranch && data?.base?.repo?.full_name === receipt.repository,
      labels_matches: diagnosticNamesMatch(data?.labels || [], receipt.labels),
      assignees_matches: diagnosticNamesMatch(data?.assignees || [], receipt.assignees),
      url_matches: data?.html_url === receipt.url,
      repository_id_matches: nativeIdentityMatches(data?.base?.repo?.id, receipt.repositoryId),
      head_repository_id_matches: nativeIdentityMatches(data?.head?.repo?.id, receipt.headRepositoryId),
    };
    log.debug("pull_request.readback.mismatch", fields);
    return { verified: false, diagnostics: { event: "pull_request.readback.mismatch", ...fields } };
  }
  const authority = await resolveParentResourceTarget(github, { repository: receipt.repository, kind: "pull_request", number: receipt.number }, data);
  const head = await resolveRepositoryTarget(github, { repository: receipt.headRepository, repository_id: nativeDecimalIdentity(data.head.repo.id), ref: `refs/heads/${receipt.branch}` });
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
        return { verified: false, diagnostics: { event: "pull_request.effect_ref_mismatch" } };
      effects.push({ kind: "git_ref", repository: receipt.headRepository, id: effect.id });
    }
    if (effect.repository === receipt.headRepository && effect.kind === "graphql" && effect.commit) {
      if (!REVISION.test(effect.commit) || (effect.commit !== receipt.commit && !(await readAncestry(github, receipt.headRepository, effect.commit, receipt.commit))))
        return { verified: false, diagnostics: { event: "pull_request.effect_graphql_commit_mismatch" } };
      effects.push({ kind: "graphql", repository: receipt.headRepository, id: effect.id });
    }
    if (effect.repository === receipt.repository && effect.kind === "issue" && effect.number === receipt.number) {
      if (!effect.expected || !Object.keys(effect.expected).length || Object.keys(effect.expected).some(field => !["labels", "assignees"].includes(field))) continue;
      for (const field of Object.keys(effect.expected)) if (canonical(names(effect.expected[field])) !== canonical(names(receipt[field]))) return { verified: false, diagnostics: { event: "pull_request.effect_issue_expected_mismatch" } };
      const { data: issue } = await github.rest.issues.get({ owner, repo, issue_number: receipt.number });
      const parent = await resolveParentResourceTarget(github, { repository: receipt.repository, kind: "issue", number: receipt.number }, issue);
      if (parent.kind !== "pull_request" || parent.resource_id !== receipt.id || (effect.id && effect.id !== String(issue.id))) return { verified: false, diagnostics: { event: "pull_request.effect_parent_mismatch" } };
      for (const field of Object.keys(effect.expected)) if (canonical(names(issue[field] || [])) !== canonical(names(receipt[field]))) return { verified: false, diagnostics: { event: "pull_request.effect_issue_fields_mismatch" } };
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

function nativeMessageMatches(receipt, result, message, claim) {
  if (!receiptMatchesClaim(receipt, claim) || !message || receipt.type !== message.type || (message.repo !== undefined && message.repo !== receipt.repository) || receipt.message_digest !== digest({ ...message, repo: receipt.repository }))
    return false;
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
  return !Object.keys(message).some(field => !fields.has(field)) && !result.staged && !result.fallback_used;
}

function validNativeReceipt(receipt) {
  const text = value => typeof value === "string" && value.length > 0;
  const hash = value => typeof value === "string" && /^[a-f0-9]{64}$/.test(value);
  const list = value => Array.isArray(value) && value.every(text);
  if (
    !receipt ||
    !["create_discussion", "create_pull_request"].includes(receipt.type) ||
    ![receipt.repository, receipt.id, receipt.url].every(text) ||
    !Number.isSafeInteger(receipt.number) ||
    receipt.number < 1 ||
    ![receipt.message_digest, receipt.title_digest, receipt.body_digest].every(hash) ||
    !list(receipt.labels)
  )
    return false;
  if (receipt.type === "create_discussion") return text(receipt.repositoryNodeId) && text(receipt.category) && list(receipt.effectIds) && receipt.effectIds.length > 0;
  return (
    [receipt.headRepository, receipt.branch, receipt.baseBranch, receipt.transportBaseBranch, receipt.repositoryId, receipt.headRepositoryId].every(text) &&
    [receipt.tree, receipt.base, receipt.transportBase, receipt.commit].every(value => typeof value === "string" && REVISION.test(value)) &&
    typeof receipt.draft === "boolean" &&
    list(receipt.assignees) &&
    receipt.transport !== null &&
    typeof receipt.transport === "object" &&
    !Array.isArray(receipt.transport) &&
    Object.keys(receipt.transport).every(key => ["head_repo", "base_branch", "base_commit"].includes(key) && text(receipt.transport[key]))
  );
}

function durableNativeCreationReceipt(result, message, claim) {
  const receipt = result && receipts.get(result);
  if (!nativeMessageMatches(receipt, result, message, claim) || !validNativeReceipt(receipt)) return null;
  return durableReceipt(receipt);
}

async function verifyNativeCreationDelivery(options) {
  const receipt = claimDeliveryReceipt(options, "native_creation", receipts);
  if (options.durable !== true && !nativeMessageMatches(receipt, options.result, options.message, options.claim)) {
    const fields = { receipt: !!receipt, claim_matches: receiptMatchesClaim(receipt, options.claim), type_matches: receipt?.type === options.message.type };
    log.debug("creation.receipt.unverified", fields);
    return { verified: false, diagnostics: { event: "creation.receipt.unverified", ...fields } };
  }
  return verifyNativeCreationReceipt(options, receipt);
}

async function verifyNativeCreationReceipt(options, receipt) {
  if (!receiptMatchesClaim(receipt, options.claim) || !validNativeReceipt(receipt)) {
    const fields = { receipt: !!receipt, claim_matches: receiptMatchesClaim(receipt, options.claim), receipt_valid: validNativeReceipt(receipt) };
    log.debug("creation.receipt.unverified", fields);
    return { verified: false, diagnostics: { event: "creation.receipt.unverified", ...fields } };
  }
  try {
    const proof = receipt.type === "create_discussion" ? await verifyDiscussion(options, receipt) : await verifyPullRequest(options, receipt);
    log.debug("creation.readback.complete", { verified: proof.verified === true, discussion: receipt.type === "create_discussion" });
    return proof;
  } catch (error) {
    log.failure("creation.readback.unverified", error);
    return { verified: false, diagnostics: { event: "creation.readback.unverified", thrown: true } };
  }
}

module.exports = { captureDiscussionDelivery, preparePullRequestDelivery, capturePullRequestDelivery, verifyNativeCreationDelivery, durableNativeCreationReceipt };
