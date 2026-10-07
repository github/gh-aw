"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { temporaryDirectory } = require("./work_queue_effect_test_helpers.cjs");
const { createCompilerDependencyResolver, credentialBindings, main } = require("./work_queue_control_adapter.cjs");
const { queueFixture, WORKFLOW, DISPATCHER, REF, REPOSITORY } = require("./work_queue_lifecycle.test_helpers.cjs");
const { main: captureIntentOrigin } = require("./capture_work_queue_intent_origin.cjs");
const { canonical } = require("./work_queue_codec.cjs");
const { createDispatchCredentialValidator, resolveDispatchCredentialPrincipal, isDispatchCredentialProof } = require("./work_queue_dispatch_credential.cjs");

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
    it("maps default-token, compiler-minted App and PAT identities independently from logical requester and START sender", async () => {
      for (const entry of [
        { descriptor: { kind: "github_token" }, principal: "41898282", login: "github-actions[bot]", type: "Bot" },
        { descriptor: { kind: "github_app", app_slug: "approved-worker" }, principal: "888", login: "approved-worker[bot]", type: "Bot" },
        { descriptor: { kind: "authenticated" }, principal: "999", login: "approved-pat-owner", type: "User" },
      ]) {
        const calls = [];
        const native = async parameters => {
          calls.push(parameters);
          return { status: 200, data: { id: entry.principal, login: entry.login, type: entry.type } };
        };
        const client = { auth: async () => ({ type: "token", token: "protected-selected-token" }), rest: { users: { getByUsername: native, getAuthenticated: native } } };
        const validate = createDispatchCredentialValidator(client, entry.descriptor, "protected-selected-token");
        const proof = await validate({ profile: { principal: entry.principal } });
        assert.equal(isDispatchCredentialProof(proof, client, { principal: entry.principal }), true);
        assert.equal(isDispatchCredentialProof({ ...proof }, client, { principal: entry.principal }), false);
        assert.equal(isDispatchCredentialProof(proof, {}, { principal: entry.principal }), false);
        assert.equal(isDispatchCredentialProof(proof, client, { principal: "11" }), false);
        for (const untrusted of [true, false, "22", JSON.stringify(proof)]) assert.equal(isDispatchCredentialProof(untrusted, client, { principal: entry.principal }), false);
        assert.equal(isDispatchCredentialProof(proof, client, { principal: entry.principal, workflow: "foreign" }), false);
        assert.equal(proof.principal, entry.principal);
        assert.notEqual(proof.principal, "11");
        assert.equal(calls.length, 1);
        assert.equal(calls[0].headers["X-GitHub-Api-Version"], "2026-03-10");
        assert.equal(calls[0].request.retries, 0);
        assert.equal(calls[0].request.timeout, 15000);
        if (entry.descriptor.kind === "authenticated") assert.equal(Object.hasOwn(calls[0], "username"), false);
        else assert.equal(calls[0].username, entry.login);
        await assert.rejects(validate({ profile: { principal: "11" } }), /credential_principal_mismatch/);
      }
    });

    it("rejects spoofed credential kinds, App metadata, human bot lookups, lossy IDs and unavailable identity APIs without a fallback", async () => {
      let reads = 0;
      const client = {
        rest: {
          users: {
            getByUsername: async () => {
              reads++;
              return { status: 200, data: { id: "11", login: "human-requester", type: "User" } };
            },
            getAuthenticated: async () => {
              reads++;
              return { status: 403, data: { id: "11", type: "User" } };
            },
          },
        },
      };
      for (const descriptor of [null, {}, { kind: "github_token", principal: "11" }, { kind: "github_token", app_slug: "spoof" }, { kind: "github_app" }, { kind: "github_app", app_slug: "${{ github.actor }}" }, { kind: "human" }]) {
        await assert.rejects(resolveDispatchCredentialPrincipal(client, descriptor));
        assert.equal(reads, 0);
      }
      await assert.rejects(resolveDispatchCredentialPrincipal(client, { kind: "github_token" }), /identity_unverified/);
      await assert.rejects(resolveDispatchCredentialPrincipal(client, { kind: "github_app", app_slug: "approved-worker" }), /identity_unverified/);
      await assert.rejects(resolveDispatchCredentialPrincipal(client, { kind: "authenticated" }), /identity_unverified/);
      assert.equal(reads, 3);
      await assert.rejects(resolveDispatchCredentialPrincipal({}, { kind: "authenticated" }), /identity_unavailable/);
      for (const id of ["0", "01", "compiler", "${{ github.actor_id }}", Number.MAX_SAFE_INTEGER + 1]) {
        const spoof = { rest: { users: { getAuthenticated: async () => ({ status: 200, data: { id, type: "User" } }) } } };
        await assert.rejects(resolveDispatchCredentialPrincipal(spoof, { kind: "authenticated" }), /canonical integer/);
      }
    });

    it("selects the compiled default, App or PAT client for launches while preserving original requester and current START sender", async () => {
      for (const entry of [
        { descriptor: { kind: "github_token" }, principal: "41898282", login: "github-actions[bot]", type: "Bot" },
        { descriptor: { kind: "github_app", app_slug: "approved-worker" }, principal: "888", login: "approved-worker[bot]", type: "Bot" },
        { descriptor: { kind: "authenticated" }, principal: "999", login: "approved-pat-owner", type: "User" },
      ]) {
        const root = temporaryDirectory("actual-dispatch-credential");
        const fixture = queueFixture({ granted: false, count: 1, workerPrincipal: entry.principal });
        fixture.dispatcherContext.runAttempt = 2;
        let identityReads = 0;
        let posts = 0;
        let workerReads = 0;
        const identity = async () => {
          identityReads++;
          return { status: 200, data: { id: entry.principal, login: entry.login, type: entry.type } };
        };
        const dispatchClient = {
          auth: async () => ({ type: "token", token: "protected-selected-token" }),
          rest: {
            users: { getByUsername: identity, getAuthenticated: identity },
            repos: {
              get: async parameters => {
                assert.equal(parameters.owner, "owner");
                assert.equal(parameters.repo, "repo");
                return { status: 200, data: { id: 7, full_name: REPOSITORY } };
              },
            },
            actions: {
              createWorkflowDispatch: async parameters => {
                posts++;
                assert.equal(identityReads, 1, "selected launch actor must be proved before START and POST");
                const dispatch = [...fixture.state.dispatches.values()][0];
                assert.equal(dispatch.state, "started");
                assert.equal(dispatch.sender.principal, "11");
                assert.equal(dispatch.sender.run_attempt, 2);
                assert.equal(parameters.ref, REF);
                return { status: 200, data: { workflow_run_id: "42", run_url: "https://api.github.com/repos/owner/repo/actions/runs/42", html_url: "https://github.com/owner/repo/actions/runs/42" } };
              },
              getWorkflowRun: async parameters => {
                workerReads++;
                assert.equal(parameters.run_id, "42");
                const dispatch = [...fixture.state.dispatches.values()][0];
                return { status: 200, data: { ...fixture.nativeRun(), display_title: `gh-aw work-queue ${dispatch.dispatch_id}` } };
              },
            },
          },
        };
        fixture.githubClient.rest.actions.createWorkflowDispatch = async () => {
          throw new Error("publisher credential must not dispatch");
        };
        try {
          const intentPath = path.join(root, "intents.jsonl");
          fs.writeFileSync(intentPath, `${JSON.stringify({ version: 3, intent_id: "launch-credential", kind: "dispatch_next", parameters: { pool: "default", max_claims: 1, max_dispatches: 1 } })}\n`);
          const result = await main({
            assignment: null,
            env: {},
            intentPath,
            githubClient: fixture.githubClient,
            context: fixture.dispatcherContext,
            workflowRef: `${REPOSITORY}/${DISPATCHER}@${REF}`,
            intentOrigin: fixture.dispatcher,
            readWorkQueueLog: fixture.readWorkQueueLog,
            publishWorkQueueRequest: fixture.publishWorkQueueRequest,
            getOctokit: token => {
              assert.equal(token, "protected-selected-token");
              return dispatchClient;
            },
            config: { work_queue_enabled: true, work_queue_workflows: ["worker"], aw_context_workflows: ["worker"], "github-token": "protected-selected-token", work_queue_dispatch_credential: entry.descriptor },
            now: fixture.at,
            sleepFn: async () => {},
            core: { setOutput: () => {}, info: () => {} },
          });
          assert.equal(result.receipts[0].status, "durable", JSON.stringify(result));
          assert.equal(posts, 1);
          assert.equal(workerReads, 1);
          assert.equal(identityReads, 1);
          const dispatch = [...fixture.state.dispatches.values()][0];
          assert.equal(dispatch.state, "bound");
          assert.equal(dispatch.run.principal, entry.principal);
          assert.equal(dispatch.run.run_attempt, 1);
          assert.equal(dispatch.sender.run_attempt, 2);
          const grant = fixture.transactions.find(commit => commit.request.kind === "dispatch_next");
          assert.equal(grant.actor.run_attempt, 1, "immutable logical requester is not current START sender");
          assert.equal(grant.actor.principal, "11");
        } finally {
          fs.rmSync(root, { recursive: true, force: true });
        }
      }
    });

    it("rejects a launch token owned by the logical human before START or POST rather than trusting the frozen profile", async () => {
      const root = temporaryDirectory("spoofed-dispatch-credential");
      const fixture = queueFixture({ granted: false, count: 1, workerPrincipal: "22" });
      let posts = 0;
      let identityReads = 0;
      try {
        const intentPath = path.join(root, "intents.jsonl");
        fs.writeFileSync(intentPath, `${JSON.stringify({ version: 3, intent_id: "wrong-launch-owner", kind: "dispatch_next", parameters: { pool: "default", max_claims: 1, max_dispatches: 1 } })}\n`);
        const result = await main({
          assignment: null,
          env: {},
          intentPath,
          githubClient: fixture.githubClient,
          context: fixture.dispatcherContext,
          workflowRef: `${REPOSITORY}/${DISPATCHER}@${REF}`,
          intentOrigin: fixture.dispatcher,
          readWorkQueueLog: fixture.readWorkQueueLog,
          publishWorkQueueRequest: fixture.publishWorkQueueRequest,
          getOctokit: () => ({
            rest: {
              users: {
                getAuthenticated: async () => {
                  identityReads++;
                  return { status: 200, data: { id: "11", login: "human-requester", type: "User" } };
                },
              },
              actions: {
                createWorkflowDispatch: async () => {
                  posts++;
                  throw new Error("wrong principal must not launch");
                },
              },
            },
          }),
          config: { work_queue_enabled: true, work_queue_workflows: ["worker"], aw_context_workflows: ["worker"], "github-token": "wrong-owner-token", work_queue_dispatch_credential: { kind: "authenticated" } },
          now: fixture.at,
          sleepFn: async () => {},
          core: { setOutput: () => {}, info: () => {} },
        });
        assert.equal(result.receipts[0].status, "blocked", JSON.stringify(result));
        assert.equal(identityReads, 1);
        assert.equal(posts, 0);
        assert.equal([...fixture.state.dispatches.values()][0]?.state, "reserved");
        assert.equal(
          fixture.transactions.some(commit => commit.operations.some(operation => operation.kind === "Dispatch" && operation.state === "started")),
          false
        );
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });

    it("requires protected credential metadata on compiled controls but consumes no launch credential in preview", async () => {
      const root = temporaryDirectory("dispatch-credential-preview");
      const fixture = queueFixture({ granted: false, count: 1, workerPrincipal: "22" });
      let credentialCalls = 0;
      let writes = 0;
      try {
        const intentPath = path.join(root, "intents.jsonl");
        fs.writeFileSync(intentPath, `${JSON.stringify({ version: 3, intent_id: "credential-preview", kind: "dispatch_next", parameters: { pool: "default", max_claims: 1, max_dispatches: 1 } })}\n`);
        const options = {
          assignment: null,
          env: {},
          intentPath,
          githubClient: fixture.githubClient,
          dispatchClient: fixture.githubClient,
          context: fixture.dispatcherContext,
          workflowRef: `${REPOSITORY}/${DISPATCHER}@${REF}`,
          intentOrigin: fixture.dispatcher,
          readWorkQueueLog: fixture.readWorkQueueLog,
          publishWorkQueueRequest: async () => {
            writes++;
            throw new Error("preview or missing binding must not publish");
          },
          getOctokit: () => {
            credentialCalls++;
            throw new Error("preview must not consume credentials");
          },
          config: { work_queue_enabled: true, work_queue_workflows: ["worker"], aw_context_workflows: ["worker"] },
          now: fixture.at,
          sleepFn: async () => {},
          core: { setOutput: () => {}, info: () => {} },
        };
        await assert.rejects(main(options), /dispatch_credential_binding_required/);
        const result = await main({ ...options, staged: true, config: { ...options.config, "github-token": "", work_queue_dispatch_credential: { kind: "agent-spoof" } } });
        assert.equal(result.receipts[0].status, "staged_preview", JSON.stringify(result));
        assert.equal(credentialCalls, 0);
        assert.equal(writes, 0);
        assert.equal(fixture.state.dispatches.size, 0);
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });

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
      const root = temporaryDirectory("control-adapter");
      const fixture = queueFixture({ bound: true, count: 1 });
      assert.ok(fixture.assignment);
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
          workflowRef: `${REPOSITORY}/${WORKFLOW}@${REF}`,
          intentOrigin: { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id },
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

    it("preserves captured agent origin across publisher retries but admits genuinely new agent-attempt intents", async () => {
      const root = temporaryDirectory("original-intent-origin");
      const fixture = queueFixture({ granted: false, count: 2, workerPrincipal: "22" });
      const keys = ["GH_AW_WORK_QUEUE_ENABLED", "GH_AW_WORK_QUEUE_ROLE", "GH_AW_WORK_QUEUE_INTENT_ORIGIN"];
      const previous = Object.fromEntries(keys.map(key => [key, process.env[key]]));
      const actions = fixture.githubClient.rest.actions;
      const getRun = actions.getWorkflowRun;
      let posts = 0;
      let originalAttemptReads = 0;
      const publicationErrors = [];
      actions.getWorkflowRun = async input => {
        const response = await getRun(input);
        if (input.run_id === "42") {
          const dispatch = [...fixture.state.dispatches.values()][0];
          response.data.display_title = `gh-aw work-queue ${dispatch.dispatch_id}`;
        }
        return response;
      };
      const getAttempt = actions.getWorkflowRunAttempt;
      actions.getWorkflowRunAttempt = async input => {
        if (input.run_id === "15" && input.attempt_number === 1) originalAttemptReads++;
        return getAttempt(input);
      };
      actions.createWorkflowDispatch = async () => {
        posts++;
        return { status: 200, data: { workflow_run_id: "42", run_url: "https://api.github.com/repos/owner/repo/actions/runs/42", html_url: "https://github.com/owner/repo/actions/runs/42" } };
      };
      Object.assign(fixture.githubClient.rest, { users: { getAuthenticated: async () => ({ status: 200, data: { id: "22", login: "approved-worker", type: "User" } }) } });
      try {
        process.env.GH_AW_WORK_QUEUE_ENABLED = "true";
        process.env.GH_AW_WORK_QUEUE_ROLE = "dispatcher";
        process.env.GH_AW_WORK_QUEUE_INTENT_ORIGIN = JSON.stringify({ role: "administrator", principal: "999", repository: "foreign/repo" });
        const outputs = new Map();
        const captureOptions = {
          githubClient: fixture.githubClient,
          context: fixture.dispatcherContext,
          workflowRef: `${REPOSITORY}/${DISPATCHER}@${REF}`,
          core: {
            setOutput: (name, value) => outputs.set(name, value),
            exportVariable: (name, value) => {
              process.env[name] = value;
            },
          },
        };
        await captureIntentOrigin(captureOptions);
        assert.deepEqual(JSON.parse(outputs.get("work_queue_origin")), fixture.dispatcher);
        const filename = path.join(root, "intents.jsonl");
        const bytes = `${JSON.stringify({ version: 3, intent_id: "same-serialized-intent", kind: "dispatch_next", parameters: { pool: "default", max_claims: 1, max_dispatches: 1 } })}\n`;
        fs.writeFileSync(filename, bytes);
        const options = {
          assignment: null,
          env: {},
          intentPath: filename,
          githubClient: fixture.githubClient,
          dispatchClient: fixture.githubClient,
          context: fixture.dispatcherContext,
          workflowRef: `${REPOSITORY}/${DISPATCHER}@${REF}`,
          readWorkQueueLog: fixture.readWorkQueueLog,
          publishWorkQueueRequest: async input => {
            try {
              return await fixture.publishWorkQueueRequest(input);
            } catch (error) {
              publicationErrors.push(error);
              throw error;
            }
          },
          config: { work_queue_enabled: true, work_queue_workflows: ["worker"], aw_context_workflows: ["worker"], "github-token": "protected-worker-token", work_queue_dispatch_credential: { kind: "authenticated" } },
          getOctokit: () => fixture.githubClient,
          maxDispatches: 1,
          now: fixture.at,
          sleepFn: async () => {},
          core: { setOutput: () => {}, info: () => {} },
        };
        const first = await main(options);
        assert.deepEqual(publicationErrors, []);
        assert.equal(first.receipts[0].status, "durable", JSON.stringify(first));
        assert.equal(fixture.state.claims.size, 1);
        assert.equal(posts, 1);
        fixture.dispatcherContext.runAttempt = 2;
        const before = JSON.stringify(fixture.transactions);
        const recovered = await main(options);
        assert.equal(recovered.receipts[0].request_id, first.receipts[0].request_id);
        assert.equal(recovered.receipts[0].status, "durable", JSON.stringify(recovered));
        assert.equal(fixture.state.claims.size, 1);
        assert.equal(posts, 1);
        assert.ok(originalAttemptReads > 0);
        assert.equal(JSON.stringify(fixture.transactions), before);
        assert.equal(fs.readFileSync(filename, "utf8"), bytes);
        delete process.env.GH_AW_WORK_QUEUE_INTENT_ORIGIN;
        const unproven = await main(options);
        assert.equal(unproven.receipts[0].status, "blocked");
        assert.equal(fixture.state.claims.size, 1);
        assert.equal(posts, 1);
        await captureIntentOrigin(captureOptions);
        assert.equal(JSON.parse(outputs.get("work_queue_origin")).run_attempt, 2);
        const getOriginalRun = actions.getWorkflowRun;
        actions.getWorkflowRun = async input => {
          if (input.run_id !== "43") return getOriginalRun(input);
          const dispatch = [...fixture.state.dispatches.values()].at(-1);
          assert.ok(dispatch);
          return {
            status: 200,
            data: { ...fixture.nativeRun({ ...fixture.workerContext, runId: "43" }), path: WORKFLOW, display_title: `gh-aw work-queue ${dispatch.dispatch_id}` },
          };
        };
        actions.createWorkflowDispatch = async () => {
          posts++;
          return { status: 200, data: { workflow_run_id: "43", run_url: "https://api.github.com/repos/owner/repo/actions/runs/43", html_url: "https://github.com/owner/repo/actions/runs/43" } };
        };
        fs.writeFileSync(filename, `${JSON.stringify({ version: 3, intent_id: "new-agent-attempt-intent", kind: "dispatch_next", parameters: { pool: "default", max_claims: 1, max_dispatches: 1 } })}\n`);
        const fresh = await main({ ...options, now: fixture.at });
        assert.deepEqual(publicationErrors, []);
        assert.equal(fresh.receipts[0].status, "durable", JSON.stringify(fresh));
        assert.notEqual(fresh.receipts[0].request_id, first.receipts[0].request_id);
        assert.equal(fixture.state.claims.size, 2);
        assert.equal(posts, 2);
        const freshDispatch = [...fixture.state.dispatches.values()].at(-1);
        assert.ok(freshDispatch);
        assert.equal(freshDispatch.sender.principal, "11");
        assert.equal(freshDispatch.sender.run_attempt, 2);
        assert.equal(freshDispatch.run.principal, "22");
        assert.equal(freshDispatch.run.run_id, "43");
        assert.equal(freshDispatch.run.run_attempt, 1);
      } finally {
        for (const key of keys) {
          if (previous[key] === undefined) delete process.env[key];
          else process.env[key] = previous[key];
        }
        fs.rmSync(root, { recursive: true, force: true });
      }
    });

    it("captures original native worker identity without writing the queue and refuses worker reruns or observer promotion", async () => {
      const fixture = queueFixture({ bound: true, count: 2, workerPrincipal: "22" });
      assert.ok(fixture.assignment);
      const before = JSON.stringify(fixture.transactions);
      const emitted = [];
      const exported = [];
      const options = {
        githubClient: fixture.githubClient,
        context: fixture.workerContext,
        workflowRef: `${REPOSITORY}/${WORKFLOW}@${REF}`,
        role: "worker",
        core: { setOutput: (name, value) => emitted.push([name, value]), exportVariable: (name, value) => exported.push([name, value]) },
      };
      const actor = await captureIntentOrigin(options);
      assert.equal(actor.principal, "22");
      assert.equal(actor.run_attempt, 1);
      assert.equal(actor.dispatch_id, fixture.assignment.dispatch_id);
      assert.equal(Object.hasOwn(actor, "claim_handle"), false);
      assert.deepEqual(emitted, [["work_queue_origin", canonical(actor)]]);
      assert.deepEqual(exported, [["GH_AW_WORK_QUEUE_INTENT_ORIGIN", canonical(actor)]]);
      assert.equal(JSON.stringify(fixture.transactions), before);
      fixture.workerContext.runAttempt = 2;
      await assert.rejects(captureIntentOrigin(options), /rerun_not_authorized/);
      await assert.rejects(captureIntentOrigin({ ...options, context: fixture.dispatcherContext, role: "observer" }), /work_queue_observer_read_only/);
      await assert.rejects(captureIntentOrigin({ ...options, context: fixture.dispatcherContext }), /work_queue_assignment_required/);
      await assert.rejects(captureIntentOrigin({ ...options, core: {} }), /work_queue_origin_output_unavailable/);
      assert.equal(emitted.length, 1);
      assert.equal(exported.length, 1);
      assert.equal(JSON.stringify(fixture.transactions), before);
    });

    it("wires the emitted runtime entrypoint into actual completed-Claim foreign admission with immutable identities and no foreign write authority", async () => {
      const root = temporaryDirectory("control-adapter");
      const fixture = queueFixture({ bound: true, count: 1, workerPrincipal: "22", configurePolicy: policy => policy.pools.default.allowed_repositories.push(foreign) });
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
          intentOrigin: { ...fixture.workerActor, dispatch_id: assignment.dispatch_id },
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
        assert.equal(submitted.actor.principal, "22");
        assert.equal(fixture.policy.producers["22"], undefined);
        assert.equal(outputs[0].name, "work_queue_requests");
        assert.equal(JSON.stringify(result).includes("separate-read-token"), false);
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });
  });
}

if (require.main === module) registerTests(require("node:test"));
async function verifyCompiledDispatchIdentity(input) {
  const { policyProposalFor, loadQueue } = require("./work_queue_binding.cjs");
  const { setupGlobals } = require("./setup_globals.cjs");
  const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
  const dispatcherRevision = "b".repeat(40);
  const dispatcherWorkflowRef = `${REPOSITORY}/${DISPATCHER}@${dispatcherRevision}`;
  assert.equal(input.policy.pools.default.profiles.default.principal, input.principal);
  assert.equal(input.policy.pools.default.profiles.default.workflow, WORKFLOW);
  assert.equal(input.policy.pools.default.profiles.default.ref, REF);
  assert.notEqual(dispatcherRevision, REF);
  assert.deepEqual(Object.keys(input.policy.producers).sort(), ["11", "12"]);
  for (const mode of ["verified", "wrong-publisher", "wrong-credential", "wrong-worker", "wrong-triggering-actor", "wrong-worker-revision", "wrong-worker-workflow"]) {
    const root = temporaryDirectory("compiled-dispatch-identity");
    const keys = [
      "GH_AW_WORK_QUEUE_ENABLED",
      "GH_AW_WORK_QUEUE_ROLE",
      "GH_AW_WORK_QUEUE_POLICY",
      "GH_AW_WORK_QUEUE_SNAPSHOT",
      "GH_AW_WORK_QUEUE_INTENT_ORIGIN",
      "GH_AW_WORK_QUEUE_INTENTS",
      "GH_AW_WORK_QUEUE_CONTROL_CONFIG",
      "GH_AW_WORK_QUEUE_DISPATCH_BUDGET",
      "GITHUB_SHA",
      "RUNNER_TEMP",
    ];
    const previous = new Map(keys.map(key => [key, process.env[key]]));
    const fixture = queueFixture({ granted: false, count: 1, workerPrincipal: input.principal, configurePolicy: policy => Object.assign(policy, structuredClone(input.policy)) });
    fixture.dispatcherContext.sha = dispatcherRevision;
    let identityReads = 0;
    let posts = 0;
    let result;
    const core = {
      setOutput() {},
      info() {},
      warning() {},
      error() {},
      exportVariable(name, value) {
        process.env[name] = value;
      },
    };
    const login = input.kind === "github_token" ? "github-actions[bot]" : input.kind === "github_app" ? "approved-worker[bot]" : "approved-pat-owner";
    const identity = async parameters => {
      identityReads++;
      if (input.kind === "authenticated") assert.equal(Object.hasOwn(parameters, "username"), false);
      else assert.equal(parameters.username, login);
      return { status: 200, data: { id: mode === "wrong-credential" ? "11" : input.principal, login, type: input.kind === "authenticated" ? "User" : "Bot" } };
    };
    const dispatchClient = {
      auth: async () => ({ type: "token", token: "protected-selected-token" }),
      hook: { before() {} },
      rest: {
        users: { getByUsername: identity, getAuthenticated: identity },
        repos: fixture.githubClient.rest.repos,
        actions: {
          createWorkflowDispatch: async parameters => {
            posts++;
            assert.equal(identityReads, 1);
            const dispatch = [...fixture.state.dispatches.values()][0];
            assert.equal(dispatch.state, "started");
            assert.equal(dispatch.sender.principal, "11");
            assert.equal(dispatch.sender.run_attempt, 2);
            assert.equal(parameters.ref, REF);
            assert.equal(parameters.workflow_id, WORKFLOW);
            assert.notEqual(parameters.ref, fixture.dispatcherContext.sha);
            return { status: 200, data: { workflow_run_id: "42", run_url: "https://api.github.com/repos/owner/repo/actions/runs/42", html_url: "https://github.com/owner/repo/actions/runs/42" } };
          },
          getWorkflowRun: async () => {
            const dispatch = [...fixture.state.dispatches.values()][0];
            const data = { ...fixture.nativeRun(), display_title: `gh-aw work-queue ${dispatch.dispatch_id}` };
            if (mode === "wrong-worker") data.actor = { id: "11" };
            if (mode === "wrong-triggering-actor") data.triggering_actor = { id: "11" };
            if (mode === "wrong-worker-revision") data.head_sha = dispatcherRevision;
            if (mode === "wrong-worker-workflow") data.path = DISPATCHER;
            return { status: 200, data };
          },
        },
      },
    };
    fixture.githubClient.hook = { before() {} };
    fixture.githubClient.rest.actions.createWorkflowDispatch = async () => {
      throw new Error("The ledger publisher credential must not launch workers");
    };
    fixture.githubClient.rest.actions.getWorkflowRun = async parameters => ({
      status: 200,
      data: parameters.run_id === "15" ? { ...fixture.nativeRun(fixture.dispatcherContext), head_sha: dispatcherRevision } : fixture.nativeRun(),
    });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = async parameters => {
      assert.equal(parameters.attempt_number, 1);
      return {
        status: 200,
        data: parameters.run_id === "15" ? { ...fixture.nativeRun({ ...fixture.dispatcherContext, actorId: "11", runAttempt: 1 }), head_sha: dispatcherRevision } : fixture.nativeRun(),
      };
    };
    try {
      process.env.RUNNER_TEMP = root;
      process.env.GITHUB_SHA = dispatcherRevision;
      process.env.GH_AW_WORK_QUEUE_ENABLED = "true";
      process.env.GH_AW_WORK_QUEUE_ROLE = "dispatcher";
      process.env.GH_AW_WORK_QUEUE_POLICY = JSON.stringify(input.policy);
      process.env.GH_AW_WORK_QUEUE_SNAPSHOT = path.join(root, "work-queue.snapshot.json");
      process.env.GH_AW_WORK_QUEUE_DISPATCH_BUDGET = "1";
      delete process.env.GH_AW_WORK_QUEUE_INTENT_ORIGIN;
      assert.equal(canonical(policyProposalFor({})), canonical(input.policy));
      assert.equal(canonical((await loadQueue({ context: fixture.dispatcherContext, githubClient: fixture.githubClient, readWorkQueueLog: fixture.readWorkQueueLog })).projection.policy), canonical(input.policy));
      const snapshot = await require("./write_work_queue_snapshot.cjs").main({
        core,
        githubClient: fixture.githubClient,
        context: fixture.dispatcherContext,
        role: "dispatcher",
        workflowRef: dispatcherWorkflowRef,
        readWorkQueueLog: fixture.readWorkQueueLog,
        now: fixture.at,
      });
      const origin = await captureIntentOrigin({ core, githubClient: fixture.githubClient, context: fixture.dispatcherContext, role: "dispatcher", workflowRef: dispatcherWorkflowRef });
      assert.equal(canonical(snapshot.origin), canonical(origin));
      assert.equal(origin.principal, "11");
      assert.equal(origin.run_attempt, 1);
      fixture.dispatcherContext.runAttempt = 2;
      fixture.dispatcherContext.actorId = mode === "wrong-publisher" ? "12" : "11";
      const intentPath = path.join(root, "intents.jsonl");
      fs.writeFileSync(intentPath, `${JSON.stringify({ version: 3, intent_id: "compiled-launch", kind: "dispatch_next", parameters: { pool: "default", max_claims: 1, max_dispatches: 1 } })}\n`);
      process.env.GH_AW_WORK_QUEUE_INTENTS = intentPath;
      process.env.GH_AW_WORK_QUEUE_CONTROL_CONFIG = JSON.stringify(input.config);
      const localRequire = module => {
        if (require("node:module").builtinModules.includes(module)) return require(module);
        if (module.endsWith("/setup_globals.cjs")) return { setupGlobals };
        if (module.endsWith("/work_queue_control_adapter.cjs")) {
          return {
            main: async options => {
              result = await main({
                ...options,
                env: {},
                workflowRef: dispatcherWorkflowRef,
                readWorkQueueLog: fixture.readWorkQueueLog,
                publishWorkQueueRequest: fixture.publishWorkQueueRequest,
                now: fixture.at,
                sleepFn: async () => {},
              });
              return result;
            },
          };
        }
        throw new Error(`Unexpected compiled runtime module: ${module}`);
      };
      const getOctokit = token => {
        assert.equal(token, "protected-selected-token");
        return dispatchClient;
      };
      await new AsyncFunction("require", "core", "github", "context", "exec", "io", "getOctokit", input.script)(localRequire, core, fixture.githubClient, fixture.dispatcherContext, {}, {}, getOctokit);
      assert.equal(canonical(fixture.state.policy), canonical(input.policy));
      if (mode === "wrong-publisher") {
        assert.equal(identityReads, 0);
        assert.equal(posts, 0);
        assert.equal(result.receipts[0].status, "blocked", JSON.stringify(result));
        assert.equal(fixture.state.dispatches.size, 0);
        continue;
      }
      assert.equal(identityReads, 1, JSON.stringify(result));
      const dispatch = [...fixture.state.dispatches.values()][0];
      const grant = fixture.transactions.find(commit => commit.request.kind === "dispatch_next");
      assert.equal(grant.actor.principal, "11");
      assert.equal(grant.actor.run_attempt, 1);
      if (mode === "verified") {
        assert.equal(result.receipts[0].status, "durable", JSON.stringify(result));
        assert.equal(posts, 1);
        assert.equal(dispatch.state, "bound");
        assert.equal(dispatch.run.principal, input.principal);
        assert.equal(dispatch.run.run_attempt, 1);
        assert.equal(dispatch.sender.principal, "11");
        assert.equal(dispatch.sender.run_attempt, 2);
      } else if (mode === "wrong-credential") {
        assert.equal(posts, 0);
        assert.equal(result.receipts[0].status, "blocked", JSON.stringify(result));
        assert.equal(dispatch.state, "reserved");
        assert.equal(
          fixture.transactions.some(commit => commit.operations.some(operation => operation.kind === "Dispatch" && operation.state === "started")),
          false
        );
      } else {
        assert.equal(posts, 1);
        assert.notEqual(dispatch.state, "bound", JSON.stringify(result));
        assert.equal(dispatch.run, undefined);
      }
    } finally {
      for (const [key, value] of previous) {
        if (value === undefined) delete process.env[key];
        else process.env[key] = value;
      }
      fs.rmSync(root, { recursive: true, force: true });
    }
  }
}

module.exports = { registerTests, verifyCompiledDispatchIdentity };
