import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const telemetry = require("./work_queue_telemetry.cjs");
const sender = require("./send_otlp_span.cjs");
const otlp = require("./otlp.cjs");
const store = require("./work_queue_store.cjs");
const h = require("./work_queue_test_helpers.cjs");
const { parseTransactionLog, replayTransactions } = require("./work_queue_replay.cjs");
const { planDispatch } = require("./work_queue_scheduler.cjs");

const PREFIX = "gh-aw.work_queue.";
const queue = fake => ({ githubClient: fake.githubClient, owner: "owner", repo: "repo" });
const span = payload => payload.resourceSpans[0].scopeSpans[0].spans[0];
const attributes = payload => Object.fromEntries(span(payload).attributes.map(({ key, value }) => [key, Object.values(value)[0]]));
const twoWorks = () => h.ledgerText(h.submit([], [h.work("private task a"), h.work("private task b")]));
const dispatchRequest = { kind: "dispatch_next", parameters: h.dispatchParameters({ concurrency: 1 }) };
const plan = (state, request) => planDispatch(state, request.parameters, request.id).operations;

describe("work queue OpenTelemetry", () => {
  let mirrors;
  let exports;

  beforeEach(() => {
    mirrors = [];
    exports = [];
    vi.stubEnv("GITHUB_AW_OTEL_TRACE_ID", "a".repeat(32));
    vi.stubEnv("GITHUB_AW_OTEL_PARENT_SPAN_ID", "b".repeat(16));
    vi.stubEnv("GH_AW_OTLP_ENDPOINTS", JSON.stringify([{ url: "https://otel.example.com" }]));
    vi.spyOn(sender, "appendToOTLPJSONL").mockImplementation(payload => mirrors.push(payload));
    vi.spyOn(sender, "sendOTLPToAllEndpoints").mockImplementation(async (_endpoints, payload) => {
      exports.push(payload);
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
  });

  it("exports a read snapshot of counts without task content or identifiers", async () => {
    const result = await store.readWorkQueueLog(queue(h.fakeQueueGitHub(twoWorks())));
    expect(mirrors).toHaveLength(1);
    expect(exports).toEqual(mirrors);
    expect(span(mirrors[0])).toMatchObject({ name: "work_queue.read.run", traceId: "a".repeat(32), parentSpanId: "b".repeat(16), status: { code: 1 } });
    expect(attributes(mirrors[0])).toMatchObject({ [`${PREFIX}operation`]: "read", [`${PREFIX}snapshot.present`]: true, [`${PREFIX}work.total`]: 2, [`${PREFIX}work.available`]: 2, [`${PREFIX}claims.open`]: 0 });
    const encoded = JSON.stringify(mirrors);
    for (const work of result.state.works.values()) {
      expect(encoded).not.toContain(work.work_id);
      expect(encoded).not.toContain(work.payload.task);
    }
  });

  it.each(["", "invalid", "0".repeat(32)])("does not export without valid trace context (%s)", async traceId => {
    vi.stubEnv("GITHUB_AW_OTEL_TRACE_ID", traceId);
    expect((await store.readWorkQueueLog(queue(h.fakeQueueGitHub(twoWorks())))).state.works.size).toBe(2);
    expect(mirrors).toHaveLength(0);
  });

  it("emits one publication span with committed transitions and recovered retries", async () => {
    const fake = h.fakeQueueGitHub(twoWorks());
    const options = { ...queue(fake), actor: h.dispatcher, request: dispatchRequest, operationsFor: plan };
    await store.publishWorkQueueRequest(options);
    expect(mirrors).toHaveLength(1);
    expect(span(mirrors[0]).name).toBe("work_queue.publish.run");
    expect(attributes(mirrors[0])).toMatchObject({
      [`${PREFIX}request.kind`]: "dispatch_next",
      [`${PREFIX}publication.attempts`]: 1,
      [`${PREFIX}publication.commits`]: 1,
      [`${PREFIX}committed.Claim`]: 1,
      [`${PREFIX}work.claimed`]: 1,
      [`${PREFIX}dispatches.reserved`]: 1,
    });
    await store.publishWorkQueueRequest(options);
    expect(attributes(mirrors[1])).toMatchObject({ [`${PREFIX}publication.commits`]: 0, [`${PREFIX}publication.recovered`]: 1 });
    expect(attributes(mirrors[1])).not.toHaveProperty(`${PREFIX}committed.Claim`);
  });

  it("counts compare-and-swap conflicts", async () => {
    const fake = h.fakeQueueGitHub(twoWorks());
    fake.state.beforeUpdate = () => fake.install(h.ledgerText(h.dispatchNext(parseTransactionLog(fake.contents()), { concurrency: 2 }, { ...h.dispatcher, run_id: "777" }).log));
    await store.publishWorkQueueRequest({ ...queue(fake), actor: h.dispatcher, request: dispatchRequest, operationsFor: plan });
    expect(attributes(mirrors[0])).toMatchObject({ [`${PREFIX}publication.attempts`]: 2, [`${PREFIX}publication.conflict`]: 1, [`${PREFIX}publication.commits`]: 0 });
  });

  it("keeps exporter failures non-fatal", async () => {
    vi.spyOn(otlp, "logSpan").mockRejectedValue(new Error("https://secret.example.com?token=x"));
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    expect((await store.readWorkQueueLog(queue(h.fakeQueueGitHub(twoWorks())))).state.works.size).toBe(2);
    expect(warn.mock.calls.flat().join(" ")).not.toContain("secret");
  });

  it("summarizes projection counts only", () => {
    const planned = h.dispatchNext(h.submit([], [h.work("a"), h.work("b")]), { concurrency: 1 });
    expect(telemetry.snapshotAttributes(replayTransactions(planned.log))).toEqual({
      "work.total": 2,
      "work.available": 1,
      "work.claimed": 1,
      "work.completed": 0,
      "work.cancelled": 0,
      "claims.open": 1,
      "dispatches.reserved": 1,
      "dispatches.started": 0,
      "dispatches.bound": 0,
    });
  });
});
