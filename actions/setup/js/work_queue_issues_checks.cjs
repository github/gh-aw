"use strict";

const assert = require("node:assert/strict");
const path = require("node:path");
const q = require("./work_queue_replay.cjs");
const helpers = require("./work_queue_test_helpers.cjs");
const { newWork } = require("./work_queue_graph.cjs");
const { defaultPolicy, validatePolicy } = require("./work_queue_policy.cjs");
const { main, ownProjectionTargets, workIssueStatus, summaryBody, journalPath } = require("./work_queue_issues.cjs");
const { issueBody } = require("./work_queue_issue_messages.cjs");
const { issueCompletionPolicy } = require("./work_queue_issue_contract.cjs");
const { STATUSES, discoverTarget, mutateIssues } = require("./work_queue_issue_api.cjs");
const { withProjectionLocks } = require("./work_queue_issue_coordination.cjs");
const { canonical, digest } = require("./work_queue_codec.cjs");
const ref = "0".repeat(40);
const resource = number => ({ kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "1", resource_id: String(1000 + number), number: String(number) });

/** @param {{count?: number, backing?: boolean, worker?: boolean, payload?: Record<string, unknown>}} [options] */
function fixture({ count = 1, backing = true, worker = false, payload = { task: "a" } } = {}) {
  const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
  policy.pools.default.profiles.default.max_claims = 16;
  policy.projectors = [".github/workflows/dispatcher.lock.yml", ".github/workflows/worker.lock.yml"].map(workflow => ({
    principal: "1001",
    workflow,
    ref,
    pools: ["default"],
    repositories: ["owner/repo"],
    ...(backing ? { backing_issues: Array.from({ length: count }, (_, index) => resource(index + 1)) } : {}),
  }));
  let log = [helpers.genesis(policy)];
  const nodes = Array.from({ length: count }, (_, index) => ({ ...newWork(payload, "graph", `n${index}`, "default", policy, 1), ...(backing ? { backing_issue: resource(index + 1) } : {}) }));
  log.push(helpers.commit("genesis", "admitted", "submit", helpers.dispatcher, { nodes }, nodes, 1));
  let origin = helpers.context(helpers.dispatcher, { ref });
  let assignment;
  if (worker) {
    const grant = helpers.grant(log, { max_claims: count, max_dispatches: 1 });
    log.push(grant.commit);
    log = helpers.bind(log, grant.assignments[0].dispatch_id);
    assignment = q.assignmentForDispatch(q.replayTransactions(log), grant.assignments[0].dispatch_id);
    origin = helpers.context(helpers.workerActor(q.replayTransactions(log), assignment.dispatch_id), { ref });
  }
  return { log, nodes, origin, assignment };
}

function protocolFixture() {
  const f = fixture({ count: 2, backing: false });
  const actor = { ...helpers.dispatcher, role: "projector" };
  const links = f.nodes.map((node, index) => ({ kind: "IssueLink", work_id: node.work_id, resource: resource(index + 1), projector_ref: ref }));
  f.log.push(helpers.operationCommit(f.log, "issue-links", "issue_link", links, actor));
  const comments = f.nodes.map((node, index) => ({ kind: "IssueComment", work_id: node.work_id, comment_id: `summary-${index}`, projector_ref: ref }));
  f.log.push(helpers.operationCommit(f.log, "issue-comments", "issue_link", comments, actor));
  return f;
}

function workerProtocolFixture() {
  const f = fixture({ count: 2, worker: true });
  f.log.push(helpers.finish(f.log, f.assignment.dispatch_id, "h1", "completed"));
  f.log.push(helpers.finish(f.log, f.assignment.dispatch_id, "h2", "cancelled"));
  const actor = { ...helpers.workerActor(q.replayTransactions(f.log), f.assignment.dispatch_id), role: "projector" };
  const comments = f.assignment.claims.flatMap((claim, index) => [
    { kind: "IssueComment", work_id: claim.work_id, comment_id: `summary-${index}`, projector_ref: ref, authority_claim_id: claim.claim_id },
    { kind: "IssueComment", work_id: claim.work_id, comment_id: `claim-${index}`, projector_ref: ref, claim_id: claim.claim_id },
  ]);
  f.log.push(helpers.operationCommit(f.log, "worker-comments", "issue_link", comments, actor));
  return f;
}

function mock(f, { field = false, labelMissing = false } = {}) {
  let transactions = f.log;
  let sha = "a".repeat(40);
  let sequence = 0;
  const journals = new Map();
  const issues = new Map(f.nodes.filter(node => node.backing_issue).map(node => [node.backing_issue.number, makeIssue(node.backing_issue)]));
  const comments = new Map();
  const calls = [];
  const locks = new Set();
  const warnings = [];
  const nativeRun = {
    id: Number(f.origin.run_id),
    run_attempt: f.origin.run_attempt,
    repository: { id: 1, full_name: "owner/repo" },
    path: f.origin.workflow,
    head_sha: ref,
    event: "workflow_dispatch",
    actor: { id: Number(f.origin.principal) },
    display_title: f.assignment ? `gh-aw work-queue ${f.assignment.dispatch_id}` : "dispatcher",
    created_at: new Date(0).toISOString(),
  };
  const discovery = {
    id: "Repository1",
    nameWithOwner: "owner/repo",
    label: labelMissing ? null : { id: "label-work", name: "work" },
    issueFields: { nodes: [{ __typename: "IssueFieldSingleSelect", id: "Field", name: "CustomStatus", options: STATUSES.map(name => ({ id: `option-${name}`, name })) }], pageInfo: { hasNextPage: false } },
  };
  function makeIssue(r) {
    return {
      id: `Issue${r.number}`,
      databaseId: Number(r.resource_id),
      number: Number(r.number),
      state: "OPEN",
      viewerCanSetFields: true,
      repository: { id: "Repository1", databaseId: 1, nameWithOwner: "owner/repo" },
      author: { login: "bot" },
      labels: { nodes: [{ id: "label-work", name: "work" }], pageInfo: { hasNextPage: false } },
      issueFieldValues: { nodes: [], pageInfo: { hasNextPage: false } },
    };
  }
  const githubClient = {
    rest: {
      repos: {
        get: async () => {
          calls.push(["authentication-repository"]);
          return { status: 200, data: { id: 1, full_name: "owner/repo" } };
        },
      },
      actions: {
        getWorkflowRun: async () => {
          calls.push(["authentication-run"]);
          return { status: 200, data: structuredClone(nativeRun) };
        },
      },
      git: {
        getRef: async ({ ref }) => {
          calls.push(["ledger-ref"]);
          assert.equal(ref, "heads/work-queue");
          return { data: { object: { sha } } };
        },
        getCommit: async ({ commit_sha }) => {
          calls.push(["ledger-commit"]);
          assert.equal(commit_sha, sha);
          return { data: { tree: { sha: "b".repeat(40) } } };
        },
        getTree: async ({ tree_sha }) => {
          calls.push(["ledger-tree"]);
          assert.equal(tree_sha, "b".repeat(40));
          const text = transactions.map(commit => canonical(commit)).join("\n") + "\n";
          return { data: { truncated: false, tree: [{ path: "work-queue.jsonl", type: "blob", mode: "100644", sha: digest(text) }] } };
        },
        getBlob: async ({ file_sha }) => {
          calls.push(["ledger-blob"]);
          const text = transactions.map(commit => canonical(commit)).join("\n") + "\n";
          assert.equal(file_sha, digest(text));
          return { data: { encoding: "base64", content: Buffer.from(text).toString("base64"), size: Buffer.byteLength(text) } };
        },
        createRef: async ({ ref }) => {
          calls.push(["lock", ref]);
          if (locks.has(ref)) throw Object.assign(new Error("held"), { status: 422 });
          locks.add(ref);
        },
        deleteRef: async ({ ref }) => {
          calls.push(["unlock", ref]);
          locks.delete(`refs/${ref}`);
        },
      },
    },
    graphql: async (query, variables) => {
      calls.push([query.startsWith("mutation") ? "mutation" : "read", query, variables]);
      if (query.includes("WorkQueueProjectionLocks")) {
        const data = {};
        const errors = [];
        for (const [key, input] of Object.entries(variables)) {
          if (!/^l\d+$/.test(key)) continue;
          if (locks.has(input.name)) errors.push({ path: [key], type: "UNPROCESSABLE" });
          else {
            locks.add(input.name);
            data[key] = { ref: { id: input.name, prefix: "refs/heads/", name: input.name.slice("refs/heads/".length), target: { oid: input.oid } } };
          }
        }
        if (errors.length) throw Object.assign(new Error("held"), { data, errors });
        return data;
      }
      if (query.includes("WorkQueueProjectionUnlocks")) {
        const data = {};
        for (const [key, input] of Object.entries(variables))
          if (/^u\d+$/.test(key)) {
            assert.ok(locks.delete(input.refId));
            data[key] = { clientMutationId: input.clientMutationId };
          }
        return data;
      }
      if (query.includes("CheckedWorkQueuePublication")) {
        const input = variables.input;
        await writeCheckedFiles({ expectedHeadOid: input.expectedHeadOid, files: input.fileChanges.additions.map(file => ({ path: file.path, content: Buffer.from(file.contents, "base64").toString("utf8") })) });
        calls.pop();
        return { createCommitOnBranch: { commit: { oid: sha } } };
      }
      if (query.includes("CheckedWorkQueue(")) {
        const text = transactions.map(commit => canonical(commit)).join("\n") + "\n";
        const blob = { oid: digest(text), text, byteSize: Buffer.byteLength(text), isTruncated: false };
        const directory = [...journals].map(([path, value]) => ({ name: path.split("/").at(-1), oid: digest(value), type: "blob", mode: 33188 }));
        const repository = {
          id: "Repository1",
          nameWithOwner: "owner/repo",
          isEmpty: false,
          defaultBranchRef: { target: { oid: "b".repeat(40) } },
          ref: {
            target: {
              oid: sha,
              tree: {
                oid: "b".repeat(40),
                entries: [
                  { name: "work-queue.jsonl", type: "blob", mode: 33188, oid: blob.oid },
                  { name: ".gh-aw", object: { entries: [{ name: "issue-projection", object: { entries: directory } }] } },
                ],
              },
            },
          },
          log: blob,
        };
        for (const [key, path] of Object.entries(variables)) {
          if (!/^j\d+$/.test(key)) continue;
          const journal = journals.get(path.slice(path.indexOf(":") + 1));
          repository[key] = journal ? { oid: digest(journal), text: canonical(journal), byteSize: Buffer.byteLength(canonical(journal)), isTruncated: false } : null;
        }
        const response = { repository, ...preflightResponse(variables) };
        for (const key of Object.keys(variables)) if (/^d\d+Name$/.test(key)) response[key.slice(0, -4)] = structuredClone(discovery);
        return response;
      }
      if (query.includes("WorkQueueIssueTarget")) return { repository: structuredClone(discovery) };
      if (query.includes("WorkQueueIssuePreflight")) return preflightResponse(variables);
      if (query.includes("WorkQueueTrackingLabel")) {
        discovery.label = { id: "label-work", name: variables.input.name };
        return { createLabel: { label: structuredClone(discovery.label) } };
      }
      if (query.includes("WorkQueueIssueProjection")) {
        const result = {};
        for (const [key, input] of Object.entries(variables)) {
          if (!/^m\d+$/.test(key)) continue;
          const name = query.match(new RegExp(`${key}: (\\w+)\\(`))[1];
          const issue = [...issues.values()].find(issue => issue.id === (input.issueId ?? input.labelableId ?? input.subjectId));
          if (name === "createIssue") {
            const number = issues.size + 1;
            const created = makeIssue(resource(number));
            created.body = input.body;
            created.title = input.title;
            if (input.issueFields) created.issueFieldValues.nodes = input.issueFields.map(value => ({ field: { id: value.fieldId }, optionId: value.singleSelectOptionId }));
            issues.set(String(number), created);
            result[key] = { issue: structuredClone(created) };
          } else if (name === "addComment") {
            const comment = { id: `Comment${++sequence}`, body: input.body, issue: { id: issue.id } };
            comments.set(comment.id, comment);
            result[key] = { commentEdge: { node: structuredClone(comment) } };
          } else if (name === "updateIssueComment") {
            comments.get(input.id).body = input.body;
            result[key] = { issueComment: structuredClone(comments.get(input.id)) };
          } else if (name === "setIssueFieldValue") {
            issue.issueFieldValues.nodes = [
              ...issue.issueFieldValues.nodes.filter(value => value.field.id !== input.issueFields[0].fieldId),
              ...input.issueFields.map(value => ({ field: { id: value.fieldId }, optionId: value.singleSelectOptionId })),
            ];
            result[key] = { issue: structuredClone(issue) };
          } else if (name === "addLabelsToLabelable") {
            issue.labels.nodes.push({ id: "label-work", name: "work" });
            result[key] = { labelable: { id: issue.id, labels: structuredClone(issue.labels) } };
          } else if (name === "closeIssue") {
            issue.state = "CLOSED";
            result[key] = { issue: structuredClone(issue) };
          } else throw new Error(`unexpected mutation ${name}`);
        }
        return result;
      }
      throw new Error(`unexpected query ${query}`);
    },
  };
  function preflightResponse(variables) {
    const result = {};
    for (const [key, value] of Object.entries(variables)) {
      if (/^n\d+$/.test(key)) result[`i${key.slice(1)}`] = { issue: structuredClone(issues.get(String(value)) ?? null) };
      if (/^c\d+_/.test(key)) result[key] = structuredClone(comments.get(value) ?? null);
    }
    return result;
  }
  const readCheckedQueue = async () => {
    calls.push(["ledger-read"]);
    return { sha, treeSha: "tree", transactions, state: q.replayTransactions(transactions), branch: "work-queue", logPath: "work-queue.jsonl", journal: new Map([...journals].map(([path, value]) => [path, structuredClone(value)])) };
  };
  const writeCheckedFiles = async ({ expectedHeadOid, files }) => {
    calls.push(["publish"]);
    assert.equal(expectedHeadOid, sha);
    for (const file of files) {
      if (file.path === "work-queue.jsonl") transactions = q.parseTransactionLog(file.content);
      else journals.set(file.path, JSON.parse(file.content));
    }
    sha = (++sequence).toString(16).padStart(40, "0");
    return sha;
  };
  const options = {
    githubClient,
    context: { repo: { owner: "owner", repo: "repo" }, payload: f.assignment ? { inputs: { work_queue_assignment: f.assignment } } : {} },
    role: f.assignment ? "worker" : "dispatcher",
    trustedContext: f.origin,
    issues: field ? { "status-field": "CustomStatus" } : true,
    now: 100,
    readCheckedQueue,
    writeCheckedFiles,
    sleep: async () => {},
    core: { info: () => {}, warning: message => warnings.push(message) },
  };
  return {
    options,
    calls,
    locks,
    issues,
    comments,
    journals,
    discovery,
    warnings,
    nativeRun,
    state: () => q.replayTransactions(transactions),
    setLog: value => {
      transactions = value;
      sha = (++sequence).toString(16).padStart(40, "0");
    },
  };
}

function registerTests({ describe, it, beforeEach, afterEach }) {
  let prompts;
  beforeEach(() => {
    prompts = process.env.GH_AW_PROMPTS_DIR;
    process.env.GH_AW_PROMPTS_DIR = path.resolve(__dirname, "../md");
  });
  afterEach(() => {
    if (prompts === undefined) delete process.env.GH_AW_PROMPTS_DIR;
    else process.env.GH_AW_PROMPTS_DIR = prompts;
  });
  describe("scoped Git-authoritative Issue projection", () => {
    it("creates, binds, and recovers known native receipts without duplicate Issues or comments", async () => {
      const f = fixture({ backing: false });
      const m = mock(f);
      const first = await main(m.options);
      assert.deepEqual(first.pending, []);
      assert.equal(m.issues.size, 1);
      assert.equal(m.comments.size, 1);
      assert.equal(m.state().works.get(f.nodes[0].work_id).issue_link.resource_id, "1001");
      const before = m.calls.length;
      const second = await main(m.options);
      assert.deepEqual(second.pending, []);
      assert.equal(m.issues.size, 1);
      assert.equal(m.comments.size, 1);
      assert.equal(m.calls.slice(before).filter(call => call[0] === "mutation").length, 0);
      assert.equal(m.locks.size, 0);
    });
    it("updates comment-only status on the canonical summary", async () => {
      const f = fixture();
      const m = mock(f);
      await main(m.options);
      assert.match([...m.comments.values()][0].body, /Status: \*\*Queued\*\*/);
      const grant = helpers.grant(f.log);
      m.setLog([...m.state().transactions, { ...grant.commit, previous: m.state().tip }]);
      await main(m.options);
      assert.match([...m.comments.values()][0].body, /Status: \*\*Assigned\*\*/);
      assert.match([...m.comments.values()][0].body, /Generated by \[dispatcher\]/);
      assert.equal(m.comments.size, 1);
    });
    it("writes selective native fields and preserves unrelated values", async () => {
      const f = fixture();
      const m = mock(f, { field: true });
      m.issues.get("1").issueFieldValues.nodes.push({ field: { id: "human-field" }, optionId: "human-option" });
      assert.deepEqual((await main(m.options)).pending, []);
      assert.ok(m.issues.get("1").issueFieldValues.nodes.some(value => value.field.id === "human-field"));
      assert.ok(m.issues.get("1").issueFieldValues.nodes.some(value => value.optionId === "option-Queued"));
      assert.doesNotMatch([...m.comments.values()][0].body, /Status:/);
      const before = m.calls.length;
      const grant = helpers.grant(m.state().transactions);
      m.setLog([...m.state().transactions, grant.commit]);
      assert.deepEqual((await main(m.options)).pending, []);
      const writes = m.calls
        .slice(before)
        .filter(call => call[0] === "mutation")
        .map(call => call[1])
        .join("\n");
      assert.match(writes, /setIssueFieldValue/);
      assert.doesNotMatch(writes, /updateIssueComment/);
    });
    it("initializes and verifies fields during creation", async () => {
      const m = mock(fixture({ backing: false }), { field: true });
      assert.deepEqual((await main(m.options)).pending, []);
      assert.equal(m.issues.get("1").issueFieldValues.nodes[0].optionId, "option-Queued");
      assert.equal(m.calls.filter(call => call[0] === "mutation").length, 2);
    });
    it("keeps committed queue authority when fields are missing or unwritable", async () => {
      const f = fixture();
      const m = mock(f, { field: true });
      m.discovery.issueFields.nodes = [];
      const result = await main(m.options);
      assert.ok(result.pending);
      assert.equal(result.pending.length, 1);
      assert.match(result.pending[0].reason, /field sync pending/);
      assert.equal(m.state().works.size, 1);
      assert.ok(m.calls.filter(call => call[0] === "mutation").every(call => !call[1].includes("setIssueFieldValue")));
      assert.ok([...m.comments.values()].some(comment => /field sync pending/.test(comment.body) && !/Status:/.test(comment.body)));
      m.discovery.issueFields.nodes = [{ __typename: "IssueFieldSingleSelect", id: "Field", name: "CustomStatus", options: STATUSES.map(name => ({ id: name, name })) }];
      m.issues.get("1").viewerCanSetFields = false;
      const unwritable = await main(m.options);
      assert.ok(unwritable.pending);
      assert.match(unwritable.pending[0].reason, /token cannot set fields/);
    });
    it("repairs labels only on owned Issues and preserves human-owned text and closure", async () => {
      const f = fixture();
      const m = mock(f);
      m.issues.get("1").title = "A human title";
      m.issues.get("1").body = "A human discussion starter.";
      m.issues.get("1").labels.nodes = [{ id: "human", name: "human" }];
      m.issues.get("1").state = "CLOSED";
      await main(m.options);
      assert.deepEqual(
        m.issues.get("1").labels.nodes.map(value => value.name),
        ["human", "work"]
      );
      assert.equal(m.issues.get("1").state, "CLOSED");
      assert.equal(m.issues.get("1").title, "A human title");
      assert.equal(m.issues.get("1").body, "A human discussion starter.");
      const writes = m.calls
        .filter(call => call[0] === "mutation")
        .map(call => call[1])
        .join("\n");
      assert.doesNotMatch(writes, /updateIssue\(/);
      assert.equal(m.state().works.values().next().value.barrier, "none");
    });
    it("keeps per-Claim historical comments for mixed original assignments", async () => {
      const f = fixture({ count: 2, worker: true });
      f.log.push(helpers.finish(f.log, f.assignment.dispatch_id, "h1", "completed"));
      f.log.push(helpers.finish(f.log, f.assignment.dispatch_id, "h2", "cancelled"));
      const m = mock(f);
      assert.deepEqual((await main(m.options)).pending, []);
      assert.equal(m.comments.size, 4);
      const claims = [...m.state().claims.values()];
      assert.ok(claims.every(claim => claim.issue_comment));
      assert.notEqual(claims[0].issue_comment, claims[1].issue_comment);
      assert.ok([...m.comments.values()].some(comment => /Verifying/.test(comment.body)));
    });
    it("does not inherit another workflow's admissions or attempt-one Claims on reruns", () => {
      const f = fixture({ worker: true });
      const state = q.replayTransactions(f.log);
      assert.equal(ownProjectionTargets(state, helpers.context({ ...helpers.dispatcher, workflow: ".github/workflows/other.lock.yml" }), ref).targets.length, 0);
      assert.throws(() => ownProjectionTargets(state, { ...f.origin, run_attempt: 2 }, ref, f.assignment), /original authenticated Claims|native attempt 1/);
      const foreign = structuredClone(f.assignment);
      foreign.claims[0].work_id = "foreign";
      assert.throws(() => ownProjectionTargets(state, f.origin, ref, foreign), /assignment_mismatch/);
    });
    it("uses restored admission and policy history instead of suffix transaction offsets", () => {
      const checkpoint = require("../../../specs/work-queue/fixtures/checkpoint.json");
      const state = q.replayTransactions(checkpoint.checkpoint);
      const work = state.works.values().next().value;
      const originalAdmission = { role: "dispatcher", principal: "1001", repository: "owner/repo", workflow: ".github/workflows/dispatcher.lock.yml", run_id: "100", run_attempt: 1 };
      const otherRun = { ...originalAdmission, run_id: "999" };
      const projector = { principal: "1001", workflow: originalAdmission.workflow, ref, pools: [work.pool], repositories: ["owner/repo"] };
      state.workCreators.set(work.work_id, originalAdmission);
      state.policy.projectors = [{ ...projector, completion_policy: "keep-open" }];
      state.historicalTransactions[0].operations[0].policy = state.policy;
      state.transactions.push({
        actor: otherRun,
        operations: [{ kind: "Policy", policy: { projectors: [{ ...projector, completion_policy: "close-on-result" }] } }],
      });
      assert.equal(ownProjectionTargets(state, helpers.context(otherRun, { ref }), ref).targets.length, 0);
      assert.equal(ownProjectionTargets(state, helpers.context(originalAdmission, { ref }), ref).targets.length, 1);
      assert.equal(issueCompletionPolicy(state, work, { ...originalAdmission, role: "projector" }, ref, "owner/repo"), "keep-open");
      assert.ok(summaryBody(state, work, false, 10).includes(`/actions/runs/${originalAdmission.run_id}`));
      assert.ok(issueBody(state, work, helpers.context(otherRun, { ref }), { label: "work" }, "work-queue").includes(`/actions/runs/${originalAdmission.run_id}`));
    });
    it("does not treat configuration or API access as installed projector authority", async () => {
      const f = fixture();
      delete f.log[0].operations[0].policy.projectors;
      f.log[0].request = q.newRequest(f.log[0].request.id, "policy", f.log[0].actor, { operations: f.log[0].operations });
      const m = mock(f);
      const result = await main(m.options);
      assert.ok(result.pending);
      assert.match(result.pending[0].reason, /installed projector/);
      assert.equal(m.calls.filter(call => call[0] === "mutation").length, 0);
    });
    it("rejects empty projector rules and noncanonical repository names", () => {
      const policy = fixture().log[0].operations[0].policy;
      assert.throws(() => validatePolicy({ ...policy, projectors: [] }), /projectors require 1..256/);
      for (const repository of ["owner:bad/repo", "owner/repo:bad", "owner/repo/path", "owner/rep\u00f3", "owner/repo name"]) {
        const invalid = structuredClone(policy);
        invalid.projectors[0].repositories = [repository];
        assert.throws(() => validatePolicy(invalid), /policy_invalid/);
      }
      const valid = structuredClone(policy);
      valid.projectors[0].repositories = ["owner-name_1/repo.name-2"];
      delete valid.projectors[0].backing_issues;
      assert.equal(validatePolicy(valid), valid);
    });
    it("validates trusted closure policies and bounded exact Issue grants", () => {
      const policy = fixture().log[0].operations[0].policy;
      for (const completion of ["keep-open", "close-on-result"]) {
        const valid = structuredClone(policy);
        valid.projectors[0].completion_policy = completion;
        assert.equal(validatePolicy(valid), valid);
      }
      for (const grant of [
        [],
        Array(257).fill(resource(1)),
        [{ ...resource(1), kind: "pull_request" }],
        [{ ...resource(1), host: "other.example" }],
        [{ ...resource(1), resource_id: "0" }],
        [{ ...resource(1), repository: "owner/other" }],
      ]) {
        const invalid = structuredClone(policy);
        invalid.projectors[0].backing_issues = grant;
        assert.throws(() => validatePolicy(invalid), /policy_invalid/);
      }
      for (const completion of ["", "close", true]) {
        const invalid = structuredClone(policy);
        invalid.projectors[0].completion_policy = completion;
        assert.throws(() => validatePolicy(invalid), /policy_invalid/);
      }
    });
    it("does no live calls when staged or disabled", async () => {
      const m = mock(fixture());
      assert.equal((await main({ ...m.options, staged: true })).staged, true);
      assert.equal((await main({ ...m.options, issues: false })).disabled, true);
      assert.equal(m.calls.length, 0);
    });
    it("leaves ambiguous Issue creation pending rather than blindly retrying", async () => {
      const f = fixture({ backing: false });
      const m = mock(f);
      const original = m.options.githubClient.graphql;
      m.options.githubClient.graphql = async (query, variables) => {
        if (query.includes("WorkQueueIssueProjection")) {
          await original(query, variables);
          throw new Error("lost response");
        }
        return original(query, variables);
      };
      const result = await main(m.options);
      assert.ok(result.pending);
      assert.match(result.pending[0].reason, /ambiguous/);
      assert.equal(m.issues.size, 1);
      assert.ok(m.locks.size > 0);
      await main(m.options);
      assert.equal(m.issues.size, 1);
    });
    it("recovers a receipt awaiting linkage, never a marker-only Issue", async () => {
      const f = fixture({ backing: false });
      const m = mock(f);
      const received = mock(fixture()).issues.get("1");
      m.issues.set("1", received);
      m.journals.set(journalPath(f.nodes[0].work_id), {
        version: 1,
        work_id: f.nodes[0].work_id,
        comments: {},
        create: { nonce: "native-receipt", origin: { ...helpers.dispatcher, role: "projector" }, ref, resource: resource(1), node_id: received.id },
      });
      assert.deepEqual((await main(m.options)).pending, []);
      assert.equal(m.issues.size, 1);
      assert.equal(m.state().works.get(f.nodes[0].work_id).issue_link.number, "1");
    });
    it("holds coordination across a fresh read and prevents stale projector overwrite", async () => {
      const m = mock(fixture());
      let unlock;
      const wait = new Promise(resolve => {
        unlock = resolve;
      });
      let entered;
      const ready = new Promise(resolve => {
        entered = resolve;
      });
      const first = withProjectionLocks({ githubClient: m.options.githubClient, owner: "owner", repo: "repo", head: "a".repeat(40), keys: ["work:a"] }, async () => {
        entered();
        await wait;
        return { ambiguous: false };
      });
      await ready;
      await assert.rejects(() => withProjectionLocks({ githubClient: m.options.githubClient, owner: "owner", repo: "repo", head: "b".repeat(40), keys: ["work:a"] }, () => assert.fail("stale writer overlapped")), /held/);
      unlock();
      await first;
      assert.equal(m.locks.size, 0);
    });
    it("counts bounded batches, coordination, binding publication, drift repair, and no-op requests", async () => {
      const m = mock(fixture({ count: 25 }));
      assert.deepEqual((await main(m.options)).pending, []);
      assert.equal(m.comments.size, 25);
      assert.equal(m.calls.filter(call => call[0] === "mutation").length, 1);
      assert.equal(m.calls.filter(call => call[0] === "lock").length, 50);
      const before = m.calls.length;
      await main(m.options);
      assert.equal(m.calls.slice(before).filter(call => call[0] === "mutation").length, 0);
      // 2 checked reads + discovery + scoped preflight + 100 coordination requests.
      assert.equal(m.calls.length - before, 104);
    });
    it("handles partial native batches without retrying successful aliases", async () => {
      const client = {
        graphql: async () => {
          throw Object.assign(new Error("partial"), { data: { m0: { issue: { id: "ok" } } }, errors: [{ type: "FORBIDDEN", path: ["m1"], message: "denied" }] });
        },
      };
      const batch = await mutateIssues(
        client,
        [0, 1].map(key => ({ key, name: "createIssue", type: "CreateIssueInput", input: {}, selection: "issue { id }" }))
      );
      assert.equal(batch.results[0].pending, false);
      assert.equal(batch.results[1].pending, true);
      assert.equal(batch.ambiguous, false);
    });
    it("does not derive Done from Completion, PR delivery, or a human status edit", () => {
      const f = fixture();
      const state = q.replayTransactions(f.log);
      const work = [...state.works.values()][0];
      work.state = "completed";
      work.barrier = "pending";
      assert.equal(workIssueStatus(state, work, 100), "Verifying");
      work.barrier = "verified";
      work.result = { outputs: [{ type: "create_pull_request", resource: { kind: "pull_request" } }] };
      assert.equal(workIssueStatus(state, work, 100), "Needs review");
      work.barrier = "failed";
      assert.equal(workIssueStatus(state, work, 100), "Needs attention");
      assert.doesNotMatch(summaryBody(state, work, { id: "Field" }, 100), /Status:/);
    });
    it("rejects concurrent Issue rebinding and immutable comment-handle replacement", () => {
      const f = protocolFixture();
      const actor = { ...helpers.dispatcher, role: "projector" };
      const attempt = operation => q.replayTransactions([...f.log, helpers.operationCommit(f.log, "conflict", "issue_link", [operation], actor)]);
      assert.throws(() => attempt({ kind: "IssueLink", work_id: f.nodes[1].work_id, resource: resource(1), projector_ref: ref }), /cannot be rebound|one Work/);
      assert.throws(() => attempt({ kind: "IssueComment", work_id: f.nodes[0].work_id, comment_id: "other", projector_ref: ref }), /cannot be rebound/);
    });
  });
}

if (require.main === module && process.argv.includes("--fixture")) process.stdout.write(JSON.stringify(protocolFixture().log));
if (require.main === module && process.argv.includes("--worker-fixture")) process.stdout.write(JSON.stringify(workerProtocolFixture().log));
module.exports = { registerTests, fixture, protocolFixture, mock };
