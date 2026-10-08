// @ts-check
"use strict";

const { canonical, queueError } = require("./work_queue_codec.cjs");

function issueIdentity(resource) {
  return canonical([resource.host, resource.repository_id, resource.resource_id]);
}

function assertProjectionAuthority(state, actor, workId, ref, repository, claimId) {
  const work = state.works.get(workId);
  if (!work || actor.role !== "projector" || !actor.workflow || !actor.run_id || !actor.run_attempt) throw queueError("projection_unauthorized", "projection requires an authenticated originating run");
  if (!state.policy.projectors?.some(rule => rule.principal === actor.principal && rule.workflow === actor.workflow && rule.ref === ref && rule.pools.includes(work.pool) && rule.repositories.includes(repository)))
    throw queueError("projection_unauthorized", "no installed projector authority for this revision and target");
  if (!state.policy.pools[work.pool].allowed_repositories.includes(repository)) throw queueError("resource_unauthorized", "backing Issue repository is not allowlisted by the Work pool");
  if (claimId !== undefined) {
    const claim = state.claims.get(claimId);
    const dispatch = claim && state.dispatches.get(claim.dispatch_id);
    const run = dispatch?.run;
    if (
      !claim ||
      claim.work_id !== workId ||
      !dispatch.claims.some(member => member.claim_id === claimId && member.work_id === workId && member.handle === claim.handle) ||
      !run ||
      run.repository !== actor.repository ||
      run.workflow !== actor.workflow ||
      run.ref !== ref ||
      run.principal !== actor.principal ||
      run.run_id !== actor.run_id ||
      run.run_attempt !== actor.run_attempt ||
      actor.run_attempt !== 1 ||
      actor.dispatch_id !== dispatch.dispatch_id
    )
      throw queueError("projection_unauthorized", "projection is outside the original authenticated Claims");
  } else {
    const admission = state.transactions[work.position.commit]?.actor;
    if (!admission || ["principal", "repository", "workflow", "run_id", "run_attempt"].some(field => admission[field] !== actor[field])) throw queueError("projection_unauthorized", "projection is outside this run's checked admissions");
  }
  return work;
}

function applyIssueBinding(state, operation, commit) {
  const work = state.works.get(operation.work_id);
  const resource = operation.kind === "IssueLink" ? operation.resource : work?.backing_issue || work?.issue_link;
  if (!resource || resource.kind !== "issue") throw queueError("issue_binding_invalid", "Issue binding requires an immutable Issue resource");
  if (!state.policy.pools[work.pool].allowed_repositories.includes(resource.repository)) throw queueError("resource_unauthorized", "backing Issue repository is not allowlisted");
  assertProjectionAuthority(state, commit.actor, operation.work_id, operation.projector_ref, resource.repository, operation.authority_claim_id ?? operation.claim_id);
  if (operation.kind === "IssueComment") {
    for (const other of state.works.values()) {
      if (other.issue_summary === operation.comment_id && (other.work_id !== work.work_id || operation.claim_id !== undefined)) throw queueError("issue_binding_conflict", "comment handle already belongs to another summary");
    }
    for (const claim of state.claims.values()) {
      if (claim.issue_comment === operation.comment_id && claim.claim_id !== operation.claim_id) throw queueError("issue_binding_conflict", "comment handle already belongs to another Claim");
    }
  }
  if (operation.kind === "IssueLink") {
    const existing = work.backing_issue || work.issue_link;
    if (existing && canonical(existing) !== canonical(resource)) throw queueError("issue_binding_conflict", "backing Issue cannot be rebound");
    for (const other of state.works.values()) {
      const bound = other.backing_issue || other.issue_link;
      if (bound && other.work_id !== work.work_id && issueIdentity(bound) === issueIdentity(resource)) throw queueError("issue_binding_conflict", "one Work per backing Issue");
    }
    if (!work.backing_issue) work.issue_link = structuredClone(resource);
  } else if (operation.claim_id !== undefined) {
    if (operation.authority_claim_id !== undefined && operation.authority_claim_id !== operation.claim_id) throw queueError("projection_unauthorized", "Claim comment requires its own original Claim");
    const claim = state.claims.get(operation.claim_id);
    if (!claim || claim.work_id !== work.work_id) throw queueError("issue_binding_invalid", "comment names a foreign Claim");
    if (claim.issue_comment && claim.issue_comment !== operation.comment_id) throw queueError("issue_binding_conflict", "Claim comment cannot be rebound");
    claim.issue_comment = operation.comment_id;
  } else {
    if (work.issue_summary && work.issue_summary !== operation.comment_id) throw queueError("issue_binding_conflict", "canonical summary cannot be rebound");
    work.issue_summary = operation.comment_id;
  }
}

module.exports = { issueIdentity, assertProjectionAuthority, applyIssueBinding };
