// @ts-check
import { afterEach, describe, expect, it } from "vitest";
import fs from "fs";
import path from "path";
import os from "node:os";
import { digest } from "./work_queue_codec.cjs";
import { nodeId } from "./work_queue_graph.cjs";
import {
  MAX_INTENTS,
  MAX_INTENT_BYTES,
  normalizeDispatchParameters,
  normalizeSubmitParameters,
  readFinishIntents,
  readIntentLines,
  readStagedIntentBatch,
  readStagedIntents,
  stageFinishIntent,
  stageIntent,
  stagePlanIntents,
  validatePreparedPlanIntentBatch,
} from "./work_queue_intents.cjs";
import { buildESLintFactoryPlan } from "./eslint_factory_portfolio.cjs";
import { buildDailyReportPlan } from "./daily_report_portfolio.cjs";
import { buildEngineConformancePlan } from "./engine_conformance_portfolio.cjs";
import { main as collectIntents } from "./collect_work_queue_intents.cjs";

const directories = [];
const policy = {
  version: 4,
  pending_limit: 30,
  issues: false,
  pools: {
    default: {
      concurrency: 1,
      per_account_limit: 0,
      max_attempts: 3,
      default_profile: "worker",
      profiles: { worker: { workflow: ".github/workflows/worker.lock.yml", effect_scope: "owner/repo" }, other: { workflow: ".github/workflows/other.lock.yml", effect_scope: "owner/repo" } },
    },
  },
};
function filename(name = "intents.jsonl") {
  const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-intents-"));
  directories.push(directory);
  return path.join(directory, name);
}
afterEach(() => {
  for (const directory of directories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

describe("staged control intents", () => {
  it("iterates intent lines lazily with split-compatible physical positions", () => {
    const file = filename();
    const lines = readIntentLines(file);
    expect(lines[Symbol.iterator]()).toBe(lines);
    fs.writeFileSync(file, '{"first":1}\n\n{"last":2}\n');
    expect([...lines]).toEqual(['{"first":1}', "", '{"last":2}', ""]);
  });

  it.each([
    '{"version":4,"intent_id":"bad","intent_id":"other","kind":"dispatch_next","parameters":{}}',
    '{"version":4,"intent_id":"bad","kind":"dispatch_next","parameters":{"max_claims":1e0}}',
    '{"version":3,"intent_id":"bad","kind":"dispatch_next","parameters":{}}',
    '{"version":4,"intent_id":"bad","kind":"finish","parameters":{}}',
    '\ufeff{"version":4,"intent_id":"bad","kind":"dispatch_next","parameters":{}}',
  ])("keeps strict validation and physical error positions for %j", invalid => {
    const file = filename();
    const sibling = { version: 4, intent_id: "sibling", kind: "dispatch_next", parameters: {} };
    fs.writeFileSync(file, `\n${invalid}\n\n${JSON.stringify(sibling)}\n`);
    const batch = readStagedIntentBatch(file);
    expect(batch.intents).toEqual([sibling]);
    expect(batch.errors).toEqual([expect.objectContaining({ line: 2, reason: "work_queue_intent_invalid" })]);
  });

  it.each([buildESLintFactoryPlan, buildDailyReportPlan, buildEngineConformancePlan])("stages the exact portfolio submission before dispatch", buildPlan => {
    const file = filename();
    const plan = buildPlan({ date: "2026-10-08", repository: "owner/repo", repositoryId: "7" });
    const staged = stagePlanIntents(file, plan);
    expect(staged.map(intent => [intent.version, intent.kind])).toEqual([
      [4, "submit"],
      [4, "dispatch_next"],
    ]);
    expect(staged[0].parameters).toEqual({ nodes: plan.nodes });
    expect(staged[1].parameters).toEqual(plan.dispatch);
    expect(readStagedIntents(file)).toEqual(staged);
    expect(validatePreparedPlanIntentBatch(staged, [])).toBeNull();
    const original = fs.readFileSync(file, "utf8");
    expect(stagePlanIntents(file, plan)).toEqual(staged);
    expect(fs.readFileSync(file, "utf8")).toBe(original);
  });

  it("rejects invalid or conflicting prepared plans without replacing staged work", () => {
    const file = filename();
    const plan = buildESLintFactoryPlan({ date: "2026-10-08", repository: "owner/repo", repositoryId: "7" });
    for (const invalid of [
      null,
      {},
      { ...plan, version: 3 },
      { ...plan, nodes: [] },
      { ...plan, nodes: [{ ...plan.nodes[0], payload: null }] },
      { ...plan, nodes: [{ ...plan.nodes[0], expires_at: 1 }] },
      { ...plan, dispatch: { ...plan.dispatch, max_claims: 0 } },
      { ...plan, dispatch: { ...plan.dispatch, pool: "other" } },
    ]) {
      expect(() => stagePlanIntents(file, invalid)).toThrow();
      expect(fs.existsSync(file)).toBe(false);
    }
    const staged = stagePlanIntents(file, plan);
    expect(() => stagePlanIntents(file, { ...plan, nodes: plan.nodes.slice(0, 1) })).toThrow(/conflict/);
    expect(validatePreparedPlanIntentBatch(staged.slice(0, 1), [])).toBe("work_queue_plan_intent_batch_invalid");
  });

  it("collects prepared intents from the same directory as the queue MCP server", () => {
    const intentPath = filename("work-queue.intents.jsonl");
    const sourceDir = path.dirname(intentPath);
    const outputDir = path.join(sourceDir, "collected");
    const staged = stagePlanIntents(intentPath, buildESLintFactoryPlan({ date: "2026-10-08", repository: "owner/repo", repositoryId: "7" }));
    const core = {
      setFailed: message => {
        throw new Error(message);
      },
    };
    expect(collectIntents({ sourceDir, outputDir, core })).toEqual([{ filename: "work-queue.intents.jsonl", bytes: fs.statSync(intentPath).size, status: "collected" }]);
    expect(readStagedIntents(path.join(outputDir, "work-queue.intents.jsonl"))).toEqual(staged);
  });

  it("deduplicates identical intents, isolates malformed lines and rejects conflicting IDs", () => {
    const file = filename();
    const first = { version: 4, intent_id: "first", kind: "dispatch_next", parameters: { pool: "default", max_claims: 1, max_dispatches: 1 } };
    stageIntent(file, first);
    stageIntent(file, first);
    expect(readStagedIntents(file)).toEqual([first]);
    expect(fs.statSync(file).mode & 0o777).toBe(0o644);
    const second = { ...first, intent_id: "second" };
    fs.appendFileSync(file, ["{not JSON", JSON.stringify({ ...first, parameters: { ...first.parameters, max_claims: 2 } }), JSON.stringify(second)].join("\n") + "\n");
    const batch = readStagedIntentBatch(file);
    expect(batch.intents).toEqual([second]);
    expect(batch.errors.map(error => error.line)).toEqual([1, 3, 4]);
    expect(() => readStagedIntents(file)).toThrow(/conflict/);
  });

  it("keeps byte, count and invalid UTF-8 failures transport-wide", () => {
    const file = filename();
    fs.writeFileSync(file, "{}\n".repeat(MAX_INTENTS + 1));
    expect(() => readStagedIntentBatch(file)).toThrow(/limit/);
    fs.writeFileSync(file, Buffer.alloc(MAX_INTENT_BYTES + 1, 0x20));
    expect(() => readStagedIntentBatch(file)).toThrow(/limit/);
    fs.writeFileSync(file, Buffer.from([0xff]));
    expect(() => readStagedIntentBatch(file)).toThrow(/transport_invalid/);
  });
});

describe("intent normalization", () => {
  it("accepts only the pool and claim bounds of a dispatch request", () => {
    const args = { pool: "default", max_claims: 1, max_dispatches: 1 };
    expect(normalizeDispatchParameters(args)).toEqual(args);
    for (const extra of [{ work_id: "preferred" }, { workflow: "preferred" }, { max_bytes: 1 }, { concurrency: 9 }]) expect(() => normalizeDispatchParameters({ ...args, ...extra })).toThrow();
    expect(() => normalizeDispatchParameters({ ...args, max_claims: 0 })).toThrow();
  });

  it("builds v4 Work operations with policy defaults", () => {
    const [work] = normalizeSubmitParameters({ nodes: [{ graph_id: "g", node_key: "n", payload: { plan: "stored" } }] }, policy);
    expect(work).toEqual({ kind: "Work", work_id: nodeId("g", "n"), graph_id: "g", node_key: "n", pool: "default", priority: 3, fairness_key: "", worker_profile: "worker", payload: { plan: "stored" }, depends_on: [] });
    expect(normalizeSubmitParameters({ nodes: [{ payload: {}, worker_profile: "other" }] }, policy)[0].worker_profile).toBe("other");
    for (const node of [
      { payload: {}, worker_profile: "missing" },
      { payload: {}, pool: "missing" },
      { payload: {}, expires_at: 1 },
      { payload: {}, execution_ref: "a".repeat(40) },
      { payload: {}, work_id: "forged" },
    ])
      expect(() => normalizeSubmitParameters({ nodes: [node] }, policy)).toThrow();
  });

  it("defaults independent roots to canonical payload hashes while allowing explicit distinct nodes", () => {
    const payload = { plan: "independent", effect_contract: { kind: "none" } };
    const [root] = normalizeSubmitParameters({ nodes: [{ payload }] }, policy);
    expect(root).toMatchObject({ graph_id: digest(payload), node_key: "root", work_id: nodeId(digest(payload), "root") });
    expect(normalizeSubmitParameters({ nodes: [{ payload: { effect_contract: { kind: "none" }, plan: "independent" } }] }, policy)[0]).toEqual(root);
    const [distinct] = normalizeSubmitParameters({ nodes: [{ node_key: "second", payload }] }, policy);
    expect(distinct).toMatchObject({ graph_id: root.graph_id, node_key: "second" });
    expect(distinct.work_id).not.toBe(root.work_id);
  });

  it("does not silently default explicit empty, null or undefined graph/node identities", () => {
    for (const field of ["graph_id", "node_key"]) {
      for (const value of ["", null, undefined]) expect(() => normalizeSubmitParameters({ nodes: [{ payload: {}, [field]: value }] }, policy)).toThrow();
    }
  });
});

describe("finish intents", () => {
  it("stages one outcome per Claim handle and reads it back", () => {
    const file = filename("work-queue.finish.jsonl");
    expect(readFinishIntents(file)).toEqual(new Map());
    expect(stageFinishIntent(file, "h1", "completed")).toBe(true);
    expect(stageFinishIntent(file, "h1", "completed")).toBe(false);
    expect(() => stageFinishIntent(file, "h1", "failed")).toThrow(/finish_conflict/);
    expect(() => stageFinishIntent(file, "h2", "cancelled")).toThrow();
    stageFinishIntent(file, "h2", "failed");
    expect(fs.readFileSync(file, "utf8")).toBe('{"claim_handle":"h1","outcome":"completed","version":4}\n{"claim_handle":"h2","outcome":"failed","version":4}\n');
    expect(readFinishIntents(file)).toEqual(
      new Map([
        ["h1", "completed"],
        ["h2", "failed"],
      ])
    );
  });

  it.each(['{"version":3,"claim_handle":"h1","outcome":"completed"}', '{"version":4,"claim_handle":"h1","outcome":"completed","keep_open":true}', "{bad"])("rejects malformed finish intent %j", line => {
    const file = filename();
    fs.writeFileSync(file, `${line}\n`);
    expect(() => readFinishIntents(file)).toThrow("work_queue_finish_intent_invalid");
  });
});
