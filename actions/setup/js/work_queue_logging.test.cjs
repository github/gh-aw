import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { createWorkQueueLogger, debugEnabled } = require("./work_queue_logging.cjs");
const { fakeGitHub, options } = require("./work_queue_store_checks.cjs");
const { genesis, submission, producer } = require("./work_queue_test_helpers.cjs");
const { publishWorkQueueRequest } = require("./work_queue_store.cjs");
const { planDispatch } = require("./work_queue_scheduler.cjs");
const { replayTransactions } = require("./work_queue_replay.cjs");

describe("privacy-preserving work queue logging", () => {
  let stderr;
  const output = () => stderr.mock.calls.map(call => call[0]).join("");

  beforeEach(() => {
    vi.stubEnv("DEBUG", "");
    vi.stubEnv("ACTIONS_RUNNER_DEBUG", "");
    vi.stubEnv("RUNNER_DEBUG", "");
    stderr = vi.spyOn(process.stderr, "write").mockReturnValue(true);
  });
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
  });

  it("is silent by default, including failures", () => {
    const logger = createWorkQueueLogger("store");
    logger.debug("request.publish.start", { attempts: 1 });
    logger.failure("request.publish.failed", new Error("secret"));
    expect(stderr).not.toHaveBeenCalled();
  });

  it.each([
    ["work-queue:*", true, true],
    ["work-queue:store", true, false],
    ["work-queue:store work-queue:dispatch", true, false],
    ["work-queue:store,work-queue:replay", true, true],
    ["*,-work-queue:replay", true, false],
    ["-work-queue:replay,*", true, false],
    ["other:*", false, false],
    ["work-queue:st.re", false, false],
  ])("filters namespaces with DEBUG=%s", (pattern, store, replay) => {
    vi.stubEnv("DEBUG", pattern);
    expect(debugEnabled("work-queue:store")).toBe(store);
    expect(debugEnabled("work-queue:replay")).toBe(replay);
  });

  it.each([
    ["ACTIONS_RUNNER_DEBUG", "true"],
    ["RUNNER_DEBUG", "1"],
  ])("supports %s=%s", (name, value) => {
    vi.stubEnv("DEBUG", "-work-queue:*");
    vi.stubEnv(name, value);
    createWorkQueueLogger("store").debug("enabled");
    expect(output()).toContain("[work-queue:store] enabled");
  });

  it("logs timestamps, counts and flags without serializing sensitive data", () => {
    vi.stubEnv("DEBUG", "work-queue:*");
    const secret = "private-token-payload";
    const toString = vi.fn(() => secret);
    createWorkQueueLogger("store").debug("candidate.checked", {
      operations: 2,
      reused: false,
      payload: secret,
      token: secret,
      nested: { secret },
      object: { toString },
      array: [secret],
      invalid: NaN,
      infinite: Infinity,
      negative: -1,
      fractional: 0.5,
      huge: Number.MAX_SAFE_INTEGER + 1,
    });
    expect(output()).toMatch(/\[\d{4}-\d{2}-\d{2}T.*Z\] \[work-queue:store\] candidate.checked operations=2 reused=false\n/);
    expect(output()).not.toContain(secret);
    expect(toString).not.toHaveBeenCalled();
  });

  it("does not log error messages, stack traces, causes, headers or responses", () => {
    vi.stubEnv("DEBUG", "work-queue:*");
    const secret = "private-error-request-body";
    const error = Object.assign(new Error(secret, { cause: new Error(secret) }), {
      status: 502,
      code: secret,
      response: { data: secret, headers: { authorization: secret } },
    });
    createWorkQueueLogger("store").failure("publication.failed", error);
    expect(output()).toContain("publication.failed failed=true http_status=502");
    expect(output()).not.toContain(secret);
    expect(output()).not.toContain("Stack");
  });

  it("does not invoke an error status getter or coerce invalid status values", () => {
    vi.stubEnv("DEBUG", "work-queue:*");
    const getter = vi.fn(() => {
      throw new Error("private");
    });
    const logger = createWorkQueueLogger("store");
    logger.failure("read.failed", Object.defineProperty({}, "status", { get: getter }));
    for (const status of ["private", 999, -1, NaN, null]) logger.failure("read.failed", { status });
    expect(getter).not.toHaveBeenCalled();
    expect(output()).not.toContain("http_status");
    expect(output()).not.toContain("private");
  });

  it("traces durable publication and idempotent recovery without Work payloads or identities", async () => {
    vi.stubEnv("DEBUG", "work-queue:store");
    const initial = [genesis()];
    const transaction = submission(initial, ["secret-task"], { graph: "private-graph" });
    const fake = fakeGitHub(initial);
    const first = await publishWorkQueueRequest(options(fake, transaction.request, producer));
    const second = await publishWorkQueueRequest(options(fake, transaction.request, producer));
    expect(first.persisted).toBe(true);
    expect(second.recovered).toBe(true);
    expect(output()).toContain("request.publish.complete");
    expect(output()).toContain("request.recovered");
    for (const secret of ["secret-task", "private-graph", "owner/repo", transaction.request.id, transaction.operations[0].work_id]) expect(output()).not.toContain(secret);
    expect(output()).not.toContain("[work-queue:replay]");
  });

  it("traces ambiguous-write recovery without changing publication semantics", async () => {
    vi.stubEnv("DEBUG", "work-queue:store");
    const initial = [genesis()];
    const transaction = submission(initial, ["private"]);
    const fake = fakeGitHub(initial);
    fake.state.ambiguousOnce = true;
    const result = await publishWorkQueueRequest(options(fake, transaction.request, producer));
    expect(result.recovered).toBe(true);
    expect(fake.state.updates).toBe(1);
    expect(output()).toContain("request.publish.write_failed failed=true http_status=502");
    expect(output()).toContain("request.publish.recovery conflict=false ambiguous=true");
    expect(output()).toContain("request.recovered");
    expect(output()).not.toContain("private");
  });

  it("reports no-grant scheduling without exposing pool or Work identities", () => {
    vi.stubEnv("DEBUG", "work-queue:scheduler");
    const state = replayTransactions([genesis()]);
    const decision = planDispatch(state, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 }, { requestId: "private-request", commitId: "private-commit", at: 10 });
    expect(decision.reason).toBe("no_work");
    expect(output()).toContain("selection.no_grant pending=0 ineligible=false");
    expect(output()).toContain("dispatch.plan.complete claims=0 assignments=0 no_grant=true");
    expect(output()).not.toContain("private");
    expect(output()).not.toContain("default");
  });
});
