"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { createHash } = require("node:crypto");

function canonical(value) {
  if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`;
  if (value && typeof value === "object")
    return `{${Object.keys(value)
      .sort((left, right) => Buffer.compare(Buffer.from(left), Buffer.from(right)))
      .map(key => `${JSON.stringify(key)}:${canonical(value[key])}`)
      .join(",")}}`;
  return JSON.stringify(value);
}

function digest(value) {
  return createHash("sha256").update(canonical(value)).digest("hex");
}

function buildFixtures() {
  const source = JSON.parse(fs.readFileSync(path.join(__dirname, "../../../specs/work-queue/fixtures/canonical-prefix.json"), "utf8"));
  const base = structuredClone(source.commits.slice(0, 8));
  const policy = base[0].operations[0].policy;
  policy.accounting_weights = { "": 1, tenant: 7 };
  policy.producers = { "11": { pools: ["default"], priorities: [1], fairness_keys: ["tenant"] } };
  policy.pools.default.profiles.default.principal = "22";
  policy.pools.other = structuredClone(policy.pools.default);
  policy.pools.other.allowed_repositories = ["foreign/repo"];
  for (const commit of base) {
    commit.actor.principal = commit.actor.role === "worker" ? "22" : "11";
    for (const operation of commit.operations) {
      if (operation.kind === "Work") {
        operation.priority = 1;
        operation.fairness_key = "tenant";
      }
      if (operation.sender) operation.sender.principal = "11";
      if (operation.run) operation.run.principal = "22";
      if (operation.evidence) operation.evidence.principal = "22";
    }
    if (commit.request.kind === "submit") commit.request.parameters.nodes = commit.operations;
    else if (!["dispatch_next", "finish"].includes(commit.request.kind)) commit.request.parameters.operations = commit.operations;
    commit.request.fingerprint = digest({ actor: commit.actor, kind: commit.request.kind, parameters: commit.request.parameters });
  }
  const grant = base.flatMap(commit => commit.operations).find(operation => operation.kind === "Claim");
  const worker = { ...base[6].actor, dispatch_id: grant.dispatch_id, claim_handle: "h1" };
  const child = {
    kind: "Work",
    work_id: digest({ graph_id: "graph", node_key: "worker-child" }),
    graph_id: "graph",
    node_key: "worker-child",
    pool: "default",
    priority: 1,
    fairness_key: "tenant",
    worker_profile: "default",
    batch_trust_domain: "default",
    payload: { task: "worker-child", effect_contract: { kind: "none" } },
    depends_on: [{ kind: "work", work_id: grant.work_id }],
    enqueued: 5000,
  };
  const definitions = [
    { name: "completed-parent-inherited-entitlement", prefix: 7, valid: true },
    { name: "verified-parent-inherited-entitlement", prefix: 8, valid: false, errorCode: "claim_effects_unauthorized" },
    { name: "open-parent-cannot-admit", prefix: 6, valid: false, errorCode: "claim_effects_unauthorized" },
    { name: "producer-principal-is-not-worker-identity", actor: { principal: "11" }, valid: false, errorCode: "run_binding_conflict" },
    { name: "foreign-native-run", actor: { run_id: "203" }, valid: false, errorCode: "run_binding_conflict" },
    { name: "native-rerun", actor: { run_attempt: 2 }, valid: false, errorCode: "actor_unauthorized" },
    { name: "foreign-original-claim", actor: { claim_handle: "h4" }, valid: false, errorCode: "claim_scope_invalid" },
    { name: "worker-is-not-a-root-producer", actor: { role: "producer" }, valid: false, errorCode: "admission_unauthorized" },
    { name: "worker-is-not-an-administrator", actor: { role: "administrator" }, valid: false, errorCode: "admission_unauthorized" },
    { name: "actor-logical-origin-is-not-artifact-input", actor: { logical_origin: { role: "producer", principal: "11", repository: "owner/repo" } }, valid: false, errorCode: "unsupported_protocol" },
    { name: "work-logical-origin-is-not-artifact-input", node: { logical_origin: { principal: "11" } }, valid: false, errorCode: "unsupported_protocol" },
    { name: "pool-escalation", node: { pool: "other" }, valid: false, errorCode: "child_entitlement" },
    { name: "priority-entitlement-change", node: { priority: 5 }, valid: false, errorCode: "child_entitlement" },
    { name: "accounting-key-escalation", node: { fairness_key: "" }, valid: false, errorCode: "child_entitlement" },
    { name: "unapproved-worker-profile", node: { worker_profile: "agent-admin" }, valid: false, errorCode: "work_invalid" },
    { name: "foreign-resource-escalation", node: { subject: { kind: "issue", host: "github.com", repository: "foreign/repo", repository_id: "9", resource_id: "10", number: "1" } }, valid: false, errorCode: "resource_unauthorized" },
  ];
  const cases = [
    ...definitions,
    ...definitions.map(definition => ({
      ...definition,
      name: definition.valid ? "completed-parent-cancelled-sibling-unit-weight-entitlement" : `unit-weight-cancelled-sibling-${definition.name}`,
      cancelledSibling: true,
    })),
    { name: "cancelled-sibling-cannot-admit-child", actor: { claim_handle: "h2" }, cancelledSibling: true, valid: false, errorCode: "claim_effects_unauthorized" },
  ].map(definition => {
    const transactions = structuredClone(base.slice(0, definition.prefix ?? 7));
    if (definition.cancelledSibling) {
      const fanoutPolicy = transactions[0].operations[0].policy;
      fanoutPolicy.accounting_weights.tenant = 1;
      fanoutPolicy.pools.default.profiles.default.max_claims = 2;
      const submission = transactions.find(commit => commit.request.kind === "submit");
      submission.operations = submission.operations.slice(0, 2).map(operation => ({
        ...operation,
        depends_on: [],
        payload: { ...operation.payload, effect_contract: { version: 1, outputs: [{ type: "work_queue_submit", min: 1, max: 1 }] } },
      }));
      submission.request.parameters.nodes = submission.operations;
      const assignment = transactions.find(commit => commit.request.kind === "dispatch_next");
      assignment.request.parameters.max_claims = 2;
      assignment.operations = assignment.operations.slice(0, 2).map(operation => ({ ...operation, observations: [] }));
      const observationIndex = transactions.findIndex(commit => commit.request.kind === "observe");
      transactions.splice(observationIndex, 1);
      const sibling = assignment.operations[1];
      const cancellationActor = { ...worker, claim_handle: sibling.handle };
      const cancellationParameters = { dispatch_id: grant.dispatch_id, claim_handle: sibling.handle, outcome: "cancelled" };
      transactions.push({
        version: 3,
        id: "cancelled-original-sibling",
        previous: transactions.at(-1).id,
        request: {
          id: "finish-original-sibling",
          kind: "finish",
          parameters: cancellationParameters,
          fingerprint: digest({ actor: cancellationActor, kind: "finish", parameters: cancellationParameters }),
        },
        actor: cancellationActor,
        policy_epoch: base[0].policy_epoch,
        at: 4400,
        operations: [{ kind: "ClaimCancellation", work_id: sibling.work_id, claim_id: sibling.claim_id, reason: "worker_cancelled", retry_not_before: 34400 }],
      });
      for (let index = 0; index < transactions.length; index++) {
        const commit = transactions[index];
        commit.previous = index === 0 ? null : transactions[index - 1].id;
        commit.request.fingerprint = digest({ actor: commit.actor, kind: commit.request.kind, parameters: commit.request.parameters });
      }
    }
    const actor = { ...worker, ...definition.actor };
    const node = { ...child, ...definition.node };
    const parameters = { nodes: [node] };
    const request = { id: `request-${definition.name}`, kind: "submit", parameters, fingerprint: digest({ actor, kind: "submit", parameters }) };
    transactions.push({
      version: 3,
      id: definition.name,
      previous: transactions.at(-1).id,
      request,
      actor,
      policy_epoch: base[0].policy_epoch,
      at: 5000,
      operations: [node],
    });
    return {
      name: definition.name,
      valid: definition.valid,
      transactions,
      canonical: transactions.map(commit => `${canonical(commit)}\n`).join(""),
      ...(!definition.valid ? { error_code: definition.errorCode } : {}),
      ...(definition.valid
        ? {
            expected: {
              worker_principal: "22",
              producer_principal: "11",
              pool: "default",
              priority: 1,
              fairness_key: "tenant",
              child_work_id: child.work_id,
              parent_work_id: grant.work_id,
              ...(definition.cancelledSibling ? { accounting_weight: 1, bound_claims: 2, cancelled_sibling_work_id: transactions.find(commit => commit.id === "cancelled-original-sibling").operations[0].work_id } : {}),
            },
          }
        : {}),
    };
  });
  return { version: 3, source: "specs/work-queue/fixtures/canonical-prefix.json", cases };
}

if (require.main === module) fs.writeFileSync(path.join(__dirname, "work_queue_worker_child_fixtures.json"), JSON.stringify(buildFixtures(), null, 2) + "\n");
module.exports = { buildFixtures };
