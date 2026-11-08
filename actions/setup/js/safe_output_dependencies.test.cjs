// @ts-check

import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { processMessages, sortMessageIndicesByTemporaryIdDependencies } from "./safe_output_handler_manager.cjs";
import { buildSafeOutputDependencyPlan, checkSafeOutputDependencies, isConfirmedDependencySuccess } from "./safe_output_dependencies.cjs";
import { classifySafeOutputResult, computeSafeOutputsStatus } from "./safe_outputs_status.cjs";
import { main as createActionHandler } from "./safe_output_action_handler.cjs";
import { main as createCallWorkflowHandler } from "./call_workflow.cjs";
import { createHandlers } from "./safe_outputs_handlers.cjs";
import { buildSafeOutputOutcomes, finalizeBufferedReviewOutcomes, projectSafeOutputData } from "./safe_output_outcomes.cjs";
import { createSessionValidator } from "./scripts/validate_session.cjs";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { main as createLedgerAppendHandler, transactionPath } from "./ledger_append.cjs";
import { validateTransactions } from "./push_ledger_changes.cjs";
import { createReducer } from "./ledger_builtin.cjs";

const id = index => `so_${String(index).padStart(12, "0")}`;
const message = (index, dependencies, type = "add_comment") => ({
  type,
  output_id: id(index),
  ...(dependencies !== undefined ? { "depends-on": dependencies } : {}),
});

describe("safe-output runtime dependencies", () => {
  let originalGithub;
  let originalContext;

  beforeEach(() => {
    originalGithub = global.github;
    originalContext = global.context;
    vi.stubEnv("GH_AW_DETECTION_CONCLUSION", "success");
    vi.stubEnv("GH_AW_DETECTION_REASON", "");
    vi.stubEnv("GH_AW_RUN_DETECTION", "false");
    vi.stubEnv("GH_AW_WORK_QUEUE_ENABLED", "false");
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "false");
    vi.stubEnv("GH_AW_SAFE_OUTPUT_JOBS", "");
    global.core = { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn(), setOutput: vi.fn() };
    global.github = {};
    global.context = { payload: {} };
  });
  afterEach(() => {
    vi.unstubAllEnvs();
    delete global.core;
    if (originalGithub === undefined) delete global.github;
    else global.github = originalGithub;
    if (originalContext === undefined) delete global.context;
    else global.context = originalContext;
    delete process.env.GH_AW_SAFE_OUTPUT_JOBS;
  });

  it("orders multiple prerequisites and aliases while leaving independent outputs alone", async () => {
    const calls = [];
    const handler = vi.fn(async msg => {
      calls.push(msg.output_id);
      return { success: true };
    });
    const messages = [message(3, [id(1), id(2)]), message(4), message(2), { ...message(1), depends_on: id(2) }];
    const result = await processMessages(new Map([["add_comment", handler]]), messages);
    expect(calls).toEqual([id(4), id(2), id(1), id(3)]);
    expect(result.results.every(item => item.success)).toBe(true);
    expect(result.results.map(item => item.output_id)).toEqual(calls);
  });

  it("combines explicit and temporary-ID ordering", () => {
    const messages = [{ ...message(1, undefined, "create_issue"), temporary_id: "aw_first", blocked_by: ["aw_second"] }, message(3, id(1)), { ...message(2, undefined, "create_issue"), temporary_id: "aw_second" }];
    expect(sortMessageIndicesByTemporaryIdDependencies(messages)).toEqual([2, 0, 1]);
  });

  it("requires every prerequisite, not just one successful member", async () => {
    const handler = vi.fn(async msg => ({ success: msg.output_id !== id(2) }));
    const result = await processMessages(new Map([["add_comment", handler]]), [message(3, [id(1), id(2)]), message(1), message(2), message(4)]);
    expect(handler.mock.calls.map(([msg]) => msg.output_id)).toEqual([id(1), id(2), id(4)]);
    expect(result.results.find(item => item.output_id === id(3))).toMatchObject({ success: false, errorCode: "safe_output_dependency_failed" });
  });

  it.each([false, true])("keeps queued ledger writes unconfirmed while accepting sequential same-batch operations (depends-on: %s)", async dependencies => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-dependencies-"));
    vi.stubEnv("RUNNER_TEMP", directory);
    const ledgers = [
      { name: "table", type: "table", key: "id" },
      { name: "notes", type: "notes" },
    ];
    try {
      const handler = await createLedgerAppendHandler({ ledgers });
      const messages = [
        { ...message(1, undefined, "ledger_append"), ledger: "table", operation: "insert", value: { id: "run", status: "inserted" } },
        { ...message(2, dependencies ? id(1) : undefined, "ledger_append"), ledger: "table", operation: "update", key: "run", patch: { status: "updated" } },
        { ...message(3, dependencies ? id(2) : undefined, "ledger_append"), ledger: "table", operation: "upsert", value: { id: "run", status: "passed" } },
        {
          ...message(4, undefined, "ledger_append"),
          ledger: "notes",
          operation: "note",
          temp_id: "smoke-note",
          subject: "smoke",
          note: "README describes the project",
          reason: "Verified source",
          citations: [{ type: "repository", path: "README.md" }],
        },
        { ...message(5, dependencies ? id(4) : undefined, "ledger_append"), ledger: "notes", operation: "vote", note_id: "#smoke-note", vote: "up" },
      ];
      const result = await processMessages(new Map([["ledger_append", handler]]), messages);
      handler.finalize();
      const artifact = JSON.parse(fs.readFileSync(transactionPath(), "utf8"));
      expect(() => validateTransactions(artifact, ledgers)).not.toThrow();
      expect(result.results.filter(item => !item.success)).toHaveLength(dependencies ? 3 : 0);
      expect(result.results.filter(item => item.success).every(item => !isConfirmedDependencySuccess(item))).toBe(true);
      if (dependencies) {
        expect(result.results.find(item => item.output_id === id(2))).toMatchObject({ errorCode: "safe_output_dependency_failed", error: expect.stringContaining("Ledger prerequisite is queued, not persisted") });
        expect(result.results.find(item => item.output_id === id(5))).toMatchObject({ errorCode: "safe_output_dependency_failed", error: expect.stringContaining("without depends-on") });
        expect(artifact.ledgers.table.appends).toHaveLength(1);
        expect(artifact.ledgers.notes.appends).toHaveLength(1);
      } else {
        const reducer = createReducer(ledgers[0]);
        for (const append of artifact.ledgers.table.appends) reducer.apply(append.record);
        expect(reducer.output().tables.state.rows).toEqual([{ key: "run", value: '{"id":"run","status":"passed"}' }]);
        const [note, vote] = artifact.ledgers.notes.appends;
        expect(vote.record.note_id).toBe(note.record.id);
        const notesReducer = createReducer(ledgers[1]);
        for (const append of artifact.ledgers.notes.appends) notesReducer.apply(append.record);
        expect(notesReducer.output().tables.note_state.rows[0]).toMatchObject({ note_id: note.record.id, upvotes: 1, net_votes: 1 });
      }
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it.each([
    [
      "throws",
      () => {
        throw new Error("failed");
      },
    ],
    ["returned failure", () => ({ success: false })],
    ["fanout failure", () => [{ success: true }, { success: false }]],
    ["empty fanout", () => []],
    ["no result", () => undefined],
    ["skipped", () => ({ success: true, skipped: true })],
    ["deferred", () => ({ deferred: true })],
    ["cancelled", () => ({ success: true, cancelled: true })],
    ["staged", () => ({ success: true, staged: true })],
    ["delegated", () => ({ success: true, delegated: true })],
    ["queued", () => ({ success: true, queued: true })],
    ["buffered", () => ({ success: true, buffered: true })],
    ["deferred review publication", () => ({ success: true, deferred_manifest: true })],
    ["warning", () => ({ success: true, warning: "Not applied" })],
    ["unconfirmed error", () => ({ error: "Not applied" })],
  ])("blocks transitive dependents of %s without affecting independent outputs", async (name, outcome) => {
    const handler = vi.fn(async msg => (msg.output_id === id(1) ? outcome() : { success: true }));
    const result = await processMessages(new Map([["add_comment", handler]]), [message(3, id(2)), message(2, id(1)), message(1), message(4)]);
    expect(handler.mock.calls.map(([msg]) => msg.output_id)).toEqual(name === "deferred" ? [id(1), id(4), id(1)] : [id(1), id(4)]);
    for (const index of [0, 1]) {
      expect(result.results.find(item => item.messageIndex === index)).toMatchObject({ success: false, errorCode: "safe_output_dependency_failed" });
    }
    expect(result.results.find(item => item.output_id === id(4))).toMatchObject({ success: true });
  });

  it.each([
    ["missing", [message(1, id(99))]],
    ["self", [message(1, id(1))]],
    ["cycle", [message(1, id(2)), message(2, id(1))]],
    ["duplicate", [message(1), message(1), message(2, id(1))]],
    ["malformed ID", [message(1, "aw_not_an_output")]],
    ["malformed output ID", [{ ...message(1), output_id: "aw_not_an_output" }]],
    ["empty list", [message(1, [])]],
    ["duplicate prerequisite", [message(1, [id(2), id(2)]), message(2)]],
    ["null", [message(1, null)]],
    ["non-string", [message(1, [id(2), 3]), message(2)]],
    ["conflicting aliases", [{ ...message(1, id(2)), depends_on: id(3) }, message(2), message(3)]],
    ["both aliases", [{ ...message(1, id(2)), depends_on: id(2) }, message(2)]],
  ])("fails closed for %s", async (_name, messages) => {
    const handler = vi.fn(async () => ({ success: true }));
    const result = await processMessages(new Map([["add_comment", handler]]), [...messages, message(100)]);
    for (const record of result.results.filter(item => buildSafeOutputDependencyPlan(messages).invalid.has(item.messageIndex))) {
      expect(record).toMatchObject({ success: false, errorCode: "safe_output_dependency_invalid" });
    }
    const invalid = buildSafeOutputDependencyPlan(messages).invalid;
    expect(invalid.size).toBeGreaterThan(0);
    expect(handler).toHaveBeenCalledTimes(messages.length - invalid.size + 1);
    expect(result.results.find(item => item.output_id === id(100))).toMatchObject({ success: true });
  });

  it("detects cycles spanning explicit and legacy temporary-ID edges", async () => {
    const messages = [
      { ...message(1, undefined, "create_issue"), temporary_id: "aw_first", blocked_by: ["aw_second"] },
      { ...message(2, id(1), "create_issue"), temporary_id: "aw_second" },
    ];
    const handler = vi.fn(async () => ({ success: true }));
    const result = await processMessages(new Map([["create_issue", handler]]), messages);
    expect(handler.mock.calls.map(([msg]) => msg.output_id)).toEqual([id(1)]);
    expect(result.results.find(item => item.output_id === id(2))).toMatchObject({ success: false, errorCode: "safe_output_dependency_invalid" });
  });

  it("does not mark an explicit dependency downstream of a legacy cycle as cyclic", async () => {
    const create = vi.fn(async () => ({ success: true, repo: "owner/repo", number: 42 }));
    const comment = vi.fn(async () => ({ success: true }));
    const result = await processMessages(
      new Map([
        ["create_issue", create],
        ["add_comment", comment],
      ]),
      [{ ...message(1, undefined, "create_issue"), temporary_id: "aw_first", blocked_by: ["aw_second"] }, { ...message(2, undefined, "create_issue"), temporary_id: "aw_second", blocked_by: ["aw_first"] }, message(3, id(1))]
    );
    expect(create).toHaveBeenCalledTimes(2);
    expect(comment).toHaveBeenCalledOnce();
    expect(result.results.find(item => item.output_id === id(3))).toMatchObject({ success: true });
  });

  it("waits for actual deferred retry success before running a dependent chain", async () => {
    const calls = [];
    let attempts = 0;
    const handler = vi.fn(async msg => {
      calls.push(msg.output_id);
      if (msg.output_id === id(1) && attempts++ === 0) return { deferred: true };
      return { success: true };
    });
    const result = await processMessages(new Map([["add_comment", handler]]), [message(3, id(2)), message(2, id(1)), message(1), message(4)]);
    expect(calls).toEqual([id(1), id(4), id(1), id(2), id(3)]);
    expect(result.results.every(item => item.success)).toBe(true);
  });

  it.each(["submit_pull_request_review", "create_pull_request_review_comment"])("waits for buffered %s submission before running a dependent check and its dependent", async type => {
    const calls = [];
    const review = vi.fn(async () => {
      calls.push("buffer");
      return { success: true, repo: "owner/repo", pull_request_number: 42, deferred_manifest: true, ...(type === "create_pull_request_review_comment" ? { buffered: true, metadata: { path: "file.js", line: 1, side: "RIGHT" } } : {}) };
    });
    const check = vi.fn(async () => {
      calls.push("check");
      return { success: true };
    });
    const comment = vi.fn(async () => {
      calls.push("comment");
      return { success: true };
    });
    const finalize = vi.fn(async results => {
      if (!results.some(result => result.result?.deferred_manifest)) return;
      calls.push("submit");
      expect(check).not.toHaveBeenCalled();
      finalizeBufferedReviewOutcomes(results, "owner/repo", 42, {
        success: true,
        review_url: "https://github.com/owner/repo/pull/42#pullrequestreview-1",
        comment_count: 1,
        review_comments: [{ metadata: { path: "file.js", line: 1, side: "RIGHT" } }],
      });
    });
    const result = await processMessages(
      new Map([
        [type, review],
        ["create_check_run", check],
        ["add_comment", comment],
      ]),
      [message(1, undefined, type), message(2, id(1), "create_check_run"), message(3, id(2))],
      null,
      finalize
    );
    expect(calls).toEqual(["buffer", "submit", "check", "comment"]);
    expect(finalize).toHaveBeenCalled();
    expect(result.results.every(item => item.success)).toBe(true);
    expect(result.results[0].result.deferred_manifest).toBe(false);
  });

  it("retries a waiting dependent that itself defers, before releasing its dependent", async () => {
    const calls = [];
    const attempts = new Map();
    const handler = vi.fn(async msg => {
      calls.push(msg.output_id);
      const count = attempts.get(msg.output_id) || 0;
      attempts.set(msg.output_id, count + 1);
      return count === 0 && msg.output_id !== id(3) ? { deferred: true } : { success: true };
    });
    const result = await processMessages(new Map([["add_comment", handler]]), [message(3, id(2)), message(2, id(1)), message(1)]);
    expect(calls).toEqual([id(1), id(1), id(2), id(2), id(3)]);
    expect(result.results.every(item => item.success)).toBe(true);
  });

  it("retries deferred dependents after review publication without replaying completed retries", async () => {
    const calls = [];
    const attempts = new Map();
    const handler = vi.fn(async msg => {
      calls.push(msg.output_id);
      const count = attempts.get(msg.output_id) || 0;
      attempts.set(msg.output_id, count + 1);
      if (msg.output_id === id(2)) return { success: true, deferred_manifest: true };
      return count === 0 && msg.output_id !== id(4) ? { deferred: true } : { success: true };
    });
    const result = await processMessages(new Map([["add_comment", handler]]), [message(1), message(2, id(1)), message(3, id(2)), message(4, id(3))], null, async results => {
      for (const record of results) {
        if (record.result?.deferred_manifest) record.result.deferred_manifest = false;
      }
    });
    expect(calls).toEqual([id(1), id(1), id(2), id(3), id(3), id(4)]);
    expect(result.results.every(item => item.success)).toBe(true);
  });

  it("finalizes a review buffered by an in-place retry without result-array growth", async () => {
    const calls = [];
    let published = false;
    const handler = vi.fn(async msg => {
      calls.push(msg.output_id);
      if (msg.output_id === id(1)) return { success: true, deferred_manifest: true };
      if (msg.output_id === id(2)) return published ? { success: true, deferred_manifest: true } : { deferred: true };
      return { success: true };
    });
    const finalize = vi.fn(async results => {
      published = true;
      for (const record of results) {
        if (record.result?.deferred_manifest) record.result.deferred_manifest = false;
      }
    });
    const result = await processMessages(new Map([["add_comment", handler]]), [message(1), message(2), message(3, id(2))], null, finalize);
    expect(calls).toEqual([id(1), id(2), id(2), id(2), id(3)]);
    expect(finalize).toHaveBeenCalledTimes(2);
    expect(result.results.every(item => classifySafeOutputResult(item) === "success")).toBe(true);
  });

  it("does not retry unresolved handlers again when finalization makes no resolution progress", async () => {
    const handler = vi.fn(async () => ({ deferred: true }));
    const finalize = vi.fn(async () => {});
    const result = await processMessages(new Map([["add_comment", handler]]), [message(1)], null, finalize);
    expect(handler).toHaveBeenCalledTimes(2);
    expect(result.results[0]).toMatchObject({ deferred: true });
  });

  it("classifies buffered review publication consistently across consumers", () => {
    const record = { messageIndex: 0, success: true, result: { deferred_manifest: true } };
    expect(classifySafeOutputResult(record)).toBe("deferred");
    expect(computeSafeOutputsStatus([record])).toMatchObject({ itemsApplied: 0, itemsDeferred: 1, status: "deferred" });
    expect(buildSafeOutputOutcomes([record])[0].status).toBe("deferred");
  });

  it("fails a skipped review prerequisite immediately with its skip reason", () => {
    const plan = buildSafeOutputDependencyPlan([message(1), message(2, id(1))]);
    const record = { messageIndex: 0, success: true, skipped: true, skipReason: "PR is closed", result: { deferred_manifest: true } };
    expect(checkSafeOutputDependencies(plan, 1, [record], true)).toEqual({
      state: "failed",
      errorCode: "safe_output_dependency_failed",
      error: "Safe-output prerequisite did not complete successfully: PR is closed",
    });
  });

  it("waits for a prerequisite's temporary-ID resolution on retry", async () => {
    const calls = [];
    const link = vi.fn(async (_message, temporaryIds) => {
      calls.push("link");
      return temporaryIds.aw_parent ? { success: true } : { deferred: true };
    });
    const create = vi.fn(async () => {
      calls.push("create");
      return { repo: "owner/repo", number: 42, temporaryId: "aw_parent" };
    });
    const comment = vi.fn(async () => {
      calls.push("comment");
      return { success: true };
    });
    const result = await processMessages(
      new Map([
        ["link_sub_issue", link],
        ["create_issue", create],
        ["add_comment", comment],
      ]),
      [message(1, undefined, "link_sub_issue"), message(2, id(1)), { ...message(3, undefined, "create_issue"), temporary_id: "aw_parent" }]
    );
    expect(calls).toEqual(["link", "create", "link", "comment"]);
    expect(result.results.every(item => item.success)).toBe(true);
    expect(result.temporaryIdMap.aw_parent).toEqual({ repo: "owner/repo", number: 42 });
  });

  it("preserves temporary-ID deferral and cyclic ordering without explicit dependencies", async () => {
    const messages = [
      { type: "create_issue", temporary_id: "aw_first", blocked_by: ["aw_second"] },
      { type: "create_issue", temporary_id: "aw_second", blocked_by: ["aw_first"] },
    ];
    expect(sortMessageIndicesByTemporaryIdDependencies(messages)).toEqual([0, 1]);
    let attempts = 0;
    const handler = vi.fn(async () => (++attempts === 1 ? { deferred: true } : { success: true }));
    const result = await processMessages(new Map([["create_issue", handler]]), messages);
    expect(handler).toHaveBeenCalledTimes(3);
    expect(result.results.every(item => item.success)).toBe(true);
  });

  it("releases waiting temporary-ID producers before retrying their legacy consumers", async () => {
    const calls = [];
    let attempts = 0;
    const handlers = new Map([
      [
        "update_issue",
        async () => {
          calls.push("update");
          return attempts++ === 0 ? { deferred: true } : { success: true };
        },
      ],
      [
        "create_issue",
        async () => {
          calls.push("create");
          return { repo: "owner/repo", number: 42, temporaryId: "aw_parent" };
        },
      ],
      [
        "link_sub_issue",
        async (_message, temporaryIds) => {
          calls.push("link");
          return temporaryIds.aw_parent ? { success: true } : { deferred: true };
        },
      ],
      [
        "add_comment",
        async () => {
          calls.push("comment");
          return { success: true };
        },
      ],
    ]);
    const result = await processMessages(handlers, [message(1, undefined, "update_issue"), { ...message(2, id(1), "create_issue"), temporary_id: "aw_parent" }, message(3, undefined, "link_sub_issue"), message(4, id(3))]);
    expect(calls).toEqual(["update", "link", "update", "create", "link", "comment"]);
    expect(result.results.every(item => item.success)).toBe(true);
  });

  it("revisits deferred consumers when waiting dependencies create new IDs", async () => {
    const calls = [];
    const link = vi.fn(async (msg, temporaryIds) => {
      if (msg.output_id === id(1)) {
        calls.push("link-P");
        return temporaryIds.aw_parent ? { success: true } : { deferred: true };
      }
      calls.push("link-Q");
      return temporaryIds.aw_child ? { success: true } : { deferred: true };
    });
    const create = vi.fn(async msg => {
      if (msg.output_id === id(3)) {
        calls.push("create-P");
        return { success: true, repo: "owner/repo", number: 1, temporaryId: "aw_parent" };
      }
      calls.push("create-Q");
      return { success: true, repo: "owner/repo", number: 2, temporaryId: "aw_child" };
    });
    const result = await processMessages(
      new Map([
        ["link_sub_issue", link],
        ["create_issue", create],
      ]),
      [message(1, undefined, "link_sub_issue"), message(2, id(4), "link_sub_issue"), message(3, id(2), "create_issue"), message(4, undefined, "create_issue")]
    );
    expect(calls).toEqual(["link-P", "create-Q", "link-Q", "create-P", "link-P"]);
    expect(result.results.every(item => item.success)).toBe(true);
    expect(create).toHaveBeenCalledTimes(2);
  });

  it.each(["throw", "failure", "deferred", "staged", "skipped"])("blocks waiting dependents after a retry ends in %s", async outcome => {
    let attempts = 0;
    const handler = vi.fn(async () => {
      if (attempts++ === 0) return { deferred: true };
      if (outcome === "throw") throw new Error("retry failed");
      if (outcome === "failure") return { success: false };
      return { success: true, [outcome]: true };
    });
    const result = await processMessages(new Map([["add_comment", handler]]), [message(2, id(1)), message(1)]);
    expect(handler).toHaveBeenCalledTimes(2);
    expect(result.results.find(item => item.output_id === id(2))).toMatchObject({ success: false, errorCode: "safe_output_dependency_failed" });
  });

  it("does not treat standalone or custom-job delegation as success", async () => {
    process.env.GH_AW_SAFE_OUTPUT_JOBS = JSON.stringify({ custom_output: {} });
    const handler = vi.fn(async () => ({ success: true }));
    const result = await processMessages(new Map([["add_comment", handler]]), [message(1, undefined, "noop"), message(2, id(1)), message(3, undefined, "custom_output"), message(4, id(3))]);
    expect(handler).not.toHaveBeenCalled();
    expect(result.results.filter(item => item.errorCode === "safe_output_dependency_failed")).toHaveLength(2);
    expect(result.results.filter(item => item.delegated)).toHaveLength(2);
  });

  it("preserves no-dependency legacy messages and successful legacy fanouts", async () => {
    const handler = vi.fn(async () => [{ commentId: 1 }, { success: true }]);
    const result = await processMessages(new Map([["add_comment", handler]]), [{ type: "add_comment" }, message(1), message(2, id(1))]);
    expect(handler).toHaveBeenCalledTimes(3);
    expect(result.results.every(item => item.success)).toBe(true);
    expect(result.results[0]).not.toHaveProperty("output_id");
  });

  it("does not mistake custom-action payload export for confirmed execution", async () => {
    const action = await createActionHandler({ action_name: "custom_action" });
    const comment = vi.fn(async () => ({ success: true }));
    const result = await processMessages(
      new Map([
        ["custom_action", action],
        ["add_comment", comment],
      ]),
      [message(1, undefined, "custom_action"), message(2, id(1))]
    );
    expect(global.core.setOutput).toHaveBeenCalledWith("action_custom_action_payload", "{}");
    expect(result.results.find(item => item.output_id === id(1))).toMatchObject({ success: true, result: { delegated: true } });
    expect(comment).not.toHaveBeenCalled();
    expect(result.results.find(item => item.output_id === id(2))).toMatchObject({ success: false, errorCode: "safe_output_dependency_failed" });
  });

  it("does not treat real workflow and coverage exports as completed prerequisites", async () => {
    global.context = { repo: { owner: "owner", repo: "repo" }, payload: {} };
    const callWorkflow = await createCallWorkflowHandler({ workflows: ["worker"] });
    const append = vi.fn();
    const handlers = createHandlers({ debug: vi.fn() }, append, {});
    const uploadCoverage = args => JSON.parse(handlers.uploadCodeCoverageHandler(args).content[0].text);
    const noop = args => JSON.parse(handlers.defaultHandler("noop")(args).content[0].text);
    const comment = vi.fn(async () => ({ success: true }));
    expect(isConfirmedDependencySuccess({ type: "upload_asset", claim_handle: "claim-1", success: true, result: { success: true } })).toBe(false);
    const result = await processMessages(
      new Map([
        ["call_workflow", callWorkflow],
        ["upload_code_coverage", uploadCoverage],
        ["noop", noop],
        ["add_comment", comment],
      ]),
      [{ ...message(1, undefined, "call_workflow"), workflow_name: "worker", inputs: {} }, message(2, id(1)), message(3, undefined, "upload_code_coverage"), message(4, id(3)), message(5, undefined, "noop"), message(6, id(5))]
    );
    expect(result.results.find(item => item.output_id === id(1))).toMatchObject({ success: true, result: { delegated: true } });
    expect(result.results.find(item => item.output_id === id(3))).toMatchObject({ success: true, result: { delegated: true } });
    expect(comment).not.toHaveBeenCalled();
    expect(result.results.filter(item => item.errorCode === "safe_output_dependency_failed")).toHaveLength(3);
  });

  it("does not treat dispatch outcomes as confirmed prerequisites", () => {
    for (const type of ["dispatch_workflow", "dispatch-repository"]) {
      expect(isConfirmedDependencySuccess({ type, claim_handle: "claim-1", success: true, result: { success: true } })).toBe(false);
    }
  });

  it("never confirms prerequisites in global staged mode, even when legacy handlers omit staged", async () => {
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "true");
    const handler = vi.fn(async () => ({ success: true }));
    const result = await processMessages(new Map([["add_comment", handler]]), [message(1), message(2, id(1)), message(3)]);
    expect(handler.mock.calls.map(([msg]) => msg.output_id)).toEqual([id(1), id(3)]);
    expect(result.results.find(item => item.output_id === id(2))).toMatchObject({ success: false, errorCode: "safe_output_dependency_failed" });
    expect(isConfirmedDependencySuccess({ success: true, result: { success: true } })).toBe(false);
    expect(isConfirmedDependencySuccess({ success: true, result: [{ success: true }] })).toBe(false);
  });

  it("rejects nested fanout outcomes even under an optimistic wrapper", () => {
    expect(isConfirmedDependencySuccess({ success: true, result: [{ success: true }, { success: true, staged: true }] })).toBe(false);
    expect(isConfirmedDependencySuccess({ success: true, result: [{ success: false }] })).toBe(false);
  });

  it("fails closed when a post-handler error makes a prerequisite's results ambiguous", async () => {
    const create = vi.fn(async () => ({ repo: "owner/repo", number: 42 }));
    const comment = vi.fn(async () => ({ success: true }));
    const result = await processMessages(
      new Map([
        ["create_issue", create],
        ["add_comment", comment],
      ]),
      [message(1, undefined, "create_issue"), message(2, id(1))],
      () => {
        throw new Error("manifest unavailable");
      }
    );
    expect(create).toHaveBeenCalledOnce();
    expect(comment).not.toHaveBeenCalled();
    expect(result.results.find(item => item.output_id === id(2))).toMatchObject({ success: false, errorCode: "safe_output_dependency_failed" });
  });

  it("preserves server output IDs and dependency error codes in manifest outcomes", async () => {
    const handler = vi.fn(async () => ({ success: false }));
    const result = await processMessages(new Map([["add_comment", handler]]), [message(1), message(2, id(1))]);
    const outcomes = buildSafeOutputOutcomes(result.results);
    expect(outcomes).toEqual([expect.objectContaining({ output_id: id(1), status: "failed" }), expect.objectContaining({ output_id: id(2), status: "failed", errorCode: "safe_output_dependency_failed" })]);
    expect(projectSafeOutputData(outcomes[1])).toMatchObject({ output_id: id(2), errorCode: "safe_output_dependency_failed" });
    expect(projectSafeOutputData({ output_id: "PRIVATE_CONTENT" })).not.toHaveProperty("output_id");
    expect(projectSafeOutputData({ output_id: "aw_temporary" })).not.toHaveProperty("output_id");
    const validator = createSessionValidator("unified");
    expect(
      validator.event({
        type: "safe_output.result",
        data: projectSafeOutputData(outcomes[1]),
        provenance: { component: "safe_outputs", phase: "safe_outputs", path: "safe-output-outcomes.jsonl", index: 0 },
      })
    ).toBe(true);
  });

  it("retains one correlation ID across fanout execution outcomes", () => {
    const outcomes = buildSafeOutputOutcomes([{ output_id: id(1), type: "add_comment", success: true, result: [{ success: true }, { success: false }] }]);
    expect(outcomes.map(outcome => [outcome.output_id, outcome.status])).toEqual([
      [id(1), "success"],
      [id(1), "failed"],
    ]);
  });
});
