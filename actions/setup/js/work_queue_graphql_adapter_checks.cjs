"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { withClaimExecution } = require("./work_queue_claim_scope.cjs");
const { validateAdapter, createClaimAdapterHandler, preparedAdapterPath } = require("./work_queue_claim_adapters.cjs");

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

function assignment(dispatch = "d") {
  return {
    dispatch_id: dispatch,
    claims: [{ handle: "h1", claim_id: `claim:${dispatch}`, work_id: `work:${dispatch}`, work: { effect_contract: { version: 1, outputs: [{ type: "custom", min: 1, max: 1 }] } }, result_refs: [] }],
  };
}

function registerTests({ describe, it }) {
  describe("trusted per-Claim GraphQL delivery", () => {
    it("executes the declared GraphQL mutation once and records its graph effect without readback", async () => {
      const root = require("./work_queue_effect_test_helpers.cjs").temporaryDirectory("claim-graphql");
      const scoped = assignment();
      const effects = [];
      const message = { type: "custom", claim_handle: "h1", title: "Exact title", body: "Exact body", category: "category" };
      let writes = 0;
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
                ...(id === "repository" ? { nameWithOwner: "owner/repo", databaseId: 7 } : { repository: { nameWithOwner: "owner/repo", databaseId: 7 } }),
              })),
            };
          if (query.startsWith("mutation")) {
            assert.equal(query.match(/createDiscussion/g).length, 1);
            assert.deepEqual(variables.input, { title: message.title, body: message.body, categoryId: "category", repositoryId: "repository" });
            writes++;
            return { createDiscussion: { discussion: { id: "discussion", __typename: "Discussion", repository: { nameWithOwner: "owner/repo" } } } };
          }
          throw new Error(`unexpected readback ${query}`);
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
          assert.deepEqual(result, { success: true, repo: "owner/repo", id: "discussion" });
          assert.equal(effects.length, 1);
          assert.equal(effects[0].kind, "graphql");
          assert.equal(effects[0].repository, "owner/repo");
          cancelled = true;
          await assert.rejects(handler(message), error => error instanceof Error && "suppressed" in error && error.suppressed === true && "state" in error && error.state === "cancelled");
          assert.equal(writes, 1);
        });
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });

    it("rejects foreign node targets before writes while another Dispatch's valid Claim can deliver", async () => {
      const { createGraphqlEffectHandler } = require("./work_queue_graphql_adapter.cjs");
      const first = assignment("d-a");
      const second = assignment("d-b");
      let writes = 0;
      let reads = 0;
      const authorize = async request => ({ claim_handle: request.claim_handle, authorized: true, state: "open" });
      const github = {
        graphql: async (query, variables) => {
          if (query.startsWith("query ClaimAdapterRepository")) {
            reads++;
            return { repository: { id: "repository", nameWithOwner: "owner/repo" } };
          }
          if (query.startsWith("query WorkQueueEffectTargets")) {
            reads++;
            return { nodes: variables.ids.map(id => ({ id, __typename: "Repository", nameWithOwner: id === "foreign" ? "other/repo" : "owner/repo", databaseId: id === "foreign" ? 8 : 7 })) };
          }
          writes++;
          return { createDiscussion: { discussion: { id: "native", __typename: "Discussion", repository: { nameWithOwner: "owner/repo" } } } };
        },
      };
      const twoMember = { dispatch_id: "d-c", claims: [first.claims[0], { ...second.claims[0], handle: "h2" }] };
      let entered = false;
      assert.throws(
        () =>
          withClaimExecution({ assignment: twoMember, claim_handle: "h1", authorize }, () => {
            entered = true;
            createGraphqlEffectHandler(adapter, github);
          }),
        /exactly one open Claim/
      );
      assert.equal(entered, false);
      assert.deepEqual({ reads, writes }, { reads: 0, writes: 0 });
      const message = { type: "custom", claim_handle: "h1", title: "title", body: "body", categoryId: "category" };
      const firstHandler = await withClaimExecution({ assignment: first, claim_handle: "h1", authorize }, async () => {
        const handler = createGraphqlEffectHandler(adapter, github);
        await assert.rejects(handler({ ...message, categoryId: "foreign" }), /fixed repository/);
        await assert.rejects(handler({ ...message, claim_handle: "h2" }), /foreign/);
        await assert.rejects(handler({ ...message, repo: "other/repo" }), /conflicts/);
        return handler;
      });
      assert.equal(writes, 0);
      await withClaimExecution({ assignment: second, claim_handle: "h1", authorize }, async () => {
        await assert.rejects(firstHandler(message), /cannot escape/);
        assert.equal(writes, 0);
        const handler = createGraphqlEffectHandler(adapter, github);
        assert.equal((await handler(message)).success, true);
        await assert.rejects(handler({ ...message, claim_handle: "h2" }), /foreign/);
        await assert.rejects(handler({ ...message, repo: "other/repo" }), /conflicts/);
      });
      assert.equal(writes, 1);
    });

    it("validates bounded declarations, accepts legacy readback keys, and keeps direct previews read-only", async () => {
      /** @type {Record<string, any>} */
      const minimal = structuredClone(adapter);
      delete minimal.graphql.fields;
      delete minimal.graphql["number-field"];
      delete minimal.graphql["resource-kind"];
      assert.equal(validateAdapter(minimal), minimal);
      for (const field of ["repositoryId", "query", "headers", "__proto__", "constructor"]) {
        const invalid = structuredClone(adapter);
        Object.defineProperty(invalid["field-map"], field, { enumerable: true, value: "value" });
        assert.throws(() => validateAdapter(invalid), /reserved effect fields/);
      }
      const invalid = structuredClone(adapter);
      invalid.graphql.mutation = "createDiscussion) { deleteRepository";
      assert.throws(() => validateAdapter(invalid), /fixed native/);
      const reserved = structuredClone(adapter);
      reserved.graphql["repository-field"] = "repository.query";
      assert.throws(() => validateAdapter(reserved), /repository field path/);
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
