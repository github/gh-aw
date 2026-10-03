// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { main, renderSummary } from "./work_queue_summary.cjs";
import { replayTransactions, serializeTransactionLog } from "./work_queue_replay.cjs";

const work = id => ({ version: 1, kind: "Work", work: id, claim: null, attempt: null });
const claim = (id, claimId) => ({ version: 1, kind: "Claim", work: id, claim: claimId, attempt: null });
const initial = [work("done"), claim("done", "claim-a"), work("retry"), claim("retry", "claim-b"), work("cancel"), work("waiting")];
const latest = [
  ...initial,
  { version: 1, kind: "Completion", work: "done", claim: "claim-a", attempt: "run-1" },
  { version: 1, kind: "ClaimCancellation", work: "retry", claim: "claim-b", attempt: null },
  { version: 1, kind: "WorkCancellation", work: "cancel", claim: null, attempt: null },
  work("new"),
  claim("new", "claim-c"),
  claim("new", "claim-d"),
];
let tempDirectory;

function snapshot(transactions = initial, worker = null) {
  return { sha: "head", worker, projection: replayTransactions(transactions) };
}

function setup() {
  tempDirectory = fs.mkdtempSync(path.join(os.tmpdir(), "work-queue-summary-"));
  const snapshotPath = path.join(tempDirectory, "snapshot.json");
  fs.writeFileSync(snapshotPath, JSON.stringify({ version: 2, sha: "head", worker: null, transactionLog: serializeTransactionLog(initial) }));
  const summary = { addRaw: vi.fn(), write: vi.fn().mockResolvedValue(undefined) };
  summary.addRaw.mockReturnValue(summary);
  const core = { summary, info: vi.fn(), warning: vi.fn() };
  return { snapshotPath, core, githubClient: {}, context: { repo: { owner: "owner", repo: "repo" } } };
}

afterEach(() => {
  if (tempDirectory) {
    fs.rmSync(tempDirectory, { recursive: true, force: true });
    tempDirectory = undefined;
  }
});

describe("work queue conclusion summary", () => {
  it("shows queue state changes and every transaction kind in collapsed details", () => {
    const summary = renderSummary(snapshot(), replayTransactions(latest));
    expect(summary).toContain("### Work queue activity\n\n5 work items in the queue; 6 new transactions observed since activation.");
    expect(summary).toContain("<details>\n<summary>Show work queue activity</summary>\n\n");
    expect(summary).not.toContain("<details open");
    expect(summary).toContain("changes may include activity from other workflow runs");
    expect(summary).toContain("| available | 2 | 2 |");
    expect(summary).toContain("| claimed | 2 | 1 |");
    expect(summary).toContain("| completed | 0 | 1 |");
    expect(summary).toContain("| cancelled | 0 | 1 |");
    expect(summary).toContain("| effective | 2 | 2 |");
    expect(summary).toContain("| superseded | 0 | 1 |");
    expect(summary).toContain("| Claims created | 2 |");
    for (const label of ["Work added", "Claims cancelled", "Work completed", "Work cancelled"]) {
      expect(summary).toContain(`| ${label} | 1 |`);
    }
    expect(summary).toContain("Unique transactions: 6 at activation; 12 at conclusion.");
    expect(summary).toContain("No worker claim was assigned to this run.");
    expect(summary).toMatch(/<\/details>\n$/);
    expect(summary.indexOf("| Work state")).toBeGreaterThan(summary.indexOf("<details>"));
  });

  it("deduplicates facts and does not count reordering as activity", () => {
    const summary = renderSummary(snapshot(), replayTransactions([...initial].reverse().concat(initial)));
    expect(summary).toContain("0 new transactions observed");
    expect(summary).toContain("Unique transactions: 6 at activation; 6 at conclusion.");
    expect(summary).toContain("| Claims created | 0 |");
  });

  it("reports an empty queue explicitly", () => {
    const summary = renderSummary(snapshot([]), replayTransactions([]));
    expect(summary).toContain("0 work items in the queue; 0 new transactions");
    expect(summary).toContain("| available | 0 | 0 |");
  });

  it.each([
    [[], [work("one")], "1 work item in the queue; 1 new transaction observed since activation."],
    [[work("one")], [work("one")], "1 work item in the queue; 0 new transactions observed since activation."],
    [[work("one")], [work("one"), work("two")], "2 work items in the queue; 1 new transaction observed since activation."],
  ])("pluralizes work items and new transactions independently", (before, after, expected) => {
    expect(renderSummary(snapshot(before), replayTransactions(after))).toContain(expected);
  });

  it.each([
    ["done", "claim-a", "completed", "effective"],
    ["retry", "claim-b", "available", "cancelled"],
    ["new", "claim-d", "claimed", "superseded"],
    ["missing", "__proto__", "absent", "absent"],
  ])("reports worker state without disclosing identifiers for %s", (workId, claimId, workState, claimState) => {
    const summary = renderSummary(snapshot(initial, { work_id: workId, claim_id: claimId }), replayTransactions(latest));
    expect(summary).toContain(`Assigned worker: work **${workState}**; claim **${claimState}**.`);
    expect(summary).not.toContain(claimId);
  });

  it("never renders untrusted queue identifiers or attempt data", () => {
    const id = "</details>\n<script>secret</script>";
    const summary = renderSummary(snapshot([]), replayTransactions([work(id), claim(id, "`unsafe|claim`"), { version: 1, kind: "Completion", work: id, claim: "`unsafe|claim`", attempt: "sensitive-attempt" }]));
    expect(summary).not.toContain(id);
    expect(summary).not.toContain("unsafe");
    expect(summary).not.toContain("sensitive-attempt");
    expect(summary.match(/<\/details>/g)).toHaveLength(1);
  });

  it("writes the summary with a read-only refresh of the durable queue", async () => {
    const options = setup();
    const readWorkQueueLog = vi.fn().mockResolvedValue({ sha: "latest", transactions: latest });
    await main({ ...options, readWorkQueueLog });
    expect(readWorkQueueLog).toHaveBeenCalledWith({
      githubClient: options.githubClient,
      owner: "owner",
      repo: "repo",
      publishUpgrades: false,
      core: options.core,
    });
    expect(options.core.summary.addRaw).toHaveBeenCalledWith(renderSummary(snapshot(), replayTransactions(latest)));
    expect(options.core.summary.write).toHaveBeenCalledOnce();
    expect(options.core.warning).not.toHaveBeenCalled();
  });

  it("reads legacy queue data without publishing upgrades", async () => {
    const options = setup();
    const legacyWork = { kind: "Work", work: "legacy", claim: null, attempt: null };
    const createBlob = vi.fn();
    const githubClient = {
      rest: {
        git: {
          getRef: async () => ({ data: { object: { sha: "legacy-head" } } }),
          getCommit: async () => ({ data: { tree: { sha: "tree" } } }),
          getTree: async () => ({ data: { tree: [{ path: "work-queue.jsonl", type: "blob", sha: "blob" }] } }),
          getBlob: async () => ({ data: { encoding: "base64", content: Buffer.from(`${JSON.stringify(legacyWork)}\n`).toString("base64") } }),
          createBlob,
        },
      },
    };
    await main({ ...options, githubClient });
    expect(createBlob).not.toHaveBeenCalled();
    expect(options.core.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("1 work item in the queue; 1 new transaction"));
  });

  it("treats a missing queue branch as an empty queue", async () => {
    const options = setup();
    const githubClient = {
      rest: {
        git: {
          getRef: async () => {
            throw Object.assign(new Error("Not Found"), { status: 404 });
          },
        },
      },
    };
    await main({ ...options, githubClient });
    expect(options.core.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("0 work items in the queue; 0 new transactions"));
    expect(options.core.warning).not.toHaveBeenCalled();
  });

  it("does not misreport a summary write failure as unavailable queue data", async () => {
    const options = setup();
    const readWorkQueueLog = vi.fn().mockResolvedValue({ transactions: initial });
    options.core.summary.write.mockRejectedValue(new Error("summary write failed"));
    await expect(main({ ...options, readWorkQueueLog })).rejects.toThrow("summary write failed");
    expect(options.core.warning).not.toHaveBeenCalled();
    expect(options.core.summary.addRaw).toHaveBeenCalledOnce();
  });

  it.each(["missing snapshot", "malformed snapshot", "failed refresh", "invalid queue"])("reports %s as unavailable rather than an empty queue", async failure => {
    const options = setup();
    const readWorkQueueLog = vi.fn().mockResolvedValue({ sha: null, transactions: [] });
    if (failure === "missing snapshot") fs.unlinkSync(options.snapshotPath);
    if (failure === "malformed snapshot") fs.writeFileSync(options.snapshotPath, '{"version":0}');
    if (failure === "failed refresh") readWorkQueueLog.mockRejectedValue(new Error("sensitive API error"));
    if (failure === "invalid queue") readWorkQueueLog.mockResolvedValue({ transactions: [{ kind: "unknown", work: "sensitive" }] });
    await expect(main({ ...options, readWorkQueueLog })).rejects.toThrow("Failed to summarize work queue activity");
    expect(options.core.warning).toHaveBeenCalledOnce();
    const summary = options.core.summary.addRaw.mock.calls[0][0];
    expect(summary).toContain("<details>");
    expect(summary).toContain("Activity is unavailable");
    expect(summary).not.toContain("0 work items");
    expect(summary).not.toContain("sensitive");
  });
});
