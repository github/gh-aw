import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const telemetry = require("./work_queue_telemetry.cjs");
const otlp = require("./otlp.cjs");
const sender = require("./send_otlp_span.cjs");
const store = require("./work_queue_store.cjs");
const { fakeGitHub, options } = require("./work_queue_store_checks.cjs");
const { genesis, submission, grant, dispatcher, administrator, operationCommit } = require("./work_queue_test_helpers.cjs");
const { newRequest, replayTransactions, serializeProjection } = require("./work_queue_replay.cjs");
const { canonical } = require("./work_queue_codec.cjs");
const { launchAssignment } = require("./work_queue_dispatch.cjs");
const { reconcileDispatch, reconcileQueue } = require("./work_queue_reconciler.cjs");
const { queueFixture, DISPATCHER, REF, REPOSITORY } = require("./work_queue_lifecycle.test_helpers.cjs");
const PREFIX = "gh-aw.work_queue.";
const queueOptions = { owner: "owner", repo: "repo" };

function fixture() {
  const log = [genesis()];
  log.push(submission(log, ["private task a", "private task b"]));
  return log;
}

function span(payload) {
  return payload.resourceSpans[0].scopeSpans[0].spans[0];
}

function attributes(payload) {
  return Object.fromEntries(span(payload).attributes.map(({ key, value }) => [key, Object.values(value)[0]]));
}

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

  it("exports a correlated read snapshot without task content or identifiers", async () => {
    const fake = fakeGitHub(fixture());
    const result = await store.readWorkQueueLog({ ...queueOptions, githubClient: fake.githubClient });
    expect(result.state.works.size).toBe(2);
    expect(mirrors).toHaveLength(1);
    expect(exports).toEqual(mirrors);
    expect(span(mirrors[0])).toMatchObject({
      name: "work_queue.read.run",
      traceId: "a".repeat(32),
      parentSpanId: "b".repeat(16),
      status: { code: 1 },
    });
    expect(attributes(mirrors[0])).toMatchObject({
      [`${PREFIX}operation`]: "read",
      [`${PREFIX}snapshot.present`]: true,
      [`${PREFIX}initialized`]: true,
      [`${PREFIX}work.total`]: 2,
      [`${PREFIX}work.available`]: 2,
      [`${PREFIX}claims.open`]: 0,
      [`${PREFIX}reservations.active`]: 0,
    });
    const encoded = JSON.stringify(mirrors);
    for (const work of result.state.works.values()) {
      expect(encoded).not.toContain(work.work_id);
      expect(encoded).not.toContain(work.payload.task);
    }
    expect(Number(span(mirrors[0]).endTimeUnixNano)).toBeGreaterThanOrEqual(Number(span(mirrors[0]).startTimeUnixNano));
  });

  it("mirrors spans even when no OTLP backend is configured", async () => {
    vi.stubEnv("GH_AW_OTLP_ENDPOINTS", "[]");
    vi.stubEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "");
    await store.readWorkQueueLog({ ...queueOptions, githubClient: fakeGitHub().githubClient });
    expect(mirrors).toHaveLength(1);
    expect(exports).toHaveLength(0);
    expect(attributes(mirrors[0])).toMatchObject({ [`${PREFIX}initialized`]: false, [`${PREFIX}work.total`]: 0 });
  });

  it.each(["", "invalid", "0".repeat(32)])("does not export or alter execution without valid trace context (%s)", async traceId => {
    vi.stubEnv("GITHUB_AW_OTEL_TRACE_ID", traceId);
    const result = await store.readWorkQueueLog({ ...queueOptions, githubClient: fakeGitHub(fixture()).githubClient });
    expect(result.state.works.size).toBe(2);
    expect(mirrors).toHaveLength(0);
  });

  it("emits one publication span with durable transitions, not nested read spans", async () => {
    const fake = fakeGitHub(fixture());
    const request = newRequest("grant", "dispatch_next", dispatcher, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 });
    const result = await store.publishWorkQueueRequest(options(fake, request));
    expect(result.persisted).toBe(true);
    expect(mirrors).toHaveLength(1);
    expect(span(mirrors[0]).name).toBe("work_queue.publish.run");
    expect(attributes(mirrors[0])).toMatchObject({
      [`${PREFIX}request.kind`]: "dispatch_next",
      [`${PREFIX}reason`]: "claim_budget_reached",
      [`${PREFIX}publication.attempts`]: 1,
      [`${PREFIX}publication.commits`]: 1,
      [`${PREFIX}committed.Claim`]: 1,
      [`${PREFIX}work.available`]: 1,
      [`${PREFIX}work.claimed`]: 1,
      [`${PREFIX}claims.open`]: 1,
      [`${PREFIX}reservations.active`]: 1,
      [`${PREFIX}reservations.unbound`]: 1,
    });
    await store.publishWorkQueueRequest(options(fake, request));
    expect(attributes(mirrors[1])).toMatchObject({
      [`${PREFIX}publication.commits`]: 0,
      [`${PREFIX}publication.recovered`]: 1,
      [`${PREFIX}claims.open`]: 1,
    });
    expect(attributes(mirrors[1])).not.toHaveProperty(`${PREFIX}committed.Claim`);
  });

  it("reports a healthy no-grant decision without claiming a publication", async () => {
    const fake = fakeGitHub([genesis()]);
    const request = newRequest("empty", "dispatch_next", dispatcher, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 });
    await store.publishWorkQueueRequest(options(fake, request));
    expect(attributes(mirrors[0])).toMatchObject({
      [`${PREFIX}reason`]: "no_work",
      [`${PREFIX}publication.commits`]: 0,
      [`${PREFIX}claims.open`]: 0,
    });
    expect(span(mirrors[0]).status.code).toBe(1);
  });

  it("counts conflicts and observes only the refreshed committed prefix", async () => {
    const fake = fakeGitHub(fixture());
    fake.state.beforeUpdate = async ({ log, install }) => {
      install([...log, grant(log, { id: "competing", at: 100 }).commit]);
    };
    const request = newRequest("contended", "dispatch_next", dispatcher, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 });
    await store.publishWorkQueueRequest(options(fake, request));
    expect(mirrors).toHaveLength(1);
    expect(attributes(mirrors[0])).toMatchObject({
      [`${PREFIX}publication.attempts`]: 2,
      [`${PREFIX}publication.conflict`]: 1,
      [`${PREFIX}publication.commits`]: 1,
      [`${PREFIX}committed.Claim`]: 1,
      [`${PREFIX}claims.open`]: 2,
    });
  });

  it("distinguishes ambiguous accepted writes from new commits", async () => {
    const fake = fakeGitHub(fixture());
    fake.state.ambiguousOnce = true;
    const request = newRequest("ambiguous", "dispatch_next", dispatcher, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 });
    const result = await store.publishWorkQueueRequest(options(fake, request));
    expect(result.recovered).toBe(true);
    expect(attributes(mirrors[0])).toMatchObject({
      [`${PREFIX}publication.ambiguous`]: 1,
      [`${PREFIX}publication.recovered`]: 1,
      [`${PREFIX}publication.commits`]: 0,
      [`${PREFIX}claims.open`]: 1,
    });
  });

  it("reports exhausted retries without exporting uncommitted Claims", async () => {
    const fake = fakeGitHub(fixture());
    vi.spyOn(fake.githubClient.rest.git, "updateRef").mockRejectedValue(Object.assign(new Error("private provider response"), { status: 409 }));
    const request = newRequest("exhausted", "dispatch_next", dispatcher, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 });
    await expect(store.publishWorkQueueRequest(options(fake, request, dispatcher, { maxRetries: 1 }))).rejects.toThrow("publication_unresolved");
    expect(span(mirrors[0]).status.code).toBe(2);
    expect(attributes(mirrors[0])).toMatchObject({
      [`${PREFIX}error.type`]: "publication_unresolved",
      [`${PREFIX}publication.attempts`]: 2,
      [`${PREFIX}publication.conflict`]: 2,
      [`${PREFIX}publication.commits`]: 0,
      [`${PREFIX}claims.open`]: 0,
      [`${PREFIX}work.available`]: 2,
    });
    expect(JSON.stringify(mirrors)).not.toContain("private provider response");
  });

  it("reports inaccessible queues as errors, not empty healthy snapshots", async () => {
    const fake = fakeGitHub(fixture());
    fake.state.visibility = false;
    await expect(store.readWorkQueueLog({ ...queueOptions, githubClient: fake.githubClient })).rejects.toThrow("repository_unavailable");
    expect(span(mirrors[0]).status.code).toBe(2);
    expect(attributes(mirrors[0])).toMatchObject({ [`${PREFIX}snapshot.present`]: false, [`${PREFIX}error.type`]: "repository_unavailable" });
    expect(attributes(mirrors[0])).not.toHaveProperty(`${PREFIX}work.total`);
  });

  it("instruments compaction without changing queue accounting", async () => {
    const fake = fakeGitHub(fixture(), { immutableIds: true });
    const before = telemetry.snapshotAttributes(replayTransactions(fake.log()), 200);
    await store.compactWorkQueue({ ...options(fake, undefined, administrator), now: () => 200 });
    expect(mirrors).toHaveLength(1);
    expect(span(mirrors[0]).name).toBe("work_queue.compact.run");
    expect(attributes(mirrors[0])).toMatchObject({
      [`${PREFIX}publication.attempts`]: 1,
      [`${PREFIX}publication.commits`]: 1,
      [`${PREFIX}work.available`]: 2,
    });
    expect(telemetry.snapshotAttributes(replayTransactions(fake.log()), 200)).toEqual(before);
  });

  it("keeps telemetry failures non-fatal and does not expose exporter errors", async () => {
    vi.spyOn(otlp, "logSpan").mockRejectedValue(new Error("https://private-exporter/?token=credential"));
    const warning = vi.spyOn(console, "warn").mockImplementation(() => {});
    const result = await store.readWorkQueueLog({ ...queueOptions, githubClient: fakeGitHub(fixture()).githubClient });
    expect(result.state.works.size).toBe(2);
    expect(warning).toHaveBeenCalledWith("[work-queue] Telemetry emission failed; queue state and publication are unchanged.");
    expect(JSON.stringify(warning.mock.calls)).not.toContain("credential");
    const error = Object.assign(new Error("private data"), { code: "ledger_invalid" });
    await expect(
      telemetry.observeQueueOperation("read", queueOptions, async () => {
        throw error;
      })
    ).rejects.toBe(error);
  });

  it("uses stable queue identities and isolates concurrent queue operations", async () => {
    const run = queue =>
      telemetry.observeQueueOperation("read", queue, async () => {
        await Promise.resolve();
        return { state: replayTransactions(fixture()) };
      });
    await Promise.all([run(queueOptions), run({ owner: "other", repo: "repo" })]);
    await run({ owner: "OWNER", repo: "REPO" });
    const identities = mirrors.map(payload => attributes(payload)[`${PREFIX}queue.id`]);
    expect(identities[0]).not.toBe(identities[1]);
    expect(identities[0]).toBe(identities[2]);
    expect(identities[0]).toMatch(/^[a-f0-9]{64}$/);
    expect(mirrors.every(payload => attributes(payload)[`${PREFIX}work.total`] === 2)).toBe(true);
  });

  it.each(["launch_unresolved", "run_binding_conflict", "rejected"])("marks non-throwing unhealthy lifecycle outcomes as errors (%s)", async state => {
    const result = { state, released: false };
    expect(await telemetry.observeQueueOperation("dispatch", queueOptions, async () => result)).toBe(result);
    expect(span(mirrors[0]).status.code).toBe(2);
    expect(attributes(mirrors[0])[`${PREFIX}outcome`]).toBe(state);
  });

  it("reports accepted launches as activation pending rather than bound or unhealthy", async () => {
    const result = { state: "activation_pending", released: false, dispatched: true };
    await telemetry.observeQueueOperation("dispatch", queueOptions, async () => result);
    expect(span(mirrors[0]).status.code).not.toBe(2);
    expect(attributes(mirrors[0])[`${PREFIX}outcome`]).toBe("activation_pending");
  });

  it("reports reconciliation failures even when the batch returns normally", async () => {
    const result = { results: [{ state: "bound" }, { state: "launch_unresolved" }] };
    await telemetry.observeQueueOperation("reconcile", queueOptions, async () => result);
    expect(span(mirrors[0]).status.code).toBe(2);
    expect(attributes(mirrors[0])).toMatchObject({ [`${PREFIX}reconciliation.checked`]: 2, [`${PREFIX}reconciliation.unresolved`]: 1 });
  });

  it("wires dispatch and reconciliation entry points to workflow traces", async () => {
    const staged = { config: { staged: true }, context: { repo: { owner: "owner", repo: "repo" } } };
    await reconcileDispatch(staged);
    await reconcileQueue(staged);
    const fixture = queueFixture({ bound: true });
    const options = {
      assignment: fixture.assignment,
      githubClient: fixture.githubClient,
      dispatchClient: fixture.githubClient,
      validateDispatchCredential: fixture.validateDispatchCredential,
      context: fixture.dispatcherContext,
      workflowRef: `${REPOSITORY}/${DISPATCHER}@${REF}`,
      readWorkQueueLog: fixture.readWorkQueueLog,
      publishWorkQueueRequest: fixture.publishWorkQueueRequest,
      config: { work_queue_workflows: ["worker"], aw_context_workflows: ["worker"], max: 3 },
      now: fixture.at,
      sleepFn: async () => {},
    };
    await expect(launchAssignment(options, fixture.assignment)).resolves.toMatchObject({ state: "bound", dispatched: false });
    expect(mirrors.map(payload => span(payload).name)).toEqual(["work_queue.reconcile_dispatch.run", "work_queue.reconcile.run", "work_queue.dispatch.run"]);
    expect(attributes(mirrors[2])[`${PREFIX}outcome`]).toBe("bound");
    expect(mirrors.every(payload => span(payload).traceId === "a".repeat(32))).toBe(true);
  });

  it("reports paused controls, retained capacity, and expired/backoff Work without mutating replay", () => {
    const log = fixture();
    log.push(grant(log).commit);
    log.push(operationCommit(log, "pause", "control", [{ kind: "Control", control: "grants_paused", value: true, reason: "operator_pause" }], administrator, 20));
    const state = replayTransactions(log);
    const available = [...state.works.values()].find(work => work.state === "available");
    available.retry_not_before = 200;
    const dispatch = [...state.dispatches.values()][0];
    dispatch.state = "unresolved";
    state.policy.pools.default.logical_limit = 1;
    state.policy.pools.default.native_limit = 1;
    const before = canonical(serializeProjection(state));
    expect(telemetry.snapshotAttributes(state, 100)).toMatchObject({
      grants_paused: true,
      "work.available": 1,
      "work.retry_delayed": 1,
      "work.oldest_available_age_ms": 99,
      "claims.open": 1,
      "reservations.active": 1,
      "reservations.unresolved": 1,
      "capacity.logical_saturated_pools": 1,
      "capacity.native_saturated_pools": 1,
    });
    expect(canonical(serializeProjection(state))).toBe(before);
    available.expires_at = 100;
    expect(telemetry.snapshotAttributes(state, 100)).toMatchObject({ "work.available": 0, "work.cancelled": 1, "work.expired": 1, "claims.open": 1, "reservations.active": 1 });
    expect(available.state).toBe("available");
    available.barrier = "pending";
    expect(telemetry.snapshotAttributes(state, 100)).toMatchObject({ "work.expired": 0, "delivery.pending": 1 });
    available.barrier = "failed";
    const claim = [...state.claims.values()][0];
    claim.keep_open = true;
    claim.barrier = "pending";
    expect(telemetry.snapshotAttributes(state, 100)).toMatchObject({ "delivery.pending": 1, "delivery.failed": 1 });
  });
});
