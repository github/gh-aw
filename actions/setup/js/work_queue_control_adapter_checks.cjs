"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { createCompilerDependencyResolver, credentialBindings, main, readOnlyRepositoryClient } = require("./work_queue_control_adapter.cjs");
const h = require("./work_queue_test_helpers.cjs");
const { replayTransactionLog } = require("./work_queue_replay.cjs");

const foreign = "foreign/design";
const bindings = { GH_AW_WORK_QUEUE_DEPENDENCY_READ_CREDENTIALS: JSON.stringify({ [foreign]: "GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_0" }), GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_0: "separate-read-token" };
const context = { repo: { owner: "owner", repo: "repo" }, runId: 100, runAttempt: 1, payload: {}, ref: "refs/heads/main" };
const POLICY = {
  version: 4,
  pending_limit: 30,
  issues: false,
  pools: { default: { concurrency: 2, per_account_limit: 0, max_attempts: 3, default_profile: "worker", profiles: { worker: { workflow: h.WORKER_WORKFLOW, effect_scope: "owner/repo" } } } },
};

function readerClient(name) {
  return { name, rest: { repos: { get: async input => ({ status: 200, data: { full_name: `${input.owner}/${input.repo}`, id: 1 } }) }, issues: { get: async () => ({}) }, pulls: { get: async () => ({}) } } };
}

function registerTests({ describe, it }) {
  describe("compiled work queue control adapter", () => {
    it("accepts only bounded dependency read credential bindings", () => {
      assert.deepEqual([...credentialBindings(bindings.GH_AW_WORK_QUEUE_DEPENDENCY_READ_CREDENTIALS).keys()], [foreign]);
      assert.equal(credentialBindings(undefined).size, 0);
      for (const raw of ['{"owner/repo":"TOKEN"}', '{"not a repo":"GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_0"}', "[]", '{"a/b":"GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_0","A/B":"GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_1"}'])
        assert.throws(() => credentialBindings(raw));
    });

    it("scopes dependency reads to the queue repository and bound repositories", async () => {
      const own = readerClient("own");
      const tokens = [];
      const resolver = createCompilerDependencyResolver({
        context,
        env: bindings,
        githubClient: own,
        getOctokit: (token, options) => {
          tokens.push([token, options]);
          return readerClient("foreign");
        },
      });
      assert.deepEqual(resolver.scopes, [
        { host: "github.com", repository: foreign },
        { host: "github.com", repository: "owner/repo" },
      ]);
      const ownReader = await resolver.getClient({ host: "github.com", repository: "owner/repo" });
      assert.equal((await ownReader.rest.repos.get({ owner: "owner", repo: "repo" })).data.full_name, "owner/repo");
      await assert.rejects(ownReader.rest.repos.get({ owner: "foreign", repo: "design" }), /scope_invalid/);
      await resolver.getClient({ host: "github.com", repository: foreign });
      assert.deepEqual(tokens, [["separate-read-token", { baseUrl: "https://api.github.com" }]]);
      await assert.rejects(resolver.getClient({ host: "github.com", repository: "other/repo" }), /not_allowlisted/);
      const unbound = createCompilerDependencyResolver({ context, env: { GH_AW_WORK_QUEUE_DEPENDENCY_READ_CREDENTIALS: bindings.GH_AW_WORK_QUEUE_DEPENDENCY_READ_CREDENTIALS }, githubClient: own });
      assert.equal(await unbound.getClient({ host: "github.com", repository: foreign }), null);
    });

    it("restricts read clients to GET calls of one repository", async () => {
      const calls = [];
      const client = { rest: { issues: { get: async input => (calls.push(input), { status: 200 }) } } };
      const scoped = readOnlyRepositoryClient(client, { repository: "owner/repo" });
      await scoped.rest.issues.get({ owner: "owner", repo: "repo", issue_number: 5 });
      assert.equal(calls[0].issue_number, 5);
      await assert.rejects(scoped.rest.issues.get({ owner: "owner", repo: "repo", issue_number: 5, state: "open" }), /scope_invalid/);
      assert.equal(Object.hasOwn(scoped.rest.issues, "update"), false);
    });

    it("runs the controls with the queue token client and the dispatch token client", async () => {
      const directory = fs.mkdtempSync(path.join(os.tmpdir(), "wq-adapter-"));
      const previous = process.env.GITHUB_WORKFLOW_REF;
      process.env.GITHUB_WORKFLOW_REF = "owner/repo/.github/workflows/dispatcher.lock.yml@refs/heads/main";
      try {
        const intentPath = path.join(directory, "intents.jsonl");
        fs.writeFileSync(intentPath, JSON.stringify({ version: 4, intent_id: "dispatch", kind: "dispatch_next", parameters: { pool: "default", max_claims: 2, max_dispatches: 2 } }) + "\n");
        const queue = h.fakeQueueGitHub(h.ledgerText(h.submit([], [h.work("a"), h.work("b"), h.work("c")])));
        // The dispatch-token client is the in-memory Actions API, which creates and reports each worker run.
        const actions = h.fakeActions();
        const posts = actions.posts;
        const dispatchClient = { rest: { actions: actions.actions } };
        const created = [];
        const outputs = new Map();
        const result = await main({
          context,
          env: { GH_AW_WORK_QUEUE_TOKEN: "queue-token", GH_AW_GITHUB_TOKEN_SOURCE: "github_actions" },
          github: readerClient("default"),
          getOctokit: (token, options) => {
            created.push([token, options]);
            return token === "queue-token" ? queue.githubClient : dispatchClient;
          },
          config: { "github-token": "dispatch-token", "target-ref": "refs/heads/main", workflows: ["worker"], workflow_files: { worker: ".lock.yml" }, work_queue_workflows: ["worker"] },
          core: { info() {}, warning() {}, setFailed: message => assert.fail(message), setOutput: (name, value) => outputs.set(name, value) },
          policy: POLICY,
          intentPath,
          maxDispatches: 2,
        });
        assert.deepEqual(created, [
          ["queue-token", { credentialSource: "github_actions" }],
          ["dispatch-token", { credentialSource: "github_actions" }],
        ]);
        assert.equal(result.success, true);
        assert.deepEqual(
          result.receipts[0]?.launches?.map(launch => launch.state),
          ["started", "started"]
        );
        assert.equal(posts.length, 2);
        assert.equal(JSON.parse(outputs.get("work_queue_requests")).version, 4);
        assert.equal([...replayTransactionLog(queue.contents()).dispatches.values()].filter(dispatch => dispatch.state === "started").length, 2);
      } finally {
        if (previous === undefined) delete process.env.GITHUB_WORKFLOW_REF;
        else process.env.GITHUB_WORKFLOW_REF = previous;
        fs.rmSync(directory, { recursive: true, force: true });
      }
    });

    it("rejects malformed compiler tokens", async () => {
      await assert.rejects(main({ context, env: { GH_AW_WORK_QUEUE_TOKEN: "bad\ntoken" }, github: readerClient("default"), getOctokit: () => ({}), config: {} }), /publisher_credential_token_invalid/);
    });
  });
}

module.exports = { registerTests };
