import { describe, expect, it } from "vitest";

const { readWorkQueuePolicyConfig } = require("./work_queue_policy_config.cjs");

describe("protected queue Policy environment", () => {
  it("retains absent and complete legacy configuration", () => {
    expect(readWorkQueuePolicyConfig({})).toBe("");
    expect(readWorkQueuePolicyConfig({ GH_AW_WORK_QUEUE_POLICY: '{"authorization":"aw"}' })).toBe('{"authorization":"aw"}');
  });

  it("reconstructs large shared scheduling and routes exactly", () => {
    const policy = JSON.stringify({ authorization: "aw", routes: Array.from({ length: 500 }, (_, index) => ({ worker: `worker-${index}`, ref: "a".repeat(40), logical_contract: "b".repeat(64) })) });
    expect(policy.length).toBeGreaterThan(21 * 1024);
    const chunks = policy.match(/[\s\S]{1,12288}/g) ?? [];
    const environment = Object.fromEntries(chunks.map((chunk, index) => [`GH_AW_WORK_QUEUE_POLICY_${index}`, chunk]));
    environment.GH_AW_WORK_QUEUE_POLICY_PARTS = String(chunks.length);
    expect(readWorkQueuePolicyConfig(environment)).toBe(policy);
    expect(JSON.parse(readWorkQueuePolicyConfig(environment))).toEqual(JSON.parse(policy));
  });

  it.each(["", "0", "01", "NaN", "257"])("rejects invalid chunk count %j", count => {
    expect(() => readWorkQueuePolicyConfig({ GH_AW_WORK_QUEUE_POLICY_PARTS: count })).toThrow(/from 1 to 256/);
  });

  it("rejects incomplete, conflicting and misnumbered configuration", () => {
    expect(() => readWorkQueuePolicyConfig({ GH_AW_WORK_QUEUE_POLICY_0: "{}" })).toThrow(/require/);
    expect(() => readWorkQueuePolicyConfig({ GH_AW_WORK_QUEUE_POLICY: "{}", GH_AW_WORK_QUEUE_POLICY_PARTS: "1", GH_AW_WORK_QUEUE_POLICY_0: "{}" })).toThrow(/conflicting/);
    expect(() => readWorkQueuePolicyConfig({ GH_AW_WORK_QUEUE_POLICY_PARTS: "2", GH_AW_WORK_QUEUE_POLICY_0: "{}" })).toThrow(/missing or extra/);
    expect(() => readWorkQueuePolicyConfig({ GH_AW_WORK_QUEUE_POLICY_PARTS: "1", GH_AW_WORK_QUEUE_POLICY_1: "{}" })).toThrow(/chunk 0/);
    expect(() => readWorkQueuePolicyConfig({ GH_AW_WORK_QUEUE_POLICY_PARTS: "1", GH_AW_WORK_QUEUE_POLICY_0: "{}", GH_AW_WORK_QUEUE_POLICY_1: "{}" })).toThrow(/missing or extra/);
  });
});
