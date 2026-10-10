// @ts-check
"use strict";
const { SAFE_OUTPUT_E001 } = require("./error_codes.cjs");
const { canonical, closed, digest, parseStrictJSON, queueError } = require("./work_queue_codec.cjs");
const { actorFromContext } = require("./work_queue_policy.cjs");
const { authenticatePublisher } = require("./work_queue_native.cjs");
const { validateStoredAssignment } = require("./work_queue_binding.cjs");
const { readInboundWorkQueueAssignment, resolveWorkQueueRuntime } = require("./aw_context.cjs");
const { assertProjectionAuthority, issueIdentity, issueCompletionPolicy } = require("./work_queue_issue_contract.cjs");
const { dependencyStatus } = require("./work_queue_graph.cjs");
const { appendCommit, newRequest, proposedCommitId, replayTransactions } = require("./work_queue_replay.cjs");
const { readCheckedQueue, writeCheckedFiles } = require("./work_queue_checked_transport.cjs");
const { withProjectionLocks, projectionNonce } = require("./work_queue_issue_coordination.cjs");
const { MAX_PROJECTION_TARGETS, LABELS_PAGE_SIZE, STATUSES, statusLabelName, ensureStatusLabel, discoverTarget, preflightIssues, mutateIssues, issueResource, ISSUE_SELECTION, issueReadQuery } = require("./work_queue_issue_api.cjs");
const { wrapGithubClient } = require("./work_queue_issue_pacing.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { validateJournal } = require("./work_queue_issue_journal.cjs");
const { issueBody, renderSummary, renderClaim, runLink } = require("./work_queue_issue_messages.cjs");

function issuesConfiguration(value) {
  if (value === undefined || value === false) return null;
  if (value === true) return { label: "work" };
  closed(value, [], ["label"], "work-queue issues");
  for (const [key, field] of Object.entries(value)) {
    if (typeof field !== "string" || !field.trim() || field.includes("${{") || Buffer.byteLength(field) > 33 || /[\x00-\x1f\x7f]/.test(field))
      throw queueError("projection_invalid", `${SAFE_OUTPUT_E001}: ${key} must be a nonblank bounded literal`);
  }
  return { label: "work", ...value };
}

function ownProjectionTargets(state, origin, ref, assignment) {
  const actor = { ...actorFromContext(origin), role: "projector" };
  const targets = new Map();
  for (const work of state.works.values()) {
    const admission = state.workCreators?.get(work.work_id);
    if (admission && ["principal", "repository", "workflow", "run_id", "run_attempt"].every(field => admission[field] === actor[field])) targets.set(work.work_id, { work_id: work.work_id, claim_ids: [] });
  }
  if (assignment) {
    const stored = validateStoredAssignment(state, assignment, { allowReleased: true }).assignment;
    actor.dispatch_id = stored.dispatch_id;
    for (const member of stored.claims) {
      const target = targets.get(member.work_id) || { work_id: member.work_id, claim_ids: [], authority_claim_id: member.claim_id };
      target.claim_ids.push(member.claim_id);
      targets.set(member.work_id, target);
    }
  }
  for (const target of targets.values()) {
    const work = state.works.get(target.work_id);
    if (work.backing_issue || work.issue_link) target.resource = work.backing_issue || work.issue_link;
    assertProjectionAuthority(state, actor, work.work_id, ref, target.resource?.repository || origin.repository, target.authority_claim_id);
    for (const claimId of target.claim_ids) assertProjectionAuthority(state, actor, work.work_id, ref, target.resource?.repository || origin.repository, claimId);
  }
  return { actor, targets: [...targets.values()] };
}

function workIssueStatus(state, work, at) {
  if (work.state === "cancelled") return "Cancelled";
  if (work.barrier === "failed") return "Needs attention";
  if (work.barrier === "verified") {
    const outputs = work.result?.outputs;
    return Array.isArray(outputs) && outputs.some(output => output.resource?.kind === "pull_request" || output.type === "create_pull_request") ? "Needs review" : "Done";
  }
  if (work.state === "completed") return "Verifying";
  if (work.state === "claimed") {
    const dispatch = state.dispatches.get(state.claims.get(work.claim_id)?.dispatch_id);
    return dispatch?.state === "bound" && dispatch.run?.run_attempt === 1 ? "Running" : "Assigned";
  }
  return dependencyStatus(state, work, at).ready && work.retry_not_before <= at ? "Queued" : "Blocked";
}

function jobDiagnostics(value) {
  const results = typeof value === "string" ? parseStrictJSON(value, { maxBytes: 1024 * 1024 }) : value;
  if (results === undefined) return [];
  if (!results || typeof results !== "object" || Array.isArray(results)) throw queueError("projection_invalid", "native job diagnostics must be an object");
  return Object.entries(results)
    .filter(([name, job]) => /^[A-Za-z_][A-Za-z0-9_-]*$/.test(name) && ["failure", "cancelled"].includes(job?.result))
    .map(([name, job]) => `Native job \`${name}\`: **${job.result}**. This does not establish Work cancellation or Result.`);
}

function summaryBody(state, work, at, diagnostics = [], branch = "work-queue", run = undefined) {
  const lines = [...diagnostics];
  const claimHistory = [];
  const outcomes = [];
  if (work.barrier === "failed") lines.push(`Delivery needs attention (${work.disposition}); Completion is not a verified Result.`);
  if (work.state === "cancelled") lines.push(`Cancellation: ${work.cancellation_reason}.`);
  for (const claim of state.claims.values()) {
    if (claim.work_id !== work.work_id) continue;
    const dispatch = state.dispatches.get(claim.dispatch_id);
    if (dispatch?.run) claimHistory.push(`- Claim \`${claim.claim_id}\`: [View attempt](${runLink(dispatch.run)})`);
  }
  if (work.result) {
    for (const output of work.result.outputs || []) {
      const resource = output.resource;
      const number = resource?.number;
      if (
        resource?.repository &&
        /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(resource.repository) &&
        ((typeof number === "string" && /^[1-9][0-9]{0,255}$/.test(number)) || (Number.isSafeInteger(number) && number > 0)) &&
        ["issue", "pull_request"].includes(resource.kind)
      )
        outcomes.push(`Verified ${resource.kind}: https://github.com/${resource.repository}/${resource.kind === "issue" ? "issues" : "pull"}/${resource.number}`);
    }
  }
  const origin = run || state.workCreators?.get(work.work_id);
  if (!origin) throw queueError("projection_unauthorized", "Work admission provenance is unavailable");
  return renderSummary(
    {
      work_id: work.work_id,
      ledger_url: `https://github.com/${state.repository}/blob/${branch}/work-queue.jsonl`,
      claim_history: claimHistory.join("\n") || "No authenticated worker run yet.",
      outcome_links: outcomes.join("\n"),
      diagnostics: lines.join("\n"),
    },
    origin,
    workIssueStatus(state, work, at)
  );
}

function claimBody(state, claim, at, diagnostics = []) {
  const work = state.works.get(claim.work_id);
  const dispatch = state.dispatches.get(claim.dispatch_id);
  const ownResult = work.claim_id === claim.claim_id;
  return renderClaim(
    {
      claim_id: claim.claim_id,
      claim_handle: claim.handle,
      run_url: runLink(dispatch.run),
      outcome: `${claim.state}${ownResult && claim.state === "completed" ? `; ${workIssueStatus(state, work, at)}` : ""}`,
      diagnostics: [...(claim.cancellation_reason ? [`Reason: ${claim.cancellation_reason}`] : []), ...diagnostics].join("\n"),
    },
    dispatch.run
  );
}

function journalPath(workId) {
  return `.gh-aw/issue-projection/${digest(workId)}.json`;
}

function bindingOperation(target, ref, resource) {
  return { kind: "IssueLink", work_id: target.work_id, resource, projector_ref: ref, ...(target.authority_claim_id ? { claim_id: target.authority_claim_id } : {}) };
}

function ambiguousKeys(batch, targets) {
  return targets
    .filter(target => batch.results.some(result => result.operation.key === target.work_id && result.uncertain))
    .flatMap(target => [`work:${target.work_id}`, ...(target.resource ? [`issue:${issueIdentity(target.resource)}`] : [])]);
}

async function publishProjection(options, current, actor, journals, operations) {
  const read = options.readCheckedQueue || readCheckedQueue;
  const write = options.writeCheckedFiles || writeCheckedFiles;
  let previous = current;
  for (let attempt = 0; attempt < 5; attempt++) {
    let transactions = previous.transactions;
    if (operations.length) {
      const request = newRequest(`issues:${digest({ actor, operations })}`, "issue_link", actor, { operations });
      if (!previous.state.requests.has(request.id)) {
        const candidate = { version: 3, id: proposedCommitId(previous.state, request), previous: previous.state.tip, request, actor, policy_epoch: previous.state.policy_epoch, at: options.now ?? Date.now(), operations };
        try {
          transactions = appendCommit(previous.transactions, candidate).transactions;
        } catch (error) {
          await publishProjection(options, previous, actor, journals, []);
          throw error;
        }
      }
    }
    const files = [...journals].map(([path, value]) => ({ path, content: canonical(value) + "\n" }));
    if (operations.length) files.push({ path: previous.logPath, content: transactions.map(commit => canonical(commit)).join("\n") + "\n" });
    try {
      const sha = await write({ ...options, branch: previous.branch, expectedHeadOid: previous.sha, files });
      return { ...previous, sha, transactions, state: operations.length ? replayTransactions(transactions) : previous.state, journal: new Map([...journals].map(([path, value]) => [path, structuredClone(value)])) };
    } catch (error) {
      const refreshed = await read({ ...options, branch: previous.branch, paths: [...journals.keys()] });
      if (previous.transactions.some((commit, index) => canonical(commit) !== canonical(refreshed.transactions[index]))) throw queueError("ledger_invalid", "projection publication saw rewritten history");
      const durable = [...journals].every(([path, value]) => canonical(refreshed.journal.get(path) ?? null) === canonical(value));
      if (durable && (!operations.length || refreshed.state.requests.has(`issues:${digest({ actor, operations })}`))) return refreshed;
      const conflict = error.errors?.some(item => /expectedHeadOid|head.*(?:changed|match)|expected.*head/i.test(item.message || "")) || error.status === 409;
      if (!conflict && error.status && error.status < 500) throw error;
      previous = refreshed;
    }
  }
  throw queueError("publication_unresolved", "projection binding publication remains pending");
}

async function projectBatch(options, initial, origin, assignment, config, targets) {
  const actor = { ...actorFromContext(origin), role: "projector", ...(assignment ? { dispatch_id: assignment.dispatch_id } : {}) };
  const paths = targets.map(target => journalPath(target.work_id));
  const read = options.readCheckedQueue || readCheckedQueue;
  const preflight = targets.map(target => {
    const work = initial.state.works.get(target.work_id);
    return {
      ...target,
      comments: {
        ...(work.issue_summary ? { summary: work.issue_summary } : {}),
        ...Object.fromEntries(target.claim_ids.filter(id => initial.state.claims.get(id).issue_comment).map(id => [id, initial.state.claims.get(id).issue_comment])),
      },
    };
  });
  const issueRead = issueReadQuery(preflight, config, origin.repository);
  let current = await read({ ...options, paths, issueRead });
  const owned = ownProjectionTargets(current.state, origin, origin.ref, assignment).targets;
  for (const target of targets) {
    const checked = owned.find(candidate => candidate.work_id === target.work_id);
    if (!checked || canonical(checked) !== canonical(target)) throw queueError("projection_scope_changed", "projection targets changed after coordination");
  }
  const journals = new Map();
  const pending = [];
  const discovery = new Map();
  const statusLabels = new Map();
  for (const target of targets) {
    const path = journalPath(target.work_id);
    const journal = current.journal.get(path) || { version: 1, work_id: target.work_id, comments: {} };
    const repository = target.resource?.repository || origin.repository;
    validateJournal(journal, current.state, target.work_id, repository);
    journals.set(path, structuredClone(journal));
    const work = current.state.works.get(target.work_id);
    if (!discovery.has(repository)) discovery.set(repository, await discoverTarget(options.githubClient, repository, config, current.nativeResponse?.[issueRead.repositoryAliases.get(repository)]));
    const status = workIssueStatus(current.state, work, options.now ?? Date.now());
    const statusKey = `${repository}:${status}`;
    if (!statusLabels.has(statusKey)) statusLabels.set(statusKey, await ensureStatusLabel(options.githubClient, discovery.get(repository).repositoryId, config, status));
    if (!target.resource && journal.create?.resource) {
      assertProjectionAuthority(current.state, journal.create.origin, target.work_id, journal.create.ref, journal.create.resource.repository, journal.create.authority_claim_id);
      target.resource = journal.create.resource;
    }
    target.comments = {};
    if (work.issue_summary) target.comments.summary = work.issue_summary;
    for (const claimId of target.claim_ids) {
      const claim = current.state.claims.get(claimId);
      if (claim.issue_comment) target.comments[claimId] = claim.issue_comment;
    }
    for (const [key, receipt] of Object.entries(journal.comments)) {
      if ((key === "summary" || target.claim_ids.includes(key)) && receipt.id) target.comments[key] ||= receipt.id;
    }
  }
  const samePreflight = targets.every((target, index) => canonical(target.resource || null) === canonical(preflight[index].resource || null) && canonical(target.comments) === canonical(preflight[index].comments));
  const issues = await preflightIssues(options.githubClient, targets, { response: samePreflight ? current.nativeResponse : undefined });
  for (const target of targets) {
    const receipt = journals.get(journalPath(target.work_id)).create;
    if (receipt?.node_id && receipt.node_id !== issues.get(target.work_id)?.id) throw queueError("projection_journal_invalid", "native Issue receipt node identity disagrees with preflight");
  }
  const operations = [];
  const creates = [];
  const existingResources = new Set(
    targets
      .filter(target => target.resource && initial.state.works.get(target.work_id) && (initial.state.works.get(target.work_id).backing_issue || initial.state.works.get(target.work_id).issue_link))
      .map(target => issueIdentity(target.resource))
  );
  for (const target of targets) {
    const journal = journals.get(journalPath(target.work_id));
    if (target.resource) {
      if (!current.state.works.get(target.work_id).backing_issue && !current.state.works.get(target.work_id).issue_link) operations.push(bindingOperation(target, origin.ref, target.resource));
      continue;
    }
    if (journal.create) {
      pending.push({ work_id: target.work_id, reason: "Issue creation is uncertain; no verified native receipt permits recreation" });
      continue;
    }
    const discovered = discovery.get(origin.repository);
    const work = current.state.works.get(target.work_id);
    const status = workIssueStatus(current.state, work, options.now ?? Date.now());
    journal.create = { nonce: projectionNonce(), origin: actor, ref: origin.ref, ...(target.authority_claim_id ? { authority_claim_id: target.authority_claim_id } : {}) };
    creates.push({
      key: target.work_id,
      name: "createIssue",
      type: "CreateIssueInput",
      input: {
        repositoryId: discovered.repositoryId,
        title: `Work ${work.work_id}`,
        body: issueBody(current.state, work, origin, config, current.branch),
        labelIds: [discovered.labelId, statusLabels.get(`${origin.repository}:${status}`)],
      },
      selection: `issue { ${ISSUE_SELECTION} }`,
    });
  }
  if (creates.length) {
    // Record intent before sending: an accepted native write followed by a crash
    // must never leave retryable state. Git and Issue writes are not atomic.
    current = await publishProjection(options, current, actor, journals, []);
    const batch = await mutateIssues(options.githubClient, creates);
    for (const result of batch.results) {
      const target = targets.find(target => target.work_id === result.operation.key);
      const journal = journals.get(journalPath(target.work_id));
      if (result.pending) {
        if (!result.uncertain) delete journal.create;
        pending.push({ work_id: target.work_id, reason: result.uncertain ? "Issue creation response is ambiguous; synchronization pending" : "Issue creation was rejected; queue committed, synchronization pending" });
        continue;
      }
      const issue = result.value.issue;
      let resource;
      try {
        resource = issueResource(issue);
      } catch {
        recordUncertainReceipt(result, batch, pending, "creation returned no lossless native identity");
        continue;
      }
      if (typeof issue.id !== "string" || !issue.id || resource.repository !== origin.repository || issue.repository.id !== discovery.get(origin.repository).repositoryId) {
        recordUncertainReceipt(result, batch, pending, "creation returned no scoped native Issue identity");
        continue;
      }
      target.resource = resource;
      journal.create.resource = resource;
      journal.create.node_id = issue.id;
      issues.set(target.work_id, { ...issue, comments: new Map() });
      const status = workIssueStatus(current.state, current.state.works.get(target.work_id), options.now ?? Date.now());
      if (issue.labels?.pageInfo?.hasNextPage !== false || !issue.labels.nodes?.some(label => label.id === statusLabels.get(`${resource.repository}:${status}`)))
        pending.push({ work_id: target.work_id, reason: "queue committed; initial status label application was not verified" });
      operations.push(bindingOperation(target, origin.ref, resource));
    }
    current = await publishProjection(options, current, actor, journals, operations);
    if (batch.ambiguous) return { pending, ambiguous: true, retain_keys: ambiguousKeys(batch, targets) };
  } else if (operations.length) current = await publishProjection(options, current, actor, journals, operations);

  const resources = targets.filter(target => target.resource && !existingResources.has(issueIdentity(target.resource))).map(target => `issue:${issueIdentity(target.resource)}`);
  const project = async () => {
    if (resources.length) {
      current = await read({ ...options, paths });
      for (const target of targets) {
        if (!target.resource) continue;
        const work = current.state.works.get(target.work_id);
        if (canonical(work.backing_issue || work.issue_link || null) !== canonical(target.resource)) throw queueError("projection_scope_changed", "new native receipt has no checked backing binding");
      }
    }
    return projectLinkedIssues(options, current, actor, origin, config, targets, journals, issues, discovery, statusLabels, pending, creates.length > 0);
  };
  return resources.length ? (options.withProjectionLocks || withProjectionLocks)({ ...options, repositoryId: current.repositoryId, head: current.sha, keys: resources }, project) : project();
}

async function projectLinkedIssues(options, current, actor, origin, config, targets, journals, issues, discovery, statusLabels, pending, created) {
  const edits = [];
  const newComments = [];
  const bindings = [];
  for (const target of targets) {
    const issue = issues.get(target.work_id);
    if (!issue) continue;
    const work = current.state.works.get(target.work_id);
    const journal = journals.get(journalPath(target.work_id));
    assertProjectionAuthority(current.state, actor, work.work_id, origin.ref, target.resource.repository, target.authority_claim_id);
    const discovered = discovery.get(target.resource.repository);
    const status = workIssueStatus(current.state, work, options.now ?? Date.now());
    const desiredStatusId = statusLabels.get(`${target.resource.repository}:${status}`);
    const missing = [discovered.labelId, desiredStatusId].filter(id => !issue.labels.nodes.some(label => label.id === id));
    if (missing.length)
      edits.push({
        key: target.work_id,
        name: "addLabelsToLabelable",
        type: "AddLabelsToLabelableInput",
        input: { labelableId: issue.id, labelIds: missing },
        selection: `labelable { id ... on Issue { labels(first:${LABELS_PAGE_SIZE}) { nodes { id } pageInfo { hasNextPage } } } }`,
        expectedLabels: missing,
      });
    const obsolete = issue.labels.nodes.filter(label => STATUSES.some(candidate => candidate !== status && label.name === statusLabelName(config, candidate))).map(label => label.id);
    if (obsolete.length)
      edits.push({
        key: target.work_id,
        name: "removeLabelsFromLabelable",
        type: "RemoveLabelsFromLabelableInput",
        input: { labelableId: issue.id, labelIds: obsolete },
        selection: `labelable { id ... on Issue { labels(first:${LABELS_PAGE_SIZE}) { nodes { id } pageInfo { hasNextPage } } } }`,
        removedLabels: obsolete,
      });
    const diagnostics = jobDiagnostics(options.jobResults ?? process.env.GH_AW_WORK_QUEUE_JOB_RESULTS);
    const bodies = new Map([["summary", summaryBody(current.state, work, options.now ?? Date.now(), diagnostics, current.branch, origin)]]);
    for (const claimId of target.claim_ids) bodies.set(claimId, claimBody(current.state, current.state.claims.get(claimId), options.now ?? Date.now(), diagnostics));
    for (const [key, body] of bodies) {
      const comment = issue.comments.get(key);
      if (comment) {
        if (comment.body !== body) edits.push({ key: target.work_id, name: "updateIssueComment", type: "UpdateIssueCommentInput", input: { id: comment.id, body }, selection: "issueComment { id body }" });
        const handle = key === "summary" ? work.issue_summary : current.state.claims.get(key).issue_comment;
        if (!handle)
          bindings.push({
            kind: "IssueComment",
            work_id: target.work_id,
            comment_id: comment.id,
            projector_ref: origin.ref,
            ...(key !== "summary" ? { claim_id: key } : {}),
            ...(target.authority_claim_id ? { authority_claim_id: key === "summary" ? target.authority_claim_id : key } : {}),
          });
      } else if (journal.comments[key]) {
        pending.push({ work_id: target.work_id, reason: "Comment creation is uncertain; no verified native receipt permits recreation" });
      } else {
        journal.comments[key] = {
          nonce: projectionNonce(),
          origin: actor,
          ref: origin.ref,
          ...(target.authority_claim_id ? { authority_claim_id: key === "summary" ? target.authority_claim_id : key } : {}),
        };
        newComments.push({ key: target.work_id, handle: key, name: "addComment", type: "AddCommentInput", input: { subjectId: issue.id, body }, selection: "commentEdge { node { id body ... on IssueComment { issue { id } } } }" });
      }
    }
    if (status === "Done" && issueCompletionPolicy(current.state, work, actor, origin.ref, target.resource.repository) === "close-on-result" && issue.state !== "CLOSED")
      edits.push({ key: target.work_id, name: "closeIssue", type: "CloseIssueInput", input: { issueId: issue.id, stateReason: "COMPLETED" }, selection: "issue { id state }" });
  }
  if (newComments.length) current = await publishProjection(options, current, actor, journals, []);
  if (created && (edits.length || newComments.length)) await (options.sleep || (delay => new Promise(resolve => setTimeout(resolve, delay))))(1000);
  const batch = await mutateIssues(options.githubClient, [...edits, ...newComments], { sleep: options.sleep });
  for (const result of batch.results) {
    if (result.pending) {
      pending.push({ work_id: result.operation.key, reason: "queue committed; Issue synchronization pending" });
      if (!result.uncertain && result.operation.handle) delete journals.get(journalPath(result.operation.key)).comments[result.operation.handle];
      continue;
    }
    if (result.operation.name === "updateIssueComment" && (result.value.issueComment?.id !== result.operation.input.id || result.value.issueComment?.body !== result.operation.input.body)) {
      recordUncertainReceipt(result, batch, pending, "comment update returned no exact native receipt");
      continue;
    }
    if (result.operation.name === "closeIssue" && (result.value.issue?.id !== result.operation.input.issueId || result.value.issue?.state !== "CLOSED")) {
      recordUncertainReceipt(result, batch, pending, "Issue closure returned no exact native receipt");
      continue;
    }
    if (
      (result.operation.expectedLabels || result.operation.removedLabels) &&
      (result.value.labelable?.id !== result.operation.input.labelableId ||
        result.value.labelable?.labels?.pageInfo?.hasNextPage !== false ||
        result.operation.expectedLabels?.some(id => !result.value.labelable.labels.nodes.some(label => label.id === id)) ||
        result.operation.removedLabels?.some(id => result.value.labelable.labels.nodes.some(label => label.id === id)))
    )
      pending.push({ work_id: result.operation.key, reason: "queue committed; status label synchronization was not verified" });
    if (result.operation.handle) {
      const node = result.value.commentEdge?.node;
      const target = targets.find(target => target.work_id === result.operation.key);
      if (!node?.id || node.issue?.id !== issues.get(target.work_id).id || node.body !== result.operation.input.body) {
        recordUncertainReceipt(result, batch, pending, "comment creation returned no exact native receipt");
        continue;
      }
      journals.get(journalPath(target.work_id)).comments[result.operation.handle].id = node.id;
      bindings.push({
        kind: "IssueComment",
        work_id: target.work_id,
        comment_id: node.id,
        projector_ref: origin.ref,
        ...(result.operation.handle !== "summary" ? { claim_id: result.operation.handle } : {}),
        ...(target.authority_claim_id ? { authority_claim_id: result.operation.handle === "summary" ? target.authority_claim_id : result.operation.handle } : {}),
      });
    }
  }
  if (newComments.length || bindings.length) await publishProjection(options, current, actor, journals, bindings);
  return { pending, ambiguous: batch.ambiguous, retain_keys: ambiguousKeys(batch, targets) };
}

function recordUncertainReceipt(result, batch, pending, reason) {
  result.pending = true;
  result.uncertain = true;
  batch.ambiguous = true;
  pending.push({ work_id: result.operation.key, reason: `queue committed; ${reason}; synchronization pending` });
}

async function main(options = {}) {
  const coreApi = options.core || core;
  const raw = Object.hasOwn(options, "issues") ? options.issues : process.env.GH_AW_WORK_QUEUE_ISSUES;
  const config = issuesConfiguration(typeof raw === "string" && raw !== "" ? parseStrictJSON(raw) : raw === "" ? undefined : raw);
  if (!config) return { disabled: true };
  if (isStagedMode(options) || isStagedMode(options.config)) {
    coreApi.info("Work queue Issues: staged/trial mode; no live mutations");
    return { staged: true };
  }
  const githubClient = options.githubClient || github;
  const repositoryContext = options.context || context;
  const boundOptions = { ...options, githubClient, owner: repositoryContext.repo.owner, repo: repositoryContext.repo.repo };
  const { client: measured, metrics } = wrapGithubClient(githubClient, { sleep: options.sleep });
  boundOptions.githubClient = measured;
  const pending = [];
  try {
    const runtime = resolveWorkQueueRuntime(repositoryContext.payload, { role: options.role });
    if (runtime.role === "observer") return { disabled: true, observer: true };
    const origin =
      options.trustedContext ||
      (await authenticatePublisher({
        ...boundOptions,
        context: repositoryContext,
        role: runtime.assignment ? "worker" : "dispatcher",
        ...(runtime.assignment ? { dispatch_id: runtime.assignment.dispatch_id } : {}),
      }));
    const assignment = runtime.assignment || readInboundWorkQueueAssignment(repositoryContext.payload);
    const initial = await (options.readCheckedQueue || readCheckedQueue)(boundOptions);
    const permitted = ownProjectionTargets(initial.state, origin, origin.ref, assignment).targets;
    for (let offset = 0; offset < permitted.length; offset += MAX_PROJECTION_TARGETS) {
      const targets = permitted.slice(offset, offset + MAX_PROJECTION_TARGETS);
      try {
        const scopes = targets.map(target => ({ work_id: target.work_id, keys: [`work:${target.work_id}`, ...(target.resource ? [`issue:${issueIdentity(target.resource)}`] : [])] }));
        const result = await (options.withProjectionLocks || withProjectionLocks)(
          { ...boundOptions, head: initial.sha, repositoryId: initial.repositoryId, scopes, keys: scopes.flatMap(scope => scope.keys) },
          (accepted = targets.map(target => target.work_id)) =>
            projectBatch(
              boundOptions,
              initial,
              origin,
              assignment,
              config,
              targets.filter(target => accepted.includes(target.work_id))
            )
        );
        pending.push(...result.pending);
      } catch (error) {
        pending.push(
          ...targets.map(target => ({
            work_id: target.work_id,
            reason: error.code === "projection_field_pending" ? `queue committed; field sync pending: ${error.message}` : `queue committed; Issue synchronization pending: ${error.message}`,
          }))
        );
      }
    }
  } catch (error) {
    coreApi.warning(`Work queue Issues: queue committed; synchronization pending: ${error.message}`);
    return { pending: [{ reason: error.message }], metrics };
  }
  for (const diagnostic of pending) coreApi.warning(`Work queue Issues (${diagnostic.work_id}): ${diagnostic.reason}`);
  coreApi.info(`Work queue Issue projection: ${metrics.requests} requests (${metrics.reads} reads, ${metrics.mutations} mutations), including authentication, coordination, journals, and retries; ${pending.length} pending targets`);
  return { pending, metrics };
}

module.exports = { main, issuesConfiguration, ownProjectionTargets, workIssueStatus, summaryBody, claimBody, publishProjection, projectBatch, journalPath };
