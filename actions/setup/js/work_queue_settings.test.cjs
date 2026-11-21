import { describe, expect, it } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { buildPolicyEnv, parseSettings } = require("./work_queue_settings.cjs");

describe("aw.json work_queue settings", () => {
  it("defaults absent or empty sections", () => {
    for (const value of [undefined, "", Buffer.alloc(0), {}, "{}"]) expect(parseSettings(value)).toEqual({ concurrency: 16, pending_limit: 4096, retry: { max_attempts: 3 }, pools: {} });
  });

  it("applies pool overrides over top-level preferences", () => {
    expect(parseSettings({ concurrency: 4, pending_limit: 30, retry: { max_attempts: 5 }, pools: { reviews: { per_account_limit: 1 }, fast: { concurrency: 9, retry: { max_attempts: 1 } } } })).toEqual({
      concurrency: 4,
      pending_limit: 30,
      retry: { max_attempts: 5 },
      pools: { fast: { concurrency: 9, per_account_limit: 0, retry: { max_attempts: 1 } }, reviews: { concurrency: 4, per_account_limit: 1, retry: { max_attempts: 5 } } },
    });
  });

  it("rejects removed scheduler and backoff settings and out-of-range numbers", () => {
    for (const value of [
      { mode: "strict-priority" },
      { class_weights: [1, 1, 1, 1, 1] },
      { accounting_weights: { "": 1 } },
      { retry: { backoff_seconds: 30 } },
      { concurrency: 0 },
      { pending_limit: 4097 },
      { retry: { max_attempts: 17 } },
      { pools: { a: { per_account_limit: 0 } } },
      { pools: { a: { logical_limit: 1 } } },
      null,
      [],
      '{"concurrency":1,"concurrency":2}',
      '{"concurrency":1.5}',
    ])
      expect(() => parseSettings(value), JSON.stringify(value)).toThrow(/update \.github\/workflows\/aw\.json/);
  });

  it("parses Issues projection options", () => {
    expect(parseSettings({}).issues).toBeUndefined();
    expect(parseSettings({ issues: false }).issues).toBeUndefined();
    expect(parseSettings({ issues: true }).issues).toEqual({ label: "work", title_prefix: "[work queue]", orphaned_after_days: 30 });
    expect(parseSettings({ issues: { label: "queue" } }).issues).toEqual({ label: "queue", title_prefix: "[work queue]", orphaned_after_days: 30 });
    for (const issues of [{ label: "" }, { label: "${{ secrets.X }}" }, { title_prefix: "x".repeat(81) }, { orphaned_after_days: 0 }, { other: 1 }]) expect(() => parseSettings({ issues })).toThrow();
  });
});

describe("compiled v4 policy environment", () => {
  it("routes every pool to every approved worker with pool limits", () => {
    const env = buildPolicyEnv({ repository: "octo/queue", workflows: ["zeta", "alpha", "alpha"], settings: { concurrency: 2, pools: { reviews: { per_account_limit: 1 } }, issues: true } });
    const profiles = { alpha: { workflow: ".github/workflows/alpha.lock.yml", effect_scope: "octo/queue" }, zeta: { workflow: ".github/workflows/zeta.lock.yml", effect_scope: "octo/queue" } };
    expect(env).toEqual({
      version: 4,
      pending_limit: 4096,
      issues: { label: "work", title_prefix: "[work queue]", orphaned_after_days: 30 },
      pools: {
        default: { concurrency: 2, per_account_limit: 0, max_attempts: 3, default_profile: "alpha", profiles },
        reviews: { concurrency: 2, per_account_limit: 1, max_attempts: 3, default_profile: "alpha", profiles },
      },
    });
    expect(() => buildPolicyEnv({ repository: "octo/queue", workflows: ["../escape"] })).toThrow();
    expect(() => buildPolicyEnv({ repository: "octo/queue", workflows: [] })).toThrow();
  });

  it("makes this repository's smoke pool concurrency 3 effective without a ledger change", () => {
    const aw = JSON.parse(fs.readFileSync(path.resolve(import.meta.dirname, "../../../.github/workflows/aw.json"), "utf8"));
    const settings = { ...aw.work_queue };
    delete settings.accounting_weights;
    if (settings.retry) delete settings.retry.backoff_seconds;
    const env = buildPolicyEnv({ repository: "github/gh-aw", workflows: ["smoke-work-queue-worker"], settings });
    expect(env.pools["smoke-work-queue"].concurrency).toBe(3);
    expect(env.pending_limit).toBe(30);
  });
});
