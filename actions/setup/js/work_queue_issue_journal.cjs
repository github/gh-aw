// @ts-check
"use strict";

const { closed, identity, queueError } = require("./work_queue_codec.cjs");
const { validateActor } = require("./work_queue_policy.cjs");
const { validateResource } = require("./work_queue_graph.cjs");
const { assertProjectionAuthority } = require("./work_queue_issue_contract.cjs");

function validateJournal(journal, state, workId, repository) {
  closed(journal, ["version", "work_id", "comments"], ["create"], "Issue projection journal");
  if (journal.version !== 1 || journal.work_id !== workId) throw queueError("projection_journal_invalid", "projection journal has a foreign Work");
  closed(journal.comments, [], Object.keys(journal.comments || {}), "comment receipts");
  if (Object.keys(journal.comments).length > 4096) throw queueError("projection_journal_invalid", "comment receipt limit exceeded");
  const validateOrigin = (receipt, claimId) => {
    identity(receipt.nonce, "projection nonce");
    validateActor(receipt.origin);
    if (!/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(receipt.ref)) throw queueError("projection_journal_invalid", "receipt requires an immutable workflow revision");
    assertProjectionAuthority(state, receipt.origin, workId, receipt.ref, repository, receipt.authority_claim_id ?? claimId);
  };
  if (journal.create) {
    closed(journal.create, ["nonce", "origin", "ref"], ["authority_claim_id", "resource", "node_id"], "Issue creation receipt");
    if (Object.hasOwn(journal.create, "resource") !== Object.hasOwn(journal.create, "node_id")) throw queueError("projection_journal_invalid", "native Issue receipt is incomplete");
    if (journal.create.resource) {
      validateResource(journal.create.resource);
      if (journal.create.resource.kind !== "issue" || journal.create.resource.repository !== repository) throw queueError("projection_journal_invalid", "native Issue receipt is foreign");
      identity(journal.create.node_id, "native Issue node");
    }
    validateOrigin(journal.create, undefined);
  }
  for (const [handle, receipt] of Object.entries(journal.comments)) {
    identity(handle, "comment handle");
    closed(receipt, ["nonce", "origin", "ref"], ["id", "authority_claim_id"], "comment creation receipt");
    if (receipt.id) identity(receipt.id, "native comment node");
    const claimId = handle === "summary" ? undefined : handle;
    if (claimId && receipt.authority_claim_id !== undefined && receipt.authority_claim_id !== claimId) throw queueError("projection_journal_invalid", "comment receipt has foreign Claim authority");
    validateOrigin(receipt, claimId);
  }
  return journal;
}

module.exports = { validateJournal };
