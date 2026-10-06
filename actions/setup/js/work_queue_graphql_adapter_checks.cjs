"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { randomUUID } = require("node:crypto");
const { withClaimExecution } = require("./work_queue_claim_scope.cjs");
const { validateAdapter, createClaimAdapterHandler, preparedAdapterPath, verifyClaimAdapterOutput } = require("./work_queue_claim_adapters.cjs");
const { verifyClaimDelivery } = require("./work_queue_delivery.cjs");

const adapter = {
  mode: "prepared",
  "effect-type": "github_graphql",
  "target-repo": "owner/repo",
  "field-map": { title: "title", body: "body", categoryId: "category" },
  graphql: {
    mutation: "createDiscussion",
    "input-type": "CreateDiscussionInput",
    "response-field": "discussion",
    "resource-type": "Discussion",
    "resource-kind": "discussion",
    "repository-input": "repositoryId",
    "repository-field": "repository.nameWithOwner",
    "number-field": "number",
    permission: "discussions",
    fields: { title: "title", body: "body", categoryId: "category.id" },
  },
};

function assignment() {
  return {
    version: 3,
    dispatch_id: "d",
    request_id: "r",
    commit_id: "c",
    policy_epoch: "epoch",
    pool: "default",
    worker_profile: "default",
    claims: ["h1", "h2"].map(handle => ({ handle, claim_id: `claim:${handle}`, work_id: `work:${handle}`, work: { effect_contract: { version: 1, outputs: [{ type: "custom", min: 1, max: 1 }] } }, result_refs: [] })),
  };
}

function registerTests({ describe, it }) {
  describe("trusted per-Claim GraphQL delivery", () => {
    it("verifies complete independent native fields and actual graph effects without trusting copied receipts or job success", async () => {
      const root = path.resolve(".queue-validation-cache", `claim-graphql-${randomUUID()}`);
      const scoped = assignment();
      const effects = [];
      const message = { type: "custom", claim_handle: "h1", title: "Exact title", body: "Exact body", category: "category" };
      let writes = 0;
      let observed;
      let cancelled = false;
      const authorize = async request => ({ claim_handle: request.claim_handle, authorized: !cancelled, ...(cancelled ? { suppressed: true, state: "cancelled" } : {}) });
      const github = {
        graphql: async (query, variables) => {
          if (query.startsWith("query ClaimAdapterRepository")) return { repository: { id: "repository", nameWithOwner: "owner/repo" } };
          if (query.startsWith("query WorkQueueEffectTargets"))
            return {
              nodes: variables.ids.map(id => ({
                id,
                __typename: id === "repository" ? "Repository" : "DiscussionCategory",
                ...(id === "repository" ? { nameWithOwner: "owner/repo" } : { repository: { nameWithOwner: id === "foreign" ? "other/repo" : "owner/repo" } }),
              })),
            };
          if (query.startsWith("mutation")) {
            assert.equal(query.match(/createDiscussion/g).length, 1);
            assert.deepEqual(variables.input, { title: message.title, body: message.body, categoryId: "category", repositoryId: "repository" });
            writes++;
            observed = { id: "discussion", __typename: "Discussion", number: 42, repository: { nameWithOwner: "owner/repo" }, title: message.title, body: message.body, category: { id: "category" } };
            return { createDiscussion: { discussion: structuredClone(observed) } };
          }
          assert.ok(query.startsWith("query ClaimAdapterReadback"));
          assert.equal(variables.id, "discussion");
          return { node: structuredClone(observed) };
        },
      };
      try {
        await withClaimExecution({ assignment: scoped, claim_handle: "h1", authorize, effects }, async () => {
          const configured = structuredClone(adapter);
          const filename = preparedAdapterPath(root, "h1", "custom");
          fs.mkdirSync(path.dirname(filename), { recursive: true });
          fs.writeFileSync(filename, JSON.stringify({ version: 3, claim_handle: "h1", type: "custom", messages: [{ input: message, payload: { title: message.title, body: message.body, category: "category" } }] }));
          const handler = await createClaimAdapterHandler({ adapter: configured, filename, github });
          configured.graphql.mutation = "updateDiscussion";
          const result = await handler(message);
          configured.graphql.mutation = "createDiscussion";
          const verification = {
            assignment: scoped,
            claim_handle: "h1",
            authorize,
            effects,
            github,
            messages: [message],
            results: [{ messageIndex: 0, success: true, result }],
            verifyOutput: input => verifyClaimAdapterOutput({ ...input, adapter: configured }),
          };
          assert.equal((await verifyClaimDelivery(verification)).verification, "verified");
          assert.equal(effects.length, 1);
          assert.equal(effects[0].kind, "graphql");
          assert.equal(effects[0].repository, "owner/repo");
          assert.equal((await verifyClaimAdapterOutput({ claim: scoped.claims[0], result: structuredClone(result), adapter: configured, github })).verified, false);
          observed.body = "different";
          assert.equal((await verifyClaimDelivery(verification)).verification, "unknown");
          observed.body = message.body;
          observed.repository.nameWithOwner = "other/repo";
          assert.equal((await verifyClaimDelivery(verification)).verification, "unknown");
          cancelled = true;
          await assert.rejects(handler(message), error => error instanceof Error && "suppressed" in error && error.suppressed === true && "state" in error && error.state === "cancelled");
          assert.equal(writes, 1);
        });
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });

    it("rejects foreign node targets before writes while a valid sibling can independently deliver", async () => {
      const { createGraphqlEffectHandler, verifyGraphqlAdapterDelivery } = require("./work_queue_graphql_adapter.cjs");
      const scoped = assignment();
      let writes = 0;
      let observed;
      const authorize = async request => ({ claim_handle: request.claim_handle, authorized: true });
      const github = {
        graphql: async (query, variables) => {
          if (query.startsWith("query ClaimAdapterRepository")) return { repository: { id: "repository", nameWithOwner: "owner/repo" } };
          if (query.startsWith("query WorkQueueEffectTargets")) return { nodes: variables.ids.map(id => ({ id, __typename: "Repository", nameWithOwner: id === "foreign" ? "other/repo" : "owner/repo" })) };
          if (query.startsWith("query ClaimAdapterReadback")) return { node: observed };
          writes++;
          observed = { id: "native", __typename: "Discussion", number: 42, repository: { nameWithOwner: "owner/repo" }, title: variables.input.title, body: variables.input.body, category: { id: variables.input.categoryId } };
          return { createDiscussion: { discussion: observed } };
        },
      };
      for (const handle of ["h1", "h2"]) {
        await withClaimExecution({ assignment: scoped, claim_handle: handle, authorize }, async () => {
          const handler = createGraphqlEffectHandler(adapter, github);
          const message = { type: "custom", claim_handle: handle, title: "title", body: "body", categoryId: handle === "h1" ? "foreign" : "category" };
          if (handle === "h1") await assert.rejects(handler(message), /fixed repository/);
          else {
            const result = await handler(message);
            assert.equal(result.success, true);
            assert.equal((await verifyGraphqlAdapterDelivery({ adapter, github, result, claim: scoped.claims[1] })).verified, true);
          }
          await assert.rejects(handler({ ...message, claim_handle: handle === "h1" ? "h2" : "h1" }), /cannot escape/);
          await assert.rejects(handler({ ...message, repo: "other/repo" }), /conflicts/);
        });
      }
      assert.equal(writes, 1);
    });

    it("requires complete bounded verifier declarations and keeps direct previews read-only", async () => {
      /** @type {Record<string, any>} */
      const malformed = structuredClone(adapter);
      delete malformed.graphql.fields.body;
      assert.throws(() => validateAdapter(malformed), /Every declared GraphQL/);
      for (const field of ["repositoryId", "query", "headers", "__proto__", "constructor"]) {
        const invalid = structuredClone(adapter);
        Object.defineProperty(invalid["field-map"], field, { enumerable: true, value: "value" });
        assert.throws(() => validateAdapter(invalid), /reserved effect fields/);
      }
      const invalid = structuredClone(adapter);
      invalid.graphql.mutation = "createDiscussion) { deleteRepository";
      assert.throws(() => validateAdapter(invalid), /fixed native/);
      const conflict = structuredClone(adapter);
      conflict.graphql.fields.categoryId = "repository";
      assert.throws(() => validateAdapter(conflict), /conflicting scalar/);
      const previous = process.env.GH_AW_SAFE_OUTPUTS_STAGED;
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";
      try {
        await withClaimExecution(
          {
            assignment: assignment(),
            claim_handle: "h1",
            authorize: async request => {
              assert.equal(request.requireCompletion, false);
              return { claim_handle: request.claim_handle, authorized: true };
            },
          },
          async () => {
            const { createGraphqlEffectHandler } = require("./work_queue_graphql_adapter.cjs");
            const handler = createGraphqlEffectHandler(adapter, {
              graphql: async () => {
                throw new Error("Preview cannot call GitHub");
              },
            });
            assert.equal((await handler({ type: "custom", claim_handle: "h1" })).staged, true);
          }
        );
      } finally {
        previous === undefined ? delete process.env.GH_AW_SAFE_OUTPUTS_STAGED : (process.env.GH_AW_SAFE_OUTPUTS_STAGED = previous);
      }
    });
  });
}

if (require.main === module) registerTests(require("node:test"));
module.exports = { registerTests };
