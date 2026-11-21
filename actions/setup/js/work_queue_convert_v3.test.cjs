import { describe, expect, it } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");
const { canonical } = require("./work_queue_codec.cjs");
const { parseTransactionLog, replayTransactions } = require("./work_queue_replay.cjs");
const { buildPolicyEnv } = require("./work_queue_settings.cjs");
const { convertV3Contents, convertV3Ledger } = require("./work_queue_convert_v3.cjs");
const { loadQueue } = require("./work_queue_binding.cjs");
const { nodeId } = require("./work_queue_graph.cjs");

// The live v3 checkpoint (github/gh-aw work-queue@f7deec4) with payloads truncated.
const v3 = fs.readFileSync(path.join(import.meta.dirname, "fixtures/work_queue_v3_checkpoint.jsonl"), "utf8");
const v3State = JSON.parse(v3).operations[0].state;
const administrator = { role: "administrator", repository: "github/gh-aw", workflow: ".github/workflows/agentics-maintenance.yml", run_id: "77", run_attempt: 1 };
const available = Object.values(v3State.works).filter(work => work.state === "available");
const workers = [...new Set(Object.values(v3State.works).map(work => work.worker_profile))];

function policy(workflows = workers) {
  return buildPolicyEnv({ repository: "github/gh-aw", workflows, settings: { pools: { "smoke-work-queue": { concurrency: 3 }, "daily-reports": {}, "engine-conformance": {} } } });
}

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
    expect(state.works.size).toBe(72 - skipped);
    expect(warnings).toHaveLength(skipped);
  });

  it("applies ownership events of v3 lines after the checkpoint", () => {
    const [first, second] = available;
    const later = [
      { version: 3, operations: [{ kind: "Claim", work_id: first.work_id }] },
      { version: 3, operations: [{ kind: "Claim", work_id: second.work_id }] },
      { version: 3, operations: [{ kind: "ClaimCancellation", work_id: second.work_id }] },
      { version: 3, operations: [{ ...first, kind: "Work", work_id: nodeId(first.graph_id, "late"), node_key: "late", issue_link: undefined }] },
    ];
    const contents = v3 + later.map(line => JSON.stringify(line) + "\n").join("");
    const { state } = convertV3Contents(contents, administrator, policy(), () => {}, 1);
    expect(state.works.has(first.work_id)).toBe(false);
    expect(state.works.has(second.work_id)).toBe(true);
    expect(state.works.get(nodeId(first.graph_id, "late"))).toMatchObject({ state: "available", node_key: "late" });
  });

  it("starts an empty log from a v3 ledger without a checkpoint", () => {
    const warnings = [];
    const plain = '{"actor":{},"id":"q_1","operations":[{"kind":"Policy"}],"version":3}\n';
    expect(convertV3Contents(plain, administrator, policy(), message => warnings.push(message), 1).commits).toEqual([]);
    expect(warnings).toEqual(["v3 ledger without checkpoint; pending Work not carried"]);
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
