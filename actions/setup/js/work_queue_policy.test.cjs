import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { actorFromContext, policyAllowsWorker, poolEnv, readWorkQueuePolicyEnv, validateActor, validatePolicyEnv, workerWorkflow } = require("./work_queue_policy.cjs");

const context = { repo: { owner: "octo", repo: "queue" }, runId: 4242, runAttempt: 2 };
const env = () => ({
  version: 4,
  pending_limit: 30,
  issues: { label: "work", title_prefix: "[work queue]", orphaned_after_days: 30 },
  effect_scope: "octo/queue",
  workers: ["alpha", "beta"],
  pools: { default: { concurrency: 3, per_account_limit: 0, max_attempts: 3 }, reports: { concurrency: 1, per_account_limit: 1, max_attempts: 2 } },
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
  it("accepts the closed compiled shape as is and rejects everything else", () => {
    const value = env();
    expect(validatePolicyEnv(value)).toBe(value);
    expect(validatePolicyEnv({ ...env(), issues: false }).issues).toBe(false);
    const longest = "a".repeat(256 - workerWorkflow("").length);
    expect(Buffer.byteLength(workerWorkflow(longest))).toBe(256);
    expect(validatePolicyEnv({ ...env(), workers: [longest] }).workers).toEqual([longest]);
    const pool = patch => ({ ...env(), pools: { default: { ...env().pools.default, ...patch } } });
    for (const [value, code] of [
      [null, "policy_invalid"],
      [{ ...env(), version: 3 }, "unsupported_protocol"],
      [{ ...env(), mode: "strict-priority" }, "policy_invalid"],
      [{ ...env(), pending_limit: 4097 }, "policy_invalid"],
      [{ ...env(), pools: {} }, "policy_invalid"],
      [{ ...env(), pools: [] }, "policy_invalid"],
      [{ ...env(), pools: Object.fromEntries(Array.from({ length: 65 }, (_, index) => [`p${index}`, env().pools.default])) }, "policy_invalid"],
      [{ ...env(), pools: { ["p".repeat(257)]: env().pools.default } }, "ledger_invalid"],
      [{ ...env(), issues: { label: "work" } }, "policy_invalid"],
      [{ ...env(), issues: { label: "w".repeat(65), title_prefix: "[work queue]" } }, "ledger_invalid"],
      [{ ...env(), issues: { label: "work", title_prefix: "[work queue]", orphaned_after_days: 0 } }, "policy_invalid"],
      [{ ...env(), effect_scope: "octo" }, "policy_invalid"],
      [{ ...env(), effect_scope: "" }, "ledger_invalid"],
      [{ ...env(), workers: [] }, "policy_invalid"],
      [{ ...env(), workers: Array.from({ length: 257 }, (_, index) => `w${String(index).padStart(3, "0")}`) }, "policy_invalid"],
      [{ ...env(), workers: ["beta", "alpha"] }, "policy_invalid"],
      [{ ...env(), workers: ["alpha", "alpha"] }, "policy_invalid"],
      [{ ...env(), workers: ["../alpha"] }, "policy_invalid"],
      [{ ...env(), workers: ["a..b"] }, "policy_invalid"],
      [{ ...env(), workers: ["-alpha"] }, "policy_invalid"],
      [{ ...env(), workers: [`${longest}a`] }, "ledger_invalid"],
      [{ ...env(), workers: "alpha" }, "policy_invalid"],
      [{ ...env(), profiles: {} }, "policy_invalid"],
      [pool({ concurrency: 0 }), "policy_invalid"],
      [pool({ per_account_limit: -1 }), "policy_invalid"],
      [pool({ max_attempts: 17 }), "policy_invalid"],
      [pool({ logical_limit: 1 }), "policy_invalid"],
      // The retired expanded representation is not accepted.
      [pool({ default_profile: "alpha" }), "policy_invalid"],
      [pool({ profiles: { alpha: { workflow: ".github/workflows/alpha.lock.yml", effect_scope: "octo/queue" } } }), "policy_invalid"],
    ])
      expect(() => validatePolicyEnv(value), JSON.stringify(value)?.slice(0, 120)).toThrow(expect.objectContaining({ code }));
    expect(poolEnv(env(), "reports")).toEqual({ concurrency: 1, per_account_limit: 1, max_attempts: 2 });
    expect(() => poolEnv(env(), "other")).toThrow(expect.objectContaining({ code: "pool_invalid" }));
  });

  it("approves a worker only for configured pools of an existing policy", () => {
    expect(workerWorkflow("beta")).toBe(".github/workflows/beta.lock.yml");
    expect(policyAllowsWorker(env(), "reports", "beta")).toBe(true);
    expect(policyAllowsWorker(env(), "reports", "gamma")).toBe(false);
    expect(policyAllowsWorker(env(), "missing", "beta")).toBe(false);
    expect(policyAllowsWorker(env(), "toString", "beta")).toBe(false);
    expect(policyAllowsWorker(null, "default", "alpha")).toBe(false);
  });

  it("reads the complete or chunked compiled environment without reshaping it", () => {
    expect(readWorkQueuePolicyEnv({})).toBeNull();
    const text = JSON.stringify(env());
    expect(readWorkQueuePolicyEnv({ GH_AW_WORK_QUEUE_POLICY: text })).toEqual(env());
    const middle = Math.floor(text.length / 2);
    expect(readWorkQueuePolicyEnv({ GH_AW_WORK_QUEUE_POLICY_PARTS: "2", GH_AW_WORK_QUEUE_POLICY_0: text.slice(0, middle), GH_AW_WORK_QUEUE_POLICY_1: text.slice(middle) })).toEqual(env());
    expect(() => readWorkQueuePolicyEnv({ GH_AW_WORK_QUEUE_POLICY: '{"version":4,"version":4}' })).toThrow(expect.objectContaining({ code: "duplicate_key" }));
    expect(() => readWorkQueuePolicyEnv({ GH_AW_WORK_QUEUE_POLICY: JSON.stringify({ ...env(), workers: ["beta", "alpha"] }) })).toThrow(expect.objectContaining({ code: "policy_invalid" }));
  });
});
