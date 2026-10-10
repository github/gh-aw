import assert from "node:assert/strict";
import { describe, it } from "vitest";
import { applySettings, buildAWPolicy, parseSettings } from "./work_queue_settings.cjs";
import { defaultPolicy } from "./work_queue_policy.cjs";

describe("work_queue scheduling settings", () => {
  it("uses scheduling-only singleton defaults when absent or empty", () => {
    for (const value of [undefined, "", Buffer.alloc(0), {}, "{}", { pools: {}, retry: {}, accounting_weights: {} }]) {
      const settings = parseSettings(value);
      const policy = defaultPolicy({ principal: "1001", repository: "owner/repo" });
      const original = JSON.stringify(policy);
      const result = applySettings(policy, settings);
      assert.equal(result.mode, "weighted-priority");
      assert.equal(result.pools.default.native_limit, 16);
      assert.equal(result.limits.pending_nodes, 4096);
      assert.equal(result.pools.default.retry.max_attempts, 3);
      assert.equal(result.pools.default.retry.backoff_ms, 30000);
      assert.equal(result.pools.default.profiles.default.max_claims, 1);
      assert.equal(JSON.stringify(policy), original);
    }
  });

  it("applies overrides to every pool and inherits only approved routes", () => {
    const policy = defaultPolicy({ principal: "1001", repository: "owner/repo" });
    policy.pools.existing = structuredClone(policy.pools.default);
    const settings = parseSettings({
      concurrency: 32,
      pending_limit: 42,
      mode: "strict-priority",
      class_weights: [1, 2, 3, 4, 5],
      accounting_weights: { team: 3 },
      retry: { max_attempts: 4, backoff_seconds: 60 },
      pools: { default: { concurrency: 2 }, reviews: { per_account_limit: 1, retry: { max_attempts: 7 } } },
    });
    const result = applySettings(policy, settings);
    assert.equal(result.pools.default.native_limit, 2);
    assert.equal(result.pools.existing.native_limit, 32);
    assert.equal(result.pools.reviews.native_limit, 32);
    assert.equal(result.pools.reviews.per_account_limit, 1);
    assert.equal(Object.hasOwn(result.pools.default, "per_account_limit"), false);
    assert.deepEqual(result.pools.reviews.retry, { max_attempts: 7, backoff_ms: 60000 });
    assert.deepEqual(result.pools.reviews.profiles, policy.pools.default.profiles);
    assert.deepEqual(result.producers, policy.producers);
    assert.deepEqual(result.accounting_weights, { "": 1, team: 3 });
    assert.equal(result.limits.pending_nodes, 42);
    result.producers["1001"].pools[0] = "reviews";
    result.pools.reviews.profiles.default.workflow = ".github/workflows/changed.lock.yml";
    assert.equal(policy.producers["1001"].pools[0], "default");
    assert.equal(policy.pools.default.profiles.default.workflow, ".github/workflows/worker.lock.yml");
    assert.equal(result.pools.default.profiles.default.workflow, ".github/workflows/worker.lock.yml");
  });

  it("rejects malformed configuration rather than silently defaulting", () => {
    const cases = [
      [null, "work_queue"],
      [[], "work_queue"],
      [false, "work_queue"],
      [" ", "work_queue"],
      [{ concurrency: 0 }, "work_queue.concurrency"],
      [{ concurrency: null }, "work_queue.concurrency"],
      [{ concurrency: "16" }, "work_queue.concurrency"],
      [{ concurrency: 4097 }, "work_queue.concurrency"],
      [{ concurrency: 1.5 }, "work_queue.concurrency"],
      ['{"concurrency":1.5}', "work_queue.concurrency"],
      ['{"concurrency":1e1}', "work_queue.concurrency"],
      ['{"accounting_weights":{"\\ud800":1}}', "work_queue"],
      [Buffer.from([0xff]), "work_queue"],
      [{ pending_limit: 0 }, "work_queue.pending_limit"],
      [{ pending_limit: 4097 }, "work_queue.pending_limit"],
      [{ mode: null }, "work_queue.mode"],
      [{ class_weights: [1, 2, 3, 4, 0] }, "work_queue.class_weights[4]"],
      [{ accounting_weights: null }, "work_queue.accounting_weights"],
      [{ accounting_weights: { "": 2 } }, "work_queue.accounting_weights."],
      [{ retry: null }, "work_queue.retry"],
      [{ retry: { max_attempts: 17 } }, "work_queue.retry.max_attempts"],
      [{ retry: { backoff_seconds: 0 } }, "work_queue.retry.backoff_seconds"],
      [{ retry: { backoff_seconds: 3601 } }, "work_queue.retry.backoff_seconds"],
      [{ pools: [] }, "work_queue.pools"],
      [{ pools: { default: null } }, "work_queue.pools.default"],
      [{ pools: { default: { profiles: {} } } }, "work_queue.pools.default.profiles"],
      [{ pools: { default: { concurrency: 0 } } }, "work_queue.pools.default.concurrency"],
      [{ pools: { default: { per_account_limit: 0 } } }, "work_queue.pools.default.per_account_limit"],
      [{ pools: { default: { per_account_limit: 4097 } } }, "work_queue.pools.default.per_account_limit"],
      [{ pools: { default: { per_account_limit: null } } }, "work_queue.pools.default.per_account_limit"],
      [{ pools: { default: { per_account_limit: "1" } } }, "work_queue.pools.default.per_account_limit"],
      [{ producers: {} }, "work_queue.producers"],
      [{ principal: "1" }, "work_queue.principal"],
      ['{"concurrency":1,"concurrency":2}', "work_queue"],
    ];
    for (const [value, property] of cases) {
      assert.throws(
        () => parseSettings(value),
        error => error instanceof Error && error.message.includes(property) && error.message.includes(".github/workflows/aw.json")
      );
    }
  });

  it("accepts inclusive documented numeric bounds", () => {
    for (const settings of [
      { concurrency: 1, pending_limit: 1, retry: { max_attempts: 1, backoff_seconds: 1 }, pools: { default: { per_account_limit: 1 } } },
      { concurrency: 4096, pending_limit: 4096, retry: { max_attempts: 16, backoff_seconds: 3600 }, pools: { default: { per_account_limit: 4096 } } },
    ])
      assert.doesNotThrow(() => parseSettings(settings));
  });

  it("builds AW-managed singleton policies from host-approved targets only", () => {
    const policy = buildAWPolicy({
      repository: "owner/repo",
      ref: "a".repeat(40),
      workflows: ["zeta", "alpha"],
      settings: { concurrency: 2, pools: { reviews: { concurrency: 1, per_account_limit: 1 } } },
    });
    assert.equal(policy.authorization, "aw");
    assert.deepEqual(policy.producers, {});
    assert.equal(policy.pools.default.default_profile, "alpha");
    assert.equal(policy.pools.default.native_limit, 2);
    assert.equal(policy.pools.reviews.native_limit, 1);
    assert.equal(policy.pools.reviews.per_account_limit, 1);
    assert.deepEqual(policy.pools.reviews.profiles, policy.pools.default.profiles);
    for (const profile of Object.values(policy.pools.default.profiles)) {
      assert.equal(Object.hasOwn(profile, "principal"), false);
      assert.equal(profile.ref, "a".repeat(40));
      assert.equal(profile.max_claims, 1);
    }
    assert.throws(() => buildAWPolicy({ repository: "owner/repo", ref: "a".repeat(40), workflows: [] }), /AW must approve/);
    assert.throws(() => buildAWPolicy({ repository: "owner/repo", ref: "0".repeat(40), workflows: ["alpha"] }), /immutable/);
    assert.throws(() => buildAWPolicy({ repository: "owner/repo", ref: "a".repeat(40), workflows: ["../alpha"] }), /approved workflow IDs/);
    assert.throws(() => buildAWPolicy({ repository: "owner/repo", ref: "a".repeat(40), workflows: ["alpha"], settings: "" }), /work_queue/);
  });

  it("keeps Issues disabled unless explicitly enabled and preserves labels outside Policy authority", () => {
    for (const value of [undefined, {}, { issues: false }]) assert.equal(Object.hasOwn(parseSettings(value), "issues"), false);
    for (const [value, label] of [
      [true, "work"],
      [{}, "work"],
      [{ label: "tasks" }, "tasks"],
      [{ label: "a".repeat(33) }, "a".repeat(33)],
      [{ label: "😀".repeat(8) + "a" }, "😀".repeat(8) + "a"],
    ]) {
      const settings = parseSettings({ issues: value });
      assert.deepEqual(settings.issues, { label });
      const policy = defaultPolicy({ principal: "1001", repository: "owner/repo" });
      assert.deepEqual(applySettings(policy, settings), policy);
      const aw = buildAWPolicy({ repository: "owner/repo", ref: "a".repeat(40), workflows: ["worker"], settings: { issues: value } });
      assert.equal(Object.hasOwn(aw, "projectors"), false);
      assert.equal(Object.hasOwn(aw, "issues"), false);
    }
  });

  it("rejects malformed Issues options and unsupported projection keys", () => {
    for (const [value, property] of [
      [null, "issues"],
      [[], "issues"],
      [0, "issues"],
      ["true", "issues"],
      [{ label: null }, "issues.label"],
      [{ label: true }, "issues.label"],
      [{ label: "" }, "issues.label"],
      [{ label: " \t " }, "issues.label"],
      [{ label: "${{ vars.LABEL }}" }, "issues.label"],
      [{ label: "work\n" }, "issues.label"],
      [{ label: "work\u0085" }, "issues.label"],
      [{ label: "a".repeat(34) }, "issues.label"],
      [{ label: "😀".repeat(9) }, "issues.label"],
      [{ "tracking-label": "work" }, "issues.tracking-label"],
      [{ "status-label-prefix": "queue" }, "issues.status-label-prefix"],
      [{ routes: [] }, "issues.routes"],
    ]) {
      assert.throws(
        () => parseSettings({ issues: value }),
        error => error instanceof Error && error.message.includes(`work_queue.${property}`) && error.message.includes(".github/workflows/aw.json")
      );
    }
  });
});
