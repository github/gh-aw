"use strict";

const assert = require("node:assert/strict");
const { createHash } = require("node:crypto");
const { canonical } = require("./work_queue_codec.cjs");
const { generateRequestOperations, newRequest, parseTransactionLog, replayTransactions, serializeProjection, serializeTransactionLog } = require("./work_queue_replay.cjs");
const { freshAuthorizer, initializeWorkQueue, publishWorkQueueRequest, readWorkQueueLog } = require("./work_queue_store.cjs");
const { defaultPolicy } = require("./work_queue_policy.cjs");
const { newChildWork, newWork } = require("./work_queue_graph.cjs");
const { planDispatch } = require("./work_queue_scheduler.cjs");
const { administrator, bind, commit, context, dispatcher, evidence, finish, genesis, grant, operationCommit, producer, reconciler, submission, workerActor } = require("./work_queue_test_helpers.cjs");
const resourceFixture = require("../../../specs/work-queue/fixtures/resource-scope.json");
const contractFixture = require("../../../specs/work-queue/fixtures/contract.json");

function fakeGitHub(initial = []) {
  let serial = 0;
  const blobs = new Map();
  const trees = new Map();
  const commits = new Map();
  const refs = new Map();
  /** @type {{calls: string[], beforeUpdate: ((snapshot: {log: ReturnType<typeof log>, candidate: ReturnType<typeof log>, install: typeof install}) => Promise<unknown>) | null, ambiguousOnce: boolean, visibility: boolean, defaultRevision: string, workerContent: string | null, workerWorkflow: {id: number, path: string, state: string} | null, missingLog: boolean, truncated: boolean, badBlob: {encoding: string, content: string} | null, emptyLog: boolean, updates: number}} */
  const state = {
    calls: [],
    beforeUpdate: null,
    ambiguousOnce: false,
    visibility: true,
    defaultRevision: "a".repeat(40),
    workerContent: "on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\n        required: true\n",
    workerWorkflow: { id: 42, path: ".github/workflows/worker.lock.yml", state: "active" },
    missingLog: false,
    truncated: false,
    badBlob: null,
    emptyLog: false,
    updates: 0,
  };
  const id = prefix => `${prefix}-${++serial}`;
  const missing = () => Object.assign(new Error("Not found"), { status: 404 });
  function install(log, branch = "work-queue") {
    const blob = id("blob");
    const tree = id("tree");
    const commit = id("commit");
    blobs.set(blob, serializeTransactionLog(log));
    trees.set(tree, blob);
    commits.set(commit, { tree, parents: [] });
    refs.set(branch, commit);
    return commit;
  }
  if (initial.length) install(initial);
  const log = () => {
    const head = refs.get("work-queue");
    return head ? parseTransactionLog(blobs.get(trees.get(commits.get(head).tree))) : [];
  };
  const githubClient = {
    rest: {
      repos: {
        get: async () => {
          state.calls.push("repos.get");
          if (!state.visibility) throw missing();
          return { data: { full_name: "owner/repo", default_branch: "main", size: 1 } };
        },
        getContent: async ({ owner, repo, path, ref }) => {
          assert.equal(`${owner}/${repo}`, "owner/repo");
          assert.equal(path, ".github/workflows/worker.lock.yml");
          assert.equal(ref, state.defaultRevision);
          state.calls.push(`getContent:${path}@${ref}`);
          if (state.workerContent === null) throw missing();
          const content = Buffer.from(state.workerContent, "utf8");
          return { data: { type: "file", path, sha: "d".repeat(40), encoding: "base64", content: content.toString("base64"), size: content.byteLength } };
        },
      },
      actions: {
        getWorkflow: async ({ owner, repo, workflow_id }) => {
          assert.equal(`${owner}/${repo}`, "owner/repo");
          assert.equal(workflow_id, "worker.lock.yml");
          state.calls.push(`getWorkflow:${workflow_id}`);
          if (state.workerWorkflow === null) throw missing();
          return { data: state.workerWorkflow };
        },
      },
      git: {
        getRef: async ({ ref }) => {
          state.calls.push(`getRef:${ref}`);
          if (ref === "heads/main") {
            if (!state.defaultRevision) throw missing();
            return { data: { object: { sha: state.defaultRevision } } };
          }
          const sha = refs.get(ref.slice("heads/".length));
          if (!sha) throw missing();
          return { data: { object: { sha } } };
        },
        getCommit: async ({ commit_sha }) => {
          if (!commits.has(commit_sha)) throw missing();
          return { data: { tree: { sha: commits.get(commit_sha).tree } } };
        },
        getTree: async ({ tree_sha }) => ({ data: { truncated: state.truncated, tree: state.missingLog ? [] : [{ path: "work-queue.jsonl", type: "blob", mode: "100644", sha: trees.get(tree_sha) }] } }),
        getBlob: async ({ file_sha }) => {
          const content = state.emptyLog ? "" : blobs.get(file_sha);
          return { data: state.badBlob || { encoding: "base64", content: Buffer.from(content, "utf8").toString("base64"), size: Buffer.byteLength(content) } };
        },
        createBlob: async ({ content, encoding }) => {
          assert.equal(encoding, "utf-8");
          const sha = id("blob");
          blobs.set(sha, content);
          return { data: { sha } };
        },
        createTree: async ({ tree }) => {
          assert.deepEqual(
            tree.map(item => [item.path, item.mode, item.type]),
            [["work-queue.jsonl", "100644", "blob"]]
          );
          const sha = id("tree");
          trees.set(sha, tree[0].sha);
          return { data: { sha } };
        },
        createCommit: async ({ tree, parents }) => {
          const sha = id("commit");
          commits.set(sha, { tree, parents });
          return { data: { sha } };
        },
        createRef: async ({ ref, sha }) => {
          const branch = ref.slice("refs/heads/".length);
          if (refs.has(branch)) throw Object.assign(new Error("Reference already exists"), { status: 422 });
          refs.set(branch, sha);
          return { data: {} };
        },
        updateRef: async ({ ref, sha, force }) => {
          state.updates++;
          assert.equal(force, false);
          const branch = ref.slice("heads/".length);
          if (state.beforeUpdate) {
            const callback = state.beforeUpdate;
            state.beforeUpdate = null;
            await callback({ log: log(), candidate: parseTransactionLog(blobs.get(trees.get(commits.get(sha).tree))), install });
          }
          if (!commits.get(sha).parents.includes(refs.get(branch))) throw Object.assign(new Error("Update is not a fast forward"), { status: 422 });
          refs.set(branch, sha);
          if (state.ambiguousOnce) {
            state.ambiguousOnce = false;
            throw Object.assign(new Error("Transport response lost"), { status: 502 });
          }
          return { data: {} };
        },
      },
    },
  };
  return { githubClient, state, refs, blobs, log, install };
}

/**
 * @param fake
 * @param request
 * @param {import("./work_queue_policy.cjs").QueueActor} [actor]
 * @param extra
 */
function options(fake, request, actor = dispatcher, extra = {}) {
  let serial = 0;
  return { githubClient: fake.githubClient, owner: "owner", repo: "repo", request, actor, context: context(actor), sleepFn: async () => {}, now: () => 100, commitId: () => `candidate-${request.id}-${++serial}`, ...extra };
}

/** @param {{kind: string, host: string, repository: string, repository_id: string, resource_id: string, number: string}} [subject] */
function authorizerFixture(payloads, subject = undefined) {
  const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
  policy.pools.default.profiles.default.max_claims = payloads.length;
  const initial = [genesis(policy)];
  initial.push(
    submission(
      initial,
      payloads.map((_, index) => `effect-${index}`),
      {
        transform(node, index) {
          node.payload = payloads[index];
          if (subject) node.subject = subject;
          return node;
        },
      }
    )
  );
  const granted = grant(initial, { max_claims: payloads.length });
  initial.push(granted.commit);
  const dispatchId = granted.assignments[0].dispatch_id;
  const log = bind(initial, dispatchId, { runId: "202" });
  for (const member of granted.assignments[0].claims) log.push(finish(log, dispatchId, member.handle, "completed"));
  const state = replayTransactions(log);
  const fake = fakeGitHub(log);
  const trusted = context(workerActor(state, dispatchId), { ref: "0".repeat(40), event: "workflow_dispatch" });
  return { fake, log, state, dispatchId, trusted, input: { githubClient: fake.githubClient, owner: "owner", repo: "repo", context: trusted } };
}

function nativeTarget(number = "7") {
  return { kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "1", resource_id: number === "7" ? "2" : "3", number };
}

function registerTests({ describe, it }) {
  describe("checked current-only Git queue CAS store", () => {
    it("validates canonical trusted principals before no-op generation or any queue API probe", async () => {
      const parameters = { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 };
      const request = newRequest("principal-no-op", "dispatch_next", dispatcher, parameters);
      const fake = fakeGitHub([genesis()]);
      let generations = 0;
      const generateOperations = () => {
        generations++;
        return { operations: [] };
      };
      const before = serializeTransactionLog(fake.log());
      const sha = fake.refs.get("work-queue");
      for (const principal of ["login", "0", "01", "-1", "+1", "1.5", "1e3", "1\n", "1\r", "\u0661", "9".repeat(257), "", 1001, null, true]) {
        await assert.rejects(publishWorkQueueRequest(options(fake, request, dispatcher, { context: context({ ...dispatcher, principal }), generateOperations })), { code: "actor_unauthorized" });
      }
      assert.equal(generations, 0);
      assert.deepEqual(fake.state.calls, []);
      assert.equal(fake.state.updates, 0);
      assert.equal(fake.refs.get("work-queue"), sha);
      assert.equal(serializeTransactionLog(fake.log()), before);
      for (const principal of ["1", "9007199254740993", "9".repeat(256)]) {
        const actor = { ...dispatcher, principal };
        const valid = newRequest(`principal-no-op-${principal.length}`, "dispatch_next", actor, parameters);
        const result = await publishWorkQueueRequest(options(fake, valid, actor, { generateOperations }));
        assert.equal(result.publishedNow, false);
        assert.equal(result.state.requests.has(valid.id), false);
      }
      assert.equal(generations, 3);
      assert.equal(fake.state.updates, 0);
      assert.equal(fake.refs.get("work-queue"), sha);
      assert.equal(serializeTransactionLog(fake.log()), before);
    });
    it("freshly authorizes every independent frozen Work resource scope without publishing", async () => {
      assert.equal(resourceFixture.cases.length, 47);
      for (const test of resourceFixture.cases) {
        const payload = { task: test.name, ...(Object.hasOwn(test, "scope") ? { resource_scope: test.scope } : {}) };
        const { fake, input, state } = authorizerFixture([payload], test.subject);
        const before = canonical(serializeProjection(state));
        const blobs = fake.blobs.size;
        const authorize = freshAuthorizer(input);
        if (test.valid) assert.equal((await authorize(test.target)).claim.handle, "h1", test.name);
        else await assert.rejects(authorize(test.target), error => error instanceof Error && "code" in error && error.code === "claim_scope_invalid", test.name);
        assert.equal(canonical(serializeProjection(replayTransactions(fake.log()))), before, test.name);
        assert.equal(fake.state.updates, 0, test.name);
        assert.equal(fake.blobs.size, blobs, test.name);
      }
    });
    it("captures original Claim and actual targets independently of caller or returned-data mutation", async () => {
      const { fake, input, trusted } = authorizerFixture([{ resource_scope: { version: 1, resources: [nativeTarget("7")] } }, { resource_scope: { version: 1, resources: [nativeTarget("8")] } }]);
      const authorize = freshAuthorizer(input);
      trusted.claim_handle = "h2";
      trusted.ref = "a".repeat(40);
      const target = nativeTarget("7");
      const reading = authorize(target);
      target.number = "8";
      const authority = await reading;
      assert.equal(authority.claim.handle, "h1");
      authority.work.payload.resource_scope.resources[0].number = "8";
      authority.dispatch.claims[0].work.resource_scope.resources[0].number = "8";
      assert.equal((await authorize(nativeTarget("7"))).claim.handle, "h1");
      await assert.rejects(authorize(nativeTarget("8")), /claim_scope_invalid/);
      assert.equal(fake.state.calls.filter(call => call === "repos.get").length, 3);
      assert.equal(fake.state.updates, 0);
    });
    it("re-reads delivery barriers for every authorization without suppressing an independent pending sibling", async () => {
      const { fake, input, state, log, dispatchId, trusted } = authorizerFixture([{ resource_scope: { version: 1, resources: [nativeTarget("7")] } }, { resource_scope: { version: 1, resources: [nativeTarget("8")] } }]);
      const authorize = freshAuthorizer(input);
      const sibling = freshAuthorizer({ ...input, context: { ...trusted, claim_handle: "h2" } });
      const target = nativeTarget("7");
      const authority = await authorize(target);
      const result = {
        kind: "Result",
        work_id: authority.work.work_id,
        claim_id: authority.claim.claim_id,
        completion_id: authority.work.completion_id,
        descriptor: { summary: "verified independently" },
        evidence: evidence(state, state.dispatches.get(dispatchId), "delivery", 50, { source: "verified_receipts", run_id: "202", run_attempt: 1, receipt: "trusted-receipt" }),
      };
      fake.install([...log, operationCommit(log, "verified-effect", "result", [result], reconciler, 50)]);
      await assert.rejects(authorize(target), /claim_effects_unauthorized/);
      assert.equal((await sibling(nativeTarget("8"))).claim.handle, "h2");
      assert.equal(fake.state.updates, 0);
    });
    it("rejects Release, prefix rewrites and malformed checked storage after an earlier authorization", async () => {
      for (const change of ["release", "rewind", "missing-log", "truncated-tree"]) {
        const target = nativeTarget();
        const { fake, input, log, state, dispatchId } = authorizerFixture([{ task: "fresh authority" }], target);
        const authorize = freshAuthorizer(input);
        await authorize(target);
        if (change === "release") {
          const terminal = evidence(state, state.dispatches.get(dispatchId), "terminal_run", 50, { run_id: "202", run_attempt: 1, status: "completed", conclusion: "success" });
          fake.install([...log, operationCommit(log, "fresh-release", "release", [{ kind: "Release", dispatch_id: dispatchId, evidence: terminal }], reconciler, 50)]);
        } else if (change === "rewind") fake.install(log.slice(0, 1));
        else if (change === "missing-log") fake.state.missingLog = true;
        else fake.state.truncated = true;
        await assert.rejects(authorize(target), change === "release" ? /claim_ineffective/ : /ledger_invalid/, change);
        assert.equal(fake.state.updates, 0);
      }
    });
    it("does not revive the original Claim when a drained queue installs another immutable Policy epoch", async () => {
      const target = nativeTarget();
      const { fake, input, log, state, dispatchId } = authorizerFixture([{ task: "original epoch" }], target);
      const authorize = freshAuthorizer(input);
      const authority = await authorize(target);
      const result = {
        kind: "Result",
        work_id: authority.work.work_id,
        claim_id: authority.claim.claim_id,
        completion_id: authority.work.completion_id,
        descriptor: { summary: "drained original epoch" },
        evidence: evidence(state, state.dispatches.get(dispatchId), "delivery", 50, { source: "verified_receipts", run_id: "202", run_attempt: 1, receipt: "trusted-epoch-receipt" }),
      };
      const drained = [...log, operationCommit(log, "drain-result", "result", [result], reconciler, 50)];
      const terminal = evidence(state, state.dispatches.get(dispatchId), "terminal_run", 51, { run_id: "202", run_attempt: 1, status: "completed", conclusion: "success" });
      drained.push(operationCommit(drained, "drain-release", "release", [{ kind: "Release", dispatch_id: dispatchId, evidence: terminal }], reconciler, 51));
      const operations = [{ kind: "Policy", epoch: "second", policy: defaultPolicy({ repository: "owner/repo", principal: "1001" }) }];
      drained.push(commit("drain-release", "install-second-policy", "policy", administrator, { operations }, operations, 52, "second"));
      fake.install(drained);
      await assert.rejects(authorize(target), /claim_ineffective: Claim belongs to a retired Policy epoch/);
      assert.equal(fake.state.updates, 0);
    });
    it("rejects invalid trusted origin and malformed or scope-bearing targets before any queue read", async () => {
      const { fake, input, trusted } = authorizerFixture([{ task: "original scope" }]);
      for (const context of [
        { ...trusted, authenticated: false },
        { ...trusted, role: "producer", roles: ["producer"] },
        { ...trusted, repository: "foreign/repo" },
        { ...trusted, run_attempt: 2 },
        { ...trusted, claim_handle: undefined },
        { ...trusted, event: "workflow_run" },
        { ...trusted, ref: "refs/heads/main" },
      ]) {
        assert.throws(() => freshAuthorizer({ ...input, context }));
      }
      const authorize = freshAuthorizer(input);
      for (const target of [null, {}, { repository: "owner/repo", claim_handle: "h2" }, { repository: "owner/repo", number: "07" }]) await assert.rejects(authorize(target), /claim_scope_invalid/);
      assert.deepEqual(fake.state.calls, []);
      assert.equal(fake.state.updates, 0);
    });
    it("exposes administrator-only racing genesis installation without overwriting installed or malformed histories", async () => {
      const fake = fakeGitHub();
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001", ref: fake.state.defaultRevision });
      const input = {
        githubClient: fake.githubClient,
        owner: "owner",
        repo: "repo",
        context: context(administrator),
        policyProposal: policy,
        maxRetries: 0,
        now: () => 100,
      };
      await assert.rejects(initializeWorkQueue({ ...input, context: context(dispatcher) }), error => error instanceof Error && "code" in error && error.code === "actor_unauthorized");
      assert.equal(fake.log().length, 0);
      const installed = await Promise.all([initializeWorkQueue(input), initializeWorkQueue(input)]);
      assert.equal(installed.filter(result => result.publishedNow).length, 1);
      assert.equal(installed.filter(result => result.recovered).length, 1);
      for (const result of installed) {
        assert.equal(result.transactions.length, 1);
        assert.equal(result.state.policy_epoch, "initial");
        assert.equal(canonical(result.state.policy), canonical(policy));
        assert.equal(typeof result.sha, "string");
      }
      const blobCount = fake.blobs.size;
      const anotherOrigin = await initializeWorkQueue({
        ...input,
        context: context({ ...administrator, workflow: ".github/workflows/bootstrap.lock.yml", run_id: "101", run_attempt: 1 }),
      });
      assert.equal(anotherOrigin.publishedNow, false);
      assert.equal(canonical(anotherOrigin.state.policy), canonical(policy));
      assert.equal(fake.blobs.size, blobCount);
      const existing = await initializeWorkQueue({ ...input, policyProposal: { ...policy, mode: "strict-priority" } });
      assert.equal(existing.publishedNow, false);
      assert.equal(canonical(existing.state.policy), canonical(policy));
      assert.equal(fake.blobs.size, blobCount);
      fake.state.defaultRevision = "not-an-immutable-revision";
      fake.state.workerContent = null;
      fake.state.workerWorkflow = null;
      const existingWithoutProposal = await initializeWorkQueue({ ...input, policyProposal: undefined });
      assert.equal(existingWithoutProposal.publishedNow, false);
      assert.equal(canonical(existingWithoutProposal.state.policy), canonical(policy));
      assert.equal(fake.blobs.size, blobCount);
      fake.state.missingLog = true;
      await assert.rejects(initializeWorkQueue(input), error => error instanceof Error && "code" in error && error.code === "ledger_invalid");
      assert.equal(fake.blobs.size, blobCount);
      assert.equal(fake.state.updates, 0);
    });
    it("requires private remediation proof for uncertain effects and revalidates it on every stale CAS", async () => {
      const fixture = require("../../../specs/work-queue/fixtures/canonical-prefix.json");
      const initial = fixture.commits;
      const state = replayTransactions(initial);
      const failed = [...state.works.values()].find(work => work.barrier === "failed");
      const actor = { ...initial[0].actor, role: "producer" };
      const node = {
        ...newWork({ task: "inspect uncertain effects" }, failed.graph_id, "trusted-replacement", failed.pool, state.policy, 100),
        priority: failed.priority,
        fairness_key: failed.fairness_key,
        replacement_of: { work_id: failed.work_id, disposition: "inspection", evidence: "out_of_band_proof" },
      };
      const request = newRequest("trusted-remediation-request", "submit", actor, { nodes: [node] });
      for (const remediationVerifier of [
        undefined,
        async () => false,
        async () => "agent assertion",
        async () => ({ verified: true }),
        async () => {
          throw new Error("unverified domain proof");
        },
      ]) {
        const fake = fakeGitHub(initial);
        await assert.rejects(publishWorkQueueRequest(options(fake, request, actor, { remediationVerifier })), /remediation_verifier_required|remediation_invalid/);
        assert.equal(fake.state.updates, 0);
        assert.equal(fake.log().length, initial.length);
      }
      const fake = fakeGitHub(initial);
      fake.state.beforeUpdate = async ({ log, install }) =>
        install([...log, operationCommit(log, "remediation-stale", "control", [{ kind: "Control", control: "grants_paused", value: false, reason: "unrelated_control" }], initial[0].actor, 100)]);
      const tips = [];
      const result = await publishWorkQueueRequest(
        options(fake, request, actor, {
          remediationVerifier: async (current, candidate, trusted) => {
            tips.push(current.tip);
            assert.equal(trusted.principal, actor.principal);
            assert.equal(candidate.replacement_of.work_id, failed.work_id);
            candidate.payload.task = "verifier cannot rewrite stable semantics";
            return true;
          },
        })
      );
      assert.equal(tips.length, 2);
      assert.notEqual(tips[0], tips[1]);
      assert.equal(result.publishedNow, true);
      assert.equal(result.state.works.get(node.work_id).payload.task, node.payload.task);
      assert.equal(result.commit.request.fingerprint, request.fingerprint);
      const writes = fake.state.updates;
      const recovered = await publishWorkQueueRequest(
        options(fake, request, actor, {
          remediationVerifier: async () => {
            assert.fail("historical accepted recovery must not reverify remediation");
          },
        })
      );
      assert.equal(recovered.publishedNow, false);
      assert.equal(recovered.reused, true);
      assert.equal(recovered.commit.id, result.commit.id);
      assert.equal(fake.state.updates, writes);
    });
    it("checks a completed worker's trusted native event and revision before zero-grant or dependency reads", async () => {
      let log = [genesis()];
      log.push(submission(log, ["parent"]));
      const granted = grant(log);
      log.push(granted.commit);
      const dispatchId = granted.assignments[0].dispatch_id;
      log = bind(log, dispatchId);
      log.push(finish(log, dispatchId, "h1", "completed"));
      const state = replayTransactions(log);
      const actor = workerActor(state, dispatchId, "h1");
      const request = newRequest("worker-zero", "dispatch_next", actor, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 });
      const fake = fakeGitHub(log);
      let reads = 0;
      const trusted = context(actor, { ref: "0".repeat(40), event: "workflow_dispatch" });
      const refreshObservations = async () => {
        reads++;
        return [];
      };
      for (const invalid of [{ ...trusted, ref: "1".repeat(40) }, { ...trusted, event: "pull_request" }, context(actor)])
        await assert.rejects(publishWorkQueueRequest(options(fake, request, actor, { context: invalid, refreshObservations })), /run_binding_conflict/);
      assert.equal(reads, 0);
      assert.equal(fake.state.updates, 0);
      const result = await publishWorkQueueRequest(options(fake, request, actor, { context: trusted, refreshObservations }));
      assert.equal(reads, 1);
      assert.equal(result.publishedNow, false);
      assert.equal(result.state.requests.has(request.id), false);
    });
    it("recovers accepted controls read-only after Result but rejects fresh controls before generation", async () => {
      const { fake, log, state, dispatchId, trusted } = authorizerFixture([{ task: "parent" }, { task: "sibling" }]);
      fake.install([...log, submission(log, ["next"], { at: 50, graph: "next", id: "next-admission" })]);
      const actor = workerActor(state, dispatchId, "h1");
      const child = newChildWork(state, actor, { task: "continuation" }, "before-result-child", 80);
      const observation = {
        kind: "Observation",
        observation_id: "accepted-control-observation",
        resource: { kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "1", resource_id: "2", number: "2" },
        condition: "completed",
        state: "ready",
        observed_at: 100,
        credential_generation: "initial",
        read_status: "ok",
        resource_state: "closed",
        state_reason: "completed",
      };
      let generations = 0;
      let clockReads = 0;
      let observationReads = 0;
      let at = 100;
      const inputs = Object.entries({
        submit: { nodes: [child] },
        observe: { operations: [observation] },
        dispatch_next: { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 },
      }).map(([kind, parameters]) =>
        options(fake, newRequest(`accepted-control-${kind}`, kind, actor, parameters), actor, {
          context: trusted,
          now: () => {
            clockReads++;
            return at;
          },
          generateOperations: (current, stable, origin, time, id, observations) => {
            generations++;
            return generateRequestOperations(current, stable, origin, time, id, observations);
          },
          ...(kind === "dispatch_next"
            ? {
                refreshObservations: async () => {
                  observationReads++;
                  return [];
                },
              }
            : {}),
        })
      );
      for (const input of inputs) assert.equal((await publishWorkQueueRequest(input)).publishedNow, true);
      const claim = state.claims.get(state.works.get(log[1].operations[0].work_id).claim_id);
      const result = {
        kind: "Result",
        work_id: claim.work_id,
        claim_id: claim.claim_id,
        completion_id: claim.terminal_commit_id,
        descriptor: { summary: "scoped controls independently verified" },
        evidence: evidence(state, state.dispatches.get(dispatchId), "delivery", 150, { source: "verified_receipts", run_id: "202", run_attempt: 1, receipt: "complete-scoped-control-receipts" }),
      };
      const acceptedLog = fake.log();
      fake.install([...acceptedLog, operationCommit(acceptedLog, "verified-controls", "result", [result], reconciler, 150)]);
      const before = serializeTransactionLog(fake.log());
      const updates = fake.state.updates;
      generations = 0;
      clockReads = 0;
      observationReads = 0;
      at = 200;
      for (const input of inputs) {
        const recovered = await publishWorkQueueRequest(input);
        assert.equal(recovered.recovered, true);
        assert.equal(recovered.publishedNow, false);
        assert.equal(recovered.commit.request.id, input.request.id);
        const other = inputs.find(candidate => candidate.request.kind !== input.request.kind);
        assert.ok(other);
        const conflicting = newRequest(input.request.id, other.request.kind, actor, other.request.parameters);
        await assert.rejects(publishWorkQueueRequest({ ...input, request: conflicting, refreshObservations: undefined }), { code: "request_reused" });
      }
      assert.equal(generations, 0);
      assert.equal(clockReads, 0);
      assert.equal(observationReads, 0);
      assert.equal(fake.state.updates, updates);
      assert.equal(serializeTransactionLog(fake.log()), before);
      const verified = replayTransactions(fake.log());
      assert.throws(() => newChildWork(verified, actor, { task: "fresh post-Result continuation" }, "after-result-child", 180), { code: "claim_effects_unauthorized" });
      for (const input of inputs) {
        const fresh = newRequest(`fresh-${input.request.id}`, input.request.kind, actor, input.request.parameters);
        await assert.rejects(publishWorkQueueRequest({ ...input, request: fresh }), { code: "claim_effects_unauthorized" });
      }
      assert.equal(generations, 0);
      assert.equal(clockReads, 0);
      assert.equal(observationReads, 0);
      assert.equal(fake.state.updates, updates);
      assert.equal(serializeTransactionLog(fake.log()), before);
      assert.equal(replayTransactions(fake.log()).works.get(claim.work_id).barrier, "verified");
      await assert.rejects(freshAuthorizer({ githubClient: fake.githubClient, owner: "owner", repo: "repo", context: trusted })(observation.resource), { code: "claim_effects_unauthorized" });
      const current = replayTransactions(fake.log());
      const sibling = workerActor(current, dispatchId, "h2");
      const siblingChild = newChildWork(current, sibling, { task: "independent sibling" }, "sibling-child", 180);
      const siblingRequest = newRequest("sibling-control-after-result", "submit", sibling, { nodes: [siblingChild] });
      const published = await publishWorkQueueRequest(options(fake, siblingRequest, sibling, { context: context(sibling, { ref: "0".repeat(40), event: "workflow_dispatch" }), now: () => 200 }));
      assert.equal(published.publishedNow, true);
      assert.equal(published.state.requests.get(siblingRequest.id).actor.claim_handle, "h2");
    });
    it("publishes inherited tenant children as worker 22, not origin 11, across original-attempt binding and CAS recovery", async () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "11" });
      policy.accounting_weights.tenant = 7;
      policy.producers["11"] = { pools: ["default"], priorities: [1], fairness_keys: ["tenant"] };
      policy.pools.default.profiles.default.principal = "22";
      policy.pools.other = structuredClone(policy.pools.default);
      const sender = { ...dispatcher, principal: "11", run_attempt: 2 };
      const origin = { ...sender, role: "producer" };
      const parent = { ...newWork({ task: "parent" }, "lineage", "parent", "default", policy, 1), priority: 1, fairness_key: "tenant" };
      const log = [genesis(policy)];
      log.push(commit("genesis", "origin-parent", "submit", origin, { nodes: [parent] }, [parent], 1));
      const parameters = { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 };
      const granted = planDispatch(replayTransactions(log), parameters, { requestId: "request-origin-grant", commitId: "origin-grant", at: 10 });
      log.push(commit("origin-parent", "origin-grant", "dispatch_next", sender, parameters, granted.operations, 10));
      const dispatchId = granted.assignments[0].dispatch_id;
      log.push(operationCommit(log, "origin-start", "dispatch", [{ kind: "Dispatch", dispatch_id: dispatchId, state: "started", sender }], sender, 29));
      const started = replayTransactions(log);
      const profile = policy.pools.default.profiles.default;
      const run = { run_id: "200", run_attempt: 1, repository: "owner/repo", workflow: profile.workflow, ref: profile.ref, principal: "22", event: "workflow_dispatch" };
      log.push(
        operationCommit(
          log,
          "origin-binding",
          "dispatch",
          [{ kind: "Dispatch", dispatch_id: dispatchId, state: "bound", run, evidence: evidence(started, started.dispatches.get(dispatchId), "reconciliation", 30, { run_id: "200", run_attempt: 1 }) }],
          reconciler,
          30
        )
      );
      log.push(finish(log, dispatchId, "h1", "completed"));
      const completed = replayTransactions(log);
      const actor = workerActor(completed, dispatchId);
      const trusted = context(actor, { ref: profile.ref, event: "workflow_dispatch" });
      const claim = completed.claims.get(completed.works.get(parent.work_id).claim_id);
      const result = operationCommit(
        log,
        "origin-result",
        "result",
        [
          {
            kind: "Result",
            work_id: parent.work_id,
            claim_id: claim.claim_id,
            completion_id: claim.terminal_commit_id,
            descriptor: {},
            evidence: evidence(completed, completed.dispatches.get(dispatchId), "delivery", 60, { source: "verified_receipts", run_id: "200", run_attempt: 1, receipt: "original-worker-receipt" }),
          },
        ],
        reconciler,
        60
      );
      for (const prefix of [log, [...log, result]]) {
        const before = replayTransactions(prefix);
        if (before.works.get(parent.work_id).barrier === "verified") {
          assert.throws(() => newChildWork(before, actor, { task: "child" }, "child", 80), { code: "claim_effects_unauthorized" });
          const fake = fakeGitHub(prefix);
          const child = newChildWork(completed, actor, { task: "child" }, "child", 80);
          const request = newRequest("original-worker-child", "submit", actor, { nodes: [child] });
          const initialLog = serializeTransactionLog(fake.log());
          const initialSha = fake.refs.get("work-queue");
          await assert.rejects(publishWorkQueueRequest(options(fake, request, actor, { context: trusted })), { code: "claim_effects_unauthorized" });
          assert.equal(fake.state.updates, 0);
          assert.equal(fake.refs.get("work-queue"), initialSha);
          assert.equal(serializeTransactionLog(fake.log()), initialLog);
          continue;
        }
        const child = newChildWork(before, actor, { task: "child" }, "child", 80);
        const request = newRequest("original-worker-child", "submit", actor, { nodes: [child] });
        const fake = fakeGitHub(prefix);
        const initialLog = serializeTransactionLog(fake.log());
        const initialSha = fake.refs.get("work-queue");
        assert.equal(Object.hasOwn(before.policy.producers, "22"), false);
        assert.equal(before.works.get(parent.work_id).barrier, prefix === log ? "pending" : "verified");
        for (const change of [{ pool: "other" }, { priority: 3 }, { fairness_key: "" }]) {
          const unauthorized = newRequest("broader-worker-child", "submit", actor, { nodes: [{ ...child, ...change }] });
          await assert.rejects(publishWorkQueueRequest(options(fake, unauthorized, actor, { context: trusted })), { code: "child_entitlement" });
        }
        for (const change of [{ principal: "11" }, { run_id: "201" }, { claim_handle: "h2" }]) {
          const spoofed = { ...actor, ...change };
          const unauthorized = newRequest("foreign-worker-child", "submit", spoofed, { nodes: [child] });
          await assert.rejects(publishWorkQueueRequest(options(fake, unauthorized, spoofed, { context: context(spoofed, { ref: profile.ref, event: "workflow_dispatch" }) })));
        }
        await assert.rejects(publishWorkQueueRequest(options(fake, request, actor, { context: { ...trusted, run_attempt: 2 } })), /attempt 1/);
        assert.equal(fake.state.updates, 0);
        assert.equal(fake.refs.get("work-queue"), initialSha);
        assert.equal(serializeTransactionLog(fake.log()), initialLog);
        fake.state.beforeUpdate = async ({ log: current }) => {
          fake.install([...current, operationCommit(current, "competing-control", "control", [{ kind: "Control", control: "grants_paused", value: true, reason: "cas_retry" }], administrator, 90)]);
        };
        fake.state.ambiguousOnce = true;
        let generations = 0;
        const input = options(fake, request, actor, {
          context: trusted,
          generateOperations: (state, stable, principal, at, id, observations) => {
            generations++;
            return generateRequestOperations(state, stable, principal, at, id, observations);
          },
        });
        const published = await publishWorkQueueRequest(input);
        assert.equal(published.recovered, true);
        assert.equal(generations, 2);
        assert.equal(fake.state.updates, 2);
        assert.equal(canonical(published.commit.actor), canonical(actor));
        assert.equal(canonical(published.commit.request), canonical(request));
        assert.equal(published.state.transactions[published.state.works.get(parent.work_id).position.commit].actor.principal, "11");
        assert.equal(canonical(published.state.dispatches.get(dispatchId).sender), canonical(sender));
        assert.equal(canonical(published.state.dispatches.get(dispatchId).run), canonical(run));
        assert.equal(published.state.works.get(child.work_id).priority, 1);
        assert.equal(published.state.works.get(child.work_id).fairness_key, "tenant");
        assert.equal(published.state.works.get(parent.work_id).barrier, before.works.get(parent.work_id).barrier);
        assert.deepEqual(published.state.clocks, before.clocks);
        assert.equal(published.state.claims.size, before.claims.size);
        assert.equal(published.state.dispatches.size, before.dispatches.size);
        const acceptedLog = serializeTransactionLog(fake.log());
        const acceptedSha = fake.refs.get("work-queue");
        const acknowledged = await publishWorkQueueRequest(input);
        assert.equal(acknowledged.recovered, true);
        assert.equal(generations, 2);
        assert.equal(fake.state.updates, 2);
        assert.equal(fake.refs.get("work-queue"), acceptedSha);
        assert.equal(serializeTransactionLog(fake.log()), acceptedLog);
        assert.deepEqual(serializeProjection(replayTransactions([...fake.log()].reverse())), serializeProjection(published.state));
      }
    });
    it("rejects a losing worker candidate after verified Result wins CAS without charging the loser", async () => {
      const { fake, state, dispatchId, trusted } = authorizerFixture([{ task: "parent" }]);
      const actor = workerActor(state, dispatchId, "h1");
      const child = newChildWork(state, actor, { task: "losing continuation" }, "losing-child", 80);
      const request = newRequest("result-race-control", "submit", actor, { nodes: [child] });
      const parent = state.works.get([...state.claims.values()][0].work_id);
      const result = {
        kind: "Result",
        work_id: parent.work_id,
        claim_id: parent.claim_id,
        completion_id: parent.completion_id,
        descriptor: { summary: "verified before competing control publication" },
        evidence: evidence(state, state.dispatches.get(dispatchId), "delivery", 150, { source: "verified_receipts", run_id: "202", run_attempt: 1, receipt: "winning-result-receipt" }),
      };
      fake.state.beforeUpdate = async ({ log }) => {
        fake.install([...log, operationCommit(log, "winning-result", "result", [result], reconciler, 150)]);
      };
      let generations = 0;
      let clockReads = 0;
      await assert.rejects(
        publishWorkQueueRequest(
          options(fake, request, actor, {
            context: trusted,
            now: () => {
              clockReads++;
              return 200;
            },
            generateOperations: (current, stable, origin, at, id, observations) => {
              generations++;
              return generateRequestOperations(current, stable, origin, at, id, observations);
            },
          })
        ),
        { code: "claim_effects_unauthorized" }
      );
      const current = replayTransactions(fake.log());
      assert.equal(current.requests.has(request.id), false);
      assert.equal(current.requests.has("request-winning-result"), true);
      assert.equal(current.works.has(child.work_id), false);
      assert.equal(current.works.get(parent.work_id).barrier, "verified");
      assert.equal(current.claims.size, state.claims.size);
      assert.equal(current.dispatches.size, state.dispatches.size);
      assert.deepEqual(current.clocks, state.clocks);
      assert.equal(generations, 1);
      assert.equal(clockReads, 1);
      assert.equal(fake.state.updates, 1);
    });
    it("recovers an exact accepted worker request after native Release without restoring current Claim authority", async () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      policy.pools.default.profiles.default.principal = "22";
      let log = [genesis(policy)];
      log.push(submission(log, ["parent"]));
      const granted = grant(log);
      log.push(granted.commit);
      const dispatchId = granted.assignments[0].dispatch_id;
      log = bind(log, dispatchId);
      log.push(finish(log, dispatchId, "h1", "completed"));
      const state = replayTransactions(log);
      const actor = workerActor(state, dispatchId, "h1");
      const child = newChildWork(state, actor, { task: "continuation" }, "child", 50);
      const request = newRequest("worker-release-ack", "submit", actor, { nodes: [child] });
      const trusted = context(actor, { ref: "0".repeat(40), event: "workflow_dispatch" });
      const fake = fakeGitHub(log);
      let generations = 0;
      let clockReads = 0;
      const input = options(fake, request, actor, {
        context: trusted,
        now: () => {
          clockReads++;
          return 100;
        },
        generateOperations: (current, stable, origin, at, id, observations) => {
          generations++;
          return generateRequestOperations(current, stable, origin, at, id, observations);
        },
      });
      const accepted = await publishWorkQueueRequest(input);
      assert.equal(accepted.publishedNow, true);
      const dispatch = accepted.state.dispatches.get(dispatchId);
      const terminal = evidence(accepted.state, dispatch, "terminal_run", 200, { run_id: "200", run_attempt: 1, status: "completed", conclusion: "success" });
      const release = operationCommit(fake.log(), "release-parent", "release", [{ kind: "Release", dispatch_id: dispatchId, evidence: terminal }], undefined, 200);
      fake.install([...fake.log(), release]);
      const before = serializeTransactionLog(fake.log());
      const updates = fake.state.updates;
      generations = 0;
      clockReads = 0;
      const recovered = await publishWorkQueueRequest(input);
      assert.equal(recovered.recovered, true);
      assert.equal(recovered.publishedNow, false);
      assert.equal(recovered.commit.id, accepted.commit.id);
      assert.equal(recovered.commit.actor.principal, "22");
      assert.equal(recovered.state.dispatches.get(dispatchId).released, true);
      assert.equal(generations, 0);
      assert.equal(clockReads, 0);
      assert.equal(fake.state.updates, updates);
      assert.equal(serializeTransactionLog(fake.log()), before);
      const conflicting = newRequest(request.id, "submit", actor, { nodes: [{ ...child, payload: { task: "changed" } }] });
      await assert.rejects(publishWorkQueueRequest({ ...input, request: conflicting }), { code: "request_reused" });
      const fresh = newRequest("worker-after-release", "submit", actor, request.parameters);
      await assert.rejects(publishWorkQueueRequest({ ...input, request: fresh }), { code: "claim_ineffective" });
      assert.equal(generations, 0);
      assert.equal(clockReads, 0);
      assert.equal(fake.state.updates, updates);
      assert.equal(serializeTransactionLog(fake.log()), before);
    });
    it("checks repository visibility before interpreting an absent branch, and never adopts a legacy queue", async () => {
      const fake = fakeGitHub();
      fake.state.visibility = false;
      await assert.rejects(readWorkQueueLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" }), /repository_unavailable/);
      assert.deepEqual(fake.state.calls, ["repos.get"]);
      fake.state.visibility = true;
      fake.refs.set("dispatch-coordinator", "legacy");
      await assert.rejects(readWorkQueueLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" }), /unsupported_protocol/);
      assert.equal(fake.state.updates, 0);
    });
    it("uses actual Git reads instead of installation-token collaborator flags", async () => {
      for (const initial of [[], [genesis()]]) {
        const fake = fakeGitHub(initial);
        const getRepository = fake.githubClient.rest.repos.get;
        fake.githubClient.rest.repos.get = async () => {
          const response = await getRepository();
          return { data: { ...response.data, permissions: { pull: false } } };
        };
        const result = await readWorkQueueLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" });
        assert.equal(result.transactions.length, initial.length);
        if (!initial.length) {
          assert.equal(result.sha, null);
          assert.ok(fake.state.calls.includes("getRef:heads/main"));
        }
        assert.equal(fake.state.updates, 0);
      }
    });
    it("does not turn denied contents access into a missing queue", async () => {
      const fake = fakeGitHub();
      const getRepository = fake.githubClient.rest.repos.get;
      fake.githubClient.rest.repos.get = async () => {
        const response = await getRepository();
        return { data: { ...response.data, permissions: { pull: false } } };
      };
      fake.githubClient.rest.git.getRef = async () => {
        throw Object.assign(new Error("Resource not accessible by integration"), { status: 403 });
      };
      await assert.rejects(readWorkQueueLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" }), { status: 403 });
      assert.equal(fake.state.updates, 0);
    });
    it("requires a readable default ref before interpreting an absent queue in a nonempty repository", async () => {
      for (const defaultBranch of ["main", ""]) {
        const fake = fakeGitHub();
        fake.state.defaultRevision = "";
        const getRepository = fake.githubClient.rest.repos.get;
        fake.githubClient.rest.repos.get = async () => {
          const response = await getRepository();
          return { data: { ...response.data, default_branch: defaultBranch, permissions: { pull: false } } };
        };
        await assert.rejects(readWorkQueueLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" }), /repository_unavailable: cannot establish contents access/);
        assert.equal(fake.state.updates, 0);
      }
    });
    it("binds every retained Actor to the actual queue repository independently of the ledger's genesis", async () => {
      for (const repository of ["foreign/repository", "Owner/Repo"]) {
        const root = genesis();
        root.actor = { ...root.actor, repository };
        root.request = newRequest(root.request.id, root.request.kind, root.actor, root.request.parameters);
        const fake = fakeGitHub([root]);
        const read = () => readWorkQueueLog({ githubClient: fake.githubClient, owner: "OWNER", repo: "REPO" });
        if (repository === "foreign/repository") {
          await assert.rejects(read(), error => error instanceof Error && "code" in error && error.code === "actor_unauthorized");
          const request = newRequest("foreign-ledger-zero-grant", "dispatch_next", dispatcher, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 });
          await assert.rejects(publishWorkQueueRequest(options(fake, request)), error => error instanceof Error && "code" in error && error.code === "actor_unauthorized");
        } else assert.equal((await read()).state.repository, repository);
        assert.equal(fake.state.updates, 0);
      }
    });
    it("reads multi-MiB ledger blobs without overflowing the base64 validator", async () => {
      const fake = fakeGitHub([genesis()]);
      const content = canonical(genesis()) + " ".repeat(6 * 1024 * 1024) + "\n";
      fake.state.badBlob = { encoding: "base64", content: Buffer.from(content).toString("base64") };
      const current = await readWorkQueueLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" });
      assert.equal(current.state.tip, "genesis");
      assert.equal(current.transactions.length, 1);
      assert.equal(fake.state.updates, 0);
    });
    it("rejects missing logs, truncated trees, malformed UTF-8/base64, empty/policyless histories without writes", async () => {
      const fake = fakeGitHub([genesis()]);
      for (const property of ["missingLog", "truncated", "emptyLog"]) {
        fake.state[property] = true;
        await assert.rejects(readWorkQueueLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" }), /ledger_invalid|policy_missing/);
        fake.state[property] = false;
      }
      for (const blob of [
        { encoding: "base64", content: "***" },
        { encoding: "base64", content: "AA=A" },
        { encoding: "base64", content: "A===" },
        { encoding: "base64", content: "AAAA=" },
        { encoding: "base64", content: Buffer.from([0xff]).toString("base64") },
        { encoding: "utf8", content: "{}" },
      ]) {
        fake.state.badBlob = blob;
        await assert.rejects(readWorkQueueLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" }), /ledger_invalid/);
      }
      assert.equal(fake.state.updates, 0);
    });
    it("installs mandatory defaults only on genuine genesis and does not overlay authoritative installed Policy", async () => {
      const fake = fakeGitHub();
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      const root = genesis(policy);
      const add = submission([root], ["a"]);
      const request = newRequest("submit-first", "submit", producer, { nodes: add.operations });
      await publishWorkQueueRequest(options(fake, request, producer, { initializationContext: context(administrator), context: context(producer, { ref: "b".repeat(40) }), maxRetries: 0 }));
      const state = replayTransactions(fake.log());
      assert.equal(state.transactions[0].operations[0].kind, "Policy");
      assert.equal(state.policy.pools.default.profiles.default.max_claims, 1);
      assert.equal(state.policy.pools.default.profiles.default.ref, "a".repeat(40));
      const seed = createHash("sha256").update(request.id, "utf8").digest("hex");
      assert.equal(state.transactions[0].request.id, `init_${seed}`);
      assert.equal(state.policy_epoch, `epoch_${seed}`);
      const proposal = structuredClone(policy);
      proposal.mode = "strict-priority";
      const later = submission(fake.log(), ["b"], { id: "second" });
      await publishWorkQueueRequest(options(fake, later.request, producer, { policyProposal: proposal }));
      assert.equal(replayTransactions(fake.log()).policy.mode, "weighted-priority");
      assert.equal(replayTransactions(fake.log()).transactions.filter(commit => commit.request.kind === "policy").length, 1);
    });
    it("never elevates a dispatcher context to initialize Policy without explicit administrator context", async () => {
      const fake = fakeGitHub();
      const root = genesis();
      const nodes = submission([root], ["a"]).operations;
      const request = newRequest("explicit-administrator-required", "submit", dispatcher, { nodes });
      await assert.rejects(
        publishWorkQueueRequest(options(fake, request, dispatcher, { context: context(dispatcher, { roles: ["dispatcher", "administrator"] }) })),
        error => error instanceof Error && "code" in error && error.code === "policy_missing"
      );
      assert.equal(fake.log().length, 0);
      assert.equal(fake.blobs.size, 0);
    });
    it("fails default genesis routing closed without a verified immutable repository revision", async () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      const add = submission([genesis(policy)], ["a"]);
      const request = newRequest("default-revision-required", "submit", producer, { nodes: add.operations });
      for (const revision of ["refs/heads/main", "A".repeat(40), "abc"]) {
        const fake = fakeGitHub();
        fake.state.defaultRevision = revision;
        await assert.rejects(publishWorkQueueRequest(options(fake, request, producer, { initializationContext: context(administrator) })), /immutable verified/);
        assert.equal(fake.refs.has("work-queue"), false);
        assert.equal(fake.state.updates, 0);
      }
    });
    it("rejects genuine default genesis before writing without an immutable assignment-capable active worker route", async () => {
      const nodes = submission([genesis()], ["a"]).operations;
      const request = newRequest("default-worker-route-required", "submit", producer, { nodes });
      for (const change of [
        state => (state.workerContent = null),
        state => (state.workerContent = "on: push\n"),
        state => (state.workerContent = "on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: boolean\n        required: true\n"),
        state => (state.workerContent = "on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        required: true\n"),
        state => (state.workerWorkflow = null),
        state => (state.workerWorkflow.state = "disabled_manually"),
        state => (state.workerWorkflow.path = ".github/workflows/unapproved.lock.yml"),
      ]) {
        const fake = fakeGitHub();
        change(fake.state);
        await assert.rejects(publishWorkQueueRequest(options(fake, request, producer, { initializationContext: context(administrator) })));
        assert.equal(fake.refs.has("work-queue"), false);
        assert.equal(fake.blobs.size, 0);
        assert.equal(fake.state.updates, 0);
      }
    });
    it("accepts an assignment-capable immutable default worker when the string input is optional", async () => {
      const fake = fakeGitHub();
      fake.state.workerContent = "on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\n        required: false\n";
      const nodes = submission([genesis()], ["a"]).operations;
      const request = newRequest("optional-default-worker-input", "submit", producer, { nodes });
      await publishWorkQueueRequest(options(fake, request, producer, { initializationContext: context(administrator) }));
      assert.equal(fake.log().length, 2);
      assert.equal(fake.log()[0].operations[0].policy.pools.default.profiles.default.ref, fake.state.defaultRevision);
    });
    it("rejects fresh explicit zero-revision profiles without rejecting retained valid constructor-fixture history", async () => {
      for (const ref of ["0".repeat(40), "0".repeat(64)]) {
        const fake = fakeGitHub();
        const policy = defaultPolicy({ repository: "owner/repo", principal: "1001", ref });
        const operation = { kind: "Policy", epoch: "approved-explicit", policy };
        const request = newRequest(`zero-default-${ref.length}`, "policy", administrator, { operations: [operation] });
        await assert.rejects(publishWorkQueueRequest(options(fake, request, administrator)), /policy_missing/);
        assert.equal(fake.blobs.size, 0);
        assert.equal(fake.refs.has("work-queue"), false);
        await assert.rejects(initializeWorkQueue({ ...options(fake, request, administrator), policyProposal: policy }), /policy_missing/);
        assert.equal(fake.blobs.size, 0);
        assert.equal(fake.refs.has("work-queue"), false);
        const retained = fakeGitHub([genesis(policy)]);
        const retainedState = (await readWorkQueueLog({ githubClient: retained.githubClient, owner: "owner", repo: "repo" })).state;
        assert.ok(retainedState.policy);
        assert.equal(retainedState.policy.pools.default.profiles.default.ref, ref);
      }
    });
    it("verifies the native worker route for direct explicit Policy genesis without a default-profile detour", async () => {
      for (const policyProposal of [false, true]) {
        const fake = fakeGitHub();
        fake.state.workerContent = null;
        const policy = defaultPolicy({ repository: "owner/repo", principal: "1001", ref: fake.state.defaultRevision });
        const operation = { kind: "Policy", epoch: "explicit-native", policy };
        const request = newRequest("explicit-native-worker-required", "policy", administrator, { operations: [operation] });
        const input = options(fake, request, administrator);
        await assert.rejects(policyProposal ? initializeWorkQueue({ ...input, policyProposal: policy, epoch: operation.epoch }) : publishWorkQueueRequest(input));
        assert.equal(fake.blobs.size, 0);
        assert.equal(fake.refs.has("work-queue"), false);
        assert.equal(fake.state.updates, 0);
      }
    });
    it("verifies prospective administrator Policy before writes but never reprovisions a historical acknowledgment", async () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001", ref: "a".repeat(40) });
      const initial = [genesis(policy)];
      const fake = fakeGitHub(initial);
      const next = { ...structuredClone(policy), mode: "strict-priority" };
      const operation = { kind: "Policy", epoch: "prospective", policy: next };
      const request = newRequest("prospective-native-worker-required", "policy", administrator, { operations: [operation] });
      const validContent = fake.state.workerContent;
      fake.state.workerContent = null;
      await assert.rejects(publishWorkQueueRequest(options(fake, request, administrator)));
      assert.equal(fake.state.updates, 0);
      assert.equal(fake.log().length, initial.length);
      fake.state.workerContent = validContent;
      const installed = await publishWorkQueueRequest(options(fake, request, administrator));
      fake.state.workerContent = null;
      fake.state.workerWorkflow = null;
      const writes = fake.state.updates;
      const recovered = await publishWorkQueueRequest(options(fake, request, administrator));
      assert.equal(recovered.publishedNow, false);
      assert.equal(recovered.reused, true);
      assert.equal(recovered.commit.id, installed.commit.id);
      assert.equal(fake.state.updates, writes);
    });
    it("rechecks prospective Policy route evidence against every fresh CAS prefix", async () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001", ref: "a".repeat(40) });
      const fake = fakeGitHub([genesis(policy)]);
      const operation = { kind: "Policy", epoch: "fresh-policy-route", policy: { ...structuredClone(policy), mode: "strict-priority" } };
      const request = newRequest("fresh-policy-route", "policy", administrator, { operations: [operation] });
      let blobsAfterConflict = 0;
      fake.state.beforeUpdate = async ({ log, install }) => {
        install([...log, operationCommit(log, "route-cas-conflict", "control", [{ kind: "Control", control: "grants_paused", value: false, reason: "concurrent_control" }], administrator, 100)]);
        fake.state.workerContent = null;
        blobsAfterConflict = fake.blobs.size;
      };
      await assert.rejects(publishWorkQueueRequest(options(fake, request, administrator)), /policy_missing/);
      assert.equal(fake.state.updates, 1);
      assert.equal(fake.blobs.size, blobsAfterConflict);
      assert.equal(fake.log().length, 2);
      assert.equal(replayTransactions(fake.log()).policy_epoch, "initial");
      assert.equal(fake.state.calls.filter(call => call.startsWith("getContent:")).length, 2);
      assert.equal(fake.state.calls.filter(call => call.startsWith("getWorkflow:")).length, 1);
    });
    it("recovers an ambiguous accepted Policy without repeated route reads or writes", async () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001", ref: "a".repeat(40) });
      const fake = fakeGitHub([genesis(policy)]);
      const operation = { kind: "Policy", epoch: "policy-lost-ack", policy: { ...structuredClone(policy), mode: "strict-priority" } };
      const request = newRequest("policy-lost-ack", "policy", administrator, { operations: [operation] });
      fake.state.ambiguousOnce = true;
      const installed = await publishWorkQueueRequest(options(fake, request, administrator));
      assert.equal(installed.recovered, true);
      assert.equal(installed.publishedNow, false);
      assert.equal(fake.state.updates, 1);
      assert.equal(fake.state.calls.filter(call => call.startsWith("getContent:")).length, 1);
      assert.equal(fake.state.calls.filter(call => call.startsWith("getWorkflow:")).length, 1);
      fake.state.workerContent = null;
      fake.state.workerWorkflow = null;
      const recovered = await publishWorkQueueRequest(options(fake, request, administrator));
      assert.equal(recovered.commit.id, installed.commit.id);
      assert.equal(fake.state.updates, 1);
      assert.equal(fake.state.calls.filter(call => call.startsWith("getContent:")).length, 1);
    });
    it("regenerates the whole fair prefix after a stale CAS instead of retaining selected Work", async () => {
      const initial = [genesis()];
      initial.push(submission(initial, ["a", "b", "c"]));
      const fake = fakeGitHub(initial);
      const request = newRequest("local-grant", "dispatch_next", dispatcher, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 });
      const attempts = [];
      fake.state.beforeUpdate = async ({ log, install }) => {
        const competing = grant(log, { id: "remote-grant", at: 100 });
        install([...log, competing.commit]);
      };
      const result = await publishWorkQueueRequest(
        options(fake, request, dispatcher, {
          generateOperations: (state, stable, actor, at, id) => {
            const next = planDispatch(state, stable.parameters, { requestId: stable.id, commitId: id, at });
            attempts.push(next.operations.map(operation => operation.work_id));
            return next;
          },
        })
      );
      assert.deepEqual(attempts, [[initial[1].operations[0].work_id], [initial[1].operations[1].work_id]]);
      assert.equal(result.commit.request.id, request.id);
      assert.deepEqual(Object.keys(result.assignments[0]).sort(), ["claims", "commit_id", "dispatch_id", "policy_epoch", "pool", "request_id", "version", "worker_profile"]);
      assert.equal(fake.state.updates, 2);
      const state = replayTransactions(fake.log());
      assert.equal(state.claims.size, 2);
      assert.equal(state.requests.get("local-grant").request.fingerprint, request.fingerprint);
    });
    it("rejects uninstalled dispatch pools before dependency probing or candidate generation", async () => {
      const fake = fakeGitHub([genesis()]);
      const before = serializeTransactionLog(fake.log());
      const request = newRequest("uninstalled-pool", "dispatch_next", dispatcher, { pool: "foreign", max_claims: 1, max_dispatches: 1, max_bytes: 49152 });
      let reads = 0;
      let generations = 0;
      let clockReads = 0;
      await assert.rejects(
        publishWorkQueueRequest(
          options(fake, request, dispatcher, {
            refreshObservations: async () => {
              reads++;
              return [];
            },
            now: () => {
              clockReads++;
              return 100;
            },
            generateOperations: (state, stable, actor, at, id, observations) => {
              generations++;
              return generateRequestOperations(state, stable, actor, at, id, observations);
            },
          })
        ),
        { code: "policy_invalid" }
      );
      assert.equal(reads, 0);
      assert.equal(clockReads, 0);
      assert.equal(generations, 0);
      assert.equal(fake.state.updates, 0);
      assert.equal(serializeTransactionLog(fake.log()), before);
    });
    it("refreshes typed observations after every CAS conflict and captures publication time after native reads", async () => {
      const resource = { kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "1", resource_id: "2", number: "7" };
      const initial = [genesis()];
      initial.push(submission(initial, ["gated"], { transform: node => ({ ...node, depends_on: [{ kind: "issue", resource, condition: "completed" }] }) }));
      const fake = fakeGitHub(initial);
      fake.state.beforeUpdate = async ({ log, install }) =>
        install([...log, operationCommit(log, "rotate-during-cas", "control", [{ kind: "Control", control: "credential_generation", value: "rotated", reason: "equivalent_scoped_credentials" }], administrator, 100)]);
      let reads = 0;
      let time = 100;
      const request = newRequest("refresh-and-grant", "dispatch_next", dispatcher, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 });
      const result = await publishWorkQueueRequest(
        options(fake, request, dispatcher, {
          now: () => time,
          refreshObservations: async state => {
            reads++;
            time++;
            return {
              operations: [
                {
                  kind: "Observation",
                  observation_id: `read-${reads}`,
                  resource,
                  condition: "completed",
                  state: "ready",
                  observed_at: time,
                  credential_generation: state.credential_generation,
                  read_status: "ok",
                  resource_state: "closed",
                  state_reason: "completed",
                },
              ],
              reads,
            };
          },
        })
      );
      assert.equal(reads, 2);
      assert.equal(result.commit.at, 102);
      assert.deepEqual(
        result.operations.map(operation => operation.kind),
        ["Observation", "Claim"]
      );
      assert.equal(result.operations[0].credential_generation, "rotated");
      assert.deepEqual(result.operations[1].observations, ["read-2"]);
      const state = replayTransactions(fake.log());
      assert.equal(state.claims.size, 1);
      assert.equal(state.observationsById.has("read-1"), false);
      assert.equal(state.observationsById.has("read-2"), true);
    });
    it("does not publish refreshed observations or consume identity when no grant is possible", async () => {
      const resource = { kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "1", resource_id: "2", number: "7" };
      const initial = [genesis()];
      initial.push(submission(initial, ["gated"], { transform: node => ({ ...node, depends_on: [{ kind: "issue", resource, condition: "completed" }] }) }));
      const fake = fakeGitHub(initial);
      const before = serializeTransactionLog(fake.log());
      const request = newRequest("refresh-no-grant", "dispatch_next", dispatcher, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 });
      const result = await publishWorkQueueRequest(
        options(fake, request, dispatcher, {
          refreshObservations: async state => [
            {
              kind: "Observation",
              observation_id: "waiting",
              resource,
              condition: "completed",
              state: "waiting",
              observed_at: 100,
              credential_generation: state.credential_generation,
              read_status: "ok",
              resource_state: "open",
              state_reason: "reopened",
            },
          ],
        })
      );
      assert.equal(result.persisted, false);
      assert.equal(result.operations.length, 0);
      assert.equal(fake.state.updates, 0);
      assert.equal(serializeTransactionLog(fake.log()), before);
      assert.equal(result.state.observations.size, 0);
      assert.equal(result.state.requests.has(request.id), false);
    });
    it("recovers an ambiguous committed response by stable identity, without selecting/charging again", async () => {
      const initial = [genesis()];
      initial.push(submission(initial, ["a", "b"]));
      const fake = fakeGitHub(initial);
      fake.state.ambiguousOnce = true;
      const request = newRequest("uncertain", "dispatch_next", dispatcher, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 });
      let generations = 0;
      const opts = options(fake, request, dispatcher, {
        generateOperations: (state, stable, actor, at, id) => {
          generations++;
          return planDispatch(state, stable.parameters, { requestId: stable.id, commitId: id, at });
        },
      });
      const response = await publishWorkQueueRequest(opts);
      assert.equal(response.recovered, true);
      assert.equal(response.publishedNow, false);
      assert.equal(response.reused, true);
      assert.deepEqual(Object.keys(response.assignments[0]).sort(), ["claims", "commit_id", "dispatch_id", "policy_epoch", "pool", "request_id", "version", "worker_profile"]);
      const payload = canonical(response.state.dispatches.get(response.assignments[0].dispatch_id).claims[0].work);
      response.assignments[0].claims[0].work.unauthorized = "caller mutation";
      assert.equal(canonical(response.state.dispatches.get(response.assignments[0].dispatch_id).claims[0].work), payload);
      assert.equal(generations, 1);
      assert.equal(replayTransactions(fake.log()).claims.size, 1);
      const repeated = await publishWorkQueueRequest(opts);
      assert.equal(repeated.commit.id, response.commit.id);
      assert.equal(repeated.publishedNow, false);
      assert.equal(repeated.reused, true);
      assert.equal(generations, 1);
      assert.equal(fake.state.updates, 1);
    });
    it("returns detached committed assignments rather than candidate generator metadata", async () => {
      const initial = [genesis()];
      initial.push(submission(initial, ["a"]));
      const fake = fakeGitHub(initial);
      const request = newRequest("checked-result", "dispatch_next", dispatcher, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 });
      const result = await publishWorkQueueRequest(
        options(fake, request, dispatcher, {
          generateOperations: (state, stable, actor, at, commitId) => ({
            ...planDispatch(state, stable.parameters, { requestId: stable.id, commitId, at }),
            assignments: [{ fabricated: true }],
            state: { fabricated: true },
            commit: { fabricated: true },
            transactions: [],
            publishedNow: false,
            reused: true,
            persisted: false,
            recovered: true,
            idempotent: true,
          }),
        })
      );
      const member = result.assignments[0].claims[0];
      assert.equal(result.publishedNow, true);
      assert.equal(result.reused, false);
      assert.equal(result.persisted, true);
      assert.equal(result.recovered, false);
      assert.equal(result.idempotent, false);
      assert.equal(result.state.claims.has(member.claim_id), true);
      assert.equal(result.commit.request.id, request.id);
      assert.equal(result.transactions.length, 3);
      assert.equal(canonical(member.work), canonical(result.state.works.get(member.work_id).payload));
      member.work.untrusted = "changed";
      assert.equal(Object.hasOwn(result.state.works.get(member.work_id).payload, "untrusted"), false);
    });
    it("uses canonical Go-compatible prefix-bound candidate IDs rather than random byte-budget metadata", async () => {
      const { createHash } = require("node:crypto");
      const initial = [genesis()];
      initial.push(submission(initial, ["a"]));
      const fake = fakeGitHub(initial);
      const request = newRequest("canonical-candidate", "dispatch_next", dispatcher, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 });
      const { commitId: omittedCommitId, ...publication } = options(fake, request);
      const result = await publishWorkQueueRequest(publication);
      const expected = `q_${createHash("sha256")
        .update(`${initial[initial.length - 1].id}\n${request.id}`, "utf8")
        .digest("hex")}`;
      assert.equal(result.commit.id, expected);
      assert.equal(result.assignments[0].commit_id, expected);
      const empty = fakeGitHub();
      const operation = genesis(defaultPolicy({ repository: "owner/repo", principal: "1001", ref: empty.state.defaultRevision })).operations[0];
      const bootstrap = newRequest("canonical-genesis", "policy", administrator, { operations: [operation] });
      const { commitId: omittedRootCommitId, ...rootPublication } = options(empty, bootstrap, administrator);
      const installed = await publishWorkQueueRequest(rootPublication);
      assert.equal(installed.commit.id, `q_${createHash("sha256").update(bootstrap.id, "utf8").digest("hex")}`);
    });
    it("matches independent native naming goldens for automatic bootstrap and the originating publication", async () => {
      const golden = contractFixture.identities.publisher_generation_example;
      const fake = fakeGitHub();
      const nodes = submission([genesis()], ["a"]).operations;
      const request = newRequest(golden.publication_request_id, "submit", producer, { nodes });
      const { commitId: omittedCommitId, ...publication } = options(fake, request, producer, { initializationContext: context(administrator), maxRetries: 0 });
      const result = await publishWorkQueueRequest(publication);
      const log = fake.log();
      assert.equal(log.length, 2);
      assert.equal(log[0].request.id, golden.genesis_request_id);
      assert.equal(log[0].id, golden.genesis_commit_id);
      assert.equal(log[0].policy_epoch, golden.genesis_policy_epoch);
      assert.equal(log[0].operations[0].epoch, golden.genesis_policy_epoch);
      assert.equal(log[1].request.id, golden.publication_request_id);
      assert.equal(log[1].previous, golden.genesis_commit_id);
      assert.equal(log[1].id, golden.publication_commit_id);
      assert.equal(result.commit.id, golden.publication_commit_id);
      assert.equal(result.state.policy_epoch, golden.genesis_policy_epoch);
    });
    it("rejects same request identity with different budget, actor, kind or validated semantics", async () => {
      const initial = [genesis()];
      initial.push(submission(initial, ["a", "b"]));
      const fake = fakeGitHub(initial);
      const parameters = { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 };
      await publishWorkQueueRequest(options(fake, newRequest("identity", "dispatch_next", dispatcher, parameters)));
      await assert.rejects(publishWorkQueueRequest(options(fake, newRequest("identity", "dispatch_next", dispatcher, { ...parameters, max_claims: 2 }))), /request_reused/);
      const alteredActor = { ...dispatcher, run_id: "101" };
      await assert.rejects(publishWorkQueueRequest(options(fake, newRequest("identity", "dispatch_next", alteredActor, parameters), alteredActor)), /request_reused/);
      const control = [{ kind: "Control", control: "grants_paused", value: true, reason: "pause" }];
      await assert.rejects(publishWorkQueueRequest(options(fake, newRequest("identity", "control", administrator, { operations: control }), administrator)), /request_reused/);
      assert.equal(fake.state.updates, 1);
    });
    it("does not consume no-grant request identity or mutate charges, active sets, logs or refs", async () => {
      const fake = fakeGitHub([genesis()]);
      const request = newRequest("empty-evaluation", "dispatch_next", dispatcher, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 });
      const before = serializeTransactionLog(fake.log());
      const response = await publishWorkQueueRequest(options(fake, request, dispatcher, { generateOperations: () => ({ operations: [], reason: "no_work", publishedNow: true, reused: true, recovered: true, idempotent: true }) }));
      assert.equal(response.reason, "no_work");
      assert.equal(response.persisted, false);
      assert.equal(response.publishedNow, false);
      assert.equal(response.reused, false);
      assert.equal(fake.state.updates, 0);
      assert.equal(serializeTransactionLog(fake.log()), before);
      assert.equal(replayTransactions(fake.log()).requests.has(request.id), false);
      const add = submission(fake.log(), ["later"], { id: "later" });
      fake.install([...fake.log(), add]);
      const granted = await publishWorkQueueRequest(options(fake, request));
      assert.equal(granted.persisted, true);
      assert.equal(granted.publishedNow, true);
      assert.equal(granted.reused, false);
      assert.equal(granted.commit.request.id, request.id);
    });
    it("fails closed on nonextending refresh and never initializes over corrupted storage", async () => {
      const initial = [genesis()];
      initial.push(submission(initial, ["a"]));
      const fake = fakeGitHub(initial);
      fake.state.beforeUpdate = async ({ install }) => install([genesis()]);
      const request = newRequest("rewrite", "dispatch_next", dispatcher, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 });
      await assert.rejects(publishWorkQueueRequest(options(fake, request)), /rewritten|does not extend/);
      assert.equal(fake.state.updates, 1);
    });
    it("derives authority only from authenticated credential context and rejects old fixed-intent writes", async () => {
      const fake = fakeGitHub([genesis()]);
      const request = newRequest("forged", "dispatch_next", dispatcher, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 });
      await assert.rejects(publishWorkQueueRequest(options(fake, request, dispatcher, { context: context({ ...dispatcher, principal: "another" }) })), /actor_unauthorized/);
      await assert.rejects(publishWorkQueueRequest(options(fake, request, dispatcher, { context: { ...dispatcher, authenticated: true } })), /approved role/);
      const { applyAndPublishWorkQueueTransactions } = require("./work_queue_store.cjs");
      assert.throws(() => applyAndPublishWorkQueueTransactions({ intents: [] }), /unsupported_protocol/);
      assert.equal(fake.state.calls.length, 0);
    });
    it("rejects unauthorized no-grant roles and foreign worker scopes before treating an empty queue as success", async () => {
      const fake = fakeGitHub([genesis()]);
      const parameters = { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 };
      const wrongRole = { ...administrator, role: "reconciler" };
      await assert.rejects(publishWorkQueueRequest(options(fake, newRequest("wrong-role", "dispatch_next", wrongRole, parameters), wrongRole)), /actor_unauthorized/);
      const foreignWorker = { ...dispatcher, role: "worker", dispatch_id: "foreign", claim_handle: "h1" };
      await assert.rejects(publishWorkQueueRequest(options(fake, newRequest("wrong-scope", "dispatch_next", foreignWorker, parameters), foreignWorker)), /claim_scope_invalid/);
      assert.equal(fake.state.updates, 0);
    });
    it("never initializes storage as a side effect of an unauthorized role or absent worker Claim", async () => {
      const fake = fakeGitHub();
      const parameters = { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 };
      const wrongRole = { ...administrator, role: "reconciler" };
      await assert.rejects(publishWorkQueueRequest(options(fake, newRequest("wrong-role", "dispatch_next", wrongRole, parameters), wrongRole, { initializationContext: context(administrator) })), /actor_unauthorized/);
      assert.equal(fake.state.calls.length, 0);
      const foreignWorker = { ...dispatcher, role: "worker", dispatch_id: "foreign", claim_handle: "h1" };
      await assert.rejects(publishWorkQueueRequest(options(fake, newRequest("wrong-scope", "dispatch_next", foreignWorker, parameters), foreignWorker, { initializationContext: context(administrator) })), /claim_scope_invalid/);
      assert.equal(fake.refs.size, 0);
      assert.equal(fake.state.updates, 0);
    });
    it("returns exact immutable node resubmissions without writing or spending new admission capacity", async () => {
      const initial = [genesis()];
      initial.push(submission(initial, ["a"]));
      const fake = fakeGitHub(initial);
      const request = newRequest("resubmit", "submit", producer, { nodes: initial[1].operations });
      const response = await publishWorkQueueRequest(options(fake, request, producer));
      assert.equal(response.reason, "already_submitted");
      assert.equal(response.idempotent, true);
      assert.equal(response.persisted, false);
      assert.equal(response.publishedNow, false);
      assert.equal(response.reused, false);
      assert.equal(fake.state.updates, 0);
      assert.equal(response.commit.id, "submit");
      assert.equal(replayTransactions(fake.log()).works.get(initial[1].operations[0].work_id).position.commit, 1);
    });
  });
}

if (require.main === module) registerTests(require("node:test"));
module.exports = { fakeGitHub, options, registerTests };
