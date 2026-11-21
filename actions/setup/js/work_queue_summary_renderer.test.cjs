import { describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { MAX_SUMMARY_BYTES, MAX_SUMMARY_ROWS, MAX_UPDATE_SUMMARIES, renderWorkQueue, writeWorkQueueUpdateSummary } = require("./work_queue_summary_renderer.cjs");
const { newState, replayTransactions, serializeProjection } = require("./work_queue_replay.cjs");
const h = require("./work_queue_test_helpers.cjs");

function summaryCore() {
  const summary = { addRaw: vi.fn(), write: vi.fn().mockResolvedValue(undefined) };
  summary.addRaw.mockReturnValue(summary);
  return { info: vi.fn(), warning: vi.fn(), summary };
}

function queue(count = 2) {
  const log = h.submit(
    [],
    Array.from({ length: count }, (_, index) => h.work(`w${index}`))
  );
  return { log, state: replayTransactions(log) };
}

describe("bounded work queue metadata renderer", () => {
  it("shows Work, attempts, Dispatch state and run without payloads", () => {
    const planned = h.dispatchNext(queue().log, { concurrency: 1 });
    const dispatch = planned.decision.dispatches[0];
    const log = h.bind(planned.log, dispatch, h.workerActor("4242", 2));
    const state = replayTransactions(log);
    const summary = renderWorkQueue(state, log.at(-1));
    expect(summary).toContain("2 Work nodes; 1 open Claims; 1 active Dispatches (0 not yet bound to a run).");
    expect(summary).toContain("<code>bound</code> | <code>4242</code> attempt 2 |");
    expect(summary).toContain("| Work / node | Profile | Priority | Accounting key | Work state | Attempts | Predecessors |");
    expect(summary).not.toContain("w0 task");
    expect(summary).not.toMatch(/barrier|Policy|delivery/i);
  });

  it("escapes Markdown and HTML labels and removes control characters and credential-shaped values", () => {
    const state = structuredClone(queue(1).state);
    const work = [...state.works.values()][0];
    work.node_key = '</details><script>alert("x")</script>|`[link](x)\n\u202e';
    work.worker_profile = "ghp_" + "A".repeat(40);
    work.fairness_key = "key|<img>";
    work.payload.plan = "private task details must never appear";
    const summary = renderWorkQueue(state);
    expect(summary).toContain("&lt;/details&gt;&lt;script&gt;");
    expect(summary).toContain("&#124;");
    expect(summary).toContain("[redacted]");
    for (const hidden of ["<script>", "<img>", "ghp_", "\u202e", "private task details"]) expect(summary).not.toContain(hidden);
    expect(summary.match(/<\/details>/g)).toHaveLength(1);
  });

  it("bounds rows and UTF-8 bytes, preserving complete tables and closing details", () => {
    const state = structuredClone(queue(MAX_SUMMARY_ROWS + 8).state);
    for (const work of state.works.values()) {
      work.node_key = "<>&|`".repeat(48);
      work.worker_profile = "\ud83d\ude00".repeat(256);
      work.fairness_key = '"'.repeat(256);
      work.depends_on = Array.from({ length: 64 }, () => ({ kind: "work", work_id: "x".repeat(256) }));
    }
    const summary = renderWorkQueue(state);
    expect(Buffer.byteLength(summary, "utf8")).toBeLessThanOrEqual(MAX_SUMMARY_BYTES);
    const shown = Number(summary.match(/Showing (\d+) of 40 Work/)[1]);
    expect(shown).toBeGreaterThan(0);
    expect(shown).toBeLessThanOrEqual(MAX_SUMMARY_ROWS);
    expect(summary).toContain("(+60 more)");
    expect(summary).toMatch(/<\/details>\n\n\n$/);
  });

  it("renders the updated node ahead of older rows", () => {
    const { state } = queue(MAX_SUMMARY_ROWS + 2);
    const last = [...state.works.values()].at(-1);
    const summary = renderWorkQueue(state, { id: "focus", request: { kind: "cancel_work" }, operations: [{ kind: "WorkCancellation", work_id: last.work_id }] });
    expect(summary.indexOf(last.work_id.slice(0, 16))).toBeLessThan(summary.indexOf([...state.works.keys()][0].slice(0, 16)));
  });

  it("renders an empty queue", () => {
    const summary = renderWorkQueue(newState());
    expect(summary).toContain("0 Work nodes; 0 open Claims; 0 active Dispatches");
    expect(summary).toMatch(/^<details>\n<summary>Work queue state<\/summary>\n\n/);
    expect(summary).toMatch(/<\/details>\n\n\n$/);
  });
});

describe("publication summary writer", () => {
  it("is a no-op outside an Actions summary context", async () => {
    await writeWorkQueueUpdateSummary(undefined, undefined, undefined);
    await writeWorkQueueUpdateSummary({ warning: vi.fn() }, undefined, undefined);
  });

  it("appends to the core summary buffer", async () => {
    const { log, state } = queue();
    const core = summaryCore();
    await writeWorkQueueUpdateSummary(core, state, log.at(-1));
    expect(core.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Work queue admission"));
    expect(core.summary.write).toHaveBeenCalledExactlyOnceWith();
    expect(core.warning).not.toHaveBeenCalled();
  });

  it("warns on summary I/O failures without leaking errors or changing state", async () => {
    const { log, state } = queue();
    const core = summaryCore();
    core.summary.write.mockRejectedValue(new Error("private credential and local path"));
    const before = serializeProjection(state);
    await writeWorkQueueUpdateSummary(core, state, log.at(-1));
    expect(core.warning).toHaveBeenCalledWith(expect.stringContaining("Queue update committed"));
    expect(core.warning.mock.calls[0][0]).not.toContain("private credential");
    expect(serializeProjection(state)).toEqual(before);
  });

  it("caps repeated intermediate views", async () => {
    const { log, state } = queue();
    const core = summaryCore();
    for (let index = 0; index < MAX_UPDATE_SUMMARIES + 5; index++) await writeWorkQueueUpdateSummary(core, state, log.at(-1));
    expect(core.summary.addRaw).toHaveBeenCalledTimes(MAX_UPDATE_SUMMARIES + 1);
    expect(core.summary.addRaw.mock.calls.at(-1)[0]).toContain("Further intermediate queue views are omitted");
  });
});
