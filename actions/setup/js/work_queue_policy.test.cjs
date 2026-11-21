import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { actorFromContext, poolEnv, readWorkQueuePolicyEnv, validateActor, validatePolicyEnv } = require("./work_queue_policy.cjs");

const context = { repo: { owner: "octo", repo: "queue" }, runId: 4242, runAttempt: 2 };
const env = () => ({
  version: 4,
  pending_limit: 30,
  issues: { label: "work", title_prefix: "[work queue]", orphaned_after_days: 30 },
  pools: { default: { concurrency: 3, per_account_limit: 0, max_attempts: 3, default_profile: "worker", profiles: { worker: { workflow: ".github/workflows/worker.lock.yml", effect_scope: "octo/queue" } } } },
});

describe("queue actors", () => {
  it("builds the actor from the job's own context without API calls", () => {
    expect(actorFromContext(context, "dispatcher", { GITHUB_WORKFLOW_REF: "octo/queue/.github/workflows/dispatch.lock.yml@refs/heads/main" })).toEqual({
      role: "dispatcher",
      repository: "octo/queue",
      workflow: ".github/workflows/dispatch.lock.yml",
      run_id: "4242",
      run_attempt: 2,
    });
    expect(actorFromContext({ repo: context.repo }, "administrator", {})).toEqual({ role: "administrator", repository: "octo/queue" });
    expect(actorFromContext({ repo: context.repo }, "reconciler", { GITHUB_RUN_ID: "9", GITHUB_RUN_ATTEMPT: "3", GITHUB_WORKFLOW_REF: "other/repo/.github/workflows/x.yml@main" })).toEqual({
      role: "reconciler",
      repository: "octo/queue",
      run_id: "9",
      run_attempt: 3,
    });
    expect(() => actorFromContext({ repo: context.repo }, "worker", {})).toThrow(expect.objectContaining({ code: "request_invalid" }));
  });

  it("validates closed actors", () => {
    expect(validateActor({ role: "observer", repository: "a/b" })).toBeTruthy();
    for (const actor of [
      { role: "god", repository: "a/b" },
      { role: "producer", repository: "a" },
      { role: "producer", repository: "a/b", run_id: "1" },
      { role: "producer", repository: "a/b", run_id: "01", run_attempt: 1 },
      { role: "producer", repository: "a/b", run_id: "1", run_attempt: 0 },
      { role: "producer", repository: "a/b", workflow: "worker.yml" },
      { role: "producer", repository: "a/b", principal: "1" },
    ])
      expect(() => validateActor(actor)).toThrow();
  });
});

describe("compiled policy environment", () => {
  it("accepts the closed v4 shape and rejects everything else", () => {
    expect(validatePolicyEnv(env())).toEqual(env());
    expect(validatePolicyEnv({ ...env(), issues: false }).issues).toBe(false);
    const broken = [
      [{ ...env(), version: 3 }, "unsupported_protocol"],
      [{ ...env(), mode: "strict-priority" }, "policy_invalid"],
      [{ ...env(), pending_limit: 4097 }, "policy_invalid"],
      [{ ...env(), pools: {} }, "policy_invalid"],
      [{ ...env(), issues: { label: "work" } }, "policy_invalid"],
    ];
    const pool = patch => ({ ...env(), pools: { default: { ...env().pools.default, ...patch } } });
    broken.push(
      [pool({ concurrency: 0 }), "policy_invalid"],
      [pool({ per_account_limit: -1 }), "policy_invalid"],
      [pool({ max_attempts: 17 }), "policy_invalid"],
      [pool({ default_profile: "missing" }), "policy_invalid"],
      [pool({ logical_limit: 1 }), "policy_invalid"],
      [pool({ profiles: { worker: { workflow: ".github/workflows/worker.yml", effect_scope: "octo/queue" } } }), "policy_invalid"],
      [pool({ profiles: { worker: { workflow: ".github/workflows/worker.lock.yml", effect_scope: "octo/queue", ref: "a".repeat(40) } } }), "policy_invalid"]
    );
    for (const [value, code] of broken) expect(() => validatePolicyEnv(value)).toThrow(expect.objectContaining({ code }));
    expect(poolEnv(env(), "default").concurrency).toBe(3);
    expect(() => poolEnv(env(), "other")).toThrow(expect.objectContaining({ code: "pool_invalid" }));
  });

  it("reads the complete or chunked environment", () => {
    expect(readWorkQueuePolicyEnv({})).toBeNull();
    const text = JSON.stringify(env());
    expect(readWorkQueuePolicyEnv({ GH_AW_WORK_QUEUE_POLICY: text })).toEqual(env());
    const middle = Math.floor(text.length / 2);
    expect(readWorkQueuePolicyEnv({ GH_AW_WORK_QUEUE_POLICY_PARTS: "2", GH_AW_WORK_QUEUE_POLICY_0: text.slice(0, middle), GH_AW_WORK_QUEUE_POLICY_1: text.slice(middle) })).toEqual(env());
    expect(() => readWorkQueuePolicyEnv({ GH_AW_WORK_QUEUE_POLICY: '{"version":4,"version":4}' })).toThrow(expect.objectContaining({ code: "duplicate_key" }));
  });
});
