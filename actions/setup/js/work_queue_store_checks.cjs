"use strict";

const assert = require("node:assert/strict");
const { createHash } = require("node:crypto");
const { canonical } = require("./work_queue_codec.cjs");
const { newRequest, parseTransactionLog, replayTransactions, serializeProjection, serializeTransactionLog } = require("./work_queue_replay.cjs");
const { initializeWorkQueue, publishWorkQueueRequest, readWorkQueueLog } = require("./work_queue_store.cjs");
const { defaultPolicy } = require("./work_queue_policy.cjs");
const { newWork } = require("./work_queue_graph.cjs");
const { planDispatch } = require("./work_queue_scheduler.cjs");
const { administrator, bind, context, dispatcher, finish, genesis, grant, operationCommit, producer, submission, workerActor } = require("./work_queue_test_helpers.cjs");

function fakeGitHub(initial = []) {
  let serial = 0;
  const blobs = new Map();
  const trees = new Map();
  const commits = new Map();
  const refs = new Map();
  const state = { calls: [], beforeUpdate: null, ambiguousOnce: false, visibility: true, defaultRevision: "a".repeat(40), missingLog: false, truncated: false, badBlob: null, emptyLog: false, updates: 0 };
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

function options(fake, request, actor = dispatcher, extra = {}) {
  let serial = 0;
  return { githubClient: fake.githubClient, owner: "owner", repo: "repo", request, actor, context: context(actor), sleepFn: async () => {}, now: () => 100, commitId: () => `candidate-${request.id}-${++serial}`, ...extra };
}

function registerTests({ describe, it }) {
  describe("checked current-only Git queue CAS store", () => {
    it("exposes administrator-only racing genesis installation without overwriting installed or malformed histories", async () => {
      const fake = fakeGitHub();
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      const input = {
        githubClient: fake.githubClient,
        owner: "owner",
        repo: "repo",
        context: context(administrator),
        policyProposal: policy,
        maxRetries: 0,
        now: () => 100,
      };
      await assert.rejects(initializeWorkQueue({ ...input, context: context(dispatcher) }), error => error.code === "actor_unauthorized");
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
      fake.state.missingLog = true;
      await assert.rejects(initializeWorkQueue(input), error => error.code === "ledger_invalid");
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
    it("binds every retained Actor to the actual queue repository independently of the ledger's genesis", async () => {
      for (const repository of ["foreign/repository", "Owner/Repo"]) {
        const root = genesis();
        root.actor = { ...root.actor, repository };
        root.request = newRequest(root.request.id, root.request.kind, root.actor, root.request.parameters);
        const fake = fakeGitHub([root]);
        const read = () => readWorkQueueLog({ githubClient: fake.githubClient, owner: "OWNER", repo: "REPO" });
        if (repository === "foreign/repository") {
          await assert.rejects(read(), error => error.code === "actor_unauthorized");
          const request = newRequest("foreign-ledger-zero-grant", "dispatch_next", dispatcher, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 });
          await assert.rejects(publishWorkQueueRequest(options(fake, request)), error => error.code === "actor_unauthorized");
        } else assert.equal((await read()).state.repository, repository);
        assert.equal(fake.state.updates, 0);
      }
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
      await assert.rejects(publishWorkQueueRequest(options(fake, request, dispatcher, { context: context(dispatcher, { roles: ["dispatcher", "administrator"] }) })), error => error.code === "policy_missing");
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
          }),
        })
      );
      const member = result.assignments[0].claims[0];
      assert.equal(result.publishedNow, true);
      assert.equal(result.reused, false);
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
      const publication = options(fake, request);
      delete publication.commitId;
      const result = await publishWorkQueueRequest(publication);
      const expected = `q_${createHash("sha256")
        .update(`${initial.at(-1).id}\n${request.id}`, "utf8")
        .digest("hex")}`;
      assert.equal(result.commit.id, expected);
      assert.equal(result.assignments[0].commit_id, expected);
      const empty = fakeGitHub();
      const operation = genesis().operations[0];
      const bootstrap = newRequest("canonical-genesis", "policy", administrator, { operations: [operation] });
      const rootPublication = options(empty, bootstrap, administrator);
      delete rootPublication.commitId;
      const installed = await publishWorkQueueRequest(rootPublication);
      assert.equal(installed.commit.id, `q_${createHash("sha256").update(bootstrap.id, "utf8").digest("hex")}`);
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
module.exports = { fakeGitHub, registerTests };
