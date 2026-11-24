import { describe, expect, it } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");
const { canonical } = require("./work_queue_codec.cjs");
const { parseTransactionLog, replayTransactions } = require("./work_queue_replay.cjs");
const { convertV3Contents, convertV3Ledger } = require("./work_queue_convert_v3.cjs");
const { loadQueue } = require("./work_queue_binding.cjs");
const { dependencyStatus, nodeId } = require("./work_queue_graph.cjs");

// The live v3 checkpoint (github/gh-aw work-queue@f7deec4) with payloads truncated.
const v3 = fs.readFileSync(path.join(import.meta.dirname, "fixtures/work_queue_v3_checkpoint.jsonl"), "utf8");
const v3State = JSON.parse(v3).operations[0].state;
const administrator = { role: "administrator", repository: "github/gh-aw", workflow: ".github/workflows/agentics-maintenance.yml", run_id: "77", run_attempt: 1 };
const available = Object.values(v3State.works).filter(work => work.state === "available");
const workers = [...new Set(Object.values(v3State.works).map(work => work.worker_profile))].sort();
const POOLS = {
  default: { concurrency: 16, per_account_limit: 0, max_attempts: 3 },
  "smoke-work-queue": { concurrency: 3, per_account_limit: 0, max_attempts: 3 },
  "daily-reports": { concurrency: 16, per_account_limit: 0, max_attempts: 3 },
  "engine-conformance": { concurrency: 16, per_account_limit: 0, max_attempts: 3 },
};

function policy(approved = workers, pools = POOLS) {
  return { version: 4, pending_limit: 4096, issues: false, effect_scope: "github/gh-aw", workers: approved, pools };
}

const checkpointId = JSON.parse(v3).id;
const options = fake => ({ githubClient: fake.githubClient, owner: "github", repo: "gh-aw", actor: administrator, policy: policy(), core: { info() {}, warning() {} } });

/** v3 lines as the v3 store wrote them: canonical, each extending the previous commit. */
function chain(commits, previous = checkpointId, prefix = "q_late") {
  return commits
    .map((commit, index) => {
      const line = { actor: { repository: "github/gh-aw", role: "dispatcher" }, at: 1795099400000 + index, id: `${prefix}_${index}`, previous, request: { kind: "observe" }, version: 3, ...commit };
      previous = line.id;
      return canonical(line) + "\n";
    })
    .join("");
}

/** A v3 Work operation in `base`'s graph, pool and worker profile. */
function v3Work(base, node_key, depends_on = []) {
  const { pool, priority, fairness_key, worker_profile, batch_trust_domain, payload, enqueued } = base;
  return { kind: "Work", work_id: nodeId(base.graph_id, node_key), graph_id: base.graph_id, node_key, pool, priority, fairness_key, worker_profile, batch_trust_domain, payload, depends_on, enqueued };
}

const after = (base, key) => ({ kind: "work", work_id: nodeId(base.graph_id, key) });
const finished = (work_id, result = true) => [
  { operations: [{ kind: "Claim", work_id, claim_id: `c_${work_id.slice(0, 8)}` }] },
  { operations: [{ kind: "Completion", work_id, claim_id: `c_${work_id.slice(0, 8)}` }] },
  ...(result ? [{ operations: [{ kind: "Result", work_id, claim_id: `c_${work_id.slice(0, 8)}` }] }] : []),
];

describe("v3 to v4 conversion", () => {
  it("carries exactly the available v3 Work as v4 migrate commits", () => {
    const warnings = [];
    const { commits, state } = convertV3Contents(v3, administrator, policy(), message => warnings.push(message), 1);
    expect(warnings).toEqual([]);
    expect(commits.every(commit => commit.version === 4 && commit.request.kind === "migrate" && commit.actor.role === "administrator")).toBe(true);
    expect(commits[0].previous).toBeNull();
    expect(available).toHaveLength(72);
    expect([...state.works.keys()].sort()).toEqual(available.map(work => work.work_id).sort());
    expect([...state.works.values()].filter(work => work.pool === "smoke-work-queue")).toHaveLength(3);
    expect(new Set([...state.works.values()].map(work => work.state))).toEqual(new Set(["available"]));
    // v3 FIFO order survives as v4 positions; linked Issues stay linked.
    const byV3Position = [...available].sort((a, b) => a.position.commit - b.position.commit || a.position.operation - b.position.operation).map(work => work.work_id);
    const byV4Position = [...state.works.values()].sort((a, b) => a.position.commit - b.position.commit || a.position.operation - b.position.operation).map(work => work.work_id);
    expect(byV4Position).toEqual(byV3Position);
    for (const work of available) expect(state.works.get(work.work_id).backing_issue).toEqual(work.issue_link);
    expect(replayTransactions(parseTransactionLog(commits.map(commit => canonical(commit) + "\n").join(""))).stats.available).toBe(72);
  });

  it("skips Work whose pool or worker is no longer configured, with a warning each", () => {
    const warnings = [];
    const { state } = convertV3Contents(v3, administrator, policy(workers.filter(name => name !== "deep-report")), message => warnings.push(message), 1);
    const skipped = available.filter(work => work.worker_profile === "deep-report").length;
    expect(skipped).toBeGreaterThan(0);
    expect(state.works.size).toBe(72 - skipped);
    expect(warnings).toHaveLength(skipped);
    expect(warnings.every(warning => warning.endsWith("/ profile deep-report is not configured"))).toBe(true);

    const { "daily-reports": _removed, ...remaining } = POOLS;
    const unpooled = [];
    const pooled = convertV3Contents(v3, administrator, policy(workers, remaining), message => unpooled.push(message), 1).state;
    const dropped = available.filter(work => work.pool === "daily-reports");
    expect(dropped.length).toBeGreaterThan(0);
    expect(pooled.works.size).toBe(72 - dropped.length);
    expect(unpooled).toHaveLength(dropped.length);
    expect(unpooled).toEqual(expect.arrayContaining(dropped.map(work => expect.stringContaining(`v3 Work ${work.work_id} skipped: pool daily-reports`))));
  });

  it("applies ownership events of v3 lines after the checkpoint", () => {
    const [first, second] = available;
    const later = [
      { operations: [{ kind: "Claim", work_id: first.work_id }] },
      { operations: [{ kind: "Claim", work_id: second.work_id }] },
      { operations: [{ kind: "ClaimCancellation", work_id: second.work_id }] },
      { operations: [v3Work(first, "late")] },
    ];
    const contents = v3 + chain(later);
    const { state } = convertV3Contents(contents, administrator, policy(), () => {}, 1);
    expect(state.works.has(first.work_id)).toBe(false);
    expect(state.works.has(second.work_id)).toBe(true);
    expect(state.works.get(nodeId(first.graph_id, "late"))).toMatchObject({ state: "available", node_key: "late" });
  });

  it("replays a v3 ledger without a checkpoint from its genesis and carries its pending Work, even past its v3 deadline", async () => {
    const [base] = available;
    const [pending, done, retried, cancelled, expired] = ["pending", "done", "retried", "cancelled", "expired"].map(key => v3Work(base, key));
    const contents = chain(
      [
        { request: { kind: "policy" }, actor: { repository: "github/gh-aw", role: "administrator" }, operations: [{ kind: "Policy", epoch: "e1" }] },
        { request: { kind: "submit" }, operations: [pending, done, retried, cancelled, { ...expired, expires_at: 1 }] },
        ...finished(done.work_id),
        { operations: [{ kind: "Claim", work_id: retried.work_id }] },
        { operations: [{ kind: "ClaimCancellation", work_id: retried.work_id }] },
        { operations: [{ kind: "WorkCancellation", work_id: cancelled.work_id }] },
        { operations: [{ kind: "WorkPriority", work_id: pending.work_id, priority: 1 }] },
      ],
      null,
      "q_root"
    );
    const warnings = [];
    const { state } = convertV3Contents(contents, administrator, policy(), message => warnings.push(message), 1795099500000);
    // Conversion reads no time: Work whose v3 deadline passed is carried, without the deadline.
    expect([...state.works.keys()]).toEqual([pending.work_id, retried.work_id, expired.work_id]);
    expect(state.works.get(pending.work_id)).toMatchObject({ state: "available", priority: 1 });
    expect(warnings).toEqual([`v3 Work ${expired.work_id} carried without its deadline: v4 Work does not expire`]);

    const fake = h.fakeQueueGitHub(contents, { repository: "github/gh-aw" });
    expect(await convertV3Ledger(options(fake))).toMatchObject({ converted: true, carried: 3 });
    expect(replayTransactions(parseTransactionLog(fake.contents())).stats.available).toBe(3);
  });

  it.each([
    ["an unreadable line", v3 + chain([{ operations: [v3Work(available[0], "late")] }]) + "{not json\n"],
    ["a line that does not extend the previous commit", v3 + chain([{ operations: [v3Work(available[0], "late")] }], "q_elsewhere")],
    ["an event naming unknown Work", v3 + chain([{ operations: [{ kind: "Claim", work_id: nodeId("unknown", "work") }] }])],
    ["a root that is neither genesis nor Checkpoint", chain([{ operations: [v3Work(available[0], "late")] }], "q_truncated")],
    ["a Checkpoint after the root", v3 + chain([{ operations: JSON.parse(v3).operations }])],
  ])("aborts without moving the ref on %s", async (_name, contents) => {
    const fake = h.fakeQueueGitHub(contents, { repository: "github/gh-aw" });
    const head = fake.refs.get("work-queue");
    await expect(convertV3Ledger(options(fake))).rejects.toMatchObject({ code: "ledger_invalid" });
    expect(fake.refs.get("work-queue")).toBe(head);
    expect(fake.contents()).toBe(contents);
  });

  it("satisfies edges to predecessors completed with a verified v3 Result and keeps their successors", () => {
    const [base] = available;
    const verified = Object.values(v3State.works).find(work => work.state === "completed" && work.barrier === "verified" && work.pool === "default");
    const predecessor = v3Work(base, "predecessor");
    const dependent = v3Work(base, "dependent", [after(base, "predecessor")]);
    const successor = v3Work(base, "successor", [after(base, "dependent")]);
    const checkpointed = v3Work(verified, "after-checkpointed", [{ kind: "work", work_id: verified.work_id }]);
    const contents = v3 + chain([{ operations: [predecessor, dependent, successor, checkpointed] }, ...finished(predecessor.work_id)]);
    const warnings = [];
    const { state } = convertV3Contents(contents, administrator, policy(), message => warnings.push(message), 1);
    expect(state.works.has(predecessor.work_id)).toBe(false);
    expect(state.works.get(dependent.work_id)).toMatchObject({ state: "available", depends_on: [] });
    expect(state.works.get(successor.work_id)).toMatchObject({ state: "available", depends_on: [after(base, "dependent")] });
    expect(state.works.get(checkpointed.work_id)).toMatchObject({ state: "available", depends_on: [] });
    expect(dependencyStatus(state, state.works.get(dependent.work_id))).toEqual({ ready: true, reason: "ready" });
    expect(dependencyStatus(state, state.works.get(checkpointed.work_id))).toEqual({ ready: true, reason: "ready" });
    expect(dependencyStatus(state, state.works.get(successor.work_id))).toEqual({ ready: false, reason: "dependency_pending", work_id: dependent.work_id });
    expect(warnings).toHaveLength(2);
    expect(warnings.every(message => message.includes("completed with a verified v3 Result"))).toBe(true);
  });

  it("skips dependents of unresolved predecessors, transitively, with a warning each", () => {
    const [base] = available;
    const keys = ["unverified", "failed", "cancelled", "claimed"];
    const predecessors = keys.map(key => v3Work(base, key));
    const dependents = keys.map(key => v3Work(base, `after-${key}`, [after(base, key)]));
    const successor = v3Work(base, "successor", [after(base, "after-unverified")]);
    const unrelated = v3Work(base, "unrelated");
    const contents =
      v3 +
      chain([
        { operations: [...predecessors, ...dependents, successor, unrelated] },
        ...finished(predecessors[0].work_id, false),
        ...finished(predecessors[1].work_id, false),
        { operations: [{ kind: "DeliveryFailure", work_id: predecessors[1].work_id }] },
        { operations: [{ kind: "WorkCancellation", work_id: predecessors[2].work_id }] },
        { operations: [{ kind: "Claim", work_id: predecessors[3].work_id }] },
      ]);
    const warnings = [];
    const { state } = convertV3Contents(contents, administrator, policy(), message => warnings.push(message), 1);
    expect(state.works.size).toBe(72 + 1);
    expect(state.works.has(unrelated.work_id)).toBe(true);
    for (const work of [...predecessors, ...dependents, successor]) expect(state.works.has(work.work_id)).toBe(false);
    expect(warnings).toEqual([
      `v3 Work ${predecessors[3].work_id} skipped: it is still claimed by a v3 run that cannot finish into the v4 log`,
      `v3 Work ${dependents[0].work_id} skipped: predecessor ${predecessors[0].work_id} completed in v3 without a verified Result`,
      `v3 Work ${dependents[1].work_id} skipped: predecessor ${predecessors[1].work_id} failed delivery in v3`,
      `v3 Work ${dependents[2].work_id} skipped: predecessor ${predecessors[2].work_id} was cancelled in v3`,
      `v3 Work ${dependents[3].work_id} skipped: predecessor ${predecessors[3].work_id} was still claimed by a v3 run`,
      `v3 Work ${successor.work_id} skipped: predecessor ${dependents[0].work_id} is not carried`,
    ]);
  });

  it("writes the converted log once, as a child of the v3 head, and loads it for writing roles only", async () => {
    const fake = h.fakeQueueGitHub(v3, { repository: "github/gh-aw" });
    const head = fake.refs.get("work-queue");
    const options = { githubClient: fake.githubClient, owner: "github", repo: "gh-aw", actor: administrator, policy: policy(), core: { info() {}, warning() {} } };
    expect(await convertV3Ledger(options)).toMatchObject({ converted: true, carried: 72 });
    expect(fake.commits.get(fake.refs.get("work-queue")).parents).toEqual([head]);
    expect(await convertV3Ledger(options)).toEqual({ converted: false });
    expect(fake.contents().startsWith('{"actor":{"repository":"github/gh-aw","role":"administrator"')).toBe(true);

    const context = { repo: { owner: "github", repo: "gh-aw" }, runId: 5, runAttempt: 1 };
    const unconverted = h.fakeQueueGitHub(v3, { repository: "github/gh-aw" });
    await expect(loadQueue({ githubClient: unconverted.githubClient, context, role: "worker", policy: policy() })).rejects.toMatchObject({ code: "unsupported_protocol" });
    const loaded = await loadQueue({ githubClient: unconverted.githubClient, context, role: "dispatcher", policy: policy(), core: { info() {}, warning() {} } });
    expect(loaded.state.stats.available).toBe(72);
  });
});
