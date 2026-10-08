import { describe, expect, it, vi } from "vitest";

const { MAX_SUMMARY_BYTES, MAX_SUMMARY_ROWS, MAX_UPDATE_SUMMARIES, renderWorkQueue, writeWorkQueueUpdateSummary } = require("./work_queue_summary_renderer.cjs");
const { newState, serializeProjection } = require("./work_queue_replay.cjs");
const { queueFixture } = require("./work_queue_lifecycle.test_helpers.cjs");

function summaryCore() {
  const summary = { addRaw: vi.fn(), write: vi.fn().mockResolvedValue(undefined) };
  summary.addRaw.mockReturnValue(summary);
  return { info: vi.fn(), warning: vi.fn(), summary };
}

function finish(fixture, handle, outcome) {
  fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: handle, outcome }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: handle });
}

describe("bounded work queue metadata renderer", () => {
  it("shows admitted nodes, priority, accounting and predecessors without rendering payloads", () => {
    const fixture = queueFixture({ granted: false });
    const before = serializeProjection(fixture.state);
    const summary = renderWorkQueue(fixture.state, fixture.transactions.at(-1));
    expect(summary).toContain("### Work queue admission");
    expect(summary).toContain("3 Work nodes; 0 open Claims; 0 pending delivery barriers; 0 native reservations");
    expect(summary).toContain("Accounting key");
    expect(summary).toContain("<code>(default)</code>");
    expect(summary).toContain("Showing 3 of 3 Work nodes");
    expect(summary).toContain("No Claims shown");
    expect(summary).toContain("<details>");
    expect(summary).toContain("table order is not scheduler order");
    expect(summary).not.toContain("stored task");
    expect(summary).not.toContain("effect_contract");
    expect(serializeProjection(fixture.state)).toEqual(before);
  });

  it("distinguishes originally singleton and mixed-Claim outcomes from verified delivery", () => {
    const singleton = queueFixture({ count: 1, bound: true });
    expect(renderWorkQueue(singleton.state)).toContain("1 Work nodes; 1 open Claims");
    finish(singleton, "h1", "completed");
    expect(renderWorkQueue(singleton.state)).toContain("1 pending delivery barriers");
    const batch = queueFixture({ count: 2, bound: true });
    finish(batch, "h1", "completed");
    finish(batch, "h2", "cancelled");
    const summary = renderWorkQueue(batch.state, batch.transactions.at(-1));
    expect(summary).toContain("<code>h1</code>");
    expect(summary).toContain("<code>h2</code>");
    expect(summary).toContain("<code>completed</code>");
    expect(summary).toContain("<code>cancelled</code>");
    expect(summary).toContain("1 pending delivery barriers");
    expect(summary).toContain("Completion is not verified delivery");
    expect(summary).toContain("<code>bound</code> (retained)");
  });

  it("shows unknown launch reservations as retained, never released or delivered", () => {
    const fixture = queueFixture({ started: true });
    const summary = renderWorkQueue(fixture.state);
    expect(summary).toContain("1 native reservations (1 unbound)");
    expect(summary).toContain("<code>started</code> (retained)");
    expect(summary).not.toContain("<code>verified</code>");
  });

  it("escapes Markdown and HTML labels and removes control characters and credential-shaped values", () => {
    const fixture = queueFixture({ granted: false });
    const state = structuredClone(fixture.state);
    const work = [...state.works.values()][0];
    work.node_key = '</details><script>alert("x")</script>|`[link](x)\n\u202e';
    work.worker_profile = "ghp_" + "A".repeat(40);
    work.fairness_key = "key|<img>";
    work.payload.plan = "private task details must never appear";
    const summary = renderWorkQueue(state);
    expect(summary).toContain("&lt;/details&gt;&lt;script&gt;");
    expect(summary).toContain("&#124;");
    expect(summary).toContain("&#96;");
    expect(summary).toContain("[redacted]");
    expect(summary).not.toContain("<script>");
    expect(summary).not.toContain("<img>");
    expect(summary).not.toContain("ghp_");
    expect(summary).not.toContain("\u202e");
    expect(summary).not.toContain("private task details");
    expect(summary.match(/<\/details>/g)).toHaveLength(1);
  });

  it("bounds rows and UTF-8 bytes, preserving complete tables and closing details", () => {
    const fixture = queueFixture({ count: MAX_SUMMARY_ROWS + 8, granted: false });
    const state = structuredClone(fixture.state);
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
    expect(summary).toContain("Identifiers are shortened");
    expect(summary).toMatch(/<\/details>\n$/);
  });

  it("renders the affected node ahead of old rows and keeps current owners ahead of historical attempts", () => {
    const fixture = queueFixture({ count: MAX_SUMMARY_ROWS + 2, granted: false });
    const work = [...fixture.state.works.values()].at(-1);
    const summary = renderWorkQueue(fixture.state, { id: "focus", request: { kind: "result" }, operations: [{ kind: "Result", work_id: work.work_id }] });
    expect(summary.indexOf(work.work_id.slice(0, 16))).toBeLessThan(summary.indexOf([...fixture.state.works.keys()][0].slice(0, 16)));
    const retry = queueFixture({ count: 1, bound: true });
    const state = structuredClone(retry.state);
    const member = retry.assignment.claims[0];
    const historical = { ...state.claims.get(member.claim_id), claim_id: "old-cancelled", state: "cancelled" };
    state.claims = new Map([[historical.claim_id, historical], ...state.claims]);
    const ownership = renderWorkQueue(state);
    expect(ownership.indexOf(member.claim_id.slice(0, 16))).toBeLessThan(ownership.indexOf("old-cancelled"));
    expect(ownership).toContain("not current owner");
  });

  it("reports genuine uninitialized views explicitly", () => {
    const summary = renderWorkQueue(newState());
    expect(summary).toContain("no installed Policy");
    expect(summary).toContain("uninitialized");
  });
});

describe("publication summary writer", () => {
  it("is a no-op outside an Actions summary context", async () => {
    await writeWorkQueueUpdateSummary(undefined, undefined, undefined);
    await writeWorkQueueUpdateSummary({ warning: vi.fn() }, undefined, undefined);
  });

  it("writes using the existing core summary buffer without overwriting other sections", async () => {
    const fixture = queueFixture({ granted: false });
    const core = summaryCore();
    await writeWorkQueueUpdateSummary(core, fixture.state, fixture.transactions.at(-1));
    expect(core.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Work queue admission"));
    expect(core.summary.write).toHaveBeenCalledExactlyOnceWith();
    expect(core.warning).not.toHaveBeenCalled();
  });

  it("annotates summary I/O failures without leaking errors or changing committed queue state", async () => {
    const fixture = queueFixture({ granted: false });
    const core = summaryCore();
    core.summary.write.mockRejectedValue(new Error("private credential and local path"));
    const before = serializeProjection(fixture.state);
    await writeWorkQueueUpdateSummary(core, fixture.state, fixture.transactions.at(-1));
    expect(core.warning).toHaveBeenCalledWith(expect.stringContaining("Queue update committed"));
    expect(core.warning.mock.calls[0][0]).not.toContain("private credential");
    expect(serializeProjection(fixture.state)).toEqual(before);
  });

  it("caps repeated intermediate views while leaving room for other step sections", async () => {
    const fixture = queueFixture({ granted: false });
    const core = summaryCore();
    for (let index = 0; index < MAX_UPDATE_SUMMARIES + 5; index++) await writeWorkQueueUpdateSummary(core, fixture.state, fixture.transactions.at(-1));
    expect(core.summary.addRaw).toHaveBeenCalledTimes(MAX_UPDATE_SUMMARIES + 1);
    expect(core.summary.addRaw.mock.calls.at(-1)[0]).toContain("Further intermediate queue views are omitted");
    expect(core.summary.addRaw.mock.calls.at(-1)[0]).toContain("conclusion summary reads the latest ledger");
    const bytes = core.summary.addRaw.mock.calls.reduce((total, [text]) => total + Buffer.byteLength(text, "utf8"), 0);
    expect(bytes).toBeLessThanOrEqual(MAX_UPDATE_SUMMARIES * MAX_SUMMARY_BYTES + 512);
  });

  it("does not change committed success when a caller has no Actions warning API", async () => {
    const fixture = queueFixture({ granted: false });
    const core = summaryCore();
    delete core.warning;
    core.summary.write.mockRejectedValue(new Error("private failure"));
    const warning = vi.spyOn(console, "warn").mockImplementation(() => {});
    try {
      await writeWorkQueueUpdateSummary(core, fixture.state, fixture.transactions.at(-1));
      expect(warning).toHaveBeenCalledWith(expect.stringContaining("Publication and Claim accounting are unchanged"));
    } finally {
      warning.mockRestore();
    }
  });
});
