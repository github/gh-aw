"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { randomUUID } = require("node:crypto");
const { createCompilerDependencyResolver, credentialBindings, main } = require("./work_queue_control_adapter.cjs");
const { queueFixture, WORKFLOW, REF, REPOSITORY } = require("./work_queue_lifecycle.test_helpers.cjs");

const repository = "owner/repo";
const foreign = "foreign/design";
const bindings = { GH_AW_WORK_QUEUE_DEPENDENCY_READ_CREDENTIALS: JSON.stringify({ [foreign]: "GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_0" }), GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_0: 'separate-read-token-"quoted"' };

function resolverOptions(overrides = {}) {
  return {
    context: { repo: { owner: "owner", repo: "repo" } },
    env: bindings,
    state: { credential_generation: "generation", policy: { pools: { default: { allowed_repositories: [repository, foreign] } } } },
    ...overrides,
  };
}

function registerTests({ describe, it }) {
  describe("compiler-bound independent dependency read clients", () => {
    it("uses installed Policy scopes, pinned generation and separate credentials without exposing write clients or token serialization", async () => {
      const calls = [];
      const factory = [];
      const client = {
        rest: {
          repos: {
            get: async input => {
              calls.push(input);
              return { data: { full_name: foreign, id: 20 } };
            },
          },
          issues: { get: async input => ({ data: input }) },
        },
      };
      const own = { rest: { repos: { get: async () => ({ data: { full_name: repository, id: 7 } }) } } };
      const resolver = createCompilerDependencyResolver(
        resolverOptions({
          githubClient: own,
          getOctokit: (token, options) => {
            factory.push({ token, options });
            return client;
          },
        })
      );
      assert.equal(resolver.scopes.length, 2);
      const scope = resolver.scopes.find(scope => scope.repository === foreign);
      assert.ok(scope);
      assert.equal(scope.access_generation, "generation");
      const read = await resolver.getClient(scope);
      assert.equal(read.rest.issues.create, undefined);
      assert.equal(read.request, undefined);
      assert.equal(read.graphql, undefined);
      await read.rest.repos.get({ owner: "foreign", repo: "design" });
      assert.deepEqual(factory, [{ token: bindings.GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_0, options: { baseUrl: "https://api.github.com" } }]);
      assert.equal(await resolver.getClient(scope), read);
      assert.equal(factory.length, 1);
      assert.equal(calls[0].headers["X-GitHub-Api-Version"], "2026-03-10");
      assert.equal(calls[0].request.retries, 0);
      assert.equal(JSON.stringify(resolver).includes("separate-read-token"), false);
      assert.equal(JSON.stringify(resolver).includes("TOKEN_0"), false);
      const local = await resolver.getClient(resolver.scopes.find(scope => scope.repository === repository));
      assert.equal((await local.rest.repos.get({ owner: "owner", repo: "repo" })).data.id, 7);
      await assert.rejects(resolver.getClient({ ...scope, access_generation: "old" }), /not_allowlisted/);
      await assert.rejects(resolver.getClient({ ...scope, repository: "foreign/unapproved" }), /not_allowlisted/);
      await assert.rejects(read.rest.repos.get({ owner: "foreign", repo: "unapproved" }), /scope_invalid/);
      await assert.rejects(read.rest.repos.get({ owner: "foreign", repo: "design", method: "POST" }), /scope_invalid/);
      await assert.rejects(read.rest.issues.get({ owner: "foreign", repo: "design", issue_number: 1, headers: { Authorization: "different" } }), /scope_invalid/);
    });

    it("never falls back to the write-capable queue client for missing foreign credentials or accepts compiled allowlists as authority", async () => {
      let calls = 0;
      const resolver = createCompilerDependencyResolver(
        resolverOptions({
          env: {},
          githubClient: {
            rest: {
              repos: {
                get: async () => {
                  calls++;
                },
              },
            },
          },
        })
      );
      assert.equal(await resolver.getClient(resolver.scopes.find(scope => scope.repository === foreign)), null);
      assert.equal(calls, 0);
      const extra = { ...bindings, GH_AW_WORK_QUEUE_DEPENDENCY_READ_CREDENTIALS: JSON.stringify({ "foreign/unapproved": "GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_0" }) };
      const approved = createCompilerDependencyResolver(resolverOptions({ env: extra }));
      await assert.rejects(approved.getClient({ host: "github.com", repository: "foreign/unapproved", access_generation: "generation" }), /not_allowlisted/);
      for (const value of ["null", "[]", '{"foreign/design":"actual-secret"}', '{"foreign/design":"GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_0","FOREIGN/design":"GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_1"}']) {
        assert.throws(() => credentialBindings(value), /credentials/);
      }
      assert.throws(() => credentialBindings(JSON.stringify(Object.fromEntries(Array.from({ length: 65 }, (_, index) => [`foreign/repo${index}`, `GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_${index}`])))), /credentials/);
      assert.throws(() => createCompilerDependencyResolver(resolverOptions({ env: { GITHUB_SERVER_URL: "https://github.com", GITHUB_API_URL: "https://foreign.example/api/v3" } })), /host_invalid/);
    });

    it("keeps independent credentials pinned on GHES and rejects native request overrides", async () => {
      const calls = [];
      const env = { ...bindings, GITHUB_SERVER_URL: "https://github.example", GITHUB_API_URL: "https://github.example/api/v3" };
      const resolver = createCompilerDependencyResolver(
        resolverOptions({
          env,
          getOctokit: (token, options) => {
            calls.push({ token, options });
            return { rest: { pulls: { get: async input => ({ data: input }) } } };
          },
        })
      );
      env.GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_0 = "changed-after-configuration";
      const scope = resolver.scopes.find(scope => scope.repository === foreign);
      assert.ok(scope);
      const read = await resolver.getClient(scope);
      assert.deepEqual(calls, [{ token: bindings.GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_0, options: { baseUrl: "https://github.example/api/v3" } }]);
      assert.equal(Object.isFrozen(scope), true);
      assert.equal(Object.isFrozen(read.rest.pulls), true);
      const response = await read.rest.pulls.get({ owner: "foreign", repo: "design", pull_number: "7" });
      assert.equal(response.data.pull_number, "7");
      await assert.rejects(read.rest.pulls.get({ owner: "foreign", repo: "design", pull_number: "07" }), /number/);
      await assert.rejects(read.rest.pulls.get({ owner: "foreign", repo: "design", pull_number: "7", request: { retries: 1, timeout: 15000 } }), /scope_invalid/);
      await assert.rejects(resolver.getClient({ ...scope, host: "github.com" }), /not_allowlisted/);
    });

    it("does not load dependency credentials or write for a staged active Claim without Completion", async () => {
      const root = path.resolve(".queue-validation-cache", `control-adapter-${randomUUID()}`);
      const fixture = queueFixture({ bound: true, count: 1 });
      const before = JSON.stringify(fixture.transactions);
      let writes = 0;
      try {
        fs.mkdirSync(root, { recursive: true });
        const filename = path.join(root, "intents.jsonl");
        fs.writeFileSync(filename, JSON.stringify({ version: 3, intent_id: "preview-submit", kind: "submit", parameters: { nodes: [{ graph_id: "preview", node_key: "child", payload: { task: "preview" }, depends_on: [] }] } }) + "\n");
        const result = await main({
          staged: true,
          requireAssignment: true,
          env: { GH_AW_WORK_QUEUE_DEPENDENCY_READ_CREDENTIALS: "invalid credentials must not be parsed in preview" },
          intentPath: filename,
          githubClient: fixture.githubClient,
          dispatchClient: fixture.githubClient,
          context: fixture.workerContext,
          readWorkQueueLog: fixture.readWorkQueueLog,
          publishWorkQueueRequest: async () => {
            writes++;
            throw new Error("preview wrote");
          },
          getOctokit: () => {
            writes++;
            throw new Error("preview consumed separate credentials");
          },
          core: { setOutput: () => {}, info: () => {} },
        });
        assert.equal(result.receipts[0].status, "staged_preview", JSON.stringify(result));
        assert.equal(writes, 0);
        assert.equal(JSON.stringify(fixture.transactions), before);
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });

    it("wires the emitted runtime entrypoint into actual completed-Claim foreign admission with immutable identities and no foreign write authority", async () => {
      const root = path.resolve(".queue-validation-cache", `control-adapter-${randomUUID()}`);
      const fixture = queueFixture({ bound: true, count: 1, configurePolicy: policy => policy.pools.default.allowed_repositories.push(foreign) });
      const assignment = fixture.assignment;
      assert.ok(assignment);
      const member = assignment.claims[0];
      fixture.append("finish", { dispatch_id: assignment.dispatch_id, claim_handle: member.handle, outcome: "completed" }, { ...fixture.workerActor, dispatch_id: assignment.dispatch_id, claim_handle: member.handle });
      const outputs = [];
      let reads = 0;
      const foreignClient = {
        rest: {
          repos: {
            get: async () => {
              reads++;
              return { status: 200, data: { id: 20, full_name: foreign } };
            },
          },
          issues: {
            get: async () => {
              reads++;
              return { status: 200, data: { id: 30, number: 7, state: "closed", state_reason: "completed" } };
            },
          },
        },
      };
      try {
        fs.mkdirSync(root, { recursive: true });
        const filename = path.join(root, "intents.jsonl");
        fs.writeFileSync(
          filename,
          JSON.stringify({
            version: 3,
            intent_id: "foreign-child",
            kind: "submit",
            claim_handle: member.handle,
            parameters: {
              nodes: [
                {
                  graph_id: "child-graph",
                  node_key: "child",
                  payload: { task: "foreign evidence" },
                  depends_on: [{ kind: "issue", condition: "completed", resource: { kind: "issue", host: "github.com", repository: foreign, number: "7" } }],
                },
              ],
            },
          }) + "\n"
        );
        const result = await main({
          env: bindings,
          intentPath: filename,
          githubClient: fixture.githubClient,
          dispatchClient: fixture.githubClient,
          context: fixture.workerContext,
          workflowRef: `${REPOSITORY}/${WORKFLOW}@${REF}`,
          readWorkQueueLog: fixture.readWorkQueueLog,
          publishWorkQueueRequest: fixture.publishWorkQueueRequest,
          config: { work_queue_workflows: ["worker"], aw_context_workflows: ["worker"] },
          getOctokit: token => {
            assert.equal(token, bindings.GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_0);
            return foreignClient;
          },
          core: { setOutput: (name, value) => outputs.push({ name, value }), info: () => {} },
        });
        assert.equal(result.receipts[0].status, "durable");
        assert.equal(reads, 2);
        const submitted = fixture.transactions.find(commit => commit.request.kind === "submit" && commit.actor.role === "worker");
        assert.ok(submitted);
        const work = submitted.operations.find(operation => operation.kind === "Work");
        assert.deepEqual(work.depends_on[0].resource, { kind: "issue", host: "github.com", repository: foreign, repository_id: "20", resource_id: "30", number: "7" });
        assert.equal(submitted.actor.claim_handle, member.handle);
        assert.equal(outputs[0].name, "work_queue_requests");
        assert.equal(JSON.stringify(result).includes("separate-read-token"), false);
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });
  });
}

if (require.main === module) registerTests(require("node:test"));
module.exports = { registerTests };
